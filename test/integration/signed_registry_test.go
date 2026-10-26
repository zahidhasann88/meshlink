package integration_test

import (
	"context"
	"testing"
	"time"

	m "github.com/zahidhasann88/meshlink/pkg/meshlink"
)

type lyingRegistry struct {
	m.Discovery
	record m.PeerRecord
}

func (l lyingRegistry) Lookup(context.Context, string) (m.PeerRecord, error) { return l.record, nil }

func TestEngineRejectsForgedRegistryRecords(t *testing.T) {
	ns := nodes(t, 3, nil)
	store, _ := m.NewMemoryRegistry(10)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if e := ns[1].Register(ctx, store, ns[1].Address(), time.Minute); e != nil {
		t.Fatal(e)
	}
	genuine, _ := store.Lookup(ctx, ns[1].ID())
	if e := ns[2].Register(ctx, store, ns[2].Address(), time.Minute); e != nil {
		t.Fatal(e)
	}
	forged, _ := store.Lookup(ctx, ns[2].ID())
	forged.ID = ns[1].ID()
	if e := ns[0].DiscoverAndConnect(ctx, lyingRegistry{store, forged}, ns[1].ID()); e == nil {
		t.Fatal("engine connected using a record signed by another peer")
	}
	if e := ns[0].DiscoverAndConnect(ctx, lyingRegistry{store, genuine}, ns[1].ID()); e != nil {
		t.Fatal("engine rejected a genuine record:", e)
	}
	if e := ns[1].Unregister(ctx, store); e != nil {
		t.Fatal(e)
	}
	if _, e := store.Lookup(ctx, ns[1].ID()); e == nil {
		t.Fatal("signed removal did not remove the entry")
	}
}
