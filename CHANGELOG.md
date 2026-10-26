# Changelog

All notable changes to this project are listed here. Versions follow
[Semantic Versioning](https://semver.org/). Before v1.0, minor versions may
contain breaking changes.

## [Unreleased]

## [v0.1.0] - 2026-10-26

First public release.

### Added

- Secure peer connections: mutual TLS 1.3 with Ed25519 identities and peer allowlists
- Messaging: one-way send, broadcast, and request/response with timeouts
- Automatic reconnect for known peers (`known_peers` config, `Node.Maintain`), with
  backoff, jitter, and giving up on repeated or authentication failures
- Bounded queues, connection limits and message size limits
- Optional registry server (`meshlink-registry`) for peer discovery, with leases
- `meshlink` CLI for Linux, macOS and Windows: identities, nodes, peers, messages,
  diagnostics, `node stop` and `version`
- Prebuilt binaries for Linux, macOS and Windows (amd64 and arm64)

### Known limitations

- No relay, NAT traversal, or transports other than TCP
- Messages are not stored or resent after a disconnect
- Not security-audited

[Unreleased]: https://github.com/zahidhasann88/meshlink/compare/v0.1.0...HEAD
[v0.1.0]: https://github.com/zahidhasann88/meshlink/releases/tag/v0.1.0
