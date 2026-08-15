package evidence

import (
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/recordauth"
)

type preparedCaptureTestInputs struct {
	recordID      string
	snapshotID    string
	descriptor    Descriptor
	preview       Preview
	intent        Intent
	authorization AuthorizationScope
	snapshot      CanonicalSnapshot
}

func TestPrepareCaptureReturnsValidImmutableValue(t *testing.T) {
	inputs := newPreparedCaptureTestInputs(t)
	prepared, err := prepareCaptureFromTestInputs(inputs)
	if err != nil {
		t.Fatalf("PrepareCapture() error = %v", err)
	}
	if err := prepared.Validate(); err != nil {
		t.Fatalf("PreparedCapture.Validate() error = %v", err)
	}
	if prepared.RecordID() != inputs.recordID || prepared.SnapshotID() != inputs.snapshotID {
		t.Fatalf("prepared identities = (%q, %q), want (%q, %q)", prepared.RecordID(), prepared.SnapshotID(), inputs.recordID, inputs.snapshotID)
	}

	wantDescriptorPath := prepared.Descriptor().Fields[0].Path
	wantMetric := prepared.Preview().Selection.Metrics[0]
	wantSubjectName := prepared.Preview().Subject.Fields["display_name"]
	wantSourceName := prepared.Preview().Source.Fields["display_name"]
	wantUnit := prepared.Preview().Units.Values["latency_ms"]
	wantRedactionPath := prepared.Preview().Redaction[0].Path
	wantIntentMetric := prepared.Intent().Selection.Metrics[0]
	wantAuthorizationRevision := prepared.Authorization().CurrentScope.PolicyRevision
	wantSnapshotName := prepared.Snapshot().Envelope().Subject.Fields["display_name"]
	wantSnapshotSourceName := prepared.Snapshot().Envelope().Source.Fields["display_name"]
	wantSnapshotBytes := prepared.Snapshot().Bytes()

	inputs.descriptor.Fields[0].Path = "changed"
	inputs.preview.Selection.Metrics[0] = "changed"
	inputs.preview.Subject.Fields["display_name"] = "changed"
	inputs.preview.Source.Fields["display_name"] = "changed"
	inputs.preview.Units.Values["latency_ms"] = "changed"
	inputs.preview.Redaction[0].Path = "changed"
	inputs.intent.Selection.Metrics[0] = "changed"
	inputs.authorization.CurrentScope.PolicyRevision++
	inputs.snapshot.envelope.Subject.Fields["display_name"] = "changed"
	inputs.snapshot.envelope.Source.Fields["display_name"] = "changed"
	inputs.snapshot.envelope.Redaction[0].Path = "changed"
	inputs.snapshot.payload.bytes[0] ^= 0xff

	assertPreparedCaptureState(t, prepared, wantDescriptorPath, wantMetric, wantSubjectName, wantSourceName, wantUnit, wantRedactionPath, wantIntentMetric, wantAuthorizationRevision, wantSnapshotName, wantSnapshotSourceName, wantSnapshotBytes)

	descriptor := prepared.Descriptor()
	descriptor.Fields[0].Path = "changed again"
	preview := prepared.Preview()
	preview.Selection.Metrics[0] = "changed again"
	preview.Subject.Fields["display_name"] = "changed again"
	preview.Source.Fields["display_name"] = "changed again"
	preview.Units.Values["latency_ms"] = "changed again"
	preview.Redaction[0].Path = "changed again"
	intent := prepared.Intent()
	intent.Selection.Metrics[0] = "changed again"
	authorization := prepared.Authorization()
	authorization.CurrentScope.PolicyRevision++
	snapshot := prepared.Snapshot()
	snapshot.envelope.Subject.Fields["display_name"] = "changed again"
	snapshot.envelope.Source.Fields["display_name"] = "changed again"
	snapshot.envelope.Redaction[0].Path = "changed again"
	snapshot.payload.bytes[0] ^= 0xff

	assertPreparedCaptureState(t, prepared, wantDescriptorPath, wantMetric, wantSubjectName, wantSourceName, wantUnit, wantRedactionPath, wantIntentMetric, wantAuthorizationRevision, wantSnapshotName, wantSnapshotSourceName, wantSnapshotBytes)
	if err := prepared.Validate(); err != nil {
		t.Fatalf("PreparedCapture.Validate() after attempted mutation error = %v", err)
	}
}

