package job

// Job is a thread-safe wrapper for proto Job that is used to represent a managed process, or container.
// Simply embeds the proto Job struct, allowing us to add thread-safe methods to it.
// Allows multiple concurrent readers, but only one concurrent writer.

import (
	"sync"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
)

type Job struct {
	JID    string
	exited <-chan int
	proto  daemon.Job

	m sync.RWMutex
}

func newJob(
	jid string,
	jobType string,
) *Job {
	return &Job{
		jid,
		nil,
		daemon.Job{
			JID:     jid,
			Type:    jobType,
			Process: &daemon.ProcessState{},
			Details: &daemon.Details{},
		},
		sync.RWMutex{},
	}
}

func (j *Job) GetPID() uint32 {
	j.m.RLock()
	defer j.m.RUnlock()
	return j.proto.GetDetails().GetPID()
}

func (j *Job) GetProto() *daemon.Job {
	j.m.RLock()
	defer j.m.RUnlock()
	return &j.proto
}

func (j *Job) GetType() string {
	j.m.RLock()
	defer j.m.RUnlock()
	return j.proto.Type
}

func (j *Job) SetType(jobType string) {
	j.m.Lock()
	defer j.m.Unlock()
	j.proto.Type = jobType
}

func (j *Job) GetProcess() *daemon.ProcessState {
	j.m.RLock()
	defer j.m.RUnlock()
	return j.proto.Process
}

func (j *Job) SetProcess(process *daemon.ProcessState) {
	j.m.Lock()
	defer j.m.Unlock()
	j.proto.Process = process
}

func (j *Job) GetDetails() *daemon.Details {
	j.m.RLock()
	defer j.m.RUnlock()
	return j.proto.Details
}

func (j *Job) SetDetails(details *daemon.Details) {
	j.m.Lock()
	defer j.m.Unlock()
	j.proto.Details = details
}

func (j *Job) GetLog() string {
	j.m.RLock()
	defer j.m.RUnlock()
	return j.proto.Log
}

func (j *Job) SetLog(log string) {
	j.m.RLock()
	defer j.m.RUnlock()
	j.proto.Log = log
}

func (j *Job) SetCheckpointPath(path string) {
	j.m.Lock()
	defer j.m.Unlock()
	j.proto.CheckpointPath = path
}

func (j *Job) GetCheckpointPath() string {
	j.m.RLock()
	defer j.m.RUnlock()
	return j.proto.CheckpointPath
}

func (j *Job) SetExited(exited <-chan int) {
	j.m.Lock()
	defer j.m.Unlock()
	j.exited = exited
}

func (j *Job) Wait() {
	j.m.RLock()
	<-j.exited
	j.m.RUnlock()

	j.m.Lock()
	defer j.m.Unlock()

	if j.proto.GetProcess().GetInfo() == nil {
		j.proto.Process.Info = &daemon.ProcessInfo{}
	}
	j.proto.Process.Info.IsRunning = false
}

func (j *Job) IsRunning() bool {
	j.m.RLock()
	defer j.m.RUnlock()
	return j.proto.GetProcess().GetInfo().GetIsRunning()
}
