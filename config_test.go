package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "win2mqtt.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadConfigIntPort(t *testing.T) {
	c, err := loadConfig(writeTempConfig(t, "mqtt_ip: 10.0.0.1\nmqtt_port: 1883\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Port != "1883" {
		t.Fatalf("Port = %q, want %q", c.Port, "1883")
	}
}

func TestLoadConfigMissingFile(t *testing.T) {
	_, err := loadConfig("/nonexistent/win2mqtt.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "/nonexistent/win2mqtt.yaml") {
		t.Fatalf("error %q does not name the path", err)
	}
}

func TestLoadConfigRequiresBroker(t *testing.T) {
	_, err := loadConfig(writeTempConfig(t, "mqtt_port: 1883\n"))
	if err == nil || !strings.Contains(err.Error(), "mqtt_ip") {
		t.Fatalf("expected error mentioning mqtt_ip, got %v", err)
	}
	if _, err := loadConfig(writeTempConfig(t, "dry_run: true\n")); err != nil {
		t.Fatalf("dry_run config should not require broker, got %v", err)
	}
}

func TestGetIdleThreshold(t *testing.T) {
	tests := []struct {
		name string
		cfg  config
		want int
	}{
		{"default when unset", config{}, 300},
		{"default when zero", config{IdleThreshold: 0}, 300},
		{"default when negative", config{IdleThreshold: -5}, 300},
		{"custom value", config{IdleThreshold: 60}, 60},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.getIdleThreshold(); got != tt.want {
				t.Fatalf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestIsAutoUpdateEnabled(t *testing.T) {
	f, tr := false, true
	tests := []struct {
		name string
		cfg  config
		want bool
	}{
		{"nil defaults to true", config{}, true},
		{"explicit false", config{AutoUpdate: &f}, false},
		{"explicit true", config{AutoUpdate: &tr}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.isAutoUpdateEnabled(); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}