func TestPrepareCaptureRejectsPreviewBoundDrift(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*preparedCaptureTestInputs)
	}{
		{name: "kind and schema", mutate: func(input *preparedCaptureTestInputs) {
			input.preview.Key = MonitoringHostV1Key()
		}},
		{name: "selection", mutate: func(input *preparedCaptureTestInputs) {
			input.preview.Selection.Metrics = append(input.preview.Selection.Metrics, "packet_loss_pct")
		}},
		{name: "selection source", mutate: func(input *preparedCaptureTestInputs) {
			input.preview.Selection.SourceID = "tg_1111111111111111"
			input.preview.Source.ID = input.preview.Selection.SourceID
			input.intent.Selection.SourceID = input.preview.Selection.SourceID
		}},
		{name: "subject identity", mutate: func(input *preparedCaptureTestInputs) {
			input.preview.Subject.Fields["display_name"] = "changed subject"
		}},
		{name: "source identity", mutate: func(input *preparedCaptureTestInputs) {
			input.preview.Source.Fields["display_name"] = "changed source"
		}},
		{name: "requested window", mutate: func(input *preparedCaptureTestInputs) {
			input.preview.RequestedWindow.Start = input.preview.RequestedWindow.Start.Add(-time.Minute)
			input.preview.Selection.RequestedWindow = input.preview.RequestedWindow
			input.intent.Selection.RequestedWindow = input.preview.RequestedWindow
		}},
		{name: "actual window", mutate: func(input *preparedCaptureTestInputs) {
			input.preview.ActualWindow.Start = input.preview.ActualWindow.Start.Add(time.Minute)
		}},
		{name: "observed time", mutate: func(input *preparedCaptureTestInputs) {
			input.preview.ObservedAt = input.preview.ObservedAt.Add(time.Second)
		}},
		{name: "source revision", mutate: func(input *preparedCaptureTestInputs) {
			input.preview.SourceRevision = "revision-2"
		}},
		{name: "source watermark", mutate: func(input *preparedCaptureTestInputs) {
			input.preview.SourceWatermark = "watermark-2"
		}},
		{name: "source digest", mutate: func(input *preparedCaptureTestInputs) {
			input.preview.SourceDigest = sha256.Sum256([]byte("different source"))
		}},
		{name: "producer version", mutate: func(input *preparedCaptureTestInputs) {
			input.preview.ProducerVersion = "producer-2"
		}},
		{name: "calculation version", mutate: func(input *preparedCaptureTestInputs) {
			input.preview.CalculationVersion = "calculation-2"
		}},
		{name: "units", mutate: func(input *preparedCaptureTestInputs) {
			input.preview.Units.Values["latency_ms"] = "seconds"
		}},
		{name: "quality", mutate: func(input *preparedCaptureTestInputs) {
			input.preview.Quality.SampleCount++
		}},
		{name: "sensitivity and redaction", mutate: func(input *preparedCaptureTestInputs) {
			input.preview.Redaction = append(input.preview.Redaction, FieldDecision{
				Path: "endpoint", Sensitivity: SensitivitySensitiveTopology, Action: RedactionActionIncluded,
			})
			input.preview.Sensitivity = SensitivitySensitiveTopology
		}},
		{name: "actual precision", mutate: func(input *preparedCaptureTestInputs) {
			input.preview.ActualPrecision.Value = 5 * time.Minute
		}},
		{name: "bucket width", mutate: func(input *preparedCaptureTestInputs) {
			input.preview.BucketWidth.Value = 5 * time.Minute
		}},
		{name: "quota", mutate: func(input *preparedCaptureTestInputs) {
			input.preview.QuotaOutcome = QuotaOutcome{Status: QuotaExceeded, Reason: "project evidence quota exceeded"}
		}},
		{name: "retention", mutate: func(input *preparedCaptureTestInputs) {
			input.preview.Retention = RetentionSemantics{}
		}},
		{name: "canonical size", mutate: func(input *preparedCaptureTestInputs) {
			input.preview.EstimatedCanonicalBytes++
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inputs := newPreparedCaptureTestInputs(t)
			tt.mutate(&inputs)
			if _, err := prepareCaptureFromTestInputs(inputs); !errors.Is(err, ErrInvalidPreparedCapture) {
				t.Fatalf("PrepareCapture() error = %v, want ErrInvalidPreparedCapture", err)
			}
		})
	}
}

