# Configuration

Cedana configuration lives in your home directory, specifically in `~/.cedana/config.json`. This file is automatically created the first time you use a Cedana command. You can also create it manually.

You may also override the configuration using environment variables. The environment variables are prefixed with `CEDANA_` and are in uppercase. For example, `Metrics.ASR` can be set with `CEDANA_METRICS_ASR`. Similarly, `Connection.URL` can be set with `CEDANA_CONNECTION_URL`, or its alias `CEDANA_URL`.

## [Config](../../pkg/config/types.go#L10-L36)

Each of the below fields can also be set through an environment variable with the same name, prefixed, and in uppercase. E.g. `Metrics.ASR` can be set with `CEDANA_METRICS_ASR`. The `env_aliases` tag below specifies alternative (alias) environment variable names (comma-separated).

```go
type Config struct {
    // Address to use for incoming/outgoing connections
    Address string `json:"address" key:"address" yaml:"address" mapstructure:"address"`
    // Protocol to use for incoming/outgoing connections (TCP, UNIX, VSOCK)
    Protocol string `json:"protocol" key:"protocol" yaml:"protocol" mapstructure:"protocol"`
    // LogLevel is the default log level used by the server
    LogLevel string `json:"log_level" key:"log_level" yaml:"log_level" mapstructure:"log_level"`

    // Connection settings
    Connection Connection `json:"connection" key:"connection" yaml:"connection" mapstructure:"connection"`
    // Checkpoint and storage settings
    Checkpoint Checkpoint `json:"checkpoint" key:"checkpoint" yaml:"checkpoint" mapstructure:"checkpoint"`
    // Database details
    DB  DB  `json:"db" key:"db" yaml:"db" mapstructure:"db"`
    // Profiling settings
    Profiling Profiling `json:"profiling" key:"profiling" yaml:"profiling" mapstructure:"profiling"`
    // Metrics settings
    Metrics Metrics `json:"metrics" key:"metrics" yaml:"metrics" mapstructure:"metrics"`
    // Client settings
    Client Client `json:"client" key:"client" yaml:"client" mapstructure:"client"`
    // CRIU settings and defaults
    CRIU CRIU `json:"criu" key:"criu" yaml:"criu" mapstructure:"criu"`
    // GPU is settings for the GPU plugin
    GPU GPU `json:"gpu" key:"gpu" yaml:"gpu" mapstructure:"gpu"`
    // Plugin settings
    Plugins Plugins `json:"plugins" key:"plugins" yaml:"plugins" mapstructure:"plugins"`
}
```

## [CRIU](../../pkg/config/types.go#L81-L86)

```go
type CRIU struct {
    // BinaryPath is a custom path to the CRIU binary
    BinaryPath string `json:"binary_path" key:"binary_path" yaml:"binary_path" mapstructure:"binary_path"`
    // LeaveRunning sets whether to leave the process running after checkpoint
    LeaveRunning bool `json:"leave_running" key:"leave_running" yaml:"leave_running" mapstructure:"leave_running"`
}
```

## [Checkpoint](../../pkg/config/types.go#L45-L53)

```go
type Checkpoint struct {
    // Dir is the default directory to store checkpoints
    Dir string `json:"dir" key:"dir" yaml:"dir" mapstructure:"dir"`
    // Compression is the default compression algorithm to use for checkpoints
    Compression string `json:"compression" key:"compression" yaml:"compression" mapstructure:"compression"`
    // Stream (for streaming checkpoints) specifies the number of parallel streams to use.
    // 0 means no streaming. n > 0 means n parallel streams (or number of pipes) to use.
    Stream int32 `json:"stream" key:"stream" yaml:"stream" mapstructure:"stream"`
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
    // LogDir is the directory to write GPU logs to. By default, logs are written to daemon's stdout
    LogDir string `json:"log_dir" key:"log_dir" yaml:"log_dir" mapstructure:"log_dir"`
    // MultiprocessType is the type of multiprocess support to use (IPC, NCCL)                                                 
    MultiprocessType string `json:"multiprocess_type" key:"multiprocess_type" yaml:"multiprocess_type" mapstructure:"multiprocess_type”`
}
```

## [Metrics](../../pkg/config/types.go#L69-L74)

```go
type Metrics struct {
    // ASR sets whether to enable ASR metrics
    ASR bool `json:"asr" key:"asr" yaml:"asr" mapstructure:"asr"`
    // Otel sets whether to enable OpenTelemetry metrics
    Otel bool `json:"otel" key:"otel" yaml:"otel" mapstructure:"otel" env_aliases:"CEDANA_OTEL_ENABLED"`
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
}
```

## [Profiling](../../pkg/config/types.go#L62-L67)

```go
type Profiling struct {
    // Enabled sets whether to enable and show profiling information
    Enabled bool `json:"enabled" key:"enabled" yaml:"enabled" mapstructure:"enabled"`
    // Precision sets the time precision when printing profiling information (auto, ns, us, ms, s)
    Precision string `json:"precision" key:"precision" yaml:"precision" mapstructure:"precision"`
}
```
