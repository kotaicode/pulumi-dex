package resources

import (
	"context"
	"net"
	"slices"
	"strings"
	"testing"

	api "github.com/dexidp/dex/api/v2"
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/integration"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/kotaicode/pulumi-dex/pkg/provider"
)

// =============================================================================
// Connector contract suite
//
// Every connector resource runs the same scenarios, against the fake Dex here
// and against real Dex servers in TestLive_ConnectorContract (dexlive build
// tag). Where the resources behave differently, the table records it and the
// suite asserts the behaviour as it is.
// =============================================================================

const contractCanary = "CANARY-CONTRACT"

// contractDex is the Dex server a contract run talks to.
type contractDex struct {
	addr       string
	fake       *fakeDex // nil for a real Dex; enables fault injection
	grantTypes bool     // the server stores connector grant types (Dex v2.46)
	id         func(name string) string
}

func (d contractDex) provider(t *testing.T) integration.Server {
	t.Helper()
	s := newTestProvider(t)
	if err := s.Configure(p.ConfigureRequest{Args: property.NewMap(map[string]property.Value{
		"host": str(d.addr), "plaintext": property.New(true), "timeoutSeconds": property.New(10.0),
	})}); err != nil {
		t.Fatalf("configure against %s: %v", d.addr, err)
	}
	return s
}

// raw returns a Dex API client for changes made outside Pulumi.
func (d contractDex) raw(t *testing.T) api.DexClient {
	t.Helper()
	c := &provider.DexConfig{Host: d.addr, Plaintext: ptrBool(true)}
	if err := c.Configure(t.Context()); err != nil {
		t.Fatalf("configure raw client: %v", err)
	}
	return c.Client
}

// serveFakeDex serves dex on a loopback port and returns its address.
func serveFakeDex(t *testing.T, dex *fakeDex) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := grpc.NewServer()
	api.RegisterDexServer(srv, dex)
	done := make(chan error, 1)
	go func() { done <- srv.Serve(lis) }()
	t.Cleanup(func() {
		srv.Stop()
		if err := <-done; err != nil {
			t.Errorf("fake Dex server: %v", err)
		}
	})
	return lis.Addr().String()
}

// configOutcome is what a refresh makes of a stored config that is not a JSON
// object.
type configOutcome int

const (
	configFailsRefresh configOutcome = iota // refresh fails naming the problem; state is kept
	configKeptRaw                           // reported verbatim as rawConfig
	configIgnored                           // the config is not read
)

type connectorContract struct {
	token   string
	dexType string
	inputs  func(id string) map[string]property.Value
	// secret is the credential input; the preview scenario makes it unknown.
	secret  string
	anyType bool // reads a connector of any Dex type and reports its type
	config  configOutcome
	// drift is what the next preview proposes after a refresh or import of a
	// connector created from inputs, as diffKeys prints it; "" means nothing.
	drift string
}

func (c connectorContract) short() string {
	return strings.ToLower(strings.TrimPrefix(c.token, "dex:resources:"))
}

