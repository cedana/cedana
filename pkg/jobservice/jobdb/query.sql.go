// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.25.0
// source: query.sql

package jobdb

import (
	"context"
)

const addJob = `-- name: AddJob :one
INSERT INTO jobs (
  id, type, status, data
) VALUES (
  ?, ?, ?, ?
)
RETURNING id, type, status, data
`

type AddJobParams struct {
	ID     string
	Type   int64
	Status int64
	Data   string
}

func (q *Queries) AddJob(ctx context.Context, arg AddJobParams) (Job, error) {
	row := q.db.QueryRowContext(ctx, addJob,
		arg.ID,
		arg.Type,
		arg.Status,
		arg.Data,
	)
	var i Job
	err := row.Scan(
		&i.ID,
		&i.Type,
		&i.Status,
		&i.Data,
	)
	return i, err
}

const listCheckpoints = `-- name: ListCheckpoints :many
SELECT id, type, status, data FROM jobs
WHERE type == 0 AND status == 0
`

func (q *Queries) ListCheckpoints(ctx context.Context) ([]Job, error) {
	rows, err := q.db.QueryContext(ctx, listCheckpoints)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Job
	for rows.Next() {
		var i Job
		if err := rows.Scan(
			&i.ID,
			&i.Type,
			&i.Status,
			&i.Data,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const listRestores = `-- name: ListRestores :many
SELECT id, type, status, data FROM jobs
WHERE type == 1 AND status == 0
`

func (q *Queries) ListRestores(ctx context.Context) ([]Job, error) {
	rows, err := q.db.QueryContext(ctx, listRestores)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Job
	for rows.Next() {
		var i Job
		if err := rows.Scan(
			&i.ID,
			&i.Type,
			&i.Status,
			&i.Data,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const updateStatus = `-- name: UpdateStatus :exec
UPDATE jobs
SET status = ?
WHERE id = ?
`

type UpdateStatusParams struct {
	Status int64
	ID     string
}

func (q *Queries) UpdateStatus(ctx context.Context, arg UpdateStatusParams) error {
	_, err := q.db.ExecContext(ctx, updateStatus, arg.Status, arg.ID)
	return err
}