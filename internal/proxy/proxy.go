// Package proxy implements the MITM HTTP/HTTPS proxy that intercepts OpenStack
// API traffic and notifies a RequestHandler for each request.
package proxy

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/elazarl/goproxy"

	"github.com/thomaslaurenson/narc/internal/catalog"
	"github.com/thomaslaurenson/narc/internal/output"
)

// maxKeystoneBodyBytes caps the Keystone token response read to protect against
// an adversarial or misconfigured endpoint returning an unbounded body.
const maxKeystoneBodyBytes = 1 << 20 // 1 MiB, far larger than any real token response

// shutdownTimeout bounds how long Stop waits for in-flight requests.
const shutdownTimeout = 5 * time.Second

// RequestHandler is called for every intercepted request.
type RequestHandler interface {
	HandleRequest(method, url string)
}

// Options configures a Proxy. CA is required; every other field may be left at
// its zero value.
type Options struct {
	// Port is the port to listen on, where 0 picks any free port.
	Port int
	// CA signs the per-host certificates presented to intercepted clients.
	CA tls.Certificate
	// Catalog is populated from intercepted Keystone token responses.
	Catalog *catalog.Catalog
	// Handler is notified of every request.
	Handler RequestHandler
	// UnmatchedLog records requests that arrive before the catalog is loaded.
	UnmatchedLog *output.UnmatchedLog
	// Status receives the lines written for the person running narc.
	Status io.Writer
	// Logger receives diagnostics.
	Logger *slog.Logger
}

// Proxy is an HTTP/HTTPS man-in-the-middle proxy that intercepts OpenStack API
// traffic and notifies a RequestHandler for each request.
type Proxy struct {
	// Port is the port the proxy listens on, updated by Start to the port it
	// actually bound.
	Port int

	mitm         *goproxy.ConnectAction
	cat          *catalog.Catalog
	handler      RequestHandler
	unmatchedLog *output.UnmatchedLog
	status       io.Writer
	logger       *slog.Logger
	server       *http.Server
}

// New returns a Proxy configured by opts.
func New(opts Options) *Proxy {
	status := opts.Status
	if status == nil {
		status = io.Discard
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	ca := opts.CA

	return &Proxy{
		Port: opts.Port,
		// Built from this proxy's own CA rather than goproxy.MitmConnect, which
		// signs with the package-level goproxy.GoproxyCa and so would force every
		// Proxy in the process to share one CA.
		mitm:         &goproxy.ConnectAction{Action: goproxy.ConnectMitm, TLSConfig: goproxy.TLSConfigFromCA(&ca)},
		cat:          opts.Catalog,
		handler:      opts.Handler,
		unmatchedLog: opts.UnmatchedLog,
		status:       status,
		logger:       logger,
	}
}

// Start binds the proxy port and begins serving in a background goroutine.
// Returns an error immediately if the port cannot be bound.
//
// The proxy serves until Stop, not until ctx is cancelled. A wrapped command
// still exiting after an interrupt can make its last API calls through it, and
// those calls belong in the rules.
func (p *Proxy) Start(ctx context.Context) error {
	proxyServer := goproxy.NewProxyHttpServer()
	proxyServer.Verbose = false

	// Intercept all HTTPS CONNECT tunnels for MITM.
	proxyServer.OnRequest().HandleConnectFunc(func(host string, _ *goproxy.ProxyCtx) (*goproxy.ConnectAction, string) {
		return p.mitm, host
	})

	proxyServer.OnRequest().DoFunc(func(req *http.Request, _ *goproxy.ProxyCtx) (*http.Request, *http.Response) {
		p.logger.Debug("request", slog.String("method", req.Method), slog.String("url", req.URL.String()))
		if p.handler != nil {
			p.handler.HandleRequest(req.Method, req.URL.String())
		}
		// Warn about requests that arrive before the catalog is populated,
		// excluding the Keystone auth request itself.
		if p.cat != nil && !p.cat.IsReady() && !isKeystoneAuthPath(req.URL.Path) {
			fmt.Fprintf(p.status, "[!] Request received before the service catalog loaded, logged as unmatched\n")
			if p.unmatchedLog != nil {
				_ = p.unmatchedLog.Write(req.URL.String())
			}
		}
		return req, nil
	})

	// Intercept POST /v3/auth/tokens responses to populate the service catalog.
	if p.cat != nil {
		proxyServer.OnResponse(keystoneAuthCondition()).DoFunc(func(resp *http.Response, _ *goproxy.ProxyCtx) *http.Response {
			if resp == nil {
				return resp
			}
			body, err := io.ReadAll(io.LimitReader(resp.Body, maxKeystoneBodyBytes))
			_ = resp.Body.Close()
			// Always restore the body so the client still receives the full response.
			resp.Body = io.NopCloser(bytes.NewReader(body))
			if err != nil {
				p.logger.Warn("could not read the Keystone response body", slog.Any("error", err))
				return resp
			}
			wasLoaded := p.cat.IsReady()
			if err := p.cat.Update(body); err != nil {
				p.logger.Debug("catalog update failed", slog.Any("error", err))
				return resp
			}
			n := p.cat.Len()
			if wasLoaded {
				fmt.Fprintf(p.status, "[*] Service catalog updated (%d services)\n", n)
			} else {
				fmt.Fprintf(p.status, "[*] Service catalog loaded (%d services)\n", n)
			}
			return resp
		})
	}

	// Bind before spawning goroutine so port errors surface immediately.
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p.Port))
	if err != nil {
		return err
	}
	// Update Port with the actual bound port (important when Port was 0).
	p.Port = ln.Addr().(*net.TCPAddr).Port

	baseCtx := context.WithoutCancel(ctx)
	srv := &http.Server{
		Handler:           proxyServer,
		ReadHeaderTimeout: 30 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		BaseContext: func(_ net.Listener) context.Context {
			return baseCtx
		},
	}
	p.server = srv

	// The goroutine holds srv rather than reading p.server, which Stop clears.
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			p.logger.Error("proxy stopped serving", slog.Any("error", err))
		}
	}()

	return nil
}

// keystoneAuthCondition matches POST requests to the Keystone v3 token endpoint.
func keystoneAuthCondition() goproxy.ReqConditionFunc {
	return func(req *http.Request, _ *goproxy.ProxyCtx) bool {
		return req.Method == http.MethodPost && isKeystoneAuthPath(req.URL.Path)
	}
}

// isKeystoneAuthPath returns true if the path is the Keystone v3 token endpoint.
func isKeystoneAuthPath(path string) bool {
	return strings.HasSuffix(path, "/v3/auth/tokens")
}

// Stop shuts the proxy down, waiting up to shutdownTimeout for in-flight
// requests. It is called once the command is finishing, often because ctx was
// cancelled, so it waits on a context detached from that cancellation. Calling
// it again does nothing.
func (p *Proxy) Stop(ctx context.Context) {
	if p.server == nil {
		return
	}
	srv := p.server
	p.server = nil

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		p.logger.Warn("proxy shutdown", slog.Any("error", err))
	}
}
