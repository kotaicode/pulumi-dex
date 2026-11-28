package resources

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"

	api "github.com/dexidp/dex/api/v2"
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

// =============================================================================
// LdapConnector: checks at preview, wire encoding, round trip and strict read
// =============================================================================

// testCAPEM returns a self-signed CA certificate in PEM.
func testCAPEM(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "pulumi-dex test CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func ldapInputs(extra map[string]property.Value) map[string]property.Value {
	return with(map[string]property.Value{
		"connectorId": str("ldap"), "name": str("LDAP"), "host": str("ldap.example.com:636"),
		"userSearch": obj(map[string]property.Value{"baseDn": str("ou=people,dc=example,dc=org"), "usernameAttributes": strs("uid")}),
	}, extra)
}

func ldapUserSearch(extra map[string]property.Value) property.Value {
	return obj(with(map[string]property.Value{"baseDn": str("ou=people,dc=example,dc=org"), "usernameAttributes": strs("uid")}, extra))
}

func TestLdapConnector_checksAtPreview(t *testing.T) {
	unknown := property.New(property.Computed)
	secret := property.New(canary).WithSecret(true)
	for name, tc := range map[string]struct {
		extra map[string]property.Value
		fails string // property that fails, "" for none
	}{
		"minimal":                     {nil, ""},
		"empty host":                  {map[string]property.Value{"host": str("")}, "host"},
		"empty userSearch.baseDn":     {map[string]property.Value{"userSearch": ldapUserSearch(map[string]property.Value{"baseDn": str("")})}, "userSearch.baseDn"},
		"no username attribute":       {map[string]property.Value{"userSearch": ldapUserSearch(map[string]property.Value{"usernameAttributes": strs()})}, "userSearch.usernameAttributes"},
		"two username attributes":     {map[string]property.Value{"userSearch": ldapUserSearch(map[string]property.Value{"usernameAttributes": strs("uid", "mail")})}, ""},
		"insecureNoSsl and startTls":  {map[string]property.Value{"host": str("ldap.example.com:389"), "insecureNoSsl": property.New(true), "startTls": property.New(true)}, "startTls"},
		"insecureNoSsl, startTls off": {map[string]property.Value{"host": str("ldap.example.com:389"), "insecureNoSsl": property.New(true), "startTls": property.New(false)}, ""},
		"skipVerify without TLS":      {map[string]property.Value{"host": str("ldap.example.com:389"), "insecureNoSsl": property.New(true), "insecureSkipVerify": property.New(true)}, "insecureSkipVerify"},
		"skipVerify with LDAPS":       {map[string]property.Value{"insecureSkipVerify": property.New(true)}, ""},
		"startTls without port":       {map[string]property.Value{"host": str("ldap.example.com"), "startTls": property.New(true)}, "host"},
		"startTls with port":          {map[string]property.Value{"host": str("ldap.example.com:389"), "startTls": property.New(true)}, ""},
		"LDAPS without port":          {map[string]property.Value{"host": str("ldap.example.com")}, ""},
		"rootCa and rootCaPem":        {map[string]property.Value{"rootCa": str("/etc/dex/ca.pem"), "rootCaPem": str(testCAPEM(t))}, "rootCaPem"},
		"rootCaPem":                   {map[string]property.Value{"rootCaPem": str(testCAPEM(t))}, ""},
		"rootCaPem not PEM":           {map[string]property.Value{"rootCaPem": str(canary)}, "rootCaPem"},
		"clientCert without key":      {map[string]property.Value{"clientCert": str("/etc/dex/client.pem")}, "clientKey"},
		"clientKey without cert":      {map[string]property.Value{"clientKey": str("/etc/dex/client.key")}, "clientCert"},
		"client cert and key":         {map[string]property.Value{"clientCert": str("/etc/dex/client.pem"), "clientKey": str("/etc/dex/client.key")}, ""},
		"bindDn without password":     {map[string]property.Value{"bindDn": str("cn=reader,dc=example,dc=org")}, "bindPassword"},
		"password without bindDn":     {map[string]property.Value{"bindPassword": secret}, "bindDn"},
		"bound search":                {map[string]property.Value{"bindDn": str("cn=reader,dc=example,dc=org"), "bindPassword": secret}, ""},
		"anonymous search":            {map[string]property.Value{"bindDn": str(""), "bindPassword": str("")}, ""},
		// Unknown values are not judged.
		"unknown host with startTls":   {map[string]property.Value{"host": unknown, "startTls": property.New(true)}, ""},
		"unknown startTls":             {map[string]property.Value{"host": str("ldap.example.com"), "startTls": unknown}, ""},
		"unknown bindPassword":         {map[string]property.Value{"bindDn": str("cn=reader,dc=example,dc=org"), "bindPassword": unknown}, ""},
		"unknown rootCaPem":            {map[string]property.Value{"rootCa": str("/etc/dex/ca.pem"), "rootCaPem": unknown}, ""},
		"unknown usernameAttributes":   {map[string]property.Value{"userSearch": ldapUserSearch(map[string]property.Value{"usernameAttributes": unknown})}, ""},
		"unknown userSearch":           {map[string]property.Value{"userSearch": unknown}, ""},
		"unknown insecureNoSsl, skipV": {map[string]property.Value{"insecureNoSsl": unknown, "insecureSkipVerify": property.New(true)}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			failures := checkFailures(t, tokenLdapConnector, ldapInputs(tc.extra))
			assertNoValueInFailures(t, failures)
			if tc.fails == "" {
				if len(failures) > 0 {
					t.Errorf("unexpected failures: %v", failures)
				}
				return
			}
			if len(failures) != 1 || failures[0].Property != tc.fails {
				t.Errorf("failures = %v, want one on %s", failures, tc.fails)
			}
		})
	}
}

