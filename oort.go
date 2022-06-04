package main

import (
	"log"
	"os"
)

func main() {
	// OK, so for the most basic version, we want the following:
	// remember that oort is an ORCHESTRATOR. So should act as a webservice that does all this work for you.
	// need an oort-client and an oort-server. The client comms w/ the server.
	// Server triggers client to dump process, process gets sent to server, server sends to client II
	// server triggers client II to restore from dump
	args := os.Args[:1]

	if args[1] == "client" {
		err := start_client()
		if err != nil {
			log.Fatal("Could not start client", err)
		}
	}
	if args[1] == "server" {
		err := start_server()
		if err != nil {
			log.Fatal("Could not start server", err)
		}
	}
}
