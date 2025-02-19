## Feature matrix

Run `cedana features` (shorthand for `cedana plugin features`) to see the features currently supported by each plugin.

```
FEATURE             RUNC  CONTAINERD  CRIO  K8S
Dump command        ✔     ✔           •     •
Restore command     ✔     ✔           •     •
Run command         ✔     ✔           •     •
Manage command      ✔     ✔           •     •
Query command       ✔     •           •     ✔
Helper command(s)   •     •           •     ✔

Dump middleware     ✔     ✔           ✔     •
Restore middleware  ✔     •           ✔     •

Run handler         ✔     ✔           ✔     •
Run middleware      ✔     ✔           ✔     •
Manage handler      ✔     ✔           •     •
Custom kill signal  ✔     •           •     •

GPU interception    ✔     ✔           •     •

Checkpoint inspect  •     •           •     •
Checkpoint decode   •     •           •     •
Checkpoint encode   •     •           •     •

Query handler       ✔     •           •     ✔

Health checks       ✔     ✔           •     •

✔ = implemented, • = unimplemented, — = not installed, ✖ = incompatible
Not showing external plugins: criu, gpu, streamer
```

When developing a new plugin, or adding a feature to a plugin, it's helpful to see if there are any compatibility errors. Use `cedana features --errors` to see any incompatibility errors.

Check out the guide on [writing plugins](dev/writing_plugins.md) for more information.

Check out the [CLI reference](cli/cedana_plugin.md) for all plugin-related subcommands.
