//go:build linux && amd64 && !cgo

package main

import (
	"bytes"
	"fmt"
	"sort"

	"golang.org/x/sys/unix"
)

// effectRegistry is owned by the original broker thread. Entries are never
// recycled: retired target keys remain as kernel and userspace tombstones.
type effectRegistry struct {
	root             uint64
	targets          map[cgroupKey]*effectRegistryTarget
	taskCount        int
	systemd          guardTask
	systemdFD        int
	systemdStat      unix.Stat_t
	nestedParent     *runtimeEffectTransaction
	nestedGroup      *liveCgroup
	nestedInvocation *runtimeInvocation
}

type effectRegistryTarget struct {
	group                                      *liveCgroup
	grant                                      *brokerGrant
	phase, retirement                          uint32
	sequence, offlineSequence, releaseSequence uint64
	tasks                                      map[effectRegistryTaskKey]*effectRegistryTask
}

type effectRegistryTaskKey struct {
	tgid  uint32
	start uint64
}
type effectRegistryTask struct {
	key                            effectRegistryTaskKey
	invocation, dev, ino, sequence uint64
}

// The caller supplies the original PID1 guard reference and a held copy of
// its executable, independently compared with the installed systemd binary.
// Ownership of fd transfers only on success.
func newEffectRegistry(b *authorityBroker, task guardTask, fd int, stat unix.Stat_t) (*effectRegistry, error) {
	if b == nil || b.authority == nil || task.TGID != 1 || task.TID != 1 || task.Cookie == 0 || task.StartBoottimeNS == 0 || fd < 0 {
		return nil, errGuardProtocol
	}
	var held, current unix.Stat_t
	if unix.Fstat(fd, &held) != nil || !sameTrustedStat(held, stat) || held.Mode&unix.S_IFMT != unix.S_IFREG || held.Uid != 0 || held.Mode&0022 != 0 || held.Ino == 0 || unix.Fstatat(heldProcDirectory, "1/exe", &current, 0) != nil || !sameTrustedStat(held, current) {
		return nil, errGuardProtocol
	}
	pinned, err := b.authority.guard.pinTask(1)
	if err != nil {
		return nil, err
	}
	err = b.authority.guard.dropTask(pinned.Cookie)
	if err != nil || pinned != task {
		return nil, errGuardProtocol
	}
	r := &effectRegistry{targets: make(map[cgroupKey]*effectRegistryTarget), systemd: task, systemdFD: fd, systemdStat: held}
	if err = b.effectRegistryGuard(false); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *effectRegistry) close() error {
	if r == nil || r.systemdFD < 0 {
		return errGuardProtocol
	}
	err := unix.Close(r.systemdFD)
	r.systemdFD = -1
	return err
}

func (b *authorityBroker) effectRegistryGuard(cleanup bool) error {
	if b == nil || b.authority == nil || b.checkpoint == nil || b.checkpoint.invalid && (!cleanup || !b.canObserveOwnerDeath()) {
		return errGuardProtocol
	}
	s, err := b.authority.guard.statusObserved(5)
	if err != nil || s.State != 1 || s.Sticky != 0 || s.Lost != 0 || s.ProbeMissed != 0 || s.BrokerCookie != b.authority.brokerCookie || s.Epoch != b.authority.seal.Epoch || s.LinkIDs != b.authority.seal.LinkIDs {
		b.checkpoint.markFault()
		return errGuardProtocol
	}
	return nil
}

