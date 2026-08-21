package portability

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	archiveV1MaxEntries         = 256
	archiveV1MaxEntryBytes      = 8 << 20
	archiveV1MaxNameBytes       = 256
	archiveV1MaxTotalBytes      = 64 << 20
	archiveV1MaxWorkingSetBytes = 64 << 20
	archiveV1MaxRatio           = 100
)

type archiveV1Limits struct {
	MaxEntries         int
	MaxEntryBytes      uint64
	MaxNameBytes       int
	MaxTotalBytes      int
	MaxWorkingSetBytes uint64
	MaxRatio           int64
	MaxDepth           int
}

func defaultArchiveV1Limits() archiveV1Limits {
	return archiveV1Limits{
		MaxEntries:         archiveV1MaxEntries,
		MaxEntryBytes:      archiveV1MaxEntryBytes,
		MaxNameBytes:       archiveV1MaxNameBytes,
		MaxTotalBytes:      archiveV1MaxTotalBytes,
		MaxWorkingSetBytes: archiveV1MaxWorkingSetBytes,
		MaxRatio:           archiveV1MaxRatio,
		MaxDepth:           1,
	}
}

func WriteArchiveV1(entries []ArchiveEntry) ([]byte, error) {
	if len(entries) == 0 || len(entries) > archiveV1MaxEntries {
		return nil, fmt.Errorf("%w: entry count", ErrInvalidArchive)
	}
	manifest, err := newArchiveManifest(entries)
	if err != nil {
		return nil, err
	}
	manifestJSON, err := canonicalManifestJSON(manifest)
	if err != nil {
		return nil, err
	}
	normalized := make([]ArchiveEntry, 0, len(entries))
	for _, entry := range entries {
		pathName, err := normalizeArchivePath(entry.Path)
		if err != nil {
			return nil, err
		}
		if err := rejectNestedArchivePayload(entry.Payload); err != nil {
			return nil, err
		}
		normalized = append(normalized, ArchiveEntry{
			Path:           pathName,
			Classification: entry.Classification,
			Payload:        append([]byte(nil), entry.Payload...),
		})
	}
	sort.Slice(normalized, func(left, right int) bool {
		return normalized[left].Path < normalized[right].Path
	})

	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	if err := writeArchiveFile(writer, ArchiveManifestName, manifestJSON); err != nil {
		return nil, err
	}
	for _, entry := range normalized {
		if err := writeArchiveFile(writer, entry.Path, entry.Payload); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("%w: close archive", ErrInvalidArchive)
	}
	if buffer.Len() == 0 || buffer.Len() > archiveV1MaxTotalBytes {
		return nil, fmt.Errorf("%w: archive size", ErrInvalidArchive)
	}
	return buffer.Bytes(), nil
}

func ReadArchiveV1(raw []byte) (ArchiveManifest, []ArchiveEntry, error) {
	return readArchiveV1(raw, defaultArchiveV1Limits())
}

