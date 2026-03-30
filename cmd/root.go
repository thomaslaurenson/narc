package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/thomaslaurenson/narc/internal/config"
)

var (
	debugFlag bool
	portFlag  int
	cfg       *config.Config
)

var rootCmd = &cobra.Command{
	Use:   "narc",
	Short: "Nectar Access Rules Creator",
	Long: `narc intercepts OpenStack API calls and generates an access_rules.json
file suitable for use with OpenStack Application Credential access rules.`,
	// Silence cobra's default error/usage printing. main.go handles all error
	// output so that ExitCodeError (which carries an empty message) doesn't
	// produce a spurious "Error: " line and usage dump on normal exit.
	SilenceErrors: true,
	SilenceUsage:  true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		var err error
		cfg, err = config.Load()
		if errors.Is(err, config.ErrNotFound) {
			cfg = config.Defaults()
			if saveErr := cfg.Save(); saveErr != nil {
				return fmt.Errorf("create default config: %w", saveErr)
			}
		} else if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		// --port flag overrides narc.json
		if portFlag != 0 {
			cfg.ProxyPort = portFlag
		}
		return nil
	},
}

func Execute() error {
	return rootCmd.Execute()
}

// ExitCodeError is returned by RunE handlers that need to propagate a specific
// exit code. main.go intercepts it and calls os.Exit with the embedded code,
// keeping os.Exit isolated to a single location and allowing defers to run.
type ExitCodeError struct{ Code int }

func (e *ExitCodeError) Error() string { return "" }

func init() {
	rootCmd.PersistentFlags().BoolVar(&debugFlag, "debug", false, "enable debug logging")
	rootCmd.PersistentFlags().IntVar(&portFlag, "port", 0, "proxy port (overrides narc.json)")

	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(shellCmd)
	rootCmd.AddCommand(versionCmd)
}
