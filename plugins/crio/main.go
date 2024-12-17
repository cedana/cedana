package main

import (
	"github.com/cedana/cedana/pkg/types"
	"github.com/jedib0t/go-pretty/v6/text"
)

///////////////////////////
//// Exported Features ////
///////////////////////////

// loaded from ldflag definitions
var Version string = "dev"

var CmdTheme text.Colors = text.Colors{text.FgMagenta}

var (
	RunHandler    types.Run                   = nil
	RunMiddleware types.Middleware[types.Run] = types.Middleware[types.Run]{}

	DumpMiddleware types.Middleware[types.Dump] = types.Middleware[types.Dump]{}

	RestoreMiddleware types.Middleware[types.Restore] = types.Middleware[types.Restore]{}
)
