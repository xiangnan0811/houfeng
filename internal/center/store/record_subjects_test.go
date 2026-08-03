package store

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
	"houfeng/internal/center/targets"
	"houfeng/internal/center/vpsassets"
)

const (
	testStoreRecordUserID       = "usr_0123456789abcdef01234567"
	testStoreRecordGroupID      = "rag_records"
	testStoreRecordVPSID        = "vps_0123456789abcdef"
	testStoreRecordMonitoringID = "mi_0123456789abcdef"
	testStoreRecordTargetID     = "tg_0123456789abcdef"
)

func TestRecordSubjectAdaptersResolveAuthoritativeLiveSources(t *testing.T) {
	t.Parallel()

	actor := mustStoreRecordActor(t)
	tests := []struct {
		name       string
		adapter    records.SubjectSourceAdapter
		reference  records.SubjectReference
		wantRoute  string
		wantFields map[string]string
	}{
		{
			name: "vps",
			adapter: newVPSRecordSubjectAdapter(&fakeVPSRecordSubjectSource{record: vpsRecordSubject{
				VPSID:       testStoreRecordVPSID,
				DisplayName: " Tokyo Edge ",
				Provider:    " Hetzner ",
				Region:      " ap-northeast-1 ",
			}}),
			reference: records.SubjectReference{
				RegistryVersion: records.SubjectRegistryVersionV1,
				Kind:            records.SubjectKindVPS,
				Role:            records.RelationRoleAffected,
				SourceID:        testStoreRecordVPSID,
				Primary:         true,
			},
			wantRoute: "/vps/" + testStoreRecordVPSID,
			wantFields: map[string]string{
				"display_name": "Tokyo Edge",
				"provider":     "Hetzner",
				"region":       "ap-northeast-1",
			},
		},
		{
			name: "monitoring instance",
			adapter: newMonitoringInstanceRecordSubjectAdapter(&fakeMonitoringRecordSubjectSource{record: monitoringRecordSubject{
				MonitoringInstanceID: testStoreRecordMonitoringID,
				DisplayName:          " Tokyo Probe ",
				AgentVersion:         " v1.2.3 ",
			}}),
			reference: records.SubjectReference{
				RegistryVersion: records.SubjectRegistryVersionV1,
				Kind:            records.SubjectKindMonitoringInstance,
				Role:            records.RelationRoleContext,
				SourceID:        testStoreRecordMonitoringID,
				Primary:         true,
			},
			wantRoute: "/monitoring/" + testStoreRecordMonitoringID,
			wantFields: map[string]string{
				"display_name": "Tokyo Probe",
				"version":      "v1.2.3",
			},
		},
		{
			name: "target",
			adapter: newTargetRecordSubjectAdapter(&fakeTargetRecordSubjectSource{record: targetRecordSubject{
				TargetID:    testStoreRecordTargetID,
				DisplayName: " Payments API ",
				TargetType:  " service ",
			}}),
			reference: records.SubjectReference{
				RegistryVersion: records.SubjectRegistryVersionV1,
				Kind:            records.SubjectKindTarget,
				Role:            records.RelationRoleEvidenceSource,
				SourceID:        testStoreRecordTargetID,
				Primary:         true,
			},
			wantRoute: "/targets/" + testStoreRecordTargetID,
			wantFields: map[string]string{
				"display_name": "Payments API",
				"target_type":  "service",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registry, err := records.NewSubjectAdapterRegistry([]records.SubjectSourceAdapter{tt.adapter})
			if err != nil {
				t.Fatalf("NewSubjectAdapterRegistry() error = %v", err)
			}
			resolved, err := registry.Resolve(context.Background(), actor, tt.reference)
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			if resolved.ProjectID != recordauth.ProjectIDDefault || resolved.StableID != tt.reference.SourceID {
				t.Fatalf("Resolve() identity = %#v", resolved)
			}
			if resolved.LiveRoute != tt.wantRoute {
				t.Fatalf("LiveRoute = %q, want %q", resolved.LiveRoute, tt.wantRoute)
			}
			assertStoreRecordStringMap(t, resolved.IdentitySnapshot.Fields(), tt.wantFields)

			authorization := resolved.CaptureAuthorization
			if authorization.State != recordauth.SourceStateLive || authorization.CurrentScope == nil {
				t.Fatalf("CaptureAuthorization = %#v, want live", authorization)
			}
			for name, scope := range map[string]recordauth.VisibilityScope{
				"capture": authorization.CaptureScope,
				"current": *authorization.CurrentScope,
			} {
				if scope.ProjectID != recordauth.ProjectIDDefault || scope.Kind != recordauth.VisibilityKindProject ||
					scope.PolicyVersion != recordauth.PolicyVersionV1 || scope.PolicyRevision != recordSubjectSourcePolicyRevisionV1 {
					t.Fatalf("%s scope = %#v", name, scope)
				}
			}
		})
	}
}

