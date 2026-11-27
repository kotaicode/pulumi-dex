package resources

import (
	"strings"
	"testing"

	api "github.com/dexidp/dex/api/v2"
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

// =============================================================================
// LocalUser
//
// Semantics verified against Dex v2.45.1 and v2.46.1: users are keyed by the
// lower-cased email; ListPasswords never returns the hash; UpdatePassword
// cannot change email or userId, clear the username or remove all groups;
// v2.46 sorts and de-duplicates groups, v2.45 drops them silently.
// =============================================================================

// Synthetic bcrypt hashes (cost 10 and 12) of throwaway test passwords.
const (
	testPasswordHash    = "$2a$10$yrNHkDh2gKcRvWit4L9eqei6YHKVn4x2T5Gve4orYfTXu179rI/qe"
	rotatedPasswordHash = "$2y$12$g1amNS6Wr/4QlrCaR.hiT.vDHDHArl0s543Lj5hMO0XimXi4sCbd."
)

const userToken = "dex:resources:LocalUser"

func userInputs(extra map[string]property.Value) map[string]property.Value {
	return with(map[string]property.Value{
		"email": str("alice@example.com"), "userId": str("u-alice"), "passwordHash": str(testPasswordHash),
	}, extra)
}

func storedUser(t *testing.T, dex *fakeDex, email string) *api.Password {
	t.Helper()
	dex.mu.Lock()
	defer dex.mu.Unlock()
	pw := dex.passwords[strings.ToLower(email)]
	if pw == nil {
		t.Fatalf("no password user %s in Dex", email)
	}
	return pw
}

func TestLocalUser_createAndRefresh(t *testing.T) {
	dex := newFakeDex()
	s := newProviderAgainst(t, dex)
	urn := testURN(userToken)
	program := property.NewMap(userInputs(map[string]property.Value{
		"email": str("Alice@Example.com"), "username": str("Alice"), "groups": strs("devs", "admins", "devs"),
	}))
	checked, err := s.Check(p.CheckRequest{Urn: urn, Inputs: program})
	if err != nil || len(checked.Failures) > 0 {
		t.Fatalf("Check: %v %v", err, checked.Failures)
	}
	created, err := s.Create(p.CreateRequest{Urn: urn, Properties: checked.Inputs})
	if err != nil || created.ID != "alice@example.com" {
		t.Fatalf("Create: id=%q err=%v", created.ID, err)
	}
	pw := storedUser(t, dex, "alice@example.com")
	if pw.UserId != "u-alice" || pw.Username != "Alice" || string(pw.Hash) != testPasswordHash || strings.Join(pw.Groups, ",") != "admins,devs" {
		t.Errorf("stored user = %+v", pw)
	}
	if !created.Properties.Get("passwordHash").IsSecret() {
		t.Error("passwordHash output is not secret")
	}

	read, err := s.Read(p.ReadRequest{ID: created.ID, Urn: urn, Properties: created.Properties, Inputs: checked.Inputs})
	if err != nil || read.ID != created.ID {
		t.Fatalf("Read: id=%q err=%v", read.ID, err)
	}
	if v := read.Inputs.Get("passwordHash"); !v.IsSecret() || v.AsString() != testPasswordHash {
		t.Error("refresh must keep the supplied hash, as a secret: Dex does not return it")
	}
	rechecked, err := s.Check(p.CheckRequest{Urn: urn, State: read.Inputs, Inputs: program})
	if err != nil || len(rechecked.Failures) > 0 {
		t.Fatalf("re-Check: %v %v", err, rechecked.Failures)
	}
	diff, err := s.Diff(p.DiffRequest{ID: read.ID, Urn: urn, State: read.Inputs, OldInputs: read.Inputs, Inputs: rechecked.Inputs})
	if err != nil || diff.HasChanges {
		t.Errorf("preview after refresh proposes changes: %s (err %v)", diffKeys(diff), err)
	}
}

func TestLocalUser_passwordHashValidation(t *testing.T) {
	for name, hash := range map[string]string{
		"plain text":       "correct horse battery staple",
		"md5 crypt":        "$1$saltsalt$qjXMvbEw8oaL.CzflDugX/",
		"cost below 10":    "$2a$09$yrNHkDh2gKcRvWit4L9eqei6YHKVn4x2T5Gve4orYfTXu179rI/qe",
		"cost above 16":    "$2a$17$yrNHkDh2gKcRvWit4L9eqei6YHKVn4x2T5Gve4orYfTXu179rI/qe",
		"truncated bcrypt": "$2a$10$yrNHkDh2gKcRvWit4L9eq",
	} {
		t.Run(name, func(t *testing.T) {
			resp, err := newTestProvider(t).Check(p.CheckRequest{Urn: testURN(userToken), Inputs: property.NewMap(userInputs(map[string]property.Value{"passwordHash": str(hash)}))})
			if err != nil {
				t.Fatal(err)
			}
			wantFailure(t, resp.Failures, "passwordHash", "passwordHash")
			for _, f := range resp.Failures {
				if strings.Contains(f.Reason, hash) {
					t.Errorf("failure quotes the hash: %s", f.Reason)
				}
			}
		})
	}
	for name, inputs := range map[string]map[string]property.Value{
		"$2y$ from htpasswd": userInputs(map[string]property.Value{"passwordHash": str(rotatedPasswordHash)}),
		"unknown hash":       userInputs(map[string]property.Value{"passwordHash": property.New(property.Computed)}),
	} {
		resp, err := newTestProvider(t).Check(p.CheckRequest{Urn: testURN(userToken), Inputs: property.NewMap(inputs)})
		if err != nil || len(resp.Failures) > 0 {
			t.Errorf("%s: %v %v", name, err, resp.Failures)
		}
	}
	// A missing hash fails the create before anything is written to Dex.
	dex := newFakeDex()
	_, err := create(t, dex, adoptionCase{token: userToken, id: "alice@example.com", user: true,
		inputs: map[string]property.Value{"email": str("alice@example.com"), "userId": str("u-alice")}})
	if err == nil || !strings.Contains(err.Error(), "passwordHash is required to create") {
		t.Errorf("create without a hash = %v", err)
	}
	if dex.writes != 0 {
		t.Errorf("create without a hash wrote %d times to Dex", dex.writes)
	}
}

func TestLocalUser_groupsNeedDexV246(t *testing.T) {
	dex := newFakeDex()
	dex.version = &api.VersionResp{Server: "v2.45.1", Api: 3}
	s := newProviderAgainst(t, dex)
	urn := testURN(userToken)
	checked, err := s.Check(p.CheckRequest{Urn: urn, Inputs: property.NewMap(userInputs(map[string]property.Value{"groups": strs("admins")}))})
	if err != nil || len(checked.Failures) > 0 {
		t.Fatalf("Check: %v %v", err, checked.Failures)
	}
	if _, err := s.Create(p.CreateRequest{Urn: urn, Properties: checked.Inputs, DryRun: true}); err == nil ||
		!strings.Contains(err.Error(), "needs Dex v2.46") {
		t.Errorf("preview with groups on Dex v2.45 = %v, want a refusal naming v2.46", err)
	}

	// Without groups the user works on v2.45 too; adding groups later is refused.
	_, created, old := createFor(t, dex, userToken, userInputs(nil))
	dex.mu.Lock()
	writes := dex.writes
	dex.mu.Unlock()
	next, err := s.Check(p.CheckRequest{Urn: urn, State: old, Inputs: property.NewMap(userInputs(map[string]property.Value{"groups": strs("admins")}))})
	if err != nil || len(next.Failures) > 0 {
		t.Fatalf("re-Check: %v %v", err, next.Failures)
	}
	if _, err := s.Update(p.UpdateRequest{ID: created.ID, Urn: urn, State: created.Properties, OldInputs: old, Inputs: next.Inputs, DryRun: true}); err == nil ||
		!strings.Contains(err.Error(), "needs Dex v2.46") {
		t.Errorf("adding groups on Dex v2.45 = %v, want a refusal", err)
	}
	if dex.writes != writes {
		t.Error("the refused update wrote to Dex")
	}
}

func TestLocalUser_identityChangesFailAtPreview(t *testing.T) {
	for name, tc := range map[string]struct {
		to   map[string]property.Value
		prop string
	}{
		"new email":                {userInputs(map[string]property.Value{"email": str("alice.new@example.com")}), "email"},
		"new userId":               {userInputs(map[string]property.Value{"userId": str("u-other")}), "userId"},
		"username removed":         {userInputs(nil), "username"},
		"username emptied":         {userInputs(map[string]property.Value{"username": str("")}), "username"},
		"all groups removed":       {userInputs(map[string]property.Value{"username": str("Alice")}), "groups"},
		"groups set to empty list": {userInputs(map[string]property.Value{"username": str("Alice"), "groups": strs()}), "groups"},
	} {
		t.Run(name, func(t *testing.T) {
			from := userInputs(map[string]property.Value{"username": str("Alice"), "groups": strs("admins")})
			if tc.prop != "groups" {
				tc.to = with(tc.to, map[string]property.Value{"groups": strs("admins")})
			}
			_, err := changeTo(t, newFakeDex(), userToken, from, tc.to, true)
			if err == nil || !strings.HasPrefix(err.Error(), tc.prop+":") {
				t.Errorf("preview error = %v, want a failure on %s", err, tc.prop)
			}
		})
	}
	// A different spelling of the same email is the same user.
	if _, err := changeTo(t, newFakeDex(), userToken, userInputs(nil),
		userInputs(map[string]property.Value{"email": str("ALICE@example.com")}), true); err != nil {
		t.Errorf("email case change: %v", err)
	}
}

func TestLocalUser_updatesInPlace(t *testing.T) {
	from := userInputs(map[string]property.Value{"username": str("Alice"), "groups": strs("admins")})

	dex := newFakeDex()
	if _, err := changeTo(t, dex, userToken, from, with(from, map[string]property.Value{"username": str("Alice B."), "groups": strs("admins", "devs")}), false); err != nil {
		t.Fatalf("update: %v", err)
	}
	pw := storedUser(t, dex, "alice@example.com")
	if pw.Username != "Alice B." || strings.Join(pw.Groups, ",") != "admins,devs" || string(pw.Hash) != testPasswordHash {
		t.Errorf("after username and groups update: %+v", pw)
	}

	// Rotating the password keeps username and groups.
	dex = newFakeDex()
	if _, err := changeTo(t, dex, userToken, from, with(from, map[string]property.Value{"passwordHash": str(rotatedPasswordHash)}), false); err != nil {
		t.Fatalf("rotation: %v", err)
	}
	pw = storedUser(t, dex, "alice@example.com")
	if string(pw.Hash) != rotatedPasswordHash || pw.Username != "Alice" || strings.Join(pw.Groups, ",") != "admins" {
		t.Errorf("after rotation: %+v", pw)
	}

	// A reordered group list is the same set: no write.
	dex = newFakeDex()
	from = userInputs(map[string]property.Value{"groups": strs("devs", "admins")})
	_, created, old := createFor(t, dex, userToken, from)
	dex.mu.Lock()
	writes := dex.writes
	dex.mu.Unlock()
	s := newProviderAgainst(t, dex)
	next, err := s.Check(p.CheckRequest{Urn: testURN(userToken), State: old, Inputs: property.NewMap(userInputs(map[string]property.Value{"groups": strs("admins", "devs")}))})
	if err != nil || len(next.Failures) > 0 {
		t.Fatalf("re-Check: %v %v", err, next.Failures)
	}
	if _, err := s.Update(p.UpdateRequest{ID: created.ID, Urn: testURN(userToken), State: created.Properties, OldInputs: old, Inputs: next.Inputs}); err != nil {
		t.Fatal(err)
	}
	if dex.writes != writes {
		t.Error("reordering groups wrote to Dex")
	}
}

// Import has no password hash. The first update afterwards must not reset or
// clear the password.
func TestLocalUser_importWithoutPassword(t *testing.T) {
	dex := newFakeDex()
	dex.passwords["bob@example.com"] = &api.Password{Email: "bob@example.com", Hash: []byte(testPasswordHash), UserId: "u-bob", Username: "Bob"}
	s := newProviderAgainst(t, dex)
	urn := testURN(userToken)
	imported, err := s.Read(p.ReadRequest{ID: "bob@example.com", Urn: urn})
	if err != nil || imported.ID != "bob@example.com" {
		t.Fatalf("import: id=%q err=%v", imported.ID, err)
	}
	if _, ok := imported.Inputs.GetOk("passwordHash"); ok {
		t.Error("import invented a password hash")
	}
	program := property.NewMap(map[string]property.Value{"email": str("bob@example.com"), "userId": str("u-bob"), "username": str("Bobby")})
	checked, err := s.Check(p.CheckRequest{Urn: urn, State: imported.Inputs, Inputs: program})
	if err != nil || len(checked.Failures) > 0 {
		t.Fatalf("Check after import: %v %v", err, checked.Failures)
	}
	if _, err := s.Update(p.UpdateRequest{ID: imported.ID, Urn: urn, State: imported.Properties, OldInputs: imported.Inputs, Inputs: checked.Inputs}); err != nil {
		t.Fatalf("first update after import: %v", err)
	}
	pw := storedUser(t, dex, "bob@example.com")
	if string(pw.Hash) != testPasswordHash || pw.Username != "Bobby" {
		t.Errorf("after the first update: username %q, hash kept %v", pw.Username, string(pw.Hash) == testPasswordHash)
	}
}

// Importing an email that Dex does not know fails instead of importing an
// empty user; a refresh of a user deleted in Dex still removes it from state.
func TestLocalUser_importOfMissingUserFails(t *testing.T) {
	dex := newFakeDex()
	s := newProviderAgainst(t, dex)
	urn := testURN(userToken)
	if _, err := s.Read(p.ReadRequest{ID: "nobody@example.com", Urn: urn}); err == nil || !strings.Contains(err.Error(), "not found in Dex") {
		t.Errorf("import of a missing user = %v, want an error", err)
	}
	_, created, old := createFor(t, dex, userToken, userInputs(nil))
	dex.mu.Lock()
	delete(dex.passwords, created.ID)
	dex.mu.Unlock()
	resp, err := s.Read(p.ReadRequest{ID: created.ID, Urn: urn, Properties: created.Properties, Inputs: old})
	if err != nil || resp.ID != "" {
		t.Errorf("refresh of a deleted user: id=%q err=%v, want it gone", resp.ID, err)
	}
}

func TestLocalUser_staticUserAndDelete(t *testing.T) {
	dex := newFakeDex()
	dex.staticPasswords = map[string]bool{"admin@example.com": true}
	_, err := create(t, dex, adoptionCase{token: userToken, id: "admin@example.com", user: true,
		inputs: userInputs(map[string]property.Value{"email": str("admin@example.com")})})
	if err == nil || !strings.Contains(err.Error(), "defined in Dex's configuration") {
		t.Errorf("creating a static user: %v", err)
	}

	s := newProviderAgainst(t, dex)
	gone := property.NewMap(map[string]property.Value{"email": str("missing@example.com"), "userId": str("u-missing")})
	if err := s.Delete(p.DeleteRequest{ID: "missing@example.com", Urn: testURN(userToken), Properties: gone}); err != nil {
		t.Errorf("deleting a missing user: %v", err)
	}
}
