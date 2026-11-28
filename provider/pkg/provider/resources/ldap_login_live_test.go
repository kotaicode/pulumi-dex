//go:build dexlive

package resources

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"

	api "github.com/dexidp/dex/api/v2"
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/integration"
	"github.com/pulumi/pulumi/sdk/v3/go/property"

	"github.com/kotaicode/pulumi-dex/pkg/provider"
)

// =============================================================================
// LDAP logins through Dex
//
// Dex stores an LDAP connector without checking its configuration, so only a
// login proves that it works. These tests create a client and an LdapConnector
// through the provider and log in through Dex's password grant, against an
// OpenLDAP server seeded with test/ldap/seed.ldif.
//
// Dex must name the connector in oauth2.passwordConnector (LDAP_LIVE_CONNECTOR,
// default "ldap-login"). The LDAP addresses are as Dex sees them:
//
//	DEX_LIVE_ISSUER             Dex's issuer URL, e.g. http://127.0.0.1:5556/dex
//	LDAP_LIVE_HOST              host:port with StartTLS
//	LDAP_LIVE_TLS_HOST          host:port with LDAPS
//	LDAP_LIVE_NO_STARTTLS_HOST  host:port of a server without TLS
//	LDAP_LIVE_CA, LDAP_LIVE_WRONG_CA  PEM files: the CA of the LDAP server's
//	                            certificate, and one that did not sign it
// =============================================================================

type ldapLoginEnv struct {
	issuer, connectorID              string
	startTLSHost, tlsHost, noTLSHost string
	ca, wrongCA                      string
}

func ldapLoginEnvFrom(t *testing.T) ldapLoginEnv {
	t.Helper()
	env := ldapLoginEnv{
		issuer: os.Getenv("DEX_LIVE_ISSUER"), connectorID: os.Getenv("LDAP_LIVE_CONNECTOR"),
		startTLSHost: os.Getenv("LDAP_LIVE_HOST"), tlsHost: os.Getenv("LDAP_LIVE_TLS_HOST"),
		noTLSHost: os.Getenv("LDAP_LIVE_NO_STARTTLS_HOST"),
	}
	if env.connectorID == "" {
		env.connectorID = "ldap-login"
	}
	if env.issuer == "" || env.startTLSHost == "" || env.tlsHost == "" || env.noTLSHost == "" ||
		os.Getenv("LDAP_LIVE_CA") == "" || os.Getenv("LDAP_LIVE_WRONG_CA") == "" {
		if os.Getenv("CI") == "true" {
			t.Fatal("the LDAP login tests need DEX_LIVE_ISSUER and the LDAP_LIVE_* variables")
		}
		t.Skip("DEX_LIVE_ISSUER and the LDAP_LIVE_* variables are not set")
	}
	for _, f := range []struct {
		dst *string
		env string
	}{{&env.ca, "LDAP_LIVE_CA"}, {&env.wrongCA, "LDAP_LIVE_WRONG_CA"}} {
		b, err := os.ReadFile(os.Getenv(f.env)) //nolint:gosec // G703: the path is set by whoever runs the tests
		if err != nil {
			t.Fatalf("%s: %v", f.env, err)
		}
		*f.dst = string(b)
	}
	return env
}

// Synthetic credentials from test/ldap/seed.ldif.
const (
	ldapBindDN       = "cn=dex-reader,ou=services,dc=example,dc=org"
	ldapBindPassword = "reader$HOME$1-synthetic" // "$" must reach the LDAP server as written
	ldapClientSecret = "live-synthetic-ldap-client-secret"
)

// ldapProgram is an LdapConnector for the seeded directory; extra replaces
// top-level inputs.
func ldapProgram(env ldapLoginEnv, extra map[string]property.Value) map[string]property.Value {
	return with(map[string]property.Value{
		"connectorId": str(env.connectorID), "name": str("LDAP login test"),
		"userSearch": obj(map[string]property.Value{
			"baseDn":             str("dc=example,dc=org"),
			"filter":             str("(&(objectClass=inetOrgPerson)(!(employeeType=excluded)))"),
			"usernameAttributes": strs("uid"),
			"idAttr":             str("uid"), "emailAttr": str("mail"), "nameAttr": str("cn"),
		}),
		"groupSearch": obj(map[string]property.Value{
			"baseDn": str("ou=groups,dc=example,dc=org"), "filter": str("(objectClass=groupOfNames)"), "nameAttr": str("cn"),
			"userMatchers": property.New([]property.Value{obj(map[string]property.Value{
				"userAttr": str("DN"), "groupAttr": str("member"), "recursionGroupAttr": str("member"),
			})}),
		}),
	}, extra)
}

