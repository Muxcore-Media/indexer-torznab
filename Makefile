.PHONY: build test clean fmt tidy ci help

GO ?= go
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "0.0.0-dev")
LDFLAGS ?= -s -w -X main.version=$(VERSION)
BINARY ?= indexer-torznab

build:
	$(GO) build -ldflags="$(LDFLAGS)" -o $(BINARY) ./cmd/module

# Fixtures + httptest only — never dials live Torznab/Prowlarr/pirate APIs.
test:
	CGO_ENABLED=0 $(GO) test -count=1 -timeout 60s ./...

clean:
	rm -f $(BINARY)
	rm -f cmd/module/module
	rm -rf dist/

fmt:
	$(GO) fmt ./...

tidy:
	$(GO) mod tidy

ci: test build

help:
	@echo "Targets: build test clean fmt tidy ci"