func effectRegistryTargetKey(g *liveCgroup) []byte {
	key := make([]byte, 16)
	guardPut64(key, 0, g.RootCookie)
	guardPut64(key, 8, g.KernfsID)
	return key
}
func effectRegistryTargetValue(t *effectRegistryTarget) []byte {
	value := make([]byte, 24)
	guardPut64(value, 0, t.group.Enrollment)
	guardPut64(value, 8, t.grant.id)
	guardPut32(value, 16, t.phase)
	guardPut32(value, 20, t.retirement)
	return value
}
func effectRegistryTargetRecord(t *effectRegistryTarget, action string) map[string]any {
	return map[string]any{"map": "realm_targets", "action": action, "key": map[string]any{"root_kn_id": jsonU64(t.group.RootCookie), "kn_id": jsonU64(t.group.KernfsID)}, "value": map[string]any{"enrollment": jsonU64(t.group.Enrollment), "grant_id": jsonU64(t.grant.id), "phase": jsonU64(uint64(t.phase)), "retirement": jsonU64(uint64(t.retirement))}, "grant_id": jsonU64(t.grant.id)}
}
func effectRegistryTaskBytes(t *effectRegistryTarget, actor *effectRegistryTask) ([]byte, []byte) {
	key := make([]byte, 32)
	guardPut64(key, 0, t.group.RootCookie)
	guardPut64(key, 8, t.group.KernfsID)
	guardPut32(key, 16, actor.key.tgid)
	guardPut64(key, 24, actor.key.start)
	value := make([]byte, 40)
	guardPut64(value, 0, t.group.Enrollment)
	guardPut64(value, 8, t.grant.id)
	guardPut64(value, 16, actor.invocation)
	guardPut64(value, 24, actor.dev)
	guardPut64(value, 32, actor.ino)
	return key, value
}
func effectRegistryTaskRecord(t *effectRegistryTarget, actor *effectRegistryTask, action string) map[string]any {
	var value any
	if action != "delete" {
		value = map[string]any{"enrollment": jsonU64(t.group.Enrollment), "grant_id": jsonU64(t.grant.id), "invocation_id": jsonU64(actor.invocation), "exe_dev": jsonU64(actor.dev), "exe_ino": jsonU64(actor.ino)}
	}
	return map[string]any{"map": "retire_tasks", "action": action, "key": map[string]any{"root_kn_id": jsonU64(t.group.RootCookie), "kn_id": jsonU64(t.group.KernfsID), "tgid": jsonU64(uint64(actor.key.tgid)), "leader_start_ns": jsonU64(actor.key.start)}, "value": value, "grant_id": jsonU64(t.grant.id)}
}

// Updates and their exact readback use separate real SYS_bpf transactions.
// Any indeterminate publication or readback poisons the current checkpoint;
// it cannot be retried as though no effect had occurred.
func (b *authorityBroker) effectRegistryWrite(name string, key, value []byte, record map[string]any, flags uint64, cleanup bool) (uint64, error) {
	if err := b.effectRegistryGuard(cleanup); err != nil {
		return 0, err
	}
	seq, _, err := b.executeRegistryMap(unix.BPF_MAP_UPDATE_ELEM, name, key, value, flags, record, 0, cleanup, b.effects.registry.nestedParent)
	if err != nil {
		b.checkpoint.markFault()
		return 0, err
	}
	_, actual, err := b.executeRegistryMap(unix.BPF_MAP_LOOKUP_ELEM, name, key, nil, 0, nil, seq, cleanup, b.effects.registry.nestedParent)
	if err != nil || !bytes.Equal(actual, value) {
		b.checkpoint.markFault()
		return 0, errGuardProtocol
	}
	if err = b.effectRegistryGuard(cleanup); err != nil {
		return 0, err
	}
	return seq, nil
}

func (b *authorityBroker) effectRegistryReadback(name string, key, expected []byte, sequence uint64) error {
	if err := b.effectRegistryGuard(false); err != nil {
		return err
	}
	_, actual, err := b.executeRegistryMap(unix.BPF_MAP_LOOKUP_ELEM, name, key, nil, 0, nil, sequence, false, b.effects.registry.nestedParent)
	if err != nil || !bytes.Equal(actual, expected) {
		b.checkpoint.markFault()
		return errGuardProtocol
	}
	return b.effectRegistryGuard(false)
}

