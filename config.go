package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type RandomDuration struct {
	Mean   float64 `json:"mean"`
	StdDev float64 `json:"stddev"`
}

type ListenerConfig struct {
	Name          string         `json:"name"`
	ListenAddress string         `json:"address"`
	ForwardTo     string         `json:"target"`
	RejectionRate float64        `json:"rejectionRate"`
	Durability    RandomDuration `json:"durability"`
	Latency       RandomDuration `json:"latency"`
}

type Config struct {
	Listeners []ListenerConfig `json:"listeners"`
}

func LoadConfigFromFile(configFile string) (Config, error) {
	data, readError := os.ReadFile(configFile)
	if readError != nil {
		return Config{}, fmt.Errorf("failed to read config file %v: %w", configFile, readError)
	}
	var config Config
	unmarshalError := json.Unmarshal(data, &config)
	if unmarshalError != nil {
		return Config{}, fmt.Errorf("failed to unmarshal config file %v: %w", configFile, unmarshalError)
	}
	return config, nil
}

func LoadConfigFromString(config string) (Config, error) {
	var c Config
	unmarshalError := json.Unmarshal([]byte(config), &c)
	if unmarshalError != nil {
		return Config{}, fmt.Errorf("failed to unmarshal config string: %w", unmarshalError)
	}
	return c, nil
}

func LoadConfigFromEnv() (Config, error) {
	configString, exists := os.LookupEnv("CHAOTIC_PROXY_CONFIG")
	if !exists {
		return Config{}, fmt.Errorf("CHAOTIC_PROXY_CONFIG environment variable not set")
	}
	return LoadConfigFromString(configString)
}

func WatchConfigFile(ctx context.Context, configFile string, configChan chan Config, errorChan chan error) {
	var lastModTime time.Time
	var reportedNotFound = false
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(1 * time.Second):
			stat, statError := os.Stat(configFile)
			if statError != nil {
				if !reportedNotFound {
					errorChan <- statError
					reportedNotFound = true
				}
			} else {
				reportedNotFound = false
				if stat.ModTime().After(lastModTime) {
					lastModTime = stat.ModTime()
					config, loadError := LoadConfigFromFile(configFile)
					if loadError != nil {
						errorChan <- loadError
					} else {
						configChan <- config
					}
				}
			}
		}
	}
}
