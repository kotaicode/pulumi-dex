package resources

import (
	"context"
	"encoding/json"
	"fmt"

	api "github.com/dexidp/dex/api/v2"
	"github.com/pulumi/pulumi-go-provider/infer"
)

// ============================================================================
// GoogleConnector - Google OpenID Connect connector (type: "google")
// ============================================================================

// GoogleConnectorArgs defines inputs for GoogleConnector.
type GoogleConnectorArgs struct {
	ConnectorId            string            `pulumi:"connectorId" provider:"replaceOnChanges"`
	Name                   string            `pulumi:"name"`
	ClientId               string            `pulumi:"clientId"`
	ClientSecret           string            `pulumi:"clientSecret" provider:"secret"` //nolint:gosec // G117: Intentionally exported secret field for Pulumi provider
	RedirectUri            string            `pulumi:"redirectUri"`
	PromptType             *string           `pulumi:"promptType,optional"`
	HostedDomains          []string          `pulumi:"hostedDomains,optional"`
	Groups                 []string          `pulumi:"groups,optional"`
	ServiceAccountFilePath *string           `pulumi:"serviceAccountFilePath,optional"`
	DomainToAdminEmail     map[string]string `pulumi:"domainToAdminEmail,optional"`
}

// GoogleConnectorState defines outputs for GoogleConnector.
type GoogleConnectorState struct {
	GoogleConnectorArgs
}

// GoogleConnector manages a Google connector in Dex.
type GoogleConnector struct{}

// Annotate provides schema metadata.
func (c *GoogleConnector) Annotate(a infer.Annotator) {
	a.Describe(c, "Manages a Google connector in Dex. This connector allows users to authenticate using their Google accounts and supports domain and group-based access control.")
}

// Annotate provides schema metadata for GoogleConnectorArgs.
func (c *GoogleConnectorArgs) Annotate(a infer.Annotator) {
	a.Describe(&c.ConnectorId, "Unique identifier for the Google connector.")
	a.Describe(&c.Name, "Human-readable name for the connector, displayed to users during login.")
	a.Describe(&c.ClientId, "Google OAuth client ID.")
	a.Describe(&c.ClientSecret, "Google OAuth client secret.")
	a.Describe(&c.RedirectUri, "Redirect URI registered in Google OAuth app. Must match Dex's callback URL.")
	a.Describe(&c.PromptType, "OAuth prompt type. Valid values: 'consent' (default) or 'select_account'.")
	a.Describe(&c.HostedDomains, "List of Google Workspace domains. Only users with email addresses in these domains will be allowed to authenticate.")
	a.Describe(&c.Groups, "List of Google Groups. Only users in these groups will be allowed to authenticate.")
	a.Describe(&c.ServiceAccountFilePath, "Path to Google service account JSON file. Required for group-based access control.")
	a.Describe(&c.DomainToAdminEmail, "Map of domain names to admin email addresses. Used for group lookups in Google Workspace.")
}

// Annotate provides schema metadata for GoogleConnectorState.
func (c *GoogleConnectorState) Annotate(a infer.Annotator) {
	// GoogleConnectorState embeds GoogleConnectorArgs, so field descriptions are inherited
}

// WireDependencies marks credential-bearing outputs secret regardless of how the
// input was supplied, so plain literals, refresh and import never store them in plaintext.
func (c *GoogleConnector) WireDependencies(f infer.FieldSelector, _ *GoogleConnectorArgs, state *GoogleConnectorState) {
	f.OutputField(&state.ClientSecret).AlwaysSecret()
}

// Check validates inputs.
func (c *GoogleConnector) Check(ctx context.Context, req infer.CheckRequest) (infer.CheckResponse[GoogleConnectorArgs], error) {
	args, failures, err := infer.DefaultCheck[GoogleConnectorArgs](ctx, req.NewInputs)
	if err != nil {
		return infer.CheckResponse[GoogleConnectorArgs]{Failures: failures}, err
	}

	// Apply defaults
	if args.PromptType == nil || *args.PromptType == "" {
		defaultPrompt := "consent"
		args.PromptType = &defaultPrompt
	}

	return infer.CheckResponse[GoogleConnectorArgs]{
		Inputs:   args,
		Failures: failures,
	}, nil
}

