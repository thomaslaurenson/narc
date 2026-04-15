package catalog

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
)

type Entry struct {
	ServiceType string
	BaseURL     string
}

type Catalog struct {
	mu      sync.RWMutex
	entries []Entry
}

func NewCatalog() *Catalog {
	return &Catalog{}
}

// keystoneResponse represents the relevant subset of a Keystone v3 token response.
type keystoneResponse struct {
	Token struct {
		Catalog []struct {
			Type      string `json:"type"`
			Endpoints []struct {
				Interface string `json:"interface"`
				URL       string `json:"url"`
			} `json:"endpoints"`
		} `json:"catalog"`
	} `json:"token"`
}

// Update replaces the current catalog entries by parsing a Keystone v3 token response body.
// Only public-interface endpoints are included. Safe for concurrent use.
func (c *Catalog) Update(body []byte) error {
	var resp keystoneResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("parse keystone response: %w", err)
	}

	var entries []Entry
	for _, svc := range resp.Token.Catalog {
		for _, ep := range svc.Endpoints {
			if ep.Interface != "public" {
				continue
			}
			if ep.URL == "" || svc.Type == "" {
				continue
			}
			entries = append(entries, Entry{
				ServiceType: svc.Type,
				BaseURL:     ep.URL,
			})
		}
	}

	// Sort longest BaseURL first so Lookup can return on the first match.
	sort.Slice(entries, func(i, j int) bool {
		return len(entries[i].BaseURL) > len(entries[j].BaseURL)
	})

	c.mu.Lock()
	c.entries = entries
	c.mu.Unlock()
	return nil
}

// Lookup returns the catalog Entry whose BaseURL is the longest prefix of requestURL.
// Returns (Entry{}, false) if no entry matches.
// The request URL is normalized (default port stripped) before matching so that
// goproxy-reconstructed HTTPS URLs (e.g. https://host:443/path) match catalog
// entries that omit the default port.
func (c *Catalog) Lookup(requestURL string) (Entry, bool) {
	normalized := stripDefaultPort(requestURL)
	c.mu.RLock()
	defer c.mu.RUnlock()

	// entries are sorted longest-first by Update, so the first match is the longest prefix.
	for _, e := range c.entries {
		if hasURLPrefix(normalized, e.BaseURL) {
			return e, true
		}
	}
	return Entry{}, false
}

// hasURLPrefix reports whether s starts with prefix and the match ends at a
// URL path boundary - i.e., prefix ends with '/', or s has no further characters,
// or the next character in s after the prefix is '/'.
// This prevents a catalog entry like "https://api.example.com/volume" (no trailing
// slash) from matching a request to "https://api.example.com/volumev3/snapshots".
func hasURLPrefix(s, prefix string) bool {
	if !strings.HasPrefix(s, prefix) {
		return false
	}
	rest := s[len(prefix):]
	return len(rest) == 0 || rest[0] == '/' || strings.HasSuffix(prefix, "/")
}

// stripDefaultPort removes the explicit port from a URL when it is the default
// for the scheme (443 for https, 80 for http).
func stripDefaultPort(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	port := u.Port()
	if (u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80") {
		u.Host = u.Hostname()
	}
	return u.String()
}

// IsReady returns true if at least one catalog entry has been loaded.
func (c *Catalog) IsReady() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries) > 0
}

// Len returns the number of entries currently in the catalog.
func (c *Catalog) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}
