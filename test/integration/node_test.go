package integration_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	m "github.com/zahidhasann88/meshlink/pkg/meshlink"
)

func identities(t testing.TB, n int) []*m.Identity {
	t.Helper()
	out := make([]*m.Identity, n)
	for i := range out {
		var e error
		out[i], e = m.GenerateIdentity()
		if e != nil {
			t.Fatal(e)
		}
	}
	return out
}
func nodes(t testing.TB, count int, edit func(int, *m.Config)) []*m.Node {
	t.Helper()
	ids := identities(t, count)
	pins := make([]string, count)
	for i := range ids {
		pins[i] = ids[i].ID()
	}
	out := make([]*m.Node, count)
	for i := range out {
		c := m.Config{Identity: ids[i], AllowedPeers: pins, IdleTimeout: time.Second * 5}
		if edit != nil {
			edit(i, &c)
		}
		n, e := m.NewNode(c)
		if e != nil {
			t.Fatal(e)
		}
		out[i] = n
		n.Handle(func(_ context.Context, _ string, b []byte) ([]byte, error) { return b, nil })
		if e = n.Start(context.Background()); e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { n.Close() })
	}
	return out
}
func wait(t *testing.T, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(time.Millisecond * 5)
	}
	t.Fatal("condition not reached")
}
func connect(t testing.TB, a, b *m.Node) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if e := a.Connect(ctx, b.Address(), b.ID()); e != nil {
		t.Fatal(e)
	}
}
func TestEncryptedMessagingAndLifecycle(t *testing.T) {
	ns := nodes(t, 3, nil)
	a, b, c := ns[0], ns[1], ns[2]
	got := make(chan string, 8)
	b.Handle(func(_ context.Context, id string, p []byte) ([]byte, error) {
		if id != a.ID() {
			return nil, fmt.Errorf("wrong sender")
		}
		got <- string(p)
		return append([]byte("echo:"), p...), nil
	})
	connect(t, a, b)
	connect(t, a, c)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if e := a.Send(ctx, b.ID(), []byte("hello")); e != nil {
		t.Fatal(e)
	}
	select {
	case s := <-got:
		if s != "hello" {
			t.Fatal(s)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	p, e := a.Request(ctx, b.ID(), []byte("world"))
	if e != nil || string(p) != "echo:world" {
		t.Fatalf("%s %v", p, e)
	}
	for _, e := range a.Broadcast(ctx, []byte("broadcast")) {
		if e != nil {
			t.Fatal(e)
		}
	}
	d := a.Diagnostics()
	if d.BytesSent == 0 || d.BytesReceived == 0 || len(d.Peers) != 2 {
		t.Fatalf("bad diagnostics: %+v", d)
	}
	addr := b.Address()
	if e = b.Close(); e != nil {
		t.Fatal(e)
	}
	b.Close()
	wait(t, func() bool { return len(a.Peers()) == 1 })
	if e = a.Connect(ctx, addr, b.ID()); e == nil {
		t.Fatal("closed listener accepted connection")
	}
}
func TestLargeMessageAndConcurrentRequests(t *testing.T) {
	ns := nodes(t, 2, func(_ int, c *m.Config) { c.QueueCapacity = 128 })
	a, b := ns[0], ns[1]
	connect(t, a, b)
	payload := bytes.Repeat([]byte("x"), 600<<10)
	p, e := a.Request(context.Background(), b.ID(), payload)
	if e != nil || !bytes.Equal(p, payload) {
		t.Fatalf("large transfer: %v", e)
	}
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			payload := []byte(fmt.Sprint(i))
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			p, e := a.Request(ctx, b.ID(), payload)
			if e != nil || !bytes.Equal(p, payload) {
				t.Errorf("correlation %d: %q %v", i, p, e)
			}
		}(i)
	}
	wg.Wait()
	if e = a.Send(context.Background(), b.ID(), bytes.Repeat([]byte("x"), 1<<20)); !errors.Is(e, m.ErrFrameTooLarge) {
		t.Fatalf("oversize %v", e)
	}
}
func TestAuthenticationRejection(t *testing.T) {
	ids := identities(t, 3)
	a, e := m.NewNode(m.Config{Identity: ids[0], AllowedPeers: []string{ids[1].ID(), ids[2].ID()}})
	if e != nil {
		t.Fatal(e)
	}
	b, e := m.NewNode(m.Config{Identity: ids[1]})
	if e != nil {
		t.Fatal(e)
	}
	for _, n := range []*m.Node{a, b} {
		if e = n.Start(context.Background()); e != nil {
			t.Fatal(e)
		}
		defer n.Close()
	}
	if e = a.Connect(context.Background(), b.Address(), ids[2].ID()); !errors.Is(e, m.ErrAuthentication) {
		t.Fatalf("substitution accepted: %v", e)
	}
	_ = a.Connect(context.Background(), b.Address(), b.ID())
	wait(t, func() bool { return b.Diagnostics().RejectedConnections >= 2 && len(a.Peers()) == 0 })
	if len(b.Peers()) != 0 {
		t.Fatal("unauthorized peer admitted")
	}
}
func TestTimeoutAndPendingCleanup(t *testing.T) {
	ns := nodes(t, 2, func(_ int, c *m.Config) { c.RequestTimeout = 80 * time.Millisecond; c.MaxPending = 1 })
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
	done := make(chan error, 1)
	go func() { _, e := a.Request(context.Background(), b.ID(), nil); done <- e }()
	<-entered
	if _, e := a.Request(context.Background(), b.ID(), nil); !errors.Is(e, m.ErrBackpressure) {
		t.Fatalf("pending cap %v", e)
	}
	if e := <-done; !errors.Is(e, context.DeadlineExceeded) {
		t.Fatalf("timeout %v", e)
	}
	if a.Diagnostics().RequestTimeouts != 1 {
		t.Fatal("missing timeout count")
	}
}
func TestShutdownDuringRequests(t *testing.T) {
	ns := nodes(t, 2, nil)
	a, b := ns[0], ns[1]
	b.Handle(func(ctx context.Context, _ string, _ []byte) ([]byte, error) { <-ctx.Done(); return nil, ctx.Err() })
	connect(t, a, b)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, c := context.WithTimeout(context.Background(), time.Second)
			defer c()
			a.Request(ctx, b.ID(), nil)
		}()
	}
	b.Close()
	a.Close()
	wg.Wait()
}
func TestPeerLimitReconnectAndEvents(t *testing.T) {
	ns := nodes(t, 3, func(i int, c *m.Config) {
		if i == 0 {
			c.MaxPeers = 1
		}
	})
	a, b, c := ns[0], ns[1], ns[2]
	connect(t, a, b)
	if e := a.Connect(context.Background(), c.Address(), c.ID()); !errors.Is(e, m.ErrPeerLimit) {
		t.Fatal(e)
	}
	a.Disconnect(b.ID())
	wait(t, func() bool { return len(b.Peers()) == 0 })
	if e := a.ConnectWithRetry(context.Background(), b.Address(), b.ID(), m.RetryPolicy{Attempts: 3, InitialDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond}); e != nil {
		t.Fatal(e)
	}
	select {
	case e := <-a.Events():
		if e.State != "connected" {
			t.Fatal(e)
		}
	default:
		t.Fatal("missing event")
	}
}
func TestConcurrentStartCloseConnect(t *testing.T) {
	for k := 0; k < 10; k++ {
		ns := nodes(t, 2, nil)
		a, b := ns[0], ns[1]
		var wg sync.WaitGroup
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); _ = a.Connect(context.Background(), b.Address(), b.ID()) }()
		}
		wg.Add(2)
		go func() { defer wg.Done(); a.Close() }()
		go func() { defer wg.Done(); a.Close() }()
		wg.Wait()
		b.Close()
	}
}
func BenchmarkRoundTrip(b *testing.B) {
	ns := nodes(b, 2, nil)
	connect(b, ns[0], ns[1])
	p := []byte("hello meshlink")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, e := ns[0].Request(context.Background(), ns[1].ID(), p); e != nil {
			b.Fatal(e)
		}
	}
}

