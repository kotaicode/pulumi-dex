//go:build dexlive

package resources

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/property"

	"github.com/kotaicode/pulumi-dex/pkg/provider"
)

// liveStoredConfig returns the config of a connector as Dex stores it.
func liveStoredConfig(t *testing.T, id string) map[string]any {
	t.Helper()
	c := &provider.DexConfig{Host: os.Getenv("DEX_LIVE_HOST"), Plaintext: ptrBool(true)}
	if err := c.Configure(t.Context()); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	conn := storedConnector(t, c.Client, id)
	if conn == nil {
		t.Fatalf("connector %s not in Dex", id)
	}
	var config map[string]any
	if err := json.Unmarshal(conn.GetConfig(), &config); err != nil {
		t.Fatal(err)
	}
	return config
}

func TestLive_LdapConnector(t *testing.T) {
	id := liveID("ldap-typed")
	program := map[string]property.Value{
		"connectorId": str(id), "name": str("Live LDAP"), "host": str("ldap.example.com:636"),
		"rootCaPem": str(testCAPEM(t)), "bindDn": str("cn=reader,dc=example,dc=org"), "bindPassword": str("live-synthetic$secret"),
		"userSearch": obj(map[string]property.Value{"baseDn": str("dc=example,dc=org"), "usernameAttributes": strs("uid")}),
		"groupSearch": obj(map[string]property.Value{"baseDn": str("ou=groups,dc=example,dc=org"), "nameAttr": str("cn"),
			"userMatchers": property.New([]property.Value{obj(map[string]property.Value{"userAttr": str("DN"), "groupAttr": str("member")})})}),
	}
	updated := with(program, map[string]property.Value{"name": str("Live LDAP renamed"), "host": str("ldap2.example.com:636")})
	lifecycle(t, tokenLdapConnector, id, program, updated, func(t *testing.T, in property.Map) {
		wantString(t, in, "host", "ldap2.example.com:636")
		config := liveStoredConfig(t, id)
		if got := storedUsername(t, config); got != `"uid"` {
			t.Errorf("stored username = %s, want the string uid, which both Dex lines read", got)
		}
		if config["bindPW"] != "live-synthetic$secret" {
			t.Error("stored bindPW differs from the input")
		}
	})
}

// Several username attributes: stored as a list on Dex v2.46, refused at
// preview on Dex v2.45, which would store the list and then fail every login.
func TestLive_LdapUsernameAttributes(t *testing.T) {
	minor := liveMinor(t)
	if minor == 0 {
		t.Skip("DEX_LIVE_VERSION is not set")
	}
	s := liveProvider(t)
	urn := testURN(tokenLdapConnector)
	id := liveID("ldap-usernames")
	checked, err := s.Check(p.CheckRequest{Urn: urn, Inputs: property.NewMap(map[string]property.Value{
		"connectorId": str(id), "name": str("Live LDAP"), "host": str("ldap.example.com:636"),
		"userSearch": obj(map[string]property.Value{"baseDn": str("dc=example,dc=org"), "usernameAttributes": strs("uid", "mail")}),
	})})
	if err != nil || len(checked.Failures) > 0 {
		t.Fatalf("Check: %v %v", err, checked.Failures)
	}
	if minor < 46 {
		_, err := s.Create(p.CreateRequest{Urn: urn, Properties: checked.Inputs, DryRun: true})
		if err == nil || !strings.Contains(err.Error(), "needs Dex v2.46") {
			t.Errorf("preview with two username attributes on Dex v2.45 = %v, want a refusal", err)
		}
		return
	}
	created, err := s.Create(p.CreateRequest{Urn: urn, Properties: checked.Inputs})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got := storedUsername(t, liveStoredConfig(t, id)); got != `["uid","mail"]` {
		t.Errorf("stored username = %s, want the list", got)
	}
	if err := s.Delete(p.DeleteRequest{ID: created.ID, Urn: urn, Properties: created.Properties}); err != nil {
		t.Errorf("Delete: %v", err)
	}
}