// ldapConnectorUnderTest applies programs to the connector named in Dex's
// passwordConnector: it creates the connector once and updates it afterwards.
// Dex v2.45 keeps a connector that is created again under the same ID cached
// with its old configuration, so it is never deleted between scenarios.
type ldapConnectorUnderTest struct {
	s       integration.Server
	state   property.Map
	inputs  property.Map
	created bool
}

func (c *ldapConnectorUnderTest) apply(t *testing.T, program map[string]property.Value) {
	t.Helper()
	urn := testURN(tokenLdapConnector)
	checked, err := c.s.Check(p.CheckRequest{Urn: urn, State: c.inputs, Inputs: property.NewMap(program)})
	if err != nil || len(checked.Failures) > 0 {
		t.Fatalf("Check: %v %v", err, checked.Failures)
	}
	id := program["connectorId"].AsString()
	if !c.created {
		created, err := c.s.Create(p.CreateRequest{Urn: urn, Properties: checked.Inputs})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		c.state, c.created = created.Properties, true
	} else {
		updated, err := c.s.Update(p.UpdateRequest{ID: id, Urn: urn, State: c.state, OldInputs: c.inputs, Inputs: checked.Inputs})
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
		c.state = updated.Properties
	}
	c.inputs = checked.Inputs
}

