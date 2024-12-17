package job

// Job is a thread-safe wrapper for proto Job that is used to represent a managed process, or container.
// Simply embeds the proto Job struct, allowing us to add thread-safe methods to it.
// Allows multiple concurrent readers, but only one concurrent writer.

import (
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
	criuCallback criu.NotifyCallbackMulti

	sync.RWMutex
}

func newJob(
	jid string,
	jobType string,
) *Job {
	return &Job{
		JID: jid,
		proto: daemon.Job{
			JID:  jid,
			Type: jobType,
		},
		criuCallback: criu.NotifyCallbackMulti{},
	}
}

func fromProto(j *daemon.Job) *Job {
	return &Job{
		JID: j.GetJID(),
		proto: daemon.Job{
			JID:         j.GetJID(),
			Type:        j.GetType(),
			Process:     j.GetProcess(),
			Details:     j.GetDetails(),
			Log:         j.GetLog(),
			Checkpoints: j.GetCheckpoints(),
			GPUEnabled:  j.GetGPUEnabled(),
		},
		criuCallback: criu.NotifyCallbackMulti{},
	}
}

func (j *Job) GetPID() uint32 {
	j.RLock()
	defer j.RUnlock()
	return j.proto.GetProcess().GetPID()
}

func (j *Job) SetPID(pid uint32) {
	j.Lock()
	defer j.Unlock()
	if j.proto.GetProcess() == nil {
		j.proto.Process = &daemon.ProcessState{}
	}
	if j.proto.GetProcess().GetInfo() == nil {
		j.proto.Process.Info = &daemon.ProcessInfo{}
	}
	j.proto.Process.PID = pid
	j.proto.Process.Info.PID = pid
}

func (j *Job) GetProto() *daemon.Job {
	j.Lock()
	defer j.Unlock()

	// Get all latest info
	j.proto.Process = j.process()
	j.proto.Log = j.log()

	return &j.proto
}

func (j *Job) GetType() string {
	j.RLock()
	defer j.RUnlock()
	return j.proto.Type
}

func (j *Job) GetProcess() *daemon.ProcessState {
	j.Lock()
	defer j.Unlock()

	return j.process()
}

func (j *Job) SetProcess(process *daemon.ProcessState) {
	j.Lock()
	defer j.Unlock()
	j.proto.Process = process
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
	return j.process().GetInfo().GetIsRunning()
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

func (j *Job) GetCRIUCallback() criu.NotifyCallbackMulti {
	j.RLock()
	defer j.RUnlock()
	return j.criuCallback
}

func (j *Job) AddCRIUCallback(n criu.NotifyCallback) {
	j.Lock()
	defer j.Unlock()
	j.criuCallback.Include(n)
}

///////////////
/// Helpers ///
///////////////

// Functions below don't use locks, so they could be called with locks held.

// WARN: Writes, so call with write lock.
func (j *Job) process() *daemon.ProcessState {
	if j.proto.GetProcess() == nil {
		j.proto.Process = &daemon.ProcessState{}
	}
	if j.proto.GetProcess().GetInfo() == nil {
		j.proto.Process.Info = &daemon.ProcessInfo{}
	}

	j.proto.Process.Info.Status = "halted"
	j.proto.Process.Info.IsRunning = false

	pid := j.proto.GetProcess().GetPID()

	p, err := process.NewProcess(int32(pid))
	if err == nil {
		status, err := p.Status()
		if err == nil {
			j.proto.Process.Info.Status = status[0]
			j.proto.Process.Info.IsRunning = true
		}
	}

	return j.proto.Process
}

func (j *Job) log() string {
	// Check if log file exists
	log := j.proto.Log
	if _, e := os.Stat(log); os.IsNotExist(e) {
		return ""
	}

	return log
}
