package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	api "github.com/dexidp/dex/api/v2"
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

// minRedactLen is the shortest connector-config string that is redacted from RPC
// errors. Shorter values (scopes such as "email", booleans spelled as strings) would
// mostly garble unrelated words in the message without protecting real credentials.
const minRedactLen = 8

// redactRPCErrors is a gRPC client interceptor that removes credential values sent
// in a request from the error Dex returns for it. Dex wraps raw storage errors into
// its messages, and the provider surfaces those messages to Pulumi output, so a
// backend that echoed the stored object would otherwise reveal secrets. The status
// code and details are preserved because callers classify errors by code.
func redactRPCErrors(
	ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption,
) error {
	err := invoker(ctx, method, req, reply, cc, opts...)
	if err == nil {
		return nil
	}
	secrets := requestSecrets(req)
	if len(secrets) == 0 {
		return err
	}
	st, ok := status.FromError(err)
	if !ok {
		return err
	}
	msg := st.Message()
	for _, s := range secrets {
		msg = strings.ReplaceAll(msg, s, "[secret]")
	}
	if msg == st.Message() {
		return err
	}
	pb := st.Proto()
	pb.Message = msg
	return status.FromProto(pb).Err()
}

// requestSecrets returns the credential values carried by a Dex write request,
// longest first so that a value containing another is replaced whole.
func requestSecrets(req any) []string {
	var out []string
	switch r := req.(type) {
	case *api.CreateClientReq:
		if s := r.GetClient().GetSecret(); s != "" {
			out = append(out, s)
		}
	case *api.CreateConnectorReq:
		out = configStrings(r.GetConnector().GetConfig())
	case *api.UpdateConnectorReq:
		out = configStrings(r.GetNewConfig())
	}
	sort.Slice(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}

// configStrings returns the raw config and every sufficiently long string leaf in
// it. Connector configs are opaque (see rawConfig), so all values are treated as
// potentially secret.
func configStrings(config []byte) []string {
	if len(config) == 0 {
		return nil
	}
	out := []string{string(config)}
	var v any
	if json.Unmarshal(config, &v) != nil {
		return out
	}
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case string:
			if len(x) >= minRedactLen {
				out = append(out, x)
			}
		case []any:
			for _, e := range x {
				walk(e)
			}
		case map[string]any:
			for _, e := range x {
				walk(e)
			}
		}
	}
	walk(v)
	return out
}

// WithSecretReadInputs marks the inputs returned by Read as secret wherever the
// provider's schema declares a secret property.
//
// The infer framework only applies output secrecy (WireDependencies) to Read; the
// inputs it returns are plain unless the request's own inputs were secret. That is
// the case on refresh, but not on `pulumi import`, which has no prior inputs, so
// imported credentials would be stored in plaintext in state inputs and emitted in
// the generated program. The schema, derived from the `provider:"secret"` struct
// tags, is the single source of truth for which properties are secret.
func WithSecretReadInputs(prov p.Provider) p.Provider {
	read := prov.Read
	getSchema := prov.GetSchema
	if read == nil || getSchema == nil {
		return prov
	}

	var (
		once    sync.Once
		paths   secretSchema
		loadErr error
	)
	prov.Read = func(ctx context.Context, req p.ReadRequest) (p.ReadResponse, error) {
		resp, err := read(ctx, req)
		if err != nil || resp.ID == "" {
			return resp, err
		}
		once.Do(func() {
			var s p.GetSchemaResponse
			if s, loadErr = getSchema(ctx, p.GetSchemaRequest{}); loadErr == nil {
				paths, loadErr = parseSecretSchema(s.Schema)
			}
		})
		if loadErr != nil {
			// Failing is safer than returning credentials we cannot classify.
			return p.ReadResponse{}, fmt.Errorf("cannot determine secret inputs for %s: %w", req.Urn.Type(), loadErr)
		}
		resp.Inputs = paths.mark(string(req.Urn.Type()), resp.Inputs)
		return resp, nil
	}
	return prov
}

// secretProps describes which properties of an object are secret and which refer
// to an object type that may itself contain secrets.
type secretProps struct {
	secret map[string]bool
	ref    map[string]string // property -> type token
}

type secretSchema struct {
	resources map[string]secretProps
	types     map[string]secretProps
}

type schemaProperty struct {
	Secret bool   `json:"secret"`
	Ref    string `json:"$ref"`
}

func newSecretProps(props map[string]schemaProperty) secretProps {
	sp := secretProps{secret: map[string]bool{}, ref: map[string]string{}}
	for name, prop := range props {
		if prop.Secret {
			sp.secret[name] = true
		}
		if typ, ok := strings.CutPrefix(prop.Ref, "#/types/"); ok {
			sp.ref[name] = typ
		}
	}
	return sp
}

func parseSecretSchema(schema string) (secretSchema, error) {
	var raw struct {
		Resources map[string]struct {
			InputProperties map[string]schemaProperty `json:"inputProperties"`
		} `json:"resources"`
		Types map[string]struct {
			Properties map[string]schemaProperty `json:"properties"`
		} `json:"types"`
	}
	if err := json.Unmarshal([]byte(schema), &raw); err != nil {
		return secretSchema{}, fmt.Errorf("decode schema: %w", err)
	}
	s := secretSchema{resources: map[string]secretProps{}, types: map[string]secretProps{}}
	for token, r := range raw.Resources {
		s.resources[token] = newSecretProps(r.InputProperties)
	}
	for token, t := range raw.Types {
		s.types[token] = newSecretProps(t.Properties)
	}
	return s, nil
}

func (s secretSchema) mark(resourceType string, inputs property.Map) property.Map {
	props, ok := s.resources[resourceType]
	if !ok {
		return inputs
	}
	return s.markMap(props, inputs, 0)
}

// maxDepth bounds recursion through (potentially self-referential) object types.
const maxDepth = 8

func (s secretSchema) markMap(props secretProps, m property.Map, depth int) property.Map {
	out := m.AsMap()
	for name, v := range out {
		switch {
		case props.secret[name]:
			out[name] = v.WithSecret(true)
		case props.ref[name] != "" && v.IsMap() && depth < maxDepth:
			if nested, ok := s.types[props.ref[name]]; ok {
				out[name] = property.New(s.markMap(nested, v.AsMap(), depth+1)).WithSecret(v.IsSecret())
			}
		}
	}
	return property.NewMap(out)
}