func connectorContracts() []connectorContract {
	oauth := func(extra map[string]property.Value) func(id string) map[string]property.Value {
		return func(id string) map[string]property.Value {
			return with(map[string]property.Value{
				"connectorId": str(id), "name": str("Contract"), "clientId": str("contract-app"),
				"clientSecret": str(contractCanary), "redirectUri": str("https://dex.example.com/callback"),
			}, extra)
		}
	}
	return []connectorContract{
		{token: tokenConnector, dexType: "ldap", secret: "rawConfig", anyType: true, config: configKeptRaw,
			inputs: func(id string) map[string]property.Value {
				return map[string]property.Value{
					"connectorId": str(id), "type": str("ldap"), "name": str("Contract"),
					"rawConfig": str(`{"host":"ldap.example.com:636","bindPW":"` + contractCanary + `"}`),
				}
			}},
		{token: tokenGitHubConnector, dexType: "github", secret: "clientSecret", inputs: oauth(nil)},
		{token: tokenGitLabConnector, dexType: "gitlab", secret: "clientSecret", inputs: oauth(nil)},
		{token: tokenGoogleConnector, dexType: "google", secret: "clientSecret", inputs: oauth(nil)},
		{token: tokenAzureOidcConnector, dexType: "oidc", secret: "clientSecret",
			inputs: oauth(map[string]property.Value{"tenantId": str("00000000-0000-0000-0000-000000000000")})},
		{token: tokenAzureMicrosoftConnector, dexType: "microsoft", secret: "clientSecret",
			inputs: oauth(map[string]property.Value{"tenant": str("organizations")})},
		{token: tokenCognitoOidcConnector, dexType: "oidc", secret: "clientSecret",
			inputs: oauth(map[string]property.Value{"region": str("eu-central-1"), "userPoolId": str("eu-central-1_pool")})},
		{token: tokenLdapConnector, dexType: "ldap", secret: "bindPassword",
			inputs: func(id string) map[string]property.Value {
				return map[string]property.Value{
					"connectorId": str(id), "name": str("Contract"), "host": str("ldap.example.com:636"),
					"bindDn": str("cn=reader,dc=example,dc=org"), "bindPassword": str(contractCanary),
					"userSearch": obj(map[string]property.Value{"baseDn": str("dc=example,dc=org"), "usernameAttributes": strs("uid")}),
				}
			}},
		{token: tokenLocalConnector, dexType: "local", config: configIgnored,
			inputs: func(id string) map[string]property.Value {
				return map[string]property.Value{"connectorId": str(id), "name": str("Contract")}
			}},
	}
}

var contractScenarios = []struct {
	name string
	run  func(t *testing.T, d contractDex, c connectorContract)
}{
	{"lifecycle", contractLifecycle},
	{"duplicate ID", contractDuplicateID},
	{"not found on update", contractUpdateNotFound},
	{"deleted in Dex", contractDeletedInDex},
	{"transport errors", contractTransportErrors},
	{"unreadable config", contractUnreadableConfig},
	{"type mismatch", contractTypeMismatch},
	{"unknown inputs at preview", contractUnknownInputs},
	{"grant types survive update", contractGrantTypes},
}

func runConnectorContract(t *testing.T, newDex func(t *testing.T) contractDex) {
	for _, c := range connectorContracts() {
		t.Run(strings.TrimPrefix(c.token, "dex:resources:"), func(t *testing.T) {
			for _, sc := range contractScenarios {
				t.Run(sc.name, func(t *testing.T) { sc.run(t, newDex(t), c) })
			}
		})
	}
}

func TestConnectorContract(t *testing.T) {
	runConnectorContract(t, func(t *testing.T) contractDex {
		dex := newFakeDex()
		return contractDex{addr: serveFakeDex(t, dex), fake: dex, grantTypes: true, id: func(name string) string { return name }}
	})
}

// --- helpers -----------------------------------------------------------------

// contractCreate checks and creates the resource and deletes the connector
// when the test ends.
func contractCreate(t *testing.T, d contractDex, s integration.Server, c connectorContract, inputs map[string]property.Value) (property.Map, p.CreateResponse) {
	t.Helper()
	urn := testURN(c.token)
	checked, err := s.Check(p.CheckRequest{Urn: urn, Inputs: property.NewMap(inputs)})
	if err != nil || len(checked.Failures) > 0 {
		t.Fatalf("Check: %v %v", err, checked.Failures)
	}
	created, err := s.Create(p.CreateRequest{Urn: urn, Properties: checked.Inputs})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	raw := d.raw(t)
	t.Cleanup(func() {
		_, _ = raw.DeleteConnector(context.Background(), &api.DeleteConnectorReq{Id: created.ID}) //nolint:errcheck // best-effort cleanup
	})
	return checked.Inputs, created
}

