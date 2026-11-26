package resources

import (
	"strings"
	"testing"

	api "github.com/dexidp/dex/api/v2"
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

// =============================================================================
// LocalConnector adds a password login
//
// Dex v2.46.1 resolves every connector of type "local" to its password
// database, whatever its ID and whether enablePasswordDB is set. A local
// connector therefore always adds a password login; there is no disabled
// state. With enablePasswordDB, the ID "local" belongs to Dex's built-in
// password login, which is defined in its configuration and read-only.
// =============================================================================

const localToken = "dex:resources:LocalConnector"

func checkLocal(t *testing.T, inputs map[string]property.Value) []p.CheckFailure {
	t.Helper()
	resp, err := newTestProvider(t).Check(p.CheckRequest{Urn: testURN(localToken), Inputs: property.NewMap(inputs)})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	return resp.Failures
}

func TestLocalConnector_enabledFalseFailsAtPreview(t *testing.T) {
	failures := checkLocal(t, map[string]property.Value{
		"connectorId": str("pw"), "name": str("Passwords"), "enabled": property.New(false),
	})
	wantFailure(t, failures, "enabled", "remove the resource to turn password login off")

	for name, extra := range map[string]map[string]property.Value{
		"enabled: true":   {"enabled": property.New(true)},
		"enabled unknown": {"enabled": property.New(property.Computed)},
		"enabled omitted": {},
	} {
		inputs := map[string]property.Value{"connectorId": str("pw"), "name": str("Passwords")}
		for k, v := range extra {
			inputs[k] = v
		}
		if f := checkLocal(t, inputs); len(f) > 0 {
			t.Errorf("%s: %v", name, f)
		}
	}
}

// Refresh reports what exists in Dex: an existing local connector is a working
// password login, so a stack recorded as enabled: false sees enabled: true and
// its next preview fails with the instruction to remove the resource.
func TestLocalConnector_refreshReportsPasswordLoginEnabled(t *testing.T) {
	dex := newFakeDex()
	dex.connectors["pw"] = &api.Connector{Id: "pw", Type: "local", Name: "Passwords", Config: []byte("{}")}
	s := newProviderAgainst(t, dex)
	urn := testURN(localToken)
	stale := property.NewMap(map[string]property.Value{
		"connectorId": str("pw"), "name": str("Passwords"), "enabled": property.New(false),
	})
	read, err := s.Read(p.ReadRequest{ID: "pw", Urn: urn, Properties: stale, Inputs: stale})
	if err != nil || read.ID != "pw" {
		t.Fatalf("Read: id=%q err=%v", read.ID, err)
	}
	if got := read.Inputs.Get("enabled"); !got.IsBool() || !got.AsBool() {
		t.Errorf("refreshed enabled = %v, want true", got)
	}
	checked, err := s.Check(p.CheckRequest{Urn: urn, State: read.Inputs, Inputs: stale})
	if err != nil {
		t.Fatal(err)
	}
	wantFailure(t, checked.Failures, "enabled", "remove the resource")
}

func TestLocalConnector_reservedIDFailsClearly(t *testing.T) {
	dex := newFakeDex()
	dex.staticConnectors = map[string]bool{"local": true}
	_, err := create(t, dex, adoptionCase{token: localToken, id: "local", connect: true,
		inputs: map[string]property.Value{"connectorId": str("local"), "name": str("Email")}})
	if err == nil {
		t.Fatal("Create of the built-in local connector succeeded")
	}
	for _, want := range []string{"enablePasswordDB", "built-in password login", "another connectorId"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should contain %q: %v", want, err)
		}
	}

	// Any other object defined in Dex's configuration file gets a clear error too.
	dex.staticConnectors = map[string]bool{"corp-ldap": true}
	_, err = create(t, dex, adoptionCase{token: "dex:resources:Connector", id: "corp-ldap", connect: true,
		inputs: map[string]property.Value{
			"connectorId": str("corp-ldap"), "type": str("ldap"), "name": str("LDAP"),
			"rawConfig": str(`{"host":"ldap.example.com:636"}`),
		}})
	if err == nil || !strings.Contains(err.Error(), "defined in Dex's configuration") {
		t.Errorf("static connector create: %v", err)
	}
}
