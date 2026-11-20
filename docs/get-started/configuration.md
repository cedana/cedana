# Configuration

Cedana configuration lives in `/etc/cedana/config.json`. You can initialize this file with default values by using the `--init-config` flag (e.g. `sudo cedana daemon start --init-config`). Any configuration in environment variables will override the default values when this file is initialized. You may also merge currently set environment variables into an existing configuration file with the `--merge-config` flag (e.g. `sudo cedana daemon start --merge-config`).

## Environment variables

You may also override the configuration file using environment variables. The environment variables are prefixed with `CEDANA_` and are in uppercase. For example, `Checkpoint.Dir` can be set with `CEDANA_CHECKPOINT_DIR`. Similarly, `Connection.URL` can be set with `CEDANA_CONNECTION_URL`, or its alias `CEDANA_URL`.

Each of the below fields can also be set through an environment variable with the same name, prefixed, and in uppercase. E.g. `Checkpoint.Dir` can be set with `CEDANA_CHECKPOINT_DIR`. The `env_aliases` tag below specifies alternative (alias) environment variable names (comma-separated).

## [Config](https://github.com/cedana/cedana/blob/fbcdf137610ca971cfaef7ef533846f29a32dd5b/pkg/config/types.go#L10-L47)

{% @github-files/github-code-block url="https://github.com/cedana/cedana/blob/fbcdf137610ca971cfaef7ef533846f29a32dd5b/pkg/config/types.go#L10-L47" %}

## [CRIU](https://github.com/cedana/cedana/blob/fbcdf137610ca971cfaef7ef533846f29a32dd5b/pkg/config/types.go#L123-L136)

{% @github-files/github-code-block url="https://github.com/cedana/cedana/blob/fbcdf137610ca971cfaef7ef533846f29a32dd5b/pkg/config/types.go#L123-L136" %}

## [Checkpoint](https://github.com/cedana/cedana/blob/fbcdf137610ca971cfaef7ef533846f29a32dd5b/pkg/config/types.go#L82-L98)

{% @github-files/github-code-block url="https://github.com/cedana/cedana/blob/fbcdf137610ca971cfaef7ef533846f29a32dd5b/pkg/config/types.go#L82-L98" %}

## [Client](https://github.com/cedana/cedana/blob/fbcdf137610ca971cfaef7ef533846f29a32dd5b/pkg/config/types.go#L118-L121)

{% @github-files/github-code-block url="https://github.com/cedana/cedana/blob/fbcdf137610ca971cfaef7ef533846f29a32dd5b/pkg/config/types.go#L118-L121" %}

## [Connection](https://github.com/cedana/cedana/blob/fbcdf137610ca971cfaef7ef533846f29a32dd5b/pkg/config/types.go#L73-L80)

{% @github-files/github-code-block url="https://github.com/cedana/cedana/blob/fbcdf137610ca971cfaef7ef533846f29a32dd5b/pkg/config/types.go#L73-L80" %}

## [DB](https://github.com/cedana/cedana/blob/fbcdf137610ca971cfaef7ef533846f29a32dd5b/pkg/config/types.go#L100-L105)

{% @github-files/github-code-block url="https://github.com/cedana/cedana/blob/fbcdf137610ca971cfaef7ef533846f29a32dd5b/pkg/config/types.go#L100-L105" %}

## [GPU](https://github.com/cedana/cedana/blob/fbcdf137610ca971cfaef7ef533846f29a32dd5b/pkg/config/types.go#L138-L182)

{% @github-files/github-code-block url="https://github.com/cedana/cedana/blob/fbcdf137610ca971cfaef7ef533846f29a32dd5b/pkg/config/types.go#L138-L182" %}

## [Plugins](https://github.com/cedana/cedana/blob/fbcdf137610ca971cfaef7ef533846f29a32dd5b/pkg/config/types.go#L184-L193)

{% @github-files/github-code-block url="https://github.com/cedana/cedana/blob/fbcdf137610ca971cfaef7ef533846f29a32dd5b/pkg/config/types.go#L184-L193" %}

## [Profiling](https://github.com/cedana/cedana/blob/fbcdf137610ca971cfaef7ef533846f29a32dd5b/pkg/config/types.go#L107-L116)

{% @github-files/github-code-block url="https://github.com/cedana/cedana/blob/fbcdf137610ca971cfaef7ef533846f29a32dd5b/pkg/config/types.go#L107-L116" %}

## [AWS](https://github.com/cedana/cedana/blob/fbcdf137610ca971cfaef7ef533846f29a32dd5b/pkg/config/types.go#L195-L206)

{% @github-files/github-code-block url="https://github.com/cedana/cedana/blob/fbcdf137610ca971cfaef7ef533846f29a32dd5b/pkg/config/types.go#L195-L206" %}

## [SLURM](https://github.com/cedana/cedana/blob/fbcdf137610ca971cfaef7ef533846f29a32dd5b/pkg/config/types.go#L49-L62)

{% @github-files/github-code-block url="https://github.com/cedana/cedana/blob/fbcdf137610ca971cfaef7ef533846f29a32dd5b/pkg/config/types.go#L49-L62" %}

## [CSX](https://github.com/cedana/cedana/blob/fbcdf137610ca971cfaef7ef533846f29a32dd5b/pkg/config/types.go#L208-L211)

{% @github-files/github-code-block url="https://github.com/cedana/cedana/blob/fbcdf137610ca971cfaef7ef533846f29a32dd5b/pkg/config/types.go#L208-L211" %}

## [K8s](https://github.com/cedana/cedana/blob/fbcdf137610ca971cfaef7ef533846f29a32dd5b/pkg/config/types.go#L64-L71)

{% @github-files/github-code-block url="https://github.com/cedana/cedana/blob/fbcdf137610ca971cfaef7ef533846f29a32dd5b/pkg/config/types.go#L64-L71" %}
