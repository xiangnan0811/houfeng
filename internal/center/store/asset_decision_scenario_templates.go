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

const assetDecisionScenarioTemplateColumns = `
	template_id,
	status,
	scenario,
	title,
	goal,
	note,
	source_manual_group_id,
	created_at,
	updated_at,
	archived_at`

func (r *PostgresAssetDecisionRepository) ListScenarioTemplates(ctx context.Context) ([]assetdecisions.ScenarioTemplateSummary, error) {
	rows, err := r.db.Query(ctx, `
		select `+assetDecisionScenarioTemplateColumns+`
		from asset_decision_scenario_templates
		order by case when status = 'active' then 0 else 1 end, updated_at desc, template_id desc`)
	if err != nil {
		return nil, fmt.Errorf("query asset decision scenario templates: %w", err)
	}
	defer rows.Close()

	templateRows := []assetdecisions.ScenarioTemplateRow{}
	templateIDs := []string{}
	for rows.Next() {
		row, err := scanAssetDecisionScenarioTemplate(rows)
		if err != nil {
			return nil, fmt.Errorf("scan asset decision scenario template: %w", err)
		}
		templateRows = append(templateRows, row)
		templateIDs = append(templateIDs, row.TemplateID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate asset decision scenario templates: %w", err)
	}
	membersByTemplate, err := r.listScenarioTemplateMembersForTemplates(ctx, templateIDs)
	if err != nil {
		return nil, err
	}

	summaries := []assetdecisions.ScenarioTemplateSummary{}
	for _, builtin := range assetdecisions.BuiltinScenarioTemplates() {
		summaries = append(summaries, builtin.ScenarioTemplateSummary)
	}
	for _, row := range templateRows {
		detail := assetdecisions.ScenarioTemplateDetailFromRows(row, membersByTemplate[row.TemplateID])
		summaries = append(summaries, detail.ScenarioTemplateSummary)
	}
	return summaries, nil
}

func (r *PostgresAssetDecisionRepository) CreateScenarioTemplate(ctx context.Context, input assetdecisions.CreateScenarioTemplateInput) (assetdecisions.ScenarioTemplateDetail, error) {
	input = assetdecisions.NormalizeCreateScenarioTemplateInput(input)
	if err := assetdecisions.ValidateCreateScenarioTemplateInput(input); err != nil {
		return assetdecisions.ScenarioTemplateDetail{}, err
	}

	if input.SourceManualGroupID != "" {
		group, err := r.GetManualGroup(ctx, input.SourceManualGroupID)
		if err != nil {
			return assetdecisions.ScenarioTemplateDetail{}, err
		}
		if input.Title == "" {
			input.Title = group.Title
		}
		if input.Goal == "" {
			input.Goal = group.Goal
		}
		if input.Note == "" {
			input.Note = group.Note
		}
		if input.Scenario == "" {
			input.Scenario = group.Scenario
		}
		if len(input.Members) == 0 {
			for _, member := range group.Members {
				input.Members = append(input.Members, assetdecisions.ScenarioTemplateMemberInput{
					VPSID:          member.VPSID,
					IntendedRole:   member.IntendedRole,
					IntendedAction: member.IntendedAction,
					Reason:         member.Reason,
					Note:           member.Note,
					SortOrder:      member.SortOrder,
				})
			}
		}
	}
	if input.Title == "" {
		return assetdecisions.ScenarioTemplateDetail{}, assetdecisions.ErrInvalidAssetDecisionInput
	}

	templateID, err := ids.New("adt")
	if err != nil {
		return assetdecisions.ScenarioTemplateDetail{}, fmt.Errorf("generate asset decision scenario template id: %w", err)
	}
	now := time.Now().UTC()
	memberRows := make([]assetdecisions.ScenarioTemplateMemberRow, 0, len(input.Members))
	for index, member := range input.Members {
		memberID, err := ids.New("adtm")
		if err != nil {
			return assetdecisions.ScenarioTemplateDetail{}, fmt.Errorf("generate asset decision scenario template member id: %w", err)
		}
		if member.SortOrder == 0 {
			member.SortOrder = index + 1
		}
		memberRows = append(memberRows, assetdecisions.ScenarioTemplateMemberRow{
			TemplateID:     templateID,
			MemberID:       memberID,
			VPSID:          member.VPSID,
			IntendedRole:   member.IntendedRole,
			IntendedAction: member.IntendedAction,
			Reason:         member.Reason,
			Note:           member.Note,
			SortOrder:      member.SortOrder,
			CreatedAt:      now,
			UpdatedAt:      now,
		})
	}

	row := assetdecisions.ScenarioTemplateRow{
		TemplateID:          templateID,
		Status:              input.Status,
		Scenario:            input.Scenario,
		Title:               input.Title,
		Goal:                input.Goal,
		Note:                input.Note,
		SourceManualGroupID: input.SourceManualGroupID,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	tx, err := r.beginAssetDecisionTx(ctx, "begin asset decision scenario template transaction")
	if err != nil {
		return assetdecisions.ScenarioTemplateDetail{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		insert into asset_decision_scenario_templates (
			template_id,
			status,
			scenario,
			title,
			goal,
			note,
			source_manual_group_id,
			created_at,
			updated_at,
			archived_at
		) values ($1,$2,$3,$4,$5,$6,$7,$8,$8,$9)`,
		row.TemplateID,
		string(row.Status),
		string(row.Scenario),
		row.Title,
		row.Goal,
		row.Note,
		row.SourceManualGroupID,
		row.CreatedAt,
		row.ArchivedAt,
	); err != nil {
		if isAssetDecisionInvalidPostgresError(err) {
			return assetdecisions.ScenarioTemplateDetail{}, assetdecisions.ErrInvalidAssetDecisionInput
		}
		return assetdecisions.ScenarioTemplateDetail{}, fmt.Errorf("insert asset decision scenario template: %w", err)
	}
	for _, member := range memberRows {
		if err := insertScenarioTemplateMember(ctx, tx, member); err != nil {
			return assetdecisions.ScenarioTemplateDetail{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return assetdecisions.ScenarioTemplateDetail{}, fmt.Errorf("commit asset decision scenario template transaction: %w", err)
	}
	return assetdecisions.ScenarioTemplateDetailFromRows(row, memberRows), nil
}

func (r *PostgresAssetDecisionRepository) GetScenarioTemplate(ctx context.Context, templateID string) (assetdecisions.ScenarioTemplateDetail, error) {
	templateID = strings.TrimSpace(templateID)
	if templateID == "" {
		return assetdecisions.ScenarioTemplateDetail{}, assetdecisions.ErrAssetDecisionScenarioTemplateNotFound
	}
	if builtin, ok := assetdecisions.FindBuiltinScenarioTemplate(templateID); ok {
		return builtin, nil
	}
	row, err := scanAssetDecisionScenarioTemplate(r.db.QueryRow(ctx, `
		select `+assetDecisionScenarioTemplateColumns+`
		from asset_decision_scenario_templates
		where template_id = $1`, templateID))
	if errors.Is(err, pgx.ErrNoRows) {
		return assetdecisions.ScenarioTemplateDetail{}, assetdecisions.ErrAssetDecisionScenarioTemplateNotFound
	}
	if err != nil {
		return assetdecisions.ScenarioTemplateDetail{}, fmt.Errorf("query asset decision scenario template %q: %w", templateID, err)
	}
	memberRows, err := r.listScenarioTemplateMembers(ctx, templateID)
	if err != nil {
		return assetdecisions.ScenarioTemplateDetail{}, err
	}
	return assetdecisions.ScenarioTemplateDetailFromRows(row, memberRows), nil
}

func (r *PostgresAssetDecisionRepository) PatchScenarioTemplate(ctx context.Context, templateID string, input assetdecisions.PatchScenarioTemplateInput) (assetdecisions.ScenarioTemplateDetail, error) {
	templateID = strings.TrimSpace(templateID)
	input = assetdecisions.NormalizePatchScenarioTemplateInput(input)
	if templateID == "" {
		return assetdecisions.ScenarioTemplateDetail{}, assetdecisions.ErrAssetDecisionScenarioTemplateNotFound
	}
	if _, ok := assetdecisions.FindBuiltinScenarioTemplate(templateID); ok {
		return assetdecisions.ScenarioTemplateDetail{}, assetdecisions.ErrInvalidAssetDecisionInput
	}
	if err := assetdecisions.ValidatePatchScenarioTemplateInput(input); err != nil {
		return assetdecisions.ScenarioTemplateDetail{}, err
	}
	now := time.Now().UTC()
	row := r.db.QueryRow(ctx, `
		update asset_decision_scenario_templates
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
		where template_id = $1
		returning template_id`,
		templateID,
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
		return assetdecisions.ScenarioTemplateDetail{}, assetdecisions.ErrAssetDecisionScenarioTemplateNotFound
	} else if err != nil {
		if isAssetDecisionInvalidPostgresError(err) {
			return assetdecisions.ScenarioTemplateDetail{}, assetdecisions.ErrInvalidAssetDecisionInput
		}
		return assetdecisions.ScenarioTemplateDetail{}, fmt.Errorf("patch asset decision scenario template %q: %w", templateID, err)
	}
	return r.GetScenarioTemplate(ctx, updatedID)
}

func (r *PostgresAssetDecisionRepository) CreateManualGroupFromTemplate(ctx context.Context, templateID string, input assetdecisions.CreateManualGroupFromTemplateInput) (assetdecisions.ManualGroupDetail, error) {
	template, err := r.GetScenarioTemplate(ctx, templateID)
	if err != nil {
		return assetdecisions.ManualGroupDetail{}, err
	}
	if template.Status == assetdecisions.ScenarioTemplateStatusArchived {
		return assetdecisions.ManualGroupDetail{}, assetdecisions.ErrInvalidAssetDecisionInput
	}
	input = assetdecisions.NormalizeCreateManualGroupFromTemplateInput(input)
	if err := assetdecisions.ValidateCreateManualGroupFromTemplateInput(input); err != nil {
		return assetdecisions.ManualGroupDetail{}, err
	}
	return r.CreateManualGroup(ctx, assetdecisions.ManualGroupInputFromTemplate(template, input))
}

func (r *PostgresAssetDecisionRepository) listScenarioTemplateMembers(ctx context.Context, templateID string) ([]assetdecisions.ScenarioTemplateMemberRow, error) {
	rows, err := r.db.Query(ctx, `
		select
			template_id,
			member_id,
			vps_id,
			intended_role,
			intended_action,
			reason,
			note,
			sort_order,
			created_at,
			updated_at
		from asset_decision_scenario_template_members
		where template_id = $1
		order by sort_order asc, member_id asc`, templateID)
	if err != nil {
		return nil, fmt.Errorf("query asset decision scenario template members for %q: %w", templateID, err)
	}
	defer rows.Close()

	memberRows := []assetdecisions.ScenarioTemplateMemberRow{}
	for rows.Next() {
		member, err := scanAssetDecisionScenarioTemplateMember(rows)
		if err != nil {
			return nil, fmt.Errorf("scan asset decision scenario template member: %w", err)
		}
		memberRows = append(memberRows, member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate asset decision scenario template members: %w", err)
	}
	return memberRows, nil
}

func (r *PostgresAssetDecisionRepository) listScenarioTemplateMembersForTemplates(ctx context.Context, templateIDs []string) (map[string][]assetdecisions.ScenarioTemplateMemberRow, error) {
	membersByTemplate := make(map[string][]assetdecisions.ScenarioTemplateMemberRow, len(templateIDs))
	if len(templateIDs) == 0 {
		return membersByTemplate, nil
	}
	rows, err := r.db.Query(ctx, `
		select
			template_id,
			member_id,
			vps_id,
			intended_role,
			intended_action,
			reason,
			note,
			sort_order,
			created_at,
			updated_at
		from asset_decision_scenario_template_members
		where template_id = any($1)
		order by template_id asc, sort_order asc, member_id asc`, templateIDs)
	if err != nil {
		return nil, fmt.Errorf("query asset decision scenario template members: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		member, err := scanAssetDecisionScenarioTemplateMember(rows)
		if err != nil {
			return nil, fmt.Errorf("scan asset decision scenario template member: %w", err)
		}
		membersByTemplate[member.TemplateID] = append(membersByTemplate[member.TemplateID], member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate asset decision scenario template members: %w", err)
	}
	return membersByTemplate, nil
}

func insertScenarioTemplateMember(ctx context.Context, tx assetDecisionTx, member assetdecisions.ScenarioTemplateMemberRow) error {
	if _, err := tx.Exec(ctx, `
		insert into asset_decision_scenario_template_members (
			member_id,
			template_id,
			vps_id,
			intended_role,
			intended_action,
			reason,
			note,
			sort_order,
			created_at,
			updated_at
		) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$9)`,
		member.MemberID,
		member.TemplateID,
		member.VPSID,
		string(member.IntendedRole),
		string(member.IntendedAction),
		member.Reason,
		member.Note,
		member.SortOrder,
		member.CreatedAt,
	); err != nil {
		if isAssetDecisionInvalidPostgresError(err) {
			return assetdecisions.ErrInvalidAssetDecisionInput
		}
		return fmt.Errorf("insert asset decision scenario template member %q/%q: %w", member.TemplateID, member.MemberID, err)
	}
	return nil
}

func scanAssetDecisionScenarioTemplate(row assetDecisionRecordSummaryScanner) (assetdecisions.ScenarioTemplateRow, error) {
	var (
		record   assetdecisions.ScenarioTemplateRow
		status   string
		scenario string
	)
	if err := row.Scan(
		&record.TemplateID,
		&status,
		&scenario,
		&record.Title,
		&record.Goal,
		&record.Note,
		&record.SourceManualGroupID,
		&record.CreatedAt,
		&record.UpdatedAt,
		&record.ArchivedAt,
	); err != nil {
		return assetdecisions.ScenarioTemplateRow{}, err
	}
	record.Status = assetdecisions.ScenarioTemplateStatus(status)
	record.Scenario = assetdecisions.ManualGroupScenario(scenario)
	return record, nil
}

func scanAssetDecisionScenarioTemplateMember(row assetDecisionRecordSummaryScanner) (assetdecisions.ScenarioTemplateMemberRow, error) {
	var (
		member         assetdecisions.ScenarioTemplateMemberRow
		intendedRole   string
		intendedAction string
	)
	if err := row.Scan(
		&member.TemplateID,
		&member.MemberID,
		&member.VPSID,
		&intendedRole,
		&intendedAction,
		&member.Reason,
		&member.Note,
		&member.SortOrder,
		&member.CreatedAt,
		&member.UpdatedAt,
	); err != nil {
		return assetdecisions.ScenarioTemplateMemberRow{}, err
	}
	member.IntendedRole = assetdecisions.SuggestedRole(intendedRole)
	member.IntendedAction = assetdecisions.SuggestedAction(intendedAction)
	return member, nil
}
