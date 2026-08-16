package evidence

import (
	"bytes"
	"crypto/sha256"
	"encoding"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"houfeng/internal/center/recordauth"
)

type canonicalDocument struct {
	CanonicalizationVersion uint16         `json:"canonicalization_version"`
	Kind                    KindName       `json:"kind"`
	SchemaVersion           SchemaVersion  `json:"schema_version"`
	Payload                 map[string]any `json:"payload"`
}

const canonicalHashDomainV1 = "houfeng.evidence.canonical-payload.v1\x00"

// These limits are enforced on Go values before JSON encoding so custom DTOs
// cannot force unbounded marshal, decode, or redaction work.
const (
	maxCanonicalNestingDepth                = 32
	maxCanonicalNodeCount                   = 300_000
	maxCanonicalCollectionEntries           = 50_000
	maxCanonicalAggregateCollectionEntries  = 250_000
	maxCanonicalAggregateStringBytes        = MaxCanonicalPayloadBytes
	maxCanonicalAggregateKeyBytes           = 1024 * 1024
	maxCanonicalEstimatedInputWorkBytes     = 8 * 1024 * 1024
	structuredIndirectionEstimatedWorkBytes = 8

	// Canonical JSON v1 expands exponents and preserves decimal text exactly.
	// It accepts at most 128 coefficient/integer digits and 64 fractional digits.
	maxCanonicalNumberDigits = 128
	maxCanonicalNumberScale  = 64
)

type structuredValueBudget struct {
	nodes             uint64
	collectionEntries uint64
	stringBytes       uint64
	keyBytes          uint64
	estimatedWork     uint64
	activeReferences  map[structuredVisit]struct{}
}

type structuredVisit struct {
	typeName reflect.Type
	pointer  uintptr
}

var (
	jsonMarshalerType = reflect.TypeOf((*json.Marshaler)(nil)).Elem()
	textMarshalerType = reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem()
	timeType          = reflect.TypeOf(time.Time{})
	jsonNumberType    = reflect.TypeOf(json.Number(""))
)

func CanonicalizePayload(descriptor Descriptor, input any, mode RedactionMode) (CanonicalPayload, RedactionReport, error) {
	if err := descriptor.Validate(); err != nil {
		return CanonicalPayload{}, RedactionReport{}, err
	}
	payload, err := normalizePayload(input)
	if err != nil {
		return CanonicalPayload{}, RedactionReport{}, err
	}
	redacted, report, err := redactPayload(descriptor, payload, mode)
	if err != nil {
		return CanonicalPayload{}, RedactionReport{}, err
	}
	document := canonicalDocument{
		CanonicalizationVersion: descriptor.Conformance.CanonicalizationVersion,
		Kind:                    descriptor.Key.Kind,
		SchemaVersion:           descriptor.Key.SchemaVersion,
		Payload:                 redacted,
	}
	encoded, err := marshalCanonicalDocument(document)
	if err != nil {
		return CanonicalPayload{}, RedactionReport{}, fmt.Errorf("%w: encode: %w", ErrInvalidCanonicalPayload, err)
	}
	if uint64(len(encoded)) > descriptor.Conformance.MaxCanonicalBytes || uint64(len(encoded)) > MaxCanonicalPayloadBytes {
		return CanonicalPayload{}, RedactionReport{}, ErrCanonicalPayloadTooLarge
	}
	return CanonicalPayload{bytes: encoded, hash: CanonicalPayloadDigest(encoded)}, report, nil
}

