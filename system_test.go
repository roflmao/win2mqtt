package main

import "testing"

func TestIsUserActive(t *testing.T) {
	tests := []struct {
		idleSeconds int64
		threshold   int
		want        bool
	}{
		{0, 300, true},
		{299, 300, true},
		{300, 300, false},
		{301, 300, false},
		{5, 10, true},
		{10, 10, false},
	}
	for _, tt := range tests {
		if got := isUserActive(tt.idleSeconds, tt.threshold); got != tt.want {
			t.Errorf("isUserActive(%d, %d) = %v, want %v", tt.idleSeconds, tt.threshold, got, tt.want)
		}
	}
}

func TestQualityToRSSI(t *testing.T) {
	tests := []struct {
		quality uint32
		want    int
	}{
		{0, -100},
		{50, -75},
		{100, -50},
	}
	for _, tt := range tests {
		if got := qualityToRSSI(tt.quality); got != tt.want {
			t.Errorf("qualityToRSSI(%d) = %d, want %d", tt.quality, got, tt.want)
		}
	}
}

func TestExeBaseName(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{`C:\Windows\explorer.exe`, "explorer"},
		{`C:/x/Code.EXE`, "Code"},
		{`notepad`, "notepad"},
		{``, ``},
	}
	for _, tt := range tests {
		if got := exeBaseName(tt.path); got != tt.want {
			t.Errorf("exeBaseName(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestIdleFromTicks(t *testing.T) {
	tests := []struct {
		lastInput, now uint32
		want           int64
	}{
		{1000, 5000, 4},
		{0xFFFFF000, 0x1000, 8}, // tick counter wrapped
		{5000, 5000, 0},
	}
	for _, tt := range tests {
		if got := idleFromTicks(tt.lastInput, tt.now); got != tt.want {
			t.Errorf("idleFromTicks(%d, %d) = %d, want %d", tt.lastInput, tt.now, got, tt.want)
		}
	}
}

func TestSanitizeHostname(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"DESKTOP-AB12.local", "DESKTOP-AB12"},
		{"Rolf's PC", "RolfsPC"},
		{"ÆØÅ", "windows"},
		{"", "windows"},
	}
	for _, tt := range tests {
		if got := sanitizeHostname(tt.raw); got != tt.want {
			t.Errorf("sanitizeHostname(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}