// login runs Dex's password grant and returns the ID token's claims, or nil
// and the HTTP status when Dex refuses.
func (env ldapLoginEnv) login(t *testing.T, clientID, username, password string) (claims *ldapClaims, status int) {
	t.Helper()
	form := url.Values{"grant_type": {"password"}, "username": {username}, "password": {password}, "scope": {"openid email profile groups"}}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, env.issuer+"/token", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(clientID, ldapClientSecret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("token request: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var tok struct {
		IDToken string `json:"id_token"`
	}
	if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &tok) != nil || tok.IDToken == "" {
		return nil, resp.StatusCode
	}
	parts := strings.Split(tok.IDToken, ".")
	if len(parts) != 3 {
		t.Fatalf("ID token has %d parts", len(parts))
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	claims = &ldapClaims{}
	if err := json.Unmarshal(payload, claims); err != nil {
		t.Fatal(err)
	}
	return claims, resp.StatusCode
}

type ldapClaims struct {
	Email  string   `json:"email"`
	Name   string   `json:"name"`
	Groups []string `json:"groups"`
}

func TestLive_LdapLogin(t *testing.T) {
	env := ldapLoginEnvFrom(t)
	s := liveProvider(t)
	minor := liveMinor(t)

	clientID := liveID("ldap-client")
	clientURN := testURN("dex:resources:Client")
	client, err := s.Check(p.CheckRequest{Urn: clientURN, Inputs: property.NewMap(map[string]property.Value{
		"clientId": str(clientID), "name": str("LDAP login test"), "secret": str(ldapClientSecret),
		"redirectUris": strs("http://127.0.0.1/callback"),
	})})
	if err != nil || len(client.Failures) > 0 {
		t.Fatalf("Check client: %v %v", err, client.Failures)
	}
	if _, err := s.Create(p.CreateRequest{Urn: clientURN, Properties: client.Inputs}); err != nil {
		t.Fatalf("Create client: %v", err)
	}
	raw := &provider.DexConfig{Host: os.Getenv("DEX_LIVE_HOST"), Plaintext: ptrBool(true)}
	if err := raw.Configure(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = raw.Client.DeleteClient(ctx, &api.DeleteClientReq{Id: clientID})              //nolint:errcheck // best-effort cleanup
		_, _ = raw.Client.DeleteConnector(ctx, &api.DeleteConnectorReq{Id: env.connectorID}) //nolint:errcheck // best-effort cleanup
	})

	conn := &ldapConnectorUnderTest{s: s}
	loginOK := func(t *testing.T, user, password string, wantGroups ...string) {
		t.Helper()
		claims, status := env.login(t, clientID, user, password)
		if claims == nil {
			t.Errorf("login as %s refused (HTTP %d)", user, status)
			return
		}
		got := slices.Sorted(slices.Values(claims.Groups))
		if !slices.Equal(got, wantGroups) {
			t.Errorf("login as %s: groups %v, want %v", user, got, wantGroups)
		}
		if claims.Email != user+"@example.org" {
			t.Errorf("login as %s: email %q", user, claims.Email)
		}
	}
	refused := func(t *testing.T, user, password string) {
		t.Helper()
		if claims, _ := env.login(t, clientID, user, password); claims != nil {
			t.Errorf("login as %s succeeded with groups %v, want it refused", user, claims.Groups)
		}
	}
	ldaps := map[string]property.Value{"host": str(env.tlsHost), "rootCaPem": str(env.ca)}

	t.Run("LDAPS with bound search", func(t *testing.T) {
		conn.apply(t, ldapProgram(env, with(ldaps, map[string]property.Value{
			"bindDn": str(ldapBindDN), "bindPassword": str(ldapBindPassword),
		})))
		loginOK(t, "alice", "alice-synthetic-password", "devs")
		loginOK(t, "bob", "bob-synthetic-password", "inner", "outer")       // nested groups
		loginOK(t, "dave", "dave-synthetic-password", "cycle-a", "cycle-b") // a group cycle ends
		refused(t, "alice", "wrong-password")
		refused(t, "carol", "carol-synthetic-password") // excluded by the user filter
		refused(t, "dup", "dup-synthetic-password")     // two entries match
	})

	t.Run("wrong bind password", func(t *testing.T) {
		conn.apply(t, ldapProgram(env, with(ldaps, map[string]property.Value{
			"bindDn": str(ldapBindDN), "bindPassword": str("reader/HOME/1-synthetic"),
		})))
		refused(t, "alice", "alice-synthetic-password")
	})

	t.Run("LDAPS with a CA that did not sign the server certificate", func(t *testing.T) {
		conn.apply(t, ldapProgram(env, map[string]property.Value{"host": str(env.tlsHost), "rootCaPem": str(env.wrongCA)}))
		refused(t, "alice", "alice-synthetic-password")
	})

	t.Run("StartTLS with anonymous search", func(t *testing.T) {
		conn.apply(t, ldapProgram(env, map[string]property.Value{
			"host": str(env.startTLSHost), "startTls": property.New(true), "rootCaPem": str(env.ca),
		}))
		loginOK(t, "alice", "alice-synthetic-password", "devs")
		refused(t, "alice", "wrong-password")
	})

	t.Run("StartTLS against a server without it", func(t *testing.T) {
		conn.apply(t, ldapProgram(env, map[string]property.Value{
			"host": str(env.noTLSHost), "startTls": property.New(true), "rootCaPem": str(env.ca),
		}))
		refused(t, "alice", "alice-synthetic-password")
	})

	t.Run("several username attributes", func(t *testing.T) {
		if minor < 46 {
			t.Skip("needs Dex v2.46")
		}
		program := ldapProgram(env, ldaps)
		program["userSearch"] = obj(with(program["userSearch"].AsMap().AsMap(), map[string]property.Value{
			"usernameAttributes": strs("uid", "mail"),
		}))
		conn.apply(t, program)
		loginOK(t, "alice", "alice-synthetic-password", "devs")
		claims, status := env.login(t, clientID, "alice@example.org", "alice-synthetic-password")
		if claims == nil || claims.Email != "alice@example.org" {
			t.Errorf("login with the mail attribute: claims %v, HTTP %d", claims, status)
		}
	})

	if err := s.Delete(p.DeleteRequest{ID: env.connectorID, Urn: testURN(tokenLdapConnector), Properties: conn.state}); err != nil {
		t.Errorf("Delete connector: %v", err)
	}
}
