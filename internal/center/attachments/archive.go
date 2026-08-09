package attachments

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"io/fs"
	"math"
	"path"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/klauspost/compress/zstd"
)

const archiveProbeBytes = 64 * 1024

var (
	zstandardMagic = []byte{0x28, 0xb5, 0x2f, 0xfd}
	sevenZipMagic  = []byte{'7', 'z', 0xbc, 0xaf, 0x27, 0x1c}
	rarMagic       = []byte{'R', 'a', 'r', '!', 0x1a, 0x07}
)

func inspectArchive(ctx context.Context, kind admissionContentKind, content []byte, limits ArchiveLimits) error {
	inspection := archiveInspection{
		limits:              limits,
		rootCompressedBytes: uint64(len(content)),
	}
	return inspectArchiveAtDepth(ctx, kind, content, 1, &inspection)
}

type archiveInspection struct {
	limits              ArchiveLimits
	entries             int
	expandedBytes       int64
	rootCompressedBytes uint64
}

func inspectArchiveAtDepth(ctx context.Context, kind admissionContentKind, content []byte, depth int, inspection *archiveInspection) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > inspection.limits.MaxNestingDepth {
		return admissionLimitError("archive nesting depth")
	}
	switch kind {
	case admissionContentZIP:
		return inspectZIP(ctx, content, depth, inspection)
	case admissionContentTAR:
		return inspectTAR(ctx, content, depth, inspection)
	case admissionContentGZIP:
		return inspectGZIP(ctx, content, depth, inspection)
	case admissionContentZstandard:
		return inspectZstandard(ctx, content, depth, inspection)
	default:
		return admissionRejectedError("unknown archive type")
	}
}

func (inspection *archiveInspection) remainingEntries() int {
	return inspection.limits.MaxEntries - inspection.entries
}

func (inspection *archiveInspection) consumeEntries(count int) error {
	if count < 0 || count > inspection.remainingEntries() {
		return admissionLimitError("archive entries")
	}
	inspection.entries += count
	return nil
}

func (inspection *archiveInspection) remainingExpandedBytes() int64 {
	return inspection.limits.MaxExpandedBytes - inspection.expandedBytes
}

func (inspection *archiveInspection) checkExpandedBytes(additional int64) error {
	if additional < 0 || additional > inspection.remainingExpandedBytes() {
		return admissionLimitError("archive expanded bytes")
	}
	return nil
}

func (inspection *archiveInspection) consumeExpandedBytes(additional int64) error {
	if err := inspection.checkExpandedBytes(additional); err != nil {
		return err
	}
	inspection.expandedBytes += additional
	if archiveRatioExceeded(uint64(inspection.expandedBytes), inspection.rootCompressedBytes, inspection.limits.MaxCompressionRatio) {
		return admissionLimitError("archive compression ratio")
	}
	return nil
}

func inspectZIP(ctx context.Context, content []byte, depth int, inspection *archiveInspection) error {
	if !bytes.HasPrefix(content, []byte{'P', 'K'}) {
		return admissionRejectedError("ZIP signature mismatch")
	}
	preflight, err := preflightZIP(content, inspection.remainingEntries())
	if err != nil {
		return err
	}
	reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil || len(reader.File) == 0 {
		return admissionRejectedError("malformed ZIP")
	}
	if len(reader.File) != len(preflight.entries) {
		return admissionRejectedError("ZIP directory entry count mismatch")
	}
	if err := inspection.consumeEntries(len(reader.File)); err != nil {
		return err
	}

	paths := make(map[string]struct{}, len(reader.File))
	for index, file := range reader.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		local := preflight.entries[index]
		dataOffset, err := file.DataOffset()
		if err != nil || dataOffset != local.dataOffset || file.Flags != local.flags ||
			file.Method != local.method || file.Name != string(local.name) ||
			file.CompressedSize64 != local.compressedSize || file.UncompressedSize64 != local.uncompressedSize {
			return admissionRejectedError("ZIP parser metadata mismatch")
		}
		if file.NonUTF8 || file.Flags&0x0001 != 0 || file.Flags&0x0040 != 0 {
			return admissionRejectedError("encrypted or non-UTF-8 ZIP entry")
		}
		if file.Flags & ^uint16(0x080e) != 0 {
			return admissionRejectedError("unsupported ZIP flags")
		}
		if file.Method != zip.Store && file.Method != zip.Deflate {
			return admissionRejectedError("unsupported ZIP compression")
		}
		normalized, err := validateArchivePath(file.Name, inspection.limits.MaxEntryNameBytes)
		if err != nil {
			return err
		}
		if _, duplicate := paths[normalized]; duplicate {
			return admissionRejectedError("duplicate archive path")
		}
		paths[normalized] = struct{}{}

		mode := file.Mode()
		if !mode.IsRegular() && !mode.IsDir() {
			return admissionRejectedError("unsafe ZIP entry type")
		}
		if mode.IsDir() {
			if file.UncompressedSize64 != 0 {
				return admissionRejectedError("ZIP directory has content")
			}
			continue
		}
		if unsafeArchiveMode(mode) {
			return admissionRejectedError("unsafe ZIP entry permissions")
		}
		declaredSize, err := archiveUint64Size(file.UncompressedSize64)
		if err != nil {
			return err
		}
		if err := inspection.checkExpandedBytes(declaredSize); err != nil {
			return err
		}
		if archiveRatioExceeded(file.UncompressedSize64, file.CompressedSize64, inspection.limits.MaxCompressionRatio) {
			return admissionLimitError("archive compression ratio")
		}

		part, err := file.Open()
		if err != nil {
			return admissionRejectedError("open ZIP entry")
		}
		_, archiveName := archiveKindFromName(normalized)
		probe, readErr := readArchivePayload(ctx, part, inspection.remainingExpandedBytes(), archiveName)
		closeErr := part.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return admissionRejectedError("close ZIP entry")
		}
		if probe.total != declaredSize {
			return admissionRejectedError("ZIP entry size mismatch")
		}
		if err := inspection.consumeExpandedBytes(probe.total); err != nil {
			return err
		}
		if err := inspectArchivePayload(ctx, normalized, probe, depth, inspection, false); err != nil {
			return err
		}
	}
	return nil
}

