package attachments

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io/fs"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestArchiveLimitsValidateThroughAdmissionLimits(t *testing.T) {
	t.Parallel()

	base := DefaultLimits()
	limits := DefaultAdmissionLimits(base)
	if limits.Archive.MaxEntries <= 0 || limits.Archive.MaxEntryNameBytes <= 0 ||
		limits.Archive.MaxNestingDepth <= 0 || limits.Archive.MaxExpandedBytes <= 0 ||
		limits.Archive.MaxCompressionRatio <= 0 {
		t.Fatalf("DefaultAdmissionLimits().Archive has non-positive budget: %#v", limits.Archive)
	}

	tests := []struct {
		name   string
		mutate func(*ArchiveLimits)
	}{
		{name: "entries", mutate: func(value *ArchiveLimits) { value.MaxEntries = 0 }},
		{name: "entry name", mutate: func(value *ArchiveLimits) { value.MaxEntryNameBytes = 0 }},
		{name: "nesting", mutate: func(value *ArchiveLimits) { value.MaxNestingDepth = 0 }},
		{name: "expanded bytes", mutate: func(value *ArchiveLimits) { value.MaxExpandedBytes = 0 }},
		{name: "compression ratio", mutate: func(value *ArchiveLimits) { value.MaxCompressionRatio = 0 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			invalid := DefaultAdmissionLimits(base)
			tt.mutate(&invalid.Archive)
			if err := invalid.Validate(); !errors.Is(err, ErrInvalidAdmissionRequest) {
				t.Fatalf("AdmissionLimits.Validate() error = %v, want ErrInvalidAdmissionRequest", err)
			}
		})
	}
}

func TestArchiveAdmissionClassifiesValidContainers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		fileName  string
		mediaType string
		content   func(testing.TB) []byte
		wantMedia string
	}{
		{name: "ZIP", fileName: "bundle.zip", mediaType: "application/zip", content: func(t testing.TB) []byte {
			return archiveZIP(t, []archiveFixtureEntry{{name: "notes.txt", body: []byte("zip payload")}})
		}, wantMedia: "application/zip"},
		{name: "TAR", fileName: "bundle.tar", mediaType: "application/x-tar", content: func(t testing.TB) []byte {
			return archiveTAR(t, []archiveFixtureEntry{{name: "notes.txt", body: []byte("tar payload")}})
		}, wantMedia: "application/x-tar"},
		{name: "GZIP", fileName: "notes.txt.gz", mediaType: "application/gzip", content: func(t testing.TB) []byte {
			return archiveGZIP(t, "notes.txt", []byte("gzip payload"))
		}, wantMedia: "application/gzip"},
		{name: "Zstandard", fileName: "notes.txt.zst", mediaType: "application/zstd", content: func(t testing.TB) []byte {
			return archiveZstandard(t, []byte("zstandard payload"))
		}, wantMedia: "application/zstd"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			content := tt.content(t)
			result, err := AdmitContent(context.Background(), admissionRequest(tt.fileName, tt.mediaType, content), DefaultAdmissionLimits(DefaultLimits()))
			if err != nil {
				t.Fatalf("AdmitContent() error = %v", err)
			}
			if result.MediaType != tt.wantMedia || result.Profile != ProcessorProfileArchive {
				t.Fatalf("AdmitContent() = %#v, want media %q archive profile", result, tt.wantMedia)
			}
		})
	}
}

func TestArchiveAdmissionRejectsActiveMarkupBeyondPayloadPrefix(t *testing.T) {
	t.Parallel()

	body := append(bytes.Repeat([]byte(" "), archiveProbeBytes+1), []byte("<script>alert(1)</script>")...)
	content := archiveZIP(t, []archiveFixtureEntry{{name: "notes.txt", body: body}})
	limits := DefaultAdmissionLimits(DefaultLimits())
	limits.Archive.MaxCompressionRatio = 1 << 20
	_, err := AdmitContent(
		context.Background(),
		admissionRequest("bundle.zip", "application/zip", content),
		limits,
	)
	if !errors.Is(err, ErrAdmissionRejected) {
		t.Fatalf("AdmitContent() error = %v, want ErrAdmissionRejected", err)
	}
}

func TestArchiveAdmissionRejectsBodyOnloadBeyondPayloadPrefix(t *testing.T) {
	t.Parallel()

	body := append(bytes.Repeat([]byte(" "), archiveProbeBytes+1), []byte("<body onload=alert(1)>")...)
	content := archiveZIP(t, []archiveFixtureEntry{{name: "notes.txt", body: body}})
	limits := DefaultAdmissionLimits(DefaultLimits())
	limits.Archive.MaxCompressionRatio = 1 << 20
	_, err := AdmitContent(
		context.Background(),
		admissionRequest("bundle.zip", "application/zip", content),
		limits,
	)
	if !errors.Is(err, ErrAdmissionRejected) {
		t.Fatalf("AdmitContent() error = %v, want ErrAdmissionRejected", err)
	}
}

func TestArchivePayloadProbeDetectsActiveMarkupAcrossReadBoundary(t *testing.T) {
	t.Parallel()

	body := append(bytes.Repeat([]byte(" "), 32*1024-3), []byte("<script>alert(1)</script>")...)
	probe, err := readArchivePayload(context.Background(), bytes.NewReader(body), int64(len(body)), false)
	if err != nil {
		t.Fatalf("readArchivePayload() error = %v", err)
	}
	if !dangerousArchivePayload("notes.txt", probe) {
		t.Fatal("dangerousArchivePayload() = false, want active markup split across reads")
	}
}

func TestArchivePayloadProbeDetectsBodyOnloadAcrossReadBoundary(t *testing.T) {
	t.Parallel()

	body := append(bytes.Repeat([]byte(" "), 32*1024-3), []byte("<body onload=alert(1)>")...)
	probe, err := readArchivePayload(context.Background(), bytes.NewReader(body), int64(len(body)), false)
	if err != nil {
		t.Fatalf("readArchivePayload() error = %v", err)
	}
	if !dangerousArchivePayload("notes.txt", probe) {
		t.Fatal("dangerousArchivePayload() = false, want body tag split across reads")
	}
}

func TestArchivePayloadProbeRejectsActiveMarkupAfterMisleadingCandidatesAcrossReadBoundary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body []byte
	}{
		{name: "malformed autolink attribute", body: []byte("<https://x onclick=alert(1)>")},
		{name: "comment quote before image", body: []byte("<!-- \" --> <img src=x onerror=alert(1)>")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body := append(bytes.Repeat([]byte(" "), 32*1024-3), tt.body...)
			probe, err := readArchivePayload(context.Background(), bytes.NewReader(body), int64(len(body)), false)
			if err != nil {
				t.Fatalf("readArchivePayload() error = %v", err)
			}
			if !dangerousArchivePayload("notes.txt", probe) {
				t.Fatal("dangerousArchivePayload() = false, want active markup split across reads")
			}
		})
	}
}

func TestArchiveAdmissionRejectsUnsafeMarkdownURIAutolinks(t *testing.T) {
	t.Parallel()

	for _, scheme := range []string{"javascript:alert(1)", "data:text/plain,unsafe", "mailto:unsafe@example.invalid", "http:foo"} {
		scheme := scheme
		t.Run(scheme, func(t *testing.T) {
			t.Parallel()
			content := archiveZIP(t, []archiveFixtureEntry{{name: "notes.txt", body: []byte("See <" + scheme + "> for details.\n")}})
			_, err := AdmitContent(
				context.Background(),
				admissionRequest("bundle.zip", "application/zip", content),
				DefaultAdmissionLimits(DefaultLimits()),
			)
			if !errors.Is(err, ErrAdmissionRejected) {
				t.Fatalf("AdmitContent() error = %v, want ErrAdmissionRejected", err)
			}
		})
	}
}

func TestReadArchivePayloadCancelsActiveTextScan(t *testing.T) {
	t.Parallel()

	_, err := readArchivePayload(
		newCancellationAfterChecksContext(12),
		bytes.NewReader(bytes.Repeat([]byte("x"), 64*1024)),
		64*1024,
		false,
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("readArchivePayload() error = %v, want context.Canceled", err)
	}
}

