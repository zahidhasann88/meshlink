package fsperm

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReadPrivate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret")
	if e := os.WriteFile(path, []byte("token"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := Restrict(path); e != nil {
		t.Fatal(e)
	}
	if b, e := ReadPrivate(path, 64); e != nil || string(b) != "token" {
		t.Fatal(string(b), e)
	}
	if _, e := ReadPrivate(path, 2); e == nil {
		t.Fatal("ignored size limit")
	}
	if _, e := ReadPrivate(dir, 64); e == nil {
		t.Fatal("accepted a directory")
	}
	fi, _ := os.Stat(path)
	if e := PrivateDir(fi); e == nil {
		t.Fatal("accepted file as directory")
	}
	exposeToEveryone(t, path)
	if _, e := ReadPrivate(path, 64); !errors.Is(e, ErrNotPrivate) {
		t.Fatal("accepted a file other users can read:", e)
	}
	if e := Restrict(path); e != nil {
		t.Fatal(e)
	}
	if POSIX {
		os.Chmod(path, 0600)
	}
	if _, e := ReadPrivate(path, 64); e != nil {
		t.Fatal("restricted file rejected:", e)
	}
}
