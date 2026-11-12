# Configuration

Cedana configuration lives in `/etc/cedana/config.json`. You can initialize this file with default values by using the `--init-config` flag (e.g. `sudo cedana daemon start --init-config`). Any configuration in environment variables will override the default values when this file is initialized. You may also merge currently set environment variables into an existing configuration file with the `--merge-config` flag (e.g. `sudo cedana daemon start --merge-config`).

## Environment variables

You may also override the configuration file using environment variables. The environment variables are prefixed with `CEDANA_` and are in uppercase. For example, `Checkpoint.Dir` can be set with `CEDANA_CHECKPOINT_DIR`. Similarly, `Connection.URL` can be set with `CEDANA_CONNECTION_URL`, or its alias `CEDANA_URL`.

Each of the below fields can also be set through an environment variable with the same name, prefixed, and in uppercase. E.g. `Checkpoint.Dir` can be set with `CEDANA_CHECKPOINT_DIR`. The `env_aliases` tag below specifies alternative (alias) environment variable names (comma-separated).

## [Config](https://github.com/cedana/cedana/blob/d7e70263c563d6b24367f105be4fdc4ecd13aeb1/pkg/config/types.go#L10-L43)

{% @github-files/github-code-block url="https://github.com/cedana/cedana/blob/d7e70263c563d6b24367f105be4fdc4ecd13aeb1/pkg/config/types.go#L10-L43" %}

## [CRIU](https://github.com/cedana/cedana/blob/d7e70263c563d6b24367f105be4fdc4ecd13aeb1/pkg/config/types.go#L108-L117)

{% @github-files/github-code-block url="https://github.com/cedana/cedana/blob/d7e70263c563d6b24367f105be4fdc4ecd13aeb1/pkg/config/types.go#L108-L117" %}

## [Checkpoint](https://github.com/cedana/cedana/blob/d7e70263c563d6b24367f105be4fdc4ecd13aeb1/pkg/config/types.go#L69-L85)

{% @github-files/github-code-block url="https://github.com/cedana/cedana/blob/d7e70263c563d6b24367f105be4fdc4ecd13aeb1/pkg/config/types.go#L69-L85" %}

## [Client](https://github.com/cedana/cedana/blob/d7e70263c563d6b24367f105be4fdc4ecd13aeb1/pkg/config/types.go#L103-L106)

{% @github-files/github-code-block url="https://github.com/cedana/cedana/blob/d7e70263c563d6b24367f105be4fdc4ecd13aeb1/pkg/config/types.go#L103-L106" %}

## [Connection](https://github.com/cedana/cedana/blob/d7e70263c563d6b24367f105be4fdc4ecd13aeb1/pkg/config/types.go#L60-L67)

{% @github-files/github-code-block url="https://github.com/cedana/cedana/blob/d7e70263c563d6b24367f105be4fdc4ecd13aeb1/pkg/config/types.go#L60-L67" %}

## [DB](https://github.com/cedana/cedana/blob/d7e70263c563d6b24367f105be4fdc4ecd13aeb1/pkg/config/types.go#L87-L92)

{% @github-files/github-code-block url="https://github.com/cedana/cedana/blob/d7e70263c563d6b24367f105be4fdc4ecd13aeb1/pkg/config/types.go#L87-L92" %}

## [GPU](https://github.com/cedana/cedana/blob/d7e70263c563d6b24367f105be4fdc4ecd13aeb1/pkg/config/types.go#L119-L132)

{% @github-files/github-code-block url="https://github.com/cedana/cedana/blob/d7e70263c563d6b24367f105be4fdc4ecd13aeb1/pkg/config/types.go#L119-L132" %}

## [Plugins](https://github.com/cedana/cedana/blob/d7e70263c563d6b24367f105be4fdc4ecd13aeb1/pkg/config/types.go#L134-L143)

{% @github-files/github-code-block url="https://github.com/cedana/cedana/blob/d7e70263c563d6b24367f105be4fdc4ecd13aeb1/pkg/config/types.go#L134-L143" %}

## [Profiling](https://github.com/cedana/cedana/blob/d7e70263c563d6b24367f105be4fdc4ecd13aeb1/pkg/config/types.go#L94-L101)

{% @github-files/github-code-block url="https://github.com/cedana/cedana/blob/d7e70263c563d6b24367f105be4fdc4ecd13aeb1/pkg/config/types.go#L94-L101" %}

## [AWS](https://github.com/cedana/cedana/blob/d7e70263c563d6b24367f105be4fdc4ecd13aeb1/pkg/config/types.go#L145-L154)

{% @github-files/github-code-block url="https://github.com/cedana/cedana/blob/d7e70263c563d6b24367f105be4fdc4ecd13aeb1/pkg/config/types.go#L145-L154" %}

## [SLURM](https://github.com/cedana/cedana/blob/d7e70263c563d6b24367f105be4fdc4ecd13aeb1/pkg/config/types.go#L45-L58)

{% @github-files/github-code-block url="https://github.com/cedana/cedana/blob/d7e70263c563d6b24367f105be4fdc4ecd13aeb1/pkg/config/types.go#L45-L58" %}

## [CSX](https://github.com/cedana/cedana/blob/e9dd65ade9c32a0a1ebef4f731aa653a1abcd4b4/pkg/config/types.go#L164-L167)

{% @github-files/github-code-block url="https://github.com/cedana/cedana/blob/e9dd65ade9c32a0a1ebef4f731aa653a1abcd4b4/pkg/config/types.go#L164-L167" %}

## [K8s](https://github.com/cedana/cedana/blob/da28c80c56426ad74d66fa78df7caa7a9f065a07/pkg/config/types.go#L64-L68)

{% @github-files/github-code-block url="https://github.com/cedana/cedana/blob/da28c80c56426ad74d66fa78df7caa7a9f065a07/pkg/config/types.go#L64-L68" %}
