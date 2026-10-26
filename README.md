# MeshLink

[![Go checks](https://github.com/zahidhasann88/meshlink/actions/workflows/ci.yml/badge.svg)](https://github.com/zahidhasann88/meshlink/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/zahidhasann88/meshlink.svg)](https://pkg.go.dev/github.com/zahidhasann88/meshlink/pkg/meshlink)
[![Latest release](https://img.shields.io/github/v/release/zahidhasann88/meshlink)](https://github.com/zahidhasann88/meshlink/releases/latest)

**MeshLink lets Go programs talk to each other securely, peer to peer.**

Each node has its own cryptographic identity. Nodes only talk to peers you have
explicitly allowed, and every connection is encrypted with TLS 1.3. You can use
MeshLink as a Go library or through the `meshlink` command-line tool.

![Two MeshLink peers connecting and exchanging a request](docs/demo.gif)

> **Status:** early development (v0.2.0). It works and is well tested, but it has not
> had a security audit. Don't rely on it for anything critical yet.

## Features

- **Secure by default:** mutual TLS 1.3 with Ed25519 keys. Unknown peers are rejected.
- **Messaging:** one-way messages, broadcast, and request/response with timeouts.
- **Auto-reconnect:** keeps known peers connected, with backoff, across drops and restarts.
- **Safe limits:** bounded queues, connection limits and message size limits.
- **Find peers by ID:** an optional registry server; nodes register and renew automatically.
- **Relay:** reach peers behind NAT through a relay that only forwards encrypted traffic.
- **Diagnostics:** connected peers, bytes sent and received, errors and more.
- **CLI included:** runs on Linux, macOS and Windows 10+.
- **Minimal dependencies:** only the Go standard library and `golang.org/x/sys`.

## Install

**Download a binary** for Linux, macOS or Windows from the
[latest release](https://github.com/zahidhasann88/meshlink/releases/latest),
unzip it, and put `meshlink` on your `PATH`.

**Or install with Go** (1.27 or newer):

```sh
go install github.com/zahidhasann88/meshlink/cmd/meshlink@latest
go install github.com/zahidhasann88/meshlink/cmd/meshlink-registry@latest   # optional
go install github.com/zahidhasann88/meshlink/cmd/meshlink-relay@latest      # optional
```

Or build from source:

```sh
git clone https://github.com/zahidhasann88/meshlink.git
cd meshlink
go build -o bin/ ./cmd/...
```

## Quick start: two peers talking

You'll create two nodes, **A** and **B**, let them trust each other, and send a message.

### 1. Create identities and configs

Each node gets a private key. The command prints the node's **peer ID**, which
you share with other peers (never share the key file).

**Linux / macOS**

```sh
mkdir -p local && chmod 700 local
A=$(meshlink identity create --key local/a.key)
B=$(meshlink identity create --key local/b.key)

# A allows B, and B allows A
echo "{\"key_path\":\"local/a.key\",\"listen_address\":\"127.0.0.1:7001\",\"allowed_peers\":[\"$B\"]}" > local/a.json
echo "{\"key_path\":\"local/b.key\",\"listen_address\":\"127.0.0.1:7002\",\"allowed_peers\":[\"$A\"]}" > local/b.json
```

**Windows (PowerShell)**

```powershell
mkdir local
$A = meshlink identity create --key local\a.key
$B = meshlink identity create --key local\b.key

# A allows B, and B allows A
@{ key_path = "local/a.key"; listen_address = "127.0.0.1:7001"; allowed_peers = @($B) } | ConvertTo-Json | Set-Content local\a.json
@{ key_path = "local/b.key"; listen_address = "127.0.0.1:7002"; allowed_peers = @($A) } | ConvertTo-Json | Set-Content local\b.json
```

### 2. Start both nodes

Open two terminals in the same folder. `--echo` makes the node reply to requests
with the same text, which is handy for testing.

```sh
# Terminal 1 (Linux/macOS)
meshlink node start --config local/a.json --socket "$PWD/local/a.sock"
# Terminal 2
meshlink node start --config local/b.json --socket "$PWD/local/b.sock" --echo
```

On Windows PowerShell, use `--socket "$PWD\local\a.sock"` (and `b.sock`) instead.

The `--socket` file is how the CLI talks to a running node. It is local only
and never opened to the network.

### 3. Connect and send messages

In a third terminal (set `B` again as in step 1, or paste B's peer ID):

```sh
meshlink peer connect --socket "$PWD/local/a.sock" --peer "$B" 127.0.0.1:7002
meshlink peer list --socket "$PWD/local/a.sock"
meshlink message request --socket "$PWD/local/a.sock" "$B" "hello"   # prints: hello
meshlink diagnostics --socket "$PWD/local/a.sock"
```

Tip: to skip `peer connect`, add `"known_peers": [{"id": "<B's ID>", "address": "127.0.0.1:7002"}]`
to `a.json`. Node A then connects by itself, and reconnects if B restarts.
See [configuration](docs/configuration.md#automatic-connections).

### 4. Stop the nodes

Press **Ctrl+C** in each node's terminal, or run:

```sh
meshlink node stop --socket "$PWD/local/a.sock"
```

## CLI commands

| Command | What it does |
|---|---|
| `identity create --key FILE` | Create a new private key and print its peer ID |
| `identity inspect --key FILE` | Print the peer ID for an existing key |
| `config validate --config FILE` | Check a config file for mistakes |
| `node start --config FILE` | Run a node in the foreground |
| `node stop` | Stop a running node |
| `peer connect --peer ID HOST:PORT` | Connect to an allowed peer |
| `peer list` | Show connected peers |
| `peer disconnect ID` | Disconnect a peer |
| `message send ID TEXT` | Send a one-way message |
| `message request ID TEXT` | Send a request and print the reply |
| `diagnostics` | Show node status and counters |
| `version` | Print the installed version |

Tips:

- Add `--json` for machine-readable output.
- Put flags **before** other arguments, e.g. `peer disconnect --socket S ID`.
- Run `meshlink --help` for the full usage text.

## Use it as a Go library

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/zahidhasann88/meshlink/pkg/meshlink"
)

func main() {
	ctx := context.Background()
	idA, _ := meshlink.GenerateIdentity()
	idB, _ := meshlink.GenerateIdentity()

	// Each node must allow the other.
	a, _ := meshlink.NewNode(meshlink.Config{Identity: idA, AllowedPeers: []string{idB.ID()}})
	b, _ := meshlink.NewNode(meshlink.Config{Identity: idB, AllowedPeers: []string{idA.ID()}})
	defer a.Close()
	defer b.Close()

	// B replies to every request.
	b.Handle(func(ctx context.Context, from string, msg []byte) ([]byte, error) {
		return append([]byte("echo: "), msg...), nil
	})

	a.Start(ctx)
	b.Start(ctx)

	if err := a.Connect(ctx, b.Address(), b.ID()); err != nil {
		log.Fatal(err)
	}
	reply, err := a.Request(ctx, b.ID(), []byte("hello"))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(reply)) // echo: hello
}
```

Runnable examples:

- `go run ./examples/messaging`: two peers and a request/reply
- `go run ./examples/relay`: two peers that can only reach each other through a relay

See the [Go API guide](docs/api.md) for everything else.

## Good to know

- **Both sides must allow each other.** A node with an empty `allowed_peers` list
  accepts no one.
- **`Send` means "written to the network,"** not "processed by the other side."
  Use `Request` when you need a reply.
- **Binding `127.0.0.1`** keeps a node local to your machine. **`0.0.0.0`** opens
  it on your network, so use a firewall.
- **Behind NAT?** Use a [relay](docs/deployment.md#relay-server-optional). MeshLink
  does not punch through NAT by itself.
- **Not built yet:** transports other than TCP, and multi-hop routing.

## Documentation

| Guide | What's inside |
|---|---|
| [Go API](docs/api.md) | Using MeshLink from Go code |
| [Configuration](docs/configuration.md) | Every setting, defaults, env variables |
| [Deployment](docs/deployment.md) | Registry server, Docker, running in production |
| [Security](SECURITY.md) | Reporting issues, how identity and trust work, known limits |
| [Architecture](docs/architecture.md) | How the engine works inside, custom transports |
| [Protocol](docs/protocol.md) | Wire format, for building compatible clients |
| [Benchmarks](docs/benchmarks.md) | Measured latency and throughput |

## Development

```sh
go test ./...            # all tests, including real network tests
make check               # format, vet, tests, race detector, build (Linux/macOS)
./scripts/check.ps1      # same checks on Windows
make test-fuzz           # short fuzz runs
make docker-smoke        # build the image and test 4 containers end to end
bash scripts/demo.sh     # re-record docs/demo.gif (needs Docker)
```

Found a security problem? Please see [SECURITY.md](SECURITY.md) instead of
opening a public issue.

## License

[MIT](LICENSE) © 2026 Zahid Hasan
