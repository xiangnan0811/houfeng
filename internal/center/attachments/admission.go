package attachments

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"mime"
	"net/http"
	"net/url"
	"path"
	"reflect"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/filter"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/fault"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"golang.org/x/image/webp"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

var (
	ErrInvalidAdmissionRequest = errors.New("invalid attachment admission request")
	ErrAdmissionRejected       = errors.New("attachment admission rejected")
	ErrAdmissionLimitExceeded  = errors.New("attachment admission limit exceeded")
	disablePDFCPUConfig        sync.Once
)

const pdfMaxObjectDepth = 32

const maxPDFObjectsPerObjectStream = 100

type ArchiveLimits struct {
	MaxEntries          int
	MaxEntryNameBytes   int
	MaxNestingDepth     int
	MaxExpandedBytes    int64
	MaxCompressionRatio int64
}

type AdmissionLimits struct {
	MaxReadBytes                   int64
	MaxImageWidth                  int
	MaxImageHeight                 int
	MaxImagePixels                 int64
	MaxPDFBytes                    int64
	MaxPDFDecodedObjectStreamBytes int64
	MaxPDFObjects                  int
	MaxPDFPages                    int
	Archive                        ArchiveLimits
}

func DefaultAdmissionLimits(base Limits) AdmissionLimits {
	maxExpandedBytes := base.MaxFileBytes
	if maxExpandedBytes > 0 && maxExpandedBytes <= math.MaxInt64/4 {
		maxExpandedBytes *= 4
	}
	return AdmissionLimits{
		MaxReadBytes:                   base.MaxFileBytes,
		MaxImageWidth:                  16_384,
		MaxImageHeight:                 16_384,
		MaxImagePixels:                 40_000_000,
		MaxPDFBytes:                    base.MaxFileBytes,
		MaxPDFDecodedObjectStreamBytes: base.MaxFileBytes,
		MaxPDFObjects:                  100_000,
		MaxPDFPages:                    500,
		Archive: ArchiveLimits{
			MaxEntries:          4_096,
			MaxEntryNameBytes:   1_024,
			MaxNestingDepth:     1,
			MaxExpandedBytes:    maxExpandedBytes,
			MaxCompressionRatio: 100,
		},
	}
}

func newPDFConfiguration(limits AdmissionLimits) (*model.Configuration, error) {
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	objectStreamProbeBytes := pdfObjectStreamProbeBytes(limits)
	if objectStreamProbeBytes <= 0 {
		return nil, ErrInvalidAdmissionRequest
	}

	disablePDFCPUConfig.Do(api.DisableConfigDir)
	parserObjectLimit := limits.MaxPDFObjects*2 + 1
	configuration := model.NewDefaultConfiguration()
	configuration.ValidationMode = model.ValidationStrict
	configuration.Reader15 = true
	configuration.Offline = true
	configuration.ValidateLinks = false
	configuration.DecodeAllStreams = false
	configuration.Optimize = false
	configuration.Cmd = model.VALIDATE
	configuration.Limits = model.ResourceLimits{
		MaxStreamBytes:       limits.MaxPDFBytes,
		MaxDecodeBytes:       limits.MaxPDFBytes,
		MaxImagePixels:       limits.MaxImagePixels,
		MaxImageBytes:        limits.MaxImagePixels * 4,
		MaxObjectCount:       parserObjectLimit,
		MaxObjectStreamCount: maxPDFObjectsPerObjectStream,
		MaxObjectStreamFirst: objectStreamProbeBytes,
		MaxXRefEntries:       parserObjectLimit,
		MaxRecursionDepth:    pdfMaxObjectDepth,
	}
	return configuration, nil
}

func newPDFInspectionConfiguration(limits AdmissionLimits, objectStreamCount int) (*model.Configuration, error) {
	if objectStreamCount < 0 || objectStreamCount > limits.MaxPDFObjects {
		return nil, admissionLimitError("PDF object streams")
	}
	configuration, err := newPDFConfiguration(limits)
	if err != nil || objectStreamCount == 0 {
		return configuration, err
	}
	configuration.Limits.MaxDecodeBytes = pdfDecodedObjectStreamBudget(limits)
	return configuration, nil
}

func pdfObjectStreamProbeBytes(limits AdmissionLimits) int64 {
	parserObjectLimit := int64(limits.MaxPDFObjects)*2 + 1
	// An object stream prolog contains at most 100 "object-number offset" pairs.
	// Larger prologs can only add comments or padding, which admission rejects.
	return int64(maxPDFObjectsPerObjectStream) * int64(decimalDigits(parserObjectLimit)+decimalDigits(limits.MaxPDFBytes)+2)
}

func pdfDecodedObjectStreamBudget(limits AdmissionLimits) int64 {
	if limits.MaxPDFDecodedObjectStreamBytes < limits.MaxPDFBytes {
		return limits.MaxPDFDecodedObjectStreamBytes
	}
	return limits.MaxPDFBytes
}

func decimalDigits(value int64) int {
	if value < 0 {
		return 0
	}
	digits := 1
	for value >= 10 {
		value /= 10
		digits++
	}
	return digits
}

func (limits AdmissionLimits) Validate() error {
	maxInt := int(^uint(0) >> 1)
	if limits.MaxReadBytes <= 0 || limits.MaxReadBytes >= math.MaxInt64 || limits.MaxReadBytes >= int64(maxInt) ||
		limits.MaxImageWidth <= 0 || limits.MaxImageHeight <= 0 || limits.MaxImagePixels <= 0 || limits.MaxImagePixels > math.MaxInt64/4 ||
		limits.MaxPDFBytes <= 0 || limits.MaxPDFBytes > limits.MaxReadBytes ||
		limits.MaxPDFDecodedObjectStreamBytes <= 0 ||
		limits.MaxPDFObjects <= 0 || limits.MaxPDFObjects > (maxInt-1)/2 || limits.MaxPDFPages <= 0 || limits.Archive.validate() != nil {
		return ErrInvalidAdmissionRequest
	}
	if uint64(limits.MaxImageWidth) > math.MaxInt64/uint64(limits.MaxImageHeight) {
		return ErrInvalidAdmissionRequest
	}
	return nil
}