type zipPreflight struct {
	entries []zipPreflightEntry
}

type zipPreflightEntry struct {
	name             []byte
	flags            uint16
	method           uint16
	crc32            uint32
	zip64Descriptor  bool
	compressedSize   uint64
	uncompressedSize uint64
	localOffset      uint64
	dataOffset       int64
	dataEnd          uint64
}

func preflightZIP(content []byte, maxEntries int) (zipPreflight, error) {
	eocdOffset, err := findZIPEOCD(content)
	if err != nil {
		return zipPreflight{}, err
	}
	entryCount, centralOffset, centralSize, directoryEnd, err := readZIPDirectoryLocation(content, eocdOffset)
	if err != nil {
		return zipPreflight{}, err
	}
	if entryCount == 0 {
		return zipPreflight{}, admissionRejectedError("empty ZIP")
	}
	if entryCount > uint64(maxEntries) {
		return zipPreflight{}, admissionLimitError("archive entries")
	}
	if centralOffset > uint64(len(content)) || centralSize > uint64(len(content))-centralOffset ||
		centralOffset+centralSize != directoryEnd {
		return zipPreflight{}, admissionRejectedError("invalid ZIP central directory bounds")
	}

	entries := make([]zipPreflightEntry, 0, int(entryCount))
	cursor := centralOffset
	centralEnd := centralOffset + centralSize
	for index := uint64(0); index < entryCount; index++ {
		if cursor > centralEnd || centralEnd-cursor < 46 {
			return zipPreflight{}, admissionRejectedError("truncated ZIP central header")
		}
		header := content[int(cursor):int(cursor+46)]
		if !bytes.Equal(header[:4], []byte{'P', 'K', 0x01, 0x02}) {
			return zipPreflight{}, admissionRejectedError("invalid ZIP central signature")
		}
		nameLength := uint64(binary.LittleEndian.Uint16(header[28:30]))
		extraLength := uint64(binary.LittleEndian.Uint16(header[30:32]))
		commentLength := uint64(binary.LittleEndian.Uint16(header[32:34]))
		variableLength := nameLength + extraLength + commentLength
		if variableLength > centralEnd-cursor-46 {
			return zipPreflight{}, admissionRejectedError("invalid ZIP central field lengths")
		}
		if binary.LittleEndian.Uint16(header[34:36]) != 0 {
			return zipPreflight{}, admissionRejectedError("multi-disk ZIP is unsupported")
		}
		compressedSize32 := binary.LittleEndian.Uint32(header[20:24])
		uncompressedSize32 := binary.LittleEndian.Uint32(header[24:28])
		localOffset32 := binary.LittleEndian.Uint32(header[42:46])
		nameStart := cursor + 46
		name := content[int(nameStart):int(nameStart+nameLength)]
		extraStart := nameStart + nameLength
		extra := content[int(extraStart):int(extraStart+extraLength)]
		needCompressedSize64 := compressedSize32 == math.MaxUint32
		needUncompressedSize64 := uncompressedSize32 == math.MaxUint32
		needLocalOffset64 := localOffset32 == math.MaxUint32
		zip64Values, err := readZIP64Extra(
			extra,
			needUncompressedSize64,
			needCompressedSize64,
			needLocalOffset64,
		)
		if err != nil {
			return zipPreflight{}, err
		}
		compressedSize := uint64(compressedSize32)
		if needCompressedSize64 {
			compressedSize = zip64Values.compressedSize
		}
		uncompressedSize := uint64(uncompressedSize32)
		if needUncompressedSize64 {
			uncompressedSize = zip64Values.uncompressedSize
		}
		localOffset := uint64(localOffset32)
		if needLocalOffset64 {
			localOffset = zip64Values.localOffset
		}
		entry := zipPreflightEntry{
			name:  append([]byte(nil), name...),
			flags: binary.LittleEndian.Uint16(header[8:10]), method: binary.LittleEndian.Uint16(header[10:12]),
			crc32:            binary.LittleEndian.Uint32(header[16:20]),
			zip64Descriptor:  needCompressedSize64 || needUncompressedSize64,
			compressedSize:   compressedSize,
			uncompressedSize: uncompressedSize,
			localOffset:      localOffset,
		}
		if err := reconcileZIPLocalHeader(content, centralOffset, &entry); err != nil {
			return zipPreflight{}, err
		}
		entries = append(entries, entry)
		cursor += 46 + variableLength
	}
	if cursor != centralEnd {
		return zipPreflight{}, admissionRejectedError("ambiguous ZIP central directory")
	}
	if err := validateZIPLocalLayout(content, centralOffset, entries); err != nil {
		return zipPreflight{}, err
	}
	return zipPreflight{entries: entries}, nil
}

