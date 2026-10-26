package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/zahidhasann88/meshlink/internal/fsperm"
	"github.com/zahidhasann88/meshlink/internal/limits"
	m "github.com/zahidhasann88/meshlink/pkg/meshlink"
)

type command struct {
	Op      string `json:"op"`
	Peer    string `json:"peer,omitempty"`
	Address string `json:"address,omitempty"`
	Payload []byte `json:"payload,omitempty"`
}
type reply struct {
	Error       string         `json:"error,omitempty"`
	Peers       []m.PeerInfo   `json:"peers,omitempty"`
	Diagnostics *m.Diagnostics `json:"diagnostics,omitempty"`
	Payload     []byte         `json:"payload,omitempty"`
}

func strictDecode(r io.Reader, v any) error {
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

func control(n *m.Node, path string, stop func(), box *inbox, done <-chan struct{}) (*http.Server, net.Listener, error) {
	if !filepath.IsAbs(path) {
		return nil, nil, errors.New("control socket path must be absolute")
	}
	parent := filepath.Dir(path)
	if e := os.MkdirAll(parent, 0700); e != nil {
		return nil, nil, e
	}
	st, e := os.Lstat(parent)
	if e != nil {
		return nil, nil, e
	}
	if e = fsperm.PrivateDir(st); e != nil {
		return nil, nil, fmt.Errorf("socket parent directory must be owner-only (chmod 700): %w", e)
	}
	ln, e := net.Listen("unix", path)
	if e != nil {
		return nil, nil, e
	}
	if runtime.GOOS != "windows" {
		if e = os.Chmod(path, 0600); e != nil {
			ln.Close()
			return nil, nil, e
		}
	}
	slots := make(chan struct{}, 8)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/v1/listen" {
			box.serve(w, r, done)
			return
		}
		if r.Method != "POST" || r.URL.Path != "/v1/command" {
			http.NotFound(w, r)
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			http.Error(w, "busy", 503)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 24<<20)
		var c command
		if e := strictDecode(r.Body, &c); e != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		var res reply
		var e error
		switch c.Op {
		case "peers":
			res.Peers = n.Peers()
		case "diagnostics":
			d := n.Diagnostics()
			res.Diagnostics = &d
		case "connect":
			e = n.Connect(ctx, c.Address, c.Peer)
		case "disconnect":
			e = n.Disconnect(c.Peer)
		case "send":
			e = n.Send(ctx, c.Peer, c.Payload)
		case "request":
			res.Payload, e = n.Request(ctx, c.Peer, c.Payload)
		case "shutdown":
			defer stop()
		default:
			http.Error(w, "unknown operation", 400)
			return
		}
		if e != nil {
			res.Error = e.Error()
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(res)
	})
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 15 * time.Second, MaxHeaderBytes: 8192}
	return srv, limits.Listener(ln, 32), nil
}
