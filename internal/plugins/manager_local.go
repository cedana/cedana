package plugins

// Implements a local (dummy) plugin manager

const (
	Supported     Type = iota // Go plugin that is supported by Cedana
	External                  // Not a go plugin, but still binary used by Cedana
	Experimental              // Go plugin that is not yet stable
	Deprecated                // Go plugin that is no longer maintained
	Unimplemented             // Go plugin that is not yet implemented
)

const (
	Unknown Status = iota
	Available
	Installed
)

var Plugins = map[string]PluginInfo{
	"runc":       {Type: Supported, Status: Unknown},
	"containerd": {Type: Unimplemented, Status: Unknown},
	"crio":       {Type: Unimplemented, Status: Unknown},
	"docker":     {Type: Unimplemented, Status: Unknown},
	"kata":       {Type: Unimplemented, Status: Unknown},
	"gpu":        {Type: External, Status: Unknown},
}
