//go:build linux && amd64 && !cgo

package main

import "golang.org/x/sys/unix"

// A grant is scoped to one admitted container and its authenticated owner. A
// terminal outcome is installed only after the actual object and cgroup
// lifecycle checks; the caller's SETTLE request alone cannot retire it.
type brokerGrant struct {
	id, owner, generation                                       uint64
	containerID, purpose, configSHA, phase, retirement, outcome string
	config                                                      map[string]any
	enrollment, rootCookie, cgroupID                            uint64
	probe                                                       *ownerProbeObservation
	facts                                                       *runtimeContainerFacts
	startObserved                                               bool
	createSettled, actualQueryConfirmed                         bool
	exec                                                        *brokerExecAdmission
	createBound                                                 bool
	bundleDirFD, bundleConfigFD                                 int
	bundlePath, bundleSHA                                       string
	bundleStat                                                  unix.Stat_t
	probeResult                                                 chan ownerProbeResult
	probeCompleted                                              bool
	initResult                                                  chan initSettlementResult
	initCompleted                                               bool
	settlementRequests                                          uint32
}

func (b *authorityBroker) bootstrapGrantsRetired(owner uint64) bool {
	if b == nil || owner == 0 {
		return false
	}
	seen := make(map[string]bool, 3)
	for _, g := range b.grants {
		if g.owner != owner {
			continue
		}
		switch g.purpose {
		case "postgres-uid", "postgres-gid", "dns-preflight":
			if seen[g.purpose] || g.phase != "retired" {
				return false
			}
			seen[g.purpose] = true
		default:
			return false
		}
	}
	return len(seen) == 3 && b.postgresUID != 0 && b.postgresGID != 0
}
