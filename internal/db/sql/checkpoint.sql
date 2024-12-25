--------------------------------
------ Checkpoint Queries ------
--------------------------------

-- name: CreateCheckpoint :one
INSERT INTO checkpoints (id, jid, path, time, size) VALUES (?, ?, ?, ?, ?)
RETURNING id, jid, path, time, size;

-- name: GetCheckpoint :one
SELECT id, jid, path, time, size FROM checkpoints WHERE id = ?;

-- name: ListCheckpoints :many
SELECT id, jid, path, time, size FROM checkpoints WHERE jid = ?;

-- name: GetLatestCheckpoint :one
SELECT id, jid, path, time, size FROM checkpoints WHERE jid = ? ORDER BY time DESC LIMIT 1;

-- name: DeleteCheckpoint :exec
DELETE FROM checkpoints WHERE id = ?;
