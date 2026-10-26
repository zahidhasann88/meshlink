package discovery

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zahidhasann88/meshlink/internal/identity"
)

func TestLeasesAndCapacity(t *testing.T) {
	m, _ := NewMemory(1)
	now := time.Now()
	m.now = func() time.Time { return now }
	id, _ := identity.Generate()
	r := Record{ID: id.ID(), Address: "127.0.0.1:4321"}
	ctx := context.Background()
	if e := m.Register(ctx, r, time.Second); e != nil {
		t.Fatal(e)
	}
	other, _ := identity.Generate()
	r2 := r
	r2.ID = other.ID()
	if e := m.Register(ctx, r2, time.Second); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	now = now.Add(time.Second)
	if _, e := m.Lookup(ctx, r.ID); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if e := m.Register(ctx, r2, time.Second); e != nil {
		t.Fatal(e)
	}
}
func TestHTTPRegistry(t *testing.T) {
	m, _ := NewMemory(8)
	token := strings.Repeat("a", 32)
	h, _ := NewHandler(m, token)
	s := httptest.NewServer(h)
	defer s.Close()
	c, e := NewClient(s.URL, token)
	if e != nil {
		t.Fatal(e)
	}
	id, _ := identity.Generate()
	r := Record{ID: id.ID(), Address: "127.0.0.1:3211"}
	ctx := context.Background()
	if e = c.Register(ctx, r, time.Minute); e != nil {
		t.Fatal(e)
	}
	found, e := c.Lookup(ctx, r.ID)
	if e != nil || found.Address != r.Address {
		t.Fatal(found, e)
	}
	if e = c.Unregister(ctx, r.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = c.Lookup(ctx, r.ID); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	res, e := http.Get(s.URL + "/v1/peers/" + r.ID)
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatal(res.StatusCode)
	}
	if _, e = NewClient("http://example.com", token); e == nil {
		t.Fatal("remote plaintext accepted")
	}
}
func TestRateAndSizeLimits(t *testing.T) {
	m, _ := NewMemory(1)
	h, _ := NewHandler(m, strings.Repeat("a", 32))
	for i := 0; i < 101; i++ {
		r := httptest.NewRequest("GET", "/v1/peers/x", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if i == 100 && w.Code != 429 {
			t.Fatal(w.Code)
		}
	}
}

func TestMetadataAndBodyValidation(t *testing.T) {
	id, _ := identity.Generate()
	for _, a := range []string{"0.0.0.0:80", "[::]:80", "127.0.0.1:0", "bad host:80", "-bad.example:80", ":9000", "a..b:80"} {
		if e := Validate(Record{ID: id.ID(), Address: a}); e == nil {
			t.Errorf("accepted %q", a)
		}
	}
	token := strings.Repeat("b", 32)
	m, _ := NewMemory(1)
	h, _ := NewHandler(m, token)
	for _, body := range []string{`{"address":"127.0.0.1:80","lease_ns":1000000000,"extra":1}`, strings.Repeat("x", 5000)} {
		r := httptest.NewRequest("PUT", "/v1/peers/"+id.ID(), strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatal(w.Code)
		}
	}
}