func storedConnector(t *testing.T, raw api.DexClient, id string) *api.Connector {
	t.Helper()
	resp, err := raw.ListConnectors(t.Context(), &api.ListConnectorReq{})
	if err != nil {
		t.Fatalf("ListConnectors: %v", err)
	}
	for _, c := range resp.GetConnectors() {
		if c.GetId() == id {
			return c
		}
	}
	return nil
}

func rawUpdate(t *testing.T, raw api.DexClient, req *api.UpdateConnectorReq) {
	t.Helper()
	resp, err := raw.UpdateConnector(t.Context(), req)
	if err != nil || resp.GetNotFound() {
		t.Fatalf("UpdateConnector outside Pulumi: not_found=%v err=%v", resp.GetNotFound(), err)
	}
}

func rawDelete(t *testing.T, raw api.DexClient, id string) {
	t.Helper()
	if _, err := raw.DeleteConnector(t.Context(), &api.DeleteConnectorReq{Id: id}); err != nil {
		t.Fatalf("DeleteConnector outside Pulumi: %v", err)
	}
}

// assertNoChanges runs the next preview of program against what Read returned.
func assertNoChanges(t *testing.T, s integration.Server, c connectorContract, read p.ReadResponse, program map[string]property.Value, when string) {
	t.Helper()
	urn := testURN(c.token)
	checked, err := s.Check(p.CheckRequest{Urn: urn, State: read.Inputs, Inputs: property.NewMap(program)})
	if err != nil || len(checked.Failures) > 0 {
		t.Fatalf("%s: Check: %v %v", when, err, checked.Failures)
	}
	diff, err := s.Diff(p.DiffRequest{ID: read.ID, Urn: urn, State: read.Properties, OldInputs: read.Inputs, Inputs: checked.Inputs})
	if err != nil {
		t.Fatalf("%s: Diff: %v", when, err)
	}
	if got := diffKeys(diff); diff.HasChanges != (c.drift != "") || got != c.drift {
		t.Errorf("%s: preview proposes %q, want %q", when, got, c.drift)
	}
}

func replaces(diff p.DiffResponse) bool {
	for _, d := range diff.DetailedDiff {
		if d.Kind == p.UpdateReplace || d.Kind == p.AddReplace || d.Kind == p.DeleteReplace {
			return true
		}
	}
	return diff.DeleteBeforeReplace
}

func noCanary(t *testing.T, what string, err error) {
	t.Helper()
	if err != nil && (strings.Contains(err.Error(), contractCanary) || strings.Contains(err.Error(), existingCanary)) {
		t.Errorf("%s reveals a credential: %v", what, err)
	}
}

// --- scenarios ---------------------------------------------------------------

