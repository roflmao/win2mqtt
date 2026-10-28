//go:build windows

package main

import (
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"syscall"

	"golang.org/x/sys/windows"
)

const scheduledTaskName = "win2mqtt"

var procIsWow64Process2 = windows.NewLazySystemDLL("kernel32.dll").NewProc("IsWow64Process2")

// nativeArch reports the machine architecture even when an amd64 build runs
// emulated on ARM64, so auto-update picks the native binary.
func nativeArch() string {
	const imageFileMachineARM64 = 0xAA64
	if err := procIsWow64Process2.Find(); err != nil {
		return runtime.GOARCH
	}
	var processMachine, nativeMachine uint16
	r, _, _ := procIsWow64Process2.Call(
		uintptr(windows.CurrentProcess()),
		uintptr(unsafePointer(&processMachine)),
		uintptr(unsafePointer(&nativeMachine)),
	)
	if r == 0 {
		return runtime.GOARCH
	}
	if nativeMachine == imageFileMachineARM64 {
		return "arm64"
	}
	return runtime.GOARCH
}

func detachedAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NO_WINDOW | windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS,
	}
}

// relaunch restarts win2mqtt after an update. When installed as a Scheduled Task
// the task is re-run so the new process stays supervised; otherwise the new
// executable is started directly.
func relaunch() error {
	query := exec.Command("schtasks", "/Query", "/TN", scheduledTaskName)
	query.SysProcAttr = detachedAttr()
	if err := query.Run(); err == nil {
		cmd := exec.Command("cmd", "/c", "timeout /t 3 /nobreak >nul & schtasks /Run /TN "+scheduledTaskName)
		cmd.SysProcAttr = detachedAttr()
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("start scheduled task relaunch: %w", err)
		}
		log.Printf("Scheduled task %s will restart win2mqtt in 3 seconds", scheduledTaskName)
		return nil
	}

	exePath, err := getCurrentExecutablePath()
	if err != nil {
		return err
	}
	cmd := exec.Command(exePath)
	cmd.SysProcAttr = detachedAttr()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start new executable: %w", err)
	}
	log.Printf("Started new executable %s (pid %d)", exePath, cmd.Process.Pid)
	return nil
}
