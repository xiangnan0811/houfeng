package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"time"
	"unicode"

	"houfeng/internal/center/evidence"
)

const AssetHistorySourceVersionV1 = "asset_history_source/v1"

type AssetHistorySource interface {
	LoadAssetHistory(context.Context, string, evidence.TimeWindow) (AssetHistoryCapture, error)
}

type AssetHistoryCapture struct {
	Version          string
	VPSID            string
	ProducerVersion  string
	SourceWatermark  string
	FactCount        uint64
	RenewalDecisions []AssetRenewalDecision
	PriceHistories   []AssetPriceHistory
	IPHistories      []AssetIPHistory
	SpecSnapshots    []AssetSpecSnapshot
}

type AssetRenewalDecision struct {
	DecisionID   string
	FromDecision string
	ToDecision   string
	Reason       string
	DecidedAt    time.Time
	RecordedAt   time.Time
}

type AssetPriceHistory struct {
	HistoryID               string
	SubscriptionID          string
	FromAmount              float64
	ToAmount                float64
	FromCurrency            string
	ToCurrency              string
	FromBillingPeriodUnit   string
	ToBillingPeriodUnit     string
	FromBillingPeriodLength int
	ToBillingPeriodLength   int
	ChangedAt               time.Time
	RecordedAt              time.Time
}

type AssetIPHistory struct {
	HistoryID  string
	FromIPv4   string
	ToIPv4     string
	FromIPv6   string
	ToIPv6     string
	ChangedAt  time.Time
	RecordedAt time.Time
}

type AssetSpecSnapshot struct {
	SnapshotID     string
	ProductName    string
	OSName         string
	Virtualization string
	SSHPort        int
	CapturedAt     time.Time
	RecordedAt     time.Time
}

type AssetHistoryAdapter struct {
	source AssetHistorySource
	clock  func() time.Time
}

func NewAssetHistoryAdapter(source AssetHistorySource) (*AssetHistoryAdapter, error) {
	if source == nil {
		return nil, fmt.Errorf("%w: nil asset history source", evidence.ErrInvalidCanonicalPayload)
	}
	return &AssetHistoryAdapter{source: source, clock: time.Now}, nil
}

func (adapter *AssetHistoryAdapter) Load(ctx context.Context, vpsID string, window evidence.TimeWindow) (AssetHistoryCapture, error) {
	if adapter == nil || adapter.source == nil || ctx == nil || !validSourceIdentifier(vpsID) || !validEvidenceWindow(window) {
		return AssetHistoryCapture{}, fmt.Errorf("%w: asset history request", evidence.ErrInvalidCanonicalPayload)
	}
	capture, err := adapter.source.LoadAssetHistory(ctx, vpsID, window)
	if err != nil {
		return AssetHistoryCapture{}, err
	}
	total := uint64(len(capture.RenewalDecisions) + len(capture.PriceHistories) + len(capture.IPHistories) + len(capture.SpecSnapshots))
	if capture.FactCount == 0 || capture.FactCount != total || total > evidence.MaxSnapshotDataPoints {
		return AssetHistoryCapture{}, fmt.Errorf("%w: asset history source bound", evidence.ErrInvalidCanonicalPayload)
	}
	capture = cloneAndSortAssetHistoryCapture(capture)
	if err := validateAssetHistoryCapture(capture, vpsID, window, adapter.clock().UTC()); err != nil {
		return AssetHistoryCapture{}, err
	}
	return capture, nil
}

func cloneAndSortAssetHistoryCapture(capture AssetHistoryCapture) AssetHistoryCapture {
	capture.RenewalDecisions = append([]AssetRenewalDecision(nil), capture.RenewalDecisions...)
	capture.PriceHistories = append([]AssetPriceHistory(nil), capture.PriceHistories...)
	capture.IPHistories = append([]AssetIPHistory(nil), capture.IPHistories...)
	capture.SpecSnapshots = append([]AssetSpecSnapshot(nil), capture.SpecSnapshots...)
	sort.Slice(capture.RenewalDecisions, func(left, right int) bool {
		if !capture.RenewalDecisions[left].DecidedAt.Equal(capture.RenewalDecisions[right].DecidedAt) {
			return capture.RenewalDecisions[left].DecidedAt.Before(capture.RenewalDecisions[right].DecidedAt)
		}
		return capture.RenewalDecisions[left].DecisionID < capture.RenewalDecisions[right].DecisionID
	})
	sort.Slice(capture.PriceHistories, func(left, right int) bool {
		if !capture.PriceHistories[left].ChangedAt.Equal(capture.PriceHistories[right].ChangedAt) {
			return capture.PriceHistories[left].ChangedAt.Before(capture.PriceHistories[right].ChangedAt)
		}
		return capture.PriceHistories[left].HistoryID < capture.PriceHistories[right].HistoryID
	})
	sort.Slice(capture.IPHistories, func(left, right int) bool {
		if !capture.IPHistories[left].ChangedAt.Equal(capture.IPHistories[right].ChangedAt) {
			return capture.IPHistories[left].ChangedAt.Before(capture.IPHistories[right].ChangedAt)
		}
		return capture.IPHistories[left].HistoryID < capture.IPHistories[right].HistoryID
	})
	sort.Slice(capture.SpecSnapshots, func(left, right int) bool {
		if !capture.SpecSnapshots[left].CapturedAt.Equal(capture.SpecSnapshots[right].CapturedAt) {
			return capture.SpecSnapshots[left].CapturedAt.Before(capture.SpecSnapshots[right].CapturedAt)
		}
		return capture.SpecSnapshots[left].SnapshotID < capture.SpecSnapshots[right].SnapshotID
	})
	return capture
}

