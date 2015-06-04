package main

import (
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"

	raw "http-sniffer/raw_socket_listener"
)

type RAWInput struct {
	data    chan []byte
	address string
}

func NewRAWInput(address string) (i *RAWInput) {
	i = new(RAWInput)
	i.data = make(chan []byte)
	i.address = address

	go i.listen(address)

	return
}

func (i *RAWInput) Read(data []byte) (int, error) {
	buf := <-i.data
	copy(data, buf)

	return len(buf), nil
}

func (i *RAWInput) listen(address string) {
	address = strings.Replace(address, "[::]", "127.0.0.1", -1)

	host, port, err := net.SplitHostPort(address)

	listen_port, _ := strconv.Atoi(port)
	if listen_port <= 0 {
		fmt.Printf("listen on %s\n", host)
	} else {
		fmt.Printf("listen on %s\n", address)
	}

	if err != nil {
		log.Fatal("input-raw: error while parsing address", err)
	}

	listener := raw.NewListener(host, port)

	for {
		// Receiving TCPMessage object
		m := listener.Receive()

		i.data <- m.Bytes()
	}
}

func (i *RAWInput) String() string {
	return "RAW Socket input: " + i.address
}