func findZIPEOCD(content []byte) (uint64, error) {
	if len(content) < 22 {
		return 0, admissionRejectedError("missing ZIP EOCD")
	}
	minimum := len(content) - 22 - math.MaxUint16
	if minimum < 0 {
		minimum = 0
	}
	found := -1
	for offset := len(content) - 22; offset >= minimum; offset-- {
		if !bytes.Equal(content[offset:offset+4], []byte{'P', 'K', 0x05, 0x06}) {
			continue
		}
		commentLength := int(binary.LittleEndian.Uint16(content[offset+20 : offset+22]))
		if offset+22+commentLength != len(content) {
			continue
		}
		if found >= 0 {
			return 0, admissionRejectedError("ambiguous ZIP EOCD")
		}
		found = offset
	}
	if found < 0 {
		return 0, admissionRejectedError("missing ZIP EOCD")
	}
	return uint64(found), nil
}

func readZIPDirectoryLocation(content []byte, eocdOffset uint64) (uint64, uint64, uint64, uint64, error) {
	header := content[int(eocdOffset):]
	diskNumber := binary.LittleEndian.Uint16(header[4:6])
	centralDisk := binary.LittleEndian.Uint16(header[6:8])
	diskEntries := binary.LittleEndian.Uint16(header[8:10])
	totalEntries := binary.LittleEndian.Uint16(header[10:12])
	centralSize32 := binary.LittleEndian.Uint32(header[12:16])
	centralOffset32 := binary.LittleEndian.Uint32(header[16:20])
	if diskNumber != 0 || centralDisk != 0 {
		return 0, 0, 0, 0, admissionRejectedError("multi-disk ZIP is unsupported")
	}
	zip64 := diskEntries == math.MaxUint16 || totalEntries == math.MaxUint16 ||
		centralSize32 == math.MaxUint32 || centralOffset32 == math.MaxUint32
	if !zip64 {
		if diskEntries != totalEntries {
			return 0, 0, 0, 0, admissionRejectedError("inconsistent ZIP entry counts")
		}
		return uint64(totalEntries), uint64(centralOffset32), uint64(centralSize32), eocdOffset, nil
	}
	if diskEntries != math.MaxUint16 || totalEntries != math.MaxUint16 ||
		centralSize32 != math.MaxUint32 || centralOffset32 != math.MaxUint32 || eocdOffset < 20 {
		return 0, 0, 0, 0, admissionRejectedError("ambiguous ZIP64 EOCD")
	}
	locatorOffset := eocdOffset - 20
	locator := content[int(locatorOffset):int(eocdOffset)]
	if !bytes.Equal(locator[:4], []byte{'P', 'K', 0x06, 0x07}) ||
		binary.LittleEndian.Uint32(locator[4:8]) != 0 || binary.LittleEndian.Uint32(locator[16:20]) != 1 {
		return 0, 0, 0, 0, admissionRejectedError("invalid ZIP64 locator")
	}
	zip64Offset := binary.LittleEndian.Uint64(locator[8:16])
	if zip64Offset > uint64(len(content)) || uint64(len(content))-zip64Offset < 56 || zip64Offset+56 != locatorOffset {
		return 0, 0, 0, 0, admissionRejectedError("invalid ZIP64 EOCD offset")
	}
	zip64Header := content[int(zip64Offset):int(zip64Offset+56)]
	if !bytes.Equal(zip64Header[:4], []byte{'P', 'K', 0x06, 0x06}) ||
		binary.LittleEndian.Uint64(zip64Header[4:12]) != 44 ||
		binary.LittleEndian.Uint32(zip64Header[16:20]) != 0 || binary.LittleEndian.Uint32(zip64Header[20:24]) != 0 {
		return 0, 0, 0, 0, admissionRejectedError("invalid ZIP64 EOCD")
	}
	diskEntryCount := binary.LittleEndian.Uint64(zip64Header[24:32])
	entryCount := binary.LittleEndian.Uint64(zip64Header[32:40])
	if diskEntryCount != entryCount {
		return 0, 0, 0, 0, admissionRejectedError("inconsistent ZIP64 entry counts")
	}
	return entryCount, binary.LittleEndian.Uint64(zip64Header[48:56]),
		binary.LittleEndian.Uint64(zip64Header[40:48]), zip64Offset, nil
}

type zip64ExtraValues struct {
	uncompressedSize uint64
	compressedSize   uint64
	localOffset      uint64
}

