--------------------------------
---------- Job Queries ---------
--------------------------------

-- name: CreateJob :one
INSERT INTO jobs (jid, state) VALUES (?, ?)
RETURNING jid, state;

-- name: GetJob :one
SELECT jid, state FROM jobs WHERE jid = ?;

-- name: UpdateJob :exec
UPDATE jobs SET state = ? WHERE jid = ?;

-- name: DeleteJob :exec
DELETE FROM jobs WHERE jid = ?;

-- name: ListJobs :many
SELECT jid, state FROM jobs;
