package output

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteUnmatchedCreateAndAppend(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "unmatched.log")

	// First write — file should be created.
	if err := WriteUnmatched(path, "https://example.com/first"); err != nil {
		t.Fatalf("first WriteUnmatched: %v", err)
	}

	// Second write — content should be appended, not overwritten.
	if err := WriteUnmatched(path, "https://example.com/second"); err != nil {
		t.Fatalf("second WriteUnmatched: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	got := string(data)
	want := "https://example.com/first\nhttps://example.com/second\n"
	if got != want {
		t.Errorf("file contents:\ngot:  %q\nwant: %q", got, want)
	}
}