func (b *authorityBroker) effectRegistryTarget(g *liveCgroup, grant *brokerGrant, diagnostic *brokerBindDiagnostic) (*effectRegistryTarget, error) {
	if diagnostic != nil {
		diagnostic.targetPoint = brokerTargetPointUnknown
	}
	if b == nil {
		return nil, diagnostic.rejectTarget(brokerTargetPointBrokerNil, errGuardProtocol)
	}
	if b.effects == nil {
		return nil, diagnostic.rejectTarget(brokerTargetPointEffectsNil, errGuardProtocol)
	}
	if b.effects.registry == nil {
		return nil, diagnostic.rejectTarget(brokerTargetPointRegistryNil, errGuardProtocol)
	}
	if g == nil {
		return nil, diagnostic.rejectTarget(brokerTargetPointGroupNil, errGuardProtocol)
	}
	if grant == nil {
		return nil, diagnostic.rejectTarget(brokerTargetPointGrantNil, errGuardProtocol)
	}
	if g.HeldFD < 0 {
		return nil, diagnostic.rejectTarget(brokerTargetPointHeldFD, errGuardProtocol)
	}
	if g.Retired {
		return nil, diagnostic.rejectTarget(brokerTargetPointGroupRetired, errGuardProtocol)
	}
	if g.GrantID != grant.id {
		return nil, diagnostic.rejectTarget(brokerTargetPointGroupGrantID, errGuardProtocol)
	}
	if g.Enrollment == 0 {
		return nil, diagnostic.rejectTarget(brokerTargetPointEnrollment, errGuardProtocol)
	}
	if g.RootCookie == 0 {
		return nil, diagnostic.rejectTarget(brokerTargetPointRootCookie, errGuardProtocol)
	}
	if g.KernfsID == 0 {
		return nil, diagnostic.rejectTarget(brokerTargetPointKernfsID, errGuardProtocol)
	}
	if grant.id == 0 {
		return nil, diagnostic.rejectTarget(brokerTargetPointGrantIDZero, errGuardProtocol)
	}
	r := b.effects.registry
	t := r.targets[cgroupKey{root: g.RootCookie, id: g.KernfsID}]
	if t == nil {
		return nil, diagnostic.rejectTarget(brokerTargetPointTargetMissing, errGuardProtocol)
	}
	if t.group != g {
		return nil, diagnostic.rejectTarget(brokerTargetPointGroupIdentity, errGuardProtocol)
	}
	if t.grant != grant {
		return nil, diagnostic.rejectTarget(brokerTargetPointGrantIdentity, errGuardProtocol)
	}
	if t.phase == 4 {
		return nil, diagnostic.rejectTarget(brokerTargetPointTargetPhaseRetired, errGuardProtocol)
	}
	return t, nil
}

func (b *authorityBroker) effectRegistrySystemdActor(t *effectRegistryTarget) (*effectRegistryTask, error) {
	r := b.effects.registry
	if r == nil || r.systemdFD < 0 {
		return nil, errGuardProtocol
	}
	var held, current unix.Stat_t
	if unix.Fstat(r.systemdFD, &held) != nil || !sameTrustedStat(held, r.systemdStat) || unix.Fstatat(heldProcDirectory, "1/exe", &current, 0) != nil || !sameTrustedStat(held, current) {
		return nil, errGuardProtocol
	}
	pinned, err := b.authority.guard.pinTask(1)
	if err != nil {
		return nil, err
	}
	err = b.authority.guard.dropTask(pinned.Cookie)
	if err != nil || pinned != r.systemd {
		return nil, errGuardProtocol
	}
	dev, err := kernelDevice(uint64(held.Dev))
	if err != nil {
		return nil, err
	}
	return &effectRegistryTask{key: effectRegistryTaskKey{tgid: 1, start: pinned.StartBoottimeNS}, dev: dev, ino: held.Ino}, nil
}

