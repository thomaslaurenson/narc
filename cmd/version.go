package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version is set at build time using:
// -ldflags "-X github.com/thomaslaurenson/narc/cmd.Version=...".
// It falls back to "dev" for local builds that do not inject a value.
var Version = "dev"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the narc version",
	Args:  cobra.NoArgs,
	// Override the root PersistentPreRunE so that `narc version` never
	// tries to load or create narc.json.
	PersistentPreRunE: func(_ *cobra.Command, _ []string) error { return nil },
	Run: func(_ *cobra.Command, _ []string) {
		fmt.Printf("narc version %s\n", Version)
	},
}
