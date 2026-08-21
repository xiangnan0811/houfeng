package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
)

func TestWitnessedRecordSubjectTombstoneReaderRejectsDigestOnlyLocalProjection(t *testing.T) {
	t.Parallel()

	floor := mustStoreRestrictedVisibility(t, []recordauth.Role{recordauth.RoleProjectAdmin}, []string{testStoreRecordGroupID}, 3)
	local := newWitnessQueryer(func(query string) pgx.Row {
		if strings.Contains(query, "from public.source_deletion_tombstones") {
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*(dest[0].(*[]byte)) = append([]byte(nil), floor.CanonicalHash[:]...)
				return nil
			}}
		}
		return fakeRecordPlatformRow{scan: func(...any) error { return errors.New("unexpected local query") }}
	})
	witness := newWitnessQueryer(func(query string) pgx.Row {
		if strings.Contains(query, "from public.deletion_witness_entries") {
			return fakeRecordPlatformRow{scan: func(...any) error { return pgx.ErrNoRows }}
		}
		return fakeRecordPlatformRow{scan: func(...any) error { return errors.New("unexpected witness query") }}
	})

	reader := mustWitnessedRecordSubjectTombstoneReader(t, witness, local)
	_, err := reader.ResolveWitnessedRecordSubjectTombstone(
		context.Background(),
		recordauth.ProjectIDDefault,
		recordauth.SourceKindVPS,
		testStoreRecordVPSID,
	)
	if !errors.Is(err, ErrWitnessedRecordSubjectTombstoneNotFound) {
		t.Fatalf("ResolveWitnessedRecordSubjectTombstone() error = %v, want ErrWitnessedRecordSubjectTombstoneNotFound", err)
	}
}

