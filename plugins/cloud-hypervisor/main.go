package main

import (
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/plugins/cloud-hypervisor/cmd"

	"github.com/cedana/cedana/plugins/cloud-hypervisor/internal/handlers"
	"github.com/spf13/cobra"
)

///////////////////////////
//// Exported Features ////
///////////////////////////

// loaded from ldflag definitions
var Version string = "dev"

var (
	DumpCmd *cobra.Command = cmd.DumpCmd
	// RestoreCmd *cobra.Command = cmd.RestoreCmd
)

var (
	DumpVMHandler    types.DumpVM                   = handlers.Dump
	DumpVMMiddleware types.Middleware[types.DumpVM] = types.Middleware[types.DumpVM]{}
)
