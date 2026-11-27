//go:build dexlive

package resources

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/integration"
	"github.com/pulumi/pulumi/sdk/v3/go/property"

	"github.com/kotaicode/pulumi-dex/pkg/provider"
)

// =============================================================================
// Lifecycle suite against a real Dex server
//
// Run with: DEX_LIVE_HOST=127.0.0.1:5557 go test ./pkg/... -tags=dexlive -run Live
// The server must serve its gRPC API without TLS and enable connector CRUD
// (DEX_API_CONNECTORS_CRUD=true). CI runs this against each supported Dex line.
// =============================================================================

// liveProvider configures the provider against the Dex in DEX_LIVE_HOST.
func liveProvider(t *testing.T) integration.Server {
	t.Helper()
	host := os.Getenv("DEX_LIVE_HOST")
	if host == "" {
		t.Fatal("DEX_LIVE_HOST is not set; the dexlive suite needs a running Dex")
	}
	s := newTestProvider(t)
	if err := s.Configure(p.ConfigureRequest{Args: property.NewMap(map[string]property.Value{
		"host": str(host), "plaintext": property.New(true), "timeoutSeconds": property.New(10.0),
	})}); err != nil {
		t.Fatalf("configure against %s: %v", host, err)
	}
	return s
}

// liveID returns an ID unique to this run, so reruns against the same server
// cannot collide.
func liveID(name string) string {
	return fmt.Sprintf("live-%s-%d", name, time.Now().UnixNano())
}

// lifecycle runs create, refresh, a clean preview, an update, a refresh that
// shows the update, delete, and a refresh that finds nothing.
func lifecycle(t *testing.T, token, id string, program, updated map[string]property.Value, check func(t *testing.T, inputs property.Map)) {
	t.Helper()
	s := liveProvider(t)
	urn := testURN(token)

	checked, err := s.Check(p.CheckRequest{Urn: urn, Inputs: property.NewMap(program)})
	if err != nil || len(checked.Failures) > 0 {
		t.Fatalf("Check: %v %v", err, checked.Failures)
	}
	created, err := s.Create(p.CreateRequest{Urn: urn, Properties: checked.Inputs})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	deleted := false
	t.Cleanup(func() {
		if !deleted {
			if err := s.Delete(p.DeleteRequest{ID: created.ID, Urn: urn, Properties: created.Properties}); err != nil {
				t.Logf("cleanup: delete %s: %v", created.ID, err)
			}
		}
	})
	if created.ID != id {
		t.Fatalf("Create returned ID %q, want %q", created.ID, id)
	}

	refreshed, err := s.Read(p.ReadRequest{ID: created.ID, Urn: urn, Properties: created.Properties, Inputs: checked.Inputs})
	if err != nil || refreshed.ID != id {
		t.Fatalf("refresh: id=%q err=%v", refreshed.ID, err)
	}
	rechecked, err := s.Check(p.CheckRequest{Urn: urn, State: refreshed.Inputs, Inputs: property.NewMap(program)})
	if err != nil || len(rechecked.Failures) > 0 {
		t.Fatalf("re-Check: %v %v", err, rechecked.Failures)
	}
	diff, err := s.Diff(p.DiffRequest{ID: id, Urn: urn, State: refreshed.Inputs, OldInputs: refreshed.Inputs, Inputs: rechecked.Inputs})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if diff.HasChanges {
		t.Errorf("preview after refresh proposes changes: %s", diffKeys(diff))
	}

	next, err := s.Check(p.CheckRequest{Urn: urn, State: refreshed.Inputs, Inputs: property.NewMap(updated)})
	if err != nil || len(next.Failures) > 0 {
		t.Fatalf("Check update: %v %v", err, next.Failures)
	}
	diff, err = s.Diff(p.DiffRequest{ID: id, Urn: urn, State: refreshed.Properties, OldInputs: refreshed.Inputs, Inputs: next.Inputs})
	if err != nil || !diff.HasChanges || diff.DeleteBeforeReplace {
		t.Fatalf("Diff update: changes=%v err=%v", diff.HasChanges, err)
	}
	for k, d := range diff.DetailedDiff {
		if d.Kind == p.UpdateReplace || d.Kind == p.AddReplace || d.Kind == p.DeleteReplace {
			t.Fatalf("update of %s would replace the object", k)
		}
	}
	upd, err := s.Update(p.UpdateRequest{ID: id, Urn: urn, State: refreshed.Properties, OldInputs: refreshed.Inputs, Inputs: next.Inputs})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	after, err := s.Read(p.ReadRequest{ID: id, Urn: urn, Properties: upd.Properties, Inputs: next.Inputs})
	if err != nil || after.ID != id {
		t.Fatalf("refresh after update: id=%q err=%v", after.ID, err)
	}
	check(t, after.Inputs)

	if err := s.Delete(p.DeleteRequest{ID: id, Urn: urn, Properties: upd.Properties}); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	deleted = true
	gone, err := s.Read(p.ReadRequest{ID: id, Urn: urn, Properties: upd.Properties, Inputs: next.Inputs})
	if err != nil || gone.ID != "" {
		t.Errorf("refresh after delete: id=%q err=%v, want the resource to be gone", gone.ID, err)
	}
}

