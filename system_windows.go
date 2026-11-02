//go:build windows

package main

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	modKernel32 = windows.NewLazySystemDLL("kernel32.dll")
	modUser32   = windows.NewLazySystemDLL("user32.dll")
	modIphlpapi = windows.NewLazySystemDLL("iphlpapi.dll")
	modWlanapi  = windows.NewLazySystemDLL("wlanapi.dll")
	modPowrprof = windows.NewLazySystemDLL("powrprof.dll")

	procGetLastInputInfo     = modUser32.NewProc("GetLastInputInfo")
	procGetTickCount         = modKernel32.NewProc("GetTickCount")
	procGetTickCount64       = modKernel32.NewProc("GetTickCount64")
	procGetSystemPowerStatus = modKernel32.NewProc("GetSystemPowerStatus")
	procGetBestInterface     = modIphlpapi.NewProc("GetBestInterface")
	procGetIfTable2          = modIphlpapi.NewProc("GetIfTable2")
	procFreeMibTable         = modIphlpapi.NewProc("FreeMibTable")
	procWlanOpenHandle       = modWlanapi.NewProc("WlanOpenHandle")
	procWlanEnumInterfaces   = modWlanapi.NewProc("WlanEnumInterfaces")
	procWlanQueryInterface   = modWlanapi.NewProc("WlanQueryInterface")
	procWlanFreeMemory       = modWlanapi.NewProc("WlanFreeMemory")
	procSetSuspendState      = modPowrprof.NewProc("SetSuspendState")
	procSendMessageTimeoutW  = modUser32.NewProc("SendMessageTimeoutW")
	procLockWorkStation      = modUser32.NewProc("LockWorkStation")
)

type windowsSystem struct {
	feat       Features
	displayOn  atomic.Bool
	locked     atomic.Bool
	audio      *audioWorker
	wlanHandle windows.Handle   // open when feat.WiFi
	bootTime   func() time.Time // computed once; recomputing would drift by up to a second
}

func newSystem() (System, error) {
	s := &windowsSystem{audio: startAudioWorker()}
	s.displayOn.Store(true)
	s.bootTime = sync.OnceValue(func() time.Time {
		ms, _, _ := procGetTickCount64.Call()
		return bootTimeFromUptime(time.Now(), uint64(ms))
	})

	s.feat.Battery = hasBattery()
	if handle, ok := openWlanWithInterfaces(); ok {
		s.feat.WiFi = true
		s.wlanHandle = handle
	}

	if err := startMessageLoop(&s.displayOn, &s.locked); err != nil {
		log.Printf("Warning: display/lock events unavailable, reporting defaults: %v", err)
	}

	return s, nil
}

func (s *windowsSystem) Features() Features { return s.feat }

func (s *windowsSystem) DeviceInfo() (string, string) {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `HARDWARE\DESCRIPTION\System\BIOS`, registry.QUERY_VALUE)
	if err != nil {
		return "Windows PC", ""
	}
	defer key.Close()

	manufacturer, _, err := key.GetStringValue("SystemManufacturer")
	if err != nil || manufacturer == "" {
		manufacturer = "Windows PC"
	}
	model, _, _ := key.GetStringValue("SystemProductName")
	return manufacturer, model
}

func (s *windowsSystem) Volume() (int, error)        { return s.audio.volume() }
func (s *windowsSystem) Mute() (bool, error)         { return s.audio.mute() }
func (s *windowsSystem) SetVolume(percent int) error { return s.audio.setVolume(percent) }
func (s *windowsSystem) SetMute(muted bool) error    { return s.audio.setMute(muted) }
func (s *windowsSystem) DisplayOn() bool             { return s.displayOn.Load() }
func (s *windowsSystem) ScreenLocked() bool          { return s.locked.Load() }

func (s *windowsSystem) ActiveApp() (string, error) {
	hwnd := windows.GetForegroundWindow()
	if hwnd == 0 {
		return "", errors.New("no foreground window")
	}
	var pid uint32
	if _, err := windows.GetWindowThreadProcessId(hwnd, &pid); err != nil {
		return "", fmt.Errorf("GetWindowThreadProcessId: %w", err)
	}
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", fmt.Errorf("OpenProcess %d: %w", pid, err)
	}
	defer windows.CloseHandle(process)

	buf := make([]uint16, 1024)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(process, 0, &buf[0], &size); err != nil {
		return "", fmt.Errorf("QueryFullProcessImageName: %w", err)
	}
	return exeBaseName(windows.UTF16ToString(buf[:size])), nil
}

func (s *windowsSystem) IdleSeconds() (int64, error) {
	var info struct {
		cbSize uint32
		dwTime uint32
	}
	info.cbSize = uint32(unsafe.Sizeof(info))
	if r, _, err := procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&info))); r == 0 {
		return 0, fmt.Errorf("GetLastInputInfo: %w", err)
	}
	now, _, _ := procGetTickCount.Call()
	return idleFromTicks(info.dwTime, uint32(now)), nil
}

func (s *windowsSystem) BootTime() (time.Time, error) {
	return s.bootTime(), nil
}

