package api

import (
	"context"
	"log"
	"net"
	"os"
	"syscall"
	"testing"

	"github.com/cedana/cedana-api/go/task"
	"github.com/cedana/cedana/pkg/db"
	"github.com/cedana/cedana/pkg/utils"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

//  Tests defined here are different from benchmarking in that we aren't looking for
// data on performance, and are instead looking for correctness in the checkpoints and restores.

// for this, we create a server and client, connect them to each other and
// checkpoint/restore each of them - validating behavior along the way

// function to validate connections pre checkpoint and post restore
// how to validate "correctness"? should we compare the queues?

// server over here to listen for connections? and then validate that the connections were
// restablished?

// run python threaded_pings
// run server in test

type TCPTest struct {
	name string
	exec string
}

var tcpTests = map[string]TCPTest{
	"multiconn":     {"threaded_pings", "python3 benchmarking/networking/threaded_pings.py"},
	"databaseconn":  {},
	"streaming":     {},
	"multiserver":   {},
	"multidatabase": {},
}

// can't do any C/R in CI. Need to figure this out though
func skipCI(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("Skipping testing in CI environment")
	}
}

// Tests the correctness of TCP checkpoint/restore on a process with
// multiple connections
func Test_MultiConn(t *testing.T) {
	skipCI(t)

	err := os.MkdirAll("dumpdir", 0755)
	if err != nil {
		t.Error(err)
	}

	lis := bufconn.Listen(1024 * 1024)
	t.Cleanup(func() {
		lis.Close()
	})

	srv := grpc.NewServer()
	t.Cleanup(func() {
		srv.Stop()
	})

	mockDB := db.NewLocalDB(context.Background())

	logger := utils.GetLogger()

	svc := service{logger: logger, db: mockDB}
	task.RegisterTaskServiceServer(srv, &svc)

	go func() {
		if err := srv.Serve(lis); err != nil {
			log.Fatalf("srv.Serve %v", err)
		}
	}()

	dialer := func(context.Context, string) (net.Conn, error) {
		return lis.Dial()
	}
	conn, err := grpc.DialContext(context.Background(), "", grpc.WithContextDialer(dialer), grpc.WithTransportCredentials(insecure.NewCredentials()))
	t.Cleanup(func() {
		conn.Close()
	})
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}

	client := task.NewTaskServiceClient(conn)

	ctx := context.Background()

	exec := tcpTests["multiconn"].exec

	resp, err := client.Start(ctx, &task.StartArgs{Task: exec, JID: exec})
	if err != nil {
		t.Errorf("test failed: %s", err)
	}

	t.Cleanup(func() {
		syscall.Kill(int(resp.PID), syscall.SIGKILL)
		os.RemoveAll("dumpdir")
	})

	oldState, _ := svc.getState(ctx, resp.JID)
	t.Logf("old state: %+v", oldState)

	_, err = client.Dump(ctx, &task.DumpArgs{Dir: "dumpdir", PID: resp.PID, Type: task.CRType_LOCAL, JID: exec})
	if err != nil {
		t.Error(err)
	}

	// we have a running process, get network data before
	// then get network data after

	// and validate/compare
	// validation is important, because even if we've C/Rd it can C/R incorrectly
}

func Test_DatabaseConn(t *testing.T) {
	// spin up a process w/ a connection to a database
	// verify correctness on restore
}

func Test_StreamingConn(t *testing.T) {
	// spin up a client w/ a streaming connection (maybe gRPC?)
	// verify correctness on restore
}

func Test_MultiServer(t *testing.T) {
	// spin up a server w/ multiple client connections
	// verify correctness on restore
}

func Test_MultiDatabase(t *testing.T) {
	// spin up a db with multiple active connections
	// verify correctness on restore
}