func TestPrepareCaptureRejectsInvalidIdentities(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*preparedCaptureTestInputs)
	}{
		{name: "empty record", mutate: func(input *preparedCaptureTestInputs) { input.recordID = "" }},
		{name: "record prefix only", mutate: func(input *preparedCaptureTestInputs) { input.recordID = "rec_" }},
		{name: "record uppercase", mutate: func(input *preparedCaptureTestInputs) { input.recordID = "rec_CAPTURE" }},
		{name: "record too long", mutate: func(input *preparedCaptureTestInputs) { input.recordID = "rec_" + strings.Repeat("a", 65) }},
		{name: "empty snapshot", mutate: func(input *preparedCaptureTestInputs) { input.snapshotID = "" }},
		{name: "snapshot prefix only", mutate: func(input *preparedCaptureTestInputs) { input.snapshotID = "evs_" }},
		{name: "snapshot uppercase", mutate: func(input *preparedCaptureTestInputs) { input.snapshotID = "evs_CAPTURE" }},
		{name: "snapshot too long", mutate: func(input *preparedCaptureTestInputs) { input.snapshotID = "evs_" + strings.Repeat("a", 65) }},
		{name: "intent prefix only", mutate: func(input *preparedCaptureTestInputs) {
			input.preview.IntentID = "evi_"
			input.intent.ID = input.preview.IntentID
		}},
		{name: "intent non-hex", mutate: func(input *preparedCaptureTestInputs) {
			input.preview.IntentID = "evi_0123456789abcdef0123456g"
			input.intent.ID = input.preview.IntentID
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inputs := newPreparedCaptureTestInputs(t)
			tt.mutate(&inputs)
			if _, err := prepareCaptureFromTestInputs(inputs); !errors.Is(err, ErrInvalidPreparedCapture) {
				t.Fatalf("PrepareCapture() error = %v, want ErrInvalidPreparedCapture", err)
			}
		})
	}
}

func TestPrepareCaptureRejectsInvalidIntentExpiryShape(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*preparedCaptureTestInputs)
	}{
		{name: "intent disagrees with preview", mutate: func(input *preparedCaptureTestInputs) {
			input.intent.ValidUntil = input.intent.ValidUntil.Add(-time.Second)
		}},
		{name: "zero previewed time", mutate: func(input *preparedCaptureTestInputs) {
			input.preview.PreviewedAt = time.Time{}
		}},
		{name: "non-positive lifetime", mutate: func(input *preparedCaptureTestInputs) {
			input.preview.ValidUntil = input.preview.PreviewedAt
			input.intent.ValidUntil = input.preview.ValidUntil
		}},
		{name: "lifetime exceeds ttl", mutate: func(input *preparedCaptureTestInputs) {
			input.preview.ValidUntil = input.preview.PreviewedAt.Add(CaptureIntentTTL + time.Second)
			input.intent.ValidUntil = input.preview.ValidUntil
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inputs := newPreparedCaptureTestInputs(t)
			tt.mutate(&inputs)
			if _, err := prepareCaptureFromTestInputs(inputs); !errors.Is(err, ErrInvalidPreparedCapture) {
				t.Fatalf("PrepareCapture() error = %v, want ErrInvalidPreparedCapture", err)
			}
		})
	}
}

func TestPrepareCaptureDefersIntentExpiryLivenessToStoreConsumption(t *testing.T) {
	inputs := newPreparedCaptureTestInputs(t)
	inputs.preview.PreviewedAt = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	inputs.preview.ValidUntil = inputs.preview.PreviewedAt.Add(CaptureIntentTTL)
	inputs.intent.ValidUntil = inputs.preview.ValidUntil

	if _, err := prepareCaptureFromTestInputs(inputs); err != nil {
		t.Fatalf("PrepareCapture() error = %v", err)
	}
}