func (c *GoogleConnector) controller() connectorController[GoogleConnectorArgs, GoogleConnectorState] {
	return connectorController[GoogleConnectorArgs, GoogleConnectorState]{
		kind:    "google-connector",
		token:   tokenGoogleConnector,
		dexType: "google",
		id:      func(a GoogleConnectorArgs) string { return a.ConnectorId },
		name:    func(a GoogleConnectorArgs) string { return a.Name },
		state:   func(a GoogleConnectorArgs) GoogleConnectorState { return GoogleConnectorState{GoogleConnectorArgs: a} },
		args:    func(s GoogleConnectorState) GoogleConnectorArgs { return s.GoogleConnectorArgs },
		encode:  encodeGoogleConfig,
		decode:  decodeGoogleConfig,
	}
}

func encodeGoogleConfig(args GoogleConnectorArgs) ([]byte, error) {
	googleConfig := map[string]any{
		"clientID":     args.ClientId,
		"clientSecret": args.ClientSecret,
		"redirectURI":  args.RedirectUri,
	}

	if args.PromptType != nil {
		googleConfig["promptType"] = *args.PromptType
	}
	if len(args.HostedDomains) > 0 {
		googleConfig["hostedDomains"] = args.HostedDomains
	}
	if len(args.Groups) > 0 {
		googleConfig["groups"] = args.Groups
	}
	if args.ServiceAccountFilePath != nil {
		googleConfig["serviceAccountFilePath"] = *args.ServiceAccountFilePath
	}
	if len(args.DomainToAdminEmail) > 0 {
		googleConfig["domainToAdminEmail"] = args.DomainToAdminEmail
	}

	configBytes, err := json.Marshal(googleConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Google config: %w", err)
	}
	return configBytes, nil
}

func decodeGoogleConfig(found *api.Connector, _ GoogleConnectorArgs) (GoogleConnectorArgs, error) {
	conf, err := parseConnectorConfig(found.Config)
	if err != nil {
		return GoogleConnectorArgs{}, err
	}

	domainToAdminEmail := conf.strMap("domainToAdminEmail")
	if domainToAdminEmail == nil {
		domainToAdminEmail = map[string]string{} // as before: absent reads back as an empty map
	}

	args := GoogleConnectorArgs{
		ConnectorId:            found.Id,
		Name:                   found.Name,
		ClientId:               conf.str("clientID"),
		ClientSecret:           conf.str("clientSecret"),
		RedirectUri:            conf.str("redirectURI"),
		PromptType:             conf.strPtr("promptType"),
		HostedDomains:          conf.strList("hostedDomains"),
		Groups:                 conf.strList("groups"),
		ServiceAccountFilePath: conf.strPtr("serviceAccountFilePath"),
		DomainToAdminEmail:     domainToAdminEmail,
	}
	return args, conf.Err()
}

// Create creates a new Google connector.
func (c *GoogleConnector) Create(ctx context.Context, req infer.CreateRequest[GoogleConnectorArgs]) (infer.CreateResponse[GoogleConnectorState], error) {
	return c.controller().create(ctx, req)
}

// Read retrieves an existing Google connector.
func (c *GoogleConnector) Read(ctx context.Context, req infer.ReadRequest[GoogleConnectorArgs, GoogleConnectorState]) (infer.ReadResponse[GoogleConnectorArgs, GoogleConnectorState], error) {
	return c.controller().read(ctx, req)
}

// Update updates an existing Google connector.
func (c *GoogleConnector) Update(ctx context.Context, req infer.UpdateRequest[GoogleConnectorArgs, GoogleConnectorState]) (infer.UpdateResponse[GoogleConnectorState], error) {
	return c.controller().update(ctx, req)
}

// Delete deletes a Google connector.
func (c *GoogleConnector) Delete(ctx context.Context, req infer.DeleteRequest[GoogleConnectorState]) (infer.DeleteResponse, error) {
	return c.controller().delete(ctx, req)
}
