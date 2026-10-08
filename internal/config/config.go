// Package config loads and persists narc's runtime configuration, kept in
// narc.json in the narc directory.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const (
	// DefaultProxyPort is the port the proxy listens on unless configured
	// otherwise.
	DefaultProxyPort = 9099
	// DefaultOutputFile is the name of the access rules file in the narc
	// directory.
	DefaultOutputFile = "access_rules.json"
	// DefaultLogFile is the name of the unmatched request log in the narc
	// directory.
	DefaultLogFile = "unmatched_requests.log"

	dirName        = ".narc"
	configFilename = "narc.json"
)

// Config holds the resolved settings for a narc session.
type Config struct {
	ProxyPort  int    `json:"proxy_port"`
	OutputFile string `json:"output_file"`
	LogFile    string `json:"log_file"`
}

// ErrNotFound is returned by Load when the config file does not exist.
var ErrNotFound = errors.New("config file not found")

// Dir returns the narc directory under home.
func Dir(home string) string {
	return filepath.Join(home, dirName)
}

// Load reads narc.json from dir. It returns ErrNotFound when the file does not
// exist.
func Load(dir string) (*Config, error) {
	path := filepath.Join(dir, configFilename)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	c := Defaults(dir)
	if err := json.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("parse %q: %w", path, err)
	}

	// A field the file sets to its zero value falls back to the default.
	if c.ProxyPort == 0 {
		c.ProxyPort = DefaultProxyPort
	}
	if c.OutputFile == "" {
		c.OutputFile = filepath.Join(dir, DefaultOutputFile)
	}
	if c.LogFile == "" {
		c.LogFile = filepath.Join(dir, DefaultLogFile)
	}

	// A bare filename is kept in dir, since it would otherwise resolve against
	// whichever directory narc happens to be run from.
	if !filepath.IsAbs(c.OutputFile) && filepath.Dir(c.OutputFile) == "." {
		c.OutputFile = filepath.Join(dir, c.OutputFile)
	}
	if !filepath.IsAbs(c.LogFile) && filepath.Dir(c.LogFile) == "." {
		c.LogFile = filepath.Join(dir, c.LogFile)
	}

	if c.ProxyPort < 1 || c.ProxyPort > 65535 {
		return nil, fmt.Errorf("proxy_port %d in %q is out of range (1-65535)", c.ProxyPort, path)
	}

	return c, nil
}

// Save writes the configuration to narc.json in dir, creating dir if it does
// not exist.
func (c *Config) Save(dir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(c, "", "    ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	return os.WriteFile(filepath.Join(dir, configFilename), data, 0600)
}

// Defaults returns a Config populated with the default settings, with its files
// kept in dir.
func Defaults(dir string) *Config {
	return &Config{
		ProxyPort:  DefaultProxyPort,
		OutputFile: filepath.Join(dir, DefaultOutputFile),
		LogFile:    filepath.Join(dir, DefaultLogFile),
	}
}