func TestDiscoveryConnect(t *testing.T) {
	ns := nodes(t, 2, nil)
	registry, e := m.NewMemoryRegistry(10)
	if e != nil {
		t.Fatal(e)
	}
	if e = ns[1].Register(context.Background(), registry, ns[1].Address(), time.Minute); e != nil {
		t.Fatal(e)
	}
	if e = ns[0].DiscoverAndConnect(context.Background(), registry, ns[1].ID()); e != nil {
		t.Fatal(e)
	}
	p, e := ns[0].Request(context.Background(), ns[1].ID(), []byte("discovered"))
	if e != nil || string(p) != "discovered" {
		t.Fatal(e)
	}
}

func BenchmarkLargeTransfer(b *testing.B) {
	ns := nodes(b, 2, nil)
	connect(b, ns[0], ns[1])
	p := bytes.Repeat([]byte("x"), 600<<10)
	b.SetBytes(int64(len(p)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, e := ns[0].Request(context.Background(), ns[1].ID(), p); e != nil {
			b.Fatal(e)
		}
	}
}
func BenchmarkConcurrentRequests(b *testing.B) {
	ns := nodes(b, 2, nil)
	connect(b, ns[0], ns[1])
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, e := ns[0].Request(context.Background(), ns[1].ID(), []byte("hello")); e != nil {
				b.Error(e)
				return
			}
		}
	})
}
func BenchmarkConnect(b *testing.B) {
	ns := nodes(b, 2, nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if e := ns[0].Connect(context.Background(), ns[1].Address(), ns[1].ID()); e != nil {
			b.Fatal(e)
		}
		ns[0].Disconnect(ns[1].ID())
		for len(ns[1].Peers()) != 0 {
			time.Sleep(time.Microsecond)
		}
	}
}
