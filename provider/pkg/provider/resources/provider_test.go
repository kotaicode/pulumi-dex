package resources

import (
	"testing"

	"github.com/blang/semver"
	"github.com/pulumi/pulumi-go-provider/infer"
	"github.com/pulumi/pulumi-go-provider/integration"

	"github.com/kotaicode/pulumi-dex/pkg/provider"
)

// newTestProvider serves the provider as main.go builds it (same resources, config
// and Read wrapper) through the framework's integration harness. It is not
// configured, which is enough for GetSchema and preview; see newProviderAgainst
// for a server talking to a fake Dex.
func newTestProvider(t *testing.T) integration.Server {
	t.Helper()
	prov, err := infer.NewProviderBuilder().
		WithNamespace("dex").
		WithResources(
			infer.Resource(&Client{}),
			infer.Resource(&Connector{}),
			infer.Resource(&AzureOidcConnector{}),
			infer.Resource(&AzureMicrosoftConnector{}),
			infer.Resource(&CognitoOidcConnector{}),
			infer.Resource(&GitLabConnector{}),
			infer.Resource(&GitHubConnector{}),
			infer.Resource(&GoogleConnector{}),
			infer.Resource(&LocalConnector{}),
			infer.Resource(&LocalUser{}),
		).
		WithConfig(infer.Config(&provider.DexConfig{})).
		Build()
	if err != nil {
		t.Fatalf("build provider: %v", err)
	}
	s, err := integration.NewServer(t.Context(), "dex", semver.MustParse("0.0.0"),
		integration.WithProvider(provider.WithTypedConfigValues(provider.WithPreviewSafeConfigure(provider.WithSecretReadInputs(prov)))))
	if err != nil {
		t.Fatalf("new integration server: %v", err)
	}
	return s
}