// systemPowerStatus mirrors SYSTEM_POWER_STATUS.
type systemPowerStatus struct {
	ACLineStatus        byte
	BatteryFlag         byte
	BatteryLifePercent  byte
	SystemStatusFlag    byte
	BatteryLifeTime     uint32
	BatteryFullLifeTime uint32
}

const (
	batteryFlagNoBattery = 128
	batteryFlagUnknown   = 255
)

func getSystemPowerStatus() (systemPowerStatus, error) {
	var status systemPowerStatus
	if r, _, err := procGetSystemPowerStatus.Call(uintptr(unsafe.Pointer(&status))); r == 0 {
		return status, fmt.Errorf("GetSystemPowerStatus: %w", err)
	}
	return status, nil
}

func hasBattery() bool {
	status, err := getSystemPowerStatus()
	if err != nil {
		return false
	}
	return status.BatteryFlag != batteryFlagNoBattery && status.BatteryFlag != batteryFlagUnknown
}

func (s *windowsSystem) Battery() (int, error) {
	status, err := getSystemPowerStatus()
	if err != nil {
		return 0, err
	}
	if status.BatteryLifePercent == 255 {
		return 0, errors.New("battery level unknown")
	}
	return int(status.BatteryLifePercent), nil
}

func (s *windowsSystem) IPAddress() (string, error) {
	// 8.8.8.8 as an in_addr; only the routing table is consulted, no packet is sent.
	const dest = 0x08080808
	var index uint32
	if r, _, _ := procGetBestInterface.Call(uintptr(dest), uintptr(unsafe.Pointer(&index))); r != 0 {
		return "", fmt.Errorf("GetBestInterface failed with code %d", r)
	}
	iface, err := net.InterfaceByIndex(int(index))
	if err != nil {
		return "", fmt.Errorf("interface %d: %w", index, err)
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return "", err
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok {
			if v4 := ipnet.IP.To4(); v4 != nil {
				return v4.String(), nil
			}
		}
	}
	return "", fmt.Errorf("interface %s has no IPv4 address", iface.Name)
}

func (s *windowsSystem) NetCounters() (int64, int64, error) {
	var table *mibIfTable2
	if r, _, _ := procGetIfTable2.Call(uintptr(unsafe.Pointer(&table))); r != 0 {
		return 0, 0, fmt.Errorf("GetIfTable2 failed with code %d", r)
	}
	defer procFreeMibTable.Call(uintptr(unsafe.Pointer(table)))

	rows := unsafe.Slice(&table.Table[0], int(table.NumEntries))
	in, out := sumHardwareCounters(rows)
	return in, out, nil
}

// WLAN API structures (wlanapi.h).
type wlanInterfaceInfo struct {
	Guid        windows.GUID
	Description [256]uint16
	State       uint32
}

type wlanInterfaceInfoList struct {
	NumberOfItems uint32
	Index         uint32
	InterfaceInfo [1]wlanInterfaceInfo
}

type dot11SSID struct {
	Length uint32
	SSID   [32]byte
}

type wlanAssociationAttributes struct {
	SSID          dot11SSID
	BssType       uint32
	Bssid         [6]byte
	PhyType       uint32
	PhyIndex      uint32
	SignalQuality uint32
	RxRate        uint32
	TxRate        uint32
}

// wlanConnectionAttributes is the leading part of WLAN_CONNECTION_ATTRIBUTES;
// the trailing security attributes are not needed.
type wlanConnectionAttributes struct {
	State                 uint32
	ConnectionMode        uint32
	ProfileName           [256]uint16
	AssociationAttributes wlanAssociationAttributes
}

const (
	wlanOpcodeCurrentConnection = 7
	wlanInterfaceStateConnected = 1
)

var procWlanCloseHandle = modWlanapi.NewProc("WlanCloseHandle")

// openWlanWithInterfaces opens a WLAN client handle and reports whether at
// least one Wi-Fi adapter exists. Without adapters the handle is closed again.
func openWlanWithInterfaces() (windows.Handle, bool) {
	var negotiated uint32
	var handle windows.Handle
	if r, _, _ := procWlanOpenHandle.Call(2, 0, uintptr(unsafe.Pointer(&negotiated)), uintptr(unsafe.Pointer(&handle))); r != 0 {
		return 0, false
	}
	guids, err := wlanInterfaces(handle)
	if err != nil || len(guids) == 0 {
		procWlanCloseHandle.Call(uintptr(handle), 0)
		return 0, false
	}
	return handle, true
}

// wlanInterfaces lists the GUIDs of all Wi-Fi adapters.
func wlanInterfaces(handle windows.Handle) ([]windows.GUID, error) {
	var list *wlanInterfaceInfoList
	if r, _, _ := procWlanEnumInterfaces.Call(uintptr(handle), 0, uintptr(unsafe.Pointer(&list))); r != 0 || list == nil {
		return nil, fmt.Errorf("WlanEnumInterfaces failed with code %d", r)
	}
	defer procWlanFreeMemory.Call(uintptr(unsafe.Pointer(list)))

	infos := unsafe.Slice(&list.InterfaceInfo[0], int(list.NumberOfItems))
	guids := make([]windows.GUID, 0, len(infos))
	for i := range infos {
		guids = append(guids, infos[i].Guid)
	}
	return guids, nil
}

