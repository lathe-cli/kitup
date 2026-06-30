SHELL := /bin/sh

BOLD  := \033[1m
CYAN  := \033[36m
GREEN := \033[32m
RESET := \033[0m

.DEFAULT_GOAL := help

TS_DIR := ts
GO_DIR := go
GO_COBRA_DIR := go-cobra
RUST_DIR := rust
EXAMPLE_TS_DIR := examples/ts
EXAMPLE_GO_DIR := examples/go
EXAMPLE_RUST_DIR := examples/rust
GO_FILES := $(shell find $(GO_DIR) $(GO_COBRA_DIR) $(EXAMPLE_GO_DIR) -name '*.go' -type f)

# ── Quality ──────────────────────────────────────────────────────────────────

.PHONY: check test test-ts test-go test-go-cobra test-rust fmt fmt-ts fmt-go fmt-rust

check: ## Full parity gate
	node scripts/check.mjs

test: test-ts test-go test-go-cobra test-rust ## Run SDK tests

test-ts: ## Run TypeScript tests
	pnpm --dir $(TS_DIR) test

test-go: ## Run Go SDK tests
	cd $(GO_DIR) && go test ./...

test-go-cobra: ## Run Go Cobra adapter tests
	cd $(GO_COBRA_DIR) && go test ./...

test-rust: ## Run Rust SDK tests
	cargo test --manifest-path $(RUST_DIR)/Cargo.toml

fmt: fmt-ts fmt-go fmt-rust ## Format all SDK code

fmt-ts: ## Format TypeScript code
	cd $(TS_DIR) && pnpm exec prettier --write src test ../examples/ts/cli.ts ../scripts/check.mjs ../scripts/prepare-release.mjs

fmt-go: ## Format Go code
	gofmt -w $(GO_FILES)

fmt-rust: ## Format Rust code
	cargo fmt --manifest-path $(RUST_DIR)/Cargo.toml
	cargo fmt --manifest-path $(EXAMPLE_RUST_DIR)/Cargo.toml

# ── Generated Data ───────────────────────────────────────────────────────────

.PHONY: generate generate-check

generate: ## Refresh generated host constants
	node scripts/sync-hosts.mjs

generate-check: ## Verify generated host constants
	node scripts/sync-hosts.mjs --check

# ── Examples ─────────────────────────────────────────────────────────────────

.PHONY: examples example-ts example-go example-rust

examples: example-ts example-go example-rust ## Run all examples

example-ts: ## Run TypeScript example
	tmp="$$(mktemp -d)" && mkdir -p "$$tmp/.codex" && HOME="$$tmp" pnpm --dir $(EXAMPLE_TS_DIR) install-skill

example-go: ## Run Go example
	cd $(EXAMPLE_GO_DIR) && tmp="$$(mktemp -d)" && mkdir -p "$$tmp/.codex" && HOME="$$tmp" go run .

example-rust: ## Run Rust example
	cd $(EXAMPLE_RUST_DIR) && tmp="$$(mktemp -d)" && mkdir -p "$$tmp/.codex" && CARGO_HOME="$${CARGO_HOME:-$$HOME/.cargo}" RUSTUP_HOME="$${RUSTUP_HOME:-$$HOME/.rustup}" HOME="$$tmp" cargo run --quiet

# ── Release ──────────────────────────────────────────────────────────────────

.PHONY: release-patch release-minor release-major

release-patch: ## Prepare patch release branch and commit
	node scripts/prepare-release.mjs patch

release-minor: ## Prepare minor release branch and commit
	node scripts/prepare-release.mjs minor

release-major: ## Prepare major release branch and commit
	node scripts/prepare-release.mjs major

# ── Maintenance ──────────────────────────────────────────────────────────────

.PHONY: hooks clean clean-ts clean-go clean-rust clean-examples

hooks: ## Install repo git hooks
	git config core.hooksPath .githooks

clean: clean-ts clean-go clean-rust clean-examples ## Remove build artifacts

clean-ts: ## Remove TypeScript build artifacts
	rm -rf \
		$(TS_DIR)/node_modules \
		$(TS_DIR)/dist \
		$(TS_DIR)/coverage \
		$(TS_DIR)/*.tsbuildinfo

clean-go: ## Remove Go build artifacts
	rm -f \
		$(GO_DIR)/coverage.out \
		$(GO_COBRA_DIR)/coverage.out

clean-rust: ## Remove Rust build artifacts
	rm -rf \
		target \
		$(RUST_DIR)/target

clean-examples: ## Remove example build artifacts
	rm -rf \
		$(EXAMPLE_TS_DIR)/node_modules \
		$(EXAMPLE_TS_DIR)/dist \
		$(EXAMPLE_TS_DIR)/coverage \
		$(EXAMPLE_TS_DIR)/*.tsbuildinfo \
		$(EXAMPLE_RUST_DIR)/target \
		$(EXAMPLE_GO_DIR)/coverage.out

# ── Help ─────────────────────────────────────────────────────────────────────

.PHONY: help

help: ## Show available targets
	@awk 'BEGIN {FS = ":.*## "; printf "$(BOLD)kitup$(RESET) — bundled Agent Skill installer SDK\n"} \
		/^# ── / {n = $$0; gsub(/(^# ── | ─+$$)/, "", n); printf "\n$(BOLD)%s$(RESET)\n", n} \
		/^[a-zA-Z_-]+:.*## / {printf "  $(CYAN)make %-18s$(RESET) %s\n", $$1, $$2} \
		END {printf "\n"}' $(MAKEFILE_LIST)
