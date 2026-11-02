package main

import (
	"errors"
	"runtime"
	"strings"
	"testing"
)

func TestIsNewerVersion(t *testing.T) {
	tests := []struct {
		current string
		latest  string
		want    bool
	}{
		{"v2.0.8", "v2.0.9", true},
		{"v2.0.8", "v2.0.10", true}, // two-digit patch must not sort as a string
		{"v2.0.8", "v2.0.11", true},
		{"v2.0.9", "v2.0.10", true},
		{"v2.0.10", "v2.0.9", false},
		{"v2.0.10", "v2.0.10", false},
		{"v2.0.8", "v2.1.0", true},
		{"v2.1.0", "v2.0.99", false},
		{"v2.0.8", "v3.0.0", true},
		{"v3.0.0", "v2.9.9", false},
		{"2.0.8", "v2.0.10", true}, // prefix optional on either side
		{"v2.0.8", "2.0.10", true},
		{"dev", "v2.0.10", true},
		{"v2.0.8", "garbage", false}, // unparseable release tag is never newer
		{"v2.0.8", "", false},
	}
	for _, tt := range tests {
		if got := isNewerVersion(tt.current, tt.latest); got != tt.want {
			t.Errorf("isNewerVersion(%q, %q) = %v, want %v", tt.current, tt.latest, got, tt.want)
		}
	}
}

func TestGetBinaryNameForPlatform(t *testing.T) {
	want := "win2mqtt-windows-" + runtime.GOARCH + ".exe"
	if got := getBinaryNameForPlatform(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFindAssetForCurrentPlatform(t *testing.T) {
	release := &GitHubRelease{Assets: []GitHubAsset{
		{Name: "win2mqtt-windows-amd64.exe"},
		{Name: "win2mqtt-windows-arm64.exe"},
	}}
	asset, err := findAssetForCurrentPlatform(release)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if asset.Name != getBinaryNameForPlatform() {
		t.Fatalf("picked %q, want %q", asset.Name, getBinaryNameForPlatform())
	}

	if _, err := findAssetForCurrentPlatform(&GitHubRelease{}); err == nil {
		t.Fatal("expected error for release without assets")
	}
}

func TestFinishUpdateKeepsRunningWhenRelaunchFails(t *testing.T) {
	buf := captureLog(t)
	restartPending.Store(false)
	t.Cleanup(func() { restartPending.Store(false) })

	exit := finishUpdate(func() error { return errors.New("no task, no exec") })

	if exit {
		t.Fatal("process must not exit when nothing will restart it")
	}
	if !restartPending.Load() {
		t.Fatal("restartPending should be set so further update checks are skipped")
	}
	buf.Reset()
	checkAndApplyUpdate()
	if !strings.Contains(buf.String(), "waiting for restart") {
		t.Fatalf("update check should be skipped while a restart is pending, got:\n%s", buf.String())
	}
}

func TestFinishUpdateExitsWhenRelaunchScheduled(t *testing.T) {
	captureLog(t)
	t.Cleanup(func() { restartPending.Store(false) })
	if !finishUpdate(func() error { return nil }) {
		t.Fatal("process should exit once the relaunch is scheduled")
	}
}