func (limits ArchiveLimits) validate() error {
	if limits.MaxEntries <= 0 || limits.MaxEntryNameBytes <= 0 || limits.MaxNestingDepth <= 0 ||
		limits.MaxExpandedBytes <= 0 || limits.MaxExpandedBytes >= math.MaxInt64 ||
		limits.MaxCompressionRatio <= 0 {
		return ErrInvalidAdmissionRequest
	}
	return nil
}

type AdmissionRequest struct {
	DisplayName       string
	DeclaredMediaType string
	SizeBytes         int64
	Content           io.Reader
	ScannerStatus     ScannerStatus
}

type AdmissionResult struct {
	MediaType string
	Profile   ProcessorProfile
}

type admissionContentKind uint8

const (
	admissionContentPNG admissionContentKind = iota + 1
	admissionContentJPEG
	admissionContentWebP
	admissionContentPDF
	admissionContentText
	admissionContentZIP
	admissionContentTAR
	admissionContentGZIP
	admissionContentZstandard
)

func AdmitContent(ctx context.Context, request AdmissionRequest, limits AdmissionLimits) (AdmissionResult, error) {
	if ctx == nil {
		return AdmissionResult{}, ErrInvalidAdmissionRequest
	}
	if err := ctx.Err(); err != nil {
		return AdmissionResult{}, err
	}
	if err := limits.Validate(); err != nil {
		return AdmissionResult{}, err
	}
	if err := validateAdmissionRequest(request); err != nil {
		return AdmissionResult{}, err
	}
	kind, canonicalMediaType, err := classifyAdmissionDeclaration(request.DisplayName, request.DeclaredMediaType)
	if err != nil {
		return AdmissionResult{}, err
	}
	if kind.isArchive() {
		if err := RequireArchiveScanner(request.ScannerStatus); err != nil {
			return AdmissionResult{}, err
		}
	}
	if request.SizeBytes > limits.MaxReadBytes {
		return AdmissionResult{}, admissionLimitError("declared bytes")
	}

	content, err := readAdmissionContent(ctx, request.Content, limits.MaxReadBytes)
	if err != nil {
		return AdmissionResult{}, err
	}
	if int64(len(content)) > limits.MaxReadBytes {
		return AdmissionResult{}, admissionLimitError("actual bytes")
	}
	if int64(len(content)) != request.SizeBytes {
		return AdmissionResult{}, admissionRejectedError("size mismatch")
	}
	if err := ctx.Err(); err != nil {
		return AdmissionResult{}, err
	}
	if hasForbiddenBinarySignature(content) {
		return AdmissionResult{}, admissionRejectedError("active content")
	}
	if kind == admissionContentText {
		activeText, err := looksLikeActiveText(ctx, content)
		if err != nil {
			return AdmissionResult{}, err
		}
		if activeText {
			return AdmissionResult{}, admissionRejectedError("active content")
		}
	}

	result := AdmissionResult{MediaType: canonicalMediaType}
	switch kind {
	case admissionContentPNG, admissionContentJPEG:
		if err := inspectStandardImage(content, kind, limits); err != nil {
			return AdmissionResult{}, err
		}
		result.Profile = ProcessorProfileImage
	case admissionContentWebP:
		if err := inspectWebP(content, limits); err != nil {
			return AdmissionResult{}, err
		}
		result.Profile = ProcessorProfileImage
	case admissionContentPDF:
		if err := inspectPDF(ctx, content, limits); err != nil {
			return AdmissionResult{}, err
		}
		result.Profile = ProcessorProfilePDF
	case admissionContentText:
		if err := inspectText(content); err != nil {
			return AdmissionResult{}, err
		}
		result.Profile = ProcessorProfileText
	case admissionContentZIP, admissionContentTAR, admissionContentGZIP, admissionContentZstandard:
		if err := inspectArchive(ctx, kind, content, limits.Archive); err != nil {
			return AdmissionResult{}, err
		}
		result.Profile = ProcessorProfileArchive
	default:
		return AdmissionResult{}, admissionRejectedError("unknown content")
	}
	return result, nil
}

func validateAdmissionRequest(request AdmissionRequest) error {
	if request.SizeBytes <= 0 || isNilAdmissionReader(request.Content) || !validAdmissionDisplayName(request.DisplayName) ||
		request.DeclaredMediaType == "" || len(request.DeclaredMediaType) > 255 {
		return ErrInvalidAdmissionRequest
	}
	return nil
}

func validAdmissionDisplayName(name string) bool {
	if name == "" || name != strings.TrimSpace(name) || len(name) > 255 || !utf8.ValidString(name) ||
		name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00") {
		return false
	}
	for _, value := range name {
		if value < 0x20 || value == 0x7f {
			return false
		}
	}
	return !hasDangerousCompoundExtension(strings.ToLower(name))
}

func hasDangerousCompoundExtension(name string) bool {
	dangerous := []string{
		".bat.", ".cmd.", ".com.", ".dmg.", ".docm.", ".exe.", ".img.", ".iso.",
		".jar.", ".js.", ".mjs.", ".ps1.", ".qcow2.", ".scr.", ".sh.", ".svg.", ".vhd.", ".vhdx.",
	}
	for _, marker := range dangerous {
		if strings.Contains(name, marker) {
			return true
		}
	}
	return false
}

