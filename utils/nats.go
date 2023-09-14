package utils

import (
	"fmt"
	"testing"

	"github.com/nats-io/nats-server/v2/server"
	testserver "github.com/nats-io/nats-server/v2/test"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// helpers for testing NATS stuff
func CreateTestConn(t *testing.T) *nats.Conn {
	url := fmt.Sprintf("nats://127.0.0.1:%d", nats.DefaultPort)
	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		nc.Close()
	})

	return nc
}

func CreateTestJetstream(t *testing.T) jetstream.JetStream {
	nc := CreateTestConn(t)
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}

	return js
}

func RunDefaultServer(t *testing.T) *server.Server {
	opts := testserver.DefaultTestOptions
	opts.Port = nats.DefaultPort
	opts.Cluster.Name = "testing"

	server := testserver.RunServer(&opts)

	t.Cleanup(func() {
		server.Shutdown()
	})

	return server
}
