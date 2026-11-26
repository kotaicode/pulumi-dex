package resources

import (
	"encoding/json"
	"strings"
	"testing"

	p "github.com/pulumi/pulumi-go-provider"
	presource "github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

// =============================================================================
// Secret handling
// =============================================================================

// TestSecretPolicy_schema pins the secrecy policy for every credential-capable
// field. Opaque containers (raw JSON, extension maps) are secret as a whole because
// their contents cannot be classified. Non-credential settings that only look
// sensitive (GitHubConnector.rootCA is a public CA certificate,
// GoogleConnector.serviceAccountFilePath is a path on the Dex server) stay plain
// so that diffs remain reviewable.
func TestSecretPolicy_schema(t *testing.T) {
	resp, err := newTestProvider(t).GetSchema(p.GetSchemaRequest{})
	if err != nil {
		t.Fatalf("GetSchema: %v", err)
	}
	var schema struct {
		Config struct {
			Variables map[string]struct {
				Secret bool `json:"secret"`
			} `json:"variables"`
		} `json:"config"`
		Resources map[string]struct {
			InputProperties map[string]struct {
				Secret bool `json:"secret"`
			} `json:"inputProperties"`
			Properties map[string]struct {
				Secret bool `json:"secret"`
			} `json:"properties"`
		} `json:"resources"`
		Types map[string]struct {
			Properties map[string]struct {
				Secret bool `json:"secret"`
			} `json:"properties"`
		} `json:"types"`
	}
	if err := json.Unmarshal([]byte(resp.Schema), &schema); err != nil {
		t.Fatalf("decode schema: %v", err)
	}

	resourceFields := map[string]map[string]bool{
		"dex:resources:Client":                  {"secret": true},
		"dex:resources:Connector":               {"rawConfig": true},
		"dex:resources:AzureOidcConnector":      {"clientSecret": true, "extraOidc": true},
		"dex:resources:AzureMicrosoftConnector": {"clientSecret": true},
		"dex:resources:CognitoOidcConnector":    {"clientSecret": true, "extraOidc": true},
		"dex:resources:GitHubConnector":         {"clientSecret": true, "rootCA": false},
		"dex:resources:GitLabConnector":         {"clientSecret": true},
		"dex:resources:GoogleConnector":         {"clientSecret": true, "serviceAccountFilePath": false},
	}
	for token, fields := range resourceFields {
		res, ok := schema.Resources[token]
		if !ok {
			t.Errorf("schema has no resource %s", token)
			continue
		}
		for field, want := range fields {
			in, ok := res.InputProperties[field]
			if !ok {
				t.Errorf("%s has no input %q", token, field)
				continue
			}
			if in.Secret != want {
				t.Errorf("%s input %q: secret = %v, want %v", token, field, in.Secret, want)
			}
			if out := res.Properties[field]; out.Secret != want {
				t.Errorf("%s output %q: secret = %v, want %v", token, field, out.Secret, want)
			}
		}
	}

	typeFields := map[string]map[string]bool{
		"dex:resources:OIDCConfig": {"clientSecret": true, "extra": true},
	}
	for token, fields := range typeFields {
		for field, want := range fields {
			if got := schema.Types[token].Properties[field].Secret; got != want {
				t.Errorf("%s property %q: secret = %v, want %v", token, field, got, want)
			}
		}
	}

	for _, v := range []string{"caCert", "clientCert", "clientKey"} {
		if !schema.Config.Variables[v].Secret {
			t.Errorf("provider config %q must be secret", v)
		}
	}
}

// TestSecretPolicy_previewOutputs checks the runtime contract that the schema
// annotation alone does not prove: values supplied as plain literals (not wrapped in
// pulumi.secret) come back from the provider marked secret, so they are neither
// shown in preview output nor stored in plaintext.
func TestSecretPolicy_previewOutputs(t *testing.T) {
	const canary = "synthetic-canary-secret"
	oauth := map[string]property.Value{
		"connectorId":  property.New("conn"),
		"name":         property.New("Conn"),
		"clientId":     property.New("cid"),
		"clientSecret": property.New(canary),
		"redirectUri":  property.New("https://dex.example.com/callback"),
	}
	withOAuth := func(extra map[string]property.Value) map[string]property.Value {
		out := map[string]property.Value{}
		for k, v := range oauth {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	extension := property.New(map[string]property.Value{"clientAssertion": property.New(canary)})

	cases := []struct {
		name   string
		typ    string
		inputs map[string]property.Value
		secret func(property.Map) property.Value
	}{
		{
			name: "Connector.rawConfig",
			typ:  "dex:resources:Connector",
			inputs: map[string]property.Value{
				"connectorId": property.New("conn"),
				"type":        property.New("ldap"),
				"name":        property.New("Conn"),
				"rawConfig":   property.New(`{"bindPW":"` + canary + `"}`),
			},
			secret: func(m property.Map) property.Value { return m.Get("rawConfig") },
		},
		{
			// The framework can only mark top-level outputs, so the whole object that
			// carries clientSecret and extra is secret.
			name: "Connector.oidcConfig",
			typ:  "dex:resources:Connector",
			inputs: map[string]property.Value{
				"connectorId": property.New("conn"),
				"type":        property.New("oidc"),
				"name":        property.New("Conn"),
				"oidcConfig": property.New(map[string]property.Value{
					"issuer":       property.New("https://issuer.example.com"),
					"clientId":     property.New("cid"),
					"clientSecret": property.New(canary),
					"redirectUri":  property.New("https://dex.example.com/callback"),
					"extra":        extension,
				}),
			},
			secret: func(m property.Map) property.Value { return m.Get("oidcConfig") },
		},
		{
			name:   "AzureOidcConnector.extraOidc",
			typ:    "dex:resources:AzureOidcConnector",
			inputs: withOAuth(map[string]property.Value{"tenantId": property.New("tenant"), "extraOidc": extension}),
			secret: func(m property.Map) property.Value { return m.Get("extraOidc") },
		},
		{
			name: "CognitoOidcConnector.extraOidc",
			typ:  "dex:resources:CognitoOidcConnector",
			inputs: withOAuth(map[string]property.Value{
				"region":     property.New("eu-central-1"),
				"userPoolId": property.New("eu-central-1_pool"),
				"extraOidc":  extension,
			}),
			secret: func(m property.Map) property.Value { return m.Get("extraOidc") },
		},
		{
			name:   "AzureOidcConnector.clientSecret",
			typ:    "dex:resources:AzureOidcConnector",
			inputs: withOAuth(map[string]property.Value{"tenantId": property.New("tenant")}),
			secret: func(m property.Map) property.Value { return m.Get("clientSecret") },
		},
		{
			name:   "AzureMicrosoftConnector.clientSecret",
			typ:    "dex:resources:AzureMicrosoftConnector",
			inputs: withOAuth(map[string]property.Value{"tenant": property.New("tenant")}),
			secret: func(m property.Map) property.Value { return m.Get("clientSecret") },
		},
		{
			name: "CognitoOidcConnector.clientSecret",
			typ:  "dex:resources:CognitoOidcConnector",
			inputs: withOAuth(map[string]property.Value{
				"region":     property.New("eu-central-1"),
				"userPoolId": property.New("eu-central-1_pool"),
			}),
			secret: func(m property.Map) property.Value { return m.Get("clientSecret") },
		},
		{
			name:   "GitHubConnector.clientSecret",
			typ:    "dex:resources:GitHubConnector",
			inputs: oauth,
			secret: func(m property.Map) property.Value { return m.Get("clientSecret") },
		},
		{
			name:   "GitLabConnector.clientSecret",
			typ:    "dex:resources:GitLabConnector",
			inputs: oauth,
			secret: func(m property.Map) property.Value { return m.Get("clientSecret") },
		},
		{
			name:   "GoogleConnector.clientSecret",
			typ:    "dex:resources:GoogleConnector",
			inputs: oauth,
			secret: func(m property.Map) property.Value { return m.Get("clientSecret") },
		},
		{
			name: "Client.secret",
			typ:  "dex:resources:Client",
			inputs: map[string]property.Value{
				"clientId":     property.New("app"),
				"name":         property.New("App"),
				"secret":       property.New(canary),
				"redirectUris": property.New([]property.Value{property.New("https://app.example.com/callback")}),
			},
			secret: func(m property.Map) property.Value { return m.Get("secret") },
		},
	}

	s := newTestProvider(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := s.Create(p.CreateRequest{
				Urn:        presource.URN("urn:pulumi:test::test::" + tc.typ + "::r"),
				Properties: property.NewMap(tc.inputs),
				DryRun:     true,
			})
			if err != nil {
				t.Fatalf("preview Create: %v", err)
			}
			if v := tc.secret(resp.Properties); !v.IsSecret() {
				t.Errorf("output is not marked secret: plain-literal input would be stored and displayed in plaintext")
			}
		})
	}
}

func TestBuildConnectorConfigBytes_invalidJSONDoesNotEchoConfig(t *testing.T) {
	const canary = "synthetic-canary-secret"
	for _, raw := range []string{
		`{"bindPW": ` + canary + `}`,  // unquoted value
		`{"bindPW": "` + canary + `"`, // truncated
		canary,
	} {
		_, err := buildConnectorConfigBytes(ConnectorArgs{RawConfig: &raw})
		if err == nil {
			t.Fatalf("expected a JSON error for %d-byte input", len(raw))
		}
		if strings.Contains(err.Error(), canary) {
			t.Errorf("error echoes rawConfig content: %v", err)
		}
	}
}
