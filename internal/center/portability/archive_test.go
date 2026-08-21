package portability

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestWriteArchiveV1BytesAreDeterministicAndIgnoreInputOrder(t *testing.T) {
	t.Parallel()

	first, err := WriteArchiveV1([]ArchiveEntry{
		{Path: "records/rec_b/document.md", Classification: ArchiveClassMarkdown, Payload: []byte("# B\n")},
		{Path: "records/rec_a/document.md", Classification: ArchiveClassMarkdown, Payload: []byte("# A\n")},
	})
	if err != nil {
		t.Fatalf("WriteArchiveV1() error = %v", err)
	}
	second, err := WriteArchiveV1([]ArchiveEntry{
		{Path: "records/rec_a/document.md", Classification: ArchiveClassMarkdown, Payload: []byte("# A\n")},
		{Path: "records/rec_b/document.md", Classification: ArchiveClassMarkdown, Payload: []byte("# B\n")},
	})
	if err != nil {
		t.Fatalf("WriteArchiveV1(reordered) error = %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("archive bytes drifted when entry order changed: %d vs %d", len(first), len(second))
	}
	manifest, entries, err := ReadArchiveV1(first)
	if err != nil {
		t.Fatalf("ReadArchiveV1() error = %v", err)
	}
	if manifest.Format != ArchiveFormatV1 || len(manifest.Files) != 2 || len(entries) != 2 {
		t.Fatalf("manifest = %#v entries=%d", manifest, len(entries))
	}
	if entries[0].Path != "records/rec_a/document.md" || entries[1].Path != "records/rec_b/document.md" {
		t.Fatalf("entry order = %#v", entries)
	}
}

func TestWriteArchiveV1RejectsUnsafePathsCollisionsAndReservedManifest(t *testing.T) {
	t.Parallel()

	payload := []byte("ok\n")
	for _, tt := range []struct {
		name    string
		entries []ArchiveEntry
	}{
		{name: "parent segment", entries: []ArchiveEntry{{Path: "records/../secret.md", Classification: ArchiveClassMarkdown, Payload: payload}}},
		{name: "absolute", entries: []ArchiveEntry{{Path: "/tmp/record.md", Classification: ArchiveClassMarkdown, Payload: payload}}},
		{name: "backslash", entries: []ArchiveEntry{{Path: `records\rec_a\document.md`, Classification: ArchiveClassMarkdown, Payload: payload}}},
		{name: "empty", entries: []ArchiveEntry{{Path: "", Classification: ArchiveClassMarkdown, Payload: payload}}},
		{name: "reserved manifest", entries: []ArchiveEntry{{Path: ArchiveManifestName, Classification: ArchiveClassMarkdown, Payload: payload}}},
		{name: "collision after clean", entries: []ArchiveEntry{
			{Path: "records/rec_a/document.md", Classification: ArchiveClassMarkdown, Payload: payload},
			{Path: "records/rec_a/./document.md", Classification: ArchiveClassMarkdown, Payload: []byte("other\n")},
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := WriteArchiveV1(tt.entries); !isInvalidArchive(err) {
				t.Fatalf("WriteArchiveV1() error = %v, want invalid archive", err)
			}
		})
	}
}

func TestReadArchiveV1RejectsSymlinkDeviceAndHashMismatch(t *testing.T) {
	t.Parallel()

	valid, err := WriteArchiveV1([]ArchiveEntry{
		{Path: "records/rec_a/document.md", Classification: ArchiveClassMarkdown, Payload: []byte("# A\n")},
	})
	if err != nil {
		t.Fatalf("WriteArchiveV1() error = %v", err)
	}
	if _, _, err := ReadArchiveV1(valid); err != nil {
		t.Fatalf("ReadArchiveV1(valid) error = %v", err)
	}

	tampered := bytes.Replace(valid, []byte("# A\n"), []byte("# Z\n"), 1)
	if _, _, err := ReadArchiveV1(tampered); !isInvalidArchive(err) {
		t.Fatalf("ReadArchiveV1(hash mismatch) error = %v, want invalid archive", err)
	}

	if _, _, err := ReadArchiveV1(hostileZIP(t, zip.FileHeader{
		Name:   "records/rec_a/link.md",
		Method: zip.Store,
	}, []byte("target"), func(header *zip.FileHeader) {
		header.SetMode(os.ModeSymlink | 0o644)
	})); !errors.Is(err, ErrInvalidArchive) {
		t.Fatalf("ReadArchiveV1(symlink) error = %v, want ErrInvalidArchive", err)
	}
}

func TestReadArchiveV1RejectsOversizeAndEntryCount(t *testing.T) {
	t.Parallel()

	tooMany := make([]ArchiveEntry, archiveV1MaxEntries+1)
	for index := range tooMany {
		tooMany[index] = ArchiveEntry{
			Path:           "records/rec_a/file" + strings.Repeat("x", 0) + itoaArchive(index) + ".md",
			Classification: ArchiveClassMarkdown,
			Payload:        []byte("x"),
		}
	}
	if _, err := WriteArchiveV1(tooMany); !isInvalidArchive(err) {
		t.Fatalf("WriteArchiveV1(too many) error = %v, want invalid archive", err)
	}

	huge := bytes.Repeat([]byte("n"), int(archiveV1MaxEntryBytes)+1)
	if _, err := WriteArchiveV1([]ArchiveEntry{{
		Path: "records/rec_a/document.md", Classification: ArchiveClassMarkdown, Payload: huge,
	}}); !isInvalidArchive(err) {
		t.Fatalf("WriteArchiveV1(too large) error = %v, want invalid archive", err)
	}
}

func TestReadArchiveV1RejectsNestedDepthRatioAndWorkingSet(t *testing.T) {
	t.Parallel()

	inner, err := WriteArchiveV1([]ArchiveEntry{{
		Path: "records/rec_a/document.md", Classification: ArchiveClassMarkdown, Payload: []byte("# inner\n"),
	}})
	if err != nil {
		t.Fatalf("WriteArchiveV1(inner) error = %v", err)
	}
	if _, err := WriteArchiveV1([]ArchiveEntry{{
		Path: "records/rec_a/nested.zip", Classification: ArchiveClassAttachment, Payload: inner,
	}}); !errors.Is(err, ErrInvalidArchive) {
		t.Fatalf("WriteArchiveV1(nested) error = %v, want ErrInvalidArchive", err)
	}
	if _, _, err := ReadArchiveV1(nestedArchiveWithManifest(t, inner)); !errors.Is(err, ErrInvalidArchive) {
		t.Fatalf("ReadArchiveV1(nested) error = %v, want ErrInvalidArchive", err)
	}

	ratioZIP := deflatedMemberZIP(t, "records/rec_a/document.md", bytes.Repeat([]byte{0}, 64*1024))
	if _, _, err := ReadArchiveV1(ratioZIP); !errors.Is(err, ErrInvalidArchive) {
		t.Fatalf("ReadArchiveV1(ratio) error = %v, want ErrInvalidArchive", err)
	}

	tight := archiveV1Limits{
		MaxEntries: archiveV1MaxEntries, MaxEntryBytes: archiveV1MaxEntryBytes,
		MaxNameBytes: archiveV1MaxNameBytes, MaxTotalBytes: archiveV1MaxTotalBytes,
		MaxWorkingSetBytes: 8, MaxRatio: archiveV1MaxRatio, MaxDepth: 1,
	}
	small, err := WriteArchiveV1([]ArchiveEntry{{
		Path: "records/rec_a/document.md", Classification: ArchiveClassMarkdown, Payload: []byte("# working set\n"),
	}})
	if err != nil {
		t.Fatalf("WriteArchiveV1(small) error = %v", err)
	}
	if _, _, err := readArchiveV1(small, tight); !errors.Is(err, ErrInvalidArchive) {
		t.Fatalf("readArchiveV1(working set) error = %v, want ErrInvalidArchive", err)
	}
}

func TestReadArchiveV1BoundedHostileCorpus(t *testing.T) {
	t.Parallel()

	valid, err := WriteArchiveV1([]ArchiveEntry{{
		Path: "records/rec_a/document.md", Classification: ArchiveClassMarkdown, Payload: []byte("# A\n"),
	}})
	if err != nil {
		t.Fatalf("WriteArchiveV1() error = %v", err)
	}
	for name, raw := range map[string][]byte{
		"truncated":   valid[:len(valid)/2],
		"empty":       nil,
		"prefix only": []byte{'P', 'K', 0x03, 0x04},
		"parent path": hostileZIP(t, zip.FileHeader{Name: "../secret.md", Method: zip.Store}, []byte("x"), nil),
		"nested zip":  nestedArchiveWithManifest(t, mustInnerArchive(t)),
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := ReadArchiveV1(raw); !errors.Is(err, ErrInvalidArchive) {
				t.Fatalf("ReadArchiveV1(%s) error = %v, want ErrInvalidArchive", name, err)
			}
		})
	}
}

