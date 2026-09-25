package incidents

import (
	"testing"

	"houfeng/internal/center/monitoringinstances"
)

func TestValidMonitoringEventMetadataRejectsCrossDomainLifecycleTransitions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		priorState     string
		resultingState string
	}{
		{name: "lifecycle into archive marker", priorState: monitoringinstances.LifecycleInUse, resultingState: "archived"},
		{name: "archive marker into lifecycle", priorState: "unarchived", resultingState: monitoringinstances.LifecycleObserving},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if ValidMonitoringEventMetadata(
				ObjectTypeMonitoringInstance,
				EventMonitoringInstanceLifecycleUpdated,
				"",
				false,
				MonitoringEventProvenanceCenter,
				MonitoringEventProducerVersion,
				MonitoringEventLifecycleRuleVersion,
				tt.priorState,
				tt.resultingState,
				"",
			) {
				t.Fatalf("ValidMonitoringEventMetadata(%q -> %q) = true, want false for mixed lifecycle/archive domains", tt.priorState, tt.resultingState)
			}
		})
	}
}

func TestValidMonitoringEventMetadataAdmitsOnlyExplicitLifecycleSameStateEvents(t *testing.T) {
	t.Parallel()

	valid := func(eventType EventType, priorState, resultingState string) bool {
		return ValidMonitoringEventMetadata(
			ObjectTypeMonitoringInstance,
			eventType,
			"",
			false,
			MonitoringEventProvenanceWeb,
			MonitoringEventProducerVersion,
			MonitoringEventLifecycleRuleVersion,
			priorState,
			resultingState,
			"",
		)
	}

	if valid(EventMonitoringInstanceLifecycleUpdated, monitoringinstances.LifecycleRetired, monitoringinstances.LifecycleRetired) {
		t.Fatal("ordinary lifecycle update accepted a same-state transition")
	}
	if !valid(EventMonitoringInstanceRetirementReconciled, monitoringinstances.LifecycleRetired, monitoringinstances.LifecycleRetired) {
		t.Fatal("retirement reconciliation rejected its explicit same-state transition")
	}
	if valid(EventMonitoringInstanceRetirementReconciled, monitoringinstances.LifecycleInUse, monitoringinstances.LifecycleRetired) {
		t.Fatal("retirement reconciliation accepted a lifecycle transition")
	}
	if !valid(EventMonitoringInstanceRestoredFromArchive, "archived", "unarchived") {
		t.Fatal("archive restore rejected its explicit archive-marker transition")
	}
	if valid(EventMonitoringInstanceRestoredFromArchive, "unarchived", "archived") {
		t.Fatal("archive restore accepted the archive direction")
	}
}
