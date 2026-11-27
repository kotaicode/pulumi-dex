package provider

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	api "github.com/dexidp/dex/api/v2"
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// DexConfig describes provider-level configuration (connection to Dex gRPC).
// This struct doubles as the configured client object passed to resources.
// The provider reads no environment variables; every option comes from the
// provider configuration.
type DexConfig struct {
	Host            string  `pulumi:"host"`
	CACertPEM       *string `pulumi:"caCert,optional" provider:"secret"`
	ClientCertPEM   *string `pulumi:"clientCert,optional" provider:"secret"`
	ClientKeyPEM    *string `pulumi:"clientKey,optional" provider:"secret"`
	InsecureSkipTLS *bool   `pulumi:"insecureSkipVerify,optional"`
	Plaintext       *bool   `pulumi:"plaintext,optional"`
	TimeoutSeconds  *int    `pulumi:"timeoutSeconds,optional"`

	// internal fields are not exposed in schema and are used at runtime only.
	Client api.DexClient
	// Server is what the connected Dex reported through GetVersion; nil until
	// Configure has connected.
	Server *ServerInfo
}

// Annotate config fields with descriptions & defaults for the schema.
func (c *DexConfig) Annotate(a infer.Annotator) {
	a.Describe(&c.Host, "Dex gRPC host:port, e.g. dex.internal.example.com:5557.")
	a.Describe(&c.CACertPEM, "PEM-encoded CA certificate that issued Dex's gRPC server certificate. "+
		"When set, only this CA is trusted instead of the system's root CAs.")
	a.Describe(&c.ClientCertPEM, "PEM-encoded client certificate for mutual TLS with Dex. Requires clientKey.")
	a.Describe(&c.ClientKeyPEM, "PEM-encoded private key for clientCert.")
	a.Describe(&c.InsecureSkipTLS, "If true, connects with TLS but does not verify Dex's certificate "+
		"(development only). The connection is still encrypted; for a Dex without TLS, use plaintext.")
	a.Describe(&c.Plaintext, "If true, connects to Dex without TLS. Only for a Dex gRPC API that is not "+
		"TLS-enabled and is reachable only on a trusted network, such as localhost or inside a cluster. "+
		"Cannot be combined with caCert, clientCert, clientKey or insecureSkipVerify. "+
		"By default the provider connects with TLS and verifies Dex's certificate against the system's root CAs.")
	a.Describe(&c.TimeoutSeconds, "Per-RPC timeout in seconds when talking to Dex.")
}

// Configure is called once per provider instance to establish a Dex gRPC client.
// It satisfies infer.CustomConfigure via pointer receiver. While the configuration
// still contains unknown values, WithPreviewSafeConfigure skips it.
func (c *DexConfig) Configure(ctx context.Context) error {
	if c.Host == "" {
		return fmt.Errorf("host is required")
	}
	tlsCfg, err := c.tlsConfig()
	if err != nil {
		return err
	}

	var (
		creds    credentials.TransportCredentials
		recorder *handshakeRecorder
	)
	if tlsCfg != nil {
		recorder = &handshakeRecorder{TransportCredentials: credentials.NewTLS(tlsCfg), last: new(atomic.Pointer[error])}
		creds = recorder
	} else {
		creds = insecure.NewCredentials()
	}

	conn, err := grpc.NewClient(
		c.Host,
		grpc.WithTransportCredentials(creds),
		grpc.WithUnaryInterceptor(redactRPCErrors),
	)
	if err != nil {
		return fmt.Errorf("failed to connect to Dex at %s: %w", c.Host, err)
	}

	dialCtx, cancel := context.WithTimeout(ctx, time.Duration(PtrOr(c.TimeoutSeconds, 5))*time.Second)
	defer cancel()

	// Trigger connection establishment and wait up to the configured timeout
	// for the connection to become READY. This approximates the previous
	// grpc.DialContext(..., WithBlock) behavior and gives us a lightweight
	// health check without issuing any RPCs. A failed TLS handshake that retrying
	// cannot fix (no TLS on the other side, untrusted or mismatched certificate)
	// is reported at once.
	conn.Connect()
	for {
		state := conn.GetState()
		if state == connectivity.Ready {
			break
		}
		if state == connectivity.TransientFailure && recorder != nil {
			if herr := recorder.lastError(); herr != nil && isPermanentTLSError(herr) {
				return errors.Join(c.handshakeError(herr), conn.Close())
			}
		}
		if !conn.WaitForStateChange(dialCtx, state) {
			err := fmt.Errorf("timed out while connecting to Dex at %s", c.Host)
			if recorder != nil {
				if herr := recorder.lastError(); herr != nil {
					err = c.handshakeError(herr)
				}
			}
			return errors.Join(err, conn.Close())
		}
	}

	c.Client = api.NewDexClient(conn)

	// Ask for the server version once per provider configuration, for the
	// feature checks in Require. A failure does not fail the connection.
	versionCtx, cancelVersion := context.WithTimeout(ctx, time.Duration(PtrOr(c.TimeoutSeconds, 5))*time.Second)
	defer cancelVersion()
	info := fetchServerInfo(versionCtx, c.Client)
	c.Server = &info
	if warning := serverWarning(c.Host, info); warning != "" {
		p.GetLogger(ctx).Warning(warning)
	}

	return nil
}

