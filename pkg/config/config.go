package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cedana/cedana/pkg/utils"
	"github.com/spf13/viper"
)

const (
	DIR_NAME   = ".cedana"
	FILE_NAME  = "config"
	FILE_TYPE  = "yaml"
	DIR_PERM   = 0o755
	FILE_PERM  = 0o644
	ENV_PREFIX = "CEDANA"

	// NOTE: `localhost` server inside kubernetes may or may not work
	// based on firewall and network configuration, it would only work
	// on local system, hence for serving default is 0.0.0.0
	DEFAULT_TCP_ADDR   = "0.0.0.0:8080"
	DEFAULT_SOCK_ADDR  = "/run/cedana.sock"
	DEFAULT_SOCK_PERMS = 0o666

	DEFAULT_PROTOCOL  = "unix"
	DEFAULT_LOG_LEVEL = "info"

	DEFAULT_COMPRESSION = "tar"
	DEFAULT_DUMP_DIR    = "/tmp"
)

// The default global config. This will get overwritten
// by the config file or env vars during startup, if they exist.
var Global Config = Config{
	// NOTE: Don't specify default address here as it depends on default protocol.
	// Use above constants for default address for each protocol.
	Protocol: DEFAULT_PROTOCOL,
	LogLevel: DEFAULT_LOG_LEVEL,
	Checkpoint: Checkpoint{
		Dir:         DEFAULT_DUMP_DIR,
		Compression: DEFAULT_COMPRESSION,
	},
	DB: DB{
		Remote: false,
		Path:   filepath.Join(os.TempDir(), "cedana.db"),
	},
	Profiling: Profiling{
		Enabled:       true,
		Precision: "auto",
	},
	Metrics: Metrics{
		ASR:  false,
		Otel: false,
	},
	CLI: CLI{
		WaitForReady: false,
	},
	CRIU: CRIU{
		LeaveRunning: false,
	},
	GPU: GPU{
		PoolSize: 0,
	},
}

func init() {
	setDefaults()
	bindEnvVars()
}

type InitArgs struct {
	Config    string
	ConfigDir string
}

func Init(args InitArgs) error {
	user, err := utils.GetUser()
	if err != nil {
		return err
	}

	var configDir string
	if args.ConfigDir == "" {
		homeDir := user.HomeDir
		configDir = filepath.Join(homeDir, DIR_NAME)
	} else {
		configDir = args.ConfigDir
	}

	viper.AddConfigPath(configDir)
	viper.SetConfigPermissions(FILE_PERM)
	viper.SetConfigType(FILE_TYPE)
	viper.SetConfigName(FILE_NAME)

	// Create config directory if it does not exist
	_, err = os.Stat(configDir)
	if os.IsNotExist(err) {
		err = os.MkdirAll(configDir, DIR_PERM)
		if err != nil {
			return err
		}
	}
	uid, _ := strconv.Atoi(user.Uid)
	gid, _ := strconv.Atoi(user.Gid)
	os.Chown(configDir, uid, gid)

	viper.ReadInConfig()

	if args.Config != "" {
		// we don't save config to file if it's passed as a string, as it's temporary
		reader := strings.NewReader(args.Config)
		err = viper.MergeConfig(reader)
	} else {
		viper.SafeWriteConfig() // Will only overwrite if file does not exist, ignore error
	}

	return viper.Unmarshal(&Global)
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
	viper.AutomaticEnv()

	for _, field := range utils.ListLeaves(Config{}) {
		tag := utils.GetTag(Config{}, field, FILE_TYPE)
		envVar := ENV_PREFIX + "_" + strings.ToUpper(strings.ReplaceAll(tag, ".", "_"))

		// get env aliases from struct tag
		aliasesStr := utils.GetTag(Config{}, field, "env_aliases")
		aliases := []string{tag, envVar}
		aliases = append(aliases, strings.Split(aliasesStr, ",")...)

		viper.MustBindEnv(aliases...)
	}
}
