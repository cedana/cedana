package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/cedana/cedana/pkg/utils"
	"github.com/spf13/viper"
)

const (
	DIR_PATH   = "/etc/cedana"
	FILE_NAME  = "config"
	FILE_TYPE  = "json"
	DIR_PERM   = 0o755
	FILE_PERM  = 0o644
	ENV_PREFIX = "CEDANA"

	// NOTE: `localhost` server inside kubernetes may or may not work
	// based on firewall and network configuration, it would only work
	// on local system, hence for serving on TCP default is 0.0.0.0
	DEFAULT_TCP_ADDR   = "0.0.0.0:8080"
	DEFAULT_SOCK_ADDR  = "/run/cedana.sock"
	DEFAULT_SOCK_PERMS = 0o666

	DEFAULT_PROTOCOL            = "unix"
	DEFAULT_LOG_LEVEL           = "info"
	DEFAULT_LOG_LEVEL_NO_SERVER = "warn"

	DEFAULT_LOCAL_STORAGE_LIMIT_GB = 8

	DEFAULT_CHECKPOINT_COMPRESSION = "none"
	DEFAULT_CHECKPOINT_DIR         = "/tmp"
	DEFAULT_CHECKPOINT_STREAMS     = 0
	DEFAULT_CHECKPOINT_ASYNC       = false

	DEFAULT_DB_REMOTE = false
	DEFAULT_DB_PATH   = "/tmp/cedana.db"

	DEFAULT_PROFILING_ENABLED   = true
	DEFAULT_PROFILING_DETAILED  = true
	DEFAULT_PROFILING_PRECISION = "auto"

	DEFAULT_CONNECTION_URL        = "https://sandbox.cedana.ai"
	DEFAULT_CONNECTION_AUTH_TOKEN = ""

	DEFAULT_METRICS = false

	DEFAULT_CLIENT_WAIT_FOR_READY = false

	DEFAULT_GPU_POOL_SIZE                = 0
	DEFAULT_GPU_LOG_DIR                  = "/tmp"
	DEFAULT_GPU_SOCK_DIR                 = "/tmp"
	DEFAULT_GPU_SHM_SIZE                 = 8 * utils.GIBIBYTE
	DEFAULT_GPU_DEBUG                    = false
	DEFAULT_GPU_SKIP_NVIDIA_RUNTIME_HOOK = false

	DEFAULT_CRIU_LEAVE_RUNNING  = false
	DEFAULT_CRIU_MANAGE_CGROUPS = "ignore"

	DEFAULT_PLUGINS_LIB_DIR = "/usr/local/lib"
	DEFAULT_PLUGINS_BIN_DIR = "/usr/local/bin"
	DEFAULT_PLUGINS_BUILDS  = "release"
)

// The default global config. This will get overwritten
// by the config file or env vars during startup, if they exist.
var Global Config = Config{
	// NOTE: Don't specify default address here as it depends on default protocol.
	// Use above constants for default address for each protocol.
	Protocol:          DEFAULT_PROTOCOL,
	LogLevel:          DEFAULT_LOG_LEVEL,
	LogLevelNoServer:  DEFAULT_LOG_LEVEL_NO_SERVER,
	Metrics:           DEFAULT_METRICS,
	LocalStorageLimit: DEFAULT_LOCAL_STORAGE_LIMIT_GB,
	Checkpoint: Checkpoint{
		Dir:         DEFAULT_CHECKPOINT_DIR,
		Compression: DEFAULT_CHECKPOINT_COMPRESSION,
		Streams:     DEFAULT_CHECKPOINT_STREAMS,
		Async:       DEFAULT_CHECKPOINT_ASYNC,
	},
	DB: DB{
		Remote: DEFAULT_DB_REMOTE,
		Path:   DEFAULT_DB_PATH,
	},
	Profiling: Profiling{
		Enabled:   DEFAULT_PROFILING_ENABLED,
		Detailed:  DEFAULT_PROFILING_DETAILED,
		Precision: DEFAULT_PROFILING_PRECISION,
	},
	Connection: Connection{
		URL:       DEFAULT_CONNECTION_URL,
		AuthToken: DEFAULT_CONNECTION_AUTH_TOKEN,
	},
	Client: Client{
		WaitForReady: DEFAULT_CLIENT_WAIT_FOR_READY,
	},
	GPU: GPU{
		PoolSize:              DEFAULT_GPU_POOL_SIZE,
		LogDir:                DEFAULT_GPU_LOG_DIR,
		SockDir:               DEFAULT_GPU_SOCK_DIR,
		ShmSize:               DEFAULT_GPU_SHM_SIZE,
		Debug:                 DEFAULT_GPU_DEBUG,
		SkipNvidiaRuntimeHook: DEFAULT_GPU_SKIP_NVIDIA_RUNTIME_HOOK,
	},
	CRIU: CRIU{
		LeaveRunning:  DEFAULT_CRIU_LEAVE_RUNNING,
		ManageCgroups: DEFAULT_CRIU_MANAGE_CGROUPS,
	},
	Plugins: Plugins{
		LibDir: DEFAULT_PLUGINS_LIB_DIR,
		BinDir: DEFAULT_PLUGINS_BIN_DIR,
		Builds: DEFAULT_PLUGINS_BUILDS,
	},
}

