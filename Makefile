SHELL := /bin/bash

.PHONY: help install install_all update clean lint format format_check update_mitmproxy tag_release

help:
	@echo "Available targets:"
	@echo "  install            Install dependencies (uv)"
	@echo "  install_all        Install all optional dependencies (uv)"
	@echo "  update             Update all packages to latest versions"
	@echo "  clean              Remove build artifacts"
	@echo "  lint               Check code with ruff"
	@echo "  format             Format code with ruff"
	@echo "  format_check       Check code formatting"
	@echo "  tag_release        Tag git with version from pyproject and push"

install:
	uv sync

install_all:
	uv sync --all-extras

update:
	uv lock --upgrade
	uv sync --all-extras

clean:
	rm -rf .venv dist build
	find . -type d -name '__pycache__' -exec rm -rf {} +
	find . -type f -name '*.pyc' -delete

lint:
	uv run ruff check .

lint_fix:
	uv run ruff check --fix .

format:
	uv run ruff format .

format_check:
	uv run ruff format --check .

# TAG
tag_release:
	VERSION=$$(grep -m1 'version = ' pyproject.toml | cut -d '"' -f 2); \
	TAG="v$$VERSION"; \
	echo "[*] Current version: $$TAG"; \
	read -p "[*] Tag and push? (y/N) " yn; \
	case $$yn in \
		[yY]*) \
			git tag $$TAG; \
			git push origin $$TAG; \
			;; \
		[nN]*) \
			echo "[*] Exiting..."; \
			;; \
		*) \
			echo "[*] Invalid response... Exiting"; \
			exit 1; \
			;; \
	esac
