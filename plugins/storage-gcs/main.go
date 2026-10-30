package main

import (
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/plugins/storage-gcs/gcs"
)

///////////////////////////
//// Exported Features ////
///////////////////////////

// loaded from ldflag definitions
var Version string = "dev"

var NewStorage = gcs.NewStorage

var HealthChecks types.Checks = types.Checks{
	List: []types.Check{
		gcs.CheckConfig(),
	},
}
