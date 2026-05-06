package scripts

import _ "embed"

//go:embed install-service.sh
var InstallService string

//go:embed reset-service.sh
var ResetService string

//go:embed install-deps.sh
var InstallDeps string

//go:embed configure-shm.sh
var ConfigureShm string

//go:embed configure-io-uring.sh
var ConfigureIoUring string

//go:embed install-yq.sh
var InstallYq string

//go:embed utils.sh
var Utils string
