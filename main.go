package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	configChannel := make(chan Config, 10)
	errorChannel := make(chan error, 10)
	eventChannel := make(chan ProxyEvent, 10)
	osSignalChannel := make(chan os.Signal, 1)
	signal.Notify(osSignalChannel)

	go WatchConfigFile(ctx, "config.json", configChannel, errorChannel)
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
			fmt.Printf("Error: %v\n", err)
		case event := <-eventChannel:
			switch e := event.(type) {
			case NamedListenerStartedEvent:
				fmt.Printf("Listener %v started on %v\n", e.Name, e.Listener.Addr())
			case NamedListenerStoppedEvent:
				fmt.Printf("Listener %v stopped: %v\n", e.Name, e.Error)
			case NamedListenerNewConnectionEvent:
				fmt.Printf("Listener %v accepted connection %v\n", e.Name, e.ConnectionName)
			case NamedListenerConnectionClosedEvent:
				fmt.Printf("Listener %v closed connection %v: %v\n", e.Name, e.ConnectionName, e.Error)
			case NamedListenerNewConnectionErrorEvent:
				fmt.Printf("Listener %v failed to accept connection: %v\n", e.Name, e.Error)
			case NamedListenerConfigUpdatedEvent:
				fmt.Printf("Listener %v configuration updated\n", e.Name)
			}
		}
	}

}
