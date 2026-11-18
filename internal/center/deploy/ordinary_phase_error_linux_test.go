//go:build linux

package deploy

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestOrdinaryUpgradePhasePreservesCauseAndRedactsDetails(t *testing.T) {
	cause := fmt.Errorf("private fixture details: %w", errOrdinaryUpgrade)
	err := ordinaryUpgradePhase("fence", cause)
	if !errors.Is(err, errOrdinaryUpgrade) || !errors.Is(err, cause) {
		t.Fatal("phase attribution lost its original cause")
	}
	want := "ordinary upgrade failed at fence: " + errOrdinaryUpgrade.Error()
	if err.Error() != want || strings.Contains(err.Error(), "private fixture details") {
		t.Fatalf("unexpected phase output: %q", err)
	}
	if got := ordinaryUpgradePhase("guard", err); got != err {
		t.Fatal("outer phase replaced the more precise failure")
	}
	outer := fmt.Errorf("another private detail: %w", err)
	wrapped := ordinaryUpgradePhase("guard", outer)
	if !errors.Is(wrapped, outer) || wrapped.Error() != want {
		t.Fatalf("nested phase did not retain cause or redact text: %q", wrapped)
	}
	foreign := errors.New("private downstream detail")
	foreignPhase := ordinaryUpgradePhase("fence", foreign)
	if !errors.Is(foreignPhase, foreign) || !errors.Is(foreignPhase, errOrdinaryUpgrade) || strings.Contains(foreignPhase.Error(), foreign.Error()) {
		t.Fatal("downstream cause was lost or exposed")
	}
	executable := ordinaryUpgradePhase("image-prepare", errOrdinaryControllerExecutable)
	if !errors.Is(executable, errOrdinaryControllerExecutable) || !strings.Contains(executable.Error(), "ordinary upgrade failed at image-prepare:") {
		t.Fatalf("distinct executable sentinel was lost: %v", executable)
	}
}
