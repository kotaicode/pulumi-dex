package provider

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	api "github.com/dexidp/dex/api/v2"
	p "github.com/pulumi/pulumi-go-provider"
	presource "github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const testSchema = `{
  "resources": {
    "dex:resources:Connector": {
      "inputProperties": {
        "connectorId": {"type": "string"},
        "rawConfig":   {"type": "string", "secret": true},
        "oidcConfig":  {"$ref": "#/types/dex:resources:OIDCConfig"}
      }
    }
  },
  "types": {
    "dex:resources:OIDCConfig": {
      "properties": {
        "issuer":       {"type": "string"},
        "clientSecret": {"type": "string", "secret": true},
        "extra":        {"type": "object", "secret": true}
      }
    }
  }
}`

func readOnlyProvider(resp p.ReadResponse, schema string, schemaErr error) p.Provider {
	return p.Provider{
		GetSchema: func(context.Context, p.GetSchemaRequest) (p.GetSchemaResponse, error) {
			return p.GetSchemaResponse{Schema: schema}, schemaErr
		},
		Read: func(context.Context, p.ReadRequest) (p.ReadResponse, error) {
			return resp, nil
		},
	}
}

var connectorURN = presource.URN("urn:pulumi:test::test::dex:resources:Connector::r")

// Import calls Read without prior inputs, so nothing in the returned inputs is
// secret yet; the wrapper must mark exactly the schema's secret paths.
func TestWithSecretReadInputs_importMarksSchemaSecrets(t *testing.T) {
	inputs := property.NewMap(map[string]property.Value{
		"connectorId": property.New("conn"),
		"rawConfig":   property.New(`{"bindPW":"synthetic-canary"}`),
		"oidcConfig": property.New(map[string]property.Value{
			"issuer":       property.New("https://issuer.example.com"),
			"clientSecret": property.New("synthetic-canary"),
			"extra":        property.New(map[string]property.Value{"k": property.New("synthetic-canary")}),
		}),
	})
	prov := WithSecretReadInputs(readOnlyProvider(p.ReadResponse{ID: "conn", Inputs: inputs}, testSchema, nil))

	resp, err := prov.Read(context.Background(), p.ReadRequest{ID: "conn", Urn: connectorURN})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	got := resp.Inputs
	if !got.Get("rawConfig").IsSecret() {
		t.Error("rawConfig input not secret")
	}
	if got.Get("connectorId").IsSecret() {
		t.Error("connectorId must stay plain")
	}
	oidc := got.Get("oidcConfig")
	if oidc.IsSecret() {
		t.Error("oidcConfig input as a whole must stay plain; only its secret fields are marked")
	}
	if !oidc.AsMap().Get("clientSecret").IsSecret() || !oidc.AsMap().Get("extra").IsSecret() {
		t.Error("nested secret fields of oidcConfig not marked")
	}
	if oidc.AsMap().Get("issuer").IsSecret() {
		t.Error("oidcConfig.issuer must stay plain")
	}
}

func TestWithSecretReadInputs_absentResourcePassesThrough(t *testing.T) {
	prov := WithSecretReadInputs(readOnlyProvider(p.ReadResponse{}, "not json", nil))
	resp, err := prov.Read(context.Background(), p.ReadRequest{ID: "conn", Urn: connectorURN})
	if err != nil || resp.ID != "" {
		t.Fatalf("absent resource must pass through untouched, got ID=%q err=%v", resp.ID, err)
	}
}

func TestWithSecretReadInputs_unclassifiableFailsClosed(t *testing.T) {
	inputs := property.NewMap(map[string]property.Value{"rawConfig": property.New("synthetic-canary")})
	for name, prov := range map[string]p.Provider{
		"schema error":   readOnlyProvider(p.ReadResponse{ID: "conn", Inputs: inputs}, "", errors.New("boom")),
		"invalid schema": readOnlyProvider(p.ReadResponse{ID: "conn", Inputs: inputs}, "not json", nil),
	} {
		t.Run(name, func(t *testing.T) {
			resp, err := WithSecretReadInputs(prov).Read(context.Background(), p.ReadRequest{ID: "conn", Urn: connectorURN})
			if err == nil {
				t.Fatal("expected an error instead of returning unclassified inputs")
			}
			if resp.Inputs.Len() != 0 {
				t.Error("inputs must not be returned on failure")
			}
		})
	}
}

// echoDex fails every write by echoing the request, as a storage backend that quotes
// the rejected object might.
type echoDex struct {
	api.UnimplementedDexServer
}

func (echoDex) CreateConnector(_ context.Context, req *api.CreateConnectorReq) (*api.CreateConnectorResp, error) {
	return nil, status.Errorf(codes.InvalidArgument, "create connector: storage rejected %s", req.GetConnector().GetConfig())
}

func (echoDex) CreateClient(_ context.Context, req *api.CreateClientReq) (*api.CreateClientResp, error) {
	return nil, status.Errorf(codes.Unknown, "create client: duplicate secret %q", req.GetClient().GetSecret())
}

// TestConfigure_redactsCredentialsFromRPCErrors drives the real Configure path, so
// it also proves the interceptor is installed on the connection.
func TestConfigure_redactsCredentialsFromRPCErrors(t *testing.T) {
	const canary = "synthetic-canary-secret"
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := grpc.NewServer()
	api.RegisterDexServer(srv, echoDex{})
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(lis) }()
	t.Cleanup(func() {
		srv.Stop()
		if err := <-serveErr; err != nil {
			t.Errorf("echo server: %v", err)
		}
	})

	cfg := &DexConfig{Host: lis.Addr().String()}
	if err := cfg.Configure(t.Context()); err != nil {
		t.Fatalf("Configure: %v", err)
	}

	_, err = cfg.Client.CreateConnector(t.Context(), &api.CreateConnectorReq{Connector: &api.Connector{
		Id: "ldap", Type: "ldap", Name: "LDAP",
		Config: []byte(`{"host":"ldap.example.com:636","bindPW":"` + canary + `"}`),
	}})
	assertRedacted(t, err, canary, codes.InvalidArgument, "create connector: storage rejected")

	_, err = cfg.Client.CreateClient(t.Context(), &api.CreateClientReq{Client: &api.Client{Id: "app", Secret: canary}})
	assertRedacted(t, err, canary, codes.Unknown, "create client: duplicate secret")
}

func assertRedacted(t *testing.T, err error, canary string, code codes.Code, keep string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), canary) {
		t.Errorf("error leaks the credential: %v", err)
	}
	if !strings.Contains(err.Error(), keep) || !strings.Contains(err.Error(), "[secret]") {
		t.Errorf("error should keep its context and show the redaction, got: %v", err)
	}
	if got := status.Code(err); got != code {
		t.Errorf("status code = %v, want %v (callers classify errors by code)", got, code)
	}
}

func TestRedactRPCErrors_passThrough(t *testing.T) {
	orig := status.Error(codes.PermissionDenied, "permission denied")
	invoker := func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error { return orig }

	// No credentials in the request, or credentials not echoed: same error value.
	for name, req := range map[string]any{
		"read request":       &api.ListClientReq{},
		"secret not echoed":  &api.CreateClientReq{Client: &api.Client{Secret: "synthetic-canary-secret"}},
		"empty config":       &api.UpdateConnectorReq{Id: "x"},
		"short config value": &api.UpdateConnectorReq{NewConfig: []byte(`{"scope":"email"}`)},
	} {
		t.Run(name, func(t *testing.T) {
			if err := redactRPCErrors(context.Background(), "/m", req, nil, nil, invoker); err != orig { //nolint:errorlint // identity is the contract
				t.Errorf("error was rewritten: %v", err)
			}
		})
	}
}
