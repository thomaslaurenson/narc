package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadReturnsErrNotFoundWhenMissing(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	dir := filepath.Join(tmp, ".narc")

	_, err := Load(dir)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Load: got %v, want ErrNotFound", err)
	}
}

func TestDefaultsValues(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	dir := filepath.Join(tmp, ".narc")

	cfg := Defaults(dir)
	if cfg.ProxyPort != DefaultProxyPort {
		t.Errorf("ProxyPort: got %d, want %d", cfg.ProxyPort, DefaultProxyPort)
	}
	wantOutput := filepath.Join(dir, DefaultOutputFile)
	if cfg.OutputFile != wantOutput {
		t.Errorf("OutputFile: got %q, want %q", cfg.OutputFile, wantOutput)
	}
	wantLog := filepath.Join(dir, DefaultLogFile)
	if cfg.LogFile != wantLog {
		t.Errorf("LogFile: got %q, want %q", cfg.LogFile, wantLog)
	}

	if err := cfg.Save(dir); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify narc.json was written with correct permissions.
	cfgPath := filepath.Join(dir, "narc.json")
	info, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatalf("narc.json not created: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("narc.json permissions: got %o, want 0600", info.Mode().Perm())
	}
}

func TestLoadRespectsExistingFile(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	dir := filepath.Join(tmp, ".narc")

	// Write a custom config.
	narcDir := dir
	if err := os.MkdirAll(narcDir, 0700); err != nil {
		t.Fatal(err)
	}
	content := `{"proxy_port": 8888, "output_file": "/srv/narc/custom-rules.json"}`
	if err := os.WriteFile(filepath.Join(narcDir, "narc.json"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ProxyPort != 8888 {
		t.Errorf("ProxyPort: got %d, want 8888", cfg.ProxyPort)
	}
	if cfg.OutputFile != "/srv/narc/custom-rules.json" {
		t.Errorf("OutputFile: got %q, want /srv/narc/custom-rules.json", cfg.OutputFile)
	}
}

func TestLoadFallsBackToDefaultsForZeroValues(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	dir := filepath.Join(tmp, ".narc")

	narcDir := dir
	if err := os.MkdirAll(narcDir, 0700); err != nil {
		t.Fatal(err)
	}
	// Partial config - missing output_file and log_file.
	if err := os.WriteFile(filepath.Join(narcDir, "narc.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ProxyPort != DefaultProxyPort {
		t.Errorf("ProxyPort: got %d, want %d", cfg.ProxyPort, DefaultProxyPort)
	}
	wantOutput := filepath.Join(dir, DefaultOutputFile)
	if cfg.OutputFile != wantOutput {
		t.Errorf("OutputFile: got %q, want %q", cfg.OutputFile, wantOutput)
	}
	wantLog := filepath.Join(dir, DefaultLogFile)
	if cfg.LogFile != wantLog {
		t.Errorf("LogFile: got %q, want %q", cfg.LogFile, wantLog)
	}
}

func TestLoadMigratesBareFilename(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	dir := filepath.Join(tmp, ".narc")

	narcDir := dir
	if err := os.MkdirAll(narcDir, 0700); err != nil {
		t.Fatal(err)
	}
	// Bare filenames without any directory component are migrated to ~/.narc/.
	content := `{"output_file": "rules.json", "log_file": "unmatched.log"}`
	if err := os.WriteFile(filepath.Join(narcDir, "narc.json"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	wantOutput := filepath.Join(narcDir, "rules.json")
	if cfg.OutputFile != wantOutput {
		t.Errorf("OutputFile: got %q, want %q", cfg.OutputFile, wantOutput)
	}
	wantLog := filepath.Join(narcDir, "unmatched.log")
	if cfg.LogFile != wantLog {
		t.Errorf("LogFile: got %q, want %q", cfg.LogFile, wantLog)
	}
}

func TestConfigSaveRoundTrip(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	dir := filepath.Join(tmp, ".narc")

	original := &Config{
		ProxyPort:  8080,
		OutputFile: "/tmp/custom_rules.json",
		LogFile:    "/tmp/custom.log",
	}
	if err := original.Save(dir); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.ProxyPort != original.ProxyPort {
		t.Errorf("ProxyPort: got %d, want %d", loaded.ProxyPort, original.ProxyPort)
	}
	if loaded.OutputFile != original.OutputFile {
		t.Errorf("OutputFile: got %q, want %q", loaded.OutputFile, original.OutputFile)
	}
	if loaded.LogFile != original.LogFile {
		t.Errorf("LogFile: got %q, want %q", loaded.LogFile, original.LogFile)
	}
}
