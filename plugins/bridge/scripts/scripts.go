package scripts

import _ "embed"

//go:embed install.sh
var Install string

//go:embed install-helper-service.sh
var InstallHelperService string

//go:embed uninstall.sh
var Uninstall string