func TestRecordSubjectAdaptersFailClosedOnMissingOrUnavailableSources(t *testing.T) {
	t.Parallel()

	actor := mustStoreRecordActor(t)
	dependencyErr := errors.New("repository unavailable")
	tests := []struct {
		name      string
		adapter   records.SubjectSourceAdapter
		reference records.SubjectReference
		wantErr   error
	}{
		{
			name:      "missing vps",
			adapter:   newVPSRecordSubjectAdapter(&fakeVPSRecordSubjectSource{err: vpsassets.ErrVPSAssetNotFound}),
			reference: storeRecordReference(records.SubjectKindVPS, testStoreRecordVPSID),
			wantErr:   ErrRecordSubjectNotFound,
		},
		{
			name:      "missing monitoring instance",
			adapter:   newMonitoringInstanceRecordSubjectAdapter(&fakeMonitoringRecordSubjectSource{err: monitoringinstances.ErrMonitoringInstanceNotFound}),
			reference: storeRecordReference(records.SubjectKindMonitoringInstance, testStoreRecordMonitoringID),
			wantErr:   ErrRecordSubjectNotFound,
		},
		{
			name:      "missing target",
			adapter:   newTargetRecordSubjectAdapter(&fakeTargetRecordSubjectSource{err: targets.ErrTargetNotFound}),
			reference: storeRecordReference(records.SubjectKindTarget, testStoreRecordTargetID),
			wantErr:   ErrRecordSubjectNotFound,
		},
		{
			name:      "repository failure",
			adapter:   newVPSRecordSubjectAdapter(&fakeVPSRecordSubjectSource{err: dependencyErr}),
			reference: storeRecordReference(records.SubjectKindVPS, testStoreRecordVPSID),
			wantErr:   ErrRecordSubjectUnavailable,
		},
		{
			name:      "nil production dependency",
			adapter:   NewVPSRecordSubjectAdapter(nil),
			reference: storeRecordReference(records.SubjectKindVPS, testStoreRecordVPSID),
			wantErr:   ErrRecordSubjectUnavailable,
		},
		{
			name:      "nil monitoring production dependency",
			adapter:   NewMonitoringInstanceRecordSubjectAdapter(nil),
			reference: storeRecordReference(records.SubjectKindMonitoringInstance, testStoreRecordMonitoringID),
			wantErr:   ErrRecordSubjectUnavailable,
		},
		{
			name:      "nil target production dependency",
			adapter:   NewTargetRecordSubjectAdapter(nil),
			reference: storeRecordReference(records.SubjectKindTarget, testStoreRecordTargetID),
			wantErr:   ErrRecordSubjectUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registry, err := records.NewSubjectAdapterRegistry([]records.SubjectSourceAdapter{tt.adapter})
			if err != nil {
				t.Fatalf("NewSubjectAdapterRegistry() error = %v", err)
			}
			if _, err := registry.Resolve(context.Background(), actor, tt.reference); !errors.Is(err, tt.wantErr) {
				t.Fatalf("Resolve() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestRecordSubjectAdaptersRejectMalformedReferencesBeforeRepositoryRead(t *testing.T) {
	t.Parallel()

	actor := mustStoreRecordActor(t)
	vpsSource := &fakeVPSRecordSubjectSource{record: vpsRecordSubject{
		VPSID:       testStoreRecordVPSID,
		DisplayName: "Tokyo Edge",
	}}
	monitoringSource := &fakeMonitoringRecordSubjectSource{record: monitoringRecordSubject{
		MonitoringInstanceID: testStoreRecordMonitoringID,
		DisplayName:          "Tokyo Probe",
	}}
	targetSource := &fakeTargetRecordSubjectSource{record: targetRecordSubject{
		TargetID:    testStoreRecordTargetID,
		DisplayName: "Payments API",
		TargetType:  targets.TargetTypeService,
	}}
	tests := []struct {
		name      string
		adapter   records.SubjectSourceAdapter
		reference records.SubjectReference
		calls     func() int
	}{
		{
			name:    "unknown registry version",
			adapter: newVPSRecordSubjectAdapter(vpsSource),
			reference: func() records.SubjectReference {
				reference := storeRecordReference(records.SubjectKindVPS, testStoreRecordVPSID)
				reference.RegistryVersion++
				return reference
			}(),
			calls: func() int { return vpsSource.calls },
		},
		{
			name:    "unknown relation role",
			adapter: newMonitoringInstanceRecordSubjectAdapter(monitoringSource),
			reference: func() records.SubjectReference {
				reference := storeRecordReference(records.SubjectKindMonitoringInstance, testStoreRecordMonitoringID)
				reference.Role = "unknown"
				return reference
			}(),
			calls: func() int { return monitoringSource.calls },
		},
		{
			name:      "malformed source identifier",
			adapter:   newTargetRecordSubjectAdapter(targetSource),
			reference: storeRecordReference(records.SubjectKindTarget, "target-from-client"),
			calls:     func() int { return targetSource.calls },
		},
		{
			name:      "known kind routed to wrong adapter",
			adapter:   newVPSRecordSubjectAdapter(vpsSource),
			reference: storeRecordReference(records.SubjectKindTarget, testStoreRecordTargetID),
			calls:     func() int { return vpsSource.calls },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.adapter.Resolve(context.Background(), actor, tt.reference); !errors.Is(err, records.ErrInvalidSubjectReference) {
				t.Fatalf("Resolve() error = %v, want ErrInvalidSubjectReference", err)
			}
			if got := tt.calls(); got != 0 {
				t.Fatalf("repository calls = %d, want 0", got)
			}
		})
	}
}

func TestRecordSubjectAdaptersRejectInvalidActorBeforeRepositoryRead(t *testing.T) {
	t.Parallel()

	source := &fakeVPSRecordSubjectSource{record: vpsRecordSubject{
		VPSID:       testStoreRecordVPSID,
		DisplayName: "Tokyo Edge",
	}}
	actor := mustStoreRecordActor(t)
	actor.ProjectID = "other"

	if _, err := newVPSRecordSubjectAdapter(source).Resolve(
		context.Background(),
		actor,
		storeRecordReference(records.SubjectKindVPS, testStoreRecordVPSID),
	); !errors.Is(err, ErrRecordSubjectUnavailable) {
		t.Fatalf("Resolve() error = %v, want ErrRecordSubjectUnavailable", err)
	}
	if source.calls != 0 {
		t.Fatalf("repository calls = %d, want 0", source.calls)
	}
}

func TestPostgresRecordSubjectSourcesUseNarrowServerOwnedReads(t *testing.T) {
	t.Parallel()

	t.Run("vps", func(t *testing.T) {
		t.Parallel()

		var query string
		repo := &PostgresVPSAssetRepository{db: fakeVPSAssetDB{queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			query = sql
			if len(args) != 1 || args[0] != testStoreRecordVPSID {
				t.Fatalf("QueryRow args = %#v", args)
			}
			return fakeVPSAssetRow{scan: func(dest ...any) error {
				if len(dest) != 4 {
					t.Fatalf("Scan destinations = %d, want 4", len(dest))
				}
				*(dest[0].(*string)) = testStoreRecordVPSID
				*(dest[1].(*string)) = "Tokyo Edge"
				*(dest[2].(*string)) = "Hetzner"
				*(dest[3].(*string)) = "ap-northeast-1"
				return nil
			}}
		}}}

		got, err := repo.loadVPSRecordSubject(context.Background(), testStoreRecordVPSID)
		if err != nil || got.VPSID != testStoreRecordVPSID {
			t.Fatalf("loadVPSRecordSubject() = %#v, %v", got, err)
		}
		assertNarrowRecordSubjectQuery(t, query,
			[]string{"vps_id", "display_name", "provider_name", "region"},
			[]string{"ssh_host", "ssh_user", "ipv4", "ipv6", "note", "labels"},
		)
	})

	t.Run("monitoring instance", func(t *testing.T) {
		t.Parallel()

		var query string
		repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			query = sql
			if len(args) != 1 || args[0] != testStoreRecordMonitoringID {
				t.Fatalf("QueryRow args = %#v", args)
			}
			return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
				if len(dest) != 3 {
					t.Fatalf("Scan destinations = %d, want 3", len(dest))
				}
				*(dest[0].(*string)) = testStoreRecordMonitoringID
				*(dest[1].(*string)) = "Tokyo Probe"
				*(dest[2].(*string)) = "v1.2.3"
				return nil
			}}
		}}}

		got, err := repo.loadMonitoringRecordSubject(context.Background(), testStoreRecordMonitoringID)
		if err != nil || got.MonitoringInstanceID != testStoreRecordMonitoringID || got.AgentVersion != "v1.2.3" {
			t.Fatalf("loadMonitoringRecordSubject() = %#v, %v", got, err)
		}
		assertNarrowRecordSubjectQuery(t, query,
			[]string{"monitoring_instance_id", "display_name", "agent_version", "monitoring_instance_heartbeats"},
			[]string{"enrollment_token_hash", "sync_token_hash", "binding_fingerprint", "note", "labels"},
		)
	})

	t.Run("target", func(t *testing.T) {
		t.Parallel()

		var query string
		repo := &PostgresTargetRepository{db: fakeTargetDB{queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			query = sql
			if len(args) != 1 || args[0] != testStoreRecordTargetID {
				t.Fatalf("QueryRow args = %#v", args)
			}
			return fakeTargetRow{scan: func(dest ...any) error {
				if len(dest) != 3 {
					t.Fatalf("Scan destinations = %d, want 3", len(dest))
				}
				*(dest[0].(*string)) = testStoreRecordTargetID
				*(dest[1].(*string)) = "Payments API"
				*(dest[2].(*string)) = targets.TargetTypeService
				return nil
			}}
		}}}

		got, err := repo.loadTargetRecordSubject(context.Background(), testStoreRecordTargetID)
		if err != nil || got.TargetID != testStoreRecordTargetID {
			t.Fatalf("loadTargetRecordSubject() = %#v, %v", got, err)
		}
		assertNarrowRecordSubjectQuery(t, query,
			[]string{"target_id", "name", "target_type"},
			[]string{"host", "base_port", "note", "labels", "execution_monitoring_instance_labels"},
		)
	})
}

func TestPostgresRecordSubjectSourcesMapMissingAndScanFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		load     func(error) error
		notFound error
	}{
		{
			name: "vps",
			load: func(scanErr error) error {
				repository := &PostgresVPSAssetRepository{db: fakeVPSAssetDB{queryRow: func(context.Context, string, ...any) pgx.Row {
					return fakeVPSAssetRow{scan: func(...any) error { return scanErr }}
				}}}
				_, err := repository.loadVPSRecordSubject(context.Background(), testStoreRecordVPSID)
				return err
			},
			notFound: vpsassets.ErrVPSAssetNotFound,
		},
		{
			name: "monitoring instance",
			load: func(scanErr error) error {
				repository := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{queryRow: func(context.Context, string, ...any) pgx.Row {
					return fakeMonitoringInstanceRow{scan: func(...any) error { return scanErr }}
				}}}
				_, err := repository.loadMonitoringRecordSubject(context.Background(), testStoreRecordMonitoringID)
				return err
			},
			notFound: monitoringinstances.ErrMonitoringInstanceNotFound,
		},
		{
			name: "target",
			load: func(scanErr error) error {
				repository := &PostgresTargetRepository{db: fakeTargetDB{queryRow: func(context.Context, string, ...any) pgx.Row {
					return fakeTargetRow{scan: func(...any) error { return scanErr }}
				}}}
				_, err := repository.loadTargetRecordSubject(context.Background(), testStoreRecordTargetID)
				return err
			},
			notFound: targets.ErrTargetNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if err := tt.load(pgx.ErrNoRows); !errors.Is(err, tt.notFound) {
				t.Fatalf("missing source error = %v, want %v", err, tt.notFound)
			}
			scanErr := errors.New("scan failed")
			if err := tt.load(scanErr); !errors.Is(err, scanErr) || errors.Is(err, tt.notFound) {
				t.Fatalf("scan failure error = %v, want wrapped scan error without not-found", err)
			}
		})
	}
}

func TestRecordSubjectReadResolverPreservesCaptureAndRefreshesLiveState(t *testing.T) {
	t.Parallel()

	actor := mustStoreRecordActor(t)
	current := &fakeVPSRecordSubjectSource{record: vpsRecordSubject{
		VPSID:       testStoreRecordVPSID,
		DisplayName: "Renamed VPS",
		Provider:    "Current Provider",
		Region:      "current-region",
	}}
	registry, err := records.NewSubjectAdapterRegistry([]records.SubjectSourceAdapter{newVPSRecordSubjectAdapter(current)})
	if err != nil {
		t.Fatalf("NewSubjectAdapterRegistry() error = %v", err)
	}
	tombstones := &fakeWitnessedRecordSubjectTombstoneSource{}
	resolver := NewRecordSubjectReadResolver(registry, tombstones)
	input := RecordSubjectReadInput{
		Reference:            storeRecordReference(records.SubjectKindVPS, testStoreRecordVPSID),
		IdentitySnapshot:     mustStoreRecordSnapshot(t, records.SubjectKindVPS, map[string]string{"display_name": "Captured VPS", "provider": "Captured Provider"}),
		CaptureAuthorization: mustStoreLiveSourceAuthorization(t, recordauth.SourceKindVPS, testStoreRecordVPSID, mustStoreProjectVisibility(t)),
	}

	resolved, err := resolver.Resolve(context.Background(), actor, input)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got := resolved.IdentitySnapshot.Fields()["display_name"]; got != "Captured VPS" {
		t.Fatalf("display_name = %q, want immutable captured value", got)
	}
	if resolved.LiveRoute != "/vps/"+testStoreRecordVPSID {
		t.Fatalf("LiveRoute = %q", resolved.LiveRoute)
	}
	if resolved.CaptureAuthorization.CaptureScope.CanonicalHash != input.CaptureAuthorization.CaptureScope.CanonicalHash {
		t.Fatal("Resolve() replaced immutable capture scope")
	}
	if resolved.CaptureAuthorization.State != recordauth.SourceStateLive || resolved.CaptureAuthorization.CurrentScope == nil {
		t.Fatalf("CaptureAuthorization = %#v, want live", resolved.CaptureAuthorization)
	}
	if tombstones.calls != 0 {
		t.Fatalf("tombstone source calls = %d, want 0 while source is live", tombstones.calls)
	}
}

