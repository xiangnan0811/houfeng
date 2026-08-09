package attachments

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func TestPDFConfigurationIsStrictOfflineAndBounded(t *testing.T) {
	limits := DefaultAdmissionLimits(DefaultLimits())
	configuration, err := newPDFConfiguration(limits)
	if err != nil {
		t.Fatalf("newPDFConfiguration() error = %v", err)
	}
	if model.ConfigPath != "disable" {
		t.Fatalf("model.ConfigPath = %q, want disable", model.ConfigPath)
	}
	if configuration.ValidationMode != model.ValidationStrict || !configuration.Reader15 {
		t.Fatalf("newPDFConfiguration() validation = %d, reader15 = %t", configuration.ValidationMode, configuration.Reader15)
	}
	if !configuration.Offline || configuration.ValidateLinks || configuration.DecodeAllStreams || configuration.Optimize {
		t.Fatalf("newPDFConfiguration() external/decoding options = %#v", configuration)
	}
	if configuration.Cmd != model.VALIDATE {
		t.Fatalf("newPDFConfiguration().Cmd = %v, want VALIDATE", configuration.Cmd)
	}
	wantLimits := model.ResourceLimits{
		MaxStreamBytes:       limits.MaxPDFBytes,
		MaxDecodeBytes:       limits.MaxPDFBytes,
		MaxImagePixels:       limits.MaxImagePixels,
		MaxImageBytes:        limits.MaxImagePixels * 4,
		MaxObjectCount:       limits.MaxPDFObjects*2 + 1,
		MaxObjectStreamCount: maxPDFObjectsPerObjectStream,
		MaxObjectStreamFirst: pdfObjectStreamProbeBytes(limits),
		MaxXRefEntries:       limits.MaxPDFObjects*2 + 1,
		MaxRecursionDepth:    32,
	}
	if configuration.Limits != wantLimits {
		t.Fatalf("newPDFConfiguration().Limits = %#v, want %#v", configuration.Limits, wantLimits)
	}
}

func TestAdmissionLimitsDefaultReusesFileLimitAndValidates(t *testing.T) {
	t.Parallel()

	base := DefaultLimits()
	limits := DefaultAdmissionLimits(base)
	_ = limits.Archive
	if limits.MaxReadBytes != base.MaxFileBytes {
		t.Fatalf("DefaultAdmissionLimits().MaxReadBytes = %d, want %d", limits.MaxReadBytes, base.MaxFileBytes)
	}
	if limits.MaxPDFBytes != base.MaxFileBytes {
		t.Fatalf("DefaultAdmissionLimits().MaxPDFBytes = %d, want %d", limits.MaxPDFBytes, base.MaxFileBytes)
	}
	if limits.MaxImageWidth <= 0 || limits.MaxImageHeight <= 0 || limits.MaxImagePixels <= 0 ||
		limits.MaxPDFBytes <= 0 || limits.MaxPDFDecodedObjectStreamBytes <= 0 || limits.MaxPDFObjects <= 0 || limits.MaxPDFPages <= 0 {
		t.Fatalf("DefaultAdmissionLimits() has non-positive budget: %#v", limits)
	}
	if err := limits.Validate(); err != nil {
		t.Fatalf("DefaultAdmissionLimits().Validate() error = %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*AdmissionLimits)
	}{
		{name: "read bytes", mutate: func(value *AdmissionLimits) { value.MaxReadBytes = 0 }},
		{name: "image width", mutate: func(value *AdmissionLimits) { value.MaxImageWidth = 0 }},
		{name: "image height", mutate: func(value *AdmissionLimits) { value.MaxImageHeight = 0 }},
		{name: "image pixels", mutate: func(value *AdmissionLimits) { value.MaxImagePixels = 0 }},
		{name: "image byte budget overflow", mutate: func(value *AdmissionLimits) { value.MaxImagePixels = math.MaxInt64/4 + 1 }},
		{name: "PDF bytes", mutate: func(value *AdmissionLimits) { value.MaxPDFBytes = 0 }},
		{name: "PDF decoded object streams", mutate: func(value *AdmissionLimits) { value.MaxPDFDecodedObjectStreamBytes = 0 }},
		{name: "PDF objects", mutate: func(value *AdmissionLimits) { value.MaxPDFObjects = 0 }},
		{name: "PDF parser object budget overflow", mutate: func(value *AdmissionLimits) { value.MaxPDFObjects = int(^uint(0) >> 1) }},
		{name: "PDF pages", mutate: func(value *AdmissionLimits) { value.MaxPDFPages = 0 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			invalid := DefaultAdmissionLimits(base)
			tt.mutate(&invalid)
			if err := invalid.Validate(); !errors.Is(err, ErrInvalidAdmissionRequest) {
				t.Fatalf("AdmissionLimits.Validate() error = %v, want ErrInvalidAdmissionRequest", err)
			}
		})
	}
}

func TestPDFInspectionConfigurationBoundsAggregateObjectStreamDecode(t *testing.T) {
	t.Parallel()

	limits := DefaultAdmissionLimits(DefaultLimits())
	limits.MaxReadBytes = 12
	limits.MaxPDFBytes = 12
	limits.MaxPDFObjects = 3
	limits.MaxPDFDecodedObjectStreamBytes = 12
	probeConfiguration, err := newPDFConfiguration(limits)
	if err != nil {
		t.Fatalf("newPDFConfiguration() error = %v", err)
	}
	if probeConfiguration.Limits.MaxObjectStreamFirst != 500 {
		t.Fatalf("probe MaxObjectStreamFirst = %d, want 500", probeConfiguration.Limits.MaxObjectStreamFirst)
	}

	pdfContext := &model.Context{XRefTable: &model.XRefTable{Table: map[int]*model.XRefTableEntry{
		1: model.NewXRefTableEntryGen0(types.ObjectStreamDict{StreamDict: types.StreamDict{Raw: []byte("x")}}),
		2: model.NewXRefTableEntryGen0(types.ObjectStreamDict{StreamDict: types.StreamDict{Raw: []byte("x")}}),
		3: model.NewXRefTableEntryGen0(types.ObjectStreamDict{StreamDict: types.StreamDict{Raw: []byte("x")}}),
	}}}
	count, err := countPDFObjectStreams(context.Background(), pdfContext, limits)
	if err != nil {
		t.Fatalf("countPDFObjectStreams() error = %v", err)
	}
	inspectionConfiguration, err := newPDFInspectionConfiguration(limits, count)
	if err != nil {
		t.Fatalf("newPDFInspectionConfiguration() error = %v", err)
	}
	if inspectionConfiguration.Limits.MaxDecodeBytes != limits.MaxPDFDecodedObjectStreamBytes {
		t.Fatalf("inspection MaxDecodeBytes = %d, want aggregate budget %d", inspectionConfiguration.Limits.MaxDecodeBytes, limits.MaxPDFDecodedObjectStreamBytes)
	}
	starvedLimits := limits
	starvedLimits.MaxPDFDecodedObjectStreamBytes = int64(count - 1)
	if err := validatePDFObjectStreamDecodeBudget(context.Background(), pdfContext, starvedLimits); !errors.Is(err, ErrAdmissionLimitExceeded) {
		t.Fatalf("validatePDFObjectStreamDecodeBudget() error = %v, want ErrAdmissionLimitExceeded for a starved aggregate budget", err)
	}

	pdfContext.XRefTable.Table[4] = model.NewXRefTableEntryGen0(types.ObjectStreamDict{})
	if _, err := countPDFObjectStreams(context.Background(), pdfContext, limits); !errors.Is(err, ErrAdmissionLimitExceeded) {
		t.Fatalf("countPDFObjectStreams() error = %v, want ErrAdmissionLimitExceeded before a fourth stream decodes", err)
	}
}

func TestAdmitContentAcceptsPDFObjectStreamsWithinAggregateDecodeLimit(t *testing.T) {
	t.Parallel()

	content := admissionPDFWithMultipleObjectStreams(t)
	limits := DefaultAdmissionLimits(DefaultLimits())
	configuration, err := newPDFConfiguration(limits)
	if err != nil {
		t.Fatalf("newPDFConfiguration() error = %v", err)
	}
	pdfContext, err := pdfcpu.ReadWithContext(context.Background(), bytes.NewReader(content), configuration)
	if err != nil {
		t.Fatalf("probe pdfcpu.ReadWithContext() error = %v", err)
	}
	decodedSizes := decodedPDFObjectStreamSizes(t, pdfContext, limits.MaxPDFBytes)
	if len(decodedSizes) != 3 {
		t.Fatalf("decoded object stream count = %d, want 3", len(decodedSizes))
	}
	var totalDecodedBytes, largestDecodedStream int64
	for _, size := range decodedSizes {
		totalDecodedBytes += size
		largestDecodedStream = max(largestDecodedStream, size)
	}
	if largestDecodedStream <= totalDecodedBytes/int64(len(decodedSizes)) {
		t.Fatalf("fixture largest stream = %d, want greater than equal-share budget %d", largestDecodedStream, totalDecodedBytes/int64(len(decodedSizes)))
	}
	limits.MaxPDFDecodedObjectStreamBytes = totalDecodedBytes

	result, err := AdmitContent(
		context.Background(),
		admissionRequest("report.pdf", "application/pdf", content),
		limits,
	)
	if err != nil {
		t.Fatalf("AdmitContent() error = %v", err)
	}
	if result.MediaType != "application/pdf" || result.Profile != ProcessorProfilePDF {
		t.Fatalf("AdmitContent() = %#v, want PDF profile", result)
	}
}

func TestPDFProbeAcceptsOptimizedMultipleHundredObjectStreams(t *testing.T) {
	t.Parallel()

	content := admissionPDFWithMultipleObjectStreams(t)
	limits := DefaultAdmissionLimits(DefaultLimits())
	configuration, err := newPDFConfiguration(limits)
	if err != nil {
		t.Fatalf("newPDFConfiguration() error = %v", err)
	}
	pdfContext, err := pdfcpu.ReadWithContext(context.Background(), bytes.NewReader(content), configuration)
	if err != nil {
		t.Fatalf("probe pdfcpu.ReadWithContext() error = %v", err)
	}
	count, err := countPDFObjectStreams(context.Background(), pdfContext, limits)
	if err != nil {
		t.Fatalf("countPDFObjectStreams() error = %v", err)
	}
	if count != 3 {
		t.Fatalf("countPDFObjectStreams() = %d, want 3 optimized object streams", count)
	}
	result, err := AdmitContent(
		context.Background(),
		admissionRequest("report.pdf", "application/pdf", content),
		limits,
	)
	if err != nil {
		t.Fatalf("AdmitContent() error = %v", err)
	}
	if result.Profile != ProcessorProfilePDF {
		t.Fatalf("AdmitContent() = %#v, want PDF profile", result)
	}
}

func TestAdmitContentRejectsAggregatePDFObjectStreamDecodeLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content func(testing.TB) []byte
	}{
		{name: "Flate object streams", content: admissionPDFWithMultipleObjectStreams},
		{name: "unfiltered object streams", content: admissionPDFWithUnfilteredObjectStreams},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			content := tt.content(t)
			limits := DefaultAdmissionLimits(DefaultLimits())
			configuration, err := newPDFConfiguration(limits)
			if err != nil {
				t.Fatalf("newPDFConfiguration() error = %v", err)
			}
			pdfContext, err := pdfcpu.ReadWithContext(context.Background(), bytes.NewReader(content), configuration)
			if err != nil {
				t.Fatalf("probe pdfcpu.ReadWithContext() error = %v", err)
			}
			decodedSizes := decodedPDFObjectStreamSizes(t, pdfContext, limits.MaxPDFBytes)
			if len(decodedSizes) != 3 {
				t.Fatalf("decoded object stream count = %d, want 3", len(decodedSizes))
			}
			var totalDecodedBytes, largestDecodedStream int64
			for _, size := range decodedSizes {
				if size <= 0 {
					t.Fatalf("decoded object stream size = %d, want positive", size)
				}
				totalDecodedBytes += size
				largestDecodedStream = max(largestDecodedStream, size)
			}
			aggregateBudget := totalDecodedBytes - 1
			if aggregateBudget <= largestDecodedStream {
				t.Fatalf("fixture aggregate budget = %d, want greater than largest stream %d", aggregateBudget, largestDecodedStream)
			}
			limits.MaxPDFDecodedObjectStreamBytes = aggregateBudget

			legacyConfiguration, err := newPDFConfiguration(limits)
			if err != nil {
				t.Fatalf("legacy newPDFConfiguration() error = %v", err)
			}
			legacyConfiguration.Limits.MaxDecodeBytes = aggregateBudget
			legacyContext, err := pdfcpu.ReadWithContext(context.Background(), bytes.NewReader(content), legacyConfiguration)
			if err != nil {
				t.Fatalf("legacy per-stream pdfcpu.ReadWithContext() error = %v", err)
			}
			if err := inspectPDFObjects(context.Background(), legacyContext, limits); err != nil {
				t.Fatalf("legacy per-stream inspectPDFObjects() error = %v", err)
			}
			if err := validatePDFContext(legacyContext); err != nil {
				t.Fatalf("legacy per-stream validatePDFContext() error = %v", err)
			}

			_, err = AdmitContent(
				context.Background(),
				admissionRequest("report.pdf", "application/pdf", content),
				limits,
			)
			if !errors.Is(err, ErrAdmissionLimitExceeded) {
				t.Fatalf("AdmitContent() error = %v, want ErrAdmissionLimitExceeded", err)
			}
		})
	}
}

