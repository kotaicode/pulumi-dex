package resources

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

// Typed inputs by the lower-case Dex OIDC config key they set. Dex decodes its
// config with encoding/json, which matches keys case-insensitively, so every
// spelling of these keys in an extension map collides with the typed input.
var (
	azureOIDCTypedKeys = map[string]string{
		"issuer": "tenantId", "clientid": "clientId", "clientsecret": "clientSecret",
		"redirecturi": "redirectUri", "scopes": "scopes", "usernamekey": "userNameSource",
	}
	cognitoOIDCTypedKeys = map[string]string{
		"issuer": "region and userPoolId", "clientid": "clientId", "clientsecret": "clientSecret",
		"redirecturi": "redirectUri", "scopes": "scopes", "usernamekey": "userNameSource",
	}
	genericOIDCTypedKeys = map[string]string{
		"issuer": "issuer", "clientid": "clientId", "clientsecret": "clientSecret", "redirecturi": "redirectUri",
		"scopes": "scopes", "insecureskipemailverified": "insecureSkipEmailVerified",
		"insecureissuer": "insecureIssuer", "usernamekey": "userNameKey", "claimmapping": "claimMapping",
	}
)

// checkExtensionKeys rejects extension-map keys that set a Dex config key a
// typed input already sets: the extension value used to replace the typed one
// silently. It names keys only, never values. An unknown map is not judged;
// its keys are, even when their values are unknown.
func checkExtensionKeys(extra property.Value, name string, typed map[string]string) []p.CheckFailure {
	if !extra.IsMap() {
		return nil
	}
	var collisions []string
	for key := range extra.AsMap().Keys() {
		if input, ok := typed[strings.ToLower(key)]; ok {
			collisions = append(collisions, fmt.Sprintf("%s (set by %s)", key, input))
		}
	}
	if len(collisions) == 0 {
		return nil
	}
	sort.Strings(collisions)
	return []p.CheckFailure{{Property: name, Reason: fmt.Sprintf(
		"%s sets %s, which typed inputs already set. Dex reads config keys case-insensitively, so the %s "+
			"value would replace the typed one; remove these keys from %s and use the typed inputs",
		name, strings.Join(collisions, ", "), name, name)}}
}

// checkConnectorInputMode enforces the generic Connector's input rule at
// preview: exactly one of oidcConfig and rawConfig, oidcConfig only for type
// oidc, and rawConfig as valid JSON. Unknown values are not judged.
func checkConnectorInputMode(inputs property.Map) []p.CheckFailure {
	oidc, oidcGiven := inputs.GetOk("oidcConfig")
	raw, rawGiven := inputs.GetOk("rawConfig")
	if (oidcGiven && oidc.IsComputed()) || (rawGiven && raw.IsComputed()) {
		return nil
	}
	oidcSet := oidcGiven && !oidc.IsNull()
	rawSet := rawGiven && raw.IsString() && raw.AsString() != ""

	var failures []p.CheckFailure
	if oidcSet == rawSet {
		failures = append(failures, p.CheckFailure{Property: "rawConfig",
			Reason: "exactly one of oidcConfig and rawConfig must be set: oidcConfig for an OIDC connector, rawConfig for any type"})
	}
	if typ := inputs.Get("type"); oidcSet && typ.IsString() && typ.AsString() != "oidc" {
		failures = append(failures, p.CheckFailure{Property: "oidcConfig",
			Reason: fmt.Sprintf("oidcConfig requires type \"oidc\"; for type %q use rawConfig", typ.AsString())})
	}
	if rawSet {
		var v any
		var syntax *json.SyntaxError
		if err := json.Unmarshal([]byte(raw.AsString()), &v); errors.As(err, &syntax) {
			failures = append(failures, p.CheckFailure{Property: "rawConfig",
				Reason: fmt.Sprintf("rawConfig is not valid JSON (syntax error at byte %d)", syntax.Offset)})
		} else if err != nil {
			failures = append(failures, p.CheckFailure{Property: "rawConfig", Reason: "rawConfig is not valid JSON"})
		}
	}
	return failures
}
