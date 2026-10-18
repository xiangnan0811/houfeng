package syncing

import (
	"strconv"

	"houfeng/internal/contracts/agentapi"
)

// ValidateBatchProtocol is shared by service and repository admission. Direct
// repository callers cannot bypass the wire revision or replay control bounds.
func ValidateBatchProtocol(batch Batch) error {
	if batch.ProtocolRevision != agentapi.SyncProtocolRevision {
		return ErrSyncProtocolRequired
	}
	if !agentapi.ValidSyncProducerID(batch.ProducerID) || batch.SessionID == "" ||
		batch.AllocatedThrough < 0 || batch.RetiredThrough < 0 || batch.RetiredThrough > batch.AllocatedThrough ||
		batch.BatchSequence < 0 || batch.BatchSequence > batch.AllocatedThrough {
		return ErrInvalidReplayState
	}
	if batch.LiveSignal != nil && (batch.LiveSignal.Sequence <= 0 || batch.LiveSignal.Fingerprint == "") {
		return ErrInvalidReplayState
	}
	if batch.BatchSequence == 0 {
		if len(batch.Heartbeats)+len(batch.Observations.HostSamples)+len(batch.Observations.ProbeObservations)+len(batch.IPQualityReports)+len(batch.CommandResults) != 0 {
			return ErrInvalidReplayState
		}
		return nil
	}
	if batch.BatchSequence <= batch.RetiredThrough {
		return ErrSyncBatchRetired
	}
	if len(batch.Heartbeats) == 0 {
		return ErrHeartbeatRequired
	}
	identity := batch.ProducerID + ":" + strconv.FormatInt(batch.BatchSequence, 10)
	for _, fact := range batch.Heartbeats {
		if fact.SyncBatchID != identity {
			return ErrInvalidReplayState
		}
	}
	for _, fact := range batch.Observations.HostSamples {
		if fact.SyncBatchID != identity {
			return ErrInvalidReplayState
		}
	}
	for _, fact := range batch.Observations.ProbeObservations {
		if fact.SyncBatchID != identity {
			return ErrInvalidReplayState
		}
	}
	for _, fact := range batch.IPQualityReports {
		if fact.SyncBatchID != identity {
			return ErrInvalidReplayState
		}
	}
	return nil
}