func TestAdmitContentParsesPDFStructure(t *testing.T) {
	t.Parallel()

	t.Run("rejects corrupt xref entry", func(t *testing.T) {
		t.Parallel()
		content := admissionPDFWithCorruptFirstXRefEntry(t, admissionPDF(1, 0))
		_, err := AdmitContent(context.Background(), admissionRequest("report.pdf", "application/pdf", content), DefaultAdmissionLimits(DefaultLimits()))
		if !errors.Is(err, ErrAdmissionRejected) {
			t.Fatalf("AdmitContent() error = %v, want ErrAdmissionRejected", err)
		}
	})

	t.Run("accepts valid xref stream", func(t *testing.T) {
		t.Parallel()
		content := admissionPDFXRefStreamGolden(t)
		result, err := AdmitContent(context.Background(), admissionRequest("report.pdf", "application/pdf", content), DefaultAdmissionLimits(DefaultLimits()))
		if err != nil {
			t.Fatalf("AdmitContent() error = %v", err)
		}
		if result.MediaType != "application/pdf" || result.Profile != ProcessorProfilePDF {
			t.Fatalf("AdmitContent() = %#v, want PDF profile", result)
		}
	})
}

func TestAdmitContentRejectsEscapedPDFActiveNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		catalogEntry string
	}{
		{name: "dictionary key", catalogEntry: "/Java#53cript true"},
		{name: "name value", catalogEntry: "/Fixture /Java#53cript"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			content := admissionPDFWithCatalogEntry(1, 0, tt.catalogEntry)
			_, err := AdmitContent(context.Background(), admissionRequest("report.pdf", "application/pdf", content), DefaultAdmissionLimits(DefaultLimits()))
			if !errors.Is(err, ErrAdmissionRejected) {
				t.Fatalf("AdmitContent() error = %v, want ErrAdmissionRejected", err)
			}
		})
	}
}

func TestAdmitContentRejectsPDFActiveNamesAndEncryption(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"JavaScript", "JS", "OpenAction", "Launch", "EmbeddedFile", "EmbeddedFiles", "RichMedia", "XFA", "AA",
	} {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			content := admissionPDFWithCatalogEntry(1, 0, "/"+name+" true")
			_, err := AdmitContent(context.Background(), admissionRequest("report.pdf", "application/pdf", content), DefaultAdmissionLimits(DefaultLimits()))
			if !errors.Is(err, ErrAdmissionRejected) {
				t.Fatalf("AdmitContent() error = %v, want ErrAdmissionRejected", err)
			}
		})
	}

	t.Run("encrypted", func(t *testing.T) {
		t.Parallel()
		content := admissionEncryptedPDF(t)
		_, err := AdmitContent(context.Background(), admissionRequest("report.pdf", "application/pdf", content), DefaultAdmissionLimits(DefaultLimits()))
		if !errors.Is(err, ErrAdmissionRejected) {
			t.Fatalf("AdmitContent() error = %v, want ErrAdmissionRejected", err)
		}
	})
}

func TestAdmitContentRejectsDirectPDFActionDictionary(t *testing.T) {
	t.Parallel()

	content := admissionPDFWithAdditionalObjects(
		1,
		"<< /S /URI /URI (https://example.invalid) >>",
	)
	_, err := AdmitContent(
		context.Background(),
		admissionRequest("report.pdf", "application/pdf", content),
		DefaultAdmissionLimits(DefaultLimits()),
	)
	if !errors.Is(err, ErrAdmissionRejected) {
		t.Fatalf("AdmitContent() error = %v, want ErrAdmissionRejected", err)
	}
}

func TestAdmitContentRejectsPDFActionSubtypes(t *testing.T) {
	t.Parallel()

	for _, subtype := range []string{
		"GoTo", "GoToR", "GoTo#52", "GoToE", "Launch", "Thread", "URI", "UR#49", "Sound",
		"Movie", "Hide", "Named", "SubmitForm", "ResetForm", "ImportData", "JavaScript",
		"SetOCGState", "Rendition", "Trans", "GoTo3DView",
	} {
		subtype := subtype
		t.Run(subtype, func(t *testing.T) {
			t.Parallel()
			content := admissionPDFWithAdditionalObjects(1, "<< /S /"+subtype+" >>")
			_, err := AdmitContent(
				context.Background(),
				admissionRequest("report.pdf", "application/pdf", content),
				DefaultAdmissionLimits(DefaultLimits()),
			)
			if !errors.Is(err, ErrAdmissionRejected) {
				t.Fatalf("AdmitContent() error = %v, want ErrAdmissionRejected", err)
			}
		})
	}
}

func TestAdmitContentRejectsIndirectPDFActionDictionaryAndSubtype(t *testing.T) {
	t.Parallel()

	content := admissionPDFDocument(
		1,
		"/Fixture 5 0 R",
		[]string{"<< /S 6 0 R >>", "/SubmitForm"},
	)
	_, err := AdmitContent(
		context.Background(),
		admissionRequest("report.pdf", "application/pdf", content),
		DefaultAdmissionLimits(DefaultLimits()),
	)
	if !errors.Is(err, ErrAdmissionRejected) {
		t.Fatalf("AdmitContent() error = %v, want ErrAdmissionRejected", err)
	}
}

func TestAdmitContentRejectsParseableStructElemDisguisedPDFActionTarget(t *testing.T) {
	t.Parallel()

	content := admissionPDFWithStructElemActionTarget()
	limits := DefaultAdmissionLimits(DefaultLimits())
	configuration, err := newPDFConfiguration(limits)
	if err != nil {
		t.Fatalf("newPDFConfiguration() error = %v", err)
	}
	if _, err := pdfcpu.ReadWithContext(context.Background(), bytes.NewReader(content), configuration); err != nil {
		t.Fatalf("parse disguised action fixture: %v", err)
	}
	_, err = AdmitContent(
		context.Background(),
		admissionRequest("report.pdf", "application/pdf", content),
		limits,
	)
	if !errors.Is(err, ErrAdmissionRejected) {
		t.Fatalf("AdmitContent() error = %v, want ErrAdmissionRejected", err)
	}
}

func TestAdmitContentRejectsPDFActionInLazyObjectStream(t *testing.T) {
	t.Parallel()

	content := admissionPDFWithLazyObjectStreamAction(t)
	limits := DefaultAdmissionLimits(DefaultLimits())
	configuration, err := newPDFConfiguration(limits)
	if err != nil {
		t.Fatalf("newPDFConfiguration() error = %v", err)
	}
	if err := api.Validate(bytes.NewReader(content), configuration); err != nil {
		t.Fatalf("object-stream PDF fixture strict validation error = %v", err)
	}
	configuration, err = newPDFConfiguration(limits)
	if err != nil {
		t.Fatalf("newPDFConfiguration() error = %v", err)
	}
	pdfContext, err := pdfcpu.ReadWithContext(context.Background(), bytes.NewReader(content), configuration)
	if err != nil {
		t.Fatalf("pdfcpu.ReadWithContext() error = %v", err)
	}
	if !hasLazyPDFURIAction(t, pdfContext) {
		t.Fatal("object-stream PDF fixture has no lazy URI action")
	}

	_, err = AdmitContent(
		context.Background(),
		admissionRequest("report.pdf", "application/pdf", content),
		limits,
	)
	if !errors.Is(err, ErrAdmissionRejected) {
		t.Fatalf("AdmitContent() error = %v, want ErrAdmissionRejected", err)
	}
}

