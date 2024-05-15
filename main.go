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
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
)

func run(configFile string, configEnv string, configStr string) {
	rootContext, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()

	configChannel := make(chan Config, 100)
	errorChannel := make(chan error, 100)
	eventChannel := make(chan ProxyEvent, 100)
	osSignalChannel := make(chan os.Signal, 1)
	signal.Notify(osSignalChannel)

	if configFile != "" {
		go WatchConfigFile(rootContext, configFile, configChannel, errorChannel)
	}
	if configEnv != "" {
		configStr = os.Getenv(configEnv)
		if configStr == "" {
			fmt.Printf("Error: %v\n", fmt.Errorf("environment variable %v not set", configEnv))
			return
		}
	}
	if configStr != "" {
		config, configError := LoadConfigFromString(configStr)
		if configError != nil {
			fmt.Printf("Error: %v\n", fmt.Errorf("failed to load configuration: %v", configError))
			return
		}
		if len(config.Listeners) == 0 {
			fmt.Printf("Warning: %v\n", fmt.Errorf("no listeners in configuration"))
		}
		configChannel <- config
	}

	proxy := NewChaoticProxy(eventChannel)

	proxyContext, proxyCancel := context.WithCancel(rootContext)
	defer proxyCancel()

	for {
		select {
		case <-osSignalChannel:
			signal.Stop(osSignalChannel)
			proxyCancel()
		case <-proxyContext.Done():
			if len(eventChannel) == 0 && len(errorChannel) == 0 && len(configChannel) == 0 {
				go func() {
					// Do proxy closure in a separate goroutine so that we can continue
					// to process events and errors until the end.
					proxy.Close()
					rootCancel()
				}()
			}
		case <-rootContext.Done():
			return
		case config := <-configChannel:
			configError := proxy.ApplyConfig(config)
			if configError != nil {
				errorChannel <- configError
			}
		case err := <-errorChannel:
			fmt.Printf("Warning: %v\n", err)
		case event := <-eventChannel:
			switch e := event.(type) {
			case NamedListenerStartedEvent:
				fmt.Printf("Listener %v started on %v\n", e.Name, e.Listener.Addr())
			case NamedListenerStoppedEvent:
				fmt.Printf("Listener %v stopped\n", e.Name)
			case NamedListenerFailedEvent:
				fmt.Printf("Listener %v failed: %v\n", e.Name, e.Error)
			case NamedListenerNewConnectionEvent:
				fmt.Printf("Listener %v accepted connection %v\n", e.Name, e.ConnectionName)
			case NamedListenerConnectionClosedEvent:
				fmt.Printf("Listener %v closed connection %v\n", e.Name, e.ConnectionName)
			case NamedListenerConnectionFailedEvent:
				fmt.Printf("Listener %v failed connection %v: %v\n", e.Name, e.ConnectionName, e.Error)
			case NamedListenerNewConnectionErrorEvent:
				fmt.Printf("Listener %v failed to accept connection: %v\n", e.Name, e.Error)
			case NamedListenerConfigUpdatedEvent:
				fmt.Printf("Listener %v configuration updated\n", e.Name)
			}
		}
	}
}

func genConfig(configFile string) {
	_, statError := os.Stat(configFile)
	if statError == nil {
		fmt.Printf("Error: %v\n", fmt.Errorf("file %v already exists", configFile))
		return
	}
	config := Config{
		Listeners: []ListenerConfig{
			{
				Name:          "sink",
				ListenAddress: "localhost:8080",
				ForwardTo:     "localhost:80",
				RequestLatency: RandomDuration{
					Mean:   0.1,
					StdDev: 0.05,
				},
				ResponseLatency: RandomDuration{
					Mean:   0.1,
					StdDev: 0.05,
				},
				RejectionRate: 0.02,
				Durability: RandomDuration{
					Mean:   120,
					StdDev: 30,
				},
			},
		},
	}
	data, marshalError := json.MarshalIndent(config, "", "  ")
	if marshalError != nil {
		fmt.Printf("Error: %v\n", fmt.Errorf("failed to marshal configuration: %v", marshalError))
		return
	}

	writeError := os.WriteFile(configFile, data, 0600)
	if writeError != nil {
		fmt.Printf("Error: %v\n", fmt.Errorf("failed to write file %v: %v", configFile, writeError))
		return
	}
	fmt.Printf("Configuration file %v created\n", configFile)
}

func main() {
	flag.Usage = func() {
		fmt.Printf("Usage: %v [options] [action]\n", filepath.Base(os.Args[0]))
		fmt.Println()
		fmt.Println("Actions:")
		fmt.Println("  run         Run the proxy")
		fmt.Println("  genconfig   Generate a sample configuration file")
		fmt.Println("  help 	   Show this help message")
		fmt.Println("If no action is specified, run is assumed")
		fmt.Println()
		fmt.Println("Options:")
		flag.PrintDefaults()
	}

	configFile := flag.String("config-file", "config.json", "Path to the configuration file. It is ignored if either -config-env or -config-str is set")
	configEnv := flag.String("config-env", "", "Environment variable containing the configuration")
	configStr := flag.String("config-str", "", "Configuration as a JSON string")
	flag.Parse()
	action := flag.Arg(0)
	if action == "" {
		action = "run"
	}

	if *configEnv != "" || *configStr != "" {
		*configFile = ""
	}

	switch action {
	case "run":
		run(*configFile, *configEnv, *configStr)
	case "genconfig":
		genConfig(*configFile)
	default:
		flag.Usage()
	}
}
