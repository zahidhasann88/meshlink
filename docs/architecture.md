# Architecture

## The big picture

```
 your app / CLI
       │
 ┌─────▼──────────────────────────────┐
 │ Node                               │  lifecycle, peers, limits, handlers
 │  ├─ session per peer               │  one reader + one writer goroutine
 │  ├─ worker pool                    │  runs your handler
 │  └─ pending requests               │  matches replies to requests
 ├────────────────────────────────────┤
 │ Protocol                           │  JSON messages + length-prefixed frames
 ├────────────────────────────────────┤
 │ Transport (TCP + TLS 1.3)          │  connect, listen, verify identity
 └────────────────────────────────────┘
```

## Code layout

| Package | Responsibility |
|---|---|
| `pkg/meshlink` | The public API. Start here |
| `internal/engine` | The node: connections, sessions, queues, workers, requests |
| `internal/transport/tcp` | TCP + mutual TLS 1.3 and the identity check |
| `internal/protocol` | Message format and framing |
| `internal/identity` | Keys and peer IDs |
| `internal/discovery` | Registry (in-memory store, HTTP server and client) |
| `internal/app` | The CLI and its local control socket |
| `internal/fsperm`, `internal/limits` | Small helpers for file permissions and connection limits |
| `cmd/meshlink`, `cmd/meshlink-registry` | The two executables |

## Life of a message

1. `node.Send` encodes the message and puts it in that peer's **outgoing queue**.
   If the queue is full, it fails right away.
2. The peer's **writer goroutine** writes it to the TLS connection.
3. On the other side, the **reader goroutine** reads a complete frame and checks it.
4. Replies go straight to the waiting `Request` call. Other messages go to a
   shared **handler queue**.
5. A fixed pool of **workers** runs your handler. For requests, the reply is sent
   back.

Because every queue and pool has a fixed size, memory use stays bounded. A peer
that sends faster than you can handle is disconnected instead of filling memory.

## Node lifecycle

`new → starting → running → closing → closed`

`Close()` stops the listener, closes all connections, fails any waiting requests,
and waits for every goroutine to finish. Calling it again just waits for the
first call to complete.

## Duplicate connections

If two nodes connect to each other at the same moment, only one connection is
kept. Both sides use the same rule (comparing peer IDs) to pick which one, so
they always agree.

## Custom transports

TCP+TLS is the only built-in transport. You can plug in your own by implementing:

```go
type Transport interface {
	Name() string
	Listen(ctx context.Context, addr string) (net.Listener, error)
	Dial(ctx context.Context, addr string) (net.Conn, error)
	Authenticate(ctx context.Context, raw net.Conn, creds Credentials, expectedID string) (Conn, error)
}
```

`Authenticate` **must** encrypt the connection and verify the peer's identity
against `creds.AllowedPeers`. Returning an unverified ID would break all security.
[`examples/custom-transport`](../examples/custom-transport/main.go) shows how to
wrap the TCP transport safely.

Not available yet: WebSocket, QUIC, UDP and WebRTC transports, relays, and NAT
traversal.
