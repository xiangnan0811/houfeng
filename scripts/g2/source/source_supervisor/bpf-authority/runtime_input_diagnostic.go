//go:build linux && amd64 && !cgo

package main

import "golang.org/x/sys/unix"

type runtimeInputDiagnostic struct {
	stage ownerProbeRuntimeStage
}

func (d *runtimeInputDiagnostic) reject(stage ownerProbeRuntimeStage) {
	if d != nil && d.stage == 0 {
		d.stage = stage
	}
}

type runtimeStatDifference uint8

const (
	runtimeStatSame runtimeStatDifference = iota
	runtimeStatDev
	runtimeStatIno
	runtimeStatMode
	runtimeStatUID
	runtimeStatGID
	runtimeStatNlink
	runtimeStatSize
	runtimeStatMtime
	runtimeStatCtime
)

func trustedStatDifference(a, b unix.Stat_t) runtimeStatDifference {
	switch {
	case a.Dev != b.Dev:
		return runtimeStatDev
	case a.Ino != b.Ino:
		return runtimeStatIno
	case a.Mode != b.Mode:
		return runtimeStatMode
	case a.Uid != b.Uid:
		return runtimeStatUID
	case a.Gid != b.Gid:
		return runtimeStatGID
	case a.Nlink != b.Nlink:
		return runtimeStatNlink
	case a.Size != b.Size:
		return runtimeStatSize
	case a.Mtim != b.Mtim:
		return runtimeStatMtime
	case a.Ctim != b.Ctim:
		return runtimeStatCtime
	default:
		return runtimeStatSame
	}
}

func (d *runtimeInputDiagnostic) rejectDifference(base ownerProbeRuntimeStage, difference runtimeStatDifference) {
	if d != nil && d.stage == 0 && difference != runtimeStatSame {
		d.stage = base + ownerProbeRuntimeStage(difference) - 1
	}
}

func runtimeHeldFileMetadataStage(st unix.Stat_t) ownerProbeRuntimeStage {
	switch {
	case st.Mode&unix.S_IFMT != unix.S_IFREG:
		return ownerProbeStageWrapperPrepareFileType
	case st.Uid != 0:
		return ownerProbeStageWrapperPrepareFileUID
	case st.Gid != 0:
		return ownerProbeStageWrapperPrepareFileGID
	case st.Mode&0022 != 0:
		return ownerProbeStageWrapperPrepareFileWriteMode
	case st.Nlink != 1:
		return ownerProbeStageWrapperPrepareFileLinks
	case st.Size <= 0:
		return ownerProbeStageWrapperPrepareFileSizeLow
	case st.Size > runtimeInputLimit:
		return ownerProbeStageWrapperPrepareFileSizeHigh
	default:
		return 0
	}
}

func heldRegularDigestMetadataStage(st unix.Stat_t, limit int64) ownerProbeRuntimeStage {
	switch {
	case st.Mode&unix.S_IFMT != unix.S_IFREG:
		return ownerProbeStageWrapperPrepareDigestType
	case st.Uid != 0:
		return ownerProbeStageWrapperPrepareDigestUID
	case st.Gid != 0:
		return ownerProbeStageWrapperPrepareDigestGID
	case st.Mode&0022 != 0:
		return ownerProbeStageWrapperPrepareDigestWriteMode
	case st.Size <= 0:
		return ownerProbeStageWrapperPrepareDigestSizeLow
	case st.Size > limit:
		return ownerProbeStageWrapperPrepareDigestSizeHigh
	default:
		return 0
	}
}
