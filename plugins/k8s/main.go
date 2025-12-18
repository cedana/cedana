package main

import (
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/plugins/k8s/cmd"
	"github.com/cedana/cedana/plugins/k8s/internal/pod"
	"github.com/spf13/cobra"
)

///////////////////////////
//// Exported Features ////
///////////////////////////

// loaded from ldflag definitions
var Version string = "dev"

var (
	QueryCmd   *cobra.Command   = cmd.QueryCmd
	HelperCmds []*cobra.Command = []*cobra.Command{cmd.HelperCmd}
)

var QueryHandler types.Query = pod.Query
