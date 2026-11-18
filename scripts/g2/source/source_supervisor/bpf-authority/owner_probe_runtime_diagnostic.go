//go:build linux && amd64 && !cgo

package main

import (
	"encoding/json"
	"errors"
	"os"
	"strconv"

	"golang.org/x/sys/unix"
)

type ownerProbeRuntimeStage uint8

type ownerProbeMountOptionsDiagnostic struct {
	mount, actual, expected, difference uint8
}

type ownerProbeRuntimeDiagnostic struct {
	stage        ownerProbeRuntimeStage
	mountOptions ownerProbeMountOptionsDiagnostic
}

const (
	ownerProbeMountProc      uint8 = 1
	ownerProbeMountDev       uint8 = 2
	ownerProbeMountDevPts    uint8 = 3
	ownerProbeMountSys       uint8 = 4
	ownerProbeMountSysCgroup uint8 = 5
	ownerProbeMountDevMqueue uint8 = 6
	ownerProbeMountDevShm    uint8 = 7
	ownerProbeMountHostname  uint8 = 8
	ownerProbeMountHosts     uint8 = 9
	ownerProbeMountResolv    uint8 = 10
	ownerProbeMountPGDATA    uint8 = 11
	ownerProbeMountOther     uint8 = 12
)

const (
	ownerProbeOptionMissing           uint8 = 1
	ownerProbeOptionNull              uint8 = 2
	ownerProbeOptionNilArray          uint8 = 3
	ownerProbeOptionOtherType         uint8 = 4
	ownerProbeOptionEmpty             uint8 = 5
	ownerProbeOptionNosuidNoexecNodev uint8 = 6
	ownerProbeOptionDevTmpfs          uint8 = 7
	ownerProbeOptionDevpts            uint8 = 8
	ownerProbeOptionSysfsRo           uint8 = 9
	ownerProbeOptionCgroupRo          uint8 = 10
	ownerProbeOptionShm64M            uint8 = 11
	ownerProbeOptionRbindRoRprivate   uint8 = 12
	ownerProbeOptionRbindRroRprivate  uint8 = 13
	ownerProbeOptionRbindRprivate     uint8 = 14
	ownerProbeOptionOtherStringArray  uint8 = 15
	ownerProbeOptionNonStringArray    uint8 = 16
	ownerProbeOptionLimit             uint8 = 17
)

const (
	ownerProbeDifferenceMissing   uint8 = 1
	ownerProbeDifferenceType      uint8 = 2
	ownerProbeDifferenceNilArray  uint8 = 3
	ownerProbeDifferenceLength    uint8 = 4
	ownerProbeDifferenceOrder     uint8 = 5
	ownerProbeDifferenceValue     uint8 = 6
	ownerProbeDifferenceNonString uint8 = 7
	ownerProbeDifferenceLimit     uint8 = 8
	ownerProbeDifferenceOther     uint8 = 9
)

const (
	ownerProbeStageWrapperEntry ownerProbeRuntimeStage = 1 + iota
	ownerProbeStageWrapperProc
	ownerProbeStageWrapperDeadline
	ownerProbeStageWrapperIdentity
	ownerProbeStageWrapperArgs
	ownerProbeStageWrapperRuncOpen
	ownerProbeStageWrapperRuncIdentity
	ownerProbeStageWrapperBootID
	ownerProbeStageWrapperInputDir
	ownerProbeStageWrapperBundlePrepare
	ownerProbeStageWrapperInputFile
	ownerProbeStageWrapperInputStat
	ownerProbeStageWrapperInputBeforeBind
	ownerProbeStageWrapperConnect
	ownerProbeStageWrapperListener
	ownerProbeStageWrapperBind
	ownerProbeStageWrapperInputAfterBind
	ownerProbeStageWrapperRuncReopen
	ownerProbeStageWrapperRuncRecheck
	ownerProbeStageWrapperRuncCheckClose
	ownerProbeStageWrapperListenerClose
	ownerProbeStageWrapperControlClose
	ownerProbeStageWrapperPreExecLive
	ownerProbeStageWrapperExecReturn
)

const (
	ownerProbeStageBrokerCreatePolicy ownerProbeRuntimeStage = 32 + iota
	ownerProbeStageBrokerBundleInput
	ownerProbeStageBrokerBundleDir
	ownerProbeStageBrokerBundleConfig
	ownerProbeStageBrokerBundleJSON
	ownerProbeStageBrokerBundleMaterialize
	ownerProbeStageBrokerBundleExpected
	ownerProbeStageBrokerBundleCompare
	ownerProbeStageBrokerBundleDeviceRules
	ownerProbeStageBrokerBundleIdentities
	ownerProbeStageBrokerBundleUserFiles
	ownerProbeStageBrokerBundleRecheck
	ownerProbeStageBrokerCreateFinal
)

const (
	ownerProbeStageBrokerHandlerRejectMapCreate ownerProbeRuntimeStage = 45
	ownerProbeStageBrokerHandlerRejectProgLoad  ownerProbeRuntimeStage = 46
	ownerProbeStageBrokerHandlerRejectInfo      ownerProbeRuntimeStage = 47
	ownerProbeStageBrokerHandlerRejectGetID     ownerProbeRuntimeStage = 48
	ownerProbeStageBrokerHandlerRejectQuery     ownerProbeRuntimeStage = 49
	ownerProbeStageBrokerHandlerRejectAttach    ownerProbeRuntimeStage = 50
	ownerProbeStageBrokerHandlerRejectDetach    ownerProbeRuntimeStage = 51
)

const ownerProbeStageRuncExecObserved ownerProbeRuntimeStage = 64