func DecodeCanonicalPayload(descriptor Descriptor, encoded []byte) (CanonicalPayload, error) {
	if err := descriptor.Validate(); err != nil {
		return CanonicalPayload{}, err
	}
	if len(encoded) == 0 {
		return CanonicalPayload{}, fmt.Errorf("%w: empty input", ErrInvalidCanonicalPayload)
	}
	if uint64(len(encoded)) > descriptor.Conformance.MaxCanonicalBytes || uint64(len(encoded)) > MaxCanonicalPayloadBytes {
		return CanonicalPayload{}, ErrCanonicalPayloadTooLarge
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	var document canonicalDocument
	if err := decoder.Decode(&document); err != nil {
		return CanonicalPayload{}, fmt.Errorf("%w: decode", ErrInvalidCanonicalPayload)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return CanonicalPayload{}, err
	}
	if document.CanonicalizationVersion != CanonicalizationVersionV1 {
		return CanonicalPayload{}, fmt.Errorf("%w: canonicalization version", ErrInvalidCanonicalPayload)
	}
	key := KindKey{Kind: document.Kind, SchemaVersion: document.SchemaVersion}
	if err := validateKnownKindKey(key); err != nil {
		return CanonicalPayload{}, err
	}
	if key != descriptor.Key || document.Payload == nil {
		return CanonicalPayload{}, fmt.Errorf("%w: descriptor mismatch", ErrInvalidCanonicalPayload)
	}
	reencoded, _, err := CanonicalizePayload(descriptor, document.Payload, RedactionIncludeSensitiveTopology)
	if err != nil {
		return CanonicalPayload{}, err
	}
	if !bytes.Equal(reencoded.bytes, encoded) {
		return CanonicalPayload{}, fmt.Errorf("%w: non-canonical encoding", ErrInvalidCanonicalPayload)
	}
	return reencoded, nil
}

// RestoreCanonicalSnapshot reconstructs one immutable logical snapshot from
// its persisted, canonical payload bytes and envelope. It never accepts a raw
// JSON fallback: the descriptor key, canonicalization version, byte-for-byte
// encoding, digest, size, and complete envelope are all revalidated.
func RestoreCanonicalSnapshot(
	descriptor Descriptor,
	envelope SnapshotEnvelope,
	encoded []byte,
) (CanonicalSnapshot, error) {
	payload, err := DecodeCanonicalPayload(descriptor, encoded)
	if err != nil {
		return CanonicalSnapshot{}, err
	}
	if envelope.CanonicalHash != payload.Hash() || envelope.CanonicalSize != payload.Size() {
		return CanonicalSnapshot{}, fmt.Errorf("%w: canonical payload binding", ErrInvalidSnapshotEnvelope)
	}
	normalized, err := normalizeSnapshotEnvelope(descriptor, envelope)
	if err != nil {
		return CanonicalSnapshot{}, err
	}
	snapshot := CanonicalSnapshot{envelope: normalized, payload: payload}
	if err := snapshot.Validate(descriptor); err != nil {
		return CanonicalSnapshot{}, err
	}
	return snapshot, nil
}

func NewCanonicalSnapshot(
	descriptor Descriptor,
	envelope SnapshotEnvelope,
	payload any,
	mode RedactionMode,
) (CanonicalSnapshot, RedactionReport, error) {
	if mode == RedactionMaskSensitiveTopology {
		return CanonicalSnapshot{}, RedactionReport{}, fmt.Errorf("%w: masked payload cannot be captured", ErrInvalidCanonicalPayload)
	}
	canonical, report, err := CanonicalizePayload(descriptor, payload, mode)
	if err != nil {
		return CanonicalSnapshot{}, RedactionReport{}, err
	}
	if envelope.CanonicalHash != [sha256.Size]byte{} && envelope.CanonicalHash != canonical.Hash() {
		return CanonicalSnapshot{}, RedactionReport{}, fmt.Errorf("%w: canonical hash", ErrInvalidSnapshotEnvelope)
	}
	if envelope.CanonicalSize != 0 && envelope.CanonicalSize != canonical.Size() {
		return CanonicalSnapshot{}, RedactionReport{}, fmt.Errorf("%w: canonical size", ErrInvalidSnapshotEnvelope)
	}
	captureRedaction := report.Decisions
	if len(envelope.Redaction) != 0 {
		if err := validateSnapshotRedaction(descriptor, envelope.Redaction); err != nil ||
			!captureRedactionMatchesPayload(envelope.Redaction, report.Decisions) {
			return CanonicalSnapshot{}, RedactionReport{}, fmt.Errorf("%w: redaction", ErrInvalidSnapshotEnvelope)
		}
		captureRedaction = append([]FieldDecision(nil), envelope.Redaction...)
	}
	envelope.CanonicalHash = canonical.Hash()
	envelope.CanonicalSize = canonical.Size()
	envelope.Redaction = append([]FieldDecision(nil), captureRedaction...)
	normalized, err := normalizeSnapshotEnvelope(descriptor, envelope)
	if err != nil {
		return CanonicalSnapshot{}, RedactionReport{}, err
	}
	return CanonicalSnapshot{envelope: normalized, payload: canonical}, RedactionReport{
		Decisions: append([]FieldDecision(nil), captureRedaction...),
	}, nil
}

func (snapshot CanonicalSnapshot) Validate(descriptor Descriptor) error {
	if err := descriptor.Validate(); err != nil {
		return err
	}
	if snapshot.envelope.Key != descriptor.Key || len(snapshot.payload.bytes) == 0 {
		return fmt.Errorf("%w: kind or payload", ErrInvalidSnapshotEnvelope)
	}
	if snapshot.envelope.CanonicalHash != CanonicalPayloadDigest(snapshot.payload.bytes) ||
		snapshot.envelope.CanonicalHash != snapshot.payload.hash ||
		snapshot.envelope.CanonicalSize != uint64(len(snapshot.payload.bytes)) {
		return fmt.Errorf("%w: canonical digest", ErrInvalidSnapshotEnvelope)
	}
	normalized, err := normalizeSnapshotEnvelope(descriptor, snapshot.envelope)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(normalized, snapshot.envelope) {
		return fmt.Errorf("%w: non-canonical envelope", ErrInvalidSnapshotEnvelope)
	}
	if _, err := DecodeCanonicalPayload(descriptor, snapshot.payload.bytes); err != nil {
		return err
	}
	return nil
}

func normalizePayload(input any) (map[string]any, error) {
	normalized, err := normalizeStructuredValue(input)
	if err != nil {
		return nil, err
	}
	payload, ok := normalized.(map[string]any)
	if !ok || payload == nil {
		return nil, fmt.Errorf("%w: payload must be an object", ErrInvalidCanonicalPayload)
	}
	return payload, nil
}

func normalizeJSONValue(value any) (any, error) {
	budget := &structuredValueBudget{}
	return normalizeDecodedJSONValue(value, 1, budget)
}

func normalizeStructuredValue(input any) (any, error) {
	budget := &structuredValueBudget{activeReferences: make(map[structuredVisit]struct{})}
	if err := budget.inspect(reflect.ValueOf(input), 1); err != nil {
		return nil, err
	}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(input); err != nil {
		return nil, fmt.Errorf("%w: marshal: %w", ErrInvalidCanonicalPayload, err)
	}
	encoded := bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'})
	if uint64(len(encoded)) > maxCanonicalEstimatedInputWorkBytes {
		return nil, canonicalResourceLimit("encoded input work")
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("%w: decode", ErrInvalidCanonicalPayload)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return nil, err
	}
	return normalizeJSONValue(value)
}

func normalizeDecodedJSONValue(value any, depth uint64, budget *structuredValueBudget) (any, error) {
	if depth > maxCanonicalNestingDepth {
		return nil, canonicalResourceLimit("nesting depth")
	}
	budget.nodes++
	if budget.nodes > maxCanonicalNodeCount {
		return nil, canonicalResourceLimit("node count")
	}
	switch typed := value.(type) {
	case nil, bool:
		return typed, nil
	case string:
		if !utf8.ValidString(typed) || len(typed) > maxCanonicalStringBytes {
			return nil, fmt.Errorf("%w: string", ErrInvalidCanonicalPayload)
		}
		budget.stringBytes += uint64(len(typed))
		if budget.stringBytes > maxCanonicalAggregateStringBytes {
			return nil, canonicalResourceLimit("aggregate string bytes")
		}
		return typed, nil
	case json.Number:
		return normalizeJSONNumber(typed)
	case []any:
		if err := budget.addCollection(len(typed)); err != nil {
			return nil, err
		}
		output := make([]any, len(typed))
		for index, item := range typed {
			normalized, err := normalizeDecodedJSONValue(item, depth+1, budget)
			if err != nil {
				return nil, err
			}
			output[index] = normalized
		}
		return output, nil
	case map[string]any:
		if err := budget.addCollection(len(typed)); err != nil {
			return nil, err
		}
		output := make(map[string]any, len(typed))
		for key, item := range typed {
			if !utf8.ValidString(key) || len(key) > 128 {
				return nil, fmt.Errorf("%w: object key", ErrInvalidCanonicalPayload)
			}
			budget.keyBytes += uint64(len(key))
			if budget.keyBytes > maxCanonicalAggregateKeyBytes {
				return nil, canonicalResourceLimit("aggregate key bytes")
			}
			normalized, err := normalizeDecodedJSONValue(item, depth+1, budget)
			if err != nil {
				return nil, err
			}
			output[key] = normalized
		}
		return output, nil
	default:
		return nil, fmt.Errorf("%w: JSON value", ErrInvalidCanonicalPayload)
	}
}

func (budget *structuredValueBudget) inspect(value reflect.Value, depth uint64) error {
	if !value.IsValid() {
		return budget.addNode(depth, 4)
	}
	if value.Kind() == reflect.Interface {
		if err := budget.addNode(depth, structuredIndirectionEstimatedWorkBytes); err != nil {
			return err
		}
		if value.IsNil() {
			return nil
		}
		return budget.inspect(value.Elem(), depth+1)
	}
	if value.Kind() == reflect.Pointer {
		if err := budget.addNode(depth, structuredIndirectionEstimatedWorkBytes); err != nil {
			return err
		}
		if value.IsNil() {
			return nil
		}
		if value.Type().Elem() != timeType &&
			implementsCustomStructuredMarshaler(value.Type()) {
			return fmt.Errorf("%w: custom JSON marshaler", ErrInvalidCanonicalPayload)
		}
		return budget.withReference(value, func() error {
			return budget.inspect(value.Elem(), depth+1)
		})
	}
	if err := budget.addNode(depth, 16); err != nil {
		return err
	}
	if value.Type() == timeType {
		return budget.addStringBytes(64)
	}
	if value.Type() == jsonNumberType {
		text := value.String()
		if _, err := normalizeJSONNumber(json.Number(text)); err != nil {
			return err
		}
		return budget.addWork(uint64(len(text)))
	}
	if implementsCustomStructuredMarshaler(value.Type()) {
		return fmt.Errorf("%w: custom JSON marshaler", ErrInvalidCanonicalPayload)
	}

	switch value.Kind() {
	case reflect.Bool:
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return budget.addWork(24)
	case reflect.Float32, reflect.Float64:
		floating := value.Float()
		if math.IsNaN(floating) || math.IsInf(floating, 0) {
			return fmt.Errorf("%w: number", ErrInvalidCanonicalPayload)
		}
		return budget.addWork(32)
	case reflect.String:
		text := value.String()
		if !utf8.ValidString(text) || len(text) > maxCanonicalStringBytes {
			return fmt.Errorf("%w: string", ErrInvalidCanonicalPayload)
		}
		return budget.addStringBytes(uint64(len(text)))
	case reflect.Slice, reflect.Array:
		if err := budget.addCollection(value.Len()); err != nil {
			return err
		}
		walk := func() error {
			for index := 0; index < value.Len(); index++ {
				if err := budget.inspect(value.Index(index), depth+1); err != nil {
					return err
				}
			}
			return nil
		}
		if value.Kind() == reflect.Slice && !value.IsNil() {
			return budget.withReference(value, walk)
		}
		return walk()
	case reflect.Map:
		if value.IsNil() {
			return nil
		}
		if value.Type().Key().Kind() != reflect.String {
			return fmt.Errorf("%w: object key type", ErrInvalidCanonicalPayload)
		}
		if err := budget.addCollection(value.Len()); err != nil {
			return err
		}
		return budget.withReference(value, func() error {
			iterator := value.MapRange()
			for iterator.Next() {
				key := iterator.Key().String()
				if err := budget.addKeyBytes(key); err != nil {
					return err
				}
				if err := budget.inspect(iterator.Value(), depth+1); err != nil {
					return err
				}
			}
			return nil
		})
	case reflect.Struct:
		fieldCount := 0
		for index := 0; index < value.NumField(); index++ {
			field := value.Type().Field(index)
			if !structuredFieldVisibleToJSON(field) {
				continue
			}
			fieldCount++
		}
		if err := budget.addCollection(fieldCount); err != nil {
			return err
		}
		for index := 0; index < value.NumField(); index++ {
			field := value.Type().Field(index)
			tag := field.Tag.Get("json")
			if !structuredFieldVisibleToJSON(field) {
				continue
			}
			key := strings.Split(tag, ",")[0]
			if key == "" {
				key = field.Name
			}
			if err := budget.addKeyBytes(key); err != nil {
				return err
			}
			if err := budget.inspect(value.Field(index), depth+1); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("%w: JSON value", ErrInvalidCanonicalPayload)
	}
}

func structuredFieldVisibleToJSON(field reflect.StructField) bool {
	if field.Tag.Get("json") == "-" {
		return false
	}
	if field.PkgPath == "" {
		return true
	}
	if !field.Anonymous {
		return false
	}
	fieldType := field.Type
	if fieldType.Kind() == reflect.Pointer {
		fieldType = fieldType.Elem()
	}
	return fieldType.Kind() == reflect.Struct
}

func implementsCustomStructuredMarshaler(valueType reflect.Type) bool {
	if valueType.Implements(jsonMarshalerType) || valueType.Implements(textMarshalerType) {
		return true
	}
	if valueType.Kind() == reflect.Pointer {
		return false
	}
	pointerType := reflect.PointerTo(valueType)
	return pointerType.Implements(jsonMarshalerType) || pointerType.Implements(textMarshalerType)
}

func (budget *structuredValueBudget) withReference(value reflect.Value, walk func() error) error {
	pointer := uintptr(value.UnsafePointer())
	if pointer == 0 {
		return walk()
	}
	visit := structuredVisit{typeName: value.Type(), pointer: pointer}
	if _, exists := budget.activeReferences[visit]; exists {
		return fmt.Errorf("%w: cyclic JSON value", ErrInvalidCanonicalPayload)
	}
	budget.activeReferences[visit] = struct{}{}
	defer delete(budget.activeReferences, visit)
	return walk()
}

func (budget *structuredValueBudget) addNode(depth, work uint64) error {
	if depth > maxCanonicalNestingDepth {
		return canonicalResourceLimit("nesting depth")
	}
	budget.nodes++
	if budget.nodes > maxCanonicalNodeCount {
		return canonicalResourceLimit("node count")
	}
	return budget.addWork(work)
}

func (budget *structuredValueBudget) addCollection(entries int) error {
	if entries > maxCanonicalCollectionEntries {
		return canonicalResourceLimit("collection entries")
	}
	budget.collectionEntries += uint64(entries)
	if budget.collectionEntries > maxCanonicalAggregateCollectionEntries {
		return canonicalResourceLimit("aggregate collection entries")
	}
	return budget.addWork(uint64(entries) * 4)
}

func (budget *structuredValueBudget) addStringBytes(size uint64) error {
	budget.stringBytes += size
	if budget.stringBytes > maxCanonicalAggregateStringBytes {
		return canonicalResourceLimit("aggregate string bytes")
	}
	return budget.addWork(size)
}

func (budget *structuredValueBudget) addKeyBytes(key string) error {
	if !utf8.ValidString(key) || len(key) > 128 {
		return fmt.Errorf("%w: object key", ErrInvalidCanonicalPayload)
	}
	budget.keyBytes += uint64(len(key))
	if budget.keyBytes > maxCanonicalAggregateKeyBytes {
		return canonicalResourceLimit("aggregate key bytes")
	}
	return budget.addWork(uint64(len(key)))
}

func (budget *structuredValueBudget) addWork(size uint64) error {
	budget.estimatedWork += size
	if budget.estimatedWork > maxCanonicalEstimatedInputWorkBytes {
		return canonicalResourceLimit("estimated input work")
	}
	return nil
}

func canonicalResourceLimit(name string) error {
	return fmt.Errorf("%w: %s resource limit", ErrInvalidCanonicalPayload, name)
}

func normalizeJSONNumber(value json.Number) (json.Number, error) {
	text := value.String()
	negative, integerPart, fractionalPart, exponent, ok := parseJSONDecimal(text)
	if !ok {
		return "", fmt.Errorf("%w: number", ErrInvalidCanonicalPayload)
	}
	digits := strings.TrimLeft(integerPart+fractionalPart, "0")
	if digits == "" {
		return json.Number("0"), nil
	}
	scale := int64(len(fractionalPart)) - exponent
	for scale > 0 && strings.HasSuffix(digits, "0") {
		digits = strings.TrimSuffix(digits, "0")
		scale--
	}
	if len(digits) > maxCanonicalNumberDigits || scale > maxCanonicalNumberScale {
		return "", fmt.Errorf("%w: number", ErrInvalidCanonicalPayload)
	}
	integerDigits := int64(len(digits)) - scale
	if integerDigits < 1 {
		integerDigits = 1
	}
	if integerDigits > maxCanonicalNumberDigits || scale < -maxCanonicalNumberDigits {
		return "", fmt.Errorf("%w: number", ErrInvalidCanonicalPayload)
	}
	var builder strings.Builder
	if negative {
		builder.WriteByte('-')
	}
	switch {
	case scale <= 0:
		builder.WriteString(digits)
		builder.WriteString(strings.Repeat("0", int(-scale)))
	case scale >= int64(len(digits)):
		builder.WriteString("0.")
		builder.WriteString(strings.Repeat("0", int(scale)-len(digits)))
		builder.WriteString(digits)
	default:
		split := len(digits) - int(scale)
		builder.WriteString(digits[:split])
		builder.WriteByte('.')
		builder.WriteString(digits[split:])
	}
	return json.Number(builder.String()), nil
}

func parseJSONDecimal(text string) (bool, string, string, int64, bool) {
	if text == "" || len(text) > maxCanonicalNumberDigits+maxCanonicalNumberScale+16 {
		return false, "", "", 0, false
	}
	index := 0
	negative := false
	if text[index] == '-' {
		negative = true
		index++
		if index == len(text) {
			return false, "", "", 0, false
		}
	}
	integerStart := index
	if text[index] == '0' {
		index++
		if index < len(text) && text[index] >= '0' && text[index] <= '9' {
			return false, "", "", 0, false
		}
	} else {
		if text[index] < '1' || text[index] > '9' {
			return false, "", "", 0, false
		}
		for index < len(text) && text[index] >= '0' && text[index] <= '9' {
			index++
		}
	}
	integerPart := text[integerStart:index]
	fractionalPart := ""
	if index < len(text) && text[index] == '.' {
		index++
		fractionalStart := index
		for index < len(text) && text[index] >= '0' && text[index] <= '9' {
			index++
		}
		if index == fractionalStart {
			return false, "", "", 0, false
		}
		fractionalPart = text[fractionalStart:index]
	}
	exponent := int64(0)
	if index < len(text) && (text[index] == 'e' || text[index] == 'E') {
		index++
		exponentStart := index
		if index < len(text) && (text[index] == '+' || text[index] == '-') {
			index++
		}
		digitStart := index
		for index < len(text) && text[index] >= '0' && text[index] <= '9' {
			index++
		}
		if index == digitStart || index != len(text) || index-digitStart > 4 {
			return false, "", "", 0, false
		}
		parsed, err := strconv.ParseInt(text[exponentStart:index], 10, 64)
		if err != nil || parsed > maxCanonicalNumberDigits+maxCanonicalNumberScale || parsed < -(maxCanonicalNumberDigits+maxCanonicalNumberScale) {
			return false, "", "", 0, false
		}
		exponent = parsed
	}
	if index != len(text) {
		return false, "", "", 0, false
	}
	return negative, integerPart, fractionalPart, exponent, true
}

func marshalCanonicalDocument(document canonicalDocument) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(document); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'}), nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("%w: trailing JSON", ErrInvalidCanonicalPayload)
	}
	return nil
}

