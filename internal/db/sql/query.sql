-- name: GetJob :one
SELECT * FROM jobs
WHERE jid = $1 LIMIT 1;

-- name: ListJobs :many
SELECT * FROM jobs
ORDER BY jid;

-- name: CreateJob :one
INSERT INTO jobs (
  jid, data
) VALUES (
  $1, $2
)
RETURNING *;

-- name: UpdateJob :exec
UPDATE jobs
SET data = $2
WHERE jid = $1;

-- name: DeleteJob :exec
DELETE FROM jobs
WHERE jid = $1;
