package resources

import (
	"context"
	"strings"
	"testing"

	"github.com/pulumi/pulumi-go-provider/infer"
)

// =============================================================================
// Client field clearing (DEX-029, fail-loud)
//
// Dex's UpdateClient ignores an empty name/logoUrl and empty redirectUris/
// trustedPeers lists (verified against Dex v2.46.1), so a removal would be
// recorded in state while Dex keeps the old value. Update must refuse it, in
// preview and in apply, before contacting Dex.
// =============================================================================

func clientFixture() ClientArgs {
	return ClientArgs{
		ClientId:     "app",
		Name:         "App",
		RedirectUris: []string{"https://app.example.com/callback"},
		TrustedPeers: []string{"peer"},
		LogoUrl:      strPtr("https://app.example.com/logo.png"),
	}
}

func TestClient_Update_refusesClearingFieldsDexIgnores(t *testing.T) {
	cases := []struct {
		field string
		edit  func(*ClientArgs)
	}{
		{"logoUrl", func(a *ClientArgs) { a.LogoUrl = nil }},
		{"logoUrl", func(a *ClientArgs) { a.LogoUrl = strPtr("") }},
		{"trustedPeers", func(a *ClientArgs) { a.TrustedPeers = nil }},
		{"trustedPeers", func(a *ClientArgs) { a.TrustedPeers = []string{} }},
		{"redirectUris", func(a *ClientArgs) { a.RedirectUris = []string{} }},
		{"name", func(a *ClientArgs) { a.Name = "" }},
	}
	for _, tc := range cases {
		for _, dryRun := range []bool{true, false} {
			name := tc.field + "/apply"
			if dryRun {
				name = tc.field + "/preview"
			}
			t.Run(name, func(t *testing.T) {
				inputs := clientFixture()
				tc.edit(&inputs)
				// No provider config in ctx: reaching Dex would panic, so passing
				// also proves the check runs first.
				_, err := (&Client{}).Update(context.Background(), infer.UpdateRequest[ClientArgs, ClientState]{
					ID:     "app",
					Inputs: inputs,
					State:  ClientState{ClientArgs: clientFixture()},
					DryRun: dryRun,
				})
				if err == nil {
					t.Fatalf("clearing %s must fail instead of being recorded as removed", tc.field)
				}
				for _, want := range []string{`"app"`, tc.field, "ignores"} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("error %q should mention %q", err, want)
					}
				}
			})
		}
	}
}

func TestClient_Update_reportsAllClearedFields(t *testing.T) {
	_, err := (&Client{}).Update(context.Background(), infer.UpdateRequest[ClientArgs, ClientState]{
		ID:     "app",
		Inputs: ClientArgs{ClientId: "app", Name: "App", RedirectUris: []string{"https://app.example.com/callback"}},
		State:  ClientState{ClientArgs: clientFixture()},
		DryRun: true,
	})
	if err == nil || !strings.Contains(err.Error(), "logoUrl") || !strings.Contains(err.Error(), "trustedPeers") {
		t.Fatalf("expected both logoUrl and trustedPeers to be reported, got %v", err)
	}
}

func TestClient_Update_allowsChangesDexApplies(t *testing.T) {
	cases := map[string]func(*ClientArgs){
		"replace redirect URI": func(a *ClientArgs) { a.RedirectUris = []string{"https://other.example.com/cb"} },
		"drop one of two peers": func(a *ClientArgs) {
			a.TrustedPeers = []string{"peer"}
		},
		"change logo":  func(a *ClientArgs) { a.LogoUrl = strPtr("https://app.example.com/new.png") },
		"rename":       func(a *ClientArgs) { a.Name = "Renamed" },
		"no change":    func(*ClientArgs) {},
		"add logo":     func(a *ClientArgs) { a.LogoUrl = strPtr("https://app.example.com/logo2.png") },
		"add new peer": func(a *ClientArgs) { a.TrustedPeers = append(a.TrustedPeers, "other") },
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			old := clientFixture()
			old.TrustedPeers = []string{"peer", "second"}
			inputs := old
			inputs.TrustedPeers = append([]string(nil), old.TrustedPeers...)
			edit(&inputs)
			_, err := (&Client{}).Update(context.Background(), infer.UpdateRequest[ClientArgs, ClientState]{
				ID:     "app",
				Inputs: inputs,
				State:  ClientState{ClientArgs: old},
				DryRun: true,
			})
			if err != nil {
				t.Errorf("preview of an applicable change failed: %v", err)
			}
		})
	}
}

// Fields that were already empty in Dex are not being cleared.
func TestClient_Update_allowsFieldsThatWereNeverSet(t *testing.T) {
	old := ClientArgs{ClientId: "app", Name: "App", RedirectUris: []string{"https://app.example.com/callback"}}
	_, err := (&Client{}).Update(context.Background(), infer.UpdateRequest[ClientArgs, ClientState]{
		ID:     "app",
		Inputs: old,
		State:  ClientState{ClientArgs: old},
		DryRun: true,
	})
	if err != nil {
		t.Errorf("unset optional fields must not be treated as cleared: %v", err)
	}
}