func normalizeSnapshotEnvelope(descriptor Descriptor, envelope SnapshotEnvelope) (SnapshotEnvelope, error) {
	if envelope.Key != descriptor.Key {
		return SnapshotEnvelope{}, fmt.Errorf("%w: kind", ErrInvalidSnapshotEnvelope)
	}
	envelope.Subject = cloneIdentitySnapshot(envelope.Subject)
	envelope.Source = cloneIdentitySnapshot(envelope.Source)
	envelope.Units = cloneUnitsSemantics(envelope.Units)
	envelope.RequestedWindow = normalizeWindow(envelope.RequestedWindow)
	envelope.ActualWindow = normalizeWindow(envelope.ActualWindow)
	envelope.ObservedAt = normalizeTime(envelope.ObservedAt)
	envelope.CapturedAt = normalizeTime(envelope.CapturedAt)
	envelope.ReferencedAt = normalizeTime(envelope.ReferencedAt)
	if err := validateIdentitySnapshot(envelope.Subject); err != nil {
		return SnapshotEnvelope{}, fmt.Errorf("%w: subject", ErrInvalidSnapshotEnvelope)
	}
	if err := validateIdentitySnapshot(envelope.Source); err != nil {
		return SnapshotEnvelope{}, fmt.Errorf("%w: source", ErrInvalidSnapshotEnvelope)
	}
	authorization, err := recordauth.NormalizeSourceAuthorization(envelope.Authorization)
	if err != nil || authorization.Digest != envelope.Authorization.Digest {
		return SnapshotEnvelope{}, fmt.Errorf("%w: authorization", ErrInvalidSnapshotEnvelope)
	}
	envelope.Authorization = authorization
	if envelope.Source.Type != string(authorization.Kind) || envelope.Source.ID != authorization.SourceID {
		return SnapshotEnvelope{}, fmt.Errorf("%w: source authorization identity", ErrInvalidSnapshotEnvelope)
	}
	if err := validateWindow(envelope.RequestedWindow); err != nil {
		return SnapshotEnvelope{}, fmt.Errorf("%w: requested window", ErrInvalidSnapshotEnvelope)
	}
	if err := validateWindow(envelope.ActualWindow); err != nil ||
		envelope.ActualWindow.Start.Before(envelope.RequestedWindow.Start) ||
		envelope.ActualWindow.End.After(envelope.RequestedWindow.End) {
		return SnapshotEnvelope{}, fmt.Errorf("%w: actual window", ErrInvalidSnapshotEnvelope)
	}
	if envelope.ObservedAt.IsZero() || envelope.CapturedAt.IsZero() || envelope.ReferencedAt.IsZero() ||
		envelope.CapturedAt.Before(envelope.ObservedAt) || envelope.ReferencedAt.Before(envelope.CapturedAt) {
		return SnapshotEnvelope{}, fmt.Errorf("%w: observation times", ErrInvalidSnapshotEnvelope)
	}
	if strings.TrimSpace(envelope.SourceRevision) == "" && strings.TrimSpace(envelope.SourceWatermark) == "" {
		return SnapshotEnvelope{}, fmt.Errorf("%w: source revision", ErrInvalidSnapshotEnvelope)
	}
	if envelope.SourceDigest == [sha256.Size]byte{} {
		return SnapshotEnvelope{}, fmt.Errorf("%w: source digest", ErrInvalidSnapshotEnvelope)
	}
	if !validEnvelopeString(envelope.SourceRevision) || !validEnvelopeString(envelope.SourceWatermark) ||
		!validEnvelopeString(envelope.ProducerVersion) || !validEnvelopeString(envelope.CalculationVersion) ||
		strings.TrimSpace(envelope.ProducerVersion) == "" || strings.TrimSpace(envelope.CalculationVersion) == "" {
		return SnapshotEnvelope{}, fmt.Errorf("%w: provenance", ErrInvalidSnapshotEnvelope)
	}
	if err := validateUnitsSemantics(envelope.Units); err != nil {
		return SnapshotEnvelope{}, fmt.Errorf("%w: units", ErrInvalidSnapshotEnvelope)
	}
	if err := validateQuality(envelope.Quality); err != nil {
		return SnapshotEnvelope{}, fmt.Errorf("%w: quality", ErrInvalidSnapshotEnvelope)
	}
	if err := validateDurationSemantics(envelope.ActualPrecision); err != nil {
		return SnapshotEnvelope{}, fmt.Errorf("%w: actual precision", ErrInvalidSnapshotEnvelope)
	}
	if err := validateDurationSemantics(envelope.BucketWidth); err != nil {
		return SnapshotEnvelope{}, fmt.Errorf("%w: bucket width", ErrInvalidSnapshotEnvelope)
	}
	if err := validateCapturedQuotaOutcome(envelope.QuotaOutcome); err != nil {
		return SnapshotEnvelope{}, fmt.Errorf("%w: quota outcome", ErrInvalidSnapshotEnvelope)
	}
	if err := validateRetentionSemantics(envelope.Retention); err != nil {
		return SnapshotEnvelope{}, fmt.Errorf("%w: retention", ErrInvalidSnapshotEnvelope)
	}
	if envelope.Sensitivity != SensitivityNormal && envelope.Sensitivity != SensitivitySensitiveTopology {
		return SnapshotEnvelope{}, fmt.Errorf("%w: sensitivity", ErrInvalidSnapshotEnvelope)
	}
	if err := validateSnapshotRedaction(descriptor, envelope.Redaction); err != nil {
		return SnapshotEnvelope{}, fmt.Errorf("%w: redaction", ErrInvalidSnapshotEnvelope)
	}
	derivedSensitivity, err := sensitivityFromRedaction(envelope.Redaction)
	if err != nil || envelope.Sensitivity != derivedSensitivity {
		return SnapshotEnvelope{}, fmt.Errorf("%w: sensitivity classification", ErrInvalidSnapshotEnvelope)
	}
	if envelope.CanonicalHash == [sha256.Size]byte{} || envelope.CanonicalSize == 0 ||
		envelope.CanonicalSize > descriptor.Conformance.MaxCanonicalBytes || envelope.CanonicalSize > MaxCanonicalPayloadBytes {
		return SnapshotEnvelope{}, fmt.Errorf("%w: canonical payload", ErrInvalidSnapshotEnvelope)
	}
	return envelope, nil
}

