package integration_test

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"net/http/httptest"

	"github.com/zahidhasann88/meshlink/internal/discovery"
	"github.com/zahidhasann88/meshlink/internal/fsperm"
	m "github.com/zahidhasann88/meshlink/pkg/meshlink"
)

func freeAddress(t *testing.T) string {
	t.Helper()
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	return l.Addr().String()
}

func startRelay(t *testing.T, edit func(*m.RelayServerConfig), clients ...*m.Identity) *m.RelayServer {
	t.Helper()
	id, _ := m.GenerateIdentity()
	c := m.RelayServerConfig{Identity: id}
	for _, client := range clients {
		c.AllowedPeers = append(c.AllowedPeers, client.ID())
	}
	if edit != nil {
		edit(&c)
	}
	r, e := m.NewRelayServer(c)
	if e != nil {
		t.Fatal(e)
	}
	if e = r.Start(context.Background()); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

func startNode(t *testing.T, c m.Config) *m.Node {
	t.Helper()
	n, e := m.NewNode(c)
	if e != nil {
		t.Fatal(e)
	}
	n.Handle(func(_ context.Context, _ string, p []byte) ([]byte, error) { return p, nil })
	if e = n.Start(context.Background()); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { n.Close() })
	return n
}

var quickReconnect = m.RetryPolicy{Attempts: 50, InitialDelay: 5 * time.Millisecond, MaxDelay: 50 * time.Millisecond}