func validateAssetHistoryCapture(capture AssetHistoryCapture, vpsID string, window evidence.TimeWindow, now time.Time) error {
	total := uint64(len(capture.RenewalDecisions) + len(capture.PriceHistories) + len(capture.IPHistories) + len(capture.SpecSnapshots))
	watermark, watermarkErr := parseCanonicalPostgresTimestamp(capture.SourceWatermark)
	if capture.Version != AssetHistorySourceVersionV1 || capture.VPSID != vpsID || !validVersionString(capture.ProducerVersion) || capture.FactCount == 0 || capture.FactCount != total || total > evidence.MaxSnapshotDataPoints || watermarkErr != nil || watermark.After(now) {
		return fmt.Errorf("%w: asset history source", evidence.ErrInvalidCanonicalPayload)
	}
	seen := make(map[string]struct{}, total)
	latestRecorded := time.Time{}
	checkIdentityAndTime := func(identity string, eventAt, recordedAt time.Time) error {
		if !validSourceIdentifier(identity) || !canonicalTask4Timestamp(eventAt) || !canonicalTask4Timestamp(recordedAt) || recordedAt.Before(eventAt) || eventAt.Before(window.Start) || !eventAt.Before(window.End) || recordedAt.After(watermark) {
			return evidence.ErrInvalidCanonicalPayload
		}
		if _, duplicate := seen[identity]; duplicate {
			return evidence.ErrInvalidCanonicalPayload
		}
		seen[identity] = struct{}{}
		if recordedAt.After(latestRecorded) {
			latestRecorded = recordedAt
		}
		return nil
	}
	for _, decision := range capture.RenewalDecisions {
		if checkIdentityAndTime(decision.DecisionID, decision.DecidedAt, decision.RecordedAt) != nil || !validOptionalRenewalDecision(decision.FromDecision) || !validRenewalDecision(decision.ToDecision) || !safeActivityText(decision.Reason, 2048) {
			return fmt.Errorf("%w: asset renewal decision", evidence.ErrInvalidCanonicalPayload)
		}
	}
	for _, history := range capture.PriceHistories {
		if checkIdentityAndTime(history.HistoryID, history.ChangedAt, history.RecordedAt) != nil || !validSourceIdentifier(history.SubscriptionID) || !finiteNonNegative(history.FromAmount) || !finiteNonNegative(history.ToAmount) || !currencyCodePattern.MatchString(history.FromCurrency) || !currencyCodePattern.MatchString(history.ToCurrency) || !knownBillingPeriod(history.FromBillingPeriodUnit) || !knownBillingPeriod(history.ToBillingPeriodUnit) || history.FromBillingPeriodLength <= 0 || history.ToBillingPeriodLength <= 0 {
			return fmt.Errorf("%w: asset price history", evidence.ErrInvalidCanonicalPayload)
		}
	}
	for _, history := range capture.IPHistories {
		if checkIdentityAndTime(history.HistoryID, history.ChangedAt, history.RecordedAt) != nil || !validOptionalIP(history.FromIPv4, true) || !validOptionalIP(history.ToIPv4, true) || !validOptionalIP(history.FromIPv6, false) || !validOptionalIP(history.ToIPv6, false) || (history.FromIPv4 == history.ToIPv4 && history.FromIPv6 == history.ToIPv6) {
			return fmt.Errorf("%w: asset IP history", evidence.ErrInvalidCanonicalPayload)
		}
	}
	for _, snapshot := range capture.SpecSnapshots {
		if checkIdentityAndTime(snapshot.SnapshotID, snapshot.CapturedAt, snapshot.RecordedAt) != nil || !safeActivityText(snapshot.ProductName, 512) || !safeActivityText(snapshot.OSName, 512) || !safeActivityText(snapshot.Virtualization, 128) || snapshot.SSHPort < 1 || snapshot.SSHPort > 65535 {
			return fmt.Errorf("%w: asset spec snapshot", evidence.ErrInvalidCanonicalPayload)
		}
	}
	if watermark.Before(latestRecorded) {
		return fmt.Errorf("%w: asset history watermark", evidence.ErrInvalidCanonicalPayload)
	}
	return nil
}

func validRenewalDecision(value string) bool {
	switch value {
	case "unreviewed", "keep", "observe", "migrate", "cancel", "auto_renew_cancelled", "replaced":
		return true
	default:
		return false
	}
}

func validOptionalRenewalDecision(value string) bool {
	return value == "" || validRenewalDecision(value)
}

func validOptionalIP(value string, wantIPv4 bool) bool {
	if value == "" {
		return true
	}
	address, err := netip.ParseAddr(value)
	if err != nil || address.IsUnspecified() {
		return false
	}
	if wantIPv4 {
		return address.Is4()
	}
	return address.Is6() && !address.Is4In6()
}

func safeActivityText(value string, maximum int) bool {
	if strings.TrimSpace(value) != value || len(value) > maximum {
		return false
	}
	trimmed := strings.TrimSpace(value)
	if (strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")) && json.Valid([]byte(trimmed)) {
		return false
	}
	lower := strings.ToLower(value)
	if strings.Contains(lower, "://") {
		return false
	}
	compact := strings.Map(func(character rune) rune {
		if unicode.IsSpace(character) {
			return -1
		}
		return character
	}, lower)
	for _, assignment := range []string{"token=", "password=", "secret=", "cookie=", "api_key=", "api-key="} {
		if strings.Contains(compact, assignment) {
			return false
		}
	}
	return true
}
