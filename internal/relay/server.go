package relay

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zahidhasann88/meshlink/internal/identity"
	"github.com/zahidhasann88/meshlink/internal/transport"
	"github.com/zahidhasann88/meshlink/internal/transport/tcp"
)

const (
	controlTimeout = 5 * time.Second
	acceptTimeout  = 10 * time.Second
	reservationTTL = 60 * time.Second
)

type Config struct {
	Identity           *identity.Identity
	ListenAddress      string
	AllowedPeers       []string
	MaxReservations    int
	MaxCircuits        int
	MaxCircuitsPerPeer int
	MaxHandshakes      int
	IdleTimeout        time.Duration
	Transport          transport.Transport
	Logger             *slog.Logger
}

type Stats struct {
	Reservations int    `json:"reservations"`
	Circuits     int    `json:"circuits"`
	BytesRelayed uint64 `json:"bytes_relayed"`
	Rejected     uint64 `json:"rejected"`
}

type Server struct {
	cfg          Config
	creds        transport.Credentials
	mu           sync.Mutex
	state        string
	ctx          context.Context
	cancel       context.CancelFunc
	ln           net.Listener
	reservations map[string]*reservation
	pending      map[string]*circuit
	active       int
	perPeer      map[string]int
	conns        map[net.Conn]struct{}
	handshakes   chan struct{}
	wg           sync.WaitGroup
	bytes        atomic.Uint64
	rejected     atomic.Uint64
}

type reservation struct {
	conn net.Conn
	wmu  sync.Mutex
}

type endpoint struct {
	conn, raw net.Conn
}

type circuit struct {
	from, target string
	ready        chan endpoint
}

func NewServer(c Config) (*Server, error) {
	if !c.Identity.Valid() {
		return nil, errors.New("relay requires a valid identity")
	}
	if c.ListenAddress == "" {
		c.ListenAddress = "127.0.0.1:0"
	}
	allowed := make(map[string]bool, len(c.AllowedPeers))
	for _, id := range c.AllowedPeers {
		if !identity.ValidID(id) {
			return nil, fmt.Errorf("invalid allowed peer ID %q", id)
		}
		allowed[id] = true
	}
	for _, v := range []struct {
		p      *int
		d, max int
	}{{&c.MaxReservations, 1024, 65536}, {&c.MaxCircuits, 256, 65536}, {&c.MaxCircuitsPerPeer, 16, 1024}, {&c.MaxHandshakes, 32, 1024}} {
		if *v.p == 0 {
			*v.p = v.d
		}
		if *v.p < 1 || *v.p > v.max {
			return nil, errors.New("relay limit outside supported range")
		}
	}
	if c.IdleTimeout == 0 {
		c.IdleTimeout = 2 * time.Minute
	}
	if c.IdleTimeout < time.Second || c.IdleTimeout > 24*time.Hour {
		return nil, errors.New("relay idle timeout must be between 1s and 24h")
	}
	if c.Transport == nil {
		c.Transport = tcp.Transport{}
	}
	if c.Logger == nil {
		c.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Server{cfg: c, creds: transport.Credentials{Identity: c.Identity, AllowedPeers: allowed}, state: "new", reservations: map[string]*reservation{}, pending: map[string]*circuit{}, perPeer: map[string]int{}, conns: map[net.Conn]struct{}{}, handshakes: make(chan struct{}, c.MaxHandshakes)}, nil
}

func (s *Server) ID() string { return s.cfg.Identity.ID() }

func (s *Server) Address() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln == nil {
		return ""
	}
	return s.ln.Addr().String()
}

func (s *Server) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Stats{Reservations: len(s.reservations), Circuits: s.active, BytesRelayed: s.bytes.Load(), Rejected: s.rejected.Load()}
}

func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != "new" {
		return errors.New("relay can only be started once")
	}
	ln, e := s.cfg.Transport.Listen(ctx, s.cfg.ListenAddress)
	if e != nil {
		return fmt.Errorf("listen: %w", e)
	}
	s.ln, s.state = ln, "running"
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.wg.Add(1)
	go s.accept()
	context.AfterFunc(s.ctx, func() { s.Close() })
	return nil
}

