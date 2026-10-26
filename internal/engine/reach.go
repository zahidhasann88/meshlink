package engine

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/zahidhasann88/meshlink/internal/discovery"
	"github.com/zahidhasann88/meshlink/internal/fsperm"
	"github.com/zahidhasann88/meshlink/internal/relay"
	"github.com/zahidhasann88/meshlink/internal/transport"
	"github.com/zahidhasann88/meshlink/internal/transport/tcp"
)

const (
	relayPing     = 20 * time.Second
	relayDeadline = 60 * time.Second
)

var errNoRoute = errors.New("no address, registry or relay for peer")

func newRegistryClient(c RegistryConfig) (discovery.Discovery, error) {
	b, e := fsperm.ReadPrivate(c.TokenFile, 4096)
	if e != nil {
		return nil, fmt.Errorf("token file: %w", e)
	}
	return discovery.NewClient(c.URL, strings.TrimSpace(string(b)))
}

func (n *Node) KeepRegistered(d discovery.Discovery, address string, lease time.Duration) (stop func(), err error) {
	if d == nil {
		return nil, errors.New("registry is required")
	}
	if e := discovery.Validate(discovery.Record{ID: n.ID(), Address: address}); e != nil {
		return nil, e
	}
	if lease < 3*time.Second || lease > 24*time.Hour {
		return nil, errors.New("lease must be between 3s and 24h")
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.keepRegisteredLocked(d, address, lease)
}

func (n *Node) keepRegisteredLocked(d discovery.Discovery, address string, lease time.Duration) (func(), error) {
	if n.state != "running" {
		return nil, ErrNotStarted
	}
	ctx, cancel := context.WithCancel(n.ctx)
	n.wg.Add(1)
	go n.keepRegistered(ctx, d, discovery.Record{ID: n.ID(), Address: address}, lease)
	return cancel, nil
}

func (n *Node) keepRegistered(ctx context.Context, d discovery.Discovery, r discovery.Record, lease time.Duration) {
	defer n.wg.Done()
	renew := lease / 3
	retry := min(time.Second, renew)
	for {
		rctx, cancel := context.WithTimeout(ctx, min(10*time.Second, renew))
		e := d.Register(rctx, r, lease)
		cancel()
		if ctx.Err() != nil {
			break
		}
		wait := renew
		if e != nil {
			n.note(fmt.Errorf("registry: %w", e))
			n.setState(&n.registration, "failing")
			wait, retry = retry, min(retry*2, renew)
		} else {
			n.setState(&n.registration, "active")
			retry = min(time.Second, renew)
		}
		if n.backoff(ctx, wait) != nil {
			break
		}
	}
	n.setState(&n.registration, "")
	uctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	d.Unregister(uctx, r.ID)
}

func (n *Node) setState(field *string, v string) {
	n.mu.Lock()
	*field = v
	n.mu.Unlock()
}

func (n *Node) reach(ctx context.Context, address, id string) (*session, error) {
	e := errNoRoute
	if address == "" && n.discovery != nil {
		lctx, cancel := context.WithTimeout(ctx, n.cfg.ConnectTimeout)
		r, le := n.discovery.Lookup(lctx, id)
		cancel()
		if le == nil {
			address = r.Address
		} else {
			e = fmt.Errorf("registry lookup: %w", le)
		}
	}
	if address != "" {
		var s *session
		s, e = n.connect(ctx, address, id)
		if e == nil || permanent(e) || errors.Is(e, ErrDuplicate) || n.cfg.Relay == nil {
			return s, e
		}
	}
	if n.cfg.Relay != nil {
		return n.connectViaRelay(ctx, id)
	}
	return nil, e
}

func (n *Node) ConnectViaRelay(ctx context.Context, id string) error {
	if n.cfg.Relay == nil {
		return errors.New("no relay configured")
	}
	_, e := n.connectViaRelay(ctx, id)
	return e
}

func (n *Node) connectViaRelay(ctx context.Context, id string) (*session, error) {
	return n.dialAndEstablish(ctx, id, n.cfg.Relay.ID, func(ctx context.Context) (net.Conn, error) {
		return n.dialRelay(ctx, relay.Message{Type: "connect", Target: id})
	})
}

func (n *Node) dialRelay(ctx context.Context, first relay.Message) (net.Conn, error) {
	raw, e := n.cfg.Transport.Dial(ctx, n.cfg.Relay.Address)
	if e != nil {
		return nil, fmt.Errorf("dial relay: %w", e)
	}
	creds := transport.Credentials{Identity: n.cfg.Identity, AllowedPeers: map[string]bool{n.cfg.Relay.ID: true}}
	c, e := n.cfg.Transport.Authenticate(ctx, raw, creds, n.cfg.Relay.ID)
	if e != nil {
		raw.Close()
		return nil, fmt.Errorf("relay: %w", e)
	}
	if d, ok := ctx.Deadline(); ok {
		c.SetDeadline(d)
	}
	e = relay.Write(c, first)
	if e == nil {
		var m relay.Message
		if m, e = relay.Read(c); e == nil && m.Type != "ok" {
			e = m.Err()
		}
	}
	c.SetDeadline(time.Time{})
	if e != nil {
		c.Close()
		return nil, fmt.Errorf("relay: %w", e)
	}
	return c, nil
}

func (n *Node) reserveRelay(ctx context.Context) {
	defer n.wg.Done()
	p := n.cfg.Reconnect
	delay := p.InitialDelay
	for ctx.Err() == nil {
		dctx, cancel := context.WithTimeout(ctx, n.cfg.ConnectTimeout)
		c, e := n.dialRelay(dctx, relay.Message{Type: "reserve"})
		cancel()
		if e == nil {
			n.setState(&n.relayState, "reserved")
			start := time.Now()
			n.serveReservation(ctx, c)
			if time.Since(start) > p.MaxDelay {
				delay = p.InitialDelay
			}
			e = errors.New("relay reservation closed")
		}
		if ctx.Err() != nil {
			return
		}
		n.note(e)
		if errors.Is(e, tcp.ErrAuthentication) {
			n.setState(&n.relayState, "rejected")
			return
		}
		n.setState(&n.relayState, "connecting")
		n.retries.Add(1)
		if n.backoff(ctx, delay) != nil {
			return
		}
		delay = p.next(delay)
	}
}

func (n *Node) serveReservation(ctx context.Context, c net.Conn) {
	defer c.Close()
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	done := make(chan struct{})
	defer close(done)
	n.wg.Add(1)
	go func() {
		defer n.wg.Done()
		t := time.NewTicker(relayPing)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				c.SetWriteDeadline(time.Now().Add(n.cfg.WriteTimeout))
				if relay.Write(c, relay.Message{Type: "ping"}) != nil {
					c.Close()
					return
				}
			}
		}
	}()
	for {
		c.SetReadDeadline(time.Now().Add(relayDeadline))
		m, e := relay.Read(c)
		if e != nil {
			return
		}
		if m.Type == "incoming" && n.creds.AllowedPeers[m.From] && m.From != n.ID() {
			n.wg.Add(1)
			go n.acceptRelayed(m.Circuit)
		}
	}
}

func (n *Node) acceptRelayed(circuit string) {
	defer n.wg.Done()
	_, e := n.dialAndEstablish(n.ctx, "", n.cfg.Relay.ID, func(ctx context.Context) (net.Conn, error) {
		return n.dialRelay(ctx, relay.Message{Type: "accept", Circuit: circuit})
	})
	if e != nil {
		n.rejected.Add(1)
	}
}
