package analyzer

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/thomaslaurenson/narc/internal/catalog"
	"github.com/thomaslaurenson/narc/internal/output"
)

var (
	uuidRE    = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	numericRE = regexp.MustCompile(`^\d+$`)
)

// AccessRule is a single normalized OpenStack API access rule.
type AccessRule struct {
	Service string `json:"service"`
	Method  string `json:"method"`
	Path    string `json:"path"`
}

// Analyzer classifies intercepted requests, normalizes their paths, deduplicates,
// and accumulates AccessRule entries.
type Analyzer struct {
	mu      sync.Mutex
	seen    map[string]bool
	rules   []AccessRule
	catalog *catalog.Catalog
	onNew   func(AccessRule)
	// OnUnmatched is called when the catalog is ready but a URL does not match any
	// catalog entry. Useful for debug logging; may be nil.
	OnUnmatched func(method, url string)
	// LogFile is the path to the file where unmatched URLs are appended.
	// If empty, unmatched URLs are silently discarded.
	LogFile string
}

// New creates an Analyzer backed by the given Catalog.
// onNew is called after the lock is released whenever a new unique rule is found; may be nil.
func New(c *catalog.Catalog, onNew func(AccessRule)) *Analyzer {
	return &Analyzer{
		seen:    make(map[string]bool),
		catalog: c,
		onNew:   onNew,
	}
}

// HandleRequest implements proxy.RequestHandler.
func (a *Analyzer) HandleRequest(method, rawURL string) {
	a.Process(method, rawURL)
}

// Process classifies rawURL using the catalog, normalizes the path, deduplicates,
// and stores the rule. Unclassified URLs (when catalog is ready) are appended to
// unmatched_requests.log.
func (a *Analyzer) Process(method, rawURL string) {
	entry, ok := a.catalog.Lookup(rawURL)
	if !ok {
		if a.catalog.IsReady() {
			if a.LogFile != "" {
				_ = output.WriteUnmatched(a.LogFile, rawURL)
			}
			if a.OnUnmatched != nil {
				a.OnUnmatched(method, rawURL)
			}
		}
		return
	}

	path := normalizePath(rawURL, entry.BaseURL)
	key := method + "|" + path

	a.mu.Lock()
	if a.seen[key] {
		a.mu.Unlock()
		return
	}
	a.seen[key] = true
	rule := AccessRule{Service: entry.ServiceType, Method: method, Path: path}
	a.rules = append(a.rules, rule)
	a.mu.Unlock()

	if a.onNew != nil {
		a.onNew(rule)
	}
}

// Rules returns a copy of all accumulated rules.
func (a *Analyzer) Rules() []AccessRule {
	a.mu.Lock()
	defer a.mu.Unlock()
	cp := make([]AccessRule, len(a.rules))
	copy(cp, a.rules)
	return cp
}

// WriteRules marshals the accumulated rules to JSON and writes them to outPath.
// The file is created/truncated with mode 0600.
func (a *Analyzer) WriteRules(outPath string) error {
	rules := a.Rules()
	data, err := json.MarshalIndent(rules, "", "    ")
	if err != nil {
		return fmt.Errorf("marshal rules: %w", err)
	}
	//nolint:gosec // file intentionally world-readable
	if err := os.WriteFile(outPath, data, 0644); err != nil {
		return fmt.Errorf("write rules: %w", err)
	}
	return nil
}

// normalizePath strips the BaseURL prefix, removes query parameters, wildcards UUID and
// numeric path segments, appends ** to a trailing slash, and ensures a leading slash.
func normalizePath(rawURL, baseURL string) string {
	path := strings.TrimPrefix(rawURL, baseURL)

	// If TrimPrefix had no effect, parse both URLs and strip the path component.
	if path == rawURL {
		if u, err := url.Parse(rawURL); err == nil {
			path = u.Path
			if bu, err := url.Parse(baseURL); err == nil {
				path = strings.TrimPrefix(path, strings.TrimRight(bu.Path, "/"))
			}
		}
	}

	// Strip query parameters.
	if idx := strings.Index(path, "?"); idx >= 0 {
		path = path[:idx]
	}

	// Wildcard UUID and numeric segments.
	segments := strings.Split(path, "/")
	for i, seg := range segments {
		if uuidRE.MatchString(seg) || numericRE.MatchString(seg) {
			segments[i] = "**"
		}
	}
	path = strings.Join(segments, "/")

	// Trailing slash → append **.
	if strings.HasSuffix(path, "/") {
		path += "**"
	}

	// Ensure leading /.
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	return path
}
