package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	m "github.com/zahidhasann88/meshlink/pkg/meshlink"
)

func TestCLIIdentityAndConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.key")
	var out bytes.Buffer
	if e := Run(context.Background(), []string{"identity", "create", "--key", path}, &out, &out); e != nil {
		t.Fatal(e)
	}
	out.Reset()
	if e := Run(context.Background(), []string{"identity", "inspect", "--key", path, "--json"}, &out, &out); e != nil {
		t.Fatal(e)
	}
	var v map[string]string
	if e := json.Unmarshal(out.Bytes(), &v); e != nil || !m.ValidPeerID(v["id"]) {
		t.Fatal(out.String(), e)
	}
	if e := Run(context.Background(), []string{"config", "validate"}, &out, &out); e != nil {
		t.Fatal(e)
	}
}
func TestActualControlSocket(t *testing.T) {
	id1, _ := m.GenerateIdentity()
	id2, _ := m.GenerateIdentity()
	a, _ := m.NewNode(m.Config{Identity: id1, AllowedPeers: []string{id2.ID()}})
	b, _ := m.NewNode(m.Config{Identity: id2, AllowedPeers: []string{id1.ID()}})
	for _, n := range []*m.Node{a, b} {
		if e := n.Start(context.Background()); e != nil {
			t.Fatal(e)
		}
		defer n.Close()
	}
	b.Handle(func(_ context.Context, _ string, p []byte) ([]byte, error) { return p, nil })
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	path := filepath.Join(dir, "ctl.sock")
	stopped := make(chan struct{})
	srv, ln, e := control(a, path, func() { close(stopped) }, newInbox(), nil)
	if e != nil {
		t.Fatal(e)
	}
	defer srv.Close()
	go srv.Serve(ln)
	ctx, c := context.WithTimeout(context.Background(), 3*time.Second)
	defer c()
	if _, e = call(ctx, path, command{Op: "connect", Peer: b.ID(), Address: b.Address()}); e != nil {
		t.Fatal(e)
	}
	res, e := call(ctx, path, command{Op: "request", Peer: b.ID(), Payload: []byte("real engine")})
	if e != nil || string(res.Payload) != "real engine" {
		t.Fatal(res, e)
	}
	res, e = call(ctx, path, command{Op: "diagnostics"})
	if e != nil || len(res.Diagnostics.Peers) != 1 {
		t.Fatal(res, e)
	}
	if _, e = call(ctx, path, command{Op: "shutdown"}); e != nil {
		t.Fatal(e)
	}
	select {
	case <-stopped:
	case <-ctx.Done():
		t.Fatal("shutdown operation did not invoke stop")
	}
}

func TestCLIVersion(t *testing.T) {
	var out bytes.Buffer
	if e := Run(context.Background(), []string{"version"}, &out, &out); e != nil || out.String() != "meshlink dev\n" {
		t.Fatal(out.String(), e)
	}
}

func TestListenStreamsMessages(t *testing.T) {
	id1, _ := m.GenerateIdentity()
	id2, _ := m.GenerateIdentity()
	a, _ := m.NewNode(m.Config{Identity: id1, AllowedPeers: []string{id2.ID()}})
	b, _ := m.NewNode(m.Config{Identity: id2, AllowedPeers: []string{id1.ID()}})
	box := newInbox()
	a.Handle(func(ctx context.Context, from string, p []byte) ([]byte, error) {
		if m.IsRequest(ctx) {
			return p, nil
		}
		box.publish(from, p)
		return nil, nil
	})
	for _, n := range []*m.Node{a, b} {
		if e := n.Start(context.Background()); e != nil {
			t.Fatal(e)
		}
		defer n.Close()
	}
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	path := filepath.Join(dir, "ctl.sock")
	done := make(chan struct{})
	srv, ln, e := control(a, path, func() {}, box, done)
	if e != nil {
		t.Fatal(e)
	}
	defer srv.Close()
	go srv.Serve(ln)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pr, pw := io.Pipe()
	result := make(chan error, 1)
	go func() { result <- listenMessages(ctx, path, true, pw, io.Discard); pw.Close() }()
	lines := bufio.NewScanner(pr)
	if e = b.Connect(ctx, a.Address(), a.ID()); e != nil {
		t.Fatal(e)
	}
	for _, text := range []string{"first", "evil \x1b[2J"} {
		for !hasListener(box) {
			time.Sleep(5 * time.Millisecond)
		}
		if e = b.Send(ctx, a.ID(), []byte(text)); e != nil {
			t.Fatal(e)
		}
		if !lines.Scan() {
			t.Fatal("no message streamed", lines.Err())
		}
		var msg inboxMessage
		if e = json.Unmarshal(lines.Bytes(), &msg); e != nil || msg.From != b.ID() || string(msg.Payload) != text {
			t.Fatal(lines.Text(), e)
		}
	}
	if got, e := b.Request(ctx, a.ID(), []byte("req")); e != nil || string(got) != "req" {
		t.Fatal("requests must not reach listeners", e)
	}
	if printable([]byte("evil \x1b[2J")) == "evil \x1b[2J" {
		t.Fatal("terminal escape sequence printed raw")
	}
	close(done)
	select {
	case e = <-result:
		if e != nil {
			t.Fatal(e)
		}
	case <-ctx.Done():
		t.Fatal("listener did not stop when the node stopped")
	}
}

func hasListener(b *inbox) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs) > 0
}
