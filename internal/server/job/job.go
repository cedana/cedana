package job

// Job is a thread-safe wrapper for proto Job that is used to represent a managed process, or container.
// Simply embeds the proto Job struct, allowing us to add thread-safe methods to it.
// Allows multiple concurrent readers, but only one concurrent writer.

import (
	"context"
	"os"
	"sync"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/shirou/gopsutil/v4/process"
	"google.golang.org/protobuf/proto"
)

type Job struct {
	JID   string
	proto daemon.Job

	// Notify callbacks that can be saved for later use.
	// Will be called each time the job is C/R'd.
	criuCallback *criu.NotifyCallbackMulti

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
			JID:         j.GetJID(),
			Type:        j.GetType(),
			State:       j.GetState(),
			Details:     j.GetDetails(),
			Log:         j.GetLog(),
			Checkpoints: j.GetCheckpoints(),
			GPUEnabled:  j.GetGPUEnabled(),
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
	j.proto.State = j.state()
	j.proto.Log = j.log()

	return &j.proto
}

func (j *Job) GetType() string {
	j.RLock()
	defer j.RUnlock()
	return j.proto.Type
}

func (j *Job) GetState() *daemon.ProcessState {
	j.Lock()
	defer j.Unlock()

	return j.state()
}

func (j *Job) SetState(state *daemon.ProcessState) {
	j.Lock()
	defer j.Unlock()
	j.proto.State = state
}

func (j *Job) FillState(ctx context.Context, pid uint32) error {
	j.Lock()
	defer j.Unlock()

	return utils.FillProcessState(ctx, pid, j.proto.State)
}

func (j *Job) GetDetails() *daemon.Details {
	j.RLock()
	defer j.RUnlock()
	return j.proto.Details
}

func (j *Job) SetDetails(details *daemon.Details) {
	j.Lock()
	defer j.Unlock()
	j.proto.Details = details
	j.proto.Details.JID = proto.String(j.JID)
}

func (j *Job) GetLog() string {
	j.RLock()
	defer j.RUnlock()

	return j.log()
}

func (j *Job) SetLog(log string) {
	j.Lock()
	defer j.Unlock()
	j.proto.Log = log
}

func (j *Job) AddCheckpoint(path string) {
	j.Lock()
	defer j.Unlock()
	if j.proto.Checkpoints == nil {
		j.proto.Checkpoints = []*daemon.Checkpoint{}
	}

	size, _ := utils.SizeFromPath(path)

	j.proto.Checkpoints = append(j.proto.Checkpoints, &daemon.Checkpoint{
		Path: path,
		Time: time.Now().UnixMilli(),
		Size: size,
	})
}

func (j *Job) GetCheckpoints() []*daemon.Checkpoint {
	j.RLock()
	defer j.RUnlock()
	return j.proto.Checkpoints
}

func (j *Job) GetLatestCheckpoint() *daemon.Checkpoint {
	j.RLock()
	defer j.RUnlock()
	if len(j.proto.Checkpoints) == 0 {
		return nil
	}
	return j.proto.Checkpoints[len(j.proto.Checkpoints)-1]
}

func (j *Job) IsRunning() bool {
	j.Lock()
	defer j.Unlock()
	return j.state().GetIsRunning()
}

func (j *Job) GPUEnabled() bool {
	j.RLock()
	defer j.RUnlock()
	return j.proto.GPUEnabled
}

func (j *Job) SetGPUEnabled(enabled bool) {
	j.Lock()
	defer j.Unlock()
	j.proto.GPUEnabled = enabled
}

func (j *Job) GetCRIUCallback() *criu.NotifyCallbackMulti {
	j.RLock()
	defer j.RUnlock()
	return j.criuCallback
}

func (j *Job) AddCRIUCallback(n *criu.NotifyCallback) {
	j.Lock()
	defer j.Unlock()
	if j.criuCallback == nil {
		j.criuCallback = &criu.NotifyCallbackMulti{}
	}
	j.criuCallback.Include(n)
}

///////////////
/// Helpers ///
///////////////

// Functions below don't use locks, so they could be called with locks held.

// WARN: Writes, so call with write lock.
func (j *Job) state() *daemon.ProcessState {
	if j.proto.GetState() == nil {
		j.proto.State = &daemon.ProcessState{}
	}

	j.proto.State.Status = "halted"
	j.proto.State.IsRunning = false

	pid := j.proto.GetState().GetPID()

	p, err := process.NewProcess(int32(pid))
	if err == nil {
		status, err := p.Status()
		if err == nil {
			j.proto.State.Status = status[0]
			j.proto.State.IsRunning = true
		}
	}

	return j.proto.State
}

func (j *Job) log() string {
	// Check if log file exists
	log := j.proto.Log
	if _, e := os.Stat(log); os.IsNotExist(e) {
		return ""
	}

	return log
}
