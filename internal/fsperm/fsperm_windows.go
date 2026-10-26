package fsperm

import "os"

const Enforced = false

func private(os.FileInfo) error { return nil }
