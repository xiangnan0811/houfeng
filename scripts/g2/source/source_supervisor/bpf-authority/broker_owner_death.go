//go:build linux && amd64 && !cgo

package main

import (
	"errors"

	"golang.org/x/sys/unix"
)

// revokedCleanupAllowed recognizes the original owner's actual whole-process
// death and only the existing grant. Revocation never grants new effects.
func (b *authorityBroker) revokedCleanupAllowed(g *brokerGrant) bool {
	if b == nil || g == nil || b.grants[g.id] != g || g.phase != "revoked" {
		return false
	}
	owner := b.principals[g.owner]
	if owner == nil || !owner.Terminal || owner.PIDFD < 0 {
		return false
	}
	dead, err := pidfdGroupExited(owner.PIDFD)
	return err == nil && dead
}

func (b *authorityBroker) serviceOwnerDeaths() error {
	if b == nil || b.effects == nil || b.checkpoint == nil {
		return errGuardProtocol
	}
	for _, principal := range b.principals {
		if principal == nil || principal.Terminal || principal.Transferred {
			continue
		}
		switch principal.Role {
		case "BOOT_OWNER", "REALM_ROOT", "RECOVERY_PENDING", "RECOVERY_OBSERVER":
		default:
			continue
		}
		dead, err := pidfdGroupExited(principal.PIDFD)
		if err != nil {
			b.checkpoint.markFault()
			return err
		}
		if !dead {
			continue
		}
		// Keep the original held pidfd and replay references: daemon and root
		// registrations may share those exact descriptors with cleanup state.
		if err = b.updateExistingIdentity("principal", principal.ID, "terminal", "exit"); err != nil {
			b.checkpoint.markFault()
			return err
		}
		principal.Terminal = true
		if err = b.failOwnerCheckpoint(principal); err != nil {
			b.checkpoint.markFault()
			return err
		}
		if stage := b.selftestFD; stage != nil && stage.owner == principal.ID && stage.phase == "armed" {
			b.checkpoint.markOwnerDeath()
			b.selftestFD = nil
		}
		if b.pendingEndpoint != nil && b.pendingEndpoint.ownerID == principal.ID {
			b.closeEndpointTransitionGate()
		}
		for _, grant := range b.grants {
			if grant != nil && grant.owner == principal.ID {
				b.clearServiceSecretForGrant(grant)
			}
		}
		for _, grant := range b.grants {
			if grant == nil || grant.owner != principal.ID {
				continue
			}
			switch grant.phase {
			case "admitted", "active", "settling":
				grant.phase = "revoked"
				if err = b.emitGrantPhase(grant); err != nil {
					b.checkpoint.markFault()
					return err
				}
			case "retired", "revoked":
			default:
				b.checkpoint.markFault()
				return errGuardProtocol
			}
		}
	}
	return nil
}

// serviceRevokedCleanup advances only existing retained creator descriptions
// and the already enrolled cgroup's fixed closing sequence. It never creates a
// new object or grants a new actor, and a revoke is never a closure receipt.
func (b *authorityBroker) serviceRevokedCleanup() error {
	if b == nil || b.effects == nil || b.checkpoint == nil || b.effects.registry == nil {
		return errGuardProtocol
	}
	if b.checkpoint.invalid && !b.canObserveOwnerDeath() || b.effects.pending != nil || b.selftestFD != nil || b.checkpoint.active != nil || b.pendingCheckpoint != nil {
		return nil
	}
	for _, grant := range b.grants {
		if grant == nil || grant.phase != "revoked" {
			continue
		}
		if !b.revokedCleanupAllowed(grant) {
			b.checkpoint.markFault()
			return errGuardProtocol
		}
		var next uint64
		aliasesReady := true
		for _, alias := range b.effects.aliases {
			if alias != nil && alias.grantID == grant.id && !alias.closed && (alias.transaction == nil || !alias.transaction.executionReleased) {
				aliasesReady = false
				break
			}
		}
		if !aliasesReady {
			continue
		}
		if err := b.closeGrantAliases(grant); err != nil {
			b.checkpoint.markFault()
			return err
		}
		for cookie, owner := range b.effects.objectGrant {
			if owner != grant.id || b.effects.grantCleanup[cookie] != nil {
				continue
			}
			object := b.effects.objects.byCookie[cookie]
			fd, held := b.effects.objectFD[cookie]
			if object == nil {
				b.checkpoint.markFault()
				return errGuardProtocol
			}
			if !held || fd < 0 {
				continue
			}
			creator := b.effects.transactions[object.ownerTransaction]
			if creator == nil {
				b.checkpoint.markFault()
				return errGuardProtocol
			}
			if creator.state != "retired" {
				continue
			}
			if next == 0 || cookie < next {
				next = cookie
			}
		}
		if next != 0 {
			if err := b.beginGrantObjectCleanup(grant, next); err != nil {
				b.checkpoint.markFault()
				return err
			}
			return nil
		}
		group := b.effects.cgroups[grant.id]
		if group == nil || group.Retired {
			continue
		}
		target := b.effects.registry.targets[cgroupKey{group.RootCookie, group.KernfsID}]
		if target == nil {
			if group.OfflineSeq == 0 || group.ReleaseSeq == 0 {
				continue
			}
			observed, err := b.authority.guard.retireCgroup(group.Enrollment)
			if errors.Is(err, unix.EAGAIN) {
				continue
			}
			if err != nil || observed.State != 2 || observed.Sequence != group.ReleaseSeq {
				b.checkpoint.markFault()
				return errGuardProtocol
			}
			if err = unix.Close(group.HeldFD); err != nil {
				b.checkpoint.markFault()
				return err
			}
			group.HeldFD, group.Retired = -1, true
			return nil
		}
		if target.grant != grant || target.group != group {
			b.checkpoint.markFault()
			return errGuardProtocol
		}
		if target.phase == 2 {
			if err := b.beginEffectCgroupSettlement(group, grant); err != nil {
				b.checkpoint.markFault()
				return err
			}
			return nil
		}
		if target.phase == 3 && target.offlineSequence != 0 && target.releaseSequence > target.offlineSequence {
			if err := b.settleEffectCgroup(group, grant); err != nil {
				b.checkpoint.markFault()
				return err
			}
			return nil
		}
		if target.phase != 3 {
			b.checkpoint.markFault()
			return errGuardProtocol
		}
	}
	return nil
}
