//go:build !windows

package cmd

import (
	"bytes"
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

	"github.com/thomaslaurenson/narc/internal/config"
	"github.com/thomaslaurenson/narc/internal/shellenv"
)

// shellOptions holds the flags of the shell command.
type shellOptions struct {
	logFile    string
	outputFile string
}

// sessionBanner is printed when a recording session starts.
const sessionBanner = `
+----------------------------------------+
|      narc is recording this session    |
|      Type 'exit' or Ctrl-D to stop     |
+----------------------------------------+
`

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
		return errors.New("already inside a narc recording session; nested narc shell is not supported")
	}

	// The session drives the real terminal: raw mode and the pty size both act
	// on the process's own stdin, so an injected reader cannot stand in for it.
	stdinFd := int(os.Stdin.Fd())
	if !term.IsTerminal(stdinFd) {
		return errors.New("narc shell needs an interactive terminal; use narc run -- <command> instead")
	}

	cfg, home, err := a.loadConfig(cmd)
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

	// Everything narc writes during the session goes through crlfWriter, since
	// the terminal is in raw mode for most of it.
	ctx := cmd.Context()
	status := &syncWriter{w: crlfWriter{w: cmd.ErrOrStderr()}}
	logger := newLogger(status, a.debug)

	s, err := startSession(ctx, cfg, config.Dir(home), status, logger)
	if err != nil {
		return err
	}

	shellPath := lookupEnv(a.environ, "SHELL")
	if shellPath == "" {
		shellPath = "/bin/sh"
	}

	kind := shellenv.Detect(shellPath, a.environ)
	if kind == shellenv.ShellUnknown {
		fmt.Fprintf(status, "[!] Unrecognised shell, so no prompt prefix: the session banner is the only recording indicator\n")
	}

	proxyEnv := buildEnv(a.environ, s.proxy.Port, s.certPath)
	promptEnv, shellArgs, cleanup, err := shellenv.BuildPromptEnv(kind, proxyEnv, home)
	if err != nil {
		s.abort(ctx)
		return fmt.Errorf("build prompt env: %w", err)
	}
	defer cleanup()

	// The default Cancel kills the shell when narc is told to stop, which ends
	// the session the same way exit would and lets the rules be written.
	sh := exec.CommandContext(ctx, shellPath, shellArgs...)
	sh.Env = append(promptEnv, "NARC_RECORDING=1")

	// Start the shell attached to a pseudo-terminal so readline, tab-completion,
	// and prompt colours all work correctly without shell-specific wiring.
	ptmx, err := pty.Start(sh)
	if err != nil {
		s.abort(ctx)
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
	oldState, err := term.MakeRaw(stdinFd)
	if err != nil {
		signal.Stop(sigwinch)
		close(sigwinch)
		_ = sh.Process.Kill()
		_ = sh.Wait()
		s.abort(ctx)
		return fmt.Errorf("set raw mode: %w", err)
	}
	var restoreOnce sync.Once
	restoreTerminal := func() {
		restoreOnce.Do(func() { _ = term.Restore(stdinFd, oldState) })
	}
	defer restoreTerminal()

	fmt.Fprint(status, sessionBanner)

	// Bidirectional copy: user keystrokes -> pty, shell output -> stdout.
	go func() { _, _ = io.Copy(ptmx, os.Stdin) }()
	go func() { _, _ = io.Copy(cmd.OutOrStdout(), ptmx) }()

	// Remind the user every 30 seconds that recording is still active, until
	// the shell exits.
	done := make(chan struct{})
	reminder := time.NewTicker(30 * time.Second)
	defer reminder.Stop()
	go func() {
		for {
			select {
			case <-reminder.C:
				fmt.Fprintf(status, "\n[*] Still recording (type 'exit' or Ctrl-D to stop)\n")
			case <-done:
				return
			}
		}
	}()

	runErr := sh.Wait()
	close(done)

	signal.Stop(sigwinch)
	close(sigwinch)

	restoreTerminal()

	fmt.Fprintln(status)
	if err := s.finish(ctx); err != nil {
		return err
	}

	// Propagate the shell's exit code.
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return &ExitCodeError{Code: exitErr.ExitCode()}
	}
	return nil
}

// crlfWriter turns each "\n" into "\r\n". A terminal in raw mode moves down a
// line on "\n" without returning to the first column, so plain newlines would
// leave each line starting where the last one ended.
type crlfWriter struct {
	w io.Writer
}

func (c crlfWriter) Write(p []byte) (int, error) {
	if _, err := c.w.Write(bytes.ReplaceAll(p, []byte("\n"), []byte("\r\n"))); err != nil {
		return 0, err
	}
	return len(p), nil
}
