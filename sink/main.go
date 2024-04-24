package main

import (
	"io"
	"net"
	"os"
	"os/signal"
)

func main() {
	listener, listenerErr := net.Listen("tcp", "127.0.0.1:8081")
	if listenerErr != nil {
		panic(listenerErr)
	}
	defer listener.Close()

	go func() {
		for {
			accepted, acceptErr := listener.Accept()
			if acceptErr != nil {
				panic(acceptErr)
			}
			_, _ = io.Copy(os.Stdout, accepted)
		}
	}()

	c := make(chan os.Signal, 1)
	signal.Notify(c)
	<-c

}
