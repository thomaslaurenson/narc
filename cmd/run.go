package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/thomaslaurenson/narc/internal/analyzer"
	"github.com/thomaslaurenson/narc/internal/catalog"
	"github.com/thomaslaurenson/narc/internal/certmgr"
	"github.com/thomaslaurenson/narc/internal/config"
	"github.com/thomaslaurenson/narc/internal/output"
	"github.com/thomaslaurenson/narc/internal/proxy"
)

// subprocessGrace is how long a wrapped command has to exit after an interrupt
// before it is killed.
const subprocessGrace = 3 * time.Second

// runOptions holds the flags of the run command.
type runOptions struct {
	background bool
	logFile    string
	outputFile string
	showOutput bool
}

// proxyVar holds a proxy environment variable name and its resolved value.
type proxyVar struct {
	key   string
	value string
}

// proxyEnvVars is the single source of truth for all proxy-related environment
// variables narc sets. buildEnv and printProxyEnv both derive from this slice,
// so adding a new variable only requires one edit here.
func proxyEnvVars(port int, certPath string) []proxyVar {
	proxyURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	return []proxyVar{
		{"https_proxy", proxyURL},
		{"HTTPS_PROXY", proxyURL},
		{"http_proxy", proxyURL},
		{"HTTP_PROXY", proxyURL},
		{"SSL_CERT_FILE", certPath},
		{"REQUESTS_CA_BUNDLE", certPath},
		{"OS_CACERT", certPath},
	}
}

