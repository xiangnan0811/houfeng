//go:build linux

package deploy

import (
	"context"
	"errors"
)

var errOrdinaryNamespaceReplay = errors.New("interrupted namespace provisioning must be completed by rerunning normal Compose start before a new cold upgrade")

// Phase labels are fixed by the controller. The cause remains available to
// errors.Is/As, but arbitrary nested error text may contain private inputs.
type ordinaryUpgradePhaseError struct {
	phase string
	cause error
}

func (e *ordinaryUpgradePhaseError) Error() string {
	detail := "operation rejected or uncertain"
	switch {
	case errors.Is(e.cause, errOrdinaryNamespaceReplay):
		detail = errOrdinaryNamespaceReplay.Error()
	case errors.Is(e.cause, errOrdinaryControllerExecutable):
		detail = errOrdinaryControllerExecutable.Error()
	case errors.Is(e.cause, context.Canceled):
		detail = context.Canceled.Error()
	case errors.Is(e.cause, context.DeadlineExceeded):
		detail = context.DeadlineExceeded.Error()
	case errors.Is(e.cause, errOrdinaryUpgrade):
		detail = errOrdinaryUpgrade.Error()
	}
	return "ordinary upgrade failed at " + e.phase + ": " + detail
}

func (e *ordinaryUpgradePhaseError) Unwrap() error { return e.cause }

func ordinaryUpgradePhase(phase string, err error) error {
	if err == nil {
		return nil
	}
	var attributed *ordinaryUpgradePhaseError
	if errors.As(err, &attributed) {
		if err == attributed {
			return err
		}
		return &ordinaryUpgradePhaseError{phase: attributed.phase, cause: err}
	}
	if !errors.Is(err, errOrdinaryUpgrade) && !errors.Is(err, errOrdinaryControllerExecutable) && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		err = errors.Join(errOrdinaryUpgrade, err)
	}
	return &ordinaryUpgradePhaseError{phase: phase, cause: err}
}
