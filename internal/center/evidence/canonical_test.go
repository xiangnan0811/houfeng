package evidence

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestCanonicalDeterministicBytesAndHash(t *testing.T) {
	descriptor := testDescriptor(t, MonitoringProbeV2Key())
	left := map[string]any{
		"tags":         map[string]any{"z": "last", "a": "first"},
		"metric_value": 12.5,
		"metric_name":  "latency_ms",
	}
	right := map[string]any{
		"metric_name":  "latency_ms",
		"metric_value": 12.5,
		"tags":         map[string]any{"a": "first", "z": "last"},
	}

	first, _, err := CanonicalizePayload(descriptor, left, RedactionIncludeSensitiveTopology)
	if err != nil {
		t.Fatalf("CanonicalizePayload(left) error = %v", err)
	}
	second, _, err := CanonicalizePayload(descriptor, right, RedactionIncludeSensitiveTopology)
	if err != nil {
		t.Fatalf("CanonicalizePayload(right) error = %v", err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) || first.Hash() != second.Hash() {
		t.Fatalf("canonical payloads differ:\nleft:  %s\nright: %s", first.Bytes(), second.Bytes())
	}
	want := `{"canonicalization_version":1,"kind":"monitoring.probe","schema_version":2,"payload":{"metric_name":"latency_ms","metric_value":12.5,"tags":{"a":"first","z":"last"}}}`
	if got := string(first.Bytes()); got != want {
		t.Fatalf("canonical bytes = %s, want %s", got, want)
	}
	const wantHash = "b89eda11f7177dd33103a5012bf2244fed8d2d235af534ff30c44299b7917d09"
	gotHash := first.Hash()
	if got := hex.EncodeToString(gotHash[:]); got != wantHash {
		t.Fatalf("canonical hash = %s, want %s", got, wantHash)
	}

	cloned := first.Bytes()
	cloned[0] = '!'
	if first.Bytes()[0] == '!' {
		t.Fatal("CanonicalPayload.Bytes() did not return a defensive copy")
	}
}

func TestCanonicalSnapshotValidatesEnvelopeAndPayload(t *testing.T) {
	descriptor := testDescriptor(t, MonitoringProbeV2Key())
	envelope := testEnvelope(t, descriptor.Key)
	snapshot, report, err := NewCanonicalSnapshot(
		descriptor,
		envelope,
		map[string]any{"metric_name": "latency_ms", "metric_value": 12.5},
		RedactionNormalOnly,
	)
	if err != nil {
		t.Fatalf("NewCanonicalSnapshot() error = %v", err)
	}
	if len(report.Decisions) != 2 {
		t.Fatalf("redaction decisions = %#v", report.Decisions)
	}
	if err := snapshot.Validate(descriptor); err != nil {
		t.Fatalf("snapshot.Validate() error = %v", err)
	}
	gotEnvelope := snapshot.Envelope()
	if gotEnvelope.CanonicalHash != snapshot.Hash() || gotEnvelope.CanonicalSize != uint64(len(snapshot.Bytes())) {
		t.Fatalf("envelope digest/size = %x/%d, payload = %x/%d", gotEnvelope.CanonicalHash, gotEnvelope.CanonicalSize, snapshot.Hash(), len(snapshot.Bytes()))
	}
	if gotEnvelope.SourceDigest == [sha256.Size]byte{} || !reflect.DeepEqual(gotEnvelope.Redaction, report.Decisions) {
		t.Fatalf("envelope source digest/redaction = %x/%#v, want %#v", gotEnvelope.SourceDigest, gotEnvelope.Redaction, report.Decisions)
	}
}

func TestCanonicalRejectsSizeLimitAndUnknownVersion(t *testing.T) {
	descriptor := testDescriptor(t, MonitoringHostV1Key())
	baseline, _, err := CanonicalizePayload(descriptor, map[string]any{"metric_name": "cpu"}, RedactionNormalOnly)
	if err != nil {
		t.Fatalf("CanonicalizePayload() error = %v", err)
	}
	descriptor.Conformance.MaxCanonicalBytes = uint64(len(baseline.Bytes()) - 1)
	if _, _, err := CanonicalizePayload(descriptor, map[string]any{"metric_name": "cpu"}, RedactionNormalOnly); !errors.Is(err, ErrCanonicalPayloadTooLarge) {
		t.Fatalf("CanonicalizePayload(over limit) error = %v, want ErrCanonicalPayloadTooLarge", err)
	}

	unknown := []byte(`{"canonicalization_version":1,"kind":"monitoring.probe","schema_version":99,"payload":{"metric_name":"latency_ms"}}`)
	if _, err := DecodeCanonicalPayload(testDescriptor(t, MonitoringProbeV2Key()), unknown); !errors.Is(err, ErrUnknownKindVersion) {
		t.Fatalf("DecodeCanonicalPayload(unknown version) error = %v, want ErrUnknownKindVersion", err)
	}

	nonCanonicalNumber := []byte(`{"canonicalization_version":1,"kind":"monitoring.probe","schema_version":2,"payload":{"metric_value":1.0}}`)
	if _, err := DecodeCanonicalPayload(testDescriptor(t, MonitoringProbeV2Key()), nonCanonicalNumber); !errors.Is(err, ErrInvalidCanonicalPayload) {
		t.Fatalf("DecodeCanonicalPayload(non-canonical number) error = %v, want ErrInvalidCanonicalPayload", err)
	}
}

func TestCanonicalPreservesExactBoundedDecimalSemantics(t *testing.T) {
	descriptor := testDescriptor(t, MonitoringProbeV2Key())
	tests := []struct {
		name  string
		input json.Number
		want  string
	}{
		{name: "integer-valued decimal above binary exact range", input: "9007199254740993.0", want: "9007199254740993"},
		{name: "equivalent exponent", input: "9.007199254740993e15", want: "9007199254740993"},
		{name: "fractional exponent", input: "1.2300e-2", want: "0.0123"},
		{name: "negative zero", input: "-0.000e20", want: "0"},
		{name: "digit boundary", input: json.Number(strings.Repeat("9", 128)), want: strings.Repeat("9", 128)},
		{name: "integer exponent boundary", input: "1e127", want: "1" + strings.Repeat("0", 127)},
		{name: "scale boundary", input: "1e-64", want: "0." + strings.Repeat("0", 63) + "1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			canonical, _, err := CanonicalizePayload(descriptor, map[string]any{
				"metric_name":  "cost",
				"metric_value": tt.input,
			}, RedactionNormalOnly)
			if err != nil {
				t.Fatalf("CanonicalizePayload(%q) error = %v", tt.input, err)
			}
			if got := string(canonical.Bytes()); !strings.Contains(got, `"metric_value":`+tt.want) {
				t.Fatalf("canonical decimal = %s, want %s", got, tt.want)
			}
		})
	}

	exact, _, err := CanonicalizePayload(descriptor, map[string]any{"metric_name": "cost", "metric_value": json.Number("9007199254740993.0")}, RedactionNormalOnly)
	if err != nil {
		t.Fatalf("CanonicalizePayload(exact decimal) error = %v", err)
	}
	nearby, _, err := CanonicalizePayload(descriptor, map[string]any{"metric_name": "cost", "metric_value": json.Number("9007199254740992.0")}, RedactionNormalOnly)
	if err != nil {
		t.Fatalf("CanonicalizePayload(nearby decimal) error = %v", err)
	}
	if exact.Hash() == nearby.Hash() || bytes.Equal(exact.Bytes(), nearby.Bytes()) {
		t.Fatalf("distinct exact decimals collided: %s vs %s", exact.Bytes(), nearby.Bytes())
	}
	const wantHash = "04ef3e68baa6efbb7920c0318a04a08b054183c5427260452a3baf2254b78234"
	exactHash := exact.Hash()
	if got := hex.EncodeToString(exactHash[:]); got != wantHash {
		t.Fatalf("exact decimal hash = %s, want %s", got, wantHash)
	}
}

