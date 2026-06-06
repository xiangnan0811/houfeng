package assetdecisions

import (
	"strings"
	"time"
)

type ScenarioTemplateStatus string

const (
	ScenarioTemplateStatusActive   ScenarioTemplateStatus = "active"
	ScenarioTemplateStatusArchived ScenarioTemplateStatus = "archived"
)

type ScenarioTemplateRow struct {
	TemplateID          string
	Builtin             bool
	Status              ScenarioTemplateStatus
	Scenario            ManualGroupScenario
	Title               string
	Goal                string
	Note                string
	SourceManualGroupID string
	CreatedAt           time.Time
	UpdatedAt           time.Time
	ArchivedAt          *time.Time
}

type ScenarioTemplateMemberRow struct {
	TemplateID     string
	MemberID       string
	VPSID          string
	IntendedRole   SuggestedRole
	IntendedAction SuggestedAction
	Reason         string
	Note           string
	SortOrder      int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type ScenarioTemplateSummary struct {
	TemplateID          string                 `json:"template_id"`
	Builtin             bool                   `json:"builtin"`
	Status              ScenarioTemplateStatus `json:"status"`
	Scenario            ManualGroupScenario    `json:"scenario"`
	Title               string                 `json:"title"`
	Goal                string                 `json:"goal"`
	Note                string                 `json:"note"`
	SourceManualGroupID string                 `json:"source_manual_group_id,omitempty"`
	MemberCount         int                    `json:"member_count"`
	CreatedAt           time.Time              `json:"created_at"`
	UpdatedAt           time.Time              `json:"updated_at"`
	ArchivedAt          *time.Time             `json:"archived_at,omitempty"`
}

type ScenarioTemplateDetail struct {
	ScenarioTemplateSummary
	Members []ScenarioTemplateMember `json:"members"`
}

type ScenarioTemplateMember struct {
	TemplateID     string          `json:"template_id,omitempty"`
	MemberID       string          `json:"member_id,omitempty"`
	VPSID          string          `json:"vps_id,omitempty"`
	IntendedRole   SuggestedRole   `json:"intended_role,omitempty"`
	IntendedAction SuggestedAction `json:"intended_action,omitempty"`
	Reason         string          `json:"reason"`
	Note           string          `json:"note"`
	SortOrder      int             `json:"sort_order"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type CreateScenarioTemplateInput struct {
	Status              ScenarioTemplateStatus        `json:"status"`
	Scenario            ManualGroupScenario           `json:"scenario"`
	Title               string                        `json:"title"`
	Goal                string                        `json:"goal"`
	Note                string                        `json:"note"`
	SourceManualGroupID string                        `json:"source_manual_group_id"`
	Members             []ScenarioTemplateMemberInput `json:"members"`
}

type ScenarioTemplateMemberInput struct {
	VPSID          string          `json:"vps_id"`
	IntendedRole   SuggestedRole   `json:"intended_role"`
	IntendedAction SuggestedAction `json:"intended_action"`
	Reason         string          `json:"reason"`
	Note           string          `json:"note"`
	SortOrder      int             `json:"sort_order"`
}

type PatchScenarioTemplateInput struct {
	Status   PatchScenarioTemplateStatus `json:"status"`
	Scenario PatchManualGroupScenario    `json:"scenario"`
	Title    PatchString                 `json:"title"`
	Goal     PatchString                 `json:"goal"`
	Note     PatchString                 `json:"note"`
}

type PatchScenarioTemplateStatus struct {
	Set   bool
	Value ScenarioTemplateStatus
}

type CreateManualGroupFromTemplateInput struct {
	RenewWithinDays int                            `json:"renew_within_days"`
	Status          ManualGroupStatus              `json:"status"`
	Scenario        ManualGroupScenario            `json:"scenario"`
	Title           string                         `json:"title"`
	Goal            string                         `json:"goal"`
	Note            string                         `json:"note"`
	Members         []CreateManualGroupMemberInput `json:"members"`
}

func BuiltinScenarioTemplates() []ScenarioTemplateDetail {
	return []ScenarioTemplateDetail{
		builtinScenarioTemplate(ManualGroupScenarioPrimaryStandby, "主力与容灾取舍", "比较主力、备用和容灾 VPS 的保留优先级", "适合跨同区或同服务商组合评估日用主力与备用机"),
		builtinScenarioTemplate(ManualGroupScenarioBudgetReduction, "预算压缩", "找出高成本、弱承载或闲置付费资产", "从成本压力切入，不直接取消，先形成组合判断"),
		builtinScenarioTemplate(ManualGroupScenarioProviderReview, "服务商组合复核", "比较同服务商多台 VPS 的成本、承载和异常信号", "用于服务商售卖质量波动或组合集中度复核"),
		builtinScenarioTemplate(ManualGroupScenarioRegionReview, "同区取舍", "比较同国家/地区/城市内多台 VPS 的保留角色", "用于同区多机、配置价格不一、承载关系不同的取舍"),
		builtinScenarioTemplate(ManualGroupScenarioMigrationRetirement, "迁移与退役收尾", "核对迁移、取消、过期状态割裂和仍在运行的关联对象", "只给出核对入口，不执行批量取消或迁移"),
		builtinScenarioTemplate(ManualGroupScenarioEvidenceCleanup, "资料补齐", "补齐订阅、监控、服务上下文和基础资料缺口", "先提高证据质量，再保存组合决策"),
		builtinScenarioTemplate(ManualGroupScenarioGeneral, "通用组合判断", "从任意 VPS 篮子开始记录组合目标和成员意图", "用于系统自动组之外的临时业务问题"),
	}
}

func builtinScenarioTemplate(scenario ManualGroupScenario, title, goal, note string) ScenarioTemplateDetail {
	now := time.Unix(0, 0).UTC()
	row := ScenarioTemplateRow{
		TemplateID: "adt_builtin_" + string(scenario),
		Builtin:    true,
		Status:     ScenarioTemplateStatusActive,
		Scenario:   scenario,
		Title:      title,
		Goal:       goal,
		Note:       note,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	return ScenarioTemplateDetailFromRows(row, nil)
}

func FindBuiltinScenarioTemplate(templateID string) (ScenarioTemplateDetail, bool) {
	templateID = strings.TrimSpace(templateID)
	for _, template := range BuiltinScenarioTemplates() {
		if template.TemplateID == templateID {
			return template, true
		}
	}
	return ScenarioTemplateDetail{}, false
}

func ScenarioTemplateDetailFromRows(row ScenarioTemplateRow, memberRows []ScenarioTemplateMemberRow) ScenarioTemplateDetail {
	members := make([]ScenarioTemplateMember, 0, len(memberRows))
	for _, memberRow := range memberRows {
		members = append(members, ScenarioTemplateMember{
			TemplateID:     memberRow.TemplateID,
			MemberID:       memberRow.MemberID,
			VPSID:          memberRow.VPSID,
			IntendedRole:   memberRow.IntendedRole,
			IntendedAction: memberRow.IntendedAction,
			Reason:         memberRow.Reason,
			Note:           memberRow.Note,
			SortOrder:      memberRow.SortOrder,
			CreatedAt:      memberRow.CreatedAt,
			UpdatedAt:      memberRow.UpdatedAt,
		})
	}
	return ScenarioTemplateDetail{
		ScenarioTemplateSummary: ScenarioTemplateSummary{
			TemplateID:          row.TemplateID,
			Builtin:             row.Builtin,
			Status:              row.Status,
			Scenario:            row.Scenario,
			Title:               row.Title,
			Goal:                row.Goal,
			Note:                row.Note,
			SourceManualGroupID: row.SourceManualGroupID,
			MemberCount:         len(members),
			CreatedAt:           row.CreatedAt,
			UpdatedAt:           row.UpdatedAt,
			ArchivedAt:          row.ArchivedAt,
		},
		Members: members,
	}
}

func NormalizeCreateScenarioTemplateInput(input CreateScenarioTemplateInput) CreateScenarioTemplateInput {
	input.Title = strings.TrimSpace(input.Title)
	input.Goal = strings.TrimSpace(input.Goal)
	input.Note = strings.TrimSpace(input.Note)
	input.SourceManualGroupID = strings.TrimSpace(input.SourceManualGroupID)
	input.Scenario = ManualGroupScenario(strings.TrimSpace(string(input.Scenario)))
	if input.Status == "" {
		input.Status = ScenarioTemplateStatusActive
	}
	if input.Scenario == "" {
		input.Scenario = ManualGroupScenarioGeneral
	}
	members := make([]ScenarioTemplateMemberInput, 0, len(input.Members))
	for _, member := range input.Members {
		members = append(members, normalizeScenarioTemplateMemberInput(member))
	}
	input.Members = members
	return input
}

func NormalizePatchScenarioTemplateInput(input PatchScenarioTemplateInput) PatchScenarioTemplateInput {
	input.Title.Value = strings.TrimSpace(input.Title.Value)
	input.Goal.Value = strings.TrimSpace(input.Goal.Value)
	input.Note.Value = strings.TrimSpace(input.Note.Value)
	return input
}

func NormalizeCreateManualGroupFromTemplateInput(input CreateManualGroupFromTemplateInput) CreateManualGroupFromTemplateInput {
	input.Title = strings.TrimSpace(input.Title)
	input.Goal = strings.TrimSpace(input.Goal)
	input.Note = strings.TrimSpace(input.Note)
	if input.RenewWithinDays == 0 {
		input.RenewWithinDays = 30
	}
	if input.Status == "" {
		input.Status = ManualGroupStatusActive
	}
	input.Scenario = ManualGroupScenario(strings.TrimSpace(string(input.Scenario)))
	members := make([]CreateManualGroupMemberInput, 0, len(input.Members))
	for _, member := range input.Members {
		members = append(members, NormalizeCreateManualGroupMemberInput(member))
	}
	input.Members = members
	return input
}

func ValidateCreateScenarioTemplateInput(input CreateScenarioTemplateInput) error {
	if input.SourceManualGroupID == "" && input.Title == "" {
		return ErrInvalidAssetDecisionInput
	}
	if err := ValidateScenarioTemplateStatus(input.Status); err != nil {
		return err
	}
	if err := ValidateManualGroupScenario(input.Scenario); err != nil {
		return err
	}
	return validateScenarioTemplateMemberInputs(input.Members)
}

func ValidatePatchScenarioTemplateInput(input PatchScenarioTemplateInput) error {
	if input.Status.Set {
		if err := ValidateScenarioTemplateStatus(input.Status.Value); err != nil {
			return err
		}
	}
	if input.Scenario.Set {
		if err := ValidateManualGroupScenario(input.Scenario.Value); err != nil {
			return err
		}
	}
	if input.Title.Set && input.Title.Value == "" {
		return ErrInvalidAssetDecisionInput
	}
	if !input.Status.Set && !input.Scenario.Set && !input.Title.Set && !input.Goal.Set && !input.Note.Set {
		return ErrInvalidAssetDecisionInput
	}
	return nil
}

func ValidateCreateManualGroupFromTemplateInput(input CreateManualGroupFromTemplateInput) error {
	if _, err := validateRenewWindow(input.RenewWithinDays); err != nil {
		return err
	}
	if err := ValidateManualGroupStatus(input.Status); err != nil {
		return err
	}
	if input.Scenario != "" {
		if err := ValidateManualGroupScenario(input.Scenario); err != nil {
			return err
		}
	}
	return validateCreateManualGroupMembers(input.Members)
}

func ValidateScenarioTemplateStatus(status ScenarioTemplateStatus) error {
	switch status {
	case ScenarioTemplateStatusActive, ScenarioTemplateStatusArchived:
		return nil
	default:
		return ErrInvalidAssetDecisionInput
	}
}

func ManualGroupInputFromTemplate(template ScenarioTemplateDetail, input CreateManualGroupFromTemplateInput) CreateManualGroupInput {
	input = NormalizeCreateManualGroupFromTemplateInput(input)
	scenario := input.Scenario
	if scenario == "" {
		scenario = template.Scenario
	}
	title := input.Title
	if title == "" {
		title = template.Title
	}
	goal := input.Goal
	if goal == "" {
		goal = template.Goal
	}
	note := input.Note
	if note == "" {
		note = template.Note
	}
	members := input.Members
	if len(members) == 0 {
		for _, member := range template.Members {
			if member.VPSID == "" {
				continue
			}
			members = append(members, CreateManualGroupMemberInput{
				VPSID:          member.VPSID,
				IntendedRole:   member.IntendedRole,
				IntendedAction: member.IntendedAction,
				Reason:         member.Reason,
				Note:           member.Note,
				SortOrder:      member.SortOrder,
			})
		}
	}
	return CreateManualGroupInput{
		SourceType:      ManualGroupSourceManual,
		RenewWithinDays: input.RenewWithinDays,
		Status:          input.Status,
		Scenario:        scenario,
		Title:           title,
		Goal:            goal,
		Note:            note,
		Members:         members,
	}
}

func normalizeScenarioTemplateMemberInput(input ScenarioTemplateMemberInput) ScenarioTemplateMemberInput {
	input.VPSID = strings.TrimSpace(input.VPSID)
	input.Reason = strings.TrimSpace(input.Reason)
	input.Note = strings.TrimSpace(input.Note)
	return input
}

func validateScenarioTemplateMemberInputs(members []ScenarioTemplateMemberInput) error {
	seen := map[string]struct{}{}
	for _, member := range members {
		if member.VPSID != "" {
			if _, ok := seen[member.VPSID]; ok {
				return ErrInvalidAssetDecisionInput
			}
			seen[member.VPSID] = struct{}{}
		}
		if member.IntendedRole != "" {
			if err := ValidateSuggestedRole(member.IntendedRole); err != nil {
				return err
			}
		}
		if member.IntendedAction != "" {
			if err := ValidateSuggestedAction(member.IntendedAction); err != nil {
				return err
			}
		}
	}
	return nil
}
