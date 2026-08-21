package recordreadiness

import (
	"context"
	"crypto/sha256"
	"errors"
	"reflect"
	"strings"
	"testing"

	"houfeng/internal/center/recorddeletion"
)

func TestRequiredCapabilityKindsAreClosedOrderedAndDefensivelyCopied(t *testing.T) {
	t.Parallel()

	want := []CapabilityKind{
		CapabilityDeletionRecordCore,
		CapabilityDeletionRecordAttachments,
		CapabilityDeletionRecordEvidence,
		CapabilityDeletionRecordMarkdownClient,
		CapabilityDeletionRecordSearch,
		CapabilityDeletionRecordActivityProjection,
		CapabilityDeletionRecordComparison,
		CapabilityDeletionRecordCollaboration,
		CapabilityDeletionRecordPortability,
		CapabilityRecoveryRecordCore,
		CapabilityRecoveryRecordAttachments,
		CapabilityRecoveryRecordEvidence,
		CapabilityRecoveryRecordSearch,
		CapabilityRecoveryRecordActivityProjection,
		CapabilityRecoveryRecordCollaboration,
		CapabilityRecoveryRecordPortability,
		CapabilityAuthorityMembership,
		CapabilityAuthorityWitness,
		CapabilityBackupOrchestration,
		CapabilityRestoreReplay,
	}
	if !reflect.DeepEqual(want[:9], deletionCapabilityKinds()) {
		t.Fatalf("deletion kinds drifted from recorddeletion.RequiredAdapterNames: %#v", want[:9])
	}
	got := RequiredCapabilityKinds()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RequiredCapabilityKinds() = %#v, want %#v", got, want)
	}
	got[0] = "tampered"
	if fresh := RequiredCapabilityKinds(); !reflect.DeepEqual(fresh, want) {
		t.Fatalf("RequiredCapabilityKinds() after caller mutation = %#v", fresh)
	}
}

func TestNewRegistryRejectsNilTypedNilDuplicateUnknownAndIncompatible(t *testing.T) {
	t.Parallel()

	var typedDeletion *deletionAdapterStub
	var typedRecovery *recoveryAdapterStub
	var typedAuthority *authorityStub
	var typedOrchestration *orchestrationStub

	tests := []struct {
		name  string
		input func(*testing.T) RegistryInput
	}{
		{name: "nil deletion adapter", input: func(t *testing.T) RegistryInput {
			input := completeRegistryInput(t)
			input.DeletionAdapters[0] = nil
			return input
		}},
		{name: "typed nil deletion adapter", input: func(t *testing.T) RegistryInput {
			input := completeRegistryInput(t)
			input.DeletionAdapters[0] = typedDeletion
			return input
		}},
		{name: "duplicate deletion adapter", input: func(t *testing.T) RegistryInput {
			input := completeRegistryInput(t)
			input.DeletionAdapters = append(input.DeletionAdapters, input.DeletionAdapters[0])
			return input
		}},
		{name: "unknown deletion adapter", input: func(t *testing.T) RegistryInput {
			input := completeRegistryInput(t)
			input.DeletionAdapters = append(input.DeletionAdapters, &deletionAdapterStub{
				descriptor: recorddeletion.AdapterDescriptor{},
			})
			return input
		}},
		{name: "nil recovery adapter", input: func(t *testing.T) RegistryInput {
			input := completeRegistryInput(t)
			input.RecoveryAdapters[0] = nil
			return input
		}},
		{name: "typed nil recovery adapter", input: func(t *testing.T) RegistryInput {
			input := completeRegistryInput(t)
			input.RecoveryAdapters[0] = typedRecovery
			return input
		}},
		{name: "duplicate recovery adapter", input: func(t *testing.T) RegistryInput {
			input := completeRegistryInput(t)
			input.RecoveryAdapters = append(input.RecoveryAdapters, input.RecoveryAdapters[0])
			return input
		}},
		{name: "unknown recovery adapter", input: func(t *testing.T) RegistryInput {
			input := completeRegistryInput(t)
			input.RecoveryAdapters = append(input.RecoveryAdapters, &recoveryAdapterStub{kind: "recovery.record_unknown", version: CapabilityContractVersionV1})
			return input
		}},
		{name: "incompatible recovery version", input: func(t *testing.T) RegistryInput {
			input := completeRegistryInput(t)
			input.RecoveryAdapters[0] = &recoveryAdapterStub{kind: CapabilityRecoveryRecordCore, version: 2}
			return input
		}},
		{name: "nil membership", input: func(t *testing.T) RegistryInput {
			input := completeRegistryInput(t)
			input.Membership = nil
			return input
		}},
		{name: "typed nil membership", input: func(t *testing.T) RegistryInput {
			input := completeRegistryInput(t)
			input.Membership = typedAuthority
			return input
		}},
		{name: "nil witness", input: func(t *testing.T) RegistryInput {
			input := completeRegistryInput(t)
			input.Witness = nil
			return input
		}},
		{name: "typed nil witness", input: func(t *testing.T) RegistryInput {
			input := completeRegistryInput(t)
			input.Witness = typedAuthority
			return input
		}},
		{name: "nil backup", input: func(t *testing.T) RegistryInput {
			input := completeRegistryInput(t)
			input.Backup = nil
			return input
		}},
		{name: "typed nil restore", input: func(t *testing.T) RegistryInput {
			input := completeRegistryInput(t)
			input.Restore = typedOrchestration
			return input
		}},
		{name: "wrong backup kind", input: func(t *testing.T) RegistryInput {
			input := completeRegistryInput(t)
			input.Backup = &orchestrationStub{kind: CapabilityRestoreReplay, version: CapabilityContractVersionV1}
			return input
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := NewRegistry(tt.input(t))
			if !errors.Is(err, ErrInvalidCapabilityRegistry) {
				t.Fatalf("NewRegistry() error = %v, want ErrInvalidCapabilityRegistry", err)
			}
		})
	}
}

