package resources

import (
	"context"
	"encoding/json"
	"fmt"

	api "github.com/dexidp/dex/api/v2"
	"github.com/pulumi/pulumi-go-provider/infer"

	"github.com/kotaicode/pulumi-dex/pkg/provider"
)

// ConnectorArgs defines the inputs for a dex.Connector resource.
type ConnectorArgs struct {
	ConnectorId string      `pulumi:"connectorId" provider:"replaceOnChanges"`
	Type        string      `pulumi:"type"`
	Name        string      `pulumi:"name"`
	OIDCConfig  *OIDCConfig `pulumi:"oidcConfig,optional"`
	RawConfig   *string     `pulumi:"rawConfig,optional" provider:"secret"`
}

// ConnectorState defines the outputs/state for a dex.Connector resource.
type ConnectorState struct {
	ConnectorArgs
}

// OIDCConfig mirrors Dex's OIDC connector JSON configuration.
// This is intentionally close to the config used in simple-client/types.go.
// Note: JSON tags match pulumi tags (camelCase) for proper decoding. We convert to Dex format in buildConnectorConfigBytes.
type OIDCConfig struct {
	Issuer                    string            `pulumi:"issuer" json:"issuer"`
	ClientId                  string            `pulumi:"clientId" json:"clientId"`                           // Match pulumi tag for decoder
	ClientSecret              string            `pulumi:"clientSecret" json:"clientSecret" provider:"secret"` //nolint:gosec // G117: Intentionally exported secret field for Pulumi provider
	RedirectUri               string            `pulumi:"redirectUri" json:"redirectUri"`                     // Match pulumi tag for decoder
	Scopes                    []string          `pulumi:"scopes,optional" json:"scopes,omitempty"`
	InsecureSkipEmailVerified *bool             `pulumi:"insecureSkipEmailVerified,optional" json:"insecureSkipEmailVerified,omitempty"`
	InsecureIssuer            *bool             `pulumi:"insecureIssuer,optional" json:"insecureIssuer,omitempty"`
	UserNameKey               *string           `pulumi:"userNameKey,optional" json:"userNameKey,omitempty"`
	ClaimMapping              *OIDCClaimMapping `pulumi:"claimMapping,optional" json:"claimMapping,omitempty"`
	Extra                     map[string]any    `pulumi:"extra,optional" json:"-" provider:"secret"`
}

// OIDCClaimMapping represents claim mapping configuration.
type OIDCClaimMapping struct {
	EmailKey  *string `pulumi:"emailKey,optional" json:"email,omitempty"`
	GroupsKey *string `pulumi:"groupsKey,optional" json:"groups,omitempty"`
}

// Connector represents a Dex connector resource (generic).
type Connector struct{}

// Annotate provides schema metadata for the Connector resource.
func (c *Connector) Annotate(a infer.Annotator) {
	a.Describe(c, "Manages a generic connector (upstream identity provider) in Dex. Use this resource for connectors not covered by specific connector types, or when you need full control over the connector configuration.")
}

// Annotate provides schema metadata for ConnectorArgs.
func (c *ConnectorArgs) Annotate(a infer.Annotator) {
	a.Describe(&c.ConnectorId, "Unique identifier for the connector.")
	a.Describe(&c.Type, "Type of connector (e.g., 'oidc', 'saml', 'ldap'). Must match a connector type supported by Dex.")
	a.Describe(&c.Name, "Human-readable name for the connector, displayed to users during login.")
	a.Describe(&c.OIDCConfig, "OIDC-specific configuration. Use this for OIDC-based connectors.")
	a.Describe(&c.RawConfig, "Raw JSON configuration for the connector. Use this for advanced configurations or connector types not directly supported. If provided, this takes precedence over OIDCConfig.")
}

// Annotate provides schema metadata for OIDCConfig.
func (c *OIDCConfig) Annotate(a infer.Annotator) {
	a.Describe(&c.Issuer, "The OIDC issuer URL (e.g., 'https://accounts.google.com').")
	a.Describe(&c.ClientId, "The OIDC client ID.")
	a.Describe(&c.ClientSecret, "The OIDC client secret.")
	a.Describe(&c.RedirectUri, "The redirect URI registered with the OIDC provider. Must match Dex's callback URL.")
	a.Describe(&c.Scopes, "List of OIDC scopes to request (e.g., 'openid', 'profile', 'email'). Defaults to ['openid', 'profile', 'email'] if not specified.")
	a.Describe(&c.InsecureSkipEmailVerified, "If true, skip verification of the 'email_verified' claim. Not recommended for production.")
	a.Describe(&c.InsecureIssuer, "If true, skip verification of the issuer URL. Not recommended for production.")
	a.Describe(&c.UserNameKey, "The claim key to use as the username (e.g., 'preferred_username', 'email', 'sub').")
	a.Describe(&c.ClaimMapping, "Mapping of OIDC claims to Dex user attributes.")
	a.Describe(&c.Extra, "Additional OIDC configuration fields as key-value pairs.")
}

