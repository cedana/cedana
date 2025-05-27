package main

import (
	"github.com/cedana/cedana/plugins/storage-cedana/propagator"
)

///////////////////////////
//// Exported Features ////
///////////////////////////

// loaded from ldflag definitions
var Version string = "dev"

var NewStorage = propagator.NewStorage