// tlsConfig validates the connection options and returns the TLS configuration,
// or nil for a plaintext connection. TLS is the default: without options the
// system's root CAs verify Dex's certificate and its host name. Empty strings
// count as unset. Errors never include certificate or key contents.
func (c *DexConfig) tlsConfig() (*tls.Config, error) {
	caCert := PtrOr(c.CACertPEM, "")
	clientCert := PtrOr(c.ClientCertPEM, "")
	clientKey := PtrOr(c.ClientKeyPEM, "")
	skipVerify := PtrOr(c.InsecureSkipTLS, false)

	if PtrOr(c.Plaintext, false) {
		var conflicts []string
		for _, opt := range []struct {
			name string
			set  bool
		}{{"caCert", caCert != ""}, {"clientCert", clientCert != ""}, {"clientKey", clientKey != ""}, {"insecureSkipVerify", skipVerify}} {
			if opt.set {
				conflicts = append(conflicts, opt.name)
			}
		}
		if len(conflicts) > 0 {
			return nil, fmt.Errorf("plaintext: true cannot be combined with TLS options (%s): "+
				"remove them for an unencrypted connection, or remove plaintext to connect with TLS",
				strings.Join(conflicts, ", "))
		}
		return nil, nil
	}

	cfg := &tls.Config{MinVersion: tls.VersionTLS12}

	// A custom CA replaces the system roots rather than extending them, so
	// setting caCert never widens the set of trusted certificates.
	if caCert != "" {
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM([]byte(caCert)) {
			return nil, fmt.Errorf("caCert does not contain a PEM-encoded certificate")
		}
		cfg.RootCAs = roots
	}

	if clientCert != "" || clientKey != "" {
		if clientCert == "" || clientKey == "" {
			return nil, fmt.Errorf("both clientCert and clientKey must be provided (and non-empty) for mTLS")
		}
		pair, err := tls.X509KeyPair([]byte(clientCert), []byte(clientKey))
		if err != nil {
			return nil, fmt.Errorf("clientCert and clientKey cannot be used as a key pair: %w", err)
		}
		cfg.Certificates = []tls.Certificate{pair}
	}

	if skipVerify {
		cfg.InsecureSkipVerify = true
	}
	return cfg, nil
}

// handshakeError explains a failed TLS handshake with the fix for the common
// cases. Certificate contents are never included.
func (c *DexConfig) handshakeError(err error) error {
	var (
		notTLS     tls.RecordHeaderError
		unknownCA  x509.UnknownAuthorityError
		hostname   x509.HostnameError
		unverified *tls.CertificateVerificationError
	)
	customCA := PtrOr(c.CACertPEM, "") != ""
	switch {
	case errors.As(err, &notTLS):
		return fmt.Errorf("the Dex gRPC API at %s did not answer with TLS. The provider connects with TLS by default; "+
			"if this Dex serves its gRPC API without TLS (for example on localhost or inside a cluster), "+
			"set plaintext: true in the provider configuration", c.Host)
	case errors.As(err, &hostname):
		return fmt.Errorf("the TLS certificate of the Dex gRPC API at %s does not cover this address (%v); "+
			"connect using a host name the certificate is issued for", c.Host, hostname)
	case errors.As(err, &unknownCA) && customCA:
		return fmt.Errorf("the TLS certificate of the Dex gRPC API at %s is not issued by the CA in caCert", c.Host)
	// Without caCert the platform verifier decides; on macOS it reports an
	// untrusted certificate without a typed error, so any verification failure
	// gets the caCert hint.
	case (errors.As(err, &unknownCA) || errors.As(err, &unverified)) && !customCA:
		return fmt.Errorf("the TLS certificate of the Dex gRPC API at %s is not trusted by this system (%v); "+
			"if Dex uses a certificate from a private CA, set caCert to that CA's certificate", c.Host, err)
	case isRemoteTLSAlert(err) && PtrOr(c.ClientCertPEM, "") == "":
		return fmt.Errorf("the Dex gRPC API at %s rejected the TLS connection (%v); "+
			"if Dex requires client certificates, set clientCert and clientKey", c.Host, err)
	case isRemoteTLSAlert(err):
		return fmt.Errorf("the Dex gRPC API at %s rejected the TLS connection (%v); "+
			"check that Dex trusts the CA that issued clientCert", c.Host, err)
	default:
		return fmt.Errorf("TLS handshake with the Dex gRPC API at %s failed: %w", c.Host, err)
	}
}

// isPermanentTLSError reports whether retrying the handshake cannot succeed.
func isPermanentTLSError(err error) bool {
	var (
		notTLS     tls.RecordHeaderError
		unverified *tls.CertificateVerificationError
		unknownCA  x509.UnknownAuthorityError
		hostname   x509.HostnameError
		invalid    x509.CertificateInvalidError
	)
	return errors.As(err, &notTLS) || errors.As(err, &unverified) || errors.As(err, &unknownCA) ||
		errors.As(err, &hostname) || errors.As(err, &invalid) || isRemoteTLSAlert(err)
}

