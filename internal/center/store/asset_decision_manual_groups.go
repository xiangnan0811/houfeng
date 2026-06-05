package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/assetdecisions"
	"houfeng/internal/center/ids"
)

const assetDecisionManualGroupColumns = `
	manual_group_id,
	status,
	scenario,
	title,
	goal,
	note,
	source_type,
	source_group_id,
	source_group_type,
	source_view,
	scope_key,
	scope_label,
	renew_within_days,
	created_at,
	updated_at,
	archived_at`

func (r *PostgresAssetDecisionRepository) ListManualGroups(ctx context.Context) ([]assetdecisions.ManualGroupSummary, error) {
	rows, err := r.db.Query(ctx, `
		select `+assetDecisionManualGroupColumns+`
		from asset_decision_manual_groups
		order by case when status = 'active' then 0 else 1 end, updated_at desc, manual_group_id desc`)
	if err != nil {
		return nil, fmt.Errorf("query asset decision manual groups: %w", err)
	}
	defer rows.Close()

	groupRows := []assetdecisions.ManualGroupRow{}
	groupIDs := []string{}
	for rows.Next() {
		row, err := scanAssetDecisionManualGroup(rows)
		if err != nil {
			return nil, fmt.Errorf("scan asset decision manual group: %w", err)
		}
		groupRows = append(groupRows, row)
		groupIDs = append(groupIDs, row.ManualGroupID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate asset decision manual groups: %w", err)
	}
	if len(groupRows) == 0 {
		return []assetdecisions.ManualGroupSummary{}, nil
	}
	memberRows, err := r.listManualGroupMembersForGroups(ctx, groupIDs)
	if err != nil {
		return nil, err
	}
	facts, err := r.loadFacts(ctx)
	if err != nil {
		return nil, err
	}
	summaries := make([]assetdecisions.ManualGroupSummary, 0, len(groupRows))
	for _, row := range groupRows {
		detail := assetdecisions.ManualGroupDetailFromRows(row, memberRows[row.ManualGroupID], facts)
		summaries = append(summaries, detail.ManualGroupSummary)
	}
	return summaries, nil
}

func (r *PostgresAssetDecisionRepository) CreateManualGroup(ctx context.Context, input assetdecisions.CreateManualGroupInput) (assetdecisions.ManualGroupDetail, error) {
	input = assetdecisions.NormalizeCreateManualGroupInput(input)
	if err := assetdecisions.ValidateCreateManualGroupInput(input); err != nil {
		return assetdecisions.ManualGroupDetail{}, err
	}
	facts, err := r.loadFacts(ctx)
	if err != nil {
		return assetdecisions.ManualGroupDetail{}, err
	}
	groupRow, memberRows, err := r.manualGroupRowsForCreate(input, facts)
	if err != nil {
		return assetdecisions.ManualGroupDetail{}, err
	}
	manualGroupID, err := ids.New("admg")
	if err != nil {
		return assetdecisions.ManualGroupDetail{}, fmt.Errorf("generate asset decision manual group id: %w", err)
	}
	now := time.Now().UTC()
	groupRow.ManualGroupID = manualGroupID
	groupRow.CreatedAt = now
	groupRow.UpdatedAt = now
	for index := range memberRows {
		memberRows[index].ManualGroupID = manualGroupID
		memberRows[index].CreatedAt = now
		memberRows[index].UpdatedAt = now
	}

	tx, err := r.beginAssetDecisionTx(ctx, "begin asset decision manual group transaction")
	if err != nil {
		return assetdecisions.ManualGroupDetail{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		insert into asset_decision_manual_groups (
			manual_group_id,
			status,
			scenario,
			title,
			goal,
			note,
			source_type,
			source_group_id,
			source_group_type,
			source_view,
			scope_key,
			scope_label,
			renew_within_days,
			created_at,
			updated_at,
			archived_at
		) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$14,$15)`,
		groupRow.ManualGroupID,
		string(groupRow.Status),
		string(groupRow.Scenario),
		groupRow.Title,
		groupRow.Goal,
		groupRow.Note,
		groupRow.SourceType,
		groupRow.SourceGroupID,
		string(groupRow.SourceGroupType),
		string(groupRow.SourceView),
		groupRow.ScopeKey,
		groupRow.ScopeLabel,
		groupRow.RenewWithinDays,
		now,
		groupRow.ArchivedAt,
	); err != nil {
		if isAssetDecisionInvalidPostgresError(err) {
			return assetdecisions.ManualGroupDetail{}, assetdecisions.ErrInvalidAssetDecisionInput
		}
		return assetdecisions.ManualGroupDetail{}, fmt.Errorf("insert asset decision manual group: %w", err)
	}
	for _, member := range memberRows {
		if err := insertManualGroupMember(ctx, tx, member); err != nil {
			return assetdecisions.ManualGroupDetail{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return assetdecisions.ManualGroupDetail{}, fmt.Errorf("commit asset decision manual group transaction: %w", err)
	}
	return assetdecisions.ManualGroupDetailFromRows(groupRow, memberRows, facts), nil
}

func (r *PostgresAssetDecisionRepository) GetManualGroup(ctx context.Context, manualGroupID string) (assetdecisions.ManualGroupDetail, error) {
	groupRow, memberRows, facts, err := r.loadManualGroupDetailParts(ctx, manualGroupID)
	if err != nil {
		return assetdecisions.ManualGroupDetail{}, err
	}
	return assetdecisions.ManualGroupDetailFromRows(groupRow, memberRows, facts), nil
}

func (r *PostgresAssetDecisionRepository) PatchManualGroup(ctx context.Context, manualGroupID string, input assetdecisions.PatchManualGroupInput) (assetdecisions.ManualGroupDetail, error) {
	manualGroupID = strings.TrimSpace(manualGroupID)
	input = assetdecisions.NormalizePatchManualGroupInput(input)
	if manualGroupID == "" {
		return assetdecisions.ManualGroupDetail{}, assetdecisions.ErrAssetDecisionManualGroupNotFound
	}
	if err := assetdecisions.ValidatePatchManualGroupInput(input); err != nil {
		return assetdecisions.ManualGroupDetail{}, err
	}
	now := time.Now().UTC()
	row := r.db.QueryRow(ctx, `
		update asset_decision_manual_groups
		set status = case when $2::boolean then $3 else status end,
		    scenario = case when $4::boolean then $5 else scenario end,
		    title = case when $6::boolean then $7 else title end,
		    goal = case when $8::boolean then $9 else goal end,
		    note = case when $10::boolean then $11 else note end,
		    archived_at = case
		      when $2::boolean and $3 = 'archived' and archived_at is null then $12
		      when $2::boolean and $3 = 'active' then null
		      else archived_at
		    end,
		    updated_at = $12
		where manual_group_id = $1
		returning manual_group_id`,
		manualGroupID,
		input.Status.Set,
		string(input.Status.Value),
		input.Scenario.Set,
		string(input.Scenario.Value),
		input.Title.Set,
		input.Title.Value,
		input.Goal.Set,
		input.Goal.Value,
		input.Note.Set,
		input.Note.Value,
		now,
	)
	var updatedID string
	if err := row.Scan(&updatedID); errors.Is(err, pgx.ErrNoRows) {
		return assetdecisions.ManualGroupDetail{}, assetdecisions.ErrAssetDecisionManualGroupNotFound
	} else if err != nil {
		if isAssetDecisionInvalidPostgresError(err) {
			return assetdecisions.ManualGroupDetail{}, assetdecisions.ErrInvalidAssetDecisionInput
		}
		return assetdecisions.ManualGroupDetail{}, fmt.Errorf("patch asset decision manual group %q: %w", manualGroupID, err)
	}
	return r.GetManualGroup(ctx, updatedID)
}

func (r *PostgresAssetDecisionRepository) AddManualGroupMember(ctx context.Context, manualGroupID string, input assetdecisions.CreateManualGroupMemberInput) (assetdecisions.ManualGroupDetail, error) {
	manualGroupID = strings.TrimSpace(manualGroupID)
	input = assetdecisions.NormalizeCreateManualGroupMemberInput(input)
	if manualGroupID == "" {
		return assetdecisions.ManualGroupDetail{}, assetdecisions.ErrAssetDecisionManualGroupNotFound
	}
	if err := assetdecisions.ValidateCreateManualGroupMemberInput(input); err != nil {
		return assetdecisions.ManualGroupDetail{}, err
	}
	groupRow, existingRows, facts, err := r.loadManualGroupDetailParts(ctx, manualGroupID)
	if err != nil {
		return assetdecisions.ManualGroupDetail{}, err
	}
	for _, existing := range existingRows {
		if existing.VPSID == input.VPSID {
			return assetdecisions.ManualGroupDetail{}, assetdecisions.ErrInvalidAssetDecisionInput
		}
	}
	fact, ok := assetdecisions.FactsByVPSID(facts)[input.VPSID]
	if !ok {
		return assetdecisions.ManualGroupDetail{}, assetdecisions.ErrInvalidAssetDecisionInput
	}
	current := assetdecisions.GroupMemberFromFact(fact, assetdecisions.ListFilters{RenewWithinDays: groupRow.RenewWithinDays})
	memberRow := manualMemberRowFromCurrent(groupRow.ManualGroupID, current, input, time.Now().UTC())
	tx, err := r.beginAssetDecisionTx(ctx, "begin asset decision manual group member transaction")
	if err != nil {
		return assetdecisions.ManualGroupDetail{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := insertManualGroupMember(ctx, tx, memberRow); err != nil {
		return assetdecisions.ManualGroupDetail{}, err
	}
	if _, err := tx.Exec(ctx, `update asset_decision_manual_groups set updated_at = $2 where manual_group_id = $1`, manualGroupID, memberRow.UpdatedAt); err != nil {
		return assetdecisions.ManualGroupDetail{}, fmt.Errorf("touch asset decision manual group %q: %w", manualGroupID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return assetdecisions.ManualGroupDetail{}, fmt.Errorf("commit asset decision manual group member transaction: %w", err)
	}
	return r.GetManualGroup(ctx, manualGroupID)
}

func (r *PostgresAssetDecisionRepository) PatchManualGroupMember(ctx context.Context, manualGroupID, vpsID string, input assetdecisions.PatchManualGroupMemberInput) (assetdecisions.ManualGroupDetail, error) {
	manualGroupID = strings.TrimSpace(manualGroupID)
	vpsID = strings.TrimSpace(vpsID)
	input = assetdecisions.NormalizePatchManualGroupMemberInput(input)
	if manualGroupID == "" {
		return assetdecisions.ManualGroupDetail{}, assetdecisions.ErrAssetDecisionManualGroupNotFound
	}
	if vpsID == "" {
		return assetdecisions.ManualGroupDetail{}, assetdecisions.ErrAssetDecisionManualGroupMemberNotFound
	}
	if err := assetdecisions.ValidatePatchManualGroupMemberInput(input); err != nil {
		return assetdecisions.ManualGroupDetail{}, err
	}
	now := time.Now().UTC()
	tx, err := r.beginAssetDecisionTx(ctx, "begin asset decision manual group member patch transaction")
	if err != nil {
		return assetdecisions.ManualGroupDetail{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `
		update asset_decision_manual_group_members
		set intended_role = case when $3::boolean then $4 else intended_role end,
		    intended_action = case when $5::boolean then $6 else intended_action end,
		    reason = case when $7::boolean then $8 else reason end,
		    note = case when $9::boolean then $10 else note end,
		    sort_order = case when $11::boolean then $12 else sort_order end,
		    updated_at = $13
		where manual_group_id = $1 and vps_id = $2`,
		manualGroupID,
		vpsID,
		input.IntendedRole.Set,
		string(input.IntendedRole.Value),
		input.IntendedAction.Set,
		string(input.IntendedAction.Value),
		input.Reason.Set,
		input.Reason.Value,
		input.Note.Set,
		input.Note.Value,
		input.SortOrder.Set,
		input.SortOrder.Value,
		now,
	)
	if err != nil {
		if isAssetDecisionInvalidPostgresError(err) {
			return assetdecisions.ManualGroupDetail{}, assetdecisions.ErrInvalidAssetDecisionInput
		}
		return assetdecisions.ManualGroupDetail{}, fmt.Errorf("patch asset decision manual group member %q/%q: %w", manualGroupID, vpsID, err)
	}
	if tag.RowsAffected() == 0 {
		return assetdecisions.ManualGroupDetail{}, assetdecisions.ErrAssetDecisionManualGroupMemberNotFound
	}
	if _, err := tx.Exec(ctx, `update asset_decision_manual_groups set updated_at = $2 where manual_group_id = $1`, manualGroupID, now); err != nil {
		return assetdecisions.ManualGroupDetail{}, fmt.Errorf("touch asset decision manual group %q: %w", manualGroupID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return assetdecisions.ManualGroupDetail{}, fmt.Errorf("commit asset decision manual group member patch transaction: %w", err)
	}
	return r.GetManualGroup(ctx, manualGroupID)
}

func (r *PostgresAssetDecisionRepository) DeleteManualGroupMember(ctx context.Context, manualGroupID, vpsID string) (assetdecisions.ManualGroupDetail, error) {
	manualGroupID = strings.TrimSpace(manualGroupID)
	vpsID = strings.TrimSpace(vpsID)
	if manualGroupID == "" {
		return assetdecisions.ManualGroupDetail{}, assetdecisions.ErrAssetDecisionManualGroupNotFound
	}
	if vpsID == "" {
		return assetdecisions.ManualGroupDetail{}, assetdecisions.ErrAssetDecisionManualGroupMemberNotFound
	}
	now := time.Now().UTC()
	tx, err := r.beginAssetDecisionTx(ctx, "begin asset decision manual group member delete transaction")
	if err != nil {
		return assetdecisions.ManualGroupDetail{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `
		delete from asset_decision_manual_group_members
		where manual_group_id = $1 and vps_id = $2`,
		manualGroupID,
		vpsID,
	)
	if err != nil {
		return assetdecisions.ManualGroupDetail{}, fmt.Errorf("delete asset decision manual group member %q/%q: %w", manualGroupID, vpsID, err)
	}
	if tag.RowsAffected() == 0 {
		return assetdecisions.ManualGroupDetail{}, assetdecisions.ErrAssetDecisionManualGroupMemberNotFound
	}
	if _, err := tx.Exec(ctx, `update asset_decision_manual_groups set updated_at = $2 where manual_group_id = $1`, manualGroupID, now); err != nil {
		return assetdecisions.ManualGroupDetail{}, fmt.Errorf("touch asset decision manual group %q: %w", manualGroupID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return assetdecisions.ManualGroupDetail{}, fmt.Errorf("commit asset decision manual group member delete transaction: %w", err)
	}
	return r.GetManualGroup(ctx, manualGroupID)
}

func (r *PostgresAssetDecisionRepository) loadManualGroupDetailParts(ctx context.Context, manualGroupID string) (assetdecisions.ManualGroupRow, []assetdecisions.ManualGroupMemberRow, []assetdecisions.Fact, error) {
	manualGroupID = strings.TrimSpace(manualGroupID)
	if manualGroupID == "" {
		return assetdecisions.ManualGroupRow{}, nil, nil, assetdecisions.ErrAssetDecisionManualGroupNotFound
	}
	groupRow, err := scanAssetDecisionManualGroup(r.db.QueryRow(ctx, `
		select `+assetDecisionManualGroupColumns+`
		from asset_decision_manual_groups
		where manual_group_id = $1`, manualGroupID))
	if errors.Is(err, pgx.ErrNoRows) {
		return assetdecisions.ManualGroupRow{}, nil, nil, assetdecisions.ErrAssetDecisionManualGroupNotFound
	}
	if err != nil {
		return assetdecisions.ManualGroupRow{}, nil, nil, fmt.Errorf("query asset decision manual group %q: %w", manualGroupID, err)
	}
	memberRows, err := r.listManualGroupMembers(ctx, manualGroupID)
	if err != nil {
		return assetdecisions.ManualGroupRow{}, nil, nil, err
	}
	facts, err := r.loadFacts(ctx)
	if err != nil {
		return assetdecisions.ManualGroupRow{}, nil, nil, err
	}
	return groupRow, memberRows, facts, nil
}

func (r *PostgresAssetDecisionRepository) listManualGroupMembers(ctx context.Context, manualGroupID string) ([]assetdecisions.ManualGroupMemberRow, error) {
	rows, err := r.db.Query(ctx, `
		select
			manual_group_id,
			vps_id,
			intended_role,
			intended_action,
			reason,
			note,
			sort_order,
			evidence_snapshot,
			created_at,
			updated_at
		from asset_decision_manual_group_members
		where manual_group_id = $1
		order by sort_order asc, vps_id asc`, manualGroupID)
	if err != nil {
		return nil, fmt.Errorf("query asset decision manual group members for %q: %w", manualGroupID, err)
	}
	defer rows.Close()
	memberRows := []assetdecisions.ManualGroupMemberRow{}
	for rows.Next() {
		member, err := scanAssetDecisionManualGroupMember(rows)
		if err != nil {
			return nil, fmt.Errorf("scan asset decision manual group member: %w", err)
		}
		memberRows = append(memberRows, member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate asset decision manual group members: %w", err)
	}
	return memberRows, nil
}

func (r *PostgresAssetDecisionRepository) listManualGroupMembersForGroups(ctx context.Context, groupIDs []string) (map[string][]assetdecisions.ManualGroupMemberRow, error) {
	membersByGroup := make(map[string][]assetdecisions.ManualGroupMemberRow, len(groupIDs))
	if len(groupIDs) == 0 {
		return membersByGroup, nil
	}
	rows, err := r.db.Query(ctx, `
		select
			manual_group_id,
			vps_id,
			intended_role,
			intended_action,
			reason,
			note,
			sort_order,
			evidence_snapshot,
			created_at,
			updated_at
		from asset_decision_manual_group_members
		where manual_group_id = any($1)
		order by manual_group_id asc, sort_order asc, vps_id asc`, groupIDs)
	if err != nil {
		return nil, fmt.Errorf("query asset decision manual group members: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		member, err := scanAssetDecisionManualGroupMember(rows)
		if err != nil {
			return nil, fmt.Errorf("scan asset decision manual group member: %w", err)
		}
		membersByGroup[member.ManualGroupID] = append(membersByGroup[member.ManualGroupID], member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate asset decision manual group members: %w", err)
	}
	return membersByGroup, nil
}

func (r *PostgresAssetDecisionRepository) manualGroupRowsForCreate(input assetdecisions.CreateManualGroupInput, facts []assetdecisions.Fact) (assetdecisions.ManualGroupRow, []assetdecisions.ManualGroupMemberRow, error) {
	row := assetdecisions.ManualGroupRow{
		Status:          input.Status,
		Scenario:        input.Scenario,
		Title:           input.Title,
		Goal:            input.Goal,
		Note:            input.Note,
		SourceType:      input.SourceType,
		SourceGroupID:   input.SourceGroupID,
		RenewWithinDays: input.RenewWithinDays,
	}
	switch input.SourceType {
	case assetdecisions.RecordSourceAutoGroup:
		group, err := assetdecisions.FindGroup(facts, input.SourceGroupID, assetdecisions.ListFilters{RenewWithinDays: input.RenewWithinDays})
		if err != nil {
			return assetdecisions.ManualGroupRow{}, nil, err
		}
		if row.Title == "" {
			row.Title = group.Title
		}
		row.SourceGroupType = group.GroupType
		row.SourceView = group.View
		row.ScopeKey = group.ScopeKey
		row.ScopeLabel = group.ScopeLabel
		overrides := assetdecisions.ManualGroupMemberInputsByVPS(input.Members)
		groupMemberIDs := map[string]struct{}{}
		members := make([]assetdecisions.ManualGroupMemberRow, 0, len(group.Members))
		for index, member := range group.Members {
			groupMemberIDs[member.VPS.VPSID] = struct{}{}
			memberInput := overrides[member.VPS.VPSID]
			memberInput.VPSID = member.VPS.VPSID
			if memberInput.IntendedRole == "" {
				memberInput.IntendedRole = member.SuggestedRole
			}
			if memberInput.IntendedAction == "" {
				memberInput.IntendedAction = member.SuggestedAction
			}
			if memberInput.SortOrder == 0 {
				memberInput.SortOrder = index + 1
			}
			members = append(members, manualMemberRowFromCurrent("", member, memberInput, time.Time{}))
		}
		for vpsID := range overrides {
			if _, ok := groupMemberIDs[vpsID]; !ok {
				return assetdecisions.ManualGroupRow{}, nil, assetdecisions.ErrInvalidAssetDecisionInput
			}
		}
		return row, members, nil
	default:
		factMap := assetdecisions.FactsByVPSID(facts)
		members := make([]assetdecisions.ManualGroupMemberRow, 0, len(input.Members))
		for index, memberInput := range input.Members {
			fact, ok := factMap[memberInput.VPSID]
			if !ok {
				return assetdecisions.ManualGroupRow{}, nil, assetdecisions.ErrInvalidAssetDecisionInput
			}
			current := assetdecisions.GroupMemberFromFact(fact, assetdecisions.ListFilters{RenewWithinDays: input.RenewWithinDays})
			if memberInput.SortOrder == 0 {
				memberInput.SortOrder = index + 1
			}
			members = append(members, manualMemberRowFromCurrent("", current, memberInput, time.Time{}))
		}
		return row, members, nil
	}
}

func manualMemberRowFromCurrent(manualGroupID string, current assetdecisions.GroupMember, input assetdecisions.CreateManualGroupMemberInput, now time.Time) assetdecisions.ManualGroupMemberRow {
	intendedRole := input.IntendedRole
	if intendedRole == "" {
		intendedRole = current.SuggestedRole
	}
	intendedAction := input.IntendedAction
	if intendedAction == "" {
		intendedAction = current.SuggestedAction
	}
	return assetdecisions.ManualGroupMemberRow{
		ManualGroupID:    manualGroupID,
		VPSID:            current.VPS.VPSID,
		IntendedRole:     intendedRole,
		IntendedAction:   intendedAction,
		Reason:           input.Reason,
		Note:             input.Note,
		SortOrder:        input.SortOrder,
		EvidenceSnapshot: assetdecisions.RecordSnapshotFromMember(current),
		CreatedAt:        now,
		UpdatedAt:        now,
	}
}

func insertManualGroupMember(ctx context.Context, tx assetDecisionTx, member assetdecisions.ManualGroupMemberRow) error {
	memberSnapshot, err := marshalAssetDecisionSnapshot(member.EvidenceSnapshot)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		insert into asset_decision_manual_group_members (
			manual_group_id,
			vps_id,
			intended_role,
			intended_action,
			reason,
			note,
			sort_order,
			evidence_snapshot,
			created_at,
			updated_at
		) values ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,$9)`,
		member.ManualGroupID,
		member.VPSID,
		string(member.IntendedRole),
		string(member.IntendedAction),
		member.Reason,
		member.Note,
		member.SortOrder,
		memberSnapshot,
		member.CreatedAt,
	); err != nil {
		if isAssetDecisionInvalidPostgresError(err) {
			return assetdecisions.ErrInvalidAssetDecisionInput
		}
		return fmt.Errorf("insert asset decision manual group member %q/%q: %w", member.ManualGroupID, member.VPSID, err)
	}
	return nil
}

func scanAssetDecisionManualGroup(row assetDecisionRecordSummaryScanner) (assetdecisions.ManualGroupRow, error) {
	var (
		record    assetdecisions.ManualGroupRow
		status    string
		scenario  string
		groupType string
		view      string
	)
	if err := row.Scan(
		&record.ManualGroupID,
		&status,
		&scenario,
		&record.Title,
		&record.Goal,
		&record.Note,
		&record.SourceType,
		&record.SourceGroupID,
		&groupType,
		&view,
		&record.ScopeKey,
		&record.ScopeLabel,
		&record.RenewWithinDays,
		&record.CreatedAt,
		&record.UpdatedAt,
		&record.ArchivedAt,
	); err != nil {
		return assetdecisions.ManualGroupRow{}, err
	}
	record.Status = assetdecisions.ManualGroupStatus(status)
	record.Scenario = assetdecisions.ManualGroupScenario(scenario)
	record.SourceGroupType = assetdecisions.GroupType(groupType)
	record.SourceView = assetdecisions.View(view)
	return record, nil
}

func scanAssetDecisionManualGroupMember(row assetDecisionRecordSummaryScanner) (assetdecisions.ManualGroupMemberRow, error) {
	var (
		member         assetdecisions.ManualGroupMemberRow
		intendedRole   string
		intendedAction string
		rawSnapshot    []byte
	)
	if err := row.Scan(
		&member.ManualGroupID,
		&member.VPSID,
		&intendedRole,
		&intendedAction,
		&member.Reason,
		&member.Note,
		&member.SortOrder,
		&rawSnapshot,
		&member.CreatedAt,
		&member.UpdatedAt,
	); err != nil {
		return assetdecisions.ManualGroupMemberRow{}, err
	}
	member.IntendedRole = assetdecisions.SuggestedRole(intendedRole)
	member.IntendedAction = assetdecisions.SuggestedAction(intendedAction)
	snapshot, err := unmarshalAssetDecisionSnapshot(rawSnapshot)
	if err != nil {
		return assetdecisions.ManualGroupMemberRow{}, err
	}
	member.EvidenceSnapshot = snapshot
	return member, nil
}

func (r *PostgresAssetDecisionRepository) beginAssetDecisionTx(ctx context.Context, action string) (assetDecisionTx, error) {
	beginTx := r.beginTx
	if beginTx == nil {
		beginner, ok := r.db.(assetDecisionTxBeginner)
		if !ok {
			return nil, fmt.Errorf("%s: transaction not supported", action)
		}
		beginTx = func(ctx context.Context, opts pgx.TxOptions) (assetDecisionTx, error) {
			return beginner.BeginTx(ctx, opts)
		}
	}
	tx, err := beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", action, err)
	}
	return tx, nil
}

