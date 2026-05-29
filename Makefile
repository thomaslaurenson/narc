SHELL := /bin/bash

BINARY  := narc
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -s -w -X github.com/thomaslaurenson/narc/cmd.Version=$(VERSION)
TAG     ?= $(shell git describe --tags --abbrev=0 2>/dev/null)

# HELP
.PHONY: help
help: ## Show this help message
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  %-16s %s\n", $$1, $$2}'

# BUILD
.PHONY: build
build: ## Build the narc binary
	go build -ldflags="$(LDFLAGS)" -o dist/$(BINARY) .

.PHONY: install
install: ## Install narc to GOPATH/bin
	go install -ldflags="$(LDFLAGS)" .

.PHONY: snapshot
snapshot: ## Build a local multi-platform snapshot via GoReleaser
	goreleaser release --snapshot --clean

.PHONY: release_check
release_check: ## Validate .goreleaser.yml without publishing
	goreleaser check

# LINT
.PHONY: fmt
fmt: ## Format all Go source files with gofmt
	gofmt -w .

.PHONY: fmt_check
fmt_check: ## Check formatting without writing
	gofmt -l .

.PHONY: mod_check
mod_check: ## Check go.mod/go.sum are tidy
	go mod tidy

.PHONY: vet
vet: ## Run go vet
	go vet ./...

# TEST
.PHONY: test
test: ## Run all tests (with -race -count=1)
	go test -race -count=1 ./...

.PHONY: test_verbose
test_verbose: ## Run all tests with verbose output
	go test -race -count=1 -v ./...

.PHONY: test_coverage
test_coverage: ## Run tests with coverage report
	go test -race -count=1 -coverpkg=./internal/... -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out
	rm coverage.out

# GET
.PHONY: get_changelog
get_changelog: ## Print release notes for TAG to stdout (default: latest tag; override with TAG=v1.0.0)
	@tag="$(TAG)"; tag="$${tag#v}"; \
	if [[ -z "$$tag" ]]; then \
	  printf 'get_changelog: TAG is empty; pass TAG=v1.0.0 or create a git tag\n' >&2; \
	  exit 1; \
	fi; \
	notes="$$(awk -v tag="$$tag" ' \
	  /^## / { if (found) exit; if (index($$0,"## "tag" ")==1 || $$0=="## "tag) found=1; next } \
	  found { lines[n++]=$$0 } \
	  END { \
	    s=0; while (s<n && lines[s]~/^[[:space:]]*$$/) s++; \
	    e=n-1; while (e>=s && lines[e]~/^[[:space:]]*$$/) e--; \
	    for (i=s;i<=e;i++) print lines[i] \
	  }' CHANGELOG.md)"; \
	if [[ -z "$$notes" ]]; then \
	  printf 'get_changelog: no CHANGELOG entry for %s\n' "$$tag" >&2; \
	  exit 1; \
	fi; \
	printf '%s\n' "$$notes"

# CI
.PHONY: ci
ci: fmt_check mod_check vet test ## Run all CI checks locally

# TASKS
.PHONY: clean
clean: ## Remove build artifacts
	rm -rf bin/ dist/
