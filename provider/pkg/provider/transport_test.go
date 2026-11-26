package provider

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// =============================================================================
// Connection security: TLS by default, plaintext only on request.
//
// Certificates are generated per test run; no key material is checked in.
// =============================================================================

type testCA struct {
	cert    *x509.Certificate
	key     *ecdsa.PrivateKey
	certPEM string
}

func newTestCA(t *testing.T, name string) testCA {
	t.Helper()
	key := newKey(t)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return testCA{cert: cert, key: key, certPEM: pemBlock("CERTIFICATE", der)}
}

// issue returns a PEM certificate and key signed by the CA. Server certificates
// cover 127.0.0.1 unless dnsOnly is set.
func (ca testCA) issue(t *testing.T, cn string, usage x509.ExtKeyUsage, dnsOnly bool) (certPEM, keyPEM string) {
	t.Helper()
	key := newKey(t)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
		DNSNames:     []string{cn},
	}
	if usage == x509.ExtKeyUsageServerAuth && !dnsOnly {
		tmpl.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pemBlock("CERTIFICATE", der), pemBlock("EC PRIVATE KEY", keyDER)
}

func newKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func pemBlock(typ string, der []byte) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}))
}

// serveGRPC starts an empty gRPC server; the provider only needs the connection
// to become ready. A nil tlsCfg serves plaintext.
func serveGRPC(t *testing.T, tlsCfg *tls.Config) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var opts []grpc.ServerOption
	if tlsCfg != nil {
		opts = append(opts, grpc.Creds(credentials.NewTLS(tlsCfg)))
	}
	srv := grpc.NewServer(opts...)
	done := make(chan error, 1)
	go func() { done <- srv.Serve(lis) }()
	t.Cleanup(func() {
		srv.Stop()
		if err := <-done; err != nil {
			t.Errorf("test gRPC server: %v", err)
		}
	})
	return lis.Addr().String()
}

func serverTLS(t *testing.T, certPEM, keyPEM string) *tls.Config {
	t.Helper()
	pair, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		t.Fatal(err)
	}
	return &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}
}

type pki struct {
	ca, otherCA                  testCA
	serverCert, serverKey        string
	dnsOnlyCert, dnsOnlyKey      string
	clientCert, clientKey        string
	otherClientCert, otherClient string
}

func newPKI(t *testing.T) pki {
	t.Helper()
	ca, other := newTestCA(t, "test-ca"), newTestCA(t, "other-ca")
	var k pki
	k.ca, k.otherCA = ca, other
	k.serverCert, k.serverKey = ca.issue(t, "dex.test", x509.ExtKeyUsageServerAuth, false)
	k.dnsOnlyCert, k.dnsOnlyKey = ca.issue(t, "dex.test", x509.ExtKeyUsageServerAuth, true)
	k.clientCert, k.clientKey = ca.issue(t, "pulumi", x509.ExtKeyUsageClientAuth, false)
	k.otherClientCert, k.otherClient = ca.issue(t, "pulumi-2", x509.ExtKeyUsageClientAuth, false)
	return k
}

func ptr[T any](v T) *T { return &v }

func configure(t *testing.T, c DexConfig) error {
	t.Helper()
	if c.TimeoutSeconds == nil {
		c.TimeoutSeconds = ptr(2)
	}
	err := c.Configure(t.Context())
	if err != nil && strings.Contains(err.Error(), "-----BEGIN") {
		t.Errorf("error contains PEM data: %v", err)
	}
	return err
}

func wantErr(t *testing.T, err error, parts ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("Configure succeeded, want an error containing %q", parts)
	}
	for _, part := range parts {
		if !strings.Contains(err.Error(), part) {
			t.Errorf("error %q should contain %q", err, part)
		}
	}
}

func TestTransport_defaultIsVerifiedTLS(t *testing.T) {
	cfg, err := (&DexConfig{Host: "dex.example.com:5557"}).tlsConfig()
	if err != nil || cfg == nil {
		t.Fatalf("tlsConfig() = %v, %v; want TLS", cfg, err)
	}
	if cfg.RootCAs != nil || cfg.InsecureSkipVerify || cfg.MinVersion != tls.VersionTLS12 {
		t.Errorf("default TLS must use system roots, verify certificates and require TLS 1.2+: %+v", cfg)
	}

	k := newPKI(t)
	t.Run("against a plaintext Dex the error names plaintext: true", func(t *testing.T) {
		err := configure(t, DexConfig{Host: serveGRPC(t, nil)})
		wantErr(t, err, "did not answer with TLS", "plaintext: true")
	})
	t.Run("a certificate from an unknown CA is rejected", func(t *testing.T) {
		err := configure(t, DexConfig{Host: serveGRPC(t, serverTLS(t, k.serverCert, k.serverKey))})
		wantErr(t, err, "not trusted by this system", "set caCert")
	})
}

func TestTransport_caCert(t *testing.T) {
	k := newPKI(t)
	host := serveGRPC(t, serverTLS(t, k.serverCert, k.serverKey))

	if err := configure(t, DexConfig{Host: host, CACertPEM: ptr(k.ca.certPEM)}); err != nil {
		t.Errorf("trusted CA: %v", err)
	}
	wantErr(t, configure(t, DexConfig{Host: host, CACertPEM: ptr(k.otherCA.certPEM)}), "not issued by the CA in caCert")
	wantErr(t, configure(t, DexConfig{Host: host, CACertPEM: ptr("not a certificate")}), "caCert does not contain a PEM-encoded certificate")

	// The custom CA replaces the system roots instead of extending them.
	cfg, err := (&DexConfig{Host: host, CACertPEM: ptr(k.ca.certPEM)}).tlsConfig()
	if err != nil || cfg.RootCAs == nil {
		t.Fatalf("tlsConfig() = %v, %v", cfg, err)
	}
}

