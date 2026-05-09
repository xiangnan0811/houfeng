package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/ids"
	"houfeng/internal/center/renewals"
	"houfeng/internal/center/vpsassets"
)

var _ renewals.Repository = (*PostgresRenewalDecisionRepository)(nil)

type renewalDecisionDB interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type renewalDecisionQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type PostgresRenewalDecisionRepository struct {
	db renewalDecisionDB
}

func NewPostgresRenewalDecisionRepository(db *pgxpool.Pool) *PostgresRenewalDecisionRepository {
	return &PostgresRenewalDecisionRepository{db: db}
}

const renewalDecisionSelectColumns = `
	decision_id,
	vps_id,
	from_decision,
	to_decision,
	reason,
	decided_at,
	created_at`

type renewalDecisionScanner interface {
	Scan(dest ...any) error
}

func scanRenewalDecision(row renewalDecisionScanner) (renewals.DecisionRecord, error) {
	var record renewals.DecisionRecord
	var fromDecision *string
	var toDecision string
	if err := row.Scan(
		&record.DecisionID,
		&record.VPSID,
		&fromDecision,
		&toDecision,
		&record.Reason,
		&record.DecidedAt,
		&record.CreatedAt,
	); err != nil {
		return renewals.DecisionRecord{}, err
	}
	if fromDecision != nil {
		decision := vpsassets.RenewalDecision(*fromDecision)
		record.FromDecision = &decision
	}
	record.ToDecision = vpsassets.RenewalDecision(toDecision)
	return record, nil
}

func (r *PostgresRenewalDecisionRepository) CreateRenewalDecision(ctx context.Context, input renewals.CreateDecisionInput) (renewals.DecisionRecord, error) {
	record, err := createRenewalDecision(ctx, r.db, input)
	if err != nil {
		return renewals.DecisionRecord{}, err
	}
	return record, nil
}

func (r *PostgresRenewalDecisionRepository) ListRenewalDecisionsForVPS(ctx context.Context, vpsID string) ([]renewals.DecisionRecord, error) {
	vpsID = renewals.NormalizeVPSID(vpsID)
	if vpsID == "" {
		return nil, fmt.Errorf("%w: vps_id is required", renewals.ErrInvalidRenewalDecisionInput)
	}

	rows, err := r.db.Query(ctx, `
		select `+renewalDecisionSelectColumns+`
		from renewal_decisions
		where vps_id = $1
		order by decided_at desc, created_at desc, decision_id desc`, vpsID)
	if err != nil {
		return nil, fmt.Errorf("query renewal decisions for vps %q: %w", vpsID, err)
	}
	defer rows.Close()

	records := make([]renewals.DecisionRecord, 0)
	for rows.Next() {
		record, err := scanRenewalDecision(rows)
		if err != nil {
			return nil, fmt.Errorf("scan renewal decision for vps %q: %w", vpsID, err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate renewal decisions for vps %q: %w", vpsID, err)
	}
	return records, nil
}

func (r *PostgresRenewalDecisionRepository) GetVPSTimeline(ctx context.Context, vpsID string) (renewals.VPSTimeline, error) {
	vpsID = renewals.NormalizeVPSID(vpsID)
	if vpsID == "" {
		return renewals.VPSTimeline{}, fmt.Errorf("%w: vps_id is required", renewals.ErrInvalidRenewalDecisionInput)
	}

	var exists bool
	if err := r.db.QueryRow(ctx, `
		select exists (
			select 1
			from vps_assets
			where vps_id = $1
		)`, vpsID).Scan(&exists); err != nil {
		return renewals.VPSTimeline{}, fmt.Errorf("check vps asset %q for renewal timeline: %w", vpsID, err)
	}
	if !exists {
		return renewals.VPSTimeline{}, renewals.ErrRenewalTimelineNotFound
	}

	records, err := r.ListRenewalDecisionsForVPS(ctx, vpsID)
	if err != nil {
		return renewals.VPSTimeline{}, err
	}
	return renewals.VPSTimeline{VPSID: vpsID, RenewalDecisions: records}, nil
}

func createRenewalDecision(ctx context.Context, db renewalDecisionQueryer, input renewals.CreateDecisionInput) (renewals.DecisionRecord, error) {
	input = renewals.NormalizeCreateDecisionInput(input)
	if err := renewals.ValidateCreateDecisionInput(input); err != nil {
		return renewals.DecisionRecord{}, err
	}

	decisionID, err := ids.New("rdec")
	if err != nil {
		return renewals.DecisionRecord{}, fmt.Errorf("generate renewal decision id: %w", err)
	}

	record, err := scanRenewalDecision(db.QueryRow(ctx, `
		insert into renewal_decisions (
			decision_id,
			vps_id,
			from_decision,
			to_decision,
			reason,
			decided_at
		) values (
			$1,
			$2,
			$3,
			$4,
			$5,
			coalesce($6::timestamptz, now())
		)
		returning `+renewalDecisionSelectColumns,
		decisionID,
		input.VPSID,
		renewalDecisionPtrArg(input.FromDecision),
		string(input.ToDecision),
		input.Reason,
		input.DecidedAt,
	))
	if err != nil {
		return renewals.DecisionRecord{}, mapRenewalDecisionWriteError(err, "create renewal decision for vps %q", input.VPSID)
	}
	return record, nil
}

func renewalDecisionPtrArg(value *vpsassets.RenewalDecision) any {
	if value == nil {
		return nil
	}
	return string(*value)
}

func mapRenewalDecisionWriteError(err error, format string, args ...any) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23503":
			return renewals.ErrRenewalTimelineNotFound
		case "23514":
			return renewals.ErrInvalidRenewalDecisionInput
		}
	}
	return fmt.Errorf("%s: %w", fmt.Sprintf(format, args...), err)
}