func TestArchiveAdmissionAcceptsZIPDataDescriptorVariants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content []byte
	}{
		{name: "unsigned ZIP32", content: archiveBase64Fixture(t, "testdata/go-no-datadesc-sig.zip.b64")},
		{name: "signed ZIP32", content: archiveZIP(t, []archiveFixtureEntry{{name: "safe.txt", body: []byte("safe"), store: true}})},
		{name: "unsigned ZIP64", content: archiveZIP64Fixture(t, archiveZIP64FixtureOptions{dataDescriptor: true})},
		{name: "signed ZIP64", content: archiveZIP64Fixture(t, archiveZIP64FixtureOptions{dataDescriptor: true, signedDescriptor: true})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := AdmitContent(
				context.Background(),
				admissionRequest("bundle.zip", "application/zip", tt.content),
				DefaultAdmissionLimits(DefaultLimits()),
			)
			if err != nil {
				t.Fatalf("AdmitContent() error = %v", err)
			}
			if result.MediaType != "application/zip" || result.Profile != ProcessorProfileArchive {
				t.Fatalf("AdmitContent() = %#v, want ZIP archive profile", result)
			}
		})
	}
}

func TestArchiveAdmissionAcceptsZIP64EntryMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content []byte
	}{
		{name: "central size sentinels", content: archiveBase64Fixture(t, "testdata/zip64.zip.b64")},
		{name: "central offset sentinel", content: archiveZIP64Fixture(t, archiveZIP64FixtureOptions{centralOffset64: true})},
		{name: "local size sentinels", content: archiveZIP64Fixture(t, archiveZIP64FixtureOptions{localSizes64: true})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := AdmitContent(
				context.Background(),
				admissionRequest("bundle.zip", "application/zip", tt.content),
				DefaultAdmissionLimits(DefaultLimits()),
			)
			if err != nil {
				t.Fatalf("AdmitContent() error = %v", err)
			}
			if result.MediaType != "application/zip" || result.Profile != ProcessorProfileArchive {
				t.Fatalf("AdmitContent() = %#v, want ZIP archive profile", result)
			}
		})
	}
}

func TestArchiveAdmissionRejectsInvalidZIP64ExtraFields(t *testing.T) {
	t.Parallel()

	valid := archiveZIP64Fixture(t, archiveZIP64FixtureOptions{})
	centralOffset := bytes.Index(valid, []byte{'P', 'K', 0x01, 0x02})
	if centralOffset < 0 {
		t.Fatal("ZIP64 fixture has no central directory")
	}
	centralExtraOffset := centralOffset + 46 + int(binary.LittleEndian.Uint16(valid[centralOffset+28:centralOffset+30]))

	missing := append([]byte(nil), valid...)
	binary.LittleEndian.PutUint16(missing[centralExtraOffset:centralExtraOffset+2], 2)
	duplicate := archiveInsertZIPCentralExtra(t, valid, append([]byte(nil), valid[centralExtraOffset:centralExtraOffset+20]...))
	truncated := append([]byte(nil), valid...)
	binary.LittleEndian.PutUint16(truncated[centralExtraOffset+2:centralExtraOffset+4], 17)
	contradictory := append([]byte(nil), valid...)
	binary.LittleEndian.PutUint64(contradictory[centralExtraOffset+4:centralExtraOffset+12], 5)
	standard := archiveZIP(t, []archiveFixtureEntry{{name: "safe.txt", body: []byte("safe"), store: true}})
	unnecessary := archiveInsertZIPCentralExtra(t, standard, archiveZIPExtraField(1, nil))
	ambiguous := archiveAppendToFirstZIPCentralExtra(t, valid, make([]byte, 8))

	tests := []struct {
		name    string
		content []byte
	}{
		{name: "missing", content: missing},
		{name: "duplicate", content: duplicate},
		{name: "truncated", content: truncated},
		{name: "contradictory", content: contradictory},
		{name: "unnecessary", content: unnecessary},
		{name: "ambiguous", content: ambiguous},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := AdmitContent(
				context.Background(),
				admissionRequest("bundle.zip", "application/zip", tt.content),
				DefaultAdmissionLimits(DefaultLimits()),
			)
			if !errors.Is(err, ErrAdmissionRejected) {
				t.Fatalf("AdmitContent() error = %v, want ErrAdmissionRejected", err)
			}
		})
	}
}

func TestArchiveAdmissionAcceptsMultipleGZIPMembers(t *testing.T) {
	t.Parallel()

	content := append(
		archiveGZIP(t, "one.txt", []byte("first member")),
		archiveGZIP(t, "two.txt", []byte("second member"))...,
	)
	result, err := AdmitContent(
		context.Background(),
		admissionRequest("bundle.gz", "application/gzip", content),
		DefaultAdmissionLimits(DefaultLimits()),
	)
	if err != nil {
		t.Fatalf("AdmitContent() error = %v", err)
	}
	if result.MediaType != "application/gzip" || result.Profile != ProcessorProfileArchive {
		t.Fatalf("AdmitContent() = %#v, want GZIP archive profile", result)
	}
}

func TestArchiveAdmissionAcceptsEmptyGZIPMemberWithinNonEmptyStream(t *testing.T) {
	t.Parallel()

	content := append(
		archiveGZIP(t, "empty.txt", nil),
		archiveGZIP(t, "payload.txt", []byte("payload"))...,
	)
	result, err := AdmitContent(
		context.Background(),
		admissionRequest("bundle.gz", "application/gzip", content),
		DefaultAdmissionLimits(DefaultLimits()),
	)
	if err != nil {
		t.Fatalf("AdmitContent() error = %v", err)
	}
	if result.MediaType != "application/gzip" || result.Profile != ProcessorProfileArchive {
		t.Fatalf("AdmitContent() = %#v, want GZIP archive profile", result)
	}
}

func TestArchiveAdmissionRejectsDuplicateGZIPMemberPaths(t *testing.T) {
	t.Parallel()

	content := append(
		archiveGZIP(t, "notes.txt", []byte("first member")),
		archiveGZIP(t, "notes.txt", []byte("second member"))...,
	)
	_, err := AdmitContent(
		context.Background(),
		admissionRequest("bundle.gz", "application/gzip", content),
		DefaultAdmissionLimits(DefaultLimits()),
	)
	if !errors.Is(err, ErrAdmissionRejected) {
		t.Fatalf("AdmitContent() error = %v, want ErrAdmissionRejected", err)
	}
}

func TestArchiveAdmissionValidatesEveryGZIPMemberName(t *testing.T) {
	t.Parallel()

	content := append(
		archiveGZIP(t, "safe.txt", []byte("first member")),
		archiveGZIP(t, "../escape.txt", []byte("second member"))...,
	)
	_, err := AdmitContent(
		context.Background(),
		admissionRequest("bundle.gz", "application/gzip", content),
		DefaultAdmissionLimits(DefaultLimits()),
	)
	if !errors.Is(err, ErrAdmissionRejected) {
		t.Fatalf("AdmitContent() error = %v, want ErrAdmissionRejected", err)
	}
}

func TestArchiveAdmissionCountsEveryGZIPMember(t *testing.T) {
	t.Parallel()

	content := append(
		archiveGZIP(t, "one.txt", []byte("first member")),
		archiveGZIP(t, "two.txt", []byte("second member"))...,
	)
	limits := DefaultAdmissionLimits(DefaultLimits())
	limits.Archive.MaxEntries = 1
	_, err := AdmitContent(
		context.Background(),
		admissionRequest("bundle.gz", "application/gzip", content),
		limits,
	)
	if !errors.Is(err, ErrAdmissionLimitExceeded) {
		t.Fatalf("AdmitContent() error = %v, want ErrAdmissionLimitExceeded", err)
	}
}

