package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func testDevice() map[string]interface{} {
	return map[string]interface{}{"identifiers": []string{"win2mqtt_h"}, "name": "h"}
}

func TestDiscoveryEntityCount(t *testing.T) {
	tests := []struct {
		feat Features
		want int
	}{
		{Features{}, 18},
		{Features{Battery: true}, 19},
		{Features{Battery: true, WiFi: true}, 21},
	}
	for _, tt := range tests {
		got := len(discoveryEntities("h", "win2mqtt/h", tt.feat, testDevice()))
		if got != tt.want {
			t.Errorf("features %+v: %d entities, want %d", tt.feat, got, tt.want)
		}
	}
}

func TestDiscoveryUniqueIDsAndTopics(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range discoveryEntities("h", "win2mqtt/h", Features{Battery: true, WiFi: true}, testDevice()) {
		if seen[e.objectID] {
			t.Errorf("duplicate objectID %s", e.objectID)
		}
		seen[e.objectID] = true

		if uid := e.config["unique_id"]; uid != "win2mqtt_h_"+e.objectID {
			t.Errorf("%s: unique_id = %v", e.objectID, uid)
		}
		for _, key := range []string{"state_topic", "command_topic", "availability_topic"} {
			if v, ok := e.config[key]; ok && !strings.HasPrefix(v.(string), "win2mqtt/h/") {
				t.Errorf("%s: %s = %v", e.objectID, key, v)
			}
		}
	}
}

func TestDiscoveryLockEntities(t *testing.T) {
	byID := map[string]discoveryEntity{}
	for _, e := range discoveryEntities("h", "win2mqtt/h", Features{}, testDevice()) {
		byID[e.objectID] = e
	}

	lock := byID["lock"]
	if lock.component != "button" || lock.config["command_topic"] != "win2mqtt/h/command/lock" || lock.config["payload_press"] != "lock" {
		t.Errorf("lock entity wrong: %+v", lock)
	}

	locked := byID["screen_locked"]
	if locked.component != "binary_sensor" || locked.config["state_topic"] != "win2mqtt/h/status/screen_locked" || locked.config["payload_on"] != "true" {
		t.Errorf("screen_locked entity wrong: %+v", locked)
	}
}

func TestPublishDiscoveryTopics(t *testing.T) {
	a := newTestApp(t, newFakeSystem())
	rc := &recordingClient{}

	a.publishDiscoveryMessages(rc)

	configs := 0
	for _, m := range rc.msgs {
		if m.payload != "" {
			configs++
		}
	}
	if configs != 18 {
		t.Fatalf("published %d configs, want 18", configs)
	}
	msg, ok := rc.find("homeassistant/button/win2mqtt_h/lock/config")
	if !ok || !msg.retained {
		t.Fatalf("lock config missing or not retained: %+v", msg)
	}
	var cfg struct {
		Device struct {
			Identifiers []string `json:"identifiers"`
		} `json:"device"`
	}
	if err := json.Unmarshal([]byte(msg.payload), &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Device.Identifiers) != 1 || cfg.Device.Identifiers[0] != "win2mqtt_h" {
		t.Fatalf("device identifiers = %v", cfg.Device.Identifiers)
	}
}

func TestPublishDiscoveryClearsAbsentOptionalEntities(t *testing.T) {
	a := newTestApp(t, newFakeSystem()) // Features{}: no battery, no Wi-Fi
	rc := &recordingClient{}

	a.publishDiscoveryMessages(rc)

	for _, topic := range []string{
		"homeassistant/sensor/win2mqtt_h/battery/config",
		"homeassistant/sensor/win2mqtt_h/wifi_ssid/config",
		"homeassistant/sensor/win2mqtt_h/wifi_signal_strength/config",
	} {
		msg, ok := rc.find(topic)
		if !ok {
			t.Errorf("%s: no clearing publish for absent hardware", topic)
			continue
		}
		if msg.payload != "" || !msg.retained {
			t.Errorf("%s: want empty retained payload, got %+v", topic, msg)
		}
	}
}
