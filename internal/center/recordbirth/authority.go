package recordbirth

import "context"

// Observation contains database-authenticated original events and the current
// authority binding. An empty RecordID request observes availability only and
// never allocates an identity or appends an event.
type Observation struct {
	Binding      Binding
	Reserved     []byte
	Prepared     []byte
	Created      []byte
	HeadSequence uint64
	HeadHash     [32]byte
}

// Authority exposes only fixed W2 commands. Implementations have no arbitrary
// signing operation and never return private receipt keys.
type Authority interface {
	Reserve(context.Context, Binding, Request, []byte) ([]byte, error)
	Prepare(context.Context, Binding, string, [32]byte) ([]byte, error)
	Confirm(context.Context, Binding, string, [32]byte) ([]byte, error)
	Observe(context.Context, Binding, string) (Observation, error)
}
