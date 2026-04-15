//go:build !windows

package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/spf13/cobra"
	"github.com/thomaslaurenson/narc/internal/shellenv"
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
	if os.Getenv("NARC_RECORDING") == "1" {
		return fmt.Errorf("already inside a narc recording session; nested narc shell is not supported")
	}

	if shellLogFileFlag != "" {
		cfg.LogFile = shellLogFileFlag
	}
	if shellOutputFileFlag != "" {
		cfg.OutputFile = shellOutputFileFlag
	}
	if err := ensureOutputDir(cfg.OutputFile); err != nil {
		return err
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

	proxyEnv := buildEnv(p.Port, certPath)
	kind := shellenv.Detect(shellPath, os.Environ())

	if kind == shellenv.ShellUnknown {
		fmt.Fprintf(os.Stderr, "[narc] Unrecognised shell — prompt integration disabled. The session banner is your only recording indicator.\n")
	}

	promptEnv, shellArgs, cleanup, err := shellenv.BuildPromptEnv(kind, proxyEnv)
	if err != nil {
		p.Stop()
		if unmatchedLog != nil {
			_ = unmatchedLog.Close()
		}
		return fmt.Errorf("build prompt env: %w", err)
	}
	defer cleanup()

	// shellPath comes from $SHELL — the subprocess launch is intentional.
	sh := exec.Command(shellPath, shellArgs...) //nolint:gosec
	sh.Env = append(promptEnv, "NARC_RECORDING=1")

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
	stdinFd := int(os.Stdin.Fd()) //nolint:gosec // fd is always a small non-negative value
	oldState, err := term.MakeRaw(stdinFd)
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
	var restoreOnce sync.Once
	restoreTerminal := func() {
		restoreOnce.Do(func() { _ = term.Restore(stdinFd, oldState) })
	}
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

func init() {
	shellCmd.Flags().StringVarP(&shellLogFileFlag, "log-file", "l", "", "path for unmatched-request log (default: ~/.narc/unmatched_requests.log)")
	shellCmd.Flags().StringVarP(&shellOutputFileFlag, "output", "o", "", "path for access rules output file (default: ~/.narc/access_rules.json)")
}