func TestRegistryEvaluateKeepsPermanentDeleteDisabledWhenIncompleteOrUnhealthy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   func(*testing.T) RegistryInput
		missing []CapabilityKind
	}{
		{
			name: "empty input",
			input: func(*testing.T) RegistryInput {
				return RegistryInput{}
			},
			missing: RequiredCapabilityKinds(),
		},
		{
			name: "deletion family only",
			input: func(t *testing.T) RegistryInput {
				return RegistryInput{DeletionAdapters: completeDeletionAdapters(t)}
			},
			missing: RequiredCapabilityKinds()[9:],
		},
		{
			name: "unhealthy recovery",
			input: func(t *testing.T) RegistryInput {
				input := completeRegistryInput(t)
				input.RecoveryAdapters[3] = &recoveryAdapterStub{
					kind:    CapabilityRecoveryRecordSearch,
					version: CapabilityContractVersionV1,
					err:     errors.New("search recovery store unavailable"),
				}
				return input
			},
		},
		{
			name: "unhealthy deletion",
			input: func(t *testing.T) RegistryInput {
				input := completeRegistryInput(t)
				core := input.DeletionAdapters[0].(*deletionAdapterStub)
				core.health = mustDeletionHealth(t, false, 9)
				return input
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registry, err := NewRegistry(tt.input(t))
			if err != nil {
				if tt.name == "unhealthy recovery" || tt.name == "unhealthy deletion" {
					t.Fatalf("NewRegistry() error = %v, unhealthy complete input must construct", err)
				}
				if !errors.Is(err, ErrInvalidCapabilityRegistry) {
					t.Fatalf("NewRegistry() error = %v, want success or ErrInvalidCapabilityRegistry", err)
				}
				return
			}
			writes := 0
			matrix, err := registry.Evaluate(context.Background())
			if err != nil && !errors.Is(err, ErrReadinessUnavailable) {
				t.Fatalf("Evaluate() error = %v", err)
			}
			if matrix.PermanentDelete() != PermanentDeleteDisabled {
				t.Fatalf("Evaluate() permanent delete = %q, want %q", matrix.PermanentDelete(), PermanentDeleteDisabled)
			}
			if writes != 0 {
				t.Fatalf("Evaluate() performed %d writes", writes)
			}
			if tt.missing != nil && !reflect.DeepEqual(matrix.Missing(), tt.missing) {
				t.Fatalf("Missing() = %#v, want %#v", matrix.Missing(), tt.missing)
			}
		})
	}
}

