// Package cmd implements the narc command-line interface.
package cmd

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/thomaslaurenson/narc/internal/config"
)

// App holds the dependencies and persistent flags shared by every subcommand.
type App struct {
	environ []string
	homeDir func() (string, error)
	debug   bool
	port    int
}

// NewRootCmd builds the command tree. environ is the environment the recorded
// commands inherit, homeDir finds the directory narc keeps its files under, and
// output goes to out and errw.
func NewRootCmd(environ []string, homeDir func() (string, error), out, errw io.Writer) *cobra.Command {
	a := &App{environ: environ, homeDir: homeDir}

	root := &cobra.Command{
		Use:   "narc",
		Short: "Nectar Access Rules Creator",
		Long: `narc intercepts OpenStack API calls and generates an access_rules.json
file suitable for use with OpenStack Application Credential access rules.`,
		SilenceErrors: true,
		SilenceUsage:  true,
		Version:       Version,
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprint(cmd.ErrOrStderr(), cmd.UsageString())
			return &ExitCodeError{Code: 1}
		},
	}
	root.SetOut(out)
	root.SetErr(errw)

	root.PersistentFlags().BoolVar(&a.debug, "debug", false, "enable debug logging")
	root.PersistentFlags().IntVar(&a.port, "port", 0, "proxy port, 0 for any free port (overrides narc.json)")

	root.AddCommand(a.newRunCmd(), a.newShellCmd(), newVersionCmd())
	return root
}

// ExitCodeError is returned by a command that has already produced its output
// and needs to set the process exit code itself.
type ExitCodeError struct{ Code int }

func (e *ExitCodeError) Error() string { return "" }

// loadConfig reads narc.json from the narc directory, writing one with the
// defaults on first run, and applies the --port override. It also returns the
// home directory, which the narc directory and the shell's startup files are
// found from. It is called by the commands that record rather than from a
// persistent pre-run hook, so help, version and completion never create the
// config file.
func (a *App) loadConfig(cmd *cobra.Command) (*config.Config, string, error) {
	home, err := a.homeDir()
	if err != nil {
		return nil, "", fmt.Errorf("find home directory: %w", err)
	}
	dir := config.Dir(home)

	cfg, err := config.Load(dir)
	if errors.Is(err, config.ErrNotFound) {
		cfg = config.Defaults(dir)
		if err := cfg.Save(dir); err != nil {
			return nil, "", fmt.Errorf("create default config: %w", err)
		}
	} else if err != nil {
		return nil, "", err
	}

	// Changed rather than a non-zero check, since 0 is a port the flag accepts.
	if cmd.Flags().Changed("port") {
		if a.port < 0 || a.port > 65535 {
			return nil, "", fmt.Errorf("port %d is out of range (0-65535)", a.port)
		}
		cfg.ProxyPort = a.port
	}
	return cfg, home, nil
}

// newLogger returns the diagnostics logger, at Warn unless --debug asked for
// Debug.
func newLogger(w io.Writer, debug bool) *slog.Logger {
	level := slog.LevelWarn
	if debug {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level}))
}

// syncWriter serialises writes to w. The proxy reports from its own goroutines
// while the command writes from the main one, and both share the error stream.
type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

// lookupEnv returns the value of key in environ, a slice of KEY=VALUE pairs,
// or the empty string when it is not set.
func lookupEnv(environ []string, key string) string {
	for _, kv := range environ {
		if k, v, ok := strings.Cut(kv, "="); ok && k == key {
			return v
		}
	}
	return ""
}
