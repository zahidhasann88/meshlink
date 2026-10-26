package main

import (
	"context"
	"fmt"
	"net"

	m "github.com/zahidhasann88/meshlink/pkg/meshlink"
)

type tunedTCP struct{ m.TCPTransport }

func (t tunedTCP) Dial(ctx context.Context, address string) (net.Conn, error) {
	raw, e := t.TCPTransport.Dial(ctx, address)
	if e == nil {
		if c, ok := raw.(*net.TCPConn); ok {
			if e = c.SetNoDelay(true); e != nil {
				raw.Close()
				return nil, e
			}
		}
	}
	return raw, e
}

var _ m.Transport = tunedTCP{}

func main() {
	n, e := m.NewNode(m.Config{Transport: tunedTCP{}})
	if e != nil {
		panic(e)
	}
	defer n.Close()
	fmt.Println("composed authenticated TCP transport for", n.ID())
}
