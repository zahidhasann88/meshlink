# Go API guide

```go
import "github.com/zahidhasann88/meshlink/pkg/meshlink"
```

Only `pkg/meshlink` is public. Everything under `internal/` can change at any time.
The API is v0.x, so breaking changes are possible and are listed in the
[CHANGELOG](../CHANGELOG.md).

## Identities

An identity is an Ed25519 private key. Its **peer ID** is a 64-character hex string
that you share with other peers.

```go
id, err := meshlink.GenerateIdentity()    // new random identity
err = id.Save("node.key")                 // write it once (never overwrites)
id, err = meshlink.LoadIdentity("node.key")
fmt.Println(id.ID())                       // the peer ID to share
```

| Function | Purpose |
|---|---|
| `GenerateIdentity()` | Create a new random identity |
| `LoadIdentity(path)` / `ParseIdentity(pem)` | Read an existing key |
| `id.Save(path)` | Save to a new file, readable only by you |
| `id.ID()` | The peer ID to share |
| `ValidPeerID(s)` | Check that a string *looks* like a peer ID (this proves nothing about who owns it) |

## Creating and running a node

```go
node, err := meshlink.NewNode(meshlink.Config{
	KeyPath:       "node.key",
	ListenAddress: "127.0.0.1:7001",
	AllowedPeers:  []string{otherPeerID},
})
if err := node.Start(ctx); err != nil { ... }
defer node.Close()
```

- `NewNode` checks the config and loads the key. It does not open any sockets.
- `Start` begins listening. Cancelling `ctx` shuts the node down.
- `Close` is safe to call many times, and from many goroutines. It waits until
  everything has stopped.
- A node can only be started once.

If you leave out `Identity` and `KeyPath`, the node gets a temporary identity that
changes on every run. See [configuration](configuration.md) for all settings.

## Connecting to peers

```go
err := node.Connect(ctx, "127.0.0.1:7002", peerID)
```

- `peerID` must be in your `AllowedPeers`. The connection fails if the other side
  presents a different identity.
- The other side must also allow **your** ID.
- Use `node.ConnectWithRetry(ctx, addr, peerID, meshlink.RetryPolicy{...})` to retry
  with exponential backoff. It stops right away on authentication errors.
- `node.Disconnect(peerID)` closes a connection. `node.Peers()` lists connected peers.

## Sending messages

**Handle incoming messages** with one handler per node:

```go
node.Handle(func(ctx context.Context, from string, msg []byte) ([]byte, error) {
	// from is the verified peer ID of the sender
	return []byte("reply"), nil // the reply is used only for requests
})
```

The handler can run many times at once, so it must be safe for concurrent use.
It must also stop when `ctx` is cancelled. Returned errors and panics are sent to
the caller as a generic failure, and the error text is never shared.

**Send** methods:

| Method | What success means |
|---|---|
| `Send(ctx, peerID, data)` | The message was fully written to the connection. It does **not** confirm the peer processed it. |
| `Request(ctx, peerID, data)` | The peer's handler ran and replied. Returns the reply. |
| `Broadcast(ctx, data)` | Calls `Send` for every connected peer. Returns a `map[peerID]error`. |

Things to know:

- MeshLink does not retry or store messages. If a request times out, the peer may
  still have processed it. Make important operations safe to repeat.
- If a peer's outgoing queue is full, `Send` fails right away with `ErrBackpressure`.
- `Request` gives up after `RequestTimeout` (default 30s) even if `ctx` has no deadline.

## Events and diagnostics

```go
for ev := range node.Events() {   // "connected" / "disconnected"
	log.Println(ev.PeerID, ev.State)
}
d := node.Diagnostics()            // peers, bytes, timeouts, last error...
```

The events channel holds up to 128 items. If you don't read it, newer events are
dropped, and `Diagnostics().DroppedEvents` counts them.

## Errors

Check errors with `errors.Is(err, meshlink.ErrX)`:

| Error | Meaning |
|---|---|
| `ErrNotConnected` | No connection to that peer |
| `ErrAuthentication` | The peer is not allowed, or presented the wrong identity |
| `ErrBackpressure` | A queue or the pending-request limit is full. Retry later |
| `ErrPeerLimit` | Too many connections or handshakes |
| `ErrRemote` | The peer's handler failed |
| `ErrDuplicate` | Already connected to that peer |
| `ErrFrameTooLarge` | The message is larger than `MaxFrameSize` |
| `ErrNotStarted` / `ErrClosed` | The node is not running |

## Discovery (optional)

A registry stores `peerID → address` so nodes can find each other.

```go
reg, _ := meshlink.NewRegistryClient("https://registry.example.com", token)
node.Register(ctx, reg, "203.0.113.5:7001", time.Minute)   // call again to renew
node.DiscoverAndConnect(ctx, reg, peerID)
```

- Use `NewMemoryRegistry(capacity)` for an in-process registry.
- The registry only gives you an address. The peer still has to pass the normal
  identity check.
- See [deployment](deployment.md) to run the registry server.

## Custom transports

`Config.Transport` accepts any `meshlink.Transport`. TCP+TLS is the default and the
only built-in transport. See [architecture](architecture.md#custom-transports).