func TestRecordSubjectReadResolverUsesOnlyWitnessedTombstoneAfterDeletion(t *testing.T) {
	t.Parallel()

	actor := mustStoreRecordActor(t)
	visibility := mustStoreRestrictedVisibility(t, []recordauth.Role{recordauth.RoleProjectAdmin}, []string{testStoreRecordGroupID}, 3)
	registry, err := records.NewSubjectAdapterRegistry([]records.SubjectSourceAdapter{
		newVPSRecordSubjectAdapter(&fakeVPSRecordSubjectSource{err: vpsassets.ErrVPSAssetNotFound}),
	})
	if err != nil {
		t.Fatalf("NewSubjectAdapterRegistry() error = %v", err)
	}
	tombstones := &fakeWitnessedRecordSubjectTombstoneSource{evidence: WitnessedRecordSubjectTombstone{
		Version:                  WitnessedRecordSubjectTombstoneVersionV1,
		ProjectID:                recordauth.ProjectIDDefault,
		Kind:                     recordauth.SourceKindVPS,
		SourceID:                 testStoreRecordVPSID,
		AuthorizationFloor:       visibility,
		LastLiveScope:            visibility,
		AuthorizationFloorDigest: visibility.CanonicalHash,
	}}
	resolver := NewRecordSubjectReadResolver(registry, tombstones)
	input := RecordSubjectReadInput{
		Reference:            storeRecordReference(records.SubjectKindVPS, testStoreRecordVPSID),
		IdentitySnapshot:     mustStoreRecordSnapshot(t, records.SubjectKindVPS, map[string]string{"display_name": "Deleted VPS"}),
		CaptureAuthorization: mustStoreLiveSourceAuthorization(t, recordauth.SourceKindVPS, testStoreRecordVPSID, visibility),
	}

	resolved, err := resolver.Resolve(context.Background(), actor, input)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.LiveRoute != "" || resolved.CaptureAuthorization.State != recordauth.SourceStateTombstoned {
		t.Fatalf("Resolve() = %#v, want tombstone without route", resolved)
	}
	if resolved.CaptureAuthorization.CurrentScope != nil || resolved.CaptureAuthorization.FinalFloor == nil ||
		resolved.CaptureAuthorization.LastLiveScope == nil {
		t.Fatalf("CaptureAuthorization = %#v, want complete tombstone union", resolved.CaptureAuthorization)
	}
	if got := resolved.IdentitySnapshot.Fields()["display_name"]; got != "Deleted VPS" {
		t.Fatalf("display_name = %q, want historical snapshot", got)
	}
	if tombstones.calls != 1 || tombstones.projectID != recordauth.ProjectIDDefault ||
		tombstones.kind != recordauth.SourceKindVPS || tombstones.sourceID != testStoreRecordVPSID {
		t.Fatalf("tombstone call = %#v", tombstones)
	}
}

func TestRecordSubjectReadResolverRejectsMalformedStoredEvidenceBeforeExternalReads(t *testing.T) {
	t.Parallel()

	actor := mustStoreRecordActor(t)
	visibility := mustStoreProjectVisibility(t)
	base := RecordSubjectReadInput{
		Reference:            storeRecordReference(records.SubjectKindVPS, testStoreRecordVPSID),
		IdentitySnapshot:     mustStoreRecordSnapshot(t, records.SubjectKindVPS, map[string]string{"display_name": "Captured VPS"}),
		CaptureAuthorization: mustStoreLiveSourceAuthorization(t, recordauth.SourceKindVPS, testStoreRecordVPSID, visibility),
	}
	targetSnapshot := mustStoreRecordSnapshot(t, records.SubjectKindTarget, map[string]string{
		"display_name": "Target",
		"target_type":  targets.TargetTypeService,
	})
	targetAuthorization := mustStoreLiveSourceAuthorization(
		t,
		recordauth.SourceKindTarget,
		testStoreRecordTargetID,
		visibility,
	)
	tests := []struct {
		name   string
		mutate func(*RecordSubjectReadInput)
	}{
		{
			name: "unknown reference registry version",
			mutate: func(input *RecordSubjectReadInput) {
				input.Reference.RegistryVersion++
			},
		},
		{
			name: "unknown reference role",
			mutate: func(input *RecordSubjectReadInput) {
				input.Reference.Role = "unknown"
			},
		},
		{
			name: "snapshot kind mismatch",
			mutate: func(input *RecordSubjectReadInput) {
				input.IdentitySnapshot = targetSnapshot
			},
		},
		{
			name: "capture source mismatch",
			mutate: func(input *RecordSubjectReadInput) {
				input.CaptureAuthorization = targetAuthorization
			},
		},
		{
			name: "capture digest mismatch",
			mutate: func(input *RecordSubjectReadInput) {
				input.CaptureAuthorization.Digest[0] ^= 0xff
			},
		},
		{
			name: "non-canonical capture scope",
			mutate: func(input *RecordSubjectReadInput) {
				input.CaptureAuthorization.CaptureScope.AllowedGroupIDs = []string{testStoreRecordGroupID}
			},
		},
		{
			name: "non-canonical current scope",
			mutate: func(input *RecordSubjectReadInput) {
				current := *input.CaptureAuthorization.CurrentScope
				current.PolicyRevision = 0
				input.CaptureAuthorization.CurrentScope = &current
			},
		},
		{
			name: "mixed live union",
			mutate: func(input *RecordSubjectReadInput) {
				floor := visibility
				input.CaptureAuthorization.FinalFloor = &floor
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			source := &fakeVPSRecordSubjectSource{record: vpsRecordSubject{
				VPSID:       testStoreRecordVPSID,
				DisplayName: "Current VPS",
			}}
			registry, err := records.NewSubjectAdapterRegistry([]records.SubjectSourceAdapter{newVPSRecordSubjectAdapter(source)})
			if err != nil {
				t.Fatalf("NewSubjectAdapterRegistry() error = %v", err)
			}
			tombstones := &fakeWitnessedRecordSubjectTombstoneSource{}
			input := base
			tt.mutate(&input)

			if _, err := NewRecordSubjectReadResolver(registry, tombstones).Resolve(context.Background(), actor, input); !errors.Is(err, ErrRecordSubjectUnavailable) {
				t.Fatalf("Resolve() error = %v, want ErrRecordSubjectUnavailable", err)
			}
			if source.calls != 0 || tombstones.calls != 0 {
				t.Fatalf("external reads = live:%d tombstone:%d, want zero", source.calls, tombstones.calls)
			}
		})
	}
}

