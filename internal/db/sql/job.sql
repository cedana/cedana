-- name: CreateJob :exec
INSERT INTO jobs (JID, Type, GPUEnabled, Log, Details, PID, Cmdline, StartTime, WorkingDir, Status, IsRunning, HostID, UIDs, GIDs, Groups)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

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
    HostID = ?,
    UIDs = ?,
    GIDs = ?,
    Groups = ?
WHERE JID = ?;

-- name: ListJobs :many
SELECT sqlc.embed(jobs), sqlc.embed(hosts)
FROM jobs
JOIN hosts ON hosts.ID = jobs.HostID;

-- name: ListJobsByIDs :many
SELECT sqlc.embed(jobs), sqlc.embed(hosts)
FROM jobs
JOIN hosts ON hosts.ID = jobs.HostID
WHERE jobs.JID IN (sqlc.slice('ids'));

-- name: ListJobsByHostIDs :many
SELECT sqlc.embed(jobs), sqlc.embed(hosts)
FROM jobs
JOIN hosts ON hosts.ID = jobs.HostID
WHERE jobs.HostID IN (sqlc.slice('host_ids'));

-- name: DeleteJob :exec
DELETE FROM jobs WHERE JID = ?;
