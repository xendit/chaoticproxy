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
	"io"
	"net"
	"time"
)

// A connection represents an active bridge between two network connections. Any data from one connection is forwarded to
// the other connection, and vice versa, with a delay and until an error occurs or either connection is closed.
type Connection struct {
	accepted            net.Conn
	forwardTo           net.Conn
	deferredToForwarded *DeferredWriter
	deferredToAccepted  *DeferredWriter
}

// Create a new connection between two network connections. This function returns immediately and the processing of the
// connection is NOT started. A call to the Forward method is required to start the forwarding.
func NewConnection(
	accepted net.Conn,
	forwardTo net.Conn,
	requestMeanDelay time.Duration,
	requestSTDDevDelay time.Duration,
	responseMeanDelay time.Duration,
	responseSTDDevDelay time.Duration,
) *Connection {
	return &Connection{
		accepted:            accepted,
		forwardTo:           forwardTo,
		deferredToForwarded: NewDeferredWriter(forwardTo, requestMeanDelay, requestSTDDevDelay),
		deferredToAccepted:  NewDeferredWriter(accepted, responseMeanDelay, responseSTDDevDelay),
	}
}

// Start forwarding data between the two connections. This function will block until an error occurs or either connection
// is closed. If an error occurs, the error will be returned.
func (c *Connection) Forward() error {
	// Start forwarding data in both directions.
	errorChan := make(chan error, 2)
	// From accepted to forwarded (i.e. request flow).
	go func() {
		_, err := io.Copy(c.deferredToForwarded, c.accepted)
		errorChan <- err
	}()
	// From forwarded to accepted (i.e. response flow).
	go func() {
		_, err := io.Copy(c.deferredToAccepted, c.forwardTo)
		errorChan <- err
	}()

	// We're only interested in the first error. Anything else is probably a consequence of the first error.
	firstError := <-errorChan

	// Abort both connections. Abort will ensure we first flush any remaining data and then close the connections.
	if firstError != nil {
		c.Abort(firstError)
	} else {
		c.Abort(io.EOF)
	}
	<-errorChan // Wait for the other goroutine to finish.
	return firstError
}

// Abort the connection. This will flush all data, stop the forwarding and close the connections. If a call to
// Forward is currently blocking, it will be unblocked and will return. It is designed to be called from a
// different goroutine than the one calling Forward.
// Calling Abort multiple times is safe and has no additional effect.
func (c *Connection) Abort(err error) {
	// Make sure we stop the deferred writers.
	c.deferredToAccepted.Stop(err)
	c.deferredToForwarded.Stop(err)

	// Also close the connections themselves. The Stop calls above will not do this for us.
	_ = c.accepted.Close()
	_ = c.forwardTo.Close()
}
