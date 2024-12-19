package plugins

// Defines theb feature type

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
)

// Feature is a typed symbol that a plugin can export
// with a description of what it does/used for.
type Feature[T any] struct {
	Symbol      string
	Description string
}

// IfAvailable checks if a feature is available in any of the plugins, and
// if it is, it calls the provided function with the plugin name and the feature.
// Always goes through all plugins, even if one of them fails. Later, the errors
// are returned together, if any. If no filter is provided, all plugins are checked.
func (feature Feature[T]) IfAvailable(
	do func(pluginName string, sym T) error,
	filter ...string,
) error {
	loadedPlugins := Load()

	errs := []error{}
	pluginSet := map[string]struct{}{}
	for _, p := range filter {
		pluginSet[p] = struct{}{}
	}
	noValidPlugins := true
	for name, p := range loadedPlugins {
		if _, ok := pluginSet[name]; len(pluginSet) > 0 && !ok {
			continue
		}
		noValidPlugins = false
		defer RecoverFromPanic(name)
		if symUntyped, err := p.Lookup(feature.Symbol); err == nil {
			sym, ok := symUntyped.(*T)
			if !ok {
				log.Debug().
					Str("plugin", name).
					Str("expected", reflect.TypeOf(sym).String()).
					Str("got", reflect.TypeOf(symUntyped).String()).
					Msgf("%s is not the expected type", feature.Description)
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
	if noValidPlugins {
		errs = append(errs, fmt.Errorf("feature '%s' is not available in %s", feature, utils.StrList(filter)))
	}
	return errors.Join(errs...)
}

// IsAvailable checks if a feature is available in any of the plugins.
// If no filter is provided, all plugins are checked.
// Also returns an error if the symbol is present but incompatible.
func (feature Feature[T]) IsAvailable(filter ...string) (bool, error) {
	loadedPlugins := Load()
	available := false

	pluginSet := map[string]struct{}{}
	for _, p := range filter {
		pluginSet[p] = struct{}{}
	}
	errs := []error{}

	for name, p := range loadedPlugins {
		if _, ok := pluginSet[name]; len(pluginSet) > 0 && !ok {
			continue
		}
		defer RecoverFromPanic(name)
		if symUntyped, err := p.Lookup(feature.Symbol); err == nil {
			sym, ok := symUntyped.(*T)
			if !ok {
				errs = append(errs, fmt.Errorf("plugin '%s' has no valid %s", name, feature))
				continue
			}
			if sym == nil {
				errs = append(errs, fmt.Errorf("plugin '%s' has no valid %s", name, feature))
				continue
			}
			available = true
		}
	}

	return available, errors.Join(errs...)
}

func (feature Feature[T]) String() string {
	return feature.Description
}
