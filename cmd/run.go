package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	cfg, err := a.loadConfig(cmd)
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

	var onUnmatched func(string, string)
	if a.debug {
		onUnmatched = func(method, url string) {
			fmt.Fprintf(os.Stderr, "[narc:debug] unmatched: %s %s\n", method, url)
		}
	}

	p, az, certPath, unmatchedLog, err := a.startRecording(cfg, onUnmatched, nil)
	if err != nil {
		return err
	}

	if opts.background {
		runBackground(cmd.Context(), p, az, certPath, unmatchedLog, cfg.OutputFile)
		return nil
	}

	exitCode := runSubprocess(cmd.Context(), args, buildEnv(a.environ, p.Port, certPath), opts.showOutput)

	fmt.Fprintf(os.Stderr, "[narc] Shutting down...\n")
	p.Stop()
	if unmatchedLog != nil {
		_ = unmatchedLog.Close()
	}
	writeRulesOnExit(az, cfg.OutputFile)
	if exitCode != 0 {
		return &ExitCodeError{Code: exitCode}
	}
	return nil
}

// startRecording creates the catalog, analyzer, and proxy, starts the proxy,
// and returns them ready for use. Shared between the run and shell commands.
// cfg.LogFile is opened for unmatched-URL logging (nil if empty). onUnmatched is
// the debug callback; nil disables it. Both decisions belong to the caller.
// logf is used for all narc status output; nil defaults to fmt.Fprintf(os.Stderr, ...).
func (a *App) startRecording(cfg *config.Config, onUnmatched func(string, string), logf func(string, ...any)) (*proxy.Proxy, *analyzer.Analyzer, string, *output.UnmatchedLog, error) {
	if logf == nil {
		logf = func(format string, args ...any) { fmt.Fprintf(os.Stderr, format, args...) }
	}
	var unmatchedLog *output.UnmatchedLog
	if cfg.LogFile != "" {
		var err error
		unmatchedLog, err = output.OpenUnmatchedLog(cfg.LogFile)
		if err != nil {
			return nil, nil, "", nil, fmt.Errorf("open unmatched log: %w", err)
		}
	}

	cat := catalog.NewCatalog()
	az := analyzer.New(cat, unmatchedLog, func(rule analyzer.AccessRule) {
		logf("[narc] %-20s %-8s %s\n", rule.Service, rule.Method, rule.Path)
	}, onUnmatched)

	p, err := proxy.New(cfg.ProxyPort, a.debug, cat, az, unmatchedLog, logf)
	if err != nil {
		if unmatchedLog != nil {
			_ = unmatchedLog.Close()
		}
		return nil, nil, "", nil, fmt.Errorf("create proxy: %w", err)
	}

	certPath, err := certmgr.CACertPath()
	if err != nil {
		if unmatchedLog != nil {
			_ = unmatchedLog.Close()
		}
		return nil, nil, "", nil, fmt.Errorf("get CA cert path: %w", err)
	}

	if err := p.Start(); err != nil {
		if unmatchedLog != nil {
			_ = unmatchedLog.Close()
		}
		return nil, nil, "", nil, fmt.Errorf("start proxy: %w", err)
	}

	logf("[narc] Proxy listening on http://127.0.0.1:%d\n", p.Port)
	return p, az, certPath, unmatchedLog, nil
}

func writeRulesOnExit(az *analyzer.Analyzer, outputFile string) {
	n, err := az.WriteRules(outputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[narc:error] Failed to write rules: %v\n", err)
		return
	}
	fmt.Fprintf(os.Stderr, "[narc] Done. %d unique access rule(s) written to %s\n", n, outputFile)
}

// ensureOutputDir checks that the directory containing outPath exists, and
// returns a clear error if it does not. This surfaces misconfigured --output
// paths early, before the proxy runs, rather than failing silently at the end.
func ensureOutputDir(outPath string) error {
	dir := filepath.Dir(outPath)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return fmt.Errorf("output directory does not exist: %s", dir)
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

// runSubprocess starts args[0] with the remaining args and waits for it to exit.
// stdout is discarded unless showOutput is true; stderr is always forwarded so
// that errors and warnings from the subprocess remain visible.
// Returns the wrapped command's exit code, or 1 on start failure.
func runSubprocess(ctx context.Context, args []string, env []string, showOutput bool) int {
	c := exec.CommandContext(ctx, args[0], args[1:]...)
	// The child shares the terminal's process group, so a Ctrl-C has already
	// reached it by the time ctx is cancelled. Cancel therefore sends nothing,
	// and WaitDelay kills the child only if it is still running after the grace
	// period. The default Cancel would kill it at once, before it could exit
	// cleanly.
	c.Cancel = nil
	c.WaitDelay = subprocessGrace
	c.Stdin = os.Stdin
	if showOutput {
		c.Stdout = os.Stdout
	} else {
		c.Stdout = io.Discard
	}
	c.Stderr = os.Stderr
	c.Env = env

	if err := c.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "[narc:error] Failed to run subprocess: %v\n", err)
		return 1
	}
	return 0
}

// runBackground prints the proxy environment and waits until ctx is cancelled,
// which is how a background session is stopped.
func runBackground(ctx context.Context, p *proxy.Proxy, az *analyzer.Analyzer, certPath string, unmatchedLog *output.UnmatchedLog, outputFile string) {
	fmt.Fprintf(os.Stderr, "[narc] Running in background. PID: %d\n", os.Getpid())
	fmt.Fprintf(os.Stderr, "[narc] Run the following in your shell:\n")
	printProxyEnv(p.Port, certPath)

	<-ctx.Done()

	fmt.Fprintf(os.Stderr, "\n[narc] Shutting down...\n")
	p.Stop()
	if unmatchedLog != nil {
		_ = unmatchedLog.Close()
	}
	writeRulesOnExit(az, outputFile)
}

// printProxyEnv prints shell export statements for the narc proxy environment.
// Values are single-quoted so paths with spaces or special characters are safe
// to copy-paste directly into a POSIX shell.
func printProxyEnv(port int, certPath string) {
	for _, v := range proxyEnvVars(port, certPath) {
		fmt.Fprintf(os.Stderr, "  export %s='%s'\n", v.key, v.value)
	}
}
