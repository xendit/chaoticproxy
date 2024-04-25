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
func NewConnection(accepted net.Conn, forwardTo net.Conn, meanDelay time.Duration, stddevDelay time.Duration) *Connection {
	return &Connection{
		accepted:            accepted,
		forwardTo:           forwardTo,
		deferredToForwarded: NewDeferredWriter(forwardTo, meanDelay, stddevDelay),
		deferredToAccepted:  NewDeferredWriter(accepted, meanDelay, stddevDelay),
	}
}

// Start forwarding data between the two connections. This function will block until an error occurs or either connection
// is closed. If an error occurs, the error will be returned.
func (c *Connection) Forward() error {
	// Ensure that the connections are closed when we're done.
	defer c.Abort(io.EOF)

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
	return <-errorChan
}

// Abort the connection. This will stop the forwarding and close the connections. If a call to Forward is currently
// blocking, it will be unblocked and will return. It is designed to be called from a different goroutine than the one
// calling Forward.
// Calling Abort multiple times is safe and has no additional effect.
func (c *Connection) Abort(err error) {
	// Make sure we stop the deferred writers.
	c.deferredToAccepted.Stop(err)
	c.deferredToForwarded.Stop(err)

	// Also close the connections themselves. The Stop calls above will not do this for us.
	_ = c.accepted.Close()
	_ = c.forwardTo.Close()
}
