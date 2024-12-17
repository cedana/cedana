package utils

import (
	"time"

	"github.com/rs/zerolog/log"
)

func LogElapsed(start time.Time, name string) {
	elapsed := time.Since(start)
	log.Trace().Str("in", name).Msgf("spent %s", elapsed)
}