func classifyAdmissionDeclaration(displayName, declared string) (admissionContentKind, string, error) {
	mediaType, parameters, err := mime.ParseMediaType(declared)
	if err != nil {
		return 0, "", admissionRejectedError("invalid declared media type")
	}
	mediaType = strings.ToLower(mediaType)
	extension := strings.ToLower(path.Ext(displayName))
	if len(parameters) > 0 {
		charset, ok := parameters["charset"]
		if len(parameters) != 1 || !ok || !strings.EqualFold(charset, "utf-8") || !strings.HasPrefix(mediaType, "text/") {
			return 0, "", admissionRejectedError("unsupported media type parameter")
		}
	}

	switch {
	case extension == ".png" && mediaType == "image/png":
		return admissionContentPNG, mediaType, nil
	case (extension == ".jpg" || extension == ".jpeg") && mediaType == "image/jpeg":
		return admissionContentJPEG, mediaType, nil
	case extension == ".webp" && mediaType == "image/webp":
		return admissionContentWebP, mediaType, nil
	case extension == ".pdf" && mediaType == "application/pdf":
		return admissionContentPDF, mediaType, nil
	case extension == ".zip" && mediaType == "application/zip":
		return admissionContentZIP, mediaType, nil
	case extension == ".tar" && mediaType == "application/x-tar":
		return admissionContentTAR, mediaType, nil
	case extension == ".gz" && mediaType == "application/gzip":
		return admissionContentGZIP, mediaType, nil
	case (extension == ".zst" || extension == ".zstd") && mediaType == "application/zstd":
		return admissionContentZstandard, mediaType, nil
	case admissionTextDeclaration(extension, mediaType):
		return admissionContentText, mediaType, nil
	default:
		return 0, "", admissionRejectedError("extension and media type mismatch")
	}
}

func admissionTextDeclaration(extension, mediaType string) bool {
	switch extension {
	case ".txt", ".log":
		return mediaType == "text/plain"
	case ".md", ".markdown":
		return mediaType == "text/markdown" || mediaType == "text/plain"
	case ".json":
		return mediaType == "application/json"
	case ".yaml", ".yml":
		return mediaType == "application/yaml" || mediaType == "application/x-yaml" || mediaType == "text/yaml"
	case ".csv":
		return mediaType == "text/csv"
	case ".tsv":
		return mediaType == "text/tab-separated-values"
	case ".ini":
		return mediaType == "text/plain"
	case ".toml":
		return mediaType == "application/toml" || mediaType == "text/plain"
	case ".patch", ".diff":
		return mediaType == "text/x-patch" || mediaType == "text/x-diff" || mediaType == "text/plain"
	default:
		return false
	}
}

func (kind admissionContentKind) isArchive() bool {
	return kind == admissionContentZIP || kind == admissionContentTAR ||
		kind == admissionContentGZIP || kind == admissionContentZstandard
}

func readAdmissionContent(ctx context.Context, reader io.Reader, maxBytes int64) ([]byte, error) {
	limited := &io.LimitedReader{R: reader, N: maxBytes + 1}
	content := make([]byte, 0, minInt(int(maxBytes), 32*1024))
	buffer := make([]byte, 32*1024)
	emptyReads := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		count, err := limited.Read(buffer)
		if count < 0 || count > len(buffer) {
			return nil, admissionRejectedError("invalid reader result")
		}
		if count > 0 {
			content = append(content, buffer[:count]...)
			emptyReads = 0
		} else if err == nil {
			emptyReads++
			if emptyReads >= 100 {
				return nil, admissionRejectedError("reader made no progress")
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				if contextErr := ctx.Err(); contextErr != nil {
					return nil, contextErr
				}
				return content, nil
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			return nil, admissionRejectedError("content read failed")
		}
		if limited.N == 0 {
			return content, nil
		}
	}
}

func inspectStandardImage(content []byte, kind admissionContentKind, limits AdmissionLimits) error {
	var (
		config  image.Config
		decoded image.Image
		err     error
	)
	switch kind {
	case admissionContentPNG:
		config, err = png.DecodeConfig(bytes.NewReader(content))
		if err == nil {
			if err := validateImageDimensions(config.Width, config.Height, limits); err != nil {
				return err
			}
			reader := bytes.NewReader(content)
			decoded, err = png.Decode(reader)
			if err == nil && reader.Len() != 0 {
				return admissionRejectedError("PNG trailing content")
			}
		}
	case admissionContentJPEG:
		if !hasExactJPEGStream(content) {
			return admissionRejectedError("JPEG trailing or malformed content")
		}
		config, err = jpeg.DecodeConfig(bytes.NewReader(content))
		if err == nil {
			if err := validateImageDimensions(config.Width, config.Height, limits); err != nil {
				return err
			}
			decoded, err = jpeg.Decode(bytes.NewReader(content))
		}
	default:
		return admissionRejectedError("unknown image type")
	}
	if err != nil {
		return admissionRejectedError("malformed image")
	}
	// Dimensions are checked immediately after DecodeConfig above so malformed
	// pixel payloads cannot force an oversized allocation before rejection.
	if decoded.Bounds().Dx() != config.Width || decoded.Bounds().Dy() != config.Height {
		return admissionRejectedError("malformed image")
	}
	return nil
}

func inspectText(content []byte) error {
	if !utf8.Valid(content) || bytes.IndexByte(content, 0) >= 0 {
		return admissionRejectedError("invalid UTF-8 text")
	}
	detected, _, err := mime.ParseMediaType(http.DetectContentType(content))
	if err != nil || detected != "text/plain" {
		return admissionRejectedError("content does not have text evidence")
	}
	for _, value := range string(content) {
		if unicode.IsControl(value) && value != '\t' && value != '\n' && value != '\r' {
			return admissionRejectedError("unsupported text control character")
		}
	}
	return nil
}

