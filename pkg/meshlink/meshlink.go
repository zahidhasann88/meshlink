package meshlink

import (
	"github.com/zahidhasann88/meshlink/internal/discovery"
	"github.com/zahidhasann88/meshlink/internal/engine"
	"github.com/zahidhasann88/meshlink/internal/identity"
	"github.com/zahidhasann88/meshlink/internal/protocol"
	"github.com/zahidhasann88/meshlink/internal/transport"
	"github.com/zahidhasann88/meshlink/internal/transport/tcp"
)

type Node = engine.Node
type Config = engine.Config
type Identity = identity.Identity
type Handler = engine.Handler
type Event = engine.Event
type PeerInfo = engine.PeerInfo
type Diagnostics = engine.Diagnostics
type RetryPolicy = engine.RetryPolicy
type Transport = transport.Transport
type Credentials = transport.Credentials
type Conn = transport.Conn
type TCPTransport = tcp.Transport

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

type Discovery = discovery.Discovery
type PeerRecord = discovery.Record
type MemoryRegistry = discovery.Memory
type RegistryClient = discovery.Client

var ErrRegistrationNotFound = discovery.ErrNotFound

func NewNode(c Config) (*Node, error) { return engine.NewNode(c) }

func GenerateIdentity() (*Identity, error) { return identity.Generate() }

func ParseIdentity(b []byte) (*Identity, error) { return identity.Parse(b) }

func LoadIdentity(path string) (*Identity, error) { return identity.Load(path) }

func ValidPeerID(id string) bool { return identity.ValidID(id) }

func ValidateConfig(c Config) error { return engine.ValidateConfig(c) }

func ParseConfig(b []byte) (Config, error) { return engine.ParseConfig(b) }

func NewMemoryRegistry(capacity int) (*MemoryRegistry, error) { return discovery.NewMemory(capacity) }

func NewRegistryClient(endpoint, token string) (*RegistryClient, error) {
	return discovery.NewClient(endpoint, token)
}
