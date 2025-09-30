# Feature matrix

Run `cedana features` (shorthand for `cedana plugin features`) to see the features currently supported by each plugin.

```
FEATURE                CLOUD-HYPERVISOR  RUNC  CONTAINERD  CRIO  KATA  K8S
Dump command           •                 ✔     ✔           •     •     •
Restore command        •                 ✔     ✔           •     •     •
Run command            •                 ✔     ✔           •     •     •
Manage command         •                 ✔     ✔           •     •     •
Query command          •                 ✔     •           •     •     ✔
Helper command(s)      •                 •     •           •     •     ✔

Dump middleware        •                 ✔     ✔           ✔     •     •
Restore middleware     •                 ✔     •           ✔     •     •
Dump VM middleware     ✔                 •     •           •     ✔     •
DumpVM handler         ✔                 •     •           •     •     •
Restore VM middleware  ✔                 •     •           •     ✔     •
RestoreVM handler      ✔                 •     •           •     •     •

Run handler            •                 ✔     ✔           ✔     •     •
Run middleware         •                 ✔     ✔           ✔     •     •
Manage handler         •                 ✔     ✔           •     •     •
Custom kill signal     •                 ✔     •           •     ✔     •

GPU interception       •                 ✔     ✔           •     •     •

Checkpoint inspect     •                 •     •           •     •     •
Checkpoint decode      •                 •     •           •     •     •
Checkpoint encode      •                 •     •           •     •     •

Query handler          •                 ✔     •           •     •     ✔

Health checks          •                 ✔     ✔           •     ✔     •

✔ = implemented, • = unimplemented, — = not installed, ✖ = incompatible
Not showing external plugins: criu, criu/cuda, gpu, streamer, k8s/runtime-shim

```

When developing a new plugin, or adding a feature to a plugin, it's helpful to see more information on any compatibility issues. Use `cedana features --errors` to see any incompatibility errors.

Check out the guide on [writing plugins](../developer-guides/writing_plugins.md) for more information.

Check out the [CLI reference](../references/cli/cedana_plugin.md) for all plugin-related subcommands.
