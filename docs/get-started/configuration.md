# Configuration

Cedana configuration lives in `/etc/cedana/config.json`. You can initialize this file with default values by using the `--init-config` flag (e.g. `sudo cedana daemon start --init-config`). Any configuration in environment variables will override the default values when this file is initialized. You may also merge currently set environment variables into an existing configuration file with the `--merge-config` flag (e.g. `sudo cedana daemon start --merge-config`).

## Environment variables

You may also override the configuration file using environment variables. The environment variables are prefixed with `CEDANA_` and are in uppercase. For example, `Checkpoint.Dir` can be set with `CEDANA_CHECKPOINT_DIR`. Similarly, `Connection.URL` can be set with `CEDANA_CONNECTION_URL`, or its alias `CEDANA_URL`.

## [Config](../../pkg/config/types.go#L10-L36)

Each of the below fields can also be set through an environment variable with the same name, prefixed, and in uppercase. E.g. `Checkpoint.Dir` can be set with `CEDANA_CHECKPOINT_DIR`. The `env_aliases` tag below specifies alternative (alias) environment variable names (comma-separated).

```go
type Config struct {
    // Address to use for incoming/outgoing connections
    Address string `json:"address" key:"address" yaml:"address" mapstructure:"address"`
    // Protocol to use for incoming/outgoing connections (TCP, UNIX, VSOCK)
    Protocol string `json:"protocol" key:"protocol" yaml:"protocol" mapstructure:"protocol"`
    // LogLevel is the default log level used by the server
    LogLevel string `json:"log_level" key:"log_level" yaml:"log_level" mapstructure:"log_level"`
		// Metrics is whether to enable metrics collection and observability
    Metrics Metrics `json:"metrics" key:"metrics" yaml:"metrics" mapstructure:"metrics"`

    // Connection settings
    Connection Connection `json:"connection" key:"connection" yaml:"connection" mapstructure:"connection"`
    // Checkpoint and storage settings
    Checkpoint Checkpoint `json:"checkpoint" key:"checkpoint" yaml:"checkpoint" mapstructure:"checkpoint"`
    // Database details
    DB  DB  `json:"db" key:"db" yaml:"db" mapstructure:"db"`
    // Profiling settings
    Profiling Profiling `json:"profiling" key:"profiling" yaml:"profiling" mapstructure:"profiling"`
    // Client settings
    Client Client `json:"client" key:"client" yaml:"client" mapstructure:"client"`
    // CRIU settings and defaults
    CRIU CRIU `json:"criu" key:"criu" yaml:"criu" mapstructure:"criu"`
    // GPU is settings for the GPU plugin
    GPU GPU `json:"gpu" key:"gpu" yaml:"gpu" mapstructure:"gpu"`
    // Plugin settings
    Plugins Plugins `json:"plugins" key:"plugins" yaml:"plugins" mapstructure:"plugins"`
		// SLURM settings
		Slurm Slurm `json:"slurm" key:"slurm" yaml:"slurm" mapstructure:"slurm"`

    // AWS settings
    AWS AWS `json:"aws" key:"aws" yaml:"aws" mapstructure:"aws"`
}
```

## [CRIU](../../pkg/config/types.go#L81-L86)

```go
type CRIU struct {
    // BinaryPath is a custom path to the CRIU binary
    BinaryPath string `json:"binary_path" key:"binary_path" yaml:"binary_path" mapstructure:"binary_path"`
    // LeaveRunning sets whether to leave the process running after checkpoint
    LeaveRunning bool `json:"leave_running" key:"leave_running" yaml:"leave_running" mapstructure:"leave_running"`
    // ManageCgroups sets the default cgroup C/R mode for CRIU (default, cg_none, props, soft, full, strict, ignore)
    ManageCgroups string `json:"manage_cgroups" key:"manage_cgroups" yaml:"manage_cgroups" mapstructure:"manage_cgroups"`
    // LogLevel sets the default log level for CRIU (2 - errors, 3 - warnings, 4 - debug)
    LogLevel int32 `json:"log_level" key:"log_level" yaml:"log_level" mapstructure:"log_level"`
}
```

