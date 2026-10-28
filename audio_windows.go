//go:build windows

package main

import (
	"errors"
	"fmt"
	"math"
	"runtime"
	"time"

	ole "github.com/go-ole/go-ole"
	"github.com/moutend/go-wca/pkg/wca"
)

const audioTimeout = 5 * time.Second

type audioReq struct {
	fn   func(*wca.IAudioEndpointVolume) error
	done chan error
}

// audioWorker owns a COM-initialised OS thread and runs Core Audio calls on it.
type audioWorker struct {
	req chan audioReq
}

func startAudioWorker() *audioWorker {
	w := &audioWorker{req: make(chan audioReq)}
	go w.loop()
	return w
}

func (w *audioWorker) loop() {
	runtime.LockOSThread()

	if err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED); err != nil {
		// go-ole reports S_FALSE (already initialised) as an error; that is fine.
		var oleErr *ole.OleError
		if !errors.As(err, &oleErr) || oleErr.Code() != 1 {
			for r := range w.req {
				r.done <- fmt.Errorf("COM initialisation failed: %w", err)
			}
			return
		}
	}
	defer ole.CoUninitialize()

	for r := range w.req {
		r.done <- withEndpointVolume(r.fn)
	}
}

// withEndpointVolume resolves the current default render device on every call,
// because a cached IAudioEndpointVolume goes stale when the device changes.
func withEndpointVolume(fn func(*wca.IAudioEndpointVolume) error) error {
	var enumerator *wca.IMMDeviceEnumerator
	if err := wca.CoCreateInstance(wca.CLSID_MMDeviceEnumerator, 0, wca.CLSCTX_ALL, wca.IID_IMMDeviceEnumerator, &enumerator); err != nil {
		return fmt.Errorf("create device enumerator: %w", err)
	}
	defer enumerator.Release()

	var device *wca.IMMDevice
	if err := enumerator.GetDefaultAudioEndpoint(wca.ERender, wca.EConsole, &device); err != nil {
		return fmt.Errorf("no default audio endpoint: %w", err)
	}
	defer device.Release()

	var volume *wca.IAudioEndpointVolume
	if err := device.Activate(wca.IID_IAudioEndpointVolume, wca.CLSCTX_ALL, nil, &volume); err != nil {
		return fmt.Errorf("activate endpoint volume: %w", err)
	}
	defer volume.Release()

	return fn(volume)
}

func (w *audioWorker) do(fn func(*wca.IAudioEndpointVolume) error) error {
	done := make(chan error, 1)
	select {
	case w.req <- audioReq{fn: fn, done: done}:
	case <-time.After(audioTimeout):
		return errors.New("audio worker busy")
	}
	select {
	case err := <-done:
		return err
	case <-time.After(audioTimeout):
		return errors.New("audio call timed out")
	}
}

func (w *audioWorker) volume() (int, error) {
	var level float32
	err := w.do(func(v *wca.IAudioEndpointVolume) error { return v.GetMasterVolumeLevelScalar(&level) })
	return int(math.Round(float64(level) * 100)), err
}

func (w *audioWorker) setVolume(percent int) error {
	return w.do(func(v *wca.IAudioEndpointVolume) error {
		return v.SetMasterVolumeLevelScalar(float32(percent)/100, nil)
	})
}

func (w *audioWorker) mute() (bool, error) {
	var muted bool
	err := w.do(func(v *wca.IAudioEndpointVolume) error { return v.GetMute(&muted) })
	return muted, err
}

func (w *audioWorker) setMute(muted bool) error {
	return w.do(func(v *wca.IAudioEndpointVolume) error { return v.SetMute(muted, nil) })
}
