package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// helperEnviron marks the test binary as standing in for the command narc
// wraps. Under -race the helper is race-instrumented too, and the race runtime
// otherwise sleeps for a second before every clean exit.
var helperEnviron = []string{"NARC_TEST_HELPER=1", "GORACE=atexit_sleep_ms=0"}

// helperCommand returns the run arguments that wrap the test binary, which
// then performs action (see TestHelperProcess).
func helperCommand(action ...string) []string {
	return append([]string{"--", os.Args[0], "-test.run=^TestHelperProcess$", "--"}, action...)
}

// TestHelperProcess is not a test of its own. The run tests wrap the test
// binary rather than a real tool, so they need nothing installed on the
// runner, and this is what the binary does when started that way.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("NARC_TEST_HELPER") != "1" {
		t.Skip("only runs as the command wrapped by the run tests")
	}

	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	if len(args) < 2 {
		os.Exit(2)
	}

	switch args[1] {
	case "print":
		fmt.Println("helper stdout")
		os.Exit(0)
	case "exit":
		code, _ := strconv.Atoi(args[2])
		os.Exit(code)
	case "get":
		// The proxy is named explicitly because http.ProxyFromEnvironment never
		// proxies a loopback address, which is where the test target listens.
		proxyURL, err := url.Parse(os.Getenv("HTTP_PROXY"))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}
		resp, err := client.Get(args[2])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		_ = resp.Body.Close()
		os.Exit(0)
	}
	os.Exit(2)
}

// rulesPath is where a run with no --output writes its rules under home.
func rulesPath(home string) string {
	return filepath.Join(home, ".narc", "access_rules.json")
}

// readRules fails the test unless path holds a JSON list of rules.
func readRules(t *testing.T, path string) []map[string]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read rules: %v", err)
	}
	var rules []map[string]string
	if err := json.Unmarshal(data, &rules); err != nil {
		t.Fatalf("rules are not a JSON list: %v", err)
	}
	return rules
}