func (b *authorityBroker) effectRegistryRuntimeActor(t *effectRegistryTarget, inv *runtimeInvocation) (*effectRegistryTask, error) {
	if inv == nil || inv.grant != t.grant || inv.grantID != t.grant.id || inv.id == 0 || inv.tgid <= 1 || inv.pid != inv.tgid || b.runtimeInvocations[inv.listener] != inv || (inv.command != "create" && inv.command != "delete" && inv.command != "kill") {
		return nil, errGuardProtocol
	}
	if inv.command != "create" && (!t.grant.createBound || t.grant.phase != "admitted" && t.grant.phase != "active" && t.grant.phase != "settling") {
		return nil, errGuardProtocol
	}
	if _, err := b.runtimeNotificationIdentity(inv); err != nil {
		return nil, err
	}
	lineage := b.checkpoint.lifecycle.lineage
	node := lineage.nodes[inv.taskCookie]
	if node == nil || node.exited || node.tid != inv.tgid || node.tgid != inv.tgid || node.startNS != inv.startNS || node.role != "runc" {
		return nil, errGuardProtocol
	}
	held, ok := lineage.executables[node.executable]
	if !ok || held.file == nil || held.role != "runc" || held.digest != runtimeRuncSHA256 {
		return nil, errGuardProtocol
	}
	var original, current unix.Stat_t
	if unix.Fstat(int(held.file.Fd()), &original) != nil || unix.Stat(fmt.Sprintf("/proc/%d/exe", inv.tgid), &current) != nil || !sameTrustedStat(original, current) {
		return nil, errGuardProtocol
	}
	dev, err := kernelDevice(uint64(original.Dev))
	if err != nil || dev != node.executable.dev || original.Ino != node.executable.ino {
		return nil, errGuardProtocol
	}
	pinned, err := b.authority.guard.pinTask(inv.tgid)
	if err != nil {
		return nil, err
	}
	err = b.authority.guard.dropTask(pinned.Cookie)
	if err != nil || pinned.Cookie != inv.taskCookie || pinned.TGID != inv.tgid || pinned.TID != inv.tgid || pinned.StartBoottimeNS != inv.startNS || !livePIDFD(inv.pidfd) {
		return nil, errGuardProtocol
	}
	return &effectRegistryTask{key: effectRegistryTaskKey{tgid: inv.tgid, start: pinned.StartBoottimeNS}, invocation: inv.id, dev: dev, ino: original.Ino}, nil
}

func (b *authorityBroker) effectRegistryAddActor(t *effectRegistryTarget, actor *effectRegistryTask) error {
	r := b.effects.registry
	if actor == nil || actor.key.tgid == 0 || actor.key.start == 0 || actor.ino == 0 || (actor.key.tgid == 1) != (actor.invocation == 0) {
		return errGuardProtocol
	}
	if old := t.tasks[actor.key]; old != nil {
		if old.invocation != actor.invocation || old.dev != actor.dev || old.ino != actor.ino {
			return errGuardProtocol
		}
		key, value := effectRegistryTaskBytes(t, old)
		return b.effectRegistryReadback("retire_tasks", key, value, old.sequence)
	}
	if r.taskCount >= 4096 {
		return errGuardProtocol
	}
	key, value := effectRegistryTaskBytes(t, actor)
	seq, err := b.effectRegistryWrite("retire_tasks", key, value, effectRegistryTaskRecord(t, actor, "insert"), unix.BPF_NOEXIST, false)
	if err != nil {
		return err
	}
	actor.sequence = seq
	t.tasks[actor.key] = actor
	r.taskCount++
	return nil
}

