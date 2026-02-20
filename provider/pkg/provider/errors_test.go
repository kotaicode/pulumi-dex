package provider

import (
	"errors"
	"strings"
	"testing"
)

func TestWrapError(t *testing.T) {
	tests := []struct {
		name         string
		operation    string
		resourceType string
		resourceID   string
		err          error
		wantContains []string
		wantNil      bool
	}{
		{
			name:         "nil error returns nil",
			operation:    "create",
			resourceType: "client",
			resourceID:   "my-client",
			err:          nil,
			wantNil:      true,
		},
		{
			name:         "wraps error with context",
			operation:    "create",
			resourceType: "client",
			resourceID:   "my-client",
			err:          errors.New("connection refused"),
			wantContains: []string{"dex", "create", "client", "my-client", "connection refused"},
			wantNil:      false,
		},
		{
			name:         "update connector",
			operation:    "update",
			resourceType: "connector",
			resourceID:   "github",
			err:          errors.New("not found"),
			wantContains: []string{"dex", "update", "connector", "github", "not found"},
			wantNil:      false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WrapError(tt.operation, tt.resourceType, tt.resourceID, tt.err)
			if tt.wantNil {
				if got != nil {
					t.Errorf("WrapError() = %v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("WrapError() = nil, want non-nil error")
			}
			msg := got.Error()
			for _, sub := range tt.wantContains {
				if !strings.Contains(msg, sub) {
					t.Errorf("WrapError() message %q does not contain %q", msg, sub)
				}
			}
			// Wrapped error should be unwrappable (WrapError uses %w)
			if !errors.Is(got, tt.err) {
				t.Errorf("WrapError() result should wrap original error (errors.Is)")
			}
		})
	}
}

