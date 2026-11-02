package main

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

// Features lists optional hardware a System detected at startup.
// Sensors for absent hardware are neither published nor announced to Home Assistant.
type Features struct {
	Battery bool
	WiFi    bool
}

// System is everything win2mqtt needs from the operating system.
// system_windows.go implements it with Win32/COM calls; system_other.go is a stub.
type System interface {
	Features() Features
	DeviceInfo() (manufacturer, model string)
	Volume() (int, error)
	Mute() (bool, error)
	SetVolume(percent int) error
	SetMute(muted bool) error
	ActiveApp() (string, error)
	IdleSeconds() (int64, error)
	DisplayOn() bool
	ScreenLocked() bool
	NetCounters() (bytesIn, bytesOut int64, err error)
	IPAddress() (string, error)
	BootTime() (time.Time, error)
	Battery() (percent int, err error)
	WiFi() (ssid string, rssi int, err error)
	Sleep() error
	DisplaySleep() error
	Shutdown() error
	Reboot() error
	Lock() error
}

func isUserActive(idleSeconds int64, threshold int) bool {
	return idleSeconds < int64(threshold)
}

// qualityToRSSI converts the WLAN API's 0-100 signal quality to an approximate dBm value.
func qualityToRSSI(quality uint32) int {
	return int(quality)/2 - 100
}

// exeBaseName turns a full image path like C:\Windows\explorer.exe into "explorer".
func exeBaseName(path string) string {
	if i := strings.LastIndexAny(path, `\/`); i >= 0 {
		path = path[i+1:]
	}
	if strings.HasSuffix(strings.ToLower(path), ".exe") {
		path = path[:len(path)-4]
	}
	return path
}

// idleFromTicks returns whole seconds between two 32-bit millisecond tick counts,
// tolerating the counter wrapping every 49.7 days.
func idleFromTicks(lastInput, now uint32) int64 {
	return int64(now-lastInput) / 1000
}

var hostnameUnsafe = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

// sanitizeHostname keeps the first DNS label and only topic-safe characters.
func sanitizeHostname(raw string) string {
	first := strings.Split(raw, ".")[0]
	clean := hostnameUnsafe.ReplaceAllString(first, "")
	if clean == "" {
		return "windows"
	}
	return clean
}

var (
	errNoBattery = errors.New("no battery present")
	errNoWiFi    = errors.New("no Wi-Fi adapter present")
)
