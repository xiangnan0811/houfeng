package evidence

import (
	"context"
	"fmt"
)

const (
	comparisonResultProducerVersion = "comparison-result/v1"
	comparisonResultRendererVersion = "comparison_result_v1"
	comparisonResultReadModel       = "comparison_result_read_model/v1"
	comparisonResultComparison      = "comparison_result_comparison/v1"
)

type ComparisonResultKind struct {
	descriptor Descriptor
}

func NewComparisonResultKind() (*ComparisonResultKind, error) {
	descriptor := comparisonResultDescriptor()
	if err := descriptor.Validate(); err != nil {
		return nil, err
	}
	return &ComparisonResultKind{descriptor: descriptor}, nil
}

func (kind *ComparisonResultKind) Descriptor() Descriptor { return kind.descriptor }

func (*ComparisonResultKind) ValidateSelection(context.Context, ActorScope, Selection) error {
	return fmt.Errorf("%w: comparison result is derived", ErrInvalidCanonicalPayload)
}

func (*ComparisonResultKind) PreviewCapture(context.Context, ActorScope, Selection) (Preview, error) {
	return Preview{}, fmt.Errorf("%w: comparison result is derived", ErrInvalidCanonicalPayload)
}

func (*ComparisonResultKind) Capture(context.Context, ActorScope, Intent) (CanonicalSnapshot, error) {
	return CanonicalSnapshot{}, fmt.Errorf("%w: comparison result is derived", ErrInvalidCanonicalPayload)
}

func (*ComparisonResultKind) Authorize(context.Context, ActorScope, Selection) (AuthorizationScope, error) {
	return AuthorizationScope{}, fmt.Errorf("%w: comparison result is derived", ErrInvalidCanonicalPayload)
}

func (kind *ComparisonResultKind) Summarize(snapshot CanonicalSnapshot) Summary {
	summary := Summary{Key: kind.descriptor.Key, RendererVersion: kind.descriptor.Conformance.RendererVersion}
	if err := snapshot.Validate(kind.descriptor); err != nil {
		return summary
	}
	payload, err := decodeSnapshotPayload(snapshot.Bytes())
	if err != nil {
		return summary
	}
	items := allowlistedComparisonResultItems(payload["items"])
	kinds := allowlistedStringList(payload["available_kinds"])
	summary.Title = "Comparison result"
	summary.SearchText = "comparison result " + stringValue(payload["alignment"]) + " " + stringValue(payload["digest"])
	summary.ReadModel = map[string]any{
		"version":             comparisonResultReadModel,
		"baseline_index":      payload["baseline_index"],
		"alignment":           payload["alignment"],
		"requested_from":      payload["requested_from"],
		"requested_to":        payload["requested_to"],
		"tolerance_seconds":   payload["tolerance_seconds"],
		"digest":              payload["digest"],
		"registry_version":    payload["registry_version"],
		"calculation_version": payload["calculation_version"],
		"items":               items,
		"warnings":            allowlistedComparisonResultWarnings(payload["warnings"]),
		"system_differences":  allowlistedComparisonResultDifferences(payload["system_differences"]),
		"available_kinds":     kinds,
		"copied_snapshot_ids": copiedSnapshotIDs(items),
	}
	if bucket := payload["bucket_seconds"]; bucket != nil {
		summary.ReadModel["bucket_seconds"] = bucket
	}
	return summary
}

func (kind *ComparisonResultKind) Compare(left, right CanonicalSnapshot, alignment Alignment) Comparison {
	compatible, reason := false, "invalid or non-exact comparison result alignment"
	values := map[string]any{"version": comparisonResultComparison}
	if alignment.Mode == AlignmentExact && left.Validate(kind.descriptor) == nil && right.Validate(kind.descriptor) == nil {
		leftEnvelope, rightEnvelope := left.Envelope(), right.Envelope()
		if leftEnvelope.Key == kind.descriptor.Key && rightEnvelope.Key == kind.descriptor.Key &&
			leftEnvelope.CalculationVersion == rightEnvelope.CalculationVersion {
			compatible = true
			reason = "exact compatible comparison result semantics"
			values["left_hash"] = fmt.Sprintf("%x", left.Hash())
			values["right_hash"] = fmt.Sprintf("%x", right.Hash())
			values["equal"] = left.Hash() == right.Hash()
		} else {
			reason = "incompatible comparison result semantics"
		}
	}
	return Comparison{Key: kind.descriptor.Key, Compatible: compatible, Reason: reason, Values: values}
}

