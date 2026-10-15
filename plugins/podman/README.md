# Cedana Podman Plugin

Adds podman support for Cedana.

## Usage

See [checkpoint/restore podman](../../docs/guides/podman/cr.md).

## Dependencies

- **crun plugin**: libcedana-crun.so (or **runc plugin**: libcedana-runc.so, if podman uses runc)
- **oci-runtime plugin**: cedana-oci-runtime, see [cedana-oci-runtime](https://github.com/cedana/cedana-oci-runtime)
