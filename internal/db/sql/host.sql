-- name: CreateHost :exec
INSERT INTO hosts (ID, MAC, Hostname, OS, Platform, KernelVersion, KernelArch, CPUPhysicalID, CPUVendorID, CPUFamily, CPUCount, MemTotal)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: UpdateHost :exec
UPDATE hosts SET
    MAC = ?,
    Hostname = ?,
    OS = ?,
    Platform = ?,
    KernelVersion = ?,
    KernelArch = ?,
    CPUPhysicalID = ?,
    CPUVendorID = ?,
    CPUFamily = ?,
    CPUCount = ?,
    MemTotal = ?
WHERE ID = ?;

-- name: ListHosts :many
SELECT * FROM hosts;

-- name: ListHostsByIDs :many
SELECT * FROM hosts WHERE ID in (sqlc.slice('ids'));

-- name: DeleteHost :exec
DELETE FROM hosts WHERE ID = ?;