func hasExactJPEGStream(content []byte) bool {
	if len(content) < 4 || content[0] != 0xff || content[1] != 0xd8 {
		return false
	}
	offset := 2
	inEntropyData := false
	for offset < len(content) {
		if inEntropyData {
			if content[offset] != 0xff {
				offset++
				continue
			}
		} else if content[offset] != 0xff {
			return false
		}
		for offset < len(content) && content[offset] == 0xff {
			offset++
		}
		if offset >= len(content) {
			return false
		}
		marker := content[offset]
		offset++
		if inEntropyData {
			switch {
			case marker == 0x00, marker >= 0xd0 && marker <= 0xd7:
				continue
			default:
				inEntropyData = false
			}
		}
		switch {
		case marker == 0xd9:
			return offset == len(content)
		case marker == 0xd8 || marker == 0x00 || marker == 0x01 || marker >= 0xd0 && marker <= 0xd7:
			return false
		}
		if len(content)-offset < 2 {
			return false
		}
		segmentLength := int(binary.BigEndian.Uint16(content[offset : offset+2]))
		if segmentLength < 2 || segmentLength > len(content)-offset {
			return false
		}
		offset += segmentLength
		if marker == 0xda {
			inEntropyData = true
		}
	}
	return false
}

func validateImageDimensions(width, height int, limits AdmissionLimits) error {
	if width <= 0 || height <= 0 {
		return admissionRejectedError("invalid image dimensions")
	}
	if width > limits.MaxImageWidth || height > limits.MaxImageHeight ||
		int64(width) > limits.MaxImagePixels/int64(height) {
		return admissionLimitError("image complexity")
	}
	return nil
}

func inspectWebP(content []byte, limits AdmissionLimits) error {
	if !hasExactWebPContainer(content) {
		return admissionRejectedError("malformed WebP container")
	}
	config, err := webp.DecodeConfig(bytes.NewReader(content))
	if err != nil {
		return admissionRejectedError("malformed WebP")
	}
	if err := validateImageDimensions(config.Width, config.Height, limits); err != nil {
		return err
	}
	decoded, err := webp.Decode(bytes.NewReader(content))
	if err != nil || decoded.Bounds().Dx() != config.Width || decoded.Bounds().Dy() != config.Height {
		return admissionRejectedError("malformed WebP")
	}
	return nil
}

func hasExactWebPContainer(content []byte) bool {
	if len(content) < 20 || string(content[:4]) != "RIFF" || string(content[8:12]) != "WEBP" ||
		uint64(binary.LittleEndian.Uint32(content[4:8]))+8 != uint64(len(content)) {
		return false
	}

	var (
		extended       bool
		featureFlags   byte
		seenICCP       bool
		seenAlphaChunk bool
		seenImage      bool
		seenEXIF       bool
		seenXMP        bool
		imageHasAlpha  bool
	)
	chunkIndex := 0
	for offset := 12; offset < len(content); {
		if len(content)-offset < 8 {
			return false
		}
		chunkKind := string(content[offset : offset+4])
		chunkSize := uint64(binary.LittleEndian.Uint32(content[offset+4 : offset+8]))
		payloadStart := offset + 8
		payloadEnd := uint64(payloadStart) + chunkSize
		paddedEnd := payloadEnd + chunkSize%2
		if payloadEnd > uint64(len(content)) || paddedEnd > uint64(len(content)) {
			return false
		}
		if chunkSize%2 != 0 && content[int(payloadEnd)] != 0 {
			return false
		}
		payload := content[payloadStart:int(payloadEnd)]

		if chunkIndex == 0 {
			switch chunkKind {
			case "VP8 ", "VP8L":
				return paddedEnd == uint64(len(content))
			case "VP8X":
				if len(payload) != 10 || payload[0]&0xc3 != 0 || payload[1] != 0 || payload[2] != 0 || payload[3] != 0 {
					return false
				}
				extended = true
				featureFlags = payload[0]
			default:
				return false
			}
		} else {
			if !extended || seenAlphaChunk && chunkKind != "VP8 " {
				return false
			}
			switch chunkKind {
			case "ICCP":
				if seenICCP || seenImage {
					return false
				}
				seenICCP = true
			case "ALPH":
				if seenAlphaChunk || seenImage {
					return false
				}
				seenAlphaChunk = true
			case "VP8 ":
				if seenImage {
					return false
				}
				seenImage = true
				imageHasAlpha = seenAlphaChunk
				seenAlphaChunk = false
			case "VP8L":
				if seenImage || len(payload) < 5 || payload[0] != 0x2f {
					return false
				}
				seenImage = true
				imageHasAlpha = payload[4]&0x10 != 0
			case "EXIF":
				if !seenImage || seenEXIF || seenXMP {
					return false
				}
				seenEXIF = true
			case "XMP ":
				if !seenImage || seenXMP {
					return false
				}
				seenXMP = true
			default:
				return false
			}
		}
		offset = int(paddedEnd)
		chunkIndex++
	}
	return extended && seenImage &&
		featureFlags&(1<<5) != 0 == seenICCP &&
		featureFlags&(1<<4) != 0 == imageHasAlpha &&
		featureFlags&(1<<3) != 0 == seenEXIF &&
		featureFlags&(1<<2) != 0 == seenXMP
}

