package main

import (
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/plugins/k8s/cmd"
	"github.com/cedana/cedana/plugins/k8s/internal/container"
	"github.com/cedana/cedana/plugins/k8s/internal/pod"
	"github.com/cedana/cedana/plugins/k8s/internal/runtime"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
)

///////////////////////////
//// Exported Features ////
///////////////////////////

// loaded from ldflag definitions
var Version string = "dev"

var (
	QueryCmd     *cobra.Command   = cmd.QueryCmd
	HelperCmds   []*cobra.Command = []*cobra.Command{cmd.HelperCmd}
	queryHandler                  = &container.DefaultQueryHandler{Fs: afero.NewOsFs()}
)

var QueryHandler types.Query = queryHandler.Query
