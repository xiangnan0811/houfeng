//go:build linux && amd64 && !cgo

package main

type brokerControlOperation uint8
type brokerControlCommand uint8
type brokerControlPoint uint8
type brokerControlTerminalRoute uint8

type brokerBindPoint uint8
type brokerBindCheckpoint uint8
type brokerDeletePoint uint8
type brokerTargetPoint uint8

type brokerBindDiagnostic struct {
	point         brokerBindPoint
	before, after brokerBindCheckpoint
	deletePoint   brokerDeletePoint
	targetPoint   brokerTargetPoint
}

const (
	brokerTargetPointUnknown            brokerTargetPoint = 0
	brokerTargetPointBrokerNil          brokerTargetPoint = 1
	brokerTargetPointEffectsNil         brokerTargetPoint = 2
	brokerTargetPointRegistryNil        brokerTargetPoint = 3
	brokerTargetPointGroupNil           brokerTargetPoint = 4
	brokerTargetPointGrantNil           brokerTargetPoint = 5
	brokerTargetPointHeldFD             brokerTargetPoint = 6
	brokerTargetPointGroupRetired       brokerTargetPoint = 7
	brokerTargetPointGroupGrantID       brokerTargetPoint = 8
	brokerTargetPointEnrollment         brokerTargetPoint = 9
	brokerTargetPointRootCookie         brokerTargetPoint = 10
	brokerTargetPointKernfsID           brokerTargetPoint = 11
	brokerTargetPointGrantIDZero        brokerTargetPoint = 12
	brokerTargetPointTargetMissing      brokerTargetPoint = 13
	brokerTargetPointGroupIdentity      brokerTargetPoint = 14
	brokerTargetPointGrantIdentity      brokerTargetPoint = 15
	brokerTargetPointTargetPhaseRetired brokerTargetPoint = 16
)

const (
	brokerDeletePointUnknown              brokerDeletePoint = 0
	brokerDeletePointBrokerNil            brokerDeletePoint = 1
	brokerDeletePointEffectsNil           brokerDeletePoint = 2
	brokerDeletePointCheckpointNil        brokerDeletePoint = 3
	brokerDeletePointLifecycleNil         brokerDeletePoint = 4
	brokerDeletePointGrantNil             brokerDeletePoint = 5
	brokerDeletePointInvocationZero       brokerDeletePoint = 6
	brokerDeletePointTaskNotLeader        brokerDeletePoint = 7
	brokerDeletePointTaskTGID             brokerDeletePoint = 8
	brokerDeletePointEntryPIDFD           brokerDeletePoint = 9
	brokerDeletePointTarget               brokerDeletePoint = 10
	brokerDeletePointTargetPhase          brokerDeletePoint = 11
	brokerDeletePointTargetOffline        brokerDeletePoint = 12
	brokerDeletePointLineageNil           brokerDeletePoint = 13
	brokerDeletePointRuncDuplicate        brokerDeletePoint = 14
	brokerDeletePointRuncFileNil          brokerDeletePoint = 15
	brokerDeletePointRuncRegisteredDigest brokerDeletePoint = 16
	brokerDeletePointRuncHeld             brokerDeletePoint = 17
	brokerDeletePointRuncDevice           brokerDeletePoint = 18
	brokerDeletePointRuncHeldDigest       brokerDeletePoint = 19
	brokerDeletePointRuncDeviceID         brokerDeletePoint = 20
	brokerDeletePointRuncInodeID          brokerDeletePoint = 21
	brokerDeletePointRuncPIDFD            brokerDeletePoint = 22
	brokerDeletePointRuncMissing          brokerDeletePoint = 23
	brokerDeletePointPriorActorMissing    brokerDeletePoint = 24
	brokerDeletePointAddActor             brokerDeletePoint = 25
)

const (
	brokerBindCheckpointMissing        brokerBindCheckpoint = 0
	brokerBindCheckpointClean          brokerBindCheckpoint = 1
	brokerBindCheckpointInvalid        brokerBindCheckpoint = 2
	brokerBindCheckpointFaulted        brokerBindCheckpoint = 3
	brokerBindCheckpointInvalidFaulted brokerBindCheckpoint = 4
)

