package output

import (
	"os"
)

// WriteUnmatched appends a raw URL to the file at path (created if absent, O_APPEND).
func WriteUnmatched(path, rawURL string) (err error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	_, err = f.WriteString(rawURL + "\n")
	return err
}
