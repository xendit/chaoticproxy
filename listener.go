package main

import (
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"
)

type ChaoticListener struct {
	config      ListenerConfig
	listener    net.Listener
	connections map[string]*Connection
	events      chan ListenerEvent
	stopping    bool
	lock        sync.Mutex
	closingChan chan struct{}
}

type ListenerEvent interface {
}

type NewConnectionEvent struct {
	Name       string
	Connection *Connection
}

type ConnectionClosedEvent struct {
	Name       string
	Connection *Connection
	Error      error
}

type NewConnectionErrorEvent struct {
	Error error
}

type ListenerStoppedEvent struct {
	Error error
}

func duration(f float64) time.Duration {
	return time.Duration(f * float64(time.Second))
}

func NewChaoticListener(config ListenerConfig, listener net.Listener, forwardTo net.Addr, events chan ListenerEvent) *ChaoticListener {
	l := &ChaoticListener{
		config:      config,
		listener:    listener,
		connections: make(map[string]*Connection),
		events:      events,
		closingChan: make(chan struct{}),
	}
	id := 0

	go func() {
		for {
			accepted, acceptErr := listener.Accept()
			if acceptErr != nil {
				l.lock.Lock()
				if !l.stopping {
					events <- ListenerStoppedEvent{Error: acceptErr}
				}
				l.lock.Unlock()
				l.closingChan <- struct{}{}
				return
			}

			go func() {
				cfg := l.GetConfig()
				defer accepted.Close()
				if Likelyhood(cfg.RejectionRate) {
					events <- NewConnectionErrorEvent{Error: fmt.Errorf("Connection chaotically rejected")}
					return
				}

				targetCon, targetError := net.Dial(forwardTo.Network(), forwardTo.String())
				if targetError != nil {
					events <- NewConnectionErrorEvent{Error: targetError}
					return
				}

				connectionErrorChan := make(chan error)
				connection := NewConnection(accepted, targetCon, duration(cfg.Latency.Mean), duration(cfg.Latency.StdDev), connectionErrorChan)

				l.lock.Lock()
				idAsString := strconv.Itoa(id)
				id++
				l.connections[idAsString] = connection
				l.lock.Unlock()
				events <- NewConnectionEvent{Name: idAsString, Connection: connection}

				if cfg.Durability.Mean != 0 && cfg.Durability.StdDev != 0 {
					go func() {
						time.Sleep(GenRandomDuration(duration(cfg.Durability.Mean), duration(cfg.Durability.StdDev)))
						conn, hasConn := l.GetConnections()[idAsString]
						if hasConn {
							_ = conn.Close()
							connectionErrorChan <- fmt.Errorf("Connection chaotically closed")
						}
					}()

				}
				connectionError := <-connectionErrorChan
				_ = connection.Close()

				l.lock.Lock()
				delete(l.connections, idAsString)
				events <- ConnectionClosedEvent{Name: idAsString, Connection: connection, Error: connectionError}
				l.lock.Unlock()
			}()

		}

	}()
	return l
}

func (l *ChaoticListener) SetConfig(config ListenerConfig) {
	l.lock.Lock()
	defer l.lock.Unlock()
	l.config = config
}

func (l *ChaoticListener) GetConfig() ListenerConfig {
	l.lock.Lock()
	defer l.lock.Unlock()
	return l.config
}

func (l *ChaoticListener) GetConnections() map[string]*Connection {
	l.lock.Lock()
	defer l.lock.Unlock()

	var ans = make(map[string]*Connection)
	for k, v := range l.connections {
		ans[k] = v
	}
	return ans
}

func (l *ChaoticListener) Addr() net.Addr {
	return l.listener.Addr()
}

func (l *ChaoticListener) Close() error {
	l.lock.Lock()
	l.stopping = true
	for name, connection := range l.connections {
		_ = connection.Close()
		l.events <- ConnectionClosedEvent{Name: name, Connection: connection, Error: fmt.Errorf("listener closed")}
	}
	l.connections = make(map[string]*Connection)
	l.lock.Unlock()
	closeErr := l.listener.Close()

	<-l.closingChan

	return closeErr
}
