package cmd

import (
	"strings"
	"testing"
)

func TestVersionCommand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{name: "prints the version", args: []string{"version"}},
		{name: "rejects an argument", args: []string{"version", "extra"}, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := execute(t, t.Context(), t.TempDir(), nil, tc.args...)
			if tc.wantErr {
				if r.err == nil {
					t.Error("err = nil, want an error")
				}
				if r.stdout != "" {
					t.Errorf("failed with output on stdout: %q", r.stdout)
				}
				return
			}
			if r.err != nil {
				t.Fatalf("err = %v", r.err)
			}
			if !strings.HasPrefix(r.stdout, "narc version ") {
				t.Errorf("stdout = %q, want it to start with %q", r.stdout, "narc version ")
			}
		})
	}
}

func TestVersionFrom(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		injected string
		module   string
		want     string
	}{
		{name: "injected version wins", injected: "1.2.3", module: "v9.9.9", want: "1.2.3"},
		{name: "go install of a tag", injected: devVersion, module: "v1.2.3", want: "1.2.3"},
		{name: "working tree", injected: devVersion, module: "(devel)", want: devVersion},
		{name: "no module version", injected: devVersion, module: "", want: devVersion},
		{name: "dirty pseudo-version", injected: devVersion, module: "v1.2.4-0.20260101120000-abcdef123456+dirty", want: devVersion},
		{name: "pseudo-version above a tag", injected: devVersion, module: "v1.2.4-0.20260101120000-abcdef123456", want: devVersion},
		{name: "pseudo-version with no tag", injected: devVersion, module: "v0.0.0-20260101120000-abcdef123456", want: devVersion},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := versionFrom(tc.injected, tc.module); got != tc.want {
				t.Errorf("versionFrom(%q, %q) = %q, want %q", tc.injected, tc.module, got, tc.want)
			}
		})
	}
}
