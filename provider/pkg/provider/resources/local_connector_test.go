package resources

import (
	"context"
	"testing"

	"github.com/pulumi/pulumi-go-provider/infer"
)

func TestLocalConnector_Create_dryRun(t *testing.T) {
	ctx := context.Background()
	r := &LocalConnector{}
	args := LocalConnectorArgs{
		ConnectorId: "local",
		Name:        "Local",
	}
	req := infer.CreateRequest[LocalConnectorArgs]{
		Name:   "localConnector",
		Inputs: args,
		DryRun: true,
	}
	resp, err := r.Create(ctx, req)
	if err != nil {
		t.Fatalf("Create(dry-run) err = %v", err)
	}
	if resp.ID != "local" {
		t.Errorf("Create(dry-run) ID = %q, want local", resp.ID)
	}
	if resp.Output.ConnectorId != args.ConnectorId || resp.Output.Name != args.Name {
		t.Errorf("Create(dry-run) output mismatch: got ConnectorId=%q Name=%q", resp.Output.ConnectorId, resp.Output.Name)
	}
}

func TestLocalConnector_Update_dryRun(t *testing.T) {
	ctx := context.Background()
	r := &LocalConnector{}
	oldState := LocalConnectorState{
		LocalConnectorArgs: LocalConnectorArgs{ConnectorId: "local", Name: "Local"},
	}
	newInputs := LocalConnectorArgs{
		ConnectorId: "local",
		Name:        "Local (updated)",
	}
	req := infer.UpdateRequest[LocalConnectorArgs, LocalConnectorState]{
		ID:     "local",
		State:  oldState,
		Inputs: newInputs,
		DryRun: true,
	}
	resp, err := r.Update(ctx, req)
	if err != nil {
		t.Fatalf("Update(dry-run) err = %v", err)
	}
	if resp.Output.Name != "Local (updated)" {
		t.Errorf("Update(dry-run) Name = %q, want Local (updated)", resp.Output.Name)
	}
}