func readZIP64Extra(extra []byte, needUncompressedSize, needCompressedSize, needLocalOffset bool) (zip64ExtraValues, error) {
	var zip64Field []byte
	for len(extra) > 0 {
		if len(extra) < 4 {
			return zip64ExtraValues{}, admissionRejectedError("truncated ZIP extra field")
		}
		identifier := binary.LittleEndian.Uint16(extra[:2])
		fieldSize := int(binary.LittleEndian.Uint16(extra[2:4]))
		extra = extra[4:]
		if fieldSize > len(extra) {
			return zip64ExtraValues{}, admissionRejectedError("truncated ZIP extra field")
		}
		if identifier == 0x0001 {
			if zip64Field != nil {
				return zip64ExtraValues{}, admissionRejectedError("duplicate ZIP64 extra field")
			}
			zip64Field = extra[:fieldSize]
		}
		extra = extra[fieldSize:]
	}

	requiredSize := 0
	if needUncompressedSize {
		requiredSize += 8
	}
	if needCompressedSize {
		requiredSize += 8
	}
	if needLocalOffset {
		requiredSize += 8
	}
	if requiredSize == 0 {
		if zip64Field != nil {
			return zip64ExtraValues{}, admissionRejectedError("unnecessary ZIP64 extra field")
		}
		return zip64ExtraValues{}, nil
	}
	if zip64Field == nil {
		return zip64ExtraValues{}, admissionRejectedError("missing ZIP64 extra field")
	}
	if len(zip64Field) != requiredSize {
		return zip64ExtraValues{}, admissionRejectedError("ambiguous ZIP64 extra field")
	}

	values := zip64ExtraValues{}
	if needUncompressedSize {
		values.uncompressedSize = binary.LittleEndian.Uint64(zip64Field[:8])
		zip64Field = zip64Field[8:]
	}
	if needCompressedSize {
		values.compressedSize = binary.LittleEndian.Uint64(zip64Field[:8])
		zip64Field = zip64Field[8:]
	}
	if needLocalOffset {
		values.localOffset = binary.LittleEndian.Uint64(zip64Field[:8])
	}
	return values, nil
}

func reconcileZIPLocalHeader(content []byte, centralOffset uint64, entry *zipPreflightEntry) error {
	if entry.localOffset > centralOffset || centralOffset-entry.localOffset < 30 {
		return admissionRejectedError("invalid ZIP local header offset")
	}
	header := content[int(entry.localOffset):int(entry.localOffset+30)]
	if !bytes.Equal(header[:4], []byte{'P', 'K', 0x03, 0x04}) {
		return admissionRejectedError("invalid ZIP local signature")
	}
	localFlags := binary.LittleEndian.Uint16(header[6:8])
	localMethod := binary.LittleEndian.Uint16(header[8:10])
	if localFlags != entry.flags || localMethod != entry.method {
		return admissionRejectedError("ZIP local and central metadata mismatch")
	}
	nameLength := uint64(binary.LittleEndian.Uint16(header[26:28]))
	extraLength := uint64(binary.LittleEndian.Uint16(header[28:30]))
	variableStart := entry.localOffset + 30
	if nameLength+extraLength > centralOffset-variableStart {
		return admissionRejectedError("invalid ZIP local field lengths")
	}
	name := content[int(variableStart):int(variableStart+nameLength)]
	if !bytes.Equal(name, entry.name) {
		return admissionRejectedError("ZIP local and central names mismatch")
	}
	localExtraStart := variableStart + nameLength
	localExtra := content[int(localExtraStart):int(localExtraStart+extraLength)]
	localCompressedSize32 := binary.LittleEndian.Uint32(header[18:22])
	localUncompressedSize32 := binary.LittleEndian.Uint32(header[22:26])
	needLocalCompressedSize64 := localCompressedSize32 == math.MaxUint32
	needLocalUncompressedSize64 := localUncompressedSize32 == math.MaxUint32
	zip64Values, err := readZIP64Extra(localExtra, needLocalUncompressedSize64, needLocalCompressedSize64, false)
	if err != nil {
		return err
	}
	localCompressedSize := uint64(localCompressedSize32)
	if needLocalCompressedSize64 {
		localCompressedSize = zip64Values.compressedSize
	}
	localUncompressedSize := uint64(localUncompressedSize32)
	if needLocalUncompressedSize64 {
		localUncompressedSize = zip64Values.uncompressedSize
	}
	if localFlags&0x0008 == 0 {
		if binary.LittleEndian.Uint32(header[14:18]) != entry.crc32 ||
			localCompressedSize != entry.compressedSize || localUncompressedSize != entry.uncompressedSize {
			return admissionRejectedError("ZIP local and central sizes mismatch")
		}
	} else if (binary.LittleEndian.Uint32(header[14:18]) != 0 && binary.LittleEndian.Uint32(header[14:18]) != entry.crc32) ||
		(localCompressedSize32 != 0 && localCompressedSize != entry.compressedSize) ||
		(localUncompressedSize32 != 0 && localUncompressedSize != entry.uncompressedSize) {
		return admissionRejectedError("ZIP local and central descriptor metadata mismatch")
	}
	entry.dataOffset = int64(variableStart + nameLength + extraLength)
	if uint64(entry.dataOffset) > centralOffset || entry.compressedSize > centralOffset-uint64(entry.dataOffset) {
		return admissionRejectedError("invalid ZIP local data bounds")
	}
	entry.dataEnd = uint64(entry.dataOffset) + entry.compressedSize
	return nil
}

