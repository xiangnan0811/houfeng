package attachments

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestProcessorExpiryInputRequiresSafeSystemOwnerAndProjectLimits(t *testing.T) {
	t.Parallel()

	valid := ProcessorExpiryInput{
		ProjectID: "default",
		OwnerID:   "processor_expiry_reaper",
		Limits:    DefaultLimits(),
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("ProcessorExpiryInput.Validate() error = %v", err)
	}

	invalid := []struct {
		name   string
		mutate func(*ProcessorExpiryInput)
	}{
		{name: "project", mutate: func(input *ProcessorExpiryInput) { input.ProjectID = "other" }},
		{name: "empty owner", mutate: func(input *ProcessorExpiryInput) { input.OwnerID = "" }},
		{name: "unsafe owner", mutate: func(input *ProcessorExpiryInput) { input.OwnerID = "Processor!" }},
		{name: "oversized owner", mutate: func(input *ProcessorExpiryInput) {
			input.OwnerID = strings.Repeat("a", 129)
		}},
		{name: "invalid limits", mutate: func(input *ProcessorExpiryInput) {
			input.Limits.MaxFileBytes = 0
		}},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := valid
			test.mutate(&input)
			if err := input.Validate(); !errors.Is(err, ErrInvalidProcessorCommand) {
				t.Fatalf("ProcessorExpiryInput.Validate() error = %v, want ErrInvalidProcessorCommand", err)
			}
		})
	}
}

func TestProcessorResultCodesAreClosed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		code ProcessorResultCode
		want string
	}{
		{code: ProcessorResultCodeClean, want: "clean"},
		{code: ProcessorResultCodeMalware, want: "malware"},
		{code: ProcessorResultCodeUnsafeContent, want: "unsafe_content"},
		{code: ProcessorResultCodeScannerUnavailable, want: "scanner_unavailable"},
		{code: ProcessorResultCodeTimeout, want: "timeout"},
		{code: ProcessorResultCodeProcessingError, want: "processing_error"},
	}
	seen := make(map[ProcessorResultCode]struct{}, len(tests))
	for _, tt := range tests {
		if _, duplicate := seen[tt.code]; duplicate {
			t.Fatalf("duplicate ProcessorResultCode %q", tt.code)
		}
		seen[tt.code] = struct{}{}
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			if got := string(tt.code); got != tt.want {
				t.Fatalf("ProcessorResultCode = %q, want %q", got, tt.want)
			}

			result := ProcessorResult{
				Source:  processorResultTestBlob(0x11, "source-v1", 17, BackendKindLocal),
				Profile: ProcessorProfileArchive,
				Code:    tt.code,
			}
			if err := result.Validate(); err != nil {
				t.Fatalf("ProcessorResult.Validate() code %q error = %v", tt.code, err)
			}
		})
	}
	for _, code := range []ProcessorResultCode{"", "other", "CLEAN"} {
		result := ProcessorResult{
			Source:  processorResultTestBlob(0x11, "source-v1", 17, BackendKindLocal),
			Profile: ProcessorProfileArchive,
			Code:    code,
		}
		if err := result.Validate(); !errors.Is(err, ErrInvalidProcessorResult) {
			t.Fatalf("ProcessorResult.Validate() unknown code %q error = %v, want ErrInvalidProcessorResult", code, err)
		}
	}
}

