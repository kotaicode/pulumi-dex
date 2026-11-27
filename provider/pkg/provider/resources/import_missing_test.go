package resources

import (
	"strings"
	"testing"

	p "github.com/pulumi/pulumi-go-provider"
)

// pulumi import of an ID that Dex does not have must fail with a not-found
// error naming the object, instead of importing an empty resource. A refresh
// of an object deleted in Dex must still report it gone, so that Pulumi
// removes it from the state.
func TestRead_missingObject(t *testing.T) {
	for _, tc := range adoptionCases() {
		t.Run(tc.token, func(t *testing.T) {
			urn := testURN(tc.token)

			_, err := newProviderAgainst(t, newFakeDex()).Read(p.ReadRequest{ID: tc.id, Urn: urn})
			if err == nil || !strings.Contains(err.Error(), "not found in Dex") || !strings.Contains(err.Error(), `"`+tc.id+`"`) {
				t.Errorf("import of a missing object = %v, want a not-found error naming %q", err, tc.id)
			}

			dex := newFakeDex()
			_, created, old := createFor(t, dex, tc.token, tc.inputs)
			dex.mu.Lock()
			delete(dex.clients, created.ID)
			delete(dex.connectors, created.ID)
			delete(dex.passwords, created.ID)
			dex.mu.Unlock()
			resp, err := newProviderAgainst(t, dex).Read(p.ReadRequest{ID: created.ID, Urn: urn, Properties: created.Properties, Inputs: old})
			if err != nil || resp.ID != "" {
				t.Errorf("refresh of an object deleted in Dex: id=%q err=%v, want it reported gone", resp.ID, err)
			}
		})
	}
}
