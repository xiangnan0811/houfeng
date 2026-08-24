package vpsoverviewperf

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/activity"
	"houfeng/internal/center/vpsoverview"
)

const testSubjectID = "vps_7c2a4e18b09d5f31"

func TestValidateHealthyOverviewRejectsPartialOrMalformedOverview(t *testing.T) {
	if err := ValidateHealthyOverview(healthyOverviewFixture(), testSubjectID); err != nil {
		t.Fatalf("healthy overview rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*vpsoverview.Overview)
	}{
		{
			name: "overall unavailable",
			mutate: func(overview *vpsoverview.Overview) {
				overview.Summary.Overall.Section.State = vpsoverview.SectionUnavailable
			},
		},
		{
			name: "monitoring unavailable",
			mutate: func(overview *vpsoverview.Overview) {
				overview.Summary.Monitoring.Section.State = vpsoverview.SectionUnavailable
			},
		},
		{
			name: "IP quality unavailable",
			mutate: func(overview *vpsoverview.Overview) {
				overview.Summary.IPQuality.Section.State = vpsoverview.SectionUnavailable
			},
		},
		{
			name: "renewal unavailable",
			mutate: func(overview *vpsoverview.Overview) {
				overview.Summary.Renewal.Section.State = vpsoverview.SectionUnavailable
			},
		},
		{
			name: "activity unavailable",
			mutate: func(overview *vpsoverview.Overview) {
				overview.RecentActivity.Section.State = vpsoverview.SectionUnavailable
			},
		},
		{
			name: "service relation unavailable",
			mutate: func(overview *vpsoverview.Overview) {
				overview.Relations[2].Section.State = vpsoverview.SectionUnavailable
			},
		},
		{
			name: "domain relation unavailable",
			mutate: func(overview *vpsoverview.Overview) {
				overview.Relations[3].Section.State = vpsoverview.SectionUnavailable
			},
		},
		{
			name: "relation count wrong",
			mutate: func(overview *vpsoverview.Overview) {
				overview.Relations[2].Count = 0
			},
		},
		{
			name: "relation order wrong",
			mutate: func(overview *vpsoverview.Overview) {
				overview.Relations[1], overview.Relations[2] = overview.Relations[2], overview.Relations[1]
			},
		},
		{
			name: "activity empty",
			mutate: func(overview *vpsoverview.Overview) {
				overview.RecentActivity.Items = nil
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			overview := healthyOverviewFixture()
			test.mutate(&overview)
			if err := ValidateHealthyOverview(overview, testSubjectID); err == nil {
				t.Fatal("ValidateHealthyOverview() error = nil, want rejection")
			}
		})
	}
}

func TestQueryTraceOwnsStableCountAndErrorContract(t *testing.T) {
	trace := &QueryTrace{}
	for index := 0; index < ExpectedQueryCount; index++ {
		ctx := trace.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{})
		end := pgx.TraceQueryEndData{}
		if index == ExpectedQueryCount-1 {
			end.Err = errors.New("query failed")
		}
		trace.TraceQueryEnd(ctx, nil, end)
	}

	stats := trace.Snapshot()
	if stats.Count != ExpectedQueryCount || stats.Errors != 1 {
		t.Fatalf("Snapshot() = %#v, want count=%d errors=1", stats, ExpectedQueryCount)
	}
	if err := stats.Validate(); err == nil {
		t.Fatal("QueryStats.Validate() error = nil with a query error")
	}
	if got := stats.ErrorRatePercent(); got <= 0 {
		t.Fatalf("ErrorRatePercent() = %f, want positive", got)
	}

	trace.Reset()
	if got := trace.Snapshot(); got != (QueryStats{}) {
		t.Fatalf("Snapshot() after Reset = %#v, want zero", got)
	}
	if err := (QueryStats{Count: ExpectedQueryCount}).Validate(); err != nil {
		t.Fatalf("healthy QueryStats rejected: %v", err)
	}
	if err := (QueryStats{Count: ExpectedQueryCount - 1}).Validate(); err == nil {
		t.Fatal("QueryStats.Validate() error = nil with unstable query count")
	}
}

func healthyOverviewFixture() vpsoverview.Overview {
	ready := vpsoverview.SectionState{State: vpsoverview.SectionReady}
	return vpsoverview.Overview{
		Identity: vpsoverview.Identity{VPSID: testSubjectID},
		Summary: vpsoverview.Summary{
			Overall:    vpsoverview.SummaryCell{Section: ready},
			Monitoring: vpsoverview.SummaryCell{Section: ready},
			IPQuality:  vpsoverview.SummaryCell{Section: ready},
			Renewal:    vpsoverview.SummaryCell{Section: ready},
		},
		RecentActivity: vpsoverview.ActivitySection{
			Section: ready,
			Items:   []activity.Event{{ActivityID: "act_1"}},
		},
		Relations: []vpsoverview.RelationSummary{
			{Kind: "monitoring_instances", Count: 1, Section: ready},
			{Kind: "subscriptions", Count: 1, Section: ready},
			{Kind: "services", Count: 1, Section: ready},
			{Kind: "domains", Count: 1, Section: ready},
		},
	}
}
