## cedana completion bash

Generate the autocompletion script for bash

### Synopsis

Generate the autocompletion script for the bash shell.

This script depends on the 'bash-completion' package.
If it is not installed already, you can install it via your OS's package manager.

To load completions in your current shell session:

	source <(cedana completion bash)

To load completions for every new session, execute once:

#### Linux:

	cedana completion bash > /etc/bash_completion.d/cedana

#### macOS:

	cedana completion bash > $(brew --prefix)/etc/bash_completion.d/cedana

You will need to start a new shell for this setup to take effect.


```
cedana completion bash
```

### Options

```
  -h, --help              help for bash
      --no-descriptions   disable completion descriptions
```

### Options inherited from parent commands

```
      --address string      address to use (host:port for TCP, path for UNIX, cid:port for VSOCK)
      --config string       one-time config JSON string (merge with existing config)
      --config-dir string   custom config directory
      --profiling           enable profiling/show profiling data
      --protocol string     protocol to use (TCP, UNIX, VSOCK)
```

### SEE ALSO

* [cedana completion](cedana_completion.md)	 - Generate the autocompletion script for the specified shell