const errorInvalidState = 5023 // ERROR_INVALID_STATE: adapter not connected

// wlanCurrentConnection queries one adapter; ok is false when it is not connected.
func (s *windowsSystem) wlanCurrentConnection(guid windows.GUID) (ssid string, rssi int, ok bool, err error) {
	var size uint32
	var data *wlanConnectionAttributes
	r, _, _ := procWlanQueryInterface.Call(
		uintptr(s.wlanHandle),
		uintptr(unsafe.Pointer(&guid)),
		wlanOpcodeCurrentConnection,
		0,
		uintptr(unsafe.Pointer(&size)),
		uintptr(unsafe.Pointer(&data)),
		0,
	)
	if r == errorInvalidState {
		return "", 0, false, nil
	}
	if r != 0 || data == nil {
		return "", 0, false, fmt.Errorf("WlanQueryInterface failed with code %d", r)
	}
	defer procWlanFreeMemory.Call(uintptr(unsafe.Pointer(data)))

	if data.State != wlanInterfaceStateConnected {
		return "", 0, false, nil
	}
	raw := data.AssociationAttributes.SSID
	n := raw.Length
	if n > uint32(len(raw.SSID)) {
		n = uint32(len(raw.SSID))
	}
	return string(raw.SSID[:n]), qualityToRSSI(data.AssociationAttributes.SignalQuality), true, nil
}

// WiFi returns the SSID and signal of whichever adapter is connected, so a
// disconnected first adapter does not hide a connected second one.
func (s *windowsSystem) WiFi() (string, int, error) {
	if !s.feat.WiFi {
		return "", 0, errNoWiFi
	}
	guids, err := wlanInterfaces(s.wlanHandle)
	if err != nil {
		return "", 0, err
	}
	var lastErr error
	for _, guid := range guids {
		ssid, rssi, ok, err := s.wlanCurrentConnection(guid)
		if err != nil {
			lastErr = err
			continue
		}
		if ok {
			return ssid, rssi, nil
		}
	}
	if lastErr != nil {
		return "", 0, lastErr
	}
	return "", 0, errors.New("Wi-Fi not connected")
}

const (
	hwndBroadcast   = 0xFFFF
	wmSysCommand    = 0x0112
	scMonitorPower  = 0xF170
	monitorPowerOff = 2
	smtoAbortIfHung = 0x0002
)

// enablePrivilege turns on a privilege the interactive user holds but that is
// disabled by default in the token, such as SeShutdownPrivilege.
func enablePrivilege(name string) error {
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, &token); err != nil {
		return fmt.Errorf("OpenProcessToken: %w", err)
	}
	defer token.Close()

	privName, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	var luid windows.LUID
	if err := windows.LookupPrivilegeValue(nil, privName, &luid); err != nil {
		return fmt.Errorf("LookupPrivilegeValue %s: %w", name, err)
	}
	tp := windows.Tokenprivileges{
		PrivilegeCount: 1,
		Privileges:     [1]windows.LUIDAndAttributes{{Luid: luid, Attributes: windows.SE_PRIVILEGE_ENABLED}},
	}
	if err := windows.AdjustTokenPrivileges(token, false, &tp, 0, nil, nil); err != nil {
		return fmt.Errorf("AdjustTokenPrivileges %s: %w", name, err)
	}
	return nil
}

func (s *windowsSystem) Sleep() error {
	// SetSuspendState requires SE_SHUTDOWN_NAME to be enabled in the caller's token.
	if err := enablePrivilege("SeShutdownPrivilege"); err != nil {
		log.Printf("Warning: %v", err)
	}
	// SetSuspendState(Hibernate=FALSE, ForceCritical=FALSE, DisableWakeEvent=FALSE)
	if r, _, err := procSetSuspendState.Call(0, 0, 0); r == 0 {
		return fmt.Errorf("SetSuspendState: %w", err)
	}
	return nil
}

func (s *windowsSystem) DisplaySleep() error {
	// Broadcast with a timeout so a hung window cannot block us. A timeout only
	// means some window did not answer; the display driver has already acted.
	r, _, err := procSendMessageTimeoutW.Call(hwndBroadcast, wmSysCommand, scMonitorPower, monitorPowerOff, smtoAbortIfHung, 1000, 0)
	if r == 0 && !errors.Is(err, windows.ERROR_TIMEOUT) {
		return fmt.Errorf("SendMessageTimeout: %w", err)
	}
	return nil
}

func (s *windowsSystem) Lock() error {
	if r, _, err := procLockWorkStation.Call(); r == 0 {
		return fmt.Errorf("LockWorkStation: %w", err)
	}
	return nil
}

func runShutdown(flag string) error {
	cmd := exec.Command("shutdown.exe", flag, "/t", "0")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("shutdown.exe %s: %w (%s)", flag, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (s *windowsSystem) Shutdown() error { return runShutdown("/s") }
func (s *windowsSystem) Reboot() error   { return runShutdown("/r") }
