package sqlite_db

// Local implementation of sqlite DB

import (
	"database/sql"
	"context"
)

const (
	SQLITE_DB_PATH		  = "/tmp/sqlite_cedana.db"
)

type LocalDB struct {
	queries *Queries
}

func NewLocalDB(ctx context.Context) DB {

	db, err := sql.Open("sqlite3", SQLITE_DB_PATH)
	if err != nil {
		return nil
	}

	// create sqlite tables
	if _, err := db.ExecContext(ctx, Ddl); err != nil {
		return nil
	}

	return &LocalDB{
		queries: New(db),
	}
}

/////////////
// Getters //
/////////////

func (db *LocalDB) Get(ctx context.Context, jid []byte) (Job, error) {
	return db.queries.GetJob(ctx, jid)
}

/////////////
// Setters //
/////////////

func (db *LocalDB) Put(ctx context.Context, jid []byte, state []byte) error {
	_, err := db.queries.CreateJob(ctx, CreateJobParams{
		Jid: jid,
		State:  state,
	})
	if err != nil {
		err = db.queries.UpdateJob(ctx, UpdateJobParams{
			Jid: jid,
			State:  state,
		})
		return err
	}

	return err
}

/////////////
// Listers //
/////////////

func (db *LocalDB) List(ctx context.Context) ([]Job, error) {
	return db.queries.ListJobs(ctx)
}