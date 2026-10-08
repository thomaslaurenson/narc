package proxy

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/thomaslaurenson/narc/internal/catalog"
	"github.com/thomaslaurenson/narc/internal/certmgr"
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

// testCA generates a CA in a temporary directory and loads it.
func testCA(t *testing.T) tls.Certificate {
	t.Helper()
	dir := t.TempDir()
	if _, err := certmgr.EnsureCACert(dir); err != nil {
		t.Fatalf("EnsureCACert: %v", err)
	}
	ca, err := certmgr.LoadTLSCert(dir)
	if err != nil {
		t.Fatalf("LoadTLSCert: %v", err)
	}
	return ca
}

func TestProxyNewNilCatalogAndHandler(t *testing.T) {
	t.Parallel()
	p := New(Options{CA: testCA(t)})
	if err := p.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	p.Stop(context.Background())
}

func TestProxyTwoInstances(t *testing.T) {
	t.Parallel()
	first := New(Options{CA: testCA(t)})
	if err := first.Start(context.Background()); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	defer first.Stop(context.Background())

	second := New(Options{CA: testCA(t)})
	if err := second.Start(context.Background()); err != nil {
		t.Fatalf("second Start: %v", err)
	}
	defer second.Stop(context.Background())

	if first.Port == second.Port {
		t.Errorf("both proxies report port %d", first.Port)
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
	t.Parallel()
	// Start a trivial target HTTP server.
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	cat := catalog.NewCatalog()
	handler := &mockHandler{}

	p := New(Options{CA: testCA(t), Catalog: cat, Handler: handler})
	if err := p.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer p.Stop(context.Background())

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
