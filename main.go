// Command narc records the OpenStack API calls a command makes and generates
// the access rules an application credential needs to make them.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/thomaslaurenson/narc/cmd"
)

func main() {
	// SIGHUP is included so a background proxy still writes its rules when the
	// controlling terminal closes.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()

	root := cmd.NewRootCmd(os.Environ(), os.UserHomeDir, os.Stdout, os.Stderr)
	if err := root.ExecuteContext(ctx); err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			os.Exit(130)
		}
		var ec *cmd.ExitCodeError
		if errors.As(err, &ec) {
			os.Exit(ec.Code)
		}
		fmt.Fprintf(os.Stderr, "[!] %v\n", err)
		os.Exit(1)
	}
}
