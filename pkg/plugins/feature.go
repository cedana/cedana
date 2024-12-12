package plugins

// Defines the type and helper functions for plugin features

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/rs/zerolog/log"
)

// Feature is a symbol that a plugin can implement
type Feature[T any] string

// IfAvailable checks if a feature is available in any of the plugins, and
// if it is, it calls the provided function with the plugin name and the feature.
// Always goes through all plugins, even if one of them fails. Later, the errors
// are returned together, if any. If no plugins are provided, all plugins are checked.
// Ensures that before calling the function, the symbol is checked for nil.
func (feature Feature[T]) IfAvailable(
	do func(name string, sym T) error,
	plugins ...string,
) error {
	loadedPlugins := Load()

	errs := []error{}
	pluginSet := map[string]struct{}{}
	for _, p := range plugins {
		pluginSet[p] = struct{}{}
	}
	for name, p := range loadedPlugins {
		if _, ok := pluginSet[name]; len(pluginSet) > 0 && !ok {
			continue
		}
		defer RecoverFromPanic(name)
		if symUntyped, err := p.Lookup(string(feature)); err == nil {
			sym, ok := symUntyped.(*T)
			if !ok {
				log.Debug().
					Str("plugin", name).
					Str("expected", reflect.TypeOf(sym).String()).
					Str("got", reflect.TypeOf(symUntyped).String()).
					Msgf("%s is not the expected type", feature)
				errs = append(errs, fmt.Errorf("plugin '%s' has no valid %s", name, feature))
				continue
			}
			if sym == nil {
				log.Debug().Str("plugin", name).Msgf("%s is nil", feature)
				errs = append(errs, fmt.Errorf("plugin '%s' has no valid %s", name, feature))
				continue
			}
			errs = append(errs, do(name, *sym))
		} else {
			errs = append(errs, fmt.Errorf("plugin '%s' exports no %s", name, feature))
		}
	}
	return errors.Join(errs...)
}