func TestAdmitContentPreservesPDFCancellationDuringInspection(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := AdmitContent(
		ctx,
		admissionRequest("report.pdf", "application/pdf", admissionPDF(1, 0)),
		DefaultAdmissionLimits(DefaultLimits()),
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("AdmitContent() error = %v, want context.Canceled", err)
	}
}

func TestInspectPDFPreservesCancellationBeforeParser(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := inspectPDF(ctx, admissionPDF(1, 0), DefaultAdmissionLimits(DefaultLimits()))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("inspectPDF() error = %v, want context.Canceled", err)
	}
}

func TestDereferencePDFObjectPreservesContextDuringLazyDecode(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	objectStream := &types.ObjectStreamDict{
		StreamDict:     types.StreamDict{Raw: []byte("lazy")},
		MaxDecodeBytes: 1024,
	}
	lazy := types.NewLazyObjectStreamObject(objectStream, 0, -1, func(got context.Context, _ string) (types.Object, error) {
		if got != ctx {
			t.Fatal("lazy decode did not receive the request context")
		}
		cancel()
		return nil, ctx.Err()
	})
	xRefTable := &model.XRefTable{Table: map[int]*model.XRefTableEntry{
		7: model.NewXRefTableEntryGen0(lazy),
	}}
	ref := types.NewIndirectRef(7, 0)
	_, err := dereferencePDFObject(ctx, xRefTable, *ref)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("dereferencePDFObject() error = %v, want context.Canceled", err)
	}
}

func TestInspectPDFObjectsMaterializesLazyEntriesBeforeValidation(t *testing.T) {
	t.Parallel()

	content := admissionPDFWithLazyObjectStreamAction(t)
	limits := DefaultAdmissionLimits(DefaultLimits())
	configuration, err := newPDFConfiguration(limits)
	if err != nil {
		t.Fatalf("newPDFConfiguration() error = %v", err)
	}
	pdfContext, err := pdfcpu.ReadWithContext(context.Background(), bytes.NewReader(content), configuration)
	if err != nil {
		t.Fatalf("pdfcpu.ReadWithContext() error = %v", err)
	}
	if err := inspectPDFObjects(context.Background(), pdfContext, limits); !errors.Is(err, ErrAdmissionRejected) {
		t.Fatalf("inspectPDFObjects() error = %v, want ErrAdmissionRejected", err)
	}
	for objectNumber, entry := range pdfContext.Table {
		if entry == nil {
			continue
		}
		switch entry.Object.(type) {
		case types.LazyObjectStreamObject, *types.LazyObjectStreamObject:
			t.Fatalf("object %d remains lazy after inspection", objectNumber)
		}
	}
	if err := validatePDFContext(pdfContext); err != nil {
		t.Fatalf("validatePDFContext() after lazy materialization error = %v", err)
	}
}

func TestAdmitContentPreservesReaderCancellationAfterBoundedPDFRead(t *testing.T) {
	t.Parallel()

	content := admissionPDF(1, 0)
	_, err := AdmitContent(
		context.Background(),
		AdmissionRequest{
			DisplayName:       "report.pdf",
			DeclaredMediaType: "application/pdf",
			SizeBytes:         int64(len(content)),
			Content:           &admissionReaderErrorAfterPayload{content: content},
		},
		DefaultAdmissionLimits(DefaultLimits()),
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("AdmitContent() error = %v, want context.Canceled", err)
	}
}

func TestAdmitContentAcceptsPDFStructureElementWithActionLikeSubtype(t *testing.T) {
	t.Parallel()

	content := admissionPDFDocument(
		1,
		"/StructTreeRoot 5 0 R /MarkInfo << /Marked true >>",
		[]string{
			"<< /Type /StructTreeRoot /K 6 0 R /RoleMap << /URI /Span >> >>",
			"<< /Type /StructElem /S /URI /P 5 0 R /A << /O /Layout >> >>",
		},
	)
	limits := DefaultAdmissionLimits(DefaultLimits())
	configuration, err := newPDFConfiguration(limits)
	if err != nil {
		t.Fatalf("newPDFConfiguration() error = %v", err)
	}
	if err := api.Validate(bytes.NewReader(content), configuration); err != nil {
		t.Fatalf("tagged PDF fixture strict validation error = %v", err)
	}
	result, err := AdmitContent(
		context.Background(),
		admissionRequest("report.pdf", "application/pdf", content),
		limits,
	)
	if err != nil {
		t.Fatalf("AdmitContent() error = %v", err)
	}
	if result.MediaType != "application/pdf" || result.Profile != ProcessorProfilePDF {
		t.Fatalf("AdmitContent() = %#v, want PDF profile", result)
	}
}

func TestAdmitContentAcceptsUnrelatedPDFSubtypeName(t *testing.T) {
	t.Parallel()

	content := admissionPDFWithAdditionalObjects(1, "<< /S /Normal /Fixture true >>")
	result, err := AdmitContent(
		context.Background(),
		admissionRequest("report.pdf", "application/pdf", content),
		DefaultAdmissionLimits(DefaultLimits()),
	)
	if err != nil {
		t.Fatalf("AdmitContent() error = %v", err)
	}
	if result.MediaType != "application/pdf" || result.Profile != ProcessorProfilePDF {
		t.Fatalf("AdmitContent() = %#v, want PDF profile", result)
	}
}

func TestAdmitContentClassifiesSafeFormats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		fileName  string
		mediaType string
		content   func(testing.TB) []byte
		wantMedia string
		want      ProcessorProfile
	}{
		{name: "PNG", fileName: "screen.png", mediaType: "image/png", content: func(t testing.TB) []byte { return admissionPNG(t, 1, 1) }, wantMedia: "image/png", want: ProcessorProfileImage},
		{name: "JPEG", fileName: "screen.jpg", mediaType: "image/jpeg", content: func(t testing.TB) []byte { return admissionJPEG(t, 1, 1) }, wantMedia: "image/jpeg", want: ProcessorProfileImage},
		{name: "WebP", fileName: "screen.webp", mediaType: "image/webp", content: admissionWebP, wantMedia: "image/webp", want: ProcessorProfileImage},
		{name: "UTF-8 text", fileName: "notes.txt", mediaType: "text/plain; charset=utf-8", content: func(testing.TB) []byte { return []byte("healthy UTF-8 notes\n") }, wantMedia: "text/plain", want: ProcessorProfileText},
		{name: "PDF", fileName: "report.pdf", mediaType: "application/pdf", content: func(testing.TB) []byte { return admissionPDF(1, 0) }, wantMedia: "application/pdf", want: ProcessorProfilePDF},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			content := tt.content(t)
			result, err := AdmitContent(context.Background(), admissionRequest(tt.fileName, tt.mediaType, content), DefaultAdmissionLimits(DefaultLimits()))
			if err != nil {
				t.Fatalf("AdmitContent() error = %v", err)
			}
			if result.MediaType != tt.wantMedia || result.Profile != tt.want {
				t.Fatalf("AdmitContent() = %#v, want media %q profile %q", result, tt.wantMedia, tt.want)
			}
		})
	}
}

func TestAdmitContentAcceptsSafeUTF8DeclarationMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		fileName  string
		mediaType string
		content   string
		wantMedia string
	}{
		{name: "plain text", fileName: "notes.txt", mediaType: "text/plain; charset=utf-8", content: "plain notes\n", wantMedia: "text/plain"},
		{name: "Markdown", fileName: "notes.md", mediaType: "text/markdown", content: "# Notes\n", wantMedia: "text/markdown"},
		{name: "Markdown as plain text", fileName: "notes.markdown", mediaType: "text/plain", content: "# Notes\n", wantMedia: "text/plain"},
		{name: "log", fileName: "service.log", mediaType: "text/plain", content: "request completed\n", wantMedia: "text/plain"},
		{name: "JSON", fileName: "status.json", mediaType: "application/json", content: "{\"status\":\"ok\"}\n", wantMedia: "application/json"},
		{name: "YAML application", fileName: "status.yaml", mediaType: "application/yaml", content: "status: ok\n", wantMedia: "application/yaml"},
		{name: "YAML legacy application", fileName: "status.yml", mediaType: "application/x-yaml", content: "status: ok\n", wantMedia: "application/x-yaml"},
		{name: "YAML text", fileName: "status.yaml", mediaType: "text/yaml", content: "status: ok\n", wantMedia: "text/yaml"},
		{name: "CSV", fileName: "status.csv", mediaType: "text/csv", content: "name,status\napi,ok\n", wantMedia: "text/csv"},
		{name: "TSV", fileName: "status.tsv", mediaType: "text/tab-separated-values", content: "name\tstatus\napi\tok\n", wantMedia: "text/tab-separated-values"},
		{name: "INI", fileName: "status.ini", mediaType: "text/plain", content: "[service]\nstatus=ok\n", wantMedia: "text/plain"},
		{name: "TOML application", fileName: "status.toml", mediaType: "application/toml", content: "status = \"ok\"\n", wantMedia: "application/toml"},
		{name: "TOML as plain text", fileName: "status.toml", mediaType: "text/plain", content: "status = \"ok\"\n", wantMedia: "text/plain"},
		{name: "patch", fileName: "change.patch", mediaType: "text/x-patch", content: "--- old\n+++ new\n", wantMedia: "text/x-patch"},
		{name: "patch as diff", fileName: "change.patch", mediaType: "text/x-diff", content: "--- old\n+++ new\n", wantMedia: "text/x-diff"},
		{name: "diff", fileName: "change.diff", mediaType: "text/x-patch", content: "--- old\n+++ new\n", wantMedia: "text/x-patch"},
		{name: "diff as plain text", fileName: "change.diff", mediaType: "text/plain", content: "--- old\n+++ new\n", wantMedia: "text/plain"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := AdmitContent(context.Background(), admissionRequest(tt.fileName, tt.mediaType, []byte(tt.content)), DefaultAdmissionLimits(DefaultLimits()))
			if err != nil {
				t.Fatalf("AdmitContent() error = %v", err)
			}
			if result.MediaType != tt.wantMedia || result.Profile != ProcessorProfileText {
				t.Fatalf("AdmitContent() = %#v, want media %q text profile", result, tt.wantMedia)
			}
		})
	}
}

