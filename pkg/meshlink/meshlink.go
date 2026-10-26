// Package meshlink connects Go programs peer to peer over mutual TLS 1.3.
// Nodes accept only peers listed in Config.AllowedPeers. See README.md.
package meshlink

import (
	"context"

	"github.com/zahidhasann88/meshlink/internal/discovery"
	"github.com/zahidhasann88/meshlink/internal/engine"
	"github.com/zahidhasann88/meshlink/internal/identity"
	"github.com/zahidhasann88/meshlink/internal/protocol"
	"github.com/zahidhasann88/meshlink/internal/relay"
	"github.com/zahidhasann88/meshlink/internal/transport"
	"github.com/zahidhasann88/meshlink/internal/transport/tcp"
)

// Node is a running MeshLink peer. Create one with NewNode.
type Node = engine.Node

// Config configures a Node. Zero values select documented defaults.
type Config = engine.Config

// Identity is a node's Ed25519 private key. Never log or share it.
type Identity = identity.Identity

// Handler processes incoming messages and requests from verified peers.
type Handler = engine.Handler

// Event reports a peer connecting or disconnecting.
type Event = engine.Event

// PeerInfo describes one connected peer.
type PeerInfo = engine.PeerInfo

// Diagnostics is a snapshot of a node's state and counters.
type Diagnostics = engine.Diagnostics

// RetryPolicy controls exponential backoff for connection attempts.
type RetryPolicy = engine.RetryPolicy

// KnownPeer is a peer a node connects to automatically and keeps connected.
type KnownPeer = engine.KnownPeer

// RegistryConfig makes a node use, and optionally register with, a registry server.
type RegistryConfig = engine.RegistryConfig

// RelayConfig is the relay a node stays reachable through and falls back to.
type RelayConfig = engine.RelayConfig

// RelayServer forwards end-to-end encrypted traffic between allowed peers.
type RelayServer = relay.Server

// RelayServerConfig configures a RelayServer. Zero values select defaults.
type RelayServerConfig = relay.Config

// RelayStats is a snapshot of a RelayServer's activity.
type RelayStats = relay.Stats

// Transport is the extension point for custom secure stream transports.
type Transport = transport.Transport

// Credentials are passed to a Transport during authentication.
type Credentials = transport.Credentials

// Conn is an authenticated, encrypted connection to a verified peer.
type Conn = transport.Conn

// TCPTransport is the default transport: TCP with mutual TLS 1.3.
type TCPTransport = tcp.Transport

// Errors returned by Node operations. Compare them with errors.Is.
var (
	ErrClosed         = engine.ErrClosed
	ErrNotStarted     = engine.ErrNotStarted
	ErrNotConnected   = engine.ErrNotConnected
	ErrBackpressure   = engine.ErrBackpressure
	ErrPeerLimit      = engine.ErrPeerLimit
	ErrDuplicate      = engine.ErrDuplicate
	ErrRemote         = engine.ErrRemote
	ErrAuthentication = tcp.ErrAuthentication
	ErrFrameTooLarge  = protocol.ErrFrameTooLarge
)

// Discovery looks up peer addresses. Results are hints, not proof of identity:
// connections are still authenticated against AllowedPeers.
type Discovery = discovery.Discovery

// PeerRecord is a signed peer registration. Verify checks that the peer
// itself signed it.
type PeerRecord = discovery.Record

// MemoryRegistry is an in-process Discovery implementation with leases.
type MemoryRegistry = discovery.Memory

// RegistryClient is a Discovery implementation backed by a meshlink-registry server.
type RegistryClient = discovery.Client

// ErrRegistrationNotFound means no unexpired registration exists for a peer.
var ErrRegistrationNotFound = discovery.ErrNotFound

// IsRequest reports whether a Handler call is for a request (true) or a
// one-way message (false).
func IsRequest(ctx context.Context) bool { return engine.IsRequest(ctx) }

// NewNode validates the configuration and loads or generates the identity.
// It does not open any sockets; call Start for that.
func NewNode(c Config) (*Node, error) { return engine.NewNode(c) }

// GenerateIdentity creates a new random Ed25519 identity.
func GenerateIdentity() (*Identity, error) { return identity.Generate() }

// ParseIdentity reads one PKCS#8 PEM-encoded Ed25519 private key.
func ParseIdentity(b []byte) (*Identity, error) { return identity.Parse(b) }

// LoadIdentity reads a key file saved with Identity.Save. On Unix the file must
// be readable only by its owner.
func LoadIdentity(path string) (*Identity, error) { return identity.Load(path) }

// ValidPeerID reports whether id is syntactically a peer ID (64 lowercase hex
// characters). It does not prove who owns the ID.
func ValidPeerID(id string) bool { return identity.ValidID(id) }

// ValidateConfig checks a configuration without reading keys or opening sockets.
func ValidateConfig(c Config) error { return engine.ValidateConfig(c) }

// ParseConfig parses a strict JSON configuration. Unknown fields are rejected
// and durations are integer nanoseconds.
func ParseConfig(b []byte) (Config, error) { return engine.ParseConfig(b) }

// NewMemoryRegistry creates an in-process registry holding at most capacity peers.
func NewMemoryRegistry(capacity int) (*MemoryRegistry, error) { return discovery.NewMemory(capacity) }

// NewRelayServer creates a relay. Call Start to begin listening.
func NewRelayServer(c RelayServerConfig) (*RelayServer, error) { return relay.NewServer(c) }

// NewRegistryClient creates a client for a meshlink-registry server. The
// endpoint must use HTTPS unless it is a numeric loopback address.
func NewRegistryClient(endpoint, token string) (*RegistryClient, error) {
	return discovery.NewClient(endpoint, token)
}
