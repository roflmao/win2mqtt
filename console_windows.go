//go:build windows

package main

import (
	"io"
	"os"

	"golang.org/x/sys/windows"
)

const attachParentProcess = ^uint32(0) // ATTACH_PARENT_PROCESS

var procAttachConsole = windows.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole")

// consoleWriter attaches to the parent console when the GUI-subsystem binary was
// started from a terminal. Go's os.Stdout is unusable in that case, so CONOUT$ is
// opened explicitly. Without a parent console, output goes nowhere.
func consoleWriter() io.Writer {
	if r, _, _ := procAttachConsole.Call(uintptr(attachParentProcess)); r == 0 {
		return io.Discard
	}
	out, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0)
	if err != nil {
		return io.Discard
	}
	return out
}