// The current config directory, set during Init
var Dir string

func init() {
	setDefaults()
	bindEnvVars()
	err := viper.Unmarshal(&Global)
	if err != nil {
		panic(err)
	}
}

type Args struct {
	Config    string
	ConfigDir string
	Merge     bool
}

func Load(args ...Args) (err error) {
	var a Args
	if len(args) > 0 {
		a = args[0]
	}

	if a.ConfigDir == "" {
		Dir = DIR_PATH
	} else {
		Dir = a.ConfigDir
	}

	viper.AddConfigPath(Dir)
	viper.SetConfigType(FILE_TYPE)
	viper.SetConfigName(FILE_NAME)

	err = viper.ReadInConfig()
	if err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return fmt.Errorf("Config file %s is either outdated or invalid. Please delete or update it: %w", viper.ConfigFileUsed(), err)
		}
	}

	if a.Config != "" {
		reader := strings.NewReader(a.Config)
		err = viper.MergeConfig(reader)
		if err != nil {
			return fmt.Errorf("Provided config string is invalid: %w", err)
		}
	}

	err = viper.UnmarshalExact(&Global)
	if err != nil {
		return fmt.Errorf("Config file %s is either outdated or invalid. Please delete or update it: %w", viper.ConfigFileUsed(), err)
	}

	return nil
}

// Init initializes and writes the config file. Overwrites any existing config file, and the values
// used are from the global defaults overridden by env vars.
func Init(args ...Args) error {
	var a Args
	if len(args) > 0 {
		a = args[0]
	}

	if a.ConfigDir == "" {
		Dir = DIR_PATH
	} else {
		Dir = a.ConfigDir
	}

	viper.AddConfigPath(Dir)
	viper.SetConfigPermissions(FILE_PERM)
	viper.SetConfigType(FILE_TYPE)
	viper.SetConfigName(FILE_NAME)

	// Create config directory if it does not exist
	_, err := os.Stat(Dir)
	if os.IsNotExist(err) {
		err = os.MkdirAll(Dir, DIR_PERM)
		if err != nil {
			return err
		}
	}

	if a.Merge {
		err = viper.ReadInConfig()
		if err != nil {
			if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
				return fmt.Errorf("Config file %s is either outdated or invalid. Please delete or update it: %w", viper.ConfigFileUsed(), err)
			}
		}
	}

	if a.Config != "" {
		reader := strings.NewReader(a.Config)
		err = viper.MergeConfig(reader)
		if err != nil {
			return fmt.Errorf("Provided config string is invalid: %w", err)
		}
	}

	err = viper.SafeWriteConfig()
	if err != nil {
		err = viper.WriteConfig()
		if err != nil {
			return fmt.Errorf("Failed to write config file: %w", err)
		}
	}

	err = viper.UnmarshalExact(&Global)
	if err != nil {
		return fmt.Errorf("Config file %s is either outdated or invalid. Please delete or update it: %w", viper.ConfigFileUsed(), err)
	}

	return nil
}

// Loads the global defaults into viper
func setDefaults() {
	viper.SetTypeByDefaultValue(true)
	for _, field := range utils.ListLeaves(Config{}) {
		tag := utils.GetTag(Config{}, field, FILE_TYPE)
		defaultVal := utils.GetValue(Global, field)
		viper.SetDefault(tag, defaultVal)
	}
}

// Add bindings for env vars so env vars can be used as backup
// when a value is not found in config. Goes through all the json keys
// in the config type and binds an env var for it. The env var
// is prefixed with the envVarPrefix, all uppercase.
//
// Example: The field `cli.wait_for_ready` will bind to env var `CEDANA_CLI_WAIT_FOR_READY`.
func bindEnvVars() {
	viper.SetEnvPrefix(ENV_PREFIX)
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	for _, field := range utils.ListLeaves(Config{}) {
		tag := utils.GetTag(Config{}, field, FILE_TYPE)
		envVar := ENV_PREFIX + "_" + strings.ToUpper(strings.ReplaceAll(tag, ".", "_"))

		// get env aliases from struct tag
		aliasesStr := utils.GetTag(Config{}, field, "env_aliases")
		aliases := []string{tag, envVar}
		aliases = append(aliases, strings.Split(aliasesStr, ",")...)

		viper.MustBindEnv(aliases...)
	}

	viper.AutomaticEnv()
}
