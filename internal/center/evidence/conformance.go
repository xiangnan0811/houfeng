package evidence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"mime"
	"path/filepath"
	"reflect"
	"strings"
	"unicode/utf8"

	"houfeng/internal/center/recordauth"
)

type ConformanceFixture struct {
	Actor      ActorScope
	Selection  Selection
	Intent     Intent
	Alignment  Alignment
	ExportMode ExportMode
}

// VerifyKindConformance provides the shared behavioral foundation used by
// each concrete adapter's focused conformance test. It never runs at startup.
func VerifyKindConformance(ctx context.Context, kind Kind, fixture ConformanceFixture) error {
	if ctx == nil || nilKind(kind) {
		return fmt.Errorf("%w: nil context or kind", ErrKindConformance)
	}
	descriptor := kind.Descriptor()
	if err := descriptor.Validate(); err != nil {
		return conformanceError("descriptor", err)
	}
	actor, err := recordauth.NormalizeActorScope(fixture.Actor)
	if err != nil || !bytes.Equal(actor.CanonicalBytes(), fixture.Actor.CanonicalBytes()) {
		return conformanceError("actor", recordauth.ErrInvalidActorScope)
	}
	if fixture.Selection.Key != descriptor.Key {
		return conformanceError("selection key", ErrInvalidKindDescriptor)
	}
	if err := kind.ValidateSelection(ctx, actor.Clone(), cloneSelection(fixture.Selection)); err != nil {
		return conformanceError("validate selection", err)
	}

	preview, err := kind.PreviewCapture(ctx, actor.Clone(), cloneSelection(fixture.Selection))
	if err != nil {
		return conformanceError("preview capture", err)
	}
	if err := validateConformancePreview(descriptor, fixture.Selection, preview); err != nil {
		return conformanceError("preview", err)
	}
	if preview.QuotaOutcome.Status != QuotaAllowed {
		return conformanceError("preview quota", ErrInvalidCanonicalPayload)
	}

	authorization, err := kind.Authorize(ctx, actor.Clone(), cloneSelection(fixture.Selection))
	if err != nil {
		return conformanceError("authorize", err)
	}
	normalizedAuthorization, err := normalizeCaptureAuthorization(fixture.Selection, authorization)
	if err != nil {
		return conformanceError("authorization scope", recordauth.ErrInvalidSourceAuthorization)
	}

	if err := validateConformanceIntent(descriptor, fixture.Selection, preview, fixture.Intent); err != nil {
		return conformanceError("intent", err)
	}
	snapshot, err := kind.Capture(ctx, actor.Clone(), cloneIntent(fixture.Intent))
	if err != nil {
		return conformanceError("capture", err)
	}
	if err := snapshot.Validate(descriptor); err != nil {
		return conformanceError("snapshot", err)
	}
	if err := validatePreviewCaptureAgreement(descriptor, fixture.Selection, preview, normalizedAuthorization, snapshot); err != nil {
		return conformanceError("snapshot drift", ErrInvalidSnapshotEnvelope)
	}

	summary := kind.Summarize(snapshot)
	if summary.Key != descriptor.Key || summary.RendererVersion != descriptor.Conformance.RendererVersion ||
		strings.TrimSpace(summary.Title) == "" || strings.TrimSpace(summary.SearchText) == "" || summary.ReadModel == nil {
		return conformanceError("summary", ErrInvalidCanonicalPayload)
	}
	if err := validateSafeStructuredValue(summary.Title, "summary.title"); err != nil {
		return conformanceError("summary", err)
	}
	if err := validateSafeStructuredValue(summary.SearchText, "summary.search_text"); err != nil {
		return conformanceError("summary", err)
	}
	if err := validateSafeStructuredValue(summary.ReadModel, "summary"); err != nil {
		return conformanceError("summary", err)
	}
	if !validVersionedReadModel(summary.ReadModel) {
		return conformanceError("summary", ErrInvalidCanonicalPayload)
	}

	if fixture.Alignment.Mode != AlignmentExact {
		return conformanceError("alignment", ErrInvalidCanonicalPayload)
	}
	comparison := kind.Compare(snapshot, snapshot, fixture.Alignment)
	if comparison.Key != descriptor.Key || !comparison.Compatible || comparison.Values == nil {
		return conformanceError("comparison", ErrInvalidCanonicalPayload)
	}
	if err := validateSafeStructuredValue(comparison.Reason, "comparison.reason"); err != nil {
		return conformanceError("comparison", err)
	}
	if err := validateSafeStructuredValue(comparison.Values, "comparison"); err != nil {
		return conformanceError("comparison", err)
	}

	if fixture.ExportMode != ExportModeSafe && fixture.ExportMode != ExportModeSensitiveTopology {
		return conformanceError("export mode", ErrInvalidCanonicalPayload)
	}
	export := kind.Export(snapshot, fixture.ExportMode)
	if export.Key != descriptor.Key || strings.TrimSpace(export.MediaType) == "" ||
		!safeExportFilename(export.Filename) || len(export.Bytes) == 0 || uint64(len(export.Bytes)) > MaxCanonicalPayloadBytes {
		return conformanceError("export", ErrInvalidCanonicalPayload)
	}
	if err := validateExportMaterial(export); err != nil {
		return conformanceError("export", err)
	}
	return nil
}

