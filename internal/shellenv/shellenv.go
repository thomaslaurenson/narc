package shellenv

import (
	"path/filepath"
	"strings"
)

// ShellKind identifies the type of shell being launched.
type ShellKind int

const (
	ShellBash    ShellKind = iota // bash
	ShellZsh                      // zsh without oh-my-zsh
	ShellZshOMZ                   // zsh with oh-my-zsh
	ShellFish                     // fish
	ShellUnknown                  // banner only - no prompt injection
)

// Detect returns the ShellKind for the given shell path and environment slice.
func Detect(shellPath string, env []string) ShellKind {
	switch filepath.Base(shellPath) {
	case "fish":
		return ShellFish
	case "bash":
		return ShellBash
	case "zsh":
		if v, ok := lookupEnv(env, "ZSH"); ok && v != "" {
			return ShellZshOMZ
		}
		return ShellZsh
	default:
		return ShellUnknown
	}
}

// BuildPromptEnv returns a modified copy of baseEnv with prompt integration
// variables set for the given shell kind. It also returns any additional shell
// launch arguments (e.g. --rcfile for bash), a cleanup function that removes
// any temporary files or directories, and any error.
// The caller must defer cleanup() to ensure temp files are removed.
func BuildPromptEnv(kind ShellKind, baseEnv []string) (env []string, shellArgs []string, cleanup func(), err error) {
	switch kind {
	case ShellFish:
		env, cleanup, err = buildFishEnv(baseEnv)
		return env, nil, cleanup, err
	case ShellBash:
		return buildBashEnv(baseEnv)
	case ShellZsh, ShellZshOMZ:
		env, cleanup, err = buildZshEnv(kind, baseEnv)
		return env, nil, cleanup, err
	default:
		return baseEnv, nil, func() {}, nil
	}
}

// setEnvVar returns a copy of env with the given key set to value. Any
// existing entry for key is removed first to prevent duplicates.
func setEnvVar(env []string, key, value string) []string {
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if k != key {
			out = append(out, kv)
		}
	}
	return append(out, key+"="+value)
}

// lookupEnv looks up key in env (a slice of "KEY=VALUE" strings) and returns
// its value and whether it was found.
func lookupEnv(env []string, key string) (string, bool) {
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		if k == key {
			return v, true
		}
	}
	return "", false
}