const (
	brokerBindPointUnknown            brokerBindPoint = 0
	brokerBindPointBrokerNil          brokerBindPoint = 1
	brokerBindPointCheckpointNil      brokerBindPoint = 2
	brokerBindPointCheckpointInvalid  brokerBindPoint = 3
	brokerBindPointCheckpointActive   brokerBindPoint = 4
	brokerBindPointPendingCheckpoint  brokerBindPoint = 5
	brokerBindPointPendingAdmit       brokerBindPoint = 6
	brokerBindPointPendingExec        brokerBindPoint = 7
	brokerBindPointAuthorityNil       brokerBindPoint = 8
	brokerBindPointLifecycleNil       brokerBindPoint = 9
	brokerBindPointRequest            brokerBindPoint = 10
	brokerBindPointBody               brokerBindPoint = 11
	brokerBindPointCommand            brokerBindPoint = 12
	brokerBindPointFeatures           brokerBindPoint = 13
	brokerBindPointContainerID        brokerBindPoint = 14
	brokerBindPointRightsCredential   brokerBindPoint = 15
	brokerBindPointPeer               brokerBindPoint = 16
	brokerBindPointTaskPin            brokerBindPoint = 17
	brokerBindPointTaskIdentity       brokerBindPoint = 18
	brokerBindPointReplayGuard        brokerBindPoint = 19
	brokerBindPointReplayRight        brokerBindPoint = 20
	brokerBindPointPIDFDOpen          brokerBindPoint = 21
	brokerBindPointProcessObserve     brokerBindPoint = 22
	brokerBindPointProcessIdentity    brokerBindPoint = 23
	brokerBindPointArgv               brokerBindPoint = 24
	brokerBindPointRuntimeArgs        brokerBindPoint = 25
	brokerBindPointListener           brokerBindPoint = 26
	brokerBindPointGuardDrain         brokerBindPoint = 27
	brokerBindPointLineageMissing     brokerBindPoint = 28
	brokerBindPointAncestry           brokerBindPoint = 29
	brokerBindPointNodeIdentity       brokerBindPoint = 30
	brokerBindPointAncestorMissing    brokerBindPoint = 31
	brokerBindPointRequiredAncestors  brokerBindPoint = 32
	brokerBindPointDaemonDuplicate    brokerBindPoint = 33
	brokerBindPointDaemon             brokerBindPoint = 34
	brokerBindPointGrantDuplicate     brokerBindPoint = 35
	brokerBindPointRetiredDuplicate   brokerBindPoint = 36
	brokerBindPointWrongGeneration    brokerBindPoint = 37
	brokerBindPointGrant              brokerBindPoint = 38
	brokerBindPointAttachmentReplace  brokerBindPoint = 39
	brokerBindPointOwner              brokerBindPoint = 40
	brokerBindPointDaemonOwner        brokerBindPoint = 41
	brokerBindPointCreatePolicy       brokerBindPoint = 42
	brokerBindPointCreateDigest       brokerBindPoint = 43
	brokerBindPointCreateBundle       brokerBindPoint = 44
	brokerBindPointStartPolicy        brokerBindPoint = 45
	brokerBindPointStartBundle        brokerBindPoint = 46
	brokerBindPointStatePolicy        brokerBindPoint = 47
	brokerBindPointStateBundle        brokerBindPoint = 48
	brokerBindPointKillDeletePolicy   brokerBindPoint = 49
	brokerBindPointKillDeleteBundle   brokerBindPoint = 50
	brokerBindPointExecPolicy         brokerBindPoint = 51
	brokerBindPointExecDigest         brokerBindPoint = 52
	brokerBindPointExecBundle         brokerBindPoint = 53
	brokerBindPointExecProcess        brokerBindPoint = 54
	brokerBindPointCommandUnsupported brokerBindPoint = 55
	brokerBindPointLiveCapacity       brokerBindPoint = 56
	brokerBindPointListenerCollision  brokerBindPoint = 57
	brokerBindPointPrincipalID        brokerBindPoint = 58
	brokerBindPointInvocationID       brokerBindPoint = 59
	brokerBindPointListenerID         brokerBindPoint = 60
	brokerBindPointPrincipalAppend    brokerBindPoint = 61
	brokerBindPointInvocationAppend   brokerBindPoint = 62
	brokerBindPointDeleteAuthorize    brokerBindPoint = 63
	brokerBindPointACK                brokerBindPoint = 64
	brokerBindPointExecAdmission      brokerBindPoint = 65
)