func TestProcessorResultValidatesOptionalManagedPreview(t *testing.T) {
	t.Parallel()

	base := validProcessorResultForTest()
	valid := []struct {
		name   string
		result ProcessorResult
	}{
		{name: "clean text", result: base},
		{name: "clean image", result: func() ProcessorResult {
			result := base
			result.Profile = ProcessorProfileImage
			result.Preview.MediaType = ManagedPreviewMediaTypePNG
			return result
		}()},
		{name: "clean PDF", result: func() ProcessorResult {
			result := base
			result.Profile = ProcessorProfilePDF
			result.Preview.MediaType = ManagedPreviewMediaTypePNG
			return result
		}()},
		{name: "clean archive without preview", result: ProcessorResult{
			Source: base.Source, Profile: ProcessorProfileArchive, Code: ProcessorResultCodeClean,
		}},
		{name: "non-clean image without preview", result: ProcessorResult{
			Source: base.Source, Profile: ProcessorProfileImage, Code: ProcessorResultCodeMalware,
		}},
		{name: "text preview aliases source", result: func() ProcessorResult {
			result := base
			result.Preview.Blob = result.Source
			return result
		}()},
	}
	for _, tt := range valid {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := tt.result.Validate(); err != nil {
				t.Fatalf("ProcessorResult.Validate() error = %v", err)
			}
			if _, err := tt.result.Digest(); err != nil {
				t.Fatalf("ProcessorResult.Digest() error = %v", err)
			}
		})
	}

	invalid := []struct {
		name   string
		mutate func(*ProcessorResult)
	}{
		{name: "unknown profile", mutate: func(result *ProcessorResult) { result.Profile = "unknown" }},
		{name: "unknown code", mutate: func(result *ProcessorResult) { result.Code = "unknown" }},
		{name: "invalid source Blob", mutate: func(result *ProcessorResult) { result.Source.Key = "sha256/invalid" }},
		{name: "missing required preview", mutate: func(result *ProcessorResult) {
			result.HasPreview = false
			result.Preview = ManagedPreview{}
		}},
		{name: "absent preview retains value", mutate: func(result *ProcessorResult) { result.HasPreview = false }},
		{name: "non-clean result has preview", mutate: func(result *ProcessorResult) { result.Code = ProcessorResultCodeMalware }},
		{name: "archive has preview", mutate: func(result *ProcessorResult) { result.Profile = ProcessorProfileArchive }},
		{name: "invalid preview Blob", mutate: func(result *ProcessorResult) { result.Preview.Blob.ObjectVersion = "" }},
		{name: "empty preview media type", mutate: func(result *ProcessorResult) { result.Preview.MediaType = "" }},
		{name: "oversized preview media type", mutate: func(result *ProcessorResult) {
			result.Preview.MediaType = strings.Repeat("a", 256)
		}},
		{name: "unknown preview media type", mutate: func(result *ProcessorResult) {
			result.Preview.MediaType = "application/octet-stream"
		}},
		{name: "image preview media mismatch", mutate: func(result *ProcessorResult) {
			result.Profile = ProcessorProfileImage
		}},
		{name: "PDF preview media mismatch", mutate: func(result *ProcessorResult) {
			result.Profile = ProcessorProfilePDF
		}},
		{name: "text preview media mismatch", mutate: func(result *ProcessorResult) {
			result.Preview.MediaType = ManagedPreviewMediaTypePNG
		}},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := base
			tt.mutate(&result)
			err := result.Validate()
			if !errors.Is(err, ErrInvalidProcessorResult) {
				t.Fatalf("ProcessorResult.Validate() error = %v, want ErrInvalidProcessorResult", err)
			}
			if !errors.Is(fmt.Errorf("commit processor result: %w", err), ErrInvalidProcessorResult) {
				t.Fatalf("wrapped ProcessorResult.Validate() error lost ErrInvalidProcessorResult: %v", err)
			}
			if _, digestErr := result.Digest(); !errors.Is(digestErr, ErrInvalidProcessorResult) {
				t.Fatalf("ProcessorResult.Digest() error = %v, want ErrInvalidProcessorResult", digestErr)
			}
		})
	}
}

