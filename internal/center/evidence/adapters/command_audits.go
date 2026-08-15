package adapters

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/contracts/agentapi"
)

const (
	commandAuditCalculationVersion = "command-audit-evidence/v1"
	commandOutputRetentionSeconds  = 24 * 60 * 60
)

type CommandAuditSource interface {
	LoadCommandAuditEvidence(context.Context, string, evidence.TimeWindow) (CommandAuditCapture, error)
}

type CommandAuditCapture struct {
	AuditCount      uint64
	ProducerVersion string
	SourceWatermark string
	Audits          []CommandAuditFact
}

type CommandAuditFact struct {
	AuditID                string
	ActionID               string
	MonitoringInstanceID   string
	MonitoringInstanceName string
	ActorUserID            string
	ActorUsername          string
	ActorDisplayName       string
	CommandID              string
	Sensitivity            string
	EventType              string
	Outcome                string
	Source                 string
	ExitCode               *int
	OccurredAt             time.Time
}

type CommandAuditAdapter struct {
	source     CommandAuditSource
	resolver   EvidenceSourceResolver
	options    AdapterOptions
	descriptor evidence.Descriptor
}

func NewCommandAuditAdapter(source CommandAuditSource, resolver EvidenceSourceResolver, options AdapterOptions) (*CommandAuditAdapter, error) {
	if source == nil || resolver == nil {
		return nil, fmt.Errorf("%w: nil command audit adapter dependency", evidence.ErrInvalidKindDescriptor)
	}
	descriptor := commandAuditDescriptor()
	if err := descriptor.Validate(); err != nil {
		return nil, err
	}
	return &CommandAuditAdapter{source: source, resolver: resolver, options: options, descriptor: descriptor}, nil
}

func (adapter *CommandAuditAdapter) Descriptor() evidence.Descriptor { return adapter.descriptor }

func (adapter *CommandAuditAdapter) ValidateSelection(_ context.Context, _ evidence.ActorScope, selection evidence.Selection) error {
	if adapter == nil || selection.Key != evidence.CommandAuditV1Key() || selection.SourceType != string(recordauth.SourceKindMonitoringInstance) ||
		!validSourceIdentifier(selection.SourceID) || !validEvidenceWindow(selection.RequestedWindow) || len(selection.Metrics) != 0 ||
		selection.Precision != 0 || len(selection.SensitiveTopologyFields) != 0 {
		return fmt.Errorf("%w: command audit selection", evidence.ErrInvalidCanonicalPayload)
	}
	return nil
}

func (adapter *CommandAuditAdapter) PreviewCapture(ctx context.Context, actor evidence.ActorScope, selection evidence.Selection) (evidence.Preview, error) {
	if err := adapter.ValidateSelection(ctx, actor, selection); err != nil {
		return evidence.Preview{}, err
	}
	evaluated, err := adapter.evaluate(ctx, actor, selection)
	if err != nil {
		return evidence.Preview{}, err
	}
	return previewDiscreteEvidence(adapter.options, adapter.descriptor, selection, evaluated)
}

func (adapter *CommandAuditAdapter) Capture(ctx context.Context, actor evidence.ActorScope, intent evidence.Intent) (evidence.CanonicalSnapshot, error) {
	if err := validateDiscreteIntent(adapter.descriptor.Key, intent); err != nil {
		return evidence.CanonicalSnapshot{}, err
	}
	if err := adapter.ValidateSelection(ctx, actor, intent.Selection); err != nil {
		return evidence.CanonicalSnapshot{}, err
	}
	evaluated, err := adapter.evaluate(ctx, actor, intent.Selection)
	if err != nil {
		return evidence.CanonicalSnapshot{}, err
	}
	return captureDiscreteEvidence(adapter.options, adapter.descriptor, intent.Selection, evaluated)
}

func (adapter *CommandAuditAdapter) Authorize(ctx context.Context, actor evidence.ActorScope, selection evidence.Selection) (evidence.AuthorizationScope, error) {
	if err := adapter.ValidateSelection(ctx, actor, selection); err != nil {
		return evidence.AuthorizationScope{}, err
	}
	resolved, err := resolveEvidenceSource(ctx, adapter.resolver, actor, selection)
	if err != nil {
		return evidence.AuthorizationScope{}, err
	}
	return resolved.Authorization, nil
}

