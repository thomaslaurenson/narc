SHELL := /bin/bash

BINARY  := narc
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -s -w -X github.com/thomaslaurenson/narc/cmd.Version=$(VERSION)
GOFMT   := $(shell go env GOROOT)/bin/gofmt

.PHONY: help build install fmt fmt_check mod_check lint test test_verbose test_coverage vet ci clean snapshot release_check tag_release

help: ## Show this help message
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-16s %s\n", $$1, $$2}'

build: ## Build the narc binary
	go build -ldflags="$(LDFLAGS)" -o $(BINARY) .

install: ## Install narc to GOPATH/bin
	go install -ldflags="$(LDFLAGS)" .

fmt: ## Format all Go source files with gofmt
	$(GOFMT) -w .

fmt_check: ## Check formatting without writing
	@unformatted=$$($(GOFMT) -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "The following files are not gofmt'd:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

mod_check: ## Check go.mod/go.sum are tidy
	go mod tidy
	git diff --exit-code go.mod go.sum

lint: ## Run golangci-lint
	golangci-lint run

test: ## Run all tests (with -race -count=1)
	go test -race -count=1 ./...

test_verbose: ## Run all tests with verbose output
	go test -race -count=1 -v ./...

test_coverage: ## Run tests with coverage report
	go test -coverpkg=./internal/... -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out
	rm coverage.out

vet: ## Run go vet
	go vet ./...

ci: fmt_check mod_check lint test ## Run all CI checks locally

clean: ## Remove build artifacts
	rm -f $(BINARY)
	rm -rf dist/

snapshot: ## Build a local multi-platform snapshot via GoReleaser
	goreleaser release --snapshot --clean

release_check: ## Validate .goreleaser.yml without publishing
	goreleaser check
