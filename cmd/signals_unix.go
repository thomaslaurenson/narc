//go:build !windows

package cmd

import (
	"os"
	"syscall"
)

// shutdownSignals is the set of signals that trigger a graceful shutdown.
// On Unix, SIGHUP is included so that background processes shut down cleanly
// when the controlling terminal closes.
var shutdownSignals = []os.Signal{
	syscall.SIGINT,
	syscall.SIGTERM,
	syscall.SIGHUP,
}