## [Checkpoint](../../pkg/config/types.go#L45-L53)

```go
type Checkpoint struct {
		// Dir is the default directory to store checkpoints
		// - "cedana://<path>" for Cedana-managed global storage (recommended)
		// - "s3://<path>" for your S3 storage
		// - "<path>" for node-local storage
    Dir string `json:"dir" key:"dir" yaml:"dir" mapstructure:"dir"`
    // Compression is the default compression algorithm to use for checkpoints
    Compression string `json:"compression" key:"compression" yaml:"compression" mapstructure:"compression"`
    // Stream (for streaming checkpoints) specifies the number of parallel streams to use.
    // 0 means no streaming. n > 0 means n parallel streams (or number of pipes) to use.
    Stream int32 `json:"stream" key:"stream" yaml:"stream" mapstructure:"stream"`
		// The amount of memory streamer is allowed to use (in MB)
		StreamMemoryLimit uint64 `json:"streamer_memory_limit" key:"streamer_memory_limit" yaml:"streamer_memory_limit" mapstructure:"streamer_memory_limit"`
    // Async defers checkpoint compression and upload (in case of remote dir) to the background, and causes
    // checkpoint request to return early.
    Async bool `json:"async" key:"async" yaml:"async" mapstructure:"async"`
}
```

## [Client](../../pkg/config/types.go#L76-L79)

```go
type Client struct {
    // Wait for ready ensures client requests block if the daemon is not up yet
    WaitForReady bool `json:"wait_for_ready" key:"wait_for_ready" yaml:"wait_for_ready" mapstructure:"wait_for_ready" env_aliases:"CEDANA_WAIT_FOR_READY"`
}
```

## [Connection](../../pkg/config/types.go#L38-L43)

```go
type Connection struct {
    // URL is your unique Cedana endpoint URL
    URL string `json:"url" key:"url" yaml:"url" mapstructure:"url" env_aliases:"CEDANA_URL"`
    // AuthToken is your authentication token for the Cedana endpoint
    AuthToken string `json:"auth_token" key:"auth_token" yaml:"auth_token" mapstructure:"auth_token" env_aliases:"CEDANA_AUTH_TOKEN"`
    // ClusterID is the cluster ID (for SLURM/K8s)
    ClusterID string `json:"cluster_id" key:"cluster_id" yaml:"cluster_id" mapstructure:"cluster_id"`
}
```

## [DB](../../pkg/config/types.go#L55-L60)

```go
type DB struct {
    // Remote sets whether to use a remote database
    Remote bool `json:"remote" key:"remote"  yaml:"remote" mapstructure:"remote" env_aliases:"CEDANA_REMOTE"`
    // Path is the local path to the database file. E.g. /tmp/cedana.db
    Path string `json:"path" key:"path" yaml:"path" mapstructure:"path"`
}
```

## [GPU](../../pkg/config/types.go#L88-L93)

```go
type GPU struct {
    // Number of warm GPU controllers to keep in pool
    PoolSize int `json:"pool_size" key:"pool_size" yaml:"pool_size" mapstructure:"pool_size"`
    // LogDir is the directory to write GPU logs to
    LogDir string `json:"log_dir" key:"log_dir" yaml:"log_dir" mapstructure:"log_dir"`
    // SockDir is the directory to use for the GPU sockets
    SockDir string `json:"sock_dir" key:"sock_dir" yaml:"sock_dir" mapstructure:"sock_dir"`
    // ShmSize is the size in bytes of the shared memory segment to use for GPU processes
    ShmSize uint64 `json:"shm_size" key:"shm_size" yaml:"shm_size" mapstructure:"shm_size"`
    // Debug enables debugging capabilities for the GPU plugin. Daemon will try to attach to existing running GPU controllers
    Debug bool `json:"debug" key:"debug" yaml:"debug" mapstructure:"debug"`
}
```

