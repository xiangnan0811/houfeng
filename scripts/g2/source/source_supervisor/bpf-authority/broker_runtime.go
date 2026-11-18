//go:build linux && amd64 && !cgo

package main

// runtimeInvocation is the broker-owned binding for exactly one wrapper,
// held listener and grant generation. The listener FD is the key in
// authorityBroker.runtimeInvocations. A subsequent runc exec is authorized
// only after the guard reports the same task's pinned executable transition.
type runtimeInvocation struct {
	id, principalID, taskCookie, daemonCookie               uint64
	grantID, generation, listenerID, execID                 uint64
	containerID, command, configSHA, bundleSHA, processSHA  string
	state                                                   string
	pid, tgid                                               uint32
	startNS, wrapperExecSequence                            uint64
	pidfd, listener                                         int
	grant                                                   *brokerGrant
	ancestry                                                []any
	featureMaps, extendedProbes, replaceProbes, deviceLoads uint32
	devicePending, deviceSucceeded                          bool
	deviceLastResult                                        int64
	deviceLastLogSize, deviceLastLogTrueSize                uint32
	runtimeExecRecorded                                     bool
	ownerProbeHandlerDiagnosticAttempted                    bool
	runtimeExecGeneration, runtimeExecSequence              uint64
	replaceAttachCommitted                                  bool
}

// runtimeNotificationIdentity must be re-evaluated after draining guard events
// on every notification, not merely when the wrapper bound the listener.
func (b *authorityBroker) runtimeNotificationIdentity(inv *runtimeInvocation) (runtimeIdentity, error) {
	var identity runtimeIdentity
	if b == nil || inv == nil || b.checkpoint == nil || b.checkpoint.invalid || inv.id == 0 || inv.grant == nil || !inv.grant.createBound || inv.grant.facts == nil || !inv.grant.facts.runtimeReady || inv.grantID != inv.grant.id || inv.grant.generation != inv.generation || inv.listener < 0 || !livePIDFD(inv.pidfd) {
		return identity, errGuardProtocol
	}
	lineage := b.checkpoint.lifecycle.lineage
	if lineage == nil {
		return identity, errGuardProtocol
	}
	if _, err := lineage.ancestry(inv.taskCookie, "runc"); err != nil {
		return identity, err
	}
	node := lineage.nodes[inv.taskCookie]
	if node == nil || node.exited || node.startNS != inv.startNS || node.tgid != inv.tgid || node.tid != inv.pid || node.execSeq <= inv.wrapperExecSequence || node.digest != runtimeRuncSHA256 {
		return identity, errGuardProtocol
	}
	return runtimeIdentity{InvocationID: inv.id, GrantID: inv.grantID, Generation: inv.generation, TGID: inv.tgid, LeaderCookie: inv.taskCookie, PIDFD: inv.pidfd, RuncSHA256: runtimeRuncSHA256, RuncExecObserved: true}, nil
}
