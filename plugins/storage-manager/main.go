package main

import (
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/plugins/storage-manager/internal/limits"
)

///////////////////////////
//// Exported Features ////
///////////////////////////

// loaded from ldflag definitions
var Version string = "dev"

var (
	DumpMiddleware types.Middleware[types.Dump] = types.Middleware[types.Dump]{
		limits.CheckStorageLimit,
	}
)
