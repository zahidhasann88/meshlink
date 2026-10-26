package engine

import (
	"context"
	"errors"
	"github.com/zahidhasann88/meshlink/internal/transport/tcp"
	"net"
	"testing"
	"time"

	"github.com/zahidhasann88/meshlink/internal/protocol"
)

func TestValidation(t *testing.T) {
	for _, c := range []Config{{MaxPeers: -1}, {MaxFrameSize: 10}, {Workers: 10000}, {AllowedPeers: []string{"spoof"}}, {WriteTimeout: -time.Second}} {
		if ValidateConfig(c) == nil {
			t.Fatalf("accepted %+v", c)
		}
	}
	if _, e := ParseConfig([]byte(`{"unknown":1}`)); e == nil {
		t.Fatal("unknown key")
	}
	if _, e := ParseConfig([]byte(`{} {}`)); e == nil {
		t.Fatal("trailing JSON")
	}
}
func TestQueueOverflow(t *testing.T) {
	n, _ := NewNode(Config{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &session{n: n, ctx: ctx, queue: make(chan job, 1)}
	s.queue <- job{}
	if e := s.send(ctx, protocol.Message{Version: 1, Type: "data", ID: "00000000000000000000000000000000"}); !errors.Is(e, ErrBackpressure) {
		t.Fatal(e)
	}
}
func TestStateTransitions(t *testing.T) {
	n, _ := NewNode(Config{})
	if e := n.Send(context.Background(), "x", nil); !errors.Is(e, ErrNotStarted) {
		t.Fatal(e)
	}
	if e := n.Start(context.Background()); e != nil {
		t.Fatal(e)
	}
	if e := n.Start(context.Background()); e == nil {
		t.Fatal("started twice")
	}
	n.Close()
	n.Close()
	if n.Diagnostics().State != "closed" {
		t.Fatal("state")
	}
	if e := n.Start(context.Background()); e == nil {
		t.Fatal("restarted closed node")
	}
}
func TestContextClosesNode(t *testing.T) {
	n, _ := NewNode(Config{})
	ctx, cancel := context.WithCancel(context.Background())
	if e := n.Start(ctx); e != nil {
		t.Fatal(e)
	}
	cancel()
	select {
	case <-n.done:
	case <-time.After(time.Second):
		t.Fatal("did not stop")
	}
}
func TestRetryValidation(t *testing.T) {
	n, _ := NewNode(Config{})
	if e := n.ConnectWithRetry(context.Background(), "", "", RetryPolicy{}); e == nil {
		t.Fatal("invalid policy")
	}
}
func FuzzConfig(f *testing.F) {
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"queue_capacity":-1}`))
	f.Fuzz(func(t *testing.T, b []byte) { ParseConfig(b) })
}

func BenchmarkQueueBackpressure(b *testing.B) {
	n, _ := NewNode(Config{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &session{n: n, ctx: ctx, queue: make(chan job, 1)}
	s.queue <- job{}
	m := protocol.Message{Version: 1, Type: "data", ID: "00000000000000000000000000000000"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if e := s.send(ctx, m); !errors.Is(e, ErrBackpressure) {
			b.Fatal(e)
		}
	}
}

type blockedListen struct {
	tcp.Transport
	entered chan struct{}
}

func (t blockedListen) Listen(ctx context.Context, _ string) (net.Listener, error) {
	close(t.entered)
	<-ctx.Done()
	return nil, ctx.Err()
}
func TestCloseDuringListen(t *testing.T) {
	entered := make(chan struct{})
	n, e := NewNode(Config{Transport: blockedListen{entered: entered}})
	if e != nil {
		t.Fatal(e)
	}
	started := make(chan error, 1)
	go func() { started <- n.Start(context.Background()) }()
	<-entered
	done := make(chan struct{})
	go func() { n.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Close blocked on Listen")
	}
	if e := <-started; !errors.Is(e, ErrClosed) {
		t.Fatal(e)
	}
}