func TestAdmitContentRequiresDecodableCompleteWebP(t *testing.T) {
	t.Parallel()

	valid := admissionWebP(t)
	result, err := AdmitContent(context.Background(), admissionRequest("screen.webp", "image/webp", valid), DefaultAdmissionLimits(DefaultLimits()))
	if err != nil {
		t.Fatalf("AdmitContent() valid golden error = %v", err)
	}
	if result.MediaType != "image/webp" || result.Profile != ProcessorProfileImage {
		t.Fatalf("AdmitContent() valid golden = %#v, want WebP image profile", result)
	}

	tests := []struct {
		name    string
		content []byte
	}{
		{name: "header only VP8", content: admissionHeaderOnlyWebP()},
		{name: "trailing ELF chunk", content: admissionWebPWithTrailingChunk(valid, []byte{0x7f, 'E', 'L', 'F', 0x02, 0x01, 0x01})},
		{name: "trailing ZIP chunk", content: admissionWebPWithTrailingChunk(valid, []byte{'P', 'K', 0x03, 0x04, 0x14, 0x00})},
		{name: "trailing extra chunk", content: admissionWebPWithTrailingChunk(valid, []byte("unexpected trailing data"))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := AdmitContent(context.Background(), admissionRequest("screen.webp", "image/webp", tt.content), DefaultAdmissionLimits(DefaultLimits()))
			if !errors.Is(err, ErrAdmissionRejected) {
				t.Fatalf("AdmitContent() error = %v, want ErrAdmissionRejected", err)
			}
		})
	}
}

func TestAdmitContentAcceptsExtendedWebPWithDeclaredEXIF(t *testing.T) {
	t.Parallel()

	content := admissionExtendedWebP(t, 1<<3, nil, []admissionWebPChunk{
		{kind: "EXIF", payload: []byte("Exif\x00\x00fixture")},
	})
	result, err := AdmitContent(
		context.Background(),
		admissionRequest("screen.webp", "image/webp", content),
		DefaultAdmissionLimits(DefaultLimits()),
	)
	if err != nil {
		t.Fatalf("AdmitContent() error = %v", err)
	}
	if result.MediaType != "image/webp" || result.Profile != ProcessorProfileImage {
		t.Fatalf("AdmitContent() = %#v, want WebP image profile", result)
	}
}

func TestAdmitContentAcceptsSupportedWebPStillVariants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content func(testing.TB) []byte
	}{
		{name: "simple VP8L", content: admissionWebPLossless},
		{name: "declared ICCP", content: func(t testing.TB) []byte {
			return admissionExtendedWebP(t, 1<<5, []admissionWebPChunk{{kind: "ICCP", payload: []byte("profile")}}, nil)
		}},
		{name: "declared XMP", content: func(t testing.TB) []byte {
			return admissionExtendedWebP(t, 1<<2, nil, []admissionWebPChunk{{kind: "XMP ", payload: []byte("<xmp/>")}})
		}},
		{name: "declared metadata order", content: func(t testing.TB) []byte {
			return admissionExtendedWebP(
				t,
				1<<5|1<<3|1<<2,
				[]admissionWebPChunk{{kind: "ICCP", payload: []byte("profile")}},
				[]admissionWebPChunk{
					{kind: "EXIF", payload: []byte("Exif\x00\x00fixture")},
					{kind: "XMP ", payload: []byte("<xmp/>")},
				},
			)
		}},
		{name: "lossy alpha", content: func(t testing.TB) []byte {
			return admissionExtendedWebP(t, 1<<4, []admissionWebPChunk{{kind: "ALPH", payload: []byte{0, 0x80}}}, nil)
		}},
		{name: "lossy alpha with EXIF", content: func(t testing.TB) []byte {
			return admissionExtendedWebP(
				t,
				1<<4|1<<3,
				[]admissionWebPChunk{{kind: "ALPH", payload: []byte{0, 0x80}}},
				[]admissionWebPChunk{{kind: "EXIF", payload: []byte("Exif\x00\x00fixture")}},
			)
		}},
		{name: "lossy alpha with EXIF and XMP", content: func(t testing.TB) []byte {
			return admissionExtendedWebP(
				t,
				1<<4|1<<3|1<<2,
				[]admissionWebPChunk{{kind: "ALPH", payload: []byte{0, 0x80}}},
				[]admissionWebPChunk{
					{kind: "EXIF", payload: []byte("Exif\x00\x00fixture")},
					{kind: "XMP ", payload: []byte("<xmp/>")},
				},
			)
		}},
		{name: "lossless alpha", content: func(t testing.TB) []byte {
			return admissionExtendedLosslessWebP(t, 1<<4, true, nil)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			content := tt.content(t)
			result, err := AdmitContent(
				context.Background(),
				admissionRequest("screen.webp", "image/webp", content),
				DefaultAdmissionLimits(DefaultLimits()),
			)
			if err != nil {
				t.Fatalf("AdmitContent() error = %v", err)
			}
			if result.MediaType != "image/webp" || result.Profile != ProcessorProfileImage {
				t.Fatalf("AdmitContent() = %#v, want WebP image profile", result)
			}
		})
	}
}

func TestAdmitContentRejectsInvalidWebPChunkPolicy(t *testing.T) {
	t.Parallel()

	vp8x := func(flags byte) admissionWebPChunk {
		payload := make([]byte, 10)
		payload[0] = flags
		return admissionWebPChunk{kind: "VP8X", payload: payload}
	}
	imageChunk := admissionWebPImageChunk(t)
	nonZeroPadding := admissionExtendedWebP(
		t,
		1<<3,
		nil,
		[]admissionWebPChunk{{kind: "EXIF", payload: []byte{1}}},
	)
	nonZeroPadding[len(nonZeroPadding)-1] = 1
	reservedVP8XBytes := admissionExtendedWebP(t, 0, nil, nil)
	reservedVP8XBytes[21] = 1
	tests := []struct {
		name    string
		content []byte
	}{
		{name: "unknown before image", content: admissionExtendedWebP(t, 0, []admissionWebPChunk{{kind: "JUNK", payload: []byte{'M', 'Z'}}}, nil)},
		{name: "undeclared EXIF", content: admissionExtendedWebP(t, 0, nil, []admissionWebPChunk{{kind: "EXIF", payload: []byte("Exif")}})},
		{name: "duplicate VP8X", content: admissionRIFFWebP(t, []admissionWebPChunk{vp8x(0), vp8x(0), imageChunk})},
		{name: "duplicate EXIF", content: admissionExtendedWebP(t, 1<<3, nil, []admissionWebPChunk{{kind: "EXIF", payload: []byte("one")}, {kind: "EXIF", payload: []byte("two")}})},
		{name: "EXIF before image", content: admissionExtendedWebP(t, 1<<3, []admissionWebPChunk{{kind: "EXIF", payload: []byte("Exif")}}, nil)},
		{name: "ICCP after image", content: admissionExtendedWebP(t, 1<<5, nil, []admissionWebPChunk{{kind: "ICCP", payload: []byte("profile")}})},
		{name: "declared ICCP missing", content: admissionExtendedWebP(t, 1<<5, nil, nil)},
		{name: "undeclared alpha", content: admissionExtendedWebP(t, 0, []admissionWebPChunk{{kind: "ALPH", payload: []byte{0, 0x80}}}, nil)},
		{name: "declared alpha missing", content: admissionExtendedWebP(t, 1<<4, nil, nil)},
		{name: "undeclared lossless alpha", content: admissionExtendedLosslessWebP(t, 0, true, nil)},
		{name: "declared lossless alpha missing", content: admissionExtendedLosslessWebP(t, 1<<4, false, nil)},
		{name: "ALPH before lossless image", content: admissionExtendedLosslessWebP(t, 1<<4, false, []admissionWebPChunk{{kind: "ALPH", payload: []byte{0, 0x80}}})},
		{name: "multiple image chunks", content: admissionRIFFWebP(t, []admissionWebPChunk{imageChunk, imageChunk})},
		{name: "animation flag", content: admissionExtendedWebP(t, 1<<1, nil, nil)},
		{name: "animation chunk", content: admissionRIFFWebP(t, []admissionWebPChunk{vp8x(1 << 1), {kind: "ANIM", payload: make([]byte, 6)}, imageChunk})},
		{name: "low reserved VP8X flag", content: admissionExtendedWebP(t, 1, nil, nil)},
		{name: "high reserved VP8X flag", content: admissionExtendedWebP(t, 1<<6, nil, nil)},
		{name: "reserved VP8X flag", content: admissionExtendedWebP(t, 1<<7, nil, nil)},
		{name: "reserved VP8X bytes", content: reservedVP8XBytes},
		{name: "non-zero padding", content: nonZeroPadding},
		{name: "metadata without VP8X", content: admissionRIFFWebP(t, []admissionWebPChunk{imageChunk, {kind: "EXIF", payload: []byte("Exif")}})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := AdmitContent(
				context.Background(),
				admissionRequest("screen.webp", "image/webp", tt.content),
				DefaultAdmissionLimits(DefaultLimits()),
			)
			if !errors.Is(err, ErrAdmissionRejected) {
				t.Fatalf("AdmitContent() error = %v, want ErrAdmissionRejected", err)
			}
		})
	}
}

