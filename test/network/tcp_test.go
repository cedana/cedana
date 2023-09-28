package test

import (
	"os"
	"syscall"
	"testing"

	"github.com/cedana/cedana/api"
)

//  Tests defined here are different from benchmarking in that we aren't looking for
// data on performance, and are instead looking for correctness in the checkpoints and restores and to validate
// any TCP-specific logic inside C/R.

// for this, we create a server and client, connect them to each other and
// checkpoint/restore each of them - validating behavior along the way.

// A gotcha here is that we can't live debug the dumps on these, because the external unix socket used
// for debugging can't be dumped. So debugging is useful for stepping through, and that's about it.

// function to validate connections pre checkpoint and post restore
// how to validate "correctness"? should we compare the queues?

// server over here to listen for connections? and then validate that the connections were
// restablished?

type TCPTest struct {
	name string
	exec string
}

// TODO NR - path to these is wonky! use git submodules
var tcpTests = map[string]TCPTest{
	"multiconn": {"threaded_pings", "python3 ../../cedana-benchmarks/networking/threaded_pings.py -n 3 google.com 80"},
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
	c, err := api.InstantiateClient()
	if err != nil {
		t.Error(err)
	}

	err = os.MkdirAll("dumpdir", 0755)
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
		os.RemoveAll("dumpdir")
		c.CleanupClient()
	})

	oldState := c.GetState(c.Process.PID)
	t.Logf("old state: %+v", oldState)

	err = c.Dump("dumpdir")
	if err != nil {
		t.Error(err)
	}

	// TODO NR - analyze dump w/ CRIT?

	// fairly simple validation - ensure that the sockets are still up and functioning for now
	restoredPID, err := c.Restore(nil, nil)
	if err != nil {
		t.Error(err)
	}

	c.Process.PID = *restoredPID
	newState := c.GetState(c.Process.PID)
	t.Logf("new state: %+v", newState)

	// compare sockets
	if len(oldState.ProcessInfo.OpenConnections) != len(newState.ProcessInfo.OpenConnections) {
		t.Error("sockets are different")
	}

	// compare ports
	for key, socket := range oldState.ProcessInfo.OpenConnections {
		if socket.Laddr.Port != newState.ProcessInfo.OpenConnections[key].Laddr.Port {
			t.Error("sockets are different")
		}
	}
}
