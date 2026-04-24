package observations

import (
	"fmt"
	"strings"

	"houfeng/internal/center/targets"
	"houfeng/internal/contracts/agentapi"
)

var allowedProbeErrorCodes = map[string]struct{}{
	agentapi.ProbeErrorTimeout:      {},
	agentapi.ProbeErrorConnect:      {},
	agentapi.ProbeErrorHTTPStatus:   {},
	agentapi.ProbeErrorTLSHandshake: {},
}

func ValidateProbeObservation(observation ProbeObservationWrite) error {
	if !targets.IsValidProbeKind(observation.ProbeKind) {
		return fmt.Errorf("%w: unsupported probe_kind %q", ErrInvalidProbeObservation, observation.ProbeKind)
	}

	switch observation.ResultKind {
	case agentapi.ProbeResultSuccess:
		if observation.ErrorCode != "" || strings.TrimSpace(observation.ErrorSummary) != "" {
			return fmt.Errorf("%w: success observation must not carry failure fields", ErrInvalidProbeObservation)
		}
	case agentapi.ProbeResultFailure:
		if observation.ErrorCode == "" {
			return fmt.Errorf("%w: failure observation must carry error_code", ErrInvalidProbeObservation)
		}
		if strings.TrimSpace(observation.ErrorSummary) == "" {
			return fmt.Errorf("%w: failure observation must carry error_summary", ErrInvalidProbeObservation)
		}
	default:
		return fmt.Errorf("%w: unsupported result_kind %q", ErrInvalidProbeObservation, observation.ResultKind)
	}

	if observation.ErrorCode != "" {
		if _, ok := allowedProbeErrorCodes[observation.ErrorCode]; !ok {
			return fmt.Errorf("%w: unsupported error_code %q", ErrInvalidProbeObservation, observation.ErrorCode)
		}
	}

	if observation.ProbeKind != agentapi.ProbeKindHTTP && observation.HTTPStatus != nil {
		return fmt.Errorf("%w: probe_kind %q must not carry http_status", ErrInvalidProbeObservation, observation.ProbeKind)
	}
	if observation.ProbeKind != agentapi.ProbeKindTLS && observation.TLSExpiryDays != nil {
		return fmt.Errorf("%w: probe_kind %q must not carry tls_expiry_days", ErrInvalidProbeObservation, observation.ProbeKind)
	}

	return nil
}
