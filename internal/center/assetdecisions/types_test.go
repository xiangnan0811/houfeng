package assetdecisions

import (
	"testing"
	"time"

	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/vpsassets"
)

func TestDeriveGroupsBuildsPortfolioDecisionGroups(t *testing.T) {
	providerID := "pv_hetzner"
	facts := []Fact{
		fact("vps_de_1", "DE Primary", providerID, "Hetzner", "Germany", "Hesse", "Falkenstein", vpsassets.UsageInUse, sub("sub_1", 12, 7)),
		fact("vps_de_2", "DE Standby", providerID, "Hetzner", "Germany", "Hesse", "Falkenstein", vpsassets.UsageStandby, sub("sub_2", 8, 21)),
		fact("vps_us_1", "US Idle", "pv_bandwagon", "Bandwagon", "United States", "CA", "Los Angeles", vpsassets.UsageIdle, sub("sub_3", 30, 14)),
	}
	facts[0].ServiceCount = 2
	facts[0].DomainCount = 1
	facts[0].TargetCount = 1
	facts[0].RunningTargetCount = 1
	facts[2].PrimarySubscription.BudgetStatus = "over"

	groups, err := DeriveGroups(facts, ListFilters{RenewWithinDays: 30})
	if err != nil {
		t.Fatalf("DeriveGroups() error = %v", err)
	}

	if !hasGroupType(groups, GroupRenewalAttention) {
		t.Fatalf("groups = %#v, want renewal attention group", groups)
	}
	if !hasGroupType(groups, GroupRegionPortfolio) {
		t.Fatalf("groups = %#v, want region portfolio group", groups)
	}
	if !hasGroupType(groups, GroupProviderPortfolio) {
		t.Fatalf("groups = %#v, want provider portfolio group", groups)
	}
	if !hasGroupType(groups, GroupCostPressure) {
		t.Fatalf("groups = %#v, want cost pressure group", groups)
	}

	region := firstGroup(groups, GroupRegionPortfolio)
	if region.MemberCount != 2 || region.ScopeLabel != "Germany / Hesse / Falkenstein" {
		t.Fatalf("region group = %#v, want two same-region VPS", region.GroupSummary)
	}
	if region.ServiceCount != 2 || region.DomainCount != 1 || region.TargetCount != 1 || region.RunningTargetCount != 1 {
		t.Fatalf("region context = %#v, want service/domain/target rollup", region.GroupSummary)
	}
	if region.GroupID != StableGroupID(GroupRegionPortfolio, region.ScopeKey) {
		t.Fatalf("group id = %q, want stable derived id", region.GroupID)
	}
}

