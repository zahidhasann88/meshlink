package integration_test

import (
	"context"
	"github.com/zahidhasann88/meshlink/internal/discovery"
	m "github.com/zahidhasann88/meshlink/pkg/meshlink"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPDiscoveryToTLS(t *testing.T) {
	ns := nodes(t, 2, nil)
	store, _ := m.NewMemoryRegistry(10)
	token := strings.Repeat("c", 32)
	h, e := discovery.NewHandler(store, token)
	if e != nil {
		t.Fatal(e)
	}
	s := httptest.NewServer(h)
	defer s.Close()
	client, e := m.NewRegistryClient(s.URL, token)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if e = ns[1].Register(ctx, client, ns[1].Address(), time.Minute); e != nil {
		t.Fatal(e)
	}
	if e = ns[0].DiscoverAndConnect(ctx, client, ns[1].ID()); e != nil {
		t.Fatal(e)
	}
	p, e := ns[0].Request(ctx, ns[1].ID(), []byte("directory to native TLS"))
	if e != nil || string(p) != "directory to native TLS" {
		t.Fatal(string(p), e)
	}
}
