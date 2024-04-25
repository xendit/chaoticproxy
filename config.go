// BSD 3-Clause License
//
// Copyright (c) 2024, Xendit
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions are met:
//
// 1. Redistributions of source code must retain the above copyright notice, this
//    list of conditions and the following disclaimer.
//
// 2. Redistributions in binary form must reproduce the above copyright notice,
//    this list of conditions and the following disclaimer in the documentation
//    and/or other materials provided with the distribution.
//
// 3. Neither the name of the copyright holder nor the names of its
//    contributors may be used to endorse or promote products derived from
//    this software without specific prior written permission.
//
// THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
// AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
// IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
// DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE
// FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
// DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
// SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
// CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
// OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
// OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

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
