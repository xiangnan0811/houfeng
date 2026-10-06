package adapters

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
)

// 来源可用但窗口内没有数据、或数据量超过上限，必须是可区分的类型化错误，不能落成 500。
func TestEvidenceAdaptersClassifyEmptyAndOversizedWindows(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	window := evidence.TimeWindow{Start: now.Add(-time.Hour), End: now}
	options := AdapterOptions{Clock: func() time.Time { return now }}

	t.Run("monitoring host without buckets", func(t *testing.T) {
		adapter, err := NewMonitoringHostAdapter(staticMonitoringSource{host: MonitoringSeriesCapture{RequestedWindow: window}},
			monitoringTestResolver(t, recordauth.SourceKindMonitoringInstance, "mi_0123456789abcdef"), options)
		if err != nil {
			t.Fatalf("NewMonitoringHostAdapter() error = %v", err)
		}
		_, err = adapter.PreviewCapture(context.Background(), monitoringTestActor(t), evidence.Selection{
			Key: evidence.MonitoringHostV1Key(), SourceType: string(recordauth.SourceKindMonitoringInstance),
			SourceID: "mi_0123456789abcdef", RequestedWindow: window, Metrics: []string{"cpu_usage_pct"},
		})
		if !errors.Is(err, evidence.ErrSourceEmpty) {
			t.Fatalf("PreviewCapture(empty host) error = %v, want ErrSourceEmpty", err)
		}
	})

	for _, test := range []struct {
		name    string
		capture MonitoringEventCapture
		want    error
	}{
		{name: "events empty", capture: MonitoringEventCapture{}, want: evidence.ErrSourceEmpty},
		{name: "events oversized", capture: MonitoringEventCapture{EventCount: evidence.MaxSnapshotDataPoints + 1, Events: make([]MonitoringEventFact, evidence.MaxSnapshotDataPoints+1)}, want: evidence.ErrWindowTooLarge},
		// 计数与条目不一致是完整性问题，即使计数超上限也不能报成“窗口过大”。
		{name: "events count drift beyond limit", capture: MonitoringEventCapture{EventCount: evidence.MaxSnapshotDataPoints + 1}, want: evidence.ErrInvalidCanonicalPayload},
	} {
		t.Run(test.name, func(t *testing.T) {
			adapter, err := NewMonitoringEventAdapter(staticMonitoringEventSource{capture: test.capture},
				monitoringTestResolver(t, recordauth.SourceKindMonitoringInstance, "mi_0123456789abcdef"), options)
			if err != nil {
				t.Fatalf("NewMonitoringEventAdapter() error = %v", err)
			}
			_, err = adapter.PreviewCapture(context.Background(), monitoringTestActor(t), evidence.Selection{
				Key: evidence.MonitoringEventV2Key(), SourceType: string(recordauth.SourceKindMonitoringInstance),
				SourceID: "mi_0123456789abcdef", RequestedWindow: window,
			})
			if !errors.Is(err, test.want) {
				t.Fatalf("PreviewCapture(%s) error = %v, want %v", test.name, err, test.want)
			}
		})
	}

	for _, test := range []struct {
		name    string
		capture CommandAuditCapture
		want    error
	}{
		{name: "audits empty", capture: CommandAuditCapture{}, want: evidence.ErrSourceEmpty},
		{name: "audits oversized", capture: CommandAuditCapture{AuditCount: evidence.MaxSnapshotDataPoints + 1, Audits: make([]CommandAuditFact, evidence.MaxSnapshotDataPoints+1)}, want: evidence.ErrWindowTooLarge},
		{name: "audits count drift beyond limit", capture: CommandAuditCapture{AuditCount: evidence.MaxSnapshotDataPoints + 1}, want: evidence.ErrInvalidCanonicalPayload},
	} {
		t.Run(test.name, func(t *testing.T) {
			adapter, err := NewCommandAuditAdapter(staticCommandAuditSource{capture: test.capture},
				monitoringTestResolver(t, recordauth.SourceKindMonitoringInstance, "mi_0123456789abcdef"), options)
			if err != nil {
				t.Fatalf("NewCommandAuditAdapter() error = %v", err)
			}
			_, err = adapter.PreviewCapture(context.Background(), monitoringTestActor(t), evidence.Selection{
				Key: evidence.CommandAuditV1Key(), SourceType: string(recordauth.SourceKindMonitoringInstance),
				SourceID: "mi_0123456789abcdef", RequestedWindow: window,
			})
			if !errors.Is(err, test.want) {
				t.Fatalf("PreviewCapture(%s) error = %v, want %v", test.name, err, test.want)
			}
		})
	}

	if !errors.Is(ErrMonitoringEvidenceLimitExceeded, evidence.ErrWindowTooLarge) {
		t.Fatal("monitoring bucket limit must classify as ErrWindowTooLarge")
	}
}

