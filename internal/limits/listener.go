package limits

import (
	"net"
	"sync"
)

type listener struct {
	net.Listener
	slots chan struct{}
	done  chan struct{}
	once  sync.Once
}
type conn struct {
	net.Conn
	release func()
	once    sync.Once
}

func (c *conn) Close() error { e := c.Conn.Close(); c.once.Do(c.release); return e }
func Listener(l net.Listener, max int) net.Listener {
	return &listener{Listener: l, slots: make(chan struct{}, max), done: make(chan struct{})}
}
func (l *listener) Accept() (net.Conn, error) {
	select {
	case l.slots <- struct{}{}:
	case <-l.done:
		return nil, net.ErrClosed
	}
	c, e := l.Listener.Accept()
	if e != nil {
		<-l.slots
		return nil, e
	}
	return &conn{Conn: c, release: func() { <-l.slots }}, nil
}
func (l *listener) Close() error { l.once.Do(func() { close(l.done) }); return l.Listener.Close() }