const (
	ownerProbeStageWrapperPrepareFileName ownerProbeRuntimeStage = 80 + iota
	ownerProbeStageWrapperPrepareFileOpen
	ownerProbeStageWrapperPrepareFileNewFile
	ownerProbeStageWrapperPrepareFileStat
	ownerProbeStageWrapperPrepareFileType
	ownerProbeStageWrapperPrepareFileUID
	ownerProbeStageWrapperPrepareFileGID
	ownerProbeStageWrapperPrepareFileWriteMode
	ownerProbeStageWrapperPrepareFileLinks
	ownerProbeStageWrapperPrepareFileSizeLow
	ownerProbeStageWrapperPrepareFileSizeHigh
	ownerProbeStageWrapperPrepareFileRead
	ownerProbeStageWrapperPrepareFileCount
	ownerProbeStageWrapperPrepareFileHeldStat
	ownerProbeStageWrapperPrepareFileNamedStat
	ownerProbeStageWrapperPrepareFileHeldDev
	ownerProbeStageWrapperPrepareFileHeldIno
	ownerProbeStageWrapperPrepareFileHeldMode
	ownerProbeStageWrapperPrepareFileHeldUID
	ownerProbeStageWrapperPrepareFileHeldGID
	ownerProbeStageWrapperPrepareFileHeldNlink
	ownerProbeStageWrapperPrepareFileHeldSize
	ownerProbeStageWrapperPrepareFileHeldMtime
	ownerProbeStageWrapperPrepareFileHeldCtime
	ownerProbeStageWrapperPrepareFileNamedDev
	ownerProbeStageWrapperPrepareFileNamedIno
	ownerProbeStageWrapperPrepareFileNamedMode
	ownerProbeStageWrapperPrepareFileNamedUID
	ownerProbeStageWrapperPrepareFileNamedGID
	ownerProbeStageWrapperPrepareFileNamedNlink
	ownerProbeStageWrapperPrepareFileNamedSize
	ownerProbeStageWrapperPrepareFileNamedMtime
	ownerProbeStageWrapperPrepareFileNamedCtime
	ownerProbeStageWrapperPrepareFileSeek
	ownerProbeStageWrapperPrepareHeldStat
	ownerProbeStageWrapperPrepareDigestFD
	ownerProbeStageWrapperPrepareDigestStat
	ownerProbeStageWrapperPrepareDigestType
	ownerProbeStageWrapperPrepareDigestUID
	ownerProbeStageWrapperPrepareDigestGID
	ownerProbeStageWrapperPrepareDigestWriteMode
	ownerProbeStageWrapperPrepareDigestSizeLow
	ownerProbeStageWrapperPrepareDigestSizeHigh
	ownerProbeStageWrapperPrepareDigestRead
	ownerProbeStageWrapperPrepareDigestCount
	ownerProbeStageWrapperPrepareDigestAfterStat
	ownerProbeStageWrapperPrepareDigestAfterDev
	ownerProbeStageWrapperPrepareDigestAfterIno
	ownerProbeStageWrapperPrepareDigestAfterMode
	ownerProbeStageWrapperPrepareDigestAfterUID
	ownerProbeStageWrapperPrepareDigestAfterGID
	ownerProbeStageWrapperPrepareDigestAfterNlink
	ownerProbeStageWrapperPrepareDigestAfterSize
	ownerProbeStageWrapperPrepareDigestAfterMtime
	ownerProbeStageWrapperPrepareDigestAfterCtime
	ownerProbeStageWrapperPrepareReadDataRead
	ownerProbeStageWrapperPrepareReadDataCount
	ownerProbeStageWrapperPrepareReadDataStat
	ownerProbeStageWrapperPrepareReadDataAfterDev
	ownerProbeStageWrapperPrepareReadDataAfterIno
	ownerProbeStageWrapperPrepareReadDataAfterMode
	ownerProbeStageWrapperPrepareReadDataAfterUID
	ownerProbeStageWrapperPrepareReadDataAfterGID
	ownerProbeStageWrapperPrepareReadDataAfterNlink
	ownerProbeStageWrapperPrepareReadDataAfterSize
	ownerProbeStageWrapperPrepareReadDataAfterMtime
	ownerProbeStageWrapperPrepareReadDataAfterCtime
	ownerProbeStageWrapperPrepareHash
	ownerProbeStageWrapperPrepareJSON
	ownerProbeStageWrapperPrepareProcess
	ownerProbeStageWrapperPrepareSelftest
)

const (
	ownerProbeStageBrokerOCIVersion ownerProbeRuntimeStage = 151 + iota
	ownerProbeStageBrokerOCIProcess
	ownerProbeStageBrokerOCIProcessTerminal
	ownerProbeStageBrokerOCIProcessConsoleSize
	ownerProbeStageBrokerOCIProcessUser
	ownerProbeStageBrokerOCIProcessUserUID
	ownerProbeStageBrokerOCIProcessUserGID
	ownerProbeStageBrokerOCIProcessUserUmask
	ownerProbeStageBrokerOCIProcessUserAdditionalGIDs
	ownerProbeStageBrokerOCIProcessUserUsername
	ownerProbeStageBrokerOCIProcessArgs
	ownerProbeStageBrokerOCIProcessCommandLine
	ownerProbeStageBrokerOCIProcessEnv
	ownerProbeStageBrokerOCIProcessCwd
	ownerProbeStageBrokerOCIProcessCapabilities
	ownerProbeStageBrokerOCIProcessCapBounding
	ownerProbeStageBrokerOCIProcessCapEffective
	ownerProbeStageBrokerOCIProcessCapInheritable
	ownerProbeStageBrokerOCIProcessCapPermitted
	ownerProbeStageBrokerOCIProcessCapAmbient
	ownerProbeStageBrokerOCIProcessRlimits
	ownerProbeStageBrokerOCIProcessNoNewPrivileges
	ownerProbeStageBrokerOCIProcessApparmor
	ownerProbeStageBrokerOCIProcessOomScore
	ownerProbeStageBrokerOCIProcessScheduler
	ownerProbeStageBrokerOCIProcessSelinux
	ownerProbeStageBrokerOCIProcessIOPriority
	ownerProbeStageBrokerOCIProcessExecCPUAffinity
	ownerProbeStageBrokerOCIRoot
	ownerProbeStageBrokerOCIRootPath
	ownerProbeStageBrokerOCIRootReadonly
	ownerProbeStageBrokerOCIHostname
	ownerProbeStageBrokerOCIDomainname
	ownerProbeStageBrokerOCIMounts
	ownerProbeStageBrokerOCIMountDestination
	ownerProbeStageBrokerOCIMountType
	ownerProbeStageBrokerOCIMountSource
	ownerProbeStageBrokerOCIMountOptions
	ownerProbeStageBrokerOCIMountUIDMappings
	ownerProbeStageBrokerOCIMountGIDMappings
	ownerProbeStageBrokerOCIHooks
	ownerProbeStageBrokerOCIAnnotations
	ownerProbeStageBrokerOCILinux
	ownerProbeStageBrokerOCILinuxUIDMappings
	ownerProbeStageBrokerOCILinuxGIDMappings
	ownerProbeStageBrokerOCILinuxSysctl
	ownerProbeStageBrokerOCILinuxResources
	ownerProbeStageBrokerOCIResourcesDevices
	ownerProbeStageBrokerOCIResourceDeviceAllow
	ownerProbeStageBrokerOCIResourceDeviceType
	ownerProbeStageBrokerOCIResourceDeviceMajor
	ownerProbeStageBrokerOCIResourceDeviceMinor
	ownerProbeStageBrokerOCIResourceDeviceAccess
	ownerProbeStageBrokerOCIResourcesMemory
	ownerProbeStageBrokerOCIMemoryLimit
	ownerProbeStageBrokerOCIMemorySwap
	ownerProbeStageBrokerOCIResourcesCPU
	ownerProbeStageBrokerOCICPUQuota
	ownerProbeStageBrokerOCICPUPeriod
	ownerProbeStageBrokerOCIResourcesPids
	ownerProbeStageBrokerOCIPidsLimit
	ownerProbeStageBrokerOCIResourcesBlockIO
	ownerProbeStageBrokerOCIResourcesHugepageLimits
	ownerProbeStageBrokerOCIResourcesNetwork
	ownerProbeStageBrokerOCIResourcesRDMA
	ownerProbeStageBrokerOCIResourcesUnified
	ownerProbeStageBrokerOCILinuxCgroupsPath
	ownerProbeStageBrokerOCILinuxNamespaces
	ownerProbeStageBrokerOCINamespaceType
	ownerProbeStageBrokerOCINamespacePath
	ownerProbeStageBrokerOCILinuxDevices
	ownerProbeStageBrokerOCILinuxNetDevices
	ownerProbeStageBrokerOCILinuxSeccomp
	ownerProbeStageBrokerOCISeccompDefaultAction
	ownerProbeStageBrokerOCISeccompDefaultErrno
	ownerProbeStageBrokerOCISeccompArchitectures
	ownerProbeStageBrokerOCISeccompFlags
	ownerProbeStageBrokerOCISeccompListenerPath
	ownerProbeStageBrokerOCISeccompListenerMetadata
	ownerProbeStageBrokerOCISeccompSyscalls
	ownerProbeStageBrokerOCISyscallNames
	ownerProbeStageBrokerOCISyscallAction
	ownerProbeStageBrokerOCISyscallErrno
	ownerProbeStageBrokerOCISyscallArgs
	ownerProbeStageBrokerOCILinuxRootfsPropagation
	ownerProbeStageBrokerOCILinuxMaskedPaths
	ownerProbeStageBrokerOCILinuxReadonlyPaths
	ownerProbeStageBrokerOCILinuxMountLabel
	ownerProbeStageBrokerOCIOther
	ownerProbeStageBrokerOCILimit
	ownerProbeStageBrokerOCIDepth
)