func validateZIPLocalLayout(content []byte, centralOffset uint64, entries []zipPreflightEntry) error {
	ordered := append([]zipPreflightEntry(nil), entries...)
	sort.Slice(ordered, func(left, right int) bool { return ordered[left].localOffset < ordered[right].localOffset })
	for index := range ordered {
		boundary := centralOffset
		if index+1 < len(ordered) {
			boundary = ordered[index+1].localOffset
		}
		entry := ordered[index]
		if index > 0 && entry.localOffset < ordered[index-1].dataEnd {
			return admissionRejectedError("overlapping ZIP local entries")
		}
		layoutEnd := entry.dataEnd
		if entry.flags&0x0008 != 0 {
			if layoutEnd > boundary {
				return admissionRejectedError("missing ZIP data descriptor")
			}
			descriptor := content[int(layoutEnd):int(boundary)]
			if !validZIPDataDescriptor(descriptor, entry) {
				return admissionRejectedError("invalid ZIP data descriptor")
			}
			layoutEnd = boundary
		}
		if layoutEnd != boundary {
			return admissionRejectedError("ambiguous ZIP local data layout")
		}
	}
	return nil
}

func validZIPDataDescriptor(descriptor []byte, entry zipPreflightEntry) bool {
	unsignedLength, signedLength := 12, 16
	if entry.zip64Descriptor {
		unsignedLength, signedLength = 20, 24
	}
	signed := false
	switch len(descriptor) {
	case unsignedLength:
		if binary.LittleEndian.Uint32(descriptor[:4]) == 0x08074b50 {
			return false
		}
	case signedLength:
		if binary.LittleEndian.Uint32(descriptor[:4]) != 0x08074b50 {
			return false
		}
		signed = true
	default:
		return false
	}
	if signed {
		descriptor = descriptor[4:]
	}
	if binary.LittleEndian.Uint32(descriptor[:4]) != entry.crc32 {
		return false
	}
	descriptor = descriptor[4:]
	if entry.zip64Descriptor {
		return binary.LittleEndian.Uint64(descriptor[:8]) == entry.compressedSize &&
			binary.LittleEndian.Uint64(descriptor[8:16]) == entry.uncompressedSize
	}
	return uint64(binary.LittleEndian.Uint32(descriptor[:4])) == entry.compressedSize &&
		uint64(binary.LittleEndian.Uint32(descriptor[4:8])) == entry.uncompressedSize
}

func inspectTAR(ctx context.Context, content []byte, depth int, inspection *archiveInspection) error {
	if len(content) < 1024 || len(content)%512 != 0 || !allZeroBytes(content[len(content)-1024:]) {
		return admissionRejectedError("truncated TAR")
	}
	source := bytes.NewReader(content)
	reader := tar.NewReader(source)
	paths := make(map[string]struct{})
	hasEntries := false
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return admissionRejectedError("malformed TAR")
		}
		hasEntries = true
		if err := inspection.consumeEntries(1); err != nil {
			return err
		}
		normalized, err := validateArchivePath(header.Name, inspection.limits.MaxEntryNameBytes)
		if err != nil {
			return err
		}
		if _, duplicate := paths[normalized]; duplicate {
			return admissionRejectedError("duplicate archive path")
		}
		paths[normalized] = struct{}{}

		switch header.Typeflag {
		case tar.TypeReg, tar.TypeRegA:
		case tar.TypeDir:
			if header.Size != 0 {
				return admissionRejectedError("TAR directory has content")
			}
			continue
		default:
			return admissionRejectedError("unsafe TAR entry type")
		}
		if unsafeArchiveMode(header.FileInfo().Mode()) {
			return admissionRejectedError("unsafe TAR entry permissions")
		}
		if header.Size < 0 {
			return admissionRejectedError("negative TAR entry size")
		}
		if err := inspection.checkExpandedBytes(header.Size); err != nil {
			return err
		}
		_, archiveName := archiveKindFromName(normalized)
		probe, err := readArchivePayload(ctx, reader, inspection.remainingExpandedBytes(), archiveName)
		if err != nil {
			return err
		}
		if probe.total != header.Size {
			return admissionRejectedError("TAR entry size mismatch")
		}
		if err := inspection.consumeExpandedBytes(probe.total); err != nil {
			return err
		}
		if err := inspectArchivePayload(ctx, normalized, probe, depth, inspection, false); err != nil {
			return err
		}
	}
	if !hasEntries {
		return admissionRejectedError("empty or inconsistent TAR")
	}
	if remaining := content[len(content)-source.Len():]; len(remaining) > 0 && !allZeroBytes(remaining) {
		return admissionRejectedError("TAR trailing content")
	}
	return nil
}

