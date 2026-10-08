//go:build !windows

package cmd

import (
	"errors"
	"os"
	"testing"

	"golang.org/x/term"
)

// The shell command drives the process's own terminal, so only the paths that
// fail before it takes the terminal can run under go test. Its flags reach the
// same session code as run's, which TestRunCommand covers.
func TestShellCommand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		environ         []string
		args            []string
		needsNoTerminal bool
		// wantErr is the error the case must match; nil accepts any error.
		wantErr error
	}{
		{
			name:    "refuses to nest inside a recording session",
			environ: []string{"NARC_RECORDING=1"},
			args:    []string{"shell"},
			wantErr: errNestedSession,
		},
		{
			name:            "refuses to run without a terminal",
			args:            []string{"shell"},
			needsNoTerminal: true,
			wantErr:         errNoTerminal,
		},
		{
			name: "rejects an argument",
			args: []string{"shell", "extra"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.needsNoTerminal && term.IsTerminal(int(os.Stdin.Fd())) {
				t.Skip("stdin is a terminal")
			}
			home := t.TempDir()
			r := execute(t, t.Context(), home, tc.environ, tc.args...)
			if r.err == nil {
				t.Fatal("err = nil, want an error")
			}
			if tc.wantErr != nil && !errors.Is(r.err, tc.wantErr) {
				t.Errorf("err = %v, want %v", r.err, tc.wantErr)
			}
			if r.stdout != "" {
				t.Errorf("failed with output on stdout: %q", r.stdout)
			}
		})
	}
}
