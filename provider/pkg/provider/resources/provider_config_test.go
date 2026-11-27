package resources

import (
	"net"
	"strings"
	"sync/atomic"
	"testing"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
	"google.golang.org/grpc"
)

// =============================================================================
// Provider configuration through the provider protocol
// =============================================================================

// countingListener counts connection attempts, so tests can prove that the
// provider did not dial Dex.
type countingListener struct {
	net.Listener
	accepted atomic.Int32
}

func (l *countingListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err == nil {
		l.accepted.Add(1)
	}
	return c, err
}

// plaintextDex serves an empty plaintext gRPC API, like a local Dex without TLS.
func plaintextDex(t *testing.T) *countingListener {
	t.Helper()
	inner, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	lis := &countingListener{Listener: inner}
	srv := grpc.NewServer()
	done := make(chan error, 1)
	go func() { done <- srv.Serve(lis) }()
	t.Cleanup(func() {
		srv.Stop()
		if err := <-done; err != nil {
			t.Errorf("plaintext Dex: %v", err)
		}
	})
	return lis
}

func configureWith(t *testing.T, args map[string]property.Value) error {
	t.Helper()
	return newTestProvider(t).Configure(p.ConfigureRequest{Args: property.NewMap(args)})
}

// A configuration without TLS options used to connect unencrypted. It now uses
// TLS, and against a Dex without TLS the error says how to opt out.
func TestProviderConfigure_defaultsToTLS(t *testing.T) {
	dex := plaintextDex(t)
	err := configureWith(t, map[string]property.Value{
		"host": property.New(dex.Addr().String()), "timeoutSeconds": property.New(2.0),
	})
	if err == nil {
		t.Fatal("the provider connected without TLS although plaintext was not requested")
	}
	if !strings.Contains(err.Error(), "plaintext: true") {
		t.Errorf("error should name plaintext: true as the fix, got: %v", err)
	}
}

func TestProviderConfigure_plaintextOptIn(t *testing.T) {
	dex := plaintextDex(t)
	if err := configureWith(t, map[string]property.Value{
		"host": property.New(dex.Addr().String()), "plaintext": property.New(true), "timeoutSeconds": property.New(2.0),
	}); err != nil {
		t.Fatalf("plaintext: true against a plaintext Dex: %v", err)
	}
}

// During a preview, provider inputs can be unknown because they come from other
// resources. The provider must not guess them and dial Dex.
func TestProviderConfigure_unknownInputsDoNotDial(t *testing.T) {
	dex := plaintextDex(t)
	for name, args := range map[string]map[string]property.Value{
		"unknown host": {"host": property.New(property.Computed)},
		"unknown caCert": {
			"host": property.New(dex.Addr().String()), "caCert": property.New(property.Computed),
			"timeoutSeconds": property.New(1.0),
		},
	} {
		t.Run(name, func(t *testing.T) {
			before := dex.accepted.Load()
			if err := configureWith(t, args); err != nil {
				t.Errorf("Configure with unknown inputs: %v", err)
			}
			if n := dex.accepted.Load() - before; n != 0 {
				t.Errorf("provider dialed Dex %d times with unknown configuration", n)
			}
		})
	}
}

// pulumi import passes the default provider's stack config to Configure as
// strings, for example "true" for a value set with --type bool. Those must work
// like typed values, or a Dex without TLS could not be imported from.
func TestProviderConfigure_acceptsStringEncodedValues(t *testing.T) {
	dex := plaintextDex(t)
	if err := configureWith(t, map[string]property.Value{
		"host": property.New(dex.Addr().String()), "plaintext": property.New("true"),
		"insecureSkipVerify": property.New("false"), "timeoutSeconds": property.New("2"),
	}); err != nil {
		t.Fatalf("string-encoded configuration: %v", err)
	}
	if err := configureWith(t, map[string]property.Value{
		"host": property.New(dex.Addr().String()), "plaintext": property.New("yes please"),
	}); err == nil || !strings.Contains(err.Error(), "plaintext") {
		t.Errorf("a string that is no boolean must fail and name the field, got %v", err)
	}
}

// pulumi import into a stack whose dex provider is not registered yet stores
// the provider's stack config as strings ("true"), while pulumi up stores the
// checked values (true). The provider must not see that as a configuration
// change: every provider change replaces the resources that use it.
func TestProviderDiffConfig_stringEncodedValuesAreNoChange(t *testing.T) {
	imported := property.NewMap(map[string]property.Value{
		"host": property.New("127.0.0.1:5557"), "plaintext": property.New("true"),
		"insecureSkipVerify": property.New("false"), "timeoutSeconds": property.New("7"),
	})
	checked := property.NewMap(map[string]property.Value{
		"host": property.New("127.0.0.1:5557"), "plaintext": property.New(true),
		"insecureSkipVerify": property.New(false), "timeoutSeconds": property.New(7.0),
	})
	diff := func(t *testing.T, olds, news property.Map) p.DiffResponse {
		t.Helper()
		resp, err := newTestProvider(t).DiffConfig(p.DiffRequest{State: olds, OldInputs: olds, Inputs: news})
		if err != nil {
			t.Fatalf("DiffConfig: %v", err)
		}
		return resp
	}

	if resp := diff(t, imported, checked); resp.HasChanges || resp.DeleteBeforeReplace || len(resp.DetailedDiff) > 0 {
		t.Errorf("imported string values against the same checked values: %+v, want no change", resp)
	}

	// A real change is still a change, and still replaces.
	for key, value := range map[string]property.Value{
		"plaintext":      property.New(false),
		"timeoutSeconds": property.New(8.0),
		"host":           property.New("dex.example.com:5557"),
	} {
		resp := diff(t, imported, checked.Set(key, value))
		if d, ok := resp.DetailedDiff[key]; !resp.HasChanges || !ok || d.Kind != p.UpdateReplace {
			t.Errorf("changing %s after an import: %+v, want a replacement", key, resp)
		}
	}
}