func wantString(t *testing.T, inputs property.Map, key, want string) {
	t.Helper()
	if got := inputs.Get(key); !got.IsString() || got.AsString() != want {
		t.Errorf("%s after update = %v, want %q", key, got, want)
	}
}

func TestLive_Client(t *testing.T) {
	id := liveID("client")
	// An explicit secret: with a generated one, refresh currently reports the
	// secret as an input and the next preview proposes removing it, on every
	// Dex line (a known issue, tracked separately).
	program := map[string]property.Value{
		"clientId": str(id), "name": str("Live client"), "secret": str("live-synthetic-client-secret"),
		"redirectUris": strs("https://app.example.com/cb"),
	}
	updated := with(program, map[string]property.Value{
		"name":         str("Live client renamed"),
		"redirectUris": strs("https://app.example.com/cb", "https://app.example.com/cb2"),
	})
	lifecycle(t, "dex:resources:Client", id, program, updated, func(t *testing.T, in property.Map) {
		wantString(t, in, "name", "Live client renamed")
		if got := in.Get("redirectUris"); !got.IsArray() || got.AsArray().Len() != 2 {
			t.Errorf("redirectUris after update = %v, want two URIs", got)
		}
	})
}

func TestLive_GitHubConnector(t *testing.T) {
	id := liveID("github")
	program := map[string]property.Value{
		"connectorId": str(id), "name": str("Live GitHub"), "clientId": str("live-app"),
		"clientSecret": str("live-synthetic-secret"), "redirectUri": str("http://127.0.0.1:5556/dex/callback"),
	}
	updated := with(program, map[string]property.Value{"name": str("Live GitHub renamed")})
	lifecycle(t, "dex:resources:GitHubConnector", id, program, updated, func(t *testing.T, in property.Map) {
		wantString(t, in, "name", "Live GitHub renamed")
	})
}

func TestLive_GenericConnector(t *testing.T) {
	id := liveID("ldap")
	program := map[string]property.Value{
		"connectorId": str(id), "type": str("ldap"), "name": str("Live LDAP"),
		"rawConfig": str(`{"host":"ldap.example.com:636","bindPW":"live-synthetic-secret"}`),
	}
	updated := with(program, map[string]property.Value{
		"rawConfig": str(`{"host":"ldap2.example.com:636","bindPW":"live-synthetic-secret"}`),
	})
	lifecycle(t, "dex:resources:Connector", id, program, updated, func(t *testing.T, in property.Map) {
		if got := in.Get("rawConfig"); !got.IsString() || !strings.Contains(got.AsString(), "ldap2.example.com") {
			t.Errorf("rawConfig after update does not contain the new host")
		}
	})
}

// TestLive_ServerVersion checks what the server reports and the feature gate.
// DEX_LIVE_VERSION names the server's release, for example v2.45.1.
func TestLive_ServerVersion(t *testing.T) {
	want := os.Getenv("DEX_LIVE_VERSION")
	if want == "" {
		t.Skip("DEX_LIVE_VERSION is not set")
	}
	c := &provider.DexConfig{Host: os.Getenv("DEX_LIVE_HOST"), Plaintext: ptrBool(true)}
	if err := c.Configure(t.Context()); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if c.Server == nil || c.Server.Err != nil || c.Server.Server != want {
		t.Fatalf("server info = %+v, want server %q", c.Server, want)
	}
	minor, err := strconv.Atoi(strings.Split(strings.TrimPrefix(want, "v"), ".")[1])
	if err != nil {
		t.Fatalf("DEX_LIVE_VERSION %q is not vMAJOR.MINOR.PATCH", want)
	}
	gate := c.Require(provider.LocalUserGroups)
	switch {
	case minor >= 46 && gate != nil:
		t.Errorf("Dex %s: LocalUser.groups refused: %v", want, gate)
	case minor < 46 && (gate == nil || !strings.Contains(gate.Error(), "needs Dex v2.46")):
		t.Errorf("Dex %s: LocalUser.groups gate = %v, want a refusal", want, gate)
	}
	t.Logf("Dex %s reports server %q, API %d", want, c.Server.Server, c.Server.API)
}