const ownerProbeDiagnosticDir = "/run/g2/owner-probe-diagnostic"
const ownerProbeSettlementDiagnosticDir = "/run/g2/owner-probe-settlement-diagnostic"

// ownerProbeRemovalDiagnostic is a bounded projection of one sampled object.
type ownerProbeRemovalDiagnostic [7]byte

// ownerProbeRemovalFollowup contains only the fixed event-five fields.
type ownerProbeRemovalFollowup [12]byte

func ownerProbeRemovalFollowupValid(d ownerProbeRemovalFollowup) bool {
	if d[0] < 1 || d[0] > 4 || d[1] > 14 || d[2] > 3 || d[0] == 2 && d[1] != 0 {
		return false
	}
	if d[2] != 1 {
		for _, v := range d[3:] {
			if v != 0 {
				return false
			}
		}
		return true
	}
	if d[3] < 1 || d[3] > 8 || d[4] < 1 || d[4] > 6 || d[5] < 1 || d[5] > 4 || d[6] < 1 || d[6] > 7 || d[7] < 1 || d[7] > 7 || d[8] < 1 || d[8] > 3 || d[9] < 1 || d[9] > 8 || d[10] < 1 || d[10] > 6 || d[11] < 1 || d[11] > 8 {
		return false
	}
	return d[8] != 2 || d[3] == 2 && d[4] == 2 && d[5] == 2 && d[6] == 7 && d[7] == 3 && d[9] <= 5 && d[10] <= 4 && d[11] >= 2 && d[11] <= 4
}

func (q *pendingBrokerSettlement) followup(b *authorityBroker, boundary byte) {
	if q == nil || !q.diagnostic.valid || q.followupObject == nil || q.diagnosticEvents&(1<<5) != 0 || boundary < 1 || boundary > 4 {
		return
	}
	q.diagnosticEvents |= 1 << 5
	d := b.ownerProbeRemovalFollowup(q, boundary)
	packet := [16]byte{7, 1, q.diagnostic.purpose, 5}
	copy(packet[4:], d[:])
	if ownerProbeSettlementPacketValid(packet[:]) {
		sendOwnerProbeRuntimePacket(q.diagnostic.cid, packet[:])
	}
}

