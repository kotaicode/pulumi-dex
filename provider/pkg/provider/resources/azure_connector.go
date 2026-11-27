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
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

// ============================================================================
// AzureOidcConnector - Uses generic OIDC connector (type: "oidc")
// ============================================================================

// AzureOidcConnectorArgs defines inputs for AzureOidcConnector using generic OIDC.
type AzureOidcConnectorArgs struct {
	ConnectorId    string         `pulumi:"connectorId" provider:"replaceOnChanges"`
	Name           string         `pulumi:"name"`
	TenantId       string         `pulumi:"tenantId"`
	ClientId       string         `pulumi:"clientId"`
	ClientSecret   string         `pulumi:"clientSecret" provider:"secret"` //nolint:gosec // G117: Intentionally exported secret field for Pulumi provider
	RedirectUri    string         `pulumi:"redirectUri"`
	Scopes         []string       `pulumi:"scopes,optional"`
	UserNameSource *string        `pulumi:"userNameSource,optional"`              // "preferred_username" | "upn" | "email"
	ExtraOidc      map[string]any `pulumi:"extraOidc,optional" provider:"secret"` // Additional OIDC config fields
}

// AzureOidcConnectorState defines outputs for AzureOidcConnector.
type AzureOidcConnectorState struct {
	AzureOidcConnectorArgs
}

// AzureOidcConnector manages an Azure/Entra ID connector using Dex's generic OIDC connector.
type AzureOidcConnector struct{}

// Annotate provides schema metadata.
func (c *AzureOidcConnector) Annotate(a infer.Annotator) {
	a.Describe(c, "Manages an Azure AD/Entra ID connector in Dex using the generic OIDC connector (type: oidc). This connector allows users to authenticate using their Azure AD/Entra ID credentials.")
}

// Annotate provides schema metadata for AzureOidcConnectorArgs.
func (c *AzureOidcConnectorArgs) Annotate(a infer.Annotator) {
	a.Describe(&c.ConnectorId, "Unique identifier for the Azure connector.")
	a.Describe(&c.Name, "Human-readable name for the connector, displayed to users during login.")
	a.Describe(&c.TenantId, "Azure AD tenant ID (UUID format). This identifies your Azure AD organization.")
	a.Describe(&c.ClientId, "Azure AD application (client) ID.")
	a.Describe(&c.ClientSecret, "Azure AD application client secret.")
	a.Describe(&c.RedirectUri, "Redirect URI registered in Azure AD. Must match Dex's callback URL (typically 'https://dex.example.com/callback').")
	a.Describe(&c.Scopes, "OIDC scopes to request from Azure AD. Defaults to ['openid', 'profile', 'email', 'offline_access'] if not specified.")
	a.Describe(&c.UserNameSource, "Source for the username claim. Valid values: 'preferred_username' (default), 'upn' (User Principal Name), or 'email'.")
	a.Describe(&c.ExtraOidc, "Additional OIDC configuration fields as key-value pairs for advanced scenarios.")
}

// Annotate provides schema metadata for AzureOidcConnectorState.
func (c *AzureOidcConnectorState) Annotate(a infer.Annotator) {
	// AzureOidcConnectorState embeds AzureOidcConnectorArgs, so field descriptions are inherited
}

// WireDependencies marks credential-bearing outputs secret regardless of how the
// input was supplied, so plain literals, refresh and import never store them in plaintext.
func (c *AzureOidcConnector) WireDependencies(f infer.FieldSelector, _ *AzureOidcConnectorArgs, state *AzureOidcConnectorState) {
	f.OutputField(&state.ClientSecret).AlwaysSecret()
	f.OutputField(&state.ExtraOidc).AlwaysSecret()
}

