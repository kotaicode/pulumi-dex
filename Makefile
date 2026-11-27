.PHONY: build install check-pulumi-version generate-schema generate-sdks test lint lint-fix clean help

# Default version for local development only. Releases always pass the
# version from the git tag (make build VERSION=x.y.z); never rely on this
# default for anything that gets published.
VERSION ?= 0.11.0

# Pulumi CLI version that generates the committed schema and SDKs. CI and the
# release workflow install the same version from .pulumi.version; another CLI
# version can generate different SDK code.
PULUMI_VERSION := $(shell cat .pulumi.version)

# Build the provider binary
build:
	@echo "Building pulumi-resource-dex (version: $(VERSION))..."
	@mkdir -p bin
	@cd provider && go build -ldflags "-X 'github.com/kotaicode/pulumi-dex/pkg/provider.Version=$(VERSION)'" -o ../bin/pulumi-resource-dex ./cmd/pulumi-resource-dex
	@echo "✓ Built bin/pulumi-resource-dex"

# Install the provider locally (requires Pulumi CLI)
install: build
	@echo "Installing provider..."
	@pulumi plugin install resource dex v$(VERSION) --file bin/pulumi-resource-dex || true
	@echo "✓ Provider installed"

# Warn when the local Pulumi CLI differs from the pinned version; fail in CI
check-pulumi-version:
	@v=$$(pulumi version 2>/dev/null | sed 's/^v//'); \
	if [ "$$v" != "$(PULUMI_VERSION)" ]; then \
		echo "⚠ Pulumi CLI is $${v:-not installed}, but schema and SDKs are generated with $(PULUMI_VERSION); the output may differ"; \
		if [ "$${CI:-}" = "true" ]; then exit 1; fi; \
	fi

# Generate schema.json from the freshly built provider (requires Pulumi CLI).
# Written to a temporary file first so a failure leaves schema.json intact.
generate-schema: build check-pulumi-version
	@echo "Generating schema.json..."
	@pulumi package get-schema ./bin/pulumi-resource-dex > bin/schema.json.tmp || { rm -f bin/schema.json.tmp; echo "✗ Schema generation failed"; exit 1; }
	@mv bin/schema.json.tmp schema.json

# Generate language SDKs from a freshly generated schema.json (requires Pulumi CLI)
generate-sdks: generate-schema
	@echo "Generating SDKs..."
	@./scripts/generate-sdks.sh

# Run tests (same scope as CI: provider library only)
test:
	@echo "Running tests..."
	@cd provider && go test ./pkg/... -v

# Run linter (same as CI)
lint:
	@cd provider && golangci-lint run ./pkg/... --build-tags=dexlive --timeout=5m

# Auto-fix linter issues (gci, gofmt, goimports)
lint-fix:
	@cd provider && golangci-lint run ./pkg/... --build-tags=dexlive --fix --timeout=5m
	@echo "✓ Lint fixes applied"

# Quick local test before publishing: generate SDK (with fix scripts), build TS SDK, run test-npm-package with isolatedModules
test-npm-package: generate-schema generate-sdks
	@chmod +x scripts/test-npm-package.sh
	@./scripts/test-npm-package.sh

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf bin/
	@rm -rf sdk/
	@rm -f pulumi-resource-dex
	@echo "✓ Cleaned"

# Start local Dex for testing
dex-up:
	@echo "Starting Dex with docker compose..."
	@if docker compose version >/dev/null 2>&1; then \
		docker compose up -d; \
	else \
		docker-compose up -d; \
	fi
	@echo "✓ Dex started at localhost:5557 (gRPC) and http://localhost:5556 (web)"

# Stop local Dex
dex-down:
	@echo "Stopping Dex..."
	@if docker compose version >/dev/null 2>&1; then \
		docker compose down; \
	else \
		docker-compose down; \
	fi
	@echo "✓ Dex stopped"

# Show help
help:
	@echo "Available targets:"
	@echo "  build           - Build the provider binary"
	@echo "  install         - Build and install the provider locally"
	@echo "  generate-schema - Generate schema.json (requires Pulumi CLI)"
	@echo "  generate-sdks   - Generate language SDKs (requires Pulumi CLI)"
	@echo "  test            - Run tests"
	@echo "  test-npm-package - Generate SDK, build TS, run test-npm-package (isolatedModules)"
	@echo "  lint            - Run golangci-lint (CI checks)"
	@echo "  lint-fix        - Auto-fix linter issues (gci, gofmt, goimports)"
	@echo "  clean           - Clean build artifacts"
	@echo "  dex-up          - Start local Dex with docker-compose"
	@echo "  dex-down        - Stop local Dex"
	@echo "  help            - Show this help message"