func (adapter *CommandAuditAdapter) Summarize(snapshot evidence.CanonicalSnapshot) evidence.Summary {
	if err := snapshot.Validate(adapter.descriptor); err != nil {
		return evidence.Summary{Key: adapter.descriptor.Key, RendererVersion: adapter.descriptor.Conformance.RendererVersion}
	}
	payload, err := decodeEvidencePayload(snapshot.Bytes())
	if err != nil {
		return evidence.Summary{Key: adapter.descriptor.Key, RendererVersion: adapter.descriptor.Conformance.RendererVersion}
	}
	audits := allowlistedObjects(payload["audits"], []string{"audit_id", "action_id", "monitoring_instance_id", "monitoring_instance_name", "actor_user_id", "actor_username", "actor_display_name", "command_id", "sensitivity", "event_type", "outcome", "source", "exit_code", "occurred_at"})
	commands := make([]string, 0)
	seen := make(map[string]struct{})
	for _, item := range audits {
		command := stringValue(item.(map[string]any)["command_id"])
		if _, exists := seen[command]; !exists {
			seen[command] = struct{}{}
			commands = append(commands, command)
		}
	}
	sort.Strings(commands)
	envelope := snapshot.Envelope()
	return evidence.Summary{Key: adapter.descriptor.Key, RendererVersion: adapter.descriptor.Conformance.RendererVersion, Title: "Command audit", SearchText: "command audit " + envelope.Source.ID + " " + strings.Join(commands, " "), ReadModel: map[string]any{"version": "command_audit_read_model/v1", "audits": audits, "audit_count": envelope.Quality.SampleCount, "command_result_retention_seconds": commandOutputRetentionSeconds, "command_result_payload_allowed": false}}
}

func (adapter *CommandAuditAdapter) Compare(left, right evidence.CanonicalSnapshot, alignment evidence.Alignment) evidence.Comparison {
	compatible, reason := compatibleDiscreteSnapshots(adapter.descriptor, left, right, alignment)
	values := map[string]any{"version": "command_audit_comparison/v1"}
	if compatible {
		leftPayload, leftErr := decodeEvidencePayload(left.Bytes())
		rightPayload, rightErr := decodeEvidencePayload(right.Bytes())
		if leftErr != nil || rightErr != nil {
			compatible, reason = false, "invalid command audit payload"
		} else {
			leftAudits := allowlistedObjects(leftPayload["audits"], []string{"event_type", "outcome"})
			rightAudits := allowlistedObjects(rightPayload["audits"], []string{"event_type", "outcome"})
			values["audit_count_left"] = len(leftAudits)
			values["audit_count_right"] = len(rightAudits)
			values["audit_count_delta"] = len(rightAudits) - len(leftAudits)
			values["event_counts_left"] = countObjectFieldValues(leftAudits, "event_type")
			values["event_counts_right"] = countObjectFieldValues(rightAudits, "event_type")
			values["outcome_counts_left"] = countObjectFieldValues(leftAudits, "outcome")
			values["outcome_counts_right"] = countObjectFieldValues(rightAudits, "outcome")
		}
	}
	return evidence.Comparison{Key: adapter.descriptor.Key, Compatible: compatible, Reason: reason, Values: values}
}

func (adapter *CommandAuditAdapter) Export(snapshot evidence.CanonicalSnapshot, mode evidence.ExportMode) evidence.ExportMaterial {
	return exportEvidenceSnapshot(adapter.descriptor, snapshot, mode)
}

