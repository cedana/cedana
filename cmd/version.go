package cmd

import "fmt"

// TODO instead read this from env vars/build scripts
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func GetVersion() string {
	return fmt.Sprintf("%s (%s)", version, commit)
}
