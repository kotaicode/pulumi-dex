package resources

import (
	"strings"
	"testing"

	api "github.com/dexidp/dex/api/v2"
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// =============================================================================
// Lookup functions: read-only, a missing object is an error naming the ID,
// other failures keep their gRPC status, secrets are marked.
// =============================================================================

func invoke(t *testing.T, dex *fakeDex, fn string, args map[string]property.Value) (property.Map, error) {
	t.Helper()
	resp, err := newProviderAgainst(t, dex).Invoke(p.InvokeRequest{Token: tokens.Type("dex:resources:" + fn), Args: property.NewMap(args)})
	if err == nil && len(resp.Failures) > 0 {
		t.Fatalf("%s: argument failures %v", fn, resp.Failures)
	}
	return resp.Return, err
}

func seededDex() *fakeDex {
	dex := newFakeDex()
	dex.clients["web"] = &api.Client{Id: "web", Name: "Web", Secret: canary + "-client", RedirectUris: []string{"https://app.example.com/cb"}, Public: true}
	dex.connectors["ldap"] = &api.Connector{Id: "ldap", Type: "ldap", Name: "LDAP", Config: []byte(`{"host":"ldap.example.com:636","bindPW":"` + canary + `"}`)}
	dex.passwords["alice@example.com"] = &api.Password{Email: "alice@example.com", Hash: []byte(testPasswordHash), UserId: "u-alice", Username: "Alice", Groups: []string{"admins"}}
	return dex
}

func TestFunctions_found(t *testing.T) {
	dex := seededDex()
	dex.mu.Lock()
	before := dex.writes
	dex.mu.Unlock()

	c, err := invoke(t, dex, "getClient", map[string]property.Value{"id": str("web")})
	if err != nil {
		t.Fatalf("getClient: %v", err)
	}
	if c.Get("name").AsString() != "Web" || !c.Get("public").AsBool() || c.Get("redirectUris").AsArray().Len() != 1 {
		t.Errorf("getClient result = %v", c)
	}
	if s := c.Get("secret"); !s.IsSecret() || s.AsString() != canary+"-client" {
		t.Error("getClient must return the client secret as a secret")
	}

	k, err := invoke(t, dex, "getConnector", map[string]property.Value{"id": str("ldap")})
	if err != nil {
		t.Fatalf("getConnector: %v", err)
	}
	if k.Get("type").AsString() != "ldap" || k.Get("name").AsString() != "LDAP" {
		t.Errorf("getConnector result = %v", k)
	}
	if cfg := k.Get("rawConfig"); !cfg.IsSecret() || !strings.Contains(cfg.AsString(), "ldap.example.com") {
		t.Error("getConnector must return the configuration as a secret")
	}

	u, err := invoke(t, dex, "getLocalUser", map[string]property.Value{"email": str("Alice@Example.com")})
	if err != nil {
		t.Fatalf("getLocalUser: %v", err)
	}
	if u.Get("userId").AsString() != "u-alice" || u.Get("username").AsString() != "Alice" || u.Get("groups").AsArray().Len() != 1 {
		t.Errorf("getLocalUser result = %v", u)
	}
	if _, ok := u.GetOk("passwordHash"); ok {
		t.Error("getLocalUser must not return a password hash")
	}
	assertNoPlainCanary(t, "getClient", property.New(c))
	assertNoPlainCanary(t, "getConnector", property.New(k))

	dex.mu.Lock()
	defer dex.mu.Unlock()
	if dex.writes != before {
		t.Errorf("lookups wrote %d times to Dex", dex.writes-before)
	}
}

func TestFunctions_notFoundNamesTheID(t *testing.T) {
	for fn, args := range map[string]map[string]property.Value{
		"getClient":    {"id": str("missing-client")},
		"getConnector": {"id": str("missing-connector")},
		"getLocalUser": {"email": str("missing@example.com")},
	} {
		t.Run(fn, func(t *testing.T) {
			_, err := invoke(t, seededDex(), fn, args)
			want := args["id"]
			if want.IsNull() {
				want = args["email"]
			}
			if err == nil || !strings.Contains(err.Error(), want.AsString()) || !strings.Contains(err.Error(), "no ") {
				t.Errorf("%s of a missing object = %v, want a not-found error naming it", fn, err)
			}
		})
	}
}

// A failure other than absence keeps its gRPC status and is not reported as
// "not found".
func TestFunctions_otherErrorsStayDistinguishable(t *testing.T) {
	denied := status.Error(codes.PermissionDenied, "not allowed")
	unavailable := status.Error(codes.Unavailable, "connection refused")
	for name, tc := range map[string]struct {
		fn   string
		args map[string]property.Value
		set  func(*fakeDex)
		code codes.Code
	}{
		"getClient permission":     {"getClient", map[string]property.Value{"id": str("web")}, func(d *fakeDex) { d.getClientErr = denied }, codes.PermissionDenied},
		"getConnector unavailable": {"getConnector", map[string]property.Value{"id": str("ldap")}, func(d *fakeDex) { d.listConnectorsErr = unavailable }, codes.Unavailable},
		"getLocalUser permission":  {"getLocalUser", map[string]property.Value{"email": str("alice@example.com")}, func(d *fakeDex) { d.listPasswordsErr = denied }, codes.PermissionDenied},
	} {
		t.Run(name, func(t *testing.T) {
			dex := seededDex()
			tc.set(dex)
			_, err := invoke(t, dex, tc.fn, tc.args)
			if err == nil || strings.Contains(err.Error(), "no ") && strings.Contains(err.Error(), "exists in Dex") {
				t.Fatalf("%s = %v, want the underlying failure", tc.fn, err)
			}
			if !strings.Contains(err.Error(), tc.code.String()) {
				t.Errorf("%s error %q should name %s", tc.fn, err, tc.code)
			}
		})
	}
}