// ownerProbeRemovalFollowup projects one pinned incarnation at one boundary.
// It does not query the kernel or modify the retained graph.
func (b *authorityBroker) ownerProbeRemovalFollowup(q *pendingBrokerSettlement, boundary byte) ownerProbeRemovalFollowup {
	d := ownerProbeRemovalFollowup{boundary}
	if q == nil {
		return d
	}
	if boundary != 2 {
		d[1] = q.lastWait
	}
	if b == nil || b.effects == nil || b.effects.objects == nil || q.followupGrant == nil || q.followupObject == nil {
		return d
	}
	g, o, cookie := q.followupGrant, q.followupObject, q.followupCookie
	owner, owned := b.effects.objectGrant[cookie]
	current := b.effects.objects.byCookie[cookie]
	if cookie == 0 || q.grant != g || b.grants[g.id] != g || !owned || owner != g.id {
		d[2] = 3
		return d
	}
	if current == nil {
		d[2] = 2
		return d
	}
	if current != o || o.cookie != cookie || o.id != q.followupID || o.ownerTransaction != q.followupCreator || o.class != q.followupClass {
		d[2] = 3
		return d
	}
	d[2] = 1
	// close, guard, tracker, lifecycle, cleanup, acceptance, attachments,
	// program type and creator response all have explicit unavailable states.
	d[3], d[4], d[5], d[6], d[7], d[8], d[9], d[10], d[11] = 8, 6, 4, 2, 7, 1, 8, 5, 8
	if o.removalSeq == 0 && o.state == "live" {
		d[5] = 1
	} else if o.removalSeq != 0 && o.state == "id_removed" {
		d[5] = 2
	} else {
		d[5] = 3
	}
	if b.checkpoint != nil && b.checkpoint.lifecycle != nil {
		item := b.checkpoint.lifecycle.objects[cookie]
		switch {
		case item == nil:
			d[6] = 1
		case item.kernelID != o.id || item.class != o.class:
			d[6] = 3
		default:
			d[6] = 4
			if item.removed {
				d[6] += 2
			}
			if item.removalAllowed {
				d[6]++
			}
		}
	}
	if o.class == "map" {
		d[10] = 1
	} else if o.class != "program" {
		d[10] = 6
	} else {
		switch o.objectType {
		case unix.BPF_PROG_TYPE_LSM:
			d[10] = 2
		case unix.BPF_PROG_TYPE_CGROUP_DEVICE:
			d[10] = 3
		default:
			d[10] = 4
		}
	}
	{
		d[9] = 1
		if len(b.effects.attachments) > 4096 {
			d[9] = 7
			b.ownerProbeRemovalEvidence(q, &d)
			return d
		}
		var categories byte
		visited := 0
		for key, a := range b.effects.attachments {
			if visited == 4096 {
				d[9] = 7
				break
			}
			visited++
			if a == nil {
				d[9] = 6
				break
			}
			if a.grantID != g.id || a.programCookie != cookie {
				continue
			}
			if key == 0 || key != a.id {
				d[9] = 6
				break
			}
			switch a.state {
			case "attached":
				if a.offlineSeq != 0 || a.releaseSeq != 0 {
					d[9] = 6
				} else {
					categories |= 1
				}
			case "detached":
				if a.offlineSeq != 0 || a.releaseSeq != 0 {
					d[9] = 6
				} else {
					categories |= 2
				}
			case "offlining":
				if a.offlineSeq == 0 || a.releaseSeq != 0 {
					d[9] = 6
				} else {
					categories |= 2
				}
			case "released":
				if a.offlineSeq == 0 || a.releaseSeq <= a.offlineSeq {
					d[9] = 6
				} else {
					categories |= 4
				}
			default:
				d[9] = 6
			}
			if d[9] == 6 {
				break
			}
		}
		if d[9] == 1 {
			switch categories {
			case 0:
			case 1:
				d[9] = 2
			case 2:
				d[9] = 3
			case 4:
				d[9] = 4
			default:
				d[9] = 5
			}
		}
	}
	b.ownerProbeRemovalEvidence(q, &d)
	return d
}

// A bounded reverse scan treats incomplete or contradictory retained records
// as uncertainty, never as proof that an event did not occur.
func (b *authorityBroker) ownerProbeRemovalEvidence(q *pendingBrokerSettlement, d *ownerProbeRemovalFollowup) {
	if b.transcript == nil {
		return
	}
	if b.transcript.total < 0 || b.transcript.total > maxAuthorityTranscriptBytes || len(b.transcript.entries) > b.transcript.total {
		d[3], d[4], d[7], d[11], d[8] = 5, 3, 4, 5, 3
		return
	}
	o, g, cookie := q.followupObject, q.followupGrant, q.followupCookie
	d[3], d[4], d[7], d[11] = 1, 1, 1, 1
	creator := b.effects.transactions[o.ownerTransaction]
	creatorValid := creator != nil && creator.id != 0 && creator.id == o.ownerTransaction && creator.grantID == g.id && creator.publication != nil && creator.publication.ObjectCookie == cookie && creator.publication.KernelID == o.id && creator.publication.Sequence == o.publicationSeq && (o.class == "program" && creator.publication.Kind == 1 && creator.command == "PROG_LOAD" || o.class == "map" && creator.publication.Kind == 3 && creator.command == "MAP_CREATE")
	if !creatorValid {
		d[11] = 8
	}
	var closeCount, removalCount, guardCount, responseCount int
	var closeResult, removalResult, guardResult int64
	var removalSequence, guardSequence uint64
	var responseCode byte
	badClose, badGuard, badResponse := false, false, false
	bytes := 0
	truncated := false
	for i, examined := len(b.transcript.entries)-1, 0; i >= 0; i, examined = i-1, examined+1 {
		e := &b.transcript.entries[i]
		if examined >= 4096 || len(e.raw)+1 > maxAuthorityTranscriptBytes-bytes {
			truncated = true
			break
		}
		bytes += len(e.raw) + 1
		kind, ok := e.record["kind"].(string)
		body, bodyOK := e.record["body"].(map[string]any)
		if !ok || !ownerProbeHistoryRecordKind(kind) || !bodyOK {
			badClose, badGuard, badResponse = true, true, true
			continue
		}
		switch kind {
		case "grant_object_cleanup":
			c, ok := exactNumber(body["object_cookie"])
			if !ok {
				badClose = true
				continue
			}
			if c != cookie {
				continue
			}
			gid, a := exactNumber(body["grant_id"])
			creator, z := exactNumber(body["creator_transaction_id"])
			step, s := exactNumber(body["step"])
			result, r := ownerProbeHistoryInt(body["result"])
			action, x := body["action"].(string)
			if !a || !z || !s || !r || !x || gid != g.id || creator != o.ownerTransaction {
				badClose = true
				continue
			}
			switch step {
			case 1:
				closeCount++
				if action != "close_ref" || body["guard_sequence"] != nil {
					badClose = true
				}
				closeResult = result
			case 2:
				removalCount++
				seq, valid := exactNumber(body["guard_sequence"])
				if action != "await_id_removed" || !valid || seq == 0 {
					badClose = true
				}
				removalSequence, removalResult = seq, result
			default:
				badClose = true
			}
		case "guard":
			record, valid := body["record"].(map[string]any)
			if !valid {
				badGuard = true
				continue
			}
			c, valid := exactNumber(record["object_cookie"])
			if !valid {
				badGuard = true
				continue
			}
			if c != cookie {
				continue
			}
			k, valid := exactNumber(record["kind"])
			if !valid {
				badGuard = true
				continue
			}
			if k != 2 && k != 4 {
				continue
			}
			guardCount++
			id, v1 := exactNumber(record["kernel_id"])
			seq, v2 := exactNumber(record["sequence"])
			result, v3 := ownerProbeHistoryInt(record["result"])
			if !v1 || !v2 || !v3 || result < -2147483648 || result > 2147483647 || seq == 0 || seq <= o.publicationSeq || id != o.id || k == 2 && o.class != "program" || k == 4 && o.class != "map" {
				badGuard = true
			}
			guardSequence, guardResult = seq, result
		case "notification_response":
			id, valid := exactNumber(body["transaction_id"])
			if !valid || id == 0 {
				badResponse = true
				continue
			}
			if id != o.ownerTransaction {
				continue
			}
			responseCount++
			tx := creator
			if !creatorValid {
				badResponse = true
				continue
			}
			slot := -1
			if tx.command == "MAP_CREATE" {
				slot = 0
			}
			if tx.command == "PROG_LOAD" {
				slot = 1
			}
			state := byte(0)
			switch tx.state {
			case "effect_observed":
				state = 3
			case "response_settled":
				state = 4
			case "retired":
				state = 5
			}
			result := byte(1)
			if tx.result != nil {
				n, valid := ownerProbeHistoryInt(tx.result)
				if !valid || n < -4095 {
					badResponse = true
					continue
				}
				if n < 0 {
					result = 2
				} else if n == 0 {
					result = 3
				} else {
					result = 4
				}
			}
			if slot < 0 {
				badResponse = true
				continue
			}
			code, valid := ownerProbeHistoryResponse(body, tx, slot, state, result)
			if !valid {
				badResponse = true
				continue
			}
			responseCode = code
		}
	}
	if truncated {
		d[3], d[4], d[7], d[11] = 7, 5, 6, 7
		if !creatorValid {
			d[11] = 8
		}
		if badClose {
			d[3], d[7], d[8] = 5, 4, 3
		} else if closeCount > 1 || removalCount > 1 {
			d[3], d[7] = 6, 5
		}
		if badGuard {
			d[4], d[8] = 3, 3
		} else if guardCount > 1 {
			d[4] = 4
		}
		if badResponse {
			d[11] = 5
		} else if responseCount > 1 {
			d[11] = 6
		}
		return
	}
	if bytes != b.transcript.total {
		d[3], d[4], d[7], d[11], d[8] = 5, 3, 4, 5, 3
		return
	}
	if badClose {
		d[3], d[7] = 5, 4
	} else if closeCount > 1 || removalCount > 1 {
		d[3], d[7] = 6, 5
	} else if closeCount == 1 {
		if closeResult == 0 {
			d[3] = 2
		} else if closeResult < 0 {
			d[3] = 3
		} else {
			d[3] = 4
		}
		d[7] = 2
		if removalCount == 1 {
			d[7] = 3
		}
	} else if removalCount != 0 {
		d[7] = 4
	}
	if badGuard {
		d[4] = 3
	} else if guardCount > 1 {
		d[4] = 4
	} else if guardCount == 1 {
		d[4] = 2
	}
	if badResponse {
		d[11] = 5
	} else if responseCount > 1 {
		d[11] = 6
	} else if responseCount == 1 {
		switch responseCode {
		case 4:
			d[11] = 2
		case 5:
			d[11] = 3
		default:
			d[11] = 4
		}
	} else if !creatorValid {
		d[11] = 8
	}
	if badClose || badGuard || d[5] == 3 || d[6] == 3 {
		d[8] = 3
	}
	cleanup := b.effects.grantCleanup[cookie]
	if cleanup != nil && (cleanup.cookie != cookie || cleanup.grantID != g.id || cleanup.creatorID != o.ownerTransaction || closeCount == 1 && cleanup.closeResult != closeResult || removalCount == 1 && cleanup.removalSequence != removalSequence) {
		d[7], d[8] = 4, 3
	}
	if guardCount == 1 && removalCount == 1 && (guardSequence != removalSequence || guardResult != removalResult) {
		d[7], d[8] = 4, 3
	}
	if d[3] == 2 && d[4] == 2 && d[5] == 2 && d[6] == 7 && d[7] == 3 && d[9] <= 5 && d[10] <= 4 && d[11] >= 2 && d[11] <= 4 && o.removalAuthorized && cleanup != nil && cleanup.cookie == cookie && cleanup.grantID == g.id && cleanup.creatorID == o.ownerTransaction && cleanup.closeResult == 0 && cleanup.removalSequence != 0 && cleanup.removalSequence == o.removalSeq && guardSequence == o.removalSeq && removalSequence == o.removalSeq && removalResult == guardResult {
		d[8] = 2
	}
}

