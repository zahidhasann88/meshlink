# Wire protocol (v2, compatible with v1)

This page is for anyone building a compatible client. You don't need it to use MeshLink.

## 1. Secure connection

- TCP with **mutual TLS 1.3**. Peers offer ALPN `meshlink/2` and `meshlink/1`, and use
  the highest version both support. v0.1 and v0.2 nodes only speak `meshlink/1`.
- Each side presents one self-signed **Ed25519** certificate
- Peer ID = lowercase hex SHA-256 of the raw 32-byte Ed25519 public key
- The receiving side verifies the peer ID against its allowlist

## 2. Framing

Each message on the connection is sent as one frame:

```
┌──────────────────────────┬─────────────────────────┐
│ length: 4 bytes          │ JSON message            │
│ unsigned, big-endian     │ (exactly length bytes)  │
└──────────────────────────┴─────────────────────────┘
```

- `length` must be greater than 0 and at most the receiver's `max_frame_size`
  (default 1 MiB).
- The connection is closed if a frame is too large or invalid.

## 3. Message

```json
{"version":1,"type":"request","id":"00000000000000000000000000000001","payload":"aGVsbG8="}
```

(`payload` is `hello` in base64.)

| Field | Required | Value |
|---|---|---|
| `version` | yes | The negotiated version: `1` or `2`. Any other value closes the connection |
| `type` | yes | `data`, `request`, `response`, `error`, and on v2 also `ping` and `pong` |
| `id` | yes | 32 lowercase hex characters (random, 128-bit) |
| `payload` | no | Bytes, base64-encoded |
| `error` | only for `error` | Always `"handler_failed"` |

### Message types

| Type | Sent when | Reply |
|---|---|---|
| `data` | One-way message | None |
| `request` | Asking for a reply | `response` or `error` with the **same id** |
| `response` | Handler succeeded | n/a |
| `error` | Handler failed (no payload, no details) | n/a |
| `ping` | v2 only. Keepalive, sent every `idle_timeout / 3` | `pong` with the **same id** |
| `pong` | v2 only. Answer to a ping (no payload) | n/a |

### Rules

- Unknown fields, duplicate fields, unknown types and unknown versions are
  rejected, and the connection is closed.
- The sender's identity comes from TLS, never from the message itself.
- A reply is matched only on the same connection it was requested on. Late or
  unknown replies are ignored.
- There are no acknowledgements, retries or duplicate detection. Requests could
  run twice if the application sends them twice.

## 4. Relay control messages

A relay speaks the same framing (section 2), with JSON control messages of at most
1024 bytes over mutual TLS 1.3. Peers authenticate the relay by its pinned peer ID,
and the relay only accepts peers on its allowlist.

| Message | Direction | Meaning |
|---|---|---|
| `{"type":"reserve"}` | peer → relay | Keep this connection open so others can reach me |
| `{"type":"ping"}` / `{"type":"pong"}` | peer ↔ relay | Keep a reservation alive (every 20s; dropped after 60s of silence) |
| `{"type":"connect","target":ID}` | peer → relay | Connect me to peer ID |
| `{"type":"incoming","from":ID,"circuit":C}` | relay → target | Peer ID wants to connect |
| `{"type":"accept","circuit":C}` | target → relay | On a **new** connection: accept circuit C |
| `{"type":"ok"}` | relay → peer | Request accepted |
| `{"type":"error","error":"..."}` | relay → peer | Request refused |

`C` is 32 lowercase hex characters. After both sides receive `ok`, the relay copies
raw bytes between the two connections. The peers then start a normal MeshLink
connection (sections 1–3) inside them, so the relay only sees encrypted data.
