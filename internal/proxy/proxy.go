package proxy

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/elazarl/goproxy"
	"github.com/thomaslaurenson/narc/internal/catalog"
	"github.com/thomaslaurenson/narc/internal/certmgr"
	"github.com/thomaslaurenson/narc/internal/output"
)

// maxKeystoneBodyBytes caps the Keystone token response read to protect against
// an adversarial or misconfigured endpoint returning an unbounded body.
const maxKeystoneBodyBytes = 1 << 20 // 1 MiB — far larger than any real token response

// RequestHandler is called for every intercepted request.
type RequestHandler interface {
	HandleRequest(method, url string)
}

type Proxy struct {
	Port         int
	Debug        bool
	handler      RequestHandler
	cat          *catalog.Catalog
	unmatchedLog *output.UnmatchedLog
	server       *http.Server
	cancel       context.CancelFunc
}

// proxyCreated guards against creating more than one Proxy per process.
// goproxy.GoproxyCa is a package-level global; a second New call would
// silently overwrite the CA used by the first, causing hard-to-diagnose
// TLS failures.
var proxyCreated atomic.Bool

// New creates a Proxy. It ensures the CA certificate exists and loads it.
// cat and handler may be nil; when non-nil, cat is used to intercept Keystone
// token responses and handler is notified of every request.
// unmatchedLog is used to log pre-catalog requests; nil disables logging.
// NOTE: goproxy.GoproxyCa is a package-level global, so only one Proxy
// instance per process is supported.
func New(port int, debug bool, cat *catalog.Catalog, handler RequestHandler, unmatchedLog *output.UnmatchedLog) (*Proxy, error) {
	if !proxyCreated.CompareAndSwap(false, true) {
		return nil, fmt.Errorf("only one Proxy instance is supported per process (goproxy.GoproxyCa is a package-level global)")
	}

	if err := certmgr.EnsureCACert(); err != nil {
		return nil, fmt.Errorf("ensure CA cert: %w", err)
	}

	tlsCert, err := certmgr.LoadTLSCert()
	if err != nil {
		return nil, fmt.Errorf("load CA cert: %w", err)
	}

	// goproxy uses cert.Leaf to sign per-site certificates — must be populated.
	if tlsCert.Leaf == nil {
		tlsCert.Leaf, err = x509.ParseCertificate(tlsCert.Certificate[0])
		if err != nil {
			return nil, fmt.Errorf("parse CA cert leaf: %w", err)
		}
	}

	goproxy.GoproxyCa = tlsCert

	return &Proxy{
		Port:         port,
		Debug:        debug,
		cat:          cat,
		handler:      handler,
		unmatchedLog: unmatchedLog,
	}, nil
}

// Start binds the proxy port and begins serving in a background goroutine.
// Returns an error immediately if the port cannot be bound.
func (p *Proxy) Start() error {
	proxyServer := goproxy.NewProxyHttpServer()
	proxyServer.Verbose = false

	// Intercept all HTTPS CONNECT tunnels for MITM.
	proxyServer.OnRequest().HandleConnect(goproxy.AlwaysMitm)

	// NOTE: long-running requests will not be interrupted when Stop is called
	// because bgCtx is not threaded into individual goproxy request handlers.
	// For narc's use case (short-lived OpenStack API calls) this is a known,
	// acceptable limitation. To fix, pass bgCtx via goproxy.ProxyCtx.
	proxyServer.OnRequest().DoFunc(func(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
		if p.Debug {
			fmt.Fprintf(os.Stderr, "[narc:debug] %s %s\n", req.Method, req.URL.String())
		}
		if p.handler != nil {
			p.handler.HandleRequest(req.Method, req.URL.String())
		}
		// Warn about requests that arrive before the catalog is populated,
		// excluding the Keystone auth request itself.
		if p.cat != nil && !p.cat.IsReady() && !isKeystoneAuthPath(req.URL.Path) {
			fmt.Fprintf(os.Stderr, "[narc:warn] Request received before catalog loaded — recording to unmatched_requests.log\n")
			if p.unmatchedLog != nil {
				_ = p.unmatchedLog.Write(req.URL.String())
			}
		}
		return req, nil
	})

	// Intercept POST /v3/auth/tokens responses to populate the service catalog.
	if p.cat != nil {
		proxyServer.OnResponse(keystoneAuthCondition()).DoFunc(func(resp *http.Response, ctx *goproxy.ProxyCtx) *http.Response {
			if resp == nil {
				return resp
			}
			body, err := io.ReadAll(io.LimitReader(resp.Body, maxKeystoneBodyBytes))
			_ = resp.Body.Close()
			// Always restore the body so the client still receives the full response.
			resp.Body = io.NopCloser(bytes.NewReader(body))
			if err != nil {
				fmt.Fprintf(os.Stderr, "[narc:warn] Failed to read Keystone response body: %v\n", err)
				return resp
			}
			wasLoaded := p.cat.IsReady()
			if err := p.cat.Update(body); err != nil {
				if p.Debug {
					fmt.Fprintf(os.Stderr, "[narc:debug] catalog update error: %v\n", err)
				}
				return resp
			}
			n := p.cat.Len()
			if wasLoaded {
				fmt.Fprintf(os.Stderr, "[narc] Service catalog updated (%d services)\n", n)
			} else {
				fmt.Fprintf(os.Stderr, "[narc] Service catalog loaded (%d services)\n", n)
			}
			return resp
		})
	}

	// Bind before spawning goroutine so port errors surface immediately.
	addr := fmt.Sprintf("127.0.0.1:%d", p.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("bind %s: %w", addr, err)
	}
	// Update Port with the actual bound port (important when Port was 0).
	p.Port = ln.Addr().(*net.TCPAddr).Port

	bgCtx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel

	p.server = &http.Server{
		Addr:              addr,
		Handler:           proxyServer,
		ReadHeaderTimeout: 30 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		BaseContext: func(_ net.Listener) context.Context {
			return bgCtx
		},
	}

	go func() {
		if err := p.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "[narc:error] proxy: %v\n", err)
		}
	}()

	return nil
}

// keystoneAuthCondition matches POST requests to the Keystone v3 token endpoint.
func keystoneAuthCondition() goproxy.ReqConditionFunc {
	return func(req *http.Request, ctx *goproxy.ProxyCtx) bool {
		return req.Method == "POST" && isKeystoneAuthPath(req.URL.Path)
	}
}

// isKeystoneAuthPath returns true if the path is the Keystone v3 token endpoint.
func isKeystoneAuthPath(path string) bool {
	return strings.HasSuffix(path, "/v3/auth/tokens")
}

// Stop gracefully shuts down the proxy, waiting up to 5 seconds for in-flight requests.
func (p *Proxy) Stop() {
	if p.cancel != nil {
		p.cancel()
	}
	if p.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := p.server.Shutdown(ctx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			fmt.Fprintf(os.Stderr, "[narc:warn] proxy shutdown: %v\n", err)
		}
	}
}
