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
- MeshLink does not traverse NAT. Peers behind routers need port forwarding or a VPN.

If a node crashes, its socket file may be left behind. Make sure the node is
stopped, then delete the file.

## Registry server (optional)

The registry is a small address book: peers register their address, and others
look it up. Messages never go through it.

**1. Create a secret token file:**

```sh
openssl rand -hex 32 > registry.token && chmod 600 registry.token
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

**4. Use it from Go:**

```go
reg, _ := meshlink.NewRegistryClient("http://127.0.0.1:8081", token)
serverNode.Register(ctx, reg, serverNode.Address(), time.Minute) // renew before it expires
clientNode.DiscoverAndConnect(ctx, reg, serverNode.ID())
```

Limitations:

- Data is kept **in memory only**. It is lost on restart and not shared between
  multiple registry servers.
- An entry disappears when its lease ends unless you call `Register` again.
- The CLI does not register with the registry automatically.

### Registry HTTP API

Every request needs the header `Authorization: Bearer <token>`.

| Request | Body / response |
|---|---|
| `PUT /v1/peers/{id}` | Body: `{"address":"host:port","lease_ns":60000000000}`. Returns 204 |
| `GET /v1/peers/{id}` | Returns `{"id":...,"address":...,"expires_at":...}`, or 404 if missing |
| `DELETE /v1/peers/{id}` | Returns 204 |
| `GET /healthz`, `GET /readyz` | No token needed. Returns `{"status":"ok"}` |

## Docker

```sh
docker build -f deployments/docker/Dockerfile -t meshlink .
docker run --rm meshlink --help
```

The image contains both binaries and runs as a non-root user (UID 10001).

`deployments/compose/compose.yml` runs a node and a registry with **Linux host
networking**. Before starting it, put `node.json`, `node.key` and `registry.token`
in `deployments/compose/data/`, with `key_path` set to `/data/node.key`.

> The Docker files have not yet been tested end to end.