func inspectGZIP(ctx context.Context, content []byte, depth int, inspection *archiveInspection) error {
	if len(content) < 18 || !bytes.HasPrefix(content, []byte{0x1f, 0x8b, 0x08}) {
		return admissionRejectedError("GZIP signature mismatch")
	}
	source := bytes.NewReader(content)
	reader, err := gzip.NewReader(source)
	if err != nil {
		return admissionRejectedError("malformed GZIP")
	}
	defer reader.Close()
	paths := make(map[string]struct{})
	hasPayload := false

	for {
		reader.Multistream(false)
		name := reader.Name
		if name != "" {
			normalized, err := validateArchivePath(name, inspection.limits.MaxEntryNameBytes)
			if err != nil {
				return err
			}
			if _, duplicate := paths[normalized]; duplicate {
				return admissionRejectedError("duplicate archive path")
			}
			paths[normalized] = struct{}{}
		}
		if err := inspection.consumeEntries(1); err != nil {
			return err
		}
		_, archiveName := archiveKindFromName(name)
		probe, err := readArchivePayload(ctx, reader, inspection.remainingExpandedBytes(), archiveName)
		if err != nil {
			return err
		}
		if probe.total > 0 {
			hasPayload = true
		}
		if err := inspection.consumeExpandedBytes(probe.total); err != nil {
			return err
		}
		if err := inspectArchivePayload(ctx, name, probe, depth, inspection, name == ""); err != nil {
			return err
		}
		if source.Len() == 0 {
			if !hasPayload {
				return admissionRejectedError("empty GZIP stream")
			}
			return nil
		}
		if err := reader.Reset(source); err != nil {
			return admissionRejectedError("trailing or malformed GZIP")
		}
	}
}

func inspectZstandard(ctx context.Context, content []byte, depth int, inspection *archiveInspection) error {
	if len(content) < 8 || !hasZstandardFrameMagic(content) {
		return admissionRejectedError("invalid Zstandard frame envelope")
	}
	frameCount, err := preflightZstandardFrames(ctx, content, inspection.remainingEntries())
	if err != nil {
		return err
	}
	maxMemory := uint64(inspection.limits.MaxExpandedBytes) + 1
	maxWindow := maxMemory
	if maxWindow > uint64(64*MiB) {
		maxWindow = uint64(64 * MiB)
	}
	if maxWindow < zstd.MinWindowSize {
		maxWindow = zstd.MinWindowSize
	}
	decoder, err := zstd.NewReader(
		bytes.NewReader(content),
		zstd.WithDecoderConcurrency(1),
		zstd.WithDecoderLowmem(true),
		zstd.WithDecodeBuffersBelow(0),
		zstd.WithDecoderMaxMemory(maxMemory),
		zstd.WithDecoderMaxWindow(maxWindow),
	)
	if err != nil {
		return admissionRejectedError("malformed Zstandard")
	}
	if err := inspection.consumeEntries(frameCount); err != nil {
		decoder.Close()
		return err
	}
	probe, readErr := readArchivePayload(ctx, decoder, inspection.remainingExpandedBytes(), false)
	decoder.Close()
	if readErr != nil {
		return readErr
	}
	if probe.total == 0 {
		return admissionRejectedError("empty Zstandard stream")
	}
	if archiveRatioExceeded(uint64(probe.total), uint64(len(content)), inspection.limits.MaxCompressionRatio) {
		return admissionLimitError("archive compression ratio")
	}
	if err := inspection.consumeExpandedBytes(probe.total); err != nil {
		return err
	}
	return inspectArchivePayload(ctx, "", probe, depth, inspection, true)
}

func preflightZstandardFrames(ctx context.Context, content []byte, maxEntries int) (int, error) {
	frameCount := 0
	for offset := 0; offset < len(content); {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if frameCount >= maxEntries {
			return 0, admissionLimitError("archive entries")
		}

		var header zstd.Header
		if err := header.Decode(content[offset:]); err != nil {
			return 0, admissionRejectedError("malformed Zstandard frame")
		}
		frameCount++
		if header.Skippable {
			frameSize := uint64(header.HeaderSize) + uint64(header.SkippableSize)
			if frameSize > uint64(len(content)-offset) {
				return 0, admissionRejectedError("truncated Zstandard skippable frame")
			}
			offset += int(frameSize)
			continue
		}

		if header.HeaderSize > len(content)-offset {
			return 0, admissionRejectedError("truncated Zstandard frame header")
		}
		cursor := offset + header.HeaderSize
		for {
			if err := ctx.Err(); err != nil {
				return 0, err
			}
			if len(content)-cursor < 3 {
				return 0, admissionRejectedError("truncated Zstandard block header")
			}
			blockHeader := uint32(content[cursor]) | uint32(content[cursor+1])<<8 | uint32(content[cursor+2])<<16
			lastBlock := blockHeader&1 != 0
			blockSize := uint64(blockHeader >> 3)
			var payloadSize uint64
			switch blockHeader >> 1 & 0x03 {
			case 0, 2:
				payloadSize = blockSize
			case 1:
				payloadSize = 1
			default:
				return 0, admissionRejectedError("reserved Zstandard block type")
			}
			cursor += 3
			if payloadSize > uint64(len(content)-cursor) {
				return 0, admissionRejectedError("truncated Zstandard block")
			}
			cursor += int(payloadSize)
			if lastBlock {
				break
			}
		}
		if header.HasCheckSum {
			if len(content)-cursor < 4 {
				return 0, admissionRejectedError("truncated Zstandard checksum")
			}
			cursor += 4
		}
		offset = cursor
	}
	return frameCount, nil
}