// isRemoteTLSAlert reports whether the server ended the connection with a TLS
// alert. Over TCP, crypto/tls reports it as a *net.OpError with Op "remote error"
// around an unexported alert type (tls.AlertError is used only with QUIC).
func isRemoteTLSAlert(err error) bool {
	var op *net.OpError
	return errors.As(err, &op) && op.Op == "remote error"
}

// handshakeRecorder keeps the last TLS handshake error, which gRPC otherwise
// only logs, so that Configure can explain why the connection never became ready.
// It also keeps TLS alerts read after the handshake: with TLS 1.3 the client
// finishes its handshake before the server checks the client certificate, so a
// missing or untrusted client certificate arrives as an alert on the first read.
type handshakeRecorder struct {
	credentials.TransportCredentials
	last *atomic.Pointer[error]
}

func (r *handshakeRecorder) ClientHandshake(ctx context.Context, authority string, raw net.Conn) (net.Conn, credentials.AuthInfo, error) {
	conn, info, err := r.TransportCredentials.ClientHandshake(ctx, authority, raw)
	if err != nil {
		r.last.Store(&err)
		return conn, info, err
	}
	return &alertRecordingConn{Conn: conn, last: r.last}, info, nil
}

type alertRecordingConn struct {
	net.Conn
	last *atomic.Pointer[error]
}

func (c *alertRecordingConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if err != nil && isRemoteTLSAlert(err) {
		c.last.Store(&err)
	}
	return n, err
}

func (r *handshakeRecorder) Clone() credentials.TransportCredentials {
	return &handshakeRecorder{TransportCredentials: r.TransportCredentials.Clone(), last: r.last}
}

func (r *handshakeRecorder) lastError() error {
	if err := r.last.Load(); err != nil {
		return *err
	}
	return nil
}

// WithPreviewSafeConfigure skips connecting to Dex while the provider
// configuration contains values that are not known yet, as in a preview where
// they come from other resources. infer would decode an unknown value as absent
// and connect with guessed options, for example without a caCert that is still
// being created. Check, Diff and preview Create/Update need no connection;
// operations that do need Dex fail with "dex client not configured".
func WithPreviewSafeConfigure(prov p.Provider) p.Provider {
	configure := prov.Configure
	if configure == nil {
		return prov
	}
	prov.Configure = func(ctx context.Context, req p.ConfigureRequest) error {
		if req.Args.HasComputed() {
			return nil
		}
		return configure(ctx, req)
	}
	return prov
}

// configValueKinds are the non-string provider settings. The engine can pass
// stack config to the provider as strings, for example "true" for a value set
// with pulumi config set --type bool during pulumi import, which infer
// rejects for a bool or int field. The import also stores these strings as the
// provider's inputs, while pulumi up stores the checked values.
var configValueKinds = map[string]func(string) (property.Value, bool){
	"plaintext":          parseBoolConfig,
	"insecureSkipVerify": parseBoolConfig,
	"timeoutSeconds": func(s string) (property.Value, bool) {
		n, err := strconv.Atoi(s)
		return property.New(float64(n)), err == nil
	},
}

func parseBoolConfig(s string) (property.Value, bool) {
	b, err := strconv.ParseBool(s)
	return property.New(b), err == nil
}

// typedConfigValues converts string-encoded booleans and integers in a
// provider configuration. A string that is not a valid value is left as it is,
// so the error still names the field.
func typedConfigValues(m property.Map) property.Map {
	for key, parse := range configValueKinds {
		if v, ok := m.GetOk(key); ok && v.IsString() {
			if typed, ok := parse(v.AsString()); ok {
				m = m.Set(key, typed)
			}
		}
	}
	return m
}

// WithTypedConfigValues makes string-encoded booleans and integers in the
// provider configuration equal to their typed values: in Configure, before
// infer decodes them, and in DiffConfig, where infer would otherwise see the
// strings an import stored as a change and replace every resource that uses
// the provider.
func WithTypedConfigValues(prov p.Provider) p.Provider {
	if configure := prov.Configure; configure != nil {
		prov.Configure = func(ctx context.Context, req p.ConfigureRequest) error {
			req.Args = typedConfigValues(req.Args)
			return configure(ctx, req)
		}
	}
	if diffConfig := prov.DiffConfig; diffConfig != nil {
		prov.DiffConfig = func(ctx context.Context, req p.DiffRequest) (p.DiffResponse, error) {
			req.State = typedConfigValues(req.State)
			req.OldInputs = typedConfigValues(req.OldInputs)
			req.Inputs = typedConfigValues(req.Inputs)
			return diffConfig(ctx, req)
		}
	}
	return prov
}

// PtrOr returns the value pointed to by p, or def if p is nil.
func PtrOr[T any](p *T, def T) T {
	if p == nil {
		return def
	}
	return *p
}
