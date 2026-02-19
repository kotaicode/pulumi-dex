.PHONY: build install generate-schema generate-sdks test clean help

# Default version for local development
VERSION ?= 0.7.12

# Build the provider binary
build:
	@echo "Building pulumi-resource-dex (version: $(VERSION))..."
	@mkdir -p bin
	@cd provider && go build -ldflags "-X 'github.com/kotaicode/pulumi-dex/pkg/provider.Version=$(VERSION)'" -o ../bin/pulumi-resource-dex ./cmd/pulumi-resource-dex
	@echo "✓ Built bin/pulumi-resource-dex"

# Install the provider locally (requires Pulumi CLI)
install: build
	@echo "Installing provider..."
	@pulumi plugin install resource dex v0.1.0 --file bin/pulumi-resource-dex || true
	@echo "✓ Provider installed"

# Generate schema.json (requires Pulumi CLI)
generate-schema: build
	@echo "Generating schema.json..."
	@pulumi package get-schema ./bin/pulumi-resource-dex > schema.json || (echo "⚠ Schema generation failed (Pulumi CLI required)" && exit 1)

# Generate language SDKs (requires Pulumi CLI)
generate-sdks: build
	@echo "Generating SDKs..."
	@mkdir -p sdk
	@# Backup TypeScript README.md if it exists
	@if [ -f sdk/typescript/nodejs/README.md ] && [ -s sdk/typescript/nodejs/README.md ]; then \
		cp sdk/typescript/nodejs/README.md /tmp/typescript-sdk-readme.md.bak; \
	fi
	@# Generate SDKs from schema.json or provider binary
	@if [ -f schema.json ]; then \
		echo "Using schema.json for SDK generation..."; \
		pulumi package gen-sdk schema.json --language typescript --out sdk/typescript || echo "⚠ TypeScript SDK generation failed"; \
		pulumi package gen-sdk schema.json --language go --out sdk/go || echo "⚠ Go SDK generation failed"; \
		pulumi package gen-sdk schema.json --language python --out sdk/python || echo "⚠ Python SDK generation failed"; \
	else \
		echo "schema.json not found, using provider binary..."; \
		pulumi plugin install resource dex v0.1.0 --file bin/pulumi-resource-dex 2>/dev/null || true; \
		pulumi package gen-sdk dex --language typescript --out sdk/typescript || echo "⚠ TypeScript SDK generation failed"; \
		pulumi package gen-sdk dex --language go --out sdk/go || echo "⚠ Go SDK generation failed"; \
		pulumi package gen-sdk dex --language python --out sdk/python || echo "⚠ Python SDK generation failed"; \
	fi
	@# Fix Go SDK directory structure (codegen creates sdk/go/go/dex, we need sdk/go/dex)
	@rm -rf sdk/go/dex && [ -d sdk/go/go/dex ] && mv sdk/go/go/dex sdk/go/dex && rmdir sdk/go/go 2>/dev/null || true
	@# Ensure sdk/go.mod is at the correct location
	@[ -f sdk/go.mod ] || ([ -f sdk/go/go.mod ] && mv sdk/go/go.mod sdk/go.mod && mv sdk/go/go.sum sdk/go.sum 2>/dev/null || true)
	@# Restore TypeScript README.md if it was backed up
	@if [ -f /tmp/typescript-sdk-readme.md.bak ]; then \
		mkdir -p sdk/typescript/nodejs; \
		cp /tmp/typescript-sdk-readme.md.bak sdk/typescript/nodejs/README.md; \
		rm -f /tmp/typescript-sdk-readme.md.bak; \
	fi
	@# Ensure Python SDK has README.md
	@if [ ! -f sdk/python/python/README.md ] || [ ! -s sdk/python/python/README.md ]; then \
		[ -f README.md ] && cp README.md sdk/python/python/README.md || true; \
	fi
	@# Apply TypeScript export fixes for isolatedModules
	@./scripts/fix-typescript-exports.sh || echo "⚠ TypeScript exports fix failed (may already be fixed)"
	@./scripts/fix-resources-index-exports.sh || echo "⚠ Resources index exports fix failed (may already be fixed)"
	@echo "✓ SDKs generated in sdk/"

# Run tests
test:
	@echo "Running tests..."
	@cd provider && go test ./... -v

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
	@echo "Starting Dex with docker-compose..."
	@docker-compose up -d
	@echo "✓ Dex started at localhost:5557 (gRPC) and http://localhost:5556 (web)"

# Stop local Dex
dex-down:
	@echo "Stopping Dex..."
	@docker-compose down
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
	@echo "  clean           - Clean build artifacts"
	@echo "  dex-up          - Start local Dex with docker-compose"
	@echo "  dex-down        - Stop local Dex"
	@echo "  help            - Show this help message"
