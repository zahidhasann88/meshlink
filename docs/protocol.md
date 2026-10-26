# Wire protocol (v1)

This page is for anyone building a compatible client. You don't need it to use MeshLink.

## 1. Secure connection

- TCP with **mutual TLS 1.3** and ALPN protocol name `meshlink/1`
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
| `version` | yes | `1` |
| `type` | yes | `data`, `request`, `response` or `error` |
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

### Rules

- Unknown fields, duplicate fields, unknown types and unknown versions are
  rejected, and the connection is closed.
- The sender's identity comes from TLS, never from the message itself.
- A reply is matched only on the same connection it was requested on. Late or
  unknown replies are ignored.
- There are no acknowledgements, retries or duplicate detection. Requests could
  run twice if the application sends them twice.