func brokerBindCheckpointState(b *authorityBroker) brokerBindCheckpoint {
	if b == nil {
		return brokerBindCheckpointMissing
	}
	cp := b.checkpoint
	if cp == nil {
		return brokerBindCheckpointMissing
	}
	if cp.invalid {
		if cp.faulted {
			return brokerBindCheckpointInvalidFaulted
		}
		return brokerBindCheckpointInvalid
	}
	if cp.faulted {
		return brokerBindCheckpointFaulted
	}
	return brokerBindCheckpointClean
}

func (d *brokerBindDiagnostic) begin(b *authorityBroker) {
	if d != nil {
		*d = brokerBindDiagnostic{before: brokerBindCheckpointState(b)}
	}
}

func (d *brokerBindDiagnostic) reject(point brokerBindPoint, err error) error {
	if d != nil && err != nil && d.point == brokerBindPointUnknown {
		d.point = point
	}
	return err
}

func (d *brokerBindDiagnostic) rejectDelete(point brokerDeletePoint, err error) error {
	if d != nil && err != nil && point != brokerDeletePointUnknown && d.deletePoint == brokerDeletePointUnknown {
		d.deletePoint = point
	}
	return err
}

func (d *brokerBindDiagnostic) rejectTarget(point brokerTargetPoint, err error) error {
	if d != nil && err != nil && point != brokerTargetPointUnknown && d.targetPoint == brokerTargetPointUnknown {
		d.targetPoint = point
	}
	return err
}

func (d *brokerBindDiagnostic) finish(b *authorityBroker, err error) {
	if d == nil {
		return
	}
	if err == nil {
		*d = brokerBindDiagnostic{}
		return
	}
	d.after = brokerBindCheckpointState(b)
}

const (
	brokerControlOpUnknown    brokerControlOperation = 0
	brokerControlOpLaunch     brokerControlOperation = 1
	brokerControlOpBind       brokerControlOperation = 2
	brokerControlOpRegister   brokerControlOperation = 3
	brokerControlOpAdmit      brokerControlOperation = 4
	brokerControlOpSettle     brokerControlOperation = 5
	brokerControlOpAdmitExec  brokerControlOperation = 6
	brokerControlOpTrackExec  brokerControlOperation = 7
	brokerControlOpCancelExec brokerControlOperation = 8
	brokerControlOpEndpoint   brokerControlOperation = 9
	brokerControlOpSelftest   brokerControlOperation = 10
	brokerControlOpCheckpoint brokerControlOperation = 11
	brokerControlOpRead       brokerControlOperation = 12
	brokerControlOpOther      brokerControlOperation = 13
)
const (
	brokerControlCmdUnknown  brokerControlCommand = 0
	brokerControlCmdCreate   brokerControlCommand = 1
	brokerControlCmdStart    brokerControlCommand = 2
	brokerControlCmdState    brokerControlCommand = 3
	brokerControlCmdDelete   brokerControlCommand = 4
	brokerControlCmdKill     brokerControlCommand = 5
	brokerControlCmdExec     brokerControlCommand = 6
	brokerControlCmdFeatures brokerControlCommand = 7
	brokerControlCmdOther    brokerControlCommand = 8
)
const (
	brokerControlPointUnknown           brokerControlPoint = 0
	brokerControlPointReceive           brokerControlPoint = 1
	brokerControlPointParse             brokerControlPoint = 2
	brokerControlPointOperation         brokerControlPoint = 3
	brokerControlPointObservationGate   brokerControlPoint = 4
	brokerControlPointRootGate          brokerControlPoint = 5
	brokerControlPointRootRecheck       brokerControlPoint = 6
	brokerControlPointContainerGate     brokerControlPoint = 7
	brokerControlPointPendingReply      brokerControlPoint = 8
	brokerControlPointLaunchHandler     brokerControlPoint = 9
	brokerControlPointStartQueue        brokerControlPoint = 10
	brokerControlPointBindHandler       brokerControlPoint = 11
	brokerControlPointAuthenticate      brokerControlPoint = 12
	brokerControlPointAuthDenial        brokerControlPoint = 13
	brokerControlPointReplayACK         brokerControlPoint = 14
	brokerControlPointReplayPostACK     brokerControlPoint = 15
	brokerControlPointAuthenticatedGate brokerControlPoint = 16
	brokerControlPointLineageInstall    brokerControlPoint = 17
	brokerControlPointHandlerGuard      brokerControlPoint = 18
	brokerControlPointHandler           brokerControlPoint = 19
	brokerControlPointDenialGuard       brokerControlPoint = 20
	brokerControlPointDenialAppend      brokerControlPoint = 21
	brokerControlPointDenialReply       brokerControlPoint = 22
	brokerControlPointACKEmpty          brokerControlPoint = 23
	brokerControlPointTaskDrop          brokerControlPoint = 24
	brokerControlPointACKDeliver        brokerControlPoint = 25
	brokerControlPointPostACK           brokerControlPoint = 26
	brokerControlPointDeferDenialAppend brokerControlPoint = 27
	brokerControlPointDeferDenialSend   brokerControlPoint = 28
)
const (
	brokerControlRouteJoinReport brokerControlTerminalRoute = 1
	brokerControlRouteFaulted    brokerControlTerminalRoute = 2
	brokerControlRouteNewInvalid brokerControlTerminalRoute = 3
)

