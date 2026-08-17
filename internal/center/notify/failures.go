package notify

import "errors"

// SendFailureClass is the content-free delivery outcome exposed to scoped
// notification adapters. It deliberately carries neither provider response
// details nor transport errors because those values may contain credentials or
// delivery content.
type SendFailureClass string

const (
	SendFailureTemporary SendFailureClass = "temporary"
	SendFailurePermanent SendFailureClass = "permanent"
	SendFailureUnknown   SendFailureClass = "unknown"
)

type sendFailure struct {
	class SendFailureClass
}

func (failure *sendFailure) Error() string {
	switch failure.class {
	case SendFailureTemporary:
		return "notification provider temporarily unavailable"
	case SendFailurePermanent:
		return "notification provider rejected request"
	default:
		return "notification provider outcome unknown"
	}
}

// NewSendFailure constructs a typed, content-free provider failure.
func NewSendFailure(class SendFailureClass) error {
	switch class {
	case SendFailureTemporary, SendFailurePermanent, SendFailureUnknown:
		return &sendFailure{class: class}
	default:
		return &sendFailure{class: SendFailureUnknown}
	}
}

// ClassifySendFailure returns the closed failure class when err was produced by
// a notifier that has deliberately discarded unsafe provider details.
func ClassifySendFailure(err error) (SendFailureClass, bool) {
	var failure *sendFailure
	if !errors.As(err, &failure) || failure == nil {
		return "", false
	}
	switch failure.class {
	case SendFailureTemporary, SendFailurePermanent, SendFailureUnknown:
		return failure.class, true
	default:
		return "", false
	}
}