func (a *App) newRunCmd() *cobra.Command {
	var opts runOptions
	c := &cobra.Command{
		Use:   "run",
		Short: "Record OpenStack API calls made by a command and generate access rules",
		// Checked as an argument validator rather than in RunE, so a missing
		// command fails before narc.json is read or created.
		Args: func(_ *cobra.Command, args []string) error {
			if !opts.background && len(args) == 0 {
				return errors.New("provide a command to wrap (narc run -- <cmd>) or use --background")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runRun(cmd, args, opts)
		},
	}
	c.Flags().BoolVarP(&opts.background, "background", "b", false, "run proxy in background, print env vars for manual use")
	c.Flags().StringVarP(&opts.logFile, "log-file", "l", "", "path for unmatched-request log (default: ~/.narc/unmatched_requests.log)")
	c.Flags().StringVarP(&opts.outputFile, "output", "o", "", "path for access rules output file (default: ~/.narc/access_rules.json)")
	c.Flags().BoolVar(&opts.showOutput, "show-output", false, "show subprocess stdout (stderr is always shown)")
	return c
}

func (a *App) runRun(cmd *cobra.Command, args []string, opts runOptions) error {
	cfg, home, err := a.loadConfig(cmd)
	if err != nil {
		return err
	}
	if opts.logFile != "" {
		cfg.LogFile = opts.logFile
	}
	if opts.outputFile != "" {
		cfg.OutputFile = opts.outputFile
	}
	if err := ensureOutputDir(cfg.OutputFile); err != nil {
		return err
	}

	ctx := cmd.Context()
	status := &syncWriter{w: cmd.ErrOrStderr()}
	logger := newLogger(status, a.debug)

	s, err := startSession(ctx, cfg, config.Dir(home), status, logger)
	if err != nil {
		return err
	}

	if opts.background {
		fmt.Fprintf(status, "[*] Running in background with PID %d\n", os.Getpid())
		fmt.Fprintf(status, "[*] Run the following in your shell:\n")
		printProxyEnv(status, s.proxy.Port, s.certPath)
		// A background session ends when narc is interrupted or terminated.
		<-ctx.Done()
		fmt.Fprintln(status)
		return s.finish(ctx)
	}

	c := exec.CommandContext(ctx, args[0], args[1:]...)
	// The child shares the terminal's process group, so a Ctrl-C has already
	// reached it by the time ctx is cancelled. Cancel therefore sends nothing,
	// and WaitDelay kills the child only if it is still running after the grace
	// period. The default Cancel would kill it at once, before it could exit
	// cleanly.
	c.Cancel = nil
	c.WaitDelay = subprocessGrace
	c.Env = buildEnv(a.environ, s.proxy.Port, s.certPath)
	c.Stdin = cmd.InOrStdin()
	c.Stdout = io.Discard
	if opts.showOutput {
		c.Stdout = cmd.OutOrStdout()
	}
	c.Stderr = cmd.ErrOrStderr()

	runErr := c.Run()
	if c.ProcessState == nil {
		// The command never started, so nothing was recorded, and writing the
		// rules would replace the last session's with an empty list.
		s.abort(ctx)
		return runErr
	}
	if err := s.finish(ctx); err != nil {
		return err
	}
	if code := c.ProcessState.ExitCode(); code != 0 {
		return &ExitCodeError{Code: code}
	}
	return nil
}

// session is one recording: the proxy, the analyser it feeds, and the files
// they write.
type session struct {
	proxy        *proxy.Proxy
	analyzer     *analyzer.Analyzer
	unmatchedLog *output.UnmatchedLog
	certPath     string
	outputFile   string
	status       io.Writer
	logger       *slog.Logger
}

// startSession prepares the CA, builds the catalog, analyser and proxy, and
// starts the proxy. Shared between the run and shell commands. status receives
// the lines written for the person running narc; the proxy writes to it from
// its own goroutines, so it must be safe for concurrent use.
func startSession(ctx context.Context, cfg *config.Config, dir string, status io.Writer, logger *slog.Logger) (*session, error) {
	certStatus, err := certmgr.EnsureCACert(dir)
	if err != nil {
		return nil, fmt.Errorf("prepare CA certificate: %w", err)
	}
	certPath := certmgr.CACertPath(dir)
	switch certStatus {
	case certmgr.StatusCreated:
		fmt.Fprintf(status, "[+] Generated CA certificate %s\n", certPath)
	case certmgr.StatusRenewed:
		fmt.Fprintf(status, "[~] Renewed CA certificate %s, which was near expiry\n", certPath)
	}
	ca, err := certmgr.LoadTLSCert(dir)
	if err != nil {
		return nil, fmt.Errorf("load CA certificate: %w", err)
	}

	var unmatchedLog *output.UnmatchedLog
	if cfg.LogFile != "" {
		unmatchedLog, err = output.OpenUnmatchedLog(cfg.LogFile)
		if err != nil {
			return nil, fmt.Errorf("open unmatched log: %w", err)
		}
	}

	cat := catalog.NewCatalog()
	az := analyzer.New(cat, unmatchedLog,
		func(rule analyzer.AccessRule) {
			fmt.Fprintf(status, "[+] %-20s %-8s %s\n", rule.Service, rule.Method, rule.Path)
		},
		func(method, url string) {
			logger.Debug("unmatched request", slog.String("method", method), slog.String("url", url))
		},
	)

	p := proxy.New(proxy.Options{
		Port:         cfg.ProxyPort,
		CA:           ca,
		Catalog:      cat,
		Handler:      az,
		UnmatchedLog: unmatchedLog,
		Status:       status,
		Logger:       logger,
	})
	if err := p.Start(ctx); err != nil {
		if unmatchedLog != nil {
			_ = unmatchedLog.Close()
		}
		return nil, fmt.Errorf("start proxy: %w", err)
	}

	fmt.Fprintf(status, "[*] Proxy listening on http://127.0.0.1:%d\n", p.Port)
	return &session{
		proxy:        p,
		analyzer:     az,
		unmatchedLog: unmatchedLog,
		certPath:     certPath,
		outputFile:   cfg.OutputFile,
		status:       status,
		logger:       logger,
	}, nil
}

// finish stops the proxy and writes the access rules.
func (s *session) finish(ctx context.Context) error {
	fmt.Fprintf(s.status, "[*] Shutting down...\n")
	s.proxy.Stop(ctx)
	s.closeLog()
	n, err := s.analyzer.WriteRules(s.outputFile)
	if err != nil {
		return err
	}
	fmt.Fprintf(s.status, "[*] Done. %d unique access rule(s) written to %s\n", n, s.outputFile)
	return nil
}

// abort stops the proxy without writing any rules, for a session that failed
// before it could record anything.
func (s *session) abort(ctx context.Context) {
	s.proxy.Stop(ctx)
	s.closeLog()
}

// closeLog closes the unmatched log. A failure is only worth a warning, since
// the rules do not depend on the log and nothing above can act on it.
func (s *session) closeLog() {
	if s.unmatchedLog == nil {
		return
	}
	if err := s.unmatchedLog.Close(); err != nil {
		s.logger.Warn("could not close the unmatched log", slog.Any("error", err))
	}
	s.unmatchedLog = nil
}

// ensureOutputDir checks that the directory containing outPath exists, and
// returns a clear error if it does not. This surfaces misconfigured --output
// paths early, before the proxy runs, rather than failing silently at the end.
func ensureOutputDir(outPath string) error {
	dir := filepath.Dir(outPath)
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("output directory %q does not exist", dir)
	}
	return nil
}

// buildEnv returns a copy of environ with all proxy-related vars removed and
// replaced by the narc proxy settings.
func buildEnv(environ []string, port int, caCertPath string) []string {
	vars := proxyEnvVars(port, caCertPath)

	keys := make(map[string]bool, len(vars))
	for _, v := range vars {
		keys[v.key] = true
	}

	env := make([]string, 0, len(environ)+len(vars))
	for _, kv := range environ {
		key, _, _ := strings.Cut(kv, "=")
		if !keys[key] {
			env = append(env, kv)
		}
	}
	for _, v := range vars {
		env = append(env, v.key+"="+v.value)
	}
	return env
}

// printProxyEnv prints shell export statements for the narc proxy environment.
// Values are single-quoted so paths with spaces or special characters are safe
// to copy-paste directly into a POSIX shell.
func printProxyEnv(w io.Writer, port int, certPath string) {
	for _, v := range proxyEnvVars(port, certPath) {
		fmt.Fprintf(w, "  export %s='%s'\n", v.key, v.value)
	}
}
