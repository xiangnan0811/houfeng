//go:build linux

package deploy

// The participant keeps the underlying failure locally; only the fixed status
// code crosses the process boundary, never the error text or private inputs.
type ordinaryParticipantPhaseError struct {
	status ordinaryNativeStatus
	cause  error
}

func (e *ordinaryParticipantPhaseError) Error() string { return errOrdinaryUpgrade.Error() }
func (e *ordinaryParticipantPhaseError) Unwrap() error { return e.cause }
