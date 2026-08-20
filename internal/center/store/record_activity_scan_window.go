package store

import (
	"strings"
	"time"

	"houfeng/internal/center/activity"
)

// windowLowerBound keeps a zero From meaning "all history" rather than the zero
// instant, which PostgreSQL would happily compare against but which reads as a
// year-1 timestamp in a query plan.
func windowLowerBound(window activity.ScanWindow) time.Time {
	if window.From.IsZero() {
		return time.Unix(0, 0).UTC()
	}
	return window.From.UTC()
}

// activityKeysetAfter returns the exclusive source-event cursor used by ScanAfter
// keyset paging. Empty means "include every row at From".
func activityKeysetAfter(window activity.ScanWindow) string {
	return strings.TrimSpace(window.AfterEventID)
}
