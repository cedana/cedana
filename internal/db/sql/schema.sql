CREATE TABLE IF NOT EXISTS hosts (
    ID              TEXT PRIMARY KEY,
    MAC             TEXT NOT NULL CHECK(MAC != ''),
    Hostname        TEXT NOT NULL CHECK(Hostname != ''),
    OS              TEXT NOT NULL CHECK(OS != ''),
    Platform        TEXT NOT NULL CHECK(Platform != ''),
    KernelVersion   TEXT NOT NULL CHECK(KernelVersion != ''),
    KernelArch      TEXT NOT NULL CHECK(KernelArch != ''),
    CPUPhysicalID   TEXT NOT NULL,
    CPUVendorID     TEXT NOT NULL,
    CPUFamily       TEXT NOT NULL,
    CPUCount        INTEGER NOT NULL,
    MemTotal        INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS jobs (
    JID                TEXT PRIMARY KEY,
    Type              TEXT NOT NULL CHECK(Type != ''),
    GPUEnabled        INTEGER NOT NULL,
    Log               TEXT NOT NULL,
    Details           BLOB NOT NULL, -- XXX: Storing as blob breaks forward compatibility.

-- Contains only a subset of fields from protobuf daemon.ProcessState below.
-- that are are reletively fixed throughout the lifetime of a process.
-- Fields such as `OpenFiles`, `OpenConnections` are only captured
-- during a dump and stored together with the dump files.

    PID               INTEGER NOT NULL CHECK(PID > 0),
    Cmdline           TEXT NOT NULL,
    StartTime         TIMESTAMP NOT NULL,
    WorkingDir        TEXT NOT NULL,
    Status            TEXT NOT NULL,
    IsRunning         INTEGER NOT NULL,
    HostID            TEXT NOT NULL CHECK(HostID != ''),
    FOREIGN KEY(HostID) REFERENCES hosts(ID) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS checkpoints (
    ID          TEXT PRIMARY KEY,
    JID         TEXT NOT NULL CHECK(JID != ''),
    Path        TEXT NOT NULL,
    Time        TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    Size        INTEGER NOT NULL,
    FOREIGN KEY(JID) REFERENCES jobs(JID) ON DELETE CASCADE
);