func unsafeArchiveMode(mode fs.FileMode) bool {
	return mode.Perm()&0o111 != 0 || mode&(fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky) != 0
}

func validateArchivePath(name string, maxNameBytes int) (string, error) {
	if len(name) > maxNameBytes {
		return "", admissionLimitError("archive entry name bytes")
	}
	if name == "" || !utf8.ValidString(name) || strings.IndexByte(name, 0) >= 0 ||
		strings.Contains(name, "\\") || path.IsAbs(name) {
		return "", admissionRejectedError("unsafe archive path")
	}
	trimmed := strings.TrimSuffix(name, "/")
	if trimmed == "" || strings.Contains(trimmed, "//") {
		return "", admissionRejectedError("unsafe archive path")
	}
	segments := strings.Split(trimmed, "/")
	for _, segment := range segments {
		if segment == ".." || segment == "" || segment != strings.TrimSpace(segment) ||
			strings.HasSuffix(segment, ".") || strings.Contains(segment, ":") {
			return "", admissionRejectedError("unsafe archive path")
		}
		for _, value := range segment {
			if value < 0x20 || value == 0x7f {
				return "", admissionRejectedError("unsafe archive path")
			}
		}
	}
	normalized := path.Clean(trimmed)
	if normalized == "." || normalized == ".." || strings.HasPrefix(normalized, "../") {
		return "", admissionRejectedError("unsafe archive path")
	}
	return normalized, nil
}

func archiveUint64Size(size uint64) (int64, error) {
	if size > math.MaxInt64 {
		return 0, admissionLimitError("archive expanded bytes")
	}
	return int64(size), nil
}

func addArchiveBytes(current, additional, maximum int64) (int64, error) {
	if current < 0 || additional < 0 || current > maximum || additional > maximum-current {
		return 0, admissionLimitError("archive expanded bytes")
	}
	return current + additional, nil
}

func archiveRatioExceeded(expanded, compressed uint64, maximum int64) bool {
	if expanded == 0 {
		return false
	}
	if compressed == 0 {
		return true
	}
	maximumRatio := uint64(maximum)
	quotient, remainder := expanded/compressed, expanded%compressed
	return quotient > maximumRatio || quotient == maximumRatio && remainder != 0
}

type archivePayloadProbe struct {
	prefix     []byte
	tail       []byte
	content    []byte
	total      int64
	captureAll bool
	activeText activeTextDetector
}

func readArchivePayload(ctx context.Context, reader io.Reader, maximum int64, captureAll bool) (archivePayloadProbe, error) {
	if maximum < 0 || maximum >= math.MaxInt64 {
		return archivePayloadProbe{}, admissionLimitError("archive expanded bytes")
	}
	limited := &io.LimitedReader{R: reader, N: maximum + 1}
	buffer := make([]byte, 32*1024)
	probe := archivePayloadProbe{
		prefix:     make([]byte, 0, minInt(archiveProbeBytes, int(maximum))),
		captureAll: captureAll,
	}
	if captureAll {
		probe.content = make([]byte, 0, minInt(archiveProbeBytes, int(maximum)))
	}
	emptyReads := 0
	for {
		if err := ctx.Err(); err != nil {
			return archivePayloadProbe{}, err
		}
		count, err := limited.Read(buffer)
		if count < 0 || count > len(buffer) {
			return archivePayloadProbe{}, admissionRejectedError("invalid archive reader result")
		}
		if count > 0 {
			probe.total += int64(count)
			if err := probe.observe(ctx, buffer[:count]); err != nil {
				return archivePayloadProbe{}, err
			}
			emptyReads = 0
		} else if err == nil {
			emptyReads++
			if emptyReads >= 100 {
				return archivePayloadProbe{}, admissionRejectedError("archive reader made no progress")
			}
		}
		if probe.total > maximum {
			return archivePayloadProbe{}, admissionLimitError("archive expanded bytes")
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return probe, nil
			}
			if errors.Is(err, zstd.ErrDecoderSizeExceeded) || errors.Is(err, zstd.ErrWindowSizeExceeded) {
				return archivePayloadProbe{}, admissionLimitError("archive expanded bytes")
			}
			return archivePayloadProbe{}, admissionRejectedError("malformed archive payload")
		}
		if limited.N == 0 {
			return probe, nil
		}
	}
}

