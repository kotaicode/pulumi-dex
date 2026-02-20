package resources

import (
	"testing"
)

func TestGetString(t *testing.T) {
	tests := []struct {
		name     string
		m        map[string]any
		key      string
		want     string
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
		name     string
		m        map[string]any
		key      string
		wantNil  bool
		wantVal  string
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
		name     string
		m        map[string]any
		key      string
		wantNil  bool
		wantVal  bool
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
