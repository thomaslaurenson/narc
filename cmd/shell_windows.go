//go:build windows

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var shellCmd = &cobra.Command{
	Use:   "shell",
	Short: "Start an interactive shell with all OpenStack API calls recorded",
	Long: `Launches an interactive shell with the narc proxy pre-configured.

Note: the interactive shell subcommand requires a Unix PTY and is not supported
on Windows. Use 'narc run -- <command>' to record a specific command instead.`,
	RunE: func(_ *cobra.Command, _ []string) error {
		return fmt.Errorf("the shell subcommand is not supported on Windows; use 'narc run -- <command>' instead")
	},
}

func init() {
	// No flags registered on Windows — the command always returns an error.
}
