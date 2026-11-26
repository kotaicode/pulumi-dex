package resources

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kotaicode/pulumi-dex/pkg/provider"
)

// connectorConfig reads a connector config stored in Dex for the typed connector
// resources. Dex only checks that a config is valid JSON (v2.46.1), so a stored
// config can still be something other than an object or hold a known field with
// the wrong type. Reading such a field as empty, or reporting the connector as
// deleted, would silently corrupt state; connectorConfig records the first problem
// by path and type instead. Errors never include config values, which may be
// credentials.
type connectorConfig struct {
	values map[string]any
	path   string // prefix for nested objects, e.g. "orgs[1]."
	err    *error // first problem, shared with nested readers
}

func parseConnectorConfig(raw []byte) (*connectorConfig, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		var syntaxErr *json.SyntaxError
		if errors.As(err, &syntaxErr) {
			return nil, fmt.Errorf("config is not valid JSON (syntax error at byte %d)", syntaxErr.Offset)
		}
		return nil, errors.New("config is not valid JSON")
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("config is a JSON %s, expected an object", jsonKind(v))
	}
	var err error
	return &connectorConfig{values: m, err: &err}, nil
}

// Err returns the first field problem found while reading.
func (c *connectorConfig) Err() error { return *c.err }

func (c *connectorConfig) fail(key, want string, got any) {
	if *c.err == nil {
		*c.err = fmt.Errorf("config.%s%s is a JSON %s, expected %s", c.path, key, jsonKind(got), want)
	}
}

// str returns a string field, or "" if it is absent or null.
func (c *connectorConfig) str(key string) string {
	v, ok := c.values[key]
	if !ok || v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		c.fail(key, "a string", v)
	}
	return s
}

// strPtr returns a string field, or nil if it is absent, null or empty.
func (c *connectorConfig) strPtr(key string) *string {
	if s := c.str(key); s != "" {
		return &s
	}
	return nil
}

// boolPtr returns a boolean field, or nil if it is absent or null.
func (c *connectorConfig) boolPtr(key string) *bool {
	v, ok := c.values[key]
	if !ok || v == nil {
		return nil
	}
	b, ok := v.(bool)
	if !ok {
		c.fail(key, "a boolean", v)
		return nil
	}
	return &b
}

// strList returns a string array field, or nil if it is absent or null.
func (c *connectorConfig) strList(key string) []string {
	items, ok := c.list(key)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for i, item := range items {
		s, ok := item.(string)
		if !ok {
			c.fail(fmt.Sprintf("%s[%d]", key, i), "a string", item)
			continue
		}
		out = append(out, s)
	}
	return out
}

// strMap returns an object field with string values, or nil if it is absent or null.
func (c *connectorConfig) strMap(key string) map[string]string {
	v, ok := c.values[key]
	if !ok || v == nil {
		return nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		c.fail(key, "an object", v)
		return nil
	}
	out := make(map[string]string, len(m))
	for k, item := range m {
		s, ok := item.(string)
		if !ok {
			c.fail(key+"."+k, "a string", item)
			continue
		}
		out[k] = s
	}
	return out
}

// objects returns readers for an array of objects, or nil if it is absent or null.
func (c *connectorConfig) objects(key string) []*connectorConfig {
	items, ok := c.list(key)
	if !ok {
		return nil
	}
	out := make([]*connectorConfig, 0, len(items))
	for i, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			c.fail(fmt.Sprintf("%s[%d]", key, i), "an object", item)
			continue
		}
		out = append(out, &connectorConfig{values: m, path: fmt.Sprintf("%s%s[%d].", c.path, key, i), err: c.err})
	}
	return out
}

func (c *connectorConfig) list(key string) ([]any, bool) {
	v, ok := c.values[key]
	if !ok || v == nil {
		return nil, false
	}
	items, ok := v.([]any)
	if !ok {
		c.fail(key, "an array", v)
		return nil, false
	}
	return items, true
}

func jsonKind(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64, json.Number:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return fmt.Sprintf("%T", v)
	}
}

// unreadableConfigError reports a connector whose stored config cannot be read. The
// read fails so that Pulumi keeps the resource in state instead of dropping it.
func unreadableConfigError(resourceType, id string, err error) error {
	return provider.WrapError("read", resourceType, id, fmt.Errorf(
		"cannot read the connector config stored in Dex: %w. The resource is kept in state; "+
			"correct or recreate the connector config in Dex, then run `pulumi refresh` again", err))
}
