package resources

import (
	"fmt"

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
)

// createTarget identifies the object a Create was for, for its error messages.
type createTarget struct {
	kind  string // label used in provider errors, e.g. "github-connector"
	token string // schema token, e.g. "dex:resources:GitHubConnector"
	name  string // Pulumi resource name
	id    string // Dex ID
}

func (t createTarget) noun() string {
	if t.token == tokenClient {
		return "client"
	}
	return "connector"
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
		return provider.WrapError("create", t.kind, t.id, err)
	}
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
