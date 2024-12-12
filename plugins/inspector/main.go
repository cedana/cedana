package main

import "github.com/cedana/cedana/plugins/inspector/internal/checkpoint"

///////////////////////////
//// Exported Features ////
///////////////////////////

// loaded from ldflag definitions
var Version string = "dev"

var CheckpointInspect = checkpoint.Inspect
