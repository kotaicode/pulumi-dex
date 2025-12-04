.PHONY: build install generate-sdks test clean help

# Build the provider binary
build:
	@echo "Building pulumi-resource-dex..."
	@mkdir -p bin
	@go build -o bin/pulumi-resource-dex ./cmd/pulumi-resource-dex
	@echo "✓ Built bin/pulumi-resource-dex"

# Install the provider locally (requires Pulumi CLI)
install: build
	@echo "Installing provider..."
	@pulumi plugin install resource dex v0.1.0 --file bin/pulumi-resource-dex || true
	@echo "✓ Provider installed"

# Generate language SDKs (requires Pulumi CLI)
generate-sdks: build
	@echo "Generating SDKs..."
	@mkdir -p sdk
	@pulumi package gen-sdk bin/pulumi-resource-dex --language typescript --out sdk/typescript || echo "⚠ TypeScript SDK generation failed (Pulumi CLI required)"
	@pulumi package gen-sdk bin/pulumi-resource-dex --language go --out sdk/go || echo "⚠ Go SDK generation failed (Pulumi CLI required)"
	@pulumi package gen-sdk bin/pulumi-resource-dex --language python --out sdk/python || echo "⚠ Python SDK generation failed (Pulumi CLI required)"
	@echo "✓ SDKs generated in sdk/"

# Run tests
test:
	@echo "Running tests..."
	@go test ./... -v

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
	@echo "  build          - Build the provider binary"
	@echo "  install        - Build and install the provider locally"
	@echo "  generate-sdks  - Generate language SDKs (requires Pulumi CLI)"
	@echo "  test           - Run tests"
	@echo "  clean          - Clean build artifacts"
	@echo "  dex-up         - Start local Dex with docker-compose"
	@echo "  dex-down       - Stop local Dex"
	@echo "  help           - Show this help message"

