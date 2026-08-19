package store

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

const maxRecordPurgeCommandBytes = 64 * 1024

// recordPurgeFunctionCommand is the authority envelope every per-record purge
// function accepts. The SQL side checks an exact sorted key set, so the JSON tags
// are part of the contract and adapters must not each invent their own shape.
type recordPurgeFunctionCommand struct {
	OperationID     string `json:"operation_id"`
	ReservationID   string `json:"reservation_id"`
	ProjectID       string `json:"project_id"`
	RecordID        string `json:"record_id"`
	FenceEpoch      int64  `json:"fence_epoch"`
	LedgerSequence  int64  `json:"ledger_sequence"`
	LedgerEntryHash string `json:"ledger_entry_hash"`
}

func encodeRecordPurgeCommand(command any) ([]byte, error) {
	encoded, err := json.Marshal(command)
	if err != nil || len(encoded) == 0 || len(encoded) > maxRecordPurgeCommandBytes {
		return nil, fmt.Errorf("encode record controlled delete command")
	}
	return encoded, nil
}

func encodeRecordPurgeLedgerHash(value [32]byte) string {
	return hex.EncodeToString(value[:])
}

func digestRecordPurgeStrings(domain string, values ...string) [sha256.Size]byte {
	encoded := make([][]byte, len(values))
	for index, value := range values {
		encoded[index] = []byte(value)
	}
	return digestRecordPurgeBytes(domain, encoded...)
}

// digestRecordPurgeBytes length-prefixes every field so no two different field
// splits can hash the same, which is what lets a receipt digest be compared
// rather than trusted.
func digestRecordPurgeBytes(domain string, values ...[]byte) [sha256.Size]byte {
	hasher := sha256.New()
	writeRecordPurgeDigestField(hasher, []byte(domain))
	for _, value := range values {
		writeRecordPurgeDigestField(hasher, value)
	}
	var digest [sha256.Size]byte
	copy(digest[:], hasher.Sum(nil))
	return digest
}

func writeRecordPurgeDigestField(hasher interface{ Write([]byte) (int, error) }, value []byte) {
	_, _ = hasher.Write(recordPurgeUint64(uint64(len(value))))
	_, _ = hasher.Write(value)
}

func recordPurgeUint64(value uint64) []byte {
	encoded := make([]byte, 8)
	binary.BigEndian.PutUint64(encoded, value)
	return encoded
}

func equalRecordPurgeDigest(raw []byte, digest [sha256.Size]byte) bool {
	if len(raw) != sha256.Size {
		return false
	}
	var difference byte
	for index := range raw {
		difference |= raw[index] ^ digest[index]
	}
	return difference == 0
}