func readArchiveV1(raw []byte, limits archiveV1Limits) (ArchiveManifest, []ArchiveEntry, error) {
	if len(raw) == 0 || len(raw) > limits.MaxTotalBytes {
		return ArchiveManifest{}, nil, fmt.Errorf("%w: archive size", ErrInvalidArchive)
	}
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return ArchiveManifest{}, nil, fmt.Errorf("%w: zip", ErrInvalidArchive)
	}
	if len(reader.File) == 0 || len(reader.File) > limits.MaxEntries+1 {
		return ArchiveManifest{}, nil, fmt.Errorf("%w: zip entries", ErrInvalidArchive)
	}
	var workingSet uint64
	files := make(map[string]*zip.File, len(reader.File))
	for _, file := range reader.File {
		if err := rejectUnsafeArchiveMember(file); err != nil {
			return ArchiveManifest{}, nil, err
		}
		if file.Method != zip.Store && file.Method != zip.Deflate {
			return ArchiveManifest{}, nil, fmt.Errorf("%w: zip method", ErrInvalidArchive)
		}
		if archiveRatioExceeded(file.UncompressedSize64, file.CompressedSize64, limits.MaxRatio) {
			return ArchiveManifest{}, nil, fmt.Errorf("%w: compression ratio", ErrInvalidArchive)
		}
		workingSet += file.UncompressedSize64
		if workingSet > limits.MaxWorkingSetBytes {
			return ArchiveManifest{}, nil, fmt.Errorf("%w: working set", ErrInvalidArchive)
		}
		normalized, err := normalizeArchivePath(file.Name)
		if err != nil {
			return ArchiveManifest{}, nil, err
		}
		if _, exists := files[normalized]; exists {
			return ArchiveManifest{}, nil, fmt.Errorf("%w: zip collision", ErrInvalidArchive)
		}
		files[normalized] = file
	}
	manifestFile, ok := files[ArchiveManifestName]
	if !ok {
		return ArchiveManifest{}, nil, fmt.Errorf("%w: missing manifest", ErrInvalidArchive)
	}
	manifestBytes, err := readArchiveMember(manifestFile, limits)
	if err != nil {
		return ArchiveManifest{}, nil, err
	}
	manifest, err := parseArchiveManifest(manifestBytes)
	if err != nil {
		return ArchiveManifest{}, nil, err
	}
	if len(files) != len(manifest.Files)+1 {
		return ArchiveManifest{}, nil, fmt.Errorf("%w: zip membership", ErrInvalidArchive)
	}
	entries := make([]ArchiveEntry, 0, len(manifest.Files))
	for _, expected := range manifest.Files {
		file, ok := files[expected.Path]
		if !ok {
			return ArchiveManifest{}, nil, fmt.Errorf("%w: missing member", ErrInvalidArchive)
		}
		payload, err := readArchiveMember(file, limits)
		if err != nil {
			return ArchiveManifest{}, nil, err
		}
		if err := rejectNestedArchivePayload(payload); err != nil {
			return ArchiveManifest{}, nil, err
		}
		if uint64(len(payload)) != expected.Size {
			return ArchiveManifest{}, nil, fmt.Errorf("%w: member size", ErrInvalidArchive)
		}
		sum := sha256.Sum256(payload)
		if hex.EncodeToString(sum[:]) != expected.SHA256 {
			return ArchiveManifest{}, nil, fmt.Errorf("%w: member digest", ErrInvalidArchive)
		}
		entries = append(entries, ArchiveEntry{
			Path:           expected.Path,
			Classification: expected.Classification,
			Payload:        payload,
		})
	}
	return manifest, entries, nil
}

func writeArchiveFile(writer *zip.Writer, name string, payload []byte) error {
	header := &zip.FileHeader{
		Name:     name,
		Method:   zip.Store,
		Modified: time.Unix(0, 0).UTC(),
	}
	header.SetMode(0o644)
	file, err := writer.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("%w: create member", ErrInvalidArchive)
	}
	if _, err := file.Write(payload); err != nil {
		return fmt.Errorf("%w: write member", ErrInvalidArchive)
	}
	return nil
}

func readArchiveMember(file *zip.File, limits archiveV1Limits) ([]byte, error) {
	if file.UncompressedSize64 > limits.MaxEntryBytes {
		return nil, fmt.Errorf("%w: member size", ErrInvalidArchive)
	}
	reader, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("%w: open member", ErrInvalidArchive)
	}
	defer reader.Close()
	payload, err := io.ReadAll(io.LimitReader(reader, int64(limits.MaxEntryBytes)+1))
	if err != nil {
		return nil, fmt.Errorf("%w: read member", ErrInvalidArchive)
	}
	if uint64(len(payload)) != file.UncompressedSize64 || uint64(len(payload)) > limits.MaxEntryBytes {
		return nil, fmt.Errorf("%w: member size", ErrInvalidArchive)
	}
	return payload, nil
}