func TestRecordSubjectReadResolverRejectsUnwitnessedInvalidOrWideningDeletionEvidence(t *testing.T) {
	t.Parallel()

	actor := mustStoreRecordActor(t)
	capture := mustStoreRestrictedVisibility(t, []recordauth.Role{recordauth.RoleProjectAdmin}, []string{testStoreRecordGroupID}, 3)
	project := mustStoreProjectVisibility(t)
	valid := WitnessedRecordSubjectTombstone{
		Version:                  WitnessedRecordSubjectTombstoneVersionV1,
		ProjectID:                recordauth.ProjectIDDefault,
		Kind:                     recordauth.SourceKindVPS,
		SourceID:                 testStoreRecordVPSID,
		AuthorizationFloor:       capture,
		LastLiveScope:            capture,
		AuthorizationFloorDigest: capture.CanonicalHash,
	}
	input := RecordSubjectReadInput{
		Reference:            storeRecordReference(records.SubjectKindVPS, testStoreRecordVPSID),
		IdentitySnapshot:     mustStoreRecordSnapshot(t, records.SubjectKindVPS, map[string]string{"display_name": "Deleted VPS"}),
		CaptureAuthorization: mustStoreLiveSourceAuthorization(t, recordauth.SourceKindVPS, testStoreRecordVPSID, capture),
	}
	deletedRegistry := func(t *testing.T, err error) records.SubjectAdapterRegistry {
		t.Helper()
		registry, registryErr := records.NewSubjectAdapterRegistry([]records.SubjectSourceAdapter{
			newVPSRecordSubjectAdapter(&fakeVPSRecordSubjectSource{err: err}),
		})
		if registryErr != nil {
			t.Fatalf("NewSubjectAdapterRegistry() error = %v", registryErr)
		}
		return registry
	}

	tests := []struct {
		name       string
		liveErr    error
		tombstones WitnessedRecordSubjectTombstoneSource
		wantCalls  int
	}{
		{name: "missing witness provider", liveErr: vpsassets.ErrVPSAssetNotFound},
		{name: "missing witness", liveErr: vpsassets.ErrVPSAssetNotFound, tombstones: &fakeWitnessedRecordSubjectTombstoneSource{err: ErrWitnessedRecordSubjectTombstoneNotFound}, wantCalls: 1},
		{name: "witness backend unavailable", liveErr: vpsassets.ErrVPSAssetNotFound, tombstones: &fakeWitnessedRecordSubjectTombstoneSource{err: errors.New("witness unavailable")}, wantCalls: 1},
		{name: "unknown witness version", liveErr: vpsassets.ErrVPSAssetNotFound, tombstones: &fakeWitnessedRecordSubjectTombstoneSource{evidence: mutateStoreTombstone(valid, func(value *WitnessedRecordSubjectTombstone) { value.Version = 2 })}, wantCalls: 1},
		{name: "cross project witness", liveErr: vpsassets.ErrVPSAssetNotFound, tombstones: &fakeWitnessedRecordSubjectTombstoneSource{evidence: mutateStoreTombstone(valid, func(value *WitnessedRecordSubjectTombstone) { value.ProjectID = "other" })}, wantCalls: 1},
		{name: "wrong source kind", liveErr: vpsassets.ErrVPSAssetNotFound, tombstones: &fakeWitnessedRecordSubjectTombstoneSource{evidence: mutateStoreTombstone(valid, func(value *WitnessedRecordSubjectTombstone) { value.Kind = recordauth.SourceKindTarget })}, wantCalls: 1},
		{name: "wrong source id", liveErr: vpsassets.ErrVPSAssetNotFound, tombstones: &fakeWitnessedRecordSubjectTombstoneSource{evidence: mutateStoreTombstone(valid, func(value *WitnessedRecordSubjectTombstone) { value.SourceID = testStoreRecordTargetID })}, wantCalls: 1},
		{name: "floor digest mismatch", liveErr: vpsassets.ErrVPSAssetNotFound, tombstones: &fakeWitnessedRecordSubjectTombstoneSource{evidence: mutateStoreTombstone(valid, func(value *WitnessedRecordSubjectTombstone) { value.AuthorizationFloorDigest[0] ^= 0xff })}, wantCalls: 1},
		{name: "non-canonical floor", liveErr: vpsassets.ErrVPSAssetNotFound, tombstones: &fakeWitnessedRecordSubjectTombstoneSource{evidence: mutateStoreTombstone(valid, func(value *WitnessedRecordSubjectTombstone) {
			groups := append([]string(nil), value.AuthorizationFloor.AllowedGroupIDs...)
			value.AuthorizationFloor.AllowedGroupIDs = append(groups, testStoreRecordGroupID)
		})}, wantCalls: 1},
		{name: "tampered last live hash", liveErr: vpsassets.ErrVPSAssetNotFound, tombstones: &fakeWitnessedRecordSubjectTombstoneSource{evidence: mutateStoreTombstone(valid, func(value *WitnessedRecordSubjectTombstone) {
			value.LastLiveScope.CanonicalHash[0] ^= 0xff
		})}, wantCalls: 1},
		{name: "final floor wider than last live", liveErr: vpsassets.ErrVPSAssetNotFound, tombstones: &fakeWitnessedRecordSubjectTombstoneSource{evidence: mutateStoreTombstone(valid, func(value *WitnessedRecordSubjectTombstone) {
			value.AuthorizationFloor = project
			value.AuthorizationFloorDigest = project.CanonicalHash
		})}, wantCalls: 1},
		{name: "last live wider than capture", liveErr: vpsassets.ErrVPSAssetNotFound, tombstones: &fakeWitnessedRecordSubjectTombstoneSource{evidence: mutateStoreTombstone(valid, func(value *WitnessedRecordSubjectTombstone) { value.LastLiveScope = project })}, wantCalls: 1},
		{name: "live repository error is not deletion", liveErr: errors.New("query failed"), tombstones: &fakeWitnessedRecordSubjectTombstoneSource{evidence: valid}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			resolver := NewRecordSubjectReadResolver(deletedRegistry(t, tt.liveErr), tt.tombstones)
			if _, err := resolver.Resolve(context.Background(), actor, input); !errors.Is(err, ErrRecordSubjectUnavailable) {
				t.Fatalf("Resolve() error = %v, want ErrRecordSubjectUnavailable", err)
			}
			if fake, ok := tt.tombstones.(*fakeWitnessedRecordSubjectTombstoneSource); ok && fake.calls != tt.wantCalls {
				t.Fatalf("tombstone calls = %d, want %d", fake.calls, tt.wantCalls)
			}
		})
	}
}

func TestRecordSubjectReadResolverRejectsLiveWidening(t *testing.T) {
	t.Parallel()

	actor := mustStoreRecordActor(t)
	capture := mustStoreRestrictedVisibility(t, []recordauth.Role{recordauth.RoleProjectAdmin}, nil, 3)
	registry, err := records.NewSubjectAdapterRegistry([]records.SubjectSourceAdapter{
		newVPSRecordSubjectAdapter(&fakeVPSRecordSubjectSource{record: vpsRecordSubject{
			VPSID:       testStoreRecordVPSID,
			DisplayName: "Current VPS",
		}}),
	})
	if err != nil {
		t.Fatalf("NewSubjectAdapterRegistry() error = %v", err)
	}
	input := RecordSubjectReadInput{
		Reference:            storeRecordReference(records.SubjectKindVPS, testStoreRecordVPSID),
		IdentitySnapshot:     mustStoreRecordSnapshot(t, records.SubjectKindVPS, map[string]string{"display_name": "Captured VPS"}),
		CaptureAuthorization: mustStoreLiveSourceAuthorization(t, recordauth.SourceKindVPS, testStoreRecordVPSID, capture),
	}

	if _, err := NewRecordSubjectReadResolver(registry, nil).Resolve(context.Background(), actor, input); !errors.Is(err, ErrRecordSubjectUnavailable) {
		t.Fatalf("Resolve() error = %v, want ErrRecordSubjectUnavailable", err)
	}
}

func TestPostgresCurrentRecordAuthorizationSourceRefreshesEveryStoredSubject(t *testing.T) {
	t.Parallel()

	actor := mustStoreRecordActor(t)
	visibility := mustStoreProjectVisibility(t)
	capture := mustStoreLiveSourceAuthorization(t, recordauth.SourceKindVPS, testStoreRecordVPSID, visibility)
	targetCapture := mustStoreLiveSourceAuthorization(t, recordauth.SourceKindTarget, testStoreRecordTargetID, visibility)
	currentVisibility := mustStoreRestrictedVisibility(t, []recordauth.Role{recordauth.RoleProjectAdmin}, nil, 2)
	currentAuthorization, err := recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{
		Version:      recordauth.SourceAuthorizationVersionV1,
		Kind:         recordauth.SourceKindVPS,
		SourceID:     testStoreRecordVPSID,
		State:        recordauth.SourceStateLive,
		CaptureScope: visibility,
		CurrentScope: &currentVisibility,
	})
	if err != nil {
		t.Fatalf("NormalizeSourceAuthorization() error = %v", err)
	}
	targetCurrentAuthorization := targetCapture
	steps := make([]string, 0, 3)
	resolver := &fakeCurrentRecordSubjectResolver{
		steps: &steps,
		resolve: func(_ recordauth.ActorScope, input RecordSubjectReadInput, _ int) (records.ResolvedSubject, error) {
			switch input.Reference.SourceID {
			case testStoreRecordVPSID:
				return records.ResolvedSubject{
					ProjectID:            recordauth.ProjectIDDefault,
					StableID:             testStoreRecordVPSID,
					IdentitySnapshot:     input.IdentitySnapshot,
					LiveRoute:            "/vps/" + testStoreRecordVPSID,
					CaptureAuthorization: currentAuthorization,
				}, nil
			case testStoreRecordTargetID:
				return records.ResolvedSubject{
					ProjectID:            recordauth.ProjectIDDefault,
					StableID:             testStoreRecordTargetID,
					IdentitySnapshot:     input.IdentitySnapshot,
					LiveRoute:            "/targets/" + testStoreRecordTargetID,
					CaptureAuthorization: targetCurrentAuthorization,
				}, nil
			default:
				return records.ResolvedSubject{}, ErrRecordSubjectUnavailable
			}
		},
	}
	source := &PostgresCurrentRecordAuthorizationSource{
		load: func(_ context.Context, recordID string) (currentRecordAuthorizationSnapshot, error) {
			steps = append(steps, "load")
			if recordID != "rec_current1" {
				t.Fatalf("recordID = %q", recordID)
			}
			return currentRecordAuthorizationSnapshot{
				recordID:           recordID,
				projectID:          recordauth.ProjectIDDefault,
				lifecycle:          records.LifecycleActive,
				currentRevisionID:  "rrv_current00000004",
				lockVersion:        7,
				authorizationEpoch: 5,
				visibility:         visibility,
				subjects: []RecordSubjectReadInput{
					{
						Reference:            storeRecordReference(records.SubjectKindVPS, testStoreRecordVPSID),
						IdentitySnapshot:     mustStoreRecordSnapshot(t, records.SubjectKindVPS, map[string]string{"display_name": "Captured VPS"}),
						CaptureAuthorization: capture,
					},
					{
						Reference: func() records.SubjectReference {
							reference := storeRecordReference(records.SubjectKindTarget, testStoreRecordTargetID)
							reference.Primary = false
							return reference
						}(),
						IdentitySnapshot:     mustStoreRecordSnapshot(t, records.SubjectKindTarget, map[string]string{"display_name": "Captured Target", "target_type": "service"}),
						CaptureAuthorization: targetCapture,
					},
				},
			}, nil
		},
		resolver: resolver,
	}

	current, err := source.ResolveCurrentRecordAuthorization(context.Background(), actor, "rec_current1")
	if err != nil {
		t.Fatalf("ResolveCurrentRecordAuthorization() error = %v", err)
	}
	if !reflect.DeepEqual(steps, []string{"load", "resolve", "resolve"}) {
		t.Fatalf("steps = %#v, want load then every current resolution", steps)
	}
	if current.RecordID != "rec_current1" || current.CurrentRevisionID != "rrv_current00000004" ||
		current.LockVersion != 7 || current.AuthorizationEpoch != 5 || current.Lifecycle != records.LifecycleActive {
		t.Fatalf("current identity = %#v", current)
	}
	if current.Evidence.ProjectID != recordauth.ProjectIDDefault ||
		current.Evidence.Visibility.CanonicalHash != visibility.CanonicalHash || len(current.Evidence.Sources) != 2 ||
		current.Evidence.Sources[0].Digest != currentAuthorization.Digest ||
		current.Evidence.Sources[1].Digest != targetCurrentAuthorization.Digest {
		t.Fatalf("current evidence = %#v, want refreshed authorization", current.Evidence)
	}
	if resolver.actor.CanonicalHash() != actor.CanonicalHash() || len(resolver.inputs) != 2 ||
		resolver.inputs[0].Reference.SourceID != testStoreRecordVPSID ||
		resolver.inputs[1].Reference.SourceID != testStoreRecordTargetID {
		t.Fatalf("resolver inputs = (%#v, %#v)", resolver.actor, resolver.inputs)
	}
}

