package discovery

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/zahidhasann88/meshlink/internal/identity"
)

type registration struct {
	Address   string        `json:"address,omitempty"`
	Lease     time.Duration `json:"lease_ns,omitempty"`
	PublicKey []byte        `json:"public_key"`
	IssuedAt  time.Time     `json:"issued_at"`
	Signature []byte        `json:"signature"`
}

func (b registration) record(id string) Record {
	return Record{ID: id, Address: b.Address, PublicKey: b.PublicKey, IssuedAt: b.IssuedAt, Signature: b.Signature}
}

func statusFor(e error) int {
	switch {
	case errors.Is(e, ErrSignature):
		return 403
	case errors.Is(e, ErrStale):
		return 409
	case errors.Is(e, ErrCapacity):
		return 503
	}
	return 400
}

func decode(r io.Reader, v any) error {
	d := json.NewDecoder(r)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return e
	}
	var extra any
	if e := d.Decode(&extra); e != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}

func NewHandler(store Discovery, token string) (http.Handler, error) {
	if len(token) < 32 {
		return nil, errors.New("registry token must contain at least 32 characters")
	}
	hash := sha256.Sum256([]byte("Bearer " + token))
	slots := make(chan struct{}, 32)
	var mu sync.Mutex
	window := time.Now()
	count := 0
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "GET" && (r.URL.Path == "/healthz" || r.URL.Path == "/readyz") {
			io.WriteString(w, `{"status":"ok"}`)
			return
		}
		mu.Lock()
		if time.Since(window) >= time.Second {
			window = time.Now()
			count = 0
		}
		count++
		limited := count > 100
		mu.Unlock()
		if limited {
			http.Error(w, "rate limited", 429)
			return
		}
		h := sha256.Sum256([]byte(r.Header.Get("Authorization")))
		if subtle.ConstantTimeCompare(hash[:], h[:]) != 1 {
			http.Error(w, "unauthorized", 401)
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			http.Error(w, "busy", 503)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/v1/peers/") {
			http.NotFound(w, r)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/v1/peers/")
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		switch r.Method {
		case "GET":
			rec, e := store.Lookup(r.Context(), id)
			if e != nil {
				http.Error(w, "not found", 404)
				return
			}
			json.NewEncoder(w).Encode(rec)
		case "PUT":
			var body registration
			if e := decode(r.Body, &body); e != nil {
				http.Error(w, "invalid JSON", 400)
				return
			}
			if body.Address == "" {
				http.Error(w, "registration needs an address", 400)
				return
			}
			if e := store.Register(r.Context(), body.record(id), body.Lease); e != nil {
				http.Error(w, e.Error(), statusFor(e))
				return
			}
			w.WriteHeader(204)
		case "DELETE":
			var body registration
			if e := decode(r.Body, &body); e != nil || body.Address != "" || body.Lease != 0 {
				http.Error(w, "invalid JSON", 400)
				return
			}
			if e := store.Unregister(r.Context(), body.record(id)); e != nil {
				http.Error(w, e.Error(), statusFor(e))
				return
			}
			w.WriteHeader(204)
		default:
			w.Header().Set("Allow", "GET, PUT, DELETE")
			http.Error(w, "method not allowed", 405)
		}
	}), nil
}

type Client struct {
	endpoint, token string
	http            *http.Client
}

func NewClient(endpoint, token string) (*Client, error) {
	u, e := url.Parse(endpoint)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" {
		return nil, errors.New("invalid registry endpoint")
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || ip == nil || !ip.IsLoopback() {
			return nil, errors.New("registry requires HTTPS except numeric loopback")
		}
	}
	if len(token) < 32 {
		return nil, errors.New("registry token must contain at least 32 characters")
	}
	return &Client{strings.TrimRight(endpoint, "/"), token, &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (c *Client) do(ctx context.Context, method, id string, body any, out any) error {
	if !identity.ValidID(id) {
		return errors.New("invalid peer ID")
	}
	var b []byte
	var e error
	if body != nil {
		b, e = json.Marshal(body)
		if e != nil {
			return e
		}
	}
	r, e := http.NewRequestWithContext(ctx, method, c.endpoint+"/v1/peers/"+id, bytes.NewReader(b))
	if e != nil {
		return e
	}
	r.Header.Set("Authorization", "Bearer "+c.token)
	r.Header.Set("Content-Type", "application/json")
	res, e := c.http.Do(r)
	if e != nil {
		return e
	}
	defer res.Body.Close()
	if res.StatusCode == 404 {
		return ErrNotFound
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("registry HTTP status %d", res.StatusCode)
	}
	if out != nil {
		return decode(io.LimitReader(res.Body, 4097), out)
	}
	return nil
}
func (c *Client) Register(ctx context.Context, r Record, lease time.Duration) error {
	if e := r.Verify(); e != nil {
		return e
	}
	return c.do(ctx, "PUT", r.ID, registration{r.Address, lease, r.PublicKey, r.IssuedAt, r.Signature}, nil)
}
func (c *Client) Lookup(ctx context.Context, id string) (Record, error) {
	var r Record
	e := c.do(ctx, "GET", id, nil, &r)
	if e != nil {
		return r, e
	}
	if e = r.CheckLookup(id, time.Now()); e != nil {
		return r, e
	}
	if !time.Now().Before(r.ExpiresAt) {
		return r, ErrNotFound
	}
	return r, nil
}
func (c *Client) Unregister(ctx context.Context, r Record) error {
	if e := r.Verify(); e != nil {
		return e
	}
	return c.do(ctx, "DELETE", r.ID, registration{PublicKey: r.PublicKey, IssuedAt: r.IssuedAt, Signature: r.Signature}, nil)
}