// activateEffectCgroup performs only the committed parent's fixed registry
// writes using the already retained, actually enrolled cgroup identity.
func (b *authorityBroker) activateEffectCgroup(g *liveCgroup, grant *brokerGrant, inv *runtimeInvocation, h *heldNotification, parent *runtimeEffectTransaction) error {
	if b == nil || b.effects == nil || b.effects.registry == nil || g == nil || grant == nil || inv == nil || h == nil || h.memory == nil || grant.phase != "admitted" || inv.command != "create" || inv.grant != grant || g.GrantID != grant.id || g.HeldFD < 0 || g.Retired {
		return errGuardProtocol
	}
	if parent == nil || parent.command != "PROG_ATTACH" {
		return errGuardProtocol
	}
	if err := b.beginNestedRegistry(parent, g, inv); err != nil {
		return err
	}
	defer b.endNestedRegistry(parent)
	if err := b.effectRegistryGuard(false); err != nil {
		return err
	}
	var st unix.Stat_t
	var fs unix.Statfs_t
	if unix.Fstat(g.HeldFD, &st) != nil || unix.Fstatfs(g.HeldFD, &fs) != nil || st.Mode&unix.S_IFMT != unix.S_IFDIR || uint64(fs.Type) != unix.CGROUP2_SUPER_MAGIC {
		return errGuardProtocol
	}
	if b.effects.cgroups[grant.id] != g || g.RootCookie == 0 || g.KernfsID == 0 || g.Enrollment == 0 {
		return errGuardProtocol
	}
	var err error
	r := b.effects.registry
	key := cgroupKey{root: g.RootCookie, id: g.KernfsID}
	if r.root != 0 && r.root != g.RootCookie {
		b.checkpoint.markFault()
		return errGuardProtocol
	}
	if prior := r.targets[key]; prior != nil {
		if prior.group != g || prior.grant != grant || prior.phase != 2 || prior.offlineSequence != 0 {
			return errGuardProtocol
		}
		if err = b.effectRegistryReadback("realm_targets", effectRegistryTargetKey(g), effectRegistryTargetValue(prior), prior.sequence); err != nil {
			return err
		}
		systemd, actorErr := b.effectRegistrySystemdActor(prior)
		if actorErr != nil {
			return actorErr
		}
		if err = b.effectRegistryAddActor(prior, systemd); err != nil {
			return err
		}
		runtimeActor, actorErr := b.effectRegistryRuntimeActor(prior, inv)
		if actorErr != nil {
			return actorErr
		}
		return b.effectRegistryAddActor(prior, runtimeActor)
	}
	if len(r.targets) >= 128 {
		return errGuardProtocol
	}
	retirement := uint32(0)
	switch grant.retirement {
	case "auto_empty":
		retirement = 1
	case "explicit_cleanup":
		retirement = 2
	default:
		return errGuardProtocol
	}
	t := &effectRegistryTarget{group: g, grant: grant, phase: 1, retirement: retirement, tasks: make(map[effectRegistryTaskKey]*effectRegistryTask)}
	systemd, err := b.effectRegistrySystemdActor(t)
	if err != nil {
		return err
	}
	runtimeActor, err := b.effectRegistryRuntimeActor(t, inv)
	if err != nil {
		return err
	}

	seq, err := b.effectRegistryWrite("realm_targets", effectRegistryTargetKey(g), effectRegistryTargetValue(t), effectRegistryTargetRecord(t, "insert"), unix.BPF_NOEXIST, false)
	if err != nil {
		return err
	}
	t.sequence = seq
	r.targets[key] = t
	r.root = g.RootCookie
	if err = b.effectRegistryAddActor(t, systemd); err != nil {
		b.checkpoint.markFault()
		return err
	}
	if err = b.effectRegistryAddActor(t, runtimeActor); err != nil {
		b.checkpoint.markFault()
		return err
	}
	t.phase = 2
	seq, err = b.effectRegistryWrite("realm_targets", effectRegistryTargetKey(g), effectRegistryTargetValue(t), effectRegistryTargetRecord(t, "update"), unix.BPF_EXIST, false)
	if err != nil {
		return err
	}
	t.sequence = seq
	return nil
}

