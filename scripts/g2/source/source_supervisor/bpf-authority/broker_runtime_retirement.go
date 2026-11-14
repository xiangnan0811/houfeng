//go:build linux && amd64 && !cgo

package main

import "golang.org/x/sys/unix"

// retireRuntimeBinding releases the replay custody only after the bound task
// has actually exited. The listener remains separately owned until every
// guard-observed descendant has exited; grant bundle descriptors transfer to
// the grant and are not closed with the binding.
func (b *authorityBroker) retireRuntimeBinding(inv *runtimeInvocation) error {
	if b == nil || inv == nil || inv.state != "settled" && inv.state != "failed" {
		return errGuardProtocol
	}
	binding := b.runtimeBinds[inv.taskCookie]
	if binding == nil {
		return nil
	}
	if binding.pidfd != inv.pidfd || livePIDFD(binding.pidfd) {
		return errGuardProtocol
	}
	for _, fd := range binding.rights {
		if fd == inv.listener || inv.command == "create" && inv.grant != nil && (fd == inv.grant.bundleDirFD || fd == inv.grant.bundleConfigFD) {
			continue
		}
		if err := unix.Close(fd); err != nil {
			return err
		}
	}
	if err := b.authority.guard.dropTask(inv.taskCookie); err != nil {
		return err
	}
	if err := unix.Close(binding.pidfd); err != nil {
		return err
	}
	inv.pidfd = -1
	if p := b.principals[inv.principalID]; p != nil {
		p.PIDFD = -1
	}
	delete(b.runtimeBinds, inv.taskCookie)
	return nil
}

func (b *authorityBroker) retireRuntimeListener(inv *runtimeInvocation, lineage *daemonLineage) error {
	if inv == nil || inv.listener < 0 || lineage == nil || inv.state != "settled" && inv.state != "failed" {
		return nil
	}
	for _, node := range lineage.nodes {
		if node.exited {
			continue
		}
		current := node
		for depth := 0; ; depth++ {
			if depth >= 256 || current == nil {
				return errGuardProtocol
			}
			if current.cookie == inv.taskCookie {
				return nil
			}
			if current.cookie == lineage.root {
				break
			}
			current = lineage.nodes[current.parent]
		}
	}
	fd := inv.listener
	if b.runtimeInvocations[fd] != inv || inv.id > uint64(^uint(0)>>1)-1 {
		return errGuardProtocol
	}
	if err := unix.Close(fd); err != nil {
		return err
	}
	delete(b.runtimeInvocations, fd)
	inv.listener = -1
	// Keep the settled identity for grant settlement and transcript history.
	b.runtimeInvocations[-int(inv.id)-1] = inv
	return nil
}

func (b *authorityBroker) retireRevokedBundleCustody() error {
	for _, grant := range b.grants {
		if grant == nil || grant.phase != "revoked" || !grant.createBound || grant.bundleDirFD < 0 && grant.bundleConfigFD < 0 {
			continue
		}
		busy := false
		for _, inv := range b.runtimeInvocations {
			if inv.grant == grant && (inv.state != "settled" && inv.state != "failed" || b.runtimeBinds[inv.taskCookie] != nil) {
				busy = true
				break
			}
		}
		if busy {
			continue
		}
		if grant.bundleDirFD < 0 || grant.bundleConfigFD < 0 || grant.bundleDirFD == grant.bundleConfigFD {
			return errGuardProtocol
		}
		for _, fd := range []int{grant.bundleDirFD, grant.bundleConfigFD} {
			if err := unix.Close(fd); err != nil {
				return err
			}
		}
		grant.bundleDirFD, grant.bundleConfigFD = -1, -1
	}
	return nil
}
