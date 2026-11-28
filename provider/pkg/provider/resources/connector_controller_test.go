package resources

import (
	"fmt"
	"sync"
	"testing"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/integration"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

// Two provider instances, each configured against its own Dex, run connector
// lifecycles in parallel with the same IDs. Each Dex must see only its own
// instance's writes: the controller keeps no state between calls and nothing
// is shared between provider instances. Run with -race.
func TestConnectorController_providerInstancesAreIsolated(t *testing.T) {
	const rounds = 10
	type instance struct {
		dex *fakeDex
		s   integration.Server
	}
	var instances [2]instance
	for i := range instances {
		dex := newFakeDex()
		instances[i] = instance{dex: dex, s: contractDex{addr: serveFakeDex(t, dex)}.provider(t)}
	}
	var contracts []connectorContract
	for _, c := range connectorContracts() {
		if c.token == tokenConnector || c.token == tokenGitHubConnector {
			contracts = append(contracts, c)
		}
	}

	var wg sync.WaitGroup
	for i, in := range instances {
		for _, c := range contracts {
			for round := range rounds {
				wg.Go(func() {
					id := fmt.Sprintf("%s-%d", c.short(), round)
					name := fmt.Sprintf("instance %d", i)
					if err := isolatedLifecycle(in.s, in.dex, c, id, name); err != nil {
						t.Errorf("instance %d, %s: %v", i, id, err)
					}
				})
			}
		}
	}
	wg.Wait()

	for i, in := range instances {
		in.dex.mu.Lock()
		writes, left := in.dex.writes, len(in.dex.connectors)
		in.dex.mu.Unlock()
		if want := len(contracts) * rounds * 3; writes != want || left != 0 {
			t.Errorf("Dex of instance %d: %d writes and %d connectors left, want %d writes and none left", i, writes, left, want)
		}
	}
}

// isolatedLifecycle creates, reads, renames and deletes one connector, and
// checks after each write that Dex holds this instance's values.
func isolatedLifecycle(s integration.Server, dex *fakeDex, c connectorContract, id, name string) error {
	urn := testURN(c.token)
	stored := func() string {
		dex.mu.Lock()
		defer dex.mu.Unlock()
		if conn, ok := dex.connectors[id]; ok {
			return conn.Name
		}
		return ""
	}
	program := with(c.inputs(id), map[string]property.Value{"name": str(name)})
	checked, err := s.Check(p.CheckRequest{Urn: urn, Inputs: property.NewMap(program)})
	if err != nil || len(checked.Failures) > 0 {
		return fmt.Errorf("check: %v %v", err, checked.Failures)
	}
	created, err := s.Create(p.CreateRequest{Urn: urn, Properties: checked.Inputs})
	if err != nil {
		return fmt.Errorf("create: %w", err)
	}
	if got := stored(); got != name {
		return fmt.Errorf("after create Dex has name %q, want %q", got, name)
	}
	read, err := s.Read(p.ReadRequest{ID: id, Urn: urn, Properties: created.Properties, Inputs: checked.Inputs})
	if err != nil || read.ID != id || read.Inputs.Get("name").AsString() != name {
		return fmt.Errorf("read: id=%q name=%v err=%v", read.ID, read.Inputs.Get("name"), err)
	}
	renamed := name + " renamed"
	next, err := s.Check(p.CheckRequest{Urn: urn, State: read.Inputs, Inputs: property.NewMap(with(program, map[string]property.Value{"name": str(renamed)}))})
	if err != nil || len(next.Failures) > 0 {
		return fmt.Errorf("check rename: %v %v", err, next.Failures)
	}
	updated, err := s.Update(p.UpdateRequest{ID: id, Urn: urn, State: read.Properties, OldInputs: read.Inputs, Inputs: next.Inputs})
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}
	if got := stored(); got != renamed {
		return fmt.Errorf("after update Dex has name %q, want %q", got, renamed)
	}
	if err := s.Delete(p.DeleteRequest{ID: id, Urn: urn, Properties: updated.Properties}); err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	if got := stored(); got != "" {
		return fmt.Errorf("after delete Dex still has %q", got)
	}
	return nil
}
