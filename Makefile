SHELL := /bin/bash

BINARY  := narc
MODULE  := github.com/thomaslaurenson/narc
VERSION := $(shell git describe --tags --always --dirty --match 'v*' 2>/dev/null || echo "dev")
LDFLAGS := -s -w -X $(MODULE)/cmd.Version=$(VERSION)

# goimports formats exactly as gofmt does and groups imports as well, which
# gofmt will not do: it sorts every import into one alphabetical block.
GOIMPORTS := go run golang.org/x/tools/cmd/goimports@latest -local $(MODULE)

##@ BUILD

.PHONY: help
help: ## Show this help message
	@awk 'BEGIN {FS = ":.*?## "} /^##@ / {printf "\n%s\n", substr($$0, 5)} \
		/^[a-zA-Z_-]+:.*## / {printf "  %-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.PHONY: build
build: ## Build the binary for the current platform
	go build -ldflags="$(LDFLAGS)" -o dist/$(BINARY) .

.PHONY: install
install: ## Install the binary to GOPATH/bin
	go install -ldflags="$(LDFLAGS)" .

.PHONY: snapshot
snapshot: ## Build binaries for every platform with goreleaser
	goreleaser build --snapshot --clean

##@ TEST

.PHONY: test
test: ## Run all tests with the race detector
	go test -race -count=1 ./...

.PHONY: test_verbose
test_verbose: ## Run all tests with the race detector and verbose output
	go test -race -count=1 -v ./...

.PHONY: test_integration
test_integration: ## Run the build-tagged integration tests
	go test -race -count=1 -tags=integration ./...

.PHONY: test_coverage
test_coverage: ## Report test coverage over the internal packages
	go test -race -count=1 -tags=integration -coverpkg=./internal/... -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out
	rm coverage.out

##@ LINT

.PHONY: format
format: ## Format Go source files and group their imports
	$(GOIMPORTS) -w .

.PHONY: check_format
check_format: ## Fail if any Go source file is unformatted or has ungrouped imports
	@out="$$($(GOIMPORTS) -l .)"; \
	if [[ -n "$$out" ]]; then \
	  printf 'Unformatted Go files:\n%s\n' "$$out"; \
	  exit 1; \
	fi

.PHONY: check_mod
check_mod: ## Fail if go.mod or go.sum is untidy
	go mod tidy && git diff --exit-code go.mod go.sum

.PHONY: vet
vet: ## Run go vet, including the build-tagged integration tests
	go vet ./...
	go vet -tags=integration ./...

.PHONY: check_cross
check_cross: ## Type-check the platform-specific files CI never builds
	GOOS=windows go vet ./...
	GOOS=darwin go vet ./...

.PHONY: check_release
check_release: ## Validate the goreleaser configs without building
	goreleaser check .goreleaser.yml .goreleaser.prerelease.yml

.PHONY: check_all
check_all: check_format check_mod vet check_cross ## Run every static check

.PHONY: vuln
vuln: ## Scan dependencies and the standard library for known vulnerabilities
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

##@ GET

.PHONY: get_version
get_version: ## Print the version build would stamp into the binary
	@echo "$(VERSION)"

.PHONY: get_changelog
get_changelog: ## Print release notes for TAG to stdout (TAG=v1.0.0)
	@tag="$(TAG)"; tag="$${tag#v}"; \
	if [[ -z "$$tag" ]]; then \
	  printf 'get_changelog: TAG is empty; pass TAG=v1.0.0\n' >&2; \
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

##@ CI

.PHONY: ci
ci: check_all test ## Run every check the lint and test workflows run

.PHONY: clean
clean: ## Remove build artefacts
	rm -rf dist/ install.sh install.ps1 checksums.txt checksums.txt.sigstore.json coverage.out
