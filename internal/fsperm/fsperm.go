package fsperm

import (
	"errors"
	"fmt"
	"os"
)

var ErrNotPrivate = errors.New("file is accessible to other users")

func ReadPrivate(path string, max int64) ([]byte, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	fi, e := f.Stat()
	if e != nil {
		return nil, e
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: expected a regular file", path)
	}
	if e = privateFile(f, fi); e != nil {
		return nil, fmt.Errorf("%s: %w; fix with: %s", path, e, hint(path))
	}
	if fi.Size() > max {
		return nil, fmt.Errorf("%s: larger than %d bytes", path, max)
	}
	b := make([]byte, fi.Size())
	if _, e = f.ReadAt(b, 0); e != nil {
		return nil, e
	}
	return b, nil
}

func PrivateDir(fi os.FileInfo) error {
	if !fi.IsDir() {
		return errors.New("expected a directory")
	}
	return privateDir(fi)
}
