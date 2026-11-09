# win2mqtt

Control and monitor your Windows PC via MQTT. Integrates with Home Assistant and
any other MQTT-based automation system.

win2mqtt is the Windows counterpart of [mac2mqtt](https://github.com/roflmao/mac2mqtt),
which was originally created by [Ivan Bessarabov](https://github.com/bessarabov/mac2mqtt).
Topic layout, payloads and Home Assistant entities follow mac2mqtt so automations
look the same for both kinds of machine.

## Features

### Status monitoring

Published to MQTT:

* Volume level and mute state
* Active (foreground) application
* Display on/off
* Screen locked
* User active and idle time (seconds since last keyboard or mouse input)
* Network upload and download rates
* IP address
* Last boot time
* Battery charge (laptops only)
* Wi-Fi SSID and signal strength (only when a Wi-Fi adapter is present)
* Connection status (alive/offline)

### Remote control

| Command | Topic | Accepted values | Description |
|---------|-------|-----------------|-------------|
| **Set volume** | `win2mqtt/HOSTNAME/command/volume` | `0` - `100` | Set system volume |
| **Mute/unmute** | `win2mqtt/HOSTNAME/command/mute` | `true` / `false` | Mute or unmute system audio |
| **Sleep** | `win2mqtt/HOSTNAME/command/sleep` | `sleep` | Put the PC to sleep |
| **Display sleep** | `win2mqtt/HOSTNAME/command/displaysleep` | `displaysleep` | Turn the display off |
| **Lock** | `win2mqtt/HOSTNAME/command/lock` | `lock` | Lock the workstation |
| **Shutdown** | `win2mqtt/HOSTNAME/command/shutdown` | `shutdown` | Shut the PC down |
| **Reboot** | `win2mqtt/HOSTNAME/command/reboot` | `reboot` | Restart the PC |

Retained messages on command topics are ignored, so a retained `shutdown` cannot
replay on every reconnect.

### Home Assistant integration

* **MQTT auto discovery** - all entities are created automatically
* **Device grouping** - everything is grouped under one device named after the PC,
  with manufacturer and model read from the BIOS
* **Availability tracking** - entities show as unavailable when win2mqtt is offline

## Available metrics

| Metric | Topic | Values | Update frequency | Notes |
|--------|-------|--------|------------------|-------|
| **Connection status** | `win2mqtt/HOSTNAME/status/alive` | `true` / `false` | On connect/disconnect | Uses the MQTT last will |
| **Volume level** | `win2mqtt/HOSTNAME/status/volume` | `0` - `100` | Every 2 seconds | Default playback device |
| **Mute status** | `win2mqtt/HOSTNAME/status/mute` | `true` / `false` | Every 2 seconds | |
| **Active application** | `win2mqtt/HOSTNAME/status/active_app` | String | Every 2 seconds | Executable name of the foreground window, e.g. `chrome` |
| **Display on** | `win2mqtt/HOSTNAME/status/display_on` | `true` / `false` | Every 2 seconds | Event driven, from Windows display state notifications |
| **Screen locked** | `win2mqtt/HOSTNAME/status/screen_locked` | `true` / `false` | Every 2 seconds | Event driven, from session lock/unlock notifications |
| **User active** | `win2mqtt/HOSTNAME/status/user_active` | `true` / `false` | Every 2 seconds | Input within `idle_threshold` seconds |
| **Idle time** | `win2mqtt/HOSTNAME/status/idle_time` | Seconds (integer) | Every 2 seconds | Seconds since the last keyboard or mouse input |
| **Network upload rate** | `win2mqtt/HOSTNAME/status/network_upload_rate` | KB/s (decimal) | Every 2 seconds | Sum over physical interfaces that are up |
| **Network download rate** | `win2mqtt/HOSTNAME/status/network_download_rate` | KB/s (decimal) | Every 2 seconds | Sum over physical interfaces that are up |
| **IP address** | `win2mqtt/HOSTNAME/status/ip_address` | IPv4 address | Every 60 seconds | Interface that routes to the internet |
| **Last boot** | `win2mqtt/HOSTNAME/status/uptime` | ISO 8601 timestamp | Every 60 seconds | Shown as relative time in Home Assistant |
| **Battery** | `win2mqtt/HOSTNAME/status/battery` | `0` - `100` | Every 60 seconds | Only on machines with a battery |
| **Wi-Fi SSID** | `win2mqtt/HOSTNAME/status/wifi_ssid` | String | Every 60 seconds | Only with a Wi-Fi adapter |
| **Wi-Fi signal strength** | `win2mqtt/HOSTNAME/status/wifi_signal_strength` | dBm | Every 60 seconds | Only with a Wi-Fi adapter |

`HOSTNAME` is the computer name with everything except letters, digits, `_` and
`-` removed.

Battery and Wi-Fi entities are only announced to Home Assistant when the hardware
exists, so a wired desktop shows no dead entities.

### Known Windows quirks

* **Wi-Fi SSID on Windows 11 24H2 and later** is empty unless *Settings → Privacy &
  security → Location → Let desktop apps access your location* is enabled. Signal
  strength works regardless.
* **Last boot with Fast Startup** (the default on Windows 10/11): a shutdown followed
  by power-on is a hibernate and resume, so the boot time does not change. Only
  *Restart* resets it.
* **Store apps** (Settings, Calculator, Photos) are hosted by
  `ApplicationFrameHost`, which is what the active app sensor reports for them.

## Requirements

* Windows 10 or 11 (64-bit, x64 or ARM64)
* An MQTT broker (e.g. Mosquitto)
* Go 1.27+ only if building from source

## Installation

1. Download `win2mqtt-windows-amd64.exe` (or `-arm64.exe`) from the
   [releases](../../releases) page and rename it to `win2mqtt.exe`.
2. Download `install.ps1` and `win2mqtt.yaml.example` from the same release into the
   same folder. Copy `win2mqtt.yaml.example` to `win2mqtt.yaml` and fill in your broker
   settings.
3. Open PowerShell in that folder and run:

   ```powershell
   Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass
   .\install.ps1
   ```

The script copies the files to `%LOCALAPPDATA%\win2mqtt`, registers a Scheduled Task
named `win2mqtt` that starts hidden when you log on, and starts it right away. No
administrator rights are needed; shutdown, sleep and lock all work for a normal
interactive user. Re-running the script upgrades the binary and keeps your config.

To remove everything:

```powershell
.\install.ps1 -Uninstall
```

### First start: SmartScreen and Defender

The release binaries are not code-signed. Windows treats an unsigned file that
was downloaded from the internet with suspicion:

* **SmartScreen** shows "Windows protected your PC" if you double-click the exe.
  The Scheduled Task normally starts it without that prompt, but to be safe clear
  the download mark before installing:

  ```powershell
  Unblock-File .\win2mqtt.exe
  ```

  If the dialog does appear, choose *More info* and then *Run anyway*.
* **Microsoft Defender** occasionally flags unsigned Go programs that call
  power and lock APIs. If the task reports that it ran but the log file never
  appears, open *Windows Security → Virus & threat protection → Protection
  history*, look for `win2mqtt.exe`, choose *Allow on device*, and run
  `install.ps1` again.

Nothing else is needed. The program has no window and no tray icon; the log
file and Home Assistant are the only places it shows up.

### Checking that it is running

Right after `install.ps1` finishes, follow the log:

```powershell
Get-Content -Wait "$env:LOCALAPPDATA\win2mqtt\win2mqtt.log"
```

A healthy start looks like this:

```
Started
Version: v1.0.0
Hostname: DESKTOP-AB12CD, user idle threshold: 300 seconds
Connecting to MQTT broker at 192.168.1.10:1883 (attempt 1)
Connected to MQTT
Published Home Assistant MQTT discovery messages
Auto-update enabled (checks every hour)
```

Within a few seconds the device appears in Home Assistant under
*Settings → Devices & services → MQTT*. Press `Ctrl+C` to stop following the
log; the program keeps running.

If the log repeats `Failed to connect to MQTT`, check `mqtt_ip`, `mqtt_port`
and the credentials in `%LOCALAPPDATA%\win2mqtt\win2mqtt.yaml`, then restart
the task:

```powershell
Restart-ScheduledTask -TaskName win2mqtt   # or: Stop-ScheduledTask / Start-ScheduledTask
```

To see whether the task is running at all:

```powershell
Get-ScheduledTask -TaskName win2mqtt | Select-Object State
Get-Process win2mqtt
```

### Why a logon task and not a service

A Windows service runs in session 0 and cannot see your desktop: no foreground
window, no idle timer and no user audio device. win2mqtt therefore runs inside your
logon session and only reports while someone is logged in.

### Running manually

Put `win2mqtt.exe` and `win2mqtt.yaml` in the same folder and start the exe. It has no
console window of its own; when started from a terminal it attaches to that terminal
for log output, and it always writes to `win2mqtt.log` next to the exe.

## Configuration

`win2mqtt.yaml` is looked up next to the executable first, then in the current
directory.

```yaml
# MQTT broker settings
mqtt_ip: 192.168.1.123
mqtt_port: 1883
mqtt_user: your_username
mqtt_password: your_password
```

Options:

* **mqtt_ip** (required*) - IP address or hostname of your MQTT broker
* **mqtt_port** (required*) - Port of your MQTT broker, usually 1883
* **mqtt_user** / **mqtt_password** (optional) - Broker credentials
* **auto_update** (optional) - Install new releases automatically (default: true)
* **idle_threshold** (optional) - Seconds without input before **User Active** reports
  `false` (default: 300)
* **debug** (optional) - Log every MQTT publish (default: false)
* **dry_run** (optional) - Log publishes instead of connecting to a broker (default: false)

\* not required in dry-run mode.

The connection is plain MQTT over TCP; use a broker on a trusted network or wait for
TLS support (see TODO.md). Restrict who may publish to the command topics with
broker ACLs.

## Logs

* Installed: `%LOCALAPPDATA%\win2mqtt\win2mqtt.log`
* Manual run: `win2mqtt.log` next to the exe

Follow the log live:

```powershell
Get-Content -Wait "$env:LOCALAPPDATA\win2mqtt\win2mqtt.log"
```

Show only the last 50 lines:

```powershell
Get-Content -Tail 50 "$env:LOCALAPPDATA\win2mqtt\win2mqtt.log"
```

Open it in Notepad:

```powershell
notepad "$env:LOCALAPPDATA\win2mqtt\win2mqtt.log"
```

Set `debug: true` in `win2mqtt.yaml` and restart the task to log every MQTT
publish. The file is truncated when it exceeds 5 MB at startup.

## Auto-update

Every hour (and 30 seconds after start) win2mqtt checks the latest GitHub release.
When a newer version exists it downloads the binary for your architecture, verifies
its size, moves the running exe aside as `win2mqtt.exe.old`, puts the new one in place
and restarts itself through the Scheduled Task (about a 3 second gap). The `.old` file
is removed at the next start.

Disable with `auto_update: false`. To roll back, stop the task, rename
`win2mqtt.exe.old` back to `win2mqtt.exe` and start the task again.

## Troubleshooting

* **Nothing in Home Assistant**: check the log for `Connected to MQTT`. If it keeps
  retrying, verify `mqtt_ip`, `mqtt_port` and credentials.
* **Volume shows a warning in the log**: the default playback device vanished (common
  when an HDMI monitor is switched off). The sensor resumes when the device returns.
* **Task not running**: open Task Scheduler and look for `win2mqtt`, or run
  `Get-ScheduledTask win2mqtt` in PowerShell.

## Building from source

```bash
git clone https://github.com/roflmao/win2mqtt.git
cd win2mqtt
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "-H windowsgui" -o win2mqtt.exe .
```

Cross-compiling from Linux or macOS works; no cgo or Windows SDK is needed. Tests run
on any platform with `go test ./...`.

## License

MIT. See LICENSE.
