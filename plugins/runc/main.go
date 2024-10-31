package main

import (
	"github.com/cedana/cedana/plugins/runc/cmd"
	"github.com/spf13/cobra"
)

// loaded from ldflag definitions
var Version = "dev"

// CMDs
var (
	DumpCmd    *cobra.Command
	RestoreCmd *cobra.Command
)

func init() {
	DumpCmd = cmd.DumpCmd
	RestoreCmd = cmd.RestoreCmd
}
