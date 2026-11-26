package resources

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	api "github.com/dexidp/dex/api/v2"
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

// =============================================================================
// Client Create → refresh → preview round trip
//
// Replays the engine's sequence through the provider protocol against the fake
// Dex: Check(program) → Create → Read(prior state and inputs) = refresh →
// Check(program, olds = refreshed inputs) → Diff = the next preview.
// =============================================================================

const clientCanary = "CANARY-CLIENT-ROUNDTRIP"

func clientProgram(extra map[string]property.Value) property.Map {
	m := map[string]property.Value{
		"clientId":     property.New("app"),
		"name":         property.New("App"),
		"secret":       property.New(clientCanary),
		"redirectUris": property.New([]property.Value{property.New("https://app.example.com/callback")}),
	}
	for k, v := range extra {
		m[k] = v
	}
	return property.NewMap(m)
}

// clientRefreshDiff creates the client, optionally edits it directly in Dex,
// refreshes, and returns the diff the next preview would show.
func clientRefreshDiff(t *testing.T, program property.Map, remoteEdit func(*api.Client)) p.DiffResponse {
	t.Helper()
	dex := newFakeDex()
	s := newProviderAgainst(t, dex)
	urn := testURN("dex:resources:Client")

	checked, err := s.Check(p.CheckRequest{Urn: urn, Inputs: program})
	if err != nil || len(checked.Failures) > 0 {
		t.Fatalf("Check: %v %v", err, checked.Failures)
	}
	created, err := s.Create(p.CreateRequest{Urn: urn, Properties: checked.Inputs})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if remoteEdit != nil {
		dex.mu.Lock()
		remoteEdit(dex.clients[created.ID])
		dex.mu.Unlock()
	}
	refreshed, err := s.Read(p.ReadRequest{ID: created.ID, Urn: urn, Properties: created.Properties, Inputs: checked.Inputs})
	if err != nil || refreshed.ID == "" {
		t.Fatalf("Read: id=%q err=%v", refreshed.ID, err)
	}
	assertClientSecretProtected(t, "refreshed inputs", refreshed.Inputs)
	assertClientSecretProtected(t, "refreshed state", refreshed.Properties)

	rechecked, err := s.Check(p.CheckRequest{Urn: urn, State: refreshed.Inputs, Inputs: program})
	if err != nil || len(rechecked.Failures) > 0 {
		t.Fatalf("re-Check: %v %v", err, rechecked.Failures)
	}
	// Current engines (SupportsOldInputs) make infer diff new inputs against the
	// refreshed inputs; the integration harness does not set that flag, so pass
	// the refreshed inputs as the base explicitly.
	diff, err := s.Diff(p.DiffRequest{
		ID: refreshed.ID, Urn: urn, State: refreshed.Inputs, OldInputs: refreshed.Inputs, Inputs: rechecked.Inputs,
	})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	return diff
}

func diffKeys(d p.DiffResponse) string {
	keys := make([]string, 0, len(d.DetailedDiff))
	for k, v := range d.DetailedDiff {
		keys = append(keys, fmt.Sprintf("%s(%v)", k, v.Kind))
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

// assertClientSecretProtected fails if the canary secret appears outside a secret.
func assertClientSecretProtected(t *testing.T, where string, m property.Map) {
	t.Helper()
	for k, v := range m.All() {
		if !v.IsSecret() && strings.Contains(fmt.Sprint(v), clientCanary) {
			t.Errorf("%s: client secret in plaintext at %s", where, k)
		}
	}
}

func TestClientRefreshRoundTrip_isClean(t *testing.T) {
	peers := property.New([]property.Value{property.New("peer")})
	cases := map[string]property.Map{
		"public omitted":     clientProgram(nil),
		"public false":       clientProgram(map[string]property.Value{"public": property.New(false)}),
		"public true":        clientProgram(map[string]property.Value{"public": property.New(true)}),
		"empty trustedPeers": clientProgram(map[string]property.Value{"trustedPeers": property.New([]property.Value{})}),
		"empty logoUrl":      clientProgram(map[string]property.Value{"logoUrl": property.New("")}),
		"all optional fields set": clientProgram(map[string]property.Value{
			"public": property.New(false), "trustedPeers": peers, "logoUrl": property.New("https://app.example.com/logo.png"),
		}),
	}
	for name, program := range cases {
		t.Run(name, func(t *testing.T) {
			if d := clientRefreshDiff(t, program, nil); d.HasChanges {
				t.Errorf("preview after refresh proposes changes: %s", diffKeys(d))
			}
		})
	}
}

// The read-back fix must not mask real drift made directly in Dex.
func TestClientRefreshRoundTrip_reportsRemoteChanges(t *testing.T) {
	cases := []struct {
		name    string
		program property.Map
		edit    func(*api.Client)
		wantKey string
	}{
		{"made public in Dex", clientProgram(nil), func(c *api.Client) { c.Public = true }, "public"},
		{"made confidential in Dex", clientProgram(map[string]property.Value{"public": property.New(true)}),
			func(c *api.Client) { c.Public = false }, "public"},
		{"peer added in Dex", clientProgram(nil), func(c *api.Client) { c.TrustedPeers = []string{"intruder"} }, "trustedPeers"},
		{"logo set in Dex", clientProgram(nil), func(c *api.Client) { c.LogoUrl = "https://evil.example.com/x.png" }, "logoUrl"},
		{"logo removed in Dex", clientProgram(map[string]property.Value{"logoUrl": property.New("https://app.example.com/logo.png")}),
			func(c *api.Client) { c.LogoUrl = "" }, "logoUrl"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := clientRefreshDiff(t, tc.program, tc.edit)
			if _, ok := d.DetailedDiff[tc.wantKey]; !d.HasChanges || !ok {
				t.Errorf("remote change to %s was masked; diff = %s", tc.wantKey, diffKeys(d))
			}
		})
	}
}
