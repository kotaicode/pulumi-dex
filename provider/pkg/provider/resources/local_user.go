package resources

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	api "github.com/dexidp/dex/api/v2"
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
	"golang.org/x/crypto/bcrypt"

	"github.com/kotaicode/pulumi-dex/pkg/provider"
)

// ============================================================================
// LocalUser - a user in Dex's password database
// ============================================================================

// LocalUserArgs defines inputs for LocalUser.
//
// Dex keys password users by their lower-cased email and cannot change the
// email or userId of an existing user, clear its username or remove all its
// groups. ListPasswords never returns the password hash.
type LocalUserArgs struct {
	Email        string   `pulumi:"email"`
	UserId       string   `pulumi:"userId"`
	Username     *string  `pulumi:"username,optional"`
	PasswordHash *string  `pulumi:"passwordHash,optional" provider:"secret"`
	Groups       []string `pulumi:"groups,optional"`
}

// LocalUserState defines outputs for LocalUser.
type LocalUserState struct {
	LocalUserArgs
}

// LocalUser manages a user in Dex's password database.
type LocalUser struct{}

// Annotate provides schema metadata.
func (c *LocalUser) Annotate(a infer.Annotator) {
	a.Describe(c, "Manages a user in Dex's password database, for logins through a password connector "+
		"(enablePasswordDB in Dex's configuration, or a LocalConnector). The password is given as a bcrypt hash, "+
		"never in plain text. Deleting the user removes only its password record: sessions and tokens that "+
		"Dex already issued stay valid until they expire.")
}

// Annotate provides schema metadata for LocalUserArgs.
func (c *LocalUserArgs) Annotate(a infer.Annotator) {
	a.Describe(&c.Email, "Email the user logs in with; it identifies the user in Dex and is stored in lower case. "+
		"Dex cannot change it: a different email fails at preview.")
	a.Describe(&c.UserId, "Stable user ID, the subject of the tokens Dex issues for this user. Dex cannot change it: "+
		"a different userId fails at preview.")
	a.Describe(&c.Username, "Display name. Can be changed, but not removed once set: Dex keeps the old value.")
	a.Describe(&c.PasswordHash, "bcrypt hash of the password ($2a$, $2b$ or $2y$, cost 10 to 16). Required to create "+
		"the user; optional after import. A new value changes the password; it is never generated. Dex does not "+
		"return the hash, so a password changed directly in Dex is not detected.")
	a.Describe(&c.Groups, "Groups of the user, which Dex adds to its tokens. Requires Dex v2.46 or later. "+
		"Dex stores them sorted and without duplicates; it cannot remove all groups from an existing user.")
}

// Annotate provides schema metadata for LocalUserState.
func (c *LocalUserState) Annotate(a infer.Annotator) {}

// WireDependencies keeps the password hash secret in every output.
func (c *LocalUser) WireDependencies(f infer.FieldSelector, _ *LocalUserArgs, state *LocalUserState) {
	f.OutputField(&state.PasswordHash).AlwaysSecret()
}

// bcryptHash matches the bcrypt variants Dex accepts.
var bcryptHash = regexp.MustCompile(`^\$2[aby]\$\d\d\$[./A-Za-z0-9]{53}$`)

// Dex's bounds for the bcrypt cost (server/passwords in v2.45 and v2.46).
const (
	minBcryptCost = 10
	maxBcryptCost = 16
)

// validPasswordHash reports a problem with a bcrypt hash without quoting it.
func validPasswordHash(hash string) string {
	if !bcryptHash.MatchString(hash) {
		return "passwordHash must be a bcrypt hash starting with $2a$, $2b$ or $2y$; see the README for how to create one"
	}
	cost, err := bcrypt.Cost([]byte(hash))
	if err != nil {
		return "passwordHash is not a valid bcrypt hash"
	}
	if cost < minBcryptCost || cost > maxBcryptCost {
		return fmt.Sprintf("passwordHash has bcrypt cost %d; Dex accepts cost %d to %d", cost, minBcryptCost, maxBcryptCost)
	}
	return ""
}

