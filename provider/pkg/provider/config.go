package provider

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"time"

	api "github.com/dexidp/dex/api/v2"
	"github.com/pulumi/pulumi-go-provider/infer"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// DexConfig describes provider-level configuration (connection to Dex gRPC).
// This struct doubles as the configured client object passed to resources.
// Note: Environment variables can be set but are not automatically read by the provider.
// Users should set them in their Pulumi program or use Pulumi config.
type DexConfig struct {
	Host            string  `pulumi:"host"`
	CACertPEM       *string `pulumi:"caCert,optional" provider:"secret"`
	ClientCertPEM   *string `pulumi:"clientCert,optional" provider:"secret"`
	ClientKeyPEM    *string `pulumi:"clientKey,optional" provider:"secret"`
	InsecureSkipTLS *bool   `pulumi:"insecureSkipVerify,optional"`
	TimeoutSeconds  *int    `pulumi:"timeoutSeconds,optional"`

	// internal fields are not exposed in schema and are used at runtime only.
	Client api.DexClient
}

// Annotate config fields with descriptions & defaults for the schema.
func (c *DexConfig) Annotate(a infer.Annotator) {
	a.Describe(&c.Host, "Dex gRPC host:port, e.g. dex.internal.example.com:5557.")
	a.Describe(&c.CACertPEM, "PEM-encoded CA certificate for validating Dex's TLS certificate.")
	a.Describe(&c.ClientCertPEM, "PEM-encoded client certificate for mTLS to Dex.")
	a.Describe(&c.ClientKeyPEM, "PEM-encoded private key for the client certificate.")
	a.Describe(&c.InsecureSkipTLS, "If true, disables TLS verification (development only).")
	a.Describe(&c.TimeoutSeconds, "Per-RPC timeout in seconds when talking to Dex.")
}

// Configure is called once per provider instance to establish a Dex gRPC client.
// It satisfies infer.CustomConfigure via pointer receiver.
func (c *DexConfig) Configure(ctx context.Context) error {
	if c.Host == "" {
		return fmt.Errorf("host is required")
	}

	// TODO: Optionally make Configure preview-safe by checking runInfo.Preview
	// For now, we'll let Configure connect to Dex even in preview mode.
	// The Create/Update methods will short-circuit based on req.DryRun before making API calls.

	dialCtx, cancel := context.WithTimeout(ctx, time.Duration(PtrOr(c.TimeoutSeconds, 5))*time.Second)
	defer cancel()

	var (
		conn *grpc.ClientConn
		err  error
	)

	// Prefer TLS/mTLS when credentials are provided; otherwise fall back to insecure (plaintext)
	// to match Dex's examples and make local development easy. See:
	// https://dexidp.io/docs/configuration/api/
	hasTLSMaterial := (c.CACertPEM != nil && *c.CACertPEM != "") ||
		(c.ClientCertPEM != nil && *c.ClientCertPEM != "") ||
		(c.ClientKeyPEM != nil && *c.ClientKeyPEM != "") ||
		PtrOr(c.InsecureSkipTLS, false)

	if hasTLSMaterial {
		tlsCfg := &tls.Config{}

		// Root CA for validating Dex's server certificate.
		if c.CACertPEM != nil && *c.CACertPEM != "" {
			rootCAs := x509.NewCertPool()
			if ok := rootCAs.AppendCertsFromPEM([]byte(*c.CACertPEM)); !ok {
				return fmt.Errorf("failed to parse CA certificate")
			}
			tlsCfg.RootCAs = rootCAs
		}

		// Optional client certificate for mTLS.
		if (c.ClientCertPEM != nil && *c.ClientCertPEM != "") || (c.ClientKeyPEM != nil && *c.ClientKeyPEM != "") {
			if c.ClientCertPEM == nil || c.ClientKeyPEM == nil || *c.ClientCertPEM == "" || *c.ClientKeyPEM == "" {
				return fmt.Errorf("both clientCert and clientKey must be provided (and non-empty) for mTLS")
			}
			cert, err := tls.X509KeyPair([]byte(*c.ClientCertPEM), []byte(*c.ClientKeyPEM))
			if err != nil {
				return fmt.Errorf("failed to load client certificate/key: %w", err)
			}
			tlsCfg.Certificates = []tls.Certificate{cert}
		}

		// Optionally skip server certificate verification (development only).
		if PtrOr(c.InsecureSkipTLS, false) {
			tlsCfg.InsecureSkipVerify = true
		}

		conn, err = grpc.NewClient(
			c.Host,
			grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
		)
	} else {
		conn, err = grpc.NewClient(
			c.Host,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
	}

	if err != nil {
		return fmt.Errorf("failed to connect to Dex at %s: %w", c.Host, err)
	}

	// Trigger connection establishment and wait up to the configured timeout
	// for the connection to become READY. This approximates the previous
	// grpc.DialContext(..., WithBlock) behavior and gives us a lightweight
	// health check without issuing any RPCs.
	conn.Connect()
	for {
		state := conn.GetState()
		if state == connectivity.Ready {
			break
		}
		if !conn.WaitForStateChange(dialCtx, state) {
			return fmt.Errorf("timed out while connecting to Dex at %s", c.Host)
		}
	}

	c.Client = api.NewDexClient(conn)

	return nil
}

// PtrOr returns the value pointed to by p, or def if p is nil.
func PtrOr[T any](p *T, def T) T {
	if p == nil {
		return def
	}
	return *p
}