// 带指标的事件按“事件数 + 指标数”计数据点；超上限是窗口过大，不能与 watermark 完整性错误混为一谈。
func TestMonitoringEventDataPointLimitIsWindowTooLarge(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	window := evidence.TimeWindow{Start: now.Add(-time.Hour), End: now}
	template := validMonitoringEventCapture(window).Events[0]
	count := int(evidence.MaxSnapshotDataPoints/2) + 1
	events := make([]MonitoringEventFact, count)
	for index := range events {
		event := template
		event.EventID = fmt.Sprintf("evt_%016x", index)
		events[index] = event
	}
	capture := MonitoringEventCapture{
		EventCount: uint64(count), ProducerVersion: "state-change-events/v2",
		SourceWatermark: template.RecordedAt.Format(time.RFC3339Nano), Events: events,
	}
	adapter, err := NewMonitoringEventAdapter(staticMonitoringEventSource{capture: capture},
		monitoringTestResolver(t, recordauth.SourceKindMonitoringInstance, "mi_0123456789abcdef"), AdapterOptions{Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("NewMonitoringEventAdapter() error = %v", err)
	}
	_, err = adapter.PreviewCapture(context.Background(), monitoringTestActor(t), evidence.Selection{
		Key: evidence.MonitoringEventV2Key(), SourceType: string(recordauth.SourceKindMonitoringInstance),
		SourceID: "mi_0123456789abcdef", RequestedWindow: window,
	})
	if !errors.Is(err, evidence.ErrWindowTooLarge) {
		t.Fatalf("PreviewCapture(data point limit) error = %v, want ErrWindowTooLarge", err)
	}
}

// 来源存在但当前用户无权采集：与不存在一样归入 ErrSourceNotFound，并保留内层拒绝。
// 发布时据此判成预览失效，而不是被当成记录本身被撤销。
func TestEvidenceSourceAuthorizationDenialIsSourceNotFound(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	viewer, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID: "usr_0123456789abcdef01234567", Role: recordauth.RoleViewer, ProjectID: recordauth.ProjectIDDefault,
	})
	if err != nil {
		t.Fatalf("NormalizeActorScope() error = %v", err)
	}
	_, err = resolveEvidenceSource(context.Background(), monitoringTestResolver(t, recordauth.SourceKindMonitoringInstance, "mi_0123456789abcdef"), viewer, evidence.Selection{
		Key: evidence.MonitoringHostV1Key(), SourceType: string(recordauth.SourceKindMonitoringInstance),
		SourceID: "mi_0123456789abcdef", RequestedWindow: evidence.TimeWindow{Start: now.Add(-time.Hour), End: now},
	})
	if !errors.Is(err, evidence.ErrSourceNotFound) || !errors.Is(err, recordauth.ErrDenied) {
		t.Fatalf("resolveEvidenceSource(viewer) error = %v, want ErrSourceNotFound wrapping ErrDenied", err)
	}
}
