package resources

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

// =============================================================================
// Create → refresh → preview round trip (DEX-009, slice 1)
//
// Replays the engine's sequence through the provider protocol against the fake
// Dex, which stores connector config verbatim like Dex v2.46.1:
//
//	Check(program) → Create → Read(prior state and inputs) = refresh
//	→ Check(program, olds = refreshed inputs) → Diff = the next preview
//
// A clean cycle means Diff reports no changes, and no credential leaves a
// secret wrapper during any conversion.
// =============================================================================

const canary = "CANARY-ROUNDTRIP"

func str(s string) property.Value { return property.New(s) }

func strs(ss ...string) property.Value {
	vs := make([]property.Value, len(ss))
	for i, s := range ss {
		vs[i] = str(s)
	}
	return property.New(vs)
}

func obj(m map[string]property.Value) property.Value { return property.New(m) }

// advancedOIDC holds settings without a typed field: nested objects, arrays,
// numbers and an explicit false.
func advancedOIDC() property.Value {
	return obj(map[string]property.Value{
		"clientAssertion":      str(canary + "-extra"),
		"promptType":           str("consent"),
		"insecureEnableGroups": property.New(false),
		"claimModifications": obj(map[string]property.Value{
			"newGroupFromClaims": property.New([]property.Value{obj(map[string]property.Value{
				"claims":    strs("org", "team"),
				"delimiter": str(":"),
				"prefix":    str("gh"),
			})}),
		}),
		"providerDiscoveryOverrides": obj(map[string]property.Value{"tokenURL": str("https://token.example.com")}),
		"maxAgeSeconds":              property.New(3600.0),
	})
}

const rawOIDC = `{"issuer":"https://issuer.example.com","clientID":"raw-client","clientSecret":"` + canary +
	`-raw","redirectURI":"https://dex.example.com/callback","scopes":["openid","email"],` +
	`"insecureSkipEmailVerified":false,"claimModifications":{"newGroupFromClaims":[{"claims":["org"],"prefix":"x"}]},` +
	`"maxAgeSeconds":3600}`

