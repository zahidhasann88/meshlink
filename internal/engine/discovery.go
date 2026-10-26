package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/zahidhasann88/meshlink/internal/discovery"
)

func (n *Node) Register(ctx context.Context, d discovery.Discovery, address string, lease time.Duration) error {
	return d.Register(ctx, discovery.Sign(n.cfg.Identity, address, time.Now()), lease)
}

func (n *Node) Unregister(ctx context.Context, d discovery.Discovery) error {
	return d.Unregister(ctx, discovery.Sign(n.cfg.Identity, "", time.Now()))
}

func (n *Node) DiscoverAndConnect(ctx context.Context, d discovery.Discovery, id string) error {
	address, e := n.lookup(ctx, d, id)
	if e != nil {
		return e
	}
	return n.Connect(ctx, address, id)
}

func (n *Node) lookup(ctx context.Context, d discovery.Discovery, id string) (string, error) {
	r, e := d.Lookup(ctx, id)
	if e != nil {
		return "", e
	}
	if e = r.CheckLookup(id, time.Now()); e != nil {
		return "", fmt.Errorf("registry record for %s: %w", id, e)
	}
	return r.Address, nil
}
