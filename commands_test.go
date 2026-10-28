package main

import (
	"strings"
	"testing"
)

func TestVolumeCommandSetsAndRepublishes(t *testing.T) {
	sys := newFakeSystem()
	a := newTestApp(t, sys)
	rc := &recordingClient{}

	a.handleCommand(rc, "win2mqtt/h/command/volume", " 50\n")

	if v, _ := sys.Volume(); v != 50 {
		t.Fatalf("volume = %d, want 50", v)
	}
	if msg, ok := rc.find("win2mqtt/h/status/volume"); !ok || msg.payload != "50" {
		t.Fatalf("status/volume = %+v (found=%v), want 50", msg, ok)
	}
}

func TestVolumeCommandRejectsBadValues(t *testing.T) {
	for _, payload := range []string{"101", "-1", "abc"} {
		buf := captureLog(t)
		sys := newFakeSystem()
		a := newTestApp(t, sys)
		rc := &recordingClient{}

		a.handleCommand(rc, "win2mqtt/h/command/volume", payload)

		if v, _ := sys.Volume(); v != 42 {
			t.Errorf("payload %q: volume changed to %d", payload, v)
		}
		if _, ok := rc.find("win2mqtt/h/status/volume"); ok {
			t.Errorf("payload %q: status/volume published", payload)
		}
		if !strings.Contains(buf.String(), "Ignoring command") {
			t.Errorf("payload %q: no 'Ignoring command' log line", payload)
		}
	}
}

func TestMuteCommand(t *testing.T) {
	sys := newFakeSystem()
	a := newTestApp(t, sys)
	rc := &recordingClient{}

	a.handleCommand(rc, "win2mqtt/h/command/mute", "true")

	if m, _ := sys.Mute(); !m {
		t.Fatal("mute not set")
	}
	if msg, ok := rc.find("win2mqtt/h/status/mute"); !ok || msg.payload != "true" {
		t.Fatalf("status/mute = %+v (found=%v), want true", msg, ok)
	}
}

func TestButtonCommandsCallSystem(t *testing.T) {
	for _, name := range []string{"sleep", "displaysleep", "shutdown", "reboot", "lock"} {
		sys := newFakeSystem()
		a := newTestApp(t, sys)
		rc := &recordingClient{}

		a.handleCommand(rc, "win2mqtt/h/command/"+name, name)
		if len(sys.calls) != 1 || sys.calls[0] != name {
			t.Errorf("%s: calls = %v, want [%s]", name, sys.calls, name)
		}

		sys.calls = nil
		a.handleCommand(rc, "win2mqtt/h/command/"+name, "x")
		if len(sys.calls) != 0 {
			t.Errorf("%s: wrong payload triggered %v", name, sys.calls)
		}
	}
}

func TestUnknownCommandIgnored(t *testing.T) {
	buf := captureLog(t)
	a := newTestApp(t, newFakeSystem())

	a.handleCommand(&recordingClient{}, "win2mqtt/h/command/bogus", "1")

	if !strings.Contains(buf.String(), "Ignoring command") {
		t.Fatalf("expected 'Ignoring command' log, got:\n%s", buf.String())
	}
}