func TestProcessorCompletionInputRejectsPreviewOutsideDurableLimits(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	base := ProcessorCompletionInput{
		Claim: ProcessorClaim{
			ProjectID: "default", ProcessorJobID: "apj_previewbounds1",
			UploadID: "aup_previewbounds1", AttachmentID: "att_previewbounds1",
			Source:  processorResultTestBlob(0x31, "source-v1", 17, BackendKindLocal),
			Profile: ProcessorProfileText, Attempt: 1, MaxAttempts: 3,
			OwnerID: "processor_previewbounds", OwnerGeneration: 1,
			LeaseExpiresAt: time.Date(2026, time.August, 5, 12, 5, 0, 0, time.UTC),
			ExpiresAt:      time.Date(2026, time.August, 5, 13, 0, 0, 0, time.UTC),
		},
		Result: validProcessorResultForTest(), Limits: limits,
	}
	base.Result.Source = base.Claim.Source

	tests := []struct {
		name   string
		mutate func(*ProcessorCompletionInput)
		valid  bool
	}{
		{name: "text at inline limit", mutate: func(input *ProcessorCompletionInput) {
			input.Result.Preview.Blob.SizeBytes = input.Limits.MaxInlineTextPreviewBytes
		}, valid: true},
		{name: "text above inline limit", mutate: func(input *ProcessorCompletionInput) {
			input.Result.Preview.Blob.SizeBytes = input.Limits.MaxInlineTextPreviewBytes + 1
		}},
		{name: "image at derived file limit", mutate: func(input *ProcessorCompletionInput) {
			input.Result.Profile = ProcessorProfileImage
			input.Result.Preview.MediaType = ManagedPreviewMediaTypePNG
			input.Result.Preview.Blob.SizeBytes = input.Limits.MaxFileBytes
		}, valid: true},
		{name: "image above derived file limit", mutate: func(input *ProcessorCompletionInput) {
			input.Result.Profile = ProcessorProfileImage
			input.Result.Preview.MediaType = ManagedPreviewMediaTypePNG
			input.Result.Preview.Blob.SizeBytes = input.Limits.MaxFileBytes + 1
		}},
		{name: "PDF above derived file limit", mutate: func(input *ProcessorCompletionInput) {
			input.Result.Profile = ProcessorProfilePDF
			input.Result.Preview.MediaType = ManagedPreviewMediaTypePNG
			input.Result.Preview.Blob.SizeBytes = input.Limits.MaxFileBytes + 1
		}},
		{name: "text profile media mismatch", mutate: func(input *ProcessorCompletionInput) {
			input.Result.Preview.MediaType = ManagedPreviewMediaTypePNG
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := base
			tt.mutate(&input)
			input.PreviewPublicationIntent = processorPreviewPublicationIntentForCompletion(input)
			err := input.Validate()
			if tt.valid {
				if err != nil {
					t.Fatalf("ProcessorCompletionInput.Validate() error = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, ErrInvalidProcessorCommand) {
				t.Fatalf("ProcessorCompletionInput.Validate() error = %v, want ErrInvalidProcessorCommand", err)
			}
			if !errors.Is(err, ErrInvalidProcessorResult) {
				t.Fatalf("ProcessorCompletionInput.Validate() error = %v, want ErrInvalidProcessorResult in chain", err)
			}
		})
	}
}

func processorPreviewPublicationIntentForCompletion(input ProcessorCompletionInput) BlobPublicationIntent {
	return BlobPublicationIntent{
		PublicationID: "bpi_previewintent1", ProjectID: input.Claim.ProjectID,
		OwnerKind: BlobPublicationOwnerProcessorPreview, OwnerID: input.Claim.ProcessorJobID,
		OwnerGeneration: input.Claim.OwnerGeneration,
		Target: BlobPublicationTarget{
			Key: input.Result.Preview.Blob.Key, SHA256: input.Result.Preview.Blob.SHA256,
			SizeBytes: input.Result.Preview.Blob.SizeBytes, BackendKind: input.Result.Preview.Blob.BackendKind,
		},
		ObjectVersion:    input.Result.Preview.Blob.ObjectVersion,
		State:            BlobPublicationStatePublished,
		PublishExpiresAt: input.Claim.ExpiresAt,
	}
}

func TestManagedPreviewByteLimitUsesInlineTextAndDerivedNonTextBounds(t *testing.T) {
	t.Parallel()

	limits := Limits{
		MaxFileBytes: 64, MaxRecordBytes: 128, MaxProjectBytes: 256,
		WarningPercent: 80, MaxInlineTextPreviewBytes: 8,
	}
	if err := limits.Validate(); err != nil {
		t.Fatalf("test limits error = %v", err)
	}
	if got, ok := managedPreviewByteLimit(ProcessorProfileText, limits); !ok || got != limits.MaxInlineTextPreviewBytes {
		t.Fatalf("text managed preview limit = %d/%t, want %d/true", got, ok, limits.MaxInlineTextPreviewBytes)
	}
	for _, profile := range []ProcessorProfile{ProcessorProfileImage, ProcessorProfilePDF} {
		if got, ok := managedPreviewByteLimit(profile, limits); !ok || got != limits.MaxFileBytes {
			t.Fatalf("%s managed preview limit = %d/%t, want %d/true", profile, got, ok, limits.MaxFileBytes)
		}
	}
}

func TestProcessorResultDigestIsVersionedDeterministicAndFieldBound(t *testing.T) {
	t.Parallel()

	base := validProcessorResultForTest()
	digest, err := base.Digest()
	if err != nil {
		t.Fatalf("ProcessorResult.Digest() error = %v", err)
	}
	equivalent := base
	equivalentDigest, err := equivalent.Digest()
	if err != nil {
		t.Fatalf("equivalent ProcessorResult.Digest() error = %v", err)
	}
	if digest != equivalentDigest {
		t.Fatalf("equal logical processor results have different digests: %x != %x", digest, equivalentDigest)
	}
	if got, want := hex.EncodeToString(digest[:]), "b3cb138f2f6efa59633bea0f68259ef225214854abde9e51fd8b813625aeb109"; got != want {
		t.Fatalf("ProcessorResult.Digest() = %q, want versioned canonical digest %q", got, want)
	}

	baseCanonical := processorResultCanonicalDigest(base)
	if baseCanonical != digest {
		t.Fatalf("validated digest = %x, canonical digest = %x", digest, baseCanonical)
	}
	mutations := []struct {
		name   string
		mutate func(*ProcessorResult)
	}{
		{name: "source key", mutate: func(result *ProcessorResult) { result.Source.Key += "x" }},
		{name: "source version", mutate: func(result *ProcessorResult) { result.Source.ObjectVersion = "source-v2" }},
		{name: "source hash", mutate: func(result *ProcessorResult) { result.Source.SHA256[0] ^= 0xff }},
		{name: "source size", mutate: func(result *ProcessorResult) { result.Source.SizeBytes++ }},
		{name: "source backend", mutate: func(result *ProcessorResult) { result.Source.BackendKind = BackendKindS3 }},
		{name: "profile", mutate: func(result *ProcessorResult) { result.Profile = ProcessorProfileImage }},
		{name: "code", mutate: func(result *ProcessorResult) { result.Code = ProcessorResultCodeMalware }},
		{name: "HasPreview", mutate: func(result *ProcessorResult) { result.HasPreview = false }},
		{name: "preview key", mutate: func(result *ProcessorResult) { result.Preview.Blob.Key += "x" }},
		{name: "preview version", mutate: func(result *ProcessorResult) { result.Preview.Blob.ObjectVersion = "preview-v3" }},
		{name: "preview hash", mutate: func(result *ProcessorResult) { result.Preview.Blob.SHA256[0] ^= 0xff }},
		{name: "preview size", mutate: func(result *ProcessorResult) { result.Preview.Blob.SizeBytes++ }},
		{name: "preview backend", mutate: func(result *ProcessorResult) { result.Preview.Blob.BackendKind = BackendKindLocal }},
		{name: "preview media type", mutate: func(result *ProcessorResult) { result.Preview.MediaType = ManagedPreviewMediaTypePNG }},
	}
	for _, tt := range mutations {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mutated := base
			tt.mutate(&mutated)
			if got := processorResultCanonicalDigest(mutated); got == baseCanonical {
				t.Fatalf("%s mutation did not change canonical digest %x", tt.name, got)
			}
		})
	}
}

func validProcessorResultForTest() ProcessorResult {
	return ProcessorResult{
		Source:     processorResultTestBlob(0x11, "source-v1", 17, BackendKindLocal),
		Profile:    ProcessorProfileText,
		Code:       ProcessorResultCodeClean,
		HasPreview: true,
		Preview: ManagedPreview{
			Blob:      processorResultTestBlob(0x22, "preview-v2", 11, BackendKindS3),
			MediaType: ManagedPreviewMediaTypeTextUTF8,
		},
	}
}

func processorResultTestBlob(fill byte, objectVersion string, sizeBytes int64, backend BackendKind) BlobObject {
	var digest [sha256.Size]byte
	for index := range digest {
		digest[index] = fill
	}
	return BlobObject{
		Key:           "sha256/" + hex.EncodeToString(digest[:]),
		SHA256:        digest,
		ObjectVersion: objectVersion,
		SizeBytes:     sizeBytes,
		BackendKind:   backend,
	}
}