func (adapter *CommandAuditAdapter) evaluate(ctx context.Context, actor evidence.ActorScope, selection evidence.Selection) (discreteEvaluation, error) {
	resolved, err := resolveEvidenceSource(ctx, adapter.resolver, actor, selection)
	if err != nil {
		return discreteEvaluation{}, err
	}
	capture, err := adapter.source.LoadCommandAuditEvidence(ctx, selection.SourceID, selection.RequestedWindow)
	if err != nil {
		return discreteEvaluation{}, err
	}
	if capture.AuditCount == 0 || capture.AuditCount != uint64(len(capture.Audits)) || capture.AuditCount > evidence.MaxSnapshotDataPoints {
		return discreteEvaluation{}, fmt.Errorf("%w: command audit source bound", evidence.ErrInvalidCanonicalPayload)
	}
	capture = cloneAndSortCommandAuditCapture(capture)
	now := adapterNow(adapter.options)
	if err := validateCommandAuditCapture(capture, selection, now); err != nil {
		return discreteEvaluation{}, err
	}
	audits := make([]any, 0, len(capture.Audits))
	for _, audit := range capture.Audits {
		audits = append(audits, map[string]any{"audit_id": audit.AuditID, "action_id": audit.ActionID, "monitoring_instance_id": audit.MonitoringInstanceID, "monitoring_instance_name": audit.MonitoringInstanceName, "actor_user_id": audit.ActorUserID, "actor_username": audit.ActorUsername, "actor_display_name": audit.ActorDisplayName, "command_id": audit.CommandID, "sensitivity": audit.Sensitivity, "event_type": audit.EventType, "outcome": audit.Outcome, "source": audit.Source, "exit_code": audit.ExitCode, "occurred_at": audit.OccurredAt.UTC().Format(time.RFC3339Nano)})
	}
	payload := map[string]any{"audit_count": capture.AuditCount, "command_result_retention_seconds": commandOutputRetentionSeconds, "command_result_payload_allowed": false, "audits": audits}
	canonical, redaction, err := evidence.CanonicalizePayload(adapter.descriptor, payload, evidence.RedactionNormalOnly)
	if err != nil {
		return discreteEvaluation{}, err
	}
	redaction.Decisions = appendForbiddenPreviewDecisions(adapter.descriptor, redaction.Decisions)
	first, last := capture.Audits[0], capture.Audits[len(capture.Audits)-1]
	actualEnd := last.OccurredAt.Add(time.Microsecond)
	if actualEnd.After(selection.RequestedWindow.End) {
		actualEnd = selection.RequestedWindow.End
	}
	return discreteEvaluation{resolved: resolved, actualWindow: evidence.TimeWindow{Start: first.OccurredAt, End: actualEnd}, observedAt: last.OccurredAt, sourceRevision: last.AuditID, sourceWatermark: capture.SourceWatermark, producerVersion: capture.ProducerVersion, calculationVersion: commandAuditCalculationVersion, units: evidence.UnitsSemantics{Status: evidence.UnitsNotApplicable, Reason: "command audit metadata"}, quality: evidence.Quality{Status: evidence.QualityComplete, SampleCount: capture.AuditCount, DataPointCount: capture.AuditCount}, payload: payload, canonical: canonical, redaction: redaction.Decisions}, nil
}

func cloneAndSortCommandAuditCapture(capture CommandAuditCapture) CommandAuditCapture {
	capture.Audits = append([]CommandAuditFact(nil), capture.Audits...)
	for index := range capture.Audits {
		if capture.Audits[index].ExitCode != nil {
			exitCode := *capture.Audits[index].ExitCode
			capture.Audits[index].ExitCode = &exitCode
		}
	}
	sort.Slice(capture.Audits, func(left, right int) bool {
		if !capture.Audits[left].OccurredAt.Equal(capture.Audits[right].OccurredAt) {
			return capture.Audits[left].OccurredAt.Before(capture.Audits[right].OccurredAt)
		}
		return capture.Audits[left].AuditID < capture.Audits[right].AuditID
	})
	return capture
}

func validateCommandAuditCapture(capture CommandAuditCapture, selection evidence.Selection, now time.Time) error {
	watermark, watermarkErr := parseCanonicalPostgresTimestamp(capture.SourceWatermark)
	if capture.AuditCount == 0 || capture.AuditCount != uint64(len(capture.Audits)) || capture.AuditCount > evidence.MaxSnapshotDataPoints || !validVersionString(capture.ProducerVersion) || watermarkErr != nil || watermark.After(now) {
		return fmt.Errorf("%w: command audit source", evidence.ErrInvalidCanonicalPayload)
	}
	seen := make(map[string]struct{}, len(capture.Audits))
	actionIdentities := make(map[string]commandActionIdentity)
	var latest time.Time
	for _, audit := range capture.Audits {
		definitionSensitivity, knownCommand := agentapi.SensitivityForCommand(audit.CommandID)
		if !validSourceIdentifier(audit.AuditID) || audit.MonitoringInstanceID != selection.SourceID || !validSourceIdentifier(audit.MonitoringInstanceID) ||
			!knownCommand || string(definitionSensitivity) != audit.Sensitivity || !knownCommandAuditEvent(audit.EventType) || !knownCommandAuditOutcome(audit.Outcome) || !knownCommandAuditSource(audit.Source) ||
			!canonicalTask4Timestamp(audit.OccurredAt) || audit.OccurredAt.Before(selection.RequestedWindow.Start) || !audit.OccurredAt.Before(selection.RequestedWindow.End) || audit.OccurredAt.After(now) ||
			!validOptionalIdentity(audit.ActorUserID) || !validSnapshotText(audit.MonitoringInstanceName, 256) || !validSnapshotText(audit.ActorUsername, 256) || !validSnapshotText(audit.ActorDisplayName, 256) {
			return fmt.Errorf("%w: command audit fact", evidence.ErrInvalidCanonicalPayload)
		}
		if _, duplicate := seen[audit.AuditID]; duplicate {
			return fmt.Errorf("%w: duplicate command audit", evidence.ErrInvalidCanonicalPayload)
		}
		seen[audit.AuditID] = struct{}{}
		if audit.EventType == "rejected" {
			if audit.ActionID != "" || audit.ExitCode != nil || audit.Source != "web" || audit.Outcome != "rejected" {
				return fmt.Errorf("%w: rejected command audit", evidence.ErrInvalidCanonicalPayload)
			}
		} else if !validSourceIdentifier(audit.ActionID) {
			return fmt.Errorf("%w: command action identity", evidence.ErrInvalidCanonicalPayload)
		}
		if (audit.EventType == "queued" && audit.Source != "web") ||
			((audit.EventType == "dispatched" || audit.EventType == "completed") && audit.Source != "agent_sync") {
			return fmt.Errorf("%w: command audit event source", evidence.ErrInvalidCanonicalPayload)
		}
		if audit.ActionID != "" {
			identity := commandActionIdentity{
				MonitoringInstanceName: audit.MonitoringInstanceName,
				ActorUserID:            audit.ActorUserID,
				ActorUsername:          audit.ActorUsername,
				ActorDisplayName:       audit.ActorDisplayName,
				CommandID:              audit.CommandID,
				Sensitivity:            audit.Sensitivity,
				Outcome:                audit.Outcome,
			}
			if existing, ok := actionIdentities[audit.ActionID]; ok && existing != identity {
				return fmt.Errorf("%w: command action identity drift", evidence.ErrInvalidCanonicalPayload)
			}
			actionIdentities[audit.ActionID] = identity
		}
		if audit.EventType == "completed" {
			if audit.ExitCode == nil || ((*audit.ExitCode == 0) != (audit.Outcome == "succeeded")) || (*audit.ExitCode != 0 && audit.Outcome != "failed") {
				return fmt.Errorf("%w: completed command audit", evidence.ErrInvalidCanonicalPayload)
			}
		} else if audit.ExitCode != nil {
			return fmt.Errorf("%w: non-completed command exit", evidence.ErrInvalidCanonicalPayload)
		}
		if audit.OccurredAt.After(latest) {
			latest = audit.OccurredAt
		}
	}
	if watermark.Before(latest) {
		return fmt.Errorf("%w: command audit watermark", evidence.ErrInvalidCanonicalPayload)
	}
	return nil
}