// The native probes also report unrelated cgroups. Only enrolled identities
// belong to this registry; an enrolled target must retire in observed order.
func (b *authorityBroker) observeEffectCgroup(record guardRecord) error {
	if b == nil || b.effects == nil || b.effects.registry == nil || (record.Kind != 5 && record.Kind != 6) || record.Sequence == 0 || record.RootCookie == 0 || record.CgroupID == 0 {
		return errGuardProtocol
	}
	var group *liveCgroup
	for _, candidate := range b.effects.cgroups {
		if candidate.RootCookie == record.RootCookie && candidate.KernfsID == record.CgroupID {
			if group != nil || candidate.Retired {
				return errGuardProtocol
			}
			group = candidate
		}
	}
	if group != nil {
		if record.Kind == 5 {
			if group.OfflineSeq != 0 || group.ReleaseSeq != 0 {
				return errGuardProtocol
			}
			group.OfflineSeq = record.Sequence
		} else {
			if group.OfflineSeq == 0 || group.ReleaseSeq != 0 || record.Sequence <= group.OfflineSeq {
				return errGuardProtocol
			}
			group.ReleaseSeq = record.Sequence
		}
	}
	t := b.effects.registry.targets[cgroupKey{root: record.RootCookie, id: record.CgroupID}]
	if t == nil {
		return nil
	}
	if t.phase != 2 && t.phase != 3 || t.retirement == 0 || t.group.Retired {
		return errGuardProtocol
	}
	if record.Kind == 5 {
		if t.offlineSequence != 0 || t.releaseSequence != 0 {
			return errGuardProtocol
		}
		t.offlineSequence = record.Sequence
	} else {
		if t.offlineSequence == 0 || t.releaseSequence != 0 || record.Sequence <= t.offlineSequence {
			return errGuardProtocol
		}
		t.releaseSequence = record.Sequence
	}
	return b.observeAttachmentCgroup(record)
}

// authorizeBoundDelete installs the bound wrapper's held-runc permit for a
// live target. An observed offline target needs no new permit; retirement
// remains governed by the separately observed release and settlement.
func (b *authorityBroker) authorizeBoundDelete(grant *brokerGrant, task guardTask, pidfd int, invocation uint64, diagnostic *brokerBindDiagnostic) error {
	if diagnostic != nil {
		diagnostic.deletePoint = brokerDeletePointUnknown
		diagnostic.targetPoint = brokerTargetPointUnknown
	}
	if b == nil {
		return diagnostic.rejectDelete(brokerDeletePointBrokerNil, errGuardProtocol)
	}
	if b.effects == nil {
		return diagnostic.rejectDelete(brokerDeletePointEffectsNil, errGuardProtocol)
	}
	if b.checkpoint == nil {
		return diagnostic.rejectDelete(brokerDeletePointCheckpointNil, errGuardProtocol)
	}
	if b.checkpoint.lifecycle == nil {
		return diagnostic.rejectDelete(brokerDeletePointLifecycleNil, errGuardProtocol)
	}
	if grant == nil {
		return diagnostic.rejectDelete(brokerDeletePointGrantNil, errGuardProtocol)
	}
	if invocation == 0 {
		return diagnostic.rejectDelete(brokerDeletePointInvocationZero, errGuardProtocol)
	}
	if task.TID != task.TGID {
		return diagnostic.rejectDelete(brokerDeletePointTaskNotLeader, errGuardProtocol)
	}
	if task.TGID <= 1 {
		return diagnostic.rejectDelete(brokerDeletePointTaskTGID, errGuardProtocol)
	}
	if !pidfdSelects(pidfd, task.TGID) {
		return diagnostic.rejectDelete(brokerDeletePointEntryPIDFD, errGuardProtocol)
	}
	group := b.effects.cgroups[grant.id]
	if group == nil || group.Retired {
		return nil
	}
	target, err := b.effectRegistryTarget(group, grant, diagnostic)
	if err != nil {
		return diagnostic.rejectDelete(brokerDeletePointTarget, errGuardProtocol)
	}
	if target.phase != 2 && target.phase != 3 {
		return diagnostic.rejectDelete(brokerDeletePointTargetPhase, errGuardProtocol)
	}
	if target.offlineSequence != 0 || group.OfflineSeq != 0 || target.releaseSequence != 0 || group.ReleaseSeq != 0 {
		if target.offlineSequence == 0 || group.OfflineSeq != target.offlineSequence ||
			group.ReleaseSeq != target.releaseSequence ||
			(target.releaseSequence != 0 && target.releaseSequence <= target.offlineSequence) {
			return diagnostic.rejectDelete(brokerDeletePointTargetOffline, errGuardProtocol)
		}
		return nil
	}
	lineage := b.checkpoint.lifecycle.lineage
	if lineage == nil {
		return diagnostic.rejectDelete(brokerDeletePointLineageNil, errGuardProtocol)
	}
	var actor *effectRegistryTask
	for key, executable := range lineage.executables {
		if executable.role != "runc" {
			continue
		}
		if actor != nil {
			return diagnostic.rejectDelete(brokerDeletePointRuncDuplicate, errGuardProtocol)
		}
		if executable.file == nil {
			return diagnostic.rejectDelete(brokerDeletePointRuncFileNil, errGuardProtocol)
		}
		if executable.digest != runtimeRuncSHA256 {
			return diagnostic.rejectDelete(brokerDeletePointRuncRegisteredDigest, errGuardProtocol)
		}
		digest, stat, e := heldRegularDigest(int(executable.file.Fd()), 128<<20)
		dev, devErr := kernelDevice(uint64(stat.Dev))
		if e != nil {
			return diagnostic.rejectDelete(brokerDeletePointRuncHeld, errGuardProtocol)
		}
		if devErr != nil {
			return diagnostic.rejectDelete(brokerDeletePointRuncDevice, errGuardProtocol)
		}
		if digest != runtimeRuncSHA256 {
			return diagnostic.rejectDelete(brokerDeletePointRuncHeldDigest, errGuardProtocol)
		}
		if dev != key.dev {
			return diagnostic.rejectDelete(brokerDeletePointRuncDeviceID, errGuardProtocol)
		}
		if stat.Ino != key.ino {
			return diagnostic.rejectDelete(brokerDeletePointRuncInodeID, errGuardProtocol)
		}
		if !pidfdSelects(pidfd, task.TGID) {
			return diagnostic.rejectDelete(brokerDeletePointRuncPIDFD, errGuardProtocol)
		}
		actor = &effectRegistryTask{key: effectRegistryTaskKey{tgid: task.TGID, start: task.StartBoottimeNS}, invocation: invocation, dev: dev, ino: stat.Ino}
	}
	if actor == nil {
		return diagnostic.rejectDelete(brokerDeletePointRuncMissing, errGuardProtocol)
	}
	previouslyObserved := false
	for _, existing := range target.tasks {
		if existing.invocation != 0 && existing.dev == actor.dev && existing.ino == actor.ino {
			previouslyObserved = true
		}
	}
	if !previouslyObserved {
		return diagnostic.rejectDelete(brokerDeletePointPriorActorMissing, errGuardProtocol)
	}
	return diagnostic.rejectDelete(brokerDeletePointAddActor, b.effectRegistryAddActor(target, actor))
}

