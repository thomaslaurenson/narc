package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/thomaslaurenson/narc/internal/analyzer"
	"github.com/thomaslaurenson/narc/internal/catalog"
	"github.com/thomaslaurenson/narc/internal/certmgr"
	"github.com/thomaslaurenson/narc/internal/proxy"
)

var backgroundFlag bool
var logFileFlag string
var outputFileFlag string
var showOutputFlag bool

// proxyKeys is the set of environment variable names that narc overrides when
// building a subprocess environment or printing manual export instructions.
var proxyKeys = map[string]bool{
	"https_proxy": true, "HTTPS_PROXY": true,
	"http_proxy": true, "HTTP_PROXY": true,
	"SSL_CERT_FILE": true, "REQUESTS_CA_BUNDLE": true,
	"OS_CACERT": true,
}

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Record OpenStack API calls made by a command and generate access rules",
	RunE:  runRun,
}

func runRun(_ *cobra.Command, args []string) error {
	if !backgroundFlag && len(args) == 0 {
		return fmt.Errorf("provide a command to wrap (narc run -- <cmd>) or use --background")
	}
	if logFileFlag != "" {
		cfg.LogFile = logFileFlag
	}
	if outputFileFlag != "" {
		cfg.OutputFile = outputFileFlag
	}

	p, az, certPath, err := startRecording()
	if err != nil {
		return err
	}

	if backgroundFlag {
		runBackground(p, az, certPath)
		return nil
	}

	exitCode := runSubprocess(args, buildEnv(p.Port, certPath), showOutputFlag)

	fmt.Fprintf(os.Stderr, "[narc] Shutting down...\n")
	p.Stop()
	writeRulesOnExit(az)
	os.Exit(exitCode)
	return nil
}

// startRecording creates the catalog, analyzer, and proxy, starts the proxy,
// and returns them ready for use. Shared between the run and shell commands.
func startRecording() (*proxy.Proxy, *analyzer.Analyzer, string, error) {
	cat := catalog.NewCatalog()
	az := analyzer.New(cat, func(rule analyzer.AccessRule) {
		fmt.Fprintf(os.Stderr, "[narc] %-20s %-8s %s\n", rule.Service, rule.Method, rule.Path)
	})
	az.LogFile = cfg.LogFile
	if debugFlag {
		az.OnUnmatched = func(method, url string) {
			fmt.Fprintf(os.Stderr, "[narc:debug] unmatched: %s %s\n", method, url)
		}
	}

	p, err := proxy.New(cfg.ProxyPort, debugFlag, cat, az, cfg.LogFile)
	if err != nil {
		return nil, nil, "", fmt.Errorf("create proxy: %w", err)
	}

	certPath, err := certmgr.CACertPath()
	if err != nil {
		return nil, nil, "", fmt.Errorf("get CA cert path: %w", err)
	}

	if err := p.Start(); err != nil {
		return nil, nil, "", fmt.Errorf("start proxy: %w", err)
	}

	fmt.Fprintf(os.Stderr, "[narc] Proxy listening on http://127.0.0.1:%d\n", p.Port)
	return p, az, certPath, nil
}

func writeRulesOnExit(az *analyzer.Analyzer) {
	n := len(az.Rules())
	if err := az.WriteRules(cfg.OutputFile); err != nil {
		fmt.Fprintf(os.Stderr, "[narc:error] Failed to write rules: %v\n", err)
		return
	}
	fmt.Fprintf(os.Stderr, "[narc] Done. %d unique access rule(s) written to %s\n", n, cfg.OutputFile)
}

// buildEnv returns a copy of the current process environment with all proxy-related
// vars removed and replaced by the narc proxy settings.
func buildEnv(port int, caCertPath string) []string {
	proxyURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	env := make([]string, 0, len(os.Environ())+7)
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if !proxyKeys[key] {
			env = append(env, kv)
		}
	}
	return append(env,
		"https_proxy="+proxyURL,
		"HTTPS_PROXY="+proxyURL,
		"http_proxy="+proxyURL,
		"HTTP_PROXY="+proxyURL,
		"SSL_CERT_FILE="+caCertPath,
		"REQUESTS_CA_BUNDLE="+caCertPath,
		"OS_CACERT="+caCertPath,
	)
}

// runSubprocess starts args[0] with the remaining args and waits for it to exit
// or for Ctrl+C. On Ctrl+C, the child is given 3 seconds to exit naturally (it
// receives SIGINT via the shared process group) before being killed.
// stdout is discarded unless showOutput is true; stderr is always forwarded so
// that errors and warnings from the subprocess remain visible.
// Returns the wrapped command's exit code, or 1 on start failure.
func runSubprocess(args []string, env []string, showOutput bool) int {
	// args[0] is the user's explicitly provided command—this subprocess launch is intentional.
	cmd := exec.Command(args[0], args[1:]...) //nolint:gosec
	cmd.Stdin = os.Stdin
	if showOutput {
		cmd.Stdout = os.Stdout
	} else {
		cmd.Stdout = io.Discard
	}
	cmd.Stderr = os.Stderr
	cmd.Env = env

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "[narc:error] Failed to start subprocess: %v\n", err)
		return 1
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)

	var waitErr error
	select {
	case <-quit:
		// Child is in the same process group and will receive SIGINT too.
		// Give it up to 3 seconds to exit cleanly before killing it.
		select {
		case waitErr = <-done:
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
			waitErr = <-done
		}
	case waitErr = <-done:
	}

	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			return exitErr.ExitCode()
		}
		return 1
	}
	return 0
}

func runBackground(p *proxy.Proxy, az *analyzer.Analyzer, certPath string) {
	fmt.Fprintf(os.Stderr, "[narc] Running in background. PID: %d\n", os.Getpid())
	fmt.Fprintf(os.Stderr, "[narc] Run the following in your shell:\n")
	printProxyEnv(p.Port, certPath)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	fmt.Fprintf(os.Stderr, "\n[narc] Shutting down...\n")
	p.Stop()
	writeRulesOnExit(az)
}

// printProxyEnv prints shell export statements for the narc proxy environment.
func printProxyEnv(port int, certPath string) {
	proxyURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	fmt.Fprintf(os.Stderr, "  export https_proxy=%s\n", proxyURL)
	fmt.Fprintf(os.Stderr, "  export HTTPS_PROXY=%s\n", proxyURL)
	fmt.Fprintf(os.Stderr, "  export http_proxy=%s\n", proxyURL)
	fmt.Fprintf(os.Stderr, "  export HTTP_PROXY=%s\n", proxyURL)
	fmt.Fprintf(os.Stderr, "  export SSL_CERT_FILE=%s\n", certPath)
	fmt.Fprintf(os.Stderr, "  export REQUESTS_CA_BUNDLE=%s\n", certPath)
	fmt.Fprintf(os.Stderr, "  export OS_CACERT=%s\n", certPath)
}

func init() {
	runCmd.Flags().BoolVarP(&backgroundFlag, "background", "b", false, "run proxy in background, print env vars for manual use")
	runCmd.Flags().StringVarP(&logFileFlag, "log-file", "l", "", "path for unmatched-request log (default: ~/.narc/unmatched_requests.log)")
	runCmd.Flags().StringVarP(&outputFileFlag, "output", "o", "", "path for access rules output file (default: ~/.narc/access_rules.json)")
	runCmd.Flags().BoolVar(&showOutputFlag, "show-output", false, "show subprocess stdout (stderr is always shown)")
}
