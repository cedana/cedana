CREATE TABLE IF NOT EXISTS jobs (
    JID               TEXT PRIMARY KEY,
    Type              TEXT NOT NULL,
    GPUEnabled        INTEGER NOT NULL,
    Log               TEXT NOT NULL,
    Details           BLOB NOT NULL,

-- Contains only a subset of fields from daemon.ProcessState below.
-- that are are reletively fixed throughout the lifetime of a process.
-- Fields such as `OpenFiles`, `OpenConnections` are only captured
-- during a dump and stored together with the dump files.

    PID               INTEGER NOT NULL,
    Cmdline           TEXT NOT NULL,
    StartTime         INTEGER NOT NULL,
    WorkingDir        TEXT NOT NULL,
    Status            TEXT NOT NULL,
    IsRunning         INTEGER NOT NULL,
    HostID            TEXT NOT NULL,
    FOREIGN KEY(HostID) REFERENCES hosts(ID) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS checkpoints (
    ID          TEXT PRIMARY KEY,
    JID         TEXT NOT NULL,
    Path        TEXT NOT NULL,
    Time        timestamp NOT NULL,
    Size        INTEGER NOT NULL,
    FOREIGN KEY(JID) REFERENCES jobs(JID) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS cpus (
    PhysicalID      TEXT PRIMARY KEY,
    VendorID        TEXT NOT NULL,
    Family          TEXT NOT NULL,
    Count           INTEGER NOT NULL,
    MemTotal        INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS hosts (
    ID              TEXT PRIMARY KEY,
    MAC             TEXT NOT NULL,
    Hostname        TEXT NOT NULL,
    OS              TEXT NOT NULL,
    Platform        TEXT NOT NULL,
    KernelVersion   TEXT NOT NULL,
    KernelArch      TEXT NOT NULL,
    CPUID           TEXT NOT NULL,
    FOREIGN KEY(CPUID) REFERENCES cpus(PhysicalID) ON DELETE CASCADE
);
