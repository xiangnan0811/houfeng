package adapters

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"houfeng/internal/center/evidence"
)

func TestAssetHistoryAdapterLoadsVersionedAuthoritativeFactsWithoutRegistryKind(t *testing.T) {
	t.Parallel()
	window := evidence.TimeWindow{Start: time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)}
	source := &staticAssetHistorySource{capture: validAssetHistoryCapture(window)}
	adapter, err := NewAssetHistoryAdapter(source)
	if err != nil {
		t.Fatalf("NewAssetHistoryAdapter() error = %v", err)
	}
	got, err := adapter.Load(context.Background(), "vps_0123456789abcdef", window)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Version != "asset_history_source/v1" || len(got.RenewalDecisions) != 1 || len(got.PriceHistories) != 1 || len(got.IPHistories) != 1 || len(got.SpecSnapshots) != 1 {
		t.Fatalf("capture = %#v, want all four history families", got)
	}
	got.RenewalDecisions[0].Reason = "mutated"
	if source.capture.RenewalDecisions[0].Reason == "mutated" {
		t.Fatal("Load() returned source-owned slice")
	}
	for _, key := range evidence.KnownKindKeys() {
		if key.Kind == "asset.history" {
			t.Fatalf("unexpected registry key %q", key)
		}
	}
	if reflect.TypeOf(adapter).Implements(reflect.TypeOf((*evidence.Kind)(nil)).Elem()) {
		t.Fatal("asset history source adapter must not implement evidence.Kind")
	}
}

func TestAssetHistoryAdapterRejectsMalformedCustomSourceFacts(t *testing.T) {
	t.Parallel()
	window := evidence.TimeWindow{Start: time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)}
	tests := []struct {
		name   string
		mutate func(*AssetHistoryCapture)
	}{
		{name: "source mismatch", mutate: func(c *AssetHistoryCapture) { c.VPSID = "vps_other" }},
		{name: "count drift", mutate: func(c *AssetHistoryCapture) { c.FactCount++ }},
		{name: "duplicate identity", mutate: func(c *AssetHistoryCapture) { c.PriceHistories[0].HistoryID = c.RenewalDecisions[0].DecisionID }},
		{name: "invalid decision", mutate: func(c *AssetHistoryCapture) { c.RenewalDecisions[0].ToDecision = "invented" }},
		{name: "invalid currency", mutate: func(c *AssetHistoryCapture) { c.PriceHistories[0].ToCurrency = "usd" }},
		{name: "invalid IP", mutate: func(c *AssetHistoryCapture) { c.IPHistories[0].ToIPv4 = "not-an-ip" }},
		{name: "submicrosecond event", mutate: func(c *AssetHistoryCapture) {
			c.SpecSnapshots[0].CapturedAt = c.SpecSnapshots[0].CapturedAt.Add(time.Nanosecond)
		}},
		{name: "non canonical UTC event", mutate: func(c *AssetHistoryCapture) {
			c.SpecSnapshots[0].CapturedAt = c.SpecSnapshots[0].CapturedAt.In(time.FixedZone("offset", 8*60*60))
		}},
		{name: "future watermark", mutate: func(c *AssetHistoryCapture) {
			c.SourceWatermark = time.Date(2099, time.January, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capture := validAssetHistoryCapture(window)
			tt.mutate(&capture)
			adapter, err := NewAssetHistoryAdapter(staticAssetHistorySourceValue{capture: capture})
			if err != nil {
				t.Fatalf("NewAssetHistoryAdapter() error = %v", err)
			}
			adapter.clock = func() time.Time { return window.End }
			_, err = adapter.Load(context.Background(), "vps_0123456789abcdef", window)
			if !errors.Is(err, evidence.ErrInvalidCanonicalPayload) {
				t.Fatalf("Load() error = %v, want ErrInvalidCanonicalPayload", err)
			}
		})
	}
}

func TestAssetHistoryAdapterCanonicalizesFactOrder(t *testing.T) {
	t.Parallel()
	window := evidence.TimeWindow{Start: time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)}
	capture := validAssetHistoryCapture(window)
	later := capture.RenewalDecisions[0]
	later.DecisionID = "ren_1123456789abcdef"
	later.DecidedAt = later.DecidedAt.Add(time.Hour)
	later.RecordedAt = later.RecordedAt.Add(time.Hour)
	capture.RenewalDecisions = append([]AssetRenewalDecision{later}, capture.RenewalDecisions...)
	capture.FactCount++
	adapter, err := NewAssetHistoryAdapter(staticAssetHistorySourceValue{capture: capture})
	if err != nil {
		t.Fatalf("NewAssetHistoryAdapter() error = %v", err)
	}
	got, err := adapter.Load(context.Background(), "vps_0123456789abcdef", window)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.RenewalDecisions[0].DecisionID != "ren_0123456789abcdef" || got.RenewalDecisions[1].DecisionID != "ren_1123456789abcdef" {
		t.Fatalf("renewal order = %#v, want canonical event-time order", got.RenewalDecisions)
	}
}

type staticAssetHistorySource struct{ capture AssetHistoryCapture }
type staticAssetHistorySourceValue struct{ capture AssetHistoryCapture }

func (source *staticAssetHistorySource) LoadAssetHistory(context.Context, string, evidence.TimeWindow) (AssetHistoryCapture, error) {
	return source.capture, nil
}
func (source staticAssetHistorySourceValue) LoadAssetHistory(context.Context, string, evidence.TimeWindow) (AssetHistoryCapture, error) {
	return source.capture, nil
}

func validAssetHistoryCapture(window evidence.TimeWindow) AssetHistoryCapture {
	first := window.Start.Add(time.Hour)
	second := first.Add(time.Hour)
	third := second.Add(time.Hour)
	fourth := third.Add(time.Hour)
	return AssetHistoryCapture{Version: "asset_history_source/v1", VPSID: "vps_0123456789abcdef", ProducerVersion: "asset-ledger/v1", SourceWatermark: fourth.Add(time.Minute).Format(time.RFC3339Nano), FactCount: 4,
		RenewalDecisions: []AssetRenewalDecision{{DecisionID: "ren_0123456789abcdef", FromDecision: "unreviewed", ToDecision: "keep", Reason: "keep", DecidedAt: first, RecordedAt: first.Add(time.Minute)}},
		PriceHistories:   []AssetPriceHistory{{HistoryID: "ph_0123456789abcdef", SubscriptionID: "sub_0123456789abcdef", FromAmount: 10, ToAmount: 20, FromCurrency: "USD", ToCurrency: "USD", FromBillingPeriodUnit: "month", ToBillingPeriodUnit: "month", FromBillingPeriodLength: 1, ToBillingPeriodLength: 1, ChangedAt: second, RecordedAt: second.Add(time.Minute)}},
		IPHistories:      []AssetIPHistory{{HistoryID: "iph_0123456789abcdef", FromIPv4: "192.0.2.1", ToIPv4: "192.0.2.2", ChangedAt: third, RecordedAt: third.Add(time.Minute)}},
		SpecSnapshots:    []AssetSpecSnapshot{{SnapshotID: "vss_0123456789abcdef", ProductName: "VPS-2", OSName: "Debian 13", Virtualization: "kvm", SSHPort: 22, CapturedAt: fourth, RecordedAt: fourth.Add(time.Minute)}},
	}
}