type recordSource struct {
	sourceType       string
	sourceGroupID    string
	sourceGroupType  assetdecisions.GroupType
	sourceView       assetdecisions.View
	scopeKey         string
	scopeLabel       string
	title            string
	renewWithinDays  int
	evidenceSnapshot assetdecisions.EvidenceSnapshot
	members          []recordSourceMember
}

type recordSourceMember struct {
	assetdecisions.GroupMember
	decidedRole      assetdecisions.SuggestedRole
	decidedAction    assetdecisions.SuggestedAction
	reason           string
	evidenceSnapshot assetdecisions.EvidenceSnapshot
}

func (r *PostgresAssetDecisionRepository) recordSourceFromInput(ctx context.Context, input assetdecisions.CreateRecordInput, facts []assetdecisions.Fact) (recordSource, error) {
	switch input.SourceType {
	case assetdecisions.RecordSourceAutoGroup:
		group, err := assetdecisions.FindGroup(facts, input.SourceGroupID, assetdecisions.ListFilters{RenewWithinDays: input.RenewWithinDays})
		if err != nil {
			return recordSource{}, err
		}
		members, err := recordSourceMembersFromGroup(group, input.Members)
		if err != nil {
			return recordSource{}, err
		}
		return recordSource{
			sourceType:       assetdecisions.RecordSourceAutoGroup,
			sourceGroupID:    group.GroupID,
			sourceGroupType:  group.GroupType,
			sourceView:       group.View,
			scopeKey:         group.ScopeKey,
			scopeLabel:       group.ScopeLabel,
			title:            group.Title,
			renewWithinDays:  input.RenewWithinDays,
			evidenceSnapshot: assetdecisions.RecordSnapshotFromGroup(group),
			members:          members,
		}, nil
	case assetdecisions.RecordSourceManualGroup:
		manual, err := r.GetManualGroup(ctx, input.SourceGroupID)
		if err != nil {
			return recordSource{}, err
		}
		members, err := recordSourceMembersFromManualGroup(manual, input.Members)
		if err != nil {
			return recordSource{}, err
		}
		groupType := manual.SourceGroupType
		if groupType == "" {
			groupType = assetdecisions.GroupEvidenceGap
		}
		view := manual.SourceView
		if view == "" {
			view = assetdecisions.ViewNeedsDecision
		}
		scopeKey := manual.ScopeKey
		if scopeKey == "" {
			scopeKey = manual.ManualGroupID
		}
		scopeLabel := manual.ScopeLabel
		if scopeLabel == "" {
			scopeLabel = manual.Title
		}
		return recordSource{
			sourceType:       assetdecisions.RecordSourceManualGroup,
			sourceGroupID:    manual.ManualGroupID,
			sourceGroupType:  groupType,
			sourceView:       view,
			scopeKey:         scopeKey,
			scopeLabel:       scopeLabel,
			title:            manual.Title,
			renewWithinDays:  manual.RenewWithinDays,
			evidenceSnapshot: assetdecisions.RecordSnapshotFromManualGroup(manual),
			members:          members,
		}, nil
	default:
		return recordSource{}, assetdecisions.ErrInvalidAssetDecisionInput
	}
}

