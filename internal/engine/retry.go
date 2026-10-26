package engine

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"time"

	"github.com/zahidhasann88/meshlink/internal/identity"
	"github.com/zahidhasann88/meshlink/internal/transport/tcp"
)

type RetryPolicy struct {
	Attempts               int
	InitialDelay, MaxDelay time.Duration
}

func (n *Node) ConnectWithRetry(ctx context.Context, address, id string, p RetryPolicy) error {
	if p.Attempts < 1 || p.Attempts > 100 || p.InitialDelay < time.Millisecond || p.MaxDelay < p.InitialDelay || p.MaxDelay > time.Hour {
		return errors.New("invalid retry policy")
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
	for i := 0; i < p.Attempts; i++ {
		e := n.Connect(ctx, address, id)
		if e == nil {
			return nil
		}
		if errors.Is(e, tcp.ErrAuthentication) || errors.Is(e, ErrDuplicate) || errors.Is(e, ErrNotStarted) || ctx.Err() != nil {
			return e
		}
		if i == p.Attempts-1 {
			return e
		}
		n.retries.Add(1)
		var b [8]byte
		if _, err := rand.Read(b[:]); err != nil {
			return err
		}
		jitter := time.Duration(binary.BigEndian.Uint64(b[:]) % uint64(delay/2+1))
		timer := time.NewTimer(delay/2 + jitter)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-n.ctx.Done():
			timer.Stop()
			return ErrClosed
		case <-timer.C:
		}
		if delay < p.MaxDelay/2 {
			delay *= 2
		} else {
			delay = p.MaxDelay
		}
	}
	return nil
}