type brokerControlDiagnostic struct {
	operation       brokerControlOperation
	command         brokerControlCommand
	first, returned brokerControlPoint
	drop            uint8
	bind            brokerBindDiagnostic
}

func brokerControlOperationFor(op string) brokerControlOperation {
	switch op {
	case "REGISTER_LAUNCH":
		return brokerControlOpLaunch
	case "BIND_RUNTIME":
		return brokerControlOpBind
	case "REGISTER_REALM", "REGISTER_RECOVERY", "REGISTER_DAEMON":
		return brokerControlOpRegister
	case "ADMIT":
		return brokerControlOpAdmit
	case "SETTLE":
		return brokerControlOpSettle
	case "ADMIT_EXEC":
		return brokerControlOpAdmitExec
	case "TRACK_EXEC":
		return brokerControlOpTrackExec
	case "CANCEL_EXEC":
		return brokerControlOpCancelExec
	case "BEGIN_ENDPOINT_TRANSITION", "END_ENDPOINT_TRANSITION":
		return brokerControlOpEndpoint
	case "SELFTEST_FD_ARM", "REGISTER_SELFTEST_PROBE", "SELFTEST_RUNTIME_ARM", "SELFTEST_BASELINE_STEP", "SELFTEST_REVOKE_AND_CLOSE", "SELFTEST_OBSERVATION_FAULT":
		return brokerControlOpSelftest
	case "QUIESCE", "RELEASE", "AUDIT_CUT":
		return brokerControlOpCheckpoint
	case "READ_STATUS", "READ_EVIDENCE":
		return brokerControlOpRead
	default:
		return brokerControlOpOther
	}
}

func brokerControlCommandFor(request map[string]any, operation brokerControlOperation) brokerControlCommand {
	if operation != brokerControlOpBind {
		return brokerControlCmdUnknown
	}
	body, ok := request["body"].(map[string]any)
	if !ok {
		return brokerControlCmdUnknown
	}
	command, ok := body["command"].(string)
	if !ok {
		return brokerControlCmdUnknown
	}
	switch command {
	case "create":
		return brokerControlCmdCreate
	case "start":
		return brokerControlCmdStart
	case "state":
		return brokerControlCmdState
	case "delete":
		return brokerControlCmdDelete
	case "kill":
		return brokerControlCmdKill
	case "exec":
		return brokerControlCmdExec
	case "features":
		return brokerControlCmdFeatures
	default:
		return brokerControlCmdOther
	}
}

func (d *brokerControlDiagnostic) reject(point brokerControlPoint, err error) error {
	if d != nil && err != nil {
		if d.first == 0 {
			d.first = point
		}
		d.returned = point
	}
	return err
}

func (d *brokerControlDiagnostic) finish(err error) {
	if d != nil && err == nil {
		*d = brokerControlDiagnostic{}
	}
}

func (d *brokerControlDiagnostic) afterTaskDrop(prior, drop error) error {
	if prior != nil {
		return prior
	}
	return d.reject(brokerControlPointTaskDrop, drop)
}

func (b *authorityBroker) finishBrokerControlTask(task guardTask, held *bool, diagnostic *brokerControlDiagnostic) error {
	if !*held {
		return nil
	}
	*held = false
	err := b.closeProcessTask(task)
	if diagnostic != nil {
		if err == nil {
			diagnostic.drop = 1
		} else {
			diagnostic.drop = 2
		}
	}
	if err != nil {
		b.checkpoint.markFault()
	}
	return err
}