// create → refresh → clean preview → rename → import → delete twice.
func contractLifecycle(t *testing.T, d contractDex, c connectorContract) {
	s, raw, id, urn := d.provider(t), d.raw(t), d.id(c.short()), testURN(c.token)
	program := c.inputs(id)
	checked, created := contractCreate(t, d, s, c, program)
	if created.ID != id {
		t.Fatalf("Create returned ID %q, want %q", created.ID, id)
	}
	if got := storedConnector(t, raw, id); got == nil || got.GetType() != c.dexType || got.GetName() != "Contract" {
		t.Fatalf("stored connector = %v, want type %q and the program's name", got, c.dexType)
	}

	refreshed, err := s.Read(p.ReadRequest{ID: id, Urn: urn, Properties: created.Properties, Inputs: checked})
	if err != nil || refreshed.ID != id {
		t.Fatalf("refresh: id=%q err=%v", refreshed.ID, err)
	}
	assertNoChanges(t, s, c, refreshed, program, "preview after refresh")

	renamed := with(program, map[string]property.Value{"name": str("Contract renamed")})
	next, err := s.Check(p.CheckRequest{Urn: urn, State: refreshed.Inputs, Inputs: property.NewMap(renamed)})
	if err != nil || len(next.Failures) > 0 {
		t.Fatalf("Check rename: %v %v", err, next.Failures)
	}
	diff, err := s.Diff(p.DiffRequest{ID: id, Urn: urn, State: refreshed.Properties, OldInputs: refreshed.Inputs, Inputs: next.Inputs})
	if err != nil || !diff.HasChanges || replaces(diff) {
		t.Fatalf("Diff rename: changes=%v replace=%v err=%v", diff.HasChanges, replaces(diff), err)
	}
	updated, err := s.Update(p.UpdateRequest{ID: id, Urn: urn, State: refreshed.Properties, OldInputs: refreshed.Inputs, Inputs: next.Inputs})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got := storedConnector(t, raw, id); got.GetName() != "Contract renamed" || got.GetType() != c.dexType {
		t.Errorf("after rename Dex has name %q type %q", got.GetName(), got.GetType())
	}

	imported, err := s.Read(p.ReadRequest{ID: id, Urn: urn})
	if err != nil || imported.ID != id {
		t.Fatalf("import: id=%q err=%v", imported.ID, err)
	}
	assertNoChanges(t, s, c, imported, renamed, "preview after import")

	for i := 1; i <= 2; i++ {
		if err := s.Delete(p.DeleteRequest{ID: id, Urn: urn, Properties: updated.Properties}); err != nil {
			t.Fatalf("Delete #%d: %v", i, err)
		}
	}
	if got := storedConnector(t, raw, id); got != nil {
		t.Fatalf("connector still in Dex after Delete: %v", got)
	}
	gone, err := s.Read(p.ReadRequest{ID: id, Urn: urn, Properties: updated.Properties, Inputs: next.Inputs})
	if err != nil || gone.ID != "" {
		t.Errorf("refresh after delete: id=%q err=%v, want gone", gone.ID, err)
	}
}

// Create on a taken ID is a conflict that recommends import and leaves the
// other owner's connector untouched.
func contractDuplicateID(t *testing.T, d contractDex, c connectorContract) {
	s, raw, id, urn := d.provider(t), d.raw(t), d.id(c.short()), testURN(c.token)
	foreign := &api.Connector{Id: id, Type: "ldap", Name: "Theirs",
		Config: []byte(`{"host":"theirs.example.com:636","bindPW":"` + existingCanary + `"}`)}
	if _, err := raw.CreateConnector(t.Context(), &api.CreateConnectorReq{Connector: foreign}); err != nil {
		t.Fatalf("seed foreign connector: %v", err)
	}
	t.Cleanup(func() {
		_, _ = raw.DeleteConnector(context.Background(), &api.DeleteConnectorReq{Id: id}) //nolint:errcheck // best-effort cleanup
	})

	checked, err := s.Check(p.CheckRequest{Urn: urn, Inputs: property.NewMap(c.inputs(id))})
	if err != nil || len(checked.Failures) > 0 {
		t.Fatalf("Check: %v %v", err, checked.Failures)
	}
	_, err = s.Create(p.CreateRequest{Urn: urn, Properties: checked.Inputs})
	if want := "pulumi import " + c.token + " r " + id; err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("Create on a taken ID = %v, want a conflict recommending %q", err, want)
	}
	noCanary(t, "conflict", err)
	got := storedConnector(t, raw, id)
	if got.GetType() != "ldap" || got.GetName() != "Theirs" || string(got.GetConfig()) != string(foreign.Config) {
		t.Errorf("the existing connector was changed: %v", got)
	}
}

