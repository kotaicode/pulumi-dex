package main

import (
	"context"
	"log"

	"github.com/pulumi/pulumi-go-provider/infer"

	"github.com/kotaicode/pulumi-dex/pkg/provider"
	"github.com/kotaicode/pulumi-dex/pkg/provider/resources"
)

// providerName is the logical name Pulumi will use for this provider.
const providerName = "dex"

func main() {
	prov, err := infer.NewProviderBuilder().
		WithNamespace("dex").
		WithDisplayName("Dex").
		WithPublisher("Kotaicode GmbH").
		WithKeywords("category/cloud").
		WithDescription("A Pulumi provider for managing Dex resources via the Dex gRPC Admin API").
		WithPluginDownloadURL("github://api.github.com/kotaicode/pulumi-dex").
		WithLanguageMap(map[string]any{
			"go": map[string]any{
				"importBasePath":                 "github.com/kotaicode/pulumi-dex/sdk/go/dex",
				"respectSchemaVersion":           true,
				"generateResourceContainerTypes": true,
			},
			"nodejs": map[string]any{
				"packageName":          "@kotaicode/pulumi-dex",
				"respectSchemaVersion": true,
				// Build-time tooling for the generated SDK. Pulumi's codegen defaults
				// (typescript ^4.7, @types/node ^20) lag behind; the committed
				// sdk/typescript/nodejs/package.json is regenerated on every release,
				// so this is the only place these versions can be changed.
				"typescriptVersion": "^6.0.0",
				"devDependencies":   map[string]string{"@types/node": "^24"},
			},
			"python": map[string]any{"packageName": "pulumi_dex", "respectSchemaVersion": true},
		}).
		WithRepository("https://github.com/kotaicode/pulumi-dex").
		WithResources(
			infer.Resource(&resources.Client{}),
			infer.Resource(&resources.Connector{}),
			infer.Resource(&resources.AzureOidcConnector{}),
			infer.Resource(&resources.AzureMicrosoftConnector{}),
			infer.Resource(&resources.CognitoOidcConnector{}),
			infer.Resource(&resources.GitLabConnector{}),
			infer.Resource(&resources.GitHubConnector{}),
			infer.Resource(&resources.GoogleConnector{}),
			infer.Resource(&resources.LocalConnector{}),
			infer.Resource(&resources.LocalUser{}),
		).
		WithFunctions(
			infer.Function(&resources.GetClient{}),
			infer.Function(&resources.GetConnector{}),
			infer.Function(&resources.GetLocalUser{}),
		).
		WithConfig(infer.Config(&provider.DexConfig{})).
		Build()
	if err != nil {
		log.Fatalf("failed to build dex provider: %v", err)
	}
	prov = provider.WithTypedConfigValues(provider.WithPreviewSafeConfigure(provider.WithSecretReadInputs(resources.WithMissingImportsFailing(prov))))

	if err := prov.Run(context.Background(), providerName, provider.Version); err != nil {
		log.Fatalf("provider run failed: %v", err)
	}
}
