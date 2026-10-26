package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	m "github.com/zahidhasann88/meshlink/pkg/meshlink"
)

func run() error {
	key := flag.String("key", os.Getenv("MESHLINK_RELAY_KEY"), "relay identity key file (create with: meshlink identity create)")
	listen := flag.String("listen", env("MESHLINK_RELAY_LISTEN", "127.0.0.1:7100"), "address to listen on")
	allow := flag.String("allow", os.Getenv("MESHLINK_RELAY_ALLOW"), "comma-separated peer IDs allowed to use this relay")
	maxCircuits := flag.Int("max-circuits", 256, "maximum relayed connections in total")
	perPeer := flag.Int("max-per-peer", 16, "maximum relayed connections per peer")
	idle := flag.Duration("idle-timeout", 2*time.Minute, "close relayed connections idle this long")
	flag.Parse()
	if *key == "" || *allow == "" {
		return errors.New("--key and --allow are required")
	}
	id, e := m.LoadIdentity(*key)
	if e != nil {
		return e
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	r, e := m.NewRelayServer(m.RelayServerConfig{Identity: id, ListenAddress: *listen, AllowedPeers: strings.Split(*allow, ","), MaxCircuits: *maxCircuits, MaxCircuitsPerPeer: *perPeer, IdleTimeout: *idle, Logger: logger})
	if e != nil {
		return e
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if e = r.Start(ctx); e != nil {
		return e
	}
	fmt.Printf("relay %s listening on %s\n", r.ID(), r.Address())
	<-ctx.Done()
	r.Close()
	s := r.Stats()
	logger.Info("relay stopped", "bytes_relayed", s.BytesRelayed, "rejected", s.Rejected)
	return nil
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, "meshlink-relay:", e)
		os.Exit(1)
	}
}
