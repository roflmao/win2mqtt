# win2mqtt design

Windows port of [mac2mqtt](https://github.com/roflmao/mac2mqtt). Written 2026-10-28.

## Background

mac2mqtt (`~/git/mac2mqtt`, github.com/roflmao/mac2mqtt) is a single-file Go daemon
that publishes macOS state (volume, mute, active app, display on, user activity, idle
time, network rates, Wi-Fi, battery, uptime, temperatures) to MQTT with Home Assistant
auto-discovery, and accepts volume/mute/sleep/displaysleep/shutdown/reboot commands.
It self-updates from GitHub releases and ships a release on every push to master.

The user wants the same thing for a Windows **desktop PC**, in a new repo at
`~/git/win2mqtt` (currently empty; GitHub remote `git@github.com:roflmao/win2mqtt.git`
exists and is empty). Decisions taken in the brainstorm:

- Runs **per-user at logon, hidden** (Scheduled Task). Not a Windows Service: session 0
  cannot see the desktop, audio device or idle timer.
- Network sensors are **generic** (primary interface, wired or Wi-Fi): `ip_address`,
  `network_upload_rate`, `network_download_rate`. Wi-Fi SSID/signal only when a Wi-Fi
  adapter is present.
- Extras: **lock screen command** and **screen locked sensor**.
- **No Windows test machine during development.** Everything that can be tested on
  Linux must be; the Windows-only layer must be thin and isolated.

---

## Design

### Intent

A Home Assistant device for the Windows PC that behaves like the mac2mqtt one: same
topic shapes, same entity types, same install-and-forget operation, same auto-update.

### Goals

- Feature parity with mac2mqtt where Windows allows it, plus lock command and
  screen-locked sensor.
- Topic layout `win2mqtt/HOSTNAME/status/*` and `win2mqtt/HOSTNAME/command/*`, HA
  discovery under `homeassistant/<component>/win2mqtt_HOSTNAME/<object>/config`.
- Single static `.exe`, no cgo, no runtime dependencies, cross-compiled from Linux.
- Runs hidden at logon, restarts itself after self-update, logs to a file.
- All non-Windows logic unit-tested on Linux; dry-run works on Linux with a stub
  platform so the MQTT/discovery flow can be verified against the real broker before
  the first Windows run.

### Non-goals

- CPU temperature and fan speed (no reliable built-in source on Windows desktops).
- Battery temperature.
- Windows Service mode, tray icon, GUI.
- Brightness, media, Bluetooth, per-app audio (candidates for TODO.md).
- Sharing code with mac2mqtt via build tags. win2mqtt is a separate repo and binary.

### Anti-goals

- Blocking the 2-second loop on a slow call (WMI, PowerShell). Use direct Win32 calls.
- Spawning a visible console window at logon.
- Changing topic/payload formats once released (HA installs depend on them).

### Sensors and commands

| Topic (under `win2mqtt/HOST/status/`) | HA entity | Interval | Windows source |
|---|---|---|---|
| `alive` | binary_sensor connectivity | LWT | MQTT will message, as mac2mqtt |
| `volume` | sensor % + number | 2s | Core Audio `IAudioEndpointVolume.GetMasterVolumeLevelScalar` (go-wca) |
| `mute` | switch | 2s | `IAudioEndpointVolume.GetMute` |
| `active_app` | sensor | 2s | `GetForegroundWindow` → `GetWindowThreadProcessId` → `OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION)` → `QueryFullProcessImageName`, basename without `.exe` |
| `display_on` | binary_sensor | event-driven, republished every 2s | `RegisterPowerSettingNotification(GUID_SESSION_DISPLAY_STATUS)` on a hidden window; 0=off,1=on,2=dim → on |
| `user_active` | binary_sensor occupancy | 2s | `idle_time < idle_threshold` |
| `idle_time` | sensor duration s | 2s | `GetLastInputInfo` vs `GetTickCount` (uint32 arithmetic for wrap) |
| `screen_locked` | binary_sensor lock | event-driven, republished every 2s | `WTSRegisterSessionNotification` → `WM_WTSSESSION_CHANGE` (WTS_SESSION_LOCK=7, UNLOCK=8); initial state from `WTSQuerySessionInformation(WTSSessionInfoEx)` SessionFlags |
| `network_upload_rate`, `network_download_rate` | sensor KB/s | 2s | `GetIfTable2` (iphlpapi): sum In/OutOctets over hardware interfaces with OperStatus up; rate math reused from mac2mqtt |
| `ip_address` | sensor | 60s | `GetBestInterfaceEx(8.8.8.8)` → interface index → first IPv4 from `net.Interfaces()` (routing-table lookup only, no traffic) |
| `uptime` (last boot, RFC3339) | sensor timestamp | 60s | `time.Now() - GetTickCount64()` |
| `battery` | sensor battery % | 60s | `GetSystemPowerStatus`; **only if** BatteryFlag != 128 (no battery) at startup |
| `wifi_ssid`, `wifi_signal_strength` | sensor, sensor dBm | 60s | wlanapi `WlanEnumInterfaces` + `WlanQueryInterface(current_connection)`; RSSI ≈ quality/2 − 100; **only if** a WLAN interface exists at startup |

| Command topic (under `win2mqtt/HOST/command/`) | Payload | HA entity | Windows action |
|---|---|---|---|
| `volume` | 0–100 | number | `SetMasterVolumeLevelScalar(v/100)` |
| `mute` | true/false | switch | `SetMute` |
| `sleep` | `sleep` | button | powrprof `SetSuspendState(FALSE, FALSE, FALSE)` |
| `displaysleep` | `displaysleep` | button | `SendMessage(HWND_BROADCAST, WM_SYSCOMMAND, SC_MONITORPOWER, 2)` |
| `shutdown` | `shutdown` | button | `shutdown.exe /s /t 0` (graceful, no `/f`, like mac2mqtt) |
| `reboot` | `reboot` | button | `shutdown.exe /r /t 0` |
| `lock` | `lock` | button | `LockWorkStation` |

Conditional sensors (battery, wifi_*) are omitted from discovery too, so HA shows no
dead entities on a wired desktop.

Known Windows quirks to document in README (not to work around in v1):
- Wi-Fi SSID is empty on Windows 11 24H2+ unless "Let desktop apps access your
  location" is enabled; signal strength still works.
- `uptime` (last boot) does not reset on shutdown+power-on when Fast Startup is on
  (that is a hibernate/resume); only Restart resets it.
- UWP/packaged apps (Settings, Calculator) report `ActiveApp` as
  `ApplicationFrameHost`; resolving the real app is a TODO item.

Device block: `identifiers: ["win2mqtt_HOST"]`, `name: HOST`, `manufacturer`/`model`
read from registry `HKLM\HARDWARE\DESCRIPTION\System\BIOS` (`SystemManufacturer`,
`SystemProductName`), falling back to `"Windows PC"`; `sw_version: version`.

### Process model

- Built with `-ldflags "-H windowsgui -X main.version=..."`: no console window.
- At startup, best-effort `AttachConsole(ATTACH_PARENT_PROCESS)` and, if it succeeds,
  reopen `CONOUT$` and add it as a second log writer (Go's `os.Stdout` is invalid in a
  windowsgui process, so attaching alone shows nothing). Logs always go to
  `win2mqtt.log` next to the exe (truncate above ~5 MB at startup).
- Config `win2mqtt.yaml` is looked up next to the exe first, then CWD. Same keys as
  mac2mqtt (`mqtt_ip`, `mqtt_port`, `mqtt_user`, `mqtt_password`, `debug`, `dry_run`,
  `auto_update`, `idle_threshold`).
- One hidden message-loop goroutine (`runtime.LockOSThread`) owns the hidden window,
  power-setting and session notifications, and publishes state changes into shared
  atomics read by the 2s ticker.
- One audio goroutine (locked OS thread, `CoInitializeEx` once) serves volume get/set
  requests over a channel. Builder may instead CoInitialize per call if simpler.
- Self-update (ported from mac2mqtt, repo `roflmao/win2mqtt`, asset
  `win2mqtt-windows-<arch>.exe`): rename running exe to `.old` (allowed on Windows),
  move new exe in, then **restart through the Scheduled Task** so the new process stays
  supervised: spawn `cmd /c timeout /t 3 && schtasks /Run /TN win2mqtt` detached
  (`CREATE_NO_WINDOW`), then exit 0. The task goes Ready when the old process exits and
  `schtasks /Run` starts the new exe under the scheduler (≈3 s gap). If the task does
  not exist (manual run), fall back to spawning the new exe detached. Delete stale
  `.old` at startup.
- **Failure policy:** after startup, no sensor or command failure ever exits the
  process. A failing getter logs (debug level after the first occurrence) and skips
  that publish. Only config errors and an unreachable executable path are fatal. This
  matters because the default audio endpoint (often HDMI/DP audio) disappears when the
  display sleeps.
- Audio: resolve the default endpoint on every call (`GetDefaultAudioEndpoint` →
  `Activate`), never cache `IAudioEndpointVolume`; it goes stale on device change.
- `displaysleep` uses `SendMessageTimeout(HWND_BROADCAST, ..., SMTO_ABORTIFHUNG, 1000)`
  rather than `SendMessage`, which would block on a hung window.
- `shutdown.exe` is spawned with `CREATE_NO_WINDOW` so no console flashes.
- Hostname: `os.Hostname()` sanitized exactly as mac2mqtt (`[a-zA-Z0-9_-]` only).

### Install

`install.ps1` (run as the normal user, no admin needed):
1. Copy `win2mqtt.exe` and `win2mqtt.yaml` to `%LOCALAPPDATA%\win2mqtt\` (skip yaml
   if one already exists there).
2. `Register-ScheduledTask "win2mqtt"`: trigger AtLogOn for current user, action = the
   exe with working dir set, settings: Hidden, ExecutionTimeLimit 0, RestartCount 3 /
   RestartInterval 1 min (crash safety net; self-update does not consume it because it
   exits 0 and relaunches via `schtasks /Run`), AllowStartIfOnBatteries,
   DontStopIfGoingOnBatteries, MultipleInstances IgnoreNew. Run level: limited
   (shutdown/sleep/lock do not need elevation for an interactive user).
3. Start the task. On re-install, stop the task first so the exe can be replaced.
`install.ps1 -Uninstall` stops and unregisters the task, kills any stray `win2mqtt.exe`
by name, and removes the folder. Document `Set-ExecutionPolicy -Scope Process Bypass`
in the README for running the script.

### Repo layout

```
win2mqtt/
  main.go              entry: config, logging, wiring, tickers (platform-agnostic)
  config.go            config struct + defaults (ported)
  mqtt.go              client, dummy client, publishMQTT, listen/dispatch (ported)
  discovery.go         HA discovery payloads, driven by a feature set (which optional sensors exist)
  sensors.go           publish functions that call the System interface; netrate math (ported)
  update.go            self-update (ported; Windows restart semantics)
  system.go            `type System interface` + pure helpers (isUserActive, rssi conversion, exe basename, ...)
  system_windows.go    //go:build windows  — all Win32/COM calls implementing System
  system_other.go      //go:build !windows — stub System returning fixed values (for Linux dry-run and tests)
  *_test.go            pure-function and fake-System tests, run on Linux
  win2mqtt.yaml.example, install.ps1, README.md, AGENTS.md, TODO.md, CHANGELOG.md, LICENSE
  .github/workflows/release.yml, .github/scripts/update_changelog.py
```

Dependencies: `github.com/eclipse/paho.mqtt.golang`, `gopkg.in/yaml.v2`,
`golang.org/x/sys/windows` (+`/registry`), `github.com/go-ole/go-ole`,
`github.com/moutend/go-wca` (pure Go, no cgo). Anything x/sys lacks
(`GetLastInputInfo`, `SetSuspendState`, `LockWorkStation`, `GetIfTable2`,
`GetTickCount64`, `GetSystemPowerStatus`, `WTS*`, `RegisterPowerSettingNotification`,
window-class/message-loop calls, `Wlan*`) is loaded via `windows.NewLazySystemDLL`.

### Release pipeline

Copy mac2mqtt's push-to-master workflow (auto patch bump, CHANGELOG from commit
subjects, GitHub release). Runs on `ubuntu-latest`; builds
`win2mqtt-windows-amd64.exe` and `win2mqtt-windows-arm64.exe` with `CGO_ENABLED=0`.
Omit mac2mqtt's second tag-triggered workflow (its known issues are in mac2mqtt's TODO).
Also run `go vet` and `go test` for `GOOS=linux` and `GOOS=windows go vet` + `go test -c`
before building.

### Left to the builder

- Exact MIB_IF_ROW2 / WLAN struct layouts and vtable plumbing.
- Whether COM init is per-call or a dedicated goroutine.
- Message-loop implementation details (window class name, WM_POWERBROADCAST parsing).
- Log rotation details.

---

