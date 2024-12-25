package db

// Local implementation of DB using SQL

import (
	"context"
	dbsql "database/sql"
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/db/sql"
	_ "github.com/mattn/go-sqlite3"
	json "google.golang.org/protobuf/encoding/protojson"
)

type LocalDB struct {
	queries *sql.Queries
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
	if _, err := db.ExecContext(ctx, sql.Ddl); err != nil {
		return nil, err
	}

	return &LocalDB{
		queries: sql.New(db),
	}, nil
}

///////////
/// Job ///
///////////

func (db *LocalDB) GetJob(ctx context.Context, jid string) (*daemon.Job, error) {
	dbJob, err := db.queries.GetJob(ctx, jid)
	if err != nil {
		return nil, err
	}

	bytes := dbJob.State

	// unmarsal the bytes into a Job struct
	job := daemon.Job{}
	err = json.Unmarshal(bytes, &job)
	if err != nil {
		return nil, err
	}

	return &job, nil
}

func (db *LocalDB) PutJob(ctx context.Context, jid string, job *daemon.Job) error {
	// marshal the Job struct into bytes
	bytes, err := json.Marshal(job)
	if err != nil {
		return err
	}
	if _, err := db.queries.GetJob(ctx, jid); err == nil {
		return db.queries.UpdateJob(ctx, sql.UpdateJobParams{
			Jid:   jid,
			State: bytes,
		})
	} else {
		_, err := db.queries.CreateJob(ctx, sql.CreateJobParams{
			Jid:   jid,
			State: bytes,
		})
		return err
	}
}

func (db *LocalDB) ListJobs(ctx context.Context) ([]*daemon.Job, error) {
	dbJobs, err := db.queries.ListJobs(ctx)

	jobs := []*daemon.Job{}
	for _, dbJob := range dbJobs {
		// unmarsal the bytes into a Job struct
		job := daemon.Job{}
		err = json.Unmarshal(dbJob.State, &job)
		if err != nil {
			return nil, err
		}

		jobs = append(jobs, &job)
	}

	return jobs, nil
}

func (db *LocalDB) DeleteJob(ctx context.Context, jid string) error {
	return db.queries.DeleteJob(ctx, jid)
}

//////////////////
/// Checkpoint ///
//////////////////

func (db *LocalDB) GetCheckpoint(ctx context.Context, id string) (*daemon.Checkpoint, error) {
	dbCheckpoint, err := db.queries.GetCheckpoint(ctx, id)
	if err != nil {
		return nil, err
	}

	return fromDBCheckpoint(dbCheckpoint), nil
}

func (db *LocalDB) CreateCheckpoint(ctx context.Context, checkpoint *daemon.Checkpoint) error {
	_, err := db.queries.CreateCheckpoint(ctx, sql.CreateCheckpointParams{
		ID:   checkpoint.ID,
		Jid:  checkpoint.JID,
		Path: checkpoint.Path,
		Time: checkpoint.Time,
		Size: checkpoint.Size,
	})
	return err
}

func (db *LocalDB) ListCheckpoints(ctx context.Context, jid string) ([]*daemon.Checkpoint, error) {
	dbCheckpoints, err := db.queries.ListCheckpoints(ctx, jid)
	if err != nil {
		return nil, err
	}

	checkpoints := []*daemon.Checkpoint{}
	for _, dbCheckpoint := range dbCheckpoints {
		checkpoints = append(checkpoints, fromDBCheckpoint(dbCheckpoint))
	}

	return checkpoints, nil
}

func (db *LocalDB) GetLatestCheckpoint(ctx context.Context, jid string) (*daemon.Checkpoint, error) {
	dbCheckpoint, err := db.queries.GetLatestCheckpoint(ctx, jid)
	if err != nil {
		return nil, err
	}

	return fromDBCheckpoint(dbCheckpoint), nil
}

func (db *LocalDB) DeleteCheckpoint(ctx context.Context, id string) error {
	return db.queries.DeleteCheckpoint(ctx, id)
}

///////////////
/// Helpers ///
///////////////

func fromDBCheckpoint(dbCheckpoint sql.Checkpoint) *daemon.Checkpoint {
	return &daemon.Checkpoint{
		ID:   dbCheckpoint.ID,
		JID:  dbCheckpoint.Jid,
		Path: dbCheckpoint.Path,
		Time: dbCheckpoint.Time,
		Size: dbCheckpoint.Size,
	}
}