// Dex's not_found flag on update is an error, not a success.
func contractUpdateNotFound(t *testing.T, d contractDex, c connectorContract) {
	s, raw, id, urn := d.provider(t), d.raw(t), d.id(c.short()), testURN(c.token)
	program := c.inputs(id)
	checked, created := contractCreate(t, d, s, c, program)
	rawDelete(t, raw, id)

	next, err := s.Check(p.CheckRequest{Urn: urn, State: checked, Inputs: property.NewMap(with(program, map[string]property.Value{"name": str("Contract renamed")}))})
	if err != nil || len(next.Failures) > 0 {
		t.Fatalf("Check: %v %v", err, next.Failures)
	}
	_, err = s.Update(p.UpdateRequest{ID: id, Urn: urn, State: created.Properties, OldInputs: checked, Inputs: next.Inputs})
	if err == nil || !strings.Contains(err.Error(), "not found in Dex") || !strings.Contains(err.Error(), id) {
		t.Errorf("Update of a connector Dex no longer has = %v, want a not-found error naming it", err)
	}
	noCanary(t, "update error", err)
	if got := storedConnector(t, raw, id); got != nil {
		t.Errorf("Update recreated the connector: %v", got)
	}
}

// A connector deleted outside Pulumi is reported gone on refresh, and its
// import fails.
func contractDeletedInDex(t *testing.T, d contractDex, c connectorContract) {
	s, raw, id, urn := d.provider(t), d.raw(t), d.id(c.short()), testURN(c.token)
	checked, created := contractCreate(t, d, s, c, c.inputs(id))
	rawDelete(t, raw, id)

	gone, err := s.Read(p.ReadRequest{ID: id, Urn: urn, Properties: created.Properties, Inputs: checked})
	if err != nil || gone.ID != "" {
		t.Errorf("refresh: id=%q err=%v, want gone", gone.ID, err)
	}
	if _, err := s.Read(p.ReadRequest{ID: id, Urn: urn}); err == nil || !strings.Contains(err.Error(), "not found in Dex") {
		t.Errorf("import: %v, want a not-found error", err)
	}
}

// A failed call is never taken for absence: refresh, update and delete fail
// and leave the state as it is.
func contractTransportErrors(t *testing.T, d contractDex, c connectorContract) {
	if d.fake == nil {
		t.Skip("needs fault injection (fake Dex only)")
	}
	s, id, urn := d.provider(t), d.id(c.short()), testURN(c.token)
	program := c.inputs(id)
	checked, created := contractCreate(t, d, s, c, program)
	unavailable := status.Error(codes.Unavailable, "injected transport failure")

	d.fake.mu.Lock()
	d.fake.listConnectorsErr = unavailable
	d.fake.mu.Unlock()
	resp, err := s.Read(p.ReadRequest{ID: id, Urn: urn, Properties: created.Properties, Inputs: checked})
	if err == nil {
		t.Errorf("refresh with ListConnectors unavailable succeeded with id=%q; it must fail", resp.ID)
	}
	noCanary(t, "refresh error", err)

	d.fake.mu.Lock()
	d.fake.listConnectorsErr, d.fake.connectorWriteErr = nil, unavailable
	d.fake.mu.Unlock()
	next, cerr := s.Check(p.CheckRequest{Urn: urn, State: checked, Inputs: property.NewMap(with(program, map[string]property.Value{"name": str("Contract renamed")}))})
	if cerr != nil || len(next.Failures) > 0 {
		t.Fatalf("Check: %v %v", cerr, next.Failures)
	}
	_, err = s.Update(p.UpdateRequest{ID: id, Urn: urn, State: created.Properties, OldInputs: checked, Inputs: next.Inputs})
	if err == nil || strings.Contains(err.Error(), "not found in Dex") {
		t.Errorf("Update with Dex unavailable = %v, want a failure that is not reported as not found", err)
	}
	noCanary(t, "update error", err)
	if err := s.Delete(p.DeleteRequest{ID: id, Urn: urn, Properties: created.Properties}); err == nil {
		t.Error("Delete with Dex unavailable succeeded; it must fail so the resource stays in state")
	}

	d.fake.mu.Lock()
	d.fake.connectorWriteErr = nil
	d.fake.mu.Unlock()
	if got := storedConnector(t, d.raw(t), id); got == nil || got.GetName() != "Contract" {
		t.Errorf("connector changed by failed calls: %v", got)
	}
}

