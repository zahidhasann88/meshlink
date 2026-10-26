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
	a, e := m.GenerateIdentity()
	if e != nil {
		return e
	}
	b, e := m.GenerateIdentity()
	if e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	n1, e := m.NewNode(m.Config{Identity: a, AllowedPeers: []string{b.ID()}})
	if e != nil {
		return e
	}
	defer n1.Close()
	n2, e := m.NewNode(m.Config{Identity: b, AllowedPeers: []string{a.ID()}})
	if e != nil {
		return e
	}
	defer n2.Close()
	n2.Handle(func(_ context.Context, _ string, p []byte) ([]byte, error) {
		return append([]byte("echo: "), p...), nil
	})
	for _, n := range []*m.Node{n1, n2} {
		if e = n.Start(ctx); e != nil {
			return e
		}
	}
	if e = n1.Connect(ctx, n2.Address(), n2.ID()); e != nil {
		return e
	}
	p, e := n1.Request(ctx, n2.ID(), []byte("hello MeshLink"))
	if e != nil {
		return e
	}
	fmt.Println(string(p))
	return nil
}
