package cmd

import (
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
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		var err error
		cfg, err = config.Load()
		if err != nil {
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

func init() {
	rootCmd.PersistentFlags().BoolVar(&debugFlag, "debug", false, "enable debug logging")
	rootCmd.PersistentFlags().IntVar(&portFlag, "port", 0, "proxy port (overrides narc.json)")

	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(shellCmd)
	rootCmd.AddCommand(versionCmd)
}