func TestArchiveAdmissionAggregatesGZIPExpandedBytes(t *testing.T) {
	t.Parallel()

	content := append(
		archiveGZIP(t, "one.txt", []byte("1234")),
		archiveGZIP(t, "two.txt", []byte("5678"))...,
	)
	limits := DefaultAdmissionLimits(DefaultLimits())
	limits.Archive.MaxExpandedBytes = 7
	_, err := AdmitContent(
		context.Background(),
		admissionRequest("bundle.gz", "application/gzip", content),
		limits,
	)
	if !errors.Is(err, ErrAdmissionLimitExceeded) {
		t.Fatalf("AdmitContent() error = %v, want ErrAdmissionLimitExceeded", err)
	}
}

func TestArchiveAdmissionAggregatesGZIPCompressionRatio(t *testing.T) {
	t.Parallel()

	payload := bytes.Repeat([]byte{0}, 4096)
	content := append(
		archiveGZIP(t, "one.bin", payload),
		archiveGZIP(t, "two.bin", payload)...,
	)
	limits := DefaultAdmissionLimits(DefaultLimits())
	limits.Archive.MaxCompressionRatio = int64(len(payload)+len(content)-1) / int64(len(content))
	_, err := AdmitContent(
		context.Background(),
		admissionRequest("bundle.gz", "application/gzip", content),
		limits,
	)
	if !errors.Is(err, ErrAdmissionLimitExceeded) {
		t.Fatalf("AdmitContent() error = %v, want ErrAdmissionLimitExceeded", err)
	}
}

func TestArchiveAdmissionRejectsCorruptLaterGZIPMember(t *testing.T) {
	t.Parallel()

	content := append(
		archiveGZIP(t, "one.txt", []byte("first member")),
		archiveGZIP(t, "two.txt", []byte("second member"))...,
	)
	content[len(content)-8] ^= 0xff
	_, err := AdmitContent(
		context.Background(),
		admissionRequest("bundle.gz", "application/gzip", content),
		DefaultAdmissionLimits(DefaultLimits()),
	)
	if !errors.Is(err, ErrAdmissionRejected) {
		t.Fatalf("AdmitContent() error = %v, want ErrAdmissionRejected", err)
	}
}

func TestArchiveAdmissionRejectsGZIPTrailingBytes(t *testing.T) {
	t.Parallel()

	content := append(archiveGZIP(t, "notes.txt", []byte("payload")), []byte("trailing")...)
	_, err := AdmitContent(
		context.Background(),
		admissionRequest("bundle.gz", "application/gzip", content),
		DefaultAdmissionLimits(DefaultLimits()),
	)
	if !errors.Is(err, ErrAdmissionRejected) {
		t.Fatalf("AdmitContent() error = %v, want ErrAdmissionRejected", err)
	}
}

func TestArchiveAdmissionAcceptsConcatenatedZstandardFrames(t *testing.T) {
	t.Parallel()

	content := append(
		archiveZstandard(t, []byte("first frame")),
		archiveZstandard(t, []byte("second frame"))...,
	)
	result, err := AdmitContent(
		context.Background(),
		admissionRequest("bundle.zst", "application/zstd", content),
		DefaultAdmissionLimits(DefaultLimits()),
	)
	if err != nil {
		t.Fatalf("AdmitContent() error = %v", err)
	}
	if result.MediaType != "application/zstd" || result.Profile != ProcessorProfileArchive {
		t.Fatalf("AdmitContent() = %#v, want Zstandard archive profile", result)
	}
}

func TestArchiveAdmissionAcceptsLeadingZstandardSkippableFrame(t *testing.T) {
	t.Parallel()

	content := append(
		archiveZstandardSkippableFrame([]byte("application metadata")),
		archiveZstandard(t, []byte("payload"))...,
	)
	result, err := AdmitContent(
		context.Background(),
		admissionRequest("bundle.zst", "application/zstd", content),
		DefaultAdmissionLimits(DefaultLimits()),
	)
	if err != nil {
		t.Fatalf("AdmitContent() error = %v", err)
	}
	if result.MediaType != "application/zstd" || result.Profile != ProcessorProfileArchive {
		t.Fatalf("AdmitContent() = %#v, want Zstandard archive profile", result)
	}
}

func TestArchiveAdmissionCountsZstandardFramesAgainstEntryBudget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content []byte
	}{
		{
			name: "two standard frames",
			content: append(
				archiveZstandard(t, []byte("first frame")),
				archiveZstandard(t, []byte("second frame"))...,
			),
		},
		{
			name: "standard and skippable frames",
			content: append(
				archiveZstandard(t, []byte("payload")),
				archiveZstandardSkippableFrame([]byte("metadata"))...,
			),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			limits := DefaultAdmissionLimits(DefaultLimits())
			limits.Archive.MaxEntries = 1
			_, err := AdmitContent(
				context.Background(),
				admissionRequest("bundle.zst", "application/zstd", tt.content),
				limits,
			)
			if !errors.Is(err, ErrAdmissionLimitExceeded) {
				t.Fatalf("AdmitContent() error = %v, want ErrAdmissionLimitExceeded", err)
			}
		})
	}
}

func TestArchiveAdmissionCountsZeroLengthZstandardSkippableFrames(t *testing.T) {
	t.Parallel()

	content := archiveZstandard(t, []byte("payload"))
	for range 8 {
		content = append(content, archiveZstandardSkippableFrame(nil)...)
	}
	limits := DefaultAdmissionLimits(DefaultLimits())
	limits.Archive.MaxEntries = 8
	_, err := AdmitContent(
		context.Background(),
		admissionRequest("bundle.zst", "application/zstd", content),
		limits,
	)
	if !errors.Is(err, ErrAdmissionLimitExceeded) {
		t.Fatalf("AdmitContent() error = %v, want ErrAdmissionLimitExceeded", err)
	}
}

func TestArchiveAdmissionDoesNotCountZstandardMagicInsideFramePayload(t *testing.T) {
	t.Parallel()

	payload := append([]byte("prefix"), zstandardMagic...)
	payload = append(payload, []byte{0x50, 0x2a, 0x4d, 0x18}...)
	payload = append(payload, []byte("suffix")...)
	content := archiveZstandard(t, payload)
	limits := DefaultAdmissionLimits(DefaultLimits())
	limits.Archive.MaxEntries = 1
	result, err := AdmitContent(
		context.Background(),
		admissionRequest("bundle.zst", "application/zstd", content),
		limits,
	)
	if err != nil {
		t.Fatalf("AdmitContent() error = %v", err)
	}
	if result.MediaType != "application/zstd" || result.Profile != ProcessorProfileArchive {
		t.Fatalf("AdmitContent() = %#v, want Zstandard archive profile", result)
	}
}

func TestArchiveAdmissionRejectsZstandardTrailingBytes(t *testing.T) {
	t.Parallel()

	content := append(archiveZstandard(t, []byte("payload")), 0x01)
	_, err := AdmitContent(
		context.Background(),
		admissionRequest("bundle.zst", "application/zstd", content),
		DefaultAdmissionLimits(DefaultLimits()),
	)
	if !errors.Is(err, ErrAdmissionRejected) {
		t.Fatalf("AdmitContent() error = %v, want ErrAdmissionRejected", err)
	}
}

func TestArchiveAdmissionRejectsTruncatedTrailingZstandardFrame(t *testing.T) {
	t.Parallel()

	content := append(archiveZstandard(t, []byte("payload")), zstandardMagic...)
	_, err := AdmitContent(
		context.Background(),
		admissionRequest("bundle.zst", "application/zstd", content),
		DefaultAdmissionLimits(DefaultLimits()),
	)
	if !errors.Is(err, ErrAdmissionRejected) {
		t.Fatalf("AdmitContent() error = %v, want ErrAdmissionRejected", err)
	}
}

func TestArchiveAdmissionAggregatesZstandardExpandedBytes(t *testing.T) {
	t.Parallel()

	content := append(
		archiveZstandard(t, []byte("1234")),
		archiveZstandard(t, []byte("5678"))...,
	)
	limits := DefaultAdmissionLimits(DefaultLimits())
	limits.Archive.MaxExpandedBytes = 7
	_, err := AdmitContent(
		context.Background(),
		admissionRequest("bundle.zst", "application/zstd", content),
		limits,
	)
	if !errors.Is(err, ErrAdmissionLimitExceeded) {
		t.Fatalf("AdmitContent() error = %v, want ErrAdmissionLimitExceeded", err)
	}
}

