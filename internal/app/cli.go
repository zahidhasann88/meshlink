package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	m "github.com/zahidhasann88/meshlink/pkg/meshlink"
)

const Help = `MeshLink — authenticated Go peer networking
Usage (flags precede positional arguments):
  meshlink identity create --key node.key
  meshlink identity inspect --key node.key [--json]
  meshlink config validate --config node.json
  meshlink node start --config node.json [--socket /private/dir/node.sock] [--echo]
  meshlink node stop [--socket PATH]
  meshlink peer list [--socket PATH] [--json]
  meshlink peer connect [--socket PATH] --peer ID HOST:PORT
  meshlink peer disconnect [--socket PATH] ID
  meshlink message send [--socket PATH] PEER_ID TEXT
  meshlink message request [--socket PATH] PEER_ID TEXT
  meshlink diagnostics [--socket PATH] [--json]

Each command accepts --help. node start runs in foreground until SIGINT/SIGTERM
(Ctrl+C) or node stop. Control uses a local Unix domain socket (also on Windows),
never a TCP API. The default socket is MESHLINK_SOCKET or a per-user path.
Both peers must authorize each other's IDs in their config. --echo explicitly
installs a request echo handler. Without it requests return handler_failed.
Exit status: 0 success/help; 1 operational or usage error.
`

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func socketDefault() string {
	if v := os.Getenv("MESHLINK_SOCKET"); v != "" {
		return v
	}
	if runtime.GOOS == "windows" {
		if dir, e := os.UserCacheDir(); e == nil {
			return filepath.Join(dir, "meshlink", "node.sock")
		}
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("meshlink-%d", os.Getuid()), "node.sock")
}
func readConfig(path string) (m.Config, error) {
	if path == "" {
		return m.Config{}, nil
	}
	f, e := os.Open(path)
	if e != nil {
		return m.Config{}, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, 65537))
	if e != nil {
		return m.Config{}, e
	}
	return m.ParseConfig(b)
}

