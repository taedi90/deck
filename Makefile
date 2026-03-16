GO ?= go
BIN_DIR ?= bin
BIN ?= $(BIN_DIR)/deck
AI_BIN ?= $(BIN_DIR)/deck-ai
GOLANGCI_LINT ?= $(BIN_DIR)/golangci-lint
GOLANGCI_LINT_PKG ?= github.com/golangci/golangci-lint/v2/cmd/golangci-lint
GOLANGCI_LINT_VERSION ?= latest

.PHONY: build build-ai test test-ai lint generate

build:
	@mkdir -p $(BIN_DIR)
	$(GO) build -o $(BIN) ./cmd/deck

build-ai:
	@mkdir -p $(BIN_DIR)
	$(GO) build -tags ai -o $(AI_BIN) ./cmd/deck

test:
	$(GO) test ./...

test-ai:
	$(GO) test -tags ai ./...

generate:
	$(GO) run ./cmd/schema-gen

lint:
	@if [ ! -x "$(GOLANGCI_LINT)" ]; then \
		mkdir -p "$(BIN_DIR)"; \
		GOBIN="$(abspath $(BIN_DIR))" $(GO) install $(GOLANGCI_LINT_PKG)@$(GOLANGCI_LINT_VERSION); \
	fi
	$(GOLANGCI_LINT) run
