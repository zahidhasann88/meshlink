# Changelog

All notable changes to this project are listed here. Versions follow
[Semantic Versioning](https://semver.org/). Before v1.0, minor versions may
contain breaking changes.

## [Unreleased]

## [v0.2.0] - 2026-10-26

### Added

- Relay: `meshlink-relay` server and `relay` config. Nodes behind NAT stay reachable
  through a relay that only forwards end-to-end encrypted traffic. Known peers fall
  back to the relay when a direct connection fails.
- Registry: `registry` config section. Nodes register and renew their address
  automatically (`Node.KeepRegistered`), and known peers can be listed by ID alone.
- Windows: key files are created private to the current user, and key and token
  files that other users can read are rejected.
- Docker image includes all three binaries, with an end-to-end smoke test in CI.
- Weekly fuzzing in CI, and published benchmarks.

### Changed

- Now depends on `golang.org/x/sys` for Windows file permissions.
- **Upgrading on Windows:** key files created by v0.1.0 may now be rejected because
  other users can read them. Fix each one with
  `icacls node.key /inheritance:r /grant:r "%USERNAME%:F"` (the error shows the exact command).
- `known_peers` entries no longer need an `address` when a registry or relay is configured.

### Known limitations

- No NAT hole punching: peers behind NAT need a relay, port forwarding or a VPN
- The relay sees metadata (which peers talk, when, and how much), not message contents
- Registry and relay keep state in memory only
- Not security-audited

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

[Unreleased]: https://github.com/zahidhasann88/meshlink/compare/v0.2.0...HEAD
[v0.2.0]: https://github.com/zahidhasann88/meshlink/compare/v0.1.0...v0.2.0
[v0.1.0]: https://github.com/zahidhasann88/meshlink/releases/tag/v0.1.0
