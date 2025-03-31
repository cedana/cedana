# Architecture

The design mostly follows what's illustrated below. Below is a simplified runtime view of invoking `cedana dump runc ...`:

![image](https://github.com/user-attachments/assets/9e6842bd-03d1-4889-b23e-11dcbe7ea25f)

1. The subcommand `cedana dump runc ...` is only available if the runc plugin is exporting the `DumpCmd` symbol (check `plugins/runc/main.go`). The runc plugin only sets the _specific_ flags it needs (such as `--id`, `--root`), while the parent cmd handles all the common flags, and sending the request to the daemon.
2. The daemon receives the request, and runs it through a list of adapters, before finally sending it to CRIU. If the request's `type` is `runc`, it will use the `DumpMiddleware` exported by the runc plugin and plug it in the adapter chain. See `internal/server/dump.go`:
```go
// The order below is the order followed before executing 
 // the final handler (criu.Dump). 
  
 middleware := types.Middleware[types.Dump]{ 
 	defaults.FillMissingDumpDefaults, 
 	validation.ValidateDumpRequest, 
 	filesystem.PrepareDumpDir(config.Global.Checkpoints.Compression), 
  
 	pluginDumpMiddleware, // middleware from plugins 
  
 	// Process state-dependent adapters 
 	process.FillProcessStateForDump, 
 	process.DetectShellJobForDump, 
 	process.DetectIOUringForDump, 
 	process.CloseCommonFilesForDump, 
 	process.AddExternalFilesForDump, 
 	network.DetectNetworkOptionsForDump, 
  
 	criu.CheckOptsForDump, 
 } 
  
 dump := criu.Dump.With(middleware...)
```
3. This way, the runc plugin only implements the specifics of the runc runtime, while the daemon handles the common logic, and invoking CRIU.
4. The same pattern is followed for `dump`, `restore`, `run`, and `manage`.

## Features

Symbols that can be exported by a plugin are well-typed and are defined in `pkg/features/supported.go`. A `feature` implements a convenient method called `IfAvailable(do func(), filter ...string)`, which is the _only_ method you will ever need to access a feature exported by a plugin. An example usage:
```go
 err = features.DumpMiddleware.IfAvailable(func( 
 	name string, 
 	pluginMiddleware types.Middleware[types.Dump], 
 ) error { 
 	middleware = append(middleware, pluginMiddleware...) 
 	return nil 
 }, t) 
 if err != nil { 
 	return nil, status.Error(codes.Unimplemented, err.Error()) 
 } 
```

A useful helper command is `cedana features` (alias of `cedana plugin features`), which lists all the features supported by all the plugins. This is useful for debugging, when implementing a new feature, or when you want to know what a plugin supports. Use the `--errors` flag to also output all errors encountered while loading the plugins.

See [feature matrix](../get-started/features.md) for more info.

![image](https://github.com/user-attachments/assets/90578e51-c7f1-44b9-b056-dc1cbdd89785)
