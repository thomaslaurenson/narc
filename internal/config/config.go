// Package config loads and persists narc's runtime configuration from ~/.narc/narc.json.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	DefaultProxyPort  = 9099
	DefaultOutputFile = "access_rules.json"
	DefaultLogFile    = "unmatched_requests.log"
	configFilename    = "narc.json"
	narcDirName       = ".narc"
)

// Config holds the resolved settings for a narc session.
type Config struct {
	ProxyPort  int    `json:"proxy_port"`
	OutputFile string `json:"output_file"`
	LogFile    string `json:"log_file"`
}

// ErrNotFound is returned by Load when the config file does not exist.
var ErrNotFound = errors.New("config file not found")

// NarcDirPath returns the path to the narc configuration directory without
// creating it. Use this when only the path is needed (e.g. read-only lookups).
func NarcDirPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, narcDirName), nil
}

// NarcDir returns the path to the narc configuration directory, creating it if
// it does not already exist. Use this before any write operations.
func NarcDir() (string, error) {
	dir, err := NarcDirPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}

// Load reads the narc configuration file from ~/.narc/narc.json.
// Returns ErrNotFound if the file does not exist.
func Load() (*Config, error) {
	dir, err := NarcDirPath()
	if err != nil {
		return nil, err
	}

	path := filepath.Join(dir, configFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	c := Defaults()
	if err := json.Unmarshal(data, c); err != nil {
		return nil, err
	}

	// Enforce defaults for zero-value fields (read-tolerant).
	d := Defaults()
	if c.ProxyPort == 0 {
		c.ProxyPort = DefaultProxyPort
	}
	if c.OutputFile == "" {
		c.OutputFile = d.OutputFile
	}
	if c.LogFile == "" {
		c.LogFile = d.LogFile
	}

	// Migrate bare filenames (no directory component) to ~/.narc/.
	// Any filename without a path separator would otherwise resolve to CWD.
	if !filepath.IsAbs(c.OutputFile) && filepath.Dir(c.OutputFile) == "." {
		c.OutputFile = filepath.Join(dir, c.OutputFile)
	}
	if !filepath.IsAbs(c.LogFile) && filepath.Dir(c.LogFile) == "." {
		c.LogFile = filepath.Join(dir, c.LogFile)
	}

	if c.ProxyPort < 1 || c.ProxyPort > 65535 {
		return nil, fmt.Errorf("proxy_port %d is out of range (1-65535)", c.ProxyPort)
	}

	return c, nil
}

// Save writes the configuration to ~/.narc/narc.json, creating the directory
// if it does not exist.
func (c *Config) Save() error {
	dir, err := NarcDir()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(c, "", "    ")
	if err != nil {
		return err
	}

	path := filepath.Join(dir, configFilename)
	return os.WriteFile(path, data, 0600)
}

// Defaults returns a Config populated with sensible defaults anchored to ~/.narc/.
func Defaults() *Config {
	dir, err := NarcDirPath()
	if err != nil {
		// Fallback to relative names if home directory is unavailable.
		return &Config{
			ProxyPort:  DefaultProxyPort,
			OutputFile: DefaultOutputFile,
			LogFile:    DefaultLogFile,
		}
	}
	return &Config{
		ProxyPort:  DefaultProxyPort,
		OutputFile: filepath.Join(dir, DefaultOutputFile),
		LogFile:    filepath.Join(dir, DefaultLogFile),
	}
}
