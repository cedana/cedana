-- name: ListCheckpoints :many
SELECT * FROM jobs
WHERE type == 0 AND status == 0;

-- name: ListRestores :many
SELECT * FROM jobs
WHERE type == 1 AND status == 0;

-- name: AddJob :one
INSERT INTO jobs (
  id, type, status, data
) VALUES (
  ?, ?, ?, ?
)
RETURNING *;

-- name: UpdateStatus :exec
UPDATE jobs
SET status = ?
WHERE id = ?;