func TestRelayEndToEnd(t *testing.T) {
	ids := identities(t, 2)
	r := startRelay(t, nil, ids...)
	via := &m.RelayConfig{Address: r.Address(), ID: r.ID()}
	b := startNode(t, m.Config{Identity: ids[1], AllowedPeers: []string{ids[0].ID()}, Relay: via, Reconnect: quickReconnect})
	wait(t, func() bool { return b.Diagnostics().Relay == "reserved" })
	a := startNode(t, m.Config{Identity: ids[0], AllowedPeers: []string{ids[1].ID()}, Relay: via, Reconnect: quickReconnect, KnownPeers: []m.KnownPeer{{ID: b.ID()}}})
	wait(t, func() bool { p := connectedTo(a, b.ID()); return p != nil && connectedTo(b, a.ID()) != nil })
	if p := connectedTo(a, b.ID()); p.Relay != r.ID() {
		t.Fatalf("expected relayed session, got %+v", p)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	large := bytes.Repeat([]byte("relay"), 120<<10)
	for _, pair := range [][2]*m.Node{{a, b}, {b, a}} {
		got, e := pair[0].Request(ctx, pair[1].ID(), large)
		if e != nil || !bytes.Equal(got, large) {
			t.Fatalf("relayed request failed: %v", e)
		}
	}
	if s := r.Stats(); s.Circuits != 1 || s.Reservations < 1 || s.BytesRelayed < uint64(4*len(large)) {
		t.Fatalf("bad relay stats: %+v", s)
	}
	r.Close()
	wait(t, func() bool { return connectedTo(a, b.ID()) == nil })
}

func TestRelayFallbackAfterDirectFailure(t *testing.T) {
	ids := identities(t, 2)
	r := startRelay(t, nil, ids...)
	via := &m.RelayConfig{Address: r.Address(), ID: r.ID()}
	b := startNode(t, m.Config{Identity: ids[1], AllowedPeers: []string{ids[0].ID()}, Relay: via, Reconnect: quickReconnect})
	wait(t, func() bool { return b.Diagnostics().Relay == "reserved" })
	unreachable := freeAddress(t)
	a := startNode(t, m.Config{Identity: ids[0], AllowedPeers: []string{ids[1].ID()}, Relay: via, Reconnect: quickReconnect, KnownPeers: []m.KnownPeer{{ID: b.ID(), Address: unreachable}}})
	wait(t, func() bool { p := connectedTo(a, b.ID()); return p != nil && p.Relay == r.ID() })
}

func TestRelayRejectsUnknownClients(t *testing.T) {
	ids := identities(t, 3)
	r := startRelay(t, nil, ids[0], ids[1])
	via := &m.RelayConfig{Address: r.Address(), ID: r.ID()}
	b := startNode(t, m.Config{Identity: ids[1], AllowedPeers: []string{ids[0].ID(), ids[2].ID()}, Relay: via, Reconnect: quickReconnect})
	wait(t, func() bool { return b.Diagnostics().Relay == "reserved" })
	stranger := startNode(t, m.Config{Identity: ids[2], AllowedPeers: []string{ids[1].ID()}, Relay: via, Reconnect: quickReconnect})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if e := stranger.ConnectViaRelay(ctx, b.ID()); e == nil {
		t.Fatal("relay served a client outside its allowlist")
	}
	if stranger.Diagnostics().Relay == "reserved" || r.Stats().Rejected == 0 {
		t.Fatalf("unknown client was not rejected: %+v", r.Stats())
	}
	if connectedTo(b, stranger.ID()) != nil {
		t.Fatal("stranger reached b through the relay")
	}
}

func TestRelayTargetStillAuthenticates(t *testing.T) {
	ids := identities(t, 2)
	r := startRelay(t, nil, ids...)
	via := &m.RelayConfig{Address: r.Address(), ID: r.ID()}
	other, _ := m.GenerateIdentity()
	b := startNode(t, m.Config{Identity: ids[1], AllowedPeers: []string{other.ID()}, Relay: via, Reconnect: quickReconnect})
	wait(t, func() bool { return b.Diagnostics().Relay == "reserved" })
	a := startNode(t, m.Config{Identity: ids[0], AllowedPeers: []string{ids[1].ID()}, Relay: via, ConnectTimeout: time.Second})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if e := a.ConnectViaRelay(ctx, b.ID()); e == nil {
		time.Sleep(100 * time.Millisecond)
		if connectedTo(b, a.ID()) != nil {
			t.Fatal("b accepted a peer it does not allow")
		}
	}
}

func TestRelayCircuitLimit(t *testing.T) {
	ids := identities(t, 3)
	r := startRelay(t, func(c *m.RelayServerConfig) { c.MaxCircuitsPerPeer = 1 }, ids...)
	via := &m.RelayConfig{Address: r.Address(), ID: r.ID()}
	b := startNode(t, m.Config{Identity: ids[1], AllowedPeers: []string{ids[0].ID(), ids[2].ID()}, Relay: via, Reconnect: quickReconnect})
	wait(t, func() bool { return b.Diagnostics().Relay == "reserved" })
	a := startNode(t, m.Config{Identity: ids[0], AllowedPeers: []string{ids[1].ID()}, Relay: via})
	c := startNode(t, m.Config{Identity: ids[2], AllowedPeers: []string{ids[1].ID()}, Relay: via})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if e := a.ConnectViaRelay(ctx, b.ID()); e != nil {
		t.Fatal(e)
	}
	e := c.ConnectViaRelay(ctx, b.ID())
	if e == nil || !strings.Contains(e.Error(), "circuit limit") {
		t.Fatalf("expected circuit limit, got %v", e)
	}
}

func writeToken(t *testing.T, token string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "registry.token")
	if e := os.WriteFile(path, []byte(token), 0600); e != nil {
		t.Fatal(e)
	}
	if e := fsperm.Restrict(path); e != nil {
		t.Fatal(e)
	}
	return path
}

func TestRegistryConfigEndToEnd(t *testing.T) {
	store, _ := m.NewMemoryRegistry(10)
	token := strings.Repeat("t", 32)
	h, _ := discovery.NewHandler(store, token)
	srv := httptest.NewServer(h)
	defer srv.Close()
	tokenFile := writeToken(t, token)
	ids := identities(t, 2)
	address := freeAddress(t)
	b := startNode(t, m.Config{Identity: ids[1], ListenAddress: address, AllowedPeers: []string{ids[0].ID()}, Registry: &m.RegistryConfig{URL: srv.URL, TokenFile: tokenFile, AdvertiseAddress: address, Lease: 3 * time.Second}})
	wait(t, func() bool { return b.Diagnostics().Registration == "active" })
	a := startNode(t, m.Config{Identity: ids[0], AllowedPeers: []string{ids[1].ID()}, Reconnect: quickReconnect, Registry: &m.RegistryConfig{URL: srv.URL, TokenFile: tokenFile}, KnownPeers: []m.KnownPeer{{ID: b.ID()}}})
	wait(t, func() bool { return connectedTo(a, b.ID()) != nil })

	time.Sleep(3500 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, e := store.Lookup(ctx, b.ID()); e != nil {
		t.Fatalf("lease was not renewed: %v", e)
	}
	b.Close()
	if _, e := store.Lookup(ctx, b.ID()); !errors.Is(e, m.ErrRegistrationNotFound) {
		t.Fatalf("expected unregistration on close, got %v", e)
	}
}

func TestKeepRegisteredValidation(t *testing.T) {
	ns := nodes(t, 1, nil)
	store, _ := m.NewMemoryRegistry(10)
	for _, c := range []struct {
		address string
		lease   time.Duration
	}{{"0.0.0.0:7001", time.Minute}, {"127.0.0.1:7001", time.Second}, {"no-port", time.Minute}} {
		if _, e := ns[0].KeepRegistered(store, c.address, c.lease); e == nil {
			t.Errorf("accepted %v", c)
		}
	}
	stop, e := ns[0].KeepRegistered(store, "127.0.0.1:7001", 3*time.Second)
	if e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	wait(t, func() bool { _, e := store.Lookup(ctx, ns[0].ID()); return e == nil })
	stop()
	wait(t, func() bool { _, e := store.Lookup(ctx, ns[0].ID()); return e != nil })
}

type spyTransport struct {
	m.TCPTransport
	seen *lockedBuffer
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) Contains(p []byte) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return bytes.Contains(l.b.Bytes(), p)
}