func TestArchiveAdmissionAggregatesZstandardCompressionRatio(t *testing.T) {
	t.Parallel()

	payload := bytes.Repeat([]byte{0}, 4096)
	content := append(
		archiveZstandard(t, payload),
		archiveZstandard(t, payload)...,
	)
	limits := DefaultAdmissionLimits(DefaultLimits())
	limits.Archive.MaxCompressionRatio = int64(len(payload)+len(content)-1) / int64(len(content))
	_, err := AdmitContent(
		context.Background(),
		admissionRequest("bundle.zst", "application/zstd", content),
		limits,
	)
	if !errors.Is(err, ErrAdmissionLimitExceeded) {
		t.Fatalf("AdmitContent() error = %v, want ErrAdmissionLimitExceeded", err)
	}
}

func TestArchiveAdmissionAcceptsSafePAXPath(t *testing.T) {
	t.Parallel()

	content := archiveTAR(t, []archiveFixtureEntry{{
		name: strings.Repeat("safe", 30) + ".txt", body: []byte("payload"), format: tar.FormatPAX,
	}})
	result, err := AdmitContent(
		context.Background(),
		admissionRequest("bundle.tar", "application/x-tar", content),
		DefaultAdmissionLimits(DefaultLimits()),
	)
	if err != nil {
		t.Fatalf("AdmitContent() error = %v", err)
	}
	if result.MediaType != "application/x-tar" || result.Profile != ProcessorProfileArchive {
		t.Fatalf("AdmitContent() = %#v, want TAR archive profile", result)
	}
}

func TestArchiveAdmissionRejectsUnsafePAXEffectivePath(t *testing.T) {
	t.Parallel()

	content := archiveTAR(t, []archiveFixtureEntry{{
		name: "../" + strings.Repeat("escape", 20) + ".txt", body: []byte("payload"), format: tar.FormatPAX,
	}})
	_, err := AdmitContent(
		context.Background(),
		admissionRequest("bundle.tar", "application/x-tar", content),
		DefaultAdmissionLimits(DefaultLimits()),
	)
	if !errors.Is(err, ErrAdmissionRejected) {
		t.Fatalf("AdmitContent() error = %v, want ErrAdmissionRejected", err)
	}
}

func TestArchiveAdmissionRejectsDuplicatePAXEffectivePaths(t *testing.T) {
	t.Parallel()

	prefix := strings.Repeat("directory", 14)
	content := archiveTAR(t, []archiveFixtureEntry{
		{name: prefix + "/./notes.txt", body: []byte("first"), format: tar.FormatPAX},
		{name: prefix + "/notes.txt", body: []byte("second"), format: tar.FormatPAX},
	})
	_, err := AdmitContent(
		context.Background(),
		admissionRequest("bundle.tar", "application/x-tar", content),
		DefaultAdmissionLimits(DefaultLimits()),
	)
	if !errors.Is(err, ErrAdmissionRejected) {
		t.Fatalf("AdmitContent() error = %v, want ErrAdmissionRejected", err)
	}
}

func TestArchiveAdmissionRejectsCorruptTARHeaderChecksum(t *testing.T) {
	t.Parallel()

	content := archiveTAR(t, []archiveFixtureEntry{{name: "notes.txt", body: []byte("payload")}})
	content[0] ^= 0xff
	_, err := AdmitContent(
		context.Background(),
		admissionRequest("bundle.tar", "application/x-tar", content),
		DefaultAdmissionLimits(DefaultLimits()),
	)
	if !errors.Is(err, ErrAdmissionRejected) {
		t.Fatalf("AdmitContent() error = %v, want ErrAdmissionRejected", err)
	}
}

func TestArchiveAdmissionRejectsMacroDocumentAndDiskImagePayloads(t *testing.T) {
	t.Parallel()

	macroDocument := archiveZIP(t, []archiveFixtureEntry{{name: "word/document.xml", body: []byte("document")}})
	tests := []struct {
		name      string
		fileName  string
		mediaType string
		content   []byte
	}{
		{
			name: "macro document", fileName: "bundle.zip", mediaType: "application/zip",
			content: archiveZIP(t, []archiveFixtureEntry{{name: "report.docm", body: macroDocument}}),
		},
		{
			name: "disk image", fileName: "bundle.tar", mediaType: "application/x-tar",
			content: archiveTAR(t, []archiveFixtureEntry{{name: "disk.bin", body: append([]byte("QFI\xfb"), bytes.Repeat([]byte{0}, 64)...)}}),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := AdmitContent(
				context.Background(),
				admissionRequest(tt.fileName, tt.mediaType, tt.content),
				DefaultAdmissionLimits(DefaultLimits()),
			)
			if !errors.Is(err, ErrAdmissionRejected) {
				t.Fatalf("AdmitContent() error = %v, want ErrAdmissionRejected", err)
			}
		})
	}
}

func TestArchiveAdmissionRejectsUnsafeZIPAndTARPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		fileName  string
		mediaType string
		content   func(testing.TB) []byte
	}{
		{name: "ZIP absolute", fileName: "bundle.zip", mediaType: "application/zip", content: func(t testing.TB) []byte {
			return archiveZIP(t, []archiveFixtureEntry{{name: "/absolute.txt", body: []byte("x")}})
		}},
		{name: "ZIP traversal", fileName: "bundle.zip", mediaType: "application/zip", content: func(t testing.TB) []byte {
			return archiveZIP(t, []archiveFixtureEntry{{name: "../escape.txt", body: []byte("x")}})
		}},
		{name: "ZIP backslash ambiguity", fileName: "bundle.zip", mediaType: "application/zip", content: func(t testing.TB) []byte {
			return archiveZIP(t, []archiveFixtureEntry{{name: `safe\..\escape.txt`, body: []byte("x")}})
		}},
		{name: "TAR absolute", fileName: "bundle.tar", mediaType: "application/x-tar", content: func(t testing.TB) []byte {
			return archiveTAR(t, []archiveFixtureEntry{{name: "/absolute.txt", body: []byte("x")}})
		}},
		{name: "TAR traversal", fileName: "bundle.tar", mediaType: "application/x-tar", content: func(t testing.TB) []byte {
			return archiveTAR(t, []archiveFixtureEntry{{name: "../escape.txt", body: []byte("x")}})
		}},
		{name: "TAR backslash ambiguity", fileName: "bundle.tar", mediaType: "application/x-tar", content: func(t testing.TB) []byte {
			return archiveTAR(t, []archiveFixtureEntry{{name: `safe\..\escape.txt`, body: []byte("x")}})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			content := tt.content(t)
			_, err := AdmitContent(context.Background(), admissionRequest(tt.fileName, tt.mediaType, content), DefaultAdmissionLimits(DefaultLimits()))
			if !errors.Is(err, ErrAdmissionRejected) {
				t.Fatalf("AdmitContent() error = %v, want ErrAdmissionRejected", err)
			}
		})
	}
}

func TestArchiveAdmissionRejectsDuplicateNormalizedPaths(t *testing.T) {
	t.Parallel()

	entries := []archiveFixtureEntry{
		{name: "dir/file.txt", body: []byte("first")},
		{name: "dir/./file.txt", body: []byte("second")},
	}
	tests := []struct {
		name      string
		fileName  string
		mediaType string
		content   []byte
	}{
		{name: "ZIP", fileName: "bundle.zip", mediaType: "application/zip", content: archiveZIP(t, entries)},
		{name: "TAR", fileName: "bundle.tar", mediaType: "application/x-tar", content: archiveTAR(t, entries)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := AdmitContent(context.Background(), admissionRequest(tt.fileName, tt.mediaType, tt.content), DefaultAdmissionLimits(DefaultLimits()))
			if !errors.Is(err, ErrAdmissionRejected) {
				t.Fatalf("AdmitContent() error = %v, want ErrAdmissionRejected", err)
			}
		})
	}
}