// Check validates inputs before creation/update.
func (c *AzureOidcConnector) Check(ctx context.Context, req infer.CheckRequest) (infer.CheckResponse[AzureOidcConnectorArgs], error) {
	args, failures, err := infer.DefaultCheck[AzureOidcConnectorArgs](ctx, req.NewInputs)
	if err != nil {
		return infer.CheckResponse[AzureOidcConnectorArgs]{Failures: failures}, err
	}
	failures = append(failures, checkExtensionKeys(req.NewInputs.Get("extraOidc"), "extraOidc", azureOIDCTypedKeys)...)

	// Validate tenantId format (UUID)
	if args.TenantId != "" {
		uuidRegex := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
		if !uuidRegex.MatchString(strings.ToLower(args.TenantId)) {
			failures = append(failures, p.CheckFailure{
				Property: "tenantId",
				Reason:   "must be a valid UUID",
			})
		}
	}

	// Validate userNameSource
	if args.UserNameSource != nil {
		valid := map[string]bool{"preferred_username": true, "upn": true, "email": true}
		if !valid[*args.UserNameSource] {
			failures = append(failures, p.CheckFailure{
				Property: "userNameSource",
				Reason:   "must be one of: preferred_username, upn, email",
			})
		}
	}

	// Apply defaults
	if len(args.Scopes) == 0 {
		args.Scopes = []string{"openid", "profile", "email", "offline_access"}
	}

	return infer.CheckResponse[AzureOidcConnectorArgs]{
		Inputs:   args,
		Failures: failures,
	}, nil
}

func (c *AzureOidcConnector) controller() connectorController[AzureOidcConnectorArgs, AzureOidcConnectorState] {
	return connectorController[AzureOidcConnectorArgs, AzureOidcConnectorState]{
		kind:    "azure-oidc-connector",
		token:   tokenAzureOidcConnector,
		dexType: "oidc",
		id:      func(a AzureOidcConnectorArgs) string { return a.ConnectorId },
		name:    func(a AzureOidcConnectorArgs) string { return a.Name },
		state: func(a AzureOidcConnectorArgs) AzureOidcConnectorState {
			return AzureOidcConnectorState{AzureOidcConnectorArgs: a}
		},
		args:   func(s AzureOidcConnectorState) AzureOidcConnectorArgs { return s.AzureOidcConnectorArgs },
		encode: encodeAzureOidcConfig,
		decode: decodeAzureOidcConfig,
	}
}

func encodeAzureOidcConfig(args AzureOidcConnectorArgs) ([]byte, error) {
	// Derive issuer from tenantId
	issuer := fmt.Sprintf("https://login.microsoftonline.com/%s/v2.0", args.TenantId)

	// Derive userNameKey from userNameSource
	userNameKey := "preferred_username" // default
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

	// Merge extraOidc fields
	for k, v := range args.ExtraOidc {
		oidcConfig[k] = v
	}

	configBytes, err := json.Marshal(oidcConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal OIDC config: %w", err)
	}
	return configBytes, nil
}

func decodeAzureOidcConfig(found *api.Connector, prior AzureOidcConnectorArgs) (AzureOidcConnectorArgs, error) {
	conf, err := parseConnectorConfig(found.Config)
	if err != nil {
		return AzureOidcConnectorArgs{}, err
	}

	// Extract tenantId from issuer
	issuer := conf.str("issuer")
	tenantId := ""
	if strings.HasPrefix(issuer, "https://login.microsoftonline.com/") {
		parts := strings.Split(issuer, "/")
		if len(parts) >= 4 {
			tenantId = parts[3]
		}
	}

	scopes := conf.strList("scopes")
	if scopes == nil {
		scopes = []string{} // as before: absent scopes read back as an empty list
	}

	args := AzureOidcConnectorArgs{
		ConnectorId:  found.Id,
		Name:         found.Name,
		TenantId:     tenantId,
		ClientId:     conf.str("clientID"),
		ClientSecret: conf.str("clientSecret"),
		RedirectUri:  conf.str("redirectURI"),
		Scopes:       scopes,
		// Create writes "preferred_username" when userNameSource is omitted.
		UserNameSource: userNameSourceFromConfig(conf.str("userNameKey"), "preferred_username", prior.UserNameSource),
		ExtraOidc:      extraOIDCFromConfig(conf.values, prior.ExtraOidc),
	}
	return args, conf.Err()
}

// Create creates a new Azure OIDC connector.
func (c *AzureOidcConnector) Create(ctx context.Context, req infer.CreateRequest[AzureOidcConnectorArgs]) (infer.CreateResponse[AzureOidcConnectorState], error) {
	return c.controller().create(ctx, req)
}

