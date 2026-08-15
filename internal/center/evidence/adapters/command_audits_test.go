package adapters

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
)

var _ evidence.Kind = (*CommandAuditAdapter)(nil)

func TestCommandAuditAdapterIsMetadataOnlyAcrossAllContracts(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	window := evidence.TimeWindow{Start: now.Add(-time.Hour), End: now}
	hostile := []string{"stdout-secret", "stderr-secret", "details-secret", "https://user:pass@example.com/run?token=secret", "api_token=secret", `{"arbitrary":"json-secret"}`}
	source := staticCommandAuditSource{capture: validCommandAuditCapture(window), ignoredDiagnostics: strings.Join(hostile, "|")}
	adapter, err := NewCommandAuditAdapter(source, monitoringTestResolver(t, recordauth.SourceKindMonitoringInstance, "mi_0123456789abcdef"), AdapterOptions{Clock: func() time.Time { return now }, NewIntentID: func() (string, error) { return "evi_333333333333333333333333", nil }})
	if err != nil {
		t.Fatalf("NewCommandAuditAdapter() error = %v", err)
	}
	selection := evidence.Selection{Key: evidence.CommandAuditV1Key(), SourceType: string(recordauth.SourceKindMonitoringInstance), SourceID: "mi_0123456789abcdef", RequestedWindow: window}
	preview, err := adapter.PreviewCapture(context.Background(), monitoringTestActor(t), selection)
	if err != nil {
		t.Fatalf("PreviewCapture() error = %v", err)
	}
	snapshot, err := adapter.Capture(context.Background(), monitoringTestActor(t), evidence.Intent{ID: preview.IntentID, Key: selection.Key, Selection: selection, PreviewDigest: sha256.Sum256([]byte("preview")), ValidUntil: preview.ValidUntil})
	if err != nil {
		t.Fatalf("Capture() error = %v", err)
	}
	summary := adapter.Summarize(snapshot)
	comparison := adapter.Compare(snapshot, snapshot, evidence.Alignment{Mode: evidence.AlignmentExact})
	exported := adapter.Export(snapshot, evidence.ExportModeSafe)
	previewBytes, err := json.Marshal(preview)
	if err != nil {
		t.Fatalf("json.Marshal(preview) error = %v", err)
	}
	outputs := [][]byte{previewBytes, snapshot.Bytes(), []byte(summary.Title), []byte(summary.SearchText), []byte(mapString(summary.ReadModel)), []byte(mapString(comparison.Values)), exported.Bytes}
	for _, output := range outputs {
		for _, forbidden := range hostile {
			if bytes.Contains(output, []byte(forbidden)) {
				t.Fatalf("output leaks hostile source-only value %q: %s", forbidden, output)
			}
		}
	}
	for _, required := range []string{`"command_result_retention_seconds":86400`, `"command_result_payload_allowed":false`, `"command_id":"uptime"`, `"event_type":"completed"`, `"exit_code":0`} {
		if !containsBytes(snapshot.Bytes(), required) {
			t.Fatalf("snapshot = %s, want %s", snapshot.Bytes(), required)
		}
	}
	if summary.ReadModel["version"] != "command_audit_read_model/v1" || comparison.Values["version"] != "command_audit_comparison/v1" {
		t.Fatalf("summary/comparison = %#v/%#v, want versioned allowlists", summary, comparison)
	}
	if err := evidence.VerifyKindConformance(context.Background(), adapter, evidence.ConformanceFixture{Actor: monitoringTestActor(t), Selection: selection, Intent: evidence.Intent{ID: preview.IntentID, Key: selection.Key, Selection: selection, PreviewDigest: sha256.Sum256([]byte("preview")), ValidUntil: preview.ValidUntil}, Alignment: evidence.Alignment{Mode: evidence.AlignmentExact}, ExportMode: evidence.ExportModeSafe}); err != nil {
		t.Fatalf("VerifyKindConformance() error = %v", err)
	}
}

