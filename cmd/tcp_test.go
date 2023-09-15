package cmd

import (
	"syscall"
	"testing"
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

// Tests the correctness of TCP checkpoint/restore on a process with
// multiple connections
func Test_MultiConn(t *testing.T) {
	c, err := InstantiateClient()
	if err != nil {
		t.Error(err)
	}

	exec := tcpTests["multiconn"].exec

	pid, err := c.RunTask(exec)
	if err != nil {
		t.Error(err)
	}

	c.Process.PID = pid
	t.Cleanup(func() {
		syscall.Kill(int(pid), syscall.SIGKILL)
	})

	oldState := c.getState(c.Process.PID)
	c.Dump()

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
