package fsperm

import "os"

const Enforced = true

func private(fi os.FileInfo) error {
	if fi.Mode().Perm()&0077 != 0 {
		return ErrNotPrivate
	}
	return nil
}