// Read retrieves an existing Azure OIDC connector.
func (c *AzureOidcConnector) Read(ctx context.Context, req infer.ReadRequest[AzureOidcConnectorArgs, AzureOidcConnectorState]) (infer.ReadResponse[AzureOidcConnectorArgs, AzureOidcConnectorState], error) {
	return c.controller().read(ctx, req)
}

// Update updates an existing Azure OIDC connector.
func (c *AzureOidcConnector) Update(ctx context.Context, req infer.UpdateRequest[AzureOidcConnectorArgs, AzureOidcConnectorState]) (infer.UpdateResponse[AzureOidcConnectorState], error) {
	return c.controller().update(ctx, req)
}

// Delete deletes a Azure OIDC connector.
func (c *AzureOidcConnector) Delete(ctx context.Context, req infer.DeleteRequest[AzureOidcConnectorState]) (infer.DeleteResponse, error) {
	return c.controller().delete(ctx, req)
}

// ============================================================================
// AzureMicrosoftConnector - Uses Dex's Microsoft-specific connector (type: "microsoft")
// ============================================================================

// AzureMicrosoftConnectorArgs defines inputs for AzureMicrosoftConnector using Microsoft connector.
type AzureMicrosoftConnectorArgs struct {
	ConnectorId  string `pulumi:"connectorId" provider:"replaceOnChanges"`
	Name         string `pulumi:"name"`
	Tenant       string `pulumi:"tenant"` // "common", "organizations", or tenant ID
	ClientId     string `pulumi:"clientId"`
	ClientSecret string `pulumi:"clientSecret" provider:"secret"` //nolint:gosec // G117: Intentionally exported secret field for Pulumi provider
	RedirectUri  string `pulumi:"redirectUri"`
	// Groups is the removed string form of Dex's "groups" list. It stays in the
	// schema so that programs using it get a migration error instead of an
	// unknown-property error.
	Groups        *string  `pulumi:"groups,optional"`
	AllowedGroups []string `pulumi:"allowedGroups,optional"`
}

// AzureMicrosoftConnectorState defines outputs for AzureMicrosoftConnector.
type AzureMicrosoftConnectorState struct {
	AzureMicrosoftConnectorArgs
}

// AzureMicrosoftConnector manages an Azure/Entra ID connector using Dex's Microsoft-specific connector.
type AzureMicrosoftConnector struct{}

// Annotate provides schema metadata.
func (c *AzureMicrosoftConnector) Annotate(a infer.Annotator) {
	a.Describe(c, "Manages an Azure AD/Entra ID connector in Dex using the Microsoft-specific connector (type: microsoft). This connector provides Microsoft-specific features like group filtering and domain restrictions.")
}

// Annotate provides schema metadata for AzureMicrosoftConnectorArgs.
func (c *AzureMicrosoftConnectorArgs) Annotate(a infer.Annotator) {
	a.Describe(&c.ConnectorId, "Unique identifier for the Azure Microsoft connector.")
	a.Describe(&c.Name, "Human-readable name for the connector, displayed to users during login.")
	a.Describe(&c.Tenant, "Microsoft Entra ID tenant: 'common' (work, school and personal Microsoft accounts), "+
		"'organizations' (work and school accounts), 'consumers' (personal Microsoft accounts), "+
		"or a specific tenant ID (UUID) or verified domain such as 'contoso.onmicrosoft.com'.")
	a.Describe(&c.ClientId, "Azure AD application (client) ID.")
	a.Describe(&c.ClientSecret, "Azure AD application client secret.")
	a.Describe(&c.RedirectUri, "Redirect URI registered in Azure AD. Must match Dex's callback URL.")
	a.Describe(&c.AllowedGroups, "Groups whose members may log in through this connector (Dex's groups setting). "+
		"Requires a specific tenant ID or domain: Dex ignores the list for 'common', 'organizations' and 'consumers'. "+
		"Omit it to allow every user of the tenant; an empty list is rejected because Dex would treat it the same way.")
	a.Describe(&c.Groups, "Removed. This string was written to Dex's groups setting, which is a list, so Dex "+
		"could not initialize the connector. Use allowedGroups.")
	a.Deprecate(&c.Groups, "groups is no longer supported; use allowedGroups to restrict login to members of groups.")
}

