# Configuration

From Go, pass a `meshlink.Config` struct. From the CLI, use a JSON file with
`--config`, plus optional environment variables and flags.

## Example config file

```json
{
  "key_path": "node.key",
  "listen_address": "127.0.0.1:7001",
  "allowed_peers": ["<peer ID of a node you trust>"]
}
```

Anything you leave out uses its default. A full example is in
[example-config.json](example-config.json).

- File paths are relative to the folder you run the command from, **not** the
  config file's folder.
- Unknown fields are an error, so typos are caught.
- Run `meshlink config validate --config node.json` to check a file.

## Settings

| JSON field | Default | What it controls |
|---|---|---|
| `key_path` | none | Path to your private key file (required by the CLI) |
| `listen_address` | `127.0.0.1:0` | Address to listen on. Port `0` means any free port |
| `allowed_peers` | `[]` | Peer IDs allowed to connect. **Empty means nobody** |
| `known_peers` | `[]` | Peers to connect to automatically and reconnect after drops (see below) |
| `reconnect` | see below | Retry policy for `known_peers` and the relay reservation |
| `registry` | none | Registry to look up peers and register with (see below) |
| `relay` | none | Relay to stay reachable through (see below) |
| `max_peers` | 64 | Maximum connections, including ones still connecting (1–65536) |
| `max_handshakes` | 8 | Maximum connections being set up at the same time (1–1024) |
| `max_frame_size` | 1048576 (1 MiB) | Largest message on the wire, in bytes (128 B–16 MiB) |
| `queue_capacity` | 32 | Outgoing messages queued per peer (1–4096) |
| `handler_queue` | 64 | Incoming messages waiting for a handler, for all peers combined (1–65536) |
| `workers` | 4 | Handlers that can run at the same time (1–1024) |
| `max_pending` | 256 | Requests waiting for a reply (1–65536) |
| `connect_timeout` | 5s | Time limit for connecting and the TLS handshake |
| `write_timeout` | 5s | Time limit for writing one message |
| `idle_timeout` | 2m | A connection with no traffic for this long is closed |
| `request_timeout` | 30s | Maximum wait for a reply, and maximum handler run time |

Notes:

- **Timeouts in JSON are in nanoseconds.** For example, 5 seconds is `5000000000`.
  In Go, use `5 * time.Second`.
- Every timeout must be between 1ms and 24h.
- Messages are base64-encoded on the wire, so the largest payload you can send is
  about 75% of `max_frame_size`.
- There is no automatic keep-alive. If you need long quiet connections, raise
  `idle_timeout` or send messages now and then.
- Go-only fields: `Identity` (use instead of `KeyPath`), `Transport`, `Logger`.

## Automatic connections

List peers in `known_peers` and the node connects to them on start. If a
connection drops, or the peer isn't running yet, the node keeps retrying.

```json
{
  "allowed_peers": ["<B's peer ID>"],
  "known_peers": [{ "id": "<B's peer ID>", "address": "127.0.0.1:7002" }],
  "reconnect": { "attempts": 10, "initial_delay": 500000000, "max_delay": 30000000000 }
}
```

| `reconnect` field | Default | Meaning |
|---|---|---|
| `attempts` | 10 | Consecutive failures before giving up (1–100) |
| `initial_delay` | 0.5s | First wait between attempts. It doubles after each failure… |
| `max_delay` | 30s | …up to this limit. Waits are randomized so peers don't retry in sync |

Rules:

- Each known peer must also be in `allowed_peers`.
- `address` is optional when a `registry` or `relay` is configured.
- Two peers can list each other. Only one connection is kept.
- A connection that drops within `max(max_delay, 1s)` of connecting counts as a
  failure. This stops tight retry loops when the other side doesn't allow you.
- Authentication failures stop retrying immediately.
- When the node gives up, `meshlink diagnostics` shows the reason under `last error`.
- Messages that were in flight during a drop are **not** resent.

## Registry

```json
"registry": {
  "url": "https://registry.example.com",
  "token_file": "registry.token",
  "advertise_address": "203.0.113.5:7001",
  "lease": 60000000000
}
```

| Field | Default | Meaning |
|---|---|---|
| `url` | required | Registry server URL. Must be HTTPS unless it's a loopback IP |
| `token_file` | required | File holding the registry token. It must be private to you |
| `advertise_address` | none | If set, the node registers here and renews automatically |
| `lease` | 1m | How long a registration lasts (3s–24h). Renewed every lease/3 |

Without `advertise_address`, the node only looks peers up.

## Relay

```json
"relay": { "address": "relay.example.com:7100", "id": "<relay's peer ID>" }
```

The node keeps a reservation with this relay, and uses it when a direct
connection to a known peer fails. The relay's ID is pinned like any peer's.

## CLI: environment variables and flags

When the same setting is given in several places, the later one wins:
**defaults → config file → environment variables → command-line flags**.

| Environment variable | Flag | Sets |
|---|---|---|
| `MESHLINK_CONFIG` | `--config` | Config file path |
| `MESHLINK_KEY` | `--key` | `key_path` |
| `MESHLINK_LISTEN` | `--listen` | `listen_address` |
| `MESHLINK_ALLOW` | `--allow` | `allowed_peers` (comma-separated; replaces the list) |
| `MESHLINK_SOCKET` | `--socket` | Path of the local control socket |

**Default control socket:**

| Platform | Path |
|---|---|
| Linux / macOS | `/tmp/meshlink-<uid>/node.sock` |
| Windows | `%LOCALAPPDATA%\meshlink\node.sock` |

Other flags:

- `--echo` (with `node start`): reply to every request with the same text.
- `--json`: machine-readable output.

Exit code `0` means success. Exit code `1` means an error.

## Registry server

`meshlink-registry` options:

| Flag | Env variable | Default | Meaning |
|---|---|---|---|
| `--listen` | `MESHLINK_REGISTRY_LISTEN` | `127.0.0.1:8081` | Address to listen on |
| `--token-file` | `MESHLINK_REGISTRY_TOKEN_FILE` | **required** | File holding the access token (32+ characters) |
| `--capacity` | | 1024 | Maximum registered peers |
| `--tls-cert`, `--tls-key` | | none | Enable HTTPS (required unless listening on localhost) |

Built-in limits:

- 100 requests per second in total
- 128 open connections
- 4 KiB request bodies

## Relay server

`meshlink-relay` options:

| Flag | Env variable | Default | Meaning |
|---|---|---|---|
| `--key` | `MESHLINK_RELAY_KEY` | **required** | Relay identity, created with `meshlink identity create` |
| `--allow` | `MESHLINK_RELAY_ALLOW` | **required** | Comma-separated peer IDs that may use the relay |
| `--listen` | `MESHLINK_RELAY_LISTEN` | `127.0.0.1:7100` | Address to listen on |
| `--max-circuits` | | 256 | Relayed connections in total |
| `--max-per-peer` | | 16 | Relayed connections per peer |
| `--idle-timeout` | | 2m | Close relayed connections idle this long |

## Private files

Key files and registry token files must be readable only by you:

- **Linux/macOS:** `chmod 600 FILE`.
- **Windows:** only you, SYSTEM and Administrators may have access.
  `meshlink identity create` sets this automatically. For other files, run
  `icacls FILE /inheritance:r /grant:r "%USERNAME%:F"`.
