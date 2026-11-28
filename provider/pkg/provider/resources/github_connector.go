package resources

import (
	"context"
	"encoding/json"
	"fmt"

	api "github.com/dexidp/dex/api/v2"
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
)

// ============================================================================
// GitHubConnector - GitHub OAuth2 connector (type: "github")
// ============================================================================

// GitHubOrg represents a GitHub organization with optional teams.
type GitHubOrg struct {
	Name  string   `pulumi:"name"`
	Teams []string `pulumi:"teams,optional"`
}

// GitHubConnectorArgs defines inputs for GitHubConnector.
type GitHubConnectorArgs struct {
	ConnectorId          string      `pulumi:"connectorId" provider:"replaceOnChanges"`
	Name                 string      `pulumi:"name"`
	ClientId             string      `pulumi:"clientId"`
	ClientSecret         string      `pulumi:"clientSecret" provider:"secret"` //nolint:gosec // G117: Intentionally exported secret field for Pulumi provider
	RedirectUri          string      `pulumi:"redirectUri"`
	Orgs                 []GitHubOrg `pulumi:"orgs,optional"`
	LoadAllGroups        *bool       `pulumi:"loadAllGroups,optional"`
	TeamNameField        *string     `pulumi:"teamNameField,optional"`
	UseLoginAsID         *bool       `pulumi:"useLoginAsID,optional"`
	PreferredEmailDomain *string     `pulumi:"preferredEmailDomain,optional"`
	HostName             *string     `pulumi:"hostName,optional"` // For GitHub Enterprise
	RootCA               *string     `pulumi:"rootCA,optional"`   // For GitHub Enterprise
}

// GitHubConnectorState defines outputs for GitHubConnector.
type GitHubConnectorState struct {
	GitHubConnectorArgs
}

// GitHubConnector manages a GitHub connector in Dex.
type GitHubConnector struct{}

// Annotate provides schema metadata.
func (c *GitHubConnector) Annotate(a infer.Annotator) {
	a.Describe(c, "Manages a GitHub connector in Dex. This connector allows users to authenticate using their GitHub accounts and supports organization and team-based access control.")
}

// Annotate provides schema metadata for GitHubConnectorArgs.
func (c *GitHubConnectorArgs) Annotate(a infer.Annotator) {
	a.Describe(&c.ConnectorId, "Unique identifier for the GitHub connector.")
	a.Describe(&c.Name, "Human-readable name for the connector, displayed to users during login.")
	a.Describe(&c.ClientId, "GitHub OAuth app client ID.")
	a.Describe(&c.ClientSecret, "GitHub OAuth app client secret.")
	a.Describe(&c.RedirectUri, "Redirect URI registered in GitHub OAuth app. Must match Dex's callback URL.")
	a.Describe(&c.Orgs, "List of GitHub organizations with optional team restrictions. Only users in these orgs/teams will be allowed to authenticate.")
	a.Describe(&c.LoadAllGroups, "If true, load all groups (teams) the user is a member of. Defaults to false.")
	a.Describe(&c.TeamNameField, "Field to use for team names in group claims. Valid values: 'name', 'slug', or 'both'. Defaults to 'slug'.")
	a.Describe(&c.UseLoginAsID, "If true, use GitHub login username as the user ID. Defaults to false.")
	a.Describe(&c.PreferredEmailDomain, "Preferred email domain. If set, users with emails in this domain will be preferred.")
	a.Describe(&c.HostName, "GitHub Enterprise hostname (e.g., 'github.example.com'). Leave empty for github.com.")
	a.Describe(&c.RootCA, "Root CA certificate for GitHub Enterprise (PEM format). Required if using self-signed certificates.")
}

// Annotate provides schema metadata for GitHubOrg.
func (c *GitHubOrg) Annotate(a infer.Annotator) {
	a.Describe(&c.Name, "GitHub organization name.")
	a.Describe(&c.Teams, "List of team names within the organization. If empty, all members of the organization can authenticate.")
}

// Annotate provides schema metadata for GitHubConnectorState.
func (c *GitHubConnectorState) Annotate(a infer.Annotator) {
	// GitHubConnectorState embeds GitHubConnectorArgs, so field descriptions are inherited
}

// WireDependencies marks credential-bearing outputs secret regardless of how the
// input was supplied, so plain literals, refresh and import never store them in plaintext.
func (c *GitHubConnector) WireDependencies(f infer.FieldSelector, _ *GitHubConnectorArgs, state *GitHubConnectorState) {
	f.OutputField(&state.ClientSecret).AlwaysSecret()
}

// Check validates inputs.
func (c *GitHubConnector) Check(ctx context.Context, req infer.CheckRequest) (infer.CheckResponse[GitHubConnectorArgs], error) {
	args, failures, err := infer.DefaultCheck[GitHubConnectorArgs](ctx, req.NewInputs)
	if err != nil {
		return infer.CheckResponse[GitHubConnectorArgs]{Failures: failures}, err
	}

	// Validate teamNameField
	if args.TeamNameField != nil {
		valid := map[string]bool{"name": true, "slug": true, "both": true}
		if !valid[*args.TeamNameField] {
			failures = append(failures, p.CheckFailure{
				Property: "teamNameField",
				Reason:   "must be one of: name, slug, both",
			})
		}
	}

	// Apply defaults
	if args.LoadAllGroups == nil {
		defaultLoadAll := false
		args.LoadAllGroups = &defaultLoadAll
	}
	if args.TeamNameField == nil {
		defaultTeamNameField := "slug"
		args.TeamNameField = &defaultTeamNameField
	}
	if args.UseLoginAsID == nil {
		defaultUseLogin := false
		args.UseLoginAsID = &defaultUseLogin
	}

	return infer.CheckResponse[GitHubConnectorArgs]{
		Inputs:   args,
		Failures: failures,
	}, nil
}

