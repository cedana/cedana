package plugins

// Defines plugins recognized by Cedana

type PluginType int

const (
	Supported     PluginType = iota // Go plugin that is supported by Cedana
	External                        // Not a go plugin, but still binary used by Cedana
	Experimental                    // Go plugin that is not yet stable
	Deprecated                      // Go plugin that is no longer maintained
	Unimplemented                   // Go plugin that is not yet implemented
)

var Plugins = map[string]PluginType{
	"runc":       Supported,
	"containerd": Unimplemented,
	"crio":       Unimplemented,
	"docker":     Unimplemented,
	"kata":       Unimplemented,
	"gpu":        External,
}
