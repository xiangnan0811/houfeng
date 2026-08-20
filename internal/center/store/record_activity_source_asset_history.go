package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/activity"
	"houfeng/internal/center/records"
)

// AssetHistoryActivitySource projects the four VPS asset history tables as one
// source: renewal decisions, price changes, IP changes, and spec snapshots.
//
// They are one source rather than four because they are one kind of fact — a
// business detail of a VPS changed — and a checkpoint per table would make the
// projector's position four positions that can disagree. Unioning them means one
// ordered scan by write time and one position to advance.
//
// The values themselves stay out of the projection. An IP address or an SSH host
// on a timeline is network topology, and a price is a billing figure; the
// timeline says which fact changed and when, and the VPS page shows what it
// changed to.
type AssetHistoryActivitySource struct {
	pool      *pgxpool.Pool
	namespace activity.Namespace
}

var _ activity.SourceAdapter = (*AssetHistoryActivitySource)(nil)

func NewAssetHistoryActivitySource(pool *pgxpool.Pool, namespace activity.Namespace) (*AssetHistoryActivitySource, error) {
	if pool == nil {
		return nil, errors.New("new asset history activity source: nil pool")
	}
	if namespace.ProjectID == "" {
		return nil, activity.ErrInvalidNamespace
	}
	return &AssetHistoryActivitySource{pool: pool, namespace: namespace}, nil
}

func (source *AssetHistoryActivitySource) Kind() activity.SourceKind {
	return activity.SourceKindAssetHistory
}

func (source *AssetHistoryActivitySource) IncrementalHead(ctx context.Context) (activity.SourceHead, error) {
	var databaseNow time.Time
	if err := source.pool.QueryRow(ctx, `select now()`).Scan(&databaseNow); err != nil {
		return activity.SourceHead{}, fmt.Errorf("read asset history head: %w", err)
	}
	return activity.NewIncrementalSourceHead(
		activity.SourceKindAssetHistory,
		databaseNow,
		activity.DefaultSourceSafetyLag,
	), nil
}

func (source *AssetHistoryActivitySource) AuthoritativeHead(
	ctx context.Context,
	_ activity.ExportScope,
) (activity.SourceHead, error) {
	settledThrough, horizon, err := settledTransactionBound(ctx, source.pool)
	if err != nil {
		return activity.SourceHead{}, fmt.Errorf("read asset history authoritative head: %w", err)
	}
	return activity.NewSettledSourceHead(activity.SourceKindAssetHistory, settledThrough, horizon), nil
}

func (source *AssetHistoryActivitySource) Readiness(
	_ context.Context,
	_ activity.ExportScope,
	head activity.SourceHead,
) (activity.SourceReadiness, error) {
	if head.Kind != activity.SourceKindAssetHistory || !head.SupportsCompletenessClaim() {
		return activity.SourceReadiness{}, fmt.Errorf("%w: asset history head carries no transaction horizon", activity.ErrSourceNotReady)
	}
	return activity.SourceReadiness{
		Kind:     activity.SourceKindAssetHistory,
		Head:     head,
		CaughtUp: true,
	}, nil
}

// Asset history fact types. The prefix is part of the projected event id: the
// four tables have independent primary keys that could collide once unioned, and
// a collision would silently merge two different facts into one activity.
const (
	assetFactRenewalDecision = "rnw"
	assetFactPriceChange     = "prc"
	assetFactIPChange        = "ipa"
	assetFactSpecSnapshot    = "spc"
)

// assetFactTitles labels each fact type. A type without a label cannot reach a
// timeline unlabelled.
var assetFactTitles = map[string]string{
	assetFactRenewalDecision: "续费决定已更新",
	assetFactPriceChange:     "价格已变更",
	assetFactIPChange:        "IP 已变更",
	assetFactSpecSnapshot:    "规格已记录",
}

// assetRenewalDecisions is the closed set the renewal column admits. A closed
// enum is safe to project because it is vocabulary rather than content, and the
// resulting decision is the one detail that makes the entry actionable.
var assetRenewalDecisions = map[string]struct{}{
	"unreviewed":           {},
	"keep":                 {},
	"observe":              {},
	"migrate":              {},
	"cancel":               {},
	"auto_renew_cancelled": {},
	"replaced":             {},
}

type assetHistoryActivityRow struct {
	factType    string
	factID      string
	vpsID       string
	displayName string
	occurredAt  time.Time
	createdAt   time.Time
	detail      string
}

// assetHistoryActivityScanSQL unions the four tables into one write-time ordered
// page.
//
// Each branch selects only the columns the projection needs: the identity, the
// two times, and for renewals the closed decision enum. The value columns are
// deliberately absent so no price, address, or hostname can be projected by
// accident.
const assetHistoryActivityScanSQL = `
	with facts as (
	  select '` + assetFactRenewalDecision + `' as fact_type, decision.decision_id as fact_id,
	         decision.vps_id, decision.decided_at as occurred_at, decision.created_at,
	         decision.to_decision as detail
	  from public.renewal_decisions decision
	  where decision.created_at >= $1 and decision.created_at <= $2
	  union all
	  select '` + assetFactPriceChange + `', price.price_history_id,
	         price.vps_id, price.changed_at, price.created_at, ''
	  from public.price_histories price
	  where price.created_at >= $1 and price.created_at <= $2
	  union all
	  select '` + assetFactIPChange + `', address.ip_history_id,
	         address.vps_id, address.changed_at, address.created_at, ''
	  from public.ip_histories address
	  where address.created_at >= $1 and address.created_at <= $2
	  union all
	  select '` + assetFactSpecSnapshot + `', spec.snapshot_id,
	         spec.vps_id, spec.captured_at, spec.created_at, ''
	  from public.vps_spec_snapshots spec
	  where spec.created_at >= $1 and spec.created_at <= $2
	)
	select
	  facts.fact_type, facts.fact_id, facts.vps_id,
	  coalesce(asset.display_name, ''),
	  facts.occurred_at, facts.created_at, facts.detail
	from facts
	left join public.vps_assets asset on asset.vps_id = facts.vps_id
	order by facts.created_at, facts.fact_type, facts.fact_id
	limit $3`

