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
	mu           sync.Mutex
	seen         map[string]bool
	rules        []AccessRule
	catalog      *catalog.Catalog
	onNew        func(AccessRule)
	onUnmatched  func(method, url string)
	unmatchedLog *output.UnmatchedLog
}

// New creates an Analyzer backed by the given Catalog.
// log is where unmatched URLs are written; nil disables file logging.
// onNew is called after the lock is released whenever a new unique rule is found; may be nil.
// onUnmatched is called for debug logging when a URL does not match any catalog entry; may be nil.
func New(c *catalog.Catalog, log *output.UnmatchedLog, onNew func(AccessRule), onUnmatched func(method, url string)) *Analyzer {
	return &Analyzer{
		seen:         make(map[string]bool),
		catalog:      c,
		onNew:        onNew,
		onUnmatched:  onUnmatched,
		unmatchedLog: log,
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
			if a.unmatchedLog != nil {
				_ = a.unmatchedLog.Write(rawURL)
			}
			if a.onUnmatched != nil {
				a.onUnmatched(method, rawURL)
			}
		}
		return
	}

	path := normalizePath(rawURL, entry.BaseURL)
	key := entry.ServiceType + "|" + method + "|" + path

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
// Returns the number of rules written. The file is created/truncated with mode 0600.
func (a *Analyzer) WriteRules(outPath string) (int, error) {
	rules := a.Rules()
	data, err := json.MarshalIndent(rules, "", "    ")
	if err != nil {
		return 0, fmt.Errorf("marshal rules: %w", err)
	}
	if err := os.WriteFile(outPath, data, 0600); err != nil {
		return 0, fmt.Errorf("write rules: %w", err)
	}
	return len(rules), nil
}

// normalizePath strips the BaseURL prefix, removes query parameters, wildcards UUID and
// numeric path segments, appends ** to a trailing slash, and ensures a leading slash.
func normalizePath(rawURL, baseURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	path := u.Path
	if bu, err := url.Parse(baseURL); err == nil {
		path = strings.TrimPrefix(path, strings.TrimRight(bu.Path, "/"))
	}

	// Wildcard UUID and numeric segments with * (single segment, no slashes).
	segments := strings.Split(path, "/")
	for i, seg := range segments {
		if uuidRE.MatchString(seg) || numericRE.MatchString(seg) {
			segments[i] = "*"
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
