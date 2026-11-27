package resources

import (
	"context"
	"errors"
	"fmt"
	"time"

	api "github.com/dexidp/dex/api/v2"
	"github.com/pulumi/pulumi-go-provider/infer"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/kotaicode/pulumi-dex/pkg/provider"
)

// ============================================================================
// Shared connector lifecycle
//
// Every connector resource stores one Dex connector. connectorController does
// the work they share: the Dex calls with the provider's timeout, response
// flags, telling a failed call from an absent connector, the type check, the
// conflict on a taken ID and the idempotent delete. Each resource supplies an
// adapter: its Dex type and how its inputs map to the connector's config and
// back. Input checks stay in each resource's Check.
// ============================================================================

// dexConnectors calls Dex's connector API through the client and timeout of
// one configured provider.
type dexConnectors struct {
	client  api.DexClient
	timeout time.Duration
}

func connectorsOf(cfg provider.DexConfig) (dexConnectors, error) {
	if cfg.Client == nil {
		return dexConnectors{}, fmt.Errorf("dex client not configured")
	}
	return dexConnectors{client: cfg.Client, timeout: time.Duration(provider.PtrOr(cfg.TimeoutSeconds, 5)) * time.Second}, nil
}

// list returns every connector in Dex. Dex has no call that gets one connector,
// so a lookup lists them all; nothing is cached, so every read sees Dex as it is.
func (d dexConnectors) list(ctx context.Context) ([]*api.Connector, error) {
	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	resp, err := d.client.ListConnectors(ctx, &api.ListConnectorReq{})
	if err != nil {
		return nil, err
	}
	return resp.GetConnectors(), nil
}

func (d dexConnectors) create(ctx context.Context, conn *api.Connector) (*api.CreateConnectorResp, error) {
	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	return d.client.CreateConnector(ctx, &api.CreateConnectorReq{Connector: conn})
}

// update replaces type, name and config. It never sends new_grant_types, so
// grant types set in Dex stay as they are.
func (d dexConnectors) update(ctx context.Context, conn *api.Connector) (*api.UpdateConnectorResp, error) {
	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	return d.client.UpdateConnector(ctx, &api.UpdateConnectorReq{
		Id: conn.Id, NewType: conn.Type, NewName: conn.Name, NewConfig: conn.Config,
	})
}

func (d dexConnectors) delete(ctx context.Context, id string) error {
	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	_, err := d.client.DeleteConnector(ctx, &api.DeleteConnectorReq{Id: id})
	return err
}

// connectorController implements Create, Read, Update and Delete of a connector
// resource with inputs A and state S, which embeds A.
type connectorController[A, S any] struct {
	kind    string // label in provider errors, e.g. "github-connector"
	token   string // schema token, for the import commands that errors recommend
	dexType string // the Dex connector type; "" when the type is an input

	id    func(A) string
	name  func(A) string
	state func(A) S
	args  func(S) A

	// encode returns the connector config sent to Dex.
	encode func(A) ([]byte, error)
	// decode rebuilds the inputs from a connector in Dex. prior holds the
	// inputs before a refresh and is empty on import. An error means the stored
	// config cannot be read; it must not contain config values.
	decode func(remote *api.Connector, prior A) (A, error)

	// requires refuses inputs that the connected Dex would store without
	// applying them, such as a field an older Dex line does not know.
	requires func(cfg provider.DexConfig, args A) error

	// The generic Connector sets these. Its type is an input, so it reads a
	// connector of any type; the rest keeps its own checks and messages.
	typeOf          func(A) string
	validate        func(A) error
	listFailure     string                      // replaces "failed to list connectors"
	idChanged       func(old, new string) error // replaces "connectorId cannot be changed"
	requireDeleteID bool                        // fail Delete without an ID instead of asking Dex
}

func (c connectorController[A, S]) connectorType(args A) string {
	if c.typeOf != nil {
		return c.typeOf(args)
	}
	return c.dexType
}

func (c connectorController[A, S]) connector(args A) (*api.Connector, error) {
	if c.validate != nil {
		if err := c.validate(args); err != nil {
			return nil, err
		}
	}
	config, err := c.encode(args)
	if err != nil {
		return nil, err
	}
	return &api.Connector{Id: c.id(args), Type: c.connectorType(args), Name: c.name(args), Config: config}, nil
}

// require runs the requires check. A preview runs it as soon as the server
// version is known; while the provider configuration is unknown, the update
// itself runs it.
func (c connectorController[A, S]) require(ctx context.Context, dryRun bool, op string, args A) error {
	if c.requires == nil {
		return nil
	}
	cfg := infer.GetConfig[provider.DexConfig](ctx)
	if dryRun && cfg.Server == nil {
		return nil
	}
	if err := c.requires(cfg, args); err != nil {
		return provider.WrapError(op, c.kind, c.id(args), err)
	}
	return nil
}

