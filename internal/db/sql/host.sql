-- name: CreateHost :exec
INSERT INTO hosts (ID, MAC, Hostname, OS, Platform, KernelVersion, KernelArch, CPUID)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: UpdateHost :exec
UPDATE hosts SET
    MAC = ?,
    Hostname = ?,
    OS = ?,
    Platform = ?,
    KernelVersion = ?,
    KernelArch = ?,
    CPUID = ?
WHERE ID = ?;

-- name: ListHosts :many
SELECT sqlc.embed(hosts), sqlc.embed(cpus)
FROM hosts
JOIN cpus ON hosts.CPUID = cpus.PhysicalID;

-- name: ListHostsByIDs :many
SELECT sqlc.embed(hosts), sqlc.embed(cpus)
FROM hosts
JOIN cpus ON hosts.CPUID = cpus.PhysicalID
WHERE hosts.ID IN (sqlc.slice('ids'));

-- name: DeleteHost :exec
DELETE FROM hosts WHERE ID = ?;
