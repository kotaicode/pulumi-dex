package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	api "github.com/dexidp/dex/api/v2"
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
)

// ============================================================================
// CognitoOidcConnector - Uses generic OIDC connector (type: "oidc")
// ============================================================================

// CognitoOidcConnectorArgs defines inputs for CognitoOidcConnector.
type CognitoOidcConnectorArgs struct {
	ConnectorId    string         `pulumi:"connectorId" provider:"replaceOnChanges"`
	Name           string         `pulumi:"name"`
	Region         string         `pulumi:"region"`
	UserPoolId     string         `pulumi:"userPoolId"`
	ClientId       string         `pulumi:"clientId"`
	ClientSecret   string         `pulumi:"clientSecret" provider:"secret"` //nolint:gosec // G117: Intentionally exported secret field for Pulumi provider
	RedirectUri    string         `pulumi:"redirectUri"`
	Scopes         []string       `pulumi:"scopes,optional"`
	UserNameSource *string        `pulumi:"userNameSource,optional"` // "email" | "sub"
	ExtraOidc      map[string]any `pulumi:"extraOidc,optional" provider:"secret"`
}

// CognitoOidcConnectorState defines outputs for CognitoOidcConnector.
type CognitoOidcConnectorState struct {
	CognitoOidcConnectorArgs
}

// CognitoOidcConnector manages an AWS Cognito connector using Dex's generic OIDC connector.
type CognitoOidcConnector struct{}

// Annotate provides schema metadata.
func (c *CognitoOidcConnector) Annotate(a infer.Annotator) {
	a.Describe(c, "Manages an AWS Cognito user pool connector in Dex using the generic OIDC connector (type: oidc). This connector allows users to authenticate using their AWS Cognito credentials.")
}

// Annotate provides schema metadata for CognitoOidcConnectorArgs.
func (c *CognitoOidcConnectorArgs) Annotate(a infer.Annotator) {
	a.Describe(&c.ConnectorId, "Unique identifier for the Cognito connector.")
	a.Describe(&c.Name, "Human-readable name for the connector, displayed to users during login.")
	a.Describe(&c.Region, "AWS region where the Cognito user pool is located (e.g., 'us-east-1', 'eu-west-1').")
	a.Describe(&c.UserPoolId, "AWS Cognito user pool ID.")
	a.Describe(&c.ClientId, "Cognito app client ID.")
	a.Describe(&c.ClientSecret, "Cognito app client secret.")
	a.Describe(&c.RedirectUri, "Redirect URI registered in Cognito. Must match Dex's callback URL.")
	a.Describe(&c.Scopes, "OIDC scopes to request from Cognito. Defaults to ['openid', 'email', 'profile'] if not specified.")
	a.Describe(&c.UserNameSource, "Source for the username claim. Valid values: 'email' or 'sub' (subject).")
	a.Describe(&c.ExtraOidc, "Additional OIDC configuration fields as key-value pairs for advanced scenarios.")
}

// Annotate provides schema metadata for CognitoOidcConnectorState.
func (c *CognitoOidcConnectorState) Annotate(a infer.Annotator) {
	// CognitoOidcConnectorState embeds CognitoOidcConnectorArgs, so field descriptions are inherited
}

// WireDependencies marks credential-bearing outputs secret regardless of how the
// input was supplied, so plain literals, refresh and import never store them in plaintext.
func (c *CognitoOidcConnector) WireDependencies(f infer.FieldSelector, _ *CognitoOidcConnectorArgs, state *CognitoOidcConnectorState) {
	f.OutputField(&state.ClientSecret).AlwaysSecret()
	f.OutputField(&state.ExtraOidc).AlwaysSecret()
}

// Check validates inputs.
func (c *CognitoOidcConnector) Check(ctx context.Context, req infer.CheckRequest) (infer.CheckResponse[CognitoOidcConnectorArgs], error) {
	args, failures, err := infer.DefaultCheck[CognitoOidcConnectorArgs](ctx, req.NewInputs)
	if err != nil {
		return infer.CheckResponse[CognitoOidcConnectorArgs]{Failures: failures}, err
	}
	failures = append(failures, checkExtensionKeys(req.NewInputs.Get("extraOidc"), "extraOidc", cognitoOIDCTypedKeys)...)

	// Validate region format (basic check)
	if args.Region != "" {
		regionRegex := regexp.MustCompile(`^[a-z0-9-]+$`)
		if !regionRegex.MatchString(args.Region) {
			failures = append(failures, p.CheckFailure{
				Property: "region",
				Reason:   "must be a valid AWS region identifier",
			})
		}
	}

	// Validate userNameSource
	if args.UserNameSource != nil {
		valid := map[string]bool{"email": true, "sub": true}
		if !valid[*args.UserNameSource] {
			failures = append(failures, p.CheckFailure{
				Property: "userNameSource",
				Reason:   "must be one of: email, sub",
			})
		}
	}

	// Apply defaults
	if len(args.Scopes) == 0 {
		args.Scopes = []string{"openid", "email", "profile"}
	}

	return infer.CheckResponse[CognitoOidcConnectorArgs]{
		Inputs:   args,
		Failures: failures,
	}, nil
}