func inspectPDF(ctx context.Context, content []byte, limits AdmissionLimits) error {
	if ctx == nil {
		return ErrInvalidAdmissionRequest
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if int64(len(content)) > limits.MaxPDFBytes {
		return admissionLimitError("PDF bytes")
	}
	if !bytes.HasPrefix(content, []byte("%PDF-")) {
		return admissionRejectedError("PDF signature mismatch")
	}
	configuration, err := newPDFConfiguration(limits)
	if err != nil {
		return err
	}
	probeCtx, err := pdfcpu.ReadWithContext(ctx, bytes.NewReader(content), configuration)
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		if isPDFParserLimitError(err) {
			return admissionLimitError("PDF parser resources")
		}
		return admissionRejectedError("malformed PDF")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	objectStreamCount, err := countPDFObjectStreams(ctx, probeCtx, limits)
	if err != nil {
		return err
	}
	if err := validatePDFObjectStreamDecodeBudget(ctx, probeCtx, limits); err != nil {
		return err
	}
	configuration, err = newPDFInspectionConfiguration(limits, objectStreamCount)
	if err != nil {
		return err
	}
	pdfCtx, err := pdfcpu.ReadWithContext(ctx, bytes.NewReader(content), configuration)
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		if isPDFParserLimitError(err) {
			return admissionLimitError("PDF parser resources")
		}
		return admissionRejectedError("malformed PDF")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if pdfCtx.Encrypt != nil {
		return admissionRejectedError("encrypted PDF")
	}
	if err := inspectPDFObjects(ctx, pdfCtx, limits); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validatePDFContext(pdfCtx); err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		if isPDFParserLimitError(err) {
			return admissionLimitError("PDF validation resources")
		}
		return admissionRejectedError("malformed PDF")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if pdfCtx.PageCount > limits.MaxPDFPages {
		return admissionLimitError("PDF pages")
	}
	if pdfCtx.PageCount <= 0 {
		return admissionRejectedError("PDF has no pages")
	}
	return nil
}

func validatePDFContext(ctx *model.Context) (err error) {
	defer fault.Catch(&err)
	return api.ValidateContext(ctx)
}

func countPDFObjectStreams(ctx context.Context, pdfCtx *model.Context, limits AdmissionLimits) (int, error) {
	if ctx == nil {
		return 0, ErrInvalidAdmissionRequest
	}
	if pdfCtx == nil || pdfCtx.XRefTable == nil || pdfCtx.Table == nil {
		return 0, admissionRejectedError("missing PDF object table")
	}
	count := 0
	for _, entry := range pdfCtx.Table {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if entry == nil {
			return 0, admissionRejectedError("invalid PDF object table")
		}
		if entry.Free {
			continue
		}
		switch entry.Object.(type) {
		case types.ObjectStreamDict, *types.ObjectStreamDict:
			count++
			if count > limits.MaxPDFObjects {
				return 0, admissionLimitError("PDF object streams")
			}
		}
	}
	return count, nil
}

func validatePDFObjectStreamDecodeBudget(ctx context.Context, pdfCtx *model.Context, limits AdmissionLimits) error {
	if ctx == nil {
		return ErrInvalidAdmissionRequest
	}
	if pdfCtx == nil || pdfCtx.XRefTable == nil || pdfCtx.Table == nil {
		return admissionRejectedError("missing PDF object table")
	}
	objectNumbers := make([]int, 0, minInt(len(pdfCtx.Table), limits.MaxPDFObjects+1))
	for objectNumber, entry := range pdfCtx.Table {
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry == nil {
			return admissionRejectedError("invalid PDF object table")
		}
		if entry.Free {
			continue
		}
		switch entry.Object.(type) {
		case types.ObjectStreamDict, *types.ObjectStreamDict:
			if len(objectNumbers) == limits.MaxPDFObjects {
				return admissionLimitError("PDF object streams")
			}
			objectNumbers = append(objectNumbers, objectNumber)
		}
	}
	sort.Ints(objectNumbers)
	remainingBytes := pdfDecodedObjectStreamBudget(limits)
	for _, objectNumber := range objectNumbers {
		if err := ctx.Err(); err != nil {
			return err
		}
		entry := pdfCtx.Table[objectNumber]
		var objectStream types.ObjectStreamDict
		switch value := entry.Object.(type) {
		case types.ObjectStreamDict:
			objectStream = value
		case *types.ObjectStreamDict:
			if value == nil {
				return admissionRejectedError("missing PDF object stream")
			}
			objectStream = *value
		}
		if err := objectStream.DecodeWithLimit(remainingBytes); err != nil {
			return classifyPDFLazyObjectError(ctx, err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		decodedBytes := int64(len(objectStream.Content))
		if decodedBytes > remainingBytes {
			return admissionLimitError("PDF object stream decode budget")
		}
		remainingBytes -= decodedBytes
	}
	return nil
}

func inspectPDFObjects(ctx context.Context, pdfCtx *model.Context, limits AdmissionLimits) error {
	if ctx == nil || pdfCtx == nil || pdfCtx.XRefTable == nil || pdfCtx.Table == nil {
		return admissionRejectedError("missing PDF object table")
	}

	objectNumbers := make([]int, 0, minInt(len(pdfCtx.Table), limits.MaxPDFObjects+1))
	for objectNumber, entry := range pdfCtx.Table {
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry == nil {
			return admissionRejectedError("invalid PDF object table")
		}
		if entry.Free {
			continue
		}
		if len(objectNumbers) == limits.MaxPDFObjects {
			return admissionLimitError("PDF objects")
		}
		objectNumbers = append(objectNumbers, objectNumber)
	}
	if len(objectNumbers) == 0 {
		return admissionRejectedError("PDF has no objects")
	}
	sort.Ints(objectNumbers)
	for _, objectNumber := range objectNumbers {
		if err := ctx.Err(); err != nil {
			return err
		}
		entry := pdfCtx.Table[objectNumber]
		decoded, err := decodePDFLazyObject(ctx, entry.Object)
		if err != nil {
			return classifyPDFLazyObjectError(ctx, err)
		}
		entry.Object = decoded
	}

	maxInt := int(^uint(0) >> 1)
	walkBudget := maxInt
	if limits.MaxPDFObjects <= maxInt/8 {
		walkBudget = limits.MaxPDFObjects * 8
	}
	for _, objectNumber := range objectNumbers {
		if err := ctx.Err(); err != nil {
			return err
		}
		entry := pdfCtx.Table[objectNumber]
		if entry.Object == nil {
			return admissionRejectedError("missing PDF object")
		}
		if err := inspectPDFObject(ctx, pdfCtx.XRefTable, entry.Object, 0, &walkBudget, false); err != nil {
			return err
		}
	}
	return nil
}

func dereferencePDFObject(ctx context.Context, xRefTable *model.XRefTable, object types.Object) (types.Object, error) {
	if ctx == nil {
		return nil, ErrInvalidAdmissionRequest
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if xRefTable == nil {
		return nil, admissionRejectedError("missing PDF object table")
	}

	var indRef *types.IndirectRef
	switch value := object.(type) {
	case types.IndirectRef:
		indRef = &value
	case *types.IndirectRef:
		indRef = value
	default:
		return object, nil
	}
	if indRef == nil {
		return nil, admissionRejectedError("missing PDF indirect reference")
	}
	entry, found := xRefTable.FindTableEntryForIndRef(indRef)
	if !found || entry == nil || entry.Free {
		return nil, nil
	}
	xRefTable.CurObj = int(indRef.ObjectNumber)
	decoded, err := decodePDFLazyObject(ctx, entry.Object)
	if err != nil {
		return nil, err
	}
	entry.Object = decoded
	return decoded, nil
}

func decodePDFLazyObject(ctx context.Context, object types.Object) (types.Object, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch value := object.(type) {
	case types.LazyObjectStreamObject:
		return value.DecodedObject(ctx)
	case *types.LazyObjectStreamObject:
		if value == nil {
			return nil, admissionRejectedError("missing PDF object stream object")
		}
		return value.DecodedObject(ctx)
	default:
		return object, nil
	}
}

func classifyPDFLazyObjectError(ctx context.Context, err error) error {
	if contextErr := ctx.Err(); contextErr != nil {
		return contextErr
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if isPDFParserLimitError(err) {
		return admissionLimitError("PDF object stream resources")
	}
	return admissionRejectedError("malformed PDF object stream")
}

func inspectPDFObject(ctx context.Context, xRefTable *model.XRefTable, object types.Object, depth int, budget *int, actionTarget bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if object == nil {
		return admissionRejectedError("missing PDF object")
	}
	if depth > pdfMaxObjectDepth || *budget <= 0 {
		return admissionLimitError("PDF object graph")
	}
	*budget--

	switch value := object.(type) {
	case types.Name:
		return inspectPDFName(string(value))
	case types.Dict:
		return inspectPDFDict(ctx, xRefTable, value, depth, budget, actionTarget)
	case types.StreamDict:
		return inspectPDFDict(ctx, xRefTable, value.Dict, depth, budget, actionTarget)
	case types.ObjectStreamDict:
		return inspectPDFDict(ctx, xRefTable, value.Dict, depth, budget, actionTarget)
	case types.XRefStreamDict:
		return inspectPDFDict(ctx, xRefTable, value.Dict, depth, budget, actionTarget)
	case types.IndirectRef:
		if !actionTarget {
			return nil
		}
		resolved, err := dereferencePDFObject(ctx, xRefTable, value)
		if err != nil {
			if contextErr := ctx.Err(); contextErr != nil {
				return contextErr
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			if isPDFParserLimitError(err) {
				return admissionLimitError("PDF action target resources")
			}
			return admissionRejectedError("malformed PDF action target")
		}
		return inspectPDFObject(ctx, xRefTable, resolved, depth+1, budget, true)
	case types.LazyObjectStreamObject:
		decoded, err := decodePDFLazyObject(ctx, value)
		if err != nil {
			return classifyPDFLazyObjectError(ctx, err)
		}
		return inspectPDFObject(ctx, xRefTable, decoded, depth+1, budget, actionTarget)
	case *types.LazyObjectStreamObject:
		if value == nil {
			return admissionRejectedError("missing PDF object stream object")
		}
		decoded, err := decodePDFLazyObject(ctx, value)
		if err != nil {
			return classifyPDFLazyObjectError(ctx, err)
		}
		return inspectPDFObject(ctx, xRefTable, decoded, depth+1, budget, actionTarget)
	case types.Array:
		for _, item := range value {
			if err := ctx.Err(); err != nil {
				return err
			}
			if _, indirect := item.(types.IndirectRef); indirect {
				continue
			}
			if err := inspectPDFObject(ctx, xRefTable, item, depth+1, budget, actionTarget); err != nil {
				return err
			}
		}
	}
	return nil
}

func inspectPDFDict(ctx context.Context, xRefTable *model.XRefTable, dict types.Dict, depth int, budget *int, actionTarget bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	structureElement, err := isPDFStructureElementDict(ctx, xRefTable, dict)
	if err != nil {
		return err
	}
	if subtype, ok := dict["S"]; ok {
		if actionTarget || !structureElement {
			resolved, err := dereferencePDFObject(ctx, xRefTable, subtype)
			if err != nil {
				if contextErr := ctx.Err(); contextErr != nil {
					return contextErr
				}
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return err
				}
				if isPDFParserLimitError(err) {
					return admissionLimitError("PDF action subtype resources")
				}
				return admissionRejectedError("malformed PDF action subtype")
			}
			name, ok := resolved.(types.Name)
			if ok {
				decoded, err := types.DecodeName(string(name))
				if err != nil {
					return admissionRejectedError("malformed PDF action subtype")
				}
				if isPDFActionSubtype(decoded) {
					return admissionRejectedError("active PDF action")
				}
			}
		}
	}
	keys := make([]string, 0, len(dict))
	for key := range dict {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := inspectPDFName(key); err != nil {
			return err
		}
	}
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return err
		}
		value := dict[key]
		if key == "A" {
			if err := inspectPDFObject(ctx, xRefTable, value, depth+1, budget, !structureElement); err != nil {
				return err
			}
			continue
		}
		if _, indirect := value.(types.IndirectRef); indirect {
			continue
		}
		if err := inspectPDFObject(ctx, xRefTable, value, depth+1, budget, false); err != nil {
			return err
		}
	}
	return nil
}

func isPDFStructureElementDict(ctx context.Context, xRefTable *model.XRefTable, dict types.Dict) (bool, error) {
	typeObject, ok := dict["Type"]
	if !ok {
		return false, nil
	}
	resolved, err := dereferencePDFObject(ctx, xRefTable, typeObject)
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return false, contextErr
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return false, err
		}
		if isPDFParserLimitError(err) {
			return false, admissionLimitError("PDF dictionary type resources")
		}
		return false, admissionRejectedError("malformed PDF dictionary type")
	}
	typeName, ok := resolved.(types.Name)
	if !ok {
		return false, admissionRejectedError("malformed PDF dictionary type")
	}
	decoded, err := types.DecodeName(string(typeName))
	if err != nil {
		return false, admissionRejectedError("malformed PDF dictionary type")
	}
	if decoded != "StructElem" {
		return false, nil
	}
	parentObject, ok := dict["P"]
	if !ok {
		return false, nil
	}
	parent, err := dereferencePDFObject(ctx, xRefTable, parentObject)
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return false, contextErr
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return false, err
		}
		if isPDFParserLimitError(err) {
			return false, admissionLimitError("PDF structure parent resources")
		}
		return false, admissionRejectedError("malformed PDF structure parent")
	}
	parentDict, ok := parent.(types.Dict)
	if !ok {
		return false, nil
	}
	parentType, ok := parentDict["Type"]
	if !ok {
		return false, nil
	}
	parentName, ok := parentType.(types.Name)
	if !ok {
		return false, admissionRejectedError("malformed PDF structure parent")
	}
	parentDecoded, err := types.DecodeName(string(parentName))
	if err != nil {
		return false, admissionRejectedError("malformed PDF structure parent")
	}
	return parentDecoded == "StructTreeRoot" || parentDecoded == "StructElem", nil
}

