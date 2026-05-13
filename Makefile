SHELL := /bin/bash

BINARY  := narc
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -s -w -X github.com/thomaslaurenson/narc/cmd.Version=$(VERSION)

# HELP

.PHONY: help
help: ## Show this help message
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  %-16s %s\n", $$1, $$2}'

# BUILD

.PHONY: build
build: ## Build the narc binary
	go build -ldflags="$(LDFLAGS)" -o bin/$(BINARY) .

.PHONY: install
install: ## Install narc to GOPATH/bin
	go install -ldflags="$(LDFLAGS)" .

.PHONY: snapshot
snapshot: ## Build a local multi-platform snapshot via GoReleaser
	goreleaser release --snapshot --clean

.PHONY: release_check
release_check: ## Validate .goreleaser.yml without publishing
	goreleaser check

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf bin/ dist/

# LINT

.PHONY: fmt
fmt: ## Format all Go source files with gofmt
	gofmt -w .

.PHONY: fmt_check
fmt_check: ## Check formatting without writing
	gofmt -l . && git diff --exit-code

.PHONY: mod_check
mod_check: ## Check go.mod/go.sum are tidy
	go mod tidy && git diff --exit-code go.mod go.sum

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

# CI

.PHONY: ci
ci: fmt_check mod_check vet test ## Run all CI checks locally