func TestWitnessedRecordSubjectTombstoneReaderFailClosedOnMissingStaleUnknownAndUnreachable(t *testing.T) {
	t.Parallel()

	floor := mustStoreRestrictedVisibility(t, []recordauth.Role{recordauth.RoleProjectAdmin}, []string{testStoreRecordGroupID}, 3)
	lastLive := floor
	valid := sourceDeletionWitnessRow{
		EntryVersion:           1,
		EntryType:              "delete_commit",
		Route:                  "source_permanent_delete",
		ObjectKind:             "vps",
		ObjectID:               testStoreRecordVPSID,
		DeploymentID:           string(testDeploymentMembershipIdentity().DeploymentID),
		ProjectID:              string(recordauth.ProjectIDDefault),
		AuthorizationFloor:     floor.CanonicalBytes(),
		AuthorizationFloorHash: floor.CanonicalHash,
		OriginIdentity:         lastLive.CanonicalBytes(),
	}

	tests := map[string]struct {
		reader func(*testing.T) *WitnessedRecordSubjectTombstoneReader
		want   error
	}{
		"typed-nil": {
			reader: func(*testing.T) *WitnessedRecordSubjectTombstoneReader { return nil },
			want:   ErrRecordSubjectUnavailable,
		},
		"unreachable witness": {
			reader: func(t *testing.T) *WitnessedRecordSubjectTombstoneReader {
				return mustWitnessedRecordSubjectTombstoneReader(t, nil, nil)
			},
			want: ErrRecordSubjectUnavailable,
		},
		"missing witness": {
			reader: func(t *testing.T) *WitnessedRecordSubjectTombstoneReader {
				return mustWitnessedRecordSubjectTombstoneReader(t, newWitnessQueryer(func(string) pgx.Row {
					return fakeRecordPlatformRow{scan: func(...any) error { return pgx.ErrNoRows }}
				}), nil)
			},
			want: ErrWitnessedRecordSubjectTombstoneNotFound,
		},
		"unknown version": {
			reader: func(t *testing.T) *WitnessedRecordSubjectTombstoneReader {
				row := valid
				row.EntryVersion = 2
				return mustWitnessedRecordSubjectTombstoneReader(t, newSourceDeletionWitnessQueryer(row), nil)
			},
			want: ErrRecordSubjectUnavailable,
		},
		"stale hash": {
			reader: func(t *testing.T) *WitnessedRecordSubjectTombstoneReader {
				row := valid
				row.AuthorizationFloorHash[0] ^= 0xff
				return mustWitnessedRecordSubjectTombstoneReader(t, newSourceDeletionWitnessQueryer(row), nil)
			},
			want: ErrRecordSubjectUnavailable,
		},
		"wrong deployment": {
			reader: func(t *testing.T) *WitnessedRecordSubjectTombstoneReader {
				row := valid
				row.DeploymentID = "dp-" + strings.Repeat("b", 64)
				return mustWitnessedRecordSubjectTombstoneReader(t, newSourceDeletionWitnessQueryer(row), nil)
			},
			want: ErrRecordSubjectUnavailable,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := test.reader(t).ResolveWitnessedRecordSubjectTombstone(
				context.Background(),
				recordauth.ProjectIDDefault,
				recordauth.SourceKindVPS,
				testStoreRecordVPSID,
			)
			if !errors.Is(err, test.want) {
				t.Fatalf("ResolveWitnessedRecordSubjectTombstone() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestWitnessedRecordSubjectTombstoneReaderReturnsFinalFloorFromFullWitness(t *testing.T) {
	t.Parallel()

	floor := mustStoreRestrictedVisibility(t, []recordauth.Role{recordauth.RoleProjectAdmin}, []string{testStoreRecordGroupID}, 3)
	lastLive := mustStoreRestrictedVisibility(t, []recordauth.Role{recordauth.RoleProjectAdmin}, []string{testStoreRecordGroupID}, 4)
	row := sourceDeletionWitnessRow{
		EntryVersion:           1,
		EntryType:              "delete_commit",
		Route:                  "source_permanent_delete",
		ObjectKind:             "vps",
		ObjectID:               testStoreRecordVPSID,
		DeploymentID:           string(testDeploymentMembershipIdentity().DeploymentID),
		ProjectID:              string(recordauth.ProjectIDDefault),
		AuthorizationFloor:     floor.CanonicalBytes(),
		AuthorizationFloorHash: floor.CanonicalHash,
		OriginIdentity:         lastLive.CanonicalBytes(),
	}
	local := newWitnessQueryer(func(query string) pgx.Row {
		if strings.Contains(query, "from public.source_deletion_tombstones") {
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*(dest[0].(*[]byte)) = append([]byte(nil), floor.CanonicalHash[:]...)
				return nil
			}}
		}
		return fakeRecordPlatformRow{scan: func(...any) error { return errors.New("unexpected local query") }}
	})

	got, err := mustWitnessedRecordSubjectTombstoneReader(t, newSourceDeletionWitnessQueryer(row), local).
		ResolveWitnessedRecordSubjectTombstone(
			context.Background(),
			recordauth.ProjectIDDefault,
			recordauth.SourceKindVPS,
			testStoreRecordVPSID,
		)
	if err != nil {
		t.Fatalf("ResolveWitnessedRecordSubjectTombstone() error = %v", err)
	}
	if got.Version != WitnessedRecordSubjectTombstoneVersionV1 ||
		got.ProjectID != recordauth.ProjectIDDefault ||
		got.Kind != recordauth.SourceKindVPS ||
		got.SourceID != testStoreRecordVPSID ||
		got.AuthorizationFloorDigest != floor.CanonicalHash ||
		!visibilityScopesEqual(got.AuthorizationFloor, floor) ||
		!visibilityScopesEqual(got.LastLiveScope, lastLive) {
		t.Fatalf("tombstone = %#v, want witnessed floor and last live", got)
	}
}

func mustWitnessedRecordSubjectTombstoneReader(
	t *testing.T,
	witness, local currentRecordAuthorizationDB,
) *WitnessedRecordSubjectTombstoneReader {
	t.Helper()
	identity := testDeploymentMembershipIdentity()
	reader, err := NewWitnessedRecordSubjectTombstoneReader(
		recordplatform.DeploymentID(identity.DeploymentID),
		recordauth.ProjectID(identity.ProjectID),
		witness,
		local,
	)
	if err != nil {
		t.Fatalf("NewWitnessedRecordSubjectTombstoneReader() error = %v", err)
	}
	return reader
}

func visibilityScopesEqual(left, right recordauth.VisibilityScope) bool {
	return left.Version == right.Version &&
		left.Kind == right.Kind &&
		left.ProjectID == right.ProjectID &&
		left.PolicyVersion == right.PolicyVersion &&
		left.PolicyRevision == right.PolicyRevision &&
		left.CanonicalHash == right.CanonicalHash
}

type sourceDeletionWitnessRow struct {
	EntryVersion           int16
	EntryType              string
	Route                  string
	ObjectKind             string
	ObjectID               string
	DeploymentID           string
	ProjectID              string
	AuthorizationFloor     []byte
	AuthorizationFloorHash [32]byte
	OriginIdentity         []byte
}

type witnessQueryer struct {
	queryRow func(string) pgx.Row
}

func newWitnessQueryer(queryRow func(string) pgx.Row) *witnessQueryer {
	return &witnessQueryer{queryRow: queryRow}
}

func (queryer *witnessQueryer) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	if queryer == nil || queryer.queryRow == nil {
		return fakeRecordPlatformRow{scan: func(...any) error { return errors.New("unreachable witness") }}
	}
	return queryer.queryRow(sql)
}

func (queryer *witnessQueryer) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected query")
}

