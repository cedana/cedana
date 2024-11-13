package db

// Remote implementation of the DB, that talks to the propogator.

import (
	"context"
	"database/sql"

	"github.com/cedana/cedana/pkg/db/models"
	"github.com/cedana/cedana/pkg/db/sqlite"
)

const (
	SQLITE_DB_PATH = "/tmp/sqlite_cedana.db"
)

type LocalDB struct {
	queries *sqlite.Queries
}

func NewLocalDB(ctx context.Context) DB {
	db, err := sql.Open("sqlite3", SQLITE_DB_PATH)
	if err != nil {
		return nil
	}

	// create sqlite tables
	if _, err := db.ExecContext(ctx, sqlite.Ddl); err != nil {
		return nil
	}

	return &LocalDB{
		queries: sqlite.New(db),
	}
}

/////////////
// Getters //
/////////////

func (db *LocalDB) GetJob(ctx context.Context, jid []byte) (models.Job, error) {
	return db.queries.GetJob(ctx, jid)
}

/////////////
// Setters //
/////////////

func (db *LocalDB) PutJob(ctx context.Context, jid []byte, state []byte) error {
	_, err := db.queries.CreateJob(ctx, sqlite.CreateJobParams{
		Jid:   jid,
		State: state,
	})
	if err != nil {
		err = db.queries.UpdateJob(ctx, sqlite.UpdateJobParams{
			Jid:   jid,
			State: state,
		})
		return err
	}

	return err
}

/////////////
// Listers //
/////////////

func (db *LocalDB) ListJobs(ctx context.Context) ([]models.Job, error) {
	return db.queries.ListJobs(ctx)
}
