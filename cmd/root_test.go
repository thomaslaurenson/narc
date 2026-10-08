package cmd

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// lockedBuffer is a bytes.Buffer that is safe for concurrent writes. A recording
// writes to the error stream from the proxy's goroutines while the wrapped
// command's stderr is copied in from another.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// result is what one invocation of the command tree produced.
type result struct {
	stdout string
	stderr string
	err    error
}

// execute builds a fresh command tree with home as the home directory and
// environ as the environment, runs it with args under ctx, and captures both
// streams.
func execute(t *testing.T, ctx context.Context, home string, environ []string, args ...string) result {
	t.Helper()

	var out, errOut lockedBuffer
	root := NewRootCmd(environ, func() (string, error) { return home, nil }, &out, &errOut)
	root.SetIn(strings.NewReader(""))
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)

	return result{stdout: out.String(), stderr: errOut.String(), err: err}
}

func TestRootCommand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		args  []string
		check func(t *testing.T, r result)
	}{
		{
			name: "bare invocation prints usage to stderr and exits 1",
			args: nil,
			check: func(t *testing.T, r result) {
				var ec *ExitCodeError
				if !errors.As(r.err, &ec) || ec.Code != 1 {
					t.Errorf("err = %v, want ExitCodeError with code 1", r.err)
				}
				if !strings.Contains(r.stderr, "Usage:") {
					t.Errorf("stderr does not show usage: %q", r.stderr)
				}
			},
		},
		{
			name: "version flag prints the version",
			args: []string{"--version"},
			check: func(t *testing.T, r result) {
				if r.err != nil {
					t.Fatalf("err = %v", r.err)
				}
				if !strings.Contains(r.stdout, "narc version "+Version) {
					t.Errorf("stdout = %q, want the version", r.stdout)
				}
			},
		},
		{
			name: "unknown subcommand is an error",
			args: []string{"no-such-subcommand"},
			check: func(t *testing.T, r result) {
				if r.err == nil {
					t.Error("err = nil, want an error")
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := execute(t, t.Context(), t.TempDir(), nil, tc.args...)
			tc.check(t, r)
			if r.err != nil && r.stdout != "" {
				t.Errorf("failed with output on stdout: %q", r.stdout)
			}
		})
	}
}
