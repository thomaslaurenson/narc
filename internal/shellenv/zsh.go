package shellenv

import (
	"fmt"
	"os"
	"path/filepath"
)

const zshRCBare = `# narc session zshrc
# Source the user's real zshrc first.
ZDOTDIR="${NARC_REAL_ZDOTDIR}"
[[ -f "${NARC_REAL_ZDOTDIR}/.zshrc" ]] && source "${NARC_REAL_ZDOTDIR}/.zshrc"

# After user rc runs, set the narc prompt prefix.
# PROMPT and PS1 are synonyms in zsh; prefer PROMPT.
[[ "$PROMPT" != "(narc)"* ]] && PROMPT="(narc) $PROMPT"
`

func buildZshEnv(kind ShellKind, baseEnv []string) (env []string, cleanup func(), err error) {
	realZDOTDIR, ok := lookupEnv(baseEnv, "ZDOTDIR")
	if !ok || realZDOTDIR == "" {
		realZDOTDIR, err = os.UserHomeDir()
		if err != nil {
			return nil, func() {}, fmt.Errorf("get user home dir: %w", err)
		}
	}

	tmpDir, err := os.MkdirTemp("", "narc-zsh-*")
	if err != nil {
		return nil, func() {}, fmt.Errorf("create temp zsh dir: %w", err)
	}

	var rcContent string
	if kind == ShellZshOMZ {
		rcContent = zshRCOMZ
	} else {
		rcContent = zshRCBare
	}

	zshrcPath := filepath.Join(tmpDir, ".zshrc")
	if err := os.WriteFile(zshrcPath, []byte(rcContent), 0600); err != nil {
		_ = os.RemoveAll(tmpDir)
		return nil, func() {}, fmt.Errorf("write temp zshrc: %w", err)
	}

	env = setEnvVar(baseEnv, "ZDOTDIR", tmpDir)
	env = setEnvVar(env, "NARC_REAL_ZDOTDIR", realZDOTDIR)
	cleanup = func() { _ = os.RemoveAll(tmpDir) }
	return env, cleanup, nil
}