func (b *authorityBroker) ownerProbeRemovalSnapshot(grant *brokerGrant, cookie uint64, object *objectIncarnation) ownerProbeRemovalDiagnostic {
	var detail ownerProbeRemovalDiagnostic
	if b == nil || b.effects == nil || b.effects.objects == nil || grant == nil || cookie == 0 || b.grants[grant.id] != grant {
		return detail
	}
	owner, present := b.effects.objectGrant[cookie]
	if !present || owner != grant.id || b.effects.objects.byCookie[cookie] != object {
		return detail
	}
	detail = ownerProbeRemovalDiagnostic{1, 1, 1, 1, 1, 2, 1}
	validObject := object != nil && object.cookie == cookie
	if object != nil {
		if !validObject {
			detail[0] = 5
		} else {
			switch object.class {
			case "map":
				detail[0] = 2
			case "program":
				detail[0] = 3
			default:
				detail[0] = 4
			}
			detail[6] = 2
			if object.removalAuthorized {
				detail[6] = 3
			}
		}
	}
	if fd, ok := b.effects.objectFD[cookie]; ok {
		detail[1] = 2
		if fd < 0 {
			detail[1] = 3
		}
	}
	if cleanup := b.effects.grantCleanup[cookie]; cleanup != nil {
		switch {
		case cleanup.cookie != cookie || cleanup.grantID != grant.id || validObject && cleanup.creatorID != object.ownerTransaction:
			detail[2] = 7
		case cleanup.closeResult > 0:
			detail[2] = 6
		case cleanup.closeResult == 0 && cleanup.removalSequence == 0:
			detail[2] = 2
		case cleanup.closeResult == 0:
			detail[2] = 3
		case cleanup.removalSequence == 0:
			detail[2] = 4
		default:
			detail[2] = 5
		}
	}
	if validObject && object.ownerTransaction != 0 {
		tx := b.effects.transactions[object.ownerTransaction]
		if tx == nil {
			detail[3] = 2
		} else if tx.id != object.ownerTransaction || tx.grantID != grant.id || tx.publication == nil || tx.publication.ObjectCookie != cookie {
			detail[3] = 3
		} else {
			switch tx.state {
			case "prepared":
				detail[3] = 4
			case "committed":
				detail[3] = 5
			case "effect_observed":
				detail[3] = 6
			case "response_settled":
				detail[3] = 7
			case "retired":
				detail[3] = 8
			default:
				detail[3] = 9
			}
			detail[4] = 2
			if tx.cleanupActive {
				detail[4] = 3
			}
		}
	}
	if b.checkpoint != nil && b.checkpoint.lifecycle != nil {
		item := b.checkpoint.lifecycle.objects[cookie]
		switch {
		case item == nil:
			detail[5] = 1
		case !validObject:
			detail[5] = 2
		case item.kernelID != object.id || item.class != object.class:
			detail[5] = 3
		default:
			detail[5] = 4
			if item.removed {
				detail[5] += 2
			}
			if item.removalAllowed {
				detail[5]++
			}
		}
	}
	return detail
}