func recordSourceMembersFromGroup(group assetdecisions.GroupDetail, inputs []assetdecisions.CreateRecordMemberInput) ([]recordSourceMember, error) {
	memberInputs := assetdecisions.CreateMemberInputsByVPS(inputs)
	memberIDs := map[string]struct{}{}
	members := make([]recordSourceMember, 0, len(group.Members))
	for _, member := range group.Members {
		memberIDs[member.VPS.VPSID] = struct{}{}
		memberInput := memberInputs[member.VPS.VPSID]
		decidedRole := memberInput.DecidedRole
		if decidedRole == "" {
			decidedRole = member.SuggestedRole
		}
		decidedAction := memberInput.DecidedAction
		if decidedAction == "" {
			decidedAction = member.SuggestedAction
		}
		members = append(members, recordSourceMember{
			GroupMember:      member,
			decidedRole:      decidedRole,
			decidedAction:    decidedAction,
			reason:           memberInput.Reason,
			evidenceSnapshot: assetdecisions.RecordSnapshotFromMember(member),
		})
	}
	for vpsID := range memberInputs {
		if _, ok := memberIDs[vpsID]; !ok {
			return nil, assetdecisions.ErrInvalidAssetDecisionInput
		}
	}
	return members, nil
}

func recordSourceMembersFromManualGroup(group assetdecisions.ManualGroupDetail, inputs []assetdecisions.CreateRecordMemberInput) ([]recordSourceMember, error) {
	memberInputs := assetdecisions.CreateMemberInputsByVPS(inputs)
	memberIDs := map[string]struct{}{}
	members := make([]recordSourceMember, 0, len(group.Members))
	for _, member := range group.Members {
		if !member.CurrentFactFound {
			return nil, assetdecisions.ErrInvalidAssetDecisionInput
		}
		memberIDs[member.VPSID] = struct{}{}
		memberInput := memberInputs[member.VPSID]
		decidedRole := memberInput.DecidedRole
		if decidedRole == "" {
			decidedRole = member.IntendedRole
		}
		decidedAction := memberInput.DecidedAction
		if decidedAction == "" {
			decidedAction = member.IntendedAction
		}
		reason := memberInput.Reason
		if reason == "" {
			reason = member.Reason
		}
		members = append(members, recordSourceMember{
			GroupMember:      member.GroupMember,
			decidedRole:      decidedRole,
			decidedAction:    decidedAction,
			reason:           reason,
			evidenceSnapshot: assetdecisions.RecordSnapshotFromMember(member.GroupMember),
		})
	}
	for vpsID := range memberInputs {
		if _, ok := memberIDs[vpsID]; !ok {
			return nil, assetdecisions.ErrInvalidAssetDecisionInput
		}
	}
	return members, nil
}
