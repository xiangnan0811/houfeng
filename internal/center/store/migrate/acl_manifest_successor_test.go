package migrate

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestInsertAppACLManifestSuccessorV1WritesRevisionTwoAndCASHead(t *testing.T) {
	previous, migrationBody, privilegeBody := appACLManifestSuccessorFixture(t)
	tx := &recordingAppACLManifestSuccessorTx{
		tags: []pgconn.CommandTag{pgconn.NewCommandTag("INSERT 0 1"), pgconn.NewCommandTag("UPDATE 1")},
	}
	wantSuccessor, err := NewAppACLManifestPersistedV1(2, previous.MigratorCatalogRole, previous.ManifestDigest, migrationBody, privilegeBody)
	if err != nil {
		t.Fatal(err)
	}
	dependencies := appACLManifestSuccessorWriterDependencies{
		readManifests: func(context.Context, pgx.Tx) ([]AppACLManifestPersistedV1, error) {
			return []AppACLManifestPersistedV1{previous, wantSuccessor}, nil
		},
		readHead: func(context.Context, pgx.Tx) (*AppACLManifestHeadV1, error) {
			return &AppACLManifestHeadV1{ManifestRevision: 2, ManifestDigest: wantSuccessor.ManifestDigest}, nil
		},
	}
	successor, err := insertAppACLManifestSuccessorV1WithDependencies(
		context.Background(), tx, previous, migrationBody, privilegeBody, dependencies,
	)
	if err != nil {
		t.Fatalf("insertAppACLManifestSuccessorV1WithDependencies() error = %v", err)
	}
	if successor.ManifestRevision != 2 || successor.PreviousManifestDigest != previous.ManifestDigest ||
		successor.MigratorCatalogRole != previous.MigratorCatalogRole ||
		!bytes.Equal(successor.CanonicalMigrationSet, migrationBody) ||
		!bytes.Equal(successor.CanonicalPrivilegeSet, privilegeBody) {
		t.Fatalf("successor manifest = %#v, want revision 2 linked to predecessor", successor)
	}
	if len(tx.calls) != 2 {
		t.Fatalf("successor SQL calls = %d, want 2", len(tx.calls))
	}
	insert := tx.calls[0]
	if !strings.Contains(insert.sql, "insert into public.app_acl_manifest_revisions") || len(insert.arguments) != 8 {
		t.Fatalf("successor insert SQL/arguments = %q/%#v", insert.sql, insert.arguments)
	}
	if got, ok := insert.arguments[0].(int64); !ok || got != 2 {
		t.Fatalf("successor insert revision = %#v, want int64(2)", insert.arguments[0])
	}
	if got, ok := insert.arguments[2].([]byte); !ok || !bytes.Equal(got, previous.ManifestDigest[:]) {
		t.Fatalf("successor previous digest = %#v, want %x", insert.arguments[2], previous.ManifestDigest)
	}
	cas := tx.calls[1]
	if !strings.Contains(cas.sql, "update public.app_acl_manifest_head") ||
		!strings.Contains(cas.sql, "manifest_revision = $3") ||
		!strings.Contains(cas.sql, "manifest_digest = $4") || len(cas.arguments) != 4 {
		t.Fatalf("successor head CAS SQL/arguments = %q/%#v", cas.sql, cas.arguments)
	}
	if cas.arguments[2] != int64(1) {
		t.Fatalf("successor CAS old revision = %#v, want int64(1)", cas.arguments[2])
	}
	if got, ok := cas.arguments[3].([]byte); !ok || !bytes.Equal(got, previous.ManifestDigest[:]) {
		t.Fatalf("successor CAS old digest = %#v, want %x", cas.arguments[3], previous.ManifestDigest)
	}
}