func newSourceDeletionWitnessQueryer(row sourceDeletionWitnessRow) *witnessQueryer {
	return newWitnessQueryer(func(query string) pgx.Row {
		if !strings.Contains(query, "from public.deletion_witness_entries") {
			return fakeRecordPlatformRow{scan: func(...any) error { return errors.New("unexpected witness query") }}
		}
		copied := row
		return fakeRecordPlatformRow{scan: func(dest ...any) error {
			*(dest[0].(*int16)) = copied.EntryVersion
			*(dest[1].(*string)) = copied.EntryType
			*(dest[2].(*string)) = copied.Route
			*(dest[3].(*string)) = copied.ObjectKind
			*(dest[4].(*string)) = copied.ObjectID
			*(dest[5].(*string)) = copied.DeploymentID
			*(dest[6].(*string)) = copied.ProjectID
			*(dest[7].(*[]byte)) = append([]byte(nil), copied.AuthorizationFloor...)
			*(dest[8].(*[]byte)) = append([]byte(nil), copied.AuthorizationFloorHash[:]...)
			*(dest[9].(*[]byte)) = append([]byte(nil), copied.OriginIdentity...)
			return nil
		}}
	})
}

func TestWitnessedRecordSubjectTombstoneReaderLocalDigestMismatchFailsClosed(t *testing.T) {
	t.Parallel()

	floor := mustStoreProjectVisibility(t)
	row := sourceDeletionWitnessRow{
		EntryVersion:           1,
		EntryType:              "delete_commit",
		Route:                  "source_permanent_delete",
		ObjectKind:             "vps",
		ObjectID:               testStoreRecordVPSID,
		DeploymentID:           string(testDeploymentMembershipIdentity().DeploymentID),
		ProjectID:              string(recordauth.ProjectIDDefault),
		AuthorizationFloor:     floor.CanonicalBytes(),
		AuthorizationFloorHash: floor.CanonicalHash,
		OriginIdentity:         floor.CanonicalBytes(),
	}
	local := newWitnessQueryer(func(query string) pgx.Row {
		if strings.Contains(query, "from public.source_deletion_tombstones") {
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				digest := sha256.Sum256([]byte("other-floor"))
				*(dest[0].(*[]byte)) = append([]byte(nil), digest[:]...)
				return nil
			}}
		}
		return fakeRecordPlatformRow{scan: func(...any) error { return errors.New("unexpected local query") }}
	})

	_, err := mustWitnessedRecordSubjectTombstoneReader(t, newSourceDeletionWitnessQueryer(row), local).
		ResolveWitnessedRecordSubjectTombstone(
			context.Background(),
			recordauth.ProjectIDDefault,
			recordauth.SourceKindVPS,
			testStoreRecordVPSID,
		)
	if !errors.Is(err, ErrRecordSubjectUnavailable) {
		t.Fatalf("digest mismatch error = %v, want ErrRecordSubjectUnavailable", err)
	}
}
