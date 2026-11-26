package resources

import (
	"context"
	"errors"
	"net"
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
// RPC outcome tests against an in-process fake Dex gRPC server (DEX-006)
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
}

func newFakeDex() *fakeDex {
	return &fakeDex{clients: map[string]*api.Client{}, connectors: map[string]*api.Connector{}}
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
	c := req.GetClient()
	if _, ok := f.clients[c.GetId()]; ok {
		return &api.CreateClientResp{AlreadyExists: true}, nil
	}
	f.clients[c.GetId()] = &api.Client{
		Id: c.GetId(), Secret: c.GetSecret(), Name: c.GetName(), LogoUrl: c.GetLogoUrl(), Public: c.GetPublic(),
		RedirectUris: append([]string(nil), c.GetRedirectUris()...),
		TrustedPeers: append([]string(nil), c.GetTrustedPeers()...),
	}
	return &api.CreateClientResp{Client: c}, nil
}

func (f *fakeDex) UpdateClient(_ context.Context, req *api.UpdateClientReq) (*api.UpdateClientResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.clients[req.Id]
	if !ok {
		return &api.UpdateClientResp{NotFound: true}, nil
	}
	c.Name = req.Name
	return &api.UpdateClientResp{}, nil
}

func (f *fakeDex) DeleteClient(_ context.Context, req *api.DeleteClientReq) (*api.DeleteClientResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
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
	c := req.GetConnector()
	if _, ok := f.connectors[c.GetId()]; ok {
		return &api.CreateConnectorResp{AlreadyExists: true}, nil
	}
	f.connectors[c.GetId()] = &api.Connector{
		Id: c.GetId(), Type: c.GetType(), Name: c.GetName(), Config: append([]byte(nil), c.GetConfig()...),
	}
	return &api.CreateConnectorResp{}, nil
}

func (f *fakeDex) UpdateConnector(_ context.Context, req *api.UpdateConnectorReq) (*api.UpdateConnectorResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.connectors[req.Id]
	if !ok {
		return &api.UpdateConnectorResp{NotFound: true}, nil
	}
	c.Name = req.NewName
	return &api.UpdateConnectorResp{}, nil
}

func (f *fakeDex) DeleteConnector(_ context.Context, req *api.DeleteConnectorReq) (*api.DeleteConnectorResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
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
