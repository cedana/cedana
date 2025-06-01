-- name: CreateGPUController :exec
INSERT INTO gpu_controllers (ID, Address, PID, AttachedPID, FreezeType)
VALUES (?, ?, ?, ?, ?);

-- name: UpdateGPUController :exec
UPDATE gpu_controllers SET
    Address = ?,
    PID = ?,
    AttachedPID = ?,
    FreezeType = ?
WHERE ID = ?;

-- name: ListGPUControllers :many
SELECT * FROM gpu_controllers;

-- name: ListGPUControllersByIDs :many
SELECT * FROM gpu_controllers
WHERE ID IN (sqlc.slice('ids'));

-- name: DeleteGPUController :exec
DELETE FROM gpu_controllers
WHERE ID = ?;
