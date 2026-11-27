package provider

import (
	"context"
	"fmt"

	api "github.com/dexidp/dex/api/v2"
)

// ServerInfo is what the Dex server reported through GetVersion. Dex release
// builds report their tag as Server (for example "v2.46.1"); builds from
// source report "DEV". API is Dex's API version: 3 for v2.45, 4 for v2.46.
type ServerInfo struct {
	Server string
	API    int32
	// Err is set when GetVersion failed; the server version is then unknown.
	Err error
}

// Feature is a provider feature that needs a minimum Dex API version.
type Feature struct {
	Name      string
	MinAPI    int32
	MinServer string // first Dex release line with MinAPI, for messages
}

// Features that need a newer Dex than the oldest supported line. Dex adds API
// fields without rejecting unknown ones, so an older server would silently
// ignore such a field instead of failing.
var (
	// LocalUserGroups is the groups list on password users (Password.groups,
	// UpdatePasswordReq.new_groups), added in Dex v2.46.0.
	LocalUserGroups = Feature{Name: "LocalUser.groups", MinAPI: 4, MinServer: "v2.46"}
	// LdapUsernameAttributes is a list in the LDAP connector's
	// userSearch.username, added in Dex v2.46.0. Dex v2.45 stores the list
	// and then fails every login through the connector.
	LdapUsernameAttributes = Feature{Name: "LdapConnector.userSearch.usernameAttributes with more than one entry", MinAPI: 4, MinServer: "v2.46"}
)

// Supported Dex lines: v2.45 (API 3) and v2.46 (API 4).
const (
	minSupportedAPI = 3
	maxSupportedAPI = 4
)

// fetchServerInfo asks Dex for its version. A failure is kept in the result
// instead of failing the connection, so existing resources keep working.
func fetchServerInfo(ctx context.Context, client api.DexClient) ServerInfo {
	resp, err := client.GetVersion(ctx, &api.VersionReq{})
	if err != nil {
		return ServerInfo{Err: err}
	}
	return ServerInfo{Server: resp.GetServer(), API: resp.GetApi()}
}

// serverWarning explains a server whose version is unknown or outside the
// supported lines, or returns "" for a supported server.
func serverWarning(host string, info ServerInfo) string {
	switch {
	case info.Err != nil:
		return fmt.Sprintf("could not determine the version of the Dex server at %s (GetVersion: %v); "+
			"existing resources keep working, but features that need a specific Dex version are refused", host, info.Err)
	case info.API < minSupportedAPI || info.API > maxSupportedAPI:
		return fmt.Sprintf("the Dex server at %s reports version %s (API %d); this provider supports Dex v2.45 and "+
			"v2.46 (API %d and %d). Existing resources keep working; features that need a newer Dex are refused "+
			"unless the server reports a high enough API version", host, describeServer(info), info.API, minSupportedAPI, maxSupportedAPI)
	default:
		return ""
	}
}

// Require reports whether the connected Dex server supports f. An unknown
// version counts as unsupported; a failed GetVersion is reported as that
// failure, not as missing support.
func (c *DexConfig) Require(f Feature) error {
	switch {
	case c.Server == nil:
		return fmt.Errorf("%s needs Dex %s or later, and the version of the Dex server is not known yet", f.Name, f.MinServer)
	case c.Server.Err != nil:
		return fmt.Errorf("cannot tell whether the Dex server at %s supports %s (Dex %s or later): GetVersion failed: %w",
			c.Host, f.Name, f.MinServer, c.Server.Err)
	case c.Server.API < f.MinAPI:
		return fmt.Errorf("%s needs Dex %s or later (API %d); the Dex server at %s reports version %s (API %d)",
			f.Name, f.MinServer, f.MinAPI, c.Host, describeServer(*c.Server), c.Server.API)
	default:
		return nil
	}
}

func describeServer(info ServerInfo) string {
	if info.Server == "" {
		return "unknown"
	}
	return fmt.Sprintf("%q", info.Server)
}
