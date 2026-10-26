package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zahidhasann88/meshlink/internal/identity"
	"github.com/zahidhasann88/meshlink/internal/protocol"
	"github.com/zahidhasann88/meshlink/internal/transport"
	"github.com/zahidhasann88/meshlink/internal/transport/tcp"
)

var (
	ErrClosed       = errors.New("node or peer closed")
	ErrNotStarted   = errors.New("node not running")
	ErrNotConnected = errors.New("peer not connected")
	ErrBackpressure = errors.New("bounded queue or pending request limit reached")
	ErrPeerLimit    = errors.New("connection or handshake limit reached")
	ErrDuplicate    = errors.New("duplicate connection")
	ErrRemote       = errors.New("remote handler failed")
)

type Handler func(ctx context.Context, peer string, payload []byte) ([]byte, error)
type Event struct {
	PeerID string
	State  string
	Time   time.Time
}
type PeerInfo struct {
	ID            string    `json:"id"`
	Address       string    `json:"address"`
	State         string    `json:"state"`
	ConnectedAt   time.Time `json:"connected_at"`
	LastRead      time.Time `json:"last_read"`
	LastWrite     time.Time `json:"last_write"`
	BytesSent     uint64    `json:"bytes_sent"`
	BytesReceived uint64    `json:"bytes_received"`
	QueueDepth    int       `json:"queue_depth"`
}
type Diagnostics struct {
	ID                  string        `json:"id"`
	State               string        `json:"state"`
	Transport           string        `json:"transport"`
	Address             string        `json:"address"`
	Uptime              time.Duration `json:"uptime_ns"`
	Peers               []PeerInfo    `json:"peers"`
	BytesSent           uint64        `json:"bytes_sent"`
	BytesReceived       uint64        `json:"bytes_received"`
	RequestTimeouts     uint64        `json:"request_timeouts"`
	ReconnectAttempts   uint64        `json:"reconnect_attempts"`
	RejectedConnections uint64        `json:"rejected_connections"`
	DroppedEvents       uint64        `json:"dropped_events"`
	LastError           string        `json:"last_error,omitempty"`
}
type result struct {
	payload []byte
	err     error
}
type pending struct {
	peer *session
	ch   chan result
}
type job struct {
	ctx  context.Context
	b    []byte
	done chan error
}
type delivery struct {
	s *session
	m protocol.Message
}
type session struct {
	n                   *Node
	id                  string
	conn                transport.Conn
	outbound            bool
	ctx                 context.Context
	cancel              context.CancelFunc
	queue               chan job
	once                sync.Once
	since               time.Time
	sent, received      atomic.Uint64
	lastRead, lastWrite atomic.Int64
}

type Node struct {
	cfg                                                  Config
	creds                                                transport.Credentials
	mu                                                   sync.Mutex
	state                                                string
	ctx                                                  context.Context
	cancel                                               context.CancelFunc
	ln                                                   net.Listener
	address                                              string
	started                                              time.Time
	peers                                                map[string]*session
	raw                                                  map[net.Conn]struct{}
	pending                                              map[string]pending
	handler                                              Handler
	lastError                                            string
	slots, handshakes                                    chan struct{}
	work                                                 chan delivery
	events                                               chan Event
	done                                                 chan struct{}
	wg                                                   sync.WaitGroup
	sent, received, timeouts, retries, rejected, dropped atomic.Uint64
}