func TestCommandAuditAdapterRejectsMalformedCustomSourceFacts(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	window := evidence.TimeWindow{Start: now.Add(-time.Hour), End: now}
	tests := []struct {
		name   string
		mutate func(*CommandAuditCapture)
	}{
		{name: "count drift", mutate: func(c *CommandAuditCapture) { c.AuditCount++ }},
		{name: "duplicate audit", mutate: func(c *CommandAuditCapture) { c.Audits[1].AuditID = c.Audits[0].AuditID }},
		{name: "source mismatch", mutate: func(c *CommandAuditCapture) { c.Audits[0].MonitoringInstanceID = "mi_other" }},
		{name: "unknown command", mutate: func(c *CommandAuditCapture) { c.Audits[0].CommandID = "shell_anything" }},
		{name: "unknown sensitivity", mutate: func(c *CommandAuditCapture) { c.Audits[0].Sensitivity = "root" }},
		{name: "unknown event", mutate: func(c *CommandAuditCapture) { c.Audits[0].EventType = "printed" }},
		{name: "unknown outcome", mutate: func(c *CommandAuditCapture) { c.Audits[0].Outcome = "maybe" }},
		{name: "raw URL identity snapshot", mutate: func(c *CommandAuditCapture) {
			for index := range c.Audits {
				c.Audits[index].MonitoringInstanceName = "https://user:pass@example.com/run?view=full"
			}
		}},
		{name: "scheme relative raw URL query", mutate: func(c *CommandAuditCapture) {
			for index := range c.Audits {
				c.Audits[index].MonitoringInstanceName = "//example.com/run?view=full"
			}
		}},
		{name: "raw URL userinfo", mutate: func(c *CommandAuditCapture) {
			for index := range c.Audits {
				c.Audits[index].ActorDisplayName = "user:pass@example.com/run"
			}
		}},
		{name: "bare raw URL query", mutate: func(c *CommandAuditCapture) {
			for index := range c.Audits {
				c.Audits[index].MonitoringInstanceName = "example.com/run?view=full"
			}
		}},
		{name: "bare raw URL userinfo", mutate: func(c *CommandAuditCapture) {
			for index := range c.Audits {
				c.Audits[index].ActorDisplayName = "operator@example.com/run"
			}
		}},
		{name: "spaced secret assignment", mutate: func(c *CommandAuditCapture) {
			for index := range c.Audits {
				c.Audits[index].ActorDisplayName = "api_token = secret"
			}
		}},
		{name: "arbitrary JSON identity snapshot", mutate: func(c *CommandAuditCapture) {
			for index := range c.Audits {
				c.Audits[index].ActorDisplayName = `{"arbitrary":"value"}`
			}
		}},
		{name: "submicrosecond time", mutate: func(c *CommandAuditCapture) { c.Audits[0].OccurredAt = c.Audits[0].OccurredAt.Add(time.Nanosecond) }},
		{name: "non canonical UTC time", mutate: func(c *CommandAuditCapture) {
			c.Audits[0].OccurredAt = c.Audits[0].OccurredAt.In(time.FixedZone("offset", 8*60*60))
		}},
		{name: "action actor identity drift", mutate: func(c *CommandAuditCapture) {
			c.Audits[1].ActorDisplayName = "Different Actor"
		}},
		{name: "queued from agent source", mutate: func(c *CommandAuditCapture) { c.Audits[0].Source = "agent_sync" }},
		{name: "completed from web source", mutate: func(c *CommandAuditCapture) { c.Audits[1].Source = "web" }},
		{name: "exit on queued", mutate: func(c *CommandAuditCapture) { exit := 1; c.Audits[0].ExitCode = &exit }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capture := validCommandAuditCapture(window)
			tt.mutate(&capture)
			adapter, err := NewCommandAuditAdapter(staticCommandAuditSource{capture: capture}, monitoringTestResolver(t, recordauth.SourceKindMonitoringInstance, "mi_0123456789abcdef"), AdapterOptions{Clock: func() time.Time { return now }})
			if err != nil {
				t.Fatalf("NewCommandAuditAdapter() error = %v", err)
			}
			_, err = adapter.PreviewCapture(context.Background(), monitoringTestActor(t), evidence.Selection{Key: evidence.CommandAuditV1Key(), SourceType: string(recordauth.SourceKindMonitoringInstance), SourceID: "mi_0123456789abcdef", RequestedWindow: window})
			if !errors.Is(err, evidence.ErrInvalidCanonicalPayload) {
				t.Fatalf("PreviewCapture() error = %v, want ErrInvalidCanonicalPayload", err)
			}
		})
	}
}

