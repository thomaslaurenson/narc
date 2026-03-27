package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCreatesDefaultsWhenMissing(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ProxyPort != DefaultProxyPort {
		t.Errorf("ProxyPort: got %d, want %d", cfg.ProxyPort, DefaultProxyPort)
	}
	wantOutput := filepath.Join(tmp, ".narc", DefaultOutputFile)
	if cfg.OutputFile != wantOutput {
		t.Errorf("OutputFile: got %q, want %q", cfg.OutputFile, wantOutput)
	}
	wantLog := filepath.Join(tmp, ".narc", DefaultLogFile)
	if cfg.LogFile != wantLog {
		t.Errorf("LogFile: got %q, want %q", cfg.LogFile, wantLog)
	}

	// Verify narc.json was written with correct permissions.
	cfgPath := filepath.Join(tmp, ".narc", "narc.json")
	info, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatalf("narc.json not created: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("narc.json permissions: got %o, want 0600", info.Mode().Perm())
	}
}

func TestLoadRespectsExistingFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// Write a custom config.
	narcDir := filepath.Join(tmp, ".narc")
	if err := os.MkdirAll(narcDir, 0700); err != nil {
		t.Fatal(err)
	}
	content := `{"proxy_port": 8888, "output_file": "custom.json"}`
	if err := os.WriteFile(filepath.Join(narcDir, "narc.json"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ProxyPort != 8888 {
		t.Errorf("ProxyPort: got %d, want 8888", cfg.ProxyPort)
	}
	if cfg.OutputFile != "custom.json" {
		t.Errorf("OutputFile: got %q, want custom.json", cfg.OutputFile)
	}
}

func TestLoadFallsBackToDefaultsForZeroValues(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	narcDir := filepath.Join(tmp, ".narc")
	if err := os.MkdirAll(narcDir, 0700); err != nil {
		t.Fatal(err)
	}
	// Partial config — missing output_file and log_file.
	if err := os.WriteFile(filepath.Join(narcDir, "narc.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ProxyPort != DefaultProxyPort {
		t.Errorf("ProxyPort: got %d, want %d", cfg.ProxyPort, DefaultProxyPort)
	}
	wantOutput := filepath.Join(tmp, ".narc", DefaultOutputFile)
	if cfg.OutputFile != wantOutput {
		t.Errorf("OutputFile: got %q, want %q", cfg.OutputFile, wantOutput)
	}
	wantLog := filepath.Join(tmp, ".narc", DefaultLogFile)
	if cfg.LogFile != wantLog {
		t.Errorf("LogFile: got %q, want %q", cfg.LogFile, wantLog)
	}
}

func TestConfigSaveRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	original := &Config{
		ProxyPort:  8080,
		OutputFile: "/tmp/custom_rules.json",
		LogFile:    "/tmp/custom.log",
	}
	if err := original.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load()
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
