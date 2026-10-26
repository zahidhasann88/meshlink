package discovery

import (
	"context"
	"crypto/ed25519"
	"errors"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zahidhasann88/meshlink/internal/identity"
)

const (
	maxClockSkew = 5 * time.Minute
	maxRecordAge = 24*time.Hour + maxClockSkew
)

var (
	ErrNotFound  = errors.New("peer registration not found or expired")
	ErrCapacity  = errors.New("registry capacity reached")
	ErrSignature = errors.New("registration signature is invalid")
	ErrStale     = errors.New("registration is older than the current one")
)

type Record struct {
	ID        string    `json:"id"`
	Address   string    `json:"address,omitempty"`
	PublicKey []byte    `json:"public_key"`
	IssuedAt  time.Time `json:"issued_at"`
	Signature []byte    `json:"signature"`
	ExpiresAt time.Time `json:"expires_at,omitzero"`
}

type Discovery interface {
	Register(context.Context, Record, time.Duration) error
	Lookup(context.Context, string) (Record, error)
	Unregister(context.Context, Record) error
}

func signedBytes(id, address string, issued time.Time) []byte {
	return []byte("meshlink-registry-v1\n" + id + "\n" + address + "\n" + strconv.FormatInt(issued.UnixNano(), 10))
}

func Sign(i *identity.Identity, address string, now time.Time) Record {
	r := Record{ID: i.ID(), Address: address, PublicKey: i.PublicKey(), IssuedAt: now.Round(0).UTC()}
	r.Signature = i.Sign(signedBytes(r.ID, r.Address, r.IssuedAt))
	return r
}

func (r Record) Verify() error {
	if r.Address != "" {
		if e := Validate(r); e != nil {
			return e
		}
	} else if !identity.ValidID(r.ID) {
		return errors.New("invalid peer ID")
	}
	if len(r.PublicKey) != ed25519.PublicKeySize || identity.ID(r.PublicKey) != r.ID {
		return ErrSignature
	}
	if !ed25519.Verify(r.PublicKey, signedBytes(r.ID, r.Address, r.IssuedAt), r.Signature) {
		return ErrSignature
	}
	return nil
}

func (r Record) CheckLookup(id string, now time.Time) error {
	if r.ID != id {
		return errors.New("registry returned a different peer")
	}
	if r.Address == "" {
		return errors.New("registry returned a record without an address")
	}
	if e := r.Verify(); e != nil {
		return e
	}
	if r.IssuedAt.After(now.Add(maxClockSkew)) || r.IssuedAt.Before(now.Add(-maxRecordAge)) {
		return ErrSignature
	}
	return nil
}

func fresh(r Record, now time.Time) error {
	if r.IssuedAt.Before(now.Add(-maxClockSkew)) || r.IssuedAt.After(now.Add(maxClockSkew)) {
		return errors.New("registration timestamp is too far from the registry's clock")
	}
	return nil
}

func Validate(r Record) error {
	if !identity.ValidID(r.ID) {
		return errors.New("invalid peer ID")
	}
	h, p, e := net.SplitHostPort(r.Address)
	if e != nil || len(h) == 0 || len(h) > 253 || len(r.Address) > 300 {
		return errors.New("invalid host:port address")
	}
	n, e := strconv.Atoi(p)
	if e != nil || n < 1 || n > 65535 {
		return errors.New("invalid port")
	}
	if ip := net.ParseIP(h); ip != nil {
		if ip.IsUnspecified() {
			return errors.New("advertise a reachable address, not a wildcard")
		}
	} else {
		for _, label := range strings.Split(strings.TrimSuffix(h, "."), ".") {
			if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				return errors.New("invalid DNS hostname")
			}
			for _, c := range label {
				if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
					return errors.New("invalid DNS hostname")
				}
			}
		}
	}
	return nil
}

type Memory struct {
	mu       sync.Mutex
	records  map[string]Record
	removed  map[string]time.Time
	capacity int
	now      func() time.Time
}

func NewMemory(capacity int) (*Memory, error) {
	if capacity < 1 || capacity > 65536 {
		return nil, errors.New("registry capacity must be 1..65536")
	}
	return &Memory{records: map[string]Record{}, removed: map[string]time.Time{}, capacity: capacity, now: time.Now}, nil
}

func (m *Memory) expire() {
	now := m.now()
	for id, r := range m.records {
		if !now.Before(r.ExpiresAt) {
			delete(m.records, id)
		}
	}
	for id, at := range m.removed {
		if at.Before(now.Add(-2 * maxClockSkew)) {
			delete(m.removed, id)
		}
	}
}

func (m *Memory) newer(r Record) error {
	if old, ok := m.records[r.ID]; ok && !r.IssuedAt.After(old.IssuedAt) {
		return ErrStale
	}
	if at, ok := m.removed[r.ID]; ok && !r.IssuedAt.After(at) {
		return ErrStale
	}
	return nil
}

func (m *Memory) Register(ctx context.Context, r Record, lease time.Duration) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if r.Address == "" {
		return errors.New("registration needs an address")
	}
	if e := r.Verify(); e != nil {
		return e
	}
	if lease < time.Second || lease > 24*time.Hour {
		return errors.New("lease must be 1s..24h")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := fresh(r, m.now()); e != nil {
		return e
	}
	m.expire()
	if e := m.newer(r); e != nil {
		return e
	}
	if _, ok := m.records[r.ID]; !ok && len(m.records) >= m.capacity {
		return ErrCapacity
	}
	delete(m.removed, r.ID)
	r.ExpiresAt = m.now().Add(lease)
	m.records[r.ID] = r
	return nil
}

func (m *Memory) Lookup(ctx context.Context, id string) (Record, error) {
	if e := ctx.Err(); e != nil {
		return Record{}, e
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expire()
	r, ok := m.records[id]
	if !ok {
		return r, ErrNotFound
	}
	return r, nil
}

func (m *Memory) Unregister(ctx context.Context, r Record) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if r.Address != "" {
		return errors.New("removal record must not have an address")
	}
	if e := r.Verify(); e != nil {
		return e
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := fresh(r, m.now()); e != nil {
		return e
	}
	m.expire()
	if e := m.newer(r); e != nil {
		return e
	}
	delete(m.records, r.ID)
	if len(m.removed) < m.capacity {
		m.removed[r.ID] = r.IssuedAt
	}
	return nil
}
