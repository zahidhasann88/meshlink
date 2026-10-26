package integration_test

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	m "github.com/zahidhasann88/meshlink/pkg/meshlink"
)

var fastRetry = m.RetryPolicy{Attempts: 3, InitialDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond}

func connectedTo(n *m.Node, id string) *m.PeerInfo {
	for _, p := range n.Peers() {
		if p.ID == id {
			return &p
		}
	}
	return nil
}

func gaveUp(n *m.Node) bool {
	d := n.Diagnostics()
	return len(d.MaintainedPeers) == 0 && strings.Contains(d.LastError, "stopped reconnecting")
}

func TestMaintainReconnectsAfterDisconnect(t *testing.T) {
	ns := nodes(t, 2, nil)
	a, b := ns[0], ns[1]
	stop, e := a.Maintain(b.Address(), b.ID(), m.RetryPolicy{Attempts: 5, InitialDelay: 5 * time.Millisecond, MaxDelay: 50 * time.Millisecond})
	if e != nil {
		t.Fatal(e)
	}
	if _, e := a.Maintain(b.Address(), b.ID(), fastRetry); e == nil {
		t.Fatal("maintained the same peer twice")
	}
	wait(t, func() bool { return connectedTo(a, b.ID()) != nil && connectedTo(b, a.ID()) != nil })
	first := connectedTo(a, b.ID()).ConnectedAt

	if e := b.Disconnect(a.ID()); e != nil {
		t.Fatal(e)
	}
	wait(t, func() bool { p := connectedTo(a, b.ID()); return p != nil && p.ConnectedAt.After(first) })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if p, e := a.Request(ctx, b.ID(), []byte("again")); e != nil || string(p) != "again" {
		t.Fatalf("%s %v", p, e)
	}
	if d := a.Diagnostics(); len(d.MaintainedPeers) != 1 || d.ReconnectAttempts == 0 {
		t.Fatalf("bad diagnostics: %+v", d)
	}

	stop()
	wait(t, func() bool { return len(a.Diagnostics().MaintainedPeers) == 0 })
	if connectedTo(a, b.ID()) == nil {
		t.Fatal("stop disconnected the peer")
	}
}

func TestMaintainSurvivesPeerRestart(t *testing.T) {
	ids := identities(t, 2)
	start := func(i int, address string, known []m.KnownPeer) *m.Node {
		t.Helper()
		n, e := m.NewNode(m.Config{Identity: ids[i], ListenAddress: address, AllowedPeers: []string{ids[1-i].ID()}, KnownPeers: known, Reconnect: m.RetryPolicy{Attempts: 50, InitialDelay: 5 * time.Millisecond, MaxDelay: 50 * time.Millisecond}})
		if e != nil {
			t.Fatal(e)
		}
		if e = n.Start(context.Background()); e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { n.Close() })
		return n
	}
	b := start(1, "127.0.0.1:0", nil)
	address := b.Address()
	a := start(0, "127.0.0.1:0", []m.KnownPeer{{ID: b.ID(), Address: address}})
	wait(t, func() bool { return connectedTo(a, b.ID()) != nil })

	b.Close()
	wait(t, func() bool { return connectedTo(a, b.ID()) == nil })
	b = start(1, address, nil)
	wait(t, func() bool { return connectedTo(a, b.ID()) != nil })
}

func TestMaintainGivesUpWhenPeerRejects(t *testing.T) {
	ids := identities(t, 2)
	b, _ := m.NewNode(m.Config{Identity: ids[1]})
	a, _ := m.NewNode(m.Config{Identity: ids[0], AllowedPeers: []string{ids[1].ID()}})
	for _, n := range []*m.Node{a, b} {
		if e := n.Start(context.Background()); e != nil {
			t.Fatal(e)
		}
		defer n.Close()
	}
	if _, e := a.Maintain(b.Address(), b.ID(), fastRetry); e != nil {
		t.Fatal(e)
	}
	wait(t, func() bool { return gaveUp(a) })
	if r := a.Diagnostics().ReconnectAttempts; r > uint64(fastRetry.Attempts) {
		t.Fatalf("%d reconnect attempts exceed the policy", r)
	}
}

func TestMaintainGivesUpWhenUnreachable(t *testing.T) {
	ns := nodes(t, 2, nil)
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	closed := l.Addr().String()
	l.Close()
	if _, e := ns[0].Maintain(closed, ns[1].ID(), fastRetry); e != nil {
		t.Fatal(e)
	}
	wait(t, func() bool { return gaveUp(ns[0]) })
	if r := ns[0].Diagnostics().ReconnectAttempts; r != uint64(fastRetry.Attempts-1) {
		t.Fatalf("expected %d retries, got %d", fastRetry.Attempts-1, r)
	}
}

func TestKnownPeersValidation(t *testing.T) {
	ids := identities(t, 2)
	other := ids[1].ID()
	for name, c := range map[string]m.Config{
		"not allowed":    {KnownPeers: []m.KnownPeer{{ID: other, Address: "127.0.0.1:1"}}},
		"duplicate":      {AllowedPeers: []string{other}, KnownPeers: []m.KnownPeer{{ID: other, Address: "a:1"}, {ID: other, Address: "b:1"}}},
		"bad reconnect":  {Reconnect: m.RetryPolicy{Attempts: 1000}},
		"inverted delay": {Reconnect: m.RetryPolicy{InitialDelay: time.Minute, MaxDelay: time.Second}},
	} {
		if e := m.ValidateConfig(c); e == nil {
			t.Errorf("%s: accepted invalid config", name)
		}
	}
	if e := m.ValidateConfig(m.Config{AllowedPeers: []string{other}, KnownPeers: []m.KnownPeer{{ID: other, Address: "127.0.0.1:7002"}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := m.NewNode(m.Config{AllowedPeers: []string{other}, KnownPeers: []m.KnownPeer{{ID: other}}}); e == nil {
		t.Fatal("accepted a known peer without address, registry or relay")
	}
}
