// meshlink-registry is an optional single-process leased discovery directory.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/zahidhasann88/meshlink/internal/discovery"
	"github.com/zahidhasann88/meshlink/internal/fsperm"
	"github.com/zahidhasann88/meshlink/internal/limits"
)

func run() error {
	address := flag.String("listen", env("MESHLINK_REGISTRY_LISTEN", "127.0.0.1:8081"), "HTTP listen address; remote bind requires TLS")
	tokenFile := flag.String("token-file", os.Getenv("MESHLINK_REGISTRY_TOKEN_FILE"), "owner-only file containing a random bearer token (at least 32 characters)")
	cert := flag.String("tls-cert", "", "HTTPS certificate PEM")
	key := flag.String("tls-key", "", "HTTPS private key PEM")
	capacity := flag.Int("capacity", 1024, "maximum registrations")
	flag.Parse()
	if *tokenFile == "" {
		return errors.New("--token-file is required")
	}
	st, e := os.Stat(*tokenFile)
	if e != nil {
		return e
	}
	if e = fsperm.PrivateFile(st); e != nil || st.Size() > 4096 {
		return fmt.Errorf("token file must be an owner-only regular file, at most 4096 bytes (%v)", e)
	}
	b, e := os.ReadFile(*tokenFile)
	if e != nil {
		return e
	}
	host, _, e := net.SplitHostPort(*address)
	if e != nil {
		return e
	}
	ip := net.ParseIP(host)
	tlsEnabled := *cert != "" && *key != ""
	if (*cert == "") != (*key == "") {
		return errors.New("both --tls-cert and --tls-key required")
	}
	if !tlsEnabled && (ip == nil || !ip.IsLoopback()) {
		return errors.New("non-loopback registry requires TLS")
	}
	store, e := discovery.NewMemory(*capacity)
	if e != nil {
		return e
	}
	h, e := discovery.NewHandler(store, strings.TrimSpace(string(b)))
	if e != nil {
		return e
	}
	ln, e := net.Listen("tcp", *address)
	if e != nil {
		return e
	}
	defer ln.Close()
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errs := make(chan error, 1)
	go func() {
		l := limits.Listener(ln, 128)
		if tlsEnabled {
			errs <- srv.ServeTLS(l, *cert, *key)
		} else {
			errs <- srv.Serve(l)
		}
	}()
	slog.Info("registry started", "address", ln.Addr().String(), "tls", tlsEnabled)
	select {
	case e = <-errs:
		if !errors.Is(e, http.ErrServerClosed) {
			return e
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if e = srv.Shutdown(shutdown); e != nil {
			srv.Close()
			return e
		}
	}
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
		fmt.Fprintln(os.Stderr, "meshlink-registry:", e)
		os.Exit(1)
	}
}
