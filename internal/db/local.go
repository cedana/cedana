package db

// Local implementation of DB using SQL

import (
	"context"
	dbsql "database/sql"
	"fmt"
	"time"

	_ "embed"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/db/sql"
	_ "github.com/mattn/go-sqlite3"
	json "google.golang.org/protobuf/encoding/protojson"
)

//go:embed sql/schema.sql
var Ddl string

type LocalDB struct {
	queries *sql.Queries
	UnimplementedDB
}

func NewLocalDB(ctx context.Context, path string) (*LocalDB, error) {
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

	return &LocalDB{
		queries: sql.New(db),
	}, nil
}

///////////
/// Job ///
///////////

func (db *LocalDB) PutJob(ctx context.Context, job *daemon.Job) error {
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
			ID:         job.JID,
			Type:       job.Type,
			Gpuenabled: gpuEnabled,
			Log:        job.Log,
			Details:    detailBytes,
			Pid:        int64(job.GetState().GetPID()),
			Cmdline:    job.GetState().GetCmdline(),
			Starttime:  int64(job.GetState().GetStartTime()),
			Workingdir: job.GetState().GetWorkingDir(),
			Status:     job.GetState().GetStatus(),
			Isrunning:  isRunning,
			Hostid:     job.GetState().GetHost().GetID(),
		})
	}

	return db.queries.CreateJob(ctx, sql.CreateJobParams{
		ID:         job.JID,
		Type:       job.Type,
		Gpuenabled: gpuEnabled,
		Log:        job.Log,
		Details:    detailBytes,
		Pid:        int64(job.GetState().GetPID()),
		Cmdline:    job.GetState().GetCmdline(),
		Starttime:  int64(job.GetState().GetStartTime()),
		Workingdir: job.GetState().GetWorkingDir(),
		Status:     job.GetState().GetStatus(),
		Isrunning:  isRunning,
		Hostid:     job.GetState().GetHost().GetID(),
	})
}

func (db *LocalDB) ListJobs(ctx context.Context, jids ...string) ([]*daemon.Job, error) {
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

func (db *LocalDB) ListJobsByHostIDs(ctx context.Context, hostIDs ...string) ([]*daemon.Job, error) {
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

func (db *LocalDB) DeleteJob(ctx context.Context, jid string) error {
	return db.queries.DeleteJob(ctx, jid)
}

////////////
/// Host ///
////////////

func (db *LocalDB) PutHost(ctx context.Context, host *daemon.Host) error {
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

func (db *LocalDB) ListHosts(ctx context.Context, ids ...string) ([]*daemon.Host, error) {
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

func (db *LocalDB) DeleteHost(ctx context.Context, id string) error {
	return db.queries.DeleteHost(ctx, id)
}

//////////////////
/// Checkpoint ///
//////////////////

func (db *LocalDB) PutCheckpoint(ctx context.Context, checkpoint *daemon.Checkpoint) error {
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

func (db *LocalDB) ListCheckpoints(ctx context.Context, ids ...string) ([]*daemon.Checkpoint, error) {
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

func (db *LocalDB) ListCheckpointsByJIDs(ctx context.Context, jids ...string) ([]*daemon.Checkpoint, error) {
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

func (db *LocalDB) DeleteCheckpoint(ctx context.Context, id string) error {
	return db.queries.DeleteCheckpoint(ctx, id)
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
		JID:     job.ID,
		Type:    job.Type,
		Log:     job.Log,
		Details: &details,
		State: &daemon.ProcessState{
			PID:        uint32(job.Pid),
			Cmdline:    job.Cmdline,
			StartTime:  uint64(job.Starttime),
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
