CREATE TABLE IF NOT EXISTS jobs (
  jid    TEXT PRIMARY KEY,
  state  BLOB
);

CREATE TABLE IF NOT EXISTS checkpoints (
  id          TEXT PRIMARY KEY,
  jid         TEXT NOT NULL,
  path        TEXT NOT NULL,
  time        INTEGER NOT NULL,
  size        INTEGER NOT NULL
);