// retireBoundDeleteActor revokes even an unused pre-exec permit. The delete
// and absence readback are real registry transactions, not evidence of rmdir.
func (b *authorityBroker) retireBoundDeleteActor(inv *runtimeInvocation) error {
	if inv == nil || inv.command != "delete" || b.effects == nil || b.effects.registry == nil {
		return nil
	}
	group := b.effects.cgroups[inv.grantID]
	if group == nil {
		return nil
	}
	target := b.effects.registry.targets[cgroupKey{root: group.RootCookie, id: group.KernfsID}]
	if target == nil {
		return nil
	}
	key := effectRegistryTaskKey{tgid: inv.tgid, start: inv.startNS}
	actor := target.tasks[key]
	if actor == nil {
		return nil
	}
	if actor.invocation != inv.id {
		return errGuardProtocol
	}
	encoded, _ := effectRegistryTaskBytes(target, actor)
	cleanup := b.checkpoint.invalid
	if err := b.effectRegistryGuard(cleanup); err != nil {
		return err
	}
	sequence, _, err := b.executeRegistryMap(unix.BPF_MAP_DELETE_ELEM, "retire_tasks", encoded, nil, 0, effectRegistryTaskRecord(target, actor, "delete"), 0, cleanup, nil)
	if err != nil {
		return err
	}
	if err = b.lookupRegistryAbsent("retire_tasks", encoded, sequence, cleanup); err != nil {
		return err
	}
	delete(target.tasks, key)
	b.effects.registry.taskCount--
	return nil
}

