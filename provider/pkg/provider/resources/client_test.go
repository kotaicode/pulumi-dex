package resources

import (
	"context"
	"testing"

	"github.com/pulumi/pulumi-go-provider/infer"
)

func TestPtrOrString(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want *string
	}{
		{"empty string", "", nil},
		{"non-empty", "hello", strPtr("hello")},
		{"single char", "x", strPtr("x")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PtrOrString(tt.s)
			if tt.want == nil {
				if got != nil {
					t.Errorf("PtrOrString(%q) = %v, want nil", tt.s, got)
				}
				return
			}
			if got == nil || *got != *tt.want {
				var v string
				if got != nil {
					v = *got
				}
				t.Errorf("PtrOrString(%q) = %q, want %q", tt.s, v, *tt.want)
			}
		})
	}
}

func strPtr(s string) *string {
	return &s
}

// =============================================================================
// Client resource tests (dry-run / preview only; no Dex API)
// =============================================================================

func TestClient_Create_dryRun(t *testing.T) {
	ctx := context.Background()
	r := &Client{}
	args := ClientArgs{
		ClientId:     "test-client",
		Name:         "Test Client",
		RedirectUris: []string{"https://app.example.com/callback"},
		TrustedPeers: []string{"other-client"},
	}
	req := infer.CreateRequest[ClientArgs]{
		Name:    "testClient",
		Inputs:  args,
		DryRun:  true,
	}
	resp, err := r.Create(ctx, req)
	if err != nil {
		t.Fatalf("Create(dry-run) err = %v", err)
	}
	if resp.ID != "test-client" {
		t.Errorf("Create(dry-run) ID = %q, want test-client", resp.ID)
	}
	if resp.Output.ClientId != args.ClientId || resp.Output.Name != args.Name {
		t.Errorf("Create(dry-run) output mismatch: got ClientId=%q Name=%q", resp.Output.ClientId, resp.Output.Name)
	}
	if len(resp.Output.RedirectUris) != 1 || resp.Output.RedirectUris[0] != "https://app.example.com/callback" {
		t.Errorf("Create(dry-run) RedirectUris = %v", resp.Output.RedirectUris)
	}
}

func TestClient_Update_dryRun(t *testing.T) {
	ctx := context.Background()
	r := &Client{}
	newInputs := ClientArgs{
		ClientId:     "test-client",
		Name:         "Updated Name",
		RedirectUris: []string{"https://app.example.com/callback", "https://other.example.com/cb"},
	}
	req := infer.UpdateRequest[ClientArgs, ClientState]{
		ID:     "test-client",
		State:  ClientState{ClientArgs: ClientArgs{ClientId: "test-client", Name: "Old", RedirectUris: []string{}}},
		Inputs: newInputs,
		DryRun: true,
	}
	resp, err := r.Update(ctx, req)
	if err != nil {
		t.Fatalf("Update(dry-run) err = %v", err)
	}
	if resp.Output.Name != "Updated Name" {
		t.Errorf("Update(dry-run) Name = %q, want Updated Name", resp.Output.Name)
	}
	if len(resp.Output.RedirectUris) != 2 {
		t.Errorf("Update(dry-run) len(RedirectUris) = %d, want 2", len(resp.Output.RedirectUris))
	}
}
