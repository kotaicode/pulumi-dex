package resources

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

// =============================================================================
// Changes Dex cannot apply in place
//
// Dex v2.46.1's UpdateClient cannot change a client's secret, public flag or
// ID; UpdateConnector cannot change a connector's ID but replaces type, name
// and the whole config. Changes Dex cannot apply must fail at preview or become
// a replacement, never be recorded as applied.
// =============================================================================

func createFor(t *testing.T, dex *fakeDex, token string, inputs map[string]property.Value) (srv interface {
	Check(p.CheckRequest) (p.CheckResponse, error)
	Diff(p.DiffRequest) (p.DiffResponse, error)
	Update(p.UpdateRequest) (p.UpdateResponse, error)
}, created p.CreateResponse, old property.Map) {
	t.Helper()
	s := newProviderAgainst(t, dex)
	urn := testURN(token)
	checked, err := s.Check(p.CheckRequest{Urn: urn, Inputs: property.NewMap(inputs)})
	if err != nil || len(checked.Failures) > 0 {
		t.Fatalf("Check: %v %v", err, checked.Failures)
	}
	created, err = s.Create(p.CreateRequest{Urn: urn, Properties: checked.Inputs})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return s, created, checked.Inputs
}

// changeTo creates the resource with the from inputs, then checks, diffs and
// updates it to the to inputs; dryRun makes the update a preview. A check
// failure is returned as the error.
func changeTo(t *testing.T, dex *fakeDex, token string, from, to map[string]property.Value, dryRun bool) (p.DiffResponse, error) {
	t.Helper()
	s, created, old := createFor(t, dex, token, from)
	urn := testURN(token)
	checked, err := s.Check(p.CheckRequest{Urn: urn, State: old, Inputs: property.NewMap(to)})
	if err != nil {
		t.Fatalf("re-Check: %v", err)
	}
	if len(checked.Failures) > 0 {
		// A check failure stops the preview just like an Update error.
		reasons := make([]string, 0, len(checked.Failures))
		for _, f := range checked.Failures {
			reasons = append(reasons, f.Property+": "+f.Reason)
		}
		return p.DiffResponse{}, errors.New(strings.Join(reasons, "; "))
	}
	diff, err := s.Diff(p.DiffRequest{ID: created.ID, Urn: urn, State: created.Properties, OldInputs: old, Inputs: checked.Inputs})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	_, err = s.Update(p.UpdateRequest{ID: created.ID, Urn: urn, State: created.Properties, OldInputs: old, Inputs: checked.Inputs, DryRun: dryRun})
	return diff, err
}

func with(base map[string]property.Value, extra map[string]property.Value) map[string]property.Value {
	m := make(map[string]property.Value, len(base)+len(extra))
	for k, v := range base {
		m[k] = v
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func clientBase() map[string]property.Value {
	return map[string]property.Value{
		"clientId": str("app"), "name": str("App"),
		"redirectUris": strs("https://app.example.com/cb"),
	}
}

func TestClientSecretChange_failsAtPreview(t *testing.T) {
	for name, tc := range map[string]struct{ from, to map[string]property.Value }{
		"explicit secret changed": {with(clientBase(), map[string]property.Value{"secret": str("first-secret")}),
			with(clientBase(), map[string]property.Value{"secret": str("second-secret")})},
		"generated secret replaced by an explicit one": {clientBase(),
			with(clientBase(), map[string]property.Value{"secret": str("explicit-secret")})},
	} {
		t.Run(name, func(t *testing.T) {
			dex := newFakeDex()
			_, err := changeTo(t, dex, "dex:resources:Client", tc.from, tc.to, true)
			if err == nil {
				t.Fatal("preview accepted a secret change that Dex cannot apply")
			}
			for _, want := range []string{"secret", "cannot change", "new client ID"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error should contain %q: %v", want, err)
				}
			}
			if strings.Contains(err.Error(), "first-secret") || strings.Contains(err.Error(), "second-secret") || strings.Contains(err.Error(), "explicit-secret") {
				t.Errorf("error reveals a secret: %v", err)
			}
		})
	}
}

func TestClientSecret_unchangedCasesPass(t *testing.T) {
	for name, tc := range map[string]struct{ from, to map[string]property.Value }{
		"generated secret, still omitted": {clientBase(), with(clientBase(), map[string]property.Value{"name": str("Renamed")})},
		"explicit secret, unchanged": {with(clientBase(), map[string]property.Value{"secret": str("same-secret")}),
			with(clientBase(), map[string]property.Value{"secret": str("same-secret"), "name": str("Renamed")})},
		"explicit secret removed from the program": {with(clientBase(), map[string]property.Value{"secret": str("kept-secret")}),
			clientBase()},
		"unknown secret in preview": {with(clientBase(), map[string]property.Value{"secret": str("first-secret")}),
			with(clientBase(), map[string]property.Value{"secret": property.New(property.Computed)})},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := changeTo(t, newFakeDex(), "dex:resources:Client", tc.from, tc.to, true); err != nil {
				t.Errorf("preview: %v", err)
			}
		})
	}
}