func TestWriteArchiveV1RepeatCountStaysStable(t *testing.T) {
	t.Parallel()

	entries := []ArchiveEntry{
		{Path: "records/rec_a/document.md", Classification: ArchiveClassMarkdown, Payload: []byte("# A\n")},
		{Path: "records/rec_a/evidence.json", Classification: ArchiveClassEvidenceJSON, Payload: []byte(`{"ok":true}`)},
	}
	var previous []byte
	for i := 0; i < 10; i++ {
		got, err := WriteArchiveV1(entries)
		if err != nil {
			t.Fatalf("WriteArchiveV1() iteration %d error = %v", i, err)
		}
		if previous != nil && !bytes.Equal(previous, got) {
			t.Fatalf("WriteArchiveV1() bytes drifted at iteration %d", i)
		}
		previous = got
	}
}

func hostileZIP(t *testing.T, header zip.FileHeader, payload []byte, mutate func(*zip.FileHeader)) []byte {
	t.Helper()
	header.Modified = time.Unix(0, 0).UTC()
	if mutate != nil {
		mutate(&header)
	}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	file, err := writer.CreateHeader(&header)
	if err != nil {
		t.Fatalf("CreateHeader() error = %v", err)
	}
	if _, err := file.Write(payload); err != nil {
		t.Fatalf("write hostile payload: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close hostile zip: %v", err)
	}
	return buffer.Bytes()
}

