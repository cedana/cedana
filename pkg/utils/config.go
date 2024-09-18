package utils

import (
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cedana/cedana/pkg/types"
	"github.com/spf13/viper"
)

const (
	configDirName  = ".cedana"
	configFileName = "client_config"
	configFileType = "json"
	envVarPrefix   = "CEDANA"
	configDirPerm  = 0755
	configFilePerm = 0644
)

const (
	GpuControllerBinaryName = "gpucontroller"
	GpuControllerBinaryPath = "/usr/local/bin/cedana-gpu-controller"
	GpuSharedLibName        = "libcedana"
	GpuSharedLibPath        = "/usr/local/lib/libcedana-gpu.so"
)

func InitConfig(args types.InitConfigArgs) error {
	user, err := getUser()
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

func GetConfig() (*types.Config, error) {
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
	viper.SetDefault("client.task", "")
	viper.SetDefault("client.leave_running", false)
	viper.SetDefault("client.forward_logs", false)

	viper.SetDefault("shared_storage.dump_storage_dir", "/tmp")

	viper.SetDefault("connection.cedana_user", "random-user")

	viper.SetDefault("cli.wait_for_ready", false)
}

// Add bindings for env vars so env vars can be used as backup
// when a value is not found in config when using viper.Get~()
func bindEnvVars() {
	// Related to the config file
	viper.BindEnv("client.task", "CEDANA_CLIENT_TASK")
	viper.BindEnv("client.leave_running", "CEDANA_CLIENT_LEAVE_RUNNING")
	viper.BindEnv("client.forward_logs", "CEDANA_CLIENT_FORWARD_LOGS")
	viper.BindEnv("shared_storage.dump_storage_dir", "CEDANA_DUMP_STORAGE_DIR")
	viper.BindEnv("connection.cedana_url", "CEDANA_URL")
	viper.BindEnv("connection.cedana_user", "CEDANA_USER")
	viper.BindEnv("connection.cedana_auth_token", "CEDANA_AUTH_TOKEN")

	// Others used across the codebase
	viper.BindEnv("log_level", "CEDANA_LOG_LEVEL")
	viper.BindEnv("otel_port", "CEDANA_OTEL_PORT")
	viper.BindEnv("otel_enabled", "CEDANA_OTEL_ENABLED")
	viper.BindEnv("gpu_controller_path", "CEDANA_GPU_CONTROLLER_PATH")
	viper.BindEnv("gpu_shared_lib_path", "CEDANA_GPU_SHARED_LIB_PATH")
	viper.BindEnv("gpu_debugging_enabled", "CEDANA_GPU_DEBUGGING_ENABLED")
	viper.BindEnv("profiling_enabled", "CEDANA_PROFILING_ENABLED")
	viper.BindEnv("remote", "CEDANA_REMOTE")

	// CLI-specific
	viper.BindEnv("cli.wait_for_ready", "CEDANA_CLI_WAIT_FOR_READY")
}

func getUser() (*user.User, error) {
	username := os.Getenv("SUDO_USER")
	if username == "" {
		// fetch the current user
		// it uses getpwuid_r iirc
		u, err := user.Current()
		if err == nil {
			return u, nil
		}
		username = os.Getenv("USER")
	}
	return user.Lookup(username)
}
