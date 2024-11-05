package db

// Local implementation of DB using SQL

import (
	"context"
	dbsql "database/sql"
	"encoding/json"

	"github.com/cedana/cedana/internal/db/sql"
	"github.com/cedana/cedana/pkg/api/daemon"
	_ "github.com/mattn/go-sqlite3"
)

const path = "/tmp/cedana.db"

type LocalDB struct {
	queries *sql.Queries
}

func NewLocalDB(ctx context.Context) (DB, error) {
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

/////////////
// Getters //
/////////////

func (db *LocalDB) GetJob(ctx context.Context, jid string) (*daemon.Job, error) {
	dbJob, err := db.queries.GetJob(ctx, jid)
	if err != nil {
		return nil, err
	}

	bytes := dbJob.Data

	// unmarsal the bytes into a Job struct
	job := daemon.Job{}
	err = json.Unmarshal(bytes, &job)
	if err != nil {
		return nil, err
	}

	return &job, nil
}

/////////////
// Setters //
/////////////

func (db *LocalDB) PutJob(ctx context.Context, jid string, job *daemon.Job) error {
	// marshal the Job struct into bytes
	bytes, err := json.Marshal(job)
	if err != nil {
		return err
	}

	if _, err := db.queries.GetJob(ctx, jid); err == nil {
		db.queries.DeleteJob(ctx, jid)
	}

	_, err = db.queries.CreateJob(ctx, sql.CreateJobParams{
		Jid:  jid,
		Data: bytes,
	})

	return err
}

/////////////
// Listers //
/////////////

func (db *LocalDB) ListJobs(ctx context.Context) ([]*daemon.Job, error) {
	dbJobs, err := db.queries.ListJobs(ctx)

	jobs := make([]*daemon.Job, len(dbJobs))
	for i, dbJob := range dbJobs {
		bytes := dbJob.Data

		// unmarsal the bytes into a Job struct
		job := daemon.Job{}
		err = json.Unmarshal(bytes, &job)
		if err != nil {
			return nil, err
		}

		jobs[i] = &job
	}

	return jobs, nil
}
