package fsperm

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPrivate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret")
	if e := os.WriteFile(path, []byte("x"), 0600); e != nil {
		t.Fatal(e)
	}
	fi, _ := os.Stat(path)
	if e := PrivateFile(fi); e != nil {
		t.Fatal(e)
	}
	if e := PrivateDir(fi); e == nil {
		t.Fatal("accepted file as directory")
	}
	dfi, _ := os.Stat(dir)
	if e := PrivateFile(dfi); e == nil {
		t.Fatal("accepted directory as file")
	}
	if !Enforced {
		t.Skip("POSIX permission bits are not enforced on this platform")
	}
	os.Chmod(path, 0644)
	fi, _ = os.Stat(path)
	if e := PrivateFile(fi); !errors.Is(e, ErrNotPrivate) {
		t.Fatal("accepted group/other-readable file", e)
	}
}
