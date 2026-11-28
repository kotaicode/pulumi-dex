package resources

import (
	"context"
	"errors"
	"net"
	"slices"
	"strings"
	"sync"
	"testing"

	api "github.com/dexidp/dex/api/v2"
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/integration"
	presource "github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// =============================================================================
// RPC outcome tests against an in-process fake Dex gRPC server
// =============================================================================

// fakeDex models the not-found semantics of Dex's API handlers as of v2.46.1
// (server/apiserver/clients.go and connectors.go at commit 75eb1923):
//   - UpdateClient/UpdateConnector/Delete* report absence via a not_found flag
//     with a nil error.
//   - GetClient returns the storage error unwrapped, so absence arrives as
//     codes.Unknown with the message "not found", not codes.NotFound.
type fakeDex struct {
	api.UnimplementedDexServer

	mu         sync.Mutex
	clients    map[string]*api.Client
	connectors map[string]*api.Connector

	// Injected failures for negative tests.
	getClientErr      error
	listClientsErr    error
	listConnectorsErr error
	listPasswordsErr  error
	// connectorWriteErr fails UpdateConnector and DeleteConnector without
	// changing anything, like a transport failure.
	connectorWriteErr error
	// alreadyExistsStatus reports an existing ID on Create as gRPC AlreadyExists
	// instead of Dex's already_exists response flag.
	alreadyExistsStatus bool
	// createErr is returned by Create after the object was stored, simulating a
	// write that committed but whose response was lost (e.g. a timeout).
	createErr error
	// staticConnectors are defined in Dex's configuration file; like Dex, the
	// fake refuses to create them through the API with an untyped error.
	staticConnectors map[string]bool
	// passwords are keyed by lower-case email, as in Dex's storage; the fake
	// keeps the hash, which Dex's ListPasswords never returns.
	passwords       map[string]*api.Password
	staticPasswords map[string]bool
	// version is what GetVersion reports; nil means Dex v2.46.1 (API 4).
	version    *api.VersionResp
	versionErr error

	writes int // Create, Update and Delete calls, to prove read-only paths
}

func (f *fakeDex) GetVersion(context.Context, *api.VersionReq) (*api.VersionResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.versionErr != nil {
		return nil, f.versionErr
	}
	if f.version != nil {
		return f.version, nil
	}
	return &api.VersionResp{Server: "v2.46.1", Api: 4}, nil
}

// groupsSupported mirrors the API change in Dex v2.46: an older server does
// not know the groups fields and drops them without an error.
func (f *fakeDex) groupsSupported() bool { return f.version == nil || f.version.Api >= 4 }

// fakeNormalizeGroups sorts and de-duplicates like Dex v2.46.1.
func fakeNormalizeGroups(groups []string) ([]string, error) {
	if slices.Contains(groups, "") {
		return nil, errors.New("group names must not be empty")
	}
	out := slices.Clone(groups)
	slices.Sort(out)
	return slices.Compact(out), nil
}

func (f *fakeDex) CreatePassword(_ context.Context, req *api.CreatePasswordReq) (*api.CreatePasswordResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes++
	pw := req.GetPassword()
	if pw.GetUserId() == "" {
		return nil, errors.New("no user ID supplied")
	}
	if len(pw.GetHash()) == 0 {
		return nil, errors.New("no hash of password supplied")
	}
	key := strings.ToLower(pw.GetEmail())
	if f.staticPasswords[key] {
		return nil, status.Error(codes.Unknown, "create password: static passwords: read-only cannot create password")
	}
	if _, ok := f.passwords[key]; ok {
		if f.alreadyExistsStatus {
			return nil, status.Error(codes.AlreadyExists, "password already exists")
		}
		return &api.CreatePasswordResp{AlreadyExists: true}, nil
	}
	var groups []string
	if f.groupsSupported() {
		var err error
		if groups, err = fakeNormalizeGroups(pw.GetGroups()); err != nil {
			return nil, err
		}
	}
	f.passwords[key] = &api.Password{Email: pw.GetEmail(), Hash: slices.Clone(pw.GetHash()), Username: pw.GetUsername(), UserId: pw.GetUserId(), Groups: groups}
	if f.createErr != nil {
		return nil, f.createErr
	}
	return &api.CreatePasswordResp{}, nil
}

func (f *fakeDex) UpdatePassword(_ context.Context, req *api.UpdatePasswordReq) (*api.UpdatePasswordResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes++
	newGroups := req.GetNewGroups()
	if !f.groupsSupported() {
		newGroups = nil
	}
	if req.GetNewHash() == nil && req.GetNewUsername() == "" && newGroups == nil {
		return nil, status.Error(codes.Unknown, "nothing to update")
	}
	pw, ok := f.passwords[strings.ToLower(req.GetEmail())]
	if !ok {
		return &api.UpdatePasswordResp{NotFound: true}, nil
	}
	if req.GetNewHash() != nil {
		pw.Hash = slices.Clone(req.GetNewHash())
	}
	if req.GetNewUsername() != "" {
		pw.Username = req.GetNewUsername()
	}
	if newGroups != nil {
		groups, err := fakeNormalizeGroups(newGroups)
		if err != nil {
			return nil, err
		}
		pw.Groups = groups
	}
	return &api.UpdatePasswordResp{}, nil
}

func (f *fakeDex) DeletePassword(_ context.Context, req *api.DeletePasswordReq) (*api.DeletePasswordResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes++
	key := strings.ToLower(req.GetEmail())
	if _, ok := f.passwords[key]; !ok {
		return &api.DeletePasswordResp{NotFound: true}, nil
	}
	delete(f.passwords, key)
	return &api.DeletePasswordResp{}, nil
}

// ListPasswords returns the users without their hashes, like Dex.
func (f *fakeDex) ListPasswords(context.Context, *api.ListPasswordReq) (*api.ListPasswordResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listPasswordsErr != nil {
		return nil, f.listPasswordsErr
	}
	out := make([]*api.Password, 0, len(f.passwords))
	for _, pw := range f.passwords {
		out = append(out, &api.Password{Email: pw.Email, Username: pw.Username, UserId: pw.UserId, Groups: slices.Clone(pw.Groups)})
	}
	return &api.ListPasswordResp{Passwords: out}, nil
}

func newFakeDex() *fakeDex {
	return &fakeDex{clients: map[string]*api.Client{}, connectors: map[string]*api.Connector{}, passwords: map[string]*api.Password{}}
}

func (f *fakeDex) GetClient(_ context.Context, req *api.GetClientReq) (*api.GetClientResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getClientErr != nil {
		return nil, f.getClientErr
	}
	c, ok := f.clients[req.Id]
	if !ok {
		return nil, errors.New("not found")
	}
	return &api.GetClientResp{Client: c}, nil
}

// CreateClient stores the client as sent, as Dex's storage does.
func (f *fakeDex) CreateClient(_ context.Context, req *api.CreateClientReq) (*api.CreateClientResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes++
	c := req.GetClient()
	if _, ok := f.clients[c.GetId()]; ok {
		if f.alreadyExistsStatus {
			return nil, status.Error(codes.AlreadyExists, "client already exists")
		}
		return &api.CreateClientResp{AlreadyExists: true}, nil
	}
	f.clients[c.GetId()] = &api.Client{
		Id: c.GetId(), Secret: c.GetSecret(), Name: c.GetName(), LogoUrl: c.GetLogoUrl(), Public: c.GetPublic(),
		RedirectUris: append([]string(nil), c.GetRedirectUris()...),
		TrustedPeers: append([]string(nil), c.GetTrustedPeers()...),
	}
	if f.createErr != nil {
		return nil, f.createErr
	}
	return &api.CreateClientResp{Client: c}, nil
}

func (f *fakeDex) UpdateClient(_ context.Context, req *api.UpdateClientReq) (*api.UpdateClientResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes++
	c, ok := f.clients[req.Id]
	if !ok {
		return &api.UpdateClientResp{NotFound: true}, nil
	}
	// Same fields and conditions as Dex v2.46.1's UpdateClient: secret and
	// public cannot change, and empty name or logo URL leave the old value.
	if req.RedirectUris != nil {
		c.RedirectUris = req.RedirectUris
	}
	if req.TrustedPeers != nil {
		c.TrustedPeers = req.TrustedPeers
	}
	if req.Name != "" {
		c.Name = req.Name
	}
	if req.LogoUrl != "" {
		c.LogoUrl = req.LogoUrl
	}
	return &api.UpdateClientResp{}, nil
}

func (f *fakeDex) DeleteClient(_ context.Context, req *api.DeleteClientReq) (*api.DeleteClientResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes++
	if _, ok := f.clients[req.Id]; !ok {
		return &api.DeleteClientResp{NotFound: true}, nil
	}
	delete(f.clients, req.Id)
	return &api.DeleteClientResp{}, nil
}

func (f *fakeDex) ListClients(context.Context, *api.ListClientReq) (*api.ListClientResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listClientsErr != nil {
		return nil, f.listClientsErr
	}
	resp := &api.ListClientResp{}
	for _, c := range f.clients {
		resp.Clients = append(resp.Clients, &api.ClientInfo{Id: c.Id, Name: c.Name})
	}
	return resp, nil
}

// CreateConnector stores the config bytes verbatim, as Dex's storage does.
func (f *fakeDex) CreateConnector(_ context.Context, req *api.CreateConnectorReq) (*api.CreateConnectorResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes++
	c := req.GetConnector()
	if f.staticConnectors[c.GetId()] {
		return nil, status.Error(codes.Unknown, "create connector: static connectors: read-only cannot create connector")
	}
	if _, ok := f.connectors[c.GetId()]; ok {
		if f.alreadyExistsStatus {
			return nil, status.Error(codes.AlreadyExists, "connector already exists")
		}
		return &api.CreateConnectorResp{AlreadyExists: true}, nil
	}
	f.connectors[c.GetId()] = &api.Connector{
		Id: c.GetId(), Type: c.GetType(), Name: c.GetName(), Config: append([]byte(nil), c.GetConfig()...),
		GrantTypes: append([]string(nil), c.GetGrantTypes()...),
	}
	if f.createErr != nil {
		return nil, f.createErr
	}
	return &api.CreateConnectorResp{}, nil
}

func (f *fakeDex) UpdateConnector(_ context.Context, req *api.UpdateConnectorReq) (*api.UpdateConnectorResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes++
	if f.connectorWriteErr != nil {
		return nil, f.connectorWriteErr
	}
	c, ok := f.connectors[req.Id]
	if !ok {
		return &api.UpdateConnectorResp{NotFound: true}, nil
	}
	// Same as Dex v2.46.1's UpdateConnector: type, name, the whole config and
	// the grant types change when set; the ID never changes.
	if req.NewType != "" {
		c.Type = req.NewType
	}
	if req.NewName != "" {
		c.Name = req.NewName
	}
	if len(req.NewConfig) != 0 {
		c.Config = req.NewConfig
	}
	if req.NewGrantTypes != nil {
		c.GrantTypes = req.NewGrantTypes.GrantTypes
	}
	return &api.UpdateConnectorResp{}, nil
}

func (f *fakeDex) DeleteConnector(_ context.Context, req *api.DeleteConnectorReq) (*api.DeleteConnectorResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes++
	if f.connectorWriteErr != nil {
		return nil, f.connectorWriteErr
	}
	if _, ok := f.connectors[req.Id]; !ok {
		return &api.DeleteConnectorResp{NotFound: true}, nil
	}
	delete(f.connectors, req.Id)
	return &api.DeleteConnectorResp{}, nil
}

func (f *fakeDex) ListConnectors(context.Context, *api.ListConnectorReq) (*api.ListConnectorResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listConnectorsErr != nil {
		return nil, f.listConnectorsErr
	}
	resp := &api.ListConnectorResp{}
	for _, c := range f.connectors {
		resp.Connectors = append(resp.Connectors, c)
	}
	return resp, nil
}

// newProviderAgainst starts dex on a loopback listener and returns newTestProvider
// configured to talk to it, exercising the real infer encode/decode path.
func newProviderAgainst(t *testing.T, dex *fakeDex) integration.Server {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	grpcServer := grpc.NewServer()
	api.RegisterDexServer(grpcServer, dex)
	serveErr := make(chan error, 1)
	go func() { serveErr <- grpcServer.Serve(lis) }()
	t.Cleanup(func() {
		grpcServer.Stop()
		if err := <-serveErr; err != nil {
			t.Errorf("fake Dex server: %v", err)
		}
	})

	s := newTestProvider(t)
	if err := s.Configure(p.ConfigureRequest{Args: property.NewMap(map[string]property.Value{
		"host":           property.New(lis.Addr().String()),
		"plaintext":      property.New(true),
		"timeoutSeconds": property.New(5.0),
	})}); err != nil {
		t.Fatalf("configure: %v", err)
	}
	return s
}

func testURN(typ string) presource.URN {
	return presource.URN("urn:pulumi:test::test::" + typ + "::r")
}

func strVal(s string) property.Value { return property.New(s) }

func TestUpdate_remoteNotFound(t *testing.T) {
	oauth := map[string]property.Value{
		"connectorId":  strVal("conn"),
		"name":         strVal("Conn"),
		"clientId":     strVal("cid"),
		"clientSecret": strVal("synthetic-canary-secret"),
		"redirectUri":  strVal("https://dex.example.com/callback"),
	}
	with := func(base map[string]property.Value, extra map[string]property.Value) map[string]property.Value {
		out := map[string]property.Value{}
		for k, v := range base {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}

	cases := []struct {
		typ    string
		id     string
		inputs map[string]property.Value
	}{
		{"dex:resources:Client", "client", map[string]property.Value{
			"clientId":     strVal("client"),
			"name":         strVal("Client"),
			"redirectUris": property.New([]property.Value{strVal("https://app.example.com/callback")}),
		}},
		{"dex:resources:Connector", "conn", map[string]property.Value{
			"connectorId": strVal("conn"),
			"type":        strVal("ldap"),
			"name":        strVal("Conn"),
			"rawConfig":   strVal(`{"host":"ldap.example.com:636"}`),
		}},
		{"dex:resources:AzureOidcConnector", "conn", with(oauth, map[string]property.Value{"tenantId": strVal("tenant")})},
		{"dex:resources:AzureMicrosoftConnector", "conn", with(oauth, map[string]property.Value{"tenant": strVal("tenant")})},
		{"dex:resources:CognitoOidcConnector", "conn", with(oauth, map[string]property.Value{
			"region":     strVal("eu-central-1"),
			"userPoolId": strVal("eu-central-1_pool"),
		})},
		{"dex:resources:GitHubConnector", "conn", oauth},
		{"dex:resources:GitLabConnector", "conn", oauth},
		{"dex:resources:GoogleConnector", "conn", oauth},
		{"dex:resources:LocalConnector", "conn", map[string]property.Value{
			"connectorId": strVal("conn"),
			"name":        strVal("Conn"),
		}},
	}

	for _, tc := range cases {
		t.Run(tc.typ, func(t *testing.T) {
			update := func(s integration.Server) error {
				m := property.NewMap(tc.inputs)
				_, err := s.Update(p.UpdateRequest{
					ID:        tc.id,
					Urn:       testURN(tc.typ),
					State:     m,
					OldInputs: m,
					Inputs:    m,
				})
				return err
			}

			t.Run("present", func(t *testing.T) {
				dex := newFakeDex()
				dex.clients[tc.id] = &api.Client{Id: tc.id}
				dex.connectors[tc.id] = &api.Connector{Id: tc.id}
				if err := update(newProviderAgainst(t, dex)); err != nil {
					t.Fatalf("Update of existing object failed: %v", err)
				}
			})

			t.Run("absent", func(t *testing.T) {
				err := update(newProviderAgainst(t, newFakeDex()))
				if err == nil {
					t.Fatal("Update reported success although Dex answered not_found")
				}
				if !strings.Contains(err.Error(), "not found") || !strings.Contains(err.Error(), tc.id) {
					t.Errorf("error should name the missing object, got: %v", err)
				}
				if strings.Contains(err.Error(), "synthetic-canary-secret") {
					t.Errorf("error leaks credential input: %v", err)
				}
			})
		})
	}
}

func TestClient_Read_outcomes(t *testing.T) {
	read := func(s integration.Server) (p.ReadResponse, error) {
		return s.Read(p.ReadRequest{
			ID:  "client",
			Urn: testURN("dex:resources:Client"),
			Properties: property.NewMap(map[string]property.Value{
				"clientId":     strVal("client"),
				"name":         strVal("Client"),
				"redirectUris": property.New([]property.Value{strVal("https://app.example.com/callback")}),
			}),
		})
	}

	t.Run("present", func(t *testing.T) {
		dex := newFakeDex()
		dex.clients["client"] = &api.Client{Id: "client", Name: "Remote Name"}
		resp, err := read(newProviderAgainst(t, dex))
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		if resp.ID != "client" {
			t.Fatalf("ID = %q, want %q", resp.ID, "client")
		}
		if got := resp.Properties.Get("name"); got.AsString() != "Remote Name" {
			t.Errorf("name = %v, want remote value", got)
		}
	})

	t.Run("deleted outside Pulumi is reported absent", func(t *testing.T) {
		resp, err := read(newProviderAgainst(t, newFakeDex()))
		if err != nil {
			t.Fatalf("Read of a deleted client must signal absence, not fail: %v", err)
		}
		if resp.ID != "" {
			t.Errorf("ID = %q, want empty (absent)", resp.ID)
		}
	})

	t.Run("lookup failure with unverifiable absence keeps state", func(t *testing.T) {
		dex := newFakeDex()
		dex.listClientsErr = status.Error(codes.Unavailable, "storage unavailable")
		if _, err := read(newProviderAgainst(t, dex)); err == nil {
			t.Fatal("Read must fail when absence cannot be confirmed")
		}
	})

	for _, code := range []codes.Code{codes.PermissionDenied, codes.Unauthenticated, codes.DeadlineExceeded, codes.Unavailable} {
		t.Run(code.String()+" is an error, not absence", func(t *testing.T) {
			dex := newFakeDex()
			dex.getClientErr = status.Error(code, "injected")
			if _, err := read(newProviderAgainst(t, dex)); err == nil {
				t.Fatalf("Read must fail on %s", code)
			}
		})
	}
}

func TestConnector_Read_listFailureIsNotAbsence(t *testing.T) {
	dex := newFakeDex()
	dex.listConnectorsErr = status.Error(codes.PermissionDenied, "injected")
	_, err := newProviderAgainst(t, dex).Read(p.ReadRequest{
		ID:  "conn",
		Urn: testURN("dex:resources:LocalConnector"),
		Properties: property.NewMap(map[string]property.Value{
			"connectorId": strVal("conn"),
			"name":        strVal("Conn"),
		}),
	})
	if err == nil {
		t.Fatal("Read must fail when ListConnectors fails")
	}
}

func TestDelete_alreadyAbsentIsSuccess(t *testing.T) {
	s := newProviderAgainst(t, newFakeDex())
	if err := s.Delete(p.DeleteRequest{
		ID:  "client",
		Urn: testURN("dex:resources:Client"),
		Properties: property.NewMap(map[string]property.Value{
			"clientId":     strVal("client"),
			"name":         strVal("Client"),
			"redirectUris": property.New([]property.Value{strVal("https://app.example.com/callback")}),
		}),
	}); err != nil {
		t.Errorf("Client delete of absent object: %v", err)
	}
	if err := s.Delete(p.DeleteRequest{
		ID:  "conn",
		Urn: testURN("dex:resources:Connector"),
		Properties: property.NewMap(map[string]property.Value{
			"connectorId": strVal("conn"),
			"type":        strVal("ldap"),
			"name":        strVal("Conn"),
		}),
	}); err != nil {
		t.Errorf("Connector delete of absent object: %v", err)
	}
}
