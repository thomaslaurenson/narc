//go:build windows

package cmd

import (
	"os"
	"syscall"
)

// shutdownSignals is the set of signals that trigger a graceful shutdown.
var shutdownSignals = []os.Signal{
	syscall.SIGINT,
	syscall.SIGTERM,
}
