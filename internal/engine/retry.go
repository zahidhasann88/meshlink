package engine

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/zahidhasann88/meshlink/internal/identity"
	"github.com/zahidhasann88/meshlink/internal/transport/tcp"
)

type RetryPolicy struct {
	Attempts     int           `json:"attempts"`
	InitialDelay time.Duration `json:"initial_delay"`
	MaxDelay     time.Duration `json:"max_delay"`
}

var DefaultRetryPolicy = RetryPolicy{Attempts: 10, InitialDelay: 500 * time.Millisecond, MaxDelay: 30 * time.Second}

func (p RetryPolicy) validate() error {
	if p.Attempts < 1 || p.Attempts > 100 || p.InitialDelay < time.Millisecond || p.MaxDelay < p.InitialDelay || p.MaxDelay > time.Hour {
		return errors.New("invalid retry policy: need attempts 1..100, initial_delay >= 1ms, initial_delay <= max_delay <= 1h")
	}
	return nil
}

func (p RetryPolicy) withDefaults() RetryPolicy {
	if p.Attempts == 0 {
		p.Attempts = DefaultRetryPolicy.Attempts
	}
	if p.InitialDelay == 0 {
		p.InitialDelay = DefaultRetryPolicy.InitialDelay
	}
	if p.MaxDelay == 0 {
		p.MaxDelay = max(DefaultRetryPolicy.MaxDelay, p.InitialDelay)
	}
	return p
}

func (p RetryPolicy) next(delay time.Duration) time.Duration {
	if delay < p.MaxDelay/2 {
		return delay * 2
	}
	return p.MaxDelay
}

func permanent(e error) bool {
	return errors.Is(e, tcp.ErrAuthentication) || errors.Is(e, ErrNotStarted) || errors.Is(e, ErrClosed)
}

func (n *Node) backoff(ctx context.Context, delay time.Duration) error {
	var b [8]byte
	if _, e := rand.Read(b[:]); e != nil {
		return e
	}
	jitter := time.Duration(binary.BigEndian.Uint64(b[:]) % uint64(delay/2+1))
	timer := time.NewTimer(delay/2 + jitter)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-n.ctx.Done():
		return ErrClosed
	case <-timer.C:
		return nil
	}
}

func (n *Node) ConnectWithRetry(ctx context.Context, address, id string, p RetryPolicy) error {
	if e := p.validate(); e != nil {
		return e
	}
	if !identity.ValidID(id) || !n.creds.AllowedPeers[id] {
		return tcp.ErrAuthentication
	}
	n.mu.Lock()
	running := n.state == "running"
	n.mu.Unlock()
	if !running {
		return ErrNotStarted
	}
	delay := p.InitialDelay
	for i := 0; ; i++ {
		e := n.Connect(ctx, address, id)
		if e == nil || permanent(e) || errors.Is(e, ErrDuplicate) || ctx.Err() != nil || i == p.Attempts-1 {
			return e
		}
		n.retries.Add(1)
		if e := n.backoff(ctx, delay); e != nil {
			return e
		}
		delay = p.next(delay)
	}
}

type maintainer struct {
	address string
	cancel  context.CancelFunc
}

func (n *Node) Maintain(address, id string, p RetryPolicy) (stop func(), err error) {
	if e := p.validate(); e != nil {
		return nil, e
	}
	if !identity.ValidID(id) || !n.creds.AllowedPeers[id] {
		return nil, fmt.Errorf("%w: peer not in allowed peers", tcp.ErrAuthentication)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.maintainLocked(address, id, p)
}

func (n *Node) maintainLocked(address, id string, p RetryPolicy) (func(), error) {
	if n.state != "running" {
		return nil, ErrNotStarted
	}
	if _, ok := n.maintained[id]; ok {
		return nil, fmt.Errorf("peer %s is already maintained", id)
	}
	ctx, cancel := context.WithCancel(n.ctx)
	m := &maintainer{address: address, cancel: cancel}
	n.maintained[id] = m
	n.wg.Add(1)
	go n.maintain(ctx, m, id, p)
	return cancel, nil
}

func (n *Node) current(id string) *session {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.peers[id]
}

func (n *Node) maintain(ctx context.Context, m *maintainer, id string, p RetryPolicy) {
	defer n.wg.Done()
	defer func() {
		m.cancel()
		n.mu.Lock()
		if n.maintained[id] == m {
			delete(n.maintained, id)
		}
		n.mu.Unlock()
	}()
	stableAfter := max(p.MaxDelay, time.Second)
	failures, delay := 0, p.InitialDelay
	var last error
	for {
		s := n.current(id)
		if s == nil {
			var e error
			s, e = n.connect(ctx, m.address, id)
			if errors.Is(e, ErrDuplicate) {
				if s, e = n.current(id), nil; s == nil {
					e = ErrNotConnected
				}
			}
			if ctx.Err() != nil {
				return
			}
			if e != nil && permanent(e) {
				n.giveUp(id, e)
				return
			}
			if e != nil {
				failures, last = failures+1, e
			}
		}
		if s != nil {
			select {
			case <-s.ctx.Done():
			case <-ctx.Done():
				return
			}
			if ctx.Err() != nil {
				return
			}
			if time.Since(s.since) >= stableAfter {
				failures, delay = 0, p.InitialDelay
				continue
			}
			failures, last = failures+1, errors.New("connection closed shortly after connecting; the peer may not allow this node")
		}
		if failures >= p.Attempts {
			n.giveUp(id, fmt.Errorf("%d consecutive failures, last: %w", failures, last))
			return
		}
		n.retries.Add(1)
		if n.backoff(ctx, delay) != nil {
			return
		}
		delay = p.next(delay)
	}
}

func (n *Node) giveUp(id string, e error) {
	e = fmt.Errorf("stopped reconnecting to %s: %w", id, e)
	n.mu.Lock()
	n.lastError = e.Error()
	n.mu.Unlock()
	n.cfg.Logger.Warn("reconnect gave up", "peer", id, "error", e)
}
