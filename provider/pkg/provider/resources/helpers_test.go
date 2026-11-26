package resources

import (
	"testing"
)

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
			got := userNameSourceFromConfig(tc.remote, "preferred_username", tc.prior)
			if (got == nil) != (tc.want == nil) || (got != nil && *got != *tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