type spyConn struct {
	m.Conn
	seen *lockedBuffer
}

func (c spyConn) Read(p []byte) (int, error) {
	n, e := c.Conn.Read(p)
	c.seen.Write(p[:n])
	return n, e
}

func (s spyTransport) Authenticate(ctx context.Context, raw net.Conn, c m.Credentials, expected string) (m.Conn, error) {
	conn, e := s.TCPTransport.Authenticate(ctx, raw, c, expected)
	if e != nil {
		return nil, e
	}
	return spyConn{conn, s.seen}, nil
}

func TestRelayCannotReadTraffic(t *testing.T) {
	ids := identities(t, 2)
	seen := &lockedBuffer{}
	r := startRelay(t, func(c *m.RelayServerConfig) { c.Transport = spyTransport{seen: seen} }, ids...)
	via := &m.RelayConfig{Address: r.Address(), ID: r.ID()}
	b := startNode(t, m.Config{Identity: ids[1], AllowedPeers: []string{ids[0].ID()}, Relay: via, Reconnect: quickReconnect})
	wait(t, func() bool { return b.Diagnostics().Relay == "reserved" })
	a := startNode(t, m.Config{Identity: ids[0], AllowedPeers: []string{ids[1].ID()}, Relay: via})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if e := a.ConnectViaRelay(ctx, b.ID()); e != nil {
		t.Fatal(e)
	}
	secret := []byte("TOP-SECRET-PAYLOAD-MARKER")
	if got, e := a.Request(ctx, b.ID(), secret); e != nil || !bytes.Equal(got, secret) {
		t.Fatal(e)
	}
	if !seen.Contains([]byte(`"type":"connect"`)) {
		t.Fatal("spy did not observe relay control traffic")
	}
	if seen.Contains(secret) || seen.Contains([]byte("VE9QLVNFQ1JFVC")) {
		t.Fatal("relay observed plaintext payload")
	}
}

func BenchmarkRelayRoundTrip(b *testing.B) {
	ids := identities(b, 2)
	id, _ := m.GenerateIdentity()
	r, _ := m.NewRelayServer(m.RelayServerConfig{Identity: id, AllowedPeers: []string{ids[0].ID(), ids[1].ID()}})
	r.Start(context.Background())
	defer r.Close()
	via := &m.RelayConfig{Address: r.Address(), ID: r.ID()}
	mk := func(i int) *m.Node {
		n, _ := m.NewNode(m.Config{Identity: ids[i], AllowedPeers: []string{ids[1-i].ID()}, Relay: via, Reconnect: quickReconnect})
		n.Handle(func(_ context.Context, _ string, p []byte) ([]byte, error) { return p, nil })
		n.Start(context.Background())
		return n
	}
	target, client := mk(1), mk(0)
	defer target.Close()
	defer client.Close()
	ctx := context.Background()
	for target.Diagnostics().Relay != "reserved" {
		time.Sleep(time.Millisecond)
	}
	if e := client.ConnectViaRelay(ctx, target.ID()); e != nil {
		b.Fatal(e)
	}
	payload := []byte("ping")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, e := client.Request(ctx, target.ID(), payload); e != nil {
			b.Fatal(e)
		}
	}
}