func TestRegistryEvaluateClosesProtectedDecisionOnAuthorityFailures(t *testing.T) {
	t.Parallel()

	reasons := []AuthorityReason{
		AuthorityNil,
		AuthorityTypedNil,
		AuthorityStale,
		AuthorityWrongDeployment,
		AuthorityDiscontinuous,
		AuthorityOutage,
	}
	for _, reason := range reasons {
		t.Run("membership "+string(reason), func(t *testing.T) {
			t.Parallel()
			assertAuthorityKeepsPermanentDeleteClosed(t, func(input *RegistryInput, stub *authorityStub) {
				input.Membership = stub
			}, reason)
		})
		t.Run("witness "+string(reason), func(t *testing.T) {
			t.Parallel()
			assertAuthorityKeepsPermanentDeleteClosed(t, func(input *RegistryInput, stub *authorityStub) {
				input.Witness = stub
			}, reason)
		})
	}
}

func TestRegistryEvaluateStatusMatrixIsContentSafe(t *testing.T) {
	t.Parallel()

	input := completeRegistryInput(t)
	input.RecoveryAdapters[0] = &recoveryAdapterStub{
		kind:    CapabilityRecoveryRecordCore,
		version: CapabilityContractVersionV1,
		err: errors.New(
			"leak markdown # title, comment body, evidence payload, attachment bytes, " +
				"archive content, password=secret, postgres://houfeng:secret@db/houfeng",
		),
	}
	registry, err := NewRegistry(input)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	matrix, err := registry.Evaluate(context.Background())
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if matrix.PermanentDelete() != PermanentDeleteDisabled {
		t.Fatalf("leaking unhealthy adapter enabled permanent delete")
	}
	encoded, err := matrix.Encode()
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	text := string(encoded)
	for _, leaked := range []string{
		"# title",
		"comment body",
		"evidence payload",
		"attachment bytes",
		"archive content",
		"password=secret",
		"postgres://",
		"DATABASE_URL",
		"houfeng:secret",
	} {
		if strings.Contains(text, leaked) {
			t.Fatalf("Encode() leaked %q: %s", leaked, text)
		}
	}
	if strings.Contains(text, `"note"`) {
		t.Fatalf("Encode() includes non-allowlisted note field: %s", text)
	}
	if !strings.Contains(text, `"permanent_delete":"disabled"`) {
		t.Fatalf("Encode() missing disabled decision: %s", text)
	}
}

func TestRegistryEvaluateEnablesPermanentDeleteOnlyForCompleteHealthyFixture(t *testing.T) {
	t.Parallel()

	registry, err := NewRegistry(completeRegistryInput(t))
	if err != nil {
		t.Fatalf("NewRegistry(complete) error = %v", err)
	}
	matrix, err := registry.Evaluate(context.Background())
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if matrix.PermanentDelete() != PermanentDeleteEnabled {
		t.Fatalf("complete healthy fixture permanent delete = %q, want %q", matrix.PermanentDelete(), PermanentDeleteEnabled)
	}
	if got := len(matrix.Rows()); got != len(RequiredCapabilityKinds()) {
		t.Fatalf("complete matrix rows = %d, want %d", got, len(RequiredCapabilityKinds()))
	}
	if missing := matrix.Missing(); len(missing) != 0 {
		t.Fatalf("complete matrix missing = %#v", missing)
	}
}

