-- name: CreateCheckpoint :exec
INSERT INTO checkpoints (ID, JID, Path, Time, Size) VALUES (?, ?, ?, ?, ?);

-- name: UpdateCheckpoint :exec
UPDATE checkpoints SET
    JID = ?,
    Path = ?,
    Time = ?,
    Size = ?
WHERE ID = ?;

-- name: ListCheckpoints :many
SELECT ID, JID, Path, Time, Size FROM checkpoints ORDER BY Time DESC;

-- name: ListCheckpointsByIDs :many
SELECT ID, JID, Path, Time, Size FROM checkpoints WHERE ID in (sqlc.slice('ids'))
ORDER BY Time DESC;

-- name: ListCheckpointsByJIDs :many
SELECT ID, JID, Path, Time, Size FROM checkpoints WHERE JID in (sqlc.slice('jids'))
ORDER BY Time DESC;

-- name: DeleteCheckpoint :exec
DELETE FROM checkpoints WHERE ID = ?;
