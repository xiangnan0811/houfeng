package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"houfeng/internal/center/assetrelations"
	"houfeng/internal/center/ids"
	"houfeng/internal/center/vpsassets"
)

type PostgresAssetRelationRepository struct{ db *pgxpool.Pool }

func NewPostgresAssetRelationRepository(db *pgxpool.Pool) *PostgresAssetRelationRepository {
	return &PostgresAssetRelationRepository{db: db}
}

func relationTables(kind string) (string, string, string, error) {
	switch kind {
	case assetrelations.Service:
		return "asset_service_associations", "asset_services", "service_id", nil
	case assetrelations.Domain:
		return "asset_domain_associations", "asset_domains", "domain_id", nil
	}
	return "", "", "", assetrelations.ErrInvalid
}
func relationColumns(kind string) string {
	if kind == assetrelations.Service {
		return "id,service_id,vps_id,target_id,null::text,address,port,started_at,ended_at,end_reason,ended_by,snapshot"
	}
	return "id,domain_id,vps_id,target_id,service_id,address,null::integer,started_at,ended_at,end_reason,ended_by,snapshot"
}
func scanAssetRelation(row interface{ Scan(...any) error }) (assetrelations.Record, error) {
	var r assetrelations.Record
	err := row.Scan(&r.AssociationID, &r.ObjectID, &r.VPSID, &r.TargetID, &r.ServiceID, &r.Address, &r.Port, &r.StartedAt, &r.EndedAt, &r.EndReason, &r.EndedBy, &r.Snapshot)
	return r, err
}
func (r *PostgresAssetRelationRepository) List(ctx context.Context, vpsID, kind string, current bool) ([]assetrelations.Record, error) {
	table, _, _, err := relationTables(kind)
	if err != nil {
		return nil, err
	}
	var exists bool
	if err = r.db.QueryRow(ctx, `select exists(select 1 from vps_assets where vps_id=$1)`, vpsID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, assetrelations.ErrNotFound
	}
	query := `select ` + relationColumns(kind) + ` from ` + table + ` where vps_id=$1`
	if current {
		query += ` and ended_at is null`
	}
	query += ` order by started_at desc,id`
	rows, err := r.db.Query(ctx, query, vpsID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]assetrelations.Record, 0)
	for rows.Next() {
		record, err := scanAssetRelation(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, record)
	}
	return result, rows.Err()
}
func (r *PostgresAssetRelationRepository) Link(ctx context.Context, vpsID, kind string, input assetrelations.LinkInput, actor string) (assetrelations.Record, error) {
	if strings.TrimSpace(vpsID) == "" || strings.TrimSpace(actor) == "" {
		return assetrelations.Record{}, assetrelations.ErrInvalid
	}
	input = assetrelations.Normalize(input)
	if err := assetrelations.Validate(kind, input); err != nil {
		return assetrelations.Record{}, err
	}
	tx, err := beginAssetGraphTx(ctx, r.db.BeginTx)
	if err != nil {
		return assetrelations.Record{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	record, err := insertAssetRelation(ctx, tx, vpsID, kind, input)
	if err != nil {
		return record, err
	}
	if err = tx.Commit(ctx); err != nil {
		return assetrelations.Record{}, err
	}
	return record, nil
}

func insertAssetRelation(ctx context.Context, tx assetServiceQueryer, vpsID, kind string, input assetrelations.LinkInput) (assetrelations.Record, error) {
	table, objects, idColumn, err := relationTables(kind)
	if err != nil {
		return assetrelations.Record{}, err
	}
	var lifecycle vpsassets.LifecycleStatus
	if err = tx.QueryRow(ctx, `select lifecycle_status from vps_assets where vps_id=$1 for update`, vpsID).Scan(&lifecycle); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return assetrelations.Record{}, assetrelations.ErrNotFound
		}
		return assetrelations.Record{}, err
	}
	if lifecycle == "archived" {
		return assetrelations.Record{}, vpsassets.ErrVPSAssetReadonly
	}
	var snapshot json.RawMessage
	if err = tx.QueryRow(ctx, `select to_jsonb(o) from `+objects+` o where `+idColumn+`=$1 for update`, input.ObjectID).Scan(&snapshot); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return assetrelations.Record{}, assetrelations.ErrNotFound
		}
		return assetrelations.Record{}, err
	}
	if input.ServiceID != nil {
		var exists bool
		if err = tx.QueryRow(ctx, `select exists(select 1 from asset_service_associations where service_id=$1 and vps_id=$2 and ended_at is null)`, *input.ServiceID, vpsID).Scan(&exists); err != nil {
			return assetrelations.Record{}, err
		}
		if !exists {
			return assetrelations.Record{}, assetrelations.ErrConflict
		}
	}
	if input.TargetID != nil {
		var status string
		if err = tx.QueryRow(ctx, `select lifecycle_status from targets where target_id=$1 for update`, *input.TargetID).Scan(&status); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return assetrelations.Record{}, assetrelations.ErrNotFound
			}
			return assetrelations.Record{}, err
		}
		if status == "retired" || status == "archived" {
			return assetrelations.Record{}, assetrelations.ErrConflict
		}
	}
	id, err := ids.New("assoc")
	if err != nil {
		return assetrelations.Record{}, err
	}
	extraColumn, extraValue := "port", any(input.Port)
	if kind == assetrelations.Domain {
		extraColumn, extraValue = "service_id", input.ServiceID
	}
	record, err := scanAssetRelation(tx.QueryRow(ctx, `insert into `+table+`(id,`+idColumn+`,vps_id,target_id,address,`+extraColumn+`,snapshot) values($1,$2,$3,$4,$5,$6,$7) returning `+relationColumns(kind), id, input.ObjectID, vpsID, input.TargetID, input.Address, extraValue, snapshot))
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return assetrelations.Record{}, assetrelations.ErrConflict
	}
	return record, err
}

