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
	"golang.org/x/term"

	"github.com/thomaslaurenson/narc/internal/shellenv"
)

// shellOptions holds the flags of the shell command.
type shellOptions struct {
	logFile    string
	outputFile string
}

// sessionBanner is printed at the start of a recording session. \r\n is used
// because the outer terminal will be in raw mode when it is displayed.
const sessionBanner = "" +
	"\r\n╔════════════════════════════════════════╗\r\n" +
	"║      narc is recording this session    ║\r\n" +
	"║      Type 'exit' or Ctrl-D to stop     ║\r\n" +
	"╚════════════════════════════════════════╝\r\n"

func (a *App) newShellCmd() *cobra.Command {
	var opts shellOptions
	c := &cobra.Command{
		Use:   "shell",
		Short: "Start an interactive shell with all OpenStack API calls recorded",
		Long: `Launches your default shell ($SHELL) with the narc proxy pre-configured.

Run OpenStack commands as normal. Every API call is intercepted and recorded.
Type 'exit' or press Ctrl-D to stop the session and write access_rules.json.

Note: requires an interactive terminal (Linux, macOS, WSL). Windows native is
not supported.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.runShell(cmd, opts)
		},
	}
	c.Flags().StringVarP(&opts.logFile, "log-file", "l", "", "path for unmatched-request log (default: ~/.narc/unmatched_requests.log)")
	c.Flags().StringVarP(&opts.outputFile, "output", "o", "", "path for access rules output file (default: ~/.narc/access_rules.json)")
	return c
}

func (a *App) runShell(cmd *cobra.Command, opts shellOptions) error {
	if lookupEnv(a.environ, "NARC_RECORDING") == "1" {
		return fmt.Errorf("already inside a narc recording session; nested narc shell is not supported")
	}

	cfg, err := a.loadConfig(cmd)
	if err != nil {
		return err
	}
	if opts.logFile != "" {
		cfg.LogFile = opts.logFile
	}
	if opts.outputFile != "" {
		cfg.OutputFile = opts.outputFile
	}
	if err := ensureOutputDir(cfg.OutputFile); err != nil {
		return err
	}

	// rawLogf writes to stderr with \r\n so lines are correctly rendered while
	// the outer terminal is in raw mode (used for the entire shell session).
	rawLogf := func(format string, args ...any) {
		// Replace any trailing \n with \r\n so the cursor returns to column 0.
		s := fmt.Sprintf(format, args...)
		if len(s) > 0 && s[len(s)-1] == '\n' {
			s = s[:len(s)-1] + "\r\n"
		}
		fmt.Fprint(os.Stderr, s)
	}

	var onUnmatched func(string, string)
	if a.debug {
		onUnmatched = func(method, url string) {
			rawLogf("[narc:debug] unmatched: %s %s\n", method, url)
		}
	}

	p, az, certPath, unmatchedLog, err := a.startRecording(cfg, onUnmatched, rawLogf)
	if err != nil {
		return err
	}

	shellPath := lookupEnv(a.environ, "SHELL")
	if shellPath == "" {
		shellPath = "/bin/sh"
	}

	proxyEnv := buildEnv(a.environ, p.Port, certPath)
	kind := shellenv.Detect(shellPath, a.environ)

	if kind == shellenv.ShellUnknown {
		fmt.Fprintf(os.Stderr, "[narc] Unrecognised shell - prompt integration disabled. The session banner is your only recording indicator.\n")
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

	// The default Cancel kills the shell when narc is told to stop, which ends
	// the session the same way exit would and lets the rules be written.
	sh := exec.CommandContext(cmd.Context(), shellPath, shellArgs...) //nolint:gosec
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

	// Remind the user every 30 seconds that recording is still active, until
	// the shell exits.
	done := make(chan struct{})
	reminder := time.NewTicker(30 * time.Second)
	defer reminder.Stop()
	go func() {
		for {
			select {
			case <-reminder.C:
				_, _ = os.Stderr.WriteString("\r\n[narc] still recording… (type 'exit' or Ctrl-D to stop)\r\n")
			case <-done:
				return
			}
		}
	}()

	runErr := sh.Wait()
	close(done)

	signal.Stop(sigwinch)
	close(sigwinch)

	// Restore terminal before printing shutdown messages so normal \n works.
	restoreTerminal()

	fmt.Fprintf(os.Stderr, "\n[narc] Shutting down...\n")
	p.Stop()
	if unmatchedLog != nil {
		_ = unmatchedLog.Close()
	}
	writeRulesOnExit(az, cfg.OutputFile)

	// Propagate the shell's exit code.
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return &ExitCodeError{Code: exitErr.ExitCode()}
	}
	return nil
}
