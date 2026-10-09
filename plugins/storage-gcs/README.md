# GCS Storage Plugin

Adds GCS remote storage support for storing checkpoints in user Google Cloud Storage buckets,
at paths `gs://<bucket>/<key>`.

Authentication is configured with `GCS.CredentialsMode`:

- `ambient` (the default) uses the SDK's default chain: Workload Identity on GKE, the
  metadata server, `GOOGLE_APPLICATION_CREDENTIALS`, or gcloud's application default credentials.
- `serviceAccount` uses the key in `GCS.ServiceAccountKey`, a key file path or the key's JSON.

`GCS.EmulatorHost` (or `STORAGE_EMULATOR_HOST`) points the plugin at a GCS emulator such as
fake-gcs-server, without credentials. It is for testing.

The writer implements `Checksummer`: its checksum is the CRC32C of the bytes it sent. GCS computes
a whole-object CRC32C of every object; the writer reads it back after the upload and logs a
difference. The storage implements `PathChecksummer` from the object's attributes.
