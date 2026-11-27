package resources

import (
	"context"
	"encoding/json"
	"fmt"

	api "github.com/dexidp/dex/api/v2"
	"github.com/pulumi/pulumi-go-provider/infer"
)

// ============================================================================
// GitLabConnector - GitLab OAuth2 connector (type: "gitlab")
// ============================================================================

// GitLabConnectorArgs defines inputs for GitLabConnector.
type GitLabConnectorArgs struct {
	ConnectorId         string   `pulumi:"connectorId" provider:"replaceOnChanges"`
	Name                string   `pulumi:"name"`
	BaseURL             *string  `pulumi:"baseURL,optional"`
	ClientId            string   `pulumi:"clientId"`
	ClientSecret        string   `pulumi:"clientSecret" provider:"secret"` //nolint:gosec // G117: Intentionally exported secret field for Pulumi provider
	RedirectUri         string   `pulumi:"redirectUri"`
	Groups              []string `pulumi:"groups,optional"`
	UseLoginAsID        *bool    `pulumi:"useLoginAsID,optional"`
	GetGroupsPermission *bool    `pulumi:"getGroupsPermission,optional"`
}

// GitLabConnectorState defines outputs for GitLabConnector.
type GitLabConnectorState struct {
	GitLabConnectorArgs
}

// GitLabConnector manages a GitLab connector in Dex.
type GitLabConnector struct{}

// Annotate provides schema metadata.
func (c *GitLabConnector) Annotate(a infer.Annotator) {
	a.Describe(c, "Manages a GitLab connector in Dex. This connector allows users to authenticate using their GitLab accounts and supports group-based access control.")
}

// Annotate provides schema metadata for GitLabConnectorArgs.
func (c *GitLabConnectorArgs) Annotate(a infer.Annotator) {
	a.Describe(&c.ConnectorId, "Unique identifier for the GitLab connector.")
	a.Describe(&c.Name, "Human-readable name for the connector, displayed to users during login.")
	a.Describe(&c.BaseURL, "GitLab instance base URL. Defaults to 'https://gitlab.com' for GitLab.com.")
	a.Describe(&c.ClientId, "GitLab OAuth application client ID.")
	a.Describe(&c.ClientSecret, "GitLab OAuth application client secret.")
	a.Describe(&c.RedirectUri, "Redirect URI registered in GitLab OAuth app. Must match Dex's callback URL.")
	a.Describe(&c.Groups, "List of GitLab group names. Only users in these groups will be allowed to authenticate.")
	a.Describe(&c.UseLoginAsID, "If true, use GitLab username as the user ID. Defaults to false.")
	a.Describe(&c.GetGroupsPermission, "If true, request 'read_api' scope to fetch group memberships. Defaults to false.")
}

// Annotate provides schema metadata for GitLabConnectorState.
func (c *GitLabConnectorState) Annotate(a infer.Annotator) {
	// GitLabConnectorState embeds GitLabConnectorArgs, so field descriptions are inherited
}

// WireDependencies marks credential-bearing outputs secret regardless of how the
// input was supplied, so plain literals, refresh and import never store them in plaintext.
func (c *GitLabConnector) WireDependencies(f infer.FieldSelector, _ *GitLabConnectorArgs, state *GitLabConnectorState) {
	f.OutputField(&state.ClientSecret).AlwaysSecret()
}

// Check validates inputs.
func (c *GitLabConnector) Check(ctx context.Context, req infer.CheckRequest) (infer.CheckResponse[GitLabConnectorArgs], error) {
	args, failures, err := infer.DefaultCheck[GitLabConnectorArgs](ctx, req.NewInputs)
	if err != nil {
		return infer.CheckResponse[GitLabConnectorArgs]{Failures: failures}, err
	}

	// Apply defaults
	if args.BaseURL == nil || *args.BaseURL == "" {
		defaultURL := "https://gitlab.com"
		args.BaseURL = &defaultURL
	}
	if args.UseLoginAsID == nil {
		defaultUseLogin := false
		args.UseLoginAsID = &defaultUseLogin
	}
	if args.GetGroupsPermission == nil {
		defaultGetGroups := false
		args.GetGroupsPermission = &defaultGetGroups
	}

	return infer.CheckResponse[GitLabConnectorArgs]{
		Inputs:   args,
		Failures: failures,
	}, nil
}