// Check validates inputs and rejects changes Dex cannot apply. Unknown values
// are not judged.
func (c *LocalUser) Check(ctx context.Context, req infer.CheckRequest) (infer.CheckResponse[LocalUserArgs], error) {
	args, failures, err := infer.DefaultCheck[LocalUserArgs](ctx, req.NewInputs)
	if err != nil {
		return infer.CheckResponse[LocalUserArgs]{Failures: failures}, err
	}
	known := func(m property.Map, key string) (property.Value, bool) {
		v, ok := m.GetOk(key)
		return v, ok && !v.IsComputed()
	}
	fail := func(prop, reason string) {
		failures = append(failures, p.CheckFailure{Property: prop, Reason: reason})
	}
	args.Email = strings.ToLower(args.Email)
	// The engine also calls Check without old inputs for an existing resource
	// (seen after pulumi import), so rules about changes run only when old
	// inputs are present, and the hash required for a create is checked in Create.
	existing := req.OldInputs.Len() > 0

	if v, ok := known(req.NewInputs, "passwordHash"); ok && v.IsString() {
		if problem := validPasswordHash(v.AsString()); problem != "" {
			fail("passwordHash", problem)
		}
	}
	if v, ok := known(req.NewInputs, "groups"); ok && v.IsArray() {
		for _, g := range v.AsArray().All() {
			if g.IsString() && g.AsString() == "" {
				fail("groups", "group names must not be empty")
				break
			}
		}
	}
	if !existing {
		return infer.CheckResponse[LocalUserArgs]{Inputs: args, Failures: failures}, nil
	}

	oldEmail, oldOK := known(req.OldInputs, "email")
	if newEmail, ok := known(req.NewInputs, "email"); ok && oldOK && oldEmail.IsString() && newEmail.IsString() &&
		!strings.EqualFold(oldEmail.AsString(), newEmail.AsString()) {
		fail("email", "Dex identifies password users by their email and cannot change it, and a replacement would "+
			"create a second user with the same userId. To give the user a new email, add a LocalUser with the new email, "+
			"then remove this one")
	}
	oldID, oldIDOK := known(req.OldInputs, "userId")
	if newID, ok := known(req.NewInputs, "userId"); ok && oldIDOK && oldID.IsString() && newID.IsString() &&
		oldID.AsString() != newID.AsString() {
		fail("userId", "Dex cannot change the userId of a password user; it is the subject of the tokens Dex issues. "+
			"To use another userId, add a LocalUser for it, then remove this one")
	}
	if old, ok := known(req.OldInputs, "username"); ok && old.IsString() && old.AsString() != "" {
		if cleared(req.NewInputs, "username") {
			fail("username", "Dex cannot remove a username once set: an empty value leaves the old one in place. "+
				"Keep a username, or delete the user and create it again")
		}
	}
	if old, ok := known(req.OldInputs, "groups"); ok && old.IsArray() && old.AsArray().Len() > 0 {
		if cleared(req.NewInputs, "groups") {
			fail("groups", "Dex cannot remove all groups from a password user: an empty list leaves the old groups in place. "+
				"Keep at least one group, or delete the user and create it again")
		}
	}
	return infer.CheckResponse[LocalUserArgs]{Inputs: args, Failures: failures}, nil
}

// cleared reports whether the inputs leave key out or set it empty. An
// unknown value is not cleared.
func cleared(m property.Map, key string) bool {
	v, ok := m.GetOk(key)
	switch {
	case !ok || v.IsNull():
		return true
	case v.IsString():
		return v.AsString() == ""
	case v.IsArray():
		return v.AsArray().Len() == 0
	default:
		return false
	}
}

// requireGroups refuses groups on a Dex that would drop them. In a preview
// whose provider configuration is not known yet, the check waits for the
// update itself.
func requireGroups(cfg provider.DexConfig, dryRun bool, op, id string, groups []string) error {
	if len(groups) == 0 || (dryRun && cfg.Server == nil) {
		return nil
	}
	if err := cfg.Require(provider.LocalUserGroups); err != nil {
		return provider.WrapError(op, "local-user", id, err)
	}
	return nil
}

