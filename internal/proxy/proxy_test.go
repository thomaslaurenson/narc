package proxy

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/thomaslaurenson/narc/internal/catalog"
)

func TestIsKeystoneAuthPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path string
		want bool
	}{
		{"/v3/auth/tokens", true},
		{"/identity/v3/auth/tokens", true},
		{"/v3/auth/tokens/extra", false},
		{"/v3/auth", false},
		{"", false},
	}
	for _, tc := range tests {
		got := isKeystoneAuthPath(tc.path)
		if got != tc.want {
			t.Errorf("isKeystoneAuthPath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestProxyNewNilCatalogAndHandler(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	p, err := New(0, false, nil, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if p == nil {
		t.Fatal("New returned nil proxy")
	}
}

// mockHandler records every HandleRequest call.
type mockHandler struct {
	calls []string
}

func (m *mockHandler) HandleRequest(method, rawURL string) {
	m.calls = append(m.calls, method+" "+rawURL)
}

func TestProxyIntegration(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// Start a trivial target HTTP server.
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	cat := catalog.NewCatalog()
	handler := &mockHandler{}

	p, err := New(0, false, cat, handler, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := p.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer p.Stop()

	// Send a plain HTTP request through the proxy.
	proxyURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", p.Port))
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}

	resp, err := client.Get(target.URL + "/test")
	if err != nil {
		t.Fatalf("GET through proxy: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("closing response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}
	if len(handler.calls) == 0 {
		t.Error("HandleRequest was never called")
	}
}
