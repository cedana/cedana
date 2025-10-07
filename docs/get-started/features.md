# Features

Run `cedana features` (shorthand for `cedana plugin features`) to see the features currently supported by each plugin.

```
FEATURE                   CLOUD-HYPERVISOR  RUNC  CONTAINERD  CRIO  KATA  STORAGE/CEDANA  STORAGE/S3  STORAGE/GCS  K8S
Dump command              •                 ✔     ✔           •     •     •               •           •            •
Restore command           •                 ✔     ✔           •     •     •               •           •            •
Run command               •                 ✔     ✔           •     •     •               •           •            •
Manage command            •                 ✔     ✔           •     •     •               •           •            •
Freeze command            •                 ✔     ✔           •     •     •               •           •            •
Unfreeze command          •                 ✔     ✔           •     •     •               •           •            •
Query command             •                 ✔     •           •     •     •               •           •            ✔
Helper command(s)         •                 •     •           •     •     •               •           •            ✔

Dump middleware           •                 ✔     ✔           ✔     •     •               •           •            •
Restore middleware        •                 ✔     •           ✔     •     •               •           •            •
Dump VM middleware        ✔                 •     •           •     ✔     •               •           •            •
Dump VM handler           ✔                 •     •           •     •     •               •           •            •
Restore VM middleware     ✔                 •     •           •     ✔     •               •           •            •
Restore VM handler        ✔                 •     •           •     •     •               •           •            •
Freeze handler            •                 ✔     ✔           •     •     •               •           •            •
Unfreeze handler          •                 ✔     ✔           •     •     •               •           •            •

Run handler               •                 ✔     ✔           ✔     •     •               •           •            •
Run handler (daemonless)  •                 ✔     •           •     •     •               •           •            •
Run middleware            •                 ✔     ✔           ✔     •     •               •           •            •
Manage handler            •                 ✔     ✔           •     •     •               •           •            •
Custom kill signal        •                 ✔     ✔           •     ✔     •               •           •            •
Custom cleanup            •                 ✔     ✔           •     •     •               •           •            •
Custom reaper             •                 ✔     •           •     •     •               •           •            •

GPU interception          •                 ✔     ✔           •     •     •               •           •            •

Checkpoint storage        •                 •     •           •     •     ✔               ✔           •            •

Query handler             •                 ✔     •           •     •     •               •           •            ✔

Health checks             •                 ✔     ✔           •     ✔     ✔               ✔           •            •

✔ = implemented, • = unimplemented, — = not installed, ✖ = incompatible
Not showing external plugins: criu, criu/cuda, gpu, streamer, k8s/runtime-shim
```

When developing a new plugin, or adding a feature to a plugin, it's helpful to see more information on any compatibility issues. Use `cedana features --errors` to see any incompatibility errors.

{% hint style="info" %}
Check out the guide on [writing plugins](../developer-guides/writing_plugins.md) for more information.
{% endhint %}

{% hint style="info" %}
Check out the [CLI reference](../references/cli/cedana_plugin.md) for all plugin-related subcommands.
{% endhint %}
