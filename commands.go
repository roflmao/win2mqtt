package main

import (
	"log"
	"strconv"
	"strings"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// handleCommand dispatches one message from win2mqtt/<host>/command/#.
// Bad topics or payloads are logged and ignored; command failures never exit.
func (a *app) handleCommand(client mqtt.Client, topic, payload string) {
	name := strings.TrimPrefix(topic, a.prefix+"/command/")
	payload = strings.TrimSpace(payload)

	ignore := func() { log.Printf("Ignoring command %s with payload %q", topic, payload) }
	run := func(fn func() error) {
		if err := fn(); err != nil {
			log.Printf("Command %s failed: %v", name, err)
		}
	}

	switch name {
	case "volume":
		v, err := strconv.Atoi(payload)
		if err != nil || v < 0 || v > 100 {
			ignore()
			return
		}
		run(func() error { return a.sys.SetVolume(v) })
		a.updateVolume(client)
		a.updateMute(client)

	case "mute":
		m, err := strconv.ParseBool(payload)
		if err != nil {
			ignore()
			return
		}
		run(func() error { return a.sys.SetMute(m) })
		a.updateVolume(client)
		a.updateMute(client)

	case "sleep", "displaysleep", "shutdown", "reboot", "lock":
		if payload != name {
			ignore()
			return
		}
		actions := map[string]func() error{
			"sleep":        a.sys.Sleep,
			"displaysleep": a.sys.DisplaySleep,
			"shutdown":     a.sys.Shutdown,
			"reboot":       a.sys.Reboot,
			"lock":         a.sys.Lock,
		}
		log.Printf("Executing command %s", name)
		run(actions[name])

	default:
		ignore()
	}
}
