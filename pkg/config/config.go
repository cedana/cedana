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
	configDirName  = ".cedana"
	configFileName = "config"
	configFileType = "json"
	configDirPerm  = 0o755
	configFilePerm = 0o644
	envVarPrefix   = "CEDANA"
)

// The default global config. This will get overwritten
// by the config file or env vars if they exist.
var Global Config = Config{
	Port:     8080,
	Host:     "0.0.0.0",
	LogLevel: "info",
	Connection: Connection{
		URL:       "unset",
		AuthToken: "unset",
	},
	Storage: Storage{
		Remote:      false,
		DumpDir:     "/tmp",
		Compression: "none",
	},
	Profiling: Profiling{
		Enabled: true,
	},
	Metrics: Metrics{
		ASR: false,
		Otel: Otel{
			Enabled: false,
			Port:    7777,
		},
	},
	CLI: CLI{
		WaitForReady: false,
	},
	CRIU: CRIU{
		BinaryPath:   "criu",
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
		configDir = filepath.Join(homeDir, configDirName)
	} else {
		configDir = args.ConfigDir
	}

	viper.AddConfigPath(configDir)
	viper.SetConfigPermissions(configFilePerm)
	viper.SetConfigType(configFileType)
	viper.SetConfigName(configFileName)

	// Create config directory if it does not exist
	_, err = os.Stat(configDir)
	if os.IsNotExist(err) {
		err = os.MkdirAll(configDir, configDirPerm)
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
		tag := utils.GetTag(Config{}, field, configFileType)
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
	for _, field := range utils.ListLeaves(Config{}, configFileType) {
		envVar := envVarPrefix + "_" + strings.ToUpper(strings.ReplaceAll(field, ".", "_"))
		viper.MustBindEnv(field, envVar)
	}
}
