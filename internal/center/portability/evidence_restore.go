package portability

import (
	"encoding/json"

	"houfeng/internal/center/evidence"
)

type officialEvidenceRestoreMember struct {
	Kind          string          `json:"kind"`
	SchemaVersion uint16          `json:"schema_version"`
	Envelope      json.RawMessage `json:"envelope"`
	Export        json.RawMessage `json:"export"`
}

func encodeOfficialEvidenceRestoreMember(snapshot evidence.CanonicalSnapshot, exported []byte) ([]byte, error) {
	if snapshot.Size() == 0 || len(exported) == 0 || !json.Valid(exported) {
		return nil, ErrInvalidArchive
	}
	if evidence.CanonicalPayloadDigest(exported) != snapshot.Hash() {
		return nil, ErrInvalidArchive
	}
	envelopeJSON, err := json.Marshal(snapshot.Envelope())
	if err != nil {
		return nil, ErrInvalidArchive
	}
	key := snapshot.Envelope().Key
	raw, err := json.Marshal(officialEvidenceRestoreMember{
		Kind:          string(key.Kind),
		SchemaVersion: uint16(key.SchemaVersion),
		Envelope:      envelopeJSON,
		Export:        append(json.RawMessage(nil), exported...),
	})
	if err != nil {
		return nil, ErrInvalidArchive
	}
	return raw, nil
}

func decodeOfficialEvidenceRestoreMember(payload []byte) (officialEvidenceRestoreMember, bool) {
	var member officialEvidenceRestoreMember
	if json.Unmarshal(payload, &member) != nil || member.Kind == "" || member.SchemaVersion == 0 ||
		len(member.Envelope) == 0 || len(member.Export) == 0 || !json.Valid(member.Export) {
		return officialEvidenceRestoreMember{}, false
	}
	return member, true
}

func restoreOfficialEvidenceSnapshot(
	kinds KindSource,
	payload []byte,
) (evidence.CanonicalSnapshot, bool, error) {
	member, ok := decodeOfficialEvidenceRestoreMember(payload)
	if !ok {
		return evidence.CanonicalSnapshot{}, false, nil
	}
	if kinds == nil {
		return evidence.CanonicalSnapshot{}, true, ErrImportSchemaBlocked
	}
	var envelope evidence.SnapshotEnvelope
	if json.Unmarshal(member.Envelope, &envelope) != nil {
		return evidence.CanonicalSnapshot{}, true, ErrUntrustedImportContent
	}
	key := envelope.Key
	if string(key.Kind) != member.Kind || uint16(key.SchemaVersion) != member.SchemaVersion {
		return evidence.CanonicalSnapshot{}, true, ErrImportSchemaBlocked
	}
	kind, err := kinds.LookupKey(key)
	if err != nil {
		return evidence.CanonicalSnapshot{}, true, ErrImportSchemaBlocked
	}
	snapshot, err := evidence.RestoreCanonicalSnapshot(kind.Descriptor(), envelope, member.Export)
	if err != nil {
		return evidence.CanonicalSnapshot{}, true, ErrUntrustedImportContent
	}
	return snapshot, true, nil
}