func TestPrepareCaptureRejectsInvalidFreshAuthorization(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, *preparedCaptureTestInputs)
	}{
		{name: "zero authorization", mutate: func(_ *testing.T, input *preparedCaptureTestInputs) {
			input.authorization = AuthorizationScope{}
		}},
		{name: "authorization digest drift", mutate: func(_ *testing.T, input *preparedCaptureTestInputs) {
			input.authorization.Digest[0] ^= 0xff
		}},
		{name: "authorization source identity drift", mutate: func(t *testing.T, input *preparedCaptureTestInputs) {
			authorization, err := recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{
				Version:      input.authorization.Version,
				Kind:         input.authorization.Kind,
				SourceID:     "tg_1111111111111111",
				State:        input.authorization.State,
				CaptureScope: input.authorization.CaptureScope,
				CurrentScope: input.authorization.CurrentScope,
			})
			if err != nil {
				t.Fatalf("NormalizeSourceAuthorization() error = %v", err)
			}
			input.authorization = authorization
		}},
		{name: "non-canonical authorization", mutate: func(_ *testing.T, input *preparedCaptureTestInputs) {
			input.authorization.CurrentScope.PolicyRevision++
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inputs := newPreparedCaptureTestInputs(t)
			tt.mutate(t, &inputs)
			if _, err := prepareCaptureFromTestInputs(inputs); !errors.Is(err, ErrInvalidPreparedCapture) {
				t.Fatalf("PrepareCapture() error = %v, want ErrInvalidPreparedCapture", err)
			}
		})
	}
}

func TestPrepareCaptureRejectsTombstonedAuthorizationForNewCapture(t *testing.T) {
	inputs := newPreparedCaptureTestInputs(t)
	lastLiveScope := *inputs.authorization.CurrentScope
	tombstoned, err := recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{
		Version:       inputs.authorization.Version,
		Kind:          inputs.authorization.Kind,
		SourceID:      inputs.authorization.SourceID,
		State:         recordauth.SourceStateTombstoned,
		CaptureScope:  inputs.authorization.CaptureScope,
		FinalFloor:    &lastLiveScope,
		LastLiveScope: &lastLiveScope,
	})
	if err != nil {
		t.Fatalf("NormalizeSourceAuthorization() error = %v", err)
	}
	envelope := inputs.snapshot.Envelope()
	envelope.Authorization = tombstoned
	snapshot, _, err := NewCanonicalSnapshot(
		inputs.descriptor,
		envelope,
		map[string]any{"metric_name": "latency_ms"},
		RedactionNormalOnly,
	)
	if err != nil {
		t.Fatalf("NewCanonicalSnapshot() error = %v", err)
	}
	inputs.authorization = tombstoned
	inputs.snapshot = snapshot

	if _, err := prepareCaptureFromTestInputs(inputs); !errors.Is(err, ErrInvalidPreparedCapture) {
		t.Fatalf("PrepareCapture() error = %v, want ErrInvalidPreparedCapture", err)
	}
}

func TestPrepareCaptureRejectsInvalidDescriptorOrSnapshot(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*preparedCaptureTestInputs)
	}{
		{name: "zero descriptor", mutate: func(input *preparedCaptureTestInputs) {
			input.descriptor = Descriptor{}
		}},
		{name: "zero snapshot", mutate: func(input *preparedCaptureTestInputs) {
			input.snapshot = CanonicalSnapshot{}
		}},
		{name: "corrupt snapshot payload", mutate: func(input *preparedCaptureTestInputs) {
			input.snapshot.payload.bytes[0] ^= 0xff
		}},
		{name: "snapshot key drift", mutate: func(input *preparedCaptureTestInputs) {
			input.snapshot.envelope.Key = MonitoringHostV1Key()
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inputs := newPreparedCaptureTestInputs(t)
			tt.mutate(&inputs)
			if _, err := prepareCaptureFromTestInputs(inputs); !errors.Is(err, ErrInvalidPreparedCapture) {
				t.Fatalf("PrepareCapture() error = %v, want ErrInvalidPreparedCapture", err)
			}
		})
	}
}