func TestTransport_hostnameMismatch(t *testing.T) {
	k := newPKI(t)
	host := serveGRPC(t, serverTLS(t, k.dnsOnlyCert, k.dnsOnlyKey)) // valid for dex.test only
	wantErr(t, configure(t, DexConfig{Host: host, CACertPEM: ptr(k.ca.certPEM)}), "does not cover this address")
}

func TestTransport_mutualTLS(t *testing.T) {
	k := newPKI(t)
	clientCAs := x509.NewCertPool()
	clientCAs.AddCert(k.ca.cert)
	srv := serverTLS(t, k.serverCert, k.serverKey)
	srv.ClientAuth, srv.ClientCAs = tls.RequireAndVerifyClientCert, clientCAs
	host := serveGRPC(t, srv)

	ok := DexConfig{Host: host, CACertPEM: ptr(k.ca.certPEM), ClientCertPEM: ptr(k.clientCert), ClientKeyPEM: ptr(k.clientKey)}
	if err := configure(t, ok); err != nil {
		t.Errorf("valid client identity: %v", err)
	}

	onlyCert := ok
	onlyCert.ClientKeyPEM = nil
	wantErr(t, configure(t, onlyCert), "both clientCert and clientKey")
	onlyKey := ok
	onlyKey.ClientCertPEM = ptr("")
	wantErr(t, configure(t, onlyKey), "both clientCert and clientKey")

	mismatched := ok
	mismatched.ClientKeyPEM = ptr(k.otherClient) // key of another certificate
	wantErr(t, configure(t, mismatched), "clientCert and clientKey cannot be used as a key pair")

	noIdentity := DexConfig{Host: host, CACertPEM: ptr(k.ca.certPEM)}
	wantErr(t, configure(t, noIdentity), "rejected the TLS connection", "set clientCert and clientKey")

	// A client certificate from a CA the server does not trust.
	untrusted := ok
	intruderCert, intruderKey := k.otherCA.issue(t, "intruder", x509.ExtKeyUsageClientAuth, false)
	untrusted.ClientCertPEM, untrusted.ClientKeyPEM = ptr(intruderCert), ptr(intruderKey)
	wantErr(t, configure(t, untrusted), "rejected the TLS connection", "check that Dex trusts the CA that issued clientCert")
}

func TestTransport_insecureSkipVerify(t *testing.T) {
	k := newPKI(t)
	// TLS without certificate checks: an untrusted certificate is accepted...
	if err := configure(t, DexConfig{Host: serveGRPC(t, serverTLS(t, k.serverCert, k.serverKey)), InsecureSkipTLS: ptr(true)}); err != nil {
		t.Errorf("untrusted certificate with insecureSkipVerify: %v", err)
	}
	// ...but it is never plaintext.
	wantErr(t, configure(t, DexConfig{Host: serveGRPC(t, nil), InsecureSkipTLS: ptr(true)}), "did not answer with TLS")
}

func TestTransport_plaintext(t *testing.T) {
	if err := configure(t, DexConfig{Host: serveGRPC(t, nil), Plaintext: ptr(true)}); err != nil {
		t.Errorf("plaintext against plaintext Dex: %v", err)
	}
	// Empty TLS strings count as unset, as before.
	if err := configure(t, DexConfig{Host: serveGRPC(t, nil), Plaintext: ptr(true), CACertPEM: ptr(""), ClientCertPEM: ptr(""), ClientKeyPEM: ptr("")}); err != nil {
		t.Errorf("plaintext with empty TLS fields: %v", err)
	}
	// No automatic fallback in either direction.
	k := newPKI(t)
	if err := configure(t, DexConfig{Host: serveGRPC(t, serverTLS(t, k.serverCert, k.serverKey)), Plaintext: ptr(true), TimeoutSeconds: ptr(1)}); err == nil {
		t.Error("plaintext connected to a TLS-only Dex")
	}
	if err := configure(t, DexConfig{Host: serveGRPC(t, nil), Plaintext: ptr(false)}); err == nil {
		t.Error("plaintext: false connected without TLS")
	}
}

func TestTransport_plaintextConflictsWithTLSOptions(t *testing.T) {
	for _, tc := range []struct {
		cfg  DexConfig
		want string
	}{
		{DexConfig{CACertPEM: ptr("x")}, "(caCert)"},
		{DexConfig{ClientCertPEM: ptr("x"), ClientKeyPEM: ptr("y")}, "(clientCert, clientKey)"},
		{DexConfig{InsecureSkipTLS: ptr(true)}, "(insecureSkipVerify)"},
		{DexConfig{CACertPEM: ptr("x"), InsecureSkipTLS: ptr(true)}, "(caCert, insecureSkipVerify)"},
	} {
		tc.cfg.Host, tc.cfg.Plaintext = "127.0.0.1:1", ptr(true)
		wantErr(t, configure(t, tc.cfg), "plaintext: true cannot be combined with TLS options "+tc.want)
	}
	// insecureSkipVerify: false enables nothing, so it does not conflict.
	if _, err := (&DexConfig{Host: "h:1", Plaintext: ptr(true), InsecureSkipTLS: ptr(false)}).tlsConfig(); err != nil {
		t.Errorf("plaintext with insecureSkipVerify: false: %v", err)
	}
}
