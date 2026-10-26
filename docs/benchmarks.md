# Benchmarks

These numbers show the rough cost of common operations. They are not guarantees,
and results on your hardware and network will differ.

## Setup

| | |
|---|---|
| CPU | Intel Core i7-8665U @ 1.90GHz (4 cores, 8 threads), laptop |
| Memory | 16 GB |
| OS | Windows 10 Pro (amd64) |
| Go | 1.27.0 |
| Network | Loopback (`127.0.0.1`), so no real network latency |
| Method | `go test -bench . -benchmem -count=3`; the median is shown |

## Results

| Benchmark | What it measures | Result |
|---|---|---|
| `Frame` | Write and read one 1 KiB frame in memory | 1.0 µs, 6 allocs |
| `Protocol` | Encode and decode one message in memory | 4.0 µs, 22 allocs |
| `RoundTrip` | One small `Request` and reply over TLS | 167 µs |
| `ConcurrentRequests` | Small requests from 8 goroutines over one connection | 53 µs per request (~19,000/s) |
| `LargeTransfer` | 600 KiB request echoed back (1.2 MB moved) | 15.5 ms, 39.6 MB/s one way |
| `Connect` | TCP connect plus mutual TLS 1.3 handshake | 2.2 ms |
| `RelayRoundTrip` | One small `Request` and reply through a local relay | 212 µs |

The relay adds about 45 µs per round trip on loopback. Over a real network, the
extra network hop to the relay usually costs far more than the relay itself.

## Run them yourself

```sh
make bench
# or
go test ./internal/protocol ./test/integration -run '^$' -bench . -benchmem -count=5
```

To compare two versions, save each run's output and use
[`benchstat`](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat).