func TestPrepareCaptureNormalizesPreviewRedactionForCapturedPayload(t *testing.T) {
	t.Run("masked topology", func(t *testing.T) {
		inputs := newPreparedCaptureTestInputs(t)
		envelope := testEnvelope(t, inputs.descriptor.Key)
		snapshot, _, err := NewCanonicalSnapshot(inputs.descriptor, envelope, map[string]any{
			"endpoint":    "https://example.com/health",
			"metric_name": "latency_ms",
		}, RedactionNormalOnly)
		if err != nil {
			t.Fatalf("NewCanonicalSnapshot() error = %v", err)
		}
		inputs.preview.Redaction = []FieldDecision{
			{Path: "endpoint", Sensitivity: SensitivitySensitiveTopology, Action: RedactionActionMasked},
			{Path: "metric_name", Sensitivity: SensitivityNormal, Action: RedactionActionIncluded},
		}
		inputs.preview.EstimatedCanonicalBytes = snapshot.Size()
		inputs.snapshot = snapshot

		prepared, err := prepareCaptureFromTestInputs(inputs)
		if err != nil {
			t.Fatalf("PrepareCapture() error = %v", err)
		}
		if got := decisionAction(t, RedactionReport{Decisions: prepared.Snapshot().Envelope().Redaction}, "endpoint"); got != RedactionActionStripped {
			t.Fatalf("captured endpoint action = %q, want %q", got, RedactionActionStripped)
		}
	})

	t.Run("forbidden field", func(t *testing.T) {
		inputs := newPreparedCaptureTestInputs(t)
		envelope := testEnvelope(t, inputs.descriptor.Key)
		envelope.Redaction = []FieldDecision{
			{Path: "metric_name", Sensitivity: SensitivityNormal, Action: RedactionActionIncluded},
			{Path: "stdout", Sensitivity: SensitivityForbidden, Action: RedactionActionStripped},
		}
		snapshot, _, err := NewCanonicalSnapshot(inputs.descriptor, envelope, map[string]any{"metric_name": "latency_ms"}, RedactionNormalOnly)
		if err != nil {
			t.Fatalf("NewCanonicalSnapshot() error = %v", err)
		}
		inputs.preview.Redaction = []FieldDecision{
			{Path: "metric_name", Sensitivity: SensitivityNormal, Action: RedactionActionIncluded},
			{Path: "stdout", Sensitivity: SensitivityForbidden, Action: RedactionActionForbidden},
		}
		inputs.preview.EstimatedCanonicalBytes = snapshot.Size()
		inputs.snapshot = snapshot

		prepared, err := prepareCaptureFromTestInputs(inputs)
		if err != nil {
			t.Fatalf("PrepareCapture() error = %v", err)
		}
		if got := decisionAction(t, RedactionReport{Decisions: prepared.Snapshot().Envelope().Redaction}, "stdout"); got != RedactionActionStripped {
			t.Fatalf("captured stdout action = %q, want %q", got, RedactionActionStripped)
		}
	})
}

func TestPrepareCaptureDoesNotCompareServerOwnedSnapshotTimesToPreview(t *testing.T) {
	inputs := newPreparedCaptureTestInputs(t)
	inputs.snapshot.envelope.CapturedAt = inputs.snapshot.envelope.CapturedAt.Add(5 * time.Minute)
	inputs.snapshot.envelope.ReferencedAt = inputs.snapshot.envelope.CapturedAt.Add(time.Minute)

	if _, err := prepareCaptureFromTestInputs(inputs); err != nil {
		t.Fatalf("PrepareCapture() error = %v", err)
	}
}

