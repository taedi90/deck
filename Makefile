GO ?= go
BIN_DIR ?= bin
BIN ?= $(BIN_DIR)/deck
GOLANGCI_LINT ?= $(BIN_DIR)/golangci-lint
GOLANGCI_LINT_PKG ?= github.com/golangci/golangci-lint/v2/cmd/golangci-lint
GOLANGCI_LINT_VERSION ?= latest

.PHONY: build test lint generate

build:
	@mkdir -p $(BIN_DIR)
	$(GO) build -o $(BIN) ./cmd/deck

test:
	$(GO) test ./...

generate:
	$(GO) run ./cmd/schema-gen

lint:
	@if [ ! -x "$(GOLANGCI_LINT)" ]; then \
		mkdir -p "$(BIN_DIR)"; \
		GOBIN="$(abspath $(BIN_DIR))" $(GO) install $(GOLANGCI_LINT_PKG)@$(GOLANGCI_LINT_VERSION); \
	fi
	$(GOLANGCI_LINT) run