// A stored config that is not a JSON object.
func contractUnreadableConfig(t *testing.T, d contractDex, c connectorContract) {
	bad := map[string]func(t *testing.T, raw api.DexClient, id string) string{
		// Dex accepts any valid JSON.
		"not an object": func(t *testing.T, raw api.DexClient, id string) string {
			rawUpdate(t, raw, &api.UpdateConnectorReq{Id: id, NewConfig: []byte(`[1]`)})
			return `[1]`
		},
	}
	if d.fake != nil {
		// Dex rejects invalid JSON on its API, but its storage can still hold it.
		bad["not JSON"] = func(t *testing.T, _ api.DexClient, id string) string {
			d.fake.mu.Lock()
			defer d.fake.mu.Unlock()
			d.fake.connectors[id].Config = []byte(`{not json`)
			return `{not json`
		}
	}
	for name, corrupt := range bad {
		t.Run(name, func(t *testing.T) {
			s, raw, id, urn := d.provider(t), d.raw(t), d.id(c.short()+"-cfg"), testURN(c.token)
			checked, created := contractCreate(t, d, s, c, c.inputs(id))
			remote := corrupt(t, raw, id)

			resp, err := s.Read(p.ReadRequest{ID: id, Urn: urn, Properties: created.Properties, Inputs: checked})
			noCanary(t, "refresh error", err)
			switch c.config {
			case configFailsRefresh:
				if err == nil || !strings.Contains(err.Error(), "cannot read the connector config stored in Dex") {
					t.Errorf("refresh = id %q, %v; want it to fail and keep the state", resp.ID, err)
				}
			case configKeptRaw:
				if err != nil || resp.ID != id || resp.Inputs.Get("rawConfig").AsString() != remote {
					t.Errorf("refresh = id %q rawConfig %v, %v; want the stored config verbatim", resp.ID, resp.Inputs.Get("rawConfig"), err)
				}
			case configIgnored:
				if err != nil || resp.ID != id || resp.Inputs.Get("name").AsString() != "Contract" {
					t.Errorf("refresh = id %q, %v; want the connector read without its config", resp.ID, err)
				}
			}
		})
	}
}

// A connector whose Dex type is not the resource's.
func contractTypeMismatch(t *testing.T, d contractDex, c connectorContract) {
	s, raw, id, urn := d.provider(t), d.raw(t), d.id(c.short()), testURN(c.token)
	checked, created := contractCreate(t, d, s, c, c.inputs(id))
	// Dex v2.45 rejects an update without a valid config, so the stored one is sent again.
	rawUpdate(t, raw, &api.UpdateConnectorReq{Id: id, NewType: "saml", NewConfig: storedConnector(t, raw, id).GetConfig()})

	resp, err := s.Read(p.ReadRequest{ID: id, Urn: urn, Properties: created.Properties, Inputs: checked})
	noCanary(t, "refresh error", err)
	if c.anyType {
		if err != nil || resp.Inputs.Get("type").AsString() != "saml" {
			t.Errorf("refresh = type %v, %v; want the new type reported", resp.Inputs.Get("type"), err)
		}
		return
	}
	if err == nil || !strings.Contains(err.Error(), `has type "saml"`) || !strings.Contains(err.Error(), tokenConnector) {
		t.Errorf("refresh = id %q, %v; want a type mismatch that recommends %s", resp.ID, err, tokenConnector)
	}
	if _, err := s.Read(p.ReadRequest{ID: id, Urn: urn}); err == nil || !strings.Contains(err.Error(), `has type "saml"`) {
		t.Errorf("import = %v, want a type mismatch", err)
	}
}

