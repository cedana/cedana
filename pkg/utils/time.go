package utils

import (
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
