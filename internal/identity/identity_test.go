package identity

import (
	"bytes"
	"crypto/ed25519"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/zahidhasann88/meshlink/internal/fsperm"
)

func TestRoundTrip(t *testing.T) {
	a, e := Generate()
	if e != nil {
		t.Fatal(e)
	}
	b, e := a.MarshalPEM()
	if e != nil {
		t.Fatal(e)
	}
	c, e := Parse(b)
	if e != nil || a.ID() != c.ID() || !ValidID(a.ID()) {
		t.Fatal(e)
	}
	if bytes.Contains(b, []byte(a.ID())) {
		t.Fatal("unexpected")
	}
	path := filepath.Join(t.TempDir(), "node.key")
	if e = a.Save(path); e != nil {
		t.Fatal(e)
	}
	if e = a.Save(path); e == nil {
		t.Fatal("overwrote key")
	}
	d, e := Load(path)
	if e != nil || d.ID() != a.ID() {
		t.Fatal(e)
	}
	if fsperm.POSIX {
		os.Chmod(path, 0644)
		if _, e = Load(path); e == nil {
			t.Fatal("accepted unsafe mode")
		}
	}
	if _, e = Parse(append(b, b...)); e == nil {
		t.Fatal("accepted multiple keys")
	}
}
func FuzzIdentity(f *testing.F) {
	a, _ := Generate()
	b, _ := a.MarshalPEM()
	f.Add(b)
	f.Add([]byte("invalid"))
	f.Fuzz(func(t *testing.T, b []byte) {
		i, e := Parse(b)
		if e == nil && !ValidID(i.ID()) {
			t.Fatal("invalid derived ID")
		}
	})
}

func TestIDVectorAndRedaction(t *testing.T) {
	if got := ID(make(ed25519.PublicKey, 32)); got != "66687aadf862bd776c8fc18b8e9f8e20089714856ee233b3902a591d0d5f2925" {
		t.Fatal(got)
	}
	i, _ := Generate()
	for _, format := range []string{"%v", "%+v", "%#v"} {
		if got := fmt.Sprintf(format, i); got != "Identity("+i.ID()+")" {
			t.Fatal("unsafe diagnostic representation")
		}
	}
	var zero Identity
	if _, e := zero.MarshalPEM(); e == nil {
		t.Fatal("serialized invalid identity")
	}
}