func TestCommandAuditAdapterAcceptsPlainBracketedIdentityText(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	window := evidence.TimeWindow{Start: now.Add(-time.Hour), End: now}
	capture := validCommandAuditCapture(window)
	for index := range capture.Audits {
		capture.Audits[index].MonitoringInstanceName = "[edge] instance"
	}
	adapter, err := NewCommandAuditAdapter(staticCommandAuditSource{capture: capture}, monitoringTestResolver(t, recordauth.SourceKindMonitoringInstance, "mi_0123456789abcdef"), AdapterOptions{Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("NewCommandAuditAdapter() error = %v", err)
	}
	_, err = adapter.PreviewCapture(context.Background(), monitoringTestActor(t), evidence.Selection{Key: evidence.CommandAuditV1Key(), SourceType: string(recordauth.SourceKindMonitoringInstance), SourceID: "mi_0123456789abcdef", RequestedWindow: window})
	if err != nil {
		t.Fatalf("PreviewCapture() error = %v, want plain bracketed identity text accepted", err)
	}
}

func TestCommandAuditAdapterAcceptsEmailUsernameSnapshot(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	window := evidence.TimeWindow{Start: now.Add(-time.Hour), End: now}
	capture := validCommandAuditCapture(window)
	for index := range capture.Audits {
		capture.Audits[index].ActorUsername = "operator@example.com"
	}
	adapter, err := NewCommandAuditAdapter(staticCommandAuditSource{capture: capture}, monitoringTestResolver(t, recordauth.SourceKindMonitoringInstance, "mi_0123456789abcdef"), AdapterOptions{Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("NewCommandAuditAdapter() error = %v", err)
	}
	_, err = adapter.PreviewCapture(context.Background(), monitoringTestActor(t), evidence.Selection{Key: evidence.CommandAuditV1Key(), SourceType: string(recordauth.SourceKindMonitoringInstance), SourceID: "mi_0123456789abcdef", RequestedWindow: window})
	if err != nil {
		t.Fatalf("PreviewCapture() error = %v, want email-shaped username accepted", err)
	}
}

func TestCommandAuditAdapterCanonicalizesCustomSourceOrder(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	window := evidence.TimeWindow{Start: now.Add(-time.Hour), End: now}
	ordered := validCommandAuditCapture(window)
	reversed := validCommandAuditCapture(window)
	reversed.Audits[0], reversed.Audits[1] = reversed.Audits[1], reversed.Audits[0]
	selection := evidence.Selection{Key: evidence.CommandAuditV1Key(), SourceType: string(recordauth.SourceKindMonitoringInstance), SourceID: "mi_0123456789abcdef", RequestedWindow: window}
	digests := make([][32]byte, 0, 2)
	for _, capture := range []CommandAuditCapture{ordered, reversed} {
		adapter, err := NewCommandAuditAdapter(staticCommandAuditSource{capture: capture}, monitoringTestResolver(t, recordauth.SourceKindMonitoringInstance, "mi_0123456789abcdef"), AdapterOptions{Clock: func() time.Time { return now }})
		if err != nil {
			t.Fatalf("NewCommandAuditAdapter() error = %v", err)
		}
		preview, err := adapter.PreviewCapture(context.Background(), monitoringTestActor(t), selection)
		if err != nil {
			t.Fatalf("PreviewCapture() error = %v", err)
		}
		digests = append(digests, preview.SourceDigest)
	}
	if digests[0] != digests[1] {
		t.Fatalf("source digests differ by source order: %x != %x", digests[0], digests[1])
	}
}

type staticCommandAuditSource struct {
	capture            CommandAuditCapture
	ignoredDiagnostics string
}

func (source staticCommandAuditSource) LoadCommandAuditEvidence(context.Context, string, evidence.TimeWindow) (CommandAuditCapture, error) {
	return source.capture, nil
}

func validCommandAuditCapture(window evidence.TimeWindow) CommandAuditCapture {
	queued := window.Start.Add(10 * time.Minute)
	completed := window.Start.Add(20 * time.Minute)
	exit := 0
	return CommandAuditCapture{AuditCount: 2, ProducerVersion: "command-audit-store/v1", SourceWatermark: completed.Format(time.RFC3339Nano), Audits: []CommandAuditFact{
		{AuditID: "aud_0123456789abcdef", ActionID: "act_0123456789abcdef", MonitoringInstanceID: "mi_0123456789abcdef", MonitoringInstanceName: "edge-1", ActorUserID: "usr_0123456789abcdef01234567", ActorUsername: "admin", ActorDisplayName: "Admin", CommandID: "uptime", Sensitivity: "standard", EventType: "queued", Outcome: "succeeded", Source: "web", OccurredAt: queued},
		{AuditID: "aud_1123456789abcdef", ActionID: "act_0123456789abcdef", MonitoringInstanceID: "mi_0123456789abcdef", MonitoringInstanceName: "edge-1", ActorUserID: "usr_0123456789abcdef01234567", ActorUsername: "admin", ActorDisplayName: "Admin", CommandID: "uptime", Sensitivity: "standard", EventType: "completed", Outcome: "succeeded", Source: "agent_sync", ExitCode: &exit, OccurredAt: completed},
	}}
}

func containsBytes(value []byte, wanted string) bool { return bytes.Contains(value, []byte(wanted)) }

func mapString(value map[string]any) string {
	return strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(toJSON(value)), "\n", ""), "\t", ""))
}

func toJSON(value any) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
