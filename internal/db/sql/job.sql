-- name: CreateJob :exec
INSERT INTO jobs (ID, Type, GPUEnabled, Log, Details, PID, Cmdline, StartTime, WorkingDir, Status, IsRunning, HostID)
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
WHERE ID = ?;

-- name: ListJobs :many
SELECT sqlc.embed(jobs), sqlc.embed(hosts)
FROM jobs
JOIN hosts ON hosts.ID = jobs.HostID;

-- name: ListJobsByIDs :many
SELECT sqlc.embed(jobs), sqlc.embed(hosts)
FROM jobs
JOIN hosts ON hosts.ID = jobs.HostID
WHERE jobs.ID IN (sqlc.slice('ids'));

-- name: ListJobsByHostIDs :many
SELECT sqlc.embed(jobs), sqlc.embed(hosts)
FROM jobs
JOIN hosts ON hosts.ID = jobs.HostID
WHERE jobs.HostID IN (sqlc.slice('host_ids'));

-- name: DeleteJob :exec
DELETE FROM jobs WHERE ID = ?;
