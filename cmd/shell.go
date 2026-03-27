package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"
)

var shellOutputFileFlag string
var shellLogFileFlag string

var shellCmd = &cobra.Command{
	Use:   "shell",
	Short: "Start an interactive shell with all OpenStack API calls recorded",
	Long: `Launches your default shell ($SHELL) with the narc proxy pre-configured.

Run OpenStack commands as normal. Every API call is intercepted and recorded.
Type 'exit' or press Ctrl-D to stop the session and write access_rules.json.`,
	RunE: runShell,
}

func runShell(_ *cobra.Command, _ []string) error {
	if shellLogFileFlag != "" {
		cfg.LogFile = shellLogFileFlag
	}
	if shellOutputFileFlag != "" {
		cfg.OutputFile = shellOutputFileFlag
	}

	p, az, certPath, err := startRecording()
	if err != nil {
		return err
	}

	shellPath := os.Getenv("SHELL")
	if shellPath == "" {
		shellPath = "/bin/sh"
	}

	fmt.Fprintf(os.Stderr, "[narc] Recording OpenStack API calls — run commands as normal.\n")
	fmt.Fprintf(os.Stderr, "[narc] Shell: %s  |  Type 'exit' or press Ctrl-D to stop recording.\n", shellPath)

	// Set terminal window/tab title via OSC escape — works in any terminal
	// emulator on Linux and macOS regardless of the shell in use.
	fmt.Fprintf(os.Stderr, "\033]0;narc recording\007")

	env := buildEnv(p.Port, certPath)
	env = append(env, "NARC_RECORDING=1")

	sh, cleanup, err := prepareShell(shellPath, env)
	if err != nil {
		return fmt.Errorf("prepare shell: %w", err)
	}
	defer cleanup()

	// Ignore SIGINT in narc while the shell is running. Interactive shells handle
	// Ctrl-C themselves (it cancels the current input line, not the session).
	// The session ends when the user types 'exit' or presses Ctrl-D.
	signal.Ignore(syscall.SIGINT)

	runErr := sh.Run()

	signal.Reset(syscall.SIGINT)

	// Restore the terminal title to blank.
	fmt.Fprintf(os.Stderr, "\033]0;\007")

	fmt.Fprintf(os.Stderr, "\n[narc] Shutting down...\n")
	p.Stop()
	writeRulesOnExit(az)

	// Propagate the shell's exit code.
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		os.Exit(exitErr.ExitCode())
	}
	return nil
}

// prepareShell constructs an exec.Cmd for shellPath with a prompt that indicates
// narc is active. Returns a cleanup function that removes any temporary files.
//
// Strategy by shell:
//   - bash: spawned with --rcfile pointing to a temp file that sources ~/.bashrc
//     then prepends "(narc) " to PS1. Works with vanilla prompts and powerline/starship
//     because we prepend to whatever PS1 was set (including dynamic PS1 expressions).
//   - zsh:  ZDOTDIR set to a temp dir whose .zshrc sources ~/.zshenv and ~/.zshrc,
//     then registers a precmd hook via add-zsh-hook. Running after the user's own
//     precmds (including starship/oh-my-zsh) means it always wins the final PROMPT value.
//   - sh/dash/ksh and others: PS1 injected directly into the environment. These shells
//     do not source rcfiles for interactive sessions, so the value sticks.
//   - fish: fish uses functions not PS1; NARC_RECORDING=1 and the terminal title
//     (set via OSC in runShell) serve as the indicator.
func prepareShell(shellPath string, env []string) (*exec.Cmd, func(), error) {
	noop := func() {}
	name := filepath.Base(shellPath)
	home, _ := os.UserHomeDir()

	switch name {
	case "bash":
		f, err := os.CreateTemp("", "narc-bash-rc-*")
		if err != nil {
			return nil, noop, fmt.Errorf("create bash rcfile: %w", err)
		}
		cleanup := func() { _ = os.Remove(f.Name()) }

		// Source the user's real .bashrc first so aliases, functions, and their
		// existing PS1 (including starship/powerline expressions) are all loaded.
		// Then prepend the (narc) indicator to whatever PS1 was set.
		content := fmt.Sprintf("[ -f %q ] && source %q\n", home+"/.bashrc", home+"/.bashrc") +
			"PS1=\"(narc) $PS1\"\n"
		if _, err := f.WriteString(content); err != nil {
			_ = f.Close()
			cleanup()
			return nil, noop, fmt.Errorf("write bash rcfile: %w", err)
		}
		_ = f.Close()

		sh := exec.Command(shellPath, "--rcfile", f.Name()) //nolint:gosec
		sh.Stdin = os.Stdin
		sh.Stdout = os.Stdout
		sh.Stderr = os.Stderr
		sh.Env = env
		return sh, cleanup, nil

	case "zsh":
		tmpDir, err := os.MkdirTemp("", "narc-zsh-*")
		if err != nil {
			return nil, noop, fmt.Errorf("create zsh tmpdir: %w", err)
		}
		cleanup := func() { _ = os.RemoveAll(tmpDir) }

		// With ZDOTDIR overridden, zsh reads $ZDOTDIR/.zshrc instead of ~/.zshrc.
		// We source the user's .zshenv and .zshrc first to preserve their full
		// environment, then register a precmd hook that runs last — after any
		// theme framework (starship, oh-my-zsh, prezto) has set PROMPT — and
		// prepends the (narc) indicator.
		content := fmt.Sprintf("[ -f %q ] && source %q\n", home+"/.zshenv", home+"/.zshenv") +
			fmt.Sprintf("[ -f %q ] && source %q\n", home+"/.zshrc", home+"/.zshrc") +
			"autoload -Uz add-zsh-hook\n" +
			"_narc_prompt_prefix() { PROMPT=\"(narc) $PROMPT\"; }\n" +
			"add-zsh-hook precmd _narc_prompt_prefix\n"
		if err := os.WriteFile(filepath.Join(tmpDir, ".zshrc"), []byte(content), 0600); err != nil {
			cleanup()
			return nil, noop, fmt.Errorf("write zsh rcfile: %w", err)
		}

		env = append(env, "ZDOTDIR="+tmpDir)
		sh := exec.Command(shellPath) //nolint:gosec
		sh.Stdin = os.Stdin
		sh.Stdout = os.Stdout
		sh.Stderr = os.Stderr
		sh.Env = env
		return sh, cleanup, nil

	default:
		// sh, dash, ksh: PS1 in the environment is respected for interactive shells
		// and is not overridden by rcfile sourcing, so it sticks.
		// fish: PS1 is ignored; NARC_RECORDING=1 and the terminal title are the indicators.
		env = append(env, "PS1=(narc) $ ")
		sh := exec.Command(shellPath) //nolint:gosec
		sh.Stdin = os.Stdin
		sh.Stdout = os.Stdout
		sh.Stderr = os.Stderr
		sh.Env = env
		return sh, noop, nil
	}
}

func init() {
	shellCmd.Flags().StringVarP(&shellLogFileFlag, "log-file", "l", "", "path for unmatched-request log (default: ~/.narc/unmatched_requests.log)")
	shellCmd.Flags().StringVarP(&shellOutputFileFlag, "output", "o", "", "path for access rules output file (default: ~/.narc/access_rules.json)")
}
