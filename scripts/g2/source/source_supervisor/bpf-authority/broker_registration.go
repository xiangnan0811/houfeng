//go:build linux && amd64 && !cgo

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"golang.org/x/sys/unix"
	"math"
	"time"
)

type launchRegistration struct {
	launcherCookie uint64
	launcherPIDFD  int
	childPIDFD     int
	retainedFDs    []int
	request, ack   []byte
}

type daemonRegistration struct {
	owner, cookie, startNS, watchSequence uint64
	pid                                   uint32
	pidfd                                 int
	mode, state                           string
	helperSHA, dockerdSHA, argvSHA        string
}

type authorityBroker struct {
	manifest                               bootstrapManifest
	bootID, epoch                          string
	authority                              *sealedAuthority
	parentTask                             guardTask
	runtimeRootState                       uint8
	runtimeRootFD                          int
	runtimeRootPIDFD                       int
	runtimeRootExeFD                       int
	runtimeRootIdentity                    procMountIdentity
	runtimeRootExeStat                     unix.Stat_t
	runtimeRootHandoffs                    uint64
	devNullFD                              int
	devNullCookie                          uint64
	devNullStat                            unix.Stat_t
	transcript                             *authorityTranscript
	audit                                  *auditMulticast
	join                                   *auditJoin
	resolver                               *brokerAuditResolver
	checkpoint                             *checkpointController
	reader                                 *auditReaderClient
	observerSocketFD                       int
	observerPIDFD                          int
	observerCookie                         uint64
	pendingCheckpoint                      *pendingCheckpointRequest
	pendingAdmit                           *pendingAdmit
	pendingSettlement                      *pendingBrokerSettlement
	pendingStart                           *pendingRuntimeStart
	pendingExecOp                          *pendingExecOperation
	pendingObservation                     *pendingObservationFault
	pendingContainerProbe                  *pendingContainerSelftestProbe
	pendingEndpoint                        *endpointTransitionState
	endpointGeneration, nextTransition     uint64
	endpointClosed                         bool
	admittedRecoveryID                     string
	serviceSecrets                         map[string]*postgresServiceSecretRecord
	execAdmissions                         map[uint64]*brokerExecAdmission
	auditCutDone                           bool
	recovery                               *recoveryBinding
	reads                                  *brokerReadState
	principals                             map[uint64]*controlPrincipal
	launches                               map[uint64]*launchRegistration
	daemons                                map[uint64]*daemonRegistration
	grants                                 map[uint64]*brokerGrant
	nextExec                               uint64
	runtimeBinds                           map[uint64]*runtimeBindReplay
	runtimeInvocations                     map[int]*runtimeInvocation
	featureInvocations                     map[uint64]*runtimeInvocation
	effects                                *runtimeEffects
	programLogScratch                      []byte
	nextListener                           uint64
	nextInvocation, nextFeatureGeneration  uint64
	nextGrant                              uint64
	postgresUID, postgresGID               uint32
	browserImage                           string
	nextPrincipal                          uint64
	bootOwner, recoveryPending             uint64
	caseFacts                              map[string]any
	selftestFD                             *selftestFDArm
	selftestMapGrant                       uint64
	selftestMapNonce                       string
	selftestMapDone, selftestProgramDone   bool
	selftestMapArmed, selftestProgramArmed bool
	selftestRuntime                        *selftestRuntimeArm
	selftestRuntimeModesUsed               map[string]bool
	selftestBaseline                       *selftestBaselineStep
	selftestBaselineUsed                   map[string]bool
	selftestFaultVariant                   string
	observationFailure                     *observationFailure
	nextProbeCookie                        uint64
	probeActors                            map[uint64]*selftestProbeActor
	probeListeners                         map[int]*selftestProbeActor
	revokeAndCloseDone                     bool
	pendingRevokeAndClose                  bool
}

