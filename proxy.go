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
	closeChan             chan struct{}
}

type ProxyEvent interface {
}

type ChaoticProxy struct {
	listeners map[string]namedChaoticListener
	events    chan ProxyEvent
	lock      sync.Mutex
}

type NamedListenerStartedEvent struct {
	Name     string
	Listener *ChaoticListener
}

type NamedListenerStoppedEvent struct {
	Name     string
	Listener *ChaoticListener
	Error    error
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

func NewChaoticProxy(eventChannel chan ProxyEvent) *ChaoticProxy {
	proxy := &ChaoticProxy{
		listeners: make(map[string]namedChaoticListener),
		events:    eventChannel,
	}

	return proxy
}

func (p *ChaoticProxy) ApplyConfig(config Config) error {
	p.lock.Lock()
	defer p.lock.Unlock()

	toRemove := make(map[string]struct{})
	for name := range p.listeners {
		toRemove[name] = struct{}{}
	}

	for _, listenerConfig := range config.Listeners {
		delete(toRemove, listenerConfig.Name)
		existing, exists := p.listeners[listenerConfig.Name]
		if exists {
			existingConfig := existing.listener.GetConfig()
			if existingConfig != listenerConfig {
				existing.listener.SetConfig(listenerConfig)
				p.events <- NamedListenerConfigUpdatedEvent{Name: listenerConfig.Name, Listener: existing.listener}
			}
		} else {
			netListener, netListenerErr := net.Listen("tcp", listenerConfig.ListenAddress)
			if netListenerErr != nil {
				return fmt.Errorf("failed to listen on %v: %w", listenerConfig.ListenAddress, netListenerErr)
			}
			targetAddr, addrErr := net.ResolveTCPAddr("tcp", listenerConfig.ForwardTo)
			if addrErr != nil {
				return fmt.Errorf("failed to resolve target address %v: %w", listenerConfig.ForwardTo, addrErr)
			}

			events := make(chan ListenerEvent, 100)
			listener := NewChaoticListener(listenerConfig, netListener, targetAddr, events)
			ctx, cancel := context.WithCancel(context.Background())
			closeChan := make(chan struct{})
			go func() {
				for {
					select {
					case event := <-events:
						switch e := event.(type) {
						case NewConnectionEvent:
							p.events <- NamedListenerNewConnectionEvent{Name: listenerConfig.Name, Listener: listener, ConnectionName: e.Name, Connection: e.Connection}
						case ConnectionClosedEvent:
							p.events <- NamedListenerConnectionClosedEvent{Name: listenerConfig.Name, Listener: listener, ConnectionName: e.Name, Connection: e.Connection}
						case ConnectionFailedEvent:
							p.events <- NamedListenerConnectionFailedEvent{Name: listenerConfig.Name, Listener: listener, ConnectionName: e.Name, Connection: e.Connection, Error: e.Error}
						case NewConnectionErrorEvent:
							p.events <- NamedListenerNewConnectionErrorEvent{Name: listenerConfig.Name, Listener: listener, Error: e.Error}
						case ListenerStoppedEvent:
							p.events <- NamedListenerStoppedEvent{Name: listenerConfig.Name, Listener: listener, Error: e.Error}
						}
					case <-ctx.Done():
						closeChan <- struct{}{}
						return
					}
				}
			}()

			p.listeners[listenerConfig.Name] = namedChaoticListener{
				name:                  listenerConfig.Name,
				listener:              listener,
				events:                events,
				cancelEventForwarding: cancel,
				closeChan:             closeChan,
			}
			p.events <- NamedListenerStartedEvent{Name: listenerConfig.Name, Listener: listener}
		}
	}

	for name := range toRemove {
		_ = p.listeners[name].listener.Close()
		p.listeners[name].cancelEventForwarding()
		<-p.listeners[name].closeChan
		delete(p.listeners, name)
		p.events <- NamedListenerStoppedEvent{Name: name, Listener: p.listeners[name].listener, Error: fmt.Errorf("listener removed")}
	}

	return nil
}

func (p *ChaoticProxy) Listeners() map[string]*ChaoticListener {
	p.lock.Lock()
	defer p.lock.Unlock()

	listeners := make(map[string]*ChaoticListener)
	for name, namedListener := range p.listeners {
		listeners[name] = namedListener.listener
	}

	return listeners
}

func (p *ChaoticProxy) Close() {
	p.lock.Lock()
	defer p.lock.Unlock()

	for _, namedListener := range p.listeners {
		_ = namedListener.listener.Close()
		namedListener.cancelEventForwarding()
		<-namedListener.closeChan
		p.events <- NamedListenerStoppedEvent{Name: namedListener.name, Listener: namedListener.listener, Error: fmt.Errorf("proxy closed")}
	}
}
