-- name: CreateJob :one
INSERT INTO jobs (jid, data) VALUES (?, ?)
RETURNING jid, data;

-- name: GetJob :one
SELECT jid, data FROM jobs WHERE jid = ?;

-- name: UpdateJob :exec
UPDATE jobs SET data = ? WHERE jid = ?;

-- name: DeleteJob :exec
DELETE FROM jobs WHERE jid = ?;

-- name: ListJobs :many
SELECT jid, data FROM jobs;