type commandActionIdentity struct {
	MonitoringInstanceName string
	ActorUserID            string
	ActorUsername          string
	ActorDisplayName       string
	CommandID              string
	Sensitivity            string
	Outcome                string
}

func knownCommandAuditEvent(value string) bool {
	switch value {
	case "queued", "dispatched", "completed", "rejected":
		return true
	default:
		return false
	}
}

func knownCommandAuditOutcome(value string) bool {
	switch value {
	case "queued", "dispatched", "succeeded", "failed", "rejected":
		return true
	default:
		return false
	}
}

func knownCommandAuditSource(value string) bool { return value == "web" || value == "agent_sync" }

func validOptionalIdentity(value string) bool { return value == "" || validSourceIdentifier(value) }

func validSnapshotText(value string, maximum int) bool {
	if !safeActivityText(value, maximum) {
		return false
	}
	parsed, err := url.Parse(value)
	if err == nil && parsed.Host != "" {
		return false
	}
	parsed, err = url.Parse("//" + value)
	if err != nil {
		return true
	}
	if parsed.User != nil {
		_, hasPassword := parsed.User.Password()
		return !hasPassword && parsed.Path == "" && parsed.RawQuery == "" && parsed.Fragment == ""
	}
	if parsed.RawQuery == "" {
		return true
	}
	host := parsed.Hostname()
	return host == "" || (!strings.Contains(host, ".") && !strings.Contains(host, ":") && !strings.EqualFold(host, "localhost"))
}

func commandAuditDescriptor() evidence.Descriptor {
	normal := []string{"audit_count", "command_result_retention_seconds", "command_result_payload_allowed", "audits.audit_id", "audits.action_id", "audits.monitoring_instance_id", "audits.monitoring_instance_name", "audits.actor_user_id", "audits.actor_username", "audits.actor_display_name", "audits.command_id", "audits.sensitivity", "audits.event_type", "audits.outcome", "audits.source", "audits.exit_code", "audits.occurred_at"}
	forbidden := []string{"details", "stdout", "stderr", "output", "raw_output", "raw_json", "diagnostics", "url", "token", "secret", "password", "cookie", "env", "mount", "container_id", "fingerprint"}
	return normalOnlyDescriptor(evidence.CommandAuditV1Key(), "command_audit_v1", normal, forbidden)
}

func countObjectFieldValues(items []any, field string) map[string]any {
	counts := make(map[string]any)
	for _, item := range items {
		value := stringValue(item.(map[string]any)[field])
		count, _ := counts[value].(int)
		counts[value] = count + 1
	}
	return counts
}