func TestCanonicalRejectsDecimalsOutsideExactContract(t *testing.T) {
	descriptor := testDescriptor(t, MonitoringProbeV2Key())
	for _, value := range []json.Number{
		json.Number(strings.Repeat("9", 129)),
		json.Number("1e-65"),
		json.Number("1e128"),
	} {
		if _, _, err := CanonicalizePayload(descriptor, map[string]any{
			"metric_name": "cost", "metric_value": value,
		}, RedactionNormalOnly); !errors.Is(err, ErrInvalidCanonicalPayload) {
			t.Fatalf("CanonicalizePayload(%q) error = %v, want ErrInvalidCanonicalPayload", value, err)
		}
	}
}

func TestCanonicalAndOutboundNormalizationAcceptOrdinaryGoDTOs(t *testing.T) {
	type evidenceDTO struct {
		Count  uint64   `json:"count"`
		Rate   float64  `json:"rate"`
		Labels []string `json:"labels"`
	}
	descriptor := testDescriptor(t, MonitoringProbeV2Key())
	descriptor.Fields = append(descriptor.Fields,
		FieldDefinition{Path: "count", Sensitivity: SensitivityNormal},
		FieldDefinition{Path: "rate", Sensitivity: SensitivityNormal},
		FieldDefinition{Path: "labels", Sensitivity: SensitivityNormal},
	)
	dto := evidenceDTO{Count: math.MaxUint64, Rate: 12.5, Labels: []string{"one", "two"}}
	canonical, _, err := CanonicalizePayload(descriptor, dto, RedactionNormalOnly)
	if err != nil {
		t.Fatalf("CanonicalizePayload(DTO) error = %v", err)
	}
	for _, want := range []string{`"count":18446744073709551615`, `"rate":12.5`, `"labels":["one","two"]`} {
		if !strings.Contains(string(canonical.Bytes()), want) {
			t.Fatalf("canonical DTO = %s, want %s", canonical.Bytes(), want)
		}
	}
	if err := validateSafeStructuredValue(dto, "summary"); err != nil {
		t.Fatalf("validateSafeStructuredValue(DTO) error = %v", err)
	}
}

