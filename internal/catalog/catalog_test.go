package catalog

import (
	"encoding/json"
	"testing"
)

func keystoneBody(t *testing.T, entries []map[string]any) []byte {
	t.Helper()
	payload := map[string]any{"token": map[string]any{"catalog": entries}}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func svcEntry(svcType, iface, url string) map[string]any {
	return map[string]any{
		"type":      svcType,
		"endpoints": []map[string]any{{"interface": iface, "url": url}},
	}
}

func TestUpdateAndLookup(t *testing.T) {
	t.Parallel()
	c := NewCatalog()
	if c.IsReady() {
		t.Fatal("should not be ready before Update")
	}
	body := keystoneBody(t, []map[string]any{
		svcEntry("compute", "public", "https://compute.example.com/"),
		svcEntry("identity", "public", "https://identity.example.com/"),
		svcEntry("network", "internal", "https://network-internal.example.com/"),
	})
	if err := c.Update(body); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !c.IsReady() {
		t.Fatal("should be ready after Update")
	}
	if c.Len() != 2 {
		t.Errorf("Len: got %d, want 2 (internal excluded)", c.Len())
	}
	e, ok := c.Lookup("https://compute.example.com/v2.1/servers")
	if !ok || e.ServiceType != "compute" {
		t.Errorf("compute lookup: got (%+v,%v)", e, ok)
	}
	e, ok = c.Lookup("https://identity.example.com/v3/auth/tokens")
	if !ok || e.ServiceType != "identity" {
		t.Errorf("identity lookup: got (%+v,%v)", e, ok)
	}
	_, ok = c.Lookup("https://unknown.example.com/foo")
	if ok {
		t.Error("unknown URL should return false")
	}
}

func TestUpdateReplacesEntries(t *testing.T) {
	t.Parallel()
	c := NewCatalog()
	if err := c.Update(keystoneBody(t, []map[string]any{svcEntry("compute", "public", "https://old.example.com/")})); err != nil {
		t.Fatalf("first Update: %v", err)
	}
	if err := c.Update(keystoneBody(t, []map[string]any{
		svcEntry("compute", "public", "https://new.example.com/"),
		svcEntry("network", "public", "https://network.example.com/"),
	})); err != nil {
		t.Fatalf("second Update: %v", err)
	}
	if c.Len() != 2 {
		t.Errorf("Len after replacement: got %d, want 2", c.Len())
	}
	_, ok := c.Lookup("https://old.example.com/v2.1/servers")
	if ok {
		t.Error("old URL should not match after replacement")
	}
}

func TestUpdateInvalidJSON(t *testing.T) {
	t.Parallel()
	c := NewCatalog()
	if err := c.Update([]byte("not json")); err == nil {
		t.Error("Update with invalid JSON should return error")
	}
}

func TestUpdateSkipsEmptyFields(t *testing.T) {
	t.Parallel()
	c := NewCatalog()
	body := keystoneBody(t, []map[string]any{
		{"type": "compute", "endpoints": []map[string]any{{"interface": "public", "url": ""}}},
		svcEntry("identity", "public", "https://identity.example.com/"),
	})
	if err := c.Update(body); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if c.Len() != 1 {
		t.Errorf("Len: got %d, want 1 (empty URL skipped)", c.Len())
	}
}

func TestLookupStripsDefaultPort(t *testing.T) {
	t.Parallel()
	c := NewCatalog()
	body := keystoneBody(t, []map[string]any{
		svcEntry("identity", "public", "https://identity.example.com/"),
		svcEntry("compute", "public", "http://compute.example.com/"),
	})
	if err := c.Update(body); err != nil {
		t.Fatalf("Update: %v", err)
	}

	tests := []struct {
		url     string
		wantSvc string
		wantOK  bool
	}{
		// goproxy reconstructs HTTPS URLs with explicit :443
		{"https://identity.example.com:443/v3/users?name=foo", "identity", true},
		// goproxy reconstructs HTTP URLs with explicit :80
		{"http://compute.example.com:80/v2.1/servers", "compute", true},
		// Non-default port must NOT be stripped
		{"https://identity.example.com:8443/v3/users", "", false},
		// Without port — still matches
		{"https://identity.example.com/v3/auth/tokens", "identity", true},
	}

	for _, tc := range tests {
		e, ok := c.Lookup(tc.url)
		if ok != tc.wantOK {
			t.Errorf("Lookup(%q): ok=%v, want %v", tc.url, ok, tc.wantOK)
			continue
		}
		if ok && e.ServiceType != tc.wantSvc {
			t.Errorf("Lookup(%q): service=%q, want %q", tc.url, e.ServiceType, tc.wantSvc)
		}
	}
}

// TestLookupLongestPrefix verifies that when two catalog entries share a URL prefix,
// the one with the longer (more specific) BaseURL wins.
func TestLookupLongestPrefix(t *testing.T) {
	t.Parallel()
	c := NewCatalog()
	body := keystoneBody(t, []map[string]any{
		svcEntry("volumev3", "public", "https://api.example.com/volume/v3/"),
		svcEntry("volume", "public", "https://api.example.com/volume/"),
	})
	if err := c.Update(body); err != nil {
		t.Fatalf("Update: %v", err)
	}

	e, ok := c.Lookup("https://api.example.com/volume/v3/abc123/snapshots")
	if !ok {
		t.Fatal("Lookup: expected a match, got none")
	}
	if e.ServiceType != "volumev3" {
		t.Errorf("Lookup: got service %q, want %q (longer prefix should win)", e.ServiceType, "volumev3")
	}
}

func TestLookupNoBoundaryFalseMatch(t *testing.T) {
	t.Parallel()
	c := NewCatalog()
	body := keystoneBody(t, []map[string]any{
		svcEntry("volume", "public", "https://api.example.com/volume"),
	})
	if err := c.Update(body); err != nil {
		t.Fatalf("Update: %v", err)
	}
	_, ok := c.Lookup("https://api.example.com/volumev3/snapshots")
	if ok {
		t.Error("Lookup should not match /volumev3/ against BaseURL /volume (no trailing slash)")
	}
}