func TestArchiveAdmissionRejectsLinksDevicesAndEncryptedZIP(t *testing.T) {
	t.Parallel()

	plainZIP := archiveZIP(t, []archiveFixtureEntry{{name: "secret.txt", body: []byte("secret")}})
	tests := []struct {
		name      string
		fileName  string
		mediaType string
		content   []byte
	}{
		{name: "ZIP symlink", fileName: "bundle.zip", mediaType: "application/zip", content: archiveZIP(t, []archiveFixtureEntry{{name: "link", body: []byte("target"), mode: fs.ModeSymlink | 0o777}})},
		{name: "ZIP encrypted flag", fileName: "bundle.zip", mediaType: "application/zip", content: archiveEncryptedZIP(t, plainZIP)},
		{name: "TAR symlink", fileName: "bundle.tar", mediaType: "application/x-tar", content: archiveTAR(t, []archiveFixtureEntry{{name: "link", typeflag: tar.TypeSymlink, linkname: "target"}})},
		{name: "TAR hardlink", fileName: "bundle.tar", mediaType: "application/x-tar", content: archiveTAR(t, []archiveFixtureEntry{{name: "hard", typeflag: tar.TypeLink, linkname: "target"}})},
		{name: "TAR device", fileName: "bundle.tar", mediaType: "application/x-tar", content: archiveTAR(t, []archiveFixtureEntry{{name: "device", typeflag: tar.TypeChar}})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := AdmitContent(context.Background(), admissionRequest(tt.fileName, tt.mediaType, tt.content), DefaultAdmissionLimits(DefaultLimits()))
			if !errors.Is(err, ErrAdmissionRejected) {
				t.Fatalf("AdmitContent() error = %v, want ErrAdmissionRejected", err)
			}
		})
	}
}

func TestArchiveAdmissionRejectsExecutableAndSpecialPermissions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		fileName  string
		mediaType string
		content   []byte
	}{
		{name: "ZIP executable", fileName: "bundle.zip", mediaType: "application/zip", content: archiveZIP(t, []archiveFixtureEntry{{name: "notes.txt", body: []byte("benign"), mode: 0o700}})},
		{name: "ZIP setuid", fileName: "bundle.zip", mediaType: "application/zip", content: archiveZIP(t, []archiveFixtureEntry{{name: "notes.txt", body: []byte("benign"), mode: fs.ModeSetuid | 0o600}})},
		{name: "ZIP setgid", fileName: "bundle.zip", mediaType: "application/zip", content: archiveZIP(t, []archiveFixtureEntry{{name: "notes.txt", body: []byte("benign"), mode: fs.ModeSetgid | 0o600}})},
		{name: "ZIP sticky", fileName: "bundle.zip", mediaType: "application/zip", content: archiveZIP(t, []archiveFixtureEntry{{name: "notes.txt", body: []byte("benign"), mode: fs.ModeSticky | 0o600}})},
		{name: "TAR executable", fileName: "bundle.tar", mediaType: "application/x-tar", content: archiveTAR(t, []archiveFixtureEntry{{name: "notes.txt", body: []byte("benign"), mode: 0o700}})},
		{name: "TAR setuid", fileName: "bundle.tar", mediaType: "application/x-tar", content: archiveTAR(t, []archiveFixtureEntry{{name: "notes.txt", body: []byte("benign"), mode: fs.ModeSetuid | 0o600}})},
		{name: "TAR setgid", fileName: "bundle.tar", mediaType: "application/x-tar", content: archiveTAR(t, []archiveFixtureEntry{{name: "notes.txt", body: []byte("benign"), mode: fs.ModeSetgid | 0o600}})},
		{name: "TAR sticky", fileName: "bundle.tar", mediaType: "application/x-tar", content: archiveTAR(t, []archiveFixtureEntry{{name: "notes.txt", body: []byte("benign"), mode: fs.ModeSticky | 0o600}})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := AdmitContent(context.Background(), admissionRequest(tt.fileName, tt.mediaType, tt.content), DefaultAdmissionLimits(DefaultLimits()))
			if !errors.Is(err, ErrAdmissionRejected) {
				t.Fatalf("AdmitContent() error = %v, want ErrAdmissionRejected", err)
			}
		})
	}
}

func TestArchiveAdmissionRejectsZIPLocalCentralMismatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func([]byte)
	}{
		{name: "local signature", mutate: func(content []byte) { content[0] = 'Q' }},
		{name: "local flags", mutate: func(content []byte) {
			binary.LittleEndian.PutUint16(content[6:8], binary.LittleEndian.Uint16(content[6:8])^0x0001)
		}},
		{name: "local compression method", mutate: func(content []byte) {
			binary.LittleEndian.PutUint16(content[8:10], zip.Store)
		}},
		{name: "local safe versus traversal name", mutate: func(content []byte) {
			copy(content[30:38], []byte("../x.txt"))
		}},
		{name: "local name length", mutate: func(content []byte) {
			binary.LittleEndian.PutUint16(content[26:28], 0xffff)
		}},
		{name: "local extra length", mutate: func(content []byte) {
			binary.LittleEndian.PutUint16(content[28:30], 0xffff)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			content := append([]byte(nil), archiveZIP(t, []archiveFixtureEntry{{name: "safe.txt", body: []byte("benign")}})...)
			tt.mutate(content)
			_, err := AdmitContent(context.Background(), admissionRequest("bundle.zip", "application/zip", content), DefaultAdmissionLimits(DefaultLimits()))
			if !errors.Is(err, ErrAdmissionRejected) {
				t.Fatalf("AdmitContent() error = %v, want ErrAdmissionRejected", err)
			}
		})
	}
}

func TestArchiveAdmissionPreflightsEntryCountBeforeCentralDirectoryParsing(t *testing.T) {
	t.Parallel()

	content := archiveZIP(t, []archiveFixtureEntry{
		{name: "one.txt", body: []byte("1")},
		{name: "two.txt", body: []byte("2")},
		{name: "three.txt", body: []byte("3")},
	})
	centralOffset := bytes.Index(content, []byte{'P', 'K', 0x01, 0x02})
	if centralOffset < 0 {
		t.Fatal("ZIP fixture has no central directory")
	}
	content[centralOffset] = 'Q'
	limits := DefaultAdmissionLimits(DefaultLimits())
	limits.Archive.MaxEntries = 2
	_, err := AdmitContent(context.Background(), admissionRequest("bundle.zip", "application/zip", content), limits)
	if !errors.Is(err, ErrAdmissionLimitExceeded) {
		t.Fatalf("AdmitContent() error = %v, want ErrAdmissionLimitExceeded before central-directory parsing", err)
	}
}

func TestArchiveAdmissionEnforcesComplexityBudgets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		fileName  string
		mediaType string
		content   []byte
		mutate    func(*ArchiveLimits)
	}{
		{name: "entry count", fileName: "bundle.zip", mediaType: "application/zip", content: archiveZIP(t, []archiveFixtureEntry{{name: "one", body: []byte("1")}, {name: "two", body: []byte("2")}}), mutate: func(value *ArchiveLimits) { value.MaxEntries = 1 }},
		{name: "entry name bytes", fileName: "bundle.tar", mediaType: "application/x-tar", content: archiveTAR(t, []archiveFixtureEntry{{name: "long-name.txt", body: []byte("x")}}), mutate: func(value *ArchiveLimits) { value.MaxEntryNameBytes = 8 }},
		{name: "declared expanded bytes", fileName: "bundle.zip", mediaType: "application/zip", content: archiveZIP(t, []archiveFixtureEntry{{name: "data", body: []byte("12345")}}), mutate: func(value *ArchiveLimits) { value.MaxExpandedBytes = 4 }},
		{name: "actual expanded bytes", fileName: "payload.gz", mediaType: "application/gzip", content: archiveGZIP(t, "payload", []byte("12345")), mutate: func(value *ArchiveLimits) { value.MaxExpandedBytes = 4 }},
		{name: "compression ratio", fileName: "bundle.zip", mediaType: "application/zip", content: archiveZIP(t, []archiveFixtureEntry{{name: "zeros", body: bytes.Repeat([]byte{0}, 1024)}}), mutate: func(value *ArchiveLimits) { value.MaxExpandedBytes = 2048; value.MaxCompressionRatio = 2 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			limits := DefaultAdmissionLimits(DefaultLimits())
			tt.mutate(&limits.Archive)
			_, err := AdmitContent(context.Background(), admissionRequest(tt.fileName, tt.mediaType, tt.content), limits)
			if !errors.Is(err, ErrAdmissionLimitExceeded) {
				t.Fatalf("AdmitContent() error = %v, want ErrAdmissionLimitExceeded", err)
			}
		})
	}
}

