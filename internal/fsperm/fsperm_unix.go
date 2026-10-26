//go:build !windows

package fsperm

import (
	"fmt"
	"os"
)

const POSIX = true

func privateFile(_ *os.File, fi os.FileInfo) error { return privateDir(fi) }

func privateDir(fi os.FileInfo) error {
	if fi.Mode().Perm()&0077 != 0 {
		return ErrNotPrivate
	}
	return nil
}

func Restrict(string) error { return nil }

func hint(path string) string { return fmt.Sprintf("chmod 600 %q", path) }
