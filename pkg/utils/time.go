package utils

import (
	"fmt"
	"time"
)

func TimeAgo(t time.Time) string {
	return fmt.Sprintf("%s ago", time.Since(t).Truncate(time.Second))
}
