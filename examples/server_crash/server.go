package main

import (
	"fmt"
	"github.com/uber/tchannel-go"
	"github.com/uber/tchannel-go/thrift"

	"github.com/uber/tchannel-go/examples/server_crash/gen-go/server"
	"io"
)

type (
	Server struct{}
)

func serverStart(hostPort string) string {
	ch, err := tchannel.NewChannel("stream", chOptions)
	if err != nil {
		fmt.Printf("NewChannel failed: %v\n", err)
	}

	svr := thrift.NewServer(ch)
	svr.RegisterStreaming(server.NewTChanBInServer(Server{}))

	//ip, err := tchannel.ListenIP()
	//if err != nil {
	//fmt.Printf("Failed to find IP to Listen on: %v\n", err)
	//}

	//hostPort := fmt.Sprintf("%s:%d", ip.String(), 5614)
	fmt.Printf("Server: Starting at:%v...\n", hostPort)

	if err := ch.ListenAndServe(hostPort); err != nil {
		fmt.Printf("ListenAndServe failed: %v\n", err)
	}

	return hostPort
}

func (Server) OpenPublisherStream(ctx thrift.Context, call server.BInOpenPublisherStreamInCall) error {
	path, ok := ctx.Headers()["path"]
	if !ok {
		path = "No Path"
	}
	fmt.Printf("Server: OpenPublisherStream called with path: %v.\n", path)

	ackChannel := make(chan *server.PutMessageAck, 100)

	// Start pump to read messages from the request stream
	go func() {
		for {
			msg, err := call.Read()
			if err == io.EOF {
				fmt.Println("Server: PublisherStream closed.")
				// Close AckChannel so we can stop sending acks
				close(ackChannel)
				break
			}

			if err != nil {
				fmt.Printf("Server: Error reading PublisherStream: %v\n", err)
				break
			}

			fmt.Printf("Server: Received msg with Id: %v\n", msg.Id)
			// TODO: Write the message to Store

			// Send Ack to Go Channel
			ack := server.NewPutMessageAck()
			ack.Id = msg.Id
			ack.Status = server.Status_OK

			fmt.Println("Writing to ack channel")
			ackChannel <- ack
			fmt.Println("Wrote to ack channel")
		}
	}()
	// Start the pump to send Acks back to the client
	for quit := false; !quit; {
		select {
		case ack, ok := <-ackChannel:
			if ok {
				if err := call.Write(ack); err != nil {
					fmt.Printf("Server: Unable to Write Ack back to client for Id: %v, error: %v\n", ack.Id, err)
				}
			} else {
				// AckChannel is closed, break out of the Ack pump
				quit = true
			}

		default:
			if err := call.Flush(); err != nil {
				fmt.Printf("Server: Unable to Flush Ack back to client: %v\n", err)
			}

			// Prevent busy loop by receiving another value from ackChannel
			ack, ok := <-ackChannel
			if ok {
				if err := call.Write(ack); err != nil {
					fmt.Printf("Server: Unable to Write Ack back to client for Id: %v, error: %v\n", ack.Id, err)
				}
			} else {
				// AckChannel is closed
				quit = true
			}
		}
	}

	// Flush pending acks on the response stream
	if err := call.Flush(); err != nil {
		fmt.Printf("Server: Unable to Flush Ack back to client: %v\n", err)
	}

	// Close MessageAck Stream
	return call.Done()
}
