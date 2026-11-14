//go:build linux && amd64 && !cgo

package main

import "golang.org/x/sys/unix"

// closeOwnedOnFault does not report cleanup or removal. It only releases
// broker-owned tracked descriptions once before the faulted process exits.
func (b *authorityBroker) closeOwnedOnFault() {
	if b == nil || b.checkpoint == nil || !b.checkpoint.faulted {
		return
	}
	closed := make(map[int]bool)
	closeFD := func(fd int) {
		if fd < 3 || closed[fd] {
			return
		}
		closed[fd] = true
		_ = unix.Close(fd)
	}
	if q := b.pendingStart; q != nil {
		closeFD(q.pidfd)
		for _, fd := range q.rights {
			closeFD(fd)
		}
	}
	if q := b.pendingExecOp; q != nil {
		closeFD(q.eventFD)
	}
	if tr := b.pendingEndpoint; tr != nil {
		closeFD(tr.coordinatorPIDFD)
		closeFD(tr.scopeDirFD)
		tr.coordinatorPIDFD, tr.scopeDirFD = -1, -1
	}
	for _, rec := range b.serviceSecrets {
		if rec != nil {
			closeFD(rec.pgdataDirFD)
			rec.close()
		}
	}
	for _, adm := range b.execAdmissions {
		if adm != nil {
			closeFD(adm.appPIDFD)
			closeFD(adm.execTaskPIDFD)
			adm.closeHeld()
		}
	}
	for _, actor := range b.probeActors {
		if actor != nil {
			closeFD(actor.pidfd)
			closeFD(actor.listenerFD)
			closeFD(actor.fixtureAFD)
			closeFD(actor.fixtureBFD)
			actor.closeHeld()
		}
	}
	if b.effects != nil {
		for cookie, fd := range b.effects.objectFD {
			closeFD(fd)
			delete(b.effects.objectFD, cookie)
		}
		if t := b.effects.pending; t != nil && (t.command == "MAP_CREATE" || t.command == "PROG_LOAD") && t.effectExecuted && t.effectErrno == 0 && t.effectFD >= 0 && !t.cleanupActive {
			closeFD(t.effectFD)
		}
		for _, alias := range b.effects.aliases {
			if alias != nil && !alias.closed {
				closeFD(alias.fd)
				alias.fd, alias.closed = -1, true
			}
		}
		for _, group := range b.effects.cgroups {
			if group != nil && group.HeldFD >= 0 {
				closeFD(group.HeldFD)
				group.HeldFD = -1
			}
		}
		if b.effects.registry != nil {
			closeFD(b.effects.registry.systemdFD)
			b.effects.registry.systemdFD = -1
		}
	}
	for _, inv := range b.runtimeInvocations {
		if inv != nil {
			closeFD(inv.listener)
			closeFD(inv.pidfd)
			inv.listener, inv.pidfd = -1, -1
		}
	}
	for _, inv := range b.featureInvocations {
		if inv != nil {
			closeFD(inv.pidfd)
			inv.pidfd = -1
		}
	}
	for _, binding := range b.runtimeBinds {
		if binding == nil {
			continue
		}
		closeFD(binding.pidfd)
		binding.pidfd = -1
		for i, fd := range binding.rights {
			closeFD(fd)
			binding.rights[i] = -1
		}
	}
	for _, grant := range b.grants {
		if grant != nil && grant.createBound {
			closeFD(grant.bundleDirFD)
			closeFD(grant.bundleConfigFD)
			grant.bundleDirFD, grant.bundleConfigFD = -1, -1
		}
	}
	for _, launch := range b.launches {
		if launch == nil {
			continue
		}
		closeFD(launch.launcherPIDFD)
		closeFD(launch.childPIDFD)
		launch.launcherPIDFD, launch.childPIDFD = -1, -1
		for i, fd := range launch.retainedFDs {
			closeFD(fd)
			launch.retainedFDs[i] = -1
		}
	}
	for _, daemon := range b.daemons {
		if daemon != nil {
			closeFD(daemon.pidfd)
			daemon.pidfd = -1
		}
	}
	for _, principal := range b.principals {
		if principal == nil {
			continue
		}
		closeFD(principal.PIDFD)
		principal.PIDFD = -1
		closeFD(principal.TransferChildPIDFD)
		principal.TransferChildPIDFD = -1
		for id, replay := range principal.Replay {
			for i, fd := range replay.HeldFDs {
				closeFD(fd)
				replay.HeldFDs[i] = -1
			}
			principal.Replay[id] = replay
		}
	}
}