func isPDFActionSubtype(name string) bool {
	switch name {
	case "GoTo", "GoToR", "GoToE", "Launch", "Thread", "URI", "Sound", "Movie", "Hide", "Named",
		"SubmitForm", "ResetForm", "ImportData", "JavaScript", "SetOCGState", "Rendition", "Trans", "GoTo3DView":
		return true
	default:
		return false
	}
}

func inspectPDFName(name string) error {
	decoded, err := types.DecodeName(name)
	if err != nil {
		return admissionRejectedError("malformed PDF name")
	}
	switch decoded {
	case "JavaScript", "JS", "OpenAction", "Launch", "EmbeddedFile", "EmbeddedFiles", "RichMedia", "XFA", "AA":
		return admissionRejectedError("active PDF")
	default:
		return nil
	}
}

func isPDFParserLimitError(err error) bool {
	return errors.Is(err, filter.ErrDecodeLimitExceeded) ||
		errors.Is(err, model.ErrMaxRecursionDepthExceeded) ||
		strings.Contains(err.Error(), " exceeds limit ")
}

func hasForbiddenBinarySignature(content []byte) bool {
	return bytes.HasPrefix(content, []byte{0x7f, 'E', 'L', 'F'}) ||
		bytes.HasPrefix(content, []byte{'M', 'Z'}) ||
		bytes.HasPrefix(content, []byte{0x00, 'a', 's', 'm'}) ||
		bytes.HasPrefix(content, []byte{0xca, 0xfe, 0xba, 0xbe}) ||
		bytes.HasPrefix(content, []byte{0xd0, 0xcf, 0x11, 0xe0}) ||
		bytes.HasPrefix(content, []byte{'Q', 'F', 'I', 0xfb}) ||
		bytes.HasPrefix(content, []byte("KDMV")) || bytes.HasPrefix(content, []byte("vhdxfile")) ||
		bytes.HasPrefix(content, []byte("dex\n")) ||
		hasMachOSignature(content) || hasDiskImageSignature(content)
}