// Annotate provides schema metadata for OIDCClaimMapping.
func (c *OIDCClaimMapping) Annotate(a infer.Annotator) {
	a.Describe(&c.EmailKey, "The OIDC claim key that contains the user's email address.")
	a.Describe(&c.GroupsKey, "The OIDC claim key that contains the user's group memberships.")
}

// Annotate provides schema metadata for ConnectorState.
func (c *ConnectorState) Annotate(a infer.Annotator) {
	// ConnectorState embeds ConnectorArgs, so field descriptions are inherited
}

// WireDependencies marks credential-bearing outputs secret regardless of how the
// input was supplied, so plain literals, refresh and import never store them in plaintext.
func (c *Connector) WireDependencies(f infer.FieldSelector, _ *ConnectorArgs, state *ConnectorState) {
	f.OutputField(&state.RawConfig).AlwaysSecret()
	f.OutputField(&state.OIDCConfig).AlwaysSecret()
}

// Check enforces the input rules at preview: exactly one of oidcConfig and
// rawConfig, and no oidcConfig.extra key that a typed oidcConfig field sets.
func (c *Connector) Check(ctx context.Context, req infer.CheckRequest) (infer.CheckResponse[ConnectorArgs], error) {
	args, failures, err := infer.DefaultCheck[ConnectorArgs](ctx, req.NewInputs)
	if err != nil {
		return infer.CheckResponse[ConnectorArgs]{Failures: failures}, err
	}
	failures = append(failures, checkConnectorInputMode(req.NewInputs)...)
	if oidc := req.NewInputs.Get("oidcConfig"); oidc.IsMap() {
		failures = append(failures, checkExtensionKeys(oidc.AsMap().Get("extra"), "oidcConfig.extra", genericOIDCTypedKeys)...)
	}
	return infer.CheckResponse[ConnectorArgs]{Inputs: args, Failures: failures}, nil
}

// controller is the shared lifecycle with the generic Connector's
// rules: the type is an input, any type is read back, and an "oidc" config
// decodes into oidcConfig unless the prior inputs used rawConfig.
func (c *Connector) controller() connectorController[ConnectorArgs, ConnectorState] {
	return connectorController[ConnectorArgs, ConnectorState]{
		kind:   "connector",
		token:  tokenConnector,
		id:     func(a ConnectorArgs) string { return a.ConnectorId },
		name:   func(a ConnectorArgs) string { return a.Name },
		state:  func(a ConnectorArgs) ConnectorState { return ConnectorState{ConnectorArgs: a} },
		args:   func(s ConnectorState) ConnectorArgs { return s.ConnectorArgs },
		encode: buildConnectorConfigBytes,
		decode: func(found *api.Connector, prior ConnectorArgs) (ConnectorArgs, error) {
			args := decodeConnector(found)
			// decodeConnector maps every "oidc" config to oidcConfig. If the prior inputs
			// used rawConfig, keep that representation so refresh does not switch modes.
			// Import has no prior inputs and still gets the typed decoding.
			if prior.RawConfig != nil && *prior.RawConfig != "" {
				raw := rawConfigFromRemote(found.Config, prior.RawConfig)
				args.OIDCConfig, args.RawConfig = nil, &raw
			}
			return args, nil
		},
		typeOf:      func(a ConnectorArgs) string { return a.Type },
		validate:    validateConnectorArgs,
		listFailure: "failed to list Dex connectors",
		idChanged: func(old, id string) error {
			return fmt.Errorf("connectorId cannot be changed (was %q, got %q)", old, id)
		},
		requireDeleteID: true,
	}
}

// Create creates a new connector in Dex.
func (c *Connector) Create(ctx context.Context, req infer.CreateRequest[ConnectorArgs]) (infer.CreateResponse[ConnectorState], error) {
	return c.controller().create(ctx, req)
}

// Read reads an existing connector from Dex.
func (c *Connector) Read(ctx context.Context, req infer.ReadRequest[ConnectorArgs, ConnectorState]) (infer.ReadResponse[ConnectorArgs, ConnectorState], error) {
	return c.controller().read(ctx, req)
}

// Update updates an existing connector in Dex.
func (c *Connector) Update(ctx context.Context, req infer.UpdateRequest[ConnectorArgs, ConnectorState]) (infer.UpdateResponse[ConnectorState], error) {
	return c.controller().update(ctx, req)
}

