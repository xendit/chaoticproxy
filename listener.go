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
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"
)

// A ChaoticListener is a listener that accepts incoming connections and forwards them to a target address, but
// with some chaos. The chaos includes:
// - Rejection of connections with a given rate.
// - Delay of connections with a given mean and standard deviation.
// - Closing of connections after a delay with a given mean and standard deviation.
type ChaoticListener struct {
	config      ListenerConfig
	listener    net.Listener
	connections map[string]*Connection
	events      chan ListenerEvent
	stopping    bool
	lock        sync.Mutex
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
}

type ConnectionFailedEvent struct {
	Name       string
	Connection *Connection
	Error      error
}

type NewConnectionErrorEvent struct {
	Error error
}

type ListenerFailedEvent struct {
	Error error
}

func duration(f float64) time.Duration {
	return time.Duration(f * float64(time.Second))
}

// Create a start a new chaotic listener. This listener will accept incoming connections and forward them to the given
// target address until the Close method is called.
// This function returns immediately after starting the listener. The actual listening is done in a separate goroutine.
// The chaotic behavior of the listener is defined by the given configuration.
// The events channel is used to send events about the listener and its connections.
// Once started, the configuration can be updated using the SetConfig method. This will only affect new connections.
func NewChaoticListener(config ListenerConfig, listener net.Listener, forwardTo net.Addr, events chan ListenerEvent) *ChaoticListener {
	l := &ChaoticListener{
		config:      config,
		listener:    listener,
		connections: make(map[string]*Connection),
		events:      events,
	}

	go func() {
		// We'll use a simple counter to generate connection Names.
		connectionName := 0

		// The main listener loop.
		for {
			accepted, acceptErr := listener.Accept()
			if acceptErr != nil {
				// The listener has been closed.

				// Do not report a failed event if we're stopping, as the error is expected in that case.
				l.lock.Lock()
				if !l.stopping {
					events <- ListenerFailedEvent{Error: acceptErr}
				}
				l.lock.Unlock()
				return
			}

			// We have a new connection. Let's handle it.
			go func() {
				defer accepted.Close()

				// Snapshot the configuration we'll use.
				cfg := l.GetConfig()

				// Check if we should reject the connection.
				if Likelihood(cfg.RejectionRate) {
					events <- NewConnectionErrorEvent{Error: fmt.Errorf("Connection chaotically rejected")}
					return
				}

				// Connect to the target.
				targetCon, targetError := net.Dial(forwardTo.Network(), forwardTo.String())
				if targetError != nil {
					events <- NewConnectionErrorEvent{Error: targetError}
					return
				}
				defer targetCon.Close()

				// Create a new connection.
				connection := NewConnection(accepted, targetCon, duration(cfg.Latency.Mean), duration(cfg.Latency.StdDev))

				// Generate a name for the connection and store it.
				l.lock.Lock()
				nameAsString := strconv.Itoa(connectionName)
				connectionName++
				l.connections[nameAsString] = connection
				l.lock.Unlock()

				// Issue a new connection event.
				events <- NewConnectionEvent{Name: nameAsString, Connection: connection}

				if cfg.Durability.Mean != 0 && cfg.Durability.StdDev != 0 {
					// If we have a durability configuration, we'll close the connection after a delay.
					go func() {
						// Mmmh... definitely not the best approach, as we'll have a goroutine per connection
						// that could survive the end of the connection itself. That will do for now, but there
						// are better ways to handle this.
						time.Sleep(GenRandomDuration(duration(cfg.Durability.Mean), duration(cfg.Durability.StdDev)))
						conn, hasConn := l.GetConnections()[nameAsString]
						if hasConn {
							conn.Abort(fmt.Errorf("connection chaotically closed"))
						}
					}()

				}

				// Start forwarding the connection. This is a blocking call.
				connectionError := connection.Forward()

				// The connection has been closed. Let's issue an event.
				l.lock.Lock()
				delete(l.connections, nameAsString)
				if connectionError != nil {
					events <- ConnectionFailedEvent{Name: nameAsString, Connection: connection, Error: connectionError}
				} else {
					events <- ConnectionClosedEvent{Name: nameAsString, Connection: connection}
				}
				l.lock.Unlock()
			}()

		}

	}()

	return l
}

// Set the configuration of the listener. This will only affect new connections.
func (l *ChaoticListener) SetConfig(config ListenerConfig) {
	l.lock.Lock()
	defer l.lock.Unlock()
	l.config = config
}

// Get the current configuration of the listener that will be used for new connections.
func (l *ChaoticListener) GetConfig() ListenerConfig {
	l.lock.Lock()
	defer l.lock.Unlock()
	return l.config
}

// Get the current connections of the listener. The returned map is a snapshot of the current connections,
// and will not be updated if connections are added or removed.
func (l *ChaoticListener) GetConnections() map[string]*Connection {
	l.lock.Lock()
	defer l.lock.Unlock()

	var ans = make(map[string]*Connection)
	for k, v := range l.connections {
		ans[k] = v
	}
	return ans
}

// Get the address of the listener.
func (l *ChaoticListener) Addr() net.Addr {
	return l.listener.Addr()
}

// Close the listener. This will stop accepting new connections and abort all existing connections.
func (l *ChaoticListener) Close() error {
	l.lock.Lock()
	l.stopping = true
	for _, connection := range l.connections {
		connection.Abort(fmt.Errorf("listener closed"))
	}
	l.connections = make(map[string]*Connection)
	l.lock.Unlock()
	return l.listener.Close()
}