func validateIdentitySnapshot(identity IdentitySnapshot) error {
	if !validRegistryToken(identity.Type) || strings.TrimSpace(identity.ID) == "" || len(identity.ID) > 256 || !validEnvelopeString(identity.ID) || len(identity.Fields) == 0 || len(identity.Fields) > 32 {
		return ErrInvalidSnapshotEnvelope
	}
	for key, value := range identity.Fields {
		if !validRegistryToken(key) || forbiddenFieldPath(key) || strings.TrimSpace(value) == "" || len(value) > 512 || !validEnvelopeString(value) {
			return ErrInvalidSnapshotEnvelope
		}
	}
	return nil
}

func validateQuality(quality Quality) error {
	switch quality.Status {
	case QualityComplete, QualityPartial, QualityDegraded, QualityUnknown:
	default:
		return ErrInvalidSnapshotEnvelope
	}
	if quality.BucketCount > MaxMetricBucketCount || quality.DataPointCount > MaxSnapshotDataPoints || quality.PeakCount > MaxPeakCount {
		return ErrInvalidSnapshotEnvelope
	}
	if quality.Partial != (quality.Status == QualityPartial) ||
		(quality.Truncated && !quality.Partial) || (quality.GapCount > 0 && !quality.Partial) {
		return ErrInvalidSnapshotEnvelope
	}
	return nil
}

