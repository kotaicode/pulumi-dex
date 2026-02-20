package resources

import (
	"context"
	"testing"

	"github.com/pulumi/pulumi-go-provider/infer"
)

func TestGoogleConnector_Create_dryRun(t *testing.T) {
	ctx := context.Background()
	r := &GoogleConnector{}
	args := GoogleConnectorArgs{
		ConnectorId:   "google",
		Name:          "Google",
		ClientId:      "client-id",
		ClientSecret:  "secret",
		RedirectUri:   "https://dex.example.com/callback",
		HostedDomains: []string{"example.com"},
	}
	req := infer.CreateRequest[GoogleConnectorArgs]{
		Name:   "googleConnector",
		Inputs: args,
		DryRun: true,
	}
	resp, err := r.Create(ctx, req)
	if err != nil {
		t.Fatalf("Create(dry-run) err = %v", err)
	}
	if resp.ID != "google" {
		t.Errorf("Create(dry-run) ID = %q, want google", resp.ID)
	}
	if resp.Output.ConnectorId != args.ConnectorId || resp.Output.Name != args.Name {
		t.Errorf("Create(dry-run) output mismatch: got ConnectorId=%q Name=%q", resp.Output.ConnectorId, resp.Output.Name)
	}
	if len(resp.Output.HostedDomains) != 1 || resp.Output.HostedDomains[0] != "example.com" {
		t.Errorf("Create(dry-run) HostedDomains = %v", resp.Output.HostedDomains)
	}
}

func TestGoogleConnector_Update_dryRun(t *testing.T) {
	ctx := context.Background()
	r := &GoogleConnector{}
	oldState := GoogleConnectorState{
		GoogleConnectorArgs: GoogleConnectorArgs{
			ConnectorId: "google", Name: "Google",
			ClientId: "x", ClientSecret: "y", RedirectUri: "https://dex/cb",
		},
	}
	newInputs := GoogleConnectorArgs{
		ConnectorId:   "google",
		Name:          "Google (updated)",
		ClientId:      "x",
		ClientSecret:  "y",
		RedirectUri:   "https://dex/cb",
		HostedDomains: []string{"example.com", "other.com"},
	}
	req := infer.UpdateRequest[GoogleConnectorArgs, GoogleConnectorState]{
		ID:     "google",
		State:  oldState,
		Inputs: newInputs,
		DryRun: true,
	}
	resp, err := r.Update(ctx, req)
	if err != nil {
		t.Fatalf("Update(dry-run) err = %v", err)
	}
	if resp.Output.Name != "Google (updated)" {
		t.Errorf("Update(dry-run) Name = %q, want Google (updated)", resp.Output.Name)
	}
	if len(resp.Output.HostedDomains) != 2 {
		t.Errorf("Update(dry-run) len(HostedDomains) = %d, want 2", len(resp.Output.HostedDomains))
	}
}
