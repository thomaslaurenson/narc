// Package cmd implements the narc command-line interface.
package cmd

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/thomaslaurenson/narc/internal/config"
)

// App holds the dependencies and persistent flags shared by every subcommand.
type App struct {
	environ []string
	debug   bool
	port    int
}

// NewRootCmd builds the command tree. environ is the environment the recorded
// commands inherit, and output goes to out and errw.
func NewRootCmd(environ []string, out, errw io.Writer) *cobra.Command {
	a := &App{environ: environ}

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

// loadConfig reads narc.json, writing one with the defaults on first run, and
// applies the --port override. It is called by the commands that record rather
// than from a persistent pre-run hook, so help, version and completion never
// create the config file.
func (a *App) loadConfig(cmd *cobra.Command) (*config.Config, error) {
	cfg, err := config.Load()
	if errors.Is(err, config.ErrNotFound) {
		cfg = config.Defaults()
		if err := cfg.Save(); err != nil {
			return nil, fmt.Errorf("create default config: %w", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	// Changed rather than a non-zero check, since 0 is a port the flag accepts.
	if cmd.Flags().Changed("port") {
		if a.port < 0 || a.port > 65535 {
			return nil, fmt.Errorf("port %d is out of range (0-65535)", a.port)
		}
		cfg.ProxyPort = a.port
	}
	return cfg, nil
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
