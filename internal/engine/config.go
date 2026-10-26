package engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/zahidhasann88/meshlink/internal/identity"
	"github.com/zahidhasann88/meshlink/internal/transport"
	"github.com/zahidhasann88/meshlink/internal/transport/tcp"
)

type Config struct {
	Identity       *identity.Identity  `json:"-"`
	KeyPath        string              `json:"key_path,omitempty"`
	ListenAddress  string              `json:"listen_address"`
	AllowedPeers   []string            `json:"allowed_peers"`
	KnownPeers     []KnownPeer         `json:"known_peers,omitempty"`
	Reconnect      RetryPolicy         `json:"reconnect"`
	MaxPeers       int                 `json:"max_peers"`
	MaxHandshakes  int                 `json:"max_handshakes"`
	MaxFrameSize   int                 `json:"max_frame_size"`
	QueueCapacity  int                 `json:"queue_capacity"`
	HandlerQueue   int                 `json:"handler_queue"`
	Workers        int                 `json:"workers"`
	MaxPending     int                 `json:"max_pending"`
	ConnectTimeout time.Duration       `json:"connect_timeout"`
	WriteTimeout   time.Duration       `json:"write_timeout"`
	IdleTimeout    time.Duration       `json:"idle_timeout"`
	RequestTimeout time.Duration       `json:"request_timeout"`
	Transport      transport.Transport `json:"-"`
	Logger         *slog.Logger        `json:"-"`
}

type KnownPeer struct {
	ID      string `json:"id"`
	Address string `json:"address"`
}

func (c Config) normalized() (Config, error) {
	if c.ListenAddress == "" {
		c.ListenAddress = "127.0.0.1:0"
	}
	allowed := make(map[string]bool, len(c.AllowedPeers))
	for _, p := range c.AllowedPeers {
		if !identity.ValidID(p) {
			return c, fmt.Errorf("invalid allowed peer ID %q", p)
		}
		allowed[p] = true
	}
	seen := make(map[string]bool, len(c.KnownPeers))
	for _, p := range c.KnownPeers {
		switch {
		case !allowed[p.ID]:
			return c, fmt.Errorf("known peer %q must also be in allowed_peers", p.ID)
		case p.Address == "":
			return c, fmt.Errorf("known peer %q needs an address", p.ID)
		case seen[p.ID]:
			return c, fmt.Errorf("known peer %q is listed twice", p.ID)
		}
		seen[p.ID] = true
	}
	c.KnownPeers = append([]KnownPeer(nil), c.KnownPeers...)
	c.Reconnect = c.Reconnect.withDefaults()
	if e := c.Reconnect.validate(); e != nil {
		return c, fmt.Errorf("reconnect: %w", e)
	}
	defaults := []struct {
		p      *int
		d, max int
	}{{&c.MaxPeers, 64, 65536}, {&c.MaxHandshakes, 8, 1024}, {&c.MaxFrameSize, 1 << 20, 16 << 20}, {&c.QueueCapacity, 32, 4096}, {&c.HandlerQueue, 64, 65536}, {&c.Workers, 4, 1024}, {&c.MaxPending, 256, 65536}}
	for _, v := range defaults {
		if *v.p == 0 {
			*v.p = v.d
		}
		if *v.p < 1 || *v.p > v.max {
			return c, errors.New("resource limit outside supported range")
		}
	}
	if c.MaxFrameSize < 128 {
		return c, errors.New("max_frame_size must be at least 128")
	}
	for _, v := range []struct {
		p *time.Duration
		d time.Duration
	}{{&c.ConnectTimeout, 5 * time.Second}, {&c.WriteTimeout, 5 * time.Second}, {&c.IdleTimeout, 2 * time.Minute}, {&c.RequestTimeout, 30 * time.Second}} {
		if *v.p == 0 {
			*v.p = v.d
		}
		if *v.p < time.Millisecond || *v.p > 24*time.Hour {
			return c, errors.New("timeouts must be between 1ms and 24h")
		}
	}
	if c.Transport == nil {
		c.Transport = tcp.Transport{}
	}
	if c.Logger == nil {
		c.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return c, nil
}

func ValidateConfig(c Config) error { _, e := c.normalized(); return e }

func ParseConfig(b []byte) (Config, error) {
	var c Config
	if len(b) > 64<<10 {
		return c, errors.New("configuration exceeds 64KiB")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e := d.Decode(&c); e != nil {
		return c, e
	}
	var extra any
	if e := d.Decode(&extra); e != io.EOF {
		return c, errors.New("trailing configuration data")
	}
	return c, ValidateConfig(c)
}
