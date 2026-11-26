package resources

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	api "github.com/dexidp/dex/api/v2"
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// =============================================================================
// Explicit adoption (DEX-004)
//
// Create must never take over an object that already exists in Dex: an equal ID
// or even an identical configuration is not proof of ownership. It returns a
// conflict that recommends `pulumi import` with the resource's schema token and
// leaves the existing object untouched. Import itself is read-only.
// =============================================================================

const existingCanary = "CANARY-EXISTING-OBJECT"

type adoptionCase struct {
	token   string
	id      string
	inputs  map[string]property.Value
	connect bool // a connector (vs a client)
}

func adoptionCases() []adoptionCase {
	oauth := func(extra map[string]property.Value) map[string]property.Value {
		m := map[string]property.Value{
			"connectorId": str("shared"), "name": str("Ours"), "clientId": str("ours"),
			"clientSecret": str("our-secret"), "redirectUri": str("https://dex.example.com/callback"),
		}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	return []adoptionCase{
		{"dex:resources:Client", "shared", map[string]property.Value{
			"clientId": str("shared"), "name": str("Ours"), "secret": str("our-secret"),
			"redirectUris": strs("https://ours.example.com/callback"),
		}, false},
		{"dex:resources:Connector", "shared", map[string]property.Value{
			"connectorId": str("shared"), "type": str("oidc"), "name": str("Ours"),
			"rawConfig": str(`{"issuer":"https://issuer.example.com","clientSecret":"our-secret"}`),
		}, true},
		{"dex:resources:AzureOidcConnector", "shared", oauth(map[string]property.Value{"tenantId": str("00000000-0000-0000-0000-000000000000")}), true},
		{"dex:resources:AzureMicrosoftConnector", "shared", oauth(map[string]property.Value{"tenant": str("organizations")}), true},
		{"dex:resources:CognitoOidcConnector", "shared", oauth(map[string]property.Value{"region": str("eu-central-1"), "userPoolId": str("eu-central-1_pool")}), true},
		{"dex:resources:GitHubConnector", "shared", oauth(nil), true},
		{"dex:resources:GitLabConnector", "shared", oauth(nil), true},
		{"dex:resources:GoogleConnector", "shared", oauth(nil), true},
		{"dex:resources:LocalConnector", "shared", map[string]property.Value{"connectorId": str("shared"), "name": str("Ours")}, true},
	}
}

// seedForeign stores an object that another owner created with the same ID.
func seedForeign(dex *fakeDex, tc adoptionCase) {
	dex.mu.Lock()
	defer dex.mu.Unlock()
	if tc.connect {
		dex.connectors[tc.id] = &api.Connector{Id: tc.id, Type: "ldap", Name: "Theirs",
			Config: []byte(`{"host":"ldap.example.com:636","bindPW":"` + existingCanary + `"}`)}
		return
	}
	dex.clients[tc.id] = &api.Client{Id: tc.id, Name: "Theirs", Secret: existingCanary,
		RedirectUris: []string{"https://theirs.example.com/callback"}}
}

// snapshot captures the stored object so tests can prove it was left untouched.
func snapshot(t *testing.T, dex *fakeDex, tc adoptionCase) string {
	t.Helper()
	dex.mu.Lock()
	defer dex.mu.Unlock()
	var v any = dex.clients[tc.id]
	if tc.connect {
		v = dex.connectors[tc.id]
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func create(t *testing.T, dex *fakeDex, tc adoptionCase) (p.CreateResponse, error) {
	t.Helper()
	s := newProviderAgainst(t, dex)
	urn := testURN(tc.token)
	checked, err := s.Check(p.CheckRequest{Urn: urn, Inputs: property.NewMap(tc.inputs)})
	if err != nil || len(checked.Failures) > 0 {
		t.Fatalf("Check: %v %v", err, checked.Failures)
	}
	return s.Create(p.CreateRequest{Urn: urn, Properties: checked.Inputs})
}

func assertImportHint(t *testing.T, err error, tc adoptionCase) {
	t.Helper()
	if err == nil {
		t.Fatal("Create succeeded on an existing ID; it must not take over the existing object")
	}
	// testURN names every resource "r".
	if want := "pulumi import " + tc.token + " r " + tc.id; !strings.Contains(err.Error(), want) {
		t.Errorf("error should recommend %q, got: %v", want, err)
	}
	if strings.Contains(err.Error(), existingCanary) {
		t.Errorf("error reveals the existing object's configuration: %v", err)
	}
}

func TestCreate_existingIDIsConflict(t *testing.T) {
	for _, tc := range adoptionCases() {
		for _, asStatus := range []bool{false, true} {
			signal := "response flag"
			if asStatus {
				signal = "gRPC AlreadyExists"
			}
			t.Run(tc.token+"/unrelated object/"+signal, func(t *testing.T) {
				dex := newFakeDex()
				dex.alreadyExistsStatus = asStatus
				seedForeign(dex, tc)
				before := snapshot(t, dex, tc)

				_, err := create(t, dex, tc)
				assertImportHint(t, err, tc)
				if after := snapshot(t, dex, tc); after != before {
					t.Errorf("existing object was modified:\nbefore %s\nafter  %s", before, after)
				}
				if dex.writes != 1 {
					t.Errorf("writes = %d, want only the single rejected Create (no retries, no update or delete)", dex.writes)
				}
			})
			t.Run(tc.token+"/identical object/"+signal, func(t *testing.T) {
				dex := newFakeDex()
				if _, err := create(t, dex, tc); err != nil {
					t.Fatalf("first Create: %v", err)
				}
				dex.alreadyExistsStatus = asStatus
				before := snapshot(t, dex, tc)

				_, err := create(t, dex, tc) // same declaration, e.g. from a second stack
				assertImportHint(t, err, tc)
				if after := snapshot(t, dex, tc); after != before {
					t.Errorf("existing object was modified:\nbefore %s\nafter  %s", before, after)
				}
			})
		}
	}
}

// A Create whose response is lost after Dex committed it has an unknown outcome:
// report that, point to inspection and import, and do not retry or delete.
func TestCreate_uncertainOutcomeIsReported(t *testing.T) {
	for _, tc := range adoptionCases() {
		for _, code := range []codes.Code{codes.DeadlineExceeded, codes.Canceled, codes.Unavailable} {
			t.Run(tc.token+"/"+code.String(), func(t *testing.T) {
				dex := newFakeDex()
				dex.createErr = status.Error(code, "response lost")
				_, err := create(t, dex, tc)
				if err == nil {
					t.Fatal("Create reported success")
				}
				for _, want := range []string{"may have been created", "pulumi import " + tc.token + " r " + tc.id, code.String()} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("error should contain %q, got: %v", want, err)
					}
				}
				if dex.writes != 1 {
					t.Errorf("writes = %d, want exactly one Create attempt (no automatic retry)", dex.writes)
				}
				if snapshot(t, dex, tc) == "null" {
					t.Error("the committed object must stay in Dex")
				}
			})
		}
	}
}

// Other Create failures are definite and are reported without the uncertain or
// conflict guidance.
func TestCreate_definiteFailureIsNotUncertain(t *testing.T) {
	tc := adoptionCases()[0]
	dex := newFakeDex()
	dex.createErr = status.Error(codes.InvalidArgument, "invalid redirect URI")
	_, err := create(t, dex, tc)
	if err == nil || strings.Contains(err.Error(), "may have been created") || strings.Contains(err.Error(), "pulumi import") {
		t.Errorf("unexpected error: %v", err)
	}
}

// Import is a Read with no prior state or inputs; it must not write to Dex.
func TestImport_isReadOnly(t *testing.T) {
	for _, tc := range adoptionCases() {
		t.Run(tc.token, func(t *testing.T) {
			dex := newFakeDex()
			if _, err := create(t, dex, tc); err != nil {
				t.Fatalf("seed Create: %v", err)
			}
			before := snapshot(t, dex, tc)
			dex.writes = 0

			s := newProviderAgainst(t, dex)
			resp, err := s.Read(p.ReadRequest{ID: tc.id, Urn: testURN(tc.token)})
			if err != nil || resp.ID != tc.id {
				t.Fatalf("import Read: id=%q err=%v", resp.ID, err)
			}
			if dex.writes != 0 {
				t.Errorf("import performed %d write RPCs", dex.writes)
			}
			if after := snapshot(t, dex, tc); after != before {
				t.Error("import modified the object")
			}
		})
	}
}

// Importing a connector as the wrong typed resource must fail instead of
// producing state that a later update would use to rewrite the connector.
func TestImport_rejectsConnectorOfAnotherType(t *testing.T) {
	for _, tc := range adoptionCases() {
		if !tc.connect || tc.token == "dex:resources:Connector" {
			continue // the generic connector represents any type
		}
		t.Run(tc.token, func(t *testing.T) {
			dex := newFakeDex()
			seedForeign(dex, tc) // an "ldap" connector
			s := newProviderAgainst(t, dex)
			_, err := s.Read(p.ReadRequest{ID: tc.id, Urn: testURN(tc.token)})
			if err == nil {
				t.Fatal("Read accepted a connector of another type")
			}
			for _, want := range []string{`"ldap"`, "dex:resources:Connector"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error should contain %q, got: %v", want, err)
				}
			}
			if strings.Contains(err.Error(), existingCanary) {
				t.Errorf("error reveals configuration: %v", err)
			}
			if dex.writes != 0 {
				t.Errorf("Read performed %d writes", dex.writes)
			}
		})
	}
}

// The import hints must use tokens that exist in the generated schema.
func TestAdoptionHintsUseSchemaTokens(t *testing.T) {
	resp, err := newTestProvider(t).GetSchema(p.GetSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Resources map[string]json.RawMessage `json:"resources"`
	}
	if err := json.Unmarshal([]byte(resp.Schema), &schema); err != nil {
		t.Fatal(err)
	}
	var tokens []string
	for _, tc := range adoptionCases() {
		tokens = append(tokens, tc.token)
		if _, ok := schema.Resources[tc.token]; !ok {
			t.Errorf("token %s is not in the schema", tc.token)
		}
	}
	for token := range schema.Resources {
		if !slices.Contains(tokens, token) {
			t.Errorf("schema resource %s has no adoption test case", token)
		}
	}
}
