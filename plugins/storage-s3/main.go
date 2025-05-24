package main

import (
	"github.com/cedana/cedana/pkg/io"
)

///////////////////////////
//// Exported Features ////
///////////////////////////

// loaded from ldflag definitions
var Version string = "dev"

var Storage io.Storage = io.Storage{
	Remote:   true,
	WriteTo:  nil,
	ReadFrom: nil,
}