func TestPreparedCaptureZeroAndMalformedStateAreInvalid(t *testing.T) {
	var zero PreparedCapture
	if err := zero.Validate(); !errors.Is(err, ErrInvalidPreparedCapture) {
		t.Fatalf("zero PreparedCapture.Validate() error = %v, want ErrInvalidPreparedCapture", err)
	}

	inputs := newPreparedCaptureTestInputs(t)
	prepared, err := prepareCaptureFromTestInputs(inputs)
	if err != nil {
		t.Fatalf("PrepareCapture() error = %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*PreparedCapture)
	}{
		{name: "record identity", mutate: func(prepared *PreparedCapture) { prepared.recordID = "record_invalid" }},
		{name: "snapshot identity", mutate: func(prepared *PreparedCapture) { prepared.snapshotID = "snapshot_invalid" }},
		{name: "intent state", mutate: func(prepared *PreparedCapture) { prepared.intent.ID = "evi_invalid" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			malformed := prepared
			tt.mutate(&malformed)
			if err := malformed.Validate(); !errors.Is(err, ErrInvalidPreparedCapture) {
				t.Fatalf("malformed PreparedCapture.Validate() error = %v, want ErrInvalidPreparedCapture", err)
			}
		})
	}
}

func newPreparedCaptureTestInputs(t *testing.T) preparedCaptureTestInputs {
	t.Helper()
	stub, fixture := testConformingKind(t)
	return preparedCaptureTestInputs{
		recordID:      "rec_capture1",
		snapshotID:    "evs_capture1",
		descriptor:    stub.descriptor,
		preview:       stub.preview,
		intent:        fixture.Intent,
		authorization: stub.authorization,
		snapshot:      stub.snapshot,
	}
}

func prepareCaptureFromTestInputs(input preparedCaptureTestInputs) (PreparedCapture, error) {
	return PrepareCapture(
		input.recordID,
		input.snapshotID,
		input.descriptor,
		input.preview,
		input.intent,
		input.authorization,
		input.snapshot,
	)
}

func assertPreparedCaptureState(
	t *testing.T,
	prepared PreparedCapture,
	wantDescriptorPath string,
	wantMetric string,
	wantSubjectName string,
	wantSourceName string,
	wantUnit string,
	wantRedactionPath string,
	wantIntentMetric string,
	wantAuthorizationRevision uint64,
	wantSnapshotName string,
	wantSnapshotSourceName string,
	wantSnapshotBytes []byte,
) {
	t.Helper()
	if got := prepared.Descriptor().Fields[0].Path; got != wantDescriptorPath {
		t.Fatalf("descriptor path = %q, want %q", got, wantDescriptorPath)
	}
	if got := prepared.Preview().Selection.Metrics[0]; got != wantMetric {
		t.Fatalf("preview metric = %q, want %q", got, wantMetric)
	}
	if got := prepared.Preview().Subject.Fields["display_name"]; got != wantSubjectName {
		t.Fatalf("preview subject name = %q, want %q", got, wantSubjectName)
	}
	if got := prepared.Preview().Source.Fields["display_name"]; got != wantSourceName {
		t.Fatalf("preview source name = %q, want %q", got, wantSourceName)
	}
	if got := prepared.Preview().Units.Values["latency_ms"]; got != wantUnit {
		t.Fatalf("preview unit = %q, want %q", got, wantUnit)
	}
	if got := prepared.Preview().Redaction[0].Path; got != wantRedactionPath {
		t.Fatalf("preview redaction path = %q, want %q", got, wantRedactionPath)
	}
	if got := prepared.Intent().Selection.Metrics[0]; got != wantIntentMetric {
		t.Fatalf("intent metric = %q, want %q", got, wantIntentMetric)
	}
	if got := prepared.Authorization().CurrentScope.PolicyRevision; got != wantAuthorizationRevision {
		t.Fatalf("authorization policy revision = %d, want %d", got, wantAuthorizationRevision)
	}
	if got := prepared.Snapshot().Envelope().Subject.Fields["display_name"]; got != wantSnapshotName {
		t.Fatalf("snapshot subject name = %q, want %q", got, wantSnapshotName)
	}
	if got := prepared.Snapshot().Envelope().Source.Fields["display_name"]; got != wantSnapshotSourceName {
		t.Fatalf("snapshot source name = %q, want %q", got, wantSnapshotSourceName)
	}
	if got := prepared.Snapshot().Bytes(); string(got) != string(wantSnapshotBytes) {
		t.Fatalf("snapshot bytes changed: got %q want %q", got, wantSnapshotBytes)
	}
}
