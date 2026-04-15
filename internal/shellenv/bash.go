package shellenv

import (
	"fmt"
	"os"
)

const bashRC = `# narc session rc — sourced instead of ~/.bashrc
[ -f "$HOME/.bashrc" ] && source "$HOME/.bashrc"

# Prepend (narc) to PS1 on every prompt redraw.
# Uses PROMPT_COMMAND so it runs after .bashrc has set PS1.
# The guard prevents double-prefixing if PROMPT_COMMAND is called multiple times.
_narc_prompt() {
    [[ "$PS1" != "(narc)"* ]] && PS1="(narc) $PS1"
}
PROMPT_COMMAND="_narc_prompt${PROMPT_COMMAND:+; $PROMPT_COMMAND}"
`

func buildBashEnv(baseEnv []string) (env []string, shellArgs []string, cleanup func(), err error) {
	f, err := os.CreateTemp("", "narc-bashrc-*")
	if err != nil {
		return nil, nil, func() {}, fmt.Errorf("create temp bashrc: %w", err)
	}

	if _, err := f.WriteString(bashRC); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return nil, nil, func() {}, fmt.Errorf("write temp bashrc: %w", err)
	}

	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return nil, nil, func() {}, fmt.Errorf("close temp bashrc: %w", err)
	}

	path := f.Name()
	cleanup = func() { _ = os.Remove(path) }
	return baseEnv, []string{"--rcfile", path}, cleanup, nil
}
