package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cedana/cedana/internal/utils"
	"github.com/cedana/cedana/pkg/types"
	"github.com/spf13/viper"
)

const (
	configDirName  = ".cedana"
	configFileName = "config"
	configFileType = "json"
	envVarPrefix   = "CEDANA"
	configDirPerm  = 0755
	configFilePerm = 0644
)

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
	viper.SetEnvPrefix(envVarPrefix)

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

	setDefaults() // Only sets defaults for when no value is found in config
	bindEnvVars()

	// Allow environment variables to be accesses through viper *if* bound.
	// For e.g. CEDANA_SECRET will be accessible as viper.Get("secret")
	// However, viper.Get() always first checks the config file
	viper.AutomaticEnv()
	viper.SetTypeByDefaultValue(true)
	viper.ReadInConfig()

	if args.Config != "" {
		// we don't save config to file if it's passed as a string, as it's temporary
		reader := strings.NewReader(args.Config)
		err = viper.MergeConfig(reader)
	} else {
		viper.SafeWriteConfig() // Will only overwrite if file does not exist, ignore error
	}

	return err
}

func Get() (*types.Config, error) {
	var config types.Config
	err := viper.Unmarshal(&config)
	if err != nil {
		return nil, err
	}
	return &config, err
}

///////////////////
//    Helpers    //
///////////////////

// Set defaults that are used when no value is found in config/env vars
func setDefaults() {
	viper.SetDefault("options.leave_running", false)

	viper.SetDefault("storage.dump_dir", "/tmp")
	viper.SetDefault("storage.remote", false)

	viper.SetDefault("connection.cedana_url", "unset")
	viper.SetDefault("connection.cedana_auth_token", "unset")

	viper.SetDefault("cli.wait_for_ready", false)

	viper.SetDefault("profiling.enabled", false)
	viper.SetDefault("profiling.otel.port", 7777)
}

// Add bindings for env vars so env vars can be used as backup
// when a value is not found in config when using viper.Get~()
func bindEnvVars() {
	// Related to the config file
	viper.BindEnv("options.leave_running", "CEDANA_OPTIONS_LEAVE_RUNNING")

	viper.BindEnv("storage.dump_dir", "CEDANA_STORAGE_DUMP_DIR")
	viper.BindEnv("storage.remote", "CEDANA_REMOTE")

	viper.BindEnv("connection.cedana_url", "CEDANA_URL")
	viper.BindEnv("connection.cedana_auth_token", "CEDANA_AUTH_TOKEN")

	viper.BindEnv("cli.wait_for_ready", "CEDANA_CLI_WAIT_FOR_READY")

	viper.BindEnv("profiling.enabled", "CEDANA_PROFILING_ENABLED")
	viper.BindEnv("profiling.otel.port", "CEDANA_PROFILING_OTEL_PORT")
}
