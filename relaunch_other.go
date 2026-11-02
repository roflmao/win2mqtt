//go:build !windows

package main

import (
	"log"
	"runtime"
)

func nativeArch() string { return runtime.GOARCH }

func relaunch() error {
	log.Println("Relaunch after update is only supported on Windows")
	return nil
}