func validateDurationSemantics(value DurationSemantics) error {
	if value.Applicable {
		if value.Value <= 0 || value.Reason != "" {
			return ErrInvalidSnapshotEnvelope
		}
		return nil
	}
	if value.Value != 0 || strings.TrimSpace(value.Reason) == "" || !validEnvelopeString(value.Reason) {
		return ErrInvalidSnapshotEnvelope
	}
	return nil
}

func validateUnitsSemantics(units UnitsSemantics) error {
	switch units.Status {
	case UnitsApplicable:
		if len(units.Values) == 0 || units.Reason != "" {
			return ErrInvalidSnapshotEnvelope
		}
		for metric, unit := range units.Values {
			if !validFieldPath(metric) || strings.TrimSpace(unit) == "" || !validEnvelopeString(unit) {
				return ErrInvalidSnapshotEnvelope
			}
		}
	case UnitsNotApplicable:
		if len(units.Values) != 0 || strings.TrimSpace(units.Reason) == "" || !validEnvelopeString(units.Reason) {
			return ErrInvalidSnapshotEnvelope
		}
	default:
		return ErrInvalidSnapshotEnvelope
	}
	return nil
}

func validatePreviewQuotaOutcome(outcome QuotaOutcome) error {
	switch outcome.Status {
	case QuotaAllowed:
		if outcome.Reason != "" {
			return ErrInvalidSnapshotEnvelope
		}
	case QuotaWarning, QuotaExceeded, QuotaUnavailable:
		if strings.TrimSpace(outcome.Reason) == "" || !validEnvelopeString(outcome.Reason) {
			return ErrInvalidSnapshotEnvelope
		}
	default:
		return ErrInvalidSnapshotEnvelope
	}
	return nil
}

