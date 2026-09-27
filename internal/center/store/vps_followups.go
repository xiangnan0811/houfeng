package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"houfeng/internal/center/ids"
	"houfeng/internal/center/vpsfollowups"
)

type PostgresVPSFollowupRepository struct{ db *pgxpool.Pool }

func NewPostgresVPSFollowupRepository(db *pgxpool.Pool) *PostgresVPSFollowupRepository {
	return &PostgresVPSFollowupRepository{db: db}
}

const followupColumns = `followup_id,vps_id,kind,dedupe_key,status,summary,details,resolution_reason,resolved_by,created_at,updated_at,resolved_at`

func scanVPSFollowup(row interface{ Scan(...any) error }) (vpsfollowups.Record, error) {
	var r vpsfollowups.Record
	err := row.Scan(&r.FollowupID, &r.VPSID, &r.Kind, &r.DedupeKey, &r.Status, &r.Summary, &r.Details, &r.ResolutionReason, &r.ResolvedBy, &r.CreatedAt, &r.UpdatedAt, &r.ResolvedAt)
	return r, err
}
func (r *PostgresVPSFollowupRepository) List(ctx context.Context, vpsID string) ([]vpsfollowups.Record, error) {
	var exists bool
	if err := r.db.QueryRow(ctx, `select exists(select 1 from vps_assets where vps_id=$1)`, vpsID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, vpsfollowups.ErrNotFound
	}
	rows, err := r.db.Query(ctx, `select `+followupColumns+` from vps_followups where vps_id=$1 order by created_at desc,followup_id`, vpsID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := make([]vpsfollowups.Record, 0)
	for rows.Next() {
		record, err := scanVPSFollowup(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}
func (r *PostgresVPSFollowupRepository) Create(ctx context.Context, vpsID string, input vpsfollowups.CreateInput, actor string) (vpsfollowups.Record, error) {
	input.Kind = strings.TrimSpace(input.Kind)
	input.Summary = strings.TrimSpace(input.Summary)
	if err := vpsfollowups.ValidateCreate(input); err != nil {
		return vpsfollowups.Record{}, err
	}
	if strings.TrimSpace(actor) == "" {
		return vpsfollowups.Record{}, vpsfollowups.ErrInvalid
	}
	tx, err := beginAssetGraphTx(ctx, r.db.BeginTx)
	if err != nil {
		return vpsfollowups.Record{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var exists bool
	if err = tx.QueryRow(ctx, `select exists(select 1 from vps_assets where vps_id=$1)`, vpsID).Scan(&exists); err != nil {
		return vpsfollowups.Record{}, err
	}
	if !exists {
		return vpsfollowups.Record{}, vpsfollowups.ErrNotFound
	}
	id, err := ids.New("followup")
	if err != nil {
		return vpsfollowups.Record{}, err
	}
	var details map[string]any
	if len(input.Details) > 0 {
		if err = json.Unmarshal(input.Details, &details); err != nil {
			return vpsfollowups.Record{}, vpsfollowups.ErrInvalid
		}
	}
	if details == nil {
		details = map[string]any{}
	}
	details["created_by"] = actor
	body, err := json.Marshal(details)
	if err != nil {
		return vpsfollowups.Record{}, err
	}
	record, err := scanVPSFollowup(tx.QueryRow(ctx, `insert into vps_followups(followup_id,vps_id,kind,dedupe_key,status,summary,details) values($1,$2,$3,$1,'pending',$4,$5) returning `+followupColumns, id, vpsID, input.Kind, input.Summary, body))
	if err != nil {
		return record, err
	}
	if err = tx.Commit(ctx); err != nil {
		return vpsfollowups.Record{}, err
	}
	return record, nil
}
func (r *PostgresVPSFollowupRepository) Resolve(ctx context.Context, vpsID, id string, input vpsfollowups.ResolveInput, actor string) (vpsfollowups.Record, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if err := vpsfollowups.ValidateResolve(input); err != nil {
		return vpsfollowups.Record{}, err
	}
	if strings.TrimSpace(actor) == "" {
		return vpsfollowups.Record{}, vpsfollowups.ErrInvalid
	}
	tx, err := beginAssetGraphTx(ctx, r.db.BeginTx)
	if err != nil {
		return vpsfollowups.Record{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := scanVPSFollowup(tx.QueryRow(ctx, `select `+followupColumns+` from vps_followups where followup_id=$1 and vps_id=$2 for update`, id, vpsID))
	if errors.Is(err, pgx.ErrNoRows) {
		return current, vpsfollowups.ErrNotFound
	}
	if err != nil {
		return current, err
	}
	if current.Status != "pending" {
		return current, vpsfollowups.ErrConflict
	}
	record, err := scanVPSFollowup(tx.QueryRow(ctx, `update vps_followups set status=$3,resolution_reason=$4,resolved_by=$5,resolved_at=now(),updated_at=now() where followup_id=$1 and vps_id=$2 returning `+followupColumns, id, vpsID, input.Status, input.Reason, actor))
	if err != nil {
		return record, err
	}
	if err = tx.Commit(ctx); err != nil {
		return vpsfollowups.Record{}, err
	}
	return record, nil
}

// upsertVPSFollowup coalesces observations for one occurrence. A closed decision
// remains closed for that occurrence; callers use a new dedupe key for a new
// occurrence (for example the next archived_at timestamp).
func upsertVPSFollowup(ctx context.Context, tx interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, vpsID, kind, dedupeKey, summary string, details json.RawMessage) error {
	id, err := ids.New("followup")
	if err != nil {
		return err
	}
	if len(details) == 0 {
		details = json.RawMessage(`{}`)
	}
	_, err = tx.Exec(ctx, `insert into vps_followups(followup_id,vps_id,kind,dedupe_key,status,summary,details)
	 select $1,$2,$3,$4,'pending',$5,$6
	 where not exists(select 1 from vps_followups where vps_id=$2 and kind=$3 and dedupe_key=$4 and status in ('resolved','ignored'))
	 on conflict(vps_id,kind,dedupe_key) where status='pending' do update set summary=excluded.summary,details=excluded.details,updated_at=now()`, id, vpsID, kind, dedupeKey, summary, details)
	return err
}