func TestAdmitContentRejectsSpoofedOrActiveContent(t *testing.T) {
	t.Parallel()

	pngContent := admissionPNG(t, 1, 1)
	jpegContent := admissionJPEG(t, 1, 1)
	tests := []struct {
		name      string
		fileName  string
		mediaType string
		content   []byte
	}{
		{name: "magic versus MIME", fileName: "photo.jpg", mediaType: "image/jpeg", content: pngContent},
		{name: "extension versus MIME", fileName: "photo.png", mediaType: "image/jpeg", content: jpegContent},
		{name: "extension versus structure", fileName: "report.png", mediaType: "image/png", content: admissionPDF(1, 0)},
		{name: "invalid UTF-8", fileName: "notes.txt", mediaType: "text/plain", content: []byte{'o', 'k', 0xff}},
		{name: "HTML", fileName: "page.html", mediaType: "text/html", content: []byte("<!doctype html><script>alert(1)</script>")},
		{name: "SVG", fileName: "vector.svg", mediaType: "image/svg+xml", content: []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script/></svg>`)},
		{name: "script", fileName: "run.sh", mediaType: "text/x-shellscript", content: []byte("#!/bin/sh\nexec id\n")},
		{name: "executable", fileName: "program.bin", mediaType: "application/octet-stream", content: append([]byte{0x7f, 'E', 'L', 'F'}, make([]byte, 12)...)},
		{name: "unknown", fileName: "payload.bin", mediaType: "application/octet-stream", content: []byte{0x01, 0x02, 0x03, 0x04}},
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

func TestAdmitContentRejectsActiveMarkupBeyondTextPrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content []byte
	}{
		{name: "benign text before script", content: []byte("meeting notes\n<script>alert(1)</script>\n")},
		{name: "whitespace before HTML", content: append(bytes.Repeat([]byte(" "), archiveProbeBytes+1), []byte("<!doctype html><html></html>")...)},
		{name: "whitespace before SVG", content: append(bytes.Repeat([]byte("\t"), archiveProbeBytes+1), []byte("<svg></svg>")...)},
		{name: "whitespace before shebang", content: append(bytes.Repeat([]byte("\n"), archiveProbeBytes+1), []byte("#!/bin/sh\n")...)},
		{name: "BOM before mixed-case script", content: append([]byte{0xef, 0xbb, 0xbf, '\n'}, []byte("<ScRiPt>alert(1)</ScRiPt>")...)},
		{name: "mixed-case echo off", content: []byte("\n@EcHo OfF\r\n")},
		{name: "benign text before body onload", content: append(bytes.Repeat([]byte(" "), archiveProbeBytes+1), []byte("<BoDy onload=alert(1)>")...)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := AdmitContent(
				context.Background(),
				admissionRequest("notes.txt", "text/plain", tt.content),
				DefaultAdmissionLimits(DefaultLimits()),
			)
			if !errors.Is(err, ErrAdmissionRejected) {
				t.Fatalf("AdmitContent() error = %v, want ErrAdmissionRejected", err)
			}
		})
	}
}

func TestAdmitContentRequiresExactPDFHeader(t *testing.T) {
	t.Parallel()

	pdf := admissionPDF(1, 0)
	tests := []struct {
		name       string
		prefix     []byte
		wantReject bool
	}{
		{name: "exact PDF header"},
		{name: "GIF prefix", prefix: []byte("GIF89a"), wantReject: true},
		{name: "active HTML prefix", prefix: []byte("<!doctype html><script>alert(1)</script>\n"), wantReject: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			content := append(append([]byte(nil), tt.prefix...), pdf...)
			result, err := AdmitContent(
				context.Background(),
				admissionRequest("report.pdf", "application/pdf", content),
				DefaultAdmissionLimits(DefaultLimits()),
			)
			if tt.wantReject {
				if !errors.Is(err, ErrAdmissionRejected) {
					t.Fatalf("AdmitContent() error = %v, want ErrAdmissionRejected", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("AdmitContent() error = %v", err)
			}
			if result.MediaType != "application/pdf" || result.Profile != ProcessorProfilePDF {
				t.Fatalf("AdmitContent() = %#v, want PDF profile", result)
			}
		})
	}
}

func TestAdmitContentRejectsActiveMarkupAfterMisleadingTextCandidates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content []byte
	}{
		{name: "malformed autolink attribute", content: []byte("<https://x onclick=alert(1)>")},
		{name: "comment quote before image", content: []byte("notes\n<!-- \" --> <img src=x onerror=alert(1)>")},
		{name: "inert malformed text before image", content: []byte("notes\n<> <img src=x onerror=alert(1)>")},
		{name: "quoted greater-than before active attribute", content: []byte("<img alt=\"safe > text\" onerror=alert(1)>")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := AdmitContent(
				context.Background(),
				admissionRequest("notes.txt", "text/plain", tt.content),
				DefaultAdmissionLimits(DefaultLimits()),
			)
			if !errors.Is(err, ErrAdmissionRejected) {
				t.Fatalf("AdmitContent() error = %v, want ErrAdmissionRejected", err)
			}
		})
	}
}

func TestAdmitContentRejectsUnsafeMarkdownURIAutolinks(t *testing.T) {
	t.Parallel()

	for _, scheme := range []string{"javascript:alert(1)", "data:text/plain,unsafe", "mailto:unsafe@example.invalid", "http:foo"} {
		scheme := scheme
		t.Run(scheme, func(t *testing.T) {
			t.Parallel()
			_, err := AdmitContent(
				context.Background(),
				admissionRequest("notes.txt", "text/plain", []byte("See <"+scheme+"> for details.\n")),
				DefaultAdmissionLimits(DefaultLimits()),
			)
			if !errors.Is(err, ErrAdmissionRejected) {
				t.Fatalf("AdmitContent() error = %v, want ErrAdmissionRejected", err)
			}
		})
	}
}

func TestAdmitContentAllowsTextDiscussingActiveMarkers(t *testing.T) {
	t.Parallel()

	content := []byte("Patch notes discuss script tags, SVG markup, shebang syntax, and @echo off without containing active markup.\n")
	result, err := AdmitContent(
		context.Background(),
		admissionRequest("notes.txt", "text/plain", content),
		DefaultAdmissionLimits(DefaultLimits()),
	)
	if err != nil {
		t.Fatalf("AdmitContent() error = %v", err)
	}
	if result.Profile != ProcessorProfileText {
		t.Fatalf("AdmitContent() = %#v, want text profile", result)
	}
}

func TestAdmitContentAllowsNonContiguousLeadingScriptSignature(t *testing.T) {
	t.Parallel()

	content := []byte("# \n! ordinary notes\n")
	result, err := AdmitContent(
		context.Background(),
		admissionRequest("notes.txt", "text/plain", content),
		DefaultAdmissionLimits(DefaultLimits()),
	)
	if err != nil {
		t.Fatalf("AdmitContent() error = %v", err)
	}
	if result.Profile != ProcessorProfileText {
		t.Fatalf("AdmitContent() = %#v, want text profile", result)
	}
}

func TestAdmitContentAllowsSafeTextWithAngleBrackets(t *testing.T) {
	t.Parallel()

	tests := []string{
		"x < y\n",
		"See <https://example.invalid/path> for details.\n",
	}
	for _, content := range tests {
		content := content
		t.Run(content, func(t *testing.T) {
			result, err := AdmitContent(
				context.Background(),
				admissionRequest("notes.txt", "text/plain", []byte(content)),
				DefaultAdmissionLimits(DefaultLimits()),
			)
			if err != nil {
				t.Fatalf("AdmitContent() error = %v", err)
			}
			if result.Profile != ProcessorProfileText {
				t.Fatalf("AdmitContent() = %#v, want text profile", result)
			}
		})
	}
}

func TestActiveTextDetectorBoundsUnterminatedTagCandidate(t *testing.T) {
	t.Parallel()

	detector := activeTextDetector{}
	if err := detector.observe(context.Background(), append([]byte("<"), bytes.Repeat([]byte("a"), maxActiveTextTagBytes)...)); err != nil {
		t.Fatalf("activeTextDetector.observe() error = %v", err)
	}
	if !detector.active {
		t.Fatal("activeTextDetector did not reject an overlong tag candidate")
	}
	if len(detector.tagCandidate) > maxActiveTextTagBytes {
		t.Fatalf("activeTextDetector retained %d tag bytes, want at most %d", len(detector.tagCandidate), maxActiveTextTagBytes)
	}
}

func TestActiveTextDetectorRejectsRepeatedQuotedGreaterThanWithoutReparsing(t *testing.T) {
	t.Parallel()

	detector := activeTextDetector{}
	if err := detector.observe(context.Background(), []byte("<img title=\">>")); err != nil {
		t.Fatalf("activeTextDetector.observe() error = %v", err)
	}
	if !detector.active {
		t.Fatal("activeTextDetector.active = false, want rejection after repeated quoted greater-than")
	}
	if detector.tokenizerCalls != 1 {
		t.Fatalf("activeTextDetector tokenizer calls = %d, want 1", detector.tokenizerCalls)
	}
}

func TestAdmitContentCancelsActiveTextScan(t *testing.T) {
	t.Parallel()

	content := bytes.Repeat([]byte("x"), 64*1024)
	_, err := AdmitContent(
		newCancellationAfterChecksContext(12),
		admissionRequest("notes.txt", "text/plain", content),
		DefaultAdmissionLimits(DefaultLimits()),
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("AdmitContent() error = %v, want context.Canceled", err)
	}
}

type cancellationAfterChecksContext struct {
	context.Context
	cancel    context.CancelFunc
	remaining int
}

func newCancellationAfterChecksContext(remaining int) context.Context {
	base, cancel := context.WithCancel(context.Background())
	return &cancellationAfterChecksContext{Context: base, cancel: cancel, remaining: remaining}
}

func (value *cancellationAfterChecksContext) Err() error {
	if err := value.Context.Err(); err != nil {
		return err
	}
	if value.remaining == 0 {
		value.cancel()
		return value.Context.Err()
	}
	value.remaining--
	return nil
}

func TestAdmitContentRejectsBinaryEvidenceDisguisedAsText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content []byte
	}{
		{name: "PDF signature", content: []byte("%PDF-1.7\nprintable payload\n%%EOF\n")},
		{name: "GIF signature", content: []byte("GIF89aprintable payload\n")},
		{name: "non-text control byte", content: []byte("status:\x1b[31mfailed\n")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := AdmitContent(context.Background(), admissionRequest("notes.txt", "text/plain", tt.content), DefaultAdmissionLimits(DefaultLimits()))
			if !errors.Is(err, ErrAdmissionRejected) {
				t.Fatalf("AdmitContent() error = %v, want ErrAdmissionRejected", err)
			}
		})
	}
}

func TestAdmitContentRejectsTrailingImagePolyglots(t *testing.T) {
	t.Parallel()

	elfSuffix := []byte{0x7f, 'E', 'L', 'F', 0x02, 0x01, 0x01}
	zipSuffix := []byte{'P', 'K', 0x03, 0x04, 0x14, 0x00}
	tests := []struct {
		name      string
		fileName  string
		mediaType string
		content   []byte
	}{
		{name: "PNG with ELF suffix", fileName: "screen.png", mediaType: "image/png", content: append(admissionPNG(t, 1, 1), elfSuffix...)},
		{name: "PNG with ZIP suffix", fileName: "screen.png", mediaType: "image/png", content: append(admissionPNG(t, 1, 1), zipSuffix...)},
		{name: "JPEG with ELF suffix", fileName: "screen.jpg", mediaType: "image/jpeg", content: append(admissionJPEG(t, 1, 1), elfSuffix...)},
		{name: "JPEG with ZIP suffix", fileName: "screen.jpg", mediaType: "image/jpeg", content: append(admissionJPEG(t, 1, 1), zipSuffix...)},
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

func TestAdmitContentRejectsImageAndPDFComplexity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		fileName  string
		mediaType string
		content   []byte
		mutate    func(*AdmissionLimits)
	}{
		{name: "image width", fileName: "screen.png", mediaType: "image/png", content: admissionPNG(t, 3, 2), mutate: func(value *AdmissionLimits) { value.MaxImageWidth = 2 }},
		{name: "image height", fileName: "screen.png", mediaType: "image/png", content: admissionPNG(t, 2, 3), mutate: func(value *AdmissionLimits) { value.MaxImageHeight = 2 }},
		{name: "image pixels", fileName: "screen.png", mediaType: "image/png", content: admissionPNG(t, 3, 3), mutate: func(value *AdmissionLimits) { value.MaxImagePixels = 8 }},
		{name: "WebP width", fileName: "screen.webp", mediaType: "image/webp", content: admissionWebPDimensions(t, 2, 1), mutate: func(value *AdmissionLimits) { value.MaxImageWidth = 1 }},
		{name: "WebP pixels", fileName: "screen.webp", mediaType: "image/webp", content: admissionWebPDimensions(t, 2, 2), mutate: func(value *AdmissionLimits) { value.MaxImagePixels = 3 }},
		{name: "PDF bytes", fileName: "report.pdf", mediaType: "application/pdf", content: admissionPDF(1, 0), mutate: func(value *AdmissionLimits) { value.MaxPDFBytes = int64(len(admissionPDF(1, 0)) - 1) }},
		{name: "PDF objects", fileName: "report.pdf", mediaType: "application/pdf", content: admissionPDF(1, 2), mutate: func(value *AdmissionLimits) { value.MaxPDFObjects = 4 }},
		{name: "PDF pages", fileName: "report.pdf", mediaType: "application/pdf", content: admissionPDF(2, 0), mutate: func(value *AdmissionLimits) { value.MaxPDFPages = 1 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			limits := DefaultAdmissionLimits(DefaultLimits())
			tt.mutate(&limits)
			_, err := AdmitContent(context.Background(), admissionRequest(tt.fileName, tt.mediaType, tt.content), limits)
			if !errors.Is(err, ErrAdmissionLimitExceeded) {
				t.Fatalf("AdmitContent() error = %v, want ErrAdmissionLimitExceeded", err)
			}
		})
	}
}

func TestAdmitContentRejectsTruncatedOversizedImageAtDimensions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		fileName  string
		mediaType string
		content   []byte
	}{
		{name: "PNG IHDR", fileName: "screen.png", mediaType: "image/png", content: admissionOversizedPNG(t)},
		{name: "JPEG SOF", fileName: "screen.jpg", mediaType: "image/jpeg", content: admissionOversizedJPEG(t)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limits := DefaultAdmissionLimits(DefaultLimits())
			_, err := AdmitContent(context.Background(), admissionRequest(tt.fileName, tt.mediaType, tt.content), limits)
			if !errors.Is(err, ErrAdmissionLimitExceeded) {
				t.Fatalf("AdmitContent() error = %v, want ErrAdmissionLimitExceeded before full decode", err)
			}
		})
	}
}

func TestAdmitContentRejectsUnavailableArchiveScannerBeforeRead(t *testing.T) {
	t.Parallel()

	reader := &admissionCountingReader{failOnRead: true}
	_, err := AdmitContent(context.Background(), AdmissionRequest{
		DisplayName: "bundle.zip", DeclaredMediaType: "application/zip", SizeBytes: 32,
		Content: reader, ScannerStatus: ScannerStatusUnhealthy,
	}, DefaultAdmissionLimits(DefaultLimits()))
	if !errors.Is(err, ErrArchiveScannerUnavailable) {
		t.Fatalf("AdmitContent() error = %v, want ErrArchiveScannerUnavailable", err)
	}
	if reader.bytesRead != 0 {
		t.Fatalf("AdmitContent() read %d bytes before scanner readiness", reader.bytesRead)
	}
}

func TestAdmitContentHonorsContextAndReadBounds(t *testing.T) {
	t.Parallel()

	limits := DefaultAdmissionLimits(DefaultLimits())
	limits.MaxReadBytes = 8
	limits.MaxPDFBytes = 8
	tests := []struct {
		name      string
		ctx       func() context.Context
		sizeBytes int64
		reader    *admissionCountingReader
		want      error
		maxRead   int
	}{
		{name: "cancelled", ctx: admissionCancelledContext, sizeBytes: 4, reader: &admissionCountingReader{data: []byte("text")}, want: context.Canceled, maxRead: 0},
		{name: "declared too large", ctx: context.Background, sizeBytes: 9, reader: &admissionCountingReader{failOnRead: true}, want: ErrAdmissionLimitExceeded, maxRead: 0},
		{name: "short read", ctx: context.Background, sizeBytes: 8, reader: &admissionCountingReader{data: []byte("text")}, want: ErrAdmissionRejected, maxRead: 4},
		{name: "read exceeds bound", ctx: context.Background, sizeBytes: 8, reader: &admissionCountingReader{infinite: true}, want: ErrAdmissionLimitExceeded, maxRead: 9},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := AdmitContent(tt.ctx(), AdmissionRequest{
				DisplayName: "notes.txt", DeclaredMediaType: "text/plain", SizeBytes: tt.sizeBytes,
				Content: tt.reader, ScannerStatus: ScannerStatusHealthy,
			}, limits)
			if !errors.Is(err, tt.want) {
				t.Fatalf("AdmitContent() error = %v, want %v", err, tt.want)
			}
			if tt.reader.bytesRead > tt.maxRead {
				t.Fatalf("AdmitContent() read %d bytes, want at most %d", tt.reader.bytesRead, tt.maxRead)
			}
		})
	}
}

func FuzzAdmissionSeeds(f *testing.F) {
	f.Add("notes.txt", "text/plain", []byte("plain text\n"))
	f.Add("notes.txt", "text/plain", []byte{0xff, 0xfe})
	f.Add("page.html", "text/html", []byte("<!doctype html><script/>"))
	f.Add("payload.bin", "application/octet-stream", []byte{0x7f, 'E', 'L', 'F'})

	f.Fuzz(func(t *testing.T, displayName, mediaType string, content []byte) {
		if len(displayName) > 256 || len(mediaType) > 256 || len(content) > 4096 {
			t.Skip()
		}
		result, err := AdmitContent(context.Background(), admissionRequest(displayName, mediaType, content), DefaultAdmissionLimits(DefaultLimits()))
		if err == nil {
			switch result.Profile {
			case ProcessorProfileImage, ProcessorProfilePDF, ProcessorProfileText, ProcessorProfileArchive:
			default:
				t.Fatalf("AdmitContent() returned unknown profile %q", result.Profile)
			}
			if result.MediaType == "" {
				t.Fatal("AdmitContent() admitted content without canonical media type")
			}
		}
	})
}

func admissionRequest(displayName, mediaType string, content []byte) AdmissionRequest {
	return AdmissionRequest{
		DisplayName: displayName, DeclaredMediaType: mediaType, SizeBytes: int64(len(content)),
		Content: bytes.NewReader(content), ScannerStatus: ScannerStatusHealthy,
	}
}

type admissionReaderErrorAfterPayload struct {
	content []byte
	done    bool
}

func (r *admissionReaderErrorAfterPayload) Read(p []byte) (int, error) {
	if r.done {
		return 0, context.Canceled
	}
	r.done = true
	return copy(p, r.content), context.Canceled
}

func admissionPNG(t testing.TB, width, height int) []byte {
	t.Helper()
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, admissionImage(width, height)); err != nil {
		t.Fatalf("encode PNG fixture: %v", err)
	}
	return buffer.Bytes()
}

func admissionJPEG(t testing.TB, width, height int) []byte {
	t.Helper()
	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, admissionImage(width, height), &jpeg.Options{Quality: 80}); err != nil {
		t.Fatalf("encode JPEG fixture: %v", err)
	}
	return buffer.Bytes()
}

func admissionOversizedPNG(t testing.TB) []byte {
	t.Helper()
	content := admissionPNG(t, 1, 1)
	binary.BigEndian.PutUint32(content[16:20], 16_385)
	binary.BigEndian.PutUint32(content[20:24], 1)
	binary.BigEndian.PutUint32(content[29:33], crc32.ChecksumIEEE(content[12:29]))
	return content
}

func admissionOversizedJPEG(t testing.TB) []byte {
	t.Helper()
	content := admissionJPEG(t, 1, 1)
	for offset := 2; offset+9 < len(content); {
		if content[offset] != 0xff {
			offset++
			continue
		}
		marker := content[offset+1]
		if marker == 0xd8 || marker == 0xd9 || marker == 0x01 || marker >= 0xd0 && marker <= 0xd7 {
			offset += 2
			continue
		}
		segmentLength := int(binary.BigEndian.Uint16(content[offset+2 : offset+4]))
		if marker >= 0xc0 && marker <= 0xcf && marker != 0xc4 && marker != 0xc8 && marker != 0xcc {
			binary.BigEndian.PutUint16(content[offset+5:offset+7], 1)
			binary.BigEndian.PutUint16(content[offset+7:offset+9], 16_385)
			return content
		}
		if segmentLength < 2 || offset+2+segmentLength > len(content) {
			break
		}
		offset += 2 + segmentLength
	}
	t.Fatal("JPEG fixture has no SOF segment")
	return nil
}

func admissionWebP(t testing.TB) []byte {
	t.Helper()
	content, err := base64.StdEncoding.DecodeString("UklGRiQAAABXRUJQVlA4IBgAAAAwAQCdASoBAAEAAUAmJaQAA3AA/vuUAAA=")
	if err != nil {
		t.Fatalf("decode WebP fixture: %v", err)
	}
	return content
}

func admissionWebPLossless(t testing.TB) []byte {
	t.Helper()
	content, err := base64.StdEncoding.DecodeString(
		"UklGRrIBAABXRUJQVlA4TKUBAAAvSsAYAA8w//M///MfeJAkbXvaSG7m8Q3GfYSBJekwQztm/IcZlgwnmWImn2BK7aFmBtnVir6q//8VOkFE/xm4baTIu8c48ArEo6+B3zFKYln3pqClSCKX0begFTAXFOLXHSyF8cCNcZEG4OywuA4KVVfJCiArU7GAgJI8+lJP/OKMT/fBAjevg1cYB7YVkFuWga2lyPi5I0HFy5YTpWIHg0RZpkniRVW9odHAKOwosWuOGdxIyn2OvaCDvhg/we6TwadPBPbqBV58MsLmMJ8yZnOWk8SRz4N+QoyPL+MnamzMvcE1rHNEr91F9GKZPVUcS9w7PhhH36suB9qPeYb/oLk6cuTiJ0wOK3m5h1cKjW6EVZCYMK7dxcKCBdgP9HkKr9gkAO2P8GKZGWVdIAatQa+1IDpt6qyorVwdy01xdW8Jkfk6xjEXmVQQ+HQdFr6OKhIN34dXWq0+0qr6EJSCeeVLH9+gvGTLyqM65PQ44ihzlTXxQKjKbAvshXgir7Lil9w4L2bvMycmjQcqXaMCO6BlY28i+FOLzbfI1vEqxAhotocAAA==",
	)
	if err != nil {
		t.Fatalf("decode lossless WebP fixture: %v", err)
	}
	return content
}

type admissionWebPChunk struct {
	kind    string
	payload []byte
}

func admissionExtendedWebP(
	t testing.TB,
	flags byte,
	beforeImage, afterImage []admissionWebPChunk,
) []byte {
	t.Helper()
	simple := admissionWebP(t)
	imageSize := int(binary.LittleEndian.Uint32(simple[16:20]))
	imagePayload := append([]byte(nil), simple[20:20+imageSize]...)
	vp8x := make([]byte, 10)
	vp8x[0] = flags
	chunks := []admissionWebPChunk{{kind: "VP8X", payload: vp8x}}
	chunks = append(chunks, beforeImage...)
	chunks = append(chunks, admissionWebPChunk{kind: "VP8 ", payload: imagePayload})
	chunks = append(chunks, afterImage...)
	return admissionRIFFWebP(t, chunks)
}

func admissionWebPImageChunk(t testing.TB) admissionWebPChunk {
	t.Helper()
	simple := admissionWebP(t)
	imageSize := int(binary.LittleEndian.Uint32(simple[16:20]))
	return admissionWebPChunk{
		kind:    string(simple[12:16]),
		payload: append([]byte(nil), simple[20:20+imageSize]...),
	}
}

func admissionExtendedLosslessWebP(t testing.TB, flags byte, alphaUsed bool, beforeImage []admissionWebPChunk) []byte {
	t.Helper()
	simple := admissionWebPLossless(t)
	imageSize := int(binary.LittleEndian.Uint32(simple[16:20]))
	imagePayload := append([]byte(nil), simple[20:20+imageSize]...)
	if alphaUsed {
		imagePayload[4] |= 1 << 4
	} else {
		imagePayload[4] &^= 1 << 4
	}
	vp8x := make([]byte, 10)
	vp8x[0] = flags
	imageBits := binary.LittleEndian.Uint32(imagePayload[1:5])
	widthMinusOne := imageBits & 0x3fff
	heightMinusOne := imageBits >> 14 & 0x3fff
	vp8x[4] = byte(widthMinusOne)
	vp8x[5] = byte(widthMinusOne >> 8)
	vp8x[6] = byte(widthMinusOne >> 16)
	vp8x[7] = byte(heightMinusOne)
	vp8x[8] = byte(heightMinusOne >> 8)
	vp8x[9] = byte(heightMinusOne >> 16)
	chunks := []admissionWebPChunk{{kind: "VP8X", payload: vp8x}}
	chunks = append(chunks, beforeImage...)
	chunks = append(chunks, admissionWebPChunk{kind: "VP8L", payload: imagePayload})
	return admissionRIFFWebP(t, chunks)
}

func admissionRIFFWebP(t testing.TB, chunks []admissionWebPChunk) []byte {
	t.Helper()
	var content bytes.Buffer
	content.WriteString("RIFF")
	content.Write(make([]byte, 4))
	content.WriteString("WEBP")
	for _, chunk := range chunks {
		if len(chunk.kind) != 4 {
			t.Fatalf("WebP chunk kind %q has length %d, want 4", chunk.kind, len(chunk.kind))
		}
		content.WriteString(chunk.kind)
		var size [4]byte
		binary.LittleEndian.PutUint32(size[:], uint32(len(chunk.payload)))
		content.Write(size[:])
		content.Write(chunk.payload)
		if len(chunk.payload)%2 != 0 {
			content.WriteByte(0)
		}
	}
	result := content.Bytes()
	binary.LittleEndian.PutUint32(result[4:8], uint32(len(result)-8))
	return result
}

func admissionWebPDimensions(t testing.TB, width, height uint16) []byte {
	t.Helper()
	content := append([]byte(nil), admissionWebP(t)...)
	binary.LittleEndian.PutUint16(content[26:28], width)
	binary.LittleEndian.PutUint16(content[28:30], height)
	return content
}

func admissionHeaderOnlyWebP() []byte {
	content := make([]byte, 30)
	copy(content[0:4], "RIFF")
	binary.LittleEndian.PutUint32(content[4:8], uint32(len(content)-8))
	copy(content[8:12], "WEBP")
	copy(content[12:16], "VP8 ")
	binary.LittleEndian.PutUint32(content[16:20], 10)
	copy(content[23:26], []byte{0x9d, 0x01, 0x2a})
	binary.LittleEndian.PutUint16(content[26:28], 1)
	binary.LittleEndian.PutUint16(content[28:30], 1)
	return content
}

func admissionWebPWithTrailingChunk(content, payload []byte) []byte {
	result := append([]byte(nil), content...)
	result = append(result, 'J', 'U', 'N', 'K', 0, 0, 0, 0)
	binary.LittleEndian.PutUint32(result[len(content)+4:len(content)+8], uint32(len(payload)))
	result = append(result, payload...)
	if len(payload)%2 != 0 {
		result = append(result, 0)
	}
	binary.LittleEndian.PutUint32(result[4:8], uint32(len(result)-8))
	return result
}

func admissionImage(width, height int) image.Image {
	value := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			value.Set(x, y, color.RGBA{R: uint8(20 + x), G: uint8(40 + y), B: 80, A: 255})
		}
	}
	return value
}

func admissionPDF(pageCount, extraObjectCount int) []byte {
	return admissionPDFWithCatalogEntry(pageCount, extraObjectCount, "")
}

func admissionPDFWithCatalogEntry(pageCount, extraObjectCount int, catalogEntry string) []byte {
	additionalObjects := make([]string, extraObjectCount)
	for index := range additionalObjects {
		additionalObjects[index] = fmt.Sprintf("<< /Fixture %d >>", index)
	}
	return admissionPDFDocument(pageCount, catalogEntry, additionalObjects)
}

func admissionPDFWithAdditionalObjects(pageCount int, additionalObjects ...string) []byte {
	return admissionPDFDocument(pageCount, "", additionalObjects)
}

func admissionPDFDocument(pageCount int, catalogEntry string, additionalObjects []string) []byte {
	objects := make([]string, 0, 2+pageCount*2+len(additionalObjects))
	kids := make([]string, 0, pageCount)
	for page := 0; page < pageCount; page++ {
		kids = append(kids, fmt.Sprintf("%d 0 R", 3+page*2))
	}
	catalog := "<< /Type /Catalog /Pages 2 0 R"
	if catalogEntry != "" {
		catalog += " " + catalogEntry
	}
	catalog += " >>"
	objects = append(objects,
		catalog,
		fmt.Sprintf("<< /Type /Pages /Count %d /Kids [%s] >>", pageCount, strings.Join(kids, " ")),
	)
	for page := 0; page < pageCount; page++ {
		pageObject := 3 + page*2
		objects = append(objects,
			fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 10 10] /Contents %d 0 R >>", pageObject+1),
			"<< /Length 1 >>\nstream\nx\nendstream",
		)
	}
	objects = append(objects, additionalObjects...)
	return admissionPDFObjects(objects)
}

func admissionPDFWithLazyObjectStreamAction(t testing.TB) []byte {
	t.Helper()
	raw := admissionPDFObjects([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Count 1 /Kids [3 0 R] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 10 10] /Contents 4 0 R /Annots [5 0 R] >>",
		"<< /Length 1 >>\nstream\nx\nendstream",
		"<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] /Border [0 0 0] /A 6 0 R >>",
		"<< /S /UR#49 /URI (https://example.invalid) >>",
	})
	configuration, err := newPDFConfiguration(DefaultAdmissionLimits(DefaultLimits()))
	if err != nil {
		t.Fatalf("newPDFConfiguration() error = %v", err)
	}
	var optimized bytes.Buffer
	if err := api.Optimize(bytes.NewReader(raw), &optimized, configuration); err != nil {
		t.Fatalf("optimize object-stream PDF fixture: %v", err)
	}
	return optimized.Bytes()
}

func admissionPDFWithMultipleObjectStreams(t testing.TB) []byte {
	t.Helper()
	annotations := make([]string, 201)
	references := make([]string, len(annotations))
	for index := range annotations {
		annotations[index] = fmt.Sprintf("<< /Type /Annot /Subtype /Text /Rect [0 0 1 1] /Contents (fixture-%d) >>", index)
		references[index] = fmt.Sprintf("%d 0 R", index+5)
	}
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Count 1 /Kids [3 0 R] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 10 10] /Contents 4 0 R /Annots [" + strings.Join(references, " ") + "] >>",
		"<< /Length 1 >>\nstream\nx\nendstream",
	}
	objects = append(objects, annotations...)
	raw := admissionPDFObjects(objects)
	configuration, err := newPDFConfiguration(DefaultAdmissionLimits(DefaultLimits()))
	if err != nil {
		t.Fatalf("newPDFConfiguration() error = %v", err)
	}
	var optimized bytes.Buffer
	if err := api.Optimize(bytes.NewReader(raw), &optimized, configuration); err != nil {
		t.Fatalf("optimize multi-object-stream PDF fixture: %v", err)
	}
	return optimized.Bytes()
}

func admissionPDFWithUnfilteredObjectStreams(t testing.TB) []byte {
	t.Helper()
	const (
		annotationCount            = 201
		firstAnnotationObject      = 5
		maxObjectsPerStream        = 100
		firstObjectStreamObject    = firstAnnotationObject + annotationCount
		objectStreamCount          = 3
		crossReferenceStreamObject = firstObjectStreamObject + objectStreamCount
		objectCount                = crossReferenceStreamObject + 1
	)

	annotations := make([]string, annotationCount)
	references := make([]string, annotationCount)
	for index := range annotations {
		annotations[index] = fmt.Sprintf("<< /Type /Annot /Subtype /Text /Rect [0 0 1 1] /Contents (fixture-%d) >>", index)
		references[index] = fmt.Sprintf("%d 0 R", firstAnnotationObject+index)
	}

	var document bytes.Buffer
	document.WriteString("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")
	offsets := make(map[int]int64, 4+objectStreamCount+1)
	writeObject := func(objectNumber int, body string) {
		offsets[objectNumber] = int64(document.Len())
		fmt.Fprintf(&document, "%d 0 obj\n%s\nendobj\n", objectNumber, body)
	}
	writeObject(1, "<< /Type /Catalog /Pages 2 0 R >>")
	writeObject(2, "<< /Type /Pages /Count 1 /Kids [3 0 R] >>")
	writeObject(3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 10 10] /Contents 4 0 R /Annots ["+strings.Join(references, " ")+"] >>")
	writeObject(4, "<< /Length 1 >>\nstream\nx\nendstream")

	type compressedLocation struct {
		streamObject int
		streamIndex  int
	}
	compressed := make(map[int]compressedLocation, annotationCount)
	for streamIndex := 0; streamIndex < objectStreamCount; streamIndex++ {
		start := streamIndex * maxObjectsPerStream
		end := min(start+maxObjectsPerStream, annotationCount)
		var prolog, payload strings.Builder
		for index := start; index < end; index++ {
			if prolog.Len() > 0 {
				prolog.WriteByte(' ')
			}
			objectNumber := firstAnnotationObject + index
			fmt.Fprintf(&prolog, "%d %d", objectNumber, payload.Len())
			payload.WriteString(annotations[index])
			payload.WriteByte('\n')
			compressed[objectNumber] = compressedLocation{
				streamObject: firstObjectStreamObject + streamIndex,
				streamIndex:  index - start,
			}
		}
		streamContent := prolog.String() + payload.String()
		writeObject(
			firstObjectStreamObject+streamIndex,
			fmt.Sprintf(
				"<< /Type /ObjStm /N %d /First %d /Length %d >>\nstream\n%sendstream",
				end-start,
				prolog.Len(),
				len(streamContent),
				streamContent,
			),
		)
	}

	crossReferenceOffset := int64(document.Len())
	offsets[crossReferenceStreamObject] = crossReferenceOffset
	crossReferenceData := make([]byte, 0, objectCount*7)
	appendCrossReferenceEntry := func(kind byte, field2 uint32, field3 uint16) {
		entry := make([]byte, 7)
		entry[0] = kind
		binary.BigEndian.PutUint32(entry[1:5], field2)
		binary.BigEndian.PutUint16(entry[5:7], field3)
		crossReferenceData = append(crossReferenceData, entry...)
	}
	appendCrossReferenceEntry(0, 0, math.MaxUint16)
	for objectNumber := 1; objectNumber < objectCount; objectNumber++ {
		if location, ok := compressed[objectNumber]; ok {
			appendCrossReferenceEntry(2, uint32(location.streamObject), uint16(location.streamIndex))
			continue
		}
		offset, ok := offsets[objectNumber]
		if !ok || offset < 0 || offset > int64(^uint32(0)) {
			t.Fatalf("invalid xref offset for object %d: %d", objectNumber, offset)
		}
		appendCrossReferenceEntry(1, uint32(offset), 0)
	}

	fmt.Fprintf(
		&document,
		"%d 0 obj\n<< /Type /XRef /Size %d /Root 1 0 R /W [1 4 2] /Length %d >>\nstream\n",
		crossReferenceStreamObject,
		objectCount,
		len(crossReferenceData),
	)
	document.Write(crossReferenceData)
	fmt.Fprintf(&document, "\nendstream\nendobj\nstartxref\n%d\n%%%%EOF\n", crossReferenceOffset)
	return document.Bytes()
}

func decodedPDFObjectStreamSizes(t testing.TB, pdfContext *model.Context, maxDecodeBytes int64) []int64 {
	t.Helper()
	if pdfContext == nil || pdfContext.XRefTable == nil || pdfContext.Table == nil {
		t.Fatal("PDF fixture has no object table")
	}
	sizes := make([]int64, 0)
	for _, entry := range pdfContext.Table {
		if entry == nil || entry.Free {
			continue
		}
		var objectStream *types.ObjectStreamDict
		switch value := entry.Object.(type) {
		case types.ObjectStreamDict:
			copy := value
			objectStream = &copy
		case *types.ObjectStreamDict:
			if value == nil {
				t.Fatal("PDF fixture has a nil object stream")
			}
			copy := *value
			objectStream = &copy
		default:
			continue
		}
		if err := objectStream.DecodeWithLimit(maxDecodeBytes); err != nil {
			t.Fatalf("decode PDF fixture object stream: %v", err)
		}
		sizes = append(sizes, int64(len(objectStream.Content)))
	}
	return sizes
}

func admissionPDFWithStructElemActionTarget() []byte {
	return admissionPDFObjects([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Count 1 /Kids [3 0 R] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 10 10] /Contents 4 0 R /Annots [5 0 R] >>",
		"<< /Length 1 >>\nstream\nx\nendstream",
		"<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] /Border [0 0 0] /A 6 0 R >>",
		"<< /Type /StructElem /S /URI /URI (https://example.invalid) >>",
	})
}

func hasLazyPDFURIAction(t testing.TB, pdfContext *model.Context) bool {
	t.Helper()
	for _, entry := range pdfContext.Table {
		if entry == nil {
			continue
		}
		lazy, ok := entry.Object.(types.LazyObjectStreamObject)
		if !ok {
			continue
		}
		object, err := lazy.DecodedObject(context.Background())
		if err != nil {
			t.Fatalf("decode lazy PDF fixture object: %v", err)
		}
		dict, ok := object.(types.Dict)
		if !ok {
			continue
		}
		name, ok := dict["S"].(types.Name)
		if !ok {
			continue
		}
		decoded, err := types.DecodeName(string(name))
		if err != nil {
			t.Fatalf("decode lazy PDF fixture action subtype: %v", err)
		}
		if decoded == "URI" {
			return true
		}
	}
	return false
}

func admissionPDFObjects(objects []string) []byte {

	var document bytes.Buffer
	document.WriteString("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")
	offsets := make([]int, len(objects)+1)
	for index, object := range objects {
		offsets[index+1] = document.Len()
		fmt.Fprintf(&document, "%d 0 obj\n%s\nendobj\n", index+1, object)
	}
	xrefOffset := document.Len()
	fmt.Fprintf(&document, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for index := 1; index < len(offsets); index++ {
		fmt.Fprintf(&document, "%010d 00000 n \n", offsets[index])
	}
	fmt.Fprintf(&document, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xrefOffset)
	return document.Bytes()
}

func admissionPDFWithCorruptFirstXRefEntry(t testing.TB, content []byte) []byte {
	t.Helper()
	result := append([]byte(nil), content...)
	xref := bytes.Index(result, []byte("xref\n"))
	if xref < 0 {
		t.Fatal("PDF fixture has no xref table")
	}
	line := xref
	for range 3 {
		next := bytes.IndexByte(result[line:], '\n')
		if next < 0 {
			t.Fatal("PDF fixture has incomplete xref table")
		}
		line += next + 1
	}
	if len(result)-line < 10 {
		t.Fatal("PDF fixture has incomplete first xref entry")
	}
	copy(result[line:line+10], "0000000001")
	return result
}

func admissionPDFXRefStreamGolden(t testing.TB) []byte {
	t.Helper()
	// Generated by pdfcpu v0.14.0 from admissionPDF(1, 0), with object/xref streams enabled.
	encoded, err := os.ReadFile("testdata/pdfcpu-generated-xref-stream.pdf.b64")
	if err != nil {
		t.Fatalf("read xref-stream PDF fixture: %v", err)
	}
	content, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(string(encoded)), ""))
	if err != nil {
		t.Fatalf("decode xref-stream PDF fixture: %v", err)
	}
	return content
}

func admissionEncryptedPDF(t testing.TB) []byte {
	t.Helper()
	configuration, err := newPDFConfiguration(DefaultAdmissionLimits(DefaultLimits()))
	if err != nil {
		t.Fatalf("newPDFConfiguration(): %v", err)
	}
	configuration.UserPW = ""
	configuration.OwnerPW = "fixture-owner-password"
	configuration.EncryptUsingAES = true
	configuration.EncryptKeyLength = 256
	var encrypted bytes.Buffer
	if err := api.Encrypt(bytes.NewReader(admissionPDF(1, 0)), &encrypted, configuration); err != nil {
		t.Fatalf("encrypt PDF fixture: %v", err)
	}
	return encrypted.Bytes()
}

func admissionCancelledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

type admissionCountingReader struct {
	data       []byte
	offset     int
	bytesRead  int
	failOnRead bool
	infinite   bool
}

func (reader *admissionCountingReader) Read(buffer []byte) (int, error) {
	if reader.failOnRead {
		return 0, errors.New("reader must not be called")
	}
	if reader.infinite {
		for index := range buffer {
			buffer[index] = 'x'
		}
		reader.bytesRead += len(buffer)
		return len(buffer), nil
	}
	if reader.offset == len(reader.data) {
		return 0, io.EOF
	}
	count := copy(buffer, reader.data[reader.offset:])
	reader.offset += count
	reader.bytesRead += count
	return count, nil
}
