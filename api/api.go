package api

// This module is simply for initialization of the API package

import (
	"github.com/cedana/cedana/utils"
	"github.com/rs/zerolog"
)

var logger *zerolog.Logger

func init() {
	// TODO: Add output to log file
	logger = utils.GetLogger()
}
