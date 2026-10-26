package discovery

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zahidhasann88/meshlink/internal/identity"
)

var ErrNotFound = errors.New("peer registration not found or expired")
var ErrCapacity = errors.New("registry capacity reached")

type Record struct {
	ID        string    `json:"id"`
	Address   string    `json:"address"`
	ExpiresAt time.Time `json:"expires_at"`
}

type Discovery interface {
	Register(context.Context, Record, time.Duration) error
	Lookup(context.Context, string) (Record, error)
	Unregister(context.Context, string) error
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
	capacity int
	now      func() time.Time
}

func NewMemory(capacity int) (*Memory, error) {
	if capacity < 1 || capacity > 65536 {
		return nil, errors.New("registry capacity must be 1..65536")
	}
	return &Memory{records: map[string]Record{}, capacity: capacity, now: time.Now}, nil
}
func (m *Memory) expire() {
	now := m.now()
	for id, r := range m.records {
		if !now.Before(r.ExpiresAt) {
			delete(m.records, id)
		}
	}
}
func (m *Memory) Register(ctx context.Context, r Record, lease time.Duration) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if e := Validate(r); e != nil {
		return e
	}
	if lease < time.Second || lease > 24*time.Hour {
		return errors.New("lease must be 1s..24h")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expire()
	if _, ok := m.records[r.ID]; !ok && len(m.records) >= m.capacity {
		return ErrCapacity
	}
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
func (m *Memory) Unregister(ctx context.Context, id string) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.records, id)
	return nil
}