// Annotate provides schema metadata for AzureMicrosoftConnectorState.
func (c *AzureMicrosoftConnectorState) Annotate(a infer.Annotator) {
	// AzureMicrosoftConnectorState embeds AzureMicrosoftConnectorArgs, so field descriptions are inherited
}

// WireDependencies marks credential-bearing outputs secret regardless of how the
// input was supplied, so plain literals, refresh and import never store them in plaintext.
func (c *AzureMicrosoftConnector) WireDependencies(f infer.FieldSelector, _ *AzureMicrosoftConnectorArgs, state *AzureMicrosoftConnectorState) {
	f.OutputField(&state.ClientSecret).AlwaysSecret()
}

// Check validates inputs.
func (c *AzureMicrosoftConnector) Check(ctx context.Context, req infer.CheckRequest) (infer.CheckResponse[AzureMicrosoftConnectorArgs], error) {
	args, failures, err := infer.DefaultCheck[AzureMicrosoftConnectorArgs](ctx, req.NewInputs)
	if err != nil {
		return infer.CheckResponse[AzureMicrosoftConnectorArgs]{Failures: failures}, err
	}

	failures = append(failures, checkMicrosoftGroups(req.NewInputs, args)...)

	return infer.CheckResponse[AzureMicrosoftConnectorArgs]{
		Inputs:   args,
		Failures: failures,
	}, nil
}

func (c *AzureMicrosoftConnector) controller() connectorController[AzureMicrosoftConnectorArgs, AzureMicrosoftConnectorState] {
	return connectorController[AzureMicrosoftConnectorArgs, AzureMicrosoftConnectorState]{
		kind:    "azure-microsoft-connector",
		token:   tokenAzureMicrosoftConnector,
		dexType: "microsoft",
		id:      func(a AzureMicrosoftConnectorArgs) string { return a.ConnectorId },
		name:    func(a AzureMicrosoftConnectorArgs) string { return a.Name },
		state: func(a AzureMicrosoftConnectorArgs) AzureMicrosoftConnectorState {
			return AzureMicrosoftConnectorState{AzureMicrosoftConnectorArgs: a}
		},
		args:   func(s AzureMicrosoftConnectorState) AzureMicrosoftConnectorArgs { return s.AzureMicrosoftConnectorArgs },
		encode: encodeAzureMicrosoftConfig,
		decode: decodeAzureMicrosoftConfig,
	}
}

func encodeAzureMicrosoftConfig(args AzureMicrosoftConnectorArgs) ([]byte, error) {
	microsoftConfig := map[string]any{
		"clientID":     args.ClientId,
		"clientSecret": args.ClientSecret,
		"redirectURI":  args.RedirectUri,
		"tenant":       args.Tenant,
	}

	if len(args.AllowedGroups) > 0 {
		microsoftConfig["groups"] = args.AllowedGroups
	}

	configBytes, err := json.Marshal(microsoftConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Microsoft config: %w", err)
	}
	return configBytes, nil
}

func decodeAzureMicrosoftConfig(found *api.Connector, _ AzureMicrosoftConnectorArgs) (AzureMicrosoftConnectorArgs, error) {
	conf, err := parseConnectorConfig(found.Config)
	if err != nil {
		return AzureMicrosoftConnectorArgs{}, err
	}

	args := AzureMicrosoftConnectorArgs{
		ConnectorId:  found.Id,
		Name:         found.Name,
		Tenant:       conf.str("tenant"),
		ClientId:     conf.str("clientID"),
		ClientSecret: conf.str("clientSecret"),
		RedirectUri:  conf.str("redirectURI"),
	}
	// A string is what earlier versions of this provider wrote; read it back as
	// the legacy input so refresh keeps working and preview reports the
	// migration. A list is a restriction and must never be dropped, or the next
	// update would remove it. Dex treats an empty list like no list.
	if legacy, ok := conf.values["groups"].(string); ok {
		args.Groups = &legacy
	} else if groups := conf.strList("groups"); len(groups) > 0 {
		args.AllowedGroups = groups
	}
	return args, conf.Err()
}