func hasMachOSignature(content []byte) bool {
	if len(content) < 4 {
		return false
	}
	magic := binary.BigEndian.Uint32(content[:4])
	return magic == 0xfeedface || magic == 0xfeedfacf || magic == 0xcefaedfe ||
		magic == 0xcffaedfe || magic == 0xcafebabe || magic == 0xbebafeca
}

func hasDiskImageSignature(content []byte) bool {
	if len(content) >= 32_774 && bytes.Equal(content[32_769:32_774], []byte("CD001")) {
		return true
	}
	if len(content) >= 512 {
		footer := content[len(content)-512:]
		if bytes.HasPrefix(footer, []byte("conectix")) || bytes.HasPrefix(footer, []byte("koly")) {
			return true
		}
	}
	return len(content) >= 4 && bytes.Equal(content[len(content)-4:], []byte("koly"))
}

func looksLikeActiveText(ctx context.Context, content []byte) (bool, error) {
	detector := activeTextDetector{}
	if err := detector.observe(ctx, content); err != nil {
		return false, err
	}
	return detector.active, nil
}

var (
	leadingActiveTextSignatures = [][]byte{[]byte("#!"), []byte("@echo off")}
	incompleteActiveMarkup      = [][]byte{[]byte("<!doctype html"), []byte("<html"), []byte("<script"), []byte("<svg"), []byte("<?xml"), []byte("<?php")}
)

const maxActiveTextTagBytes = 4 * 1024

const activeTextContextCheckBytes = 4 * 1024

type activeTextDetector struct {
	active           bool
	initialized      bool
	bom              []byte
	bomChecked       bool
	leading          bool
	leadingCandidate []byte
	tagCandidate     []byte
	incompleteTag    bool
	tokenizerCalls   int
}

func (detector *activeTextDetector) observe(ctx context.Context, content []byte) error {
	if ctx == nil {
		return ErrInvalidAdmissionRequest
	}
	if !detector.initialized {
		detector.initialized = true
		detector.leading = true
	}
	for index, value := range content {
		if index%activeTextContextCheckBytes == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		if detector.active {
			return nil
		}
		if !detector.bomChecked {
			detector.bom = append(detector.bom, value)
			if bytes.HasPrefix([]byte{0xef, 0xbb, 0xbf}, detector.bom) && len(detector.bom) < 3 {
				continue
			}
			detector.bomChecked = true
			if bytes.Equal(detector.bom, []byte{0xef, 0xbb, 0xbf}) {
				detector.bom = nil
				continue
			}
			for _, initial := range detector.bom {
				if err := detector.observeByte(ctx, initial); err != nil {
					return err
				}
			}
			detector.bom = nil
			continue
		}
		if err := detector.observeByte(ctx, value); err != nil {
			return err
		}
	}
	return nil
}

