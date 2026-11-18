//go:build linux && amd64 && !cgo

package main

import (
	"time"

	"golang.org/x/sys/unix"
)

const authorityGuestDeadlineNS uint64 = 3300 * 1_000_000_000

// guestDeadline is relative to the current guest boot's monotonic clock; it
// never restarts the 3300-second budget for a new principal or invocation.
func guestDeadline(maxDuration time.Duration) (time.Time, error) {
	if maxDuration <= 0 {
		return time.Time{}, errGuardProtocol
	}
	base := time.Now()
	now, err := monoNowNS()
	if err != nil {
		return time.Time{}, err
	}
	if now >= authorityGuestDeadlineNS {
		return time.Time{}, unix.ETIMEDOUT
	}
	remaining := time.Duration(authorityGuestDeadlineNS - now)
	if maxDuration < remaining {
		remaining = maxDuration
	}
	return base.Add(remaining), nil
}

func clampGuestDeadlineNS(deadline uint64) (uint64, error) {
	if deadline == 0 {
		return 0, errGuardProtocol
	}
	now, err := monoNowNS()
	if err != nil {
		return 0, err
	}
	if deadline > authorityGuestDeadlineNS {
		deadline = authorityGuestDeadlineNS
	}
	if deadline <= now {
		return 0, unix.ETIMEDOUT
	}
	return deadline, nil
}