func NewNode(c Config) (*Node, error) {
	c, e := c.normalized()
	if e != nil {
		return nil, e
	}
	if c.Identity != nil && c.KeyPath != "" {
		return nil, errors.New("configure Identity or KeyPath, not both")
	}
	if c.Identity == nil {
		if c.KeyPath != "" {
			c.Identity, e = identity.Load(c.KeyPath)
		} else {
			c.Identity, e = identity.Generate()
		}
		if e != nil {
			return nil, fmt.Errorf("load identity: %w", e)
		}
	}
	if !c.Identity.Valid() {
		return nil, errors.New("invalid or zero-value identity")
	}
	allowed := make(map[string]bool)
	for _, id := range c.AllowedPeers {
		allowed[id] = true
	}
	c.AllowedPeers = append([]string(nil), c.AllowedPeers...)
	return &Node{cfg: c, creds: transport.Credentials{Identity: c.Identity, AllowedPeers: allowed}, state: "new", peers: map[string]*session{}, raw: map[net.Conn]struct{}{}, pending: map[string]pending{}, slots: make(chan struct{}, c.MaxPeers), handshakes: make(chan struct{}, c.MaxHandshakes), work: make(chan delivery, c.HandlerQueue), events: make(chan Event, 128), done: make(chan struct{})}, nil
}
func (n *Node) ID() string      { return n.cfg.Identity.ID() }
func (n *Node) Address() string { n.mu.Lock(); defer n.mu.Unlock(); return n.address }

func (n *Node) Events() <-chan Event { return n.events }
func (n *Node) Handle(h Handler)     { n.mu.Lock(); n.handler = h; n.mu.Unlock() }
func (n *Node) Start(ctx context.Context) error {
	n.mu.Lock()
	if n.state != "new" {
		n.mu.Unlock()
		return errors.New("node can only be started once")
	}
	if e := ctx.Err(); e != nil {
		n.mu.Unlock()
		return e
	}
	n.state = "starting"
	n.ctx, n.cancel = context.WithCancel(ctx)
	n.wg.Add(1)
	base := n.ctx
	n.mu.Unlock()
	defer n.wg.Done()
	ln, e := n.cfg.Transport.Listen(base, n.cfg.ListenAddress)
	n.mu.Lock()
	if n.state != "starting" {
		n.mu.Unlock()
		if ln != nil {
			ln.Close()
		}
		return ErrClosed
	}
	if e != nil {
		n.cancel()
		n.state = "new"
		n.mu.Unlock()
		return fmt.Errorf("listen: %w", e)
	}
	n.ln = ln
	n.address = ln.Addr().String()
	n.started = time.Now()
	n.state = "running"
	n.wg.Add(1 + n.cfg.Workers)
	go n.accept()
	for i := 0; i < n.cfg.Workers; i++ {
		go n.worker()
	}
	context.AfterFunc(base, func() { n.Close() })
	n.mu.Unlock()
	return nil
}
func (n *Node) note(err error) {
	if err == nil {
		return
	}
	n.mu.Lock()
	n.lastError = err.Error()
	n.mu.Unlock()
	n.cfg.Logger.Debug("connection ended", "error", err)
}
func (n *Node) emit(id, state string) {
	select {
	case n.events <- Event{id, state, time.Now()}:
	default:
		n.dropped.Add(1)
	}
}

func (n *Node) reserve() error {
	if n.state != "running" {
		return ErrNotStarted
	}
	select {
	case n.slots <- struct{}{}:
	default:
		return ErrPeerLimit
	}
	select {
	case n.handshakes <- struct{}{}:
	default:
		<-n.slots
		return ErrPeerLimit
	}
	n.wg.Add(1)
	return nil
}
func (n *Node) accept() {
	defer n.wg.Done()
	for {
		raw, e := n.ln.Accept()
		if e != nil {
			if n.ctx.Err() == nil {
				n.note(e)
			}
			return
		}
		n.mu.Lock()
		e = n.reserve()
		if e == nil {
			n.raw[raw] = struct{}{}
		}
		n.mu.Unlock()
		if e != nil {
			n.rejected.Add(1)
			raw.Close()
			continue
		}
		go func() {
			defer n.wg.Done()
			_, e := n.establish(n.ctx, raw, "", false)
			if e != nil {
				n.note(e)
				n.rejected.Add(1)
			}
		}()
	}
}

