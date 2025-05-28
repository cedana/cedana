package job

// Job is a thread-safe wrapper for proto Job that is used to represent a managed process, or container.
// Simply embeds the proto Job struct, allowing us to add thread-safe methods to it.
// Allows multiple concurrent readers, but only one concurrent writer.

import (
	"context"
	"os"
	"sync"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/process"
	"google.golang.org/protobuf/proto"
)

type Job struct {
	JID   string
	proto daemon.Job

	sync.RWMutex
}

func newJob(
	jid string,
	jobType string,
) *Job {
	return &Job{
		JID: jid,
		proto: daemon.Job{
			JID:   jid,
			State: &daemon.ProcessState{},
			Type:  jobType,
		},
	}
}

func fromProto(j *daemon.Job) *Job {
	return &Job{
		JID: j.GetJID(),
		proto: daemon.Job{
			JID:     j.GetJID(),
			Type:    j.GetType(),
			State:   j.GetState(),
			Details: j.GetDetails(),
			Log:     j.GetLog(),
		},
	}
}

func (j *Job) GetPID() uint32 {
	j.RLock()
	defer j.RUnlock()
	return j.proto.GetState().GetPID()
}

func (j *Job) GetProto() *daemon.Job {
	j.Lock()
	defer j.Unlock()

	// Get all latest info
	j.proto.State = j.latestState()
	j.proto.Log = j.latestLog()

	return proto.CloneOf(&j.proto)
}

func (j *Job) GetType() string {
	j.RLock()
	defer j.RUnlock()
	return j.proto.Type
}

func (j *Job) GetState() *daemon.ProcessState {
	j.Lock()
	defer j.Unlock()

	return j.latestState()
}

func (j *Job) SetState(state *daemon.ProcessState) {
	j.Lock()
	defer j.Unlock()
	j.proto.State = proto.CloneOf(state)
}

func (j *Job) FillState(ctx context.Context, pid uint32) error {
	j.Lock()
	defer j.Unlock()

	return utils.FillProcessState(ctx, pid, j.proto.State)
}

func (j *Job) GetDetails() *daemon.Details {
	j.RLock()
	defer j.RUnlock()
	return proto.CloneOf(j.proto.Details)
}

func (j *Job) SetDetails(details *daemon.Details) {
	j.Lock()
	defer j.Unlock()
	j.proto.Details = proto.CloneOf(details)
	j.proto.Details.JID = proto.String(j.JID)
}

func (j *Job) GetLog() string {
	j.RLock()
	defer j.RUnlock()

	return j.latestLog()
}

func (j *Job) SetLog(log string) {
	j.Lock()
	defer j.Unlock()
	j.proto.Log = log
}

func (j *Job) IsRunning() bool {
	j.Lock()
	defer j.Unlock()
	return j.latestState().GetIsRunning()
}

func (j *Job) IsRemote() bool {
	j.Lock()
	defer j.Unlock()
	return j.latestState().GetStatus() == "remote"
}

func (j *Job) GPUEnabled() bool {
	j.RLock()
	defer j.RUnlock()
	return j.proto.GetState().GetGPUEnabled()
}

func (j *Job) SetGPUEnabled(enabled bool) {
	j.Lock()
	defer j.Unlock()
	if j.proto.State == nil {
		j.proto.State = &daemon.ProcessState{}
	}

	j.proto.State.GPUEnabled = enabled
}

///////////////
/// Helpers ///
///////////////

// Functions below don't use locks, so they could be called with locks held.

// WARN: Writes, so call with write lock.
func (j *Job) latestState() (state *daemon.ProcessState) {
	if j.proto.State == nil {
		return nil
	}
	state = proto.CloneOf(j.proto.State)

	// Check if job belongs to this machine

	hostId, _ := host.HostID()
	if state.GetHost().GetID() != hostId {
		state.Status = "remote"
		state.IsRunning = false
		return
	}

	state.Status = "halted"
	state.IsRunning = false

	// Get latest process info

	p, err := process.NewProcess(int32(state.PID))
	if err != nil {
		return
	}

	// Now check if this exact process is running on this
	// machine. Because, you could simply have another process
	// running right now with this saved job PID.

	createTime, err := p.CreateTime()
	if err != nil {
		return
	}
	if state.StartTime != uint64(createTime) {
		return
	}

	// Try to fill rest as much as possible, let it error
	utils.FillProcessState(context.TODO(), state.PID, state)

	return
}

func (j *Job) latestLog() string {
	// Check if log file exists
	log := j.proto.Log
	if _, e := os.Stat(log); os.IsNotExist(e) {
		return ""
	}

	return log
}