func TestPostgresCurrentRecordAuthorizationSourceFailsClosedBeforeReturningPartialEvidence(t *testing.T) {
	t.Parallel()

	actor := mustStoreRecordActor(t)
	visibility := mustStoreProjectVisibility(t)
	capture := mustStoreLiveSourceAuthorization(t, recordauth.SourceKindVPS, testStoreRecordVPSID, visibility)
	valid := currentRecordAuthorizationSnapshot{
		recordID:           "rec_current1",
		projectID:          recordauth.ProjectIDDefault,
		lifecycle:          records.LifecycleActive,
		currentRevisionID:  "rrv_current00000004",
		lockVersion:        7,
		authorizationEpoch: 5,
		visibility:         visibility,
		subjects: []RecordSubjectReadInput{{
			Reference:            storeRecordReference(records.SubjectKindVPS, testStoreRecordVPSID),
			IdentitySnapshot:     mustStoreRecordSnapshot(t, records.SubjectKindVPS, map[string]string{"display_name": "Captured VPS"}),
			CaptureAuthorization: capture,
		}},
	}
	loadErr := errors.New("load failed")
	resolveErr := errors.New("resolve failed")
	targetCapture := mustStoreLiveSourceAuthorization(t, recordauth.SourceKindTarget, testStoreRecordTargetID, visibility)
	twoSubjects := valid
	twoSubjects.subjects = append(append([]RecordSubjectReadInput(nil), valid.subjects...), RecordSubjectReadInput{
		Reference: func() records.SubjectReference {
			reference := storeRecordReference(records.SubjectKindTarget, testStoreRecordTargetID)
			reference.Primary = false
			return reference
		}(),
		IdentitySnapshot:     mustStoreRecordSnapshot(t, records.SubjectKindTarget, map[string]string{"display_name": "Captured Target", "target_type": "service"}),
		CaptureAuthorization: targetCapture,
	})
	tests := []struct {
		name      string
		load      func(context.Context, string) (currentRecordAuthorizationSnapshot, error)
		resolver  *fakeCurrentRecordSubjectResolver
		wantErr   error
		wantCalls int
	}{
		{
			name: "loader failure",
			load: func(context.Context, string) (currentRecordAuthorizationSnapshot, error) {
				return currentRecordAuthorizationSnapshot{}, loadErr
			},
			resolver:  &fakeCurrentRecordSubjectResolver{},
			wantErr:   loadErr,
			wantCalls: 0,
		},
		{
			name: "malformed current root",
			load: func(context.Context, string) (currentRecordAuthorizationSnapshot, error) {
				malformed := valid
				malformed.currentRevisionID = ""
				return malformed, nil
			},
			resolver:  &fakeCurrentRecordSubjectResolver{},
			wantErr:   ErrRecordSubjectUnavailable,
			wantCalls: 0,
		},
		{
			name: "subject refresh failure",
			load: func(context.Context, string) (currentRecordAuthorizationSnapshot, error) {
				return valid, nil
			},
			resolver:  &fakeCurrentRecordSubjectResolver{err: resolveErr},
			wantErr:   resolveErr,
			wantCalls: 1,
		},
		{
			name: "later subject refresh failure",
			load: func(context.Context, string) (currentRecordAuthorizationSnapshot, error) {
				return twoSubjects, nil
			},
			resolver: &fakeCurrentRecordSubjectResolver{resolve: func(_ recordauth.ActorScope, input RecordSubjectReadInput, call int) (records.ResolvedSubject, error) {
				if call == 2 {
					return records.ResolvedSubject{}, resolveErr
				}
				return records.ResolvedSubject{
					ProjectID:            recordauth.ProjectIDDefault,
					StableID:             input.Reference.SourceID,
					IdentitySnapshot:     input.IdentitySnapshot,
					LiveRoute:            "/vps/" + input.Reference.SourceID,
					CaptureAuthorization: input.CaptureAuthorization,
				}, nil
			}},
			wantErr:   resolveErr,
			wantCalls: 2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			source := &PostgresCurrentRecordAuthorizationSource{load: test.load, resolver: test.resolver}
			current, err := source.ResolveCurrentRecordAuthorization(context.Background(), actor, "rec_current1")
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("ResolveCurrentRecordAuthorization() error = %v, want %v", err, test.wantErr)
			}
			if !reflect.DeepEqual(current, records.CurrentRecordAuthorization{}) || test.resolver.calls != test.wantCalls {
				t.Fatalf("partial result/calls = (%#v, %d), want zero/%d", current, test.resolver.calls, test.wantCalls)
			}
		})
	}
}

func TestPostgresRecordRevisionAuthorizationSourceUsesRequestedRevisionEvidence(t *testing.T) {
	t.Parallel()

	actor := mustStoreRecordActor(t)
	visibility := mustStoreProjectVisibility(t)
	capture := mustStoreLiveSourceAuthorization(t, recordauth.SourceKindVPS, testStoreRecordVPSID, visibility)
	steps := make([]string, 0, 2)
	resolver := &fakeCurrentRecordSubjectResolver{
		steps: &steps,
		resolved: records.ResolvedSubject{
			ProjectID:            recordauth.ProjectIDDefault,
			StableID:             testStoreRecordVPSID,
			IdentitySnapshot:     mustStoreRecordSnapshot(t, records.SubjectKindVPS, map[string]string{"display_name": "Current VPS"}),
			LiveRoute:            "/vps/" + testStoreRecordVPSID,
			CaptureAuthorization: capture,
		},
	}
	source := &PostgresCurrentRecordAuthorizationSource{
		loadRevision: func(_ context.Context, recordID, revisionID string) (recordRevisionAuthorizationSnapshot, error) {
			steps = append(steps, "load:"+revisionID)
			return recordRevisionAuthorizationSnapshot{
				recordID:           recordID,
				projectID:          recordauth.ProjectIDDefault,
				lifecycle:          records.LifecycleActive,
				revisionID:         revisionID,
				currentRevisionID:  "rrv_current00000004",
				lockVersion:        7,
				authorizationEpoch: 5,
				visibility:         visibility,
				subjects: []RecordSubjectReadInput{{
					Reference:            storeRecordReference(records.SubjectKindVPS, testStoreRecordVPSID),
					IdentitySnapshot:     mustStoreRecordSnapshot(t, records.SubjectKindVPS, map[string]string{"display_name": "Historical VPS"}),
					CaptureAuthorization: capture,
				}},
			}, nil
		},
		resolver: resolver,
	}

	got, err := source.ResolveRecordRevisionAuthorization(
		context.Background(),
		actor,
		"rec_current1",
		"rrv_history00000002",
	)
	if err != nil {
		t.Fatalf("ResolveRecordRevisionAuthorization() error = %v", err)
	}
	if !reflect.DeepEqual(steps, []string{"load:rrv_history00000002", "resolve"}) {
		t.Fatalf("steps = %#v", steps)
	}
	if got.RecordID != "rec_current1" || got.RevisionID != "rrv_history00000002" ||
		got.CurrentRevisionID != "rrv_current00000004" || got.LockVersion != 7 ||
		got.AuthorizationEpoch != 5 || len(got.Evidence.Sources) != 1 ||
		got.Evidence.Sources[0].Digest != capture.Digest {
		t.Fatalf("historical authorization = %#v", got)
	}
}