type canonicalTestCustomMarshaler struct {
	called      *bool
	outputBytes int
}

func (value *canonicalTestCustomMarshaler) MarshalJSON() ([]byte, error) {
	*value.called = true
	if value.outputBytes > 0 {
		return []byte(`"` + strings.Repeat("x", value.outputBytes) + `"`), nil
	}
	return []byte(`{"safe":true}`), nil
}

type canonicalTestTextMarshaler struct {
	called      *bool
	outputBytes int
}

func (value *canonicalTestTextMarshaler) MarshalText() ([]byte, error) {
	*value.called = true
	return []byte(strings.Repeat("x", value.outputBytes)), nil
}

type canonicalTestEmbeddedSummaryDTO struct {
	Value canonicalTestCustomMarshaler `json:"value"`
}

type canonicalTestEmbeddedSummary struct {
	canonicalTestEmbeddedSummaryDTO
}

func TestStructuredNormalizationRejectsCustomMarshalerBeforeInvocation(t *testing.T) {
	called := false
	err := validateSafeStructuredValue(&canonicalTestCustomMarshaler{called: &called}, "summary")
	if !errors.Is(err, ErrInvalidCanonicalPayload) {
		t.Fatalf("validateSafeStructuredValue(custom marshaler) error = %v, want ErrInvalidCanonicalPayload", err)
	}
	if called {
		t.Fatal("validateSafeStructuredValue invoked custom marshaler before bounding its output")
	}
}

func TestStructuredNormalizationRejectsAddressablePointerMarshalersBeforeOversizeInvocation(t *testing.T) {
	t.Run("JSON marshaler nested struct field", func(t *testing.T) {
		type summaryDTO struct {
			Value canonicalTestCustomMarshaler `json:"value"`
		}
		called := false
		err := validateSafeStructuredValue(&summaryDTO{Value: canonicalTestCustomMarshaler{
			called:      &called,
			outputBytes: maxCanonicalEstimatedInputWorkBytes + 1,
		}}, "summary")
		if !errors.Is(err, ErrInvalidCanonicalPayload) {
			t.Fatalf("validateSafeStructuredValue(addressable JSON marshaler) error = %v, want ErrInvalidCanonicalPayload", err)
		}
		if called {
			t.Fatal("validateSafeStructuredValue invoked an addressable pointer JSON marshaler before bounding its output")
		}
	})

	t.Run("text marshaler nested slice element", func(t *testing.T) {
		called := false
		err := validateSafeStructuredValue([]canonicalTestTextMarshaler{{
			called:      &called,
			outputBytes: maxCanonicalEstimatedInputWorkBytes + 1,
		}}, "summary")
		if !errors.Is(err, ErrInvalidCanonicalPayload) {
			t.Fatalf("validateSafeStructuredValue(addressable text marshaler) error = %v, want ErrInvalidCanonicalPayload", err)
		}
		if called {
			t.Fatal("validateSafeStructuredValue invoked an addressable pointer text marshaler before bounding its output")
		}
	})

	t.Run("JSON marshaler nested unexported anonymous struct", func(t *testing.T) {
		called := false
		err := validateSafeStructuredValue(&canonicalTestEmbeddedSummary{
			canonicalTestEmbeddedSummaryDTO: canonicalTestEmbeddedSummaryDTO{
				Value: canonicalTestCustomMarshaler{
					called:      &called,
					outputBytes: maxCanonicalEstimatedInputWorkBytes + 1,
				},
			},
		}, "summary")
		if !errors.Is(err, ErrInvalidCanonicalPayload) {
			t.Fatalf("validateSafeStructuredValue(embedded JSON marshaler) error = %v, want ErrInvalidCanonicalPayload", err)
		}
		if called {
			t.Fatal("validateSafeStructuredValue invoked a pointer JSON marshaler through an unexported anonymous struct")
		}
	})
}

