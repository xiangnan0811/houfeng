package incidents

import (
	"testing"
	"time"
)

func TestAdministrativeClosurePreservesReasonWithoutNaturalRecovery(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	previous := []IncidentRecord{{IncidentID: "inc_test", IncidentClass: IncidentMonitoringInstanceDiskPressure, Severity: SeverityAlert}}
	mutation := buildAdministrativeRecoveryMutation(ObjectTypeMonitoringInstance, "mi_test", "123", previous, now, "operator paused monitoring")
	if len(mutation.Active) != 0 || len(mutation.Events) != 1 {
		t.Fatalf("mutation=%+v", mutation)
	}
	e := mutation.Events[0]
	if e.EventType != EventIncidentClosedByManagement || e.ResultingState != "closed_by_management" || e.ClosureReason != "operator paused monitoring" {
		t.Fatalf("closure=%+v", e)
	}
	if !ValidMonitoringEventMetadata(e.ObjectType, e.EventType, e.Severity, e.IsBackfilled, e.Provenance, e.ProducerVersion, e.RuleVersion, e.PriorState, e.ResultingState, e.CorrectionOfEventID) {
		t.Fatal("closure cannot be consumed by evidence readers")
	}
	if ValidMonitoringEventMetadata(e.ObjectType, EventIncidentRecovered, e.Severity, e.IsBackfilled, e.Provenance, e.ProducerVersion, e.RuleVersion, e.PriorState, e.ResultingState, e.CorrectionOfEventID) {
		t.Fatal("management closure accepted as natural recovery")
	}
}
