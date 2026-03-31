package output

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUnmatchedLogWriteAndAppend(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "unmatched.log")

	// First session: create file and write one entry.
	log1, err := OpenUnmatchedLog(path)
	if err != nil {
		t.Fatalf("OpenUnmatchedLog (1st): %v", err)
	}
	if err := log1.Write("https://example.com/first"); err != nil {
		t.Fatalf("first Write: %v", err)
	}
	if err := log1.Close(); err != nil {
		t.Fatalf("Close (1st): %v", err)
	}

	// Second session: reopen and append — must not truncate.
	log2, err := OpenUnmatchedLog(path)
	if err != nil {
		t.Fatalf("OpenUnmatchedLog (2nd): %v", err)
	}
	if err := log2.Write("https://example.com/second"); err != nil {
		t.Fatalf("second Write: %v", err)
	}
	if err := log2.Close(); err != nil {
		t.Fatalf("Close (2nd): %v", err)
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