func ownerProbeRemovalDiagnosticValid(detail ownerProbeRemovalDiagnostic) bool {
	if detail == (ownerProbeRemovalDiagnostic{}) {
		return true
	}
	if detail[0] < 1 || detail[0] > 5 || detail[1] < 1 || detail[1] > 3 ||
		detail[2] < 1 || detail[2] > 7 || detail[3] < 1 || detail[3] > 9 ||
		detail[4] < 1 || detail[4] > 3 || detail[5] < 1 || detail[5] > 7 || detail[6] < 1 || detail[6] > 3 {
		return false
	}
	validObject := detail[0] >= 2 && detail[0] <= 4
	if !validObject && (detail[3] != 1 || detail[4] != 1 || detail[6] != 1 || detail[5] >= 3) {
		return false
	}
	if validObject && detail[6] == 1 {
		return false
	}
	return (detail[3] <= 3 && detail[4] == 1) || (detail[3] >= 4 && detail[4] >= 2)
}

type ownerProbeSettlementReceipt struct {
	cid       string
	purpose   byte
	valid     bool
	attempted bool
}

func (r *ownerProbeSettlementReceipt) send(event, point byte, detail ownerProbeRemovalDiagnostic) {
	if r == nil || !r.valid || r.attempted {
		return
	}
	packet := [16]byte{7, 1, r.purpose, event, point}
	copy(packet[5:12], detail[:])
	if !ownerProbeSettlementPacketValid(packet[:]) {
		return
	}
	r.attempted = true
	sendOwnerProbeRuntimePacket(r.cid, packet[:])
}

func ownerProbeSettlementPacketValid(packet []byte) bool {
	if len(packet) != 16 || packet[0] != 7 || packet[1] != 1 ||
		(packet[2] != 1 && packet[2] != 2) || packet[3] < 1 || packet[3] > 5 {
		return false
	}
	if packet[3] == 5 {
		var followup ownerProbeRemovalFollowup
		copy(followup[:], packet[4:])
		return ownerProbeRemovalFollowupValid(followup)
	}
	if (packet[3] == 2 && (packet[4] < 1 || packet[4] > 13)) || (packet[3] != 2 && packet[4] != 0) ||
		packet[12] != 0 || packet[13] != 0 || packet[14] != 0 || packet[15] != 0 {
		return false
	}
	var detail ownerProbeRemovalDiagnostic
	copy(detail[:], packet[5:12])
	return ownerProbeRemovalDiagnosticValid(detail) && (packet[3] == 2 && packet[4] == 10 || detail == (ownerProbeRemovalDiagnostic{}))
}

func ownerProbeWrapperStageValid(stage ownerProbeRuntimeStage) bool {
	return stage >= ownerProbeStageWrapperEntry && stage <= ownerProbeStageWrapperExecReturn ||
		stage >= ownerProbeStageWrapperPrepareFileName && stage <= ownerProbeStageWrapperPrepareSelftest
}

func ownerProbeBrokerStageValid(stage ownerProbeRuntimeStage) bool {
	return stage >= ownerProbeStageBrokerCreatePolicy && stage <= ownerProbeStageBrokerHandlerRejectDetach ||
		stage >= ownerProbeStageBrokerOCIVersion && stage <= ownerProbeStageBrokerOCIDepth
}

func (b *authorityBroker) reserveOwnerProbeHandlerDiagnostic(inv *runtimeInvocation, command uint32, observed error) ownerProbeRuntimeStage {
	var stage ownerProbeRuntimeStage
	switch command {
	case unix.BPF_MAP_CREATE:
		stage = ownerProbeStageBrokerHandlerRejectMapCreate
	case unix.BPF_PROG_LOAD:
		stage = ownerProbeStageBrokerHandlerRejectProgLoad
	case unix.BPF_OBJ_GET_INFO_BY_FD:
		stage = ownerProbeStageBrokerHandlerRejectInfo
	case unix.BPF_PROG_GET_FD_BY_ID:
		stage = ownerProbeStageBrokerHandlerRejectGetID
	case unix.BPF_PROG_QUERY:
		stage = ownerProbeStageBrokerHandlerRejectQuery
	case unix.BPF_PROG_ATTACH:
		stage = ownerProbeStageBrokerHandlerRejectAttach
	case unix.BPF_PROG_DETACH:
		stage = ownerProbeStageBrokerHandlerRejectDetach
	default:
		return 0
	}
	if observed == nil || b == nil || b.selftestFD != nil || inv == nil || inv.id == 0 || inv.listener < 0 ||
		b.runtimeInvocations[inv.listener] != inv || inv.grant == nil || inv.grantID != inv.grant.id ||
		inv.generation != inv.grant.generation || inv.state != "running" || !inv.runtimeExecRecorded ||
		!b.reportsOwnerProbeCreate(inv.grant, inv.containerID, inv.command) || inv.ownerProbeHandlerDiagnosticAttempted {
		return 0
	}
	inv.ownerProbeHandlerDiagnosticAttempted = true
	return stage
}

func ownerProbeRuntimeDiagnosticValid(diagnostic ownerProbeRuntimeDiagnostic) bool {
	w := diagnostic.mountOptions
	if diagnostic.stage != ownerProbeStageBrokerOCIMountOptions {
		return w == (ownerProbeMountOptionsDiagnostic{})
	}
	return w == (ownerProbeMountOptionsDiagnostic{}) ||
		w.mount >= 1 && w.mount <= 12 && w.actual >= 1 && w.actual <= 17 &&
			w.expected >= 1 && w.expected <= 17 && w.difference >= 1 && w.difference <= 9
}

