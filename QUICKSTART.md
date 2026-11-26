# Quick Start Guide

This guide will help you get started with the Pulumi Dex provider quickly.

## Prerequisites

- [Pulumi CLI](https://www.pulumi.com/docs/get-started/install/) installed
- [Docker](https://www.docker.com/) and Docker Compose (for local Dex)
- Go 1.24+ (for building the provider)

## Step 1: Build the Provider

```bash
# Build the provider binary
make build
# or manually:
go build -o bin/pulumi-resource-dex ./cmd/pulumi-resource-dex
```

## Step 2: Start Local Dex (Optional)

For local testing, start Dex using docker-compose:

```bash
# Start Dex
make dex-up
# or manually:
docker-compose up -d

# Verify Dex is running
curl http://localhost:5556/healthz
```

Dex will be available at:
- Web UI: http://localhost:5556
- gRPC API: localhost:5557 (insecure for development)

## Step 3: Install the Provider

```bash
# Install the provider locally
make install
# or manually (<version> = VERSION in the Makefile):
pulumi plugin install resource dex v<version> --file bin/pulumi-resource-dex
```

## Step 4: Generate SDKs (Optional)

If you want to use the provider from TypeScript, Go, or Python:

```bash
# Generate all SDKs
make generate-sdks

# Or generate specific SDKs:
pulumi package gen-sdk bin/pulumi-resource-dex --language typescript --out sdk/typescript
pulumi package gen-sdk bin/pulumi-resource-dex --language go --out sdk/go
```

## Step 5: Create a Pulumi Program

### TypeScript Example

```bash
# Navigate to examples
cd examples/typescript

# Install dependencies (after SDK is generated)
npm install

# Initialize Pulumi stack
pulumi stack init dev

# Edit index.ts with your configuration
# Then preview and apply:
pulumi preview
pulumi up
```

### Go Example

After generating the Go SDK:

```go
package main

import (
    "github.com/pulumi/pulumi/sdk/v3/go/pulumi"
    dex "github.com/kotaicode/pulumi-dex/sdk/go/dex"
)

func main() {
    pulumi.Run(func(ctx *pulumi.Context) error {
        // Configure provider
        provider, err := dex.NewProvider(ctx, "dex", &dex.ProviderArgs{
            Host:      pulumi.String("localhost:5557"),
            Plaintext: pulumi.Bool(true), // the local docker-compose Dex serves gRPC without TLS
        })
        if err != nil {
            return err
        }

        // Create a client
        client, err := dex.NewClient(ctx, "webClient", &dex.ClientArgs{
            ClientId: pulumi.String("my-web-app"),
            Name: pulumi.String("My Web App"),
            RedirectUris: pulumi.StringArray{
                pulumi.String("http://localhost:3000/callback"),
            },
        }, pulumi.Provider(provider))
        if err != nil {
            return err
        }

        ctx.Export("clientId", client.ClientId)
        return nil
    })
}
```

## Step 6: Test the Provider

### Using the Simple Client

The `simple-client` directory contains a test program:

```bash
cd simple-client
go run . --host localhost:5557
```

### Using Pulumi

```bash
# In your Pulumi program directory
pulumi preview  # See what will be created
pulumi up       # Create resources
pulumi destroy  # Clean up
```

## Troubleshooting

### Provider not found

If you get "provider not found" errors:

```bash
# Reinstall the provider
make install

# Or check installed plugins
pulumi plugin ls
```

### Dex connection errors

- Verify Dex is running: `curl http://localhost:5556/healthz`
- Check gRPC port: `netstat -an | grep 5557`
- The provider connects with TLS by default. For the local docker-compose Dex, which serves gRPC without TLS, set `plaintext: true`
- For a Dex with TLS, set `caCert` if its certificate comes from a private CA, and `clientCert` and `clientKey` if it requires client certificates

### SDK generation fails

- Ensure Pulumi CLI is installed: `pulumi version`
- Ensure provider binary exists: `ls -la bin/pulumi-resource-dex`
- Check Pulumi CLI version compatibility

## Next Steps

- Read the full [README.md](README.md) for detailed documentation
- Check [examples/](examples/) for more examples
- Review [docs/](docs/) for design and implementation details