func deflatedMemberZIP(t *testing.T, name string, payload []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	header := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: time.Unix(0, 0).UTC()}
	file, err := writer.CreateHeader(header)
	if err != nil {
		t.Fatalf("CreateHeader() error = %v", err)
	}
	if _, err := file.Write(payload); err != nil {
		t.Fatalf("write deflated member: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close deflated zip: %v", err)
	}
	return buffer.Bytes()
}

func mustInnerArchive(t *testing.T) []byte {
	t.Helper()
	inner, err := WriteArchiveV1([]ArchiveEntry{{
		Path: "records/rec_a/document.md", Classification: ArchiveClassMarkdown, Payload: []byte("# inner\n"),
	}})
	if err != nil {
		t.Fatalf("WriteArchiveV1(inner) error = %v", err)
	}
	return inner
}

func nestedArchiveWithManifest(t *testing.T, inner []byte) []byte {
	t.Helper()
	sum := sha256.Sum256(inner)
	manifestJSON, err := canonicalManifestJSON(ArchiveManifest{
		Format: ArchiveFormatV1,
		Files: []ArchiveManifestFile{{
			Path:           "records/rec_a/nested.zip",
			SHA256:         hex.EncodeToString(sum[:]),
			Size:           uint64(len(inner)),
			Classification: ArchiveClassAttachment,
		}},
	})
	if err != nil {
		t.Fatalf("canonicalManifestJSON() error = %v", err)
	}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	if err := writeArchiveFile(writer, ArchiveManifestName, manifestJSON); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := writeArchiveFile(writer, "records/rec_a/nested.zip", inner); err != nil {
		t.Fatalf("write nested member: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close nested zip: %v", err)
	}
	return buffer.Bytes()
}

func itoaArchive(value int) string {
	if value == 0 {
		return "0"
	}
	digits := make([]byte, 0, 4)
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}

var _ = io.Discard
