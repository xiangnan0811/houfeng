package assetlinks

import "testing"

func TestAssociationHistoryHasIndependentDependencyStatus(t *testing.T) {
	if got := ClassifyDependency("active", "service", "current"); got != DependencyCurrent {
		t.Fatalf("current association = %s", got)
	}
	if got := ClassifyDependency("active", "domain", "ended"); got != DependencyHistorical {
		t.Fatalf("ended association = %s", got)
	}
	if got := ClassifyDependency("archived", "service", "current"); got != DependencyHistorical {
		t.Fatalf("archived association = %s", got)
	}
}
