package utils

import (
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/xeonx/timeago"
)

const MaxTimeAgo = 24 * 365 * 20 * time.Hour // 20 years

func TimeAgo(t time.Time) string {
	if t.IsZero() {
		return ""
	}

	config := timeago.English
	config.Max = MaxTimeAgo
	return config.Format(t)
}

var debugDumpTimeout = sync.OnceValue(func() time.Duration {
	seconds, err := strconv.Atoi(os.Getenv("DEBUG_DUMP_TIMEOUT"))
	if err != nil || seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
})

func DumpTimeout(d time.Duration) time.Duration {
	if override := debugDumpTimeout(); override > 0 {
		return override
	}
	return d
}