func Run(ctx context.Context, args []string, out, errout io.Writer) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(out, Help)
		return nil
	}
	group := args[0]
	sub := ""
	args = args[1:]
	if group != "diagnostics" {
		if len(args) == 0 {
			return errors.New("missing subcommand; run meshlink --help")
		}
		if args[0] == "--help" {
			fmt.Fprint(out, Help)
			return nil
		}
		sub = args[0]
		args = args[1:]
	}
	fs := flag.NewFlagSet(strings.TrimSpace(group+" "+sub), flag.ContinueOnError)
	fs.SetOutput(errout)
	socket := fs.String("socket", socketDefault(), "owner-only control socket path")
	jsonOut := fs.Bool("json", false, "machine-readable JSON output")
	config := fs.String("config", env("MESHLINK_CONFIG", ""), "strict JSON node configuration file")
	key := fs.String("key", "", "identity file path")
	listen := fs.String("listen", "", "override listen address")
	allow := fs.String("allow", "", "override comma-separated allowed peer IDs")
	peer := fs.String("peer", "", "expected pinned peer ID")
	echo := fs.Bool("echo", false, "install request echo handler (node start only)")
	if e := fs.Parse(args); e != nil {
		if errors.Is(e, flag.ErrHelp) {
			return nil
		}
		return e
	}
	pos := fs.Args()
	switch group {
	case "identity":
		if *key == "" {
			return errors.New("--key is required")
		}
		var id *m.Identity
		var e error
		switch sub {
		case "create":
			id, e = m.GenerateIdentity()
			if e == nil {
				e = id.Save(*key)
			}
		case "inspect":
			id, e = m.LoadIdentity(*key)
		default:
			return errors.New("unknown identity command")
		}
		if e != nil {
			return e
		}
		if *jsonOut {
			return json.NewEncoder(out).Encode(map[string]string{"id": id.ID()})
		}
		fmt.Fprintln(out, id.ID())
		return nil
	case "config", "node":
		if group == "node" && sub == "stop" {
			if _, e := call(ctx, *socket, command{Op: "shutdown"}); e != nil {
				return e
			}
			fmt.Fprintln(out, "node stopping")
			return nil
		}
		c, e := readConfig(*config)
		if e != nil {
			return e
		}
		if v := os.Getenv("MESHLINK_KEY"); v != "" {
			c.KeyPath = v
		}
		if v := os.Getenv("MESHLINK_LISTEN"); v != "" {
			c.ListenAddress = v
		}
		if v := os.Getenv("MESHLINK_ALLOW"); v != "" {
			c.AllowedPeers = strings.Split(v, ",")
		}
		if *key != "" {
			c.KeyPath = *key
		}
		if *listen != "" {
			c.ListenAddress = *listen
		}
		if *allow != "" {
			c.AllowedPeers = strings.Split(*allow, ",")
		}
		if e = m.ValidateConfig(c); e != nil {
			return e
		}
		if group == "config" && sub == "validate" {
			fmt.Fprintln(out, "configuration valid (key readability is checked on node start)")
			return nil
		}
		if group != "node" || sub != "start" {
			return errors.New("unknown command")
		}
		if c.KeyPath == "" {
			return errors.New("CLI nodes require a persistent --key or key_path; create it with identity create")
		}
		c.Logger = slog.New(slog.NewJSONHandler(errout, nil))
		n, e := m.NewNode(c)
		if e != nil {
			return e
		}
		defer n.Close()
		ctx, stop := context.WithCancel(ctx)
		defer stop()
		if *echo {
			n.Handle(func(_ context.Context, _ string, p []byte) ([]byte, error) { return p, nil })
		}
		if e = n.Start(ctx); e != nil {
			return e
		}
		srv, ln, e := control(n, *socket, stop)
		if e != nil {
			return e
		}
		defer ln.Close()
		done := make(chan error, 1)
		go func() { done <- srv.Serve(ln) }()
		fmt.Fprintf(out, "node %s listening on %s; control %s\n", n.ID(), n.Address(), *socket)
		select {
		case e = <-done:
			if !errors.Is(e, http.ErrServerClosed) {
				return e
			}
		case <-ctx.Done():
		}
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if e = srv.Shutdown(shutdown); e != nil {
			srv.Close()
			return e
		}
		return nil
	}
	c := command{}
	switch group {
	case "diagnostics":
		c.Op = "diagnostics"
	case "peer":
		switch sub {
		case "list":
			c.Op = "peers"
		case "connect":
			if len(pos) != 1 || *peer == "" {
				return errors.New("peer connect requires --peer ID HOST:PORT")
			}
			c.Op = "connect"
			c.Peer = *peer
			c.Address = pos[0]
		case "disconnect":
			if len(pos) != 1 {
				return errors.New("peer disconnect requires ID")
			}
			c.Op = "disconnect"
			c.Peer = pos[0]
		default:
			return errors.New("unknown peer command")
		}
	case "message":
		if sub != "send" && sub != "request" {
			return errors.New("unknown message command")
		}
		if len(pos) != 2 {
			return errors.New("message command requires PEER_ID TEXT")
		}
		c.Op = sub
		c.Peer = pos[0]
		c.Payload = []byte(pos[1])
	default:
		return errors.New("unknown command; run meshlink --help")
	}
	res, e := call(ctx, *socket, c)
	if e != nil {
		return e
	}
	if *jsonOut {
		return json.NewEncoder(out).Encode(res)
	}
	if res.Diagnostics != nil {
		d := res.Diagnostics
		fmt.Fprintf(out, "node %s\nstate: %s  transport: %s  address: %s\npeers: %d  sent: %d bytes  received: %d bytes  uptime: %s\n", d.ID, d.State, d.Transport, d.Address, len(d.Peers), d.BytesSent, d.BytesReceived, d.Uptime)
		return nil
	}
	if c.Op == "peers" {
		for _, p := range res.Peers {
			fmt.Fprintf(out, "%s  %s  %s\n", p.ID, p.State, p.Address)
		}
		return nil
	}
	if c.Op == "request" {
		fmt.Fprintln(out, string(res.Payload))
	} else {
		fmt.Fprintln(out, "ok")
	}
	return nil
}
func call(ctx context.Context, path string, c command) (reply, error) {
	var res reply
	b, e := json.Marshal(c)
	if e != nil {
		return res, e
	}
	tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", path)
	}}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 12 * time.Second}
	r, e := http.NewRequestWithContext(ctx, "POST", "http://meshlink/v1/command", bytes.NewReader(b))
	if e != nil {
		return res, e
	}
	r.Header.Set("Content-Type", "application/json")
	response, e := client.Do(r)
	if e != nil {
		return res, fmt.Errorf("control request (is node running at %s?): %w", path, e)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return res, fmt.Errorf("control HTTP status %d", response.StatusCode)
	}
	if e = strictDecode(io.LimitReader(response.Body, 24<<20), &res); e != nil {
		return res, e
	}
	if res.Error != "" {
		return res, errors.New(res.Error)
	}
	return res, nil
}
