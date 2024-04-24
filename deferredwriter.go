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

type DeferredWriter struct {
	writer      io.Writer
	meanDelay   time.Duration
	stddevDelay time.Duration
	pipeline    chan scheduledItem
	writeLen    int
	writeError  error
	ctx         context.Context
	cancel      context.CancelFunc
	mutex       sync.Mutex
}

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

	go func() {
		for {
			select {
			case nextItem := <-dw.pipeline:
				{
					select {
					case <-dw.ctx.Done():
						return
					case <-time.After(time.Until(nextItem.time)):
						writtenLen, writeErr := dw.writer.Write(nextItem.data)
						if writeErr != nil {
							dw.mutex.Lock()
							defer dw.mutex.Unlock()
							dw.writeError = writeErr
							dw.writeLen = writtenLen
							dw.cancel()
							return
						}
					}
				}
			case <-dw.ctx.Done():
				return
			}
		}
	}()

	return dw
}

func (dw *DeferredWriter) Write(p []byte) (n int, err error) {
	dw.mutex.Lock()
	defer dw.mutex.Unlock()
	if dw.writeError != nil {
		return dw.writeLen, dw.writeError
	}

	cp := make([]byte, len(p))
	copy(cp, p)

	item := scheduledItem{
		time: time.Now().Add(GenRandomDuration(dw.meanDelay, dw.stddevDelay)),
		data: cp,
	}

	dw.pipeline <- item
	return len(cp), nil
}

func (dw *DeferredWriter) Abort(err error) {
	dw.mutex.Lock()
	defer dw.mutex.Unlock()

	dw.writeError = err
	dw.writeLen = 0
	dw.cancel()
}