func TestInsertAppACLManifestSuccessorV1PropagatesWriteAndReadbackFailures(t *testing.T) {
	previous, migrationBody, privilegeBody := appACLManifestSuccessorFixture(t)
	sentinel := errors.New("sentinel")
	for _, tc := range []struct {
		name         string
		tx           *recordingAppACLManifestSuccessorTx
		dependencies appACLManifestSuccessorWriterDependencies
		want         string
	}{
		{
			name: "insert",
			tx:   &recordingAppACLManifestSuccessorTx{errors: []error{sentinel}},
			dependencies: appACLManifestSuccessorWriterDependencies{
				readManifests: unexpectedAppACLManifestSuccessorReadManifests(t),
				readHead:      unexpectedAppACLManifestSuccessorReadHead(t),
			},
			want: "insert",
		},
		{
			name: "head update",
			tx:   &recordingAppACLManifestSuccessorTx{tags: []pgconn.CommandTag{pgconn.NewCommandTag("INSERT 0 1")}, errors: []error{nil, sentinel}},
			dependencies: appACLManifestSuccessorWriterDependencies{
				readManifests: unexpectedAppACLManifestSuccessorReadManifests(t),
				readHead:      unexpectedAppACLManifestSuccessorReadHead(t),
			},
			want: "cas",
		},
		{
			name: "head changed",
			tx:   &recordingAppACLManifestSuccessorTx{tags: []pgconn.CommandTag{pgconn.NewCommandTag("INSERT 0 1"), pgconn.NewCommandTag("UPDATE 0")}},
			dependencies: appACLManifestSuccessorWriterDependencies{
				readManifests: unexpectedAppACLManifestSuccessorReadManifests(t),
				readHead:      unexpectedAppACLManifestSuccessorReadHead(t),
			},
			want: "concurrently",
		},
		{
			name: "revision readback",
			tx:   &recordingAppACLManifestSuccessorTx{tags: []pgconn.CommandTag{pgconn.NewCommandTag("INSERT 0 1"), pgconn.NewCommandTag("UPDATE 1")}},
			dependencies: appACLManifestSuccessorWriterDependencies{
				readManifests: func(context.Context, pgx.Tx) ([]AppACLManifestPersistedV1, error) { return nil, sentinel },
				readHead:      unexpectedAppACLManifestSuccessorReadHead(t),
			},
			want: "read back",
		},
		{
			name: "head readback",
			tx:   &recordingAppACLManifestSuccessorTx{tags: []pgconn.CommandTag{pgconn.NewCommandTag("INSERT 0 1"), pgconn.NewCommandTag("UPDATE 1")}},
			dependencies: appACLManifestSuccessorWriterDependencies{
				readManifests: func(context.Context, pgx.Tx) ([]AppACLManifestPersistedV1, error) {
					return []AppACLManifestPersistedV1{previous}, nil
				},
				readHead: func(context.Context, pgx.Tx) (*AppACLManifestHeadV1, error) { return nil, sentinel },
			},
			want: "read back",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := insertAppACLManifestSuccessorV1WithDependencies(
				context.Background(), tc.tx, previous, migrationBody, privilegeBody, tc.dependencies,
			)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), tc.want) {
				t.Fatalf("successor writer error = %v, want %q", err, tc.want)
			}
		})
	}
}

func appACLManifestSuccessorFixture(t *testing.T) (AppACLManifestPersistedV1, []byte, []byte) {
	t.Helper()
	migrationBody, err := CanonicalMigrationSetBodyV1([]MigrationChecksumEntry{{
		Filename: "0062_create_vps_create_idempotency.sql",
		Checksum: appACLConvergenceChecksum(t, "62"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	privilegeBody, err := CanonicalPrivilegeSetBodyV1(appACLCurrentCatalogTestBindings(), nil)
	if err != nil {
		t.Fatal(err)
	}
	previous, err := NewAppACLManifestPersistedV1(1, "houfeng_migrator", [32]byte{}, migrationBody, privilegeBody)
	if err != nil {
		t.Fatal(err)
	}
	currentBody, err := CanonicalMigrationSetBodyV1([]MigrationChecksumEntry{
		{Filename: "0062_create_vps_create_idempotency.sql", Checksum: appACLConvergenceChecksum(t, "62")},
		{Filename: "0063_tune_heartbeat_incident_policy.sql", Checksum: appACLConvergenceChecksum(t, "63")},
	})
	if err != nil {
		t.Fatal(err)
	}
	return previous, currentBody, privilegeBody
}

type appACLManifestSuccessorSQLCall struct {
	sql       string
	arguments []any
}

type recordingAppACLManifestSuccessorTx struct {
	pgx.Tx
	calls  []appACLManifestSuccessorSQLCall
	tags   []pgconn.CommandTag
	errors []error
}

func (tx *recordingAppACLManifestSuccessorTx) Exec(_ context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	index := len(tx.calls)
	tx.calls = append(tx.calls, appACLManifestSuccessorSQLCall{sql: sql, arguments: append([]any(nil), arguments...)})
	var tag pgconn.CommandTag
	if index < len(tx.tags) {
		tag = tx.tags[index]
	}
	if index < len(tx.errors) {
		return tag, tx.errors[index]
	}
	return tag, nil
}

func unexpectedAppACLManifestSuccessorReadManifests(t *testing.T) func(context.Context, pgx.Tx) ([]AppACLManifestPersistedV1, error) {
	t.Helper()
	return func(context.Context, pgx.Tx) ([]AppACLManifestPersistedV1, error) {
		t.Fatal("successor writer must not read revisions after write failure")
		return nil, nil
	}
}

func unexpectedAppACLManifestSuccessorReadHead(t *testing.T) func(context.Context, pgx.Tx) (*AppACLManifestHeadV1, error) {
	t.Helper()
	return func(context.Context, pgx.Tx) (*AppACLManifestHeadV1, error) {
		t.Fatal("successor writer must not read head after earlier failure")
		return nil, nil
	}
}