func (c *CognitoOidcConnector) controller() connectorController[CognitoOidcConnectorArgs, CognitoOidcConnectorState] {
	return connectorController[CognitoOidcConnectorArgs, CognitoOidcConnectorState]{
		kind:    "cognito-oidc-connector",
		token:   tokenCognitoOidcConnector,
		dexType: "oidc",
		id:      func(a CognitoOidcConnectorArgs) string { return a.ConnectorId },
		name:    func(a CognitoOidcConnectorArgs) string { return a.Name },
		state: func(a CognitoOidcConnectorArgs) CognitoOidcConnectorState {
			return CognitoOidcConnectorState{CognitoOidcConnectorArgs: a}
		},
		args:   func(s CognitoOidcConnectorState) CognitoOidcConnectorArgs { return s.CognitoOidcConnectorArgs },
		encode: encodeCognitoConfig,
		decode: decodeCognitoConfig,
	}
}

func encodeCognitoConfig(args CognitoOidcConnectorArgs) ([]byte, error) {
	// Derive issuer from region and userPoolId
	issuer := fmt.Sprintf("https://cognito-idp.%s.amazonaws.com/%s", args.Region, args.UserPoolId)

	// Derive userNameKey from userNameSource
	userNameKey := "email" // default
	if args.UserNameSource != nil {
		userNameKey = *args.UserNameSource
	}

	oidcConfig := map[string]any{
		"issuer":       issuer,
		"clientID":     args.ClientId,
		"clientSecret": args.ClientSecret,
		"redirectURI":  args.RedirectUri,
		"scopes":       args.Scopes,
		"userNameKey":  userNameKey,
	}

	for k, v := range args.ExtraOidc {
		oidcConfig[k] = v
	}

	configBytes, err := json.Marshal(oidcConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal OIDC config: %w", err)
	}
	return configBytes, nil
}

func decodeCognitoConfig(found *api.Connector, prior CognitoOidcConnectorArgs) (CognitoOidcConnectorArgs, error) {
	conf, err := parseConnectorConfig(found.Config)
	if err != nil {
		return CognitoOidcConnectorArgs{}, err
	}

	// Extract region and userPoolId from issuer
	issuer := conf.str("issuer")
	region := ""
	userPoolId := ""
	if strings.HasPrefix(issuer, "https://cognito-idp.") && strings.Contains(issuer, ".amazonaws.com/") {
		parts := strings.Split(issuer, "/")
		if len(parts) >= 4 {
			domainParts := strings.Split(parts[2], ".")
			if len(domainParts) >= 2 {
				region = domainParts[1]
			}
			userPoolId = parts[3]
		}
	}

	scopes := conf.strList("scopes")
	if scopes == nil {
		scopes = []string{} // as before: absent scopes read back as an empty list
	}

	args := CognitoOidcConnectorArgs{
		ConnectorId:  found.Id,
		Name:         found.Name,
		Region:       region,
		UserPoolId:   userPoolId,
		ClientId:     conf.str("clientID"),
		ClientSecret: conf.str("clientSecret"),
		RedirectUri:  conf.str("redirectURI"),
		Scopes:       scopes,
		// Create writes "email" when userNameSource is omitted.
		UserNameSource: userNameSourceFromConfig(conf.str("userNameKey"), "email", prior.UserNameSource),
		ExtraOidc:      extraOIDCFromConfig(conf.values, prior.ExtraOidc),
	}
	return args, conf.Err()
}

// Create creates a new Cognito OIDC connector.
func (c *CognitoOidcConnector) Create(ctx context.Context, req infer.CreateRequest[CognitoOidcConnectorArgs]) (infer.CreateResponse[CognitoOidcConnectorState], error) {
	return c.controller().create(ctx, req)
}

// Read retrieves an existing Cognito OIDC connector.
func (c *CognitoOidcConnector) Read(ctx context.Context, req infer.ReadRequest[CognitoOidcConnectorArgs, CognitoOidcConnectorState]) (infer.ReadResponse[CognitoOidcConnectorArgs, CognitoOidcConnectorState], error) {
	return c.controller().read(ctx, req)
}

// Update updates an existing Cognito OIDC connector.
func (c *CognitoOidcConnector) Update(ctx context.Context, req infer.UpdateRequest[CognitoOidcConnectorArgs, CognitoOidcConnectorState]) (infer.UpdateResponse[CognitoOidcConnectorState], error) {
	return c.controller().update(ctx, req)
}

// Delete deletes a Cognito OIDC connector.
func (c *CognitoOidcConnector) Delete(ctx context.Context, req infer.DeleteRequest[CognitoOidcConnectorState]) (infer.DeleteResponse, error) {
	return c.controller().delete(ctx, req)
}
