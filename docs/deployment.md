# Deployment

## Running a node

`meshlink node start` runs in the foreground until you stop it:

- Press **Ctrl+C**, or
- run `meshlink node stop --socket PATH`, or
- send SIGTERM (Linux/macOS).

Shutdown is graceful. Open connections are closed cleanly before the process exits.

Use a process manager such as systemd, a Windows service wrapper or Docker to keep
it running.

**Health check:** `meshlink diagnostics --json` should report `"state":"running"`.
A node opens no HTTP or metrics port.

**Network exposure:**

- `127.0.0.1:7001` is reachable only from this machine.
- `0.0.0.0:7001` is reachable from your network. Add firewall rules.
- MeshLink does not traverse NAT by itself. Peers behind routers need port
  forwarding, a VPN, or a [relay](#relay-server-optional).

If a node crashes, its socket file may be left behind. Make sure the node is
stopped, then delete the file.

## Registry server (optional)

The registry is a small address book: peers register their address, and others
look it up. Messages never go through it.

**1. Create a secret token file:**

```sh
openssl rand -hex 32 > registry.token && chmod 600 registry.token
```

On Windows (PowerShell):

```powershell
$b = New-Object byte[] 32; [Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($b)
-join ($b | % { '{0:x2}' -f $_ }) | Set-Content registry.token
icacls registry.token /inheritance:r /grant:r "$env:USERNAME`:F"
```

**2. Run locally (plain HTTP is allowed only on localhost):**

```sh
meshlink-registry --listen 127.0.0.1:8081 --token-file registry.token
curl http://127.0.0.1:8081/healthz     # {"status":"ok"}
```

**3. Run on a network (HTTPS required):**

```sh
meshlink-registry --listen 0.0.0.0:8443 --token-file registry.token \
  --tls-cert server.crt --tls-key server.key
```

**4. Point nodes at it** with a [`registry` config section](configuration.md#registry).
Nodes with `advertise_address` register and renew automatically. Others can then
list them in `known_peers` by ID alone.

Limitations:

- Data is kept **in memory only**. It is lost on restart and not shared between
  multiple registry servers. Registered nodes re-register within a third of their lease.
- A registration is removed when the node stops, or when its lease runs out.

### Registry HTTP API

Every request needs the header `Authorization: Bearer <token>`.

| Request | Body / response |
|---|---|
| `PUT /v1/peers/{id}` | Body: a signed registration (below) plus `lease_ns`. Returns 204, or 403 for a bad signature and 409 for a stale one |
| `GET /v1/peers/{id}` | Returns `{"id":...,"address":...,"expires_at":...}`, or 404 if missing |
| `DELETE /v1/peers/{id}` | Body: a signed removal (`public_key`, `issued_at`, `signature`). Returns 204 |
| `GET /healthz`, `GET /readyz` | No token needed. Returns `{"status":"ok"}` |

Registrations are signed by the peer itself. The signature is Ed25519 over:

```
meshlink-registry-v1\n<id>\n<address>\n<issued_at as Unix nanoseconds>
```

A removal uses the same format with an empty address. The registry rejects
signatures more than 5 minutes off its clock, and anything not newer than what it
already has.

**Upgrading from v0.2:** registries and nodes must both run v0.3.0, because v0.2
nodes send unsigned registrations.

## Receiving messages from the CLI

`meshlink message listen` prints one-way messages that a running node receives,
until you press Ctrl+C. Add `--json` for one JSON object per line, for scripts.
Requests still go to `--echo`, or fail without it. Up to 4 listeners can run at
once, and a listener that falls far behind loses messages rather than slowing
the node.

## Relay server (optional)

A relay lets peers reach each other when they can't connect directly, for example
when both are behind NAT. Run it on a host both peers can reach.

```sh
meshlink identity create --key relay.key          # prints the relay's ID
meshlink-relay --key relay.key --listen 0.0.0.0:7100 --allow "<peer ID>,<peer ID>"
```

Then add `"relay": {"address": "relay-host:7100", "id": "<relay ID>"}` to each
node's config. A node behind NAT only needs outbound access to the relay.

- The relay forwards **end-to-end encrypted** traffic. It cannot read or change
  messages, but it can see which peers talk, when, and how much.
- Only peers in `--allow` can use it, and each peer still checks the other's identity.
- All relayed traffic passes through the relay host, so plan its bandwidth.

## Docker

```sh
docker build -f deployments/docker/Dockerfile --build-arg VERSION=v0.2.0 -t meshlink .
docker run --rm meshlink version
```

The image contains `meshlink`, `meshlink-registry` and `meshlink-relay`, and runs
as a non-root user (UID 10001). Use `--entrypoint meshlink-relay` (or
`meshlink-registry`) to run the servers.

`make docker-smoke` builds the image and starts a relay plus three nodes on a Docker
network. It checks a direct connection and a relayed connection, then a clean stop.
CI runs it on every push.

`deployments/compose/compose.yml` runs a node and a registry with **Linux host
networking**. Before starting it, put `node.json`, `node.key` and `registry.token`
in `deployments/compose/data/`, with `key_path` set to `/data/node.key`.
