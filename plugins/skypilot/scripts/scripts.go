package scripts

import _ "embed"

//go:embed install.sh
var Install string

//go:embed uninstall.sh
var Uninstall string
