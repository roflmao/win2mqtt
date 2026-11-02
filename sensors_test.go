package main

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSystem overrides selected stub behaviour for tests.
type fakeSystem struct {
	stubSystem
	feat      Features
	volumeErr error
	counters  [][2]int64
	callsMu   sync.Mutex
	calls     []string
	blockLock chan struct{} // when set, Lock() waits until it is closed
}

func (f *fakeSystem) record(name string) {
	f.callsMu.Lock()
	defer f.callsMu.Unlock()
	f.calls = append(f.calls, name)
}

func (f *fakeSystem) callCount() int {
	f.callsMu.Lock()
	defer f.callsMu.Unlock()
	return len(f.calls)
}

func (f *fakeSystem) Features() Features { return f.feat }

func (f *fakeSystem) Volume() (int, error) {
	if f.volumeErr != nil {
		return 0, f.volumeErr
	}
	return f.stubSystem.Volume()
}

func (f *fakeSystem) NetCounters() (int64, int64, error) {
	if len(f.counters) == 0 {
		return f.stubSystem.NetCounters()
	}
	c := f.counters[0]
	f.counters = f.counters[1:]
	return c[0], c[1], nil
}

func (f *fakeSystem) Battery() (int, error) {
	if f.feat.Battery {
		return 77, nil
	}
	return 0, errNoBattery
}

func (f *fakeSystem) WiFi() (string, int, error) {
	if f.feat.WiFi {
		return "HomeNet", -60, nil
	}
	return "", 0, errNoWiFi
}

func (f *fakeSystem) Sleep() error        { f.record("sleep"); return nil }
func (f *fakeSystem) DisplaySleep() error { f.record("displaysleep"); return nil }
func (f *fakeSystem) Shutdown() error     { f.record("shutdown"); return nil }
func (f *fakeSystem) Reboot() error       { f.record("reboot"); return nil }
func (f *fakeSystem) Lock() error {
	if f.blockLock != nil {
		<-f.blockLock
	}
	f.record("lock")
	return nil
}

func newFakeSystem() *fakeSystem {
	return &fakeSystem{stubSystem: stubSystem{volume: 42}}
}

func TestNetworkStatsFirstSampleIsZero(t *testing.T) {
	var n networkStats
	t0 := time.Unix(1000, 0)

	n.update(1000, 500, t0)

	if up, down := n.rates(); up != "0.00" || down != "0.00" {
		t.Fatalf("rates = (%s, %s), want (0.00, 0.00)", up, down)
	}
}

func TestNetworkStatsRate(t *testing.T) {
	var n networkStats
	t0 := time.Unix(1000, 0)
	n.update(1000, 500, t0)

	n.update(1000+2048, 500+1024, t0.Add(2*time.Second))

	if up, down := n.rates(); up != "0.50" || down != "1.00" {
		t.Fatalf("rates = (%s, %s), want (0.50, 1.00)", up, down)
	}
}

func TestNetworkStatsCounterResetClampsToZero(t *testing.T) {
	var n networkStats
	t0 := time.Unix(1000, 0)
	n.update(1000, 500, t0)
	n.update(1000+2048, 500+1024, t0.Add(2*time.Second))

	n.update(10, 5, t0.Add(4*time.Second))

	if up, down := n.rates(); up != "0.00" || down != "0.00" {
		t.Fatalf("rates = (%s, %s), want (0.00, 0.00)", up, down)
	}
}

func TestUpdateFastPublishesAllTopics(t *testing.T) {
	a := newTestApp(t, newFakeSystem())
	rc := &recordingClient{}

	a.updateFast(rc)

	want := map[string]string{
		"volume":        "42",
		"mute":          "false",
		"active_app":    "stub",
		"display_on":    "true",
		"screen_locked": "false",
		"idle_time":     "0",
		"user_active":   "true",
	}
	for suffix, payload := range want {
		msg, ok := rc.find("win2mqtt/h/status/" + suffix)
		if !ok {
			t.Errorf("%s not published", suffix)
			continue
		}
		if msg.payload != payload {
			t.Errorf("%s = %q, want %q", suffix, msg.payload, payload)
		}
	}
	for _, suffix := range []string{"network_upload_rate", "network_download_rate"} {
		if _, ok := rc.find("win2mqtt/h/status/" + suffix); !ok {
			t.Errorf("%s not published", suffix)
		}
	}
}

func TestUpdateVolumeErrorSkipsPublishAndLogsOnce(t *testing.T) {
	buf := captureLog(t)
	sys := newFakeSystem()
	sys.volumeErr = errors.New("no audio endpoint")
	a := newTestApp(t, sys)
	rc := &recordingClient{}

	for i := 0; i < 3; i++ {
		a.updateVolume(rc)
	}

	if _, ok := rc.find("win2mqtt/h/status/volume"); ok {
		t.Fatal("volume must not be published when the getter fails")
	}
	if n := strings.Count(buf.String(), "Warning: volume"); n != 1 {
		t.Fatalf("expected exactly one warning, got %d:\n%s", n, buf.String())
	}
}

func TestUpdateSlowSkipsOptionalSensors(t *testing.T) {
	a := newTestApp(t, newFakeSystem())
	rc := &recordingClient{}
	a.updateSlow(rc)
	for _, suffix := range []string{"battery", "wifi_ssid", "wifi_signal_strength"} {
		if _, ok := rc.find("win2mqtt/h/status/" + suffix); ok {
			t.Errorf("%s published although feature absent", suffix)
		}
	}
	for _, suffix := range []string{"ip_address", "uptime"} {
		if _, ok := rc.find("win2mqtt/h/status/" + suffix); !ok {
			t.Errorf("%s not published", suffix)
		}
	}

	sys := newFakeSystem()
	sys.feat = Features{Battery: true, WiFi: true}
	a = newTestApp(t, sys)
	rc = &recordingClient{}
	a.updateSlow(rc)
	want := map[string]string{"battery": "77", "wifi_ssid": "HomeNet", "wifi_signal_strength": "-60"}
	for suffix, payload := range want {
		msg, ok := rc.find("win2mqtt/h/status/" + suffix)
		if !ok || msg.payload != payload {
			t.Errorf("%s = %+v (found=%v), want %q", suffix, msg, ok, payload)
		}
	}
}
