//go:build !windows

package main

import (
	"io"
	"os"
)

func consoleWriter() io.Writer { return os.Stderr }