func TestClientPublicChange_failsAtPreview(t *testing.T) {
	for name, tc := range map[string]struct{ from, to *bool }{
		"confidential to public":         {nil, ptrBool(true)},
		"explicitly false to public":     {ptrBool(false), ptrBool(true)},
		"public to confidential":         {ptrBool(true), ptrBool(false)},
		"public to omitted (not public)": {ptrBool(true), nil},
	} {
		t.Run(name, func(t *testing.T) {
			from, to := clientBase(), clientBase()
			if tc.from != nil {
				from["public"] = property.New(*tc.from)
			}
			if tc.to != nil {
				to["public"] = property.New(*tc.to)
			}
			_, err := changeTo(t, newFakeDex(), "dex:resources:Client", from, to, true)
			if err == nil || !strings.Contains(err.Error(), "public") || !strings.Contains(err.Error(), "new client ID") {
				t.Errorf("preview error = %v, want a public-cannot-change error with guidance", err)
			}
		})
	}
	// A value only known after other resources are created is not judged.
	if _, err := changeTo(t, newFakeDex(), "dex:resources:Client", with(clientBase(), map[string]property.Value{"public": property.New(true)}),
		with(clientBase(), map[string]property.Value{"public": property.New(property.Computed)}), true); err != nil {
		t.Errorf("unknown public in preview: %v", err)
	}
	// Omitted and false both mean not public: no change for Dex.
	if _, err := changeTo(t, newFakeDex(), "dex:resources:Client", clientBase(),
		with(clientBase(), map[string]property.Value{"public": property.New(false)}), true); err != nil {
		t.Errorf("omitted to false: %v", err)
	}
}

func ptrBool(b bool) *bool { return &b }

// Changing the Dex ID is a replacement: create the new object, then delete
// the old one. The IDs differ, so creating first cannot collide.
func TestIDChange_isReplacement(t *testing.T) {
	for _, tc := range adoptionCases() {
		if tc.user {
			continue // Dex cannot rename a password user; see TestLocalUser_identityChangesFailAtPreview
		}
		t.Run(tc.token, func(t *testing.T) {
			key := "connectorId"
			if !tc.connect {
				key = "clientId"
			}
			diff, err := changeTo(t, newFakeDex(), tc.token, tc.inputs, with(tc.inputs, map[string]property.Value{key: str("renamed-id")}), true)
			if err != nil {
				t.Fatalf("preview: %v", err)
			}
			d, ok := diff.DetailedDiff[key]
			if !ok || d.Kind != p.UpdateReplace {
				t.Errorf("diff for %s = %+v, want %s", key, diff.DetailedDiff, p.UpdateReplace)
			}
			if diff.DeleteBeforeReplace {
				t.Error("replacement must create the new object before deleting the old one")
			}
		})
	}
}

// Settings stored in a connector's config can change in place: Dex replaces
// the whole config.
func TestConnectorConfigFields_updateInPlace(t *testing.T) {
	oauth := func(extra map[string]property.Value) map[string]property.Value {
		return with(map[string]property.Value{
			"connectorId": str("conn"), "name": str("Conn"), "clientId": str("app"),
			"clientSecret": str(canary), "redirectUri": str("https://dex.example.com/callback"),
		}, extra)
	}
	for _, tc := range []struct {
		token    string
		from, to map[string]property.Value
		key      string // Dex config key that must carry the new value
		want     string
	}{
		{"dex:resources:AzureOidcConnector", oauth(map[string]property.Value{"tenantId": str("00000000-0000-0000-0000-000000000001")}),
			oauth(map[string]property.Value{"tenantId": str("00000000-0000-0000-0000-000000000002")}), "issuer", "00000000-0000-0000-0000-000000000002"},
		{"dex:resources:AzureMicrosoftConnector", oauth(map[string]property.Value{"tenant": str("organizations")}),
			oauth(map[string]property.Value{"tenant": str("contoso.onmicrosoft.com")}), "tenant", "contoso.onmicrosoft.com"},
		{"dex:resources:CognitoOidcConnector", oauth(map[string]property.Value{"region": str("eu-central-1"), "userPoolId": str("eu-central-1_old")}),
			oauth(map[string]property.Value{"region": str("eu-west-1"), "userPoolId": str("eu-west-1_new")}), "issuer", "eu-west-1_new"},
		{"dex:resources:GitHubConnector", oauth(map[string]property.Value{"hostName": str("github.example.com")}),
			oauth(map[string]property.Value{"hostName": str("github.other.example.com")}), "hostName", "github.other.example.com"},
		{"dex:resources:GitLabConnector", oauth(map[string]property.Value{"baseURL": str("https://gitlab.example.com")}),
			oauth(map[string]property.Value{"baseURL": str("https://gitlab.other.example.com")}), "baseURL", "https://gitlab.other.example.com"},
		{"dex:resources:Connector",
			map[string]property.Value{"connectorId": str("conn"), "type": str("oidc"), "name": str("Conn"), "rawConfig": str(`{"issuer":"https://a.example.com"}`)},
			map[string]property.Value{"connectorId": str("conn"), "type": str("ldap"), "name": str("Conn"), "rawConfig": str(`{"host":"ldap.example.com:636"}`)},
			"host", "ldap.example.com:636"},
	} {
		t.Run(tc.token, func(t *testing.T) {
			dex := newFakeDex()
			diff, err := changeTo(t, dex, tc.token, tc.from, tc.to, false)
			if err != nil {
				t.Fatalf("update: %v", err)
			}
			for k, d := range diff.DetailedDiff {
				if d.Kind == p.UpdateReplace || d.Kind == p.AddReplace || d.Kind == p.DeleteReplace {
					t.Errorf("%s requires replacement, want an in-place update", k)
				}
			}
			dex.mu.Lock()
			stored := dex.connectors["conn"]
			dex.mu.Unlock()
			var cfg map[string]any
			if err := json.Unmarshal(stored.Config, &cfg); err != nil {
				t.Fatal(err)
			}
			if got, ok := cfg[tc.key].(string); !ok || !strings.Contains(got, tc.want) {
				t.Errorf("Dex config %s = %q, want it to contain %q", tc.key, got, tc.want)
			}
			if tc.token == "dex:resources:Connector" && stored.Type != "ldap" {
				t.Errorf("Dex connector type = %q, want ldap", stored.Type)
			}
		})
	}
}
