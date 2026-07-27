package migrate

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io/fs"
	"strings"
	"testing"

	"houfeng/db/migrations"
)

func TestCanonicalMigrationSetBodyV1SortsAndRoundTrips(t *testing.T) {
	entries := []MigrationChecksumEntry{
		{Filename: "0051_create_record_platform_foundation.sql", Checksum: checksumFromHex(t, strings.Repeat("51", 32))},
		{Filename: "0001_initial_schema.sql", Checksum: checksumFromHex(t, strings.Repeat("01", 32))},
	}

	body, err := CanonicalMigrationSetBodyV1(entries)
	if err != nil {
		t.Fatalf("CanonicalMigrationSetBodyV1() error = %v", err)
	}

	want := append([]byte("HOUFENG-APP-MIGRATION-SET-V1"), canonicalMigrationSetEntry("0001_initial_schema.sql", checksumFromHex(t, strings.Repeat("01", 32)))...)
	want = append(want, canonicalMigrationSetEntry("0051_create_record_platform_foundation.sql", checksumFromHex(t, strings.Repeat("51", 32)))...)
	if !bytes.Equal(body, want) {
		t.Fatalf("CanonicalMigrationSetBodyV1() = %x, want %x", body, want)
	}

	got, err := ParseCanonicalMigrationSetBodyV1(body)
	if err != nil {
		t.Fatalf("ParseCanonicalMigrationSetBodyV1() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("parsed entry count = %d, want 2", len(got))
	}
	if got[0].Filename != "0001_initial_schema.sql" || got[1].Filename != "0051_create_record_platform_foundation.sql" {
		t.Fatalf("parsed filenames = %#v, want raw-byte sorted order", got)
	}
	if got[0].Checksum != checksumFromHex(t, strings.Repeat("01", 32)) || got[1].Checksum != checksumFromHex(t, strings.Repeat("51", 32)) {
		t.Fatalf("parsed checksums = %#v, want original checksums", got)
	}
}

func TestCanonicalMigrationSetBodyV1RejectsNonCanonicalEntries(t *testing.T) {
	checksum := checksumFromHex(t, strings.Repeat("ab", 32))
	tests := []struct {
		name    string
		entries []MigrationChecksumEntry
	}{
		{
			name:    "empty filename",
			entries: []MigrationChecksumEntry{{Filename: "", Checksum: checksum}},
		},
		{
			name:    "not a sql file",
			entries: []MigrationChecksumEntry{{Filename: "0001_initial_schema.txt", Checksum: checksum}},
		},
		{
			name:    "path separator",
			entries: []MigrationChecksumEntry{{Filename: "nested/0001_initial_schema.sql", Checksum: checksum}},
		},
		{
			name:    "nul byte",
			entries: []MigrationChecksumEntry{{Filename: "0001\x00_initial_schema.sql", Checksum: checksum}},
		},
		{
			name:    "oversized filename",
			entries: []MigrationChecksumEntry{{Filename: strings.Repeat("a", 252) + ".sql", Checksum: checksum}},
		},
		{
			name: "duplicate filename",
			entries: []MigrationChecksumEntry{
				{Filename: "0001_initial_schema.sql", Checksum: checksum},
				{Filename: "0001_initial_schema.sql", Checksum: checksum},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := CanonicalMigrationSetBodyV1(tt.entries); err == nil {
				t.Fatal("CanonicalMigrationSetBodyV1() error = nil, want non-canonical entry rejection")
			}
		})
	}
}

func TestParseCanonicalMigrationSetBodyV1RejectsNonCanonicalBytes(t *testing.T) {
	checksum := checksumFromHex(t, strings.Repeat("ab", 32))
	reverseSorted := append([]byte("HOUFENG-APP-MIGRATION-SET-V1"), canonicalMigrationSetEntry("0051_create_record_platform_foundation.sql", checksum)...)
	reverseSorted = append(reverseSorted, canonicalMigrationSetEntry("0001_initial_schema.sql", checksum)...)
	truncated := append([]byte("HOUFENG-APP-MIGRATION-SET-V1"), 0, 0, 0, 4, 'x')

	for _, body := range [][]byte{
		[]byte("wrong magic"),
		reverseSorted,
		truncated,
	} {
		if _, err := ParseCanonicalMigrationSetBodyV1(body); err == nil {
			t.Fatalf("ParseCanonicalMigrationSetBodyV1(%x) error = nil, want rejection", body)
		}
	}
}

func TestCanonicalMigrationSetFromFSCoversEveryEmbeddedMigration(t *testing.T) {
	body, err := CanonicalMigrationSetFromFS(migrations.FS)
	if err != nil {
		t.Fatalf("CanonicalMigrationSetFromFS() error = %v", err)
	}
	entries, err := ParseCanonicalMigrationSetBodyV1(body)
	if err != nil {
		t.Fatalf("ParseCanonicalMigrationSetBodyV1() error = %v", err)
	}
	if got, want := len(entries), frozenR1MigrationSourceCount; got != want {
		t.Fatalf("canonical entry count = %d, want frozen r1 count %d", got, want)
	}
	for _, entry := range entries {
		payload, err := fs.ReadFile(migrations.FS, entry.Filename)
		if err != nil {
			t.Fatalf("read embedded migration %q: %v", entry.Filename, err)
		}
		if want := sha256.Sum256(payload); entry.Checksum != want {
			t.Fatalf("canonical entry %q checksum = %x, want %x", entry.Filename, entry.Checksum, want)
		}
	}
}

func checksumFromHex(t *testing.T, value string) [32]byte {
	t.Helper()
	decoded, err := hex.DecodeString(value)
	if err != nil {
		t.Fatalf("decode checksum %q: %v", value, err)
	}
	var checksum [32]byte
	copy(checksum[:], decoded)
	return checksum
}

func canonicalMigrationSetEntry(filename string, checksum [32]byte) []byte {
	entry := make([]byte, 4+len(filename)+len(checksum))
	binary.BigEndian.PutUint32(entry[:4], uint32(len(filename)))
	copy(entry[4:], filename)
	copy(entry[4+len(filename):], checksum[:])
	return entry
}
