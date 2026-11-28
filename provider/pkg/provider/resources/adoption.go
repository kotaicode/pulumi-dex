package resources

import (
	"context"
	"fmt"
	"strings"

	p "github.com/pulumi/pulumi-go-provider"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/kotaicode/pulumi-dex/pkg/provider"
)

// Resource tokens as they appear in schema.json. They are used in the
// `pulumi import` commands that Create and Read recommend; a test keeps them in
// sync with the generated schema.
const (
	tokenClient                  = "dex:resources:Client"
	tokenConnector               = "dex:resources:Connector"
	tokenAzureOidcConnector      = "dex:resources:AzureOidcConnector"
	tokenAzureMicrosoftConnector = "dex:resources:AzureMicrosoftConnector"
	tokenCognitoOidcConnector    = "dex:resources:CognitoOidcConnector"
	tokenGitHubConnector         = "dex:resources:GitHubConnector"
	tokenGitLabConnector         = "dex:resources:GitLabConnector"
	tokenGoogleConnector         = "dex:resources:GoogleConnector"
	tokenLocalConnector          = "dex:resources:LocalConnector"
	tokenLdapConnector           = "dex:resources:LdapConnector"
	tokenLocalUser               = "dex:resources:LocalUser"
)

// createTarget identifies the object a Create was for, for its error messages.
type createTarget struct {
	kind  string // label used in provider errors, e.g. "github-connector"
	token string // schema token, e.g. "dex:resources:GitHubConnector"
	name  string // Pulumi resource name
	id    string // Dex ID
}

func (t createTarget) noun() string {
	switch t.token {
	case tokenClient:
		return "client"
	case tokenLocalUser:
		return "local user"
	default:
		return "connector"
	}
}

func (t createTarget) importCommand() string {
	return fmt.Sprintf("pulumi import %s %s %s", t.token, t.name, t.id)
}

// alreadyExists reports that the ID is taken. The same ID, or even an identical
// configuration, is no proof that this stack owns the object, so Create never
// takes it over: another stack could otherwise destroy it. Taking over an
// object requires an explicit import.
func (t createTarget) alreadyExists() error {
	return provider.WrapError("create", t.kind, t.id, fmt.Errorf(
		"a %s with this ID already exists in Dex, and Pulumi does not take over existing objects; "+
			"to manage it with this resource, import it with `%s`, or choose a different ID",
		t.noun(), t.importCommand()))
}

// failed reports a Create RPC error. Create is never retried: Dex has no
// idempotency key, so a repeated Create cannot tell its own earlier write from
// another owner's object. When the response was lost, the outcome is unknown.
func (t createTarget) failed(err error) error {
	switch status.Code(err) {
	case codes.AlreadyExists:
		return t.alreadyExists()
	case codes.DeadlineExceeded, codes.Canceled, codes.Unavailable:
		return provider.WrapError("create", t.kind, t.id, fmt.Errorf(
			"the outcome is unknown (%w): the %s may have been created in Dex before the response was lost. "+
				"Check whether %s %q exists in Dex; if it does, import it with `%s` instead of creating it again",
			err, t.noun(), t.noun(), t.id, t.importCommand()))
	default:
		if isStaticObjectError(err) {
			return t.definedInDexConfig()
		}
		return provider.WrapError("create", t.kind, t.id, err)
	}
}

// isStaticObjectError reports Dex's refusal to create a client or connector
// that its configuration file defines. Dex v2.46.1 returns it untyped (gRPC
// Unknown), so the message is the only signal.
func isStaticObjectError(err error) bool {
	if status.Code(err) != codes.Unknown {
		return false
	}
	msg := status.Convert(err).Message()
	return strings.Contains(msg, "static connectors: read-only") || strings.Contains(msg, "static clients: read-only") ||
		strings.Contains(msg, "static passwords: read-only")
}

// definedInDexConfig explains that the ID belongs to an object from Dex's
// configuration file, which the API cannot create or change.
func (t createTarget) definedInDexConfig() error {
	if t.token == tokenLocalConnector && t.id == "local" {
		return provider.WrapError("create", t.kind, t.id, fmt.Errorf(
			"the connector ID \"local\" belongs to Dex's built-in password login, which Dex provides when "+
				"enablePasswordDB is true in its configuration; Dex does not let the API create or change it. "+
				"Password login through \"local\" already exists: remove this resource, or choose another connectorId "+
				"for an additional password login"))
	}
	return provider.WrapError("create", t.kind, t.id, fmt.Errorf(
		"a %s with this ID is defined in Dex's configuration file, and Dex does not let the API create or change it; "+
			"remove it from Dex's configuration to manage it with Pulumi, or choose a different ID", t.noun()))
}

// wrongConnectorType reports a connector whose Dex type does not match the typed
// resource reading it. Reading it anyway would produce state that a later update
// uses to rewrite the connector as this resource's type.
func wrongConnectorType(kind, id, got, want string) error {
	return provider.WrapError("read", kind, id, fmt.Errorf(
		"the connector in Dex has type %q, not %q, so this resource cannot manage it; "+
			"import it as %s, or as the resource for type %q",
		got, want, tokenConnector, got))
}

// WithMissingImportsFailing makes the import of an ID that Dex does not have
// fail with a not-found error. Read reports a missing object with an empty ID,
// which on refresh removes the resource from the state. An import sends no
// prior state, and there infer still encodes the empty response as
// properties, so the Pulumi CLI would import an empty resource instead.
func WithMissingImportsFailing(prov p.Provider) p.Provider {
	read := prov.Read
	if read == nil {
		return prov
	}
	prov.Read = func(ctx context.Context, req p.ReadRequest) (p.ReadResponse, error) {
		resp, err := read(ctx, req)
		if err != nil || resp.ID != "" || req.Properties.Len() > 0 {
			return resp, err
		}
		t := createTarget{token: string(req.Urn.Type())}
		key := "ID"
		if t.token == tokenLocalUser {
			key = "email"
		}
		return p.ReadResponse{}, provider.WrapError("import", t.noun(), req.ID,
			fmt.Errorf("%w: no %s with this %s exists", provider.ErrNotFound, t.noun(), key))
	}
	return prov
}
