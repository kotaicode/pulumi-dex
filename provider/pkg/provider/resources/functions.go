package resources

import (
	"context"
	"fmt"
	"strings"
	"time"

	api "github.com/dexidp/dex/api/v2"
	"github.com/pulumi/pulumi-go-provider/infer"

	"github.com/kotaicode/pulumi-dex/pkg/provider"
)

// ============================================================================
// Lookup functions: read one Dex object, without state, writes or adoption.
// A missing object is an error naming the ID; other failures keep their gRPC
// status so that network and permission errors stay distinguishable.
// ============================================================================

// GetClient looks up an OAuth2 client (dex:resources:getClient).
type GetClient struct{}

// GetClientArgs are the inputs of getClient.
type GetClientArgs struct {
	Id string `pulumi:"id"`
}

// GetClientResult is a client as Dex stores it.
type GetClientResult struct {
	ClientId     string   `pulumi:"clientId"`
	Name         string   `pulumi:"name"`
	Secret       string   `pulumi:"secret" provider:"secret"` //nolint:gosec // G117: the client secret is returned as a Pulumi secret
	RedirectUris []string `pulumi:"redirectUris"`
	TrustedPeers []string `pulumi:"trustedPeers"`
	Public       bool     `pulumi:"public"`
	LogoUrl      string   `pulumi:"logoUrl"`
}

func (f *GetClient) Annotate(a infer.Annotator) {
	a.Describe(f, "Looks up an OAuth2 client in Dex by its client ID. Read-only: it does not manage or adopt the client. "+
		"Fails if no client with this ID exists. The client secret is returned as a secret.")
}

func (a *GetClientArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.Id, "Client ID to look up.")
}

func (r *GetClientResult) Annotate(a infer.Annotator) {
	a.Describe(&r.ClientId, "The client ID.")
	a.Describe(&r.Name, "Display name.")
	a.Describe(&r.Secret, "Client secret.")
	a.Describe(&r.RedirectUris, "Allowed redirect URIs.")
	a.Describe(&r.TrustedPeers, "Client IDs that may exchange tokens for this client.")
	a.Describe(&r.Public, "Whether the client is public (cannot keep a secret).")
	a.Describe(&r.LogoUrl, "Logo URL, or empty.")
}

func (f *GetClient) Invoke(ctx context.Context, req infer.FunctionRequest[GetClientArgs]) (infer.FunctionResponse[GetClientResult], error) {
	cfg := infer.GetConfig[provider.DexConfig](ctx)
	if cfg.Client == nil {
		return infer.FunctionResponse[GetClientResult]{}, fmt.Errorf("dex client not configured")
	}
	id := req.Input.Id
	getCtx, cancel := context.WithTimeout(ctx, time.Duration(provider.PtrOr(cfg.TimeoutSeconds, 5))*time.Second)
	defer cancel()
	resp, err := cfg.Client.GetClient(getCtx, &api.GetClientReq{Id: id})
	if err != nil {
		if clientAbsent(ctx, cfg, id, err) {
			return infer.FunctionResponse[GetClientResult]{}, lookupNotFound("client", id)
		}
		return infer.FunctionResponse[GetClientResult]{}, provider.WrapError("get", "client", id, err)
	}
	c := resp.GetClient()
	if c == nil {
		return infer.FunctionResponse[GetClientResult]{}, lookupNotFound("client", id)
	}
	return infer.FunctionResponse[GetClientResult]{Output: GetClientResult{
		ClientId: c.GetId(), Name: c.GetName(), Secret: c.GetSecret(), RedirectUris: c.GetRedirectUris(),
		TrustedPeers: c.GetTrustedPeers(), Public: c.GetPublic(), LogoUrl: c.GetLogoUrl(),
	}}, nil
}

// GetConnector looks up a connector of any type (dex:resources:getConnector).
type GetConnector struct{}

// GetConnectorArgs are the inputs of getConnector.
type GetConnectorArgs struct {
	Id string `pulumi:"id"`
}

// GetConnectorResult is a connector in the generic shape: its configuration
// as raw JSON, which may contain credentials and is returned as a secret.
type GetConnectorResult struct {
	ConnectorId string `pulumi:"connectorId"`
	Type        string `pulumi:"type"`
	Name        string `pulumi:"name"`
	RawConfig   string `pulumi:"rawConfig" provider:"secret"`
}

func (f *GetConnector) Annotate(a infer.Annotator) {
	a.Describe(f, "Looks up a connector in Dex by its ID, in the generic shape: type, name and the configuration as raw JSON. "+
		"Read-only: it does not manage or adopt the connector. Fails if no connector with this ID exists. "+
		"The configuration is returned as a secret because it may contain credentials.")
}