func TestPostgresCurrentRecordAuthorizationLoaderReadsCanonicalCurrentRevisionEvidenceByOrdinal(t *testing.T) {
	t.Parallel()

	visibility := mustStoreProjectVisibility(t)
	vpsAuthorization := mustStoreLiveSourceAuthorization(t, recordauth.SourceKindVPS, testStoreRecordVPSID, visibility)
	targetAuthorization := mustStoreLiveSourceAuthorization(t, recordauth.SourceKindTarget, testStoreRecordTargetID, visibility)
	visibilityJSON := mustStoreRecordSubjectJSON(t, visibility)
	vpsIdentityJSON := mustStoreRecordSubjectJSON(t, map[string]string{"display_name": "Tokyo Edge"})
	targetIdentityJSON := mustStoreRecordSubjectJSON(t, map[string]string{"display_name": "Payments API", "target_type": "service"})
	vpsAuthorizationJSON := mustStoreRecordSubjectJSON(t, vpsAuthorization)
	targetAuthorizationJSON := mustStoreRecordSubjectJSON(t, targetAuthorization)

	var rootSQL string
	var subjectSQL string
	db := fakeCurrentRecordAuthorizationDB{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			rootSQL = sql
			if !reflect.DeepEqual(args, []any{"rec_current1"}) {
				t.Fatalf("root args = %#v", args)
			}
			return fakeCurrentRecordAuthorizationRow{scan: func(dest ...any) error {
				if len(dest) != 10 {
					t.Fatalf("root scan destinations = %d, want 10", len(dest))
				}
				*(dest[0].(*string)) = "rec_current1"
				*(dest[1].(*string)) = string(recordauth.ProjectIDDefault)
				*(dest[2].(*string)) = string(records.LifecycleActive)
				*(dest[3].(*string)) = "rrv_current00000004"
				*(dest[4].(*int64)) = 7
				*(dest[5].(*int64)) = 5
				*(dest[6].(*[]byte)) = append([]byte(nil), visibilityJSON...)
				*(dest[7].(*[]byte)) = append([]byte(nil), visibility.CanonicalHash[:]...)
				*(dest[8].(*[]byte)) = append([]byte(nil), visibilityJSON...)
				*(dest[9].(*[]byte)) = append([]byte(nil), visibility.CanonicalHash[:]...)
				return nil
			}}
		},
		query: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
			subjectSQL = sql
			if !reflect.DeepEqual(args, []any{"rrv_current00000004"}) {
				t.Fatalf("subject args = %#v", args)
			}
			return &fakeCurrentRecordAuthorizationRows{rows: []func(...any) error{
				func(dest ...any) error {
					setFakeCurrentRecordSubjectRow(
						t,
						dest,
						0,
						records.SubjectKindVPS,
						records.RelationRoleAffected,
						testStoreRecordVPSID,
						true,
						vpsIdentityJSON,
						vpsAuthorizationJSON,
						vpsAuthorization.Digest[:],
					)
					return nil
				},
				func(dest ...any) error {
					setFakeCurrentRecordSubjectRow(
						t,
						dest,
						1,
						records.SubjectKindTarget,
						records.RelationRoleEvidenceSource,
						testStoreRecordTargetID,
						false,
						targetIdentityJSON,
						targetAuthorizationJSON,
						targetAuthorization.Digest[:],
					)
					return nil
				},
			}}, nil
		},
	}

	snapshot, err := (&PostgresCurrentRecordAuthorizationSource{db: db}).loadCurrentRecordAuthorizationSnapshot(
		context.Background(),
		"rec_current1",
	)
	if err != nil {
		t.Fatalf("loadCurrentRecordAuthorizationSnapshot() error = %v", err)
	}
	if !strings.Contains(rootSQL, "join public.record_revisions") ||
		!strings.Contains(rootSQL, "current_visibility_scope") ||
		!strings.Contains(rootSQL, "visibility_scope") {
		t.Fatalf("root SQL = %q", rootSQL)
	}
	if !strings.Contains(subjectSQL, "from public.record_revision_subjects") ||
		!strings.Contains(subjectSQL, "order by ordinal asc") {
		t.Fatalf("subject SQL = %q", subjectSQL)
	}
	if snapshot.recordID != "rec_current1" || snapshot.projectID != recordauth.ProjectIDDefault ||
		snapshot.lifecycle != records.LifecycleActive || snapshot.currentRevisionID != "rrv_current00000004" ||
		snapshot.lockVersion != 7 || snapshot.authorizationEpoch != 5 ||
		snapshot.visibility.CanonicalHash != visibility.CanonicalHash || len(snapshot.subjects) != 2 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if snapshot.subjects[0].Reference.SourceID != testStoreRecordVPSID ||
		snapshot.subjects[1].Reference.SourceID != testStoreRecordTargetID ||
		snapshot.subjects[0].CaptureAuthorization.Digest != vpsAuthorization.Digest ||
		snapshot.subjects[1].CaptureAuthorization.Digest != targetAuthorization.Digest {
		t.Fatalf("subjects = %#v", snapshot.subjects)
	}
}

func TestNewPostgresCurrentRecordAuthorizationSourceBindsLoaderAndFailsClosedOnNilDependencies(t *testing.T) {
	t.Parallel()

	source := NewPostgresCurrentRecordAuthorizationSource(nil, nil, nil)
	if source == nil || source.load == nil {
		t.Fatalf("NewPostgresCurrentRecordAuthorizationSource() = %#v, want bound loader", source)
	}
	if _, err := source.ResolveCurrentRecordAuthorization(
		context.Background(),
		mustStoreRecordActor(t),
		"rec_current1",
	); !errors.Is(err, ErrRecordSubjectUnavailable) {
		t.Fatalf("ResolveCurrentRecordAuthorization() error = %v, want ErrRecordSubjectUnavailable", err)
	}
}

func TestPostgresCurrentRecordAuthorizationSourceAdmitsAndFencesBeforeSnapshotReads(t *testing.T) {
	t.Parallel()

	actor := mustStoreRecordActor(t)
	t.Run("admission", func(t *testing.T) {
		tx := &fakeRecordReadFenceTx{}
		resolver := &fakeCurrentRecordSubjectResolver{}
		source := &PostgresCurrentRecordAuthorizationSource{
			platform: &PostgresRecordPlatformRepository{
				beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil },
				gate: AdmissionGateFunc(func(context.Context, pgx.Tx) error {
					return ErrRecordPlatformAdmissionUnavailable
				}),
			},
			resolver: resolver,
		}
		source.load = source.loadAdmittedCurrentRecordAuthorizationSnapshot

		_, err := source.ResolveCurrentRecordAuthorization(context.Background(), actor, "rec_current1")
		if !errors.Is(err, ErrRecordPlatformAdmissionUnavailable) {
			t.Fatalf("ResolveCurrentRecordAuthorization() error = %v, want admission unavailable", err)
		}
		if tx.queryRowCalls != 0 || tx.queryCalls != 0 || resolver.calls != 0 {
			t.Fatalf("reads after denied admission = row:%d rows:%d resolver:%d", tx.queryRowCalls, tx.queryCalls, resolver.calls)
		}
	})

	for _, test := range []struct {
		name    string
		resolve func(*PostgresCurrentRecordAuthorizationSource) error
	}{
		{
			name: "current",
			resolve: func(source *PostgresCurrentRecordAuthorizationSource) error {
				_, err := source.ResolveCurrentRecordAuthorization(context.Background(), actor, "rec_current1")
				return err
			},
		},
		{
			name: "historical",
			resolve: func(source *PostgresCurrentRecordAuthorizationSource) error {
				_, err := source.ResolveRecordRevisionAuthorization(
					context.Background(), actor, "rec_current1", "rrv_history1",
				)
				return err
			},
		},
	} {
		t.Run(test.name+" fence", func(t *testing.T) {
			tx := &fakeRecordReadFenceTx{reservationState: "fenced"}
			resolver := &fakeCurrentRecordSubjectResolver{}
			source := &PostgresCurrentRecordAuthorizationSource{
				platform: &PostgresRecordPlatformRepository{
					beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil },
					gate:    allowRecordPlatformAdmissionGate,
				},
				resolver: resolver,
			}
			source.load = source.loadAdmittedCurrentRecordAuthorizationSnapshot
			source.loadRevision = source.loadAdmittedRecordRevisionAuthorizationSnapshot

			err := test.resolve(source)
			if !errors.Is(err, records.ErrRecordDeletionReserved) {
				t.Fatalf("authorization resolve error = %v, want ErrRecordDeletionReserved", err)
			}
			if tx.queryRowCalls != 1 || tx.queryCalls != 0 || tx.contentQueries != 0 || resolver.calls != 0 {
				t.Fatalf("reads after fence = row:%d rows:%d content:%d resolver:%d", tx.queryRowCalls, tx.queryCalls, tx.contentQueries, resolver.calls)
			}
		})
	}
}

