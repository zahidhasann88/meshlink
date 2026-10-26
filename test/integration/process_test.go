package integration_test

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	m "github.com/zahidhasann88/meshlink/pkg/meshlink"
)

func TestIndependentCLIProcesses(t *testing.T) {
	if testing.Short() {
		t.Skip("subprocess integration test disabled by -short")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	bin := filepath.Join(dir, "meshlink"+exeSuffix())
	build := []string{"build", "-o", bin}
	if raceEnabled {
		build = append(build, "-race")
	}
	if b, e := exec.CommandContext(ctx, "go", append(build, "../../cmd/meshlink")...).CombinedOutput(); e != nil {
		t.Fatalf("build: %v %s", e, b)
	}
	ids := identities(t, 2)
	sockets := []string{filepath.Join(dir, "a.sock"), filepath.Join(dir, "b.sock")}
	addresses := make([]string, 2)
	procs := make([]*exec.Cmd, 2)
	for i := 0; i < 2; i++ {
		key := filepath.Join(dir, string(rune('a'+i))+".key")
		if e := ids[i].Save(key); e != nil {
			t.Fatal(e)
		}
		config := filepath.Join(dir, string(rune('a'+i))+".json")
		b, _ := json.Marshal(m.Config{KeyPath: key, ListenAddress: "127.0.0.1:0", AllowedPeers: []string{ids[1-i].ID()}})
		if e := os.WriteFile(config, b, 0600); e != nil {
			t.Fatal(e)
		}
		cmd := exec.CommandContext(ctx, bin, "node", "start", "--config", config, "--socket", sockets[i], "--echo")
		pipe, e := cmd.StdoutPipe()
		if e != nil {
			t.Fatal(e)
		}
		if e = cmd.Start(); e != nil {
			t.Fatal(e)
		}
		procs[i] = cmd
		t.Cleanup(func() {
			if cmd.ProcessState == nil {
				cmd.Process.Kill()
				cmd.Wait()
			}
		})
		ready := make(chan string, 1)
		go func() {
			scanner := bufio.NewScanner(pipe)
			if scanner.Scan() {
				ready <- scanner.Text()
			} else {
				ready <- ""
			}
		}()
		select {
		case line := <-ready:
			fields := strings.Fields(line)
			if len(fields) < 5 {
				t.Fatalf("no startup line: %q", line)
			}
			addresses[i] = strings.TrimSuffix(fields[4], ";")
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	run := func(args ...string) string {
		t.Helper()
		b, e := exec.CommandContext(ctx, bin, args...).CombinedOutput()
		if e != nil {
			t.Fatalf("CLI %v: %v %s", args, e, b)
		}
		return strings.TrimSpace(string(b))
	}
	run("peer", "connect", "--socket", sockets[0], "--peer", ids[1].ID(), addresses[1])
	if got := run("message", "request", "--socket", sockets[0], ids[1].ID(), "across independent processes"); got != "across independent processes" {
		t.Fatal(got)
	}
	run("message", "send", "--socket", sockets[0], ids[1].ID(), "one-way")
	if got := run("diagnostics", "--socket", sockets[0], "--json"); !strings.Contains(got, `"tcp+tls"`) {
		t.Fatal(got)
	}

	run("node", "stop", "--socket", sockets[0])
	if runtime.GOOS == "windows" {
		run("node", "stop", "--socket", sockets[1])
	} else if e := procs[1].Process.Signal(os.Interrupt); e != nil {
		t.Fatal(e)
	}
	for i, cmd := range procs {
		if e := cmd.Wait(); e != nil {
			t.Errorf("node %d shutdown: %v", i, e)
		}
		if _, e := os.Stat(sockets[i]); !os.IsNotExist(e) {
			t.Errorf("node %d left its control socket behind: %v", i, e)
		}
	}
}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}
