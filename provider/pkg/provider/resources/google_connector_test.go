package resources

import (
	"context"
	"testing"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
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

// Dex stores no domainToAdminEmail for an empty map, and refresh and import read
// an absent one as absent. A program without the map, or with entries, previews
// without changes after either. A program that sets an empty map previews
// without changes after a refresh, but after an import proposes adding it.
func runGoogleDomainToAdminEmailRoundTrip(t *testing.T, d contractDex) {
	var google connectorContract
	for _, c := range connectorContracts() {
		if c.token == tokenGoogleConnector {
			google = c
		}
	}
	empty := map[string]property.Value{"domainToAdminEmail": obj(map[string]property.Value{})}
	for _, tc := range []struct {
		name        string
		maps        map[string]property.Value
		importDrift string
	}{
		{name: "absent"},
		{name: "filled", maps: map[string]property.Value{"domainToAdminEmail": obj(map[string]property.Value{"example.com": str("admin@example.com")})}},
		{name: "empty", maps: empty, importDrift: "domainToAdminEmail(add)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, id, urn := d.provider(t), d.id("google-admin"), testURN(tokenGoogleConnector)
			program := with(google.inputs(id), tc.maps)
			checked, created := contractCreate(t, d, s, google, program)

			refreshed, err := s.Read(p.ReadRequest{ID: id, Urn: urn, Properties: created.Properties, Inputs: checked})
			if err != nil || refreshed.ID != id {
				t.Fatalf("refresh: id=%q err=%v", refreshed.ID, err)
			}
			assertNoChanges(t, s, google, refreshed, program, "preview after refresh")

			imported, err := s.Read(p.ReadRequest{ID: id, Urn: urn})
			if err != nil || imported.ID != id {
				t.Fatalf("import: id=%q err=%v", imported.ID, err)
			}
			afterImport := google
			afterImport.drift = tc.importDrift
			assertNoChanges(t, s, afterImport, imported, program, "preview after import")
		})
	}

	// A refresh with 0.12.0 wrote an empty map into inputs and state, and the
	// SDK keeps an empty map that was there before. Such a stack proposes
	// removing it until one update, which sends the same config to Dex.
	t.Run("absent, state refreshed by 0.12.0", func(t *testing.T) {
		s, id, urn := d.provider(t), d.id("google-admin"), testURN(tokenGoogleConnector)
		program := google.inputs(id)
		checked, created := contractCreate(t, d, s, google, program)
		inputs := property.NewMap(with(checked.AsMap(), empty))
		state := property.NewMap(with(created.Properties.AsMap(), empty))

		refreshed, err := s.Read(p.ReadRequest{ID: id, Urn: urn, Properties: state, Inputs: inputs})
		if err != nil || refreshed.ID != id {
			t.Fatalf("refresh: id=%q err=%v", refreshed.ID, err)
		}
		before := google
		before.drift = "domainToAdminEmail(delete)"
		assertNoChanges(t, s, before, refreshed, program, "preview before the update")

		next, err := s.Check(p.CheckRequest{Urn: urn, State: refreshed.Inputs, Inputs: property.NewMap(program)})
		if err != nil || len(next.Failures) > 0 {
			t.Fatalf("Check: %v %v", err, next.Failures)
		}
		updated, err := s.Update(p.UpdateRequest{ID: id, Urn: urn, State: refreshed.Properties, OldInputs: refreshed.Inputs, Inputs: next.Inputs})
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
		again, err := s.Read(p.ReadRequest{ID: id, Urn: urn, Properties: updated.Properties, Inputs: next.Inputs})
		if err != nil || again.ID != id {
			t.Fatalf("refresh after update: id=%q err=%v", again.ID, err)
		}
		assertNoChanges(t, s, google, again, program, "preview after the update and a refresh")
	})
}

func TestGoogleConnector_domainToAdminEmailRoundTrip(t *testing.T) {
	runGoogleDomainToAdminEmailRoundTrip(t, contractDex{addr: serveFakeDex(t, newFakeDex()), id: func(name string) string { return name }})
}
