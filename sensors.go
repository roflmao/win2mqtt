package main

import (
	"fmt"
	"strconv"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// networkStats turns cumulative interface byte counters into rates.
type networkStats struct {
	mu           sync.Mutex
	bytesIn      int64
	bytesOut     int64
	timestamp    time.Time
	uploadRate   float64 // bytes per second
	downloadRate float64 // bytes per second
}

// update records a new counter sample and recomputes the rates.
// A counter that went backwards (adapter reset) yields a rate of zero.
func (n *networkStats) update(bytesIn, bytesOut int64, now time.Time) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.timestamp.IsZero() {
		n.bytesIn, n.bytesOut, n.timestamp = bytesIn, bytesOut, now
		n.uploadRate, n.downloadRate = 0, 0
		return
	}

	seconds := now.Sub(n.timestamp).Seconds()
	if seconds <= 0 {
		return
	}

	diffIn := bytesIn - n.bytesIn
	diffOut := bytesOut - n.bytesOut
	if diffIn < 0 {
		diffIn = 0
	}
	if diffOut < 0 {
		diffOut = 0
	}

	n.downloadRate = float64(diffIn) / seconds
	n.uploadRate = float64(diffOut) / seconds
	n.bytesIn, n.bytesOut, n.timestamp = bytesIn, bytesOut, now
}

// rates returns upload and download rates formatted in KB/s.
func (n *networkStats) rates() (uploadKBps, downloadKBps string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return fmt.Sprintf("%.2f", n.uploadRate/1024.0), fmt.Sprintf("%.2f", n.downloadRate/1024.0)
}

func (a *app) status(suffix string) string {
	return a.prefix + "/status/" + suffix
}

func (a *app) updateVolume(c mqtt.Client) {
	v, err := a.sys.Volume()
	if err != nil {
		a.warnOnce("volume", err)
		return
	}
	a.publish(c, a.status("volume"), false, strconv.Itoa(v))
}

func (a *app) updateMute(c mqtt.Client) {
	m, err := a.sys.Mute()
	if err != nil {
		a.warnOnce("mute", err)
		return
	}
	a.publish(c, a.status("mute"), false, strconv.FormatBool(m))
}

func (a *app) updateActiveApp(c mqtt.Client) {
	name, err := a.sys.ActiveApp()
	if err != nil {
		a.warnOnce("active_app", err)
		return
	}
	a.publish(c, a.status("active_app"), false, name)
}

func (a *app) updateNetworkActivity(c mqtt.Client) {
	in, out, err := a.sys.NetCounters()
	if err != nil {
		a.warnOnce("network", err)
		return
	}
	a.net.update(in, out, time.Now())
	up, down := a.net.rates()
	a.publish(c, a.status("network_upload_rate"), false, up)
	a.publish(c, a.status("network_download_rate"), false, down)
}

func (a *app) updateDisplayOn(c mqtt.Client) {
	a.publish(c, a.status("display_on"), false, strconv.FormatBool(a.sys.DisplayOn()))
}

func (a *app) updateScreenLocked(c mqtt.Client) {
	a.publish(c, a.status("screen_locked"), false, strconv.FormatBool(a.sys.ScreenLocked()))
}

func (a *app) updateUserActivity(c mqtt.Client) {
	idle, err := a.sys.IdleSeconds()
	if err != nil {
		a.warnOnce("idle_time", err)
		return
	}
	a.publish(c, a.status("idle_time"), false, strconv.FormatInt(idle, 10))
	a.publish(c, a.status("user_active"), false, strconv.FormatBool(isUserActive(idle, a.idleThreshold)))
}

func (a *app) updateIPAddress(c mqtt.Client) {
	ip, err := a.sys.IPAddress()
	if err != nil {
		a.warnOnce("ip_address", err)
		return
	}
	a.publish(c, a.status("ip_address"), false, ip)
}

func (a *app) updateBootTime(c mqtt.Client) {
	boot, err := a.sys.BootTime()
	if err != nil {
		a.warnOnce("uptime", err)
		return
	}
	a.publish(c, a.status("uptime"), false, boot.Format(time.RFC3339))
}

func (a *app) updateBattery(c mqtt.Client) {
	if !a.sys.Features().Battery {
		return
	}
	percent, err := a.sys.Battery()
	if err != nil {
		a.warnOnce("battery", err)
		return
	}
	a.publish(c, a.status("battery"), false, strconv.Itoa(percent))
}

func (a *app) updateWiFi(c mqtt.Client) {
	if !a.sys.Features().WiFi {
		return
	}
	ssid, rssi, err := a.sys.WiFi()
	if err != nil {
		a.warnOnce("wifi", err)
		return
	}
	a.publish(c, a.status("wifi_ssid"), false, ssid)
	a.publish(c, a.status("wifi_signal_strength"), false, strconv.Itoa(rssi))
}

// updateFast publishes the sensors refreshed every two seconds.
func (a *app) updateFast(c mqtt.Client) {
	a.updateVolume(c)
	a.updateMute(c)
	a.updateActiveApp(c)
	a.updateNetworkActivity(c)
	a.updateDisplayOn(c)
	a.updateScreenLocked(c)
	a.updateUserActivity(c)
}

// updateSlow publishes the sensors refreshed every minute.
func (a *app) updateSlow(c mqtt.Client) {
	a.updateIPAddress(c)
	a.updateBootTime(c)
	a.updateBattery(c)
	a.updateWiFi(c)
}
