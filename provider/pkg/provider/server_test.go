package provider

import (
	"context"
	"net"
	"strings"
	"testing"

	api "github.com/dexidp/dex/api/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// =============================================================================
// Server version and feature checks
//
// Dex v2.45.1 reports API 3 and v2.46.1 API 4 (verified against both release
// builds); the API version decides which features a server supports.
// =============================================================================

type versionDex struct {
	api.UnimplementedDexServer
	server string
	api    int32
	err    error
}

func (v *versionDex) GetVersion(context.Context, *api.VersionReq) (*api.VersionResp, error) {
	if v.err != nil {
		return nil, v.err
	}
	return &api.VersionResp{Server: v.server, Api: v.api}, nil
}

// configureAgainst connects a plaintext DexConfig to a fake Dex.
func configureAgainst(t *testing.T, dex api.DexServer) *DexConfig {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer()
	api.RegisterDexServer(srv, dex)
	done := make(chan error, 1)
	go func() { done <- srv.Serve(lis) }()
	t.Cleanup(func() {
		srv.Stop()
		if err := <-done; err != nil {
			t.Errorf("fake Dex: %v", err)
		}
	})
	c := &DexConfig{Host: lis.Addr().String(), Plaintext: ptr(true), TimeoutSeconds: ptr(2)}
	if err := c.Configure(t.Context()); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	return c
}

func TestServerVersion_featureGate(t *testing.T) {
	for _, tc := range []struct {
		name        string
		dex         *versionDex
		wantGroups  string // "" = supported, else a substring of the refusal
		wantWarning string // "" = no warning
	}{
		{"v2.45.1", &versionDex{server: "v2.45.1", api: 3}, "needs Dex v2.46 or later (API 4); the Dex server at", ""},
		{"v2.46.1", &versionDex{server: "v2.46.1", api: 4}, "", ""},
		{"source build of v2.46", &versionDex{server: "DEV", api: 4}, "", ""},
		{"older than supported", &versionDex{server: "v2.44.0", api: 2}, "reports version \"v2.44.0\" (API 2)", "supports Dex v2.45 and v2.46"},
		{"newer than tested", &versionDex{server: "v2.47.0", api: 5}, "", "supports Dex v2.45 and v2.46"},
		{"no version reported", &versionDex{}, "reports version unknown (API 0)", "reports version unknown"},
		{"permission denied", &versionDex{err: status.Error(codes.PermissionDenied, "not allowed")}, "GetVersion failed", "could not determine the version"},
		{"GetVersion unimplemented", &versionDex{err: status.Error(codes.Unimplemented, "unknown method")}, "GetVersion failed", "could not determine the version"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := configureAgainst(t, tc.dex)
			err := c.Require(LocalUserGroups)
			switch {
			case tc.wantGroups == "" && err != nil:
				t.Errorf("Require: %v, want supported", err)
			case tc.wantGroups != "" && (err == nil || !strings.Contains(err.Error(), tc.wantGroups)):
				t.Errorf("Require = %v, want an error containing %q", err, tc.wantGroups)
			}
			if got := serverWarning(c.Host, *c.Server); (tc.wantWarning == "") != (got == "") || !strings.Contains(got, tc.wantWarning) {
				t.Errorf("warning = %q, want %q", got, tc.wantWarning)
			}
		})
	}
}

// A failed GetVersion keeps its gRPC status, so permission and network errors
// stay distinguishable from missing support.
func TestServerVersion_errorKeepsStatus(t *testing.T) {
	c := configureAgainst(t, &versionDex{err: status.Error(codes.PermissionDenied, "not allowed")})
	if got := status.Code(c.Require(LocalUserGroups)); got != codes.PermissionDenied {
		t.Errorf("status code of the refusal = %v, want PermissionDenied", got)
	}
}

func TestServerVersion_unknownBeforeConfigure(t *testing.T) {
	err := (&DexConfig{Host: "dex:5557"}).Require(LocalUserGroups)
	if err == nil || !strings.Contains(err.Error(), "not known yet") {
		t.Errorf("Require without a connection = %v, want an unknown-version refusal", err)
	}
}
