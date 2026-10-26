package main

import (
	"context"
	"fmt"
	"time"

	m "github.com/zahidhasann88/meshlink/pkg/meshlink"
)

func main() {
	if e := run(); e != nil {
		panic(e)
	}
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	alice, _ := m.GenerateIdentity()
	bob, _ := m.GenerateIdentity()
	relayID, _ := m.GenerateIdentity()

	relay, e := m.NewRelayServer(m.RelayServerConfig{Identity: relayID, AllowedPeers: []string{alice.ID(), bob.ID()}})
	if e != nil {
		return e
	}
	if e = relay.Start(ctx); e != nil {
		return e
	}
	defer relay.Close()
	via := &m.RelayConfig{Address: relay.Address(), ID: relay.ID()}

	b, e := m.NewNode(m.Config{Identity: bob, AllowedPeers: []string{alice.ID()}, Relay: via})
	if e != nil {
		return e
	}
	b.Handle(func(_ context.Context, _ string, p []byte) ([]byte, error) {
		return []byte("bob got: " + string(p)), nil
	})
	if e = b.Start(ctx); e != nil {
		return e
	}
	defer b.Close()

	a, e := m.NewNode(m.Config{Identity: alice, AllowedPeers: []string{bob.ID()}, Relay: via, KnownPeers: []m.KnownPeer{{ID: bob.ID()}}})
	if e != nil {
		return e
	}
	if e = a.Start(ctx); e != nil {
		return e
	}
	defer a.Close()

	for len(a.Peers()) == 0 {
		select {
		case <-ctx.Done():
			return fmt.Errorf("not connected: %s", a.Diagnostics().LastError)
		case <-time.After(10 * time.Millisecond):
		}
	}
	reply, e := a.Request(ctx, bob.ID(), []byte("hello through the relay"))
	if e != nil {
		return e
	}
	fmt.Println(string(reply))
	fmt.Println("connected via relay:", a.Peers()[0].Relay == relay.ID())
	fmt.Println("bytes relayed:", relay.Stats().BytesRelayed)
	return nil
}