// ownerProbeCreateHistory projects retained native results and recorded response ioctls.
func (b *authorityBroker) ownerProbeCreateHistory(grant *brokerGrant) ([64]byte, bool) {
	var packet [64]byte
	var empty [64]byte
	if b == nil || b.selftestFD != nil || b.effects == nil || grant == nil || !grant.createBound ||
		!b.reportsOwnerProbeCreate(grant, grant.containerID, "create") || len(b.runtimeInvocations) > 4096 || len(b.effects.transactions) > 4096 {
		return empty, false
	}
	var create *runtimeInvocation
	for _, inv := range b.runtimeInvocations {
		if inv == nil {
			return empty, false
		}
		if inv.grantID != grant.id || inv.command != "create" {
			continue
		}
		if create != nil || inv.id == 0 || inv.grant != grant || inv.generation != grant.generation || inv.containerID != grant.containerID || inv.configSHA != grant.configSHA {
			return empty, false
		}
		create = inv
	}
	if create == nil {
		return empty, false
	}
	packet[0], packet[1] = 4, 1
	if grant.purpose == "postgres-uid" {
		packet[2] = 1
	} else {
		packet[2] = 2
	}
	if create.runtimeExecRecorded {
		packet[3] = 1
	}
	group := b.effects.cgroups[grant.id]
	if group != nil {
		switch {
		case group.GrantID != grant.id || group.Enrollment == 0 || group.RootCookie == 0 || group.KernfsID == 0:
			packet[4] = 3
		case group.Retired:
			packet[4] = 2
		case group.HeldFD < 0:
			packet[4] = 3
		default:
			packet[4] = 1
		}
		if (packet[4] == 1 || packet[4] == 2) && b.effects.registry != nil {
			target := b.effects.registry.targets[cgroupKey{root: group.RootCookie, id: group.KernfsID}]
			switch {
			case target == nil:
				packet[5] = 1
			case target.group != group || target.grant != grant:
				packet[5] = 8
			case target.phase <= 4:
				packet[5] = byte(target.phase) + 2
			default:
				packet[5] = 7
			}
		}
	}
	var latest [7]uint64
	var selected [7]*runtimeEffectTransaction
	var total uint16
	for key, tx := range b.effects.transactions {
		if tx == nil {
			return empty, false
		}
		if tx.grantID != grant.id || tx.invocationID != create.id || tx.internalDetach || tx.internalSelftest || tx.cleanupParent != nil {
			continue
		}
		if tx.id == 0 || tx.id != key {
			return empty, false
		}
		var slot int
		switch tx.command {
		case "MAP_CREATE":
			slot = 0
		case "PROG_LOAD":
			slot = 1
		case "OBJ_GET_INFO_BY_FD":
			slot = 2
		case "PROG_GET_FD_BY_ID":
			slot = 3
		case "PROG_QUERY":
			slot = 4
		case "PROG_ATTACH":
			slot = 5
		case "PROG_DETACH":
			slot = 6
		default:
			return empty, false
		}
		var state byte
		switch tx.state {
		case "prepared":
			state = 1
		case "committed":
			state = 2
		case "effect_observed":
			state = 3
		case "response_settled":
			state = 4
		case "retired":
			state = 5
		default:
			return empty, false
		}
		result, code := byte(1), uint16(0)
		if tx.result != nil {
			n, ok := tx.result.(json.Number)
			if !ok {
				return empty, false
			}
			v, err := strconv.ParseInt(string(n), 10, 64)
			if err != nil || v < -4095 {
				return empty, false
			}
			switch {
			case v < 0:
				result, code = 2, uint16(-v)
			case v == 0:
				result = 3
			default:
				result = 4
			}
		}
		if (state <= 2 && result != 1) || ((state == 3 || state == 4) && result == 1) {
			return empty, false
		}
		total++
		if total > 4096 {
			return empty, false
		}
		i := 8 + slot*8
		count := uint16(packet[i]) | uint16(packet[i+1])<<8
		count++
		packet[i], packet[i+1] = byte(count), byte(count>>8)
		if tx.id > latest[slot] {
			latest[slot] = tx.id
			selected[slot] = tx
			packet[i+2], packet[i+3], packet[i+4], packet[i+5] = state, result, byte(code), byte(code>>8)
			if tx.policyErr != nil {
				packet[i+6] = 1
			} else {
				packet[i+6] = 0
			}
		}
	}
	if tx := selected[1]; tx != nil {
		if len(tx.attributes) < 24 || len(tx.attributes) > 4096 {
			return empty, false
		}
		license, ok := tx.license.(string)
		if !ok {
			return empty, false
		}
		switch typ := guardU32(tx.attributes, 0); {
		case typ == unix.BPF_PROG_TYPE_CGROUP_DEVICE && license == "Apache":
			packet[6] = 1
		case typ == unix.BPF_PROG_TYPE_SOCKET_FILTER && license == "MIT":
			packet[6] = 2
		case typ == unix.BPF_PROG_TYPE_CGROUP_DEVICE && license == "MIT":
			packet[6] = 3
		default:
			return empty, false
		}
	}
	if b.transcript == nil || b.transcript.total < 0 || b.transcript.total > maxAuthorityTranscriptBytes || len(b.transcript.entries) > b.transcript.total {
		return empty, false
	}
	for _, entry := range b.transcript.entries {
		kind, ok := entry.record["kind"].(string)
		if !ok || !ownerProbeHistoryRecordKind(kind) {
			return empty, false
		}
		if kind != "notification_response" {
			continue
		}
		body, ok := entry.record["body"].(map[string]any)
		if !ok {
			return empty, false
		}
		id, ok := exactNumber(body["transaction_id"])
		if !ok || id == 0 {
			return empty, false
		}
		for slot, tx := range selected {
			if tx == nil || tx.id != id {
				continue
			}
			i := 8 + slot*8
			if packet[i+7] != 0 {
				return empty, false
			}
			response, valid := ownerProbeHistoryResponse(body, tx, slot, packet[i+2], packet[i+3])
			if !valid {
				return empty, false
			}
			packet[i+7] = response
		}
	}
	for slot, tx := range selected {
		if tx == nil {
			continue
		}
		i := 8 + slot*8
		if packet[i+7] == 0 {
			if packet[i+2] == 4 || packet[i+2] == 5 && packet[i+3] != 1 {
				return empty, false
			}
			packet[i+7] = 1
		}
	}
	return packet, true
}

func ownerProbeHistoryRecordKind(kind string) bool {
	switch kind {
	case "principal", "grant", "invocation", "daemon", "transaction", "object", "attachment", "audit", "audit_wire", "audit_control", "audit_readback", "audit_cut", "checkpoint", "guard", "registry", "selftest_fd_stage", "selftest_runtime_stage", "selftest_case", "selftest_guard_status", "selftest_audit_reorder", "control_rejection", "probe_actor", "probe_import", "module_instance", "exec_admission", "endpoint_transition", "notification_denial", "notification_cancel", "notification_response", "cgroup_enrollment", "held_file", "grant_object_cleanup", "runtime_exec", "runtime_root", "containerd_launcher":
		return true
	}
	return false
}