func validateCapturedQuotaOutcome(outcome QuotaOutcome) error {
	if err := validatePreviewQuotaOutcome(outcome); err != nil ||
		(outcome.Status != QuotaAllowed && outcome.Status != QuotaWarning) {
		return ErrInvalidSnapshotEnvelope
	}
	return nil
}

func validateRetentionSemantics(retention RetentionSemantics) error {
	if !retention.Immutable || retention.Scope != RetentionScopeRecordRevision ||
		retention.SourceDeletion != SourceDeletionSnapshotRetained {
		return ErrInvalidSnapshotEnvelope
	}
	return nil
}

func validateSnapshotRedaction(descriptor Descriptor, decisions []FieldDecision) error {
	if len(decisions) == 0 {
		return ErrInvalidSnapshotEnvelope
	}
	definitions := make(map[string]FieldDefinition, len(descriptor.Fields))
	for _, definition := range descriptor.Fields {
		definitions[definition.Path] = definition
	}
	for index, decision := range decisions {
		definition, exists := definitions[decision.Path]
		if !exists || definition.Sensitivity != decision.Sensitivity ||
			(index > 0 && decisions[index-1].Path >= decision.Path) {
			return ErrInvalidSnapshotEnvelope
		}
		switch decision.Sensitivity {
		case SensitivityNormal:
			if decision.Action != RedactionActionIncluded {
				return ErrInvalidSnapshotEnvelope
			}
		case SensitivitySensitiveTopology:
			if decision.Action != RedactionActionIncluded && decision.Action != RedactionActionStripped {
				return ErrInvalidSnapshotEnvelope
			}
		case SensitivityForbidden:
			if decision.Action != RedactionActionStripped {
				return ErrInvalidSnapshotEnvelope
			}
		default:
			return ErrInvalidSnapshotEnvelope
		}
	}
	return nil
}

