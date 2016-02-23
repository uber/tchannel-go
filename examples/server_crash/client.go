package main

import (
	"fmt"
	"github.com/uber/tchannel-go"
	"github.com/uber/tchannel-go/thrift"
	"strconv"

	"github.com/uber/tchannel-go/gen-go/server"
	"io"
	"time"
)

func runClient(hostPort string) error {
	ch, err := tchannel.NewChannel("stream-client", chOptions)
	if err != nil {
		return err
	}

	ch.Peers().Add(hostPort)

	tClient := thrift.NewClient(ch, "stream", nil)
	client := server.NewTChanBInClient(tClient)

	ctx, cancel := thrift.NewContext(100 * time.Minute)
	defer cancel()

	fmt.Printf("Client: Starting at:%v...\n", hostPort)
	headers := make(map[string]string)
	headers["path"] = "foo"
	ctx = thrift.WithHeaders(ctx, headers)
	call, err := client.OpenPublisherStream(ctx)
	if err != nil {
		fmt.Printf("Client: OpenPublisherStream failed: %v\n", err)
		return nil
	}

	ackStreamClosed := make(chan bool)

	go func() {
		for {
			fmt.Println("Client: Enter Read.")
			ack, err := call.Read()
			fmt.Println("Client: Exit Read.")
			if err == io.EOF {
				fmt.Println("Client: Ack Stream Closed.")
				ackStreamClosed <- true
				break
			}

			if err != nil {
				fmt.Printf("Client: Error reading Ack Stream: %v\n", err)
				ackStreamClosed <- false
				break
			}

			fmt.Printf("Client: Received Ack for message Id: %v, Status: %v\n", ack.Id, ack.Status)
		}
	}()
	for i := 0; i < 100; i++ {
		msg := server.NewPutMessage()
		msg.Id = strconv.Itoa(i)
		msg.Data = fmt.Sprintf("Message number: %v", i)
		fmt.Println("Client: Enter Write.")
		if err := call.Write(msg); err != nil {
			fmt.Printf("Client: Error writing messages to stream: %v\n", err)
			return fmt.Errorf("Client: Error writing messages to stream: %v\n", err)
		}

		if err := call.Flush(); err != nil {
			fmt.Printf("Client: Error flushing messages to stream: %v\n", err)
			return fmt.Errorf("Client: Error flushing messages to stream: %v\n", err)
		}
		fmt.Println("Client: Exit Write.")

		time.Sleep(1 * time.Second)
	}

	if err := call.Done(); err != nil {
		fmt.Println("Client: Error closing message stream.")
	}

	ack := <-ackStreamClosed
	if ack == false {
		return fmt.Errorf("Client: Error closing ack stream.")
	}

	return nil
}
