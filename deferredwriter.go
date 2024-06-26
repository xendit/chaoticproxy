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
	writer                       io.Writer
	meanDelay                    time.Duration
	stddevDelay                  time.Duration
	pipeline                     chan scheduledItem
	writeErrorFromWriterLoop     error
	writeErrorToReturnDuringStop error
	cancel                       context.CancelFunc
	mutex                        sync.Mutex
	stopChan                     chan struct{}
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
		cancel:      cancel,
		pipeline:    make(chan scheduledItem, 100),
		stopChan:    make(chan struct{}),
	}

	// This is the main writer loop.
	go func() {
		defer close(dw.stopChan)
		for {
			select {
			case nextItem := <-dw.pipeline:
				// We have some data to write.
				{
					// Wait for the scheduled time.
					// We do NOT want to allow this sleep to be interrupted
					// by the context. We want to write the data even if the
					// context is done.
					time.Sleep(time.Until(nextItem.time))
					// Perform the actual write.
					_, writeErr := dw.writer.Write(nextItem.data)
					if writeErr != nil {
						// The write failed. We remember this error to return it
						// on the next write.
						// We also decide to stop the writer, as we assume errors
						// are not recoverable.
						dw.mutex.Lock()
						dw.writeErrorFromWriterLoop = writeErr
						dw.mutex.Unlock()
						dw.cancel()
						return
					}
				}
			case <-ctx.Done():
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
	// If we have an error, we return it.
	dw.mutex.Lock()
	// First check if we are currently stopping. If so, we return the error to the caller.
	writeError := dw.writeErrorToReturnDuringStop
	// If we are not stopping, we also check if there is an error coming from the writer loop.
	if writeError == nil {
		writeError = dw.writeErrorFromWriterLoop
	}
	dw.mutex.Unlock()
	if writeError != nil {
		return 0, writeError
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

// Flush then stop the DeferredWriter. This will stop any further writes and return the given error on
// the next write. If the writer is already stopped, this method will do nothing.
func (dw *DeferredWriter) Stop(err error) {
	// First we set the error to return on the next write. This prevents any further writes
	// from being queued while we are stopping.
	dw.mutex.Lock()
	dw.writeErrorToReturnDuringStop = err
	dw.mutex.Unlock()

	// Then, we must wait for the pipeline to be empty. This means there are no pending
	// writes in the pipe. Note that it does NOT indicate that the last write has been
	// written to the underlying writer...
	for len(dw.pipeline) > 0 {
		// Check the write error. It will be set if the writer has stopped on its own.
		dw.mutex.Lock()
		writeErr := dw.writeErrorFromWriterLoop
		dw.mutex.Unlock()
		if writeErr != nil {
			break
		}

		// Wait a bit before checking again.
		time.Sleep(10 * time.Millisecond)
	}

	// At this stage, the pipe is empty and it will stay this way. Let's request the writer
	// to stop (if it hasn't already).
	dw.cancel()

	// Wait for the chan to be closed. That is the indication that the writer has stopped,
	// and therefore the last write has been written to the underlying writer.
	<-dw.stopChan
}
