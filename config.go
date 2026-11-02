package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v2"
)

const defaultIdleThreshold = 300 // seconds

const configFileName = "win2mqtt.yaml"

type config struct {
	IP            string `yaml:"mqtt_ip"`
	Port          string `yaml:"mqtt_port"`
	User          string `yaml:"mqtt_user"`
	Password      string `yaml:"mqtt_password"`
	Debug         bool   `yaml:"debug"`
	DryRun        bool   `yaml:"dry_run"`
	AutoUpdate    *bool  `yaml:"auto_update"`    // nil = default true
	IdleThreshold int    `yaml:"idle_threshold"` // seconds without input before user is inactive (0 = default)
}

// loadConfig reads and validates the YAML config at path.
func loadConfig(path string) (*config, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	var c config
	if err := yaml.Unmarshal(content, &c); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}

	if !c.DryRun {
		if c.IP == "" {
			return nil, errors.New("must specify mqtt_ip in " + path)
		}
		if c.Port == "" {
			return nil, errors.New("must specify mqtt_port in " + path)
		}
	}

	return &c, nil
}

// findConfigPath prefers a config next to the executable, then the working directory.
func findConfigPath() string {
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), configFileName)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return configFileName
}

func (c *config) isAutoUpdateEnabled() bool {
	if c.AutoUpdate == nil {
		return true
	}
	return *c.AutoUpdate
}

func (c *config) getIdleThreshold() int {
	if c.IdleThreshold <= 0 {
		return defaultIdleThreshold
	}
	return c.IdleThreshold
}
