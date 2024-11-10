package plugins

// Functions here never error out, they just return empty values. If a plugin
// cannot be accessed, the feature that depends on it is just disabled.

// Here, we simply load installed plugins. It assumes that the plugins were
// already installed by the user/plugin manager.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"plugin"

	"github.com/cedana/cedana/internal/config"
	"github.com/cedana/cedana/internal/logger"
	"github.com/rs/zerolog/log"
)

const (
	// Features (symbols) that plugins can implement
	FEATURE_VERSION            = "Version"
	FEATURE_DUMP_CMD           = "DumpCmd"
	FEATURE_DUMP_MIDDLEWARE    = "DumpMiddleware"
	FEATURE_RESTORE_CMD        = "RestoreCmd"
	FEATURE_RESTORE_MIDDLEWARE = "RestoreMiddleware"
	FEATURE_START_CMD          = "StartCmd"
	FEATURE_START_HANDLER      = "StartHandler"
)

var LoadedPlugins = map[string]*plugin.Plugin{}

func init() {
	log.Logger = logger.DefaultLogger
	LoadPlugins()
}

func LoadPlugins() {
	// look for plugins in the installation directory
	// and return a list of paths

	installationDir := config.Get(config.PLUGINS_LIB_DIR)

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

	for _, t := range Plugins {
		if t.Type != Supported {
			continue
		}

		for _, filename := range t.Libraries {
			path := filepath.Join(installationDir, filename)
			if _, err := os.Stat(path); os.IsNotExist(err) {
				continue
			}

			p, err := plugin.Open(path)
			if err != nil {
				log.Debug().Err(err).Str("plugin", t.Name).Msgf("Error loading plugin")
				continue
			}

			LoadedPlugins[t.Name] = p
		}
	}
}

func RecoverFromPanic(plugin string) {
	if r := recover(); r != nil {
		log.Debug().Err(r.(error)).Str("plugin", plugin).Msg("plugin failure")
	}
}

// IfFeatureAvailable checks if a feature is available in any of the plugins, and
// if it is, it calls the provided function with the plugin name and the feature.
// Always goes through all plugins, even if one of them fails. Later, the errors
// are returned together, if any. If no plugins are provided, all plugins are checked.
func IfFeatureAvailable[T plugin.Symbol](feature string, do func(name string, sym T) error, plugins ...string) error {
	errs := []error{}
	pluginSet := map[string]struct{}{}
	for _, p := range plugins {
		pluginSet[p] = struct{}{}
	}
	for name, p := range LoadedPlugins {
		defer RecoverFromPanic(name)
		if _, ok := pluginSet[name]; len(pluginSet) > 0 && !ok {
			continue
		}
		if symUntyped, err := p.Lookup(feature); err == nil {
			// Add new subcommand from supported plugins
			sym, ok := symUntyped.(T)
			if !ok {
				log.Debug().Str("plugin", name).Msgf("%s is not valid", feature)
				errs = append(errs, fmt.Errorf("plugin '%s' has no valid %s", name, feature))
				continue
			}
			errs = append(errs, do(name, sym))
		} else {
			errs = append(errs, fmt.Errorf("plugin '%s' has no valid %s", name, feature))
		}
	}
	return errors.Join(errs...)
}
