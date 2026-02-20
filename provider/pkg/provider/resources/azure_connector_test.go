package resources

import (
	"context"
	"testing"

	"github.com/pulumi/pulumi-go-provider/infer"
)

func TestAzureOidcConnector_Create_dryRun(t *testing.T) {
	ctx := context.Background()
	r := &AzureOidcConnector{}
	args := AzureOidcConnectorArgs{
		ConnectorId:  "azure-oidc",
		Name:        "Azure AD",
		TenantId:    "00000000-0000-0000-0000-000000000001",
		ClientId:    "client-id",
		ClientSecret: "secret",
		RedirectUri:  "https://dex.example.com/callback",
	}
	req := infer.CreateRequest[AzureOidcConnectorArgs]{
		Name:   "azureOidcConnector",
		Inputs: args,
		DryRun: true,
	}
	resp, err := r.Create(ctx, req)
	if err != nil {
		t.Fatalf("Create(dry-run) err = %v", err)
	}
	if resp.ID != "azure-oidc" {
		t.Errorf("Create(dry-run) ID = %q, want azure-oidc", resp.ID)
	}
	if resp.Output.Name != "Azure AD" || resp.Output.TenantId != args.TenantId {
		t.Errorf("Create(dry-run) output mismatch: Name=%q TenantId=%q", resp.Output.Name, resp.Output.TenantId)
	}
}

func TestAzureOidcConnector_Update_dryRun(t *testing.T) {
	ctx := context.Background()
	r := &AzureOidcConnector{}
	req := infer.UpdateRequest[AzureOidcConnectorArgs, AzureOidcConnectorState]{
		ID: "azure-oidc",
		State: AzureOidcConnectorState{
			AzureOidcConnectorArgs: AzureOidcConnectorArgs{
				ConnectorId: "azure-oidc", Name: "Azure AD", TenantId: "00000000-0000-0000-0000-000000000001",
				ClientId: "x", ClientSecret: "y", RedirectUri: "https://dex/cb",
			},
		},
		Inputs: AzureOidcConnectorArgs{
			ConnectorId:  "azure-oidc",
			Name:        "Azure AD (updated)",
			TenantId:    "00000000-0000-0000-0000-000000000001",
			ClientId:    "x",
			ClientSecret: "y",
			RedirectUri:  "https://dex/cb",
		},
		DryRun: true,
	}
	resp, err := r.Update(ctx, req)
	if err != nil {
		t.Fatalf("Update(dry-run) err = %v", err)
	}
	if resp.Output.Name != "Azure AD (updated)" {
		t.Errorf("Update(dry-run) Name = %q", resp.Output.Name)
	}
}

func TestAzureMicrosoftConnector_Create_dryRun(t *testing.T) {
	ctx := context.Background()
	r := &AzureMicrosoftConnector{}
	args := AzureMicrosoftConnectorArgs{
		ConnectorId:  "azure-microsoft",
		Name:         "Azure Microsoft",
		Tenant:       "00000000-0000-0000-0000-000000000001",
		ClientId:     "client-id",
		ClientSecret: "secret",
		RedirectUri:  "https://dex.example.com/callback",
	}
	req := infer.CreateRequest[AzureMicrosoftConnectorArgs]{
		Name:   "azureMicrosoftConnector",
		Inputs: args,
		DryRun: true,
	}
	resp, err := r.Create(ctx, req)
	if err != nil {
		t.Fatalf("Create(dry-run) err = %v", err)
	}
	if resp.ID != "azure-microsoft" {
		t.Errorf("Create(dry-run) ID = %q, want azure-microsoft", resp.ID)
	}
	if resp.Output.Name != "Azure Microsoft" {
		t.Errorf("Create(dry-run) Name = %q", resp.Output.Name)
	}
}

func TestAzureMicrosoftConnector_Update_dryRun(t *testing.T) {
	ctx := context.Background()
	r := &AzureMicrosoftConnector{}
	req := infer.UpdateRequest[AzureMicrosoftConnectorArgs, AzureMicrosoftConnectorState]{
		ID: "azure-microsoft",
		State: AzureMicrosoftConnectorState{
			AzureMicrosoftConnectorArgs: AzureMicrosoftConnectorArgs{
				ConnectorId: "azure-microsoft", Name: "Azure Microsoft", Tenant: "00000000-0000-0000-0000-000000000001",
				ClientId: "x", ClientSecret: "y", RedirectUri: "https://dex/cb",
			},
		},
		Inputs: AzureMicrosoftConnectorArgs{
			ConnectorId:  "azure-microsoft",
			Name:         "Azure Microsoft (updated)",
			Tenant:       "00000000-0000-0000-0000-000000000001",
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
	if resp.Output.Name != "Azure Microsoft (updated)" {
		t.Errorf("Update(dry-run) Name = %q", resp.Output.Name)
	}
}
