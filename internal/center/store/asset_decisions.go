package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/assetdecisions"
	"houfeng/internal/center/ids"
	"houfeng/internal/center/subscriptions"
)

var _ assetdecisions.Repository = (*PostgresAssetDecisionRepository)(nil)

type assetDecisionDB interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type assetDecisionTxBeginner interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

type assetDecisionTx interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Commit(context.Context) error
	Rollback(context.Context) error
}

type PostgresAssetDecisionRepository struct {
	db      assetDecisionDB
	beginTx func(context.Context, pgx.TxOptions) (assetDecisionTx, error)
}

const assetDecisionRecordSummaryColumns = `
	record_id,
	title,
	goal,
	status,
	source_type,
	source_group_id,
	source_group_type,
	source_view,
	scope_key,
	scope_label,
	renew_within_days,
	member_count,
	followup_todo_count,
	followup_in_progress_count,
	followup_blocked_count,
	followup_done_count,
	followup_skipped_count,
	evidence_snapshot,
	created_at,
	updated_at,
	decided_at,
	completed_at`

func NewPostgresAssetDecisionRepository(db *pgxpool.Pool) *PostgresAssetDecisionRepository {
	return &PostgresAssetDecisionRepository{
		db: db,
		beginTx: func(ctx context.Context, opts pgx.TxOptions) (assetDecisionTx, error) {
			return db.BeginTx(ctx, opts)
		},
	}
}

func (r *PostgresAssetDecisionRepository) GetOverview(ctx context.Context, filters assetdecisions.ListFilters) (assetdecisions.Overview, error) {
	facts, err := r.loadFacts(ctx)
	if err != nil {
		return assetdecisions.Overview{}, err
	}
	return assetdecisions.DeriveOverview(facts, filters)
}

func (r *PostgresAssetDecisionRepository) ListGroups(ctx context.Context, filters assetdecisions.ListFilters) ([]assetdecisions.GroupSummary, error) {
	facts, err := r.loadFacts(ctx)
	if err != nil {
		return nil, err
	}
	groups, err := assetdecisions.DeriveGroups(facts, filters)
	if err != nil {
		return nil, err
	}
	summaries := make([]assetdecisions.GroupSummary, 0, len(groups))
	for _, group := range groups {
		summaries = append(summaries, group.GroupSummary)
	}
	return summaries, nil
}

func (r *PostgresAssetDecisionRepository) GetGroup(ctx context.Context, groupID string, filters assetdecisions.ListFilters) (assetdecisions.GroupDetail, error) {
	facts, err := r.loadFacts(ctx)
	if err != nil {
		return assetdecisions.GroupDetail{}, err
	}
	return assetdecisions.FindGroup(facts, groupID, filters)
}

func (r *PostgresAssetDecisionRepository) ListRecords(ctx context.Context, filters assetdecisions.ListFilters) ([]assetdecisions.RecordSummary, error) {
	filters = assetdecisions.NormalizeFilters(filters)
	if err := assetdecisions.ValidateFilters(filters); err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, `
		select `+assetDecisionRecordSummaryColumns+`
		from asset_decision_records_with_counts
		order by updated_at desc, record_id desc`)
	if err != nil {
		return nil, fmt.Errorf("query asset decision records: %w", err)
	}
	defer rows.Close()

	records := []assetdecisions.RecordSummary{}
	for rows.Next() {
		record, err := scanAssetDecisionRecordSummary(rows)
		if err != nil {
			return nil, fmt.Errorf("scan asset decision record: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate asset decision records: %w", err)
	}
	if len(records) == 0 {
		return records, nil
	}
	facts, err := r.loadFacts(ctx)
	if err != nil {
		return nil, err
	}
	membersByRecord, err := r.listMembersForRecords(ctx, recordIDs(records))
	if err != nil {
		return nil, err
	}
	records = assetdecisions.ApplyExecutionReadbackToSummaries(records, membersByRecord, facts)
	return assetdecisions.FilterRecordSummaries(records, membersByRecord, facts, filters), nil
}