func TestEvidenceAssessmentRatesCompleteEvidenceAsDecisionReady(t *testing.T) {
	facts := []Fact{
		fact("vps_de_1", "DE Primary", "pv_hetzner", "Hetzner", "Germany", "Hesse", "Falkenstein", vpsassets.UsageInUse, sub("sub_1", 12, 120)),
		fact("vps_de_2", "DE Peer", "pv_hetzner", "Hetzner", "Germany", "Hesse", "Falkenstein", vpsassets.UsageInUse, sub("sub_2", 10, 120)),
	}
	for i := range facts {
		facts[i].ServiceCount = 2
		facts[i].DomainCount = 1
		facts[i].TargetCount = 1
		facts[i].RunningTargetCount = 1
		facts[i].MonitoringLinkCount = 1
		facts[i].RunningMonitoringCount = 1
	}

	groups, err := DeriveGroups(facts, ListFilters{RenewWithinDays: 30, View: ViewRegion})
	if err != nil {
		t.Fatalf("DeriveGroups() error = %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("groups = %#v, want one region group", groups)
	}

	assessment := groups[0].EvidenceAssessment
	if assessment.QualityTier != EvidenceTierStrong {
		t.Fatalf("group assessment = %#v, want strong evidence", assessment)
	}
	if assessment.DecisionBias != EvidenceBiasKeep {
		t.Fatalf("group assessment = %#v, want keep bias", assessment)
	}
	if assessment.ConfidenceScore < 80 || assessment.ReadinessScore < 70 {
		t.Fatalf("group assessment = %#v, want high confidence and readiness", assessment)
	}
	memberAssessment := groups[0].Members[0].EvidenceAssessment
	if memberAssessment.SupportSignalCount < 4 || memberAssessment.GapSignalCount != 0 {
		t.Fatalf("member assessment = %#v, want support signals without gaps", memberAssessment)
	}
}

func TestComparisonInsightRanksAndExplainsPortfolioMembers(t *testing.T) {
	facts := []Fact{
		fact("vps_primary", "DE Primary", "pv_hetzner", "Hetzner", "Germany", "Hesse", "Falkenstein", vpsassets.UsageInUse, sub("sub_primary", 12, 120)),
		fact("vps_standby", "DE Standby", "pv_hetzner", "Hetzner", "Germany", "Hesse", "Falkenstein", vpsassets.UsageStandby, sub("sub_standby", 8, 120)),
		fact("vps_idle", "DE Idle", "pv_hetzner", "Hetzner", "Germany", "Hesse", "Falkenstein", vpsassets.UsageIdle, sub("sub_idle", 30, 120)),
	}
	facts[0].ServiceCount = 2
	facts[0].DomainCount = 1
	facts[0].TargetCount = 1
	facts[0].RunningTargetCount = 1
	for index := range facts {
		facts[index].MonitoringLinkCount = 1
		facts[index].RunningMonitoringCount = 1
	}

	groups, err := DeriveGroups(facts, ListFilters{RenewWithinDays: 30, View: ViewRegion})
	if err != nil {
		t.Fatalf("DeriveGroups() error = %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("groups = %#v, want one region portfolio", groups)
	}
	group := groups[0]
	if group.ComparisonInsight.PrimaryAxis != ComparisonAxisServiceContext {
		t.Fatalf("comparison = %#v, want service context axis", group.ComparisonInsight)
	}
	for _, lane := range []ComparisonLane{ComparisonLanePrimary, ComparisonLaneStandby, ComparisonLaneRetire} {
		if !hasComparisonLane(group.ComparisonInsight.LaneCounts, lane) {
			t.Fatalf("lane counts = %#v, want lane %s", group.ComparisonInsight.LaneCounts, lane)
		}
	}
	if len(group.ComparisonInsight.PriorityVPSIDs) == 0 || group.ComparisonInsight.PriorityVPSIDs[0] != "vps_idle" {
		t.Fatalf("priority ids = %#v, want idle paid candidate first", group.ComparisonInsight.PriorityVPSIDs)
	}

	primary := comparisonMemberByID(group.Members, "vps_primary")
	if primary.ComparisonInsight.Rank == 0 || primary.ComparisonInsight.Lane != ComparisonLanePrimary {
		t.Fatalf("primary comparison = %#v, want ranked primary lane", primary.ComparisonInsight)
	}
	if !hasComparisonSignal(primary.ComparisonInsight.Strengths, "service_context") || !hasComparisonSignal(primary.ComparisonInsight.Strengths, "monitoring_context") {
		t.Fatalf("primary strengths = %#v, want service and monitoring strengths", primary.ComparisonInsight.Strengths)
	}

	idle := comparisonMemberByID(group.Members, "vps_idle")
	if idle.ComparisonInsight.Lane != ComparisonLaneRetire || !hasComparisonSignal(idle.ComparisonInsight.Risks, string(EvidenceIdlePaid)) {
		t.Fatalf("idle comparison = %#v, risks=%#v, want retire lane with idle paid risk", idle.ComparisonInsight, idle.ComparisonInsight.Risks)
	}
	assertNoFutureComparisonSignals(t, group)
}

func TestDeriveGroupsCancellationAndArchivedBoundaries(t *testing.T) {
	facts := []Fact{
		fact("vps_cancelled_running", "Cancelled Runtime", "pv_1", "Provider", "Japan", "", "Tokyo", vpsassets.UsageInUse, sub("sub_running", 10, 30)),
		fact("vps_archived", "Archived", "pv_1", "Provider", "Japan", "", "Tokyo", vpsassets.UsageIdle, sub("sub_archived", 10, 30)),
	}
	facts[0].VPS.LifecycleStatus = vpsassets.LifecycleCancelled
	facts[0].RunningMonitoringCount = 1
	facts[0].RunningTargetCount = 1
	facts[1].VPS.LifecycleStatus = vpsassets.LifecycleArchived

	groups, err := DeriveGroups(facts, ListFilters{RenewWithinDays: 30})
	if err != nil {
		t.Fatalf("DeriveGroups() error = %v", err)
	}

	cancellation := firstGroup(groups, GroupCancellationAttention)
	if cancellation.MemberCount != 1 || cancellation.Members[0].VPS.VPSID != "vps_cancelled_running" {
		t.Fatalf("cancellation group = %#v, want cancelled runtime member only", cancellation)
	}
	if hasGroupType(groups, GroupRegionPortfolio) {
		t.Fatalf("groups = %#v, want archived/cancelled excluded from ordinary portfolio groups", groups)
	}
	if cancellation.EvidenceAssessment.PressureScore < 70 || cancellation.EvidenceAssessment.DecisionBias != EvidenceBiasRetire {
		t.Fatalf("cancellation assessment = %#v, want high pressure retire bias", cancellation.EvidenceAssessment)
	}
}

func TestDeriveGroupsEvidenceGapDoesNotMisreportUnavailableSubscriptions(t *testing.T) {
	f := fact("vps_001", "Evidence Gap", "", "", "", "", "", vpsassets.UsageInUse, nil)
	f.SourceAvailability.Subscriptions = false
	f.MonitoringLinkCount = 0

	groups, err := DeriveGroups([]Fact{f}, ListFilters{RenewWithinDays: 30, View: ViewEvidence})
	if err != nil {
		t.Fatalf("DeriveGroups() error = %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("groups = %#v, want evidence group", groups)
	}
	member := groups[0].Members[0]
	if !hasEvidence(member, EvidenceSubscriptionUnavailable) {
		t.Fatalf("member chips = %#v, want subscription unavailable evidence", member.EvidenceChips)
	}
	if hasEvidence(member, EvidenceMissingSubscription) {
		t.Fatalf("member chips = %#v, do not want missing subscription when source unavailable", member.EvidenceChips)
	}
	if member.SuggestedAction != ActionCompleteEvidence || member.SuggestedRole != RoleEvidenceNeeded {
		t.Fatalf("member suggestion = (%q,%q), want evidence completion", member.SuggestedRole, member.SuggestedAction)
	}
	if member.EvidenceAssessment.DecisionBias != EvidenceBiasCompleteEvidence {
		t.Fatalf("member assessment = %#v, want complete evidence bias", member.EvidenceAssessment)
	}
	if member.EvidenceAssessment.ConfidenceScore >= 60 || member.EvidenceAssessment.GapSignalCount == 0 {
		t.Fatalf("member assessment = %#v, want low confidence with evidence gaps", member.EvidenceAssessment)
	}
	if groups[0].EvidenceAssessment.DecisionBias != EvidenceBiasCompleteEvidence {
		t.Fatalf("group assessment = %#v, want complete evidence bias", groups[0].EvidenceAssessment)
	}
}

func TestFindGroupRecomputesStableGroups(t *testing.T) {
	facts := []Fact{
		fact("vps_de_1", "DE 1", "pv_1", "Provider", "Germany", "", "Frankfurt", vpsassets.UsageInUse, sub("sub_1", 12, 30)),
		fact("vps_de_2", "DE 2", "pv_2", "Other", "Germany", "", "Frankfurt", vpsassets.UsageStandby, sub("sub_2", 8, 30)),
	}
	groupID := StableGroupID(GroupRegionPortfolio, "germany / frankfurt")

	group, err := FindGroup(facts, groupID, ListFilters{RenewWithinDays: 30})
	if err != nil {
		t.Fatalf("FindGroup() error = %v", err)
	}
	if group.GroupID != groupID || group.MemberCount != 2 {
		t.Fatalf("group = %#v, want recomputed same-region group", group.GroupSummary)
	}

	if _, err := FindGroup(facts, "adg_auto_missing", ListFilters{RenewWithinDays: 30}); err != ErrAssetDecisionGroupNotFound {
		t.Fatalf("FindGroup() error = %v, want not found sentinel", err)
	}
}

func TestRecordSnapshotsIncludeEvidenceAssessmentAndComparisonInsight(t *testing.T) {
	facts := []Fact{
		fact("vps_de_1", "DE Primary", "pv_1", "Provider", "Germany", "", "Frankfurt", vpsassets.UsageInUse, sub("sub_1", 12, 30)),
		fact("vps_de_2", "DE Standby", "pv_1", "Provider", "Germany", "", "Frankfurt", vpsassets.UsageStandby, nil),
	}
	facts[0].ServiceCount = 1
	facts[0].MonitoringLinkCount = 1
	facts[0].RunningMonitoringCount = 1

	group, err := FindGroup(facts, StableGroupID(GroupRegionPortfolio, "germany / frankfurt"), ListFilters{RenewWithinDays: 30})
	if err != nil {
		t.Fatalf("FindGroup() error = %v", err)
	}

	groupSnapshot := RecordSnapshotFromGroup(group)
	groupAssessment, ok := groupSnapshot["evidence_assessment"].(EvidenceAssessment)
	if !ok {
		t.Fatalf("group snapshot = %#v, want evidence assessment", groupSnapshot)
	}
	if groupAssessment.GapSignalCount == 0 {
		t.Fatalf("group assessment = %#v, want member evidence gaps rolled up", groupAssessment)
	}
	groupComparison, ok := groupSnapshot["comparison_insight"].(ComparisonInsight)
	if !ok {
		t.Fatalf("group snapshot = %#v, want comparison insight", groupSnapshot)
	}
	if groupComparison.PrimaryAxis == "" || len(groupComparison.LaneCounts) == 0 {
		t.Fatalf("group comparison snapshot = %#v, want populated comparison insight", groupComparison)
	}

	memberSnapshot := RecordSnapshotFromMember(group.Members[0])
	memberAssessment, ok := memberSnapshot["evidence_assessment"].(EvidenceAssessment)
	if !ok {
		t.Fatalf("member snapshot = %#v, want evidence assessment", memberSnapshot)
	}
	if memberAssessment.ConfidenceScore == 0 {
		t.Fatalf("member assessment = %#v, want scored member snapshot", memberAssessment)
	}
	memberComparison, ok := memberSnapshot["comparison_insight"].(MemberComparisonInsight)
	if !ok {
		t.Fatalf("member snapshot = %#v, want comparison insight", memberSnapshot)
	}
	if memberComparison.Lane == "" || memberComparison.Rank == 0 {
		t.Fatalf("member comparison snapshot = %#v, want populated lane and rank", memberComparison)
	}
}

func TestManualGroupComparisonIncludesCurrentFactMissing(t *testing.T) {
	now := time.Date(2026, time.June, 7, 9, 0, 0, 0, time.UTC)
	row := ManualGroupRow{
		ManualGroupID:   "admg_001",
		Status:          ManualGroupStatusActive,
		Scenario:        ManualGroupScenarioPrimaryStandby,
		Title:           "德国主备复核",
		Goal:            "保留主力，核对备用",
		SourceType:      ManualGroupSourceManual,
		RenewWithinDays: 30,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	memberRows := []ManualGroupMemberRow{
		{
			ManualGroupID:  "admg_001",
			VPSID:          "vps_primary",
			IntendedRole:   RolePrimaryCandidate,
			IntendedAction: ActionKeep,
			SortOrder:      10,
			CreatedAt:      now,
			UpdatedAt:      now,
		},
		{
			ManualGroupID:  "admg_001",
			VPSID:          "vps_missing",
			IntendedRole:   RoleStandbyCandidate,
			IntendedAction: ActionObserve,
			SortOrder:      20,
			CreatedAt:      now,
			UpdatedAt:      now,
		},
	}
	f := fact("vps_primary", "DE Primary", "pv_1", "Provider", "Germany", "", "Frankfurt", vpsassets.UsageInUse, sub("sub_1", 12, 120))
	f.ServiceCount = 1
	f.MonitoringLinkCount = 1
	f.RunningMonitoringCount = 1

	detail := ManualGroupDetailFromRows(row, memberRows, []Fact{f})
	if detail.MemberCount != 2 || !hasComparisonLane(detail.ComparisonInsight.LaneCounts, ComparisonLaneEvidence) {
		t.Fatalf("manual comparison = %#v, want evidence lane for missing fact member", detail.ComparisonInsight)
	}
	if detail.SourceAvailability.Subscriptions || detail.SourceAvailability.Monitoring || detail.SourceAvailability.Targets {
		t.Fatalf("source availability = %#v, want missing current fact to keep summary unavailable", detail.SourceAvailability)
	}
	missing := manualComparisonMemberByID(detail.Members, "vps_missing")
	if missing.CurrentFactFound || missing.ComparisonInsight.Lane != ComparisonLaneEvidence || !hasComparisonSignal(missing.ComparisonInsight.Gaps, string(EvidenceCurrentFactMissing)) {
		t.Fatalf("missing member = %#v, want current fact missing evidence comparison", missing)
	}
	snapshot := RecordSnapshotFromManualGroup(detail)
	comparison, ok := snapshot["comparison_insight"].(ComparisonInsight)
	if !ok || !hasComparisonLane(comparison.LaneCounts, ComparisonLaneEvidence) {
		t.Fatalf("manual snapshot = %#v, want manual comparison insight with evidence lane", snapshot)
	}
}

func TestValidatePatchRecordInputMemberFollowup(t *testing.T) {
	tests := []struct {
		name    string
		input   PatchRecordInput
		wantErr bool
	}{
		{
			name: "valid member followup",
			input: PatchRecordInput{Members: []PatchRecordMemberInput{{
				VPSID:          " vps_001 ",
				FollowupStatus: PatchFollowupStatus{Set: true, Value: FollowupBlocked},
				FollowupNote:   PatchString{Set: true, Value: " 等待迁移窗口 "},
			}}},
		},
		{
			name: "note can be cleared",
			input: PatchRecordInput{Members: []PatchRecordMemberInput{{
				VPSID:        "vps_001",
				FollowupNote: PatchString{Set: true, Value: ""},
			}}},
		},
		{
			name: "missing vps id",
			input: PatchRecordInput{Members: []PatchRecordMemberInput{{
				FollowupStatus: PatchFollowupStatus{Set: true, Value: FollowupDone},
			}}},
			wantErr: true,
		},
		{
			name: "member patch without fields",
			input: PatchRecordInput{Members: []PatchRecordMemberInput{{
				VPSID: "vps_001",
			}}},
			wantErr: true,
		},
		{
			name: "duplicate member",
			input: PatchRecordInput{Members: []PatchRecordMemberInput{
				{VPSID: "vps_001", FollowupStatus: PatchFollowupStatus{Set: true, Value: FollowupDone}},
				{VPSID: " vps_001 ", FollowupNote: PatchString{Set: true, Value: "duplicate"}},
			}},
			wantErr: true,
		},
		{
			name: "invalid followup status",
			input: PatchRecordInput{Members: []PatchRecordMemberInput{{
				VPSID:          "vps_001",
				FollowupStatus: PatchFollowupStatus{Set: true, Value: FollowupStatus("bad")},
			}}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := NormalizePatchRecordInput(tt.input)
			err := ValidatePatchRecordInput(input)
			if tt.wantErr {
				if err != ErrInvalidAssetDecisionInput {
					t.Fatalf("ValidatePatchRecordInput() error = %v, want invalid input", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidatePatchRecordInput() error = %v", err)
			}
			if len(input.Members) > 0 {
				member := input.Members[0]
				if member.VPSID != "vps_001" {
					t.Fatalf("normalized member vps_id = %q, want vps_001", member.VPSID)
				}
				if member.FollowupNote.Set && member.FollowupNote.Value != "" && member.FollowupNote.Value != "等待迁移窗口" {
					t.Fatalf("normalized followup note = %q, want trimmed note", member.FollowupNote.Value)
				}
			}
		})
	}
}

func fact(vpsID, name, providerID, providerName, country, region, city string, usage vpsassets.UsageStatus, subscription *subscriptions.Record) Fact {
	providerPtr := &providerID
	if providerID == "" {
		providerPtr = nil
	}
	return Fact{
		VPS: vpsassets.Record{
			VPSID:           vpsID,
			DisplayName:     name,
			ProviderID:      providerPtr,
			ProviderName:    providerName,
			Country:         country,
			Region:          region,
			City:            city,
			IPv4:            "192.0.2.1",
			SSHPort:         22,
			LifecycleStatus: vpsassets.LifecycleActive,
			UsageStatus:     usage,
			RenewalDecision: vpsassets.RenewalUnreviewed,
			CreatedAt:       time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC),
			UpdatedAt:       time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC),
		},
		PrimarySubscription: subscription,
		SubscriptionCount: func() int {
			if subscription == nil {
				return 0
			}
			return 1
		}(),
		ActiveSubscriptionCount: func() int {
			if subscription != nil && subscription.Status == subscriptions.StatusActive {
				return 1
			}
			return 0
		}(),
		SourceAvailability: SourceAvailability{
			Subscriptions: true,
			Services:      true,
			Domains:       true,
			Monitoring:    true,
			Targets:       true,
		},
	}
}

func sub(id string, monthly float64, renewInDays int) *subscriptions.Record {
	renewAt := subscriptions.NewDate(time.Now().UTC().AddDate(0, 0, renewInDays))
	return &subscriptions.Record{
		SubscriptionID: id,
		VPSID:          "vps",
		Currency:       "USD",
		MonthlyPrice:   monthly,
		RenewAt:        &renewAt,
		Status:         subscriptions.StatusActive,
		CreatedAt:      time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:      time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC),
	}
}

func hasGroupType(groups []GroupDetail, groupType GroupType) bool {
	for _, group := range groups {
		if group.GroupType == groupType {
			return true
		}
	}
	return false
}

func firstGroup(groups []GroupDetail, groupType GroupType) GroupDetail {
	for _, group := range groups {
		if group.GroupType == groupType {
			return group
		}
	}
	return GroupDetail{}
}

func comparisonMemberByID(members []GroupMember, vpsID string) GroupMember {
	for _, member := range members {
		if member.VPS.VPSID == vpsID {
			return member
		}
	}
	return GroupMember{}
}

func manualComparisonMemberByID(members []ManualGroupMember, vpsID string) ManualGroupMember {
	for _, member := range members {
		if member.VPSID == vpsID {
			return member
		}
	}
	return ManualGroupMember{}
}

func hasComparisonLane(counts []ComparisonLaneCount, lane ComparisonLane) bool {
	for _, count := range counts {
		if count.Lane == lane && count.Count > 0 {
			return true
		}
	}
	return false
}

func hasComparisonSignal(signals []ComparisonSignal, kind string) bool {
	for _, signal := range signals {
		if signal.Kind == kind {
			return true
		}
	}
	return false
}

func assertNoFutureComparisonSignals(t *testing.T, group GroupDetail) {
	t.Helper()
	forbidden := map[string]struct{}{
		"ip_quality":     {},
		"route_quality":  {},
		"performance":    {},
		"cpu":            {},
		"io":             {},
		"oversell":       {},
		"cpu_trend":      {},
		"io_trend":       {},
		"route_latency":  {},
		"performance_io": {},
	}
	for _, signal := range group.ComparisonInsight.Tradeoffs {
		if _, ok := forbidden[signal.Kind]; ok {
			t.Fatalf("group comparison tradeoffs = %#v, must not include future agent-derived signal %q", group.ComparisonInsight.Tradeoffs, signal.Kind)
		}
	}
	for _, member := range group.Members {
		for _, signals := range [][]ComparisonSignal{
			member.ComparisonInsight.Strengths,
			member.ComparisonInsight.Risks,
			member.ComparisonInsight.Gaps,
			member.ComparisonInsight.Tradeoffs,
		} {
			for _, signal := range signals {
				if _, ok := forbidden[signal.Kind]; ok {
					t.Fatalf("member comparison = %#v, must not include future agent-derived signal %q", member.ComparisonInsight, signal.Kind)
				}
			}
		}
	}
}

func TestExecutionReadbackCancelDetectsDoneDrift(t *testing.T) {
	f := fact("vps_cancel", "Cancel Candidate", "pv_1", "Provider", "Japan", "", "Tokyo", vpsassets.UsageIdle, sub("sub_1", 10, 30))
	f.RunningMonitoringCount = 1
	member := RecordMember{
		VPSID:          "vps_cancel",
		DecidedAction:  ActionOpenCancellationWorkbench,
		FollowupStatus: FollowupDone,
	}

	readback := EvaluateMemberExecutionReadback(member, FactsByVPSID([]Fact{f}))
	if readback.Status != ReadbackDrift {
		t.Fatalf("readback = %#v, want drift", readback)
	}
	if !hasReadbackIssue(readback, "cancel_lifecycle_open") || !hasReadbackIssue(readback, "active_subscription_remaining") || !hasReadbackIssue(readback, "running_monitoring_remaining") {
		t.Fatalf("issues = %#v, want cancel lifecycle/subscription/monitoring drift", readback.Issues)
	}
}

func TestExecutionReadbackCancelAlignedWhenFactsClosed(t *testing.T) {
	f := fact("vps_cancel", "Cancel Candidate", "pv_1", "Provider", "Japan", "", "Tokyo", vpsassets.UsageIdle, nil)
	f.VPS.LifecycleStatus = vpsassets.LifecycleCancelled
	member := RecordMember{VPSID: "vps_cancel", DecidedAction: ActionCancel, FollowupStatus: FollowupDone}

	readback := EvaluateMemberExecutionReadback(member, FactsByVPSID([]Fact{f}))
	if readback.Status != ReadbackAligned || len(readback.Issues) != 0 {
		t.Fatalf("readback = %#v, want aligned without issues", readback)
	}
}

func TestExecutionReadbackMigrateKeepsOldCarrierAsDrift(t *testing.T) {
	f := fact("vps_migrate", "Migrate Candidate", "pv_1", "Provider", "US", "CA", "Los Angeles", vpsassets.UsageInUse, sub("sub_1", 20, 30))
	f.VPS.RenewalDecision = vpsassets.RenewalMigrate
	f.ServiceCount = 1
	member := RecordMember{VPSID: "vps_migrate", DecidedAction: ActionMigrate, FollowupStatus: FollowupDone}

	readback := EvaluateMemberExecutionReadback(member, FactsByVPSID([]Fact{f}))
	if readback.Status != ReadbackDrift || !hasReadbackIssue(readback, "old_carrier_remaining") {
		t.Fatalf("readback = %#v, want old carrier drift", readback)
	}
}

func TestExecutionReadbackCompleteEvidenceUsesOnlyCurrentEvidenceGaps(t *testing.T) {
	f := fact("vps_gap", "Evidence Gap", "", "", "", "", "", vpsassets.UsageInUse, nil)
	f.SourceAvailability.Subscriptions = false
	member := RecordMember{VPSID: "vps_gap", DecidedAction: ActionCompleteEvidence, FollowupStatus: FollowupTodo}

	readback := EvaluateMemberExecutionReadback(member, FactsByVPSID([]Fact{f}))
	if readback.Status != ReadbackNeedsEvidence {
		t.Fatalf("readback = %#v, want needs evidence", readback)
	}
	if !hasReadbackIssue(readback, "evidence_gap") {
		t.Fatalf("issues = %#v, want existing evidence gaps", readback.Issues)
	}
	for _, issue := range readback.Issues {
		if issue.Kind == "performance" || issue.Kind == "route_quality" || issue.Kind == "ip_quality" {
			t.Fatalf("issues = %#v, must not include future agent-derived signals", readback.Issues)
		}
	}
}

func TestExecutionReadbackFollowupPriority(t *testing.T) {
	f := fact("vps_keep", "Keep Candidate", "pv_1", "Provider", "Germany", "", "Frankfurt", vpsassets.UsageInUse, sub("sub_1", 12, 30))
	f.VPS.RenewalDecision = vpsassets.RenewalKeep

	blocked := EvaluateMemberExecutionReadback(RecordMember{VPSID: "vps_keep", DecidedAction: ActionKeep, FollowupStatus: FollowupBlocked}, FactsByVPSID([]Fact{f}))
	if blocked.Status != ReadbackBlocked {
		t.Fatalf("blocked readback = %#v, want blocked", blocked)
	}

	skipped := EvaluateMemberExecutionReadback(RecordMember{VPSID: "vps_keep", DecidedAction: ActionKeep, FollowupStatus: FollowupSkipped}, FactsByVPSID([]Fact{f}))
	if skipped.Status != ReadbackAligned {
		t.Fatalf("skipped readback = %#v, want aligned", skipped)
	}

	f.VPS.LifecycleStatus = vpsassets.LifecycleCancelled
	skippedDrift := EvaluateMemberExecutionReadback(RecordMember{VPSID: "vps_keep", DecidedAction: ActionKeep, FollowupStatus: FollowupSkipped}, FactsByVPSID([]Fact{f}))
	if skippedDrift.Status != ReadbackDrift {
		t.Fatalf("skipped drift readback = %#v, want drift for critical fact split", skippedDrift)
	}
}

func TestExecutionReadbackAggregatesRecordStatus(t *testing.T) {
	members := []RecordMember{
		{ExecutionReadback: MemberExecutionReadback{Status: ReadbackAligned}},
		{ExecutionReadback: MemberExecutionReadback{Status: ReadbackNeedsEvidence}},
	}
	readback := EvaluateRecordExecutionReadback(RecordStatusInProgress, members)
	if readback.Status != ReadbackNeedsEvidence || readback.NeedsEvidenceCount != 1 {
		t.Fatalf("record readback = %#v, want needs evidence", readback)
	}

	abandoned := EvaluateRecordExecutionReadback(RecordStatusAbandoned, members)
	if abandoned.Status != ReadbackInactive {
		t.Fatalf("abandoned readback = %#v, want inactive", abandoned)
	}
}

func TestExecutionReadbackMissingCurrentFact(t *testing.T) {
	readback := EvaluateMemberExecutionReadback(RecordMember{VPSID: "vps_missing", DecidedAction: ActionKeep}, map[string]Fact{})
	if readback.Status != ReadbackDrift || !hasReadbackIssue(readback, "current_fact_missing") || readback.CurrentFacts.Found {
		t.Fatalf("readback = %#v, want missing fact drift", readback)
	}
}

func TestExecutionPlanMapsActionsToLanesAndSteps(t *testing.T) {
	tests := []struct {
		name     string
		action   SuggestedAction
		readback MemberExecutionReadback
		lane     ExecutionPlanLane
		step     ExecutionPlanStepKind
	}{
		{
			name:     "cancel opens cancellation workbench",
			action:   ActionOpenCancellationWorkbench,
			readback: MemberExecutionReadback{Status: ReadbackOpen},
			lane:     PlanLaneCancelRetire,
			step:     PlanStepOpenCancellationWorkbench,
		},
		{
			name:     "migrate opens vps detail",
			action:   ActionMigrate,
			readback: MemberExecutionReadback{Status: ReadbackOpen},
			lane:     PlanLaneMigration,
			step:     PlanStepOpenVPSDetail,
		},
		{
			name:     "keep opens vps detail",
			action:   ActionKeep,
			readback: MemberExecutionReadback{Status: ReadbackAligned},
			lane:     PlanLaneKeepObserve,
			step:     PlanStepOpenVPSDetail,
		},
		{
			name:   "subscription evidence opens subscription context",
			action: ActionCompleteEvidence,
			readback: MemberExecutionReadback{Status: ReadbackNeedsEvidence, Issues: []ExecutionReadbackIssue{{
				Kind:  "evidence_gap",
				Label: "缺订阅",
				Tone:  "alert",
			}}},
			lane: PlanLaneEvidence,
			step: PlanStepOpenSubscriptionContext,
		},
		{
			name:     "review stays on record",
			action:   ActionReview,
			readback: MemberExecutionReadback{Status: ReadbackOpen},
			lane:     PlanLaneReview,
			step:     PlanStepReviewRecord,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := EvaluateMemberExecutionPlan(RecordStatusInProgress, RecordMember{
				VPSID:             "vps_001",
				DecidedAction:     tt.action,
				FollowupStatus:    FollowupTodo,
				ExecutionReadback: tt.readback,
			})
			if plan.Lane != tt.lane || plan.StepKind != tt.step {
				t.Fatalf("plan = %#v, want lane=%s step=%s", plan, tt.lane, tt.step)
			}
		})
	}
}

func TestExecutionPlanPreservesCompletedDriftAndAbandonedInactive(t *testing.T) {
	f := fact("vps_cancel", "Cancel Candidate", "pv_1", "Provider", "Japan", "", "Tokyo", vpsassets.UsageIdle, sub("sub_1", 10, 30))
	detail := ApplyExecutionReadback(RecordDetail{
		RecordSummary: RecordSummary{Status: RecordStatusCompleted},
		Members: []RecordMember{{
			VPSID:          "vps_cancel",
			DecidedAction:  ActionCancel,
			FollowupStatus: FollowupDone,
		}},
	}, []Fact{f})
	if detail.ExecutionReadback.Status != ReadbackDrift || detail.ExecutionPlan.ActionableCount != 1 {
		t.Fatalf("completed detail readback=%#v plan=%#v, want drift/actionable", detail.ExecutionReadback, detail.ExecutionPlan)
	}
	if detail.Members[0].ExecutionPlan.Lane != PlanLaneCancelRetire || detail.Members[0].ExecutionPlan.StepKind != PlanStepOpenCancellationWorkbench {
		t.Fatalf("member plan = %#v, want cancellation workbench drift plan", detail.Members[0].ExecutionPlan)
	}

	abandoned := ApplyExecutionReadback(RecordDetail{
		RecordSummary: RecordSummary{Status: RecordStatusAbandoned},
		Members: []RecordMember{{
			VPSID:          "vps_cancel",
			DecidedAction:  ActionCancel,
			FollowupStatus: FollowupTodo,
		}},
	}, []Fact{f})
	if abandoned.ExecutionReadback.Status != ReadbackInactive || abandoned.ExecutionPlan.ActionableCount != 0 || abandoned.Members[0].ExecutionPlan.Actionable {
		t.Fatalf("abandoned readback=%#v plan=%#v member=%#v, want inactive non-actionable", abandoned.ExecutionReadback, abandoned.ExecutionPlan, abandoned.Members[0].ExecutionPlan)
	}
}

func TestExecutionPlanBlockedAndCurrentFactMissing(t *testing.T) {
	blocked := EvaluateMemberExecutionPlan(RecordStatusInProgress, RecordMember{
		VPSID:          "vps_blocked",
		DecidedAction:  ActionKeep,
		FollowupStatus: FollowupBlocked,
		ExecutionReadback: MemberExecutionReadback{
			Status: ReadbackBlocked,
		},
	})
	if !blocked.Blocked || !blocked.Actionable || blocked.Tone != PlanToneCritical {
		t.Fatalf("blocked plan = %#v, want critical actionable blocked plan", blocked)
	}

	missing := EvaluateMemberExecutionReadback(RecordMember{VPSID: "vps_missing", DecidedAction: ActionKeep}, map[string]Fact{})
	plan := EvaluateMemberExecutionPlan(RecordStatusInProgress, RecordMember{
		VPSID:             "vps_missing",
		DecidedAction:     ActionKeep,
		FollowupStatus:    FollowupTodo,
		ExecutionReadback: missing,
	})
	if plan.Lane != PlanLaneReview || plan.StepKind != PlanStepReviewRecord || !plan.Actionable || plan.IssueCount != 1 {
		t.Fatalf("missing fact plan = %#v, want review step with stable issue", plan)
	}
}

func hasReadbackIssue(readback MemberExecutionReadback, kind string) bool {
	for _, issue := range readback.Issues {
		if issue.Kind == kind {
			return true
		}
	}
	return false
}
