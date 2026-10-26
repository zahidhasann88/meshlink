package engine

import (
	"context"
	"time"

	"github.com/zahidhasann88/meshlink/internal/discovery"
)

func (n *Node) Register(ctx context.Context, d discovery.Discovery, address string, lease time.Duration) error {
	return d.Register(ctx, discovery.Record{ID: n.ID(), Address: address}, lease)
}
func (n *Node) DiscoverAndConnect(ctx context.Context, d discovery.Discovery, id string) error {
	r, e := d.Lookup(ctx, id)
	if e != nil {
		return e
	}
	return n.Connect(ctx, r.Address, id)
}
