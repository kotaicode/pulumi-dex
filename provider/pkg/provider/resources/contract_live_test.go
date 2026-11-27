//go:build dexlive

package resources

import (
	"os"
	"testing"

	"github.com/kotaicode/pulumi-dex/pkg/provider"
)

// TestLive_ConnectorContract runs the connector contract suite against the Dex
// in DEX_LIVE_HOST. Scenarios that need fault injection are skipped; grant
// types are checked when the server stores them (Dex API 4, v2.46).
func TestLive_ConnectorContract(t *testing.T) {
	host := os.Getenv("DEX_LIVE_HOST")
	if host == "" {
		t.Fatal("DEX_LIVE_HOST is not set; the dexlive suite needs a running Dex")
	}
	c := &provider.DexConfig{Host: host, Plaintext: ptrBool(true)}
	if err := c.Configure(t.Context()); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if c.Server == nil || c.Server.Err != nil {
		t.Fatalf("server version unknown: %+v", c.Server)
	}
	grantTypes := c.Server.API >= 4
	t.Logf("Dex %s, API %d: grant types checked = %v", c.Server.Server, c.Server.API, grantTypes)
	runConnectorContract(t, func(*testing.T) contractDex {
		return contractDex{addr: host, grantTypes: grantTypes, id: liveID}
	})
}
