package resources

import (
	"strings"
	"testing"

	api "github.com/dexidp/dex/api/v2"
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

// =============================================================================
// Connector config rules
//
// Extension maps (extraOidc, oidcConfig.extra) were merged over the typed
// fields, so an extension key could silently replace, for example, the client
// ID or issuer. Dex decodes its config case-insensitively, so every spelling of
// a typed field's Dex key collides. The generic Connector takes exactly one of
// oidcConfig and rawConfig, which is now checked at preview.
// =============================================================================

func checkFailures(t *testing.T, token string, inputs map[string]property.Value) []p.CheckFailure {
	t.Helper()
	resp, err := newTestProvider(t).Check(p.CheckRequest{Urn: testURN(token), Inputs: property.NewMap(inputs)})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	return resp.Failures
}

func assertNoValueInFailures(t *testing.T, failures []p.CheckFailure) {
	t.Helper()
	for _, f := range failures {
		if strings.Contains(f.Reason, canary) {
			t.Errorf("failure reveals a value: %s: %s", f.Property, f.Reason)
		}
	}
}

func typedOIDCInputs(token string, extraKey string, extraValue property.Value) map[string]property.Value {
	m := map[string]property.Value{
		"connectorId": str("conn"), "name": str("Conn"), "clientId": str("app"),
		"clientSecret": str(canary), "redirectUri": str("https://dex.example.com/callback"),
	}
	if token == "dex:resources:AzureOidcConnector" {
		m["tenantId"] = str("00000000-0000-0000-0000-000000000001")
	} else {
		m["region"], m["userPoolId"] = str("eu-central-1"), str("eu-central-1_pool")
	}
	if extraKey != "" {
		m["extraOidc"] = obj(map[string]property.Value{extraKey: extraValue})
	}
	return m
}

func TestExtraOidc_collisionsFailAtPreview(t *testing.T) {
	collisions := []string{"issuer", "clientID", "clientId", "CLIENTID", "clientSecret", "redirectURI", "redirectUri", "scopes", "userNameKey", "UserNameKey"}
	for _, token := range []string{"dex:resources:AzureOidcConnector", "dex:resources:CognitoOidcConnector"} {
		for _, key := range collisions {
			t.Run(token+"/"+key, func(t *testing.T) {
				failures := checkFailures(t, token, typedOIDCInputs(token, key, str(canary+"-override")))
				wantFailure(t, failures, "extraOidc", key)
				assertNoValueInFailures(t, failures)
			})
		}
		// Keys without a typed field stay allowed, and an unknown map is not judged.
		for name, inputs := range map[string]map[string]property.Value{
			"untyped key": typedOIDCInputs(token, "insecureSkipEmailVerified", property.New(true)),
			"unknown map": func() map[string]property.Value {
				m := typedOIDCInputs(token, "", property.Value{})
				m["extraOidc"] = property.New(property.Computed)
				return m
			}(),
			"unknown value under an untyped key": typedOIDCInputs(token, "promptType", property.New(property.Computed)),
		} {
			if f := checkFailures(t, token, inputs); len(f) > 0 {
				t.Errorf("%s %s: %v", token, name, f)
			}
		}
	}
}

func genericOIDC(extra map[string]property.Value) map[string]property.Value {
	cfg := map[string]property.Value{
		"issuer": str("https://issuer.example.com"), "clientId": str("app"),
		"clientSecret": str(canary), "redirectUri": str("https://dex.example.com/callback"),
	}
	if extra != nil {
		cfg["extra"] = obj(extra)
	}
	return map[string]property.Value{"connectorId": str("conn"), "type": str("oidc"), "name": str("Conn"), "oidcConfig": obj(cfg)}
}

func TestOidcConfigExtra_collisionsFailAtPreview(t *testing.T) {
	for _, key := range []string{"issuer", "clientID", "clientid", "clientSecret", "redirectURI", "scopes", "insecureSkipEmailVerified", "insecureIssuer", "userNameKey", "claimMapping"} {
		t.Run(key, func(t *testing.T) {
			failures := checkFailures(t, "dex:resources:Connector", genericOIDC(map[string]property.Value{key: str(canary + "-override")}))
			wantFailure(t, failures, "oidcConfig.extra", key)
			assertNoValueInFailures(t, failures)
		})
	}
	if f := checkFailures(t, "dex:resources:Connector", genericOIDC(map[string]property.Value{"promptType": str("consent")})); len(f) > 0 {
		t.Errorf("untyped extra key: %v", f)
	}
}

func TestGenericConnector_inputModeAtPreview(t *testing.T) {
	base := func(extra map[string]property.Value) map[string]property.Value {
		m := map[string]property.Value{"connectorId": str("conn"), "type": str("oidc"), "name": str("Conn")}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	oidc := genericOIDC(nil)["oidcConfig"]
	raw := str(`{"issuer":"https://issuer.example.com"}`)

	wantFailure(t, checkFailures(t, "dex:resources:Connector", base(map[string]property.Value{"oidcConfig": oidc, "rawConfig": raw})),
		"rawConfig", "exactly one of oidcConfig and rawConfig")
	wantFailure(t, checkFailures(t, "dex:resources:Connector", base(nil)), "rawConfig", "exactly one of oidcConfig and rawConfig")
	wantFailure(t, checkFailures(t, "dex:resources:Connector", map[string]property.Value{
		"connectorId": str("conn"), "type": str("ldap"), "name": str("Conn"), "oidcConfig": oidc,
	}), "oidcConfig", "type")
	failures := checkFailures(t, "dex:resources:Connector", base(map[string]property.Value{"rawConfig": str(`{"clientSecret":"` + canary + `",}`)}))
	wantFailure(t, failures, "rawConfig", "not valid JSON")
	assertNoValueInFailures(t, failures)

	for name, extra := range map[string]map[string]property.Value{
		"only oidcConfig":    {"oidcConfig": oidc},
		"only rawConfig":     {"rawConfig": raw},
		"unknown rawConfig":  {"rawConfig": property.New(property.Computed)},
		"unknown oidcConfig": {"oidcConfig": property.New(property.Computed)},
		"unknown both":       {"oidcConfig": property.New(property.Computed), "rawConfig": property.New(property.Computed)},
	} {
		if f := checkFailures(t, "dex:resources:Connector", base(extra)); len(f) > 0 {
			t.Errorf("%s: %v", name, f)
		}
	}
}

// Import rule (unchanged since 0.10.0): an imported generic oidc connector becomes
// oidcConfig when that is lossless, otherwise rawConfig with the stored config.
func TestGenericConnector_importRule(t *testing.T) {
	for _, tc := range []struct {
		name, config string
		wantOIDC     bool
	}{
		{"standard OIDC config", `{"issuer":"https://issuer.example.com","clientID":"app","clientSecret":"` + canary + `","redirectURI":"https://dex.example.com/callback","promptType":"consent"}`, true},
		{"known field with another type", `{"issuer":"https://issuer.example.com","scopes":"openid"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dex := newFakeDex()
			dex.connectors["conn"] = &api.Connector{Id: "conn", Type: "oidc", Name: "Conn", Config: []byte(tc.config)}
			resp, err := newProviderAgainst(t, dex).Read(p.ReadRequest{ID: "conn", Urn: testURN("dex:resources:Connector")})
			if err != nil || resp.ID != "conn" {
				t.Fatalf("import Read: id=%q err=%v", resp.ID, err)
			}
			_, hasOIDC := resp.Inputs.GetOk("oidcConfig")
			_, hasRaw := resp.Inputs.GetOk("rawConfig")
			if hasOIDC != tc.wantOIDC || hasRaw == tc.wantOIDC {
				t.Errorf("imported inputs: oidcConfig=%v rawConfig=%v, want oidcConfig=%v", hasOIDC, hasRaw, tc.wantOIDC)
			}
			assertNoPlainCanary(t, "imported inputs", property.New(resp.Inputs))
		})
	}
}
