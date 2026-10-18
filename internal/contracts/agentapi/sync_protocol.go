package agentapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

const (
	SyncProtocolRevision          = 2
	SyncProtocolHeader            = "X-Houfeng-Sync-Protocol"
	SyncReplayWindow        int64 = 131072
	LiveSequenceReservation int64 = 1024

	ErrorCodeSyncProtocolRequired      = "sync_protocol_required"
	ErrorCodeSyncProducerMismatch      = "sync_producer_mismatch"
	ErrorCodeSyncBatchRetired          = "sync_batch_retired"
	ErrorCodeSyncBatchIdentityConflict = "sync_batch_identity_conflict"

	SyncDispositionRecorded       = "recorded"
	SyncDispositionExactDuplicate = "exact_duplicate"
	SyncDispositionSuppressed     = "suppressed"
	SyncDispositionDeferred       = "deferred"
)

// ValidSyncProducerID accepts the standard hyphenated UUID representation only.
func ValidSyncProducerID(id string) bool {
	if len(id) != 36 {
		return false
	}
	parsed, err := uuid.Parse(id)
	return err == nil && strings.EqualFold(parsed.String(), id)
}

// CanonicalSyncBatchDigest binds the immutable fact payload to its session and
// sequence. Transport-only state is excluded: replaying a live batch as backfill
// must retain exactly the same identity. Decoding with UseNumber preserves large
// JSON integers in raw reports, while encoding/json sorts object keys recursively.
// The request and all referenced queue payloads remain untouched.
func CanonicalSyncBatchDigest(req SyncRequest) ([32]byte, error) {
	encoded, err := json.Marshal(req)
	if err != nil {
		return [32]byte{}, fmt.Errorf("encode sync batch digest: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var batch map[string]any
	if err := decoder.Decode(&batch); err != nil {
		return [32]byte{}, fmt.Errorf("normalize sync batch digest: %w", err)
	}
	for _, key := range []string{"sync_token", "live_signal", "protocol_revision", "allocated_through", "retired_through"} {
		delete(batch, key)
	}
	for _, key := range []string{"heartbeats", "host_samples", "probe_observations", "ip_quality_reports", "command_results"} {
		if batch[key] == nil {
			batch[key] = []any{}
		}
		for _, value := range batch[key].([]any) {
			fact := value.(map[string]any)
			delete(fact, "is_backfilled")
			if key == "ip_quality_reports" {
				normalizeDigestReportCollections(fact)
			}
		}
	}
	canonical, err := json.Marshal(batch)
	if err != nil {
		return [32]byte{}, fmt.Errorf("encode canonical sync batch: %w", err)
	}
	return sha256.Sum256(canonical), nil
}

// Optional collections already use omitempty, making nil and empty equivalent.
// These non-omitempty collections need explicit normalization. Never traverse
// arbitrary RawJSON/ExtraJSON: a report's own null and empty values are facts.
func normalizeDigestReportCollections(report map[string]any) {
	if product, ok := report["product"].(map[string]any); ok {
		for _, key := range []string{"ptr_names", "mail"} {
			if product[key] == nil {
				product[key] = []any{}
			}
		}
	}
	providers, _ := report["provider_results"].([]any)
	for _, provider := range providers {
		if details, ok := provider.(map[string]any)["details"].(map[string]any); ok {
			if details["scores"] == nil {
				details["scores"] = []any{}
			}
			if details["field_states"] == nil {
				details["field_states"] = map[string]any{}
			}
		}
	}
}