func TestPostgresCurrentRecordAuthorizationLoaderFailsClosedOnMalformedOrUnavailablePersistence(t *testing.T) {
	t.Parallel()

	rootErr := errors.New("root scan failed")
	queryErr := errors.New("subject query failed")
	scanErr := errors.New("subject scan failed")
	rowsErr := errors.New("subject rows failed")
	restricted := mustStoreRestrictedVisibility(t, []recordauth.Role{recordauth.RoleProjectAdmin}, nil, 2)
	restrictedJSON := mustStoreRecordSubjectJSON(t, restricted)
	tests := []struct {
		name      string
		mutate    func(*currentRecordAuthorizationDBFixture)
		wantErr   error
		wantCause error
	}{
		{
			name: "missing root",
			mutate: func(fixture *currentRecordAuthorizationDBFixture) {
				fixture.rootErr = pgx.ErrNoRows
			},
			wantErr: records.ErrRecordNotFound,
		},
		{
			name: "root scan failure",
			mutate: func(fixture *currentRecordAuthorizationDBFixture) {
				fixture.rootErr = rootErr
			},
			wantErr:   ErrRecordSubjectUnavailable,
			wantCause: rootErr,
		},
		{
			name: "malformed current visibility JSON",
			mutate: func(fixture *currentRecordAuthorizationDBFixture) {
				fixture.currentVisibilityJSON = []byte("{")
			},
			wantErr: ErrRecordSubjectUnavailable,
		},
		{
			name: "current visibility digest drift",
			mutate: func(fixture *currentRecordAuthorizationDBFixture) {
				fixture.currentVisibilityHash[0] ^= 0xff
			},
			wantErr: ErrRecordSubjectUnavailable,
		},
		{
			name: "root and revision visibility drift",
			mutate: func(fixture *currentRecordAuthorizationDBFixture) {
				fixture.revisionVisibilityJSON = append([]byte(nil), restrictedJSON...)
				fixture.revisionVisibilityHash = append([]byte(nil), restricted.CanonicalHash[:]...)
			},
			wantErr: ErrRecordSubjectUnavailable,
		},
		{
			name: "subject query failure",
			mutate: func(fixture *currentRecordAuthorizationDBFixture) {
				fixture.subjectQueryErr = queryErr
			},
			wantErr:   ErrRecordSubjectUnavailable,
			wantCause: queryErr,
		},
		{
			name: "nil subject rows",
			mutate: func(fixture *currentRecordAuthorizationDBFixture) {
				fixture.nilSubjectRows = true
			},
			wantErr: ErrRecordSubjectUnavailable,
		},
		{
			name: "subject scan failure",
			mutate: func(fixture *currentRecordAuthorizationDBFixture) {
				fixture.subjectScanErr = scanErr
			},
			wantErr:   ErrRecordSubjectUnavailable,
			wantCause: scanErr,
		},
		{
			name: "malformed subject identity JSON",
			mutate: func(fixture *currentRecordAuthorizationDBFixture) {
				fixture.identityJSON = []byte("{")
			},
			wantErr: ErrRecordSubjectUnavailable,
		},
		{
			name: "subject authorization digest drift",
			mutate: func(fixture *currentRecordAuthorizationDBFixture) {
				fixture.authorizationHash[0] ^= 0xff
			},
			wantErr: ErrRecordSubjectUnavailable,
		},
		{
			name: "subject ordinal gap",
			mutate: func(fixture *currentRecordAuthorizationDBFixture) {
				fixture.ordinal = 1
			},
			wantErr: ErrRecordSubjectUnavailable,
		},
		{
			name: "missing primary",
			mutate: func(fixture *currentRecordAuthorizationDBFixture) {
				fixture.primary = false
			},
			wantErr: ErrRecordSubjectUnavailable,
		},
		{
			name: "subject rows failure",
			mutate: func(fixture *currentRecordAuthorizationDBFixture) {
				fixture.subjectRowsErr = rowsErr
			},
			wantErr:   ErrRecordSubjectUnavailable,
			wantCause: rowsErr,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := newCurrentRecordAuthorizationDBFixture(t)
			test.mutate(&fixture)
			_, err := (&PostgresCurrentRecordAuthorizationSource{db: fixture.db(t)}).
				loadCurrentRecordAuthorizationSnapshot(context.Background(), fixture.recordID)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("loadCurrentRecordAuthorizationSnapshot() error = %v, want %v", err, test.wantErr)
			}
			if test.wantCause != nil && !errors.Is(err, test.wantCause) {
				t.Fatalf("loadCurrentRecordAuthorizationSnapshot() error = %v, want cause %v", err, test.wantCause)
			}
		})
	}
}

type fakeCurrentRecordSubjectResolver struct {
	resolved records.ResolvedSubject
	err      error
	resolve  func(recordauth.ActorScope, RecordSubjectReadInput, int) (records.ResolvedSubject, error)
	steps    *[]string
	calls    int
	actor    recordauth.ActorScope
	input    RecordSubjectReadInput
	inputs   []RecordSubjectReadInput
}

type fakeCurrentRecordAuthorizationDB struct {
	queryRow func(context.Context, string, ...any) pgx.Row
	query    func(context.Context, string, ...any) (pgx.Rows, error)
}

type currentRecordAuthorizationDBFixture struct {
	recordID               string
	projectID              string
	lifecycle              string
	currentRevisionID      string
	lockVersion            int64
	authorizationEpoch     int64
	currentVisibilityJSON  []byte
	currentVisibilityHash  []byte
	revisionVisibilityJSON []byte
	revisionVisibilityHash []byte
	rootErr                error
	subjectQueryErr        error
	nilSubjectRows         bool
	subjectScanErr         error
	subjectRowsErr         error
	ordinal                int64
	registryVersion        int64
	subjectKind            string
	relationRole           string
	sourceID               string
	primary                bool
	identityJSON           []byte
	authorizationJSON      []byte
	authorizationHash      []byte
}

func newCurrentRecordAuthorizationDBFixture(t *testing.T) currentRecordAuthorizationDBFixture {
	t.Helper()
	visibility := mustStoreProjectVisibility(t)
	authorization := mustStoreLiveSourceAuthorization(t, recordauth.SourceKindVPS, testStoreRecordVPSID, visibility)
	return currentRecordAuthorizationDBFixture{
		recordID:               "rec_current1",
		projectID:              string(recordauth.ProjectIDDefault),
		lifecycle:              string(records.LifecycleActive),
		currentRevisionID:      "rrv_current00000004",
		lockVersion:            7,
		authorizationEpoch:     5,
		currentVisibilityJSON:  mustStoreRecordSubjectJSON(t, visibility),
		currentVisibilityHash:  append([]byte(nil), visibility.CanonicalHash[:]...),
		revisionVisibilityJSON: mustStoreRecordSubjectJSON(t, visibility),
		revisionVisibilityHash: append([]byte(nil), visibility.CanonicalHash[:]...),
		ordinal:                0,
		registryVersion:        int64(records.SubjectRegistryVersionV1),
		subjectKind:            string(records.SubjectKindVPS),
		relationRole:           string(records.RelationRoleAffected),
		sourceID:               testStoreRecordVPSID,
		primary:                true,
		identityJSON:           mustStoreRecordSubjectJSON(t, map[string]string{"display_name": "Tokyo Edge"}),
		authorizationJSON:      mustStoreRecordSubjectJSON(t, authorization),
		authorizationHash:      append([]byte(nil), authorization.Digest[:]...),
	}
}

func (fixture currentRecordAuthorizationDBFixture) db(t *testing.T) fakeCurrentRecordAuthorizationDB {
	t.Helper()
	return fakeCurrentRecordAuthorizationDB{
		queryRow: func(context.Context, string, ...any) pgx.Row {
			return fakeCurrentRecordAuthorizationRow{scan: func(dest ...any) error {
				if fixture.rootErr != nil {
					return fixture.rootErr
				}
				if len(dest) != 10 {
					t.Fatalf("root scan destinations = %d, want 10", len(dest))
				}
				*(dest[0].(*string)) = fixture.recordID
				*(dest[1].(*string)) = fixture.projectID
				*(dest[2].(*string)) = fixture.lifecycle
				*(dest[3].(*string)) = fixture.currentRevisionID
				*(dest[4].(*int64)) = fixture.lockVersion
				*(dest[5].(*int64)) = fixture.authorizationEpoch
				*(dest[6].(*[]byte)) = append([]byte(nil), fixture.currentVisibilityJSON...)
				*(dest[7].(*[]byte)) = append([]byte(nil), fixture.currentVisibilityHash...)
				*(dest[8].(*[]byte)) = append([]byte(nil), fixture.revisionVisibilityJSON...)
				*(dest[9].(*[]byte)) = append([]byte(nil), fixture.revisionVisibilityHash...)
				return nil
			}}
		},
		query: func(context.Context, string, ...any) (pgx.Rows, error) {
			if fixture.subjectQueryErr != nil {
				return nil, fixture.subjectQueryErr
			}
			if fixture.nilSubjectRows {
				return nil, nil
			}
			return &fakeCurrentRecordAuthorizationRows{
				rows: []func(...any) error{func(dest ...any) error {
					if fixture.subjectScanErr != nil {
						return fixture.subjectScanErr
					}
					if len(dest) != 9 {
						t.Fatalf("subject scan destinations = %d, want 9", len(dest))
					}
					*(dest[0].(*int64)) = fixture.ordinal
					*(dest[1].(*int64)) = fixture.registryVersion
					*(dest[2].(*string)) = fixture.subjectKind
					*(dest[3].(*string)) = fixture.relationRole
					*(dest[4].(*string)) = fixture.sourceID
					*(dest[5].(*bool)) = fixture.primary
					*(dest[6].(*[]byte)) = append([]byte(nil), fixture.identityJSON...)
					*(dest[7].(*[]byte)) = append([]byte(nil), fixture.authorizationJSON...)
					*(dest[8].(*[]byte)) = append([]byte(nil), fixture.authorizationHash...)
					return nil
				}},
				err: fixture.subjectRowsErr,
			}, nil
		},
	}
}

func (db fakeCurrentRecordAuthorizationDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return db.queryRow(ctx, sql, args...)
}

func (db fakeCurrentRecordAuthorizationDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return db.query(ctx, sql, args...)
}

type fakeCurrentRecordAuthorizationRow struct {
	scan func(...any) error
}

func (row fakeCurrentRecordAuthorizationRow) Scan(dest ...any) error {
	return row.scan(dest...)
}

type fakeCurrentRecordAuthorizationRows struct {
	rows []func(...any) error
	err  error
	idx  int
}

func (rows *fakeCurrentRecordAuthorizationRows) Close()     {}
func (rows *fakeCurrentRecordAuthorizationRows) Err() error { return rows.err }
func (rows *fakeCurrentRecordAuthorizationRows) CommandTag() pgconn.CommandTag {
	return pgconn.CommandTag{}
}
func (rows *fakeCurrentRecordAuthorizationRows) FieldDescriptions() []pgconn.FieldDescription {
	return nil
}
func (rows *fakeCurrentRecordAuthorizationRows) RawValues() [][]byte    { return nil }
func (rows *fakeCurrentRecordAuthorizationRows) Values() ([]any, error) { return nil, nil }
func (rows *fakeCurrentRecordAuthorizationRows) Conn() *pgx.Conn        { return nil }
func (rows *fakeCurrentRecordAuthorizationRows) Next() bool {
	if rows.idx >= len(rows.rows) {
		return false
	}
	rows.idx++
	return true
}
func (rows *fakeCurrentRecordAuthorizationRows) Scan(dest ...any) error {
	return rows.rows[rows.idx-1](dest...)
}

func setFakeCurrentRecordSubjectRow(
	t *testing.T,
	dest []any,
	ordinal int64,
	kind records.SubjectKind,
	role records.RelationRole,
	sourceID string,
	primary bool,
	identityJSON []byte,
	authorizationJSON []byte,
	digest []byte,
) {
	t.Helper()
	if len(dest) != 9 {
		t.Fatalf("subject scan destinations = %d, want 9", len(dest))
	}
	*(dest[0].(*int64)) = ordinal
	*(dest[1].(*int64)) = int64(records.SubjectRegistryVersionV1)
	*(dest[2].(*string)) = string(kind)
	*(dest[3].(*string)) = string(role)
	*(dest[4].(*string)) = sourceID
	*(dest[5].(*bool)) = primary
	*(dest[6].(*[]byte)) = append([]byte(nil), identityJSON...)
	*(dest[7].(*[]byte)) = append([]byte(nil), authorizationJSON...)
	*(dest[8].(*[]byte)) = append([]byte(nil), digest...)
}

func mustStoreRecordSubjectJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return encoded
}

func (resolver *fakeCurrentRecordSubjectResolver) Resolve(
	_ context.Context,
	actor recordauth.ActorScope,
	input RecordSubjectReadInput,
) (records.ResolvedSubject, error) {
	resolver.calls++
	resolver.actor = actor.Clone()
	resolver.input = input
	resolver.inputs = append(resolver.inputs, input)
	if resolver.steps != nil {
		*resolver.steps = append(*resolver.steps, "resolve")
	}
	if resolver.resolve != nil {
		return resolver.resolve(actor, input, resolver.calls)
	}
	return resolver.resolved, resolver.err
}

type fakeVPSRecordSubjectSource struct {
	record vpsRecordSubject
	err    error
	calls  int
	id     string
}

func (source *fakeVPSRecordSubjectSource) loadVPSRecordSubject(_ context.Context, id string) (vpsRecordSubject, error) {
	source.calls++
	source.id = id
	return source.record, source.err
}

type fakeMonitoringRecordSubjectSource struct {
	record monitoringRecordSubject
	err    error
	calls  int
	id     string
}

func (source *fakeMonitoringRecordSubjectSource) loadMonitoringRecordSubject(_ context.Context, id string) (monitoringRecordSubject, error) {
	source.calls++
	source.id = id
	return source.record, source.err
}

type fakeTargetRecordSubjectSource struct {
	record targetRecordSubject
	err    error
	calls  int
	id     string
}

func (source *fakeTargetRecordSubjectSource) loadTargetRecordSubject(_ context.Context, id string) (targetRecordSubject, error) {
	source.calls++
	source.id = id
	return source.record, source.err
}

type fakeWitnessedRecordSubjectTombstoneSource struct {
	evidence  WitnessedRecordSubjectTombstone
	err       error
	calls     int
	projectID recordauth.ProjectID
	kind      recordauth.SourceKind
	sourceID  string
}

func (source *fakeWitnessedRecordSubjectTombstoneSource) ResolveWitnessedRecordSubjectTombstone(
	_ context.Context,
	projectID recordauth.ProjectID,
	kind recordauth.SourceKind,
	sourceID string,
) (WitnessedRecordSubjectTombstone, error) {
	source.calls++
	source.projectID = projectID
	source.kind = kind
	source.sourceID = sourceID
	return source.evidence, source.err
}

func storeRecordReference(kind records.SubjectKind, sourceID string) records.SubjectReference {
	return records.SubjectReference{
		RegistryVersion: records.SubjectRegistryVersionV1,
		Kind:            kind,
		Role:            records.RelationRoleAffected,
		SourceID:        sourceID,
		Primary:         true,
	}
}

func mustStoreRecordActor(t *testing.T) recordauth.ActorScope {
	t.Helper()
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID:    testStoreRecordUserID,
		Role:      recordauth.RoleProjectAdmin,
		ProjectID: recordauth.ProjectIDDefault,
		GroupIDs:  []string{testStoreRecordGroupID},
	})
	if err != nil {
		t.Fatalf("NormalizeActorScope() error = %v", err)
	}
	return actor
}

func mustStoreProjectVisibility(t *testing.T) recordauth.VisibilityScope {
	t.Helper()
	return mustStoreVisibility(t, recordauth.VisibilityKindProject, nil, nil, recordSubjectSourcePolicyRevisionV1)
}

func mustStoreRestrictedVisibility(t *testing.T, roles []recordauth.Role, groups []string, revision uint64) recordauth.VisibilityScope {
	t.Helper()
	return mustStoreVisibility(t, recordauth.VisibilityKindRestricted, roles, groups, revision)
}

func mustStoreVisibility(
	t *testing.T,
	kind recordauth.VisibilityKind,
	roles []recordauth.Role,
	groups []string,
	revision uint64,
) recordauth.VisibilityScope {
	t.Helper()
	scope, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{
		Version:         recordauth.VisibilityScopeVersionV1,
		Kind:            kind,
		ProjectID:       recordauth.ProjectIDDefault,
		AllowedRoles:    roles,
		AllowedGroupIDs: groups,
		PolicyVersion:   recordauth.PolicyVersionV1,
		PolicyRevision:  revision,
	})
	if err != nil {
		t.Fatalf("NormalizeVisibilityScope() error = %v", err)
	}
	return scope
}

func mustStoreLiveSourceAuthorization(
	t *testing.T,
	kind recordauth.SourceKind,
	sourceID string,
	scope recordauth.VisibilityScope,
) recordauth.SourceAuthorization {
	t.Helper()
	authorization, err := recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{
		Version:      recordauth.SourceAuthorizationVersionV1,
		Kind:         kind,
		SourceID:     sourceID,
		State:        recordauth.SourceStateLive,
		CaptureScope: scope,
		CurrentScope: &scope,
	})
	if err != nil {
		t.Fatalf("NormalizeSourceAuthorization() error = %v", err)
	}
	return authorization
}

func mustStoreRecordSnapshot(t *testing.T, kind records.SubjectKind, fields map[string]string) records.SubjectIdentitySnapshot {
	t.Helper()
	snapshot, err := records.NewSubjectIdentitySnapshot(kind, fields)
	if err != nil {
		t.Fatalf("NewSubjectIdentitySnapshot() error = %v", err)
	}
	return snapshot
}

func assertStoreRecordStringMap(t *testing.T, got, want map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("map = %#v, want %#v", got, want)
	}
	for key, wantValue := range want {
		if got[key] != wantValue {
			t.Fatalf("map[%q] = %q, want %q", key, got[key], wantValue)
		}
	}
}

func assertNarrowRecordSubjectQuery(t *testing.T, query string, required, forbidden []string) {
	t.Helper()
	normalized := strings.ToLower(query)
	for _, snippet := range required {
		if !strings.Contains(normalized, strings.ToLower(snippet)) {
			t.Fatalf("query missing %q: %s", snippet, query)
		}
	}
	for _, snippet := range forbidden {
		if strings.Contains(normalized, strings.ToLower(snippet)) {
			t.Fatalf("query contains unsafe field %q: %s", snippet, query)
		}
	}
}

func mutateStoreTombstone(
	input WitnessedRecordSubjectTombstone,
	mutate func(*WitnessedRecordSubjectTombstone),
) WitnessedRecordSubjectTombstone {
	mutate(&input)
	return input
}