func sensitivityFromRedaction(decisions []FieldDecision) (Sensitivity, error) {
	sensitivity := SensitivityNormal
	for _, decision := range decisions {
		if decision.Sensitivity == SensitivitySensitiveTopology && decision.Action == RedactionActionIncluded {
			sensitivity = SensitivitySensitiveTopology
		}
		if decision.Action == RedactionActionForbidden || decision.Action == RedactionActionMasked {
			return "", ErrInvalidSnapshotEnvelope
		}
	}
	return sensitivity, nil
}

func captureRedactionMatchesPayload(declared, payload []FieldDecision) bool {
	payloadByPath := make(map[string]FieldDecision, len(payload))
	for _, decision := range payload {
		payloadByPath[decision.Path] = decision
	}
	declaredByPath := make(map[string]FieldDecision, len(declared))
	for _, decision := range declared {
		declaredByPath[decision.Path] = decision
		payloadDecision, exists := payloadByPath[decision.Path]
		if !exists && (decision.Sensitivity != SensitivityForbidden || decision.Action != RedactionActionStripped) {
			return false
		}
		if exists && payloadDecision != decision {
			return false
		}
	}
	for _, decision := range payload {
		if declaredByPath[decision.Path] != decision {
			return false
		}
	}
	return true
}

// CanonicalPayloadDigest computes the versioned content address used by
// canonical evidence payloads and their persistence boundary.
func CanonicalPayloadDigest(encoded []byte) [sha256.Size]byte {
	domainSeparated := make([]byte, 0, len(canonicalHashDomainV1)+len(encoded))
	domainSeparated = append(domainSeparated, canonicalHashDomainV1...)
	domainSeparated = append(domainSeparated, encoded...)
	return sha256.Sum256(domainSeparated)
}