// Create creates a password user.
func (c *LocalUser) Create(ctx context.Context, req infer.CreateRequest[LocalUserArgs]) (infer.CreateResponse[LocalUserState], error) {
	args := req.Inputs
	args.Email = strings.ToLower(args.Email)
	cfg := infer.GetConfig[provider.DexConfig](ctx)
	if err := requireGroups(cfg, req.DryRun, "create", args.Email, args.Groups); err != nil {
		return infer.CreateResponse[LocalUserState]{}, err
	}
	if req.DryRun {
		return infer.CreateResponse[LocalUserState]{ID: args.Email, Output: LocalUserState{LocalUserArgs: args}}, nil
	}
	// Checked here rather than in preview: a preview cannot tell an omitted hash
	// from one that is still unknown, such as another resource's output.
	hash := provider.PtrOr(args.PasswordHash, "")
	if hash == "" {
		return infer.CreateResponse[LocalUserState]{}, provider.WrapError("create", "local-user", args.Email,
			fmt.Errorf("passwordHash is required to create a user: Dex stores only the bcrypt hash of the password"))
	}
	if cfg.Client == nil {
		return infer.CreateResponse[LocalUserState]{}, fmt.Errorf("dex client not configured")
	}

	createCtx, cancel := context.WithTimeout(ctx, time.Duration(provider.PtrOr(cfg.TimeoutSeconds, 5))*time.Second)
	defer cancel()
	resp, err := cfg.Client.CreatePassword(createCtx, &api.CreatePasswordReq{Password: &api.Password{
		Email:    args.Email,
		Hash:     []byte(hash),
		Username: provider.PtrOr(args.Username, ""),
		UserId:   args.UserId,
		Groups:   args.Groups,
	}})
	target := createTarget{kind: "local-user", token: tokenLocalUser, name: req.Name, id: args.Email}
	if err != nil {
		return infer.CreateResponse[LocalUserState]{}, target.failed(err)
	}
	if resp.GetAlreadyExists() {
		return infer.CreateResponse[LocalUserState]{}, target.alreadyExists()
	}
	return infer.CreateResponse[LocalUserState]{ID: args.Email, Output: LocalUserState{LocalUserArgs: args}}, nil
}

// Read reports the user as Dex stores it. Dex does not return the password
// hash, so the last supplied hash is kept; an import has none.
func (c *LocalUser) Read(ctx context.Context, req infer.ReadRequest[LocalUserArgs, LocalUserState]) (infer.ReadResponse[LocalUserArgs, LocalUserState], error) {
	cfg := infer.GetConfig[provider.DexConfig](ctx)
	if cfg.Client == nil {
		return infer.ReadResponse[LocalUserArgs, LocalUserState]{}, fmt.Errorf("dex client not configured")
	}
	listCtx, cancel := context.WithTimeout(ctx, time.Duration(provider.PtrOr(cfg.TimeoutSeconds, 5))*time.Second)
	defer cancel()
	resp, err := cfg.Client.ListPasswords(listCtx, &api.ListPasswordReq{})
	if err != nil {
		return infer.ReadResponse[LocalUserArgs, LocalUserState]{}, provider.WrapError("read", "local-user", req.ID, err)
	}
	var found *api.Password
	for _, pw := range resp.GetPasswords() {
		if strings.EqualFold(pw.GetEmail(), req.ID) {
			found = pw
			break
		}
	}
	if found == nil {
		return infer.ReadResponse[LocalUserArgs, LocalUserState]{}, nil
	}

	hash := req.Inputs.PasswordHash
	if hash == nil {
		hash = req.State.PasswordHash
	}
	args := LocalUserArgs{
		Email:        strings.ToLower(found.GetEmail()),
		UserId:       found.GetUserId(),
		Username:     usernameFromDex(found.GetUsername(), req.Inputs.Username),
		PasswordHash: hash,
		Groups:       groupsFromDex(found.GetGroups(), req.Inputs.Groups),
	}
	return infer.ReadResponse[LocalUserArgs, LocalUserState]{
		ID: args.Email, Inputs: args, State: LocalUserState{LocalUserArgs: args},
	}, nil
}

// usernameFromDex reports Dex's username; an empty one reads back the way the
// program gave it (omitted or empty), so refresh alone shows no change.
func usernameFromDex(dex string, prior *string) *string {
	if dex == "" {
		if prior != nil && *prior == "" {
			return prior
		}
		return nil
	}
	return &dex
}