func (detector *activeTextDetector) observeByte(ctx context.Context, value byte) error {
	lower := lowerASCII(value)
	detector.observeLeadingSignature(lower, value)
	if detector.active {
		return nil
	}
	return detector.observeTag(ctx, value, lower)
}

func (detector *activeTextDetector) observeLeadingSignature(lower, original byte) {
	if !detector.leading {
		return
	}
	if len(detector.leadingCandidate) == 0 && isActiveTextWhitespace(original) {
		return
	}
	detector.leadingCandidate = append(detector.leadingCandidate, lower)
	for _, signature := range leadingActiveTextSignatures {
		if bytes.Equal(detector.leadingCandidate, signature) {
			detector.active = true
			return
		}
	}
	for _, signature := range leadingActiveTextSignatures {
		if bytes.HasPrefix(signature, detector.leadingCandidate) {
			return
		}
	}
	detector.leading = false
	detector.leadingCandidate = nil
}

func (detector *activeTextDetector) observeTag(ctx context.Context, original, lower byte) error {
	if len(detector.tagCandidate) == 0 {
		if original != '<' {
			return nil
		}
		detector.tagCandidate = append(detector.tagCandidate, lower)
		return nil
	}
	if len(detector.tagCandidate) >= maxActiveTextTagBytes {
		detector.active = true
		return nil
	}
	detector.tagCandidate = append(detector.tagCandidate, lower)
	if len(detector.tagCandidate) == 2 && isActiveTextWhitespace(original) {
		detector.resetTag()
		return nil
	}
	for _, marker := range incompleteActiveMarkup {
		if bytes.Equal(detector.tagCandidate, marker) {
			detector.active = true
			return nil
		}
	}
	if original == '>' {
		if detector.incompleteTag {
			detector.active = true
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		detector.tokenizerCalls++
		active, complete := activeMarkupCandidate(detector.tagCandidate)
		if active {
			detector.active = true
			return nil
		}
		if complete {
			detector.resetTag()
		} else {
			detector.incompleteTag = true
		}
		return nil
	}
	if len(detector.tagCandidate) == 2 && original != '/' && original != '!' && original != '?' && !isASCIIAlpha(original) {
		detector.resetTag()
	}
	return nil
}

func (detector *activeTextDetector) resetTag() {
	detector.tagCandidate = detector.tagCandidate[:0]
	detector.incompleteTag = false
}

func activeMarkupCandidate(candidate []byte) (active, complete bool) {
	tokenizer := html.NewTokenizer(bytes.NewReader(candidate))
	tokenType := tokenizer.Next()
	if tokenType == html.ErrorToken {
		if errors.Is(tokenizer.Err(), io.EOF) {
			return false, false
		}
		return true, false
	}
	if uriShaped, allowed := classifyMarkdownAutolink(candidate); uriShaped {
		return !allowed, true
	}
	switch tokenType {
	case html.StartTagToken, html.SelfClosingTagToken:
		name, hasAttr := tokenizer.TagName()
		if len(name) == 0 {
			return false, true
		}
		knownTag := atom.Lookup(name) != 0
		activeAttribute := false
		for hasAttr {
			key, _, moreAttr := tokenizer.TagAttr()
			if isActiveHTMLAttribute(key) {
				activeAttribute = true
			}
			hasAttr = moreAttr
		}
		return knownTag || activeAttribute || bytes.Contains(name, []byte{'-'}), true
	case html.DoctypeToken:
		return true, true
	default:
		return false, true
	}
}

func classifyMarkdownAutolink(candidate []byte) (uriShaped, allowed bool) {
	if len(candidate) < 3 || candidate[0] != '<' || candidate[len(candidate)-1] != '>' {
		return false, false
	}
	rawURI := string(candidate[1 : len(candidate)-1])
	scheme, uriShaped := markdownURIScheme(rawURI)
	if !uriShaped {
		return false, false
	}
	if strings.ContainsAny(rawURI, " \t\r\n\v\f") {
		return true, false
	}
	parsed, err := url.ParseRequestURI(rawURI)
	if err != nil || parsed.Scheme != scheme || (scheme != "http" && scheme != "https") || parsed.Host == "" {
		return true, false
	}
	return true, true
}

func markdownURIScheme(value string) (string, bool) {
	if len(value) < 2 || !isASCIIAlpha(value[0]) {
		return "", false
	}
	for index := 1; index < len(value); index++ {
		switch value[index] {
		case ':':
			return value[:index], true
		case '+', '-', '.':
			continue
		default:
			if value[index] >= 'a' && value[index] <= 'z' || value[index] >= 'A' && value[index] <= 'Z' || value[index] >= '0' && value[index] <= '9' {
				continue
			}
			return "", false
		}
	}
	return "", false
}

func isActiveHTMLAttribute(key []byte) bool {
	lower := bytes.ToLower(key)
	return bytes.HasPrefix(lower, []byte("on")) || bytes.Equal(lower, []byte("action")) ||
		bytes.Equal(lower, []byte("formaction")) || bytes.Equal(lower, []byte("href")) ||
		bytes.Equal(lower, []byte("src")) || bytes.Equal(lower, []byte("style")) || bytes.Equal(lower, []byte("xlink:href"))
}

func isASCIIAlpha(value byte) bool {
	return value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}

func lowerASCII(value byte) byte {
	if value >= 'A' && value <= 'Z' {
		return value + ('a' - 'A')
	}
	return value
}

func isActiveTextWhitespace(value byte) bool {
	switch value {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	default:
		return false
	}
}

func admissionRejectedError(reason string) error {
	return fmt.Errorf("%w: %s", ErrAdmissionRejected, reason)
}

func admissionLimitError(limit string) error {
	return fmt.Errorf("%w: %s", ErrAdmissionLimitExceeded, limit)
}

func isNilAdmissionReader(reader io.Reader) bool {
	if reader == nil {
		return true
	}
	value := reflect.ValueOf(reader)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
