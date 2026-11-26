package resources

import (
	"strings"
	"testing"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

// =============================================================================
// Unreadable connector config in Dex (DEX-009 requirement 6)
//
// Dex v2.46.1 only checks that a connector config is valid JSON, so its API
// accepts a config that is not an object or holds a known field with the wrong
// type; a syntactically invalid config can only come from storage written
// outside the API. Refresh must fail with an error naming the problem by path,
// without revealing config values, instead of reporting the connector as deleted
// or silently reading the field as empty.
// =============================================================================

const unreadableCanary = "CANARY-UNREADABLE"

type unreadableCase struct {
	name, typ string
	inputs    map[string]property.Value
	// fieldConfig is valid JSON with one known field of the wrong type.
	fieldConfig string
	wantPath    string
}

func unreadableCases() []unreadableCase {
	oauth := func(extra map[string]property.Value) map[string]property.Value {
		m := map[string]property.Value{
			"connectorId": str("conn"), "name": str("Conn"), "clientId": str("client"),
			"clientSecret": str(unreadableCanary), "redirectUri": str("https://dex.example.com/callback"),
		}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	secret := `"clientSecret":"` + unreadableCanary + `"`
	return []unreadableCase{
		{"AzureOidcConnector", "dex:resources:AzureOidcConnector",
			oauth(map[string]property.Value{"tenantId": str("00000000-0000-0000-0000-000000000000")}),
			`{"issuer":"https://login.microsoftonline.com/t/v2.0","clientID":"client",` + secret + `,"scopes":"openid"}`, "config.scopes"},
		{"AzureMicrosoftConnector", "dex:resources:AzureMicrosoftConnector",
			oauth(map[string]property.Value{"tenant": str("organizations")}),
			`{"tenant":true,"clientID":"client",` + secret + `}`, "config.tenant"},
		{"CognitoOidcConnector", "dex:resources:CognitoOidcConnector",
			oauth(map[string]property.Value{"region": str("eu-central-1"), "userPoolId": str("eu-central-1_pool")}),
			`{"issuer":5,"clientID":"client",` + secret + `}`, "config.issuer"},
		{"GitHubConnector", "dex:resources:GitHubConnector", oauth(nil),
			`{"clientID":"client",` + secret + `,"orgs":[{"name":"ok"},{"name":5}]}`, "config.orgs[1].name"},
		{"GitLabConnector", "dex:resources:GitLabConnector", oauth(nil),
			`{"clientID":"client",` + secret + `,"groups":"admins"}`, "config.groups"},
		{"GoogleConnector", "dex:resources:GoogleConnector", oauth(nil),
			`{"clientID":"client",` + secret + `,"hostedDomains":["example.com",1]}`, "config.hostedDomains[1]"},
	}
}

// readAfterRemoteConfig creates the resource, replaces its config in the fake Dex
// and runs the refresh Read.
func readAfterRemoteConfig(t *testing.T, typ string, inputs map[string]property.Value, remote string) (p.ReadResponse, error) {
	t.Helper()
	dex := newFakeDex()
	s := newProviderAgainst(t, dex)
	urn := testURN(typ)
	checked, err := s.Check(p.CheckRequest{Urn: urn, Inputs: property.NewMap(inputs)})
	if err != nil || len(checked.Failures) > 0 {
		t.Fatalf("Check: %v %v", err, checked.Failures)
	}
	created, err := s.Create(p.CreateRequest{Urn: urn, Properties: checked.Inputs})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	dex.mu.Lock()
	dex.connectors[created.ID].Config = []byte(remote)
	dex.mu.Unlock()
	return s.Read(p.ReadRequest{ID: created.ID, Urn: urn, Properties: created.Properties, Inputs: checked.Inputs})
}

func TestUnreadableConfig_typedConnectorsFailRead(t *testing.T) {
	broken := map[string]struct{ config, want string }{
		"invalid JSON":     {`{"clientID":"client","clientSecret":"` + unreadableCanary, "not valid JSON"},
		"array":            {`["` + unreadableCanary + `"]`, "JSON array, expected an object"},
		"null":             {`null`, "JSON null, expected an object"},
		"string":           {`"` + unreadableCanary + `"`, "JSON string, expected an object"},
		"wrong field type": {},
	}
	for _, tc := range unreadableCases() {
		for name, b := range broken {
			config, want := b.config, b.want
			if name == "wrong field type" {
				config, want = tc.fieldConfig, tc.wantPath
			}
			t.Run(tc.name+"/"+name, func(t *testing.T) {
				resp, err := readAfterRemoteConfig(t, tc.typ, tc.inputs, config)
				if err == nil {
					t.Fatalf("Read succeeded (ID=%q); an unreadable config must fail the refresh, not report the connector as deleted or empty", resp.ID)
				}
				msg := err.Error()
				for _, sub := range []string{`"conn"`, want} {
					if !strings.Contains(msg, sub) {
						t.Errorf("error %q should contain %q", msg, sub)
					}
				}
				if strings.Contains(msg, unreadableCanary) {
					t.Errorf("error reveals config content: %q", msg)
				}
			})
		}
	}
}

// The generic connector can represent any stored config losslessly as rawConfig,
// so it keeps the resource and falls back to raw instead of failing. It must not
// silently drop a known field whose type does not fit oidcConfig.
func TestUnreadableConfig_genericConnectorFallsBackToRaw(t *testing.T) {
	typed := map[string]property.Value{
		"connectorId": str("conn"), "type": str("oidc"), "name": str("Conn"),
		"oidcConfig": obj(map[string]property.Value{
			"issuer": str("https://issuer.example.com"), "clientId": str("client"),
			"clientSecret": str(unreadableCanary), "redirectUri": str("https://dex.example.com/callback"),
		}),
	}
	for name, remote := range map[string]string{
		"wrong field type": `{"issuer":"https://issuer.example.com","clientID":"client","clientSecret":"` + unreadableCanary + `","scopes":"openid"}`,
		"array":            `["` + unreadableCanary + `"]`,
		"invalid JSON":     `{"issuer":"https://issuer.example.com","clientSecret":"` + unreadableCanary,
	} {
		t.Run(name, func(t *testing.T) {
			resp, err := readAfterRemoteConfig(t, "dex:resources:Connector", typed, remote)
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			raw := resp.Inputs.Get("rawConfig")
			if !raw.IsString() || raw.AsString() != remote {
				t.Errorf("rawConfig = %v, want the stored config verbatim", raw)
			}
			if !raw.IsSecret() || !resp.Properties.Get("rawConfig").IsSecret() {
				t.Error("rawConfig must stay secret")
			}
			if v, ok := resp.Inputs.GetOk("oidcConfig"); ok && !v.IsNull() {
				t.Errorf("oidcConfig should be absent after the raw fallback, got %v", v)
			}
		})
	}
}
