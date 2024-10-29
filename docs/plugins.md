# Cedana Plugins

Plugins are used to extend the support of C/R to various container runtimes.

Each plugin here is a separate go module. The plugin can choose to implement the following interfaces:
- **cobra.Command**: The plugin can implement the `cobra.Command` interface to add new subcommands to the Cedana CLI. By default, the daemon only implements the commands for simple process C/R.
- **Middleware**: WIP
