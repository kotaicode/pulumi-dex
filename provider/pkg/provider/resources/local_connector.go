package resources

import (
	"context"

	api "github.com/dexidp/dex/api/v2"
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
)

// ============================================================================
// LocalConnector - Builtin/local connector (type: "local")
// ============================================================================

// LocalConnectorArgs defines inputs for LocalConnector.
type LocalConnectorArgs struct {
	ConnectorId string `pulumi:"connectorId" provider:"replaceOnChanges"`
	Name        string `pulumi:"name"`
	Enabled     *bool  `pulumi:"enabled,optional"`
}

// LocalConnectorState defines outputs for LocalConnector.
type LocalConnectorState struct {
	LocalConnectorArgs
}

// LocalConnector adds a password login to Dex. Dex v2.46.1 resolves every
// connector of type "local" to its password database, whatever its ID and
// whether enablePasswordDB is set, so the connector cannot exist disabled.
type LocalConnector struct{}

// Annotate provides schema metadata.
func (c *LocalConnector) Annotate(a infer.Annotator) {
	a.Describe(c, "Adds a password login to Dex: a connector of type local, through which users log in with the "+
		"passwords stored in Dex's password database. Dex offers it even when enablePasswordDB is false in its "+
		"configuration. To turn this password login off, remove the resource. With enablePasswordDB: true, Dex "+
		"already provides a password login with the ID 'local', which cannot be managed through the API.")
}

// Annotate provides schema metadata for LocalConnectorArgs.
func (c *LocalConnectorArgs) Annotate(a infer.Annotator) {
	a.Describe(&c.ConnectorId, "Unique identifier for the password login connector. 'local' is reserved for Dex's "+
		"built-in password login when enablePasswordDB is true.")
	a.Describe(&c.Name, "Human-readable name for the connector, displayed to users during login.")
	a.Describe(&c.Enabled, "Deprecated: a local connector in Dex always offers password login. Only true is accepted; "+
		"to turn password login off, remove the resource.")
	a.Deprecate(&c.Enabled, "enabled has no effect in Dex; remove the resource to turn password login off.")
}

// Annotate provides schema metadata for LocalConnectorState.
func (c *LocalConnectorState) Annotate(a infer.Annotator) {
	// LocalConnectorState embeds LocalConnectorArgs, so field descriptions are inherited
}

// Check validates inputs.
func (c *LocalConnector) Check(ctx context.Context, req infer.CheckRequest) (infer.CheckResponse[LocalConnectorArgs], error) {
	args, failures, err := infer.DefaultCheck[LocalConnectorArgs](ctx, req.NewInputs)
	if err != nil {
		return infer.CheckResponse[LocalConnectorArgs]{Failures: failures}, err
	}

	// Dex has no disabled local connector: enabled: false used to be accepted
	// while password login stayed on.
	if v, ok := req.NewInputs.GetOk("enabled"); ok && v.IsBool() && !v.AsBool() {
		failures = append(failures, p.CheckFailure{Property: "enabled",
			Reason: "enabled: false does not turn password login off: Dex offers password login through every " +
				"connector of type local, so users can log in through this connector with their Dex passwords; " +
				"remove the resource to turn password login off"})
	}

	// Apply defaults
	if args.Enabled == nil {
		defaultEnabled := true
		args.Enabled = &defaultEnabled
	}

	return infer.CheckResponse[LocalConnectorArgs]{
		Inputs:   args,
		Failures: failures,
	}, nil
}

func (c *LocalConnector) controller() connectorController[LocalConnectorArgs, LocalConnectorState] {
	return connectorController[LocalConnectorArgs, LocalConnectorState]{
		kind:    "local-connector",
		token:   tokenLocalConnector,
		dexType: "local",
		id:      func(a LocalConnectorArgs) string { return a.ConnectorId },
		name:    func(a LocalConnectorArgs) string { return a.Name },
		state:   func(a LocalConnectorArgs) LocalConnectorState { return LocalConnectorState{LocalConnectorArgs: a} },
		args:    func(s LocalConnectorState) LocalConnectorArgs { return s.LocalConnectorArgs },
		encode:  encodeLocalConfig,
		decode:  decodeLocalConfig,
	}
}

// encodeLocalConfig returns an empty JSON object: a local connector has no settings.
func encodeLocalConfig(LocalConnectorArgs) ([]byte, error) {
	return []byte("{}"), nil
}

// decodeLocalConfig does not read the config. An existing local connector is a
// working password login.
func decodeLocalConfig(found *api.Connector, _ LocalConnectorArgs) (LocalConnectorArgs, error) {
	enabled := true
	return LocalConnectorArgs{
		ConnectorId: found.Id,
		Name:        found.Name,
		Enabled:     &enabled,
	}, nil
}

// Create creates a new local connector.
func (c *LocalConnector) Create(ctx context.Context, req infer.CreateRequest[LocalConnectorArgs]) (infer.CreateResponse[LocalConnectorState], error) {
	return c.controller().create(ctx, req)
}

// Read retrieves an existing local connector.
func (c *LocalConnector) Read(ctx context.Context, req infer.ReadRequest[LocalConnectorArgs, LocalConnectorState]) (infer.ReadResponse[LocalConnectorArgs, LocalConnectorState], error) {
	return c.controller().read(ctx, req)
}

// Update updates an existing local connector.
func (c *LocalConnector) Update(ctx context.Context, req infer.UpdateRequest[LocalConnectorArgs, LocalConnectorState]) (infer.UpdateResponse[LocalConnectorState], error) {
	return c.controller().update(ctx, req)
}

// Delete deletes a local connector.
func (c *LocalConnector) Delete(ctx context.Context, req infer.DeleteRequest[LocalConnectorState]) (infer.DeleteResponse, error) {
	return c.controller().delete(ctx, req)
}
