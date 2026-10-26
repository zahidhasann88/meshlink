package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zahidhasann88/meshlink/internal/identity"
)

func newID(t *testing.T) *identity.Identity {
	t.Helper()
	id, e := identity.Generate()
	if e != nil {
		t.Fatal(e)
	}
	return id
}

func TestLeasesAndCapacity(t *testing.T) {
	m, _ := NewMemory(1)
	now := time.Now()
	m.now = func() time.Time { return now }
	ctx := context.Background()
	a, b := newID(t), newID(t)
	if e := m.Register(ctx, Sign(a, "127.0.0.1:4321", now), time.Second); e != nil {
		t.Fatal(e)
	}
	if e := m.Register(ctx, Sign(b, "127.0.0.1:4322", now), time.Second); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	now = now.Add(time.Second)
	if _, e := m.Lookup(ctx, a.ID()); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if e := m.Register(ctx, Sign(b, "127.0.0.1:4322", now), time.Second); e != nil {
		t.Fatal(e)
	}
}

func TestSignedRecords(t *testing.T) {
	m, _ := NewMemory(8)
	now := time.Now()
	m.now = func() time.Time { return now }
	ctx := context.Background()
	owner, attacker := newID(t), newID(t)
	good := Sign(owner, "10.0.0.1:7001", now)
	if e := good.CheckLookup(owner.ID(), now); e != nil {
		t.Fatal(e)
	}

	tampered := good
	tampered.Address = "10.6.6.6:7001"
	forgedKey := Sign(attacker, "10.6.6.6:7001", now)
	forgedKey.ID = owner.ID()
	badSig := good
	badSig.Signature = append([]byte(nil), good.Signature...)
	badSig.Signature[0] ^= 1
	for name, r := range map[string]Record{"tampered address": tampered, "other key": forgedKey, "bad signature": badSig} {
		if e := m.Register(ctx, r, time.Minute); !errors.Is(e, ErrSignature) {
			t.Errorf("%s: expected ErrSignature, got %v", name, e)
		}
	}
	for name, at := range map[string]time.Time{"too old": now.Add(-10 * time.Minute), "future": now.Add(10 * time.Minute)} {
		if e := m.Register(ctx, Sign(owner, "10.0.0.1:7001", at), time.Minute); e == nil {
			t.Errorf("%s: accepted", name)
		}
	}

	if e := m.Register(ctx, good, time.Minute); e != nil {
		t.Fatal(e)
	}
	if e := m.Register(ctx, good, time.Minute); !errors.Is(e, ErrStale) {
		t.Fatal("replayed the same registration:", e)
	}
	if e := m.Unregister(ctx, Sign(attacker, "", now.Add(time.Second))); e != nil {
		t.Fatal(e)
	}
	if _, e := m.Lookup(ctx, owner.ID()); e != nil {
		t.Fatal("another peer's removal deleted the owner's entry")
	}
	stolen := Sign(attacker, "", now.Add(time.Second))
	stolen.ID = owner.ID()
	if e := m.Unregister(ctx, stolen); !errors.Is(e, ErrSignature) {
		t.Fatal("accepted a removal not signed by the owner:", e)
	}
	if e := m.Unregister(ctx, Sign(owner, "", now.Add(2*time.Second))); e != nil {
		t.Fatal(e)
	}
	if _, e := m.Lookup(ctx, owner.ID()); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if e := m.Register(ctx, Sign(owner, "10.0.0.1:7001", now.Add(time.Second)), time.Minute); !errors.Is(e, ErrStale) {
		t.Fatal("replayed a registration from before the removal:", e)
	}
	if e := m.Register(ctx, Sign(owner, "10.0.0.2:7001", now.Add(3*time.Second)), time.Minute); e != nil {
		t.Fatal("rejected a new registration after removal:", e)
	}

	if e := Sign(owner, "10.0.0.1:7001", now.Add(-25*time.Hour)).CheckLookup(owner.ID(), now); e == nil {
		t.Fatal("lookup accepted a record older than 24h")
	}
	if e := good.CheckLookup(attacker.ID(), now); e == nil {
		t.Fatal("lookup accepted a record for a different peer")
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
	id := newID(t)
	ctx := context.Background()
	if e = c.Register(ctx, Sign(id, "127.0.0.1:3211", time.Now()), time.Minute); e != nil {
		t.Fatal(e)
	}
	found, e := c.Lookup(ctx, id.ID())
	if e != nil || found.Address != "127.0.0.1:3211" {
		t.Fatal(found, e)
	}
	forged := Sign(newID(t), "10.6.6.6:1", time.Now())
	forged.ID = id.ID()
	body, _ := json.Marshal(registration{forged.Address, time.Minute, forged.PublicKey, forged.IssuedAt, forged.Signature})
	r, _ := http.NewRequest("PUT", s.URL+"/v1/peers/"+id.ID(), strings.NewReader(string(body)))
	r.Header.Set("Authorization", "Bearer "+token)
	if res, e := http.DefaultClient.Do(r); e != nil || res.StatusCode != 403 {
		t.Fatal("forged registration over HTTP:", res.StatusCode, e)
	}
	if e = c.Unregister(ctx, Sign(id, "", time.Now())); e != nil {
		t.Fatal(e)
	}
	if _, e = c.Lookup(ctx, id.ID()); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	res, e := http.Get(s.URL + "/v1/peers/" + id.ID())
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

func TestClientRejectsForgedRecordsFromRegistry(t *testing.T) {
	victim, attacker := newID(t), newID(t)
	forged := Sign(attacker, "10.6.6.6:7001", time.Now())
	forged.ID = victim.ID()
	forged.ExpiresAt = time.Now().Add(time.Minute)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(forged)
	}))
	defer s.Close()
	c, _ := NewClient(s.URL, strings.Repeat("a", 32))
	if _, e := c.Lookup(context.Background(), victim.ID()); !errors.Is(e, ErrSignature) {
		t.Fatal("client accepted a forged record:", e)
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
	id := newID(t)
	for _, a := range []string{"0.0.0.0:80", "[::]:80", "127.0.0.1:0", "bad host:80", "-bad.example:80", ":9000", "a..b:80"} {
		if e := Validate(Record{ID: id.ID(), Address: a}); e == nil {
			t.Errorf("accepted %q", a)
		}
	}
	token := strings.Repeat("b", 32)
	m, _ := NewMemory(1)
	h, _ := NewHandler(m, token)
	for _, body := range []string{`{"address":"127.0.0.1:80","lease_ns":1000000000,"extra":1}`, `{"lease_ns":1000000000}`, strings.Repeat("x", 5000)} {
		r := httptest.NewRequest("PUT", "/v1/peers/"+id.ID(), strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatal(body[:10], w.Code)
		}
	}
}

func FuzzRecordVerify(f *testing.F) {
	id, _ := identity.Generate()
	good, _ := json.Marshal(Sign(id, "127.0.0.1:7001", time.Now()))
	f.Add(good)
	f.Add([]byte(`{"id":"x","public_key":"AAAA","signature":"AAAA"}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		var r Record
		if json.Unmarshal(b, &r) != nil {
			return
		}
		if r.Verify() == nil && identity.ID(r.PublicKey) != r.ID {
			t.Fatal("verified a record whose key does not match its ID")
		}
	})
}
