package namespace

import (
	"strings"

	"github.com/opencontainers/runc/libcontainer/configs"
)

// lifted from libcontainer
func CriuNsToKey(t configs.NamespaceType) string {
	return "extRoot" + strings.ToTitle(
		configs.NsName(t),
	) + "NS"
}
