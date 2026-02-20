package resources

import (
	"context"
	"testing"

	"github.com/pulumi/pulumi-go-provider/infer"
)

func TestConnector_Create_dryRun(t *testing.T) {
	ctx := context.Background()
	r := &Connector{}
	args := ConnectorArgs{
		ConnectorId: "my-oidc",
		Type:        "oidc",
		Name:        "My OIDC",
	}
	req := infer.CreateRequest[ConnectorArgs]{
		Name:   "connector",
		Inputs: args,
		DryRun: true,
	}
	resp, err := r.Create(ctx, req)
	if err != nil {
		t.Fatalf("Create(dry-run) err = %v", err)
	}
	if resp.ID != "my-oidc" {
		t.Errorf("Create(dry-run) ID = %q, want my-oidc", resp.ID)
	}
	if resp.Output.ConnectorId != args.ConnectorId || resp.Output.Type != args.Type || resp.Output.Name != args.Name {
		t.Errorf("Create(dry-run) output mismatch: got ConnectorId=%q Type=%q Name=%q",
			resp.Output.ConnectorId, resp.Output.Type, resp.Output.Name)
	}
}

func TestConnector_Update_dryRun(t *testing.T) {
	ctx := context.Background()
	r := &Connector{}
	oldState := ConnectorState{
		ConnectorArgs: ConnectorArgs{ConnectorId: "my-oidc", Type: "oidc", Name: "Old Name"},
	}
	newInputs := ConnectorArgs{
		ConnectorId: "my-oidc",
		Type:        "oidc",
		Name:        "Updated OIDC Name",
	}
	req := infer.UpdateRequest[ConnectorArgs, ConnectorState]{
		ID:     "my-oidc",
		State:  oldState,
		Inputs: newInputs,
		DryRun: true,
	}
	resp, err := r.Update(ctx, req)
	if err != nil {
		t.Fatalf("Update(dry-run) err = %v", err)
	}
	if resp.Output.Name != "Updated OIDC Name" {
		t.Errorf("Update(dry-run) Name = %q, want Updated OIDC Name", resp.Output.Name)
	}
}