// storedLdapConfig creates the connector through the provider and returns the
// config Dex stored.
func storedLdapConfig(t *testing.T, dex *fakeDex, inputs map[string]property.Value) map[string]any {
	t.Helper()
	_, created, _ := createFor(t, dex, tokenLdapConnector, inputs)
	dex.mu.Lock()
	defer dex.mu.Unlock()
	var config map[string]any
	if err := json.Unmarshal(dex.connectors[created.ID].Config, &config); err != nil {
		t.Fatal(err)
	}
	if dex.connectors[created.ID].Type != "ldap" {
		t.Errorf("type = %q, want ldap", dex.connectors[created.ID].Type)
	}
	return config
}

func TestLdapConnector_wireEncoding(t *testing.T) {
	ca := testCAPEM(t)
	config := storedLdapConfig(t, newFakeDex(), ldapInputs(map[string]property.Value{
		"host": str("ldap.example.com:389"), "startTls": property.New(true), "insecureSkipVerify": property.New(false),
		"rootCaPem": str(ca), "clientCert": str("/etc/dex/client.pem"), "clientKey": str("/etc/dex/client.key"),
		"bindDn": str("cn=reader,dc=example,dc=org"), "bindPassword": str("pa$$word$HOME"), "usernamePrompt": str("Email"),
		"userSearch": ldapUserSearch(map[string]property.Value{
			"filter": str("(objectClass=person)"), "scope": str("one"), "idAttr": str("uid"), "emailAttr": str("mail"),
			"nameAttr": str("cn"), "preferredUsernameAttr": str("uid"), "emailSuffix": str("example.org"),
		}),
		"groupSearch": obj(map[string]property.Value{
			"baseDn": str("ou=groups,dc=example,dc=org"), "filter": str("(objectClass=groupOfNames)"), "scope": str("sub"),
			"nameAttr": str("cn"), "userMatchers": property.New([]property.Value{obj(map[string]property.Value{
				"userAttr": str("DN"), "groupAttr": str("member"), "recursionGroupAttr": str("member"),
			})}),
		}),
	}))
	want := map[string]any{
		"host": "ldap.example.com:389", "startTLS": true, "insecureSkipVerify": false,
		"rootCAData": jsonBase64(ca), "clientCert": "/etc/dex/client.pem", "clientKey": "/etc/dex/client.key",
		"bindDN": "cn=reader,dc=example,dc=org", "bindPW": "pa$$word$HOME", "usernamePrompt": "Email",
		"userSearch": map[string]any{
			"baseDN": "ou=people,dc=example,dc=org", "username": "uid", "filter": "(objectClass=person)", "scope": "one",
			"idAttr": "uid", "emailAttr": "mail", "nameAttr": "cn", "preferredUsernameAttr": "uid", "emailSuffix": "example.org",
		},
		"groupSearch": map[string]any{
			"baseDN": "ou=groups,dc=example,dc=org", "filter": "(objectClass=groupOfNames)", "scope": "sub", "nameAttr": "cn",
			"userMatchers": []any{map[string]any{"userAttr": "DN", "groupAttr": "member", "recursionGroupAttr": "member"}},
		},
	}
	if got, wantJSON := mustJSON(t, config), mustJSON(t, want); !jsonEqual([]byte(got), []byte(wantJSON)) {
		t.Errorf("stored config:\n got %s\nwant %s", got, wantJSON)
	}

	minimal := mustJSON(t, storedLdapConfig(t, newFakeDex(), ldapInputs(nil)))
	if !jsonEqual([]byte(minimal), []byte(`{"host":"ldap.example.com:636","userSearch":{"baseDN":"ou=people,dc=example,dc=org","username":"uid"}}`)) {
		t.Errorf("minimal config = %s: unset fields must be left out", minimal)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func jsonBase64(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

// storedUsername returns userSearch.username from a stored LDAP config, as JSON.
func storedUsername(t *testing.T, config map[string]any) string {
	t.Helper()
	userSearch, ok := config["userSearch"].(map[string]any)
	if !ok {
		t.Fatalf("stored config has no userSearch object")
	}
	return mustJSON(t, userSearch["username"])
}

// Several username attributes are a list, which only Dex v2.46 reads; Dex
// v2.45 would store it and then fail every login.
func TestLdapConnector_usernameAttributesNeedDex246(t *testing.T) {
	two := ldapInputs(map[string]property.Value{"userSearch": ldapUserSearch(map[string]property.Value{"usernameAttributes": strs("uid", "mail")})})

	t.Run("v2.46 stores a list", func(t *testing.T) {
		if got := storedUsername(t, storedLdapConfig(t, newFakeDex(), two)); got != `["uid","mail"]` {
			t.Errorf("username = %s, want a list", got)
		}
	})

	t.Run("v2.45 refuses", func(t *testing.T) {
		dex := newFakeDex()
		dex.version = &api.VersionResp{Server: "v2.45.1", Api: 3}
		s := newProviderAgainst(t, dex)
		urn := testURN(tokenLdapConnector)
		checked, err := s.Check(p.CheckRequest{Urn: urn, Inputs: property.NewMap(two)})
		if err != nil || len(checked.Failures) > 0 {
			t.Fatalf("Check: %v %v", err, checked.Failures)
		}
		for _, dryRun := range []bool{true, false} {
			_, err := s.Create(p.CreateRequest{Urn: urn, Properties: checked.Inputs, DryRun: dryRun})
			if err == nil || !strings.Contains(err.Error(), "needs Dex v2.46") {
				t.Errorf("create (preview=%v) on Dex v2.45 = %v, want a refusal", dryRun, err)
			}
		}
		if dex.writes != 0 {
			t.Errorf("%d writes to Dex", dex.writes)
		}

		one, err := s.Check(p.CheckRequest{Urn: urn, Inputs: property.NewMap(ldapInputs(nil))})
		if err != nil || len(one.Failures) > 0 {
			t.Fatalf("Check: %v %v", err, one.Failures)
		}
		created, err := s.Create(p.CreateRequest{Urn: urn, Properties: one.Inputs})
		if err != nil {
			t.Fatalf("one username attribute on Dex v2.45: %v", err)
		}
		next, err := s.Check(p.CheckRequest{Urn: urn, State: one.Inputs, Inputs: property.NewMap(two)})
		if err != nil || len(next.Failures) > 0 {
			t.Fatalf("Check: %v %v", err, next.Failures)
		}
		_, err = s.Update(p.UpdateRequest{ID: created.ID, Urn: urn, State: created.Properties, OldInputs: one.Inputs, Inputs: next.Inputs})
		if err == nil || !strings.Contains(err.Error(), "needs Dex v2.46") {
			t.Errorf("update to two attributes on Dex v2.45 = %v, want a refusal", err)
		}
	})
}

// A program with every field set refreshes and imports without changes, and
// the bind password stays secret throughout.
func TestLdapConnector_refreshAndImportRoundTrip(t *testing.T) {
	program := ldapInputs(map[string]property.Value{
		"host": str("ldap.example.com:389"), "startTls": property.New(true), "insecureSkipVerify": property.New(false),
		"rootCaPem": str(testCAPEM(t)), "bindDn": str("cn=reader,dc=example,dc=org"), "bindPassword": str(canary + "-bind"),
		"usernamePrompt": str(""),
		"userSearch": ldapUserSearch(map[string]property.Value{
			"usernameAttributes": strs("uid", "mail"), "filter": str("(objectClass=person)"), "idAttr": str("uid"), "emailSuffix": str(""),
		}),
		"groupSearch": obj(map[string]property.Value{
			"baseDn": str("ou=groups,dc=example,dc=org"), "nameAttr": str("cn"),
			"userMatchers": property.New([]property.Value{
				obj(map[string]property.Value{"userAttr": str("DN"), "groupAttr": str("member"), "recursionGroupAttr": str("member")}),
				obj(map[string]property.Value{"userAttr": str("uid"), "groupAttr": str("memberUid")}),
			}),
		}),
	})
	s := newProviderAgainst(t, newFakeDex())
	urn := testURN(tokenLdapConnector)
	checked, err := s.Check(p.CheckRequest{Urn: urn, Inputs: property.NewMap(program)})
	if err != nil || len(checked.Failures) > 0 {
		t.Fatalf("Check: %v %v", err, checked.Failures)
	}
	created, err := s.Create(p.CreateRequest{Urn: urn, Properties: checked.Inputs})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	assertNoPlainCanary(t, "create outputs", property.New(created.Properties))

	refreshed, err := s.Read(p.ReadRequest{ID: created.ID, Urn: urn, Properties: created.Properties, Inputs: checked.Inputs})
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	imported, err := s.Read(p.ReadRequest{ID: created.ID, Urn: urn})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	for when, read := range map[string]p.ReadResponse{"refresh": refreshed, "import": imported} {
		assertNoPlainCanary(t, when+" inputs", property.New(read.Inputs))
		assertNoPlainCanary(t, when+" state", property.New(read.Properties))
		if !read.Inputs.Get("bindPassword").IsSecret() || !read.Properties.Get("bindPassword").IsSecret() {
			t.Errorf("%s: bindPassword is not secret", when)
		}
		assertNoChanges(t, s, connectorContract{token: tokenLdapConnector}, read, program, "preview after "+when)
	}
}

// A setting the resource does not model fails refresh and import, naming the
// setting and never its value.
func TestLdapConnector_unmodelledSettingsFailRead(t *testing.T) {
	base := `"host":"ldap.example.com:636","bindDN":"cn=r","bindPW":"` + canary + `","userSearch":{"baseDN":"dc=example,dc=org","username":"uid"`
	for name, tc := range map[string]struct{ config, key string }{
		"kerberos":                    {`{` + base + `},"kerberos":{"enabled":true,"keytabPath":"` + canary + `"}}`, "kerberos"},
		"deprecated groupSearch keys": {`{` + base + `},"groupSearch":{"baseDN":"ou=groups","nameAttr":"cn","userAttr":"DN","groupAttr":"` + canary + `"}}`, "groupSearch.groupAttr, groupSearch.userAttr"},
		"unknown userSearch key":      {`{` + base + `,"extra":"` + canary + `"}}`, "userSearch.extra"},
		"unknown matcher key":         {`{` + base + `},"groupSearch":{"baseDN":"ou=groups","nameAttr":"cn","userMatchers":[{"userAttr":"DN","groupAttr":"member","x":"` + canary + `"}]}}`, "groupSearch.userMatchers[0].x"},
		"key with other spelling":     {`{` + base + `},"BindPW":"` + canary + `"}`, "BindPW"},
		"rootCAData not base64":       {`{` + base + `},"rootCAData":"` + canary + `!"}`, "rootCAData"},
	} {
		t.Run(name, func(t *testing.T) {
			dex := newFakeDex()
			dex.connectors["ldap"] = &api.Connector{Id: "ldap", Type: "ldap", Name: "LDAP", Config: []byte(tc.config)}
			s := newProviderAgainst(t, dex)
			urn := testURN(tokenLdapConnector)
			for when, req := range map[string]p.ReadRequest{
				"import":  {ID: "ldap", Urn: urn},
				"refresh": {ID: "ldap", Urn: urn, Properties: property.NewMap(ldapInputs(nil)), Inputs: property.NewMap(ldapInputs(nil))},
			} {
				_, err := s.Read(req)
				if err == nil || !strings.Contains(err.Error(), tc.key) {
					t.Errorf("%s = %v, want a failure naming %s", when, err, tc.key)
					continue
				}
				if strings.Contains(err.Error(), canary) {
					t.Errorf("%s error reveals a value: %v", when, err)
				}
				if tc.key != "rootCAData" && !strings.Contains(err.Error(), tokenConnector) {
					t.Errorf("%s error should recommend %s: %v", when, tokenConnector, err)
				}
			}
			if dex.writes != 0 {
				t.Errorf("%d writes to Dex", dex.writes)
			}
		})
	}
}