func (source *AssetHistoryActivitySource) ScanAfter(
	ctx context.Context,
	window activity.ScanWindow,
	limit int,
) ([]activity.CandidateEvent, error) {
	if limit <= 0 {
		limit = activity.DefaultPageSize
	}
	rows, err := source.pool.Query(
		ctx,
		assetHistoryActivityScanSQL,
		windowLowerBound(window),
		window.Through.UTC(),
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("scan asset history: %w", err)
	}
	defer rows.Close()

	candidates := make([]activity.CandidateEvent, 0, limit)
	for rows.Next() {
		var row assetHistoryActivityRow
		if err := rows.Scan(
			&row.factType, &row.factID, &row.vpsID, &row.displayName,
			&row.occurredAt, &row.createdAt, &row.detail,
		); err != nil {
			return nil, fmt.Errorf("scan asset history row: %w", err)
		}
		candidate, err := buildAssetHistoryCandidate(source.namespace, row)
		if err != nil {
			return nil, fmt.Errorf("normalize asset history %s/%s: %w", row.factType, row.factID, err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan asset history: %w", err)
	}
	return candidates, nil
}

// assetHistoryEventID prefixes the table's own key so two tables cannot mint the
// same coordinate once unioned.
func assetHistoryEventID(factType string, factID string) string {
	return factType + "-" + factID
}

func buildAssetHistoryCandidate(
	namespace activity.Namespace,
	row assetHistoryActivityRow,
) (activity.CandidateEvent, error) {
	title, known := assetFactTitles[row.factType]
	if !known {
		return activity.CandidateEvent{}, fmt.Errorf("%w: asset fact type %q", activity.ErrInvalidEventKind, row.factType)
	}
	if !records.ValidSubjectSourceID(records.SubjectKindVPS, row.vpsID) {
		return activity.CandidateEvent{}, fmt.Errorf("%w: vps %q", activity.ErrUnreachableCandidate, row.vpsID)
	}

	summary := ""
	if row.factType == assetFactRenewalDecision {
		if _, admitted := assetRenewalDecisions[row.detail]; !admitted {
			return activity.CandidateEvent{}, fmt.Errorf("%w: renewal decision %q", activity.ErrInvalidEventKind, row.detail)
		}
		summary = row.detail
	} else if row.detail != "" {
		// Only renewals carry a projectable detail. Anything else arriving with one
		// means a value column leaked into the scan.
		return activity.CandidateEvent{}, fmt.Errorf("%w: %s carries a value that must not be projected", activity.ErrInvalidPresentation, row.factType)
	}

	// These tables have no row version: a history row is written once and never
	// revised, so the fact is fully identified by which row it is.
	sourceIdentity := activity.SourceIdentity{
		Kind:    activity.SourceKindAssetHistory,
		EventID: assetHistoryEventID(row.factType, row.factID),
		Version: 1,
	}
	activityID, err := activity.NewActivityID(namespace, sourceIdentity, activity.EventKindAssetFactChanged)
	if err != nil {
		return activity.CandidateEvent{}, err
	}

	resolved, err := activity.ResolveEventTime(activity.EventTimeInput{
		Kind:          activity.EventKindAssetFactChanged,
		OccurredAt:    row.occurredAt,
		SavedAt:       row.createdAt,
		Authoritative: true,
	})
	if err != nil {
		return activity.CandidateEvent{}, err
	}

	identity := map[string]string{}
	if row.displayName != "" {
		identity["display_name"] = row.displayName
	}

	candidate := activity.CandidateEvent{
		ActivityID: activityID,
		Source:     sourceIdentity,
		EventKind:  activity.EventKindAssetFactChanged,
		EventAt:    resolved.EventAt,
		RecordedAt: resolved.RecordedAt,
		Subjects: []activity.SubjectSnapshot{{
			Kind:     records.SubjectKindVPS,
			SourceID: row.vpsID,
			Role:     records.RelationRoleAffected,
			Primary:  true,
			Identity: identity,
		}},
		Presentation: activity.Presentation{
			Version: activity.PresentationVersionV1,
			Title:   title,
			Summary: summary,
		},
		Severity: "info",
	}

	candidate.CanonicalHash = candidate.ComputeCanonicalHash()
	return candidate, nil
}

// AssetHistoryFactTypes is the closed set of fact types this source unions.
func AssetHistoryFactTypes() []string {
	types := make([]string, 0, len(assetFactTitles))
	for factType := range assetFactTitles {
		types = append(types, factType)
	}
	return types
}

// assetHistoryScanReadsColumn reports whether the union reads a named column. It
// exists so a test can hold the value-exclusion boundary rather than trusting a
// reviewer to notice a new column in the select list.
func assetHistoryScanReadsColumn(column string) bool {
	return strings.Contains(assetHistoryActivityScanSQL, column)
}
