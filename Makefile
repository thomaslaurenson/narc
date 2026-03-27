SHELL := /bin/bash

BINARY  := narc
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -s -w -X github.com/thomaslaurenson/narc/cmd.Version=$(VERSION)

.PHONY: help build install test test_verbose test_coverage vet clean snapshot release_check tag_release

help:
	@echo "Available targets:"
	@echo "  build           Build the narc binary"
	@echo "  install         Install narc to GOPATH/bin"
	@echo "  test            Run all tests"
	@echo "  test_verbose    Run all tests with verbose output"
	@echo "  vet             Run go vet"
	@echo "  clean           Remove build artifacts"
	@echo "  snapshot        Build a local multi-platform snapshot via GoReleaser"
	@echo "  release_check   Validate .goreleaser.yml without publishing"
	@echo "  tag_release     Tag a release and push to origin"

build:
	go build -ldflags="$(LDFLAGS)" -o $(BINARY) .

install:
	go install .

test:
	go test ./...

test_verbose:
	go test -v ./...

test_coverage:
	go test -coverpkg=./internal/... -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out
	rm coverage.out

vet:
	go vet ./...

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
