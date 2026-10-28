# win2mqtt Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Go daemon for Windows desktops that publishes system state to MQTT with Home Assistant discovery and accepts power/audio/lock commands, ported from mac2mqtt.

**Architecture:** A platform-agnostic core (config, MQTT, discovery, publishers, command dispatch, self-update) talks to the OS only through a `System` interface. `system_windows.go` implements it with direct Win32/COM calls; `system_other.go` is a stub so the whole MQTT flow runs and is tested on Linux. A hidden window message loop feeds display and lock state into atomics.

**Tech Stack:** Go 1.27, `github.com/eclipse/paho.mqtt.golang`, `gopkg.in/yaml.v2`, `golang.org/x/sys/windows` (+`registry`), `github.com/go-ole/go-ole`, `github.com/moutend/go-wca`. No cgo. PowerShell install script. GitHub Actions release on push to master.

**Spec:** `docs/superpowers/specs/2026-10-28-win2mqtt-design.md`

## Global Constraints

- Module path `github.com/roflmao/win2mqtt`, `go 1.27`, `CGO_ENABLED=0` for every build.
- Every file must compile for both `GOOS=linux` and `GOOS=windows`; Windows-only code lives in `*_windows.go`, its Linux counterpart in `*_other.go` (`//go:build !windows`).
- Topic prefix `win2mqtt/<hostname>`; discovery topic `homeassistant/<component>/win2mqtt_<hostname>/<objectID>/config`; `unique_id` = `win2mqtt_<hostname>_<objectID>`; device identifier `win2mqtt_<hostname>`.
- Payload formats as mac2mqtt: booleans `true`/`false`, volume `0`–`100` integer, rates `%.2f` KB/s, uptime RFC3339, idle seconds integer.
- After startup, no sensor or command failure may exit the process.
- Never block the 2-second loop on a slow call. No WMI, no PowerShell at runtime.
- Commit subjects are short, user-facing, imperative; no AI attribution lines (user's CLAUDE.md).
- Run `go vet ./... && go test ./... && GOOS=windows go vet ./... && GOOS=windows go test -c -o /dev/null .` before claiming any task done.

## Review Focus

1. Hostname with spaces, apostrophes or non-ASCII (`Rolf's PC`) or empty after sanitising: must yield a non-empty, topic-safe name (`RolfsPC`, fallback `windows`). Test in Task 2.
2. Volume command payloads `101`, `-1`, `abc`, ` 50\n`: out-of-range and junk are ignored with a log line; surrounding whitespace is trimmed. Test in Task 5.
3. A getter that fails on every tick (audio endpoint gone while display sleeps): nothing is published for that sensor, the process keeps running, and the error is logged once rather than every 2 s. Test in Task 4.
4. Network counters that go backwards (adapter re-enabled): rates clamp to `0.00`, never negative. Test in Task 4.
5. `mqtt_port: 1883` written as a YAML integer, and a missing config file: the integer is accepted as the string `1883`; a missing file produces an error naming the path that was tried. Test in Task 1.

---

### Task 1: Repo scaffold and config

**Files:**
- Create: `go.mod`, `LICENSE`, `.gitignore`, `config.go`, `config_test.go`, `main.go` (placeholder)
- Already present: `docs/superpowers/specs/2026-10-28-win2mqtt-design.md`, this plan

**Interfaces:**
- Produces: `type config struct { IP, Port, User, Password string; Debug, DryRun bool; AutoUpdate *bool; IdleThreshold int }` with yaml tags `mqtt_ip`, `mqtt_port`, `mqtt_user`, `mqtt_password`, `debug`, `dry_run`, `auto_update`, `idle_threshold`; `func loadConfig(path string) (*config, error)`; `func findConfigPath() string`; `func (c *config) isAutoUpdateEnabled() bool`; `func (c *config) getIdleThreshold() int`; `const defaultIdleThreshold = 300`; `var version = "dev"`.

- [ ] **Step 1: Scaffold**

```bash
cd /home/rolf/git/win2mqtt
go mod init github.com/roflmao/win2mqtt
cp ../mac2mqtt/LICENSE LICENSE   # MIT; keep Ivan Bessarabov's copyright line, add "2026 Rolf" line
printf 'win2mqtt.yaml\n*.exe\n*.log\n*.old\n' > .gitignore
```
`main.go`: `package main` with `var version = "dev"` and an empty `main()` so the module builds.

- [ ] **Step 2: Write the failing tests in `config_test.go`**

```go
func TestLoadConfigIntPort(t *testing.T)      // yaml "mqtt_ip: 10.0.0.1\nmqtt_port: 1883\n" → c.Port == "1883"
func TestLoadConfigMissingFile(t *testing.T)  // path "/nonexistent/win2mqtt.yaml" → err != nil && strings.Contains(err.Error(), "/nonexistent/win2mqtt.yaml")
func TestLoadConfigRequiresBroker(t *testing.T) // "mqtt_port: 1883" without ip, dry_run false → error mentioning mqtt_ip; with "dry_run: true" → no error
func TestGetIdleThreshold(t *testing.T)       // {0→300, -5→300, 60→60} (ported from mac2mqtt)
func TestIsAutoUpdateEnabled(t *testing.T)    // nil→true, false→false, true→true
```
Write temp files with `t.TempDir()`.

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./... -run 'Config|IdleThreshold|AutoUpdate' -v`
Expected: FAIL, undefined: loadConfig

- [ ] **Step 4: Implement `config.go`**

Port `config`, `isAutoUpdateEnabled`, `getIdleThreshold` from `~/git/mac2mqtt/main.go:49-106`. `loadConfig` returns errors instead of `log.Fatal`; wrap the read error with the path (`fmt.Errorf("read config %s: %w", path, err)`). `findConfigPath()`: `filepath.Join(filepath.Dir(os.Executable()), "win2mqtt.yaml")` if it exists, else `"win2mqtt.yaml"`.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./... -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "Add repo scaffold, design spec and config loading"
```

---

### Task 2: System interface, pure helpers and Linux stub

**Files:**
- Create: `system.go`, `system_test.go`, `system_other.go`

**Interfaces:**
- Produces (in `system.go`):

```go
type Features struct{ Battery, WiFi bool }

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

func isUserActive(idleSeconds int64, threshold int) bool
func qualityToRSSI(quality uint32) int          // int(quality)/2 - 100
func exeBaseName(path string) string            // `C:\Windows\explorer.exe` → `explorer`; strips dir (both slashes) and case-insensitive ".exe"
func idleFromTicks(lastInput, now uint32) int64 // seconds, uint32 wrap-safe
func sanitizeHostname(raw string) string        // first "." part, keep [a-zA-Z0-9_-], empty → "windows"
```
- Produces (in `system_other.go`, `//go:build !windows`): `func newSystem() (System, error)` returning `*stubSystem` with fixed values: volume 42, mute false, app `stub`, idle 0, display on, unlocked, counters that grow by 1024 in/512 out per call, IP `127.0.0.1`, boot time `time.Now().Add(-time.Hour)`, `Features{}`; command methods log `"<name> requested (stub)"` and return nil. `SetVolume`/`SetMute` store the value so `Volume()`/`Mute()` reflect it.

- [ ] **Step 1: Write failing tests in `system_test.go`**

```go
func TestIsUserActive(t *testing.T)     // ported table: {0,300,true},{299,300,true},{300,300,false},{10,10,false}
func TestQualityToRSSI(t *testing.T)    // 0→-100, 50→-75, 100→-50
func TestExeBaseName(t *testing.T)      // `C:\Windows\explorer.exe`→explorer, `C:/x/Code.EXE`→Code, `notepad`→notepad, ``→``
func TestIdleFromTicks(t *testing.T)    // (1000,5000)→4, (0xFFFFF000, 0x1000)→8 (wrap), (5000,5000)→0
func TestSanitizeHostname(t *testing.T) // "DESKTOP-AB12.local"→"DESKTOP-AB12", "Rolf's PC"→"RolfsPC", "ÆØÅ"→"windows", ""→"windows"
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./... -run 'UserActive|RSSI|ExeBase|IdleFrom|Sanitize' -v`
Expected: FAIL, undefined functions

- [ ] **Step 3: Implement `system.go` and `system_other.go`**

- [ ] **Step 4: Run tests and cross-compile check**

Run: `go test ./... -v && GOOS=windows go vet ./...`
Expected: PASS; vet reports `newSystem` undefined for windows only if Task 8 not done yet — that is expected now, so run vet with `GOOS=linux` only in this task.

- [ ] **Step 5: Commit**

```bash
git add system.go system_test.go system_other.go && git commit -m "Add System interface, pure helpers and Linux stub"
```

---

### Task 3: MQTT client, publish helper and logging

**Files:**
- Create: `mqtt.go`, `mqtt_test.go`, `logging.go`, `console_windows.go`, `console_other.go`
- Modify: `main.go`

**Interfaces:**
- Produces (in `mqtt.go`):

```go
type app struct {
	cfg           *config
	sys           System
	hostname      string
	prefix        string          // "win2mqtt/" + hostname
	idleThreshold int
	warned        sync.Map         // sensor name → struct{}; for log-once
	// Task 4 adds: net networkStats
}
func newApp(cfg *config, sys System, hostname string) *app
func (a *app) publish(client mqtt.Client, topic string, retained bool, payload string)      // logs when cfg.Debug or cfg.DryRun; waits on token; logs token error
func (a *app) newMQTTClient() mqtt.Client   // dry-run → &dummyClient{} and calls a.onConnect immediately; else paho client with LWT on prefix+"/status/alive"="false" retained, auto-reconnect, retry loop ported from mac2mqtt main.go:651-722
func (a *app) onConnect(client mqtt.Client) // in this task: publishes alive=true retained only. Task 6 adds discovery, Task 4 adds initial fast+slow updates, Task 5 adds the prefix+"/command/#" subscription
func (a *app) warnOnce(key string, err error) // first time: log.Printf("Warning: %s: %v", key, err); afterwards only when cfg.Debug
type dummyClient struct{}; type dummyToken struct{}   // ported verbatim from mac2mqtt main.go:754-786
```
- Produces (in `mqtt_test.go`, reused by later tasks): `type recordingClient struct { dummyClient; mu sync.Mutex; msgs []published }` with `type published struct{ topic, payload string; retained bool }`, overriding `Publish` to record and `Subscribe` to store the callback in `subscribed map[string]mqtt.MessageHandler`; `func (r *recordingClient) find(topic string) (published, bool)` returning the last message for a topic.
- Produces (in `logging.go`): `func setupLogging(dir string) (func(), error)` opening `dir/win2mqtt.log` for append (truncate first if size > 5 MB), `log.SetOutput(io.MultiWriter(file, consoleWriter()))`, returns a close func. `consoleWriter() io.Writer` is in `console_other.go` (returns `os.Stderr`) and `console_windows.go` (`AttachConsole(ATTACH_PARENT_PROCESS)` via `kernel32` lazy DLL; on success `os.OpenFile("CONOUT$", os.O_WRONLY, 0)`; on failure `io.Discard`).

- [ ] **Step 1: Write failing tests in `mqtt_test.go`**

```go
func TestPublishRecordsTopicAndPayload(t *testing.T) // a.publish(rc, "win2mqtt/h/status/x", false, "1") → rc.find returns payload "1", retained false
func TestWarnOnceLogsOnce(t *testing.T)              // capture log output with log.SetOutput(&buf); two warnOnce("volume", err) calls → exactly one "Warning: volume" line when cfg.Debug is false
func TestNewMQTTClientDryRunUsesDummy(t *testing.T)  // cfg.DryRun=true → returned client is *dummyClient; onConnect must not panic with stub system (discovery/updates may be no-ops until later tasks)
```
In this task `onConnect` only publishes alive; later tasks extend it.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./... -run 'Publish|WarnOnce|DryRun' -v`
Expected: FAIL, undefined: app

- [ ] **Step 3: Implement `mqtt.go`, `logging.go`, console files; wire `main.go`**

`main.go` `main()`: `setupLogging(filepath.Dir(exe))`, log "Started" + version, `loadConfig(findConfigPath())` (fatal on error), `sanitizeHostname(os.Hostname())`, `newSystem()` (fatal on error), `newApp`, `a.newMQTTClient()`, then block forever (`select {}`); tickers come in Task 4.

- [ ] **Step 4: Run tests and both-OS vet**

Run: `go test ./... -v && GOOS=linux go vet ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "Add MQTT client, publish helper and file logging"
```

---

### Task 4: Sensors, rate math and tickers

**Files:**
- Create: `sensors.go`, `sensors_test.go`
- Modify: `mqtt.go` (`onConnect` calls `a.updateFast` and `a.updateSlow`), `main.go` (tickers)

**Interfaces:**
- Produces:

```go
type networkStats struct { mu sync.Mutex; bytesIn, bytesOut int64; timestamp time.Time; uploadRate, downloadRate float64 }
func (n *networkStats) update(bytesIn, bytesOut int64, now time.Time)   // ported math from mac2mqtt main.go:1413-1466, inputs injected
func (n *networkStats) rates() (uploadKBps, downloadKBps string)       // "%.2f" of rate/1024

func (a *app) updateVolume(c mqtt.Client)          // status/volume
func (a *app) updateMute(c mqtt.Client)            // status/mute
func (a *app) updateActiveApp(c mqtt.Client)       // status/active_app
func (a *app) updateNetworkActivity(c mqtt.Client) // sys.NetCounters → net.update → status/network_upload_rate, status/network_download_rate
func (a *app) updateDisplayOn(c mqtt.Client)       // status/display_on
func (a *app) updateScreenLocked(c mqtt.Client)    // status/screen_locked
func (a *app) updateUserActivity(c mqtt.Client)    // status/idle_time, status/user_active
func (a *app) updateIPAddress(c mqtt.Client)       // status/ip_address
func (a *app) updateBootTime(c mqtt.Client)        // status/uptime, RFC3339
func (a *app) updateBattery(c mqtt.Client)         // status/battery, only if Features().Battery
func (a *app) updateWiFi(c mqtt.Client)            // status/wifi_ssid, status/wifi_signal_strength, only if Features().WiFi
func (a *app) updateFast(c mqtt.Client)            // volume, mute, activeApp, network, displayOn, screenLocked, userActivity
func (a *app) updateSlow(c mqtt.Client)            // ipAddress, bootTime, battery, wifi
```
Every getter error → `a.warnOnce("<sensor>", err)` and no publish for that sensor.

- [ ] **Step 1: Write failing tests in `sensors_test.go`**

Define `type fakeSystem struct { stubSystem; volumeErr error; counters [][2]int64; calls []string }` embedding the Linux stub and overriding what each test needs (`Volume` returns `volumeErr` when set; `NetCounters` pops from `counters`; command methods append their name to `calls`).

```go
func TestNetworkStatsFirstSampleIsZero(t *testing.T)   // update(1000,500,t0) → rates() == ("0.00","0.00")
func TestNetworkStatsRate(t *testing.T)                // then update(1000+2048, 500+1024, t0+2s) → ("0.50","1.00")  (upload=out)
func TestNetworkStatsCounterResetClampsToZero(t *testing.T) // then update(10, 5, t0+4s) → ("0.00","0.00")
func TestUpdateFastPublishesAllTopics(t *testing.T)    // stub system → topics volume=42, mute=false, active_app=stub, display_on=true, screen_locked=false, idle_time=0, user_active=true, network_*_rate present
func TestUpdateVolumeErrorSkipsPublishAndLogsOnce(t *testing.T) // volumeErr set; call updateVolume 3× → no status/volume message; log buffer contains exactly one "Warning: volume"
func TestUpdateSlowSkipsOptionalSensors(t *testing.T)  // Features{} → no battery/wifi topics; a fake with Features{Battery:true, WiFi:true} → battery, wifi_ssid, wifi_signal_strength present
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./... -run 'NetworkStats|UpdateFast|UpdateVolume|UpdateSlow' -v`
Expected: FAIL

- [ ] **Step 3: Implement `sensors.go`; add tickers to `main.go`**

`main.go`: `fast := time.NewTicker(2*time.Second)`, `slow := time.NewTicker(60*time.Second)`, loop calling `a.updateFast(client)` / `a.updateSlow(client)`; `onConnect` calls both once after discovery.

- [ ] **Step 4: Run tests**

Run: `go test ./... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "Add sensor publishers, network rate maths and update tickers"
```

---

### Task 5: Command dispatch

**Files:**
- Create: `commands.go`, `commands_test.go`
- Modify: `mqtt.go` (`onConnect` subscribes `prefix+"/command/#"` with `a.handleCommand`)

**Interfaces:**
- Produces: `func (a *app) handleCommand(client mqtt.Client, topic, payload string)`. Topic suffix after `prefix+"/command/"` selects: `volume` (int 0–100 after `strings.TrimSpace`; then `updateVolume`+`updateMute`), `mute` (`strconv.ParseBool`; then both updates), `sleep`/`displaysleep`/`shutdown`/`reboot`/`lock` (payload must equal the command name). Unknown topic or bad payload → `log.Printf("Ignoring command %s with payload %q", topic, payload)`. Command errors → `log.Printf("Command %s failed: %v", ...)`, never fatal.
- Paho adapter in `mqtt.go`: `func(c mqtt.Client, m mqtt.Message) { a.handleCommand(c, m.Topic(), string(m.Payload())) }`.

- [ ] **Step 1: Write failing tests in `commands_test.go`**

```go
func TestVolumeCommandSetsAndRepublishes(t *testing.T) // payload " 50\n" → sys.Volume()==50 and status/volume == "50"
func TestVolumeCommandRejectsBadValues(t *testing.T)   // "101", "-1", "abc" → volume unchanged (42), no status/volume published, log contains "Ignoring command"
func TestMuteCommand(t *testing.T)                     // "true" → Mute()==true, status/mute=="true"
func TestButtonCommandsCallSystem(t *testing.T)        // table: sleep, displaysleep, shutdown, reboot, lock → fake.calls == [name]; wrong payload "x" → no call
func TestUnknownCommandIgnored(t *testing.T)           // topic prefix+"/command/bogus" → no panic, log "Ignoring command"
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./... -run 'Command' -v`
Expected: FAIL, undefined: handleCommand

- [ ] **Step 3: Implement `commands.go` and the subscription in `onConnect`**

- [ ] **Step 4: Run tests**

Run: `go test ./... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "Add MQTT command handling for volume, mute, power and lock"
```

---

### Task 6: Home Assistant discovery

**Files:**
- Create: `discovery.go`, `discovery_test.go`
- Modify: `mqtt.go` (`onConnect` calls `a.publishDiscoveryMessages` after alive)

**Interfaces:**
- Produces:

```go
type discoveryEntity struct { component, objectID string; config map[string]interface{} }
func discoveryEntities(hostname, prefix string, feat Features, device map[string]interface{}) []discoveryEntity
func (a *app) publishDiscoveryMessages(client mqtt.Client)  // builds device from sys.DeviceInfo() {identifiers:["win2mqtt_"+hostname], name:hostname, manufacturer, model, sw_version:version}; publishes each entity retained to homeassistant/<component>/win2mqtt_<hostname>/<objectID>/config
```
Entity list (objectID → component), ported field-for-field from `~/git/mac2mqtt/main.go:271-602` with `mac2mqtt_` → `win2mqtt_`: `alive` binary_sensor (connectivity, no availability block); `volume_sensor` sensor; `mute` switch; `volume` number; `sleep`, `shutdown`, `reboot`, `displaysleep` buttons; `display_on`, `user_active` binary_sensors; `idle_time`, `active_app`, `uptime` sensors; `network_upload_rate`, `network_download_rate` sensors. New: `lock` button (`payload_press: "lock"`, icon `mdi:lock`); `screen_locked` binary_sensor (no device_class, icon `mdi:monitor-lock`, `payload_on: "true"` = locked); `ip_address` sensor (icon `mdi:ip-network`, replaces `wifi_ip`). Conditional: `battery` sensor only when `feat.Battery`; `wifi_ssid`, `wifi_signal_strength` only when `feat.WiFi`. All non-alive entities carry the availability block on `prefix+"/status/alive"`.

- [ ] **Step 1: Write failing tests in `discovery_test.go`**

```go
func TestDiscoveryEntityCount(t *testing.T)        // Features{} → 18; {Battery:true} → 19; {Battery:true,WiFi:true} → 21
func TestDiscoveryUniqueIDsAndTopics(t *testing.T) // every unique_id == "win2mqtt_h_"+objectID; every state/command topic starts with "win2mqtt/h/"; objectIDs are unique
func TestDiscoveryLockEntities(t *testing.T)       // lock: component button, command_topic prefix+"/command/lock", payload_press "lock"; screen_locked: binary_sensor, state_topic prefix+"/status/screen_locked", payload_on "true"
func TestPublishDiscoveryTopics(t *testing.T)      // recordingClient: 18 retained messages, topic "homeassistant/button/win2mqtt_h/lock/config" present, JSON payload has device.identifiers == ["win2mqtt_h"]
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./... -run 'Discovery' -v`
Expected: FAIL

- [ ] **Step 3: Implement `discovery.go`; call it from `onConnect`**

- [ ] **Step 4: Run tests, then a Linux dry-run smoke test**

Run: `go test ./... -v`
Expected: PASS
Run: `printf 'dry_run: true\ndebug: true\n' > win2mqtt.yaml && timeout 5 go run . ; rm win2mqtt.yaml win2mqtt.log`
Expected: log lines `[DRY-RUN] Publishing to topic 'homeassistant/...'` for 18 entities and `win2mqtt/<host>/status/volume: 42`.

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "Add Home Assistant discovery with lock, screen locked and IP entities"
```

---

### Task 7: Self-update

**Files:**
- Create: `update.go`, `update_test.go`, `relaunch_windows.go`, `relaunch_other.go`
- Modify: `main.go` (hourly ticker + 30 s initial check when enabled and `runtime.GOOS == "windows"`)

**Interfaces:**
- Produces (in `update.go`, ported from `~/git/mac2mqtt/main.go:1587-1940`): `const updateRepo = "roflmao/win2mqtt"`; `func parseVersion(v string) ([3]int, error)`; `func isNewerVersion(currentVer, newVer string) bool`; `func getBinaryNameForPlatform() string` → `fmt.Sprintf("win2mqtt-windows-%s.exe", nativeArch())`; `checkForUpdate`, `findAssetForCurrentPlatform`, `downloadBinary`, `replaceBinary` (rename running exe to `.old`, rename new in; restore on failure), `performUpdate`, `checkAndApplyUpdate`, `func cleanupOldBinary()` (removes `<exe>.old` at startup, ignore errors). After a successful replace: `relaunch()` then `os.Exit(0)`.
- Produces (`relaunch_other.go`): `func nativeArch() string { return runtime.GOARCH }`; `func relaunch() error` that just logs.
- Produces (`relaunch_windows.go`): `nativeArch()` via `IsWow64Process2` (`kernel32`), mapping `IMAGE_FILE_MACHINE_ARM64 (0xAA64)` → `arm64`, else `runtime.GOARCH`; `relaunch()`: if `schtasks /Query /TN win2mqtt` exits 0, start `cmd /c timeout /t 3 /nobreak >nul & schtasks /Run /TN win2mqtt` with `SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW | windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS}`; otherwise `exec.Command(exePath)` with the same attrs. Do not wait on either.

- [ ] **Step 1: Write failing tests in `update_test.go`**

```go
func TestIsNewerVersion(t *testing.T)           // ported table from mac2mqtt main_test.go:116-143
func TestGetBinaryNameForPlatform(t *testing.T)  // on linux host: "win2mqtt-windows-" + runtime.GOARCH + ".exe"
func TestFindAssetForCurrentPlatform(t *testing.T) // release with assets [win2mqtt-windows-amd64.exe, win2mqtt-windows-arm64.exe] → picks name == getBinaryNameForPlatform(); empty → error
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./... -run 'Version|BinaryName|FindAsset' -v`
Expected: FAIL

- [ ] **Step 3: Implement the files; wire `main.go`**

`main.go`: `cleanupOldBinary()` at start; if `cfg.isAutoUpdateEnabled() && runtime.GOOS == "windows"` start the 30 s initial check and `time.NewTicker(time.Hour)`; else log `Auto-update disabled` / `Auto-update only supported on Windows`.

- [ ] **Step 4: Run tests and Windows vet**

Run: `go test ./... -v && GOOS=windows go vet ./...`
Expected: PASS; Windows vet may still fail only on `newSystem`/`consoleWriter` if Task 8/3 symbols are missing — Task 3 provides `consoleWriter`, so only `newSystem` may be missing until Task 8.

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "Add self-update from GitHub releases with Scheduled Task relaunch"
```

---

### Task 8: Windows layer, part 1: audio and system queries

**Files:**
- Create: `system_windows.go`, `audio_windows.go`, `iftable.go`, `iftable_test.go`
- Modify: `go.mod` (`go get golang.org/x/sys@latest github.com/go-ole/go-ole@v1.3.0 github.com/moutend/go-wca@v0.3.0`)

**Interfaces:**
- Produces (`system_windows.go`, `//go:build windows`): `type windowsSystem struct { feat Features; displayOn, locked atomic.Bool; audio *audioWorker }`; `func newSystem() (System, error)` detects features (battery via `GetSystemPowerStatus` BatteryFlag != 128; WiFi via `WlanOpenHandle`+`WlanEnumInterfaces` count > 0), starts the audio worker, and (Task 9) the message loop. Methods:
  - `ActiveApp`: `windows.GetForegroundWindow` → `windows.GetWindowThreadProcessId` → `windows.OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION)` → `windows.QueryFullProcessImageName` → `exeBaseName`.
  - `IdleSeconds`: `user32.GetLastInputInfo` with `struct{ cbSize uint32; dwTime uint32 }` and `kernel32.GetTickCount` → `idleFromTicks`.
  - `BootTime`: `kernel32.GetTickCount64` → `time.Now().Add(-ms)`.
  - `Battery`: `kernel32.GetSystemPowerStatus` into `struct{ ACLineStatus, BatteryFlag, BatteryLifePercent, SystemStatusFlag byte; BatteryLifeTime, BatteryFullLifeTime uint32 }`; percent 255 → error "unknown".
  - `IPAddress`: `iphlpapi.GetBestInterface(0x08080808 as big-endian DWORD, &idx)` → `net.InterfaceByIndex(int(idx)).Addrs()` → first IPv4.
  - `NetCounters`: `iphlpapi.GetIfTable2(&table)` → slice rows with `unsafe.Slice` → `sumHardwareCounters` → `iphlpapi.FreeMibTable`.
  - `WiFi`: `wlanapi.WlanQueryInterface(handle, &guid, 7 /*current_connection*/, ...)`; `isState != 1` → error "not connected"; SSID from `dot11Ssid` (`uSSIDLength`, `ucSSID [32]byte`), RSSI via `qualityToRSSI(wlanSignalQuality)`; `WlanFreeMemory`.
  - `DeviceInfo`: `registry.OpenKey(registry.LOCAL_MACHINE, "HARDWARE\\DESCRIPTION\\System\\BIOS", registry.QUERY_VALUE)` → `SystemManufacturer`, `SystemProductName`; fallback `"Windows PC"`, `""`.
- Produces (`audio_windows.go`): `type audioWorker struct { req chan audioReq }`; `func startAudioWorker() *audioWorker` runs a goroutine with `runtime.LockOSThread()` and `ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED)` (treat `S_FALSE` (1) as success); `func (w *audioWorker) do(fn func(*wca.IAudioEndpointVolume) error) error` sends the request, and inside the worker resolves `CoCreateInstance(CLSID_MMDeviceEnumerator) → GetDefaultAudioEndpoint(ERender, EConsole) → Activate(IID_IAudioEndpointVolume)` **per request**, releasing all three afterwards. `Volume` = `round(scalar*100)`, `SetVolume(p)` = `SetMasterVolumeLevelScalar(float32(p)/100, nil)`, `Mute`/`SetMute` via `GetMute`/`SetMute`.
- Produces (`iftable.go`, no build tag): `type mibIfRow2 struct {...}` mirroring `MIB_IF_ROW2` (field order: `InterfaceLuid uint64; InterfaceIndex uint32; InterfaceGuid windowsGUID /*[16]byte*/; Alias [257]uint16; Description [257]uint16; PhysicalAddressLength uint32; PhysicalAddress, PermanentPhysicalAddress [32]byte; Mtu, Type, TunnelType, MediaType, PhysicalMediumType, AccessType, DirectionType uint32; InterfaceAndOperStatusFlags uint8; OperStatus, AdminStatus, MediaConnectState uint32; NetworkGuid [16]byte; ConnectionType uint32; TransmitLinkSpeed, ReceiveLinkSpeed, InOctets, InUcastPkts, InNUcastPkts, InDiscards, InErrors, InUnknownProtos, InUcastOctets, InMulticastOctets, InBroadcastOctets, OutOctets, OutUcastPkts, OutNUcastPkts, OutDiscards, OutErrors, OutUcastOctets, OutMulticastOctets, OutBroadcastOctets, OutQLen uint64`); `const ifOperStatusUp = 1`, `ifTypeSoftwareLoopback = 24`, `ifFlagHardwareInterface = 0x01`; `func sumHardwareCounters(rows []mibIfRow2) (in, out int64)` summing rows where flags&0x01 != 0, OperStatus == 1, Type != 24.

- [ ] **Step 1: Write failing tests in `iftable_test.go`**

```go
func TestMibIfRow2Size(t *testing.T)        // unsafe.Sizeof(mibIfRow2{}) == 1352 and unsafe.Offsetof(InOctets) == 1208
func TestSumHardwareCounters(t *testing.T)  // rows: hardware+up (100/10), software (flags 0, 999/999), loopback type 24 (5/5), hardware+down (OperStatus 2, 7/7) → (100, 10)
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./... -run 'MibIfRow2|SumHardware' -v`
Expected: FAIL

- [ ] **Step 3: Implement `iftable.go`, `system_windows.go`, `audio_windows.go`** (DisplayOn/ScreenLocked return the atomics; Sleep/DisplaySleep/Shutdown/Reboot/Lock may return `errors.New("not implemented")` until Task 9)

- [ ] **Step 4: Run Linux tests and the full Windows compile**

Run: `go test ./... -v && GOOS=windows go vet ./... && GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o /dev/null . && GOOS=windows GOARCH=arm64 CGO_ENABLED=0 go build -o /dev/null .`
Expected: all PASS / exit 0

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "Add Windows audio, process, power, network and Wi-Fi sources"
```

---

### Task 9: Windows layer, part 2: hidden window, display/lock events and commands

**Files:**
- Create: `window_windows.go`
- Modify: `system_windows.go` (start the loop in `newSystem`; implement commands)

**Interfaces:**
- Produces: `func startMessageLoop(displayOn, locked *atomic.Bool) error`. Runs a goroutine with `runtime.LockOSThread()`: `RegisterClassExW` (class `win2mqtt_events`, wndproc from `syscall.NewCallback`), `CreateWindowExW(0, class, "win2mqtt", WS_OVERLAPPED, 0,0,0,0, 0, 0, hInstance, nil)` (no `WS_VISIBLE`; not `HWND_MESSAGE`), `RegisterPowerSettingNotification(hwnd, &GUID_SESSION_DISPLAY_STATUS{2B84C20E-AD23-4DDF-93DB-05FFBD7EFCA5}, 0)`, `WTSRegisterSessionNotification(hwnd, 0 /*NOTIFY_FOR_THIS_SESSION*/)`, then `GetMessageW`/`TranslateMessage`/`DispatchMessageW` loop. Wndproc: `WM_POWERBROADCAST (0x218)` with wParam `0x8013` → lParam points to `struct{ PowerSetting [16]byte; DataLength uint32; Data [4]byte }`; `Data==0` → `displayOn.Store(false)`, else `true`. `WM_WTSSESSION_CHANGE (0x2B1)`: wParam 7 → `locked.Store(true)`, 8 → `false`. Startup values: `displayOn=true`; `locked` from `WTSQuerySessionInformationW(0, 0xFFFFFFFF, 25 /*WTSSessionInfoEx*/)` reading `SessionFlags` at byte offset 12 (`0` = locked, `1` = unlocked); `WTSFreeMemory`. Return error before starting the loop if the window cannot be created; the caller logs and continues with defaults.
- Commands in `system_windows.go`: `Sleep` → `powrprof.SetSuspendState(0,0,0)`; `DisplaySleep` → `user32.SendMessageTimeoutW(0xFFFF, 0x112, 0xF170, 2, 0x0002 /*SMTO_ABORTIFHUNG*/, 1000, 0)`; `Lock` → `user32.LockWorkStation`; `Shutdown`/`Reboot` → `exec.Command("shutdown.exe", "/s"|"/r", "/t", "0")` with `SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}`.

- [ ] **Step 1: Implement `window_windows.go` and the commands**

No Linux-runnable test exists for this file; the check is the Windows compile.

- [ ] **Step 2: Run the full check**

Run: `go test ./... && GOOS=windows go vet ./... && GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "-H windowsgui -X main.version=dev" -o win2mqtt.exe . && file win2mqtt.exe && rm win2mqtt.exe`
Expected: PASS; `file` prints `PE32+ executable (GUI) x86-64`.

- [ ] **Step 3: Commit**

```bash
git add -A && git commit -m "Add display and lock event tracking and Windows power commands"
```

---

### Task 10: Install script, example config and docs

**Files:**
- Create: `install.ps1`, `win2mqtt.yaml.example`, `README.md`, `AGENTS.md`, `TODO.md`, `CHANGELOG.md`

- [ ] **Step 1: Write `install.ps1`**

Parameters: `[switch]$Uninstall`. Install: `$dir = "$env:LOCALAPPDATA\win2mqtt"`; stop task `win2mqtt` if it exists; copy `win2mqtt.exe` from the script directory; copy `win2mqtt.yaml` only if absent in `$dir`; `Register-ScheduledTask -TaskName win2mqtt -Force` with `New-ScheduledTaskTrigger -AtLogOn -User $env:USERNAME`, `New-ScheduledTaskAction -Execute "$dir\win2mqtt.exe" -WorkingDirectory $dir`, `New-ScheduledTaskSettingsSet -Hidden -ExecutionTimeLimit ([TimeSpan]::Zero) -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1) -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -MultipleInstances IgnoreNew`, `New-ScheduledTaskPrincipal -UserId $env:USERNAME -LogonType Interactive -RunLevel Limited`; then `Start-ScheduledTask`. Uninstall: `Stop-ScheduledTask`, `Unregister-ScheduledTask -Confirm:$false`, `Stop-Process -Name win2mqtt -ErrorAction SilentlyContinue`, `Remove-Item -Recurse $dir`. Print the log path at the end.

- [ ] **Step 2: Lint the script syntax on Linux**

Run: `pwsh -NoProfile -Command '[System.Management.Automation.Language.Parser]::ParseFile("install.ps1", [ref]$null, [ref]$e); $e.Count'` if `pwsh` is installed; otherwise skip and note it in the commit body.
Expected: `0`

- [ ] **Step 3: Write `win2mqtt.yaml.example`** (same keys and comments as `~/git/mac2mqtt/mac2mqtt.yaml.example`, renamed)

- [ ] **Step 4: Write `README.md`**

Sections: what it is (credit mac2mqtt and Ivan Bessarabov); features; metrics table (topic, values, interval) for every status topic from the spec incl. conditional ones; commands table incl. `lock`; Home Assistant integration; requirements (Windows 10/11, MQTT broker); install (`Set-ExecutionPolicy -Scope Process Bypass`, `.\install.ps1`, `-Uninstall`); configuration keys; logs at `%LOCALAPPDATA%\win2mqtt\win2mqtt.log`; auto-update behaviour; known quirks verbatim from the spec (Wi-Fi SSID needs location permission on 24H2+, Fast Startup and last boot, `ApplicationFrameHost`); building from source on Linux.

- [ ] **Step 5: Write `AGENTS.md`, `TODO.md`, empty `CHANGELOG.md`**

`AGENTS.md`: adapt `~/git/mac2mqtt/AGENTS.md`: layout table for the files in this plan; build/test commands from Global Constraints; "adding a sensor" = `System` method + stub value + `updateFoo` + discovery entity + `updateFast`/`updateSlow` + README row + tests; failure policy; git/release workflow. `TODO.md`: implemented list, then roadmap: tray icon, UWP active app via `ApplicationFrameHost` child walk, brightness, media controls, per-app volume, temperatures via LibreHardwareMonitor, hibernate, service mode, TLS, custom topic prefix. `CHANGELOG.md`: `# Changelog` heading only.

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "Add install script, example config and documentation"
```

---

### Task 11: Release workflow

**Files:**
- Create: `.github/workflows/release.yml`, `.github/scripts/update_changelog.py`

- [ ] **Step 1: Copy and adapt**

```bash
mkdir -p .github/workflows .github/scripts
cp ../mac2mqtt/.github/scripts/update_changelog.py .github/scripts/
cp ../mac2mqtt/.github/workflows/release.yml .github/workflows/release.yml
```
Edit `release.yml`: `runs-on: ubuntu-latest`; add a step `go vet ./... && go test ./... && GOOS=windows go vet ./...` before the version step; replace the four build steps with two: `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "-H windowsgui -X main.version=${VERSION}" -o win2mqtt-windows-amd64.exe .` and the `arm64` equivalent; release body downloads list names the two `.exe` files plus install steps (`install.ps1` from the repo); `files:` lists the two exes and `install.ps1` and `win2mqtt.yaml.example`. Check `update_changelog.py` for hard-coded `mac2mqtt` strings and rename.

- [ ] **Step 2: Validate YAML**

Run: `python3 -c 'import yaml,sys; yaml.safe_load(open(".github/workflows/release.yml"))' && grep -c mac2mqtt .github/workflows/release.yml .github/scripts/update_changelog.py`
Expected: no exception; grep counts `0`.

- [ ] **Step 3: Commit**

```bash
git add -A && git commit -m "Add release workflow building Windows binaries"
```

---

### Task 12: End-to-end verification on Linux

- [ ] **Step 1: Full check**

Run: `go vet ./... && go test ./... && GOOS=windows go vet ./... && GOOS=windows go test -c -o /dev/null . && GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "-H windowsgui" -o win2mqtt.exe . && file win2mqtt.exe`
Expected: all pass; PE32+ GUI.

- [ ] **Step 2: Live run against the real broker from Linux**

Copy broker settings from `~/git/mac2mqtt/mac2mqtt.yaml` into a local `win2mqtt.yaml` with `debug: true`, run `go run .` for ~30 s, confirm in the log: `Connected to MQTT`, 18 discovery publishes, status publishes with stub values. Ask the user to confirm the device appears in Home Assistant and that pressing **Lock** logs `lock requested (stub)`.

- [ ] **Step 3: Remove the Linux ghost device**

Run (with the broker credentials): `mosquitto_sub -h <ip> -u <user> -P <pw> -t 'homeassistant/+/win2mqtt_<linuxhost>/+/config' -v -W 3` to list, then for each topic `mosquitto_pub ... -t <topic> -r -n`; also clear `win2mqtt/<linuxhost>/status/alive`. If `mosquitto_*` is not installed, do it with a 20-line Go snippet using paho from the scratchpad directory.
Expected: the listing is empty afterwards.

- [ ] **Step 4: Optional Wine smoke test** (only if the user agreed to install Wine)

Run: `wine win2mqtt.exe` with `dry_run: true`; read `win2mqtt.log`.
Expected: no crash; stub-free values for idle time, boot time, active app; audio may fail gracefully with a single warning.

- [ ] **Step 5: Hand over**

Tell the user: push `master` to release (CI builds the exes), then on Windows download `win2mqtt-windows-amd64.exe`, rename to `win2mqtt.exe`, put `win2mqtt.yaml` beside it, run `install.ps1`, check `%LOCALAPPDATA%\win2mqtt\win2mqtt.log` and Home Assistant; lock the screen and turn the display off to verify the two event-driven sensors.