func (n *Node) Connect(ctx context.Context, address, expected string) error {
	if !identity.ValidID(expected) {
		return errors.New("expected peer ID is required")
	}
	if !n.creds.AllowedPeers[expected] {
		return fmt.Errorf("%w: expected identity not authorized", tcp.ErrAuthentication)
	}
	n.mu.Lock()
	e := n.reserve()
	if e != nil {
		n.mu.Unlock()
		return e
	}
	base := n.ctx
	n.mu.Unlock()
	defer n.wg.Done()
	dialCtx, cancel := context.WithTimeout(ctx, n.cfg.ConnectTimeout)
	defer cancel()
	stop := context.AfterFunc(base, cancel)
	defer stop()
	raw, e := n.cfg.Transport.Dial(dialCtx, address)
	if e != nil {
		<-n.handshakes
		<-n.slots
		n.note(e)
		return fmt.Errorf("dial: %w", e)
	}
	n.mu.Lock()
	if n.state != "running" {
		n.mu.Unlock()
		raw.Close()
		<-n.handshakes
		<-n.slots
		return ErrClosed
	}
	n.raw[raw] = struct{}{}
	n.mu.Unlock()
	_, e = n.establish(dialCtx, raw, expected, true)
	if e != nil {
		n.note(e)
	}
	return e
}
func (n *Node) establish(ctx context.Context, raw net.Conn, expected string, out bool) (*session, error) {
	success := false
	defer func() {
		<-n.handshakes
		n.mu.Lock()
		delete(n.raw, raw)
		n.mu.Unlock()
		if !success {
			raw.Close()
			<-n.slots
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, n.cfg.ConnectTimeout)
	defer cancel()
	c, e := n.cfg.Transport.Authenticate(ctx, raw, n.creds, expected)
	if e != nil {
		return nil, e
	}
	id := c.PeerID()
	if !identity.ValidID(id) || !n.creds.AllowedPeers[id] || id == n.ID() || (out && id != expected) {
		return nil, tcp.ErrAuthentication
	}
	n.mu.Lock()
	if n.state != "running" {
		n.mu.Unlock()
		return nil, ErrClosed
	}
	old := n.peers[id]
	preferred := (n.ID() < id) == out
	if old != nil && (!preferred || old.outbound == out) {
		n.mu.Unlock()
		return nil, ErrDuplicate
	}
	sessionCtx, sessionCancel := context.WithCancel(n.ctx)
	s := &session{n: n, id: id, conn: c, outbound: out, ctx: sessionCtx, cancel: sessionCancel, queue: make(chan job, n.cfg.QueueCapacity), since: time.Now()}
	n.peers[id] = s
	n.wg.Add(2)
	n.emit(id, "connected")
	n.mu.Unlock()
	if old != nil {
		old.close()
	}
	success = true
	go s.writer()
	go s.reader()
	return s, nil
}
func (n *Node) Close() error {
	n.mu.Lock()
	if n.state == "closed" || n.state == "closing" {
		n.mu.Unlock()
		<-n.done
		return nil
	}
	n.state = "closing"
	if n.cancel != nil {
		n.cancel()
	}
	ln := n.ln
	raws := make([]net.Conn, 0, len(n.raw))
	for c := range n.raw {
		raws = append(raws, c)
	}
	peers := make([]*session, 0, len(n.peers))
	for _, s := range n.peers {
		peers = append(peers, s)
	}
	n.mu.Unlock()
	if ln != nil {
		ln.Close()
	}
	for _, c := range raws {
		c.Close()
	}
	for _, s := range peers {
		s.close()
	}
	n.wg.Wait()
	n.mu.Lock()
	n.state = "closed"
	close(n.events)
	close(n.done)
	n.mu.Unlock()
	return nil
}
func (s *session) close() {
	s.once.Do(func() {
		s.cancel()
		s.conn.Close()
		n := s.n
		n.mu.Lock()
		if n.peers[s.id] == s {
			delete(n.peers, s.id)
			n.emit(s.id, "disconnected")
		}
		for id, p := range n.pending {
			if p.peer == s {
				delete(n.pending, id)
				p.ch <- result{err: ErrClosed}
			}
		}
		n.mu.Unlock()
		<-n.slots
	})
}
func (n *Node) Disconnect(id string) error {
	s, e := n.peer(id)
	if e != nil {
		return e
	}
	s.close()
	return nil
}
func (n *Node) peer(id string) (*session, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.state != "running" {
		return nil, ErrNotStarted
	}
	s := n.peers[id]
	if s == nil {
		return nil, ErrNotConnected
	}
	return s, nil
}
func newID() (string, error) {
	var b [16]byte
	_, e := rand.Read(b[:])
	return hex.EncodeToString(b[:]), e
}

func (n *Node) Send(ctx context.Context, id string, payload []byte) error {
	s, e := n.peer(id)
	if e != nil {
		return e
	}
	mid, e := newID()
	if e != nil {
		return e
	}
	return s.send(ctx, protocol.Message{Version: 1, Type: "data", ID: mid, Payload: payload})
}
func (s *session) send(ctx context.Context, m protocol.Message) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if s.ctx.Err() != nil {
		return ErrClosed
	}
	b, e := protocol.Encode(m, s.n.cfg.MaxFrameSize)
	if e != nil {
		return e
	}
	j := job{ctx, b, make(chan error, 1)}
	select {
	case <-s.ctx.Done():
		return ErrClosed
	case s.queue <- j:
	default:
		return ErrBackpressure
	}
	select {
	case e := <-j.done:
		return e
	case <-ctx.Done():
		return ctx.Err()
	case <-s.ctx.Done():
		return ErrClosed
	}
}
func (s *session) writer() {
	defer s.n.wg.Done()
	defer s.close()
	for {
		select {
		case <-s.ctx.Done():
			return
		case j := <-s.queue:
			if e := j.ctx.Err(); e != nil {
				j.done <- e
				continue
			}
			deadline := time.Now().Add(s.n.cfg.WriteTimeout)
			if d, ok := j.ctx.Deadline(); ok && d.Before(deadline) {
				deadline = d
			}
			s.conn.SetWriteDeadline(deadline)
			e := protocol.WriteFrame(s.conn, j.b, s.n.cfg.MaxFrameSize)
			j.done <- e
			if e != nil {
				s.n.note(e)
				return
			}
			size := uint64(len(j.b) + 4)
			s.sent.Add(size)
			s.n.sent.Add(size)
			s.lastWrite.Store(time.Now().UnixNano())
		}
	}
}
func (s *session) reader() {
	defer s.n.wg.Done()
	defer s.close()
	for {
		s.conn.SetReadDeadline(time.Now().Add(s.n.cfg.IdleTimeout))
		b, e := protocol.ReadFrame(s.conn, s.n.cfg.MaxFrameSize)
		if e != nil {
			if s.ctx.Err() == nil {
				s.n.note(e)
			}
			return
		}
		size := uint64(len(b) + 4)
		s.received.Add(size)
		s.n.received.Add(size)
		s.lastRead.Store(time.Now().UnixNano())
		m, e := protocol.Decode(b)
		if e != nil {
			s.n.note(e)
			return
		}
		if m.Type == "response" || m.Type == "error" {
			s.n.mu.Lock()
			p, ok := s.n.pending[m.ID]
			if ok && p.peer == s {
				delete(s.n.pending, m.ID)
				r := result{payload: m.Payload}
				if m.Type == "error" {
					r.err = ErrRemote
				}
				p.ch <- r
			}
			s.n.mu.Unlock()
			continue
		}
		select {
		case s.n.work <- delivery{s, m}:
		case <-s.ctx.Done():
			return
		default:
			s.n.note(ErrBackpressure)
			return
		}
	}
}
func (n *Node) worker() {
	defer n.wg.Done()
	for {
		select {
		case <-n.ctx.Done():
			return
		case d := <-n.work:
			if d.s.ctx.Err() != nil {
				continue
			}
			n.mu.Lock()
			h := n.handler
			n.mu.Unlock()
			ctx, cancel := context.WithTimeout(d.s.ctx, n.cfg.RequestTimeout)
			var payload []byte
			var e error
			if h == nil {
				e = errors.New("no handler")
			} else {
				payload, e = callHandler(h, ctx, d.s.id, d.m.Payload)
			}
			if d.m.Type == "request" {
				m := protocol.Message{Version: 1, Type: "response", ID: d.m.ID, Payload: payload}
				if e != nil {
					m.Type = "error"
					m.Payload = nil
					m.Error = "handler_failed"
				}
				if e = d.s.send(ctx, m); e != nil {
					n.note(e)
				}
			}
			cancel()
		}
	}
}
func callHandler(h Handler, ctx context.Context, id string, b []byte) (out []byte, err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("handler panic")
		}
	}()
	return h(ctx, id, b)
}
func (n *Node) Request(ctx context.Context, id string, payload []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, n.cfg.RequestTimeout)
	defer cancel()
	s, e := n.peer(id)
	if e != nil {
		return nil, e
	}
	mid, e := newID()
	if e != nil {
		return nil, e
	}
	p := pending{s, make(chan result, 1)}
	n.mu.Lock()
	if len(n.pending) >= n.cfg.MaxPending {
		n.mu.Unlock()
		return nil, ErrBackpressure
	}
	if s.ctx.Err() != nil {
		n.mu.Unlock()
		return nil, ErrClosed
	}
	n.pending[mid] = p
	n.mu.Unlock()
	defer func() { n.mu.Lock(); delete(n.pending, mid); n.mu.Unlock() }()
	if e = s.send(ctx, protocol.Message{Version: 1, Type: "request", ID: mid, Payload: payload}); e != nil {
		if errors.Is(e, context.DeadlineExceeded) {
			n.timeouts.Add(1)
		}
		return nil, e
	}
	select {
	case r := <-p.ch:
		return r.payload, r.err
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			n.timeouts.Add(1)
		}
		return nil, ctx.Err()
	case <-s.ctx.Done():
		return nil, ErrClosed
	}
}