func TestStructuredNormalizationBoundsAcyclicIndirectionWithoutCustomMarshalers(t *testing.T) {
	t.Run("pointer depth exact limit and plus one", func(t *testing.T) {
		atLimit := nestedPointerStructuredValue(maxCanonicalNestingDepth - 1)
		if err := validateSafeStructuredValue(atLimit, "summary"); err != nil {
			t.Fatalf("validateSafeStructuredValue(pointer depth limit) error = %v", err)
		}
		budget := &structuredValueBudget{activeReferences: make(map[structuredVisit]struct{})}
		if err := budget.inspect(reflect.ValueOf(atLimit), 1); err != nil {
			t.Fatalf("inspect(pointer depth limit) error = %v", err)
		}
		if budget.nodes != maxCanonicalNestingDepth {
			t.Fatalf("inspect(pointer depth limit) nodes = %d, want %d", budget.nodes, maxCanonicalNestingDepth)
		}
		wantWork := uint64(maxCanonicalNestingDepth-1)*structuredIndirectionEstimatedWorkBytes + 16
		if budget.estimatedWork != wantWork {
			t.Fatalf("inspect(pointer depth limit) estimated work = %d, want %d", budget.estimatedWork, wantWork)
		}

		if err := validateSafeStructuredValue(nestedPointerStructuredValue(maxCanonicalNestingDepth), "summary"); !errors.Is(err, ErrInvalidCanonicalPayload) || !strings.Contains(err.Error(), "nesting depth") {
			t.Fatalf("validateSafeStructuredValue(pointer depth + 1) error = %v, want nesting depth limit", err)
		}
	})

	t.Run("interface depth exact limit and plus one", func(t *testing.T) {
		atLimit := nestedInterfaceStructuredValue(maxCanonicalNestingDepth/2, nil)
		if err := validateSafeStructuredValue(atLimit, "summary"); err != nil {
			t.Fatalf("validateSafeStructuredValue(interface depth limit) error = %v", err)
		}
		budget := &structuredValueBudget{activeReferences: make(map[structuredVisit]struct{})}
		if err := budget.inspect(reflect.ValueOf(atLimit), 1); err != nil {
			t.Fatalf("inspect(interface depth limit) error = %v", err)
		}
		if budget.nodes != maxCanonicalNestingDepth {
			t.Fatalf("inspect(interface depth limit) nodes = %d, want %d", budget.nodes, maxCanonicalNestingDepth)
		}
		nilInterface := reflect.ValueOf(canonicalTestInterfaceNode{}).Field(0)
		interfaceBudget := &structuredValueBudget{activeReferences: make(map[structuredVisit]struct{})}
		if err := interfaceBudget.inspect(nilInterface, 1); err != nil {
			t.Fatalf("inspect(nil interface) error = %v", err)
		}
		if interfaceBudget.nodes != 1 || interfaceBudget.estimatedWork != structuredIndirectionEstimatedWorkBytes {
			t.Fatalf("inspect(nil interface) nodes/work = %d/%d, want 1/%d", interfaceBudget.nodes, interfaceBudget.estimatedWork, structuredIndirectionEstimatedWorkBytes)
		}

		plusOne := nestedInterfaceStructuredValue(maxCanonicalNestingDepth/2, true)
		if err := validateSafeStructuredValue(plusOne, "summary"); !errors.Is(err, ErrInvalidCanonicalPayload) || !strings.Contains(err.Error(), "nesting depth") {
			t.Fatalf("validateSafeStructuredValue(interface depth + 1) error = %v, want nesting depth limit", err)
		}
	})

	t.Run("ordinary nil DTO fields", func(t *testing.T) {
		type summaryDTO struct {
			Optional *string `json:"optional"`
			Metadata any     `json:"metadata"`
		}
		if err := validateSafeStructuredValue(summaryDTO{}, "summary"); err != nil {
			t.Fatalf("validateSafeStructuredValue(nil DTO fields) error = %v", err)
		}
	})
}

