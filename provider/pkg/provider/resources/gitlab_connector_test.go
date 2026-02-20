package resources

import (
	"context"
	"testing"

	"github.com/pulumi/pulumi-go-provider/infer"
)

func TestGitLabConnector_Create_dryRun(t *testing.T) {
	ctx := context.Background()
	r := &GitLabConnector{}
	args := GitLabConnectorArgs{
		ConnectorId:  "gitlab",
		Name:         "GitLab",
		ClientId:     "client-id",
		ClientSecret: "secret",
		RedirectUri:  "https://dex.example.com/callback",
		Groups:       []string{"my-group"},
	}
	req := infer.CreateRequest[GitLabConnectorArgs]{
		Name:   "gitlabConnector",
		Inputs: args,
		DryRun: true,
	}
	resp, err := r.Create(ctx, req)
	if err != nil {
		t.Fatalf("Create(dry-run) err = %v", err)
	}
	if resp.ID != "gitlab" {
		t.Errorf("Create(dry-run) ID = %q, want gitlab", resp.ID)
	}
	if resp.Output.Name != "GitLab" || len(resp.Output.Groups) != 1 {
		t.Errorf("Create(dry-run) output mismatch: Name=%q len(Groups)=%d", resp.Output.Name, len(resp.Output.Groups))
	}
}

func TestGitLabConnector_Update_dryRun(t *testing.T) {
	ctx := context.Background()
	r := &GitLabConnector{}
	req := infer.UpdateRequest[GitLabConnectorArgs, GitLabConnectorState]{
		ID:    "gitlab",
		State: GitLabConnectorState{GitLabConnectorArgs: GitLabConnectorArgs{ConnectorId: "gitlab", Name: "GitLab", ClientId: "x", ClientSecret: "y", RedirectUri: "https://dex/cb"}},
		Inputs: GitLabConnectorArgs{
			ConnectorId:  "gitlab",
			Name:         "GitLab (updated)",
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
	if resp.Output.Name != "GitLab (updated)" {
		t.Errorf("Update(dry-run) Name = %q", resp.Output.Name)
	}
}
