-- name: CreateCPU :exec
INSERT INTO cpus (PhysicalID, VendorID, Family, Count, MemTotal)
VALUES (?, ?, ?, ?, ?);

-- name: UpdateCPU :exec
UPDATE cpus SET
    VendorID = ?,
    Family = ?,
    Count = ?,
    MemTotal = ?
WHERE PhysicalID = ?;

-- name: ListCPUs :many
SELECT * FROM cpus;

-- name: ListCPUsByIDs :many
SELECT * FROM cpus WHERE PhysicalID in (sqlc.slice('ids'));

-- name: DeleteCPU :exec
DELETE FROM cpus WHERE PhysicalID = ?;
