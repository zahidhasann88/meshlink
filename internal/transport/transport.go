package transport

import (
	"context"
	"net"

	"github.com/zahidhasann88/meshlink/internal/identity"
)

type Credentials struct {
	Identity     *identity.Identity
	AllowedPeers map[string]bool
}

type Conn interface {
	net.Conn
	PeerID() string
}

type Transport interface {
	Name() string
	Listen(context.Context, string) (net.Listener, error)
	Dial(context.Context, string) (net.Conn, error)
	Authenticate(context.Context, net.Conn, Credentials, string) (Conn, error)
}