func TestStructuredNormalizationResourceBoundaries(t *testing.T) {
	t.Run("nesting limit plus one", func(t *testing.T) {
		atLimit := nestedStructuredValue(31)
		if err := validateSafeStructuredValue(atLimit, "summary"); err != nil {
			t.Fatalf("validateSafeStructuredValue(depth limit) error = %v", err)
		}
		if err := validateSafeStructuredValue(nestedStructuredValue(32), "summary"); !errors.Is(err, ErrInvalidCanonicalPayload) {
			t.Fatalf("validateSafeStructuredValue(depth + 1) error = %v, want ErrInvalidCanonicalPayload", err)
		}
	})

	t.Run("per collection limit plus one", func(t *testing.T) {
		if err := validateSafeStructuredValue(make([]bool, 50_000), "summary"); err != nil {
			t.Fatalf("validateSafeStructuredValue(collection limit) error = %v", err)
		}
		if err := validateSafeStructuredValue(make([]bool, 50_001), "summary"); !errors.Is(err, ErrInvalidCanonicalPayload) {
			t.Fatalf("validateSafeStructuredValue(collection + 1) error = %v, want ErrInvalidCanonicalPayload", err)
		}
	})

	t.Run("string limit plus one", func(t *testing.T) {
		if err := validateSafeStructuredValue(strings.Repeat("a", 64*1024), "summary"); err != nil {
			t.Fatalf("validateSafeStructuredValue(string limit) error = %v", err)
		}
		if err := validateSafeStructuredValue(strings.Repeat("a", 64*1024+1), "summary"); !errors.Is(err, ErrInvalidCanonicalPayload) {
			t.Fatalf("validateSafeStructuredValue(string + 1) error = %v, want ErrInvalidCanonicalPayload", err)
		}
	})

	t.Run("key limit plus one", func(t *testing.T) {
		if err := validateSafeStructuredValue(map[string]any{strings.Repeat("a", 128): true}, "summary"); err != nil {
			t.Fatalf("validateSafeStructuredValue(key limit) error = %v", err)
		}
		if err := validateSafeStructuredValue(map[string]any{strings.Repeat("a", 129): true}, "summary"); !errors.Is(err, ErrInvalidCanonicalPayload) {
			t.Fatalf("validateSafeStructuredValue(key + 1) error = %v, want ErrInvalidCanonicalPayload", err)
		}
	})

	t.Run("aggregate collections, nodes, and estimated work", func(t *testing.T) {
		aggregate := make([][]bool, 50_000)
		for index := range aggregate {
			aggregate[index] = []bool{true, false, true, false}
		}
		if err := validateSafeStructuredValue(aggregate, "summary"); err != nil {
			t.Fatalf("validateSafeStructuredValue(aggregate boundary) error = %v", err)
		}
		aggregate = append(aggregate, []bool{true, false, true, false})
		if err := validateSafeStructuredValue(aggregate, "summary"); !errors.Is(err, ErrInvalidCanonicalPayload) {
			t.Fatalf("validateSafeStructuredValue(aggregate + 1) error = %v, want ErrInvalidCanonicalPayload", err)
		}
	})

	t.Run("aggregate string bytes limit plus one", func(t *testing.T) {
		values := make([]string, 80)
		for index := range values {
			values[index] = strings.Repeat("a", 64*1024)
		}
		if err := validateSafeStructuredValue(values, "summary"); err != nil {
			t.Fatalf("validateSafeStructuredValue(aggregate strings limit) error = %v", err)
		}
		values = append(values, "a")
		if err := validateSafeStructuredValue(values, "summary"); !errors.Is(err, ErrInvalidCanonicalPayload) || !strings.Contains(err.Error(), "aggregate string bytes") {
			t.Fatalf("validateSafeStructuredValue(aggregate strings + 1) error = %v, want aggregate string limit", err)
		}
	})

	t.Run("aggregate key bytes limit plus one", func(t *testing.T) {
		values := make(map[string]bool, 8192)
		for index := range 8192 {
			values[fmt.Sprintf("%0128d", index)] = true
		}
		if err := validateSafeStructuredValue(values, "summary"); err != nil {
			t.Fatalf("validateSafeStructuredValue(aggregate keys limit) error = %v", err)
		}
		values["x"] = true
		if err := validateSafeStructuredValue(values, "summary"); !errors.Is(err, ErrInvalidCanonicalPayload) || !strings.Contains(err.Error(), "aggregate key bytes") {
			t.Fatalf("validateSafeStructuredValue(aggregate keys + 1) error = %v, want aggregate key limit", err)
		}
	})

	t.Run("estimated input work limit plus one", func(t *testing.T) {
		values := make([][]string, 50_000)
		for index := range values {
			values[index] = []string{
				strings.Repeat("a", 16),
				strings.Repeat("b", 16),
				strings.Repeat("c", 16),
				strings.Repeat("d", 16),
			}
		}
		values[0][0] = strings.Repeat("a", 62_880)
		values[0][1] = strings.Repeat("b", 62_880)
		values[0][2] = strings.Repeat("c", 62_880)
		if err := validateSafeStructuredValue(values, "summary"); err != nil {
			t.Fatalf("validateSafeStructuredValue(work limit) error = %v", err)
		}
		values[0][3] += "d"
		if err := validateSafeStructuredValue(values, "summary"); !errors.Is(err, ErrInvalidCanonicalPayload) || !strings.Contains(err.Error(), "estimated input work") {
			t.Fatalf("validateSafeStructuredValue(work + 1) error = %v, want estimated work limit", err)
		}
	})
}

func nestedStructuredValue(depth int) any {
	value := reflect.ValueOf(true)
	for range depth {
		array := reflect.New(reflect.ArrayOf(1, value.Type())).Elem()
		array.Index(0).Set(value)
		value = array
	}
	return value.Interface()
}

func nestedPointerStructuredValue(depth int) any {
	value := reflect.ValueOf(true)
	for range depth {
		pointer := reflect.New(value.Type())
		pointer.Elem().Set(value)
		value = pointer
	}
	return value.Interface()
}

type canonicalTestInterfaceNode struct {
	Next any `json:"next"`
}

