package integration_test

import (
	"context"
	"testing"
	"time"

	m "github.com/zahidhasann88/meshlink/pkg/meshlink"
)

func TestKeepaliveHoldsIdleConnection(t *testing.T) {
	ns := nodes(t, 2, func(_ int, c *m.Config) { c.IdleTimeout = 300 * time.Millisecond })
	a, b := ns[0], ns[1]
	connect(t, a, b)
	time.Sleep(1500 * time.Millisecond)
	p := connectedTo(a, b.ID())
	if p == nil || connectedTo(b, a.ID()) == nil {
		t.Fatal("idle v2 connection was closed despite keepalive")
	}
	if p.Protocol != 2 {
		t.Fatalf("expected protocol 2, got %+v", p)
	}
}

func TestProtocolV1Compatibility(t *testing.T) {
	ns := nodes(t, 2, func(i int, c *m.Config) {
		c.IdleTimeout = 300 * time.Millisecond
		if i == 0 {
			c.Transport = m.TCPTransport{Protocols: []string{"meshlink/1"}}
		}
	})
	old, current := ns[0], ns[1]
	for _, pair := range [][2]*m.Node{{old, current}, {current, old}} {
		connect(t, pair[0], pair[1])
		wait(t, func() bool { return connectedTo(pair[1], pair[0].ID()) != nil })
		if p := connectedTo(pair[0], pair[1].ID()); p.Protocol != 1 {
			t.Fatalf("expected protocol 1, got %d", p.Protocol)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		got, e := pair[0].Request(ctx, pair[1].ID(), []byte("v1"))
		cancel()
		if e != nil || string(got) != "v1" {
			t.Fatal(string(got), e)
		}
		wait(t, func() bool { return connectedTo(pair[0], pair[1].ID()) == nil })
	}
}
