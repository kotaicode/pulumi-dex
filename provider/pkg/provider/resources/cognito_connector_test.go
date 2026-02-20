package resources

import (
	"context"
	"testing"

	"github.com/pulumi/pulumi-go-provider/infer"
)

func TestCognitoOidcConnector_Create_dryRun(t *testing.T) {
	ctx := context.Background()
	r := &CognitoOidcConnector{}
	args := CognitoOidcConnectorArgs{
		ConnectorId:  "cognito",
		Name:        "Cognito",
		Region:      "us-east-1",
		UserPoolId:  "us-east-1_abc",
		ClientId:    "client-id",
		ClientSecret: "secret",
		RedirectUri: "https://dex.example.com/callback",
	}
	req := infer.CreateRequest[CognitoOidcConnectorArgs]{
		Name:   "cognitoConnector",
		Inputs: args,
		DryRun: true,
	}
	resp, err := r.Create(ctx, req)
	if err != nil {
		t.Fatalf("Create(dry-run) err = %v", err)
	}
	if resp.ID != "cognito" {
		t.Errorf("Create(dry-run) ID = %q, want cognito", resp.ID)
	}
	if resp.Output.Name != "Cognito" || resp.Output.Region != "us-east-1" {
		t.Errorf("Create(dry-run) output mismatch: Name=%q Region=%q", resp.Output.Name, resp.Output.Region)
	}
}

func TestCognitoOidcConnector_Update_dryRun(t *testing.T) {
	ctx := context.Background()
	r := &CognitoOidcConnector{}
	req := infer.UpdateRequest[CognitoOidcConnectorArgs, CognitoOidcConnectorState]{
		ID: "cognito",
		State: CognitoOidcConnectorState{
			CognitoOidcConnectorArgs: CognitoOidcConnectorArgs{
				ConnectorId: "cognito", Name: "Cognito", Region: "us-east-1",
				UserPoolId: "x", ClientId: "y", ClientSecret: "z", RedirectUri: "https://dex/cb",
			},
		},
		Inputs: CognitoOidcConnectorArgs{
			ConnectorId:  "cognito",
			Name:         "Cognito (updated)",
			Region:       "us-east-1",
			UserPoolId:   "x",
			ClientId:     "y",
			ClientSecret: "z",
			RedirectUri:  "https://dex/cb",
		},
		DryRun: true,
	}
	resp, err := r.Update(ctx, req)
	if err != nil {
		t.Fatalf("Update(dry-run) err = %v", err)
	}
	if resp.Output.Name != "Cognito (updated)" {
		t.Errorf("Update(dry-run) Name = %q", resp.Output.Name)
	}
}
