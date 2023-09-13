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
func CreateTestConn() (*nats.Conn, error) {
	url := fmt.Sprintf("nats://127.0.0.1:%d", nats.DefaultPort)
	nc, err := nats.Connect(url)
	if err != nil {
		return nil, err
	}

	return nc, nil
}

func CreateTestJetstream(nc *nats.Conn) (*jetstream.JetStream, error) {
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, err
	}
	return &js, nil
}

func RunDefaultServer() *server.Server {
	opts := testserver.DefaultTestOptions
	opts.Port = nats.DefaultPort
	opts.Cluster.Name = "testing"

	server := testserver.RunServer(&opts)

	return server
}

func CreateBenchmarkConn(b *testing.B) *nats.Conn {
	url := fmt.Sprintf("nats://127.0.0.1:%d", nats.DefaultPort)
	nc, err := nats.Connect(url)
	if err != nil {
		b.Fatal(err)
	}

	b.Cleanup(func() {
		nc.Close()
	})

	return nc
}

func CreateBnechmarkJetstream(b *testing.B) jetstream.JetStream {
	nc := CreateBenchmarkConn(b)
	js, err := jetstream.New(nc)
	if err != nil {
		b.Fatal(err)
	}

	return js
}

func RunDefaultServerBenchmark(b *testing.B) *server.Server {
	opts := testserver.DefaultTestOptions
	opts.Port = nats.DefaultPort
	opts.Cluster.Name = "benchmarking"

	server := testserver.RunServer(&opts)

	b.Cleanup(func() {
		server.Shutdown()
	})

	return server
}
