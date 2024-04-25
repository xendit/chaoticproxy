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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	configChannel := make(chan Config, 10)
	errorChannel := make(chan error, 10)
	eventChannel := make(chan ProxyEvent, 10)
	osSignalChannel := make(chan os.Signal, 1)
	signal.Notify(osSignalChannel)

	if configFile != "" {
		go WatchConfigFile(ctx, configFile, configChannel, errorChannel)
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

	for {
		select {
		case <-osSignalChannel:
			cancel()
		case <-ctx.Done():
			proxy.Close()
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
				Latency: RandomDuration{
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
