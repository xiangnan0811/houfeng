package assetdecisions

import (
	"fmt"
	"strings"

	"houfeng/internal/center/vpsassets"
)

func GroupMatchesFilters(group GroupDetail, filters ListFilters) bool {
	filters = NormalizeFilters(filters)
	if filters.Scenario != "" && !scenarioMatchesGroup(filters.Scenario, group.GroupType) {
		return false
	}
	if !hasContextFilters(filters) {
		return true
	}
	for _, member := range group.Members {
		if groupMemberMatchesContext(member, filters) {
			return true
		}
	}
	return false
}

func ManualGroupMatchesFilters(group ManualGroupDetail, filters ListFilters) bool {
	filters = NormalizeFilters(filters)
	if filters.Scenario != "" && group.Scenario != filters.Scenario {
		return false
	}
	if !hasContextFilters(filters) {
		return true
	}
	for _, member := range group.Members {
		if member.CurrentFactFound && groupMemberMatchesContext(member.GroupMember, filters) {
			return true
		}
		if filters.VPSID != "" && member.VPSID == filters.VPSID {
			return true
		}
	}
	return false
}

func RecordMatchesFilters(record RecordSummary, members []RecordMember, factsByVPS map[string]Fact, filters ListFilters) bool {
	filters = NormalizeFilters(filters)
	if filters.Scenario != "" && recordScenario(record) != filters.Scenario {
		return false
	}
	if !hasContextFilters(filters) {
		return true
	}
	for _, member := range members {
		if filters.VPSID != "" && member.VPSID == filters.VPSID {
			return true
		}
		fact, ok := factsByVPS[member.VPSID]
		if !ok {
			continue
		}
		if factMatchesContext(fact, filters) {
			return true
		}
	}
	return false
}

func FilterRecordSummaries(records []RecordSummary, membersByRecord map[string][]RecordMember, facts []Fact, filters ListFilters) []RecordSummary {
	filters = NormalizeFilters(filters)
	if filters.View != "" {
		records = filterRecordsByView(records, filters.View)
	}
	if filters.Scenario == "" && !hasContextFilters(filters) {
		return records
	}
	factMap := FactsByVPSID(facts)
	filtered := make([]RecordSummary, 0, len(records))
	for _, record := range records {
		if RecordMatchesFilters(record, membersByRecord[record.RecordID], factMap, filters) {
			filtered = append(filtered, record)
		}
	}
	return filtered
}

func filterGroupsByView(groups []GroupDetail, view View) []GroupDetail {
	filtered := make([]GroupDetail, 0, len(groups))
	for _, group := range groups {
		if group.View == view {
			filtered = append(filtered, group)
		}
	}
	return filtered
}

func filterRecordsByView(records []RecordSummary, view View) []RecordSummary {
	filtered := make([]RecordSummary, 0, len(records))
	for _, record := range records {
		if record.SourceView == view {
			filtered = append(filtered, record)
		}
	}
	return filtered
}

func hasContextFilters(filters ListFilters) bool {
	return filters.ProviderID != "" ||
		filters.VPSID != "" ||
		filters.Country != "" ||
		filters.Region != "" ||
		filters.City != ""
}

func groupMemberMatchesContext(member GroupMember, filters ListFilters) bool {
	if filters.VPSID != "" && member.VPS.VPSID != filters.VPSID {
		return false
	}
	if filters.ProviderID != "" && pointerOr(member.VPS.ProviderID, member.VPS.ProviderName) != filters.ProviderID {
		return false
	}
	if filters.Country != "" && !sameLoose(member.VPS.Country, filters.Country) {
		return false
	}
	if filters.Region != "" && !sameLoose(member.VPS.Region, filters.Region) {
		return false
	}
	if filters.City != "" && !sameLoose(member.VPS.City, filters.City) {
		return false
	}
	return true
}

func factMatchesContext(fact Fact, filters ListFilters) bool {
	member := GroupMember{VPS: fact.VPS}
	return groupMemberMatchesContext(member, filters)
}

func scenarioMatchesGroup(scenario ManualGroupScenario, groupType GroupType) bool {
	switch scenario {
	case "", ManualGroupScenarioGeneral:
		return true
	case ManualGroupScenarioPrimaryStandby:
		return groupType == GroupRegionPortfolio || groupType == GroupProviderPortfolio || groupType == GroupRenewalAttention
	case ManualGroupScenarioBudgetReduction:
		return groupType == GroupCostPressure || groupType == GroupProviderPortfolio || groupType == GroupRegionPortfolio
	case ManualGroupScenarioProviderReview:
		return groupType == GroupProviderPortfolio
	case ManualGroupScenarioRegionReview:
		return groupType == GroupRegionPortfolio
	case ManualGroupScenarioMigrationRetirement:
		return groupType == GroupCancellationAttention || groupType == GroupRenewalAttention || groupType == GroupCostPressure
	case ManualGroupScenarioEvidenceCleanup:
		return groupType == GroupEvidenceGap
	default:
		return false
	}
}

func recordScenario(record RecordSummary) ManualGroupScenario {
	if value, ok := record.EvidenceSnapshot["manual_group_scenario"]; ok {
		if scenario, ok := value.(string); ok {
			return ManualGroupScenario(strings.TrimSpace(scenario))
		}
		return ManualGroupScenario(strings.TrimSpace(fmt.Sprint(value)))
	}
	return scenarioFromGroupType(record.SourceGroupType)
}

func scenarioFromGroupType(groupType GroupType) ManualGroupScenario {
	switch groupType {
	case GroupProviderPortfolio:
		return ManualGroupScenarioProviderReview
	case GroupRegionPortfolio:
		return ManualGroupScenarioRegionReview
	case GroupCostPressure:
		return ManualGroupScenarioBudgetReduction
	case GroupEvidenceGap:
		return ManualGroupScenarioEvidenceCleanup
	case GroupCancellationAttention:
		return ManualGroupScenarioMigrationRetirement
	default:
		return ManualGroupScenarioGeneral
	}
}

func sameLoose(left, right string) bool {
	return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
}

func factProviderID(fact Fact) string {
	if fact.VPS.ProviderID != nil && strings.TrimSpace(*fact.VPS.ProviderID) != "" {
		return strings.TrimSpace(*fact.VPS.ProviderID)
	}
	return strings.TrimSpace(fact.VPS.ProviderName)
}

func providerIDForVPS(vps vpsassets.Record) string {
	if vps.ProviderID != nil && strings.TrimSpace(*vps.ProviderID) != "" {
		return strings.TrimSpace(*vps.ProviderID)
	}
	return strings.TrimSpace(vps.ProviderName)
}
