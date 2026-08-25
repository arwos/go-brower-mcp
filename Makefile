# Local Browser MCP — development commands
# Run `make help` to view all targets.

SHELL := /bin/bash
.DEFAULT_GOAL := help

SERVER_DIR := server
EXTENSION_DIR := extension
BIN_DIR := bin
SERVER_BIN := $(BIN_DIR)/browser-mcp
EXTENSION_DIST := $(EXTENSION_DIR)/dist/browser

.PHONY: help install build build-server build-extension test test-server test-extension lint lint-server lint-extension run run-http run-stdio clean check

help: ## Show available targets.
	@awk 'BEGIN {FS = ":.*##"}; /^[a-zA-Z0-9_-]+:.*##/ {printf "\033[36m%-18s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

install: ## Install JavaScript dependencies for the extension.
	cd $(EXTENSION_DIR) && pnpm install
	cd $(SERVER_DIR) && go mod tidy

build: install build-server build-extension ## Build MCP server and unpacked Chrome extension.

build-server: ## Build the Go MCP server into bin/browser-mcp.
	@mkdir -p $(BIN_DIR)
	cd $(SERVER_DIR) && go build -o ../$(SERVER_BIN) ./cmd/browser-mcp

build-extension: ## Build the Angular popup and MV3 service worker.
	cd $(EXTENSION_DIR) && pnpm build
	@test -f $(EXTENSION_DIST)/manifest.json
	@test -f $(EXTENSION_DIST)/service-worker.js
	@echo "Chrome unpacked extension is ready in $(EXTENSION_DIST)"

test: test-server test-extension ## Run all automated tests.

test-server: ## Run Go unit tests.
	cd $(SERVER_DIR) && go test ./...

test-extension: ## Type-check the Angular extension.
	cd $(EXTENSION_DIR) && node_modules/.bin/tsc --noEmit -p tsconfig.app.json

lint: lint-server lint-extension ## Run all static checks.

lint-server: ## Run Go vet.
	cd $(SERVER_DIR) && go vet ./...

lint-extension: ## Type-check Angular/TypeScript extension sources.
	cd $(EXTENSION_DIR) && node_modules/.bin/tsc --noEmit -p tsconfig.app.json

check: build test lint ## Build and run all verification commands.

run: build-server ## Run both stdio and HTTP MCP transports locally.
	./$(SERVER_BIN) -mode both

run-http: build-server ## Run the HTTP MCP transport locally on 127.0.0.1:8787.
	./$(SERVER_BIN) -mode http \
					-allowed-origin=*

run-stdio: build-server ## Run the stdio MCP transport locally.
	./$(SERVER_BIN) -mode stdio

clean: ## Remove generated server, Angular and local cache artifacts.
	rm -rf $(BIN_DIR) $(SERVER_DIR)/.cache $(EXTENSION_DIR)/dist

install-mcp:
	cd $(SERVER_DIR) && go install ./cmd/browser-mcp

license:
	goppy license