func (s *Server) Close() error {
	s.mu.Lock()
	if s.state != "running" {
		s.state = "closed"
		s.mu.Unlock()
		s.wg.Wait()
		return nil
	}
	s.state = "closed"
	s.cancel()
	s.ln.Close()
	for c := range s.conns {
		c.Close()
	}
	s.mu.Unlock()
	s.wg.Wait()
	return nil
}

func (s *Server) track(c net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != "running" {
		return false
	}
	s.conns[c] = struct{}{}
	return true
}

func (s *Server) release(c net.Conn) {
	c.Close()
	s.mu.Lock()
	delete(s.conns, c)
	s.mu.Unlock()
}

func (s *Server) accept() {
	defer s.wg.Done()
	for {
		raw, e := s.ln.Accept()
		if e != nil {
			return
		}
		select {
		case s.handshakes <- struct{}{}:
		default:
			s.rejected.Add(1)
			raw.Close()
			continue
		}
		if !s.track(raw) {
			<-s.handshakes
			raw.Close()
			return
		}
		s.wg.Add(1)
		go s.handle(raw)
	}
}

func (s *Server) handle(raw net.Conn) {
	defer s.wg.Done()
	handedOff := false
	defer func() {
		if !handedOff {
			s.release(raw)
		}
	}()
	ctx, cancel := context.WithTimeout(s.ctx, controlTimeout)
	c, e := s.cfg.Transport.Authenticate(ctx, raw, s.creds, "")
	cancel()
	<-s.handshakes
	if e != nil {
		s.rejected.Add(1)
		return
	}
	c.SetDeadline(time.Now().Add(controlTimeout))
	m, e := Read(c)
	c.SetDeadline(time.Time{})
	if e != nil {
		return
	}
	id := c.PeerID()
	switch m.Type {
	case "reserve":
		s.reserve(c, id)
	case "connect":
		s.connect(c, id, m.Target)
	case "accept":
		handedOff = s.acceptCircuit(c, raw, id, m.Circuit)
	default:
		s.reply(c, Message{Type: "error", Error: "unexpected message"})
	}
}

func (s *Server) reply(c net.Conn, m Message) error {
	c.SetWriteDeadline(time.Now().Add(controlTimeout))
	defer c.SetWriteDeadline(time.Time{})
	return Write(c, m)
}

func (r *reservation) send(s *Server, m Message) error {
	r.wmu.Lock()
	defer r.wmu.Unlock()
	return s.reply(r.conn, m)
}

func (s *Server) reserve(c net.Conn, id string) {
	r := &reservation{conn: c}
	s.mu.Lock()
	old := s.reservations[id]
	if old == nil && len(s.reservations) >= s.cfg.MaxReservations {
		s.mu.Unlock()
		s.rejected.Add(1)
		s.reply(c, Message{Type: "error", Error: "relay is full"})
		return
	}
	s.reservations[id] = r
	s.mu.Unlock()
	if old != nil {
		old.conn.Close()
	}
	defer func() {
		s.mu.Lock()
		if s.reservations[id] == r {
			delete(s.reservations, id)
		}
		s.mu.Unlock()
	}()
	if r.send(s, Message{Type: "ok"}) != nil {
		return
	}
	s.cfg.Logger.Info("relay reservation", "peer", id)
	for {
		c.SetReadDeadline(time.Now().Add(reservationTTL))
		m, e := Read(c)
		if e != nil || m.Type != "ping" || r.send(s, Message{Type: "pong"}) != nil {
			return
		}
	}
}

