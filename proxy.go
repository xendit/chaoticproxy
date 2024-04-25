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
	"fmt"
	"net"
	"sync"
)

type namedChaoticListener struct {
	name                  string
	listener              *ChaoticListener
	events                chan ListenerEvent
	cancelEventForwarding context.CancelFunc
}

type ProxyEvent interface {
}

// A chaotic proxy manages a collection of chaotic listeners, each of which listens on a specific address and forwards
// connections to a target address. The proxy can be configured with a new set of listeners, and will start, stop, or
// update existing listeners as needed. The proxy will emit events when listeners are started, stopped, or encounter
// errors.
type ChaoticProxy struct {
	listeners map[string]namedChaoticListener
	events    chan ProxyEvent
	lock      sync.Mutex
}

type NamedListenerStartedEvent struct {
	Name     string
	Listener *ChaoticListener
}

type NamedListenerFailedEvent struct {
	Name     string
	Listener *ChaoticListener
	Error    error
}

type NamedListenerStoppedEvent struct {
	Name     string
	Listener *ChaoticListener
}

type NamedListenerNewConnectionEvent struct {
	Name           string
	Listener       *ChaoticListener
	ConnectionName string
	Connection     *Connection
}

type NamedListenerConnectionClosedEvent struct {
	Name           string
	Listener       *ChaoticListener
	ConnectionName string
	Connection     *Connection
}

type NamedListenerConnectionFailedEvent struct {
	Name           string
	Listener       *ChaoticListener
	ConnectionName string
	Connection     *Connection
	Error          error
}

type NamedListenerNewConnectionErrorEvent struct {
	Name     string
	Listener *ChaoticListener
	Error    error
}

type NamedListenerConfigUpdatedEvent struct {
	Name     string
	Listener *ChaoticListener
}

// Create a new chaotic proxy that emits events on the given channel. The initial state of the proxy is empty, and
// listeners must be added by calling ApplyConfig. This function returns immediately.
func NewChaoticProxy(eventChannel chan ProxyEvent) *ChaoticProxy {
	proxy := &ChaoticProxy{
		listeners: make(map[string]namedChaoticListener),
		events:    eventChannel,
	}

	return proxy
}

// Apply a new configuration to the proxy. The proxy will start, stop, or update listeners as needed to match the new
// configuration. The function returns immediately.
func (p *ChaoticProxy) ApplyConfig(config Config) error {
	p.lock.Lock()
	defer p.lock.Unlock()

	// Keep track of all our existing listeners so we can remove any that are no longer needed.
	toRemove := make(map[string]struct{})
	for name := range p.listeners {
		toRemove[name] = struct{}{}
	}

	// Go through the new configuration and start, stop, or update listeners as needed.
	for _, listenerConfig := range config.Listeners {
		// We don't need to remove this listener, since it's still in the configuration.
		delete(toRemove, listenerConfig.Name)

		existing, exists := p.listeners[listenerConfig.Name]
		if exists {
			// Update the listener if the configuration has changed.
			existingConfig := existing.listener.GetConfig()
			if existingConfig != listenerConfig {
				existing.listener.SetConfig(listenerConfig)
				p.events <- NamedListenerConfigUpdatedEvent{Name: listenerConfig.Name, Listener: existing.listener}
			}
		} else {
			// Start a new listener. First create the net.Listener.
			netListener, netListenerErr := net.Listen("tcp", listenerConfig.ListenAddress)
			if netListenerErr != nil {
				return fmt.Errorf("failed to listen on %v: %w", listenerConfig.ListenAddress, netListenerErr)
			}
			// Next, resolve the target address.
			targetAddr, addrErr := net.ResolveTCPAddr("tcp", listenerConfig.ForwardTo)
			if addrErr != nil {
				return fmt.Errorf("failed to resolve target address %v: %w", listenerConfig.ForwardTo, addrErr)
			}

			// All is good, start the listener.
			events := make(chan ListenerEvent, 100)
			listener := NewChaoticListener(listenerConfig, netListener, targetAddr, events)
			ctx, cancel := context.WithCancel(context.Background())

			// Process events asynchronously. We'll forward them to the proxy's event channel.
			go func() {
				for {
					select {
					case event := <-events:
						switch e := event.(type) {
						case NewConnectionEvent:
							p.events <- NamedListenerNewConnectionEvent{
								Name: listenerConfig.Name, Listener: listener, ConnectionName: e.Name, Connection: e.Connection}
						case ConnectionClosedEvent:
							p.events <- NamedListenerConnectionClosedEvent{
								Name: listenerConfig.Name, Listener: listener, ConnectionName: e.Name, Connection: e.Connection}
						case ConnectionFailedEvent:
							p.events <- NamedListenerConnectionFailedEvent{
								Name: listenerConfig.Name, Listener: listener, ConnectionName: e.Name, Connection: e.Connection, Error: e.Error}
						case NewConnectionErrorEvent:
							p.events <- NamedListenerNewConnectionErrorEvent{
								Name: listenerConfig.Name, Listener: listener, Error: e.Error}
						case ListenerFailedEvent:
							p.events <- NamedListenerFailedEvent{
								Name: listenerConfig.Name, Listener: listener, Error: e.Error}
						}
					case <-ctx.Done():
						return
					}
				}
			}()

			// Add the listener to the proxy, and report that it has started.
			p.listeners[listenerConfig.Name] = namedChaoticListener{
				name:                  listenerConfig.Name,
				listener:              listener,
				events:                events,
				cancelEventForwarding: cancel,
			}
			p.events <- NamedListenerStartedEvent{Name: listenerConfig.Name, Listener: listener}
		}
	}

	// So by now, whatever is left in toRemove is no longer in the configuration. We should remove them.
	for name := range toRemove {
		_ = p.listeners[name].listener.Close()
		p.listeners[name].cancelEventForwarding()
		delete(p.listeners, name)
		p.events <- NamedListenerStoppedEvent{Name: name, Listener: p.listeners[name].listener}
	}

	return nil
}

// Get a map of all the listeners currently managed by the proxy. The map is indexed by listener name.
// The map is a copy of the proxy's internal state, so any further configuration changes will not be reflected in the
// map.
func (p *ChaoticProxy) Listeners() map[string]*ChaoticListener {
	p.lock.Lock()
	defer p.lock.Unlock()

	listeners := make(map[string]*ChaoticListener)
	for name, namedListener := range p.listeners {
		listeners[name] = namedListener.listener
	}

	return listeners
}

// Close the proxy and all its listeners. This will stop all listeners and emit events for each listener that is stopped.
func (p *ChaoticProxy) Close() {
	p.lock.Lock()
	defer p.lock.Unlock()

	for _, namedListener := range p.listeners {
		_ = namedListener.listener.Close()
		namedListener.cancelEventForwarding()
		p.events <- NamedListenerStoppedEvent{Name: namedListener.name, Listener: namedListener.listener}
	}
}