func assertAuthorityKeepsPermanentDeleteClosed(
	t *testing.T,
	assign func(*RegistryInput, *authorityStub),
	reason AuthorityReason,
) {
	t.Helper()

	input := completeRegistryInput(t)
	stub := &authorityStub{
		kind:   CapabilityAuthorityMembership,
		report: AuthorityReport{Healthy: false, Reason: reason},
	}
	if assign != nil && strings.Contains(t.Name(), "witness") {
		stub.kind = CapabilityAuthorityWitness
	}
	assign(&input, stub)
	registry, err := NewRegistry(input)
	if err != nil {
		if errors.Is(err, ErrInvalidCapabilityRegistry) && (reason == AuthorityNil || reason == AuthorityTypedNil) {
			return
		}
		t.Fatalf("NewRegistry() error = %v", err)
	}
	writes := 0
	matrix, err := registry.Evaluate(context.Background())
	if err != nil && !errors.Is(err, ErrReadinessUnavailable) {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if matrix.PermanentDelete() != PermanentDeleteDisabled {
		t.Fatalf("authority %s left permanent delete = %q", reason, matrix.PermanentDelete())
	}
	if writes != 0 {
		t.Fatalf("authority %s allowed %d writes", reason, writes)
	}
	if stub.probes == 0 && reason != AuthorityNil && reason != AuthorityTypedNil {
		t.Fatalf("Evaluate() did not probe live authority for %s", reason)
	}
}

func deletionCapabilityKinds() []CapabilityKind {
	kinds := make([]CapabilityKind, 0, len(recorddeletion.RequiredAdapterNames()))
	for _, name := range recorddeletion.RequiredAdapterNames() {
		kinds = append(kinds, DeletionCapabilityKind(name))
	}
	return kinds
}

func completeRegistryInput(t *testing.T) RegistryInput {
	t.Helper()
	return RegistryInput{
		DeletionAdapters: completeDeletionAdapters(t),
		RecoveryAdapters: completeRecoveryAdapters(),
		Membership:       &authorityStub{kind: CapabilityAuthorityMembership, report: AuthorityReport{Healthy: true, Reason: AuthorityOK}},
		Witness:          &authorityStub{kind: CapabilityAuthorityWitness, report: AuthorityReport{Healthy: true, Reason: AuthorityOK}},
		Backup:           &orchestrationStub{kind: CapabilityBackupOrchestration, version: CapabilityContractVersionV1},
		Restore:          &orchestrationStub{kind: CapabilityRestoreReplay, version: CapabilityContractVersionV1},
	}
}

func completeDeletionAdapters(t *testing.T) []recorddeletion.Adapter {
	t.Helper()
	adapters := make([]recorddeletion.Adapter, 0, len(recorddeletion.RequiredAdapterNames()))
	for index, name := range recorddeletion.RequiredAdapterNames() {
		adapters = append(adapters, newDeletionAdapterStub(t, name, byte(index+1)))
	}
	return adapters
}

func completeRecoveryAdapters() []RecoveryAdapter {
	kinds := []CapabilityKind{
		CapabilityRecoveryRecordCore,
		CapabilityRecoveryRecordAttachments,
		CapabilityRecoveryRecordEvidence,
		CapabilityRecoveryRecordSearch,
		CapabilityRecoveryRecordActivityProjection,
		CapabilityRecoveryRecordCollaboration,
		CapabilityRecoveryRecordPortability,
	}
	adapters := make([]RecoveryAdapter, 0, len(kinds))
	for _, kind := range kinds {
		adapters = append(adapters, &recoveryAdapterStub{kind: kind, version: CapabilityContractVersionV1})
	}
	return adapters
}

type deletionAdapterStub struct {
	descriptor recorddeletion.AdapterDescriptor
	health     recorddeletion.AdapterHealthSnapshot
	err        error
}

func (adapter *deletionAdapterStub) Descriptor() recorddeletion.AdapterDescriptor {
	if adapter == nil {
		return recorddeletion.AdapterDescriptor{}
	}
	return adapter.descriptor
}

func (adapter *deletionAdapterStub) HealthSnapshot(context.Context) (recorddeletion.AdapterHealthSnapshot, error) {
	if adapter == nil {
		return recorddeletion.AdapterHealthSnapshot{}, recorddeletion.ErrInvalidAdapterHealthSnapshot
	}
	return adapter.health, adapter.err
}

func newDeletionAdapterStub(t *testing.T, name recorddeletion.AdapterName, seed byte) *deletionAdapterStub {
	t.Helper()
	surfaces := []recorddeletion.SurfaceName{recorddeletion.SurfaceName("fixture." + strings.ReplaceAll(string(name), "_", "."))}
	switch name {
	case recorddeletion.AdapterNameRecordCore:
		surfaces = recorddeletion.RecordCoreSurfaceNames()
	case recorddeletion.AdapterNameRecordAttachments:
		surfaces = recorddeletion.RecordAttachmentsSurfaceNames()
	case recorddeletion.AdapterNameRecordEvidence:
		surfaces = recorddeletion.RecordEvidenceSurfaceNames()
	case recorddeletion.AdapterNameRecordCollaboration:
		surfaces = recorddeletion.RecordCollaborationSurfaceNames()
	case recorddeletion.AdapterNameRecordSearch:
		surfaces = recorddeletion.RecordSearchSurfaceNames()
	case recorddeletion.AdapterNameRecordActivityProjection:
		surfaces = recorddeletion.RecordActivitySurfaceNames()
	case recorddeletion.AdapterNameRecordPortability:
		surfaces = recorddeletion.RecordPortabilitySurfaceNames()
	}
	descriptor, err := recorddeletion.NewAdapterDescriptor(name, surfaces)
	if err != nil {
		t.Fatalf("NewAdapterDescriptor(%q) error = %v", name, err)
	}
	return &deletionAdapterStub{descriptor: descriptor, health: mustDeletionHealth(t, true, seed)}
}

func mustDeletionHealth(t *testing.T, healthy bool, seed byte) recorddeletion.AdapterHealthSnapshot {
	t.Helper()
	var proof [sha256.Size]byte
	proof[0] = seed
	proof[31] = seed + 1
	health, err := recorddeletion.NewAdapterHealthSnapshot(healthy, uint64(seed), proof)
	if err != nil {
		t.Fatalf("NewAdapterHealthSnapshot() error = %v", err)
	}
	return health
}

type recoveryAdapterStub struct {
	kind    CapabilityKind
	version uint32
	err     error
}

func (adapter *recoveryAdapterStub) Kind() CapabilityKind {
	if adapter == nil {
		return ""
	}
	return adapter.kind
}

func (adapter *recoveryAdapterStub) Version() uint32 {
	if adapter == nil {
		return 0
	}
	return adapter.version
}

func (adapter *recoveryAdapterStub) Health(context.Context) error {
	if adapter == nil {
		return ErrInvalidCapabilityRegistry
	}
	return adapter.err
}

type orchestrationStub struct {
	kind    CapabilityKind
	version uint32
	err     error
}

func (adapter *orchestrationStub) Kind() CapabilityKind {
	if adapter == nil {
		return ""
	}
	return adapter.kind
}

func (adapter *orchestrationStub) Version() uint32 {
	if adapter == nil {
		return 0
	}
	return adapter.version
}

func (adapter *orchestrationStub) Health(context.Context) error {
	if adapter == nil {
		return ErrInvalidCapabilityRegistry
	}
	return adapter.err
}

type authorityStub struct {
	kind   CapabilityKind
	report AuthorityReport
	err    error
	probes int
}

func (adapter *authorityStub) Kind() CapabilityKind {
	if adapter == nil {
		return ""
	}
	return adapter.kind
}

func (adapter *authorityStub) Probe(context.Context) (AuthorityReport, error) {
	if adapter == nil {
		return AuthorityReport{Reason: AuthorityTypedNil}, ErrReadinessUnavailable
	}
	adapter.probes++
	return adapter.report, adapter.err
}
