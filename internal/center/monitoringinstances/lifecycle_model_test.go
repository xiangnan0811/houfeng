package monitoringinstances

import "testing"

func TestOnlyMonitoringLifecycleStatesAccepted(t *testing.T) {
	for _, state := range []string{LifecyclePendingEnrollment, LifecycleEnrolled, LifecycleRetired} {
		if !IsValidLifecycleStatus(state) {
			t.Fatalf("reject current state %q", state)
		}
	}
	for _, state := range []string{"在用", "观察中", "不续费", "已归档"} {
		if IsValidLifecycleStatus(state) {
			t.Fatalf("accept obsolete state %q", state)
		}
	}
}

func TestOnboardingCompletesOnTrustedLiveEvidenceWithoutSamples(t *testing.T) {
	r := Record{LifecycleStatus: LifecycleEnrolled, BindingStatus: BindingBound, EverConnected: true}
	if got := DeriveOnboardingPhase(r, false, false); got != OnboardingPhaseCompleted {
		t.Fatalf("phase=%q", got)
	}
	r.EverConnected = false
	if got := DeriveOnboardingPhase(r, true, true); got == OnboardingPhaseCompleted {
		t.Fatal("samples alone invented trusted enrollment")
	}
}
