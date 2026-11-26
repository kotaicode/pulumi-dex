package resources

import (
	"encoding/json"
	"strings"
	"testing"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

// =============================================================================
// AzureMicrosoftConnector group restriction
//
// Dex's Microsoft connector reads "groups" as a list of groups whose members may
// log in. It applies the list only for a specific tenant (ID or domain) and
// ignores it for common, organizations and consumers. Dex v2.46.1 fails to open
// a connector whose "groups" is a string, so the earlier string input never
// restricted anything: it broke login through the connector.
// =============================================================================

const microsoftToken = "dex:resources:AzureMicrosoftConnector"

func microsoftInputs(extra map[string]property.Value) map[string]property.Value {
	m := map[string]property.Value{
		"connectorId":  str("ms"),
		"name":         str("Microsoft"),
		"tenant":       str("00000000-0000-0000-0000-000000000001"),
		"clientId":     str("app"),
		"clientSecret": str(canary + "-ms"),
		"redirectUri":  str("https://dex.example.com/callback"),
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func checkMicrosoft(t *testing.T, inputs map[string]property.Value) []p.CheckFailure {
	t.Helper()
	resp, err := newTestProvider(t).Check(p.CheckRequest{Urn: testURN(microsoftToken), Inputs: property.NewMap(inputs)})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	return resp.Failures
}

func wantFailure(t *testing.T, failures []p.CheckFailure, property string, parts ...string) {
	t.Helper()
	for _, f := range failures {
		if f.Property != property {
			continue
		}
		for _, part := range parts {
			if !strings.Contains(f.Reason, part) {
				t.Errorf("failure on %s: %q should contain %q", property, f.Reason, part)
			}
		}
		return
	}
	t.Errorf("no check failure on %s, got %v", property, failures)
}

// storedMicrosoftConfig creates the connector against the fake Dex and returns
// the config Dex stored.
func storedMicrosoftConfig(t *testing.T, inputs map[string]property.Value) map[string]any {
	t.Helper()
	dex := newFakeDex()
	s := newProviderAgainst(t, dex)
	urn := testURN(microsoftToken)
	checked, err := s.Check(p.CheckRequest{Urn: urn, Inputs: property.NewMap(inputs)})
	if err != nil || len(checked.Failures) > 0 {
		t.Fatalf("Check: %v %v", err, checked.Failures)
	}
	created, err := s.Create(p.CreateRequest{Urn: urn, Properties: checked.Inputs})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	dex.mu.Lock()
	defer dex.mu.Unlock()
	var cfg map[string]any
	if err := json.Unmarshal(dex.connectors[created.ID].Config, &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestMicrosoftAllowedGroups_writtenAsList(t *testing.T) {
	cfg := storedMicrosoftConfig(t, microsoftInputs(map[string]property.Value{"allowedGroups": strs("admins", "devs")}))
	got, ok := cfg["groups"].([]any)
	if !ok || len(got) != 2 || got[0] != "admins" || got[1] != "devs" {
		t.Errorf("Dex groups = %#v, want [admins devs]", cfg["groups"])
	}

	cfg = storedMicrosoftConfig(t, microsoftInputs(nil))
	if _, ok := cfg["groups"]; ok {
		t.Errorf("without allowedGroups, Dex config must not contain groups: %#v", cfg["groups"])
	}
}

func TestMicrosoftLegacyGroups_failsWithMigration(t *testing.T) {
	failures := checkMicrosoft(t, microsoftInputs(map[string]property.Value{"groups": str("groups")}))
	wantFailure(t, failures, "groups", "allowedGroups", "failed to initialize")

	failures = checkMicrosoft(t, microsoftInputs(map[string]property.Value{
		"groups": str("groups"), "allowedGroups": strs("admins"),
	}))
	wantFailure(t, failures, "groups", "allowedGroups")
}

func TestMicrosoftAllowedGroups_rules(t *testing.T) {
	// An empty list is what Dex treats as no restriction; it must not look like "nobody".
	wantFailure(t, checkMicrosoft(t, microsoftInputs(map[string]property.Value{"allowedGroups": strs()})),
		"allowedGroups", "every user", "omit allowedGroups")

	for _, tenant := range []string{"common", "organizations", "consumers"} {
		failures := checkMicrosoft(t, microsoftInputs(map[string]property.Value{
			"tenant": str(tenant), "allowedGroups": strs("admins"),
		}))
		wantFailure(t, failures, "allowedGroups", "ignores", tenant)
	}

	for _, tenant := range []string{"00000000-0000-0000-0000-000000000001", "contoso.onmicrosoft.com", "example.com"} {
		if f := checkMicrosoft(t, microsoftInputs(map[string]property.Value{
			"tenant": str(tenant), "allowedGroups": strs("admins"),
		})); len(f) > 0 {
			t.Errorf("tenant %s with allowedGroups: %v", tenant, f)
		}
	}
}

// Dex documents common, organizations, consumers, a tenant ID or a tenant domain.
func TestMicrosoftTenant_acceptsDocumentedForms(t *testing.T) {
	for _, tenant := range []string{"common", "organizations", "consumers", "00000000-0000-0000-0000-000000000001", "contoso.onmicrosoft.com", "Contoso.COM"} {
		if f := checkMicrosoft(t, microsoftInputs(map[string]property.Value{"tenant": str(tenant)})); len(f) > 0 {
			t.Errorf("tenant %q rejected: %v", tenant, f)
		}
	}
	for _, tenant := range []string{"not a tenant", "contoso", "https://login.microsoftonline.com/contoso"} {
		wantFailure(t, checkMicrosoft(t, microsoftInputs(map[string]property.Value{"tenant": str(tenant)})), "tenant", "tenant ID")
	}
}

func TestMicrosoftAllowedGroups_unknownValuesInPreview(t *testing.T) {
	unknown := property.New(property.Computed)
	for name, extra := range map[string]map[string]property.Value{
		"unknown allowedGroups":           {"allowedGroups": unknown},
		"unknown tenant with groups":      {"tenant": unknown, "allowedGroups": strs("admins")},
		"unknown legacy groups":           {"groups": unknown},
		"unknown group in allowedGroups":  {"allowedGroups": property.New([]property.Value{str("admins"), unknown})},
		"unknown tenant without anything": {"tenant": unknown},
	} {
		if f := checkMicrosoft(t, microsoftInputs(extra)); len(f) > 0 {
			t.Errorf("%s: preview must not reject unknown values: %v", name, f)
		}
	}
}

func TestMicrosoftAllowedGroups_read(t *testing.T) {
	read := func(remoteGroups string) property.Map {
		t.Helper()
		remote := `{"clientID":"app","clientSecret":"` + canary + `","redirectURI":"https://dex.example.com/callback",` +
			`"tenant":"00000000-0000-0000-0000-000000000001"` + remoteGroups + `}`
		resp, err := readAfterRemoteConfig(t, microsoftToken, microsoftInputs(nil), remote)
		if err != nil || resp.ID == "" {
			t.Fatalf("Read: id=%q err=%v", resp.ID, err)
		}
		return resp.Inputs
	}

	// A restriction set in Dex must be read, never dropped: a later update would
	// otherwise remove it.
	got := read(`,"groups":["admins","devs"]`).Get("allowedGroups")
	if !got.IsArray() || got.AsArray().Len() != 2 {
		t.Errorf("allowedGroups after refresh = %v, want [admins devs]", got)
	}
	if in := read(`,"groups":[]`); in.Get("allowedGroups").IsArray() && in.Get("allowedGroups").AsArray().Len() > 0 {
		t.Errorf("empty Dex list read as a restriction: %v", in.Get("allowedGroups"))
	}
	// The string the provider wrote before is read back as the legacy input, so
	// refresh works and the next preview shows the migration error.
	legacy := read(`,"groups":"groups"`)
	if legacy.Get("groups").AsString() != "groups" || legacy.Get("allowedGroups").IsArray() {
		t.Errorf("legacy string groups read as %v / %v", legacy.Get("groups"), legacy.Get("allowedGroups"))
	}
}

func TestMicrosoftAllowedGroups_refreshIsClean(t *testing.T) {
	s := newProviderAgainst(t, newFakeDex())
	urn := testURN(microsoftToken)
	program := property.NewMap(microsoftInputs(map[string]property.Value{"allowedGroups": strs("admins", "devs")}))
	checked, err := s.Check(p.CheckRequest{Urn: urn, Inputs: program})
	if err != nil || len(checked.Failures) > 0 {
		t.Fatalf("Check: %v %v", err, checked.Failures)
	}
	created, err := s.Create(p.CreateRequest{Urn: urn, Properties: checked.Inputs})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	refreshed, err := s.Read(p.ReadRequest{ID: created.ID, Urn: urn, Properties: created.Properties, Inputs: checked.Inputs})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	assertNoPlainCanary(t, "refreshed inputs", property.New(refreshed.Inputs))
	diff, err := s.Diff(p.DiffRequest{ID: refreshed.ID, Urn: urn, State: refreshed.Inputs, OldInputs: refreshed.Inputs, Inputs: checked.Inputs})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if diff.HasChanges {
		t.Errorf("preview after refresh proposes changes: %v", diffKeys(diff))
	}
}