func (b *authorityBroker) nextPrincipalID() (uint64, error) {
	if b.nextPrincipal == 0 || b.nextPrincipal == math.MaxUint64 {
		return 0, errGuardProtocol
	}
	id := b.nextPrincipal
	b.nextPrincipal++
	return id, nil
}

func (b *authorityBroker) appendPrincipal(p *controlPrincipal, process observedProcess, parentCookie uint64, scriptSHA, registration string) error {
	if p == nil || p.ID == 0 || p.TaskCookie == 0 || process.PID == 0 {
		return errGuardProtocol
	}
	var script any
	if scriptSHA != "" {
		script = scriptSHA
	}
	_, _, err := b.transcript.append("principal", map[string]any{
		"id": jsonU64(p.ID), "role": p.Role, "task_cookie": jsonU64(p.TaskCookie),
		"tgid": jsonU64(uint64(process.PID)), "tid": jsonU64(uint64(process.PID)), "start_ns": jsonU64(p.StartNS),
		"parent_cookie": jsonU64(parentCookie), "exe_sha256": process.ExecutableSHA256, "script_sha256": script,
		"argv_sha256": process.ArgvSHA256, "uid": jsonU64(uint64(process.UID)), "gid": jsonU64(uint64(process.GID)),
		"cgroup_sha256": process.CgroupSHA256, "registration": registration, "terminal": nil,
	})
	return err
}

