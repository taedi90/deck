GO ?= go
BIN_DIR ?= bin
BIN ?= $(BIN_DIR)/deck
GOLANGCI_LINT ?= $(BIN_DIR)/golangci-lint
GOLANGCI_LINT_PKG ?= github.com/golangci/golangci-lint/v2/cmd/golangci-lint
GOLANGCI_LINT_VERSION ?= v2.11.3
GOVULNCHECK ?= $(BIN_DIR)/govulncheck
GOVULNCHECK_PKG ?= golang.org/x/vuln/cmd/govulncheck
GOVULNCHECK_VERSION ?= v1.1.4
GOVULNCHECK_GOTOOLCHAIN ?= go1.25.8
BUILDINFO_PKG ?= github.com/Airgap-Castaways/deck/internal/buildinfo
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || printf unknown)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
DIRTY ?= $(shell if [ -n "$$(git status --short 2>/dev/null)" ]; then printf true; else printf false; fi)
LDFLAGS ?= -X $(BUILDINFO_PKG).Version=$(VERSION) -X $(BUILDINFO_PKG).Commit=$(COMMIT) -X $(BUILDINFO_PKG).Date=$(DATE) -X $(BUILDINFO_PKG).Dirty=$(DIRTY)

.PHONY: build test lint vuln generate print-build-meta release-check release-snapshot

build:
	@mkdir -p $(BIN_DIR)
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/deck

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


$(GOVULNCHECK):
	@mkdir -p "$(BIN_DIR)"
	GOBIN="$(abspath $(BIN_DIR))" GOTOOLCHAIN="$(GOVULNCHECK_GOTOOLCHAIN)" $(GO) install $(GOVULNCHECK_PKG)@$(GOVULNCHECK_VERSION)

vuln: $(GOVULNCHECK)
	$(GOVULNCHECK) ./...

print-build-meta:
	@printf 'VERSION=%s\nCOMMIT=%s\nDATE=%s\nDIRTY=%s\n' "$(VERSION)" "$(COMMIT)" "$(DATE)" "$(DIRTY)"

release-check:
	goreleaser check

release-snapshot:
	goreleaser release --snapshot --clean
