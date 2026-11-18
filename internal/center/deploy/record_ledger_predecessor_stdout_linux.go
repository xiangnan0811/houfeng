//go:build linux

package deploy

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"unicode/utf8"
)

const ledgerPredecessorStdoutLimit = 32 << 10

// This immutable carrier preserves captured bytes, not a re-encoding of Result.
// It must be durably published before the result-written receipt and its ACK.
type ledgerPredecessorStdout struct {
	Version         uint64                          `json:"version"`
	PhaseGeneration uint64                          `json:"phase_generation"`
	RecoveryID      string                          `json:"recovery_id"`
	Application     RecordLedgerApplicationIdentity `json:"application"`
	RealmDigest     string                          `json:"realm_digest"`
	ResultDigest    string                          `json:"result_digest"`
	StdoutBytes     []byte                          `json:"stdout_bytes"`
}

func ledgerPredecessorParseOutput(body []byte, phase uint64) (ledgerPredecessorOutput, error) {
	var result ledgerPredecessorOutput
	if len(body) == 0 || len(body) > ledgerCallbackFrameLimit || body[len(body)-1] != '\n' || bytes.Count(body, []byte{'\n'}) != 1 || !utf8.Valid(body) {
		return result, ErrRecordLedgerFence
	}
	fields, err := ledgerCallbackFields(body, "version", "phase_generation", "status", "operation_id", "reason")
	if err != nil || ledgerCallbackDecode(fields, "version", &result.Version) != nil || ledgerCallbackDecode(fields, "phase_generation", &result.PhaseGeneration) != nil || ledgerCallbackDecode(fields, "status", &result.Status) != nil || ledgerCallbackDecode(fields, "operation_id", &result.OperationID) != nil || ledgerCallbackDecode(fields, "reason", &result.Reason) != nil || !ledgerPredecessorOutputValid(result, phase) {
		return result, ErrRecordLedgerFence
	}
	return result, nil
}

func ledgerPredecessorPublishStdout(dir int, intent ledgerPredecessorIntent, digest string, body []byte) error {
	carrier := ledgerPredecessorStdout{
		Version: 1, PhaseGeneration: intent.PhaseGeneration, RecoveryID: intent.RecoveryID,
		Application: intent.Application, RealmDigest: intent.RealmDigest,
		ResultDigest: digest, StdoutBytes: body,
	}
	encoded, err := json.Marshal(carrier)
	if err != nil || len(encoded) > ledgerPredecessorStdoutLimit {
		return ErrRecordLedgerFence
	}
	_, err = ledgerPublishExact(dir, ledgerPredecessorName("stdout", intent.PhaseGeneration), json.RawMessage(encoded))
	return err
}

func ledgerPredecessorReadStdout(dir int, intent ledgerPredecessorIntent) (ledgerPredecessorStdout, ledgerPredecessorOutput, bool, error) {
	var carrier ledgerPredecessorStdout
	var result ledgerPredecessorOutput
	body, found, err := fenceReadFileLimit(dir, ledgerPredecessorName("stdout", intent.PhaseGeneration), ledgerPredecessorStdoutLimit)
	if err != nil || !found {
		return carrier, result, found, err
	}
	fields, err := ledgerCallbackFields(body, "version", "phase_generation", "recovery_id", "application", "realm_digest", "result_digest", "stdout_bytes")
	var encoded string
	if err != nil || !utf8.Valid(body) ||
		ledgerCallbackDecode(fields, "version", &carrier.Version) != nil ||
		ledgerCallbackDecode(fields, "phase_generation", &carrier.PhaseGeneration) != nil ||
		ledgerCallbackDecode(fields, "recovery_id", &carrier.RecoveryID) != nil ||
		ledgerCallbackDecode(fields, "application", &carrier.Application) != nil ||
		ledgerCallbackDecode(fields, "realm_digest", &carrier.RealmDigest) != nil ||
		ledgerCallbackDecode(fields, "result_digest", &carrier.ResultDigest) != nil ||
		ledgerCallbackDecode(fields, "stdout_bytes", &encoded) != nil {
		return carrier, result, true, ErrRecordLedgerFence
	}
	appFields, err := ledgerCallbackFields(fields["application"], "ID", "SystemIdentifier", "DatabaseName", "DatabaseOID", "Epoch")
	if err != nil {
		return carrier, result, true, ErrRecordLedgerFence
	}
	for _, value := range appFields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return carrier, result, true, ErrRecordLedgerFence
		}
	}
	if carrier.Version != 1 || carrier.PhaseGeneration != intent.PhaseGeneration || carrier.RecoveryID != intent.RecoveryID || carrier.Application != intent.Application || carrier.RealmDigest != intent.RealmDigest || strings.ContainsAny(encoded, "\r\n") {
		return carrier, result, true, ErrRecordLedgerFence
	}
	carrier.StdoutBytes, err = base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || ledgerPredecessorHash(carrier.StdoutBytes) != carrier.ResultDigest {
		return carrier, result, true, ErrRecordLedgerFence
	}
	result, err = ledgerPredecessorParseOutput(carrier.StdoutBytes, intent.PhaseGeneration)
	return carrier, result, true, err
}