func (n *Node) Broadcast(ctx context.Context, payload []byte) map[string]error {
	peers := n.Peers()
	out := make(map[string]error, len(peers))
	for _, p := range peers {
		out[p.ID] = n.Send(ctx, p.ID, payload)
	}
	return out
}
func timeValue(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	return time.Unix(0, v)
}
func (n *Node) Peers() []PeerInfo { n.mu.Lock(); defer n.mu.Unlock(); return n.peersLocked() }
func (n *Node) peersLocked() []PeerInfo {
	out := make([]PeerInfo, 0, len(n.peers))
	for _, s := range n.peers {
		out = append(out, PeerInfo{ID: s.id, Address: s.conn.RemoteAddr().String(), State: "connected", ConnectedAt: s.since, LastRead: timeValue(s.lastRead.Load()), LastWrite: timeValue(s.lastWrite.Load()), BytesSent: s.sent.Load(), BytesReceived: s.received.Load(), QueueDepth: len(s.queue)})
	}
	return out
}
func (n *Node) Diagnostics() Diagnostics {
	n.mu.Lock()
	defer n.mu.Unlock()
	var uptime time.Duration
	if !n.started.IsZero() {
		uptime = time.Since(n.started)
	}
	return Diagnostics{ID: n.ID(), State: n.state, Transport: n.cfg.Transport.Name(), Address: n.address, Uptime: uptime, Peers: n.peersLocked(), BytesSent: n.sent.Load(), BytesReceived: n.received.Load(), RequestTimeouts: n.timeouts.Load(), ReconnectAttempts: n.retries.Load(), RejectedConnections: n.rejected.Load(), DroppedEvents: n.dropped.Load(), LastError: n.lastError}
}
