package runtime

import (
	"testing"
	"time"

	"houfeng/internal/contracts/agentapi"
)

func TestHostSampleDueSupportsFiveSecondTier(t *testing.T) {
	t.Parallel()

	firstAt := time.Date(2026, time.May, 18, 8, 0, 0, 0, time.UTC)
	if !hostSampleDue(agentapi.FrequencyTier5s, time.Time{}, firstAt) {
		t.Fatal("hostSampleDue(5s) first sample = false, want true")
	}
	if hostSampleDue(agentapi.FrequencyTier5s, firstAt, firstAt.Add(4*time.Second)) {
		t.Fatal("hostSampleDue(5s) after 4s = true, want false")
	}
	if !hostSampleDue(agentapi.FrequencyTier5s, firstAt, firstAt.Add(5*time.Second)) {
		t.Fatal("hostSampleDue(5s) after 5s = false, want true")
	}
}