## [Plugins](../../pkg/config/types.go#L95-L100)

```go
type Plugins struct {
    // BinDir is the directory where plugin binaries are stored
    BinDir string `json:"bin_dir" key:"bin_dir" yaml:"bin_dir" mapstructure:"bin_dir"`
    // LibDir is the directory where plugin libraries are stored
    LibDir string `json:"lib_dir" key:"lib_dir" yaml:"lib_dir" mapstructure:"lib_dir" env_aliases:"CEDANA_PLUGINS_LIB_DIR"`
    // Builds is the build versions to list/download for plugins (release, alpha)
    Builds string `json:"builds" key:"build" yaml:"builds" mapstructure:"builds"`
		// LocalSearchPath is a colon-separated list of local directories to search for locally built plugins
		LocalSearchPath string `json:"local_search_path" key:"local_search_path" yaml:"local_search_path" mapstructure:"local_search_path"`
}
```

## [Profiling](../../pkg/config/types.go#L62-L67)

```go
type Profiling struct {
    // Enabled sets whether to enable and show profiling information
    Enabled bool `json:"enabled" key:"enabled" yaml:"enabled" mapstructure:"enabled"`
    // Detailed sets whether to show detailed profiling information
    Detailed bool `json:"detailed" key:"detailed" yaml:"detailed" mapstructure:"detailed"`
    // Precision sets the time precision when printing profiling information (auto, ns, us, ms, s)
    Precision string `json:"precision" key:"precision" yaml:"precision" mapstructure:"precision"`
}
```

## [AWS](../../pkg/config/types.go#L123-L131)

```go
type AWS struct {
    // AccessKeyID is the AWS access key ID
    AccessKeyID string `json:"access_key_id" key:"access_key_id" yaml:"access_key_id" mapstructure:"access_key_id" env_aliases:"AWS_ACCESS_KEY_ID"`
    // SecretAccessKey is the AWS secret access key
    SecretAccessKey string `json:"secret_access_key" key:"secret_access_key" yaml:"secret_access_key" mapstructure:"secret_access_key" env_alias:"AWS_SECRET_ACCESS_KEY"`
    // Region is the AWS region to use
    Region string `json:"region" key:"region" yaml:"region" mapstructure:"region"`
		// Endpoint is a custom AWS endpoint to use (e.g. for S3-compatible storage)
		Endpoint string `json:"endpoint" key:"endpoint" yaml:"endpoint" mapstructure:"endpoint" env_aliases:"AWS_ENDPOINT"`
}
```

## [SLURM](../../pkg/config/types.go#L48-L61)

```go
type Slurm struct {
		// Unprivileged uses an embedded cedana instance for dump instead of the cedana daemon.
		// Requires CAP_SYS_PTRACE,CAP_DAC_READ_SEARCH,CAP_CHECKPOINT_RESTORE on the cedana-slurm binary.
		// Can also be set with CEDANA_SLURM_UNPRIVILEGED=1.
		Unprivileged bool `json:"unprivileged" key:"unprivileged" yaml:"unprivileged" mapstructure:"unprivileged" env_aliases:"CEDANA_SLURM_UNPRIVILEGED"`
		// DBHost is the hostname of the slurmdbd database server
		DBHost string `json:"db_host" key:"db_host" yaml:"db_host" mapstructure:"db_host"`
		// DBSocket is the socket path of the slurmdbd database server (if using UNIX socket connection)
		DBSocket string `json:"db_socket" key:"db_socket" yaml:"db_socket" mapstructure:"db_socket"`
		// DBPort is the port of the slurmdbd database server
		DBPort int `json:"db_port" key:"db_port" yaml:"db_port" mapstructure:"db_port"`
		// DBName is the name of the slurmdbd database to connect to
		DBName string `json:"db_name" key:"db_name" yaml:"db_name" mapstructure:"db_name"`
	}
```
