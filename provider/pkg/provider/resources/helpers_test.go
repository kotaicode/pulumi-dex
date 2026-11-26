package resources

import (
	"testing"
)

func TestGetString(t *testing.T) {
	tests := []struct {
		name string
		m    map[string]any
		key  string
		want string
	}{
		{"missing key", map[string]any{}, "x", ""},
		{"nil map", nil, "x", ""},
		{"wrong type", map[string]any{"x": 42}, "x", ""},
		{"present", map[string]any{"x": "hello"}, "x", "hello"},
		{"empty string", map[string]any{"x": ""}, "x", ""},
		{"other key", map[string]any{"a": "b"}, "x", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetString(tt.m, tt.key)
			if got != tt.want {
				t.Errorf("GetString(%v, %q) = %q, want %q", tt.m, tt.key, got, tt.want)
			}
		})
	}
}

func TestGetStringPtr(t *testing.T) {
	tests := []struct {
		name    string
		m       map[string]any
		key     string
		wantNil bool
		wantVal string
	}{
		{"missing key", map[string]any{}, "x", true, ""},
		{"nil map", nil, "x", true, ""},
		{"wrong type", map[string]any{"x": 42}, "x", true, ""},
		{"present", map[string]any{"x": "hello"}, "x", false, "hello"},
		{"empty string returns nil", map[string]any{"x": ""}, "x", true, ""},
		{"other key", map[string]any{"a": "b"}, "x", true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetStringPtr(tt.m, tt.key)
			if tt.wantNil {
				if got != nil {
					t.Errorf("GetStringPtr(%v, %q) = %v, want nil", tt.m, tt.key, got)
				}
				return
			}
			if got == nil || *got != tt.wantVal {
				var v string
				if got != nil {
					v = *got
				}
				t.Errorf("GetStringPtr(%v, %q) = %q, want %q", tt.m, tt.key, v, tt.wantVal)
			}
		})
	}
}

func TestGetBoolPtr(t *testing.T) {
	tests := []struct {
		name    string
		m       map[string]any
		key     string
		wantNil bool
		wantVal bool
	}{
		{"missing key", map[string]any{}, "x", true, false},
		{"nil map", nil, "x", true, false},
		{"wrong type", map[string]any{"x": "true"}, "x", true, false},
		{"true", map[string]any{"x": true}, "x", false, true},
		{"false", map[string]any{"x": false}, "x", false, false},
		{"other key", map[string]any{"a": true}, "x", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetBoolPtr(tt.m, tt.key)
			if tt.wantNil {
				if got != nil {
					t.Errorf("GetBoolPtr(%v, %q) = %v, want nil", tt.m, tt.key, got)
				}
				return
			}
			if got == nil || *got != tt.wantVal {
				var v bool
				if got != nil {
					v = *got
				}
				t.Errorf("GetBoolPtr(%v, %q) = %v, want %v", tt.m, tt.key, v, tt.wantVal)
			}
		})
	}
}

func TestJSONEqual(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{`{"a":1,"b":[1,2]}`, "{\n  \"b\": [1, 2],\n  \"a\": 1\n}", true},
		{`{"a":[1,2]}`, `{"a":[2,1]}`, false},
		{`{"a":false}`, `{}`, false},
		{`{"a":""}`, `{}`, false},
		{`{"n":12345678901234567890}`, `{"n":12345678901234567891}`, false},
		{`{"a":1}`, `{"a":1} {"b":2}`, false},
		{`{"a":1}`, `not json`, false},
	}
	for _, tc := range cases {
		if got := jsonEqual([]byte(tc.a), []byte(tc.b)); got != tc.want {
			t.Errorf("jsonEqual(%s, %s) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestExtraOIDCFromConfig(t *testing.T) {
	config := map[string]any{"issuer": "i", "clientID": "c", "clientSecret": "s", "redirectURI": "r", "scopes": []any{"openid"}, "userNameKey": "u"}
	if got := extraOIDCFromConfig(config, nil); got != nil {
		t.Errorf("no extra keys, omitted input: got %v, want nil", got)
	}
	if got := extraOIDCFromConfig(config, map[string]any{}); got == nil || len(got) != 0 {
		t.Errorf("no extra keys, empty input: got %#v, want empty map", got)
	}
	config["promptType"] = "consent"
	config["nested"] = map[string]any{"n": 1.0}
	got := extraOIDCFromConfig(config, nil)
	if len(got) != 2 || got["promptType"] != "consent" || got["nested"] == nil {
		t.Errorf("extra keys not returned: %#v", got)
	}
}

func TestUserNameSourceFromConfig(t *testing.T) {
	explicit := "preferred_username"
	cases := []struct {
		name   string
		remote string
		prior  *string
		want   *string
	}{
		{"default written for omitted input", "preferred_username", nil, nil},
		{"explicit default kept", "preferred_username", &explicit, &explicit},
		{"non-default is real state", "email", nil, strPtr("email")},
		{"absent remote key", "", &explicit, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			config := map[string]any{}
			if tc.remote != "" {
				config["userNameKey"] = tc.remote
			}
			got := userNameSourceFromConfig(config, "preferred_username", tc.prior)
			if (got == nil) != (tc.want == nil) || (got != nil && *got != *tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