func (r *PostgresAssetDecisionRepository) CreateRecord(ctx context.Context, input assetdecisions.CreateRecordInput) (assetdecisions.RecordDetail, error) {
	input = assetdecisions.NormalizeCreateRecordInput(input)
	if err := assetdecisions.ValidateCreateRecordInput(input); err != nil {
		return assetdecisions.RecordDetail{}, err
	}

	facts, err := r.loadFacts(ctx)
	if err != nil {
		return assetdecisions.RecordDetail{}, err
	}
	recordSource, err := r.recordSourceFromInput(ctx, input, facts)
	if err != nil {
		return assetdecisions.RecordDetail{}, err
	}
	input.RenewWithinDays = recordSource.renewWithinDays
	if input.Title == "" {
		input.Title = recordSource.title
	}

	recordID, err := ids.New("adr")
	if err != nil {
		return assetdecisions.RecordDetail{}, fmt.Errorf("generate asset decision record id: %w", err)
	}
	groupSnapshot, err := marshalAssetDecisionSnapshot(recordSource.evidenceSnapshot)
	if err != nil {
		return assetdecisions.RecordDetail{}, err
	}
	now := time.Now().UTC()
	decidedAt, completedAt := recordStatusTimestamps(input.Status, now)

	beginTx := r.beginTx
	if beginTx == nil {
		beginner, ok := r.db.(assetDecisionTxBeginner)
		if !ok {
			return assetdecisions.RecordDetail{}, fmt.Errorf("begin asset decision record transaction: transaction not supported")
		}
		beginTx = func(ctx context.Context, opts pgx.TxOptions) (assetDecisionTx, error) {
			return beginner.BeginTx(ctx, opts)
		}
	}
	tx, err := beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return assetdecisions.RecordDetail{}, fmt.Errorf("begin asset decision record transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		insert into asset_decision_records (
			record_id,
			source_type,
			source_group_id,
			source_group_type,
			source_view,
			scope_key,
			scope_label,
			renew_within_days,
			title,
			goal,
			status,
			evidence_snapshot,
			created_at,
			updated_at,
			decided_at,
			completed_at
		) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::jsonb,$13,$13,$14,$15)`,
		recordID,
		recordSource.sourceType,
		recordSource.sourceGroupID,
		string(recordSource.sourceGroupType),
		string(recordSource.sourceView),
		recordSource.scopeKey,
		recordSource.scopeLabel,
		input.RenewWithinDays,
		input.Title,
		input.Goal,
		string(input.Status),
		groupSnapshot,
		now,
		decidedAt,
		completedAt,
	); err != nil {
		if isAssetDecisionInvalidPostgresError(err) {
			return assetdecisions.RecordDetail{}, assetdecisions.ErrInvalidAssetDecisionInput
		}
		return assetdecisions.RecordDetail{}, fmt.Errorf("insert asset decision record: %w", err)
	}

	recordMembers := make([]assetdecisions.RecordMember, 0, len(recordSource.members))
	for _, member := range recordSource.members {
		memberSnapshotMap := member.evidenceSnapshot
		memberSnapshot, err := marshalAssetDecisionSnapshot(memberSnapshotMap)
		if err != nil {
			return assetdecisions.RecordDetail{}, err
		}
		if _, err := tx.Exec(ctx, `
			insert into asset_decision_record_members (
				record_id,
				vps_id,
				display_name,
				suggested_role,
				decided_role,
				suggested_action,
				decided_action,
				reason,
				evidence_snapshot,
				created_at,
				updated_at
			) values ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10,$10)`,
			recordID,
			member.VPS.VPSID,
			member.VPS.DisplayName,
			string(member.SuggestedRole),
			string(member.decidedRole),
			string(member.SuggestedAction),
			string(member.decidedAction),
			member.reason,
			memberSnapshot,
			now,
		); err != nil {
			if isAssetDecisionInvalidPostgresError(err) {
				return assetdecisions.RecordDetail{}, assetdecisions.ErrInvalidAssetDecisionInput
			}
			return assetdecisions.RecordDetail{}, fmt.Errorf("insert asset decision record member %q: %w", member.VPS.VPSID, err)
		}
		recordMembers = append(recordMembers, assetdecisions.RecordMember{
			RecordID:         recordID,
			VPSID:            member.VPS.VPSID,
			DisplayName:      member.VPS.DisplayName,
			SuggestedRole:    member.SuggestedRole,
			DecidedRole:      member.decidedRole,
			SuggestedAction:  member.SuggestedAction,
			DecidedAction:    member.decidedAction,
			Reason:           member.reason,
			FollowupStatus:   assetdecisions.FollowupTodo,
			EvidenceSnapshot: memberSnapshotMap,
			CreatedAt:        now,
			UpdatedAt:        now,
		})
	}

	if err := tx.Commit(ctx); err != nil {
		return assetdecisions.RecordDetail{}, fmt.Errorf("commit asset decision record transaction: %w", err)
	}
	return assetdecisions.ApplyExecutionReadback(assetdecisions.RecordDetail{
		RecordSummary: assetdecisions.RecordSummary{
			RecordID:          recordID,
			Title:             input.Title,
			Goal:              input.Goal,
			Status:            input.Status,
			SourceType:        recordSource.sourceType,
			SourceGroupID:     recordSource.sourceGroupID,
			SourceGroupType:   recordSource.sourceGroupType,
			SourceView:        recordSource.sourceView,
			ScopeKey:          recordSource.scopeKey,
			ScopeLabel:        recordSource.scopeLabel,
			RenewWithinDays:   input.RenewWithinDays,
			MemberCount:       len(recordMembers),
			FollowupTodoCount: len(recordMembers),
			EvidenceSnapshot:  recordSource.evidenceSnapshot,
			CreatedAt:         now,
			UpdatedAt:         now,
			DecidedAt:         decidedAt,
			CompletedAt:       completedAt,
		},
		Members: recordMembers,
	}, facts), nil
}

func (r *PostgresAssetDecisionRepository) GetRecord(ctx context.Context, recordID string) (assetdecisions.RecordDetail, error) {
	recordID = strings.TrimSpace(recordID)
	if recordID == "" {
		return assetdecisions.RecordDetail{}, assetdecisions.ErrAssetDecisionRecordNotFound
	}
	summary, err := scanAssetDecisionRecordSummary(r.db.QueryRow(ctx, `
		select `+assetDecisionRecordSummaryColumns+`
		from asset_decision_records_with_counts
		where record_id = $1`, recordID))
	if errors.Is(err, pgx.ErrNoRows) {
		return assetdecisions.RecordDetail{}, assetdecisions.ErrAssetDecisionRecordNotFound
	}
	if err != nil {
		return assetdecisions.RecordDetail{}, fmt.Errorf("query asset decision record %q: %w", recordID, err)
	}
	members, err := r.listRecordMembers(ctx, recordID)
	if err != nil {
		return assetdecisions.RecordDetail{}, err
	}
	facts, err := r.loadFacts(ctx)
	if err != nil {
		return assetdecisions.RecordDetail{}, err
	}
	return assetdecisions.ApplyExecutionReadback(assetdecisions.RecordDetail{RecordSummary: summary, Members: members}, facts), nil
}

func (r *PostgresAssetDecisionRepository) PatchRecord(ctx context.Context, recordID string, input assetdecisions.PatchRecordInput) (assetdecisions.RecordDetail, error) {
	recordID = strings.TrimSpace(recordID)
	input = assetdecisions.NormalizePatchRecordInput(input)
	if recordID == "" {
		return assetdecisions.RecordDetail{}, assetdecisions.ErrAssetDecisionRecordNotFound
	}
	if err := assetdecisions.ValidatePatchRecordInput(input); err != nil {
		return assetdecisions.RecordDetail{}, err
	}
	hasMemberPatch := len(input.Members) > 0
	if !input.Title.Set && !input.Goal.Set && !input.Status.Set && !hasMemberPatch {
		return r.GetRecord(ctx, recordID)
	}

	now := time.Now().UTC()
	decidedAt, completedAt := recordStatusTimestamps(input.Status.Value, now)
	beginTx := r.beginTx
	if beginTx == nil {
		beginner, ok := r.db.(assetDecisionTxBeginner)
		if !ok {
			return assetdecisions.RecordDetail{}, fmt.Errorf("begin asset decision record patch transaction: transaction not supported")
		}
		beginTx = func(ctx context.Context, opts pgx.TxOptions) (assetDecisionTx, error) {
			return beginner.BeginTx(ctx, opts)
		}
	}
	tx, err := beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return assetdecisions.RecordDetail{}, fmt.Errorf("begin asset decision record patch transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `
		update asset_decision_records
		set title = case when $2::boolean then $3 else title end,
		    goal = case when $4::boolean then $5 else goal end,
		    status = case when $6::boolean then $7 else status end,
		    decided_at = case
		      when $6::boolean and $7 in ('decided', 'in_progress', 'completed') and decided_at is null then $8
		      else decided_at
		    end,
		    completed_at = case
		      when $6::boolean and $7 = 'completed' and completed_at is null then $9
		      when $6::boolean and $7 <> 'completed' then null
		      else completed_at
		    end,
		    updated_at = $10
		where record_id = $1`,
		recordID,
		input.Title.Set,
		input.Title.Value,
		input.Goal.Set,
		input.Goal.Value,
		input.Status.Set,
		string(input.Status.Value),
		decidedAt,
		completedAt,
		now,
	)
	if err != nil {
		if isAssetDecisionInvalidPostgresError(err) {
			return assetdecisions.RecordDetail{}, assetdecisions.ErrInvalidAssetDecisionInput
		}
		return assetdecisions.RecordDetail{}, fmt.Errorf("patch asset decision record %q: %w", recordID, err)
	}
	if tag.RowsAffected() == 0 {
		return assetdecisions.RecordDetail{}, assetdecisions.ErrAssetDecisionRecordNotFound
	}

	for _, member := range input.Members {
		tag, err := tx.Exec(ctx, `
			update asset_decision_record_members
			set followup_status = case when $3::boolean then $4 else followup_status end,
			    followup_note = case when $5::boolean then $6 else followup_note end,
			    followup_updated_at = $7,
			    updated_at = $7
			where record_id = $1 and vps_id = $2`,
			recordID,
			member.VPSID,
			member.FollowupStatus.Set,
			string(member.FollowupStatus.Value),
			member.FollowupNote.Set,
			member.FollowupNote.Value,
			now,
		)
		if err != nil {
			if isAssetDecisionInvalidPostgresError(err) {
				return assetdecisions.RecordDetail{}, assetdecisions.ErrInvalidAssetDecisionInput
			}
			return assetdecisions.RecordDetail{}, fmt.Errorf("patch asset decision record member %q/%q: %w", recordID, member.VPSID, err)
		}
		if tag.RowsAffected() == 0 {
			return assetdecisions.RecordDetail{}, assetdecisions.ErrInvalidAssetDecisionInput
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return assetdecisions.RecordDetail{}, fmt.Errorf("commit asset decision record patch transaction: %w", err)
	}
	return r.GetRecord(ctx, recordID)
}

func (r *PostgresAssetDecisionRepository) loadFacts(ctx context.Context) ([]assetdecisions.Fact, error) {
	rows, err := r.db.Query(ctx, `
		with primary_subscriptions as (
			select distinct on (s.vps_id)
				s.subscription_id,
				s.vps_id,
				s.price::float8,
				s.currency,
				s.billing_cycle,
				s.billing_months,
				s.billing_period_unit,
				s.billing_period_length,
				s.monthly_price::float8,
				s.started_at,
				s.renew_at,
				s.auto_renew,
				s.auto_renew_cancelled,
				s.renewal_mode,
				s.status,
				s.payment_method,
				s.display_name,
				s.cost_category,
				s.labels,
				s.trial_ends_at,
				s.ends_at,
				s.note,
				s.created_at,
				s.updated_at
			from subscriptions s
			order by
				s.vps_id,
				case when s.status = 'active' then 0 else 1 end,
				s.renew_at asc nulls last,
				s.updated_at desc,
				s.subscription_id asc
		),
		subscription_rollup as (
			select
				s.vps_id,
				count(*)::int as subscription_count,
				(count(*) filter (where s.status = 'active'))::int as active_subscription_count,
				(count(*) filter (where s.status in ('expired', 'cancelled', 'paused')))::int as inactive_subscription_count
			from subscriptions s
			group by s.vps_id
		),
		service_rollup as (
			select
				vps_id,
				count(*)::int as service_count
			from asset_services
			group by vps_id
		),
		domain_rollup as (
			select
				vps_id,
				count(*)::int as domain_count
			from asset_domains
			group by vps_id
		),
		target_rollup as (
			select
				a.vps_id,
				count(distinct a.target_id)::int as target_count,
				(count(distinct a.target_id) filter (where t.run_status not in ('已归档', '暂停')))::int as running_target_count
			from (
				select vps_id, target_id from asset_services where target_id is not null
				union all
				select vps_id, target_id from asset_domains where target_id is not null
			) a
			left join targets t on t.target_id = a.target_id
			group by a.vps_id
		),
		monitoring_rollup as (
			select
				l.vps_id,
				count(*)::int as monitoring_link_count,
				(count(*) filter (where n.lifecycle_status not in ('不续费', '已退役')))::int as running_monitoring_count,
				(count(*) filter (where n.current_health_status <> '正常'))::int as abnormal_monitoring_count,
				coalesce(sum(n.current_active_incident_count), 0)::int as active_incident_count,
				coalesce((array_remove(array_agg(nullif(n.current_primary_issue_summary, '') order by n.current_active_incident_count desc, n.updated_at desc), null))[1], '') as primary_issue_summary
			from vps_monitoring_instance_links l
			left join monitoring_instances n on n.monitoring_instance_id = l.monitoring_instance_id
			where l.unlinked_at is null
			group by l.vps_id
		)
		select
			v.vps_id,
			v.display_name,
			v.provider_id,
			coalesce(nullif(v.provider_name, ''), p.name, ''),
			v.product_name,
			v.order_ref,
			v.country,
			v.region,
			v.city,
			v.datacenter,
			v.ipv4,
			v.ipv6,
			v.ssh_host,
			v.ssh_port,
			v.ssh_user,
			v.os_name,
			v.virtualization,
			v.lifecycle_status,
			v.usage_status,
			v.renewal_decision,
			v.importance,
			v.labels,
			v.note,
			coalesce(mr.monitoring_link_count, 0),
			coalesce(mr.running_monitoring_count, 0),
			coalesce(tr.running_target_count, 0),
			v.created_at,
			v.updated_at,
			v.archived_at,
			coalesce(sr.subscription_count, 0),
			coalesce(sr.active_subscription_count, 0),
			coalesce(sr.inactive_subscription_count, 0),
			coalesce(svr.service_count, 0),
			coalesce(dr.domain_count, 0),
			coalesce(tr.target_count, 0),
			coalesce(tr.running_target_count, 0),
			coalesce(mr.monitoring_link_count, 0),
			coalesce(mr.running_monitoring_count, 0),
			coalesce(mr.abnormal_monitoring_count, 0),
			coalesce(mr.active_incident_count, 0),
			coalesce(mr.primary_issue_summary, ''),
			ps.subscription_id is not null,
			coalesce(ps.subscription_id, ''),
			coalesce(ps.vps_id, ''),
			coalesce(ps.price, 0),
			coalesce(ps.currency, ''),
			coalesce(ps.billing_cycle, ''),
			coalesce(ps.billing_months, 0),
			coalesce(ps.billing_period_unit, ''),
			coalesce(ps.billing_period_length, 0),
			coalesce(ps.monthly_price, 0),
			ps.started_at,
			ps.renew_at,
			coalesce(ps.auto_renew, false),
			coalesce(ps.auto_renew_cancelled, false),
			coalesce(ps.renewal_mode, ''),
			coalesce(ps.status, ''),
			coalesce(ps.payment_method, ''),
			coalesce(ps.display_name, ''),
			coalesce(ps.cost_category, ''),
			coalesce(ps.labels, '{}'::text[]),
			ps.trial_ends_at,
			ps.ends_at,
			coalesce(ps.note, ''),
			ps.created_at,
			ps.updated_at
		from vps_assets v
		left join providers p on p.provider_id = v.provider_id
		left join subscription_rollup sr on sr.vps_id = v.vps_id
		left join primary_subscriptions ps on ps.vps_id = v.vps_id
		left join service_rollup svr on svr.vps_id = v.vps_id
		left join domain_rollup dr on dr.vps_id = v.vps_id
		left join target_rollup tr on tr.vps_id = v.vps_id
		left join monitoring_rollup mr on mr.vps_id = v.vps_id
		where v.lifecycle_status not in ('cancelled', 'archived')
		order by lower(v.display_name), v.vps_id`)
	if err != nil {
		return nil, fmt.Errorf("query asset decision facts: %w", err)
	}
	defer rows.Close()

	facts := make([]assetdecisions.Fact, 0)
	for rows.Next() {
		fact, err := scanAssetDecisionFact(rows)
		if err != nil {
			return nil, fmt.Errorf("scan asset decision fact: %w", err)
		}
		facts = append(facts, fact)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate asset decision facts: %w", err)
	}
	return facts, nil
}

func (r *PostgresAssetDecisionRepository) listRecordMembers(ctx context.Context, recordID string) ([]assetdecisions.RecordMember, error) {
	rows, err := r.db.Query(ctx, `
		select
			record_id,
			vps_id,
			display_name,
			suggested_role,
			decided_role,
			suggested_action,
			decided_action,
			reason,
			followup_status,
			followup_note,
			followup_updated_at,
			evidence_snapshot,
			created_at,
			updated_at
		from asset_decision_record_members
		where record_id = $1
		order by display_name asc, vps_id asc`, recordID)
	if err != nil {
		return nil, fmt.Errorf("query asset decision record members for %q: %w", recordID, err)
	}
	defer rows.Close()

	members := []assetdecisions.RecordMember{}
	for rows.Next() {
		member, err := scanAssetDecisionRecordMember(rows)
		if err != nil {
			return nil, fmt.Errorf("scan asset decision record member: %w", err)
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate asset decision record members: %w", err)
	}
	return members, nil
}

func (r *PostgresAssetDecisionRepository) listMembersForRecords(ctx context.Context, recordIDs []string) (map[string][]assetdecisions.RecordMember, error) {
	membersByRecord := make(map[string][]assetdecisions.RecordMember, len(recordIDs))
	if len(recordIDs) == 0 {
		return membersByRecord, nil
	}
	rows, err := r.db.Query(ctx, `
		select
			record_id,
			vps_id,
			display_name,
			suggested_role,
			decided_role,
			suggested_action,
			decided_action,
			reason,
			followup_status,
			followup_note,
			followup_updated_at,
			evidence_snapshot,
			created_at,
			updated_at
		from asset_decision_record_members
		where record_id = any($1)
		order by record_id asc, display_name asc, vps_id asc`, recordIDs)
	if err != nil {
		return nil, fmt.Errorf("query asset decision record members for readback: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		member, err := scanAssetDecisionRecordMember(rows)
		if err != nil {
			return nil, fmt.Errorf("scan asset decision record member for readback: %w", err)
		}
		membersByRecord[member.RecordID] = append(membersByRecord[member.RecordID], member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate asset decision record members for readback: %w", err)
	}
	return membersByRecord, nil
}

func recordIDs(records []assetdecisions.RecordSummary) []string {
	ids := make([]string, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.RecordID)
	}
	return ids
}

type assetDecisionFactScanner interface {
	Scan(dest ...any) error
}

func scanAssetDecisionFact(row assetDecisionFactScanner) (assetdecisions.Fact, error) {
	var (
		fact        assetdecisions.Fact
		hasSub      bool
		sub         subscriptions.Record
		startedAt   *time.Time
		renewAt     *time.Time
		trialEndsAt *time.Time
		endsAt      *time.Time
		subCreated  *time.Time
		subUpdated  *time.Time
	)
	if err := row.Scan(
		&fact.VPS.VPSID,
		&fact.VPS.DisplayName,
		&fact.VPS.ProviderID,
		&fact.VPS.ProviderName,
		&fact.VPS.ProductName,
		&fact.VPS.OrderRef,
		&fact.VPS.Country,
		&fact.VPS.Region,
		&fact.VPS.City,
		&fact.VPS.Datacenter,
		&fact.VPS.IPv4,
		&fact.VPS.IPv6,
		&fact.VPS.SSHHost,
		&fact.VPS.SSHPort,
		&fact.VPS.SSHUser,
		&fact.VPS.OSName,
		&fact.VPS.Virtualization,
		&fact.VPS.LifecycleStatus,
		&fact.VPS.UsageStatus,
		&fact.VPS.RenewalDecision,
		&fact.VPS.Importance,
		&fact.VPS.Labels,
		&fact.VPS.Note,
		&fact.VPS.ActiveMonitoringInstanceLinkCount,
		&fact.VPS.RunningMonitoringInstanceCount,
		&fact.VPS.RunningTargetCount,
		&fact.VPS.CreatedAt,
		&fact.VPS.UpdatedAt,
		&fact.VPS.ArchivedAt,
		&fact.SubscriptionCount,
		&fact.ActiveSubscriptionCount,
		&fact.InactiveSubscriptionCount,
		&fact.ServiceCount,
		&fact.DomainCount,
		&fact.TargetCount,
		&fact.RunningTargetCount,
		&fact.MonitoringLinkCount,
		&fact.RunningMonitoringCount,
		&fact.AbnormalMonitoringCount,
		&fact.ActiveIncidentCount,
		&fact.PrimaryIssueSummary,
		&hasSub,
		&sub.SubscriptionID,
		&sub.VPSID,
		&sub.Price,
		&sub.Currency,
		&sub.BillingCycle,
		&sub.BillingMonths,
		&sub.BillingPeriodUnit,
		&sub.BillingPeriodLength,
		&sub.MonthlyPrice,
		&startedAt,
		&renewAt,
		&sub.AutoRenew,
		&sub.AutoRenewCancelled,
		&sub.RenewalMode,
		&sub.Status,
		&sub.PaymentMethod,
		&sub.DisplayName,
		&sub.CostCategory,
		&sub.Labels,
		&trialEndsAt,
		&endsAt,
		&sub.Note,
		&subCreated,
		&subUpdated,
	); err != nil {
		return assetdecisions.Fact{}, err
	}

	fact.SourceAvailability = assetdecisions.SourceAvailability{
		Subscriptions: true,
		Services:      true,
		Domains:       true,
		Monitoring:    true,
		Targets:       true,
	}
	if hasSub {
		sub.StartedAt = subscriptions.DateFromTimePtr(startedAt)
		sub.RenewAt = subscriptions.DateFromTimePtr(renewAt)
		sub.TrialEndsAt = subscriptions.DateFromTimePtr(trialEndsAt)
		sub.EndsAt = subscriptions.DateFromTimePtr(endsAt)
		if subCreated != nil {
			sub.CreatedAt = *subCreated
		}
		if subUpdated != nil {
			sub.UpdatedAt = *subUpdated
		}
		fact.PrimarySubscription = &sub
	}
	return fact, nil
}

type assetDecisionRecordSummaryScanner interface {
	Scan(dest ...any) error
}

func scanAssetDecisionRecordSummary(row assetDecisionRecordSummaryScanner) (assetdecisions.RecordSummary, error) {
	var (
		record      assetdecisions.RecordSummary
		status      string
		groupType   string
		view        string
		rawSnapshot []byte
	)
	if err := row.Scan(
		&record.RecordID,
		&record.Title,
		&record.Goal,
		&status,
		&record.SourceType,
		&record.SourceGroupID,
		&groupType,
		&view,
		&record.ScopeKey,
		&record.ScopeLabel,
		&record.RenewWithinDays,
		&record.MemberCount,
		&record.FollowupTodoCount,
		&record.FollowupInProgressCount,
		&record.FollowupBlockedCount,
		&record.FollowupDoneCount,
		&record.FollowupSkippedCount,
		&rawSnapshot,
		&record.CreatedAt,
		&record.UpdatedAt,
		&record.DecidedAt,
		&record.CompletedAt,
	); err != nil {
		return assetdecisions.RecordSummary{}, err
	}
	record.Status = assetdecisions.RecordStatus(status)
	record.SourceGroupType = assetdecisions.GroupType(groupType)
	record.SourceView = assetdecisions.View(view)
	snapshot, err := unmarshalAssetDecisionSnapshot(rawSnapshot)
	if err != nil {
		return assetdecisions.RecordSummary{}, err
	}
	record.EvidenceSnapshot = snapshot
	return record, nil
}

func scanAssetDecisionRecordMember(row assetDecisionRecordSummaryScanner) (assetdecisions.RecordMember, error) {
	var (
		member          assetdecisions.RecordMember
		suggestedRole   string
		decidedRole     string
		suggestedAction string
		decidedAction   string
		followupStatus  string
		rawSnapshot     []byte
	)
	if err := row.Scan(
		&member.RecordID,
		&member.VPSID,
		&member.DisplayName,
		&suggestedRole,
		&decidedRole,
		&suggestedAction,
		&decidedAction,
		&member.Reason,
		&followupStatus,
		&member.FollowupNote,
		&member.FollowupUpdatedAt,
		&rawSnapshot,
		&member.CreatedAt,
		&member.UpdatedAt,
	); err != nil {
		return assetdecisions.RecordMember{}, err
	}
	member.SuggestedRole = assetdecisions.SuggestedRole(suggestedRole)
	member.DecidedRole = assetdecisions.SuggestedRole(decidedRole)
	member.SuggestedAction = assetdecisions.SuggestedAction(suggestedAction)
	member.DecidedAction = assetdecisions.SuggestedAction(decidedAction)
	member.FollowupStatus = assetdecisions.FollowupStatus(followupStatus)
	snapshot, err := unmarshalAssetDecisionSnapshot(rawSnapshot)
	if err != nil {
		return assetdecisions.RecordMember{}, err
	}
	member.EvidenceSnapshot = snapshot
	return member, nil
}

func marshalAssetDecisionSnapshot(snapshot assetdecisions.EvidenceSnapshot) ([]byte, error) {
	if snapshot == nil {
		snapshot = assetdecisions.EvidenceSnapshot{}
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return nil, fmt.Errorf("marshal asset decision evidence snapshot: %w", err)
	}
	return raw, nil
}

func unmarshalAssetDecisionSnapshot(raw []byte) (assetdecisions.EvidenceSnapshot, error) {
	if len(raw) == 0 {
		return assetdecisions.EvidenceSnapshot{}, nil
	}
	var snapshot assetdecisions.EvidenceSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return nil, fmt.Errorf("decode asset decision evidence snapshot: %w", err)
	}
	if snapshot == nil {
		return assetdecisions.EvidenceSnapshot{}, nil
	}
	return snapshot, nil
}

func recordStatusTimestamps(status assetdecisions.RecordStatus, now time.Time) (*time.Time, *time.Time) {
	switch status {
	case assetdecisions.RecordStatusCompleted:
		return &now, &now
	case assetdecisions.RecordStatusDecided, assetdecisions.RecordStatusInProgress:
		return &now, nil
	default:
		return nil, nil
	}
}

func isAssetDecisionInvalidPostgresError(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23503" || pgErr.Code == "23505" || pgErr.Code == "23514"
}
