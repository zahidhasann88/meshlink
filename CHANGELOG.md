# Changelog

## Unreleased (v0.1.0)

First version.

- Secure peer connections: mutual TLS 1.3 with Ed25519 identities and peer allowlists
- Messaging: one-way send, broadcast, and request/response with timeouts
- Bounded queues, connection limits and message size limits
- Optional registry server for peer discovery, with leases
- `meshlink` CLI for Linux, macOS and Windows, including `node stop`
- Diagnostics: peers, byte counters, timeouts and errors
- Tests with real network connections, fuzz tests, and CI on Linux and Windows
