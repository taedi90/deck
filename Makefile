GO ?= go
BIN_DIR ?= bin
BIN ?= $(BIN_DIR)/deck
AI_BIN ?= $(BIN_DIR)/deck-ai
GOLANGCI_LINT ?= $(BIN_DIR)/golangci-lint
GOLANGCI_LINT_PKG ?= github.com/golangci/golangci-lint/v2/cmd/golangci-lint
GOLANGCI_LINT_VERSION ?= latest
GOVULNCHECK ?= $(BIN_DIR)/govulncheck
GOVULNCHECK_PKG ?= golang.org/x/vuln/cmd/govulncheck
GOVULNCHECK_VERSION ?= v1.1.4
BUILDINFO_PKG ?= github.com/taedi90/deck/internal/buildinfo
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || printf unknown)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
DIRTY ?= $(shell if [ -n "$$(git status --short 2>/dev/null)" ]; then printf true; else printf false; fi)
LDFLAGS ?= -X $(BUILDINFO_PKG).Version=$(VERSION) -X $(BUILDINFO_PKG).Commit=$(COMMIT) -X $(BUILDINFO_PKG).Date=$(DATE) -X $(BUILDINFO_PKG).Dirty=$(DIRTY)

.PHONY: build build-ai test test-ai lint vuln vuln-ai generate print-build-meta

build:
	@mkdir -p $(BIN_DIR)
	$(GO) build -ldflags "$(LDFLAGS) -X $(BUILDINFO_PKG).Variant=core" -o $(BIN) ./cmd/deck

build-ai:
	@mkdir -p $(BIN_DIR)
	$(GO) build -tags ai -ldflags "$(LDFLAGS) -X $(BUILDINFO_PKG).Variant=ai" -o $(AI_BIN) ./cmd/deck

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

vuln:
	@if [ ! -x "$(GOVULNCHECK)" ]; then \
		mkdir -p "$(BIN_DIR)"; \
		GOBIN="$(abspath $(BIN_DIR))" $(GO) install $(GOVULNCHECK_PKG)@$(GOVULNCHECK_VERSION); \
	fi
	$(GOVULNCHECK) ./...

vuln-ai:
	@if [ ! -x "$(GOVULNCHECK)" ]; then \
		mkdir -p "$(BIN_DIR)"; \
		GOBIN="$(abspath $(BIN_DIR))" $(GO) install $(GOVULNCHECK_PKG)@$(GOVULNCHECK_VERSION); \
	fi
	$(GOVULNCHECK) -tags ai ./...

print-build-meta:
	@printf 'VERSION=%s\nCOMMIT=%s\nDATE=%s\nDIRTY=%s\n' "$(VERSION)" "$(COMMIT)" "$(DATE)" "$(DIRTY)"