// groupsFromDex reports Dex's groups. Dex stores them sorted and without
// duplicates; when they are the same set as the program's, the program's list
// is kept so that refresh alone shows no change.
func groupsFromDex(dex, prior []string) []string {
	if sameGroups(dex, prior) {
		return prior
	}
	if len(dex) == 0 {
		return nil
	}
	return dex
}

func sameGroups(a, b []string) bool {
	norm := func(s []string) []string {
		out := slices.Clone(s)
		slices.Sort(out)
		return slices.Compact(out)
	}
	return slices.Equal(norm(a), norm(b))
}

// Update changes the password, username or groups in place.
func (c *LocalUser) Update(ctx context.Context, req infer.UpdateRequest[LocalUserArgs, LocalUserState]) (infer.UpdateResponse[LocalUserState], error) {
	args := req.Inputs
	args.Email = strings.ToLower(args.Email)
	old := req.State
	// The identity checks also run in Check; here they protect against an
	// update whose old inputs were not available there.
	if !strings.EqualFold(args.Email, old.Email) || args.UserId != old.UserId {
		return infer.UpdateResponse[LocalUserState]{}, provider.WrapError("update", "local-user", old.Email,
			fmt.Errorf("the email and userId of a password user cannot change in Dex; add a new LocalUser and remove this one"))
	}
	cfg := infer.GetConfig[provider.DexConfig](ctx)
	groupsChanged := len(args.Groups) > 0 && !sameGroups(args.Groups, old.Groups)
	if groupsChanged {
		if err := requireGroups(cfg, req.DryRun, "update", old.Email, args.Groups); err != nil {
			return infer.UpdateResponse[LocalUserState]{}, err
		}
	}

	update := &api.UpdatePasswordReq{Email: old.Email}
	if h := provider.PtrOr(args.PasswordHash, ""); h != "" && h != provider.PtrOr(old.PasswordHash, "") {
		update.NewHash = []byte(h)
	}
	if u := provider.PtrOr(args.Username, ""); u != "" && u != provider.PtrOr(old.Username, "") {
		update.NewUsername = u
	}
	if groupsChanged {
		update.NewGroups = args.Groups
	}
	// The hash in state is the last one supplied; an omitted hash keeps it, so
	// the first update after an import never resets the password.
	if args.PasswordHash == nil {
		args.PasswordHash = old.PasswordHash
	}
	state := LocalUserState{LocalUserArgs: args}
	if req.DryRun || (update.NewHash == nil && update.NewUsername == "" && update.NewGroups == nil) {
		return infer.UpdateResponse[LocalUserState]{Output: state}, nil
	}
	if cfg.Client == nil {
		return infer.UpdateResponse[LocalUserState]{}, fmt.Errorf("dex client not configured")
	}

	updateCtx, cancel := context.WithTimeout(ctx, time.Duration(provider.PtrOr(cfg.TimeoutSeconds, 5))*time.Second)
	defer cancel()
	resp, err := cfg.Client.UpdatePassword(updateCtx, update)
	if err != nil {
		return infer.UpdateResponse[LocalUserState]{}, provider.WrapError("update", "local-user", old.Email, err)
	}
	if resp.GetNotFound() {
		return infer.UpdateResponse[LocalUserState]{}, provider.NotFoundError("update", "local-user", old.Email)
	}
	return infer.UpdateResponse[LocalUserState]{Output: state}, nil
}

// Delete removes the password record. It does not end sessions or revoke
// tokens that Dex already issued. Deleting a missing user succeeds.
func (c *LocalUser) Delete(ctx context.Context, req infer.DeleteRequest[LocalUserState]) (infer.DeleteResponse, error) {
	cfg := infer.GetConfig[provider.DexConfig](ctx)
	if cfg.Client == nil {
		return infer.DeleteResponse{}, fmt.Errorf("dex client not configured")
	}
	email := req.ID
	if email == "" {
		email = req.State.Email
	}
	deleteCtx, cancel := context.WithTimeout(ctx, time.Duration(provider.PtrOr(cfg.TimeoutSeconds, 5))*time.Second)
	defer cancel()
	if _, err := cfg.Client.DeletePassword(deleteCtx, &api.DeletePasswordReq{Email: email}); err != nil {
		return infer.DeleteResponse{}, provider.WrapError("delete", "local-user", email, err)
	}
	return infer.DeleteResponse{}, nil
}