func (b *authorityBroker) registerLaunch(fd int, raw []byte, request map[string]any, cred *unix.Ucred, rights []int) (_ []byte, _ bool, err error) {
	stage := "REGISTER_CHECK"
	if body, ok := request["body"].(map[string]any); ok && body["kind"] == "source" && g2d1.role == 'B' && g2d1.fd >= 0 {
		g2d1.registerFD = fd
		g2d1Mark("REGISTER_ENTRY")
	}
	defer func() {
		if err != nil && g2d1.registerFD == fd {
			g2d1Fail(stage, err)
		}
	}()
	if !exactKeys(request, "version", "boot_id", "request_id", "operation", "body") || request["version"] != json.Number("1") || request["boot_id"] != b.bootID || request["request_id"] != json.Number("1") || request["operation"] != "REGISTER_LAUNCH" {
		return nil, false, errControlFrame
	}
	body, ok := request["body"].(map[string]any)
	if !ok || !exactKeys(body, "kind", "child_pid", "child_starttime") {
		return nil, false, errControlFrame
	}
	kind, ok := body["kind"].(string)
	if !ok || kind != "source" && kind != "reboot" {
		return nil, false, errControlFrame
	}
	expectedFDs := 2
	if kind == "source" {
		expectedFDs = 4
	}
	if len(rights) != expectedFDs || cred == nil || cred.Pid <= 1 || cred.Uid != 0 || cred.Gid != 0 {
		return nil, false, errControlFrame
	}
	stage = "REGISTER_PEER"
	peer, err := unix.GetsockoptUcred(fd, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil || peer.Pid != cred.Pid || peer.Uid != cred.Uid || peer.Gid != cred.Gid {
		if g2d1.registerFD == fd {
			g2d1Fail(stage, err)
		}
		return nil, false, errControlFrame
	}
	childValue, ok := exactNumber(body["child_pid"])
	if !ok || childValue == 0 || childValue > math.MaxInt32 {
		return nil, false, errControlFrame
	}
	childStart, ok := exactNumber(body["child_starttime"])
	if !ok || childStart == 0 {
		return nil, false, errControlFrame
	}
	stage = "ROOT_INIT"
	if err := b.initializeRuntimeRoot(); err != nil {
		b.checkpoint.markFault()
		return nil, false, err
	}
	if g2d1.registerFD == fd {
		g2d1Mark("ROOT_READY")
	}
	stage = "LAUNCH_PIN"
	launcherTask, err := b.authority.guard.pinTask(uint32(cred.Pid))
	if err != nil {
		return nil, false, err
	}
	keepLauncher := false
	defer func() {
		if err != nil && g2d1.registerFD == fd {
			g2d1Fail(stage, err)
		}
		if !keepLauncher {
			_ = b.authority.guard.dropTask(launcherTask.Cookie)
		}
	}()
	stage = "LAUNCH_REPLAY"
	if old := b.launches[launcherTask.Cookie]; old != nil {
		if !bytes.Equal(old.request, raw) || len(old.retainedFDs) != len(rights) || !livePIDFD(old.launcherPIDFD) || !livePIDFD(old.childPIDFD) {
			return nil, false, errControlFrame
		}
		for i := range rights {
			if !sameHeldDescription(old.retainedFDs[i], rights[i]) {
				return nil, false, errControlFrame
			}
		}
		return old.ack, false, nil
	}
	if kind == "source" && b.bootOwner != 0 || kind == "reboot" && b.recoveryPending != 0 {
		return nil, false, errControlFrame
	}
	stage = "LAUNCH_PIDFD"
	launcherPIDFD, err := unix.PidfdOpen(int(cred.Pid), 0)
	if err != nil {
		return nil, false, err
	}
	keepLauncherFD := false
	defer func() {
		if err != nil && g2d1.registerFD == fd {
			g2d1Fail(stage, err)
		}
		if !keepLauncherFD {
			_ = unix.Close(launcherPIDFD)
		}
	}()
	stage = "LAUNCH_IDENTITY"
	scriptIndex := len(rights) - 1
	parent, child, err := verifyLaunchProcessPair(b.manifest, kind, cred, launcherPIDFD, rights[0], uint32(childValue), childStart, rights[scriptIndex])
	if err != nil {
		return nil, false, err
	}
	if kind == "source" {
		stage = "CARRIER"
		facts, err := verifySourceCarrier(rights[1], rights[2], b.manifest)
		if err != nil {
			return nil, false, err
		}
		image, ok := facts["_broker_browser_image"].(string)
		if !ok {
			return nil, false, errGuardProtocol
		}
		delete(facts, "_broker_browser_image")
		b.caseFacts, b.browserImage = facts, image
	}
	stage = "CHILD_PIN"
	childTask, err := b.authority.guard.pinTask(uint32(childValue))
	if err != nil {
		return nil, false, err
	}
	keepChild := false
	defer func() {
		if err != nil && g2d1.registerFD == fd {
			g2d1Fail(stage, err)
		}
		if !keepChild {
			_ = b.authority.guard.dropTask(childTask.Cookie)
		}
	}()
	if childTask.TGID != uint32(childValue) || childTask.TID != uint32(childValue) || !livePIDFD(rights[0]) || !livePIDFD(launcherPIDFD) {
		return nil, false, errGuardProtocol
	}
	stage = "PRINCIPAL"
	id, err := b.nextPrincipalID()
	if err != nil {
		return nil, false, err
	}
	role, registration, script := "BOOT_OWNER", "launch_source", b.manifest.Scripts["source_guest"]
	if kind == "reboot" {
		role, registration, script = "RECOVERY_PENDING", "launch_reboot", b.manifest.Scripts["reboot_observer"]
	}
	p := &controlPrincipal{ID: id, TaskCookie: childTask.Cookie, PIDFD: rights[0], PID: uint32(childValue), UID: 0, GID: 0, StartNS: childTask.StartBoottimeNS, Role: role, NextRequest: 1, ExecutableSHA256: child.ExecutableSHA256, ArgvSHA256: child.ArgvSHA256, CgroupSHA256: child.CgroupSHA256}
	if err = b.appendPrincipal(p, child, launcherTask.Cookie, script, registration); err != nil {
		return nil, false, err
	}
	stage = "ACK_BUILD"
	ack, err := controlACK(b.reads, p, 1, raw, "OK", map[string]any{"child_cookie": jsonU64(childTask.Cookie), "role": role})
	if err != nil {
		return nil, false, err
	}
	stage = "EXPORTER"
	if kind == "source" {
		if b.observerSocketFD >= 0 && len(rights) >= 3 {
			caseRaw, e := readHeldRegular(rights[2], 4096)
			if e != nil {
				if g2d1.registerFD == fd {
					g2d1Fail("EXPORTER_CASE", e)
				}
				b.checkpoint.markFault()
				return nil, false, errGuardProtocol
			}
			stage = "EXPORTER_DUP"
			dupLauncherFD, e := unix.FcntlInt(uintptr(launcherPIDFD), unix.F_DUPFD_CLOEXEC, 0)
			if e != nil {
				b.checkpoint.markFault()
				return nil, false, e
			}
			stage = "EXPORTER_DEADLINE"
			dl, e := guestDeadline(5 * time.Second)
			if e != nil {
				if g2d1.registerFD == fd {
					g2d1Fail(stage, e)
				}
				_ = unix.Close(dupLauncherFD)
				b.checkpoint.markFault()
				return nil, false, e
			}
			stage = "EXPORTER_SEND"
			e = earlyProbeSend(b.observerSocketFD, map[string]any{
				"version":     json.Number("1"),
				"boot_id":     b.bootID,
				"kind":        "bind_exporter",
				"pid":         jsonU64(uint64(parent.PID)),
				"task_cookie": jsonU64(launcherTask.Cookie),
				"start_ns":    jsonU64(launcherTask.StartBoottimeNS),
				"exe_sha256":  parent.ExecutableSHA256,
				"case_sha256": hashBytes(caseRaw),
				"epoch":       b.epoch,
			}, []int{dupLauncherFD}, dl)
			if e != nil && g2d1.registerFD == fd {
				g2d1Fail(stage, e)
			}
			_ = unix.Close(dupLauncherFD)
			if e != nil {
				b.checkpoint.markFault()
				return nil, false, e
			}
		}
		b.bootOwner = id
	} else {
		b.recoveryPending = id
	}
	retained := append([]int(nil), rights...)
	b.principals[id] = p
	b.launches[launcherTask.Cookie] = &launchRegistration{launcherCookie: launcherTask.Cookie, launcherPIDFD: launcherPIDFD, childPIDFD: rights[0], retainedFDs: retained, request: append([]byte(nil), raw...), ack: append([]byte(nil), ack...)}
	_ = parent
	if g2d1.registerFD == fd {
		g2d1.registered = true
	}
	keepLauncher, keepLauncherFD, keepChild = true, true, true
	return ack, true, nil
}

func (b *authorityBroker) authenticatePrincipal(fd int, raw []byte, request map[string]any, cred *unix.Ucred, rights []int) (*controlPrincipal, *controlReplay, guardTask, error) {
	var empty guardTask
	id, ok := exactNumber(request["principal_id"])
	if !ok || id == 0 {
		return nil, nil, empty, errControlFrame
	}
	p := b.principals[id]
	if p == nil || cred == nil {
		return nil, nil, empty, errControlFrame
	}
	peer, err := unix.GetsockoptUcred(fd, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil || peer.Pid != cred.Pid || peer.Uid != cred.Uid || peer.Gid != cred.Gid {
		return nil, nil, empty, errControlFrame
	}
	task, err := b.authority.guard.pinTask(uint32(cred.Pid))
	if err != nil {
		return nil, nil, empty, err
	}
	replay, err := p.authenticate(request, raw, cred, task, b.bootID, b.epoch, authorityRunToken, rights)
	if err != nil {
		_ = b.authority.guard.dropTask(task.Cookie)
		return nil, nil, empty, err
	}
	return p, replay, task, nil
}

func (b *authorityBroker) closeProcessTask(task guardTask) error {
	if task.Cookie == 0 {
		return fmt.Errorf("BPF_AUTH_UNAVAILABLE: missing task pin")
	}
	return b.authority.guard.dropTask(task.Cookie)
}
