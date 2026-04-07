SHELL := /bin/bash

BINARY  := narc
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -s -w -X github.com/thomaslaurenson/narc/cmd.Version=$(VERSION)
GOFMT   := $(shell go env GOROOT)/bin/gofmt

.PHONY: help build install fmt fmt_check mod_check lint test test_verbose test_coverage vet ci clean snapshot release_check tag_release

help:
	@echo "Available targets:"
	@echo "  build           Build the narc binary"
	@echo "  install         Install narc to GOPATH/bin"
	@echo "  fmt             Format all Go source files with gofmt"
	@echo "  fmt_check       Check formatting without writing (mirrors CI)"
	@echo "  mod_check       Check go.mod/go.sum are tidy (mirrors CI)"
	@echo "  lint            Run golangci-lint"
	@echo "  test            Run all tests (with -race -count=1)"
	@echo "  test_verbose    Run all tests with verbose output"
	@echo "  test_coverage   Run tests with coverage report"
	@echo "  vet             Run go vet"
	@echo "  ci              Run all CI checks locally (fmt_check, mod_check, lint, test)"
	@echo "  clean           Remove build artifacts"
	@echo "  snapshot        Build a local multi-platform snapshot via GoReleaser"
	@echo "  release_check   Validate .goreleaser.yml without publishing"
	@echo "  tag_release     Tag a release and push to origin"

build:
	go build -ldflags="$(LDFLAGS)" -o $(BINARY) .

install:
	go install -ldflags="$(LDFLAGS)" .

fmt:
	$(GOFMT) -w .

fmt_check:
	@unformatted=$$($(GOFMT) -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "The following files are not gofmt'd:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

mod_check:
	go mod tidy
	git diff --exit-code go.mod go.sum

lint:
	golangci-lint run

test:
	go test -race -count=1 ./...

test_verbose:
	go test -race -count=1 -v ./...

test_coverage:
	go test -coverpkg=./internal/... -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out
	rm coverage.out

vet:
	go vet ./...

ci: fmt_check mod_check lint test

clean:
	rm -f $(BINARY)
	rm -rf dist/

snapshot:
	goreleaser release --snapshot --clean

release_check:
	goreleaser check

tag_release:
	@read -p "Enter version tag (e.g. v0.1.0): " TAG; \
	echo "[*] Tagging: $$TAG"; \
	read -p "[*] Tag and push? (y/N) " yn; \
	case $$yn in \
		[yY]*) \
			git tag "$$TAG"; \
			git push origin "$$TAG"; \
			echo "[*] Released $$TAG"; \
			;; \
		*) \
			echo "[*] Aborted"; \
			;; \
	esac