// Create creates a new Azure Microsoft connector.
func (c *AzureMicrosoftConnector) Create(ctx context.Context, req infer.CreateRequest[AzureMicrosoftConnectorArgs]) (infer.CreateResponse[AzureMicrosoftConnectorState], error) {
	return c.controller().create(ctx, req)
}

// Read retrieves an existing Azure Microsoft connector.
func (c *AzureMicrosoftConnector) Read(ctx context.Context, req infer.ReadRequest[AzureMicrosoftConnectorArgs, AzureMicrosoftConnectorState]) (infer.ReadResponse[AzureMicrosoftConnectorArgs, AzureMicrosoftConnectorState], error) {
	return c.controller().read(ctx, req)
}

// Update updates an existing Azure Microsoft connector.
func (c *AzureMicrosoftConnector) Update(ctx context.Context, req infer.UpdateRequest[AzureMicrosoftConnectorArgs, AzureMicrosoftConnectorState]) (infer.UpdateResponse[AzureMicrosoftConnectorState], error) {
	return c.controller().update(ctx, req)
}

// Delete deletes a Azure Microsoft connector.
func (c *AzureMicrosoftConnector) Delete(ctx context.Context, req infer.DeleteRequest[AzureMicrosoftConnectorState]) (infer.DeleteResponse, error) {
	return c.controller().delete(ctx, req)
}

// microsoftTenant matches the tenant forms Dex documents for its Microsoft
// connector besides common, organizations and consumers: a tenant ID or a
// verified domain.
var microsoftTenant = regexp.MustCompile(`^([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}|[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+)$`)

// multiTenant lists the tenants for which Dex ignores the groups restriction.
var multiTenant = map[string]bool{"common": true, "organizations": true, "consumers": true}

// checkMicrosoftGroups validates tenant, allowedGroups and the removed groups
// string. Unknown values are not judged, so previews with values from other
// resources pass.
func checkMicrosoftGroups(inputs property.Map, args AzureMicrosoftConnectorArgs) []p.CheckFailure {
	known := func(key string) (property.Value, bool) {
		v, ok := inputs.GetOk(key)
		return v, ok && !v.IsComputed() && !v.IsNull()
	}
	var failures []p.CheckFailure

	tenantValue, tenantKnown := known("tenant")
	tenant := strings.ToLower(args.Tenant)
	if tenantKnown && tenant != "" && !multiTenant[tenant] && !microsoftTenant.MatchString(tenant) {
		failures = append(failures, p.CheckFailure{Property: "tenant",
			Reason: "must be \"common\", \"organizations\", \"consumers\", a tenant ID (UUID) or a tenant domain such as contoso.onmicrosoft.com"})
	}

	if _, ok := known("groups"); ok {
		failures = append(failures, p.CheckFailure{Property: "groups",
			Reason: "groups is no longer supported. It was written to Dex as a string, but Dex reads groups as a list, " +
				"so Dex failed to initialize this connector and no user could log in through it. " +
				"To restrict login to members of groups, set allowedGroups to a list of group names " +
				"(this requires a specific tenant ID or domain); otherwise remove groups"})
	}

	if v, ok := known("allowedGroups"); ok && v.IsArray() {
		switch {
		case v.AsArray().Len() == 0:
			failures = append(failures, p.CheckFailure{Property: "allowedGroups",
				Reason: "allowedGroups is empty. Dex treats an empty list like no list, so every user of the tenant could log in; " +
					"to allow every user, omit allowedGroups, otherwise list at least one group"})
		case tenantKnown && tenantValue.IsString() && multiTenant[tenant]:
			failures = append(failures, p.CheckFailure{Property: "allowedGroups",
				Reason: fmt.Sprintf("Dex ignores allowedGroups for tenant %q and lets every user log in; "+
					"set tenant to a tenant ID or domain to restrict login to these groups", args.Tenant)})
		}
	}
	return failures
}
