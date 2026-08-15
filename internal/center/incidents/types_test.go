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