func nestedInterfaceStructuredValue(depth int, terminal any) any {
	value := terminal
	for range depth {
		value = canonicalTestInterfaceNode{Next: value}
	}
	return value
}

func TestCanonicalRejectsInvalidEnvelopeTimes(t *testing.T) {
	descriptor := testDescriptor(t, MonitoringProbeV2Key())
	envelope := testEnvelope(t, descriptor.Key)
	envelope.ActualWindow.End = envelope.RequestedWindow.End.Add(time.Minute)
	if _, _, err := NewCanonicalSnapshot(descriptor, envelope, map[string]any{"metric_name": "latency_ms"}, RedactionNormalOnly); !errors.Is(err, ErrInvalidSnapshotEnvelope) {
		t.Fatalf("NewCanonicalSnapshot(invalid actual window) error = %v, want ErrInvalidSnapshotEnvelope", err)
	}
}

func TestCanonicalRejectsContentFreeCapture(t *testing.T) {
	descriptor := testDescriptor(t, MonitoringProbeV2Key())
	for _, payload := range []map[string]any{
		{},
		{"endpoint": "https://example.com/health"},
	} {
		if _, _, err := CanonicalizePayload(descriptor, payload, RedactionNormalOnly); !errors.Is(err, ErrInvalidCanonicalPayload) {
			t.Fatalf("CanonicalizePayload(content-free %#v) error = %v, want ErrInvalidCanonicalPayload", payload, err)
		}
		envelope := testEnvelope(t, descriptor.Key)
		envelope.Redaction = []FieldDecision{{Path: "stdout", Sensitivity: SensitivityForbidden, Action: RedactionActionStripped}}
		if _, _, err := NewCanonicalSnapshot(descriptor, envelope, payload, RedactionNormalOnly); !errors.Is(err, ErrInvalidCanonicalPayload) {
			t.Fatalf("NewCanonicalSnapshot(content-free %#v) error = %v, want ErrInvalidCanonicalPayload", payload, err)
		}
	}
}

func TestCanonicalQualityCoreInvariantMatrix(t *testing.T) {
	good := []Quality{
		{Status: QualityComplete, SampleCount: 60},
		{Status: QualityPartial, SampleCount: 60, GapCount: 1, Partial: true},
		{Status: QualityPartial, SampleCount: 60, Truncated: true, Partial: true},
		{Status: QualityDegraded, SampleCount: 60},
		{Status: QualityUnknown},
		{Status: QualityComplete, SampleCount: 20, MaintenanceCount: 2, BackfilledCount: 3, BucketCount: 10, DataPointCount: 20, PeakCount: 5},
	}
	for _, quality := range good {
		if err := validateQuality(quality); err != nil {
			t.Fatalf("validateQuality(good %#v) error = %v", quality, err)
		}
	}

	bad := []Quality{
		{Status: QualityComplete, Partial: true},
		{Status: QualityComplete, Truncated: true},
		{Status: QualityComplete, GapCount: 1},
		{Status: QualityPartial, Partial: false},
		{Status: QualityDegraded, Partial: true},
		{Status: QualityUnknown, Truncated: true, Partial: true},
		{Status: QualityComplete, BucketCount: MaxMetricBucketCount + 1},
		{Status: QualityComplete, DataPointCount: MaxSnapshotDataPoints + 1},
		{Status: QualityComplete, PeakCount: MaxPeakCount + 1},
	}
	for _, quality := range bad {
		if err := validateQuality(quality); !errors.Is(err, ErrInvalidSnapshotEnvelope) {
			t.Fatalf("validateQuality(bad %#v) error = %v, want ErrInvalidSnapshotEnvelope", quality, err)
		}
	}
}

func TestCanonicalRequiresObservedCapturedReferencedOrder(t *testing.T) {
	descriptor := testDescriptor(t, MonitoringProbeV2Key())
	t.Run("captured before observed", func(t *testing.T) {
		envelope := testEnvelope(t, descriptor.Key)
		envelope.CapturedAt = envelope.ObservedAt.Add(-time.Second)
		if _, _, err := NewCanonicalSnapshot(descriptor, envelope, map[string]any{"metric_name": "latency_ms"}, RedactionNormalOnly); !errors.Is(err, ErrInvalidSnapshotEnvelope) {
			t.Fatalf("NewCanonicalSnapshot(captured before observed) error = %v, want ErrInvalidSnapshotEnvelope", err)
		}
	})
	t.Run("referenced before captured", func(t *testing.T) {
		envelope := testEnvelope(t, descriptor.Key)
		envelope.ReferencedAt = envelope.CapturedAt.Add(-time.Second)
		if _, _, err := NewCanonicalSnapshot(descriptor, envelope, map[string]any{"metric_name": "latency_ms"}, RedactionNormalOnly); !errors.Is(err, ErrInvalidSnapshotEnvelope) {
			t.Fatalf("NewCanonicalSnapshot(referenced before captured) error = %v, want ErrInvalidSnapshotEnvelope", err)
		}
	})
}

