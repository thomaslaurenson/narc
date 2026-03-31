# Changelog

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
