package main

import (
	"github.com/cedana/cedana/plugins/k8s/cmd"
	"github.com/spf13/cobra"
)

///////////////////////////
//// Exported Features ////
///////////////////////////

// loaded from ldflag definitions
var Version string = "dev"

var HelperCmds []*cobra.Command = []*cobra.Command{cmd.HelperCmd}
