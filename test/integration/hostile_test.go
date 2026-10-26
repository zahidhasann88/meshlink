package integration_test

import (
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"

	m "github.com/zahidhasann88/meshlink/pkg/meshlink"
)

func hostile(t *testing.T) (*m.Node, m.Conn) {
	t.Helper()
	ids := identities(t, 2)
	n, e := m.NewNode(m.Config{Identity: ids[0], AllowedPeers: []string{ids[1].ID()}, MaxFrameSize: 1024, IdleTimeout: 100 * time.Millisecond, ConnectTimeout: 100 * time.Millisecond})
	if e != nil {
		t.Fatal(e)
	}
	if e = n.Start(context.Background()); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { n.Close() })
	tr := m.TCPTransport{}
	raw, e := tr.Dial(context.Background(), n.Address())
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c, e := tr.Authenticate(ctx, raw, m.Credentials{Identity: ids[1], AllowedPeers: map[string]bool{ids[0].ID(): true}}, n.ID())
	if e != nil {
		raw.Close()
		t.Fatal(e)
	}
	t.Cleanup(func() { c.Close() })
	wait(t, func() bool { return len(n.Peers()) == 1 })
	return n, c
}
func TestHostileFramesAndIdleTimeout(t *testing.T) {
	for _, tc := range []struct {
		name     string
		body     []byte
		declared uint32
	}{{"oversize", nil, 0xffffffff}, {"zero", nil, 0}, {"malformed", []byte(`{bad`), 4}, {"version", []byte(`{"version":9,"type":"data","id":"00000000000000000000000000000000"}`), 0}, {"unknown", []byte(`{"version":1,"type":"alien","id":"00000000000000000000000000000000"}`), 0}, {"partial", []byte{1}, 100}} {
		t.Run(tc.name, func(t *testing.T) {
			n, c := hostile(t)
			var h [4]byte
			size := tc.declared
			if size == 0 && len(tc.body) > 0 {
				size = uint32(len(tc.body))
			}
			binary.BigEndian.PutUint32(h[:], size)
			c.SetWriteDeadline(time.Now().Add(time.Second))
			c.Write(h[:])
			c.Write(tc.body)
			wait(t, func() bool { return len(n.Peers()) == 0 })
		})
	}
	t.Run("idle", func(t *testing.T) { n, _ := hostile(t); wait(t, func() bool { return len(n.Peers()) == 0 }) })
}
func TestBoundedHandshakeFlood(t *testing.T) {
	n, e := m.NewNode(m.Config{MaxPeers: 2, MaxHandshakes: 1, ConnectTimeout: 100 * time.Millisecond})
	if e != nil {
		t.Fatal(e)
	}
	if e = n.Start(context.Background()); e != nil {
		t.Fatal(e)
	}
	defer n.Close()
	connections := []net.Conn{}
	defer func() {
		for _, c := range connections {
			c.Close()
		}
	}()
	for i := 0; i < 10; i++ {
		c, e := net.DialTimeout("tcp", n.Address(), time.Second)
		if e != nil {
			t.Fatal(e)
		}
		connections = append(connections, c)
	}
	wait(t, func() bool { return n.Diagnostics().RejectedConnections >= 9 })
	if len(n.Peers()) != 0 {
		t.Fatal("unauthenticated connections counted as peers")
	}
}
func TestSlowHandlerIsBounded(t *testing.T) {
	ns := nodes(t, 2, func(i int, c *m.Config) {
		if i == 1 {
			c.Workers = 1
			c.HandlerQueue = 1
		}
	})
	a, b := ns[0], ns[1]
	entered := make(chan struct{}, 1)
	b.Handle(func(ctx context.Context, _ string, _ []byte) ([]byte, error) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return nil, ctx.Err()
	})
	connect(t, a, b)
	a.Send(context.Background(), b.ID(), []byte("block"))
	<-entered
	for i := 0; i < 10; i++ {
		_ = a.Send(context.Background(), b.ID(), []byte("bounded"))
	}
	wait(t, func() bool { return len(b.Peers()) == 0 })
}
func TestRetryAndPermanentAuthFailure(t *testing.T) {
	ns := nodes(t, 2, nil)
	a, b := ns[0], ns[1]
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if e := a.ConnectWithRetry(ctx, "127.0.0.1:0", b.ID(), m.RetryPolicy{Attempts: 3, InitialDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond}); e == nil {
		t.Fatal("unexpected connection")
	}
	if a.Diagnostics().ReconnectAttempts != 2 {
		t.Fatal(a.Diagnostics())
	}
	before := a.Diagnostics().ReconnectAttempts
	other := identities(t, 1)[0]
	if e := a.ConnectWithRetry(ctx, b.Address(), other.ID(), m.RetryPolicy{Attempts: 3, InitialDelay: time.Millisecond, MaxDelay: time.Millisecond}); e == nil {
		t.Fatal("unauthorized")
	}
	if a.Diagnostics().ReconnectAttempts != before {
		t.Fatal("retried permanent error")
	}
}