func ownerProbeHistoryInt(value any) (int64, bool) {
	n, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	v, err := strconv.ParseInt(string(n), 10, 64)
	return v, err == nil
}

func ownerProbeHistoryResponse(body map[string]any, tx *runtimeEffectTransaction, slot int, state, result byte) (byte, bool) {
	if !exactKeys(body, "transaction_id", "invocation_id", "requester_cookie", "notification_id", "operation", "response", "ioctl_errno", "returned_fd", "guard_before", "guard_after") || state < 3 || result == 1 {
		return 0, false
	}
	for _, pair := range [...]struct {
		name     string
		expected uint64
	}{
		{"invocation_id", tx.invocationID}, {"requester_cookie", tx.requesterCookie}, {"notification_id", tx.notificationID},
	} {
		v, ok := exactNumber(body[pair.name])
		if !ok || v == 0 || v != pair.expected {
			return 0, false
		}
	}
	before, okBefore := exactNumber(body["guard_before"])
	after, okAfter := exactNumber(body["guard_after"])
	ioctlErr, okErr := exactNumber(body["ioctl_errno"])
	if !okBefore || !okAfter || after < before || !okErr || (ioctlErr != 0 && ioctlErr != uint64(unix.ENOENT)) {
		return 0, false
	}
	operation, ok := body["operation"].(string)
	if !ok {
		return 0, false
	}
	needsFD := (slot == 0 || slot == 1 || slot == 3) && (result == 3 || result == 4)
	returned := body["returned_fd"]
	var code byte
	switch operation {
	case "SEND":
		response, ok := body["response"].(map[string]any)
		if !ok || !exactKeys(response, "value", "error", "flags") || returned != nil {
			return 0, false
		}
		value, valueOK := ownerProbeHistoryInt(response["value"])
		errorValue, errorOK := ownerProbeHistoryInt(response["error"])
		flags, flagsOK := exactNumber(response["flags"])
		if !valueOK || !errorOK || errorValue < -4095 || errorValue > 0 || !flagsOK || flags != 0 || (errorValue != 0 && value != 0) || (needsFD && (errorValue != -1 || value != 0)) {
			return 0, false
		}
		code = 2
	case "ADDFD_SEND":
		if !needsFD || body["response"] != nil {
			return 0, false
		}
		if ioctlErr == 0 {
			fd, valid := exactNumber(returned)
			if !valid || fd > 1<<31-1 {
				return 0, false
			}
		} else if returned != nil {
			return 0, false
		}
		code = 4
	default:
		return 0, false
	}
	if state == 4 || state == 5 {
		if returned == nil {
			if tx.returnedFD != nil {
				return 0, false
			}
		} else {
			actual, actualOK := exactNumber(returned)
			recorded, recordedOK := exactNumber(tx.returnedFD)
			if !actualOK || !recordedOK || actual != recorded {
				return 0, false
			}
		}
	}
	if ioctlErr != 0 {
		code++
	}
	return code, true
}

func (b *authorityBroker) reportOwnerProbeCreateHistory(grant *brokerGrant, deleteErr error) {
	if deleteErr == nil {
		return
	}
	packet, ok := b.ownerProbeCreateHistory(grant)
	if ok {
		sendOwnerProbeRuntimePacket(grant.containerID, packet[:])
	}
}

func sendOwnerProbeRuntimeDiagnostic(cid string, diagnostic ownerProbeRuntimeDiagnostic, observed error) {
	stage := diagnostic.stage
	failure := ownerProbeWrapperStageValid(stage) || ownerProbeBrokerStageValid(stage)
	if !ownerProbeRuntimeDiagnosticValid(diagnostic) || !runtimeContainerID(cid) || os.Geteuid() != 0 || os.Getegid() != 0 || !(failure && observed != nil || stage == ownerProbeStageRuncExecObserved && observed == nil) {
		return
	}
	var code unix.Errno
	var value uint16
	if errors.As(observed, &code) && code >= 1 && code <= 4095 {
		value = uint16(code)
	}
	if stage == ownerProbeStageBrokerOCIMountOptions && value != 0 {
		return
	}
	w := diagnostic.mountOptions
	packet := [8]byte{2, byte(stage), byte(value), byte(value >> 8), w.mount, w.actual, w.expected, w.difference}
	sendOwnerProbeRuntimePacket(cid, packet[:])
}

func sendOwnerProbeRuntimePacket(cid string, packet []byte) {
	if !runtimeContainerID(cid) || os.Geteuid() != 0 || os.Getegid() != 0 {
		return
	}
	name := ownerProbeDiagnosticDir
	if ownerProbeSettlementPacketValid(packet) {
		name = ownerProbeSettlementDiagnosticDir
	} else if !(len(packet) == 8 && packet[0] == 2 || len(packet) == 64 && packet[0] == 4 && packet[1] == 1) {
		return
	}
	dir, err := runtimeDirectory(name, false)
	if err != nil {
		return
	}
	defer unix.Close(dir)
	var st unix.Stat_t
	var fs unix.Statfs_t
	if unix.Fstat(dir, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Mode&07777 != 0700 || st.Uid != 0 || st.Gid != 0 || unix.Fstatfs(dir, &fs) != nil || fs.Type != unix.TMPFS_MAGIC {
		return
	}
	if unix.Fstatat(dir, cid, &st, unix.AT_SYMLINK_NOFOLLOW) != nil || st.Mode&unix.S_IFMT != unix.S_IFSOCK || st.Uid != 0 || st.Gid != 0 {
		return
	}
	fd, err := unix.Socket(unix.AF_UNIX, unix.SOCK_DGRAM|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return
	}
	defer unix.Close(fd)
	_ = unix.Sendto(fd, packet[:], unix.MSG_DONTWAIT|unix.MSG_NOSIGNAL, &unix.SockaddrUnix{Name: name + "/" + cid})
}

func reportOwnerProbeWrapperFailure(args []string, stage ownerProbeRuntimeStage, observed error) {
	if !ownerProbeWrapperStageValid(stage) || observed == nil || len(args) < 1 || len(args) > 16 || !runtimeContainerID(args[len(args)-1]) {
		return
	}
	creates := 0
	for _, arg := range args[:len(args)-1] {
		switch arg {
		case "create":
			creates++
		case "features", "start", "state", "delete", "kill", "exec":
			return
		}
	}
	if creates == 1 {
		sendOwnerProbeRuntimeDiagnostic(args[len(args)-1], ownerProbeRuntimeDiagnostic{stage: stage}, observed)
	}
}
