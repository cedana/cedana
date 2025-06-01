package db

// Local implementation of DB using SQL

import (
	"context"
	dbsql "database/sql"
	"fmt"
	"strings"
	"time"

	_ "embed"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/db/sql"
	"github.com/cedana/cedana/pkg/utils"
	_ "github.com/mattn/go-sqlite3"
	json "google.golang.org/protobuf/encoding/protojson"
)

//go:embed sql/schema.sql
var Ddl string

type SqliteDB struct {
	queries *sql.Queries
	UnimplementedDB
}

func NewSqliteDB(ctx context.Context, path string) (*SqliteDB, error) {
	if path == "" {
		return nil, fmt.Errorf("please provide a DB path")
	}

	db, err := dbsql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}

	// create sqlite tables
	if _, err := db.ExecContext(ctx, Ddl); err != nil {
		return nil, err
	}

	return &SqliteDB{
		queries: sql.New(db),
	}, nil
}

///////////
/// Job ///
///////////

func (db *SqliteDB) PutJob(ctx context.Context, job *daemon.Job) error {
	// marshal the Job struct into bytes
	detailBytes, err := json.Marshal(job.Details)
	if err != nil {
		return err
	}
	var gpuEnabled, isRunning int64
	if job.GetState().GetGPUEnabled() {
		gpuEnabled = 1
	}
	if job.GetState().GetIsRunning() {
		isRunning = 1
	}
	if list, _ := db.queries.ListJobsByIDs(ctx, []string{job.JID}); len(list) > 0 {
		return db.queries.UpdateJob(ctx, sql.UpdateJobParams{
			Jid:        job.JID,
			Type:       job.Type,
			Gpuenabled: gpuEnabled,
			Log:        job.Log,
			Details:    detailBytes,
			Pid:        int64(job.GetState().GetPID()),
			Cmdline:    job.GetState().GetCmdline(),
			Starttime:  time.Unix(0, int64(job.GetState().GetStartTime())*int64(time.Millisecond)),
			Workingdir: job.GetState().GetWorkingDir(),
			Status:     job.GetState().GetStatus(),
			Isrunning:  isRunning,
			Hostid:     job.GetState().GetHost().GetID(),
			Uids:       strings.Join(utils.Uint32ToStringSlice(job.GetState().GetUIDs()), ","),
			Gids:       strings.Join(utils.Uint32ToStringSlice(job.GetState().GetGIDs()), ","),
			Groups:     strings.Join(utils.Uint32ToStringSlice(job.GetState().GetGroups()), ","),
		})
	}

	return db.queries.CreateJob(ctx, sql.CreateJobParams{
		Jid:        job.JID,
		Type:       job.Type,
		Gpuenabled: gpuEnabled,
		Log:        job.Log,
		Details:    detailBytes,
		Pid:        int64(job.GetState().GetPID()),
		Cmdline:    job.GetState().GetCmdline(),
		Starttime:  time.Unix(0, int64(job.GetState().GetStartTime())*int64(time.Millisecond)),
		Workingdir: job.GetState().GetWorkingDir(),
		Status:     job.GetState().GetStatus(),
		Isrunning:  isRunning,
		Hostid:     job.GetState().GetHost().GetID(),
		Uids:       strings.Join(utils.Uint32ToStringSlice(job.GetState().GetUIDs()), ","),
		Gids:       strings.Join(utils.Uint32ToStringSlice(job.GetState().GetGIDs()), ","),
		Groups:     strings.Join(utils.Uint32ToStringSlice(job.GetState().GetGroups()), ","),
	})
}

func (db *SqliteDB) ListJobs(ctx context.Context, jids ...string) ([]*daemon.Job, error) {
	if len(jids) > 0 {
		rows, err := db.queries.ListJobsByIDs(ctx, jids)
		if err != nil {
			return nil, err
		}

		jobs := []*daemon.Job{}
		for _, row := range rows {
			job, err := fromDBJobRow(row)
			if err != nil {
				return nil, err
			}

			jobs = append(jobs, job)
		}

		return jobs, nil
	} else {
		rows, err := db.queries.ListJobs(ctx)
		if err != nil {
			return nil, err
		}

		jobs := []*daemon.Job{}
		for _, row := range rows {
			job, err := fromDBJobRow(row)
			if err != nil {
				return nil, err
			}

			jobs = append(jobs, job)
		}

		return jobs, nil
	}
}

func (db *SqliteDB) ListJobsByHostIDs(ctx context.Context, hostIDs ...string) ([]*daemon.Job, error) {
	rows, err := db.queries.ListJobsByHostIDs(ctx, hostIDs)
	if err != nil {
		return nil, err
	}

	jobs := []*daemon.Job{}
	for _, row := range rows {
		job, err := fromDBJobRow(row)
		if err != nil {
			return nil, err
		}

		jobs = append(jobs, job)
	}

	return jobs, nil
}

func (db *SqliteDB) DeleteJob(ctx context.Context, jid string) error {
	return db.queries.DeleteJob(ctx, jid)
}

