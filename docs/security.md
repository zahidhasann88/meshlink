# Security

MeshLink uses standard, well-reviewed cryptography (TLS 1.3 and Ed25519 from the
Go standard library). It has **not** had an external security audit.

## How identity works

- Each node has an **Ed25519 private key**. Keep it secret.
- The **peer ID** is the SHA-256 hash of the public key, written as 64 hex
  characters. It is safe to share.
- Saved keys are created readable only by you (`chmod 600`), and existing files
  are never overwritten. On Linux/macOS, MeshLink refuses to load a key that
  others can read.
- **Windows:** Go can't read Windows file permissions, so this check is skipped.
  Keep keys inside your user folder, which only you and administrators can read
  by default.
- Keys are stored unencrypted. Use disk encryption if you need protection at rest.
- Private keys and message contents are never logged or sent to peers.

## How trust works

1. You share peer IDs with each other through a channel you already trust.
2. Each node lists the peers it trusts in `allowed_peers`.
3. On connect, both sides prove they own their key using mutual TLS 1.3. Each side
   checks that the other's key matches an allowed peer ID.

What this means:

- An empty `allowed_peers` list rejects **everyone**.
- A peer ID on its own proves nothing. Only the TLS handshake proves identity.
  Registry entries are only address hints.
- There are no certificate authorities or domain names. Trust comes from your
  allowlist alone. (Internally, the normal TLS check is replaced by this
  peer-ID check, never just switched off.)
- Every allowed peer can call your handler. If different peers need different
  permissions, check the verified `from` ID inside your handler.

## Changing or revoking keys

There is no online revocation. To replace a key:

1. Create a new identity.
2. Update `allowed_peers` on every node that trusts it.
3. Restart those nodes.

If a key is stolen, remove its ID from every allowlist and restart. A lost key
cannot be recovered; create a new one.

## What MeshLink protects against

| Threat | Protection |
|---|---|
| Eavesdropping or tampering | TLS 1.3 encryption |
| Someone pretending to be a peer | Key-based identity check on every connection |
| Huge or malformed messages | Size checked before reading; strict format checks |
| Too many connections | Limits on connections and handshakes, plus timeouts |
| Slow or flooding peers | Bounded queues; an overloaded peer is disconnected |
| Unauthorized registry use | Token required; rate and size limits |

## What it does NOT protect against

- **Network-level denial of service.** Use a firewall and your OS limits.
- **Replayed requests.** A trusted peer can send the same request twice. Make
  important operations safe to repeat.
- **A malicious trusted peer.** Allowed peers can still use CPU and bandwidth.
- **The registry operator.** Anyone with the registry token can change any entry.
  Identity checks still stop impersonation, but a bad entry can make you connect
  to the wrong address.
- **Local users with your account (or admin/root).** They can control your node
  through its control socket.
- **Custom transports.** These run inside your process and must do their own
  encryption and authentication.

## Control socket

The CLI controls a running node through a local socket file, never over the
network.

- **Linux/macOS:** the socket's folder must be private (`chmod 700`).
- **Windows:** the socket follows its folder's permissions. The default
  `%LOCALAPPDATA%` folder is private to your account.

## Reporting a vulnerability

See [SECURITY.md](../SECURITY.md).
