package migrate

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestClassifyAppACLR2StateComposesOnlyExactCatalogPredicates(t *testing.T) {
	tests := []struct {
		name       string
		predicates AppACLR2CatalogPredicates
		want       AppACLR2State
	}{
		{
			name:       "exact R1",
			predicates: AppACLR2CatalogPredicates{ExactL1M1: true, L2Absent: true, M2Absent: true},
			want:       AppACLR2StateR1,
		},
		{
			name:       "exact prepared",
			predicates: AppACLR2CatalogPredicates{ExactL1M1: true, ExactL2: true, M2Absent: true},
			want:       AppACLR2StatePrepared,
		},
		{
			name:       "exact finalized",
			predicates: AppACLR2CatalogPredicates{ExactL1M1: true, ExactL2: true, ExactM2: true},
			want:       AppACLR2StateFinalized,
		},
		{
			name:       "missing frozen R1",
			predicates: AppACLR2CatalogPredicates{ExactL2: true, M2Absent: true},
			want:       AppACLR2StateCorrupt,
		},
		{
			name:       "one-sided M2",
			predicates: AppACLR2CatalogPredicates{ExactL1M1: true, ExactL2: true},
			want:       AppACLR2StateCorrupt,
		},
		{
			name:       "mixed L2 and absent evidence",
			predicates: AppACLR2CatalogPredicates{ExactL1M1: true, ExactL2: true, L2Absent: true, M2Absent: true},
			want:       AppACLR2StateCorrupt,
		},
		{
			name:       "mixed M2 and absent evidence",
			predicates: AppACLR2CatalogPredicates{ExactL1M1: true, ExactL2: true, ExactM2: true, M2Absent: true},
			want:       AppACLR2StateCorrupt,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := classifyAppACLR2StateWithDependencies(context.Background(), &fakeAppACLR2StateTx{}, appACLR2StateDependencies{
				readPredicates: func(context.Context, pgx.Tx) (AppACLR2CatalogPredicates, error) {
					return tt.predicates, nil
				},
			})
			if err != nil {
				t.Fatalf("classifyAppACLR2StateWithDependencies() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("classified state = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClassifyAppACLR2StateIsCredentialNeutral(t *testing.T) {
	identities := []struct {
		name        string
		sessionUser string
		currentUser string
	}{
		{name: "center runtime", sessionUser: "center_runtime", currentUser: "center_runtime"},
		{name: "direct migrator", sessionUser: "direct_migrator", currentUser: "direct_migrator"},
		{name: "bootstrap", sessionUser: "postgres", currentUser: "postgres"},
		{name: "platform admin", sessionUser: "platform_admin", currentUser: "platform_admin"},
		{name: "unrelated direct role", sessionUser: "unrelated_login", currentUser: "unrelated_login"},
		{name: "distinct pair", sessionUser: "member_login", currentUser: "center_runtime"},
	}
	fixtures := []struct {
		name       string
		predicates AppACLR2CatalogPredicates
		want       AppACLR2State
	}{
		{name: "R1", predicates: AppACLR2CatalogPredicates{ExactL1M1: true, L2Absent: true, M2Absent: true}, want: AppACLR2StateR1},
		{name: "prepared", predicates: AppACLR2CatalogPredicates{ExactL1M1: true, ExactL2: true, M2Absent: true}, want: AppACLR2StatePrepared},
		{name: "finalized", predicates: AppACLR2CatalogPredicates{ExactL1M1: true, ExactL2: true, ExactM2: true}, want: AppACLR2StateFinalized},
	}

	for _, fixture := range fixtures {
		for _, identity := range identities {
			t.Run(fixture.name+"/"+identity.name, func(t *testing.T) {
				tx := &fakeAppACLR2StateTx{sessionUser: identity.sessionUser, currentUser: identity.currentUser}
				got, err := classifyAppACLR2StateWithDependencies(context.Background(), tx, appACLR2StateDependencies{
					readPredicates: func(_ context.Context, gotTx pgx.Tx) (AppACLR2CatalogPredicates, error) {
						if gotTx != tx {
							t.Fatalf("predicate transaction = %T %p, want caller transaction %p", gotTx, gotTx, tx)
						}
						return fixture.predicates, nil
					},
				})
				if err != nil {
					t.Fatalf("classifyAppACLR2StateWithDependencies() error = %v", err)
				}
				if got != fixture.want {
					t.Fatalf("classified state = %v, want %v", got, fixture.want)
				}
				if len(tx.queries) != 0 {
					t.Fatalf("credential-neutral classifier queried identity: %q", tx.queries)
				}
			})
		}
	}
}

func TestClassifyAppACLR2StatePublicBoundaryRejectsUnknownReservedObject(t *testing.T) {
	tx := newAppACLR2PublicR1Tx(t, "center_runtime", "center_runtime", []AppACLR2ReservedCatalogObjectV1{{
		OID: 9001, Kind: "relation", Schema: "third_party", Identity: "app_acl_r2_extra", Detail: "r",
	}})

	got, err := ClassifyAppACLR2State(context.Background(), tx)
	if err != nil {
		t.Fatalf("ClassifyAppACLR2State() error = %v", err)
	}
	if got != AppACLR2StateCorrupt {
		t.Fatalf("ClassifyAppACLR2State() = %v with an unknown reserved object, want CORRUPT", got)
	}
	tx.assertCallerOwnedAndComplete(t)
}

func TestPublicAppACLR2StateReadersAreCredentialNeutralAndTransactionBound(t *testing.T) {
	identities := []struct {
		name        string
		sessionUser string
		currentUser string
	}{
		{name: "center runtime", sessionUser: "center_runtime", currentUser: "center_runtime"},
		{name: "direct migrator", sessionUser: "direct_migrator", currentUser: "direct_migrator"},
		{name: "bootstrap", sessionUser: "postgres", currentUser: "postgres"},
		{name: "platform admin", sessionUser: "platform_admin", currentUser: "platform_admin"},
		{name: "unrelated direct role", sessionUser: "unrelated_login", currentUser: "unrelated_login"},
		{name: "distinct pair", sessionUser: "member_login", currentUser: "center_runtime"},
	}
	readers := []struct {
		name     string
		classify func(context.Context, pgx.Tx) (AppACLR2State, error)
	}{
		{name: "ClassifyAppACLR2State", classify: ClassifyAppACLR2State},
		{name: "PostgresAppACLR2StateReader", classify: NewPostgresAppACLR2StateReader().ClassifyAppACLR2State},
	}

	for _, reader := range readers {
		for _, identity := range identities {
			t.Run(reader.name+"/"+identity.name, func(t *testing.T) {
				tx := newAppACLR2PublicR1Tx(t, identity.sessionUser, identity.currentUser, nil)
				got, err := reader.classify(context.Background(), tx)
				if err != nil {
					t.Fatalf("%s() error = %v, want the same nil error class for identical evidence", reader.name, err)
				}
				if got != AppACLR2StateR1 {
					t.Fatalf("%s() state = %v, want R1 for identical evidence", reader.name, got)
				}
				tx.assertCallerOwnedAndComplete(t)
			})
		}
	}
}

func TestPublicAppACLR2StateReadersPropagateCatalogBoundaryErrors(t *testing.T) {
	queryErr := errors.New("catalog query failed")
	scanErr := errors.New("catalog scan failed")
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	deadlineContext, deadlineCancel := context.WithDeadline(context.Background(), time.Time{})
	defer deadlineCancel()

	readers := []struct {
		name     string
		classify func(context.Context, pgx.Tx) (AppACLR2State, error)
	}{
		{name: "ClassifyAppACLR2State", classify: ClassifyAppACLR2State},
		{name: "PostgresAppACLR2StateReader", classify: NewPostgresAppACLR2StateReader().ClassifyAppACLR2State},
	}
	failures := []struct {
		name  string
		ctx   context.Context
		newTx func() *appACLR2PublicBoundaryTx
		want  error
	}{
		{name: "query error", ctx: context.Background(), newTx: func() *appACLR2PublicBoundaryTx {
			return &appACLR2PublicBoundaryTx{queryErr: queryErr}
		}, want: queryErr},
		{name: "scan error", ctx: context.Background(), newTx: func() *appACLR2PublicBoundaryTx {
			return &appACLR2PublicBoundaryTx{scanErr: scanErr}
		}, want: scanErr},
		{name: "context canceled", ctx: canceledContext, newTx: func() *appACLR2PublicBoundaryTx {
			return &appACLR2PublicBoundaryTx{}
		}, want: context.Canceled},
		{name: "deadline exceeded", ctx: deadlineContext, newTx: func() *appACLR2PublicBoundaryTx {
			return &appACLR2PublicBoundaryTx{}
		}, want: context.DeadlineExceeded},
	}

	for _, reader := range readers {
		for _, failure := range failures {
			t.Run(reader.name+"/"+failure.name, func(t *testing.T) {
				tx := failure.newTx()
				state, err := reader.classify(failure.ctx, tx)
				if state != AppACLR2StateCorrupt {
					t.Fatalf("%s() state = %v on catalog failure, want CORRUPT zero value with propagated error", reader.name, state)
				}
				if !errors.Is(err, failure.want) {
					t.Fatalf("%s() error = %v, want wrapped %v", reader.name, err, failure.want)
				}
				if tx.beginCalls != 0 || tx.commitCalls != 0 || tx.rollbackCalls != 0 {
					t.Fatalf("%s() transaction ownership = begin:%d commit:%d rollback:%d, want all zero", reader.name, tx.beginCalls, tx.commitCalls, tx.rollbackCalls)
				}
			})
		}
	}
}

func TestPublicAppACLR2StateReadersTreatPartialEvidencePermissionErrorsAsErrorOnly(t *testing.T) {
	tests := []struct {
		name          string
		object        AppACLR2ReservedCatalogObjectV1
		queryFragment string
	}{
		{
			name: "partial L2 receipt",
			object: AppACLR2ReservedCatalogObjectV1{
				OID: 1001, Kind: "relation", Schema: "public", Identity: "app_acl_r2_bootstrap_receipt", Detail: "r",
			},
			queryFragment: "from public.app_acl_r2_bootstrap_receipt",
		},
		{
			name: "partial M2 revisions",
			object: AppACLR2ReservedCatalogObjectV1{
				OID: 2001, Kind: "relation", Schema: "public", Identity: "app_acl_r2_manifest_revisions", Detail: "r",
			},
			queryFragment: "from public.app_acl_r2_manifest_revisions",
		},
		{
			name: "partial M2 head",
			object: AppACLR2ReservedCatalogObjectV1{
				OID: 2002, Kind: "relation", Schema: "public", Identity: "app_acl_r2_manifest_head", Detail: "r",
			},
			queryFragment: "from public.app_acl_r2_manifest_head",
		},
	}
	readers := []struct {
		name     string
		classify func(context.Context, pgx.Tx) (AppACLR2State, error)
	}{
		{name: "ClassifyAppACLR2State", classify: ClassifyAppACLR2State},
		{name: "PostgresAppACLR2StateReader", classify: NewPostgresAppACLR2StateReader().ClassifyAppACLR2State},
	}

	for _, reader := range readers {
		for _, tt := range tests {
			t.Run(reader.name+"/"+tt.name, func(t *testing.T) {
				permissionErr := &pgconn.PgError{Code: "42501", Message: "permission denied"}
				tx := newAppACLR2PublicR1Tx(t, "platform_admin", "platform_admin", []AppACLR2ReservedCatalogObjectV1{tt.object})
				tx.queryErrorFragment = tt.queryFragment
				tx.queryError = permissionErr

				state, err := reader.classify(context.Background(), tx)
				if state != AppACLR2StateCorrupt {
					t.Fatalf("%s() state = %v on permission failure, want zero value", reader.name, state)
				}
				var pgErr *pgconn.PgError
				if !errors.As(err, &pgErr) {
					t.Fatalf("%s() error = %v, want recognizable PostgreSQL permission error; zero-value state is not a verdict", reader.name, err)
				}
				if pgErr != permissionErr || pgErr.Code != "42501" {
					t.Fatalf("%s() PostgreSQL error = %#v, want original %#v", reader.name, pgErr, permissionErr)
				}
			})
		}
	}
}

func TestPublicAppACLR2StateReadersTreatWrongRelkindPermissionErrorsAsErrorOnly(t *testing.T) {
	tests := []struct {
		name           string
		object         AppACLR2ReservedCatalogObjectV1
		authorityProbe string
	}{
		{
			name: "wrong-relkind L2 receipt",
			object: AppACLR2ReservedCatalogObjectV1{
				OID: 1001, Kind: "relation", Schema: "public", Identity: "app_acl_r2_bootstrap_receipt", Detail: "v",
			},
			authorityProbe: "select * from public.app_acl_r2_bootstrap_receipt limit 0",
		},
		{
			name: "wrong-relkind M2 revisions",
			object: AppACLR2ReservedCatalogObjectV1{
				OID: 2001, Kind: "relation", Schema: "public", Identity: "app_acl_r2_manifest_revisions", Detail: "v",
			},
			authorityProbe: "select * from public.app_acl_r2_manifest_revisions limit 0",
		},
		{
			name: "wrong-relkind M2 head",
			object: AppACLR2ReservedCatalogObjectV1{
				OID: 2002, Kind: "relation", Schema: "public", Identity: "app_acl_r2_manifest_head", Detail: "v",
			},
			authorityProbe: "select * from public.app_acl_r2_manifest_head limit 0",
		},
	}
	readers := []struct {
		name     string
		classify func(context.Context, pgx.Tx) (AppACLR2State, error)
	}{
		{name: "ClassifyAppACLR2State", classify: ClassifyAppACLR2State},
		{name: "PostgresAppACLR2StateReader", classify: NewPostgresAppACLR2StateReader().ClassifyAppACLR2State},
	}

	for _, reader := range readers {
		for _, tt := range tests {
			t.Run(reader.name+"/"+tt.name, func(t *testing.T) {
				permissionErr := &pgconn.PgError{Code: "42501", Message: "permission denied"}
				tx := newAppACLR2PublicR1Tx(t, "platform_admin", "platform_admin", []AppACLR2ReservedCatalogObjectV1{tt.object})
				tx.queryErrorFragment = tt.authorityProbe
				tx.queryError = permissionErr

				state, err := reader.classify(context.Background(), tx)
				if !tx.queried(tt.authorityProbe) {
					t.Fatalf("%s() did not issue native authority probe %q for wrong-relkind evidence", reader.name, tt.authorityProbe)
				}
				if state != AppACLR2StateCorrupt {
					t.Fatalf("%s() state = %v on permission failure, want zero value", reader.name, state)
				}
				if err == nil {
					t.Fatalf("%s() error = nil, want PostgreSQL permission error; zero-value state is not a verdict", reader.name)
				}
				var pgErr *pgconn.PgError
				if !errors.As(err, &pgErr) {
					t.Fatalf("%s() error = %v, want recognizable PostgreSQL permission error; zero-value state is not a verdict", reader.name, err)
				}
				if pgErr != permissionErr || pgErr.Code != "42501" {
					t.Fatalf("%s() PostgreSQL error = %#v, want original %#v", reader.name, pgErr, permissionErr)
				}
				if tx.beginCalls != 0 || tx.commitCalls != 0 || tx.rollbackCalls != 0 {
					t.Fatalf("%s() transaction ownership = begin:%d commit:%d rollback:%d, want all zero", reader.name, tx.beginCalls, tx.commitCalls, tx.rollbackCalls)
				}
				if len(tx.identityQueries) != 0 {
					t.Fatalf("credential-neutral %s queried connection identity: %q", reader.name, tx.identityQueries)
				}
			})
		}
	}
}

func TestPublicAppACLR2StateReadersClassifyStructuralDriftAsCorrupt(t *testing.T) {
	structuralDriftTransactions := []struct {
		name  string
		newTx func(*testing.T) *appACLR2PublicR1Tx
	}{
		{name: "frozen M1 revision inventory", newTx: newAppACLR2PublicFrozenM1StructuralDriftTx},
		{name: "M2 relation owner", newTx: newAppACLR2PublicM2StructuralDriftTx},
	}
	readers := []struct {
		name     string
		classify func(context.Context, pgx.Tx) (AppACLR2State, error)
	}{
		{name: "ClassifyAppACLR2State", classify: ClassifyAppACLR2State},
		{name: "PostgresAppACLR2StateReader", classify: NewPostgresAppACLR2StateReader().ClassifyAppACLR2State},
	}

	for _, reader := range readers {
		for _, drift := range structuralDriftTransactions {
			t.Run(reader.name+"/"+drift.name, func(t *testing.T) {
				state, err := reader.classify(context.Background(), drift.newTx(t))
				if err != nil {
					t.Fatalf("%s() structural drift error = %v, want nil", reader.name, err)
				}
				if state != AppACLR2StateCorrupt {
					t.Fatalf("%s() structural drift state = %v, want CORRUPT", reader.name, state)
				}
			})
		}
	}
}

func newAppACLR2PublicFrozenM1StructuralDriftTx(t *testing.T) *appACLR2PublicR1Tx {
	t.Helper()
	tx := newAppACLR2PublicR1Tx(t, "center_runtime", "center_runtime", nil)
	reservedRows := tx.queryRows[len(tx.queryRows)-1]
	ledgerRows := tx.queryRows[1]
	tx.queryRows = [][][]any{nil, ledgerRows, reservedRows}
	tx.queryRowValues = tx.queryRowValues[:2]
	return tx
}

func newAppACLR2PublicM2StructuralDriftTx(t *testing.T) *appACLR2PublicR1Tx {
	t.Helper()
	objects := appACLR2M2ReservedObjects()
	for index := range objects {
		objects[index].OID = uint32(2000 + index)
	}
	tx := newAppACLR2PublicR1Tx(t, "center_runtime", "center_runtime", objects)
	shape := validAppACLR2FinalizedCatalogShape(t)
	tx.queryRows = append(tx.queryRows,
		nil,
		[][]any{appACLR2M2ManifestReaderRow(shape.M2Revisions[0])},
		nil,
		[][]any{appACLR2M2HeadReaderRow(shape.M2Heads[0])},
		[][]any{
			{shape.FrozenState.CenterRuntimeRole, int64(20)},
			{shape.FrozenState.DirectMigratorRole, int64(21)},
			{shape.FrozenState.PlatformAdminRole, int64(22)},
		},
		[][]any{
			{"app_acl_r2_manifest_head", int64(999), "r"},
			{"app_acl_r2_manifest_revisions", int64(21), "r"},
		},
	)
	return tx
}

func TestAppACLR2CatalogAndStateSourcesDoNotReadConnectionIdentity(t *testing.T) {
	for _, name := range []string{"app_acl_r2_catalog.go", "app_acl_r2_state.go"} {
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, forbidden := range []string{"session_user", "current_user", "AdmitAppACLRuntime("} {
			if strings.Contains(string(source), forbidden) {
				t.Fatalf("%s contains forbidden dependency %q", name, forbidden)
			}
		}
	}
}

type fakeAppACLR2StateTx struct {
	pgx.Tx
	sessionUser string
	currentUser string
	queries     []string
}
