package main

import (
	"flag"
	"fmt"

	"github.com/uber/tchannel-go"
)

var chOptions = &tchannel.ChannelOptions{Logger: tchannel.SimpleLogger}

func main() {
	var startServer, startClient bool
	flag.BoolVar(&startServer, "server", false, "Start TChannel Server.")
	flag.BoolVar(&startClient, "client", false, "Start TChannel Client.")
	flag.Parse()

	ip, err := tchannel.ListenIP()
	if err != nil {
		fmt.Printf("Failed to find IP to Listen on: %v\n", err)
	}
	hostPort := fmt.Sprintf("%s:%d", ip.String(), 5615)

	if startServer {
		serverStart(hostPort)
	} else if startClient {
		if err := runClient(hostPort); err != nil {
			fmt.Printf("RunClient failed: %v", err)
			return
		}
	}

	select {}
}
