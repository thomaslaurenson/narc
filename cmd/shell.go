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

	"github.com/creack/pty"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var shellOutputFileFlag string
var shellLogFileFlag string

var shellCmd = &cobra.Command{
	Use:   "shell",
	Short: "Start an interactive shell with all OpenStack API calls recorded",
	Long: `Launches your default shell ($SHELL) with the narc proxy pre-configured.

Run OpenStack commands as normal. Every API call is intercepted and recorded.
Type 'exit' or press Ctrl-D to stop the session and write access_rules.json.

Note: requires an interactive terminal (Linux, macOS, WSL). Windows native is
not supported.`,
	RunE: runShell,
}

// sessionBanner is printed at the start of a recording session. \r\n is used
// because the outer terminal will be in raw mode when it is displayed.
const sessionBanner = "" +
	"\r\n╔════════════════════════════════════════╗\r\n" +
	"║      narc is recording this session    ║\r\n" +
	"║      Type 'exit' or Ctrl-D to stop     ║\r\n" +
	"╚════════════════════════════════════════╝\r\n"

func runShell(_ *cobra.Command, _ []string) error {
	if shellLogFileFlag != "" {
		cfg.LogFile = shellLogFileFlag
	}
	if shellOutputFileFlag != "" {
		cfg.OutputFile = shellOutputFileFlag
	}

	var onUnmatched func(string, string)
	if debugFlag {
		onUnmatched = func(method, url string) {
			fmt.Fprintf(os.Stderr, "[narc:debug] unmatched: %s %s\n", method, url)
		}
	}

	p, az, certPath, unmatchedLog, err := startRecording(cfg.LogFile, onUnmatched)
	if err != nil {
		return err
	}

	shellPath := os.Getenv("SHELL")
	if shellPath == "" {
		shellPath = "/bin/sh"
	}

	env := shellFilterEnv(buildEnv(p.Port, certPath))
	env = append(env, "NARC_RECORDING=1")

	// shellPath comes from $SHELL — the subprocess launch is intentional.
	sh := exec.Command(shellPath) //nolint:gosec
	sh.Env = env

	// Start the shell attached to a pseudo-terminal so readline, tab-completion,
	// and prompt colours all work correctly without shell-specific wiring.
	ptmx, err := pty.Start(sh)
	if err != nil {
		p.Stop()
		if unmatchedLog != nil {
			_ = unmatchedLog.Close()
		}
		return fmt.Errorf("start pty: %w", err)
	}
	defer func() { _ = ptmx.Close() }()

	// Match the pty size to the outer terminal so line-wrapping is correct.
	if sz, err := pty.GetsizeFull(os.Stdin); err == nil {
		_ = pty.Setsize(ptmx, sz)
	}

	// Forward SIGWINCH so the pty tracks window resize events.
	sigwinch := make(chan os.Signal, 1)
	signal.Notify(sigwinch, syscall.SIGWINCH)
	go func() {
		for range sigwinch {
			if sz, err := pty.GetsizeFull(os.Stdin); err == nil {
				_ = pty.Setsize(ptmx, sz)
			}
		}
	}()

	// Put the outer terminal into raw mode: keystrokes pass directly to the pty
	// without line-buffering, echo, or control-character processing by the host.
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		signal.Stop(sigwinch)
		close(sigwinch)
		_ = sh.Process.Kill()
		_ = sh.Wait()
		p.Stop()
		if unmatchedLog != nil {
			_ = unmatchedLog.Close()
		}
		return fmt.Errorf("set raw mode: %w", err)
	}
	restoreTerminal := func() { _ = term.Restore(int(os.Stdin.Fd()), oldState) }
	defer restoreTerminal()

	// Banner printed after entering raw mode so \r\n renders correctly.
	_, _ = os.Stderr.WriteString(sessionBanner)

	// Bidirectional copy: user keystrokes → pty, shell output → stdout.
	go func() { _, _ = io.Copy(ptmx, os.Stdin) }()
	go func() { _, _ = io.Copy(os.Stdout, ptmx) }()

	// Remind the user every 30 seconds that recording is still active.
	reminder := time.NewTicker(30 * time.Second)
	defer reminder.Stop()
	go func() {
		for range reminder.C {
			_, _ = os.Stderr.WriteString("\r\n[narc] still recording… (type 'exit' or Ctrl-D to stop)\r\n")
		}
	}()

	runErr := sh.Wait()

	signal.Stop(sigwinch)
	close(sigwinch)

	// Restore terminal before printing shutdown messages so normal \n works.
	restoreTerminal()

	fmt.Fprintf(os.Stderr, "\n[narc] Shutting down...\n")
	p.Stop()
	if unmatchedLog != nil {
		_ = unmatchedLog.Close()
	}
	writeRulesOnExit(az)

	// Propagate the shell's exit code.
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return &ExitCodeError{Code: exitErr.ExitCode()}
	}
	return nil
}

// shellFilterEnv removes VS Code shell-integration variables from env and
// injects a best-effort narc prompt. VS Code injects PROMPT_COMMAND and
// VSCODE_* vars that reference shell functions sourced only in the outer
// terminal; they cause 'command not found' errors in the new pty shell.
// PS1/PROMPT are set as a hint for bare bash/zsh; users with starship,
// oh-my-zsh, etc. will see their own prompt (their rc overrides these), which
// is acceptable — the session banner already makes the state clear.
func shellFilterEnv(env []string) []string {
	out := make([]string, 0, len(env)+2)
	for _, kv := range env {
		key, _, _ := strings.Cut(kv, "=")
		if key == "PROMPT_COMMAND" || strings.HasPrefix(key, "VSCODE_") {
			continue
		}
		out = append(out, kv)
	}
	out = append(out,
		// best-effort for bare bash/zsh with no rc file or a rc that doesn't
		// set a prompt (picked up before .bashrc/.zshrc runs).
		`PS1=(narc) \u@\h:\w\$ `,
		`PROMPT=(narc) %n@%m:%~%# `,
		// bash: runs once before the first prompt, after .bashrc has already
		// set PS1 to its final value. Prepends "(narc) " to whatever PS1 is
		// at that point, then unsets itself so it never fires again.
		// This is a no-op for zsh and other shells that ignore PROMPT_COMMAND.
		`PROMPT_COMMAND=PS1="(narc) $PS1"; unset PROMPT_COMMAND`,
	)
	return out
}

func init() {
	shellCmd.Flags().StringVarP(&shellLogFileFlag, "log-file", "l", "", "path for unmatched-request log (default: ~/.narc/unmatched_requests.log)")
	shellCmd.Flags().StringVarP(&shellOutputFileFlag, "output", "o", "", "path for access rules output file (default: ~/.narc/access_rules.json)")
}