func (a *GetConnectorArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.Id, "Connector ID to look up.")
}

func (r *GetConnectorResult) Annotate(a infer.Annotator) {
	a.Describe(&r.ConnectorId, "The connector ID.")
	a.Describe(&r.Type, "Dex connector type, for example oidc, github or ldap.")
	a.Describe(&r.Name, "Display name.")
	a.Describe(&r.RawConfig, "Connector configuration as stored in Dex, as JSON.")
}

func (f *GetConnector) Invoke(ctx context.Context, req infer.FunctionRequest[GetConnectorArgs]) (infer.FunctionResponse[GetConnectorResult], error) {
	cfg := infer.GetConfig[provider.DexConfig](ctx)
	if cfg.Client == nil {
		return infer.FunctionResponse[GetConnectorResult]{}, fmt.Errorf("dex client not configured")
	}
	id := req.Input.Id
	listCtx, cancel := context.WithTimeout(ctx, time.Duration(provider.PtrOr(cfg.TimeoutSeconds, 5))*time.Second)
	defer cancel()
	resp, err := cfg.Client.ListConnectors(listCtx, &api.ListConnectorReq{})
	if err != nil {
		return infer.FunctionResponse[GetConnectorResult]{}, provider.WrapError("get", "connector", id, err)
	}
	for _, c := range resp.GetConnectors() {
		if c.GetId() == id {
			return infer.FunctionResponse[GetConnectorResult]{Output: GetConnectorResult{
				ConnectorId: c.GetId(), Type: c.GetType(), Name: c.GetName(), RawConfig: string(c.GetConfig()),
			}}, nil
		}
	}
	return infer.FunctionResponse[GetConnectorResult]{}, lookupNotFound("connector", id)
}

// GetLocalUser looks up a user in Dex's password database (dex:resources:getLocalUser).
type GetLocalUser struct{}

// GetLocalUserArgs are the inputs of getLocalUser.
type GetLocalUserArgs struct {
	Email string `pulumi:"email"`
}

// GetLocalUserResult is a password user. Dex does not return password hashes,
// so there is none.
type GetLocalUserResult struct {
	Email    string   `pulumi:"email"`
	UserId   string   `pulumi:"userId"`
	Username string   `pulumi:"username"`
	Groups   []string `pulumi:"groups"`
}

func (f *GetLocalUser) Annotate(a infer.Annotator) {
	a.Describe(f, "Looks up a user in Dex's password database by email (case-insensitive). Read-only: it does not "+
		"manage or adopt the user. Fails if no user with this email exists. Dex does not return password hashes, "+
		"so the result has none.")
}

func (a *GetLocalUserArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.Email, "Email of the user to look up.")
}

func (r *GetLocalUserResult) Annotate(a infer.Annotator) {
	a.Describe(&r.Email, "Login email, as Dex stores it.")
	a.Describe(&r.UserId, "Stable user ID, the subject of the user's tokens.")
	a.Describe(&r.Username, "Display name, or empty.")
	a.Describe(&r.Groups, "Groups of the user (Dex v2.46 or later; always empty on older servers).")
}

func (f *GetLocalUser) Invoke(ctx context.Context, req infer.FunctionRequest[GetLocalUserArgs]) (infer.FunctionResponse[GetLocalUserResult], error) {
	cfg := infer.GetConfig[provider.DexConfig](ctx)
	if cfg.Client == nil {
		return infer.FunctionResponse[GetLocalUserResult]{}, fmt.Errorf("dex client not configured")
	}
	email := req.Input.Email
	listCtx, cancel := context.WithTimeout(ctx, time.Duration(provider.PtrOr(cfg.TimeoutSeconds, 5))*time.Second)
	defer cancel()
	resp, err := cfg.Client.ListPasswords(listCtx, &api.ListPasswordReq{})
	if err != nil {
		return infer.FunctionResponse[GetLocalUserResult]{}, provider.WrapError("get", "local-user", email, err)
	}
	for _, pw := range resp.GetPasswords() {
		if strings.EqualFold(pw.GetEmail(), email) {
			return infer.FunctionResponse[GetLocalUserResult]{Output: GetLocalUserResult{
				Email: pw.GetEmail(), UserId: pw.GetUserId(), Username: pw.GetUsername(), Groups: pw.GetGroups(),
			}}, nil
		}
	}
	return infer.FunctionResponse[GetLocalUserResult]{}, lookupNotFound("local user", email)
}

func lookupNotFound(kind, id string) error {
	return fmt.Errorf("dex get %s %q: no %s with this ID exists in Dex", kind, id, kind)
}
