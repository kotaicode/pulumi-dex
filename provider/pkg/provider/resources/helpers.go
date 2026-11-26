package resources

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
)

// ============================================================================
// Helper functions for connector implementations
// ============================================================================

// GetString extracts a string value from a map, returning empty string if not found.
func GetString(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if str, ok := v.(string); ok {
			return str
		}
	}
	return ""
}

// GetStringPtr extracts a string value from a map, returning nil if not found or empty.
func GetStringPtr(m map[string]any, key string) *string {
	if v, ok := m[key]; ok {
		if str, ok := v.(string); ok && str != "" {
			return &str
		}
	}
	return nil
}

// GetBoolPtr extracts a boolean value from a map, returning nil if not found.
func GetBoolPtr(m map[string]any, key string) *bool {
	if v, ok := m[key]; ok {
		if b, ok := v.(bool); ok {
			return &b
		}
	}
	return nil
}

// ============================================================================
// Refresh round-trip helpers (DEX-009)
// ============================================================================

// typedOIDCConfigKeys are the Dex OIDC config keys that AzureOidcConnector and
// CognitoOidcConnector derive from their typed inputs. Every other key in a
// remote config came from extraOidc.
var typedOIDCConfigKeys = map[string]bool{
	"issuer": true, "clientID": true, "clientSecret": true, "redirectURI": true, "scopes": true, "userNameKey": true,
}

// extraOIDCFromConfig returns the config entries that have no typed field, so that
// extraOidc survives a refresh. When there are none, it mirrors the prior input:
// nil if extraOidc was omitted, an empty map if it was given empty.
func extraOIDCFromConfig(config map[string]any, prior map[string]any) map[string]any {
	extra := map[string]any{}
	for k, v := range config {
		if !typedOIDCConfigKeys[k] {
			extra[k] = v
		}
	}
	if len(extra) == 0 && prior == nil {
		return nil
	}
	return extra
}

// userNameSourceFromConfig reports the remote userNameKey as userNameSource. Create
// writes defaultKey when userNameSource is omitted, so that value is reported as
// omitted again unless the prior input set it explicitly. Any other value is real
// remote state and is reported as is.
func userNameSourceFromConfig(config map[string]any, defaultKey string, prior *string) *string {
	key := GetString(config, "userNameKey")
	if key == "" || (key == defaultKey && prior == nil) {
		return nil
	}
	return &key
}

// rawConfigFromRemote returns the remote connector config as rawConfig. If it is
// the same JSON document as the prior input, ignoring formatting and object key
// order, the prior string is kept so that reformatting alone never shows a change.
func rawConfigFromRemote(remote []byte, prior *string) string {
	if prior != nil && jsonEqual([]byte(*prior), remote) {
		return *prior
	}
	return string(remote)
}

// jsonEqual compares two JSON documents semantically. Numbers are compared by their
// literal text so that large integers are not rounded through float64.
func jsonEqual(a, b []byte) bool {
	var va, vb any
	if decodeJSONNumber(a, &va) != nil || decodeJSONNumber(b, &vb) != nil {
		return false
	}
	return reflect.DeepEqual(va, vb)
}

func decodeJSONNumber(data []byte, v *any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("trailing data after JSON value")
	}
	return nil
}
