// Package output provides the UnmatchedLog writer for recording HTTP requests
// that do not match any catalog entry.
package output

import (
	"os"
	"sync"
)

// UnmatchedLog writes unmatched request URLs to a file. It holds an open file
// handle and a mutex to serialise concurrent writes from the proxy and analyzer
// goroutines.
type UnmatchedLog struct {
	mu sync.Mutex
	f  *os.File
}

// OpenUnmatchedLog opens path for appending (creating it if absent) and
// returns an UnmatchedLog ready for use. The caller must call Close when done.
func OpenUnmatchedLog(path string) (*UnmatchedLog, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return nil, err
	}
	return &UnmatchedLog{f: f}, nil
}

// Write appends rawURL followed by a newline to the log file.
func (l *UnmatchedLog) Write(rawURL string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, err := l.f.WriteString(rawURL + "\n")
	return err
}

// Close flushes and closes the underlying file handle.
func (l *UnmatchedLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.f.Sync(); err != nil {
		// Best-effort: still attempt to close even if sync fails.
		_ = l.f.Close()
		return err
	}
	return l.f.Close()
}
