-- name: CreateJob :exec
INSERT INTO jobs (JID, Type, GPUEnabled, Log, Details, PID, Cmdline, StartTime, WorkingDir, Status, IsRunning, HostID)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: UpdateJob :exec
UPDATE jobs SET
    Type = ?,
    GPUEnabled = ?,
    Log = ?,
    Details = ?,
    PID = ?,
    Cmdline = ?,
    StartTime = ?,
    WorkingDir = ?,
    Status = ?,
    IsRunning = ?,
    HostID = ?
WHERE JID = ?;

-- name: ListJobs :many
SELECT sqlc.embed(jobs), sqlc.embed(hosts), sqlc.embed(cpus)
FROM jobs
JOIN hosts ON hosts.ID = jobs.HostID
JOIN cpus ON hosts.CPUID = cpus.PhysicalID;

-- name: ListJobsByJIDs :many
SELECT sqlc.embed(jobs), sqlc.embed(hosts), sqlc.embed(cpus)
FROM jobs
JOIN hosts ON hosts.ID = jobs.HostID
JOIN cpus ON hosts.CPUID = cpus.PhysicalID
WHERE jobs.JID IN (sqlc.slice('ids'));

-- name: DeleteJob :exec
DELETE FROM jobs WHERE JID = ?;