func normalizeWindow(window TimeWindow) TimeWindow {
	return TimeWindow{Start: normalizeTime(window.Start), End: normalizeTime(window.End)}
}

func validateWindow(window TimeWindow) error {
	if window.Start.IsZero() || window.End.IsZero() || window.End.Before(window.Start) {
		return ErrInvalidSnapshotEnvelope
	}
	return nil
}

func normalizeTime(value time.Time) time.Time {
	if value.IsZero() {
		return time.Time{}
	}
	return value.Round(0).UTC()
}

func validEnvelopeString(value string) bool {
	if !utf8.ValidString(value) || len(value) > 1024 || forbiddenStringContent(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func cloneIdentitySnapshot(identity IdentitySnapshot) IdentitySnapshot {
	identity.Fields = cloneStringMap(identity.Fields)
	return identity
}

func cloneSnapshotEnvelope(envelope SnapshotEnvelope) SnapshotEnvelope {
	envelope.Subject = cloneIdentitySnapshot(envelope.Subject)
	envelope.Source = cloneIdentitySnapshot(envelope.Source)
	envelope.Units = cloneUnitsSemantics(envelope.Units)
	envelope.Redaction = append([]FieldDecision(nil), envelope.Redaction...)
	if authorization, err := recordauth.NormalizeSourceAuthorization(envelope.Authorization); err == nil {
		envelope.Authorization = authorization
	}
	return envelope
}

func cloneUnitsSemantics(units UnitsSemantics) UnitsSemantics {
	units.Values = cloneStringMap(units.Values)
	return units
}

func cloneStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	cloned := make(map[string]string, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}