func (probe *archivePayloadProbe) observe(ctx context.Context, content []byte) error {
	if err := probe.activeText.observe(ctx, content); err != nil {
		return err
	}
	wasCapturing := probe.captureAll
	if len(probe.prefix) < archiveProbeBytes {
		remaining := archiveProbeBytes - len(probe.prefix)
		probe.prefix = append(probe.prefix, content[:minInt(len(content), remaining)]...)
	}
	if !probe.captureAll {
		if _, possibleArchive := archiveKindFromMagic(probe.prefix); possibleArchive {
			probe.captureAll = true
			probe.content = append(probe.content, probe.prefix...)
		}
	} else if wasCapturing {
		probe.content = append(probe.content, content...)
	}
	const tailBytes = 512
	if len(content) >= tailBytes {
		probe.tail = append(probe.tail[:0], content[len(content)-tailBytes:]...)
		return nil
	}
	if len(probe.tail)+len(content) > tailBytes {
		drop := len(probe.tail) + len(content) - tailBytes
		copy(probe.tail, probe.tail[drop:])
		probe.tail = probe.tail[:len(probe.tail)-drop]
	}
	probe.tail = append(probe.tail, content...)
	return nil
}

func inspectArchivePayload(
	ctx context.Context,
	name string,
	probe archivePayloadProbe,
	depth int,
	inspection *archiveInspection,
	allowMagicOnly bool,
) error {
	declaredKind, hasArchiveName := archiveKindFromName(name)
	magicKind, hasArchiveMagic := archiveKindFromMagic(probe.prefix)
	if hasUnsupportedArchiveMagic(probe.prefix) {
		return admissionRejectedError("unsupported nested archive")
	}
	if hasArchiveName {
		if !hasArchiveMagic || magicKind != declaredKind {
			return admissionRejectedError("nested archive extension and signature mismatch")
		}
	} else if hasArchiveMagic && !allowMagicOnly {
		return admissionRejectedError("nested archive magic without matching extension")
	}
	if hasArchiveMagic {
		if int64(len(probe.content)) != probe.total {
			return admissionRejectedError("incomplete nested archive capture")
		}
		return inspectArchiveAtDepth(ctx, magicKind, probe.content, depth+1, inspection)
	}
	if dangerousArchivePayload(name, probe) {
		return admissionRejectedError("unsafe archive payload")
	}
	return nil
}

func archiveKindFromName(name string) (admissionContentKind, bool) {
	switch strings.ToLower(path.Ext(name)) {
	case ".zip":
		return admissionContentZIP, true
	case ".tar":
		return admissionContentTAR, true
	case ".gz", ".tgz":
		return admissionContentGZIP, true
	case ".zst", ".zstd":
		return admissionContentZstandard, true
	default:
		return 0, false
	}
}

func archiveKindFromMagic(content []byte) (admissionContentKind, bool) {
	switch {
	case bytes.HasPrefix(content, []byte{'P', 'K', 0x03, 0x04}), bytes.HasPrefix(content, []byte{'P', 'K', 0x05, 0x06}):
		return admissionContentZIP, true
	case bytes.HasPrefix(content, []byte{0x1f, 0x8b, 0x08}):
		return admissionContentGZIP, true
	case hasZstandardFrameMagic(content):
		return admissionContentZstandard, true
	case len(content) >= 262 && string(content[257:262]) == "ustar":
		return admissionContentTAR, true
	default:
		return 0, false
	}
}

func hasUnsupportedArchiveMagic(content []byte) bool {
	return bytes.HasPrefix(content, sevenZipMagic) || bytes.HasPrefix(content, rarMagic)
}

func dangerousArchivePayload(name string, probe archivePayloadProbe) bool {
	if hasForbiddenBinarySignature(probe.prefix) || probe.activeText.active ||
		bytes.HasPrefix(probe.tail, []byte("conectix")) || bytes.HasPrefix(probe.tail, []byte("koly")) ||
		bytes.HasSuffix(probe.tail, []byte("koly")) {
		return true
	}
	lowerName := strings.ToLower(name)
	for _, extension := range []string{
		".apk", ".bat", ".cmd", ".com", ".deb", ".dll", ".dmg", ".docm", ".exe", ".gz",
		".htm", ".html", ".img", ".iso", ".jar", ".js", ".mjs", ".msi", ".php", ".pl", ".ps1",
		".py", ".qcow2", ".rar", ".rb", ".rpm", ".scr", ".sh", ".svg", ".tar", ".tgz", ".vhd", ".vhdx", ".vmdk", ".xlsm",
		".xlam", ".pptm", ".zip", ".zst", ".zstd", ".7z",
	} {
		if strings.HasSuffix(lowerName, extension) {
			return true
		}
	}
	return false
}

func hasArchiveMagic(content []byte) bool {
	_, supported := archiveKindFromMagic(content)
	return supported || hasUnsupportedArchiveMagic(content)
}

func hasZstandardFrameMagic(content []byte) bool {
	return bytes.HasPrefix(content, zstandardMagic) || len(content) >= 4 &&
		content[0]&0xf0 == 0x50 && content[1] == 0x2a && content[2] == 0x4d && content[3] == 0x18
}

func allZeroBytes(content []byte) bool {
	for _, value := range content {
		if value != 0 {
			return false
		}
	}
	return true
}
