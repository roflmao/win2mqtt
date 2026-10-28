package main

import (
	"encoding/json"
	"fmt"
	"log"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// discoveryEntity is one Home Assistant MQTT discovery config.
type discoveryEntity struct {
	component string
	objectID  string
	config    map[string]interface{}
}

// discoveryEntities lists every entity win2mqtt announces for a host.
// Battery and Wi-Fi entities are included only when the hardware exists.
func discoveryEntities(hostname, prefix string, feat Features, device map[string]interface{}) []discoveryEntity {
	var entities []discoveryEntity

	add := func(component, objectID, name string, fields map[string]interface{}, availability bool) {
		cfg := map[string]interface{}{
			"name":      name,
			"unique_id": topicRoot + "_" + hostname + "_" + objectID,
			"device":    device,
		}
		if availability {
			cfg["availability_topic"] = prefix + "/status/alive"
			cfg["payload_available"] = "true"
			cfg["payload_not_available"] = "false"
		}
		for k, v := range fields {
			cfg[k] = v
		}
		entities = append(entities, discoveryEntity{component: component, objectID: objectID, config: cfg})
	}
	status := func(s string) string { return prefix + "/status/" + s }
	command := func(s string) string { return prefix + "/command/" + s }

	add("binary_sensor", "alive", "Status", map[string]interface{}{
		"state_topic": status("alive"), "payload_on": "true", "payload_off": "false",
		"device_class": "connectivity",
	}, false)

	if feat.Battery {
		add("sensor", "battery", "Battery", map[string]interface{}{
			"state_topic": status("battery"), "unit_of_measurement": "%", "device_class": "battery",
		}, true)
	}

	add("sensor", "volume_sensor", "Volume Level", map[string]interface{}{
		"state_topic": status("volume"), "unit_of_measurement": "%", "icon": "mdi:volume-high",
	}, true)
	add("switch", "mute", "Mute", map[string]interface{}{
		"state_topic": status("mute"), "command_topic": command("mute"),
		"payload_on": "true", "payload_off": "false", "icon": "mdi:volume-mute",
	}, true)
	add("number", "volume", "Volume", map[string]interface{}{
		"state_topic": status("volume"), "command_topic": command("volume"),
		"min": 0, "max": 100, "step": 1, "icon": "mdi:volume-medium",
	}, true)

	buttons := []struct{ id, name, icon string }{
		{"sleep", "Sleep", "mdi:power-sleep"},
		{"shutdown", "Shutdown", "mdi:power"},
		{"reboot", "Reboot", "mdi:restart"},
		{"displaysleep", "Display Sleep", "mdi:monitor-off"},
		{"lock", "Lock", "mdi:lock"},
	}
	for _, b := range buttons {
		add("button", b.id, b.name, map[string]interface{}{
			"command_topic": command(b.id), "payload_press": b.id, "icon": b.icon,
		}, true)
	}

	add("binary_sensor", "display_on", "Display On", map[string]interface{}{
		"state_topic": status("display_on"), "payload_on": "true", "payload_off": "false", "icon": "mdi:monitor",
	}, true)
	add("binary_sensor", "user_active", "User Active", map[string]interface{}{
		"state_topic": status("user_active"), "payload_on": "true", "payload_off": "false",
		"device_class": "occupancy", "icon": "mdi:keyboard",
	}, true)
	add("sensor", "idle_time", "Idle Time", map[string]interface{}{
		"state_topic": status("idle_time"), "unit_of_measurement": "s", "device_class": "duration",
		"state_class": "measurement", "icon": "mdi:timer-sand",
	}, true)
	add("binary_sensor", "screen_locked", "Screen Locked", map[string]interface{}{
		"state_topic": status("screen_locked"), "payload_on": "true", "payload_off": "false", "icon": "mdi:monitor-lock",
	}, true)
	add("sensor", "active_app", "Active App", map[string]interface{}{
		"state_topic": status("active_app"), "icon": "mdi:application",
	}, true)

	if feat.WiFi {
		add("sensor", "wifi_ssid", "Wi-Fi SSID", map[string]interface{}{
			"state_topic": status("wifi_ssid"), "icon": "mdi:wifi",
		}, true)
		add("sensor", "wifi_signal_strength", "Wi-Fi Signal Strength", map[string]interface{}{
			"state_topic": status("wifi_signal_strength"), "unit_of_measurement": "dBm", "icon": "mdi:wifi-strength-2",
		}, true)
	}

	add("sensor", "ip_address", "IP Address", map[string]interface{}{
		"state_topic": status("ip_address"), "icon": "mdi:ip-network",
	}, true)
	add("sensor", "uptime", "Last Boot", map[string]interface{}{
		"state_topic": status("uptime"), "device_class": "timestamp", "icon": "mdi:clock-outline",
	}, true)
	add("sensor", "network_upload_rate", "Network Upload", map[string]interface{}{
		"state_topic": status("network_upload_rate"), "unit_of_measurement": "KB/s",
		"icon": "mdi:upload", "state_class": "measurement",
	}, true)
	add("sensor", "network_download_rate", "Network Download", map[string]interface{}{
		"state_topic": status("network_download_rate"), "unit_of_measurement": "KB/s",
		"icon": "mdi:download", "state_class": "measurement",
	}, true)

	return entities
}

// publishDiscoveryMessages announces the device and all its entities to Home Assistant.
func (a *app) publishDiscoveryMessages(client mqtt.Client) {
	manufacturer, model := a.sys.DeviceInfo()
	device := map[string]interface{}{
		"identifiers":  []string{topicRoot + "_" + a.hostname},
		"name":         a.hostname,
		"manufacturer": manufacturer,
		"model":        model,
		"sw_version":   version,
	}

	configTopic := func(e discoveryEntity) string {
		return fmt.Sprintf("homeassistant/%s/%s_%s/%s/config", e.component, topicRoot, a.hostname, e.objectID)
	}

	present := map[string]bool{}
	for _, e := range discoveryEntities(a.hostname, a.prefix, a.sys.Features(), device) {
		present[e.objectID] = true
		payload, err := json.Marshal(e.config)
		if err != nil {
			log.Printf("Error marshaling discovery config for %s: %v", e.objectID, err)
			continue
		}
		a.publish(client, configTopic(e), true, string(payload))
	}

	// Hardware that was announced on an earlier run but is gone now must be
	// removed from the broker, or Home Assistant keeps the stale entities.
	for _, e := range discoveryEntities(a.hostname, a.prefix, Features{Battery: true, WiFi: true}, device) {
		if !present[e.objectID] {
			a.publish(client, configTopic(e), true, "")
		}
	}

	log.Println("Published Home Assistant MQTT discovery messages")
}