func TestArchiveAdmissionHonorsNestedArchiveDepth(t *testing.T) {
	t.Parallel()

	innerZIP := archiveZIP(t, []archiveFixtureEntry{{name: "inner.txt", body: []byte("nested")}})
	tests := []struct {
		name      string
		fileName  string
		mediaType string
		content   []byte
	}{
		{name: "ZIP entry", fileName: "outer.zip", mediaType: "application/zip", content: archiveZIP(t, []archiveFixtureEntry{{name: "inner.zip", body: innerZIP}})},
		{name: "TAR entry", fileName: "outer.tar", mediaType: "application/x-tar", content: archiveTAR(t, []archiveFixtureEntry{{name: "inner.zip", body: innerZIP}})},
		{name: "GZIP stream", fileName: "outer.gz", mediaType: "application/gzip", content: archiveGZIP(t, "inner.zip", innerZIP)},
		{name: "Zstandard stream", fileName: "outer.zst", mediaType: "application/zstd", content: archiveZstandard(t, innerZIP)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			limits := DefaultAdmissionLimits(DefaultLimits())
			limits.Archive.MaxNestingDepth = 2
			result, err := AdmitContent(context.Background(), admissionRequest(tt.fileName, tt.mediaType, tt.content), limits)
			if err != nil {
				t.Fatalf("AdmitContent() depth 2 error = %v", err)
			}
			if result.Profile != ProcessorProfileArchive {
				t.Fatalf("AdmitContent() depth 2 profile = %q, want %q", result.Profile, ProcessorProfileArchive)
			}

			limits = DefaultAdmissionLimits(DefaultLimits())
			limits.Archive.MaxNestingDepth = 1
			_, err = AdmitContent(context.Background(), admissionRequest(tt.fileName, tt.mediaType, tt.content), limits)
			if !errors.Is(err, ErrAdmissionLimitExceeded) {
				t.Fatalf("AdmitContent() depth 1 error = %v, want ErrAdmissionLimitExceeded", err)
			}
		})
	}
}

func TestArchiveAdmissionRejectsNestedArchiveDeclarationMismatch(t *testing.T) {
	t.Parallel()

	innerZIP := archiveZIP(t, []archiveFixtureEntry{{name: "inner.txt", body: []byte("nested")}})
	innerGZIP := archiveGZIP(t, "inner.txt", []byte("nested"))
	tests := []struct {
		name    string
		content []byte
	}{
		{name: "archive magic disguised as text", content: archiveZIP(t, []archiveFixtureEntry{{name: "notes.txt", body: innerZIP}})},
		{name: "ZIP extension with GZIP magic", content: archiveZIP(t, []archiveFixtureEntry{{name: "inner.zip", body: innerGZIP}})},
		{name: "GZIP extension with ZIP magic", content: archiveZIP(t, []archiveFixtureEntry{{name: "inner.gz", body: innerZIP}})},
		{name: "archive extension without archive magic", content: archiveZIP(t, []archiveFixtureEntry{{name: "inner.zip", body: []byte("plain text")}})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			limits := DefaultAdmissionLimits(DefaultLimits())
			limits.Archive.MaxNestingDepth = 2
			_, err := AdmitContent(context.Background(), admissionRequest("outer.zip", "application/zip", tt.content), limits)
			if !errors.Is(err, ErrAdmissionRejected) {
				t.Fatalf("AdmitContent() error = %v, want ErrAdmissionRejected", err)
			}
		})
	}
}

func TestArchiveAdmissionEnforcesCumulativeNestedBudgets(t *testing.T) {
	t.Parallel()

	twoFileInner := archiveZIP(t, []archiveFixtureEntry{
		{name: "one.txt", body: []byte("one")},
		{name: "two.txt", body: []byte("two")},
	})
	expandedBody := []byte("nested expanded bytes")
	expandedInner := archiveZIP(t, []archiveFixtureEntry{{name: "payload.txt", body: expandedBody}})
	incompressible := make([]byte, 512)
	for index := range incompressible {
		incompressible[index] = byte(index*73 + 19)
	}
	ratioInner := archiveZIP(t, []archiveFixtureEntry{{name: "payload.bin", body: incompressible, store: true}})

	tests := []struct {
		name    string
		content []byte
		mutate  func(*ArchiveLimits)
	}{
		{name: "entries", content: archiveZIP(t, []archiveFixtureEntry{{name: "inner.zip", body: twoFileInner}}), mutate: func(value *ArchiveLimits) {
			value.MaxEntries = 2
		}},
		{name: "expanded bytes", content: archiveZIP(t, []archiveFixtureEntry{{name: "inner.zip", body: expandedInner}}), mutate: func(value *ArchiveLimits) {
			value.MaxExpandedBytes = int64(len(expandedInner) + len(expandedBody) - 1)
		}},
		{name: "compression ratio", content: archiveZIP(t, []archiveFixtureEntry{{name: "inner.zip", body: ratioInner, store: true}}), mutate: func(value *ArchiveLimits) {
			value.MaxCompressionRatio = 1
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			limits := DefaultAdmissionLimits(DefaultLimits())
			limits.Archive.MaxNestingDepth = 2
			tt.mutate(&limits.Archive)
			_, err := AdmitContent(context.Background(), admissionRequest("outer.zip", "application/zip", tt.content), limits)
			if !errors.Is(err, ErrAdmissionLimitExceeded) {
				t.Fatalf("AdmitContent() error = %v, want ErrAdmissionLimitExceeded", err)
			}
		})
	}
}

func TestArchiveAdmissionRejectsMalformedOrTruncatedContainers(t *testing.T) {
	t.Parallel()

	validZIP := archiveZIP(t, []archiveFixtureEntry{{name: "file", body: []byte("zip")}})
	invalidTAR := archiveTAR(t, []archiveFixtureEntry{{name: "file", body: []byte("tar")}})
	invalidTAR[0] ^= 0xff
	validGZIP := archiveGZIP(t, "file", []byte("gzip"))
	validZstandard := archiveZstandard(t, []byte("zstandard"))
	tests := []struct {
		name      string
		fileName  string
		mediaType string
		content   []byte
	}{
		{name: "ZIP", fileName: "bundle.zip", mediaType: "application/zip", content: validZIP[:len(validZIP)-5]},
		{name: "TAR", fileName: "bundle.tar", mediaType: "application/x-tar", content: invalidTAR},
		{name: "GZIP", fileName: "bundle.gz", mediaType: "application/gzip", content: validGZIP[:len(validGZIP)-4]},
		{name: "Zstandard", fileName: "bundle.zst", mediaType: "application/zstd", content: validZstandard[:len(validZstandard)-3]},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := AdmitContent(context.Background(), admissionRequest(tt.fileName, tt.mediaType, tt.content), DefaultAdmissionLimits(DefaultLimits()))
			if !errors.Is(err, ErrAdmissionRejected) {
				t.Fatalf("AdmitContent() error = %v, want ErrAdmissionRejected", err)
			}
		})
	}
}

