# SDK Generation Guide

This guide explains how to generate language SDKs from the Pulumi Dex provider.

## Prerequisites

1. **Pulumi CLI installed**: Required for SDK generation
   ```bash
   curl -fsSL https://get.pulumi.com | sh
   ```

2. **Provider binary built**: The provider must be built first
   ```bash
   make build
   # or
   go build -o bin/pulumi-resource-dex ./cmd/pulumi-resource-dex
   ```

## Generating SDKs

### Generate All SDKs

```bash
make generate-sdks
```

This will generate SDKs for:
- TypeScript/JavaScript → `sdk/typescript/`
- Go → `sdk/go/`
- Python → `sdk/python/`

### Generate Individual SDKs

#### TypeScript SDK

```bash
pulumi package gen-sdk bin/pulumi-resource-dex \
  --language typescript \
  --out sdk/typescript
```

After generation, you can use it in your TypeScript projects:

```bash
cd sdk/typescript
npm install
npm link  # or publish to npm
```

Then in your Pulumi program:

```bash
npm install @kotaicode/pulumi-dex
```

#### Go SDK

```bash
pulumi package gen-sdk bin/pulumi-resource-dex \
  --language go \
  --out sdk/go
```

After generation, you can use it in your Go projects:

```go
import dex "github.com/kotaicode/pulumi-dex/sdk/go/dex"
```

#### Python SDK

```bash
pulumi package gen-sdk bin/pulumi-resource-dex \
  --language python \
  --out sdk/python
```

After generation, you can use it in your Python projects:

```bash
cd sdk/python
pip install -e .
```

Then in your Pulumi program:

```python
import pulumi_dex as dex
```

## Using Generated SDKs

### TypeScript Example

```typescript
import * as dex from "@kotaicode/pulumi-dex";

const provider = new dex.Provider("dex", {
    host: "localhost:5557",
    insecureSkipVerify: true,
});

const client = new dex.Client("webClient", {
    clientId: "my-app",
    name: "My App",
    redirectUris: ["http://localhost:3000/callback"],
}, { provider });
```

### Go Example

```go
package main

import (
    "github.com/pulumi/pulumi/sdk/v3/go/pulumi"
    dex "github.com/kotaicode/pulumi-dex/sdk/go/dex"
)

func main() {
    pulumi.Run(func(ctx *pulumi.Context) error {
        provider, err := dex.NewProvider(ctx, "dex", &dex.ProviderArgs{
            Host: pulumi.String("localhost:5557"),
        })
        if err != nil {
            return err
        }

        client, err := dex.NewClient(ctx, "webClient", &dex.ClientArgs{
            ClientId: pulumi.String("my-app"),
            Name: pulumi.String("My App"),
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

### Python Example

```python
import pulumi
import pulumi_dex as dex

provider = dex.Provider("dex",
    host="localhost:5557",
    insecure_skip_verify=True
)

client = dex.Client("webClient",
    client_id="my-app",
    name="My App",
    redirect_uris=["http://localhost:3000/callback"],
    opts=pulumi.ResourceOptions(provider=provider)
)

pulumi.export("client_id", client.client_id)
```

## Troubleshooting

### "Command not found: pulumi"

Install Pulumi CLI:
```bash
curl -fsSL https://get.pulumi.com | sh
```

### "Provider binary not found"

Build the provider first:
```bash
make build
```

### SDK generation fails

- Ensure Pulumi CLI version is compatible (v3.x)
- Check that the provider binary is executable
- Verify the binary was built correctly: `./bin/pulumi-resource-dex --version`

### Import errors in generated SDKs

- Regenerate the SDKs after making changes to the provider
- Ensure you're using the correct import path
- For TypeScript, run `npm install` in the SDK directory
- For Go, ensure the module path matches your `go.mod`

## Publishing SDKs

### TypeScript/JavaScript (npm)

```bash
cd sdk/typescript
npm publish
```

### Go

The Go SDK is typically used directly from the repository or published as a Go module.

### Python (PyPI)

```bash
cd sdk/python
python setup.py sdist bdist_wheel
twine upload dist/*
```

## Versioning

When updating the provider:
1. Update the version in `cmd/pulumi-resource-dex/main.go` (in `prov.Run()`)
2. Rebuild the provider
3. Regenerate all SDKs
4. Update SDK versions accordingly
5. Publish updated SDKs