func TestRefreshRoundTrip_isClean(t *testing.T) {
	oauth := func(extra map[string]property.Value) map[string]property.Value {
		m := map[string]property.Value{
			"connectorId":  str("conn"),
			"name":         str("Conn"),
			"clientId":     str("client"),
			"clientSecret": str(canary + "-secret"),
			"redirectUri":  str("https://dex.example.com/callback"),
		}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}

	cases := []struct {
		name, typ string
		inputs    map[string]property.Value
	}{
		{"generic raw OIDC", "dex:resources:Connector", map[string]property.Value{
			"connectorId": str("conn"), "type": str("oidc"), "name": str("Conn"),
			"rawConfig": str(rawOIDC),
		}},
		{"generic raw OIDC, reformatted JSON", "dex:resources:Connector", map[string]property.Value{
			"connectorId": str("conn"), "type": str("oidc"), "name": str("Conn"),
			"rawConfig": str("{\n  \"issuer\": \"https://issuer.example.com\",\n  \"clientSecret\": \"" + canary + "\"\n}"),
		}},
		{"generic raw LDAP", "dex:resources:Connector", map[string]property.Value{
			"connectorId": str("conn"), "type": str("ldap"), "name": str("Conn"),
			"rawConfig": str(`{"host":"ldap.example.com:636","bindPW":"` + canary + `","insecureNoSSL":false}`),
		}},
		{"generic typed OIDC, minimal", "dex:resources:Connector", map[string]property.Value{
			"connectorId": str("conn"), "type": str("oidc"), "name": str("Conn"),
			"oidcConfig": obj(map[string]property.Value{
				"issuer": str("https://issuer.example.com"), "clientId": str("client"),
				"clientSecret": str(canary), "redirectUri": str("https://dex.example.com/callback"),
			}),
		}},
		{"generic typed OIDC, all fields and extra", "dex:resources:Connector", map[string]property.Value{
			"connectorId": str("conn"), "type": str("oidc"), "name": str("Conn"),
			"oidcConfig": obj(map[string]property.Value{
				"issuer": str("https://issuer.example.com"), "clientId": str("client"),
				"clientSecret": str(canary), "redirectUri": str("https://dex.example.com/callback"),
				"scopes":                    strs("openid", "profile", "groups"),
				"insecureSkipEmailVerified": property.New(false),
				"insecureIssuer":            property.New(false),
				"userNameKey":               str("preferred_username"),
				"claimMapping":              obj(map[string]property.Value{"emailKey": str("mail"), "groupsKey": str("roles")}),
				"extra":                     advancedOIDC(),
			}),
		}},
		{"Azure OIDC, minimal", "dex:resources:AzureOidcConnector",
			oauth(map[string]property.Value{"tenantId": str("00000000-0000-0000-0000-000000000000")})},
		{"Azure OIDC, explicit default userNameSource", "dex:resources:AzureOidcConnector",
			oauth(map[string]property.Value{
				"tenantId": str("00000000-0000-0000-0000-000000000000"), "userNameSource": str("preferred_username"),
			})},
		{"Azure OIDC, upn, scopes and extraOidc", "dex:resources:AzureOidcConnector",
			oauth(map[string]property.Value{
				"tenantId": str("00000000-0000-0000-0000-000000000000"), "userNameSource": str("upn"),
				"scopes": strs("openid", "email"), "extraOidc": advancedOIDC(),
			})},
		{"Cognito OIDC, minimal", "dex:resources:CognitoOidcConnector",
			oauth(map[string]property.Value{"region": str("eu-central-1"), "userPoolId": str("eu-central-1_pool")})},
		{"Cognito OIDC, explicit default userNameSource", "dex:resources:CognitoOidcConnector",
			oauth(map[string]property.Value{
				"region": str("eu-central-1"), "userPoolId": str("eu-central-1_pool"), "userNameSource": str("email"),
			})},
		{"Cognito OIDC, extraOidc", "dex:resources:CognitoOidcConnector",
			oauth(map[string]property.Value{
				"region": str("eu-central-1"), "userPoolId": str("eu-central-1_pool"), "extraOidc": advancedOIDC(),
			})},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newProviderAgainst(t, newFakeDex())
			urn := testURN(tc.typ)
			program := property.NewMap(tc.inputs)

			checked, err := s.Check(p.CheckRequest{Urn: urn, Inputs: program})
			if err != nil || len(checked.Failures) > 0 {
				t.Fatalf("Check: %v %v", err, checked.Failures)
			}
			created, err := s.Create(p.CreateRequest{Urn: urn, Properties: checked.Inputs})
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			refreshed, err := s.Read(p.ReadRequest{
				ID: created.ID, Urn: urn, Properties: created.Properties, Inputs: checked.Inputs,
			})
			if err != nil || refreshed.ID == "" {
				t.Fatalf("Read: id=%q err=%v", refreshed.ID, err)
			}
			assertNoPlainCanary(t, "refreshed inputs", property.New(refreshed.Inputs))
			assertNoPlainCanary(t, "refreshed state", property.New(refreshed.Properties))

			rechecked, err := s.Check(p.CheckRequest{Urn: urn, State: refreshed.Inputs, Inputs: program})
			if err != nil || len(rechecked.Failures) > 0 {
				t.Fatalf("re-Check: %v %v", err, rechecked.Failures)
			}
			// Current engines (SupportsOldInputs) make infer diff the new inputs against
			// the refreshed *inputs*. The integration harness does not set that flag
			// and would fall back to the state, where outputs such as the whole-secret
			// oidcConfig differ from inputs only in secret wrapping, so pass the
			// refreshed inputs as the base explicitly.
			diff, err := s.Diff(p.DiffRequest{
				ID: refreshed.ID, Urn: urn,
				State: refreshed.Inputs, OldInputs: refreshed.Inputs, Inputs: rechecked.Inputs,
			})
			if err != nil {
				t.Fatalf("Diff: %v", err)
			}
			if diff.HasChanges {
				keys := make([]string, 0, len(diff.DetailedDiff))
				for k, d := range diff.DetailedDiff {
					keys = append(keys, fmt.Sprintf("%s(%v)", k, d.Kind))
				}
				sort.Strings(keys)
				t.Errorf("preview after refresh proposes changes: %s", strings.Join(keys, ", "))
			}
		})
	}
}