func rejectNestedArchivePayload(payload []byte) error {
	if looksLikeNestedArchive(payload) {
		return fmt.Errorf("%w: nested archive", ErrInvalidArchive)
	}
	return nil
}

func looksLikeNestedArchive(payload []byte) bool {
	switch {
	case bytes.HasPrefix(payload, []byte{'P', 'K', 0x03, 0x04}), bytes.HasPrefix(payload, []byte{'P', 'K', 0x05, 0x06}):
		return true
	case bytes.HasPrefix(payload, []byte{0x1f, 0x8b, 0x08}):
		return true
	case bytes.HasPrefix(payload, []byte{0x28, 0xb5, 0x2f, 0xfd}):
		return true
	case bytes.HasPrefix(payload, []byte{'7', 'z', 0xbc, 0xaf, 0x27, 0x1c}):
		return true
	case bytes.HasPrefix(payload, []byte{'R', 'a', 'r', '!'}):
		return true
	case len(payload) >= 262 && string(payload[257:262]) == "ustar":
		return true
	default:
		return false
	}
}

func archiveRatioExceeded(uncompressed, compressed uint64, maxRatio int64) bool {
	if maxRatio <= 0 || uncompressed == 0 {
		return maxRatio <= 0
	}
	if compressed == 0 {
		return uncompressed > 0
	}
	return uncompressed/compressed > uint64(maxRatio)
}

func isInvalidArchive(err error) bool {
	return err != nil && (err == ErrInvalidArchive || errors.Is(err, ErrInvalidArchive))
}

func rejectUnsafeArchiveMember(file *zip.File) error {
	mode := file.Mode()
	if mode&os.ModeSymlink != 0 || mode&os.ModeDevice != 0 || mode&os.ModeNamedPipe != 0 ||
		mode&os.ModeSocket != 0 || mode&fs.ModeIrregular != 0 || file.NonUTF8 {
		return fmt.Errorf("%w: unsafe member", ErrInvalidArchive)
	}
	return nil
}

func validateArchivePayload(payload []byte) error {
	if payload == nil || uint64(len(payload)) == 0 || uint64(len(payload)) > archiveV1MaxEntryBytes {
		return fmt.Errorf("%w: payload", ErrInvalidArchive)
	}
	return nil
}

func normalizeArchivePath(name string) (string, error) {
	if len(name) > archiveV1MaxNameBytes {
		return "", fmt.Errorf("%w: path bytes", ErrInvalidArchive)
	}
	if name == "" || !utf8.ValidString(name) || strings.IndexByte(name, 0) >= 0 ||
		strings.Contains(name, "\\") || path.IsAbs(name) {
		return "", fmt.Errorf("%w: unsafe path", ErrInvalidArchive)
	}
	trimmed := strings.TrimSuffix(name, "/")
	if trimmed == "" || strings.Contains(trimmed, "//") {
		return "", fmt.Errorf("%w: unsafe path", ErrInvalidArchive)
	}
	segments := strings.Split(trimmed, "/")
	for _, segment := range segments {
		if segment == ".." || segment == "." || segment == "" || segment != strings.TrimSpace(segment) ||
			strings.HasSuffix(segment, ".") || strings.Contains(segment, ":") {
			return "", fmt.Errorf("%w: unsafe path", ErrInvalidArchive)
		}
		for _, value := range segment {
			if value < 0x20 || value == 0x7f {
				return "", fmt.Errorf("%w: unsafe path", ErrInvalidArchive)
			}
		}
	}
	normalized := path.Clean(trimmed)
	if normalized == "." || normalized == ".." || strings.HasPrefix(normalized, "../") || normalized != trimmed {
		return "", fmt.Errorf("%w: unsafe path", ErrInvalidArchive)
	}
	return normalized, nil
}