func (c *GitHubConnector) controller() connectorController[GitHubConnectorArgs, GitHubConnectorState] {
	return connectorController[GitHubConnectorArgs, GitHubConnectorState]{
		kind:    "github-connector",
		token:   tokenGitHubConnector,
		dexType: "github",
		id:      func(a GitHubConnectorArgs) string { return a.ConnectorId },
		name:    func(a GitHubConnectorArgs) string { return a.Name },
		state:   func(a GitHubConnectorArgs) GitHubConnectorState { return GitHubConnectorState{GitHubConnectorArgs: a} },
		args:    func(s GitHubConnectorState) GitHubConnectorArgs { return s.GitHubConnectorArgs },
		encode:  encodeGitHubConfig,
		decode:  decodeGitHubConfig,
	}
}

func encodeGitHubConfig(args GitHubConnectorArgs) ([]byte, error) {
	githubConfig := map[string]any{
		"clientID":     args.ClientId,
		"clientSecret": args.ClientSecret,
		"redirectURI":  args.RedirectUri,
	}

	if len(args.Orgs) > 0 {
		orgsConfig := make([]map[string]any, 0, len(args.Orgs))
		for _, org := range args.Orgs {
			orgConfig := map[string]any{"name": org.Name}
			if len(org.Teams) > 0 {
				orgConfig["teams"] = org.Teams
			}
			orgsConfig = append(orgsConfig, orgConfig)
		}
		githubConfig["orgs"] = orgsConfig
	}
	if args.LoadAllGroups != nil {
		githubConfig["loadAllGroups"] = *args.LoadAllGroups
	}
	if args.TeamNameField != nil {
		githubConfig["teamNameField"] = *args.TeamNameField
	}
	if args.UseLoginAsID != nil {
		githubConfig["useLoginAsID"] = *args.UseLoginAsID
	}
	if args.PreferredEmailDomain != nil {
		githubConfig["preferredEmailDomain"] = *args.PreferredEmailDomain
	}
	if args.HostName != nil {
		githubConfig["hostName"] = *args.HostName
	}
	if args.RootCA != nil {
		githubConfig["rootCA"] = *args.RootCA
	}

	configBytes, err := json.Marshal(githubConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal GitHub config: %w", err)
	}
	return configBytes, nil
}

func decodeGitHubConfig(found *api.Connector, _ GitHubConnectorArgs) (GitHubConnectorArgs, error) {
	conf, err := parseConnectorConfig(found.Config)
	if err != nil {
		return GitHubConnectorArgs{}, err
	}

	var orgs []GitHubOrg
	for _, org := range conf.objects("orgs") {
		orgs = append(orgs, GitHubOrg{Name: org.str("name"), Teams: org.strList("teams")})
	}

	args := GitHubConnectorArgs{
		ConnectorId:          found.Id,
		Name:                 found.Name,
		ClientId:             conf.str("clientID"),
		ClientSecret:         conf.str("clientSecret"),
		RedirectUri:          conf.str("redirectURI"),
		Orgs:                 orgs,
		LoadAllGroups:        conf.boolPtr("loadAllGroups"),
		TeamNameField:        conf.strPtr("teamNameField"),
		UseLoginAsID:         conf.boolPtr("useLoginAsID"),
		PreferredEmailDomain: conf.strPtr("preferredEmailDomain"),
		HostName:             conf.strPtr("hostName"),
		RootCA:               conf.strPtr("rootCA"),
	}
	return args, conf.Err()
}

// Create creates a new GitHub connector.
func (c *GitHubConnector) Create(ctx context.Context, req infer.CreateRequest[GitHubConnectorArgs]) (infer.CreateResponse[GitHubConnectorState], error) {
	return c.controller().create(ctx, req)
}

// Read retrieves an existing GitHub connector.
func (c *GitHubConnector) Read(ctx context.Context, req infer.ReadRequest[GitHubConnectorArgs, GitHubConnectorState]) (infer.ReadResponse[GitHubConnectorArgs, GitHubConnectorState], error) {
	return c.controller().read(ctx, req)
}

// Update updates an existing GitHub connector.
func (c *GitHubConnector) Update(ctx context.Context, req infer.UpdateRequest[GitHubConnectorArgs, GitHubConnectorState]) (infer.UpdateResponse[GitHubConnectorState], error) {
	return c.controller().update(ctx, req)
}

// Delete deletes a GitHub connector.
func (c *GitHubConnector) Delete(ctx context.Context, req infer.DeleteRequest[GitHubConnectorState]) (infer.DeleteResponse, error) {
	return c.controller().delete(ctx, req)
}
