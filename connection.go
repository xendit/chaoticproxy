package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"time"

	"github.com/xendit/chaoticproxy/utils"
)

type Connection struct {
	accepted              net.Conn
	forwardTo             net.Conn
	acceptedSourceAddress net.Addr
	acceptedLocalAddress  net.Addr
	forwardedLocalAddress net.Addr
	forwardedToAddress    net.Addr
	deferredToForwarded   *utils.DeferredWriter
	deferredToAccepted    *utils.DeferredWriter
	closing               atomic.Bool
	closingChan           chan struct{}
}

func NewConnection(accepted net.Conn, forwardTo net.Conn, meanDelay time.Duration, stddevDelay time.Duration, errorChan chan error) *Connection {
	connection := &Connection{
		accepted:              accepted,
		forwardTo:             forwardTo,
		acceptedSourceAddress: accepted.RemoteAddr(),
		acceptedLocalAddress:  accepted.LocalAddr(),
		forwardedLocalAddress: forwardTo.LocalAddr(),
		forwardedToAddress:    forwardTo.RemoteAddr(),
		deferredToForwarded:   utils.NewDeferredWriter(forwardTo, meanDelay, stddevDelay),
		deferredToAccepted:    utils.NewDeferredWriter(accepted, meanDelay, stddevDelay),
		closingChan:           make(chan struct{}),
	}

	go func() {
		_, err := io.Copy(connection.deferredToForwarded, accepted)
		if err == nil {
			err = fmt.Errorf("connection closed by initiator")
		}
		if !connection.closing.Load() {
			errorChan <- err
		}
		connection.closingChan <- struct{}{}
	}()
	go func() {
		_, err := io.Copy(connection.deferredToAccepted, forwardTo)
		if err == nil {
			err = fmt.Errorf("connection closed by target")
		}
		if !connection.closing.Load() {
			errorChan <- err
		}
		connection.closingChan <- struct{}{}
	}()

	return connection
}

func (c *Connection) AcceptedSourceAddress() net.Addr {
	return c.acceptedSourceAddress
}

func (c *Connection) AcceptedLocalAddress() net.Addr {
	return c.acceptedLocalAddress
}

func (c *Connection) ForwardedLocalAddress() net.Addr {
	return c.forwardedLocalAddress
}

func (c *Connection) ForwardedToAddress() net.Addr {
	return c.forwardedToAddress
}

func (c *Connection) String() string {
	return fmt.Sprintf(`%s->%s[Connection]%s->%s`,
		c.AcceptedSourceAddress().String(),
		c.AcceptedLocalAddress().String(),
		c.ForwardedLocalAddress().String(),
		c.ForwardedToAddress().String(),
	)
}

func (c *Connection) Close() error {
	if c.closing.Load() {
		return nil
	}
	c.closing.Store(true)
	errs := errors.Join(
		c.deferredToAccepted.Close(),
		c.deferredToForwarded.Close(),
		c.accepted.Close(),
		c.forwardTo.Close(),
	)
	<-c.closingChan
	<-c.closingChan

	return errs
}