func TestArchiveAdmissionBoundsDeclaredAndActualReads(t *testing.T) {
	t.Parallel()

	limits := DefaultAdmissionLimits(DefaultLimits())
	limits.MaxReadBytes = 8
	limits.MaxPDFBytes = 8
	tests := []struct {
		name      string
		sizeBytes int64
		reader    *admissionCountingReader
		maxRead   int
	}{
		{name: "declared", sizeBytes: 9, reader: &admissionCountingReader{failOnRead: true}, maxRead: 0},
		{name: "actual", sizeBytes: 8, reader: &admissionCountingReader{infinite: true}, maxRead: 9},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := AdmitContent(context.Background(), AdmissionRequest{
				DisplayName: "bundle.zip", DeclaredMediaType: "application/zip", SizeBytes: tt.sizeBytes,
				Content: tt.reader, ScannerStatus: ScannerStatusHealthy,
			}, limits)
			if !errors.Is(err, ErrAdmissionLimitExceeded) {
				t.Fatalf("AdmitContent() error = %v, want ErrAdmissionLimitExceeded", err)
			}
			if tt.reader.bytesRead > tt.maxRead {
				t.Fatalf("AdmitContent() read %d bytes, want at most %d", tt.reader.bytesRead, tt.maxRead)
			}
		})
	}
}

func FuzzArchiveSeeds(f *testing.F) {
	validZIP := archiveZIP(f, []archiveFixtureEntry{{name: "file.txt", body: []byte("safe")}})
	unsafeZIP := archiveZIP(f, []archiveFixtureEntry{{name: "../escape.txt", body: []byte("unsafe")}})
	validGZIP := archiveGZIP(f, "file.txt", []byte("safe"))
	validZstandard := archiveZstandard(f, []byte("safe"))
	f.Add(uint8(0), validZIP)
	f.Add(uint8(0), unsafeZIP)
	f.Add(uint8(0), validZIP[:len(validZIP)-3])
	f.Add(uint8(1), archiveTAR(f, []archiveFixtureEntry{{name: "file.txt", body: []byte("safe")}}))
	f.Add(uint8(2), validGZIP)
	f.Add(uint8(3), validZstandard)

	f.Fuzz(func(t *testing.T, kind uint8, content []byte) {
		if len(content) > 8192 {
			t.Skip()
		}
		formats := []struct {
			fileName  string
			mediaType string
		}{
			{fileName: "bundle.zip", mediaType: "application/zip"},
			{fileName: "bundle.tar", mediaType: "application/x-tar"},
			{fileName: "bundle.gz", mediaType: "application/gzip"},
			{fileName: "bundle.zst", mediaType: "application/zstd"},
		}
		format := formats[int(kind)%len(formats)]
		result, err := AdmitContent(context.Background(), admissionRequest(format.fileName, format.mediaType, content), DefaultAdmissionLimits(DefaultLimits()))
		if err == nil && (result.MediaType != format.mediaType || result.Profile != ProcessorProfileArchive) {
			t.Fatalf("AdmitContent() = %#v, want media %q archive profile", result, format.mediaType)
		}
	})
}

func FuzzArchiveHostilePayloads(f *testing.F) {
	f.Add(uint8(0), []byte("safe"))
	f.Add(uint8(1), []byte{0x00, 0xff, 0x01})
	f.Add(uint8(2), bytes.Repeat([]byte{0}, 512))
	f.Add(uint8(3), []byte("payload"))

	f.Fuzz(func(t *testing.T, kind uint8, payload []byte) {
		if len(payload) > 8192 {
			t.Skip()
		}
		var fileName, mediaType string
		var content []byte
		switch kind % 4 {
		case 0:
			fileName, mediaType = "bundle.zip", "application/zip"
			content = archiveZIP(t, []archiveFixtureEntry{{name: "../escape.txt", body: payload}})
		case 1:
			fileName, mediaType = "bundle.tar", "application/x-tar"
			content = archiveTAR(t, []archiveFixtureEntry{{name: "../escape.txt", body: payload}})
		case 2:
			fileName, mediaType = "bundle.gz", "application/gzip"
			content = archiveGZIP(t, "../escape.txt", payload)
		case 3:
			fileName, mediaType = "bundle.zst", "application/zstd"
			content = archiveZstandard(t, append([]byte{'M', 'Z'}, payload...))
		}
		_, err := AdmitContent(
			context.Background(),
			admissionRequest(fileName, mediaType, content),
			DefaultAdmissionLimits(DefaultLimits()),
		)
		if !errors.Is(err, ErrAdmissionRejected) && !errors.Is(err, ErrAdmissionLimitExceeded) {
			t.Fatalf("AdmitContent() error = %v, want hostile rejection or typed limit", err)
		}
	})
}

type archiveFixtureEntry struct {
	name     string
	body     []byte
	mode     fs.FileMode
	typeflag byte
	linkname string
	store    bool
	format   tar.Format
}

type archiveZIP64FixtureOptions struct {
	dataDescriptor   bool
	signedDescriptor bool
	centralOffset64  bool
	localSizes64     bool
}

func archiveBase64Fixture(t testing.TB, name string) []byte {
	t.Helper()
	encoded, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read archive fixture %q: %v", name, err)
	}
	content, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(string(encoded)), ""))
	if err != nil {
		t.Fatalf("decode archive fixture %q: %v", name, err)
	}
	return content
}

func archiveZIP64Fixture(t testing.TB, options archiveZIP64FixtureOptions) []byte {
	t.Helper()
	name := []byte("safe.txt")
	body := []byte("safe")
	checksum := crc32.ChecksumIEEE(body)
	flags := uint16(0x0800)
	if options.dataDescriptor {
		flags |= 0x0008
	}

	var localExtra []byte
	localCRC := checksum
	localCompressedSize := uint32(len(body))
	localUncompressedSize := uint32(len(body))
	if options.dataDescriptor {
		localCRC = 0
		localCompressedSize = 0
		localUncompressedSize = 0
	} else if options.localSizes64 {
		localCompressedSize = math.MaxUint32
		localUncompressedSize = math.MaxUint32
		payload := make([]byte, 16)
		binary.LittleEndian.PutUint64(payload[:8], uint64(len(body)))
		binary.LittleEndian.PutUint64(payload[8:], uint64(len(body)))
		localExtra = archiveZIPExtraField(1, payload)
	}

	local := make([]byte, 30)
	copy(local[:4], []byte{'P', 'K', 0x03, 0x04})
	binary.LittleEndian.PutUint16(local[4:6], 45)
	binary.LittleEndian.PutUint16(local[6:8], flags)
	binary.LittleEndian.PutUint16(local[8:10], zip.Store)
	binary.LittleEndian.PutUint32(local[14:18], localCRC)
	binary.LittleEndian.PutUint32(local[18:22], localCompressedSize)
	binary.LittleEndian.PutUint32(local[22:26], localUncompressedSize)
	binary.LittleEndian.PutUint16(local[26:28], uint16(len(name)))
	binary.LittleEndian.PutUint16(local[28:30], uint16(len(localExtra)))
	content := append(local, name...)
	content = append(content, localExtra...)
	content = append(content, body...)
	if options.dataDescriptor {
		if options.signedDescriptor {
			var signature [4]byte
			binary.LittleEndian.PutUint32(signature[:], 0x08074b50)
			content = append(content, signature[:]...)
		}
		var descriptor [20]byte
		binary.LittleEndian.PutUint32(descriptor[:4], checksum)
		binary.LittleEndian.PutUint64(descriptor[4:12], uint64(len(body)))
		binary.LittleEndian.PutUint64(descriptor[12:20], uint64(len(body)))
		content = append(content, descriptor[:]...)
	}

	centralOffset := len(content)
	centralPayload := make([]byte, 16, 24)
	binary.LittleEndian.PutUint64(centralPayload[:8], uint64(len(body)))
	binary.LittleEndian.PutUint64(centralPayload[8:16], uint64(len(body)))
	localOffset := uint32(0)
	if options.centralOffset64 {
		localOffset = math.MaxUint32
		centralPayload = append(centralPayload, make([]byte, 8)...)
	}
	centralExtra := archiveZIPExtraField(1, centralPayload)
	central := make([]byte, 46)
	copy(central[:4], []byte{'P', 'K', 0x01, 0x02})
	binary.LittleEndian.PutUint16(central[4:6], 3<<8|45)
	binary.LittleEndian.PutUint16(central[6:8], 45)
	binary.LittleEndian.PutUint16(central[8:10], flags)
	binary.LittleEndian.PutUint16(central[10:12], zip.Store)
	binary.LittleEndian.PutUint32(central[16:20], checksum)
	binary.LittleEndian.PutUint32(central[20:24], math.MaxUint32)
	binary.LittleEndian.PutUint32(central[24:28], math.MaxUint32)
	binary.LittleEndian.PutUint16(central[28:30], uint16(len(name)))
	binary.LittleEndian.PutUint16(central[30:32], uint16(len(centralExtra)))
	binary.LittleEndian.PutUint32(central[38:42], 0x81a40000)
	binary.LittleEndian.PutUint32(central[42:46], localOffset)
	content = append(content, central...)
	content = append(content, name...)
	content = append(content, centralExtra...)

	centralSize := len(content) - centralOffset
	eocd := make([]byte, 22)
	copy(eocd[:4], []byte{'P', 'K', 0x05, 0x06})
	binary.LittleEndian.PutUint16(eocd[8:10], 1)
	binary.LittleEndian.PutUint16(eocd[10:12], 1)
	binary.LittleEndian.PutUint32(eocd[12:16], uint32(centralSize))
	binary.LittleEndian.PutUint32(eocd[16:20], uint32(centralOffset))
	return append(content, eocd...)
}