func (r *PostgresAssetRelationRepository) End(ctx context.Context, vpsID, kind, id, reason, actor string) (assetrelations.Record, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" || strings.TrimSpace(actor) == "" {
		return assetrelations.Record{}, assetrelations.ErrInvalid
	}
	table, objects, idColumn, err := relationTables(kind)
	if err != nil {
		return assetrelations.Record{}, err
	}
	tx, err := beginAssetGraphTx(ctx, r.db.BeginTx)
	if err != nil {
		return assetrelations.Record{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	record, err := scanAssetRelation(tx.QueryRow(ctx, `select `+relationColumns(kind)+` from `+table+` where id=$1 and vps_id=$2 for update`, id, vpsID))
	if errors.Is(err, pgx.ErrNoRows) {
		return record, assetrelations.ErrNotFound
	}
	if err != nil {
		return record, err
	}
	if record.EndedAt != nil {
		return record, assetrelations.ErrConflict
	}
	record, err = scanAssetRelation(tx.QueryRow(ctx, `update `+table+` a set ended_at=now(),end_reason=$3,ended_by=$4,snapshot=(select to_jsonb(o) from `+objects+` o where o.`+idColumn+`=a.`+idColumn+`) where id=$1 and vps_id=$2 returning `+relationColumns(kind), id, vpsID, reason, actor))
	if err != nil {
		return record, err
	}
	if err = tx.Commit(ctx); err != nil {
		return assetrelations.Record{}, err
	}
	return record, nil
}

// archiveVPSRelations runs inside the caller's asset graph transaction. Object
// identities and associations to other VPS assets are deliberately retained.
func archiveVPSRelations(ctx context.Context, tx pgx.Tx, vpsID, reason string) error {
	for _, kind := range []string{assetrelations.Service, assetrelations.Domain} {
		table, objects, idColumn, _ := relationTables(kind)
		_, err := tx.Exec(ctx, `update `+table+` a set ended_at=now(),end_reason=$2,ended_by='system:archive',snapshot=(select to_jsonb(o) from `+objects+` o where o.`+idColumn+`=a.`+idColumn+`) where a.vps_id=$1 and a.ended_at is null`, vpsID, reason)
		if err != nil {
			return fmt.Errorf("end %s associations: %w", kind, err)
		}
	}
	return nil
}
