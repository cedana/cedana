# Architecture

The design mostly follows what's illustrated below. Below is a simplified runtime view of invoking `cedana dump runc ...`:

<figure><img src="https://github.com/user-attachments/assets/9e6842bd-03d1-4889-b23e-11dcbe7ea25f" alt="Cedana&#x27;s plugin architecture"><figcaption><p>Cedana's plugin architecture</p></figcaption></figure>

1. The subcommand `cedana dump runc ...` is only available if the runc plugin is exporting the `DumpCmd` symbol (check `plugins/runc/main.go`). The runc plugin only sets the _specific_ flags it needs (such as `--id`, `--root`), while the parent cmd handles all the common flags, and sending the request to the daemon.
2. The daemon receives the request, and runs it through a list of adapters, before finally sending it to CRIU. If the request's `type` is `runc`, it will use the `DumpMiddleware` exported by the runc plugin and plug it in the adapter chain. See `internal/cedana/dump.go`:

{% @github-files/github-code-block url="https://github.com/cedana/cedana/blob/3a6a8000e207f7480dbda977eb9181f09b514cc4/internal/cedana/dump.go#L30-L51" %}

3. This way, the runc plugin only implements the specifics of the runc runtime, while the daemon handles the common logic, and invoking CRIU.
4. The same pattern is followed for `dump`, `restore`, `run`, and `manage`.

## Features

Symbols that can be exported by a plugin are well-typed and are defined in `pkg/features/supported.go`. A `feature` implements a convenient method called `IfAvailable(do func(), filter ...string)`, which is the _only_ method you will ever need to access a feature exported by a plugin. An example usage:

{% @github-files/github-code-block url="https://github.com/cedana/cedana/blob/3a6a8000e207f7480dbda977eb9181f09b514cc4/internal/cedana/dump.go#L98-L104" %}

A useful helper command is `cedana features` (alias of `cedana plugin features`), which lists all the features supported by all the plugins. This is useful for debugging, when implementing a new feature, or when you want to know what a plugin supports. Use the `--errors` flag to also output all errors encountered while loading the plugins.

{% hint style="info" %}
See [features](../get-started/features.md) for more info.
{% endhint %}

![Output from cedana plugin features](https://github.com/user-attachments/assets/90578e51-c7f1-44b9-b056-dc1cbdd89785)
