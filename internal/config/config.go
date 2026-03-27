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

type Config struct {
	ProxyPort  int    `json:"proxy_port"`
	OutputFile string `json:"output_file"`
	LogFile    string `json:"log_file"`
}

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

func Load() (*Config, error) {
	dir, err := NarcDirPath()
	if err != nil {
		return nil, err
	}

	path := filepath.Join(dir, configFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			c := defaults()
			return c, c.Save()
		}
		return nil, err
	}

	c := defaults()
	if err := json.Unmarshal(data, c); err != nil {
		return nil, err
	}

	// Enforce defaults for zero-value fields (read-tolerant).
	d := defaults()
	if c.ProxyPort == 0 {
		c.ProxyPort = DefaultProxyPort
	}
	if c.OutputFile == "" {
		c.OutputFile = d.OutputFile
	}
	if c.LogFile == "" {
		c.LogFile = d.LogFile
	}

	// Migrate bare filenames written by older narc versions (before paths defaulted
	// to ~/.narc/). A value equal to just the bare filename has no directory component
	// and would resolve to CWD — replace it with the correct ~/.narc/ path.
	if c.OutputFile == DefaultOutputFile {
		c.OutputFile = d.OutputFile
	}
	if c.LogFile == DefaultLogFile {
		c.LogFile = d.LogFile
	}

	if c.ProxyPort < 1 || c.ProxyPort > 65535 {
		return nil, fmt.Errorf("proxy_port %d is out of range (1-65535)", c.ProxyPort)
	}

	return c, nil
}

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

func defaults() *Config {
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
