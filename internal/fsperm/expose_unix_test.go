//go:build !windows

package fsperm

import (
	"os"
	"testing"
)

func exposeToEveryone(t *testing.T, path string) {
	if e := os.Chmod(path, 0644); e != nil {
		t.Fatal(e)
	}
}
