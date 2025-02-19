## Architecture

The design mostly follows what's illustrated below. Below is a simplified runtime view of invoking `cedana dump runc ...`:

![image](https://github.com/user-attachments/assets/9e6842bd-03d1-4889-b23e-11dcbe7ea25f)

1. The subcommand `cedana dump runc ...` is only available if the runc plugin is exporting the `DumpCmd` symbol (check `plugins/runc/main.go`). The runc plugin only sets the _specific_ flags it needs (such as `--id`, `--root`), while the parent cmd handles all the common flags, and sending the request to the daemon.
2. The daemon receives the request, and runs it through a list of adapters, before finally sending it to CRIU. If the request's `type` is `runc`, it will use the `DumpMiddleware` exported by the runc plugin and plug it in the adapter chain. See `internal/server/dump.go`: https://github.com/cedana/cedana/blob/24b37c64d3f51630fc193640d0df92080fed0d4a/internal/server/dump.go#L23-L44
3. This way, the runc plugin only implements the specifics of the runc runtime, while the daemon handles the common logic, and invoking CRIU.
4. The same pattern is followed for `dump`, `restore`, `run`, and `manage`.

### Features
Symbols that can be exported by a plugin are well-typed and are defined in `pkg/features/supported.go`. A `feature` implements a convenient method called `IfAvailable(do func(), filter ...string)`, which is the _only_ method you will ever need to access a feature exported by a plugin. For a usage example, see: https://github.com/cedana/cedana/blob/3e97d8f5241b53aa40307635771724f0ac8b4b54/internal/server/dump.go#L86-L95

A useful helper command is `cedana features` (alias of `cedana plugin features`), which lists all the features supported by all the plugins. This is useful for debugging, when implementing a new feature, or when you want to know what a plugin supports. Use the `--errors` flag to also output all errors encountered while loading the plugins.

See [feature matrix](../features.md) for more info. 

![image](https://github.com/user-attachments/assets/90578e51-c7f1-44b9-b056-dc1cbdd89785)
