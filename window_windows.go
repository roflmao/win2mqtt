//go:build windows

package main

import (
	"fmt"
	"log"
	"runtime"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modWtsapi32 = windows.NewLazySystemDLL("wtsapi32.dll")

	procGetModuleHandleW                 = modKernel32.NewProc("GetModuleHandleW")
	procRegisterClassExW                 = modUser32.NewProc("RegisterClassExW")
	procCreateWindowExW                  = modUser32.NewProc("CreateWindowExW")
	procDefWindowProcW                   = modUser32.NewProc("DefWindowProcW")
	procGetMessageW                      = modUser32.NewProc("GetMessageW")
	procTranslateMessage                 = modUser32.NewProc("TranslateMessage")
	procDispatchMessageW                 = modUser32.NewProc("DispatchMessageW")
	procRegisterPowerSettingNotification = modUser32.NewProc("RegisterPowerSettingNotification")
	procWTSRegisterSessionNotification   = modWtsapi32.NewProc("WTSRegisterSessionNotification")
	procWTSQuerySessionInformationW      = modWtsapi32.NewProc("WTSQuerySessionInformationW")
	procWTSFreeMemory                    = modWtsapi32.NewProc("WTSFreeMemory")
)

const (
	wmPowerBroadcast       = 0x0218
	pbtPowerSettingChange  = 0x8013
	wmWtsSessionChange     = 0x02B1
	wtsSessionLock         = 0x7
	wtsSessionUnlock       = 0x8
	notifyForThisSession   = 0
	deviceNotifyWindow     = 0
	wsOverlapped           = 0x00000000
	wtsCurrentSession      = 0xFFFFFFFF
	wtsSessionInfoEx       = 25
	wtsSessionStateLock    = 0
	wtsSessionStateUnlock  = 1
	eventWindowClassName   = "win2mqtt_events"
	monitorDisplayStateOff = 0
)

// GUID_SESSION_DISPLAY_STATUS {2B84C20E-AD23-4DDF-93DB-05FFBD7EFCA5}
var guidSessionDisplayStatus = windows.GUID{
	Data1: 0x2B84C20E, Data2: 0xAD23, Data3: 0x4DDF,
	Data4: [8]byte{0x93, 0xDB, 0x05, 0xFF, 0xBD, 0x7E, 0xFC, 0xA5},
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   windows.Handle
	Icon       windows.Handle
	Cursor     windows.Handle
	Background windows.Handle
	MenuName   *uint16
	ClassName  *uint16
	IconSm     windows.Handle
}

type msg struct {
	Hwnd    windows.HWND
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      [2]int32
	Private uint32
}

type powerBroadcastSetting struct {
	PowerSetting windows.GUID
	DataLength   uint32
	Data         [4]byte
}

// wtsInfoExLevel1 mirrors WTSINFOEX_LEVEL1_W; the int64 fields give it the
// 8-byte alignment that places it at offset 8 inside wtsInfoEx, as in C.
type wtsInfoExLevel1 struct {
	SessionId               uint32
	SessionState            uint32
	SessionFlags            int32
	WinStationName          [33]uint16
	UserName                [21]uint16
	DomainName              [18]uint16
	LogonTime               int64
	ConnectTime             int64
	DisconnectTime          int64
	LastInputTime           int64
	CurrentTime             int64
	IncomingBytes           uint32
	OutgoingBytes           uint32
	IncomingFrames          uint32
	OutgoingFrames          uint32
	IncomingCompressedBytes uint32
	OutgoingCompressedBytes uint32
}

type wtsInfoEx struct {
	Level uint32
	Data  wtsInfoExLevel1
}

// startMessageLoop creates a hidden window on its own OS thread and keeps the
// display and lock flags current from power-setting and session notifications.
// It returns once the window is registered, or with the error that prevented it.
func startMessageLoop(displayOn, locked *atomic.Bool) error {
	locked.Store(querySessionLocked())

	ready := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		runMessageLoop(displayOn, locked, ready)
	}()
	return <-ready
}

func runMessageLoop(displayOn, locked *atomic.Bool, ready chan<- error) {
	wndProc := windows.NewCallback(func(hwnd windows.HWND, message uint32, wParam, lParam uintptr) uintptr {
		switch message {
		case wmPowerBroadcast:
			if wParam == pbtPowerSettingChange && lParam != 0 {
				// lParam is a pointer the OS owns for the duration of the call.
				setting := (*powerBroadcastSetting)(*(*unsafe.Pointer)(unsafe.Pointer(&lParam)))
				if setting.PowerSetting == guidSessionDisplayStatus && setting.DataLength >= 4 {
					state := *(*uint32)(unsafe.Pointer(&setting.Data[0]))
					displayOn.Store(state != monitorDisplayStateOff)
				}
			}
			return 1 // TRUE
		case wmWtsSessionChange:
			switch wParam {
			case wtsSessionLock:
				locked.Store(true)
			case wtsSessionUnlock:
				locked.Store(false)
			}
			return 0
		}
		r, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(message), wParam, lParam)
		return r
	})

	instance, _, _ := procGetModuleHandleW.Call(0)
	className, _ := windows.UTF16PtrFromString(eventWindowClassName)
	class := wndClassEx{
		WndProc:   wndProc,
		Instance:  windows.Handle(instance),
		ClassName: className,
	}
	class.Size = uint32(unsafe.Sizeof(class))
	if atom, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&class))); atom == 0 {
		ready <- fmt.Errorf("RegisterClassEx: %w", err)
		return
	}

	title, _ := windows.UTF16PtrFromString("win2mqtt")
	hwnd, _, err := procCreateWindowExW.Call(
		0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(title)), wsOverlapped,
		0, 0, 0, 0, 0, 0, instance, 0,
	)
	if hwnd == 0 {
		ready <- fmt.Errorf("CreateWindowEx: %w", err)
		return
	}

	if r, _, err := procRegisterPowerSettingNotification.Call(hwnd, uintptr(unsafe.Pointer(&guidSessionDisplayStatus)), deviceNotifyWindow); r == 0 {
		log.Printf("Warning: display state notifications unavailable: %v", err)
	}
	if r, _, err := procWTSRegisterSessionNotification.Call(hwnd, notifyForThisSession); r == 0 {
		log.Printf("Warning: session lock notifications unavailable: %v", err)
	}
	ready <- nil

	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 { // WM_QUIT or error
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// querySessionLocked reads the current session's lock state; unknown counts as unlocked.
func querySessionLocked() bool {
	var buf *wtsInfoEx
	var size uint32
	r, _, _ := procWTSQuerySessionInformationW.Call(0, wtsCurrentSession, wtsSessionInfoEx, uintptr(unsafe.Pointer(&buf)), uintptr(unsafe.Pointer(&size)))
	if r == 0 || buf == nil {
		return false
	}
	defer procWTSFreeMemory.Call(uintptr(unsafe.Pointer(buf)))
	if buf.Level != 1 {
		return false
	}
	return buf.Data.SessionFlags == wtsSessionStateLock
}