func archiveZIPExtraField(identifier uint16, payload []byte) []byte {
	field := make([]byte, 4, 4+len(payload))
	binary.LittleEndian.PutUint16(field[:2], identifier)
	binary.LittleEndian.PutUint16(field[2:4], uint16(len(payload)))
	return append(field, payload...)
}

func archiveInsertZIPCentralExtra(t testing.TB, content, field []byte) []byte {
	t.Helper()
	centralOffset := bytes.Index(content, []byte{'P', 'K', 0x01, 0x02})
	if centralOffset < 0 {
		t.Fatal("ZIP fixture has no central directory")
	}
	nameLength := int(binary.LittleEndian.Uint16(content[centralOffset+28 : centralOffset+30]))
	extraLength := int(binary.LittleEndian.Uint16(content[centralOffset+30 : centralOffset+32]))
	insertAt := centralOffset + 46 + nameLength + extraLength
	result := archiveInsertBytes(content, insertAt, field)
	binary.LittleEndian.PutUint16(result[centralOffset+30:centralOffset+32], uint16(extraLength+len(field)))
	archiveGrowZIPCentralSize(t, result, len(field))
	return result
}

func archiveAppendToFirstZIPCentralExtra(t testing.TB, content, payload []byte) []byte {
	t.Helper()
	centralOffset := bytes.Index(content, []byte{'P', 'K', 0x01, 0x02})
	if centralOffset < 0 {
		t.Fatal("ZIP fixture has no central directory")
	}
	nameLength := int(binary.LittleEndian.Uint16(content[centralOffset+28 : centralOffset+30]))
	extraLength := int(binary.LittleEndian.Uint16(content[centralOffset+30 : centralOffset+32]))
	extraOffset := centralOffset + 46 + nameLength
	fieldLength := int(binary.LittleEndian.Uint16(content[extraOffset+2 : extraOffset+4]))
	result := archiveInsertBytes(content, extraOffset+4+fieldLength, payload)
	binary.LittleEndian.PutUint16(result[extraOffset+2:extraOffset+4], uint16(fieldLength+len(payload)))
	binary.LittleEndian.PutUint16(result[centralOffset+30:centralOffset+32], uint16(extraLength+len(payload)))
	archiveGrowZIPCentralSize(t, result, len(payload))
	return result
}

func archiveInsertBytes(content []byte, offset int, addition []byte) []byte {
	result := make([]byte, len(content)+len(addition))
	copy(result, content[:offset])
	copy(result[offset:], addition)
	copy(result[offset+len(addition):], content[offset:])
	return result
}

func archiveGrowZIPCentralSize(t testing.TB, content []byte, addition int) {
	t.Helper()
	eocdOffset := bytes.LastIndex(content, []byte{'P', 'K', 0x05, 0x06})
	if eocdOffset < 0 {
		t.Fatal("ZIP fixture has no EOCD")
	}
	centralSize := binary.LittleEndian.Uint32(content[eocdOffset+12 : eocdOffset+16])
	binary.LittleEndian.PutUint32(content[eocdOffset+12:eocdOffset+16], centralSize+uint32(addition))
}

func archiveZIP(t testing.TB, entries []archiveFixtureEntry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		if entry.store {
			header.Method = zip.Store
		}
		if entry.mode != 0 {
			header.SetMode(entry.mode)
		}
		part, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatalf("create ZIP fixture entry: %v", err)
		}
		if _, err := part.Write(entry.body); err != nil {
			t.Fatalf("write ZIP fixture entry: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close ZIP fixture: %v", err)
	}
	return buffer.Bytes()
}

func archiveTAR(t testing.TB, entries []archiveFixtureEntry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	for _, entry := range entries {
		typeflag := entry.typeflag
		if typeflag == 0 {
			typeflag = tar.TypeReg
		}
		header := &tar.Header{
			Name: entry.name, Mode: 0o600, Typeflag: typeflag, Linkname: entry.linkname, Format: entry.format,
		}
		if entry.mode != 0 {
			header.Mode = int64(entry.mode.Perm())
			if entry.mode&fs.ModeSetuid != 0 {
				header.Mode |= 0o4000
			}
			if entry.mode&fs.ModeSetgid != 0 {
				header.Mode |= 0o2000
			}
			if entry.mode&fs.ModeSticky != 0 {
				header.Mode |= 0o1000
			}
		}
		if typeflag == tar.TypeReg {
			header.Size = int64(len(entry.body))
		}
		if typeflag == tar.TypeChar {
			header.Devmajor, header.Devminor = 1, 3
		}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatalf("write TAR fixture header: %v", err)
		}
		if typeflag == tar.TypeReg {
			if _, err := writer.Write(entry.body); err != nil {
				t.Fatalf("write TAR fixture entry: %v", err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close TAR fixture: %v", err)
	}
	return buffer.Bytes()
}

func archiveGZIP(t testing.TB, name string, content []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	writer.Name = name
	if _, err := writer.Write(content); err != nil {
		t.Fatalf("write GZIP fixture: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close GZIP fixture: %v", err)
	}
	return buffer.Bytes()
}

func archiveZstandard(t testing.TB, content []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer, err := zstd.NewWriter(&buffer, zstd.WithEncoderConcurrency(1))
	if err != nil {
		t.Fatalf("create Zstandard fixture: %v", err)
	}
	if _, err := writer.Write(content); err != nil {
		t.Fatalf("write Zstandard fixture: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close Zstandard fixture: %v", err)
	}
	return buffer.Bytes()
}

func archiveZstandardSkippableFrame(content []byte) []byte {
	result := make([]byte, 8, 8+len(content))
	copy(result, []byte{0x50, 0x2a, 0x4d, 0x18})
	binary.LittleEndian.PutUint32(result[4:], uint32(len(content)))
	return append(result, content...)
}

func archiveEncryptedZIP(t testing.TB, content []byte) []byte {
	t.Helper()
	result := append([]byte(nil), content...)
	patchedLocal, patchedCentral := false, false
	for offset := 0; offset+10 <= len(result); offset++ {
		switch string(result[offset : offset+4]) {
		case "PK\x03\x04":
			flags := binary.LittleEndian.Uint16(result[offset+6 : offset+8])
			binary.LittleEndian.PutUint16(result[offset+6:offset+8], flags|1)
			patchedLocal = true
		case "PK\x01\x02":
			flags := binary.LittleEndian.Uint16(result[offset+8 : offset+10])
			binary.LittleEndian.PutUint16(result[offset+8:offset+10], flags|1)
			patchedCentral = true
		}
	}
	if !patchedLocal || !patchedCentral {
		t.Fatal("encrypted ZIP fixture did not contain local and central headers")
	}
	return result
}
