package main

import (
	"log"
	"os"
	"path/filepath"
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

	a := newApp(cfg, sys, hostname)
	log.Printf("Hostname: %s, user idle threshold: %d seconds", hostname, a.idleThreshold)

	client := a.newMQTTClient()

	fast := time.NewTicker(2 * time.Second)
	slow := time.NewTicker(60 * time.Second)
	for {
		select {
		case <-fast.C:
			a.updateFast(client)
		case <-slow.C:
			a.updateSlow(client)
		}
	}
}
