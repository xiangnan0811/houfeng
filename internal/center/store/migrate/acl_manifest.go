package migrate

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"unicode/utf8"
)

const canonicalMigrationSetMagic = "HOUFENG-APP-MIGRATION-SET-V1"

// MigrationChecksumEntry is one filename/checksum pair in the application
// migration ledger. Its checksum is raw SHA-256 bytes, not a hex string.
type MigrationChecksumEntry struct {
	Filename string
	Checksum [32]byte
}

// CanonicalMigrationSetFromFS builds the complete canonical migration set for
// one embedded migration source.
func CanonicalMigrationSetFromFS(fsys fs.FS) ([]byte, error) {
	sources, err := migrationSources(fsys)
	if err != nil {
		return nil, err
	}
	entries := make([]MigrationChecksumEntry, 0, len(sources))
	for filename, source := range sources {
		checksumBytes, err := hex.DecodeString(source.checksum)
		if err != nil {
			return nil, fmt.Errorf("decode migration checksum for %q: %w", filename, err)
		}
		if len(checksumBytes) != 32 {
			return nil, fmt.Errorf("migration checksum for %q has length %d, want 32", filename, len(checksumBytes))
		}
		var checksum [32]byte
		copy(checksum[:], checksumBytes)
		entries = append(entries, MigrationChecksumEntry{Filename: filename, Checksum: checksum})
	}
	return CanonicalMigrationSetBodyV1(entries)
}

// CanonicalMigrationSetBodyV1 encodes the complete application migration set
// in its fixed v1 form. The output order is raw filename-byte order.
func CanonicalMigrationSetBodyV1(entries []MigrationChecksumEntry) ([]byte, error) {
	if len(entries) == 0 {
		return nil, fmt.Errorf("canonical migration set has no entries")
	}

	sorted := append([]MigrationChecksumEntry(nil), entries...)
	for _, entry := range sorted {
		if err := validateMigrationFilename(entry.Filename); err != nil {
			return nil, err
		}
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Filename < sorted[j].Filename
	})
	for i := 1; i < len(sorted); i++ {
		if sorted[i-1].Filename == sorted[i].Filename {
			return nil, fmt.Errorf("duplicate migration filename %q", sorted[i].Filename)
		}
	}

	size := len(canonicalMigrationSetMagic)
	for _, entry := range sorted {
		size += 4 + len(entry.Filename) + len(entry.Checksum)
	}
	body := make([]byte, 0, size)
	body = append(body, canonicalMigrationSetMagic...)
	for _, entry := range sorted {
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(entry.Filename)))
		body = append(body, length[:]...)
		body = append(body, entry.Filename...)
		body = append(body, entry.Checksum[:]...)
	}
	return body, nil
}

// ParseCanonicalMigrationSetBodyV1 accepts only fully canonical v1 migration
// set bytes. It rejects duplicate, unsorted, malformed, and trailing data.
func ParseCanonicalMigrationSetBodyV1(body []byte) ([]MigrationChecksumEntry, error) {
	if !bytes.HasPrefix(body, []byte(canonicalMigrationSetMagic)) {
		return nil, fmt.Errorf("invalid canonical migration set magic")
	}
	offset := len(canonicalMigrationSetMagic)
	if offset == len(body) {
		return nil, fmt.Errorf("canonical migration set has no entries")
	}

	entries := make([]MigrationChecksumEntry, 0)
	previousFilename := ""
	for offset < len(body) {
		if len(body)-offset < 4 {
			return nil, fmt.Errorf("truncated canonical migration filename length")
		}
		filenameLength := int(binary.BigEndian.Uint32(body[offset : offset+4]))
		offset += 4
		if filenameLength < 1 || filenameLength > 255 || len(body)-offset < filenameLength+32 {
			return nil, fmt.Errorf("invalid canonical migration filename length")
		}

		filename := string(body[offset : offset+filenameLength])
		offset += filenameLength
		if err := validateMigrationFilename(filename); err != nil {
			return nil, err
		}
		if previousFilename != "" && previousFilename >= filename {
			return nil, fmt.Errorf("canonical migration filenames are not strictly sorted")
		}

		var checksum [32]byte
		copy(checksum[:], body[offset:offset+len(checksum)])
		offset += len(checksum)
		entries = append(entries, MigrationChecksumEntry{Filename: filename, Checksum: checksum})
		previousFilename = filename
	}
	return entries, nil
}

func validateMigrationFilename(filename string) error {
	if len(filename) < 1 || len(filename) > 255 || !utf8.ValidString(filename) {
		return fmt.Errorf("invalid migration filename")
	}
	if !strings.HasSuffix(filename, ".sql") || strings.ContainsAny(filename, "/\\\x00") {
		return fmt.Errorf("invalid migration filename %q", filename)
	}
	return nil
}
