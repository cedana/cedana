package main

import (
	"github.com/cedana/cedana/plugins/storage-s3/s3"
)

///////////////////////////
//// Exported Features ////
///////////////////////////

// loaded from ldflag definitions
var Version string = "dev"

var NewStorage = s3.NewStorage
