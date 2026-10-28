package main

import (
	"strings"
	"testing"
)

func TestRelaunchCommandLineWorksWithoutConsole(t *testing.T) {
	cmd := relaunchCommandLine("win2mqtt", `C:\Apps\win2mqtt.exe`)

	// timeout.exe refuses to run when stdin is not a console, which is always the
	// case for a detached child of a GUI-subsystem process.
	if strings.Contains(cmd, "timeout") {
		t.Fatalf("relaunch must not rely on timeout.exe: %q", cmd)
	}
	if !strings.Contains(cmd, "ping -n 4 127.0.0.1") {
		t.Fatalf("relaunch should delay with ping: %q", cmd)
	}
	if !strings.Contains(cmd, "schtasks /Run /TN win2mqtt") {
		t.Fatalf("relaunch must re-run the scheduled task: %q", cmd)
	}
	// If the task run fails (task disabled or deleted meanwhile) the shell must
	// start the executable directly, since the old process has already exited.
	if !strings.Contains(cmd, `|| start "" "C:\Apps\win2mqtt.exe"`) {
		t.Fatalf("relaunch must fall back to starting the exe: %q", cmd)
	}
}

func TestBootTimeFromUptime(t *testing.T) {
	now := mustTime(t, "2026-10-28T12:00:00.750Z")
	got := bootTimeFromUptime(now, 90_500) // 90.5 s of uptime
	want := mustTime(t, "2026-10-28T11:58:30Z")
	if !got.Equal(want) {
		t.Fatalf("bootTimeFromUptime = %v, want %v", got, want)
	}
}