////////////
/// Host ///
////////////

func (db *SqliteDB) PutHost(ctx context.Context, host *daemon.Host) error {
	if host.CPU == nil || host.Memory == nil {
		return fmt.Errorf("CPU or Memory info missing")
	}

	if list, _ := db.queries.ListHostsByIDs(ctx, []string{host.ID}); len(list) > 0 {
		return db.queries.UpdateHost(ctx, sql.UpdateHostParams{
			ID:            host.ID,
			Mac:           host.MAC,
			Hostname:      host.Hostname,
			Os:            host.OS,
			Platform:      host.Platform,
			Kernelversion: host.KernelVersion,
			Kernelarch:    host.KernelArch,
			Cpuphysicalid: host.CPU.PhysicalID,
			Cpuvendorid:   host.CPU.VendorID,
			Cpufamily:     host.CPU.Family,
			Cpucount:      int64(host.CPU.Count),
			Memtotal:      int64(host.Memory.Total),
		})
	}

	return db.queries.CreateHost(ctx, sql.CreateHostParams{
		ID:            host.ID,
		Mac:           host.MAC,
		Hostname:      host.Hostname,
		Os:            host.OS,
		Platform:      host.Platform,
		Kernelversion: host.KernelVersion,
		Kernelarch:    host.KernelArch,
		Cpuphysicalid: host.CPU.PhysicalID,
		Cpuvendorid:   host.CPU.VendorID,
		Cpufamily:     host.CPU.Family,
		Cpucount:      int64(host.CPU.Count),
		Memtotal:      int64(host.Memory.Total),
	})
}

func (db *SqliteDB) ListHosts(ctx context.Context, ids ...string) ([]*daemon.Host, error) {
	hosts := []*daemon.Host{}
	if len(ids) == 0 {
		dbHosts, err := db.queries.ListHosts(ctx)
		if err != nil {
			return nil, err
		}

		for _, dbHost := range dbHosts {
			host, err := fromDBHost(&dbHost)
			if err != nil {
				return nil, err
			}
			hosts = append(hosts, host)
		}

		return hosts, nil
	} else {
		dbHosts, err := db.queries.ListHostsByIDs(ctx, ids)
		if err != nil {
			return nil, err
		}
		for _, dbHost := range dbHosts {
			host, err := fromDBHost(&dbHost)
			if err != nil {
				return nil, err
			}
			hosts = append(hosts, host)
		}

		return hosts, nil
	}
}

func (db *SqliteDB) DeleteHost(ctx context.Context, id string) error {
	return db.queries.DeleteHost(ctx, id)
}

//////////////////
/// Checkpoint ///
//////////////////

func (db *SqliteDB) PutCheckpoint(ctx context.Context, checkpoint *daemon.Checkpoint) error {
	if list, _ := db.queries.ListCheckpointsByIDs(ctx, []string{checkpoint.ID}); len(list) > 0 {
		return db.queries.UpdateCheckpoint(ctx, sql.UpdateCheckpointParams{
			ID:   checkpoint.ID,
			Jid:  checkpoint.JID,
			Path: checkpoint.Path,
			Time: time.Unix(0, checkpoint.Time*int64(time.Millisecond)),
			Size: checkpoint.Size,
		})
	}

	return db.queries.CreateCheckpoint(ctx, sql.CreateCheckpointParams{
		ID:   checkpoint.ID,
		Jid:  checkpoint.JID,
		Path: checkpoint.Path,
		Time: time.Unix(0, checkpoint.Time*int64(time.Millisecond)),
		Size: checkpoint.Size,
	})
}

func (db *SqliteDB) ListCheckpoints(ctx context.Context, ids ...string) ([]*daemon.Checkpoint, error) {
	var dbCheckpoints []sql.Checkpoint
	var err error

	if len(ids) == 0 {
		dbCheckpoints, err = db.queries.ListCheckpoints(ctx)
	} else {
		dbCheckpoints, err = db.queries.ListCheckpointsByIDs(ctx, ids)
	}

	if err != nil {
		return nil, err
	}

	checkpoints := []*daemon.Checkpoint{}
	for _, dbCheckpoint := range dbCheckpoints {
		checkpoints = append(checkpoints, fromDBCheckpoint(&dbCheckpoint))
	}

	return checkpoints, nil
}

func (db *SqliteDB) ListCheckpointsByJIDs(ctx context.Context, jids ...string) ([]*daemon.Checkpoint, error) {
	var dbCheckpoints []sql.Checkpoint
	var err error

	if len(jids) == 0 {
		dbCheckpoints, err = db.queries.ListCheckpoints(ctx)
	} else {
		dbCheckpoints, err = db.queries.ListCheckpointsByJIDs(ctx, jids)
	}

	if err != nil {
		return nil, err
	}

	checkpoints := []*daemon.Checkpoint{}
	for _, dbCheckpoint := range dbCheckpoints {
		checkpoints = append(checkpoints, fromDBCheckpoint(&dbCheckpoint))
	}

	return checkpoints, nil
}