func (b *authorityBroker) authorizeEffectRuntimeRetirement(g *liveCgroup, grant *brokerGrant, inv *runtimeInvocation, parent *runtimeEffectTransaction) error {
	if parent == nil || parent.command != "PROG_DETACH" {
		return errGuardProtocol
	}
	if err := b.beginNestedRegistry(parent, g, inv); err != nil {
		return err
	}
	defer b.endNestedRegistry(parent)
	t, err := b.effectRegistryTarget(g, grant, nil)
	if err != nil {
		return err
	}
	if t.phase != 2 && t.phase != 3 {
		return errGuardProtocol
	}
	actor, err := b.effectRegistryRuntimeActor(t, inv)
	if err != nil {
		return err
	}
	return b.effectRegistryAddActor(t, actor)
}

// Mark settling before any granted cleanup. The map continues to permit only
// the exact recorded actors; this transition does not assert offline or release.
func (b *authorityBroker) beginEffectCgroupSettlement(g *liveCgroup, grant *brokerGrant) error {
	t, err := b.effectRegistryTarget(g, grant, nil)
	if err != nil {
		return err
	}
	if grant.phase != "settling" && !b.revokedCleanupAllowed(grant) || t.phase != 2 {
		return errGuardProtocol
	}
	t.phase = 3
	seq, err := b.effectRegistryWrite("realm_targets", effectRegistryTargetKey(g), effectRegistryTargetValue(t), effectRegistryTargetRecord(t, "update"), unix.BPF_EXIST, true)
	if err != nil {
		return err
	}
	t.sequence = seq
	return nil
}

// Called only after the actual cgroup release event has been observed. First
// revoke every actor and read back the retired target tombstone, then ask the
// guard to release its retained reference and finally close the userspace FD.
func (b *authorityBroker) settleEffectCgroup(g *liveCgroup, grant *brokerGrant) error {
	t, err := b.effectRegistryTarget(g, grant, nil)
	if err != nil {
		return err
	}
	if grant.phase != "settling" && !b.revokedCleanupAllowed(grant) || t.phase != 3 || t.offlineSequence == 0 || t.releaseSequence <= t.offlineSequence {
		return errGuardProtocol
	}
	keys := make([]effectRegistryTaskKey, 0, len(t.tasks))
	for k := range t.tasks {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].tgid != keys[j].tgid {
			return keys[i].tgid < keys[j].tgid
		}
		return keys[i].start < keys[j].start
	})
	for _, k := range keys {
		actor := t.tasks[k]
		key, _ := effectRegistryTaskBytes(t, actor)
		if err = b.effectRegistryGuard(true); err != nil {
			return err
		}
		deleted, _, deleteErr := b.executeRegistryMap(unix.BPF_MAP_DELETE_ELEM, "retire_tasks", key, nil, 0, effectRegistryTaskRecord(t, actor, "delete"), 0, true, nil)
		if deleteErr != nil {
			b.checkpoint.markFault()
			return deleteErr
		}
		if err = b.lookupRegistryAbsent("retire_tasks", key, deleted, true); err != nil {
			b.checkpoint.markFault()
			return err
		}
		delete(t.tasks, k)
		b.effects.registry.taskCount--
	}
	if b.checkpoint.invalid && !b.canObserveOwnerDeath() {
		return errGuardProtocol
	}
	t.phase = 4
	seq, err := b.effectRegistryWrite("realm_targets", effectRegistryTargetKey(g), effectRegistryTargetValue(t), effectRegistryTargetRecord(t, "update"), unix.BPF_EXIST, true)
	if err != nil {
		return err
	}
	t.sequence = seq
	observed, err := b.authority.guard.retireCgroup(g.Enrollment)
	if err != nil || observed.State != 2 || observed.Sequence != t.releaseSequence {
		b.checkpoint.markFault()
		return errGuardProtocol
	}
	if err = unix.Close(g.HeldFD); err != nil {
		b.checkpoint.markFault()
		return err
	}
	g.HeldFD = -1
	g.Retired = true
	return nil
}
