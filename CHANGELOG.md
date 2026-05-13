# Changelog

## v0.5.2 - 2026-05-13

### Fixed

- Goreleaser now makes binary releases
- Docstring everywhere!

## v0.5.1 - 2026-04-15

### Added

- Shell prompt integration for oh-my-zsh and fish

### Fixed

- Shell prompt integration for bash and zsh
- Running `narc shell` inside VS Code no longer crashes the terminal
- Starting `narc shell` inside an existing `narc shell` session now exits
- Terminal state is now restored exactly once on exit

## v0.5.0 - 2026-04-07

### Added

- Windows support, excluding the shell command (requires a Unix terminal)
- Platform-specific shutdown signal files to handle system differences
- SIGHUP handling so background sessions shut down cleanly
- Output directory check before a recording session starts

### Fixed

- Read and write timeouts to the proxy server to prevent hung connections
- URL prefix matching to stop false matches when one service path starts with another
- Rule deduplication to include the service name so rules are never silently dropped
- Unmatched request log is now flushed to disk before closing to prevent data loss
- Proxy environment variable output is now correctly quoted for safe copy-paste into a shell

### Changed

- Improved the PROMPT_COMMAND comment in the shell command to clarify the filter and re-inject pattern
- Tests now run in parallel for faster CI
- Fixed the certmgr idempotency test to call the real function rather than duplicating logic

## v0.4.0 - 2026-03-30

### Changed

- Rewrote `narc shell` using a pseudo-terminal (`creack/pty`)
- Rewrote `narc shell` prompt to show `(narc)` prefix
- Rewrote `narc shell` exit printing spurious error and usage block
- Switched CA certificate generation from RSA-4096 to ECDSA P-256
- Tightened file permissions on `access_rules.json` to `0600`

## v0.3.0 - 2026-03-25

### Changed

- Complete rewrite in golang

## v0.2.0 - 2026-03-02

### Added

- Migrated to `pyproject.toml`
- Added `uv` for dependency management
- Switched linting to `ruff`

## v0.1.0 - 2024-01-01

### Added

- Initial release