func (c connectorController[A, S]) create(ctx context.Context, req infer.CreateRequest[A]) (infer.CreateResponse[S], error) {
	args := req.Inputs
	if err := c.require(ctx, req.DryRun, "create", args); err != nil {
		return infer.CreateResponse[S]{}, err
	}
	// A preview must not need a configured provider.
	if req.DryRun {
		return infer.CreateResponse[S]{ID: c.id(args), Output: c.state(args)}, nil
	}
	dex, err := connectorsOf(infer.GetConfig[provider.DexConfig](ctx))
	if err != nil {
		return infer.CreateResponse[S]{}, err
	}
	conn, err := c.connector(args)
	if err != nil {
		return infer.CreateResponse[S]{}, err
	}
	target := createTarget{kind: c.kind, token: c.token, name: req.Name, id: conn.Id}
	resp, err := dex.create(ctx, conn)
	if err != nil {
		return infer.CreateResponse[S]{}, target.failed(err)
	}
	if resp.GetAlreadyExists() {
		return infer.CreateResponse[S]{}, target.alreadyExists()
	}
	return infer.CreateResponse[S]{ID: conn.Id, Output: c.state(args)}, nil
}

// read reports an absent connector with an empty response: a refresh then
// removes the resource, and WithMissingImportsFailing fails an import. A failed
// call is returned as an error, so the state is kept.
func (c connectorController[A, S]) read(ctx context.Context, req infer.ReadRequest[A, S]) (infer.ReadResponse[A, S], error) {
	dex, err := connectorsOf(infer.GetConfig[provider.DexConfig](ctx))
	if err != nil {
		return infer.ReadResponse[A, S]{}, err
	}
	all, err := dex.list(ctx)
	if err != nil {
		listFailure := c.listFailure
		if listFailure == "" {
			listFailure = "failed to list connectors"
		}
		return infer.ReadResponse[A, S]{}, fmt.Errorf("%s: %w", listFailure, err)
	}
	var found *api.Connector
	for _, conn := range all {
		if conn.Id == req.ID {
			found = conn
			break
		}
	}
	if found == nil {
		return infer.ReadResponse[A, S]{}, nil
	}
	if c.dexType != "" && found.Type != c.dexType {
		return infer.ReadResponse[A, S]{}, wrongConnectorType(c.kind, found.Id, found.Type, c.dexType)
	}
	args, err := c.decode(found, req.Inputs)
	if err != nil {
		return infer.ReadResponse[A, S]{}, unreadableConfigError(c.kind, found.Id, err)
	}
	return infer.ReadResponse[A, S]{ID: found.Id, Inputs: args, State: c.state(args)}, nil
}

func (c connectorController[A, S]) update(ctx context.Context, req infer.UpdateRequest[A, S]) (infer.UpdateResponse[S], error) {
	args := req.Inputs
	if err := c.require(ctx, req.DryRun, "update", args); err != nil {
		return infer.UpdateResponse[S]{}, err
	}
	if req.DryRun {
		return infer.UpdateResponse[S]{Output: c.state(args)}, nil
	}
	dex, err := connectorsOf(infer.GetConfig[provider.DexConfig](ctx))
	if err != nil {
		return infer.UpdateResponse[S]{}, err
	}
	// connectorId is replaceOnChanges, so the engine never sends a changed ID.
	if old, id := c.id(c.args(req.State)), c.id(args); id != old {
		if c.idChanged != nil {
			return infer.UpdateResponse[S]{}, c.idChanged(old, id)
		}
		return infer.UpdateResponse[S]{}, errors.New("connectorId cannot be changed")
	}
	conn, err := c.connector(args)
	if err != nil {
		return infer.UpdateResponse[S]{}, err
	}
	resp, err := dex.update(ctx, conn)
	if err != nil {
		return infer.UpdateResponse[S]{}, provider.WrapError("update", c.kind, conn.Id, err)
	}
	if resp.GetNotFound() {
		return infer.UpdateResponse[S]{}, provider.NotFoundError("update", c.kind, conn.Id)
	}
	return infer.UpdateResponse[S]{Output: c.state(args)}, nil
}

// delete succeeds when the connector is already gone, so a repeated delete
// succeeds too. Dex reports that with a not_found flag, or with codes.NotFound.
func (c connectorController[A, S]) delete(ctx context.Context, req infer.DeleteRequest[S]) (infer.DeleteResponse, error) {
	dex, err := connectorsOf(infer.GetConfig[provider.DexConfig](ctx))
	if err != nil {
		return infer.DeleteResponse{}, err
	}
	id := req.ID
	if id == "" {
		id = c.id(c.args(req.State))
	}
	if id == "" && c.requireDeleteID {
		return infer.DeleteResponse{}, fmt.Errorf("cannot delete connector: no ID provided in request or state")
	}
	if err := dex.delete(ctx, id); err != nil && status.Code(err) != codes.NotFound {
		return infer.DeleteResponse{}, provider.WrapError("delete", c.kind, id, err)
	}
	return infer.DeleteResponse{}, nil
}
