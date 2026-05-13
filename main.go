// Package main is the entry point for the narc command-line tool.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/thomaslaurenson/narc/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		var ec *cmd.ExitCodeError
		if errors.As(err, &ec) {
			os.Exit(ec.Code)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