func validateExportMaterial(material ExportMaterial) error {
	mediaType, parameters, err := mime.ParseMediaType(material.MediaType)
	if err != nil {
		return ErrInvalidCanonicalPayload
	}
	if mediaType != "application/json" && !strings.HasSuffix(mediaType, "+json") {
		return ErrInvalidCanonicalPayload
	}
	for name, value := range parameters {
		if name != "charset" || !strings.EqualFold(value, "utf-8") {
			return ErrInvalidCanonicalPayload
		}
	}
	if err := validateSafeStructuredValue(material.Filename, "export.filename"); err != nil {
		return err
	}
	return validateSafeJSONBytes(material.Bytes, "export")
}

func validateSafeJSONBytes(encoded []byte, path string) error {
	if len(encoded) == 0 || uint64(len(encoded)) > MaxCanonicalPayloadBytes || !utf8.Valid(encoded) {
		return ErrInvalidCanonicalPayload
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	budget := &structuredValueBudget{}
	if err := walkSafeJSONTokens(decoder, path, 1, budget); err != nil {
		return err
	}
	return requireJSONEOF(decoder)
}

func walkSafeJSONTokens(decoder *json.Decoder, path string, depth uint64, budget *structuredValueBudget) error {
	token, err := decoder.Token()
	if err != nil {
		return ErrInvalidCanonicalPayload
	}
	if err := budget.addNode(depth, 16); err != nil {
		return err
	}
	switch typed := token.(type) {
	case json.Delim:
		switch typed {
		case '{':
			seen := make(map[string]struct{})
			entries := 0
			for decoder.More() {
				keyToken, keyErr := decoder.Token()
				key, ok := keyToken.(string)
				if keyErr != nil || !ok {
					return ErrInvalidCanonicalPayload
				}
				entries++
				if entries > maxCanonicalCollectionEntries {
					return canonicalResourceLimit("collection entries")
				}
				if err := budget.addCollection(1); err != nil {
					return err
				}
				if err := budget.addKeyBytes(key); err != nil {
					return err
				}
				if _, duplicate := seen[key]; duplicate {
					return fmt.Errorf("%w: duplicate export field", ErrInvalidCanonicalPayload)
				}
				seen[key] = struct{}{}
				childPath := path + "." + key
				if forbiddenFieldPath(childPath) {
					return ErrForbiddenField
				}
				if err := walkSafeJSONTokens(decoder, childPath, depth+1, budget); err != nil {
					return err
				}
			}
			closing, closeErr := decoder.Token()
			if closeErr != nil || closing != json.Delim('}') {
				return ErrInvalidCanonicalPayload
			}
		case '[':
			entries := 0
			for decoder.More() {
				entries++
				if entries > maxCanonicalCollectionEntries {
					return canonicalResourceLimit("collection entries")
				}
				if err := budget.addCollection(1); err != nil {
					return err
				}
				if err := walkSafeJSONTokens(decoder, path, depth+1, budget); err != nil {
					return err
				}
			}
			closing, closeErr := decoder.Token()
			if closeErr != nil || closing != json.Delim(']') {
				return ErrInvalidCanonicalPayload
			}
		default:
			return ErrInvalidCanonicalPayload
		}
	case string:
		if !utf8.ValidString(typed) || len(typed) > maxCanonicalStringBytes {
			return ErrInvalidCanonicalPayload
		}
		if err := budget.addStringBytes(uint64(len(typed))); err != nil {
			return err
		}
		if forbiddenStringContent(typed) {
			return ErrForbiddenField
		}
	case json.Number:
		if _, err := normalizeJSONNumber(typed); err != nil {
			return err
		}
	case bool, nil:
	default:
		return ErrInvalidCanonicalPayload
	}
	return nil
}

func validateConformancePreview(descriptor Descriptor, selection Selection, preview Preview) error {
	if preview.Key != descriptor.Key || preview.Selection.Key != descriptor.Key ||
		!reflect.DeepEqual(cloneSelection(preview.Selection), cloneSelection(selection)) ||
		!ValidCaptureIntentID(preview.IntentID) {
		return ErrInvalidCanonicalPayload
	}
	preview.RequestedWindow = normalizeWindow(preview.RequestedWindow)
	preview.ActualWindow = normalizeWindow(preview.ActualWindow)
	if err := validateWindow(preview.RequestedWindow); err != nil ||
		preview.RequestedWindow != normalizeWindow(selection.RequestedWindow) {
		return ErrInvalidCanonicalPayload
	}
	if err := validateWindow(preview.ActualWindow); err != nil ||
		preview.ActualWindow.Start.Before(preview.RequestedWindow.Start) ||
		preview.ActualWindow.End.After(preview.RequestedWindow.End) {
		return ErrInvalidCanonicalPayload
	}
	if err := validateIdentitySnapshot(preview.Subject); err != nil || validateIdentitySnapshot(preview.Source) != nil ||
		preview.Source.Type != selection.SourceType || preview.Source.ID != selection.SourceID {
		return ErrInvalidCanonicalPayload
	}
	if normalizeTime(preview.ObservedAt).IsZero() ||
		(strings.TrimSpace(preview.SourceRevision) == "" && strings.TrimSpace(preview.SourceWatermark) == "") ||
		strings.TrimSpace(preview.ProducerVersion) == "" || strings.TrimSpace(preview.CalculationVersion) == "" ||
		validateUnitsSemantics(preview.Units) != nil || validateQuality(preview.Quality) != nil ||
		(preview.Sensitivity != SensitivityNormal && preview.Sensitivity != SensitivitySensitiveTopology) ||
		preview.EstimatedCanonicalBytes == 0 || preview.EstimatedCanonicalBytes > descriptor.Conformance.MaxCanonicalBytes ||
		preview.SourceDigest == [sha256.Size]byte{} || preview.RendererVersion != descriptor.Conformance.RendererVersion {
		return ErrInvalidCanonicalPayload
	}
	if validateDurationSemantics(preview.ActualPrecision) != nil || validateDurationSemantics(preview.BucketWidth) != nil ||
		validatePreviewQuotaOutcome(preview.QuotaOutcome) != nil || validateRetentionSemantics(preview.Retention) != nil {
		return ErrInvalidCanonicalPayload
	}
	if len(preview.Redaction) == 0 {
		return ErrInvalidCanonicalPayload
	}
	definitions := make(map[string]FieldDefinition, len(descriptor.Fields))
	for _, definition := range descriptor.Fields {
		definitions[definition.Path] = definition
	}
	derivedSensitivity := SensitivityNormal
	seenDecisions := make(map[string]struct{}, len(preview.Redaction))
	for _, decision := range preview.Redaction {
		definition, exists := definitions[decision.Path]
		if !exists || definition.Sensitivity != decision.Sensitivity || !knownRedactionAction(decision.Action) {
			return ErrInvalidCanonicalPayload
		}
		if _, duplicate := seenDecisions[decision.Path]; duplicate {
			return ErrInvalidCanonicalPayload
		}
		seenDecisions[decision.Path] = struct{}{}
		switch decision.Sensitivity {
		case SensitivityNormal:
			if decision.Action != RedactionActionIncluded {
				return ErrInvalidCanonicalPayload
			}
		case SensitivitySensitiveTopology:
			if decision.Action != RedactionActionIncluded && decision.Action != RedactionActionStripped && decision.Action != RedactionActionMasked {
				return ErrInvalidCanonicalPayload
			}
			if decision.Action == RedactionActionIncluded {
				derivedSensitivity = SensitivitySensitiveTopology
			}
		case SensitivityForbidden:
			if decision.Action != RedactionActionForbidden {
				return ErrInvalidCanonicalPayload
			}
		}
	}
	if preview.Sensitivity != derivedSensitivity {
		return ErrInvalidCanonicalPayload
	}
	previewedAt := normalizeTime(preview.PreviewedAt)
	validUntil := normalizeTime(preview.ValidUntil)
	if previewedAt.IsZero() || validUntil.IsZero() || !validUntil.After(previewedAt) || validUntil.Sub(previewedAt) > CaptureIntentTTL {
		return ErrInvalidCanonicalPayload
	}
	return nil
}

func validateConformanceIntent(descriptor Descriptor, selection Selection, preview Preview, intent Intent) error {
	if intent.ID != preview.IntentID || intent.Key != descriptor.Key ||
		!reflect.DeepEqual(cloneSelection(intent.Selection), cloneSelection(selection)) ||
		intent.PreviewDigest == [sha256.Size]byte{} || normalizeTime(intent.ValidUntil) != normalizeTime(preview.ValidUntil) {
		return ErrInvalidCanonicalPayload
	}
	return nil
}

func validateSafeStructuredValue(value any, path string) error {
	normalized, err := normalizeStructuredValue(value)
	if err != nil {
		return err
	}
	return walkSafeStructuredValue(normalized, path)
}

func walkSafeStructuredValue(value any, path string) error {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			childPath := path + "." + key
			if forbiddenFieldPath(childPath) {
				return ErrForbiddenField
			}
			if err := walkSafeStructuredValue(item, childPath); err != nil {
				return err
			}
		}
	case []any:
		for _, item := range typed {
			if err := walkSafeStructuredValue(item, path); err != nil {
				return err
			}
		}
	case string:
		if forbiddenStringContent(typed) {
			return ErrForbiddenField
		}
	}
	return nil
}