// Delete deletes a connector from Dex.
func (c *Connector) Delete(ctx context.Context, req infer.DeleteRequest[ConnectorState]) (infer.DeleteResponse, error) {
	return c.controller().delete(ctx, req)
}

// validateConnectorArgs enforces high-level invariants for connectors.
func validateConnectorArgs(args ConnectorArgs) error {
	if args.ConnectorId == "" {
		return fmt.Errorf("connectorId is required")
	}
	if args.Type == "" {
		return fmt.Errorf("type is required")
	}
	if args.Name == "" {
		return fmt.Errorf("name is required")
	}

	oidcSet := args.OIDCConfig != nil
	rawSet := args.RawConfig != nil && *args.RawConfig != ""
	if oidcSet == rawSet {
		return fmt.Errorf("exactly one of oidcConfig or rawConfig must be set")
	}
	if args.Type != "oidc" && oidcSet {
		return fmt.Errorf("oidcConfig is only valid when type == \"oidc\"")
	}
	return nil
}

// buildConnectorConfigBytes produces the JSON config bytes to send to Dex.
func buildConnectorConfigBytes(args ConnectorArgs) ([]byte, error) {
	if args.OIDCConfig != nil {
		// Convert from Pulumi format (camelCase) to Dex format (PascalCase for clientID/redirectURI).
		base := map[string]any{}
		raw, err := json.Marshal(args.OIDCConfig) //nolint:gosec // G117: the secret is meant to go into the Dex connector config
		if err != nil {
			return nil, fmt.Errorf("failed to marshal OIDC config: %w", err)
		}
		if err := json.Unmarshal(raw, &base); err != nil {
			return nil, fmt.Errorf("failed to rehydrate OIDC config: %w", err)
		}
		// Convert camelCase keys to Dex format
		if clientId, ok := base["clientId"]; ok {
			base["clientID"] = clientId
			delete(base, "clientId")
		}
		if redirectUri, ok := base["redirectUri"]; ok {
			base["redirectURI"] = redirectUri
			delete(base, "redirectUri")
		}
		// Merge Extra fields
		for k, v := range args.OIDCConfig.Extra {
			base[k] = v
		}
		out, err := json.Marshal(base)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal merged OIDC config: %w", err)
		}
		return out, nil
	}

	// Raw JSON path.
	var raw json.RawMessage
	if err := json.Unmarshal([]byte(provider.PtrOr(args.RawConfig, "")), &raw); err != nil {
		return nil, fmt.Errorf("rawConfig must be valid JSON: %w", err)
	}
	return []byte(provider.PtrOr(args.RawConfig, "")), nil
}

// decodeConnector converts a Dex Connector into ConnectorArgs.
func decodeConnector(con *api.Connector) ConnectorArgs {
	args := ConnectorArgs{
		ConnectorId: con.Id,
		Type:        con.Type,
		Name:        con.Name,
	}

	// Try to parse as OIDC config when type == "oidc".
	if con.Type == "oidc" && len(con.Config) > 0 {
		// Map known fields into oidcConfig only when that is lossless: the config must
		// be a JSON object and every known field must have the expected type.
		// Otherwise fall back to rawConfig below, which keeps the stored config verbatim.
		var base map[string]any
		oidc := &OIDCConfig{}
		if json.Unmarshal(con.Config, &base) == nil && base != nil && json.Unmarshal(con.Config, oidc) == nil {

			// Convert from Dex format (clientID, redirectURI) to Pulumi format (clientId, redirectUri)
			if clientID, ok := base["clientID"]; ok {
				base["clientId"] = clientID
				delete(base, "clientID")
			}
			if redirectURI, ok := base["redirectURI"]; ok {
				base["redirectUri"] = redirectURI
				delete(base, "redirectURI")
			}
			// Remove known fields from base, whatever remains goes into Extra.
			delete(base, "issuer")
			delete(base, "clientId")
			delete(base, "clientSecret")
			delete(base, "redirectUri")
			delete(base, "scopes")
			delete(base, "insecureSkipEmailVerified")
			delete(base, "insecureIssuer")
			delete(base, "userNameKey")
			delete(base, "claimMapping")

			if len(base) > 0 {
				oidc.Extra = base
			}

			args.OIDCConfig = oidc
		} else {
			// Fall back to rawConfig if the config cannot be mapped losslessly.
			rc := string(con.Config)
			args.RawConfig = &rc
		}
	} else if len(con.Config) > 0 {
		rc := string(con.Config)
		args.RawConfig = &rc
	}
	return args
}
