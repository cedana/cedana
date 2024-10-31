package plugins

// Functions here never error out, they just return empty values. If a plugin
// cannot be accessed, the feature that depends on it is just disabled.

// Here, we simply load installed plugins. It assumes that the plugins were
// already installed by the user/plugin manager.

import (
	"os"
	"path/filepath"
	"plugin"

	"github.com/cedana/cedana/internal/logger"
	"github.com/rs/zerolog/log"
)

const (
	filePrefix             = "libcedana-"
	defaultInstallationDir = "/usr/local/lib/"

	// Features (symbols) that plugins can implement
	FEATURE_VERSION = "Version"

	FEATURE_DUMP_CMD        = "DumpCmd"
	FEATURE_DUMP_MIDDLEWARE = "DumpMiddleware"

	FEATURE_RESTORE_CMD        = "RestoreCmd"
	FEATURE_RESTORE_MIDDLEWARE = "RestoreMiddleware"
)

var LoadedPlugins = map[string]*plugin.Plugin{}

func init() {
	log.Logger = logger.DefaultLogger

	// look for plugins in the installation directory
	// and return a list of paths

	installationDir := GetInstallationDir()

	if installationDir == "" {
		log.Debug().Msg("No installation directory set for plugins")
		return
	}

	if _, err := os.Stat(installationDir); os.IsNotExist(err) {
		log.Debug().Msg("Installation directory for plugins does not exist")
		return
	}

	_, err := os.ReadDir(installationDir)
	if err != nil {
		log.Debug().Msg("Error reading installation directory for plugins")
	}

	for name, t := range Plugins {
		if t.Type != Supported {
			continue
		}

		path := filepath.Join(installationDir, filePrefix+name+".so")
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}

		p, err := plugin.Open(path)
		if err != nil {
			log.Debug().Err(err).Str("plugin", name).Msgf("Error loading plugin")
			continue
		}

		LoadedPlugins[name] = p
	}
}

func GetInstallationDir() string {
	if installationDir := os.Getenv("CEDANA_PLUGINS_DIR"); installationDir != "" {
		return installationDir
	} else {
		return defaultInstallationDir
	}
}

func RecoverFromPanic(plugin string) {
	if r := recover(); r != nil {
		log.Debug().Err(r.(error)).Str("plugin", plugin).Msg("Plugin failure, will proceed without it")
	}
}
