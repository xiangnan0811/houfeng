package recordreadiness

import "context"

type presentRecovery struct {
	kind    CapabilityKind
	version uint32
}

func NewPresentRecovery(kind CapabilityKind, version uint32) RecoveryAdapter {
	return presentRecovery{kind: kind, version: version}
}

func (adapter presentRecovery) Kind() CapabilityKind { return adapter.kind }

func (adapter presentRecovery) Version() uint32 { return adapter.version }

func (adapter presentRecovery) Health(context.Context) error { return nil }