func (s *Server) connect(c net.Conn, from, target string) {
	if target == from {
		s.reply(c, Message{Type: "error", Error: "cannot connect to self"})
		return
	}
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return
	}
	id := hex.EncodeToString(b[:])
	ci := &circuit{from: from, target: target, ready: make(chan endpoint, 1)}
	s.mu.Lock()
	r := s.reservations[target]
	switch {
	case r == nil:
		s.mu.Unlock()
		s.reply(c, Message{Type: "error", Error: "target is not connected to this relay"})
		return
	case s.active+len(s.pending) >= s.cfg.MaxCircuits || s.perPeer[from] >= s.cfg.MaxCircuitsPerPeer || s.perPeer[target] >= s.cfg.MaxCircuitsPerPeer:
		s.mu.Unlock()
		s.rejected.Add(1)
		s.reply(c, Message{Type: "error", Error: "circuit limit reached"})
		return
	}
	s.pending[id] = ci
	s.perPeer[from]++
	s.perPeer[target]++
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.pending, id)
		select {
		case orphan := <-ci.ready:
			defer s.release(orphan.raw)
		default:
		}
		for _, p := range []string{from, target} {
			if s.perPeer[p]--; s.perPeer[p] <= 0 {
				delete(s.perPeer, p)
			}
		}
		s.mu.Unlock()
	}()
	if r.send(s, Message{Type: "incoming", From: from, Circuit: id}) != nil {
		s.reply(c, Message{Type: "error", Error: "target is not reachable"})
		return
	}
	timer := time.NewTimer(acceptTimeout)
	defer timer.Stop()
	var other endpoint
	select {
	case other = <-ci.ready:
	case <-timer.C:
	case <-s.ctx.Done():
	}
	if other.conn == nil {
		s.mu.Lock()
		_, waiting := s.pending[id]
		delete(s.pending, id)
		s.mu.Unlock()
		if waiting {
			s.reply(c, Message{Type: "error", Error: "target did not accept"})
			return
		}
		other = <-ci.ready
	}
	defer s.release(other.raw)
	if s.ctx.Err() != nil {
		return
	}
	s.mu.Lock()
	s.active++
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.active--
		s.mu.Unlock()
	}()
	if s.reply(c, Message{Type: "ok"}) != nil || s.reply(other.conn, Message{Type: "ok"}) != nil {
		return
	}
	s.cfg.Logger.Info("relay circuit opened", "from", from, "to", target)
	s.pipe(c, other.conn)
	s.cfg.Logger.Info("relay circuit closed", "from", from, "to", target)
}

func (s *Server) acceptCircuit(c, raw net.Conn, id, circuitID string) bool {
	s.mu.Lock()
	ci := s.pending[circuitID]
	if ci == nil || ci.target != id {
		s.mu.Unlock()
		s.reply(c, Message{Type: "error", Error: "unknown circuit"})
		return false
	}
	delete(s.pending, circuitID)
	ci.ready <- endpoint{conn: c, raw: raw}
	s.mu.Unlock()
	return true
}

func (s *Server) pipe(a, b net.Conn) {
	var last atomic.Int64
	last.Store(time.Now().UnixNano())
	var once sync.Once
	stop := func() { a.Close(); b.Close() }
	var wg sync.WaitGroup
	copyFrom := func(dst, src net.Conn) {
		defer wg.Done()
		defer once.Do(stop)
		buf := make([]byte, 32<<10)
		for {
			src.SetReadDeadline(time.Now().Add(s.cfg.IdleTimeout))
			n, e := src.Read(buf)
			if n > 0 {
				last.Store(time.Now().UnixNano())
				dst.SetWriteDeadline(time.Now().Add(s.cfg.IdleTimeout))
				if _, we := dst.Write(buf[:n]); we != nil {
					return
				}
				s.bytes.Add(uint64(n))
			}
			if e != nil {
				var ne net.Error
				if errors.As(e, &ne) && ne.Timeout() && time.Since(time.Unix(0, last.Load())) < s.cfg.IdleTimeout {
					continue
				}
				return
			}
		}
	}
	wg.Add(2)
	go copyFrom(a, b)
	copyFrom(b, a)
	wg.Wait()
}
