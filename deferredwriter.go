package main

import (
	"context"
	"io"
	"sync"
	"time"
)

type scheduledItem struct {
	time time.Time
	data []byte
}

// A DeferredWriter is an io.Writer that writes data with a delay. The delay follows a normal distribution from a given
// mean and deviation.
// Note that the data is always written in the order it is received.
// Because the writer is deferred, any error that occurs during writing will also be deferred. This means that the error
// of a previous write may only be returned after a subsequent write to this writer.
// As a consequence, if a write errors occurs but there is no subsequent write, the error will never be detected by the
// caller. This is a limitation inherent to the design of this writer.
// It is important to call the Stop method when the writer is no longer needed. Otherwise the writer will continue to
// wait for the next write indefinitely. Closing the underlying writer will NOT stop the DeferredWriter.
type DeferredWriter struct {
	writer      io.Writer
	meanDelay   time.Duration
	stddevDelay time.Duration
	pipeline    chan scheduledItem
	writeError  error
	ctx         context.Context
	cancel      context.CancelFunc
	mutex       sync.Mutex
}

// Create a start a new deferred writer. Any write to this writer will eventually be written to the underlying writer
// with a delay. The delay is generated from a normal distribution with the given mean and deviation.
// Note that the data is always written in the order it is received.
// It is important to understand that the writer's Write method will be invoked from a different goroutine. This
// function will return immediately after starting the writer, and the Write method of the DeferredWriter will also
// return immediately. The actual write will be performed in a separate goroutine.
// The writer will continue to write until the Stop method is called. It is important to call Stop when the writer is
// no longer needed, otherwise the writer will continue to wait for the next write indefinitely. Closing the underlying
// writer will NOT stop the DeferredWriter.
func NewDeferredWriter(writer io.Writer, meanDelay time.Duration, stddevDelay time.Duration) *DeferredWriter {
	ctx, cancel := context.WithCancel(context.Background())
	dw := &DeferredWriter{
		writer:      writer,
		meanDelay:   meanDelay,
		stddevDelay: stddevDelay,
		ctx:         ctx,
		cancel:      cancel,
		pipeline:    make(chan scheduledItem, 100),
	}

	// This is the main writer loop.
	go func() {
		for {
			select {
			case nextItem := <-dw.pipeline:
				// We have some data to write.
				{
					select {
					case <-dw.ctx.Done():
						// Tive to leave.
						return
					// We want to wait until the time has come to write the data.
					case <-time.After(time.Until(nextItem.time)):
						// Perform the actual write.
						_, writeErr := dw.writer.Write(nextItem.data)
						if writeErr != nil {
							// The write failed. We remember this error to return it
							// on the next write.
							// We also decide to stop the writer, as we assume errors
							// are not recoverable.
							dw.mutex.Lock()
							defer dw.mutex.Unlock()
							dw.writeError = writeErr
							dw.cancel()
							return
						}
					}
				}
			case <-dw.ctx.Done():
				// Time to leave.
				return
			}
		}
	}()

	return dw
}

// Write data to the DeferredWriter. The data will not be written immediately, but will be deferred to a
// different goroutine. If a previous deferred write has failed, this method will return the error of that
// previous write. Otherwise, it will "pretend" the write is successful and return the length of the data.
func (dw *DeferredWriter) Write(p []byte) (n int, err error) {
	dw.mutex.Lock()
	defer dw.mutex.Unlock()

	// If we have an error, we return it.
	if dw.writeError != nil {
		return 0, dw.writeError
	}

	// We must copy the data, as the caller may modify it after this call, or
	// reuse the buffer between calls.
	cp := make([]byte, len(p))
	copy(cp, p)

	// Queue the data.
	dw.pipeline <- scheduledItem{
		time: time.Now().Add(GenRandomDuration(dw.meanDelay, dw.stddevDelay)),
		data: cp,
	}

	// We pretend to have written all the data.
	return len(cp), nil
}

// Stop the DeferredWriter. This will stop any further writes and return the given error on the next write.
// If the writer is already stopped, this method will do nothing.
func (dw *DeferredWriter) Stop(err error) {
	dw.mutex.Lock()
	defer dw.mutex.Unlock()

	if dw.writeError != nil {
		dw.writeError = err
	}
	dw.cancel()
}