func (db *SqliteDB) DeleteCheckpoint(ctx context.Context, id string) error {
	return db.queries.DeleteCheckpoint(ctx, id)
}

/////////////////////
/// GPU controller ///
/////////////////////

func (db *SqliteDB) PutGPUController(ctx context.Context, controller *GPUController) error {
	if list, _ := db.queries.ListGPUControllersByIDs(ctx, []string{controller.ID}); len(list) > 0 {
		return db.queries.UpdateGPUController(ctx, sql.UpdateGPUControllerParams{
			ID:          controller.ID,
			Address:     controller.Address,
			Pid:         int64(controller.PID),
			Attachedpid: int64(controller.AttachedPID),
		})
	}

	return db.queries.CreateGPUController(ctx, sql.CreateGPUControllerParams{
		ID:          controller.ID,
		Address:     controller.Address,
		Pid:         int64(controller.PID),
		Attachedpid: int64(controller.AttachedPID),
	})
}

func (db *SqliteDB) ListGPUControllers(ctx context.Context, ids ...string) ([]*GPUController, error) {
	var dbControllers []sql.GpuController
	var err error

	if len(ids) == 0 {
		dbControllers, err = db.queries.ListGPUControllers(ctx)
	} else {
		dbControllers, err = db.queries.ListGPUControllersByIDs(ctx, ids)
	}

	if err != nil {
		return nil, err
	}

	controllers := []*GPUController{}
	for _, dbController := range dbControllers {
		controllers = append(controllers, &GPUController{
			ID:          dbController.ID,
			Address:     dbController.Address,
			PID:         uint32(dbController.Pid),
			AttachedPID: uint32(dbController.Attachedpid),
		})
	}

	return controllers, nil
}

func (db *SqliteDB) DeleteGPUController(ctx context.Context, id string) error {
	return db.queries.DeleteGPUController(ctx, id)
}

///////////////
/// Helpers ///
///////////////

func fromDBCheckpoint(dbCheckpoint *sql.Checkpoint) *daemon.Checkpoint {
	return &daemon.Checkpoint{
		ID:   dbCheckpoint.ID,
		JID:  dbCheckpoint.Jid,
		Path: dbCheckpoint.Path,
		Time: dbCheckpoint.Time.UnixMilli(),
		Size: dbCheckpoint.Size,
	}
}

func fromDBJobRow(row any) (*daemon.Job, error) {
	var job sql.Job
	var host sql.Host

	switch row := row.(type) {
	case sql.ListJobsRow:
		job = row.Job
		host = row.Host
	case sql.ListJobsByIDsRow:
		job = row.Job
		host = row.Host
	default:
		return nil, fmt.Errorf("unknown row type")
	}

	var details daemon.Details
	err := json.Unmarshal(job.Details, &details)
	if err != nil {
		return nil, err
	}

	return &daemon.Job{
		JID:     job.Jid,
		Type:    job.Type,
		Log:     job.Log,
		Details: &details,
		State: &daemon.ProcessState{
			PID:        uint32(job.Pid),
			Cmdline:    job.Cmdline,
			StartTime:  uint64(job.Starttime.UnixMilli()),
			WorkingDir: job.Workingdir,
			Status:     job.Status,
			IsRunning:  job.Isrunning > 0,
			GPUEnabled: job.Gpuenabled > 0,
			Host: &daemon.Host{
				ID:            host.ID,
				MAC:           host.Mac,
				Hostname:      host.Hostname,
				OS:            host.Os,
				Platform:      host.Platform,
				KernelVersion: host.Kernelversion,
				KernelArch:    host.Kernelarch,
				CPU: &daemon.CPU{
					PhysicalID: host.Cpuphysicalid,
					VendorID:   host.Cpuvendorid,
					Family:     host.Cpufamily,
					Count:      int32(host.Cpucount),
				},
				Memory: &daemon.Memory{
					Total: uint64(host.Memtotal),
				},
			},
			UIDs:   utils.StringToUint32Slice(strings.Split(job.Uids, ",")),
			GIDs:   utils.StringToUint32Slice(strings.Split(job.Gids, ",")),
			Groups: utils.StringToUint32Slice(strings.Split(job.Groups, ",")),
		},
	}, nil
}

func fromDBHost(host *sql.Host) (*daemon.Host, error) {
	return &daemon.Host{
		ID:            host.ID,
		MAC:           host.Mac,
		Hostname:      host.Hostname,
		OS:            host.Os,
		Platform:      host.Platform,
		KernelVersion: host.Kernelversion,
		KernelArch:    host.Kernelarch,
		CPU: &daemon.CPU{
			PhysicalID: host.Cpuphysicalid,
			VendorID:   host.Cpuvendorid,
			Family:     host.Cpufamily,
			Count:      int32(host.Cpucount),
		},
		Memory: &daemon.Memory{
			Total: uint64(host.Memtotal),
		},
	}, nil
}