func TestRunCommand(t *testing.T) {
	t.Parallel()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(target.Close)
	probe := target.URL + "/probe"

	tests := []struct {
		name string
		// args is built per case, since some name files under the case's home.
		args      func(home string) []string
		cancelled bool
		check     func(t *testing.T, home string, r result)
	}{
		{
			name: "default wraps the command, hides its stdout and writes the rules",
			args: func(string) []string { return append([]string{"run", "--port", "0"}, helperCommand("print")...) },
			check: func(t *testing.T, home string, r result) {
				if r.err != nil {
					t.Fatalf("err = %v, stderr = %q", r.err, r.stderr)
				}
				if strings.Contains(r.stdout, "helper stdout") {
					t.Errorf("stdout shows the command's output without --show-output: %q", r.stdout)
				}
				if strings.Contains(r.stderr, "level=DEBUG") {
					t.Errorf("stderr has debug lines without --debug: %q", r.stderr)
				}
				readRules(t, rulesPath(home))
			},
		},
		{
			name: "show-output passes the command's stdout through",
			args: func(string) []string {
				return append([]string{"run", "--port", "0", "--show-output"}, helperCommand("print")...)
			},
			check: func(t *testing.T, _ string, r result) {
				if r.err != nil {
					t.Fatalf("err = %v, stderr = %q", r.err, r.stderr)
				}
				if !strings.Contains(r.stdout, "helper stdout") {
					t.Errorf("stdout = %q, want the command's output", r.stdout)
				}
			},
		},
		{
			name: "output writes the rules to the given path",
			args: func(home string) []string {
				return append([]string{"run", "--port", "0", "--output", filepath.Join(home, "rules.json")}, helperCommand("print")...)
			},
			check: func(t *testing.T, home string, r result) {
				if r.err != nil {
					t.Fatalf("err = %v, stderr = %q", r.err, r.stderr)
				}
				readRules(t, filepath.Join(home, "rules.json"))
				if _, err := os.Stat(rulesPath(home)); err == nil {
					t.Error("rules were also written to the default path")
				}
			},
		},
		{
			name: "log-file records unmatched requests at the given path",
			args: func(home string) []string {
				return append([]string{"run", "--port", "0", "--log-file", filepath.Join(home, "unmatched.log")}, helperCommand("get", probe)...)
			},
			check: func(t *testing.T, home string, r result) {
				if r.err != nil {
					t.Fatalf("err = %v, stderr = %q", r.err, r.stderr)
				}
				data, err := os.ReadFile(filepath.Join(home, "unmatched.log"))
				if err != nil {
					t.Fatalf("read unmatched log: %v", err)
				}
				if !strings.Contains(string(data), "/probe") {
					t.Errorf("unmatched log = %q, want the request", data)
				}
			},
		},
		{
			name: "debug logs each request through the proxy",
			args: func(string) []string {
				return append([]string{"run", "--port", "0", "--debug"}, helperCommand("get", probe)...)
			},
			check: func(t *testing.T, _ string, r result) {
				if r.err != nil {
					t.Fatalf("err = %v, stderr = %q", r.err, r.stderr)
				}
				if !strings.Contains(r.stderr, "level=DEBUG") || !strings.Contains(r.stderr, "/probe") {
					t.Errorf("stderr = %q, want a debug line for the request", r.stderr)
				}
			},
		},
		{
			name: "port 0 listens on a free port rather than the default",
			args: func(string) []string { return append([]string{"run", "--port", "0"}, helperCommand("print")...) },
			check: func(t *testing.T, _ string, r result) {
				if r.err != nil {
					t.Fatalf("err = %v, stderr = %q", r.err, r.stderr)
				}
				if !strings.Contains(r.stderr, "Proxy listening on http://127.0.0.1:") || strings.Contains(r.stderr, ":9099") {
					t.Errorf("stderr = %q, want a free port other than 9099", r.stderr)
				}
			},
		},
		{
			name:      "background prints the proxy environment and writes the rules when stopped",
			args:      func(string) []string { return []string{"run", "--port", "0", "--background"} },
			cancelled: true,
			check: func(t *testing.T, home string, r result) {
				if r.err != nil {
					t.Fatalf("err = %v, stderr = %q", r.err, r.stderr)
				}
				if !strings.Contains(r.stderr, "export HTTPS_PROXY='http://127.0.0.1:") {
					t.Errorf("stderr = %q, want the export lines", r.stderr)
				}
				readRules(t, rulesPath(home))
			},
		},
		{
			name: "passes the command's exit code through",
			args: func(string) []string { return append([]string{"run", "--port", "0"}, helperCommand("exit", "3")...) },
			check: func(t *testing.T, home string, r result) {
				var ec *ExitCodeError
				if !errors.As(r.err, &ec) || ec.Code != 3 {
					t.Errorf("err = %v, want ExitCodeError with code 3", r.err)
				}
				readRules(t, rulesPath(home))
			},
		},
		{
			name: "no command and no background is an error",
			args: func(string) []string { return []string{"run"} },
			check: func(t *testing.T, home string, r result) {
				if r.err == nil {
					t.Fatal("err = nil, want an error")
				}
				if _, err := os.Stat(filepath.Join(home, ".narc")); err == nil {
					t.Error("the narc directory was created before the arguments were checked")
				}
			},
		},
		{
			name: "port out of range is an error",
			args: func(string) []string { return append([]string{"run", "--port", "70000"}, helperCommand("print")...) },
			check: func(t *testing.T, _ string, r result) {
				if r.err == nil {
					t.Error("err = nil, want an error")
				}
			},
		},
		{
			name: "missing output directory is an error",
			args: func(home string) []string {
				return append([]string{"run", "--port", "0", "--output", filepath.Join(home, "missing", "rules.json")}, helperCommand("print")...)
			},
			check: func(t *testing.T, _ string, r result) {
				if r.err == nil {
					t.Error("err = nil, want an error")
				}
			},
		},
		{
			name: "a command that cannot start writes no rules",
			args: func(string) []string { return []string{"run", "--port", "0", "--", "narc-test-no-such-command"} },
			check: func(t *testing.T, home string, r result) {
				if r.err == nil {
					t.Fatal("err = nil, want an error")
				}
				if _, err := os.Stat(rulesPath(home)); err == nil {
					t.Error("rules were written for a command that never ran")
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			home := t.TempDir()
			ctx := t.Context()
			if tc.cancelled {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			r := execute(t, ctx, home, helperEnviron, tc.args(home)...)
			tc.check(t, home, r)
			if r.err != nil && r.stdout != "" {
				t.Errorf("failed with output on stdout: %q", r.stdout)
			}
		})
	}
}