func (kind *ComparisonResultKind) Export(snapshot CanonicalSnapshot, mode ExportMode) ExportMaterial {
	if (mode != ExportModeSafe && mode != ExportModeSensitiveTopology) || snapshot.Validate(kind.descriptor) != nil {
		return ExportMaterial{Key: kind.descriptor.Key}
	}
	filename := string(kind.descriptor.Key.Kind) + "_v1.json"
	return ExportMaterial{
		Key: kind.descriptor.Key, MediaType: "application/json", Filename: filename, Bytes: snapshot.Bytes(),
	}
}

func comparisonResultDescriptor() Descriptor {
	normal := []string{
		"version", "baseline_index", "alignment", "requested_from", "requested_to",
		"tolerance_seconds", "bucket_seconds", "digest", "registry_version", "calculation_version",
		"items.original_snapshot_id", "items.copied_snapshot_id", "items.hash", "items.kind",
		"items.revision_context", "items.record_id", "items.revision_id",
		"items.record_type", "items.business_status", "items.status_group",
		"items.impact_level", "items.occurred_at",
		"warnings.item_index", "warnings.kind", "warnings.reason",
		"system_differences.item_index", "system_differences.kind", "system_differences.compatible",
		"system_differences.reason", "system_differences.left_hash", "system_differences.right_hash",
		"system_differences.equal", "system_differences.matched",
		"system_differences.unmatched_baseline", "system_differences.unmatched_item",
		"system_differences.deltas.baseline_start", "system_differences.deltas.baseline_end",
		"system_differences.deltas.item_start", "system_differences.deltas.item_end",
		"system_differences.deltas.baseline_value", "system_differences.deltas.item_value",
		"system_differences.deltas.delta",
		"available_kinds",
	}
	forbidden := []string{
		"conclusion", "markdown", "body_markdown", "title",
		"token", "secret", "password", "details", "stdout", "stderr", "output", "raw_json",
	}
	fields := make([]FieldDefinition, 0, len(normal)+len(forbidden))
	for _, path := range normal {
		fields = append(fields, FieldDefinition{Path: path, Sensitivity: SensitivityNormal})
	}
	for _, path := range forbidden {
		fields = append(fields, FieldDefinition{Path: path, Sensitivity: SensitivityForbidden})
	}
	return Descriptor{
		Key:    ComparisonResultV1Key(),
		Fields: fields,
		Conformance: ConformanceMetadata{
			CanonicalizationVersion: CanonicalizationVersionV1,
			ForbiddenCorpusVersion:  ForbiddenCorpusVersionV1,
			RendererVersion:         comparisonResultRendererVersion,
			MaxCanonicalBytes:       MaxCanonicalPayloadBytes,
		},
	}
}

func allowlistedComparisonResultItems(value any) []any {
	return allowlistedObjects(value, []string{
		"original_snapshot_id", "copied_snapshot_id", "hash", "kind", "revision_context",
		"record_id", "revision_id",
		"record_type", "business_status", "status_group", "impact_level", "occurred_at",
	})
}

func allowlistedComparisonResultWarnings(value any) []any {
	return allowlistedObjects(value, []string{"item_index", "kind", "reason"})
}

func allowlistedComparisonResultDifferences(value any) []any {
	items := allowlistedObjects(value, []string{
		"item_index", "kind", "compatible", "reason", "left_hash", "right_hash", "equal",
		"matched", "unmatched_baseline", "unmatched_item", "deltas",
	})
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if _, exists := object["deltas"]; exists {
			object["deltas"] = allowlistedObjects(object["deltas"], []string{
				"baseline_start", "baseline_end", "item_start", "item_end",
				"baseline_value", "item_value", "delta",
			})
		}
	}
	return items
}

func allowlistedStringList(value any) []any {
	items, ok := value.([]any)
	if !ok {
		return []any{}
	}
	out := make([]any, 0, len(items))
	for _, item := range items {
		if text := stringValue(item); text != "" {
			out = append(out, text)
		}
	}
	return out
}

func copiedSnapshotIDs(items []any) []any {
	out := make([]any, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if id := stringValue(object["copied_snapshot_id"]); id != "" {
			out = append(out, id)
		}
	}
	return out
}

func allowlistedObjects(value any, fields []string) []any {
	items, ok := value.([]any)
	if !ok {
		return []any{}
	}
	out := make([]any, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		filtered := make(map[string]any, len(fields))
		for _, field := range fields {
			if value, exists := object[field]; exists {
				filtered[field] = value
			}
		}
		out = append(out, filtered)
	}
	return out
}
