package plugins

// Defines the plugin manager interface

type (
	Type   int
	Status int
)

type PluginInfo struct {
	Name    string
	Type    Type
	Version string
	Status  Status
}

type Manager interface {
	List() ([]PluginInfo, error)
	Install(PluginInfo) error
	Remove(PluginInfo) error

	// Misc
	TypeToString(Type) string
	StatusToString(Status) string
}