func TestCanonicalRejectsNormalSensitivityForIncludedTopology(t *testing.T) {
	descriptor := testDescriptor(t, MonitoringProbeV2Key())
	envelope := testEnvelope(t, descriptor.Key)
	envelope.Sensitivity = SensitivityNormal
	if _, _, err := NewCanonicalSnapshot(descriptor, envelope, map[string]any{
		"metric_name": "latency_ms",
		"endpoint":    "https://example.com/health",
	}, RedactionIncludeSensitiveTopology); !errors.Is(err, ErrInvalidSnapshotEnvelope) {
		t.Fatalf("NewCanonicalSnapshot(mislabeled topology) error = %v, want ErrInvalidSnapshotEnvelope", err)
	}
}

func TestCanonicalFinalSnapshotRejectsBlockingQuotaOutcomes(t *testing.T) {
	descriptor := testDescriptor(t, MonitoringProbeV2Key())
	for _, outcome := range []QuotaOutcome{
		{Status: QuotaExceeded, Reason: "project evidence quota exceeded"},
		{Status: QuotaUnavailable, Reason: "quota service unavailable"},
	} {
		t.Run(string(outcome.Status), func(t *testing.T) {
			envelope := testEnvelope(t, descriptor.Key)
			envelope.QuotaOutcome = outcome
			if _, _, err := NewCanonicalSnapshot(descriptor, envelope, map[string]any{"metric_name": "latency_ms"}, RedactionNormalOnly); !errors.Is(err, ErrInvalidSnapshotEnvelope) {
				t.Fatalf("NewCanonicalSnapshot(%q quota) error = %v, want ErrInvalidSnapshotEnvelope", outcome.Status, err)
			}
		})
	}
}

func TestCanonicalUnitsSupportMetricAndNonMetricKinds(t *testing.T) {
	t.Run("metric units", func(t *testing.T) {
		descriptor := testDescriptor(t, MonitoringProbeV2Key())
		envelope := testEnvelope(t, descriptor.Key)
		snapshot, _, err := NewCanonicalSnapshot(descriptor, envelope, map[string]any{"metric_name": "latency_ms"}, RedactionNormalOnly)
		if err != nil {
			t.Fatalf("NewCanonicalSnapshot(metric units) error = %v", err)
		}
		units := snapshot.Envelope().Units
		if units.Status != UnitsApplicable || !reflect.DeepEqual(units.Values, map[string]string{"latency_ms": "ms"}) || units.Reason != "" {
			t.Fatalf("metric units = %#v", units)
		}
	})

	t.Run("units not applicable", func(t *testing.T) {
		descriptor := testDescriptor(t, CommandAuditV1Key())
		envelope := testEnvelope(t, descriptor.Key)
		envelope.Units = UnitsSemantics{Status: UnitsNotApplicable, Reason: "command audit is non-metric"}
		snapshot, _, err := NewCanonicalSnapshot(descriptor, envelope, map[string]any{"command_id": "cmd_1"}, RedactionNormalOnly)
		if err != nil {
			t.Fatalf("NewCanonicalSnapshot(non-metric units) error = %v", err)
		}
		if got := snapshot.Envelope().Units; got.Status != UnitsNotApplicable || len(got.Values) != 0 || got.Reason != "command audit is non-metric" {
			t.Fatalf("non-metric units = %#v", got)
		}
	})
}

func TestCanonicalRejectsInvalidUnitsSemantics(t *testing.T) {
	descriptor := testDescriptor(t, MonitoringProbeV2Key())
	for _, units := range []UnitsSemantics{
		{Status: UnitsApplicable},
		{Status: UnitsApplicable, Values: map[string]string{"latency_ms": "ms"}, Reason: "unexpected"},
		{Status: UnitsNotApplicable, Values: map[string]string{"latency_ms": "ms"}, Reason: "non-metric"},
		{Status: UnitsNotApplicable},
		{Status: UnitsStatus("unknown")},
	} {
		t.Run(string(units.Status)+"_"+units.Reason, func(t *testing.T) {
			envelope := testEnvelope(t, descriptor.Key)
			envelope.Units = units
			if _, _, err := NewCanonicalSnapshot(descriptor, envelope, map[string]any{"metric_name": "latency_ms"}, RedactionNormalOnly); !errors.Is(err, ErrInvalidSnapshotEnvelope) {
				t.Fatalf("NewCanonicalSnapshot(units=%#v) error = %v, want ErrInvalidSnapshotEnvelope", units, err)
			}
		})
	}
}

