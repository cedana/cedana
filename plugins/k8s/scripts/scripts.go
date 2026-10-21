package scripts

import _ "embed"

//go:embed install.sh
var Install string

//go:embed install-plugins.sh
var InstallPlugins string

//go:embed configure-containerd.sh
var ConfigureContainerd string

//go:embed uninstall.sh
var Uninstall string

//go:embed configure-kubelet.sh
var ConfigureKubelet string
