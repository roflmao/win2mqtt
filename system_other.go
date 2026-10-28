//go:build !windows

package main

import (
	"log"
	"sync"
	"time"
)

// stubSystem stands in for Windows on other platforms so the MQTT flow can be
// exercised with dry_run or against a real broker.
type stubSystem struct {
	mu       sync.Mutex
	volume   int
	muted    bool
	bytesIn  int64
	bytesOut int64
}

func newSystem() (System, error) {
	return &stubSystem{volume: 42}, nil
}

func (s *stubSystem) Features() Features           { return Features{} }
func (s *stubSystem) DeviceInfo() (string, string) { return "Stub", "Linux host" }
func (s *stubSystem) ActiveApp() (string, error)   { return "stub", nil }
func (s *stubSystem) IdleSeconds() (int64, error)  { return 0, nil }
func (s *stubSystem) DisplayOn() bool              { return true }
func (s *stubSystem) ScreenLocked() bool           { return false }
func (s *stubSystem) IPAddress() (string, error)   { return "127.0.0.1", nil }
func (s *stubSystem) BootTime() (time.Time, error) { return time.Now().Add(-time.Hour), nil }
func (s *stubSystem) Battery() (int, error)        { return 0, errNoBattery }
func (s *stubSystem) WiFi() (string, int, error)   { return "", 0, errNoWiFi }

func (s *stubSystem) Volume() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.volume, nil
}

func (s *stubSystem) Mute() (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.muted, nil
}

func (s *stubSystem) SetVolume(percent int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.volume = percent
	return nil
}

func (s *stubSystem) SetMute(muted bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.muted = muted
	return nil
}

func (s *stubSystem) NetCounters() (int64, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bytesIn += 1024
	s.bytesOut += 512
	return s.bytesIn, s.bytesOut, nil
}

func (s *stubSystem) stubCommand(name string) error {
	log.Printf("%s requested (stub)", name)
	return nil
}

func (s *stubSystem) Sleep() error        { return s.stubCommand("sleep") }
func (s *stubSystem) DisplaySleep() error { return s.stubCommand("displaysleep") }
func (s *stubSystem) Shutdown() error     { return s.stubCommand("shutdown") }
func (s *stubSystem) Reboot() error       { return s.stubCommand("reboot") }
func (s *stubSystem) Lock() error         { return s.stubCommand("lock") }