// Unknown values pass preview, for a new and for an existing resource, and
// preview writes nothing.
func contractUnknownInputs(t *testing.T, d contractDex, c connectorContract) {
	s, id, urn := d.provider(t), d.id(c.short()), testURN(c.token)
	unknown := func(program map[string]property.Value) map[string]property.Value {
		m := with(program, map[string]property.Value{"name": property.New(property.Computed)})
		if c.secret != "" {
			m[c.secret] = property.New(property.Computed)
		}
		return m
	}
	writes := func() int {
		if d.fake == nil {
			return 0
		}
		d.fake.mu.Lock()
		defer d.fake.mu.Unlock()
		return d.fake.writes
	}

	before := writes()
	checked, err := s.Check(p.CheckRequest{Urn: urn, Inputs: property.NewMap(unknown(c.inputs(id)))})
	if err != nil || len(checked.Failures) > 0 {
		t.Fatalf("Check with unknown values: %v %v", err, checked.Failures)
	}
	created, err := s.Create(p.CreateRequest{Urn: urn, Properties: checked.Inputs, DryRun: true})
	if err != nil || created.ID != id {
		t.Errorf("preview of create: id=%q err=%v", created.ID, err)
	}
	if after := writes(); after != before {
		t.Errorf("preview wrote to Dex (%d writes)", after-before)
	}

	program := c.inputs(id)
	old, existing := contractCreate(t, d, s, c, program)
	before = writes()
	next, err := s.Check(p.CheckRequest{Urn: urn, State: old, Inputs: property.NewMap(unknown(program))})
	if err != nil || len(next.Failures) > 0 {
		t.Fatalf("Check update with unknown values: %v %v", err, next.Failures)
	}
	diff, err := s.Diff(p.DiffRequest{ID: id, Urn: urn, State: existing.Properties, OldInputs: old, Inputs: next.Inputs})
	if err != nil || !diff.HasChanges || replaces(diff) {
		t.Errorf("Diff with unknown values: changes=%v replace=%v err=%v", diff.HasChanges, replaces(diff), err)
	}
	if _, err := s.Update(p.UpdateRequest{ID: id, Urn: urn, State: existing.Properties, OldInputs: old, Inputs: next.Inputs, DryRun: true}); err != nil {
		t.Errorf("preview of update: %v", err)
	}
	if after := writes(); after != before {
		t.Errorf("preview wrote to Dex (%d writes)", after-before)
	}
}

// Grant types set directly in Dex survive an update: the provider never sends
// new_grant_types.
func contractGrantTypes(t *testing.T, d contractDex, c connectorContract) {
	if !d.grantTypes {
		t.Skip("the server does not store connector grant types (Dex v2.45)")
	}
	s, raw, id, urn := d.provider(t), d.raw(t), d.id(c.short()), testURN(c.token)
	program := c.inputs(id)
	checked, created := contractCreate(t, d, s, c, program)
	restricted := []string{"authorization_code", "refresh_token"}
	rawUpdate(t, raw, &api.UpdateConnectorReq{Id: id, NewGrantTypes: &api.GrantTypes{GrantTypes: restricted}})

	renamed := with(program, map[string]property.Value{"name": str("Contract renamed")})
	next, err := s.Check(p.CheckRequest{Urn: urn, State: checked, Inputs: property.NewMap(renamed)})
	if err != nil || len(next.Failures) > 0 {
		t.Fatalf("Check: %v %v", err, next.Failures)
	}
	updated, err := s.Update(p.UpdateRequest{ID: id, Urn: urn, State: created.Properties, OldInputs: checked, Inputs: next.Inputs})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	got := storedConnector(t, raw, id)
	if got.GetName() != "Contract renamed" || !slices.Equal(got.GetGrantTypes(), restricted) {
		t.Errorf("after update: name %q grant types %v, want the rename and grant types %v", got.GetName(), got.GetGrantTypes(), restricted)
	}
	refreshed, err := s.Read(p.ReadRequest{ID: id, Urn: urn, Properties: updated.Properties, Inputs: next.Inputs})
	if err != nil || refreshed.ID != id {
		t.Fatalf("refresh: id=%q err=%v", refreshed.ID, err)
	}
	assertNoChanges(t, s, c, refreshed, renamed, "preview after refresh")
}