// assertNoPlainCanary fails if a canary credential appears outside a secret.
func assertNoPlainCanary(t *testing.T, where string, v property.Value) {
	t.Helper()
	var walk func(path string, v property.Value)
	walk = func(path string, v property.Value) {
		switch {
		case v.IsSecret():
			return
		case v.IsString():
			if strings.Contains(v.AsString(), canary) {
				t.Errorf("%s: credential in plaintext at %s", where, path)
			}
		case v.IsMap():
			for k, e := range v.AsMap().All() {
				walk(path+"."+k, e)
			}
		case v.IsArray():
			for i, e := range v.AsArray().All() {
				walk(fmt.Sprintf("%s[%d]", path, i), e)
			}
		}
	}
	walk("", v)
}

// The round-trip fixes must not mask real drift: a change made directly in Dex
// has to show up in the next preview.
func TestRefreshRoundTrip_reportsRemoteChanges(t *testing.T) {
	azure := map[string]property.Value{
		"connectorId": str("conn"), "name": str("Conn"),
		"tenantId": str("00000000-0000-0000-0000-000000000000"), "clientId": str("client"),
		"clientSecret": str(canary), "redirectUri": str("https://dex.example.com/callback"),
	}
	cases := []struct {
		name, typ string
		inputs    map[string]property.Value
		remote    string // config written directly to Dex after Create
		wantKey   string
	}{
		{"raw config changed", "dex:resources:Connector", map[string]property.Value{
			"connectorId": str("conn"), "type": str("oidc"), "name": str("Conn"), "rawConfig": str(rawOIDC),
		}, `{"issuer":"https://other.example.com","clientSecret":"` + canary + `"}`, "rawConfig"},
		{"Azure userNameKey changed from the default", "dex:resources:AzureOidcConnector", azure,
			`{"issuer":"https://login.microsoftonline.com/00000000-0000-0000-0000-000000000000/v2.0","clientID":"client",` +
				`"clientSecret":"` + canary + `","redirectURI":"https://dex.example.com/callback",` +
				`"scopes":["openid","profile","email","offline_access"],"userNameKey":"email"}`, "userNameSource"},
		{"Azure extra key added", "dex:resources:AzureOidcConnector", azure,
			`{"issuer":"https://login.microsoftonline.com/00000000-0000-0000-0000-000000000000/v2.0","clientID":"client",` +
				`"clientSecret":"` + canary + `","redirectURI":"https://dex.example.com/callback",` +
				`"scopes":["openid","profile","email","offline_access"],"userNameKey":"preferred_username","promptType":"none"}`, "extraOidc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dex := newFakeDex()
			s := newProviderAgainst(t, dex)
			urn := testURN(tc.typ)
			program := property.NewMap(tc.inputs)
			checked, err := s.Check(p.CheckRequest{Urn: urn, Inputs: program})
			if err != nil || len(checked.Failures) > 0 {
				t.Fatalf("Check: %v %v", err, checked.Failures)
			}
			created, err := s.Create(p.CreateRequest{Urn: urn, Properties: checked.Inputs})
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			dex.mu.Lock()
			dex.connectors[created.ID].Config = []byte(tc.remote)
			dex.mu.Unlock()

			refreshed, err := s.Read(p.ReadRequest{ID: created.ID, Urn: urn, Properties: created.Properties, Inputs: checked.Inputs})
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			assertNoPlainCanary(t, "refreshed inputs", property.New(refreshed.Inputs))
			rechecked, err := s.Check(p.CheckRequest{Urn: urn, State: refreshed.Inputs, Inputs: program})
			if err != nil {
				t.Fatalf("re-Check: %v", err)
			}
			diff, err := s.Diff(p.DiffRequest{
				ID: refreshed.ID, Urn: urn, State: refreshed.Inputs, OldInputs: refreshed.Inputs, Inputs: rechecked.Inputs,
			})
			if err != nil {
				t.Fatalf("Diff: %v", err)
			}
			if _, ok := diff.DetailedDiff[tc.wantKey]; !diff.HasChanges || !ok {
				t.Errorf("remote change to %s was masked; diff = %v", tc.wantKey, diff.DetailedDiff)
			}
		})
	}
}
