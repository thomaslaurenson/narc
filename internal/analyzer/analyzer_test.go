package analyzer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/thomaslaurenson/narc/internal/catalog"
)

// buildCatalog creates and populates a Catalog from an ordered slice of
// (serviceType, baseURL) pairs. A slice is used intentionally to give
// deterministic ordering, since iterating a map is non-deterministic.
func buildCatalog(t *testing.T, entries [][2]string) *catalog.Catalog {
	t.Helper()
	var svcList []map[string]any
	for _, e := range entries {
		svcList = append(svcList, map[string]any{
			"type":      e[0],
			"endpoints": []map[string]any{{"interface": "public", "url": e[1]}},
		})
	}
	body, err := json.Marshal(map[string]any{"token": map[string]any{"catalog": svcList}})
	if err != nil {
		t.Fatalf("marshal catalog: %v", err)
	}
	c := catalog.NewCatalog()
	if err := c.Update(body); err != nil {
		t.Fatalf("catalog update: %v", err)
	}
	return c
}

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		name    string
		rawURL  string
		baseURL string
		want    string
	}{
		{
			name:    "simple path with trailing slash on base",
			rawURL:  "https://compute.example.com/v2.1/servers",
			baseURL: "https://compute.example.com/",
			want:    "/v2.1/servers",
		},
		{
			name:    "UUID segment wildcarded",
			rawURL:  "https://compute.example.com/v2.1/servers/550e8400-e29b-41d4-a716-446655440000",
			baseURL: "https://compute.example.com/",
			want:    "/v2.1/servers/*",
		},
		{
			name:    "numeric segment wildcarded",
			rawURL:  "https://compute.example.com/v2.1/servers/12345",
			baseURL: "https://compute.example.com/",
			want:    "/v2.1/servers/*",
		},
		{
			name:    "UUID in middle of path uses single-segment wildcard",
			rawURL:  "https://compute.example.com/v2.1/servers/550e8400-e29b-41d4-a716-446655440000/ips",
			baseURL: "https://compute.example.com/",
			want:    "/v2.1/servers/*/ips",
		},
		{
			name:    "query parameters stripped",
			rawURL:  "https://compute.example.com/v2.1/servers?sort_key=name&limit=10",
			baseURL: "https://compute.example.com/",
			want:    "/v2.1/servers",
		},
		{
			name:    "trailing slash appends **",
			rawURL:  "https://compute.example.com/v2.1/servers/",
			baseURL: "https://compute.example.com/",
			want:    "/v2.1/servers/**",
		},
		{
			name:    "base URL without trailing slash",
			rawURL:  "https://compute.example.com/v2.1/servers",
			baseURL: "https://compute.example.com",
			want:    "/v2.1/servers",
		},
		{
			name:    "multiple wildcarded segments",
			rawURL:  "https://volume.example.com/v3/550e8400-e29b-41d4-a716-446655440000/snapshots/660f9500-f30c-52e5-b827-557766551111",
			baseURL: "https://volume.example.com/",
			want:    "/v3/*/snapshots/*",
		},
		{
			name:    "version segment not wildcarded",
			rawURL:  "https://compute.example.com/v2.1/flavors",
			baseURL: "https://compute.example.com/",
			want:    "/v2.1/flavors",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizePath(tc.rawURL, tc.baseURL)
			if got != tc.want {
				t.Errorf("normalizePath(%q, %q) = %q, want %q",
					tc.rawURL, tc.baseURL, got, tc.want)
			}
		})
	}
}

func TestProcessDeduplication(t *testing.T) {
	cat := buildCatalog(t, [][2]string{
		{"compute", "https://compute.example.com/"},
	})

	az := New(cat, nil, nil, nil)

	az.Process("GET", "https://compute.example.com/v2.1/servers")
	az.Process("GET", "https://compute.example.com/v2.1/servers")  // duplicate
	az.Process("POST", "https://compute.example.com/v2.1/servers") // different method

	rules := az.Rules()
	if len(rules) != 2 {
		t.Fatalf("expected 2 unique rules, got %d", len(rules))
	}
}

func TestProcessOnNewCallback(t *testing.T) {
	cat := buildCatalog(t, [][2]string{
		{"compute", "https://compute.example.com/"},
		{"network", "https://network.example.com/"},
	})

	var called []AccessRule
	az := New(cat, nil, func(r AccessRule) {
		called = append(called, r)
	}, nil)

	az.Process("GET", "https://compute.example.com/v2.1/servers")
	az.Process("GET", "https://compute.example.com/v2.1/servers") // duplicate — callback not called again
	az.Process("GET", "https://network.example.com/v2.0/networks")

	if len(called) != 2 {
		t.Fatalf("expected onNew called 2 times, got %d", len(called))
	}
	if called[0].Service != "compute" || called[0].Method != "GET" {
		t.Errorf("first rule: got %+v", called[0])
	}
	if called[1].Service != "network" || called[1].Method != "GET" {
		t.Errorf("second rule: got %+v", called[1])
	}
}

// TestProcessOnNewCallbackCanCallRules verifies that the onNew callback is invoked
// with the mutex released, so calling Rules() or Process() inside the callback
// does not deadlock.
func TestProcessOnNewCallbackCanCallRules(t *testing.T) {
	cat := buildCatalog(t, [][2]string{
		{"compute", "https://compute.example.com/"},
	})

	done := make(chan struct{})
	var az *Analyzer
	az = New(cat, nil, func(r AccessRule) {
		// If the mutex were still held this would deadlock.
		rules := az.Rules()
		if len(rules) == 0 {
			t.Error("Rules() returned empty slice inside onNew callback")
		}
		close(done)
	}, nil)

	az.Process("GET", "https://compute.example.com/v2.1/servers")

	select {
	case <-done:
	default:
		t.Error("onNew callback was never called")
	}
}

func TestProcessUnknownURL(t *testing.T) {
	cat := buildCatalog(t, [][2]string{
		{"compute", "https://compute.example.com/"},
	})

	az := New(cat, nil, nil, nil)
	az.Process("GET", "https://unknown.example.com/v2.1/servers")

	if len(az.Rules()) != 0 {
		t.Error("expected no rules for unmatched URL")
	}
}

func TestRulesReturnsCopy(t *testing.T) {
	cat := buildCatalog(t, [][2]string{
		{"compute", "https://compute.example.com/"},
	})

	az := New(cat, nil, nil, nil)
	az.Process("GET", "https://compute.example.com/v2.1/servers")

	rules1 := az.Rules()
	rules1[0].Service = "mutated"

	rules2 := az.Rules()
	if rules2[0].Service == "mutated" {
		t.Error("Rules() should return a copy, not a reference")
	}
}

func TestWriteRulesPermissions(t *testing.T) {
	cat := buildCatalog(t, [][2]string{
		{"compute", "https://compute.example.com/"},
	})
	az := New(cat, nil, nil, nil)
	az.Process("GET", "https://compute.example.com/v2.1/servers")

	out := filepath.Join(t.TempDir(), "rules.json")
	if _, err := az.WriteRules(out); err != nil {
		t.Fatalf("WriteRules: %v", err)
	}

	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("stat output file: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("file permissions: got %04o, want 0600", info.Mode().Perm())
	}
}
