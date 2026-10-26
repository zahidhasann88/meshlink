package fsperm

import (
	"errors"
	"os"
)

var ErrNotPrivate = errors.New("path is accessible to other users")

func PrivateFile(fi os.FileInfo) error {
	if !fi.Mode().IsRegular() {
		return errors.New("expected a regular file")
	}
	return private(fi)
}

func PrivateDir(fi os.FileInfo) error {
	if !fi.IsDir() {
		return errors.New("expected a directory")
	}
	return private(fi)
}
