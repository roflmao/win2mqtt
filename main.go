package main

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

var version = "dev" // Overridden by ldflags at build time

func main() {
	exe, err := os.Executable()
	if err != nil {
		log.Fatalf("cannot determine executable path: %v", err)
	}
	closeLog, err := setupLogging(filepath.Dir(exe))
	if err != nil {
		log.Fatalf("cannot open log file: %v", err)
	}
	defer closeLog()

	log.Println("Started")
	log.Printf("Version: %s", version)

	cfg, err := loadConfig(findConfigPath())
	if err != nil {
		log.Fatal(err)
	}
	if cfg.DryRun {
		log.Println("DRY RUN MODE ENABLED - No actual MQTT connection will be made")
	}

	rawHost, err := os.Hostname()
	if err != nil {
		log.Fatalf("cannot determine hostname: %v", err)
	}
	hostname := sanitizeHostname(rawHost)

	sys, err := newSystem()
	if err != nil {
		log.Fatalf("cannot initialise system access: %v", err)
	}

	cleanupOldBinary()

	a := newApp(cfg, sys, hostname)
	log.Printf("Hostname: %s, user idle threshold: %d seconds", hostname, a.idleThreshold)

	client := a.newMQTTClient()

	autoUpdate := cfg.isAutoUpdateEnabled() && runtime.GOOS == "windows"
	switch {
	case autoUpdate:
		log.Println("Auto-update enabled (checks every hour)")
		go func() {
			time.Sleep(30 * time.Second)
			checkAndApplyUpdate()
		}()
	case cfg.isAutoUpdateEnabled():
		log.Println("Auto-update only supported on Windows")
	default:
		log.Println("Auto-update disabled")
	}

	// Each schedule runs independently and skips ticks while busy, so a slow
	// audio call or a stalled publish cannot hold up the other schedules.
	go runOnTicks(time.NewTicker(2*time.Second).C, func() { a.updateFast(client) })
	go runOnTicks(time.NewTicker(60*time.Second).C, func() { a.updateSlow(client) })
	if autoUpdate {
		go runOnTicks(time.NewTicker(time.Hour).C, checkAndApplyUpdate)
	}

	select {}
}
