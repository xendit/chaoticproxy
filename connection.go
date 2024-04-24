package main

import (
	"io"
	"net"
	"time"
)

type Connection struct {
	accepted            net.Conn
	forwardTo           net.Conn
	deferredToForwarded *DeferredWriter
	deferredToAccepted  *DeferredWriter
}

func NewConnection(accepted net.Conn, forwardTo net.Conn, meanDelay time.Duration, stddevDelay time.Duration) *Connection {
	return &Connection{
		accepted:            accepted,
		forwardTo:           forwardTo,
		deferredToForwarded: NewDeferredWriter(forwardTo, meanDelay, stddevDelay),
		deferredToAccepted:  NewDeferredWriter(accepted, meanDelay, stddevDelay),
	}
}

func (c *Connection) Forward() error {
	errorChan := make(chan error, 2)
	go func() {
		_, err := io.Copy(c.deferredToForwarded, c.accepted)
		errorChan <- err
	}()
	go func() {
		_, err := io.Copy(c.deferredToAccepted, c.forwardTo)
		errorChan <- err
	}()

	err := <-errorChan

	_ = c.accepted.Close()
	_ = c.forwardTo.Close()

	return err
}

func (c *Connection) Abort(err error) {
	c.deferredToAccepted.Abort(err)
	c.deferredToForwarded.Abort(err)
	_ = c.accepted.Close()
	_ = c.forwardTo.Close()
}
