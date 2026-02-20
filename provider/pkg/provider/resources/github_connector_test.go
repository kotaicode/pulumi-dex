package resources

import (
	"context"
	"testing"

	"github.com/pulumi/pulumi-go-provider/infer"
)

func TestGitHubConnector_Create_dryRun(t *testing.T) {
	ctx := context.Background()
	r := &GitHubConnector{}
	args := GitHubConnectorArgs{
		ConnectorId:  "github",
		Name:         "GitHub",
		ClientId:     "client-id",
		ClientSecret: "secret",
		RedirectUri:  "https://dex.example.com/callback",
		Orgs:         []GitHubOrg{{Name: "my-org"}},
	}
	req := infer.CreateRequest[GitHubConnectorArgs]{
		Name:   "githubConnector",
		Inputs: args,
		DryRun: true,
	}
	resp, err := r.Create(ctx, req)
	if err != nil {
		t.Fatalf("Create(dry-run) err = %v", err)
	}
	if resp.ID != "github" {
		t.Errorf("Create(dry-run) ID = %q, want github", resp.ID)
	}
	if resp.Output.Name != "GitHub" || len(resp.Output.Orgs) != 1 {
		t.Errorf("Create(dry-run) output mismatch: Name=%q len(Orgs)=%d", resp.Output.Name, len(resp.Output.Orgs))
	}
}

func TestGitHubConnector_Update_dryRun(t *testing.T) {
	ctx := context.Background()
	r := &GitHubConnector{}
	req := infer.UpdateRequest[GitHubConnectorArgs, GitHubConnectorState]{
		ID:    "github",
		State: GitHubConnectorState{GitHubConnectorArgs: GitHubConnectorArgs{ConnectorId: "github", Name: "GitHub", ClientId: "x", ClientSecret: "y", RedirectUri: "https://dex/cb"}},
		Inputs: GitHubConnectorArgs{
			ConnectorId:  "github",
			Name:         "GitHub (updated)",
			ClientId:     "x",
			ClientSecret: "y",
			RedirectUri:  "https://dex/cb",
		},
		DryRun: true,
	}
	resp, err := r.Update(ctx, req)
	if err != nil {
		t.Fatalf("Update(dry-run) err = %v", err)
	}
	if resp.Output.Name != "GitHub (updated)" {
		t.Errorf("Update(dry-run) Name = %q", resp.Output.Name)
	}
}