func conformanceError(stage string, err error) error {
	return fmt.Errorf("%w: %s: %w", ErrKindConformance, stage, err)
}

// ValidCaptureIntentID reports whether value uses the only capture-intent
// identity grammar accepted by both evidence behavior and persistence.
func ValidCaptureIntentID(value string) bool {
	if len(value) != len("evi_")+24 || !strings.HasPrefix(value, "evi_") {
		return false
	}
	for _, character := range value[len("evi_"):] {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func safeExportFilename(value string) bool {
	return value != "" && filepath.Base(value) == value && value != "." && value != ".." && !strings.ContainsAny(value, "\x00/\\")
}

func knownRedactionAction(action RedactionAction) bool {
	switch action {
	case RedactionActionIncluded, RedactionActionStripped, RedactionActionMasked, RedactionActionForbidden:
		return true
	default:
		return false
	}
}

func cloneSelection(selection Selection) Selection {
	selection.Metrics = append([]string(nil), selection.Metrics...)
	selection.SensitiveTopologyFields = append([]string(nil), selection.SensitiveTopologyFields...)
	selection.RequestedWindow = normalizeWindow(selection.RequestedWindow)
	return selection
}

func cloneIntent(intent Intent) Intent {
	intent.Selection = cloneSelection(intent.Selection)
	intent.ValidUntil = normalizeTime(intent.ValidUntil)
	return intent
}