func (c *GitLabConnector) controller() connectorController[GitLabConnectorArgs, GitLabConnectorState] {
	return connectorController[GitLabConnectorArgs, GitLabConnectorState]{
		kind:    "gitlab-connector",
		token:   tokenGitLabConnector,
		dexType: "gitlab",
		id:      func(a GitLabConnectorArgs) string { return a.ConnectorId },
		name:    func(a GitLabConnectorArgs) string { return a.Name },
		state:   func(a GitLabConnectorArgs) GitLabConnectorState { return GitLabConnectorState{GitLabConnectorArgs: a} },
		args:    func(s GitLabConnectorState) GitLabConnectorArgs { return s.GitLabConnectorArgs },
		encode:  encodeGitLabConfig,
		decode:  decodeGitLabConfig,
	}
}

func encodeGitLabConfig(args GitLabConnectorArgs) ([]byte, error) {
	gitlabConfig := map[string]any{
		"clientID":     args.ClientId,
		"clientSecret": args.ClientSecret,
		"redirectURI":  args.RedirectUri,
	}

	if args.BaseURL != nil && *args.BaseURL != "" {
		gitlabConfig["baseURL"] = *args.BaseURL
	}
	if len(args.Groups) > 0 {
		gitlabConfig["groups"] = args.Groups
	}
	if args.UseLoginAsID != nil {
		gitlabConfig["useLoginAsID"] = *args.UseLoginAsID
	}
	if args.GetGroupsPermission != nil {
		gitlabConfig["getGroupsPermission"] = *args.GetGroupsPermission
	}

	configBytes, err := json.Marshal(gitlabConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal GitLab config: %w", err)
	}
	return configBytes, nil
}

func decodeGitLabConfig(found *api.Connector, _ GitLabConnectorArgs) (GitLabConnectorArgs, error) {
	conf, err := parseConnectorConfig(found.Config)
	if err != nil {
		return GitLabConnectorArgs{}, err
	}

	args := GitLabConnectorArgs{
		ConnectorId:         found.Id,
		Name:                found.Name,
		BaseURL:             conf.strPtr("baseURL"),
		ClientId:            conf.str("clientID"),
		ClientSecret:        conf.str("clientSecret"),
		RedirectUri:         conf.str("redirectURI"),
		Groups:              conf.strList("groups"),
		UseLoginAsID:        conf.boolPtr("useLoginAsID"),
		GetGroupsPermission: conf.boolPtr("getGroupsPermission"),
	}
	return args, conf.Err()
}

// Create creates a new GitLab connector.
func (c *GitLabConnector) Create(ctx context.Context, req infer.CreateRequest[GitLabConnectorArgs]) (infer.CreateResponse[GitLabConnectorState], error) {
	return c.controller().create(ctx, req)
}

// Read retrieves an existing GitLab connector.
func (c *GitLabConnector) Read(ctx context.Context, req infer.ReadRequest[GitLabConnectorArgs, GitLabConnectorState]) (infer.ReadResponse[GitLabConnectorArgs, GitLabConnectorState], error) {
	return c.controller().read(ctx, req)
}

// Update updates an existing GitLab connector.
func (c *GitLabConnector) Update(ctx context.Context, req infer.UpdateRequest[GitLabConnectorArgs, GitLabConnectorState]) (infer.UpdateResponse[GitLabConnectorState], error) {
	return c.controller().update(ctx, req)
}

// Delete deletes a GitLab connector.
func (c *GitLabConnector) Delete(ctx context.Context, req infer.DeleteRequest[GitLabConnectorState]) (infer.DeleteResponse, error) {
	return c.controller().delete(ctx, req)
}