func TestCanonicalDecodeEmptyInputIsInvalid(t *testing.T) {
	if _, err := DecodeCanonicalPayload(testDescriptor(t, MonitoringProbeV2Key()), nil); !errors.Is(err, ErrInvalidCanonicalPayload) {
		t.Fatalf("DecodeCanonicalPayload(nil) error = %v, want ErrInvalidCanonicalPayload", err)
	}
}

func FuzzCanonicalDeterminism(f *testing.F) {
	f.Add("latency_ms", 12.5, "ok")
	f.Add("packet_loss", 0.0, "partial")
	f.Fuzz(func(t *testing.T, metric string, value float64, status string) {
		if strings.ContainsAny(metric+status, "\x00\r\n") {
			t.Skip()
		}
		descriptor := testDescriptor(t, MonitoringProbeV2Key())
		left := map[string]any{"metric_name": metric, "metric_value": value, "status": status}
		right := map[string]any{"status": status, "metric_value": value, "metric_name": metric}
		first, _, firstErr := CanonicalizePayload(descriptor, left, RedactionNormalOnly)
		second, _, secondErr := CanonicalizePayload(descriptor, right, RedactionNormalOnly)
		if (firstErr == nil) != (secondErr == nil) {
			t.Fatalf("errors differ: %v vs %v", firstErr, secondErr)
		}
		if firstErr == nil && (!bytes.Equal(first.Bytes(), second.Bytes()) || first.Hash() != second.Hash()) {
			t.Fatalf("canonicalization is not deterministic: %q vs %q", first.Bytes(), second.Bytes())
		}
	})
}

func FuzzCanonicalNumberNormalization(f *testing.F) {
	for _, seed := range []string{"0", "-0.0", "12.5", "1.2300e-2", "9007199254740993.0", "1e-65", strings.Repeat("9", 129)} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		normalized, err := normalizeJSONNumber(json.Number(input))
		if err != nil {
			return
		}
		if !json.Valid([]byte(normalized.String())) {
			t.Fatalf("normalizeJSONNumber(%q) returned invalid JSON number %q", input, normalized)
		}
		again, err := normalizeJSONNumber(normalized)
		if err != nil || again != normalized {
			t.Fatalf("normalization is not idempotent: %q -> %q -> %q, err=%v", input, normalized, again, err)
		}
	})
}

func FuzzCanonicalHostileContent(f *testing.F) {
	for _, seed := range []string{
		"safe status",
		"secret=abc",
		"tokenizer",
		"\uff53\uff45\uff43\uff52\uff45\uff54\uff1dabc",
		"-----BEGIN OPENSSH PRIVATE KEY-----",
		"-----BEGIN PGP PRIVATE KEY BLOCK-----",
		"\uff0d\uff0d\uff0d\uff0d\uff0d\uff22\uff25\uff27\uff29\uff2e \uff30\uff27\uff30 \uff30\uff32\uff29\uff36\uff21\uff34\uff25 \uff2b\uff25\uff39 \uff22\uff2c\uff2f\uff23\uff2b\uff0d\uff0d\uff0d\uff0d\uff0d",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		if !utf8.ValidString(input) || len(input) > maxCanonicalStringBytes {
			return
		}
		descriptor := testDescriptor(t, IPQualityReportV1Key())
		_, _, err := CanonicalizePayload(descriptor, map[string]any{"metric_name": "risk", "diagnostic_summary": input}, RedactionNormalOnly)
		if forbiddenStringContent(input) && !errors.Is(err, ErrForbiddenField) {
			t.Fatalf("hostile input %q was classified but not rejected: %v", input, err)
		}
	})
}

func FuzzStructuredNormalizationDepthLimit(f *testing.F) {
	for _, depth := range []uint8{0, 31, 32, 33, 64} {
		f.Add(depth)
	}
	f.Fuzz(func(t *testing.T, depth uint8) {
		depth %= 65
		err := validateSafeStructuredValue(nestedStructuredValue(int(depth)), "summary")
		if depth <= 31 && err != nil {
			t.Fatalf("depth %d error = %v", depth, err)
		}
		if depth > 31 && !errors.Is(err, ErrInvalidCanonicalPayload) {
			t.Fatalf("depth %d error = %v, want ErrInvalidCanonicalPayload", depth, err)
		}
	})
}

func FuzzStructuredNormalizationPointerDepthLimit(f *testing.F) {
	for _, depth := range []uint8{0, 31, 32, 33, 64} {
		f.Add(depth)
	}
	f.Fuzz(func(t *testing.T, depth uint8) {
		depth %= 65
		err := validateSafeStructuredValue(nestedPointerStructuredValue(int(depth)), "summary")
		if depth <= 31 && err != nil {
			t.Fatalf("pointer depth %d error = %v", depth, err)
		}
		if depth > 31 && !errors.Is(err, ErrInvalidCanonicalPayload) {
			t.Fatalf("pointer depth %d error = %v, want ErrInvalidCanonicalPayload", depth, err)
		}
	})
}
