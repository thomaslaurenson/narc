package shellenv

import (
	"os"
	"strings"
	"testing"
)

func TestDetect(t *testing.T) {
	tests := []struct {
		name      string
		shellPath string
		env       []string
		want      ShellKind
	}{
		{"bash", "/bin/bash", nil, ShellBash},
		{"zsh bare", "/usr/bin/zsh", nil, ShellZsh},
		{"zsh with ZSH set", "/usr/bin/zsh", []string{"ZSH=/home/user/.oh-my-zsh"}, ShellZshOMZ},
		{"zsh with ZSH empty", "/usr/bin/zsh", []string{"ZSH="}, ShellZsh},
		{"fish", "/usr/bin/fish", nil, ShellFish},
		{"fish local bin", "/usr/local/bin/fish", nil, ShellFish},
		{"sh unknown", "/bin/sh", nil, ShellUnknown},
		{"dash unknown", "/bin/dash", nil, ShellUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Detect(tt.shellPath, tt.env)
			if got != tt.want {
				t.Errorf("Detect(%q, %v) = %v, want %v", tt.shellPath, tt.env, got, tt.want)
			}
		})
	}
}

func TestBuildFishEnv_SetsPrefix(t *testing.T) {
	base := []string{"FOO=bar", "HOME=/home/user"}
	env, cleanup, err := buildFishEnv(base)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer cleanup()

	found := false
	for _, kv := range env {
		if kv == "SHELL_PROMPT_PREFIX=(narc) " {
			found = true
		}
	}
	if !found {
		t.Errorf("SHELL_PROMPT_PREFIX=(narc)  not set; env = %v", env)
	}
}

func TestBuildFishEnv_NoDuplication(t *testing.T) {
	base := []string{"SHELL_PROMPT_PREFIX=old "}
	env, cleanup, err := buildFishEnv(base)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer cleanup()

	count := 0
	for _, kv := range env {
		if strings.HasPrefix(kv, "SHELL_PROMPT_PREFIX=") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("SHELL_PROMPT_PREFIX appears %d times, want 1; env = %v", count, env)
	}
}

func TestBuildBashEnv(t *testing.T) {
	base := []string{"FOO=bar"}
	env, shellArgs, cleanup, err := buildBashEnv(base)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Env should be unchanged.
	if len(env) != len(base) || env[0] != base[0] {
		t.Errorf("env modified unexpectedly: %v", env)
	}

	// shellArgs must be [--rcfile, <path>].
	if len(shellArgs) != 2 || shellArgs[0] != "--rcfile" {
		t.Fatalf("expected shellArgs [--rcfile <path>], got %v", shellArgs)
	}

	tmpPath := shellArgs[1]

	// Temp file must exist with expected content.
	content, err := os.ReadFile(tmpPath)
	if err != nil {
		t.Fatalf("could not read temp bashrc %q: %v", tmpPath, err)
	}
	if !strings.Contains(string(content), ".bashrc") {
		t.Errorf("temp bashrc does not source .bashrc:\n%s", content)
	}
	if !strings.Contains(string(content), "_narc_prompt") {
		t.Errorf("temp bashrc does not define _narc_prompt:\n%s", content)
	}

	// Cleanup must remove the file.
	cleanup()
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Errorf("temp bashrc %q not removed after cleanup", tmpPath)
		_ = os.Remove(tmpPath) // best-effort cleanup for the test itself
	}
}

func TestBuildZshEnv_Bare(t *testing.T) {
	base := []string{"FOO=bar"}
	env, cleanup, err := buildZshEnv(ShellZsh, base)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	zdotdir, ok := lookupEnv(env, "ZDOTDIR")
	if !ok || zdotdir == "" {
		t.Fatalf("ZDOTDIR not set in env: %v", env)
	}

	// .zshrc must exist inside the temp dir.
	zshrcPath := zdotdir + "/.zshrc"
	if _, err := os.Stat(zshrcPath); os.IsNotExist(err) {
		t.Errorf(".zshrc not found at %q", zshrcPath)
	}

	// NARC_REAL_ZDOTDIR must be set.
	if _, ok := lookupEnv(env, "NARC_REAL_ZDOTDIR"); !ok {
		t.Errorf("NARC_REAL_ZDOTDIR not set in env")
	}

	// Cleanup must remove the temp dir.
	cleanup()
	if _, err := os.Stat(zdotdir); !os.IsNotExist(err) {
		t.Errorf("temp zsh dir %q not removed after cleanup", zdotdir)
		_ = os.RemoveAll(zdotdir) // best-effort cleanup for the test itself
	}
}

func TestBuildZshEnv_OMZ(t *testing.T) {
	base := []string{"ZSH=/home/user/.oh-my-zsh"}
	env, cleanup, err := buildZshEnv(ShellZshOMZ, base)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer cleanup()

	zdotdir, ok := lookupEnv(env, "ZDOTDIR")
	if !ok || zdotdir == "" {
		t.Fatalf("ZDOTDIR not set in env: %v", env)
	}

	content, err := os.ReadFile(zdotdir + "/.zshrc")
	if err != nil {
		t.Fatalf("could not read temp zshrc: %v", err)
	}
	if !strings.Contains(string(content), "precmd_functions+=(_narc_precmd)") {
		t.Errorf("OMZ zshrc missing precmd hook:\n%s", content)
	}
}
