package migrate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestAppACLR2M2ControlACLBindsDirectOwnerAndOrdinaryReaderPolicySeparately(t *testing.T) {
	const directMigratorOID = uint32(4242)
	contract := appACLR2ControlACLContract(directMigratorOID)
	if len(contract.Objects) != 3 {
		t.Fatalf("M2 control object count = %d, want two direct-owned relations and one direct-owned immutable trigger function", len(contract.Objects))
	}

	for _, object := range contract.Objects {
		if object.OwnerRole != AppACLControlRoleDirectMigratorR2 || object.OwnerOID != directMigratorOID {
			t.Fatalf("M2 object %q owner = role %d/OID %d, want direct-migrator role/OID %d", object.Identity, object.OwnerRole, object.OwnerOID, directMigratorOID)
		}
		switch object.Kind {
		case AppACLControlObjectTableR2:
			want := []AppACLControlGrantR2V1{
				{GranteeRole: AppACLControlRoleDirectMigratorR2, Privilege: AppACLControlPrivilegeSelectR2},
				{GranteeRole: AppACLControlRoleCenterRuntimeR2, Privilege: AppACLControlPrivilegeSelectR2},
			}
			if !reflect.DeepEqual(object.ExplicitGrants, want) || object.EffectiveRelevantPrivilegeMask != 0x06 {
				t.Fatalf("M2 table %q ACL = grants %#v/mask %#x, want direct/runtime SELECT and ordinary-reader mask 0x06", object.Identity, object.ExplicitGrants, object.EffectiveRelevantPrivilegeMask)
			}
		case AppACLControlObjectFunctionR2:
			want := []AppACLControlGrantR2V1{{GranteeRole: AppACLControlRoleDirectMigratorR2, Privilege: AppACLControlPrivilegeExecuteR2}}
			if !reflect.DeepEqual(object.ExplicitGrants, want) || object.EffectiveRelevantPrivilegeMask != 0x02 {
				t.Fatalf("M2 helper %q ACL = grants %#v/mask %#x, want direct-owner EXECUTE and ordinary-reader mask 0x02", object.Identity, object.ExplicitGrants, object.EffectiveRelevantPrivilegeMask)
			}
		default:
			t.Fatalf("M2 control ACL contains unexpected object kind %d for %q", object.Kind, object.Identity)
		}
	}
	for _, trigger := range contract.Triggers {
		if trigger.TableOwnerOID != directMigratorOID || trigger.FunctionOwnerOID != directMigratorOID {
			t.Fatalf("M2 trigger %q owner binding = table %d/function %d, want direct-migrator OID %d", trigger.TriggerName, trigger.TableOwnerOID, trigger.FunctionOwnerOID, directMigratorOID)
		}
	}
	for _, assertion := range contract.DefaultACLAssertions {
		if assertion.OwnerRole != AppACLControlRoleDirectMigratorR2 {
			t.Fatalf("M2 default ACL assertion owner = %d, want direct-migrator role", assertion.OwnerRole)
		}
	}
}

func TestAppACLR2CatalogPredicatesRecognizeExactR1ObjectAbsence(t *testing.T) {
	predicates := evaluateAppACLR2CatalogShape(appACLR2CatalogShape{})

	if !predicates.L2Absent {
		t.Fatal("L2 absence predicate = false, want true")
	}
	if !predicates.M2Absent {
		t.Fatal("M2 absence predicate = false, want true")
	}
	if predicates.ExactL2 || predicates.ExactM2 {
		t.Fatalf("empty R2 inventory exact predicates = L2:%t M2:%t, want both false", predicates.ExactL2, predicates.ExactM2)
	}
}

func TestAppACLR2CatalogPredicatesRejectUnknownReservedObjectFromExactR1(t *testing.T) {
	shape := appACLR2CatalogShape{
		FrozenExact: true,
		ReservedObjects: []AppACLR2ReservedCatalogObjectV1{{
			OID: 9001, Kind: "relation", Schema: "third_party", Identity: "app_acl_r2_extra", Detail: "r",
		}},
	}

	predicates := evaluateAppACLR2CatalogShape(shape)
	if predicates.ExactR1() {
		t.Fatal("exact R1 predicate = true with an unknown reserved object, want false")
	}
}

func TestAppACLR2CatalogPredicatesRejectUnknownReservedObjectFromExactPrepared(t *testing.T) {
	predicates := AppACLR2CatalogPredicates{
		ExactL1M1:                 true,
		ExactL2:                   true,
		M2Absent:                  true,
		HasUnknownReservedObjects: true,
	}

	if predicates.ExactPrepared() {
		t.Fatal("exact PREPARED predicate = true with an unknown reserved object, want false")
	}
}

func TestAppACLR2CatalogPredicatesRejectUnknownReservedObjectFromExactFinalized(t *testing.T) {
	predicates := AppACLR2CatalogPredicates{
		ExactL1M1:                 true,
		ExactL2:                   true,
		ExactM2:                   true,
		HasUnknownReservedObjects: true,
	}

	if predicates.ExactFinalized() {
		t.Fatal("exact FINALIZED predicate = true with an unknown reserved object, want false")
	}
}

func TestAppACLR2CatalogPredicatesRecognizeCanonicalPG16FinalizedInventory(t *testing.T) {
	shape := validAppACLR2FinalizedCatalogShape(t)
	if got := len(shape.ReservedObjects); got != 13 {
		t.Fatalf("canonical PG16 finalized reserved-object count = %d, want 13", got)
	}

	predicates := evaluateAppACLR2CatalogShape(shape)
	if !predicates.ExactFinalized() {
		t.Fatalf(
			"canonical PG16 finalized predicates = L1M1:%t L2:%t M2:%t unknown:%t, want exact finalized",
			predicates.ExactL1M1,
			predicates.ExactL2,
			predicates.ExactM2,
			predicates.HasUnknownReservedObjects,
		)
	}
	state, err := classifyAppACLR2StateWithDependencies(context.Background(), &fakeAppACLR2CatalogPredicateTx{}, appACLR2StateDependencies{
		readPredicates: func(context.Context, pgx.Tx) (AppACLR2CatalogPredicates, error) {
			return predicates, nil
		},
	})
	if err != nil {
		t.Fatalf("classifyAppACLR2StateWithDependencies() error = %v", err)
	}
	if state != AppACLR2StateFinalized {
		t.Fatalf("classified state = %v, want FINALIZED", state)
	}
}

func TestAppACLR2CatalogPredicatesClassifyExactPreparedWithoutM2(t *testing.T) {
	shape := validAppACLR2FinalizedCatalogShape(t)
	shape.ReservedObjects = appACLR2FilterReservedObjects(shape.ReservedObjects, appACLR2L2ReservedObjects())
	shape.M2Revisions = nil
	shape.M2Heads = nil
	shape.M2ControlACL = AppACLControlACLBodyR2V1{}

	predicates := evaluateAppACLR2CatalogShape(shape)
	if !predicates.ExactL1M1 {
		t.Fatal("exact PREPARED fixture did not retain frozen L1/M1 evidence")
	}
	if !predicates.ExactL2 {
		t.Fatal("exact PREPARED fixture L2 predicate = false, want true without M2")
	}
	if !predicates.M2Absent {
		t.Fatal("exact PREPARED fixture M2 absence predicate = false, want true")
	}
	if !predicates.ExactPrepared() {
		t.Fatalf("exact PREPARED predicates = %#v, want exact PREPARED", predicates)
	}
	state, err := classifyAppACLR2StateWithDependencies(context.Background(), &fakeAppACLR2CatalogPredicateTx{}, appACLR2StateDependencies{
		readPredicates: func(context.Context, pgx.Tx) (AppACLR2CatalogPredicates, error) {
			return predicates, nil
		},
	})
	if err != nil {
		t.Fatalf("classifyAppACLR2StateWithDependencies() error = %v", err)
	}
	if state != AppACLR2StatePrepared {
		t.Fatalf("classified state = %v, want PREPARED", state)
	}
}

func TestAppACLR2CatalogPredicatesRejectExtraPrefixedIndexAsCorrupt(t *testing.T) {
	shape := validAppACLR2FinalizedCatalogShape(t)
	shape.ReservedObjects = append(shape.ReservedObjects, AppACLR2ReservedCatalogObjectV1{
		OID: 9000, Kind: "relation", Schema: "public", Identity: "app_acl_r2_manifest_revisions_extra_idx", Detail: "i",
	})

	predicates := evaluateAppACLR2CatalogShape(shape)
	state, err := classifyAppACLR2StateWithDependencies(context.Background(), &fakeAppACLR2CatalogPredicateTx{}, appACLR2StateDependencies{
		readPredicates: func(context.Context, pgx.Tx) (AppACLR2CatalogPredicates, error) {
			return predicates, nil
		},
	})
	if err != nil {
		t.Fatalf("classifyAppACLR2StateWithDependencies() error = %v", err)
	}
	if state != AppACLR2StateCorrupt {
		t.Fatalf("classified state = %v with an extra prefixed index, want CORRUPT", state)
	}
}

func TestAppACLR2CatalogPredicateReaderPropagatesL2OperationalError(t *testing.T) {
	shape := validAppACLR2FinalizedCatalogShape(t)
	shape.ReservedObjects = append([]AppACLR2ReservedCatalogObjectV1(nil), appACLR2ReservedCatalogObjectFixture()...)
	operationErr := errors.New("receipt catalog query canceled")

	_, err := readAppACLR2CatalogPredicatesInTxWithDependencies(context.Background(), &fakeAppACLR2CatalogPredicateTx{}, appACLR2CatalogPredicateReadDependencies{
		verifyFrozen: func(context.Context, pgx.Tx) (FrozenAppACLR1StateV1, error) {
			return shape.FrozenState, nil
		},
		readReservedObjects: func(context.Context, pgx.Tx) ([]AppACLR2ReservedCatalogObjectV1, error) {
			return append([]AppACLR2ReservedCatalogObjectV1(nil), shape.ReservedObjects...), nil
		},
		readL2Rows: func(context.Context, pgx.Tx) ([]appACLR2ReceiptRowV1, error) {
			return append([]appACLR2ReceiptRowV1(nil), shape.L2Rows...), nil
		},
		verifyL2: func(context.Context, pgx.Tx, FrozenAppACLR1StateV1, appACLR2ReceiptRowV1) error {
			return operationErr
		},
		readM2Revisions: func(context.Context, pgx.Tx) ([]appACLR2ManifestRowV1, error) {
			return nil, nil
		},
		readM2Heads: func(context.Context, pgx.Tx) ([]appACLR2ManifestHeadRowV1, error) {
			return nil, nil
		},
		readM2ControlACL: func(context.Context, pgx.Tx, FrozenAppACLR1StateV1) (AppACLControlACLBodyR2V1, error) {
			return AppACLControlACLBodyR2V1{}, nil
		},
	})
	if !errors.Is(err, operationErr) {
		t.Fatalf("readAppACLR2CatalogPredicatesInTxWithDependencies() error = %v, want wrapped operational error", err)
	}
}

func TestReadAppACLR2CatalogPredicatesInTxPropagatesPermissionDeniedForPresentPartialEvidenceRelation(t *testing.T) {
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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			permissionErr := &pgconn.PgError{Code: "42501", Message: "permission denied"}
			tx := newAppACLR2PublicR1Tx(t, "platform_admin", "platform_admin", []AppACLR2ReservedCatalogObjectV1{tt.object})
			tx.queryErrorFragment = tt.queryFragment
			tx.queryError = permissionErr

			_, err := ReadAppACLR2CatalogPredicatesInTx(context.Background(), tx)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) {
				t.Fatalf("ReadAppACLR2CatalogPredicatesInTx() error = %v, want recognizable PostgreSQL permission error", err)
			}
			if pgErr != permissionErr || pgErr.Code != "42501" {
				t.Fatalf("ReadAppACLR2CatalogPredicatesInTx() PostgreSQL error = %#v, want original %#v", pgErr, permissionErr)
			}
		})
	}
}

func TestReadAppACLR2CatalogPredicatesInTxPropagatesStructuralSQLStateBeforeLaterEvidenceRead(t *testing.T) {
	const revisionsAuthorityProbe = "select * from public.app_acl_r2_manifest_revisions limit 0"
	const headAuthorityProbe = "select * from public.app_acl_r2_manifest_head limit 0"

	for _, tt := range []struct {
		name string
		code string
	}{
		{name: "undefined table", code: "42P01"},
		{name: "undefined column", code: "42703"},
		{name: "undefined object", code: "42704"},
		{name: "wrong object type", code: "42809"},
		{name: "cannot coerce", code: "42846"},
		{name: "undefined function", code: "42883"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			originalErr := &pgconn.PgError{Code: tt.code, Message: "catalog structural SQLSTATE"}
			abortedTxErr := &pgconn.PgError{Code: "25P02", Message: "current transaction is aborted"}
			baseTx := newAppACLR2PublicR1Tx(t, "center_runtime", "center_runtime", []AppACLR2ReservedCatalogObjectV1{
				{OID: 2001, Kind: "relation", Schema: "public", Identity: "app_acl_r2_manifest_revisions", Detail: "r"},
				{OID: 2002, Kind: "relation", Schema: "public", Identity: "app_acl_r2_manifest_head", Detail: "r"},
			})
			tx := &appACLR2AbortAfterCatalogQueryTx{
				appACLR2PublicR1Tx:    baseTx,
				originalQueryFragment: revisionsAuthorityProbe,
				originalErr:           originalErr,
				abortedTxErr:          abortedTxErr,
			}

			_, err := ReadAppACLR2CatalogPredicatesInTx(context.Background(), tx)
			if !tx.originalObserved {
				t.Errorf("M2 revisions authority probe %q did not run", revisionsAuthorityProbe)
			}
			if tx.laterQueryCalls != 0 || baseTx.queried(headAuthorityProbe) {
				t.Errorf(
					"catalog reader continued after original SQLSTATE %s: later_calls=%d head_probe=%t",
					tt.code,
					tx.laterQueryCalls,
					baseTx.queried(headAuthorityProbe),
				)
			}
			var gotPgErr *pgconn.PgError
			if !errors.As(err, &gotPgErr) {
				t.Errorf("ReadAppACLR2CatalogPredicatesInTx() error = %v, want original PostgreSQL error", err)
			} else if gotPgErr != originalErr || gotPgErr.Code != tt.code {
				t.Errorf("ReadAppACLR2CatalogPredicatesInTx() PostgreSQL error = %#v, want original %#v", gotPgErr, originalErr)
			}
		})
	}
}

func TestReadAppACLR2CatalogPredicatesInTxProbesWrongRelkindBeforePropagatingPermissionDenied(t *testing.T) {
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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			permissionErr := &pgconn.PgError{Code: "42501", Message: "permission denied"}
			tx := newAppACLR2PublicR1Tx(t, "platform_admin", "platform_admin", []AppACLR2ReservedCatalogObjectV1{tt.object})
			tx.queryErrorFragment = tt.authorityProbe
			tx.queryError = permissionErr

			_, err := ReadAppACLR2CatalogPredicatesInTx(context.Background(), tx)
			if !tx.queried(tt.authorityProbe) {
				t.Fatalf("wrong-relkind evidence did not issue native authority probe %q", tt.authorityProbe)
			}
			if err == nil {
				t.Fatal("ReadAppACLR2CatalogPredicatesInTx() error = nil, want PostgreSQL permission error")
			}
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) {
				t.Fatalf("ReadAppACLR2CatalogPredicatesInTx() error = %v, want recognizable PostgreSQL permission error", err)
			}
			if pgErr != permissionErr || pgErr.Code != "42501" {
				t.Fatalf("ReadAppACLR2CatalogPredicatesInTx() PostgreSQL error = %#v, want original %#v", pgErr, permissionErr)
			}
			if tx.beginCalls != 0 || tx.commitCalls != 0 || tx.rollbackCalls != 0 {
				t.Fatalf("public catalog reader transaction ownership = begin:%d commit:%d rollback:%d, want all zero", tx.beginCalls, tx.commitCalls, tx.rollbackCalls)
			}
			if len(tx.identityQueries) != 0 {
				t.Fatalf("credential-neutral public catalog reader queried connection identity: %q", tx.identityQueries)
			}
		})
	}
}

func TestReadAppACLR2CatalogPredicatesInTxPropagatesPublicBoundaryErrors(t *testing.T) {
	queryErr := errors.New("catalog query failed")
	scanErr := errors.New("catalog scan failed")
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	deadlineContext, deadlineCancel := context.WithDeadline(context.Background(), time.Time{})
	defer deadlineCancel()

	tests := []struct {
		name string
		ctx  context.Context
		tx   *appACLR2PublicBoundaryTx
		want error
	}{
		{name: "query error", ctx: context.Background(), tx: &appACLR2PublicBoundaryTx{queryErr: queryErr}, want: queryErr},
		{name: "scan error", ctx: context.Background(), tx: &appACLR2PublicBoundaryTx{scanErr: scanErr}, want: scanErr},
		{name: "context canceled", ctx: canceledContext, tx: &appACLR2PublicBoundaryTx{}, want: context.Canceled},
		{name: "deadline exceeded", ctx: deadlineContext, tx: &appACLR2PublicBoundaryTx{}, want: context.DeadlineExceeded},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ReadAppACLR2CatalogPredicatesInTx(tt.ctx, tt.tx)
			if !errors.Is(err, tt.want) {
				t.Fatalf("ReadAppACLR2CatalogPredicatesInTx() error = %v, want wrapped %v", err, tt.want)
			}
			tx := tt.tx
			if tx.beginCalls != 0 || tx.commitCalls != 0 || tx.rollbackCalls != 0 {
				t.Fatalf("public catalog reader transaction ownership = begin:%d commit:%d rollback:%d, want all zero", tx.beginCalls, tx.commitCalls, tx.rollbackCalls)
			}
		})
	}
}

func TestReadAppACLR2CatalogPredicatesInTxPinsSearchPathBeforeCatalogEvidence(t *testing.T) {
	const wantSearchPath = "SET LOCAL search_path = pg_catalog, public"
	callerSearchPaths := []string{"pg_catalog", "pg_catalog, public"}

	var first AppACLR2CatalogPredicates
	for index, callerSearchPath := range callerSearchPaths {
		t.Run(callerSearchPath, func(t *testing.T) {
			tx := newAppACLR2PublicR1Tx(t, "center_runtime", "center_runtime", nil)
			tx.searchPath = callerSearchPath

			got, err := ReadAppACLR2CatalogPredicatesInTx(context.Background(), tx)
			if err != nil {
				t.Fatalf("ReadAppACLR2CatalogPredicatesInTx() error = %v", err)
			}
			if !got.ExactR1() {
				t.Fatalf("catalog predicates = %#v, want exact R1", got)
			}
			if index == 0 {
				first = got
			} else if !reflect.DeepEqual(got, first) {
				t.Fatalf("catalog predicates with caller search_path %q = %#v, want same evidence as %q: %#v", callerSearchPath, got, callerSearchPaths[0], first)
			}
			if !reflect.DeepEqual(tx.execSQL, []string{wantSearchPath}) {
				t.Fatalf("search-path statements = %#v, want %#v", tx.execSQL, []string{wantSearchPath})
			}
			if len(tx.catalogSearchPaths) == 0 {
				t.Fatal("catalog reader made no catalog observation")
			}
			for observation, gotSearchPath := range tx.catalogSearchPaths {
				if gotSearchPath != "pg_catalog, public" {
					t.Fatalf("catalog/deparse observation %d search_path = %q, want %q", observation, gotSearchPath, "pg_catalog, public")
				}
			}
			tx.assertCallerOwnedAndComplete(t)
		})
	}
}

func TestReadAppACLR2CatalogPredicatesInTxPropagatesSearchPathPinFailureBeforeCatalogEvidence(t *testing.T) {
	pinErr := errors.New("set local search path denied")
	tx := &appACLR2PublicBoundaryTx{execErr: pinErr}

	_, err := ReadAppACLR2CatalogPredicatesInTx(context.Background(), tx)
	if !errors.Is(err, pinErr) {
		t.Fatalf("ReadAppACLR2CatalogPredicatesInTx() error = %v, want wrapped %v", err, pinErr)
	}
	if !reflect.DeepEqual(tx.execSQL, []string{"SET LOCAL search_path = pg_catalog, public"}) {
		t.Fatalf("search-path statements = %#v, want exact one pin", tx.execSQL)
	}
	if tx.queryCalls != 0 || tx.queryRowCalls != 0 {
		t.Fatalf("catalog queries after search-path pin failure = Query:%d QueryRow:%d, want none", tx.queryCalls, tx.queryRowCalls)
	}
	if tx.beginCalls != 0 || tx.commitCalls != 0 || tx.rollbackCalls != 0 {
		t.Fatalf("public catalog reader transaction ownership = begin:%d commit:%d rollback:%d, want all zero", tx.beginCalls, tx.commitCalls, tx.rollbackCalls)
	}
}

func TestVerifyAppACLR2M2RelationsRejectsColumnACL(t *testing.T) {
	tx := &appACLR2M2ColumnACLProbeTx{}
	err := verifyAppACLR2M2RelationsInTx(context.Background(), tx, FrozenAppACLR1StateV1{}, appACLR2M2RoleOIDs{
		DirectMigrator: 21,
		CenterRuntime:  20,
		PlatformAdmin:  22,
	})
	if err == nil || !strings.Contains(err.Error(), "column ACL") {
		t.Fatalf("verifyAppACLR2M2RelationsInTx() error = %v, want column ACL rejection", err)
	}
	if !tx.columnACLRead {
		t.Fatal("M2 relation verifier did not query column ACLs")
	}
}

func TestVerifyAppACLR2M2RelationColumnACLsRejectDroppedColumnResidue(t *testing.T) {
	tx := &appACLR2M2DroppedColumnACLProbeTx{}
	err := verifyAppACLR2M2RelationColumnACLsInTx(context.Background(), tx, []string{
		"app_acl_r2_manifest_head",
		"app_acl_r2_manifest_revisions",
	})
	if strings.Contains(strings.ToLower(tx.query), "attisdropped") {
		t.Fatalf("M2 column ACL query filters dropped-column residue: %q", tx.query)
	}
	if err == nil || !strings.Contains(err.Error(), "column ACL") {
		t.Fatalf("verifyAppACLR2M2RelationColumnACLsInTx() error = %v, want dropped-column ACL rejection", err)
	}
}

func TestVerifyAppACLR2M2RelationColumnsRejectDroppedColumnResidue(t *testing.T) {
	tx := &appACLR2M2DroppedColumnResidueProbeTx{}
	err := verifyAppACLR2M2RelationColumnsInTx(context.Background(), tx, []string{
		"app_acl_r2_manifest_head",
		"app_acl_r2_manifest_revisions",
	})
	if strings.Contains(strings.ToLower(tx.query), "attisdropped") {
		t.Fatalf("M2 physical column query filters dropped-column residue: %q", tx.query)
	}
	if err == nil || !strings.Contains(err.Error(), "columns") {
		t.Fatalf("verifyAppACLR2M2RelationColumnsInTx() error = %v, want dropped-column exactness rejection", err)
	}
}

func TestVerifyAppACLR2M2RelationGrantsRequireOwnerSelfACL(t *testing.T) {
	roles := appACLR2M2RoleOIDs{DirectMigrator: 21, CenterRuntime: 20, PlatformAdmin: 22}
	tx := &appACLR2M2ACLQueryCaptureTx{relationGrantRows: [][]any{
		{"app_acl_r2_manifest_head", int64(roles.DirectMigrator), int64(roles.DirectMigrator), "SELECT", false},
		{"app_acl_r2_manifest_head", int64(roles.DirectMigrator), int64(roles.CenterRuntime), "SELECT", false},
		{"app_acl_r2_manifest_revisions", int64(roles.DirectMigrator), int64(roles.DirectMigrator), "SELECT", false},
		{"app_acl_r2_manifest_revisions", int64(roles.DirectMigrator), int64(roles.CenterRuntime), "SELECT", false},
	}}
	err := verifyAppACLR2M2RelationGrantsInTx(context.Background(), tx, []string{
		"app_acl_r2_manifest_head",
		"app_acl_r2_manifest_revisions",
	}, roles)
	if err != nil {
		t.Fatalf("verifyAppACLR2M2RelationGrantsInTx() error = %v, want exact direct-migrator and center-runtime SELECT rows", err)
	}
	if strings.Contains(tx.relationGrantQuery, "acl_grant.grantee <> relation.relowner") {
		t.Fatalf("M2 relation grant query filters direct-migrator self ACL rows: %q", tx.relationGrantQuery)
	}
}

func TestVerifyAppACLR2M2RelationGrantsRejectsRevokedOwnerSelfACL(t *testing.T) {
	roles := appACLR2M2RoleOIDs{DirectMigrator: 21, CenterRuntime: 20, PlatformAdmin: 22}
	tx := &appACLR2M2ACLQueryCaptureTx{relationGrantRows: [][]any{
		{"app_acl_r2_manifest_head", int64(roles.DirectMigrator), int64(roles.CenterRuntime), "SELECT", false},
		{"app_acl_r2_manifest_revisions", int64(roles.DirectMigrator), int64(roles.CenterRuntime), "SELECT", false},
	}}
	err := verifyAppACLR2M2RelationGrantsInTx(context.Background(), tx, []string{
		"app_acl_r2_manifest_head",
		"app_acl_r2_manifest_revisions",
	}, roles)
	if err == nil || !strings.Contains(err.Error(), "explicit SELECT grants") {
		t.Fatalf("verifyAppACLR2M2RelationGrantsInTx() error = %v, want revoked direct-migrator self SELECT rejection", err)
	}
}

func TestVerifyAppACLR2M2RelationEffectivePrivilegesRequireDirectOwnerNativeVector(t *testing.T) {
	roles := appACLR2M2RoleOIDs{DirectMigrator: 21, CenterRuntime: 20, PlatformAdmin: 22}
	names := []string{"app_acl_r2_manifest_head", "app_acl_r2_manifest_revisions"}
	directOwner := [7]bool{true, true, true, true, true, true, true}
	runtimeReader := [7]bool{true, false, false, false, false, false, false}
	noPrivileges := [7]bool{}
	tx := &appACLR2M2RelationEffectivePrivilegeTx{
		rows: [][]any{
			appACLR2M2RelationEffectivePrivilegeRow(names[0], directOwner, runtimeReader, noPrivileges),
			appACLR2M2RelationEffectivePrivilegeRow(names[1], directOwner, runtimeReader, noPrivileges),
		},
		wantArgs: []any{int64(roles.DirectMigrator), int64(roles.CenterRuntime), int64(roles.PlatformAdmin), names},
	}
	if err := verifyAppACLR2M2RelationEffectivePrivilegesInTx(context.Background(), tx, names, roles); err != nil {
		t.Fatalf("verifyAppACLR2M2RelationEffectivePrivilegesInTx() error = %v, want all seven direct-owner privileges and runtime SELECT only", err)
	}
	if !strings.Contains(tx.query, "has_table_privilege($1::pg_catalog.oid") {
		t.Fatalf("M2 effective table privilege query does not start with direct-migrator probe: %q", tx.query)
	}

	for index, privilege := range []string{"SELECT", "INSERT", "UPDATE", "DELETE", "TRUNCATE", "REFERENCES", "TRIGGER"} {
		t.Run("missing direct owner "+privilege, func(t *testing.T) {
			missing := directOwner
			missing[index] = false
			tx := &appACLR2M2RelationEffectivePrivilegeTx{
				rows: [][]any{
					appACLR2M2RelationEffectivePrivilegeRow(names[0], missing, runtimeReader, noPrivileges),
					appACLR2M2RelationEffectivePrivilegeRow(names[1], missing, runtimeReader, noPrivileges),
				},
				wantArgs: []any{int64(roles.DirectMigrator), int64(roles.CenterRuntime), int64(roles.PlatformAdmin), names},
			}
			if err := verifyAppACLR2M2RelationEffectivePrivilegesInTx(context.Background(), tx, names, roles); err == nil || !strings.Contains(err.Error(), "effective privilege drift") {
				t.Fatalf("verifyAppACLR2M2RelationEffectivePrivilegesInTx() error = %v, want missing direct-owner %s rejection", err, privilege)
			}
		})
	}
}

func TestVerifyAppACLR2M2RelationGrantsRejectRevokedOwnerSelfACLWhileOwnerNativeAccessRemains(t *testing.T) {
	roles := appACLR2M2RoleOIDs{DirectMigrator: 21, CenterRuntime: 20, PlatformAdmin: 22}
	names := []string{"app_acl_r2_manifest_head", "app_acl_r2_manifest_revisions"}
	rawACLTx := &appACLR2M2ACLQueryCaptureTx{relationGrantRows: [][]any{
		{"app_acl_r2_manifest_head", int64(roles.DirectMigrator), int64(roles.CenterRuntime), "SELECT", false},
		{"app_acl_r2_manifest_revisions", int64(roles.DirectMigrator), int64(roles.CenterRuntime), "SELECT", false},
	}}
	if err := verifyAppACLR2M2RelationGrantsInTx(context.Background(), rawACLTx, names, roles); err == nil || !strings.Contains(err.Error(), "explicit SELECT grants") {
		t.Fatalf("verifyAppACLR2M2RelationGrantsInTx() error = %v, want revoked direct-migrator self SELECT raw-ACL rejection", err)
	}

	directOwner := [7]bool{true, true, true, true, true, true, true}
	runtimeReader := [7]bool{true, false, false, false, false, false, false}
	ownerNativeTx := &appACLR2M2RelationEffectivePrivilegeTx{
		rows: [][]any{
			appACLR2M2RelationEffectivePrivilegeRow(names[0], directOwner, runtimeReader, [7]bool{}),
			appACLR2M2RelationEffectivePrivilegeRow(names[1], directOwner, runtimeReader, [7]bool{}),
		},
		wantArgs: []any{int64(roles.DirectMigrator), int64(roles.CenterRuntime), int64(roles.PlatformAdmin), names},
	}
	if err := verifyAppACLR2M2RelationEffectivePrivilegesInTx(context.Background(), ownerNativeTx, names, roles); err != nil {
		t.Fatalf("verifyAppACLR2M2RelationEffectivePrivilegesInTx() error = %v, want owner-native access to remain true after self-ACL revocation", err)
	}
}

func TestVerifyAppACLR2M2RelationGrantsRejectsNonOwnerGrantDrift(t *testing.T) {
	roles := appACLR2M2RoleOIDs{DirectMigrator: 21, CenterRuntime: 20, PlatformAdmin: 22}
	baseRows := [][]any{
		{"app_acl_r2_manifest_head", int64(roles.DirectMigrator), int64(roles.DirectMigrator), "SELECT", false},
		{"app_acl_r2_manifest_head", int64(roles.DirectMigrator), int64(roles.CenterRuntime), "SELECT", false},
		{"app_acl_r2_manifest_revisions", int64(roles.DirectMigrator), int64(roles.DirectMigrator), "SELECT", false},
		{"app_acl_r2_manifest_revisions", int64(roles.DirectMigrator), int64(roles.CenterRuntime), "SELECT", false},
	}
	for _, tt := range []struct {
		name string
		row  []any
		want string
	}{
		{name: "non-owner", row: []any{"app_acl_r2_manifest_head", int64(roles.DirectMigrator), int64(roles.PlatformAdmin), "SELECT", false}, want: "unexpected ACL grant"},
		{name: "bootstrap", row: []any{"app_acl_r2_manifest_head", int64(roles.DirectMigrator), int64(10), "SELECT", false}, want: "unexpected ACL grant"},
		{name: "PUBLIC", row: []any{"app_acl_r2_manifest_head", int64(roles.DirectMigrator), int64(0), "SELECT", false}, want: "grantee OID"},
		{name: "grant option", row: []any{"app_acl_r2_manifest_head", int64(roles.DirectMigrator), int64(roles.CenterRuntime), "SELECT", true}, want: "unexpected ACL grant"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rows := append([][]any(nil), baseRows...)
			rows = append(rows, tt.row)
			tx := &appACLR2M2ACLQueryCaptureTx{relationGrantRows: rows}
			err := verifyAppACLR2M2RelationGrantsInTx(context.Background(), tx, []string{
				"app_acl_r2_manifest_head",
				"app_acl_r2_manifest_revisions",
			}, roles)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("verifyAppACLR2M2RelationGrantsInTx() error = %v, want non-owner grant rejection", err)
			}
		})
	}
}

func TestVerifyAppACLR2M2RelationGrantsRejectsGrantorSubstitution(t *testing.T) {
	roles := appACLR2M2RoleOIDs{DirectMigrator: 21, CenterRuntime: 20, PlatformAdmin: 22}
	tx := &appACLR2M2ACLGrantorSubstitutionRelationTx{rows: []appACLR2M2ACLGrantorRow{
		{name: "app_acl_r2_manifest_head", grantor: int64(roles.DirectMigrator), grantee: int64(roles.DirectMigrator), privilege: "SELECT"},
		{name: "app_acl_r2_manifest_head", grantor: 10, grantee: int64(roles.CenterRuntime), privilege: "SELECT"},
		{name: "app_acl_r2_manifest_revisions", grantor: int64(roles.DirectMigrator), grantee: int64(roles.DirectMigrator), privilege: "SELECT"},
		{name: "app_acl_r2_manifest_revisions", grantor: int64(roles.DirectMigrator), grantee: int64(roles.CenterRuntime), privilege: "SELECT"},
	}}

	err := verifyAppACLR2M2RelationGrantsInTx(context.Background(), tx, []string{
		"app_acl_r2_manifest_head",
		"app_acl_r2_manifest_revisions",
	}, roles)
	if err == nil || !strings.Contains(err.Error(), "grantor") {
		t.Fatalf("verifyAppACLR2M2RelationGrantsInTx() error = %v, want substituted table ACL grantor rejection", err)
	}
}

// PG16 marks GRANT as RESERVED_KEYWORD; bare unquoted range alias `grant` is a
// parser error. Relation and helper ACL readers must use a non-reserved alias.
func TestVerifyAppACLR2M2ACLQueriesAvoidBareGrantRangeAlias(t *testing.T) {
	relationTx := &appACLR2M2ACLQueryCaptureTx{
		relationGrantRows: [][]any{
			{"app_acl_r2_manifest_head", int64(21), int64(21), "SELECT", false},
			{"app_acl_r2_manifest_head", int64(21), int64(20), "SELECT", false},
			{"app_acl_r2_manifest_revisions", int64(21), int64(21), "SELECT", false},
			{"app_acl_r2_manifest_revisions", int64(21), int64(20), "SELECT", false},
		},
	}
	if err := verifyAppACLR2M2RelationGrantsInTx(context.Background(), relationTx, []string{
		"app_acl_r2_manifest_head",
		"app_acl_r2_manifest_revisions",
	}, appACLR2M2RoleOIDs{DirectMigrator: 21, CenterRuntime: 20, PlatformAdmin: 22}); err != nil {
		t.Fatalf("verifyAppACLR2M2RelationGrantsInTx() error = %v", err)
	}
	if relationTx.relationGrantQuery == "" {
		t.Fatal("M2 relation grant reader did not issue an aclexplode query")
	}
	if appACLR2SQLUsesBareGrantRangeAlias(relationTx.relationGrantQuery) {
		t.Errorf("M2 relation grant query uses bare reserved grant alias: %q", relationTx.relationGrantQuery)
	}

	roles := appACLR2M2RoleOIDs{DirectMigrator: 21, CenterRuntime: 20, PlatformAdmin: 22}
	functionTx := newAppACLR2M2FunctionVerifierTx(&scriptedAppACLR2ReceiptTx{
		queryRows: []scriptedAppACLR2ReceiptQueryRow{
			{values: []any{int64(500), int64(roles.DirectMigrator), "f"}},
			{values: []any{true, false, false}},
		},
		queries: []scriptedAppACLR2ReceiptQuery{
			{rows: [][]any{appACLR2M2FunctionProfileRow(appACLR2M2FunctionProfile(roles.DirectMigrator).Source)}},
			{rows: [][]any{{int64(roles.DirectMigrator), int64(roles.DirectMigrator), "EXECUTE", false}}},
		},
	})
	if err := verifyAppACLR2M2FunctionInTx(context.Background(), functionTx, roles); err != nil {
		t.Fatalf("verifyAppACLR2M2FunctionInTx() error = %v", err)
	}
	foundFunctionGrantQuery := false
	for _, query := range functionTx.queryTexts {
		if !strings.Contains(query, "aclexplode(procedure.proacl)") {
			continue
		}
		foundFunctionGrantQuery = true
		if appACLR2SQLUsesBareGrantRangeAlias(query) {
			t.Fatalf("M2 helper function grant query uses bare reserved grant alias: %q", query)
		}
	}
	if !foundFunctionGrantQuery {
		t.Fatal("M2 helper function verifier did not issue an aclexplode(proacl) query")
	}
}

func appACLR2SQLUsesBareGrantRangeAlias(query string) bool {
	// PG16: GRANT is RESERVED_KEYWORD and invalid as an unquoted ColId range alias.
	// Match the production bare form `aclexplode(...) grant` only; do not flag
	// safe renames such as `acl_grant` or quoted `"grant"`.
	normalized := strings.Join(strings.Fields(query), " ")
	return strings.Contains(normalized, "aclexplode(relation.relacl) grant") ||
		strings.Contains(normalized, "aclexplode(procedure.proacl) grant")
}

func TestVerifyAppACLR2M2FunctionRequiresDirectMigratorEffectiveExecute(t *testing.T) {
	roles := appACLR2M2RoleOIDs{DirectMigrator: 21, CenterRuntime: 20, PlatformAdmin: 22}
	tx := newAppACLR2M2FunctionVerifierTx(&scriptedAppACLR2ReceiptTx{
		queryRows: []scriptedAppACLR2ReceiptQueryRow{
			{values: []any{int64(500), int64(roles.DirectMigrator), "f"}},
			{
				checkArgs: func(args []any) error {
					if len(args) != 4 {
						return fmt.Errorf("effective helper ACL argument count = %d, want 4", len(args))
					}
					for index, want := range []int64{int64(roles.DirectMigrator), int64(roles.CenterRuntime), int64(roles.PlatformAdmin), 500} {
						got, ok := args[index].(int64)
						if !ok || got != want {
							return fmt.Errorf("effective helper ACL argument %d = %#v, want numeric OID %d", index, args[index], want)
						}
					}
					return nil
				},
				values: []any{true, false, false},
			},
		},
		queries: []scriptedAppACLR2ReceiptQuery{
			{rows: [][]any{appACLR2M2FunctionProfileRow(appACLR2M2FunctionProfile(roles.DirectMigrator).Source)}},
			{rows: [][]any{{int64(roles.DirectMigrator), int64(roles.DirectMigrator), "EXECUTE", false}}},
		},
	})

	if err := verifyAppACLR2M2FunctionInTx(context.Background(), tx, roles); err != nil {
		t.Fatalf("verifyAppACLR2M2FunctionInTx() error = %v", err)
	}
}

func TestVerifyAppACLR2M2FunctionGrantsRequireOwnerSelfACL(t *testing.T) {
	roles := appACLR2M2RoleOIDs{DirectMigrator: 21, CenterRuntime: 20, PlatformAdmin: 22}
	tx := newAppACLR2M2FunctionVerifierTx(&scriptedAppACLR2ReceiptTx{
		queryRows: []scriptedAppACLR2ReceiptQueryRow{
			{values: []any{int64(500), int64(roles.DirectMigrator), "f"}},
			{values: []any{true, false, false}},
		},
		queries: []scriptedAppACLR2ReceiptQuery{
			{rows: [][]any{appACLR2M2FunctionProfileRow(appACLR2M2FunctionProfile(roles.DirectMigrator).Source)}},
			{rows: [][]any{{int64(roles.DirectMigrator), int64(roles.DirectMigrator), "EXECUTE", false}}},
		},
	})
	if err := verifyAppACLR2M2FunctionInTx(context.Background(), tx, roles); err != nil {
		t.Fatalf("verifyAppACLR2M2FunctionInTx() error = %v, want exact direct-migrator self EXECUTE row", err)
	}
	var functionGrantQuery string
	for _, query := range tx.queryTexts {
		if strings.Contains(query, "aclexplode(procedure.proacl)") {
			functionGrantQuery = query
			break
		}
	}
	if functionGrantQuery == "" {
		t.Fatal("M2 helper function verifier did not issue an aclexplode(proacl) query")
	}
	if strings.Contains(functionGrantQuery, "acl_grant.grantee <> procedure.proowner") {
		t.Fatalf("M2 helper grant query filters direct-migrator self ACL row: %q", functionGrantQuery)
	}
}

func TestVerifyAppACLR2M2FunctionRejectsGrantorSubstitution(t *testing.T) {
	roles := appACLR2M2RoleOIDs{DirectMigrator: 21, CenterRuntime: 20, PlatformAdmin: 22}
	tx := &appACLR2M2ACLGrantorSubstitutionFunctionTx{
		appACLR2M2FunctionVerifierTx: newAppACLR2M2FunctionVerifierTx(&scriptedAppACLR2ReceiptTx{
			queryRows: []scriptedAppACLR2ReceiptQueryRow{
				{values: []any{int64(500), int64(roles.DirectMigrator), "f"}},
				{values: []any{true, false, false}},
			},
			queries: []scriptedAppACLR2ReceiptQuery{
				{rows: [][]any{appACLR2M2FunctionProfileRow(appACLR2M2FunctionProfile(roles.DirectMigrator).Source)}},
			},
		}),
		rows: []appACLR2M2ACLGrantorRow{{
			grantor:   10,
			grantee:   int64(roles.DirectMigrator),
			privilege: "EXECUTE",
		}},
	}

	err := verifyAppACLR2M2FunctionInTx(context.Background(), tx, roles)
	if err == nil || !strings.Contains(err.Error(), "grantor") {
		t.Fatalf("verifyAppACLR2M2FunctionInTx() error = %v, want substituted helper ACL grantor rejection", err)
	}
}

func TestVerifyAppACLR2M2FunctionRejectsRevokedOwnerSelfExecuteWhileOwnerNativeAccessRemains(t *testing.T) {
	roles := appACLR2M2RoleOIDs{DirectMigrator: 21, CenterRuntime: 20, PlatformAdmin: 22}
	rawACLTx := newAppACLR2M2FunctionVerifierTx(&scriptedAppACLR2ReceiptTx{
		queryRows: []scriptedAppACLR2ReceiptQueryRow{
			{values: []any{int64(500), int64(roles.DirectMigrator), "f"}},
		},
		queries: []scriptedAppACLR2ReceiptQuery{
			{rows: [][]any{appACLR2M2FunctionProfileRow(appACLR2M2FunctionProfile(roles.DirectMigrator).Source)}},
			{rows: nil},
		},
	})
	if err := verifyAppACLR2M2FunctionInTx(context.Background(), rawACLTx, roles); err == nil || !strings.Contains(err.Error(), "explicit EXECUTE ACL drift") {
		t.Fatalf("verifyAppACLR2M2FunctionInTx() error = %v, want revoked direct-migrator self EXECUTE rejection", err)
	}

	ownerNativeTx := newAppACLR2M2FunctionVerifierTx(&scriptedAppACLR2ReceiptTx{
		queryRows: []scriptedAppACLR2ReceiptQueryRow{
			{values: []any{int64(500), int64(roles.DirectMigrator), "f"}},
			{values: []any{true, false, false}},
		},
		queries: []scriptedAppACLR2ReceiptQuery{
			{rows: [][]any{appACLR2M2FunctionProfileRow(appACLR2M2FunctionProfile(roles.DirectMigrator).Source)}},
			{rows: [][]any{{int64(roles.DirectMigrator), int64(roles.DirectMigrator), "EXECUTE", false}}},
		},
	})
	if err := verifyAppACLR2M2FunctionInTx(context.Background(), ownerNativeTx, roles); err != nil {
		t.Fatalf("verifyAppACLR2M2FunctionInTx() error = %v, want owner-native EXECUTE to remain true independently of the raw ACL self-row", err)
	}
}

func TestVerifyAppACLR2M2FunctionRejectsDirectMigratorNativeExecuteDrift(t *testing.T) {
	roles := appACLR2M2RoleOIDs{DirectMigrator: 21, CenterRuntime: 20, PlatformAdmin: 22}
	tx := newAppACLR2M2FunctionVerifierTx(&scriptedAppACLR2ReceiptTx{
		queryRows: []scriptedAppACLR2ReceiptQueryRow{
			{values: []any{int64(500), int64(roles.DirectMigrator), "f"}},
			{values: []any{false, false, false}},
		},
		queries: []scriptedAppACLR2ReceiptQuery{
			{rows: [][]any{appACLR2M2FunctionProfileRow(appACLR2M2FunctionProfile(roles.DirectMigrator).Source)}},
			{rows: [][]any{{int64(roles.DirectMigrator), int64(roles.DirectMigrator), "EXECUTE", false}}},
		},
	})
	if err := verifyAppACLR2M2FunctionInTx(context.Background(), tx, roles); err == nil || !strings.Contains(err.Error(), "effective EXECUTE drift") {
		t.Fatalf("verifyAppACLR2M2FunctionInTx() error = %v, want direct-migrator native EXECUTE rejection", err)
	}
}

func TestVerifyAppACLR2M2FunctionRejectsNonOwnerExecuteGrant(t *testing.T) {
	roles := appACLR2M2RoleOIDs{DirectMigrator: 21, CenterRuntime: 20, PlatformAdmin: 22}
	for _, tt := range []struct {
		name    string
		grantee int64
	}{
		{name: "center runtime", grantee: int64(roles.CenterRuntime)},
		{name: "bootstrap", grantee: 10},
		{name: "platform admin", grantee: int64(roles.PlatformAdmin)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tx := newAppACLR2M2FunctionVerifierTx(&scriptedAppACLR2ReceiptTx{
				queryRows: []scriptedAppACLR2ReceiptQueryRow{
					{values: []any{int64(500), int64(roles.DirectMigrator), "f"}},
				},
				queries: []scriptedAppACLR2ReceiptQuery{
					{rows: [][]any{appACLR2M2FunctionProfileRow(appACLR2M2FunctionProfile(roles.DirectMigrator).Source)}},
					{rows: [][]any{{int64(roles.DirectMigrator), tt.grantee, "EXECUTE", false}}},
				},
			})
			if err := verifyAppACLR2M2FunctionInTx(context.Background(), tx, roles); err == nil || !strings.Contains(err.Error(), "explicit EXECUTE ACL drift") {
				t.Fatalf("verifyAppACLR2M2FunctionInTx() error = %v, want non-owner EXECUTE rejection", err)
			}
		})
	}
}

func TestVerifyAppACLR2M2FunctionRejectsDefinitionDrift(t *testing.T) {
	tx := &appACLR2M2FunctionDefinitionProbeTx{}
	err := verifyAppACLR2M2FunctionInTx(context.Background(), tx, appACLR2M2RoleOIDs{
		DirectMigrator: 21,
		CenterRuntime:  20,
		PlatformAdmin:  22,
	})
	if err == nil || !strings.Contains(err.Error(), "definition") {
		t.Fatalf("verifyAppACLR2M2FunctionInTx() error = %v, want function-definition rejection", err)
	}
	if !tx.profileRead {
		t.Fatal("M2 function verifier did not read the full PostgreSQL function profile")
	}
}

func TestReadAppACLR2M2FunctionProfileRejectsOversizedTextBeforeTransfer(t *testing.T) {
	want := appACLR2M2FunctionProfile(21)
	for _, field := range []string{"definition", "source"} {
		t.Run(field, func(t *testing.T) {
			rows := &appACLR2BoundedCatalogTextRows{
				kind:              "function",
				rawRows:           [][]any{appACLR2M2FunctionProfileRow(want.Source)},
				overflowField:     field,
				definitionMaximum: len(want.Definition),
				sourceMaximum:     len(want.Source),
			}
			tx := &appACLR2BoundedCatalogTextTx{rowQueue: []pgx.Rows{rows}}

			_, err := readAppACLR2M2FunctionProfileInTx(context.Background(), tx)
			if rows.transferredOversizedBytes != 0 {
				t.Fatalf("helper profile reader transferred %d oversized %s bytes before rejection", rows.transferredOversizedBytes, field)
			}
			if err == nil || !strings.Contains(err.Error(), field+" size") {
				t.Fatalf("readAppACLR2M2FunctionProfileInTx() error = %v, want observed %s-size rejection", err, field)
			}
			query := strings.ToLower(tx.queries[0])
			if !strings.Contains(query, "octet_length") || !strings.Contains(query, "case when") || !strings.Contains(query, "null::bytea") {
				t.Fatalf("helper profile query = %q, want bounded text projections", tx.queries[0])
			}
			if !reflect.DeepEqual(tx.arguments[0], []any{len(want.Definition), len(want.Source)}) {
				t.Fatalf("helper profile bounds = %#v, want definition/source lengths %d/%d", tx.arguments[0], len(want.Definition), len(want.Source))
			}
		})
	}
}

func TestReadAppACLR2M2FunctionProfileObservesAtMostTwoOverloads(t *testing.T) {
	want := appACLR2M2FunctionProfile(21)
	rows := &appACLR2BoundedCatalogTextRows{
		kind:              "function",
		rawRows:           [][]any{appACLR2M2FunctionProfileRow(want.Source), appACLR2M2FunctionProfileRow(want.Source), appACLR2M2FunctionProfileRow(want.Source)},
		definitionMaximum: len(want.Definition),
		sourceMaximum:     len(want.Source),
	}
	tx := &appACLR2BoundedCatalogTextTx{rowQueue: []pgx.Rows{rows}}

	profiles, err := readAppACLR2M2FunctionProfileInTx(context.Background(), tx)
	if err != nil {
		t.Fatalf("readAppACLR2M2FunctionProfileInTx() error = %v", err)
	}
	if len(profiles) != 2 || rows.scanCalls != 2 {
		t.Fatalf("helper overload witnesses = profiles:%d scans:%d, want exactly two", len(profiles), rows.scanCalls)
	}
	query := strings.ToLower(tx.queries[0])
	if !strings.Contains(query, "limit 2") || strings.Contains(query, "order by") {
		t.Fatalf("helper profile query = %q, want unordered server-side LIMIT 2", tx.queries[0])
	}
}

func TestVerifyAppACLR2M2FunctionStopsAfterProfileCompletionError(t *testing.T) {
	want := appACLR2M2FunctionProfile(21)
	original := &pgconn.PgError{Code: "57014", Message: "query canceled while completing helper profile"}
	rows := &appACLR2BoundedCatalogTextRows{
		kind:              "function",
		rawRows:           [][]any{appACLR2M2FunctionProfileRow(want.Source), appACLR2M2FunctionProfileRow(want.Source)},
		definitionMaximum: len(want.Definition),
		sourceMaximum:     len(want.Source),
		completionErr:     original,
	}
	tx := &appACLR2BoundedCatalogTextTx{
		rowQueue:         []pgx.Rows{rows},
		functionIdentity: []any{int64(500), int64(21), "f"},
	}

	err := verifyAppACLR2M2FunctionInTx(context.Background(), tx, appACLR2M2RoleOIDs{
		DirectMigrator: 21,
		CenterRuntime:  20,
		PlatformAdmin:  22,
	})
	var got *pgconn.PgError
	if !errors.As(err, &got) || got != original {
		t.Fatalf("verifyAppACLR2M2FunctionInTx() error = %v, want original completion error", err)
	}
	if tx.queryCalls != 1 || tx.laterQueryCalls != 0 {
		t.Fatalf("helper evidence queries = total:%d later:%d, want profile only", tx.queryCalls, tx.laterQueryCalls)
	}
	if !rows.closed || rows.closeCalls != 1 {
		t.Fatalf("helper profile rows close lifecycle = closed:%t calls:%d, want one explicit close", rows.closed, rows.closeCalls)
	}
}

func TestVerifyAppACLR2M2TriggersRejectDefinitionDrift(t *testing.T) {
	tx := &appACLR2M2TriggerDefinitionProbeTx{}
	err := verifyAppACLR2M2TriggersInTx(context.Background(), tx, 21)
	if err == nil || !strings.Contains(err.Error(), "definition") {
		t.Fatalf("verifyAppACLR2M2TriggersInTx() error = %v, want trigger-definition rejection", err)
	}
	if !tx.definitionRead {
		t.Fatal("M2 trigger verifier did not read pg_get_triggerdef")
	}
}

func TestVerifyAppACLR2M2TriggersDistinguishUserImmutableFromInternalFK(t *testing.T) {
	t.Run("accepts exact internal foreign-key triggers beside exact user immutables", func(t *testing.T) {
		tx := &appACLR2M2TriggerInternalFKProbeTx{}
		if err := verifyAppACLR2M2TriggersInTx(context.Background(), tx, 21); err != nil {
			t.Fatalf("verifyAppACLR2M2TriggersInTx() error = %v, want exact internal FK triggers and the two user immutables", err)
		}
		if !tx.definitionRead {
			t.Fatal("M2 trigger verifier did not read pg_get_triggerdef")
		}
		if strings.Contains(strings.ToLower(tx.query), "and not trigger.tgisinternal") {
			t.Fatalf("M2 trigger reader query = %q, want internal RI triggers included", tx.query)
		}
		if strings.Contains(tx.query, "constraint.") || strings.Contains(tx.query, "pg_catalog.pg_constraint constraint on") {
			t.Fatalf("M2 trigger reader query uses reserved PostgreSQL alias constraint: %q", tx.query)
		}
	})
	t.Run("rejects unexpected extra user trigger", func(t *testing.T) {
		tx := &appACLR2M2TriggerInternalFKProbeTx{includeExtraUser: true}
		err := verifyAppACLR2M2TriggersInTx(context.Background(), tx, 21)
		if err == nil || !strings.Contains(err.Error(), "catalog drift") {
			t.Fatalf("verifyAppACLR2M2TriggersInTx() error = %v, want unexpected user-trigger rejection", err)
		}
	})
}

func TestVerifyAppACLR2M2TriggersRejectInternalForeignKeyTriggerDrift(t *testing.T) {
	tests := []struct {
		name   string
		mutate func([][]any) [][]any
	}{
		{
			name: "disabled internal trigger",
			mutate: func(rows [][]any) [][]any {
				rows[0][appACLR2M2TriggerProbeEnabled] = false
				return rows
			},
		},
		{
			name: "missing internal trigger",
			mutate: func(rows [][]any) [][]any {
				return append(rows[:0], rows[1:]...)
			},
		},
		{
			name: "wrong constraint binding",
			mutate: func(rows [][]any) [][]any {
				rows[0][appACLR2M2TriggerProbeConstraintDefinition] = appACLR2LivePG16DesignPathHeadForeignKey
				return rows
			},
		},
		{
			name: "wrong relation binding",
			mutate: func(rows [][]any) [][]any {
				rows[0][appACLR2M2TriggerProbeBoundRelation] = "public.app_acl_manifest_revisions"
				return rows
			},
		},
		{
			name: "wrong function binding",
			mutate: func(rows [][]any) [][]any {
				rows[0][appACLR2M2TriggerProbeFunctionIdentity] = "pg_catalog.RI_FKey_check_upd()"
				return rows
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &appACLR2M2TriggerInternalFKProbeTx{mutateRows: tt.mutate}
			err := verifyAppACLR2M2TriggersInTx(context.Background(), tx, 21)
			if err == nil || !strings.Contains(err.Error(), "internal foreign-key trigger") {
				t.Fatalf("verifyAppACLR2M2TriggersInTx() error = %v, want internal foreign-key trigger rejection", err)
			}
		})
	}
}

func TestVerifyAppACLR2M2TriggersRejectOversizedDefinitionsBeforeTransfer(t *testing.T) {
	userMaximum := appACLR2TestM2UserTriggerDefinitionMaximum()
	constraintMaximum := max(len(appACLR2LivePG16DesignPathHeadForeignKey), len(appACLR2LivePG16DesignPathRevForeignKey))
	for _, field := range []string{"trigger_definition", "constraint_definition"} {
		t.Run(field, func(t *testing.T) {
			rows := &appACLR2BoundedCatalogTextRows{
				kind:              "trigger",
				rawRows:           appACLR2M2ExactTriggerRows(),
				overflowField:     field,
				definitionMaximum: userMaximum,
				sourceMaximum:     constraintMaximum,
			}
			tx := &appACLR2BoundedCatalogTextTx{rowQueue: []pgx.Rows{rows}}

			err := verifyAppACLR2M2TriggersInTx(context.Background(), tx, 21)
			if rows.transferredOversizedBytes != 0 {
				t.Fatalf("trigger reader transferred %d oversized %s bytes before rejection", rows.transferredOversizedBytes, field)
			}
			if err == nil || !strings.Contains(err.Error(), "size") {
				t.Fatalf("verifyAppACLR2M2TriggersInTx() error = %v, want observed %s size rejection", err, field)
			}
			query := strings.ToLower(tx.queries[0])
			if !strings.Contains(query, "octet_length") || !strings.Contains(query, "case when") || !strings.Contains(query, "null::bytea") {
				t.Fatalf("M2 trigger query = %q, want bounded definition projections", tx.queries[0])
			}
			if !reflect.DeepEqual(tx.arguments[0], []any{userMaximum, constraintMaximum, len(appACLR2M2ExactTriggerRows()) + 1}) {
				t.Fatalf("M2 trigger bounds = %#v, want user/constraint/row bounds %d/%d/%d", tx.arguments[0], userMaximum, constraintMaximum, len(appACLR2M2ExactTriggerRows())+1)
			}
		})
	}
}

func TestVerifyAppACLR2M2TriggersQueriesExpectedPlusOneWitness(t *testing.T) {
	rawRows := append(appACLR2M2ExactTriggerRows(),
		appACLR2M2TriggerProbeRow("app_acl_r2_manifest_head", "app_acl_r2_manifest_head_extra", false, "CREATE TRIGGER extra"),
		appACLR2M2TriggerProbeRow("app_acl_r2_manifest_head", "app_acl_r2_manifest_head_later", false, "CREATE TRIGGER later"),
	)
	rows := &appACLR2BoundedCatalogTextRows{
		kind:              "trigger",
		rawRows:           rawRows,
		definitionMaximum: appACLR2TestM2UserTriggerDefinitionMaximum(),
		sourceMaximum:     max(len(appACLR2LivePG16DesignPathHeadForeignKey), len(appACLR2LivePG16DesignPathRevForeignKey)),
	}
	tx := &appACLR2BoundedCatalogTextTx{rowQueue: []pgx.Rows{rows}}

	err := verifyAppACLR2M2TriggersInTx(context.Background(), tx, 21)
	if err == nil {
		t.Fatal("verifyAppACLR2M2TriggersInTx() accepted an excess trigger witness")
	}
	query := strings.ToLower(tx.queries[0])
	if !strings.Contains(query, "limit $3::integer") || tx.arguments[0][2] != len(appACLR2M2ExactTriggerRows())+1 {
		t.Fatalf("M2 trigger query/arguments = %q %#v, want expected+1 server witness", tx.queries[0], tx.arguments[0])
	}
}

func TestVerifyAppACLR2M2TriggersPreservesCompletionError(t *testing.T) {
	original := &pgconn.PgError{Code: "57014", Message: "query canceled while completing trigger evidence"}
	rows := &appACLR2BoundedCatalogTextRows{
		kind:              "trigger",
		rawRows:           appACLR2M2ExactTriggerRows(),
		definitionMaximum: appACLR2TestM2UserTriggerDefinitionMaximum(),
		sourceMaximum:     max(len(appACLR2LivePG16DesignPathHeadForeignKey), len(appACLR2LivePG16DesignPathRevForeignKey)),
		completionErr:     original,
	}
	tx := &appACLR2BoundedCatalogTextTx{rowQueue: []pgx.Rows{rows}}

	err := verifyAppACLR2M2TriggersInTx(context.Background(), tx, 21)
	var got *pgconn.PgError
	if !errors.As(err, &got) || got != original {
		t.Fatalf("verifyAppACLR2M2TriggersInTx() error = %v, want original completion error", err)
	}
}

func TestVerifyAppACLR2M2RelationConstraintsProveReferencedForeignKeyIndex(t *testing.T) {
	tests := []struct {
		name   string
		mutate func([][]any)
		want   string
	}{
		{name: "exact referenced unique indexes"},
		{name: "zero foreign-key conindid", mutate: func(rows [][]any) {
			for _, row := range rows {
				if row[1] == "f" {
					row[4] = int64(0)
					row[5] = false
					row[6] = false
					row[7] = false
				}
			}
		}, want: "foreign key"},
		{name: "invalid referenced index", mutate: func(rows [][]any) {
			for _, row := range rows {
				if row[1] == "f" {
					row[7] = false
				}
			}
		}, want: "foreign key"},
		{name: "non-unique referenced index", mutate: func(rows [][]any) {
			for _, row := range rows {
				if row[1] == "f" {
					row[5] = false
					row[6] = false
				}
			}
		}, want: "foreign key"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := appACLR2M2ConstraintReaderRows()
			if tt.mutate != nil {
				tt.mutate(rows)
			}
			tx := &appACLR2M2ScriptedQueryTx{queryRows: appACLR2M2ConstraintQueryRows(rows)}
			err := verifyAppACLR2M2RelationConstraintsInTx(context.Background(), tx, []string{
				"app_acl_r2_manifest_head",
				"app_acl_r2_manifest_revisions",
			})
			if tt.want == "" {
				if err != nil {
					t.Fatalf("verifyAppACLR2M2RelationConstraintsInTx() error = %v", err)
				}
				if len(tx.queryTexts) != 2 || !strings.Contains(strings.Join(tx.queryTexts, "\n"), "constraint_catalog.confrelid") {
					t.Fatalf("M2 constraint reader query = %q, want FK join through constraint.confrelid", tx.queryTexts)
				}
				queries := strings.Join(tx.queryTexts, "\n")
				if strings.Contains(queries, "index_catalog.indrelid = constraint_catalog.conrelid") &&
					!strings.Contains(queries, "constraint_catalog.confrelid") {
					t.Fatal("M2 constraint reader still joins FK indexes only to the local relation")
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("verifyAppACLR2M2RelationConstraintsInTx() error = %v, want %q rejection", err, tt.want)
			}
		})
	}
}

func TestVerifyAppACLR2M2RelationConstraintsBindExpectedIndexOIDs(t *testing.T) {
	tx := &appACLR2M2ScriptedQueryTx{queryRows: appACLR2M2ConstraintQueryRows(appACLR2M2ConstraintReaderRows())}
	if err := verifyAppACLR2M2RelationConstraintsInTx(context.Background(), tx, []string{
		"app_acl_r2_manifest_head",
		"app_acl_r2_manifest_revisions",
	}); err != nil {
		t.Fatalf("verifyAppACLR2M2RelationConstraintsInTx() error = %v", err)
	}
	queries := strings.Join(tx.queryTexts, "\n")
	if len(tx.queryTexts) != 2 || !strings.Contains(queries, "constraint_catalog.conindid = expected_index.oid") {
		t.Fatalf("M2 constraint reader query = %q, want exact expected-index OID binding", tx.queryTexts)
	}
	for _, name := range []string{
		"app_acl_r2_manifest_head_pkey",
		"app_acl_r2_manifest_revisions_pkey",
		"app_acl_r2_manifest_revisions_protocol_version_manifest_rev_key",
		"app_acl_manifest_revisions_manifest_revision_manifest_diges_key",
	} {
		if !strings.Contains(queries, name) {
			t.Fatalf("M2 constraint reader query does not bind allowlisted index %q", name)
		}
	}
}

func TestVerifyAppACLR2M2RelationConstraintsRejectOversizedDefinitionBeforeTransfer(t *testing.T) {
	maximum := len(appACLR2LivePG16DesignPathManifestDigest)
	rows := &appACLR2BoundedCatalogTextRows{
		kind: "constraint",
		rawRows: [][]any{{
			"app_acl_r2_manifest_head", "c", "CHECK (singleton)", true, int64(0), false, false, false,
		}},
		overflowField:     "definition",
		definitionMaximum: maximum,
	}
	tx := &appACLR2BoundedCatalogTextTx{rowQueue: []pgx.Rows{rows}}

	err := verifyAppACLR2M2RelationConstraintsInTx(context.Background(), tx, []string{"app_acl_r2_manifest_head"})
	if rows.transferredOversizedBytes != 0 {
		t.Fatalf("constraint reader transferred %d oversized definition bytes before rejection", rows.transferredOversizedBytes)
	}
	if err == nil || !strings.Contains(err.Error(), "definition size") {
		t.Fatalf("verifyAppACLR2M2RelationConstraintsInTx() error = %v, want observed definition-size rejection", err)
	}
	query := strings.ToLower(tx.queries[0])
	if !strings.Contains(query, "octet_length") || !strings.Contains(query, "case when") || !strings.Contains(query, "null::bytea") {
		t.Fatalf("M2 constraint query = %q, want bounded definition projection", tx.queries[0])
	}
}

func TestVerifyAppACLR2M2RelationConstraintsUsePerRelationExpectedPlusOneWitnesses(t *testing.T) {
	maximum := len(appACLR2LivePG16DesignPathManifestDigest)
	for _, relation := range []string{"app_acl_r2_manifest_head", "app_acl_r2_manifest_revisions"} {
		t.Run(relation+" excess", func(t *testing.T) {
			exact := appACLR2M2ConstraintRowsForRelation(relation)
			rawRows := append(exact,
				[]any{relation, "c", "CHECK (false)", true, int64(0), false, false, false},
				[]any{relation, "c", "CHECK (true)", true, int64(0), false, false, false},
			)
			rows := &appACLR2BoundedCatalogTextRows{kind: "constraint", rawRows: rawRows, definitionMaximum: maximum}
			tx := &appACLR2BoundedCatalogTextTx{rowQueue: []pgx.Rows{rows}}

			err := verifyAppACLR2M2RelationConstraintsInTx(context.Background(), tx, []string{relation})
			if err == nil {
				t.Fatal("verifyAppACLR2M2RelationConstraintsInTx() accepted excess constraints")
			}
			wantWitnesses := len(exact) + 1
			if rows.scanCalls != wantWitnesses {
				t.Fatalf("constraint row scans = %d, want expected+1 witness count %d", rows.scanCalls, wantWitnesses)
			}
			if len(tx.arguments[0]) != 3 || tx.arguments[0][0] != relation || tx.arguments[0][1] != maximum || tx.arguments[0][2] != wantWitnesses {
				t.Fatalf("constraint query arguments = %#v, want relation/definition/expected+1 %#v", tx.arguments[0], []any{relation, maximum, wantWitnesses})
			}
			if !strings.Contains(strings.ToLower(tx.queries[0]), "limit $3::integer") {
				t.Fatalf("constraint query = %q, want per-relation expected+1 LIMIT", tx.queries[0])
			}
		})
	}

	t.Run("both relations retain independent witnesses", func(t *testing.T) {
		headRows := &appACLR2BoundedCatalogTextRows{kind: "constraint", rawRows: appACLR2M2ConstraintRowsForRelation("app_acl_r2_manifest_head"), definitionMaximum: maximum}
		revisionRows := &appACLR2BoundedCatalogTextRows{kind: "constraint", rawRows: appACLR2M2ConstraintRowsForRelation("app_acl_r2_manifest_revisions"), definitionMaximum: maximum}
		tx := &appACLR2BoundedCatalogTextTx{rowQueue: []pgx.Rows{headRows, revisionRows}}

		if err := verifyAppACLR2M2RelationConstraintsInTx(context.Background(), tx, []string{
			"app_acl_r2_manifest_head",
			"app_acl_r2_manifest_revisions",
		}); err != nil {
			t.Fatalf("verifyAppACLR2M2RelationConstraintsInTx() error = %v", err)
		}
		if tx.queryCalls != 2 || len(tx.rowQueue) != 0 {
			t.Fatalf("constraint relation queries = %d with %d queues left, want one bounded query per relation", tx.queryCalls, len(tx.rowQueue))
		}
	})
}

func TestVerifyAppACLR2M2RelationConstraintsStopsBeforeLaterRelationOnCompletionError(t *testing.T) {
	original := &pgconn.PgError{Code: "57014", Message: "query canceled while completing constraint evidence"}
	maximum := len(appACLR2LivePG16DesignPathManifestDigest)
	headRows := &appACLR2BoundedCatalogTextRows{
		kind:              "constraint",
		rawRows:           appACLR2M2ConstraintRowsForRelation("app_acl_r2_manifest_head"),
		definitionMaximum: maximum,
		completionErr:     original,
	}
	revisionRows := &appACLR2BoundedCatalogTextRows{kind: "constraint", rawRows: appACLR2M2ConstraintRowsForRelation("app_acl_r2_manifest_revisions"), definitionMaximum: maximum}
	tx := &appACLR2BoundedCatalogTextTx{rowQueue: []pgx.Rows{headRows, revisionRows}}

	err := verifyAppACLR2M2RelationConstraintsInTx(context.Background(), tx, []string{
		"app_acl_r2_manifest_head",
		"app_acl_r2_manifest_revisions",
	})
	var got *pgconn.PgError
	if !errors.As(err, &got) || got != original {
		t.Fatalf("verifyAppACLR2M2RelationConstraintsInTx() error = %v, want original completion error", err)
	}
	if tx.queryCalls != 1 || tx.laterQueryCalls != 0 || len(tx.rowQueue) != 1 {
		t.Fatalf("constraint queries after completion error = calls:%d later:%d queued:%d, want no later relation read", tx.queryCalls, tx.laterQueryCalls, len(tx.rowQueue))
	}
}

// Literal live PostgreSQL 16.14 pg_get_constraintdef(..., true) strings observed
// under the design-mandated search_path = pg_catalog, public (codex-pgcrypto-
// privilege-probe, rollback-only temporary objects). These must not be derived
// from production expected constants or shared fixture helpers.
const (
	appACLR2LivePG16DesignPathHeadForeignKey  = "FOREIGN KEY (protocol_version, manifest_revision, manifest_digest) REFERENCES app_acl_r2_manifest_revisions(protocol_version, manifest_revision, manifest_digest) ON DELETE RESTRICT"
	appACLR2LivePG16DesignPathRevForeignKey   = "FOREIGN KEY (m1_revision, m1_manifest_digest) REFERENCES app_acl_manifest_revisions(manifest_revision, manifest_digest) ON DELETE RESTRICT"
	appACLR2LivePG16DesignPathSourceDigest    = "CHECK (r2_source_set_digest = record_platform_internal.digest(r2_source_set_body, 'sha256'::text))"
	appACLR2LivePG16DesignPathPrivilegeDigest = "CHECK (r2_privilege_set_digest = record_platform_internal.digest(r2_privilege_set_body, 'sha256'::text))"
	appACLR2LivePG16DesignPathDomainDigest    = "CHECK (domain_digest = record_platform_internal.digest(domain_body, 'sha256'::text))"
	appACLR2LivePG16DesignPathControlDigest   = "CHECK (control_acl_digest = record_platform_internal.digest(control_acl_body, 'sha256'::text))"
	// Full canonical M2 preimage recomputation CHECK (CanonicalAppACLManifestR2BodyV1
	// contract): live PG16.14 pg_get_constraintdef under search_path=pg_catalog, public.
	appACLR2LivePG16DesignPathManifestDigest = "CHECK (manifest_digest = record_platform_internal.digest((((((((((((((((((((((((((convert_to('HOUFENG-APP-ACL-MANIFEST-R2-V1'::text, 'UTF8'::name) || int2send(1::smallint)) || int2send(protocol_version)) || int8send(manifest_revision)) || int8send(m1_revision)) || m1_manifest_digest) || m1_source_set_digest) || m1_privilege_set_digest) || int2send(octet_length(m1_migrator_catalog_role)::smallint)) || convert_to(m1_migrator_catalog_role, 'UTF8'::name)) || int2send(octet_length(direct_migrator_name)::smallint)) || convert_to(direct_migrator_name, 'UTF8'::name)) || int4send(direct_migrator_oid::integer)) || int4send(octet_length(r2_source_set_body))) || r2_source_set_body) || r2_source_set_digest) || int4send(octet_length(r2_privilege_set_body))) || r2_privilege_set_body) || r2_privilege_set_digest) || int4send(octet_length(domain_body))) || domain_body) || domain_digest) || receipt_digest) || int4send(octet_length(control_acl_body))) || control_acl_body) || control_acl_digest) || int8send((EXTRACT(epoch FROM recorded_at) * 1000000::numeric)::bigint), 'sha256'::text))"
)

func appACLR2LivePG16DesignPathHeadConstraints() []AppACLR2ReceiptTableConstraintCatalogV1 {
	return []AppACLR2ReceiptTableConstraintCatalogV1{
		{Type: "p", Definition: "PRIMARY KEY (singleton)", Validated: true, IndexOID: 1001, IndexPrimary: true, IndexUnique: true, IndexValid: true},
		{Type: "f", Definition: appACLR2LivePG16DesignPathHeadForeignKey, Validated: true, IndexOID: 1003, IndexPrimary: false, IndexUnique: true, IndexValid: true},
		{Type: "c", Definition: "CHECK (singleton)", Validated: true},
		{Type: "c", Definition: "CHECK (protocol_version = 2)", Validated: true},
		{Type: "c", Definition: "CHECK (manifest_revision = 2)", Validated: true},
		{Type: "c", Definition: "CHECK (octet_length(manifest_digest) = 32)", Validated: true},
	}
}

func appACLR2LivePG16DesignPathRevisionConstraints() []AppACLR2ReceiptTableConstraintCatalogV1 {
	checks := []string{
		"CHECK (protocol_version = 2)",
		"CHECK (manifest_revision = 2)",
		"CHECK (m1_revision = 1)",
		"CHECK (octet_length(m1_manifest_digest) = 32)",
		"CHECK (octet_length(m1_source_set_digest) = 32)",
		"CHECK (octet_length(m1_privilege_set_digest) = 32)",
		"CHECK (octet_length(r2_source_set_digest) = 32)",
		"CHECK (octet_length(r2_privilege_set_digest) = 32)",
		"CHECK (octet_length(domain_digest) = 32)",
		"CHECK (octet_length(receipt_digest) = 32)",
		"CHECK (octet_length(control_acl_digest) = 32)",
		"CHECK (octet_length(manifest_digest) = 32)",
		appACLR2LivePG16DesignPathSourceDigest,
		appACLR2LivePG16DesignPathPrivilegeDigest,
		appACLR2LivePG16DesignPathDomainDigest,
		appACLR2LivePG16DesignPathControlDigest,
		appACLR2LivePG16DesignPathManifestDigest,
	}
	out := []AppACLR2ReceiptTableConstraintCatalogV1{
		{Type: "p", Definition: "PRIMARY KEY (protocol_version, manifest_revision)", Validated: true, IndexOID: 1002, IndexPrimary: true, IndexUnique: true, IndexValid: true},
		{Type: "u", Definition: "UNIQUE (protocol_version, manifest_revision, manifest_digest)", Validated: true, IndexOID: 1003, IndexPrimary: false, IndexUnique: true, IndexValid: true},
		{Type: "f", Definition: appACLR2LivePG16DesignPathRevForeignKey, Validated: true, IndexOID: 2001, IndexPrimary: false, IndexUnique: true, IndexValid: true},
	}
	for _, definition := range checks {
		out = append(out, AppACLR2ReceiptTableConstraintCatalogV1{Type: "c", Definition: definition, Validated: true})
	}
	return out
}

func TestValidateAppACLR2M2RelationConstraintsEnforcesCanonicalManifestDigestRecomputation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func([]AppACLR2ReceiptTableConstraintCatalogV1) []AppACLR2ReceiptTableConstraintCatalogV1
		want   string
	}{
		{name: "canonical live PG16 fixture accepted"},
		{
			name: "missing recomputation check rejected",
			mutate: func(constraints []AppACLR2ReceiptTableConstraintCatalogV1) []AppACLR2ReceiptTableConstraintCatalogV1 {
				out := constraints[:0]
				for _, constraint := range constraints {
					if constraint.Definition != appACLR2LivePG16DesignPathManifestDigest {
						out = append(out, constraint)
					}
				}
				return out
			},
			want: "missing check",
		},
		{
			name: "tampered recomputation check rejected",
			mutate: func(constraints []AppACLR2ReceiptTableConstraintCatalogV1) []AppACLR2ReceiptTableConstraintCatalogV1 {
				for index := range constraints {
					if constraints[index].Definition == appACLR2LivePG16DesignPathManifestDigest {
						constraints[index].Definition = strings.Replace(
							constraints[index].Definition,
							"'sha256'::text",
							"'sha512'::text",
							1,
						)
					}
				}
				return constraints
			},
			want: "unexpected check",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			constraints := appACLR2LivePG16DesignPathRevisionConstraints()
			if tt.mutate != nil {
				constraints = tt.mutate(constraints)
			}
			err := validateAppACLR2M2RelationConstraints("app_acl_r2_manifest_revisions", constraints)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("validateAppACLR2M2RelationConstraints() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validateAppACLR2M2RelationConstraints() error = %v, want %q rejection", err, tt.want)
			}
		})
	}
}

func TestValidateAppACLR2M2RelationConstraintsAcceptsLivePG16DesignSearchPathDeparse(t *testing.T) {
	// Independent regression: literal live PG16 design-path deparse must satisfy
	// ExactM2 constraint validation. Do not consult production expected constants.
	if err := validateAppACLR2M2RelationConstraints("app_acl_r2_manifest_head", appACLR2LivePG16DesignPathHeadConstraints()); err != nil {
		t.Fatalf("validateAppACLR2M2RelationConstraints(head, live PG16 design-path deparse) error = %v", err)
	}
	if err := validateAppACLR2M2RelationConstraints("app_acl_r2_manifest_revisions", appACLR2LivePG16DesignPathRevisionConstraints()); err != nil {
		t.Fatalf("validateAppACLR2M2RelationConstraints(revisions, live PG16 design-path deparse) error = %v", err)
	}
}

func TestValidateAppACLR2M2RelationConstraintsRejectsNonDesignSearchPathDeparseVariants(t *testing.T) {
	// Adversarial: public-qualified FKs (search_path=pg_catalog only) and
	// digest CHECKs missing the live ::text cast must remain rejected.
	tests := []struct {
		name        string
		relation    string
		constraints []AppACLR2ReceiptTableConstraintCatalogV1
		want        string
	}{
		{
			name:     "public-qualified head foreign key",
			relation: "app_acl_r2_manifest_head",
			constraints: func() []AppACLR2ReceiptTableConstraintCatalogV1 {
				rows := appACLR2LivePG16DesignPathHeadConstraints()
				for i := range rows {
					if rows[i].Type == "f" {
						rows[i].Definition = "FOREIGN KEY (protocol_version, manifest_revision, manifest_digest) REFERENCES public.app_acl_r2_manifest_revisions(protocol_version, manifest_revision, manifest_digest) ON DELETE RESTRICT"
					}
				}
				return rows
			}(),
			want: "foreign key",
		},
		{
			name:     "public-qualified revisions foreign key",
			relation: "app_acl_r2_manifest_revisions",
			constraints: func() []AppACLR2ReceiptTableConstraintCatalogV1 {
				rows := appACLR2LivePG16DesignPathRevisionConstraints()
				for i := range rows {
					if rows[i].Type == "f" {
						rows[i].Definition = "FOREIGN KEY (m1_revision, m1_manifest_digest) REFERENCES public.app_acl_manifest_revisions(manifest_revision, manifest_digest) ON DELETE RESTRICT"
					}
				}
				return rows
			}(),
			want: "foreign key",
		},
		{
			name:     "digest check missing text cast",
			relation: "app_acl_r2_manifest_revisions",
			constraints: func() []AppACLR2ReceiptTableConstraintCatalogV1 {
				rows := appACLR2LivePG16DesignPathRevisionConstraints()
				for i := range rows {
					if rows[i].Definition == appACLR2LivePG16DesignPathSourceDigest {
						rows[i].Definition = "CHECK (r2_source_set_digest = record_platform_internal.digest(r2_source_set_body, 'sha256'))"
					}
				}
				return rows
			}(),
			want: "unexpected check",
		},
		{
			name:     "digest check wrong algorithm",
			relation: "app_acl_r2_manifest_revisions",
			constraints: func() []AppACLR2ReceiptTableConstraintCatalogV1 {
				rows := appACLR2LivePG16DesignPathRevisionConstraints()
				for i := range rows {
					if rows[i].Definition == appACLR2LivePG16DesignPathSourceDigest {
						rows[i].Definition = "CHECK (r2_source_set_digest = record_platform_internal.digest(r2_source_set_body, 'sha512'::text))"
					}
				}
				return rows
			}(),
			want: "unexpected check",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAppACLR2M2RelationConstraints(tt.relation, tt.constraints)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validateAppACLR2M2RelationConstraints() error = %v, want %q rejection", err, tt.want)
			}
		})
	}
}

func TestVerifyAppACLR2M2RelationPhysicalContractReadsExactPG16Facts(t *testing.T) {
	tests := []struct {
		name   string
		mutate func([][][]any)
		want   string
	}{
		{name: "exact catalog"},
		{name: "unlogged relation", mutate: func(queries [][][]any) { queries[0][0][1] = "u" }, want: "persistence"},
		{name: "missing revision column", mutate: func(queries [][][]any) { queries[1] = queries[1][:len(queries[1])-1] }, want: "columns"},
		{name: "inherits parent", mutate: func(queries [][][]any) {
			queries[2] = append(queries[2], []any{"app_acl_r2_manifest_head", true, false})
		}, want: "inheritance"},
		{name: "invalid primary index", mutate: func(queries [][][]any) { queries[3][0][7] = false }, want: "primary key"},
		{name: "fk missing referenced index", mutate: func(queries [][][]any) {
			for _, constraintQuery := range queries[3:] {
				for _, row := range constraintQuery {
					if row[1] == "f" {
						row[4] = int64(0)
						row[5] = false
						row[6] = false
						row[7] = false
					}
				}
			}
		}, want: "foreign key"},
		{name: "unexpected check", mutate: func(queries [][][]any) {
			queries[3] = append(queries[3], []any{"app_acl_r2_manifest_head", "c", "CHECK (false)", true, int64(0), false, false, false})
		}, want: "unexpected check"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queries := appACLR2M2PhysicalCatalogQueryRows()
			if tt.mutate != nil {
				tt.mutate(queries)
			}
			tx := &appACLR2M2ScriptedQueryTx{queryRows: queries}
			err := verifyAppACLR2M2RelationPhysicalContractInTx(context.Background(), tx, []string{
				"app_acl_r2_manifest_head",
				"app_acl_r2_manifest_revisions",
			})
			if tt.want == "" {
				if err != nil {
					t.Fatalf("verifyAppACLR2M2RelationPhysicalContractInTx() error = %v", err)
				}
				if len(tx.queryRows) != 0 {
					t.Fatalf("M2 physical reader left %d scripted queries unused", len(tx.queryRows))
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("verifyAppACLR2M2RelationPhysicalContractInTx() error = %v, want %q rejection", err, tt.want)
			}
		})
	}
}

func TestAppACLR2M2ManifestReadersMapSQLScanOrder(t *testing.T) {
	shape := validAppACLR2FinalizedCatalogShape(t)
	tx := &appACLR2M2ScriptedQueryTx{queryRows: [][][]any{
		{appACLR2M2ManifestReaderRow(shape.M2Revisions[0])},
		{appACLR2M2HeadReaderRow(shape.M2Heads[0])},
	}}

	revisions, err := readAppACLR2ManifestRowsInTx(context.Background(), tx)
	if err != nil {
		t.Fatalf("readAppACLR2ManifestRowsInTx() error = %v", err)
	}
	heads, err := readAppACLR2ManifestHeadRowsInTx(context.Background(), tx)
	if err != nil {
		t.Fatalf("readAppACLR2ManifestHeadRowsInTx() error = %v", err)
	}
	if !reflect.DeepEqual(revisions, shape.M2Revisions) {
		t.Fatalf("M2 revision reader = %#v, want %#v", revisions, shape.M2Revisions)
	}
	if !reflect.DeepEqual(heads, shape.M2Heads) {
		t.Fatalf("M2 head reader = %#v, want %#v", heads, shape.M2Heads)
	}
	if len(tx.queryRows) != 0 {
		t.Fatalf("M2 row/head readers left %d scripted queries unused", len(tx.queryRows))
	}
}

func TestAppACLR2ExactOneReadersObserveAtMostTwoRows(t *testing.T) {
	tests := []struct {
		name string
		kind string
		read func(context.Context, pgx.Tx) (int, error)
	}{
		{
			name: "receipt",
			kind: "receipt",
			read: func(ctx context.Context, tx pgx.Tx) (int, error) {
				rows, err := readAppACLR2ReceiptRowsInTx(ctx, tx)
				return len(rows), err
			},
		},
		{
			name: "M2 revisions",
			kind: "manifest",
			read: func(ctx context.Context, tx pgx.Tx) (int, error) {
				rows, err := readAppACLR2ManifestRowsInTx(ctx, tx)
				return len(rows), err
			},
		},
		{
			name: "M2 head",
			kind: "head",
			read: func(ctx context.Context, tx pgx.Tx) (int, error) {
				rows, err := readAppACLR2ManifestHeadRowsInTx(ctx, tx)
				return len(rows), err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := &appACLR2BoundedReaderRows{kind: tt.kind, count: 3}
			tx := &appACLR2BoundedReaderTx{rows: rows}

			count, err := tt.read(context.Background(), tx)
			if err != nil {
				t.Fatalf("reader error = %v", err)
			}
			if count != 2 {
				t.Fatalf("reader retained %d rows, want exactly two duplicate witnesses", count)
			}
			if rows.thirdConsumed || rows.nextCalls != 2 {
				t.Fatalf("reader consumed a third row: next calls=%d third=%t", rows.nextCalls, rows.thirdConsumed)
			}
			query := strings.ToLower(tx.query)
			if !strings.Contains(query, "limit 2") || strings.Contains(query, "order by") {
				t.Fatalf("reader query = %q, want unordered server-side LIMIT 2", tx.query)
			}
		})
	}
}

func TestAppACLR2ExactOneReadersPropagateCompletionErrorsAfterTwoRows(t *testing.T) {
	tests := []struct {
		name string
		kind string
		read func(context.Context, pgx.Tx) error
	}{
		{
			name: "receipt",
			kind: "receipt",
			read: func(ctx context.Context, tx pgx.Tx) error {
				_, err := readAppACLR2ReceiptRowsInTx(ctx, tx)
				return err
			},
		},
		{
			name: "M2 revisions",
			kind: "manifest",
			read: func(ctx context.Context, tx pgx.Tx) error {
				_, err := readAppACLR2ManifestRowsInTx(ctx, tx)
				return err
			},
		},
		{
			name: "M2 head",
			kind: "head",
			read: func(ctx context.Context, tx pgx.Tx) error {
				_, err := readAppACLR2ManifestHeadRowsInTx(ctx, tx)
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := &pgconn.PgError{Code: "57014", Message: "query canceled while completing"}
			bounded := &appACLR2BoundedReaderRows{kind: tt.kind, count: 2}
			rows := &appACLR2CompletionErrorRows{
				appACLR2BoundedReaderRows: bounded,
				completionErr:             original,
			}
			tx := &appACLR2BoundedReaderTx{rows: rows}

			err := tt.read(context.Background(), tx)
			if bounded.nextCalls != 2 || bounded.scanCalls != 2 || bounded.thirdConsumed {
				t.Fatalf("reader lifecycle = Next:%d Scan:%d third:%t, want exactly two successful rows", bounded.nextCalls, bounded.scanCalls, bounded.thirdConsumed)
			}
			if !rows.closed || rows.closeCalls != 1 || rows.Err() != original {
				t.Fatalf("reader completion lifecycle = closed:%t close_calls:%d err:%v, want Close to expose original completion error", rows.closed, rows.closeCalls, rows.Err())
			}
			var got *pgconn.PgError
			if !errors.As(err, &got) || got != original {
				t.Fatalf("reader error = %v, want original PostgreSQL completion error", err)
			}
		})
	}
}

func TestReadAppACLR2CatalogPredicatesInTxPropagatesReceiptCompletionErrorAfterStructuralRow(t *testing.T) {
	original := &pgconn.PgError{Code: "57014", Message: "query canceled while completing structurally drifted receipt evidence"}
	bounded := &appACLR2BoundedReaderRows{
		kind:          "receipt",
		count:         1,
		oversizedBody: "receipt",
	}
	rows := &appACLR2CompletionErrorRows{
		appACLR2BoundedReaderRows: bounded,
		completionErr:             original,
	}
	reservedObjects := append([]AppACLR2ReservedCatalogObjectV1{
		{OID: 1001, Kind: "relation", Schema: "public", Identity: "app_acl_r2_bootstrap_receipt", Detail: "r"},
	}, appACLR2CompletionPipelineM2ReservedObjects()...)
	tx := &appACLR2CompletionPipelineTx{
		appACLR2PublicR1Tx: newAppACLR2PublicR1Tx(t, "center_runtime", "center_runtime", reservedObjects),
		target:             "receipt",
		targetRows:         rows,
	}

	_, err := ReadAppACLR2CatalogPredicatesInTx(context.Background(), tx)
	if !tx.targetObserved {
		t.Fatal("catalog predicate reader did not reach the structural receipt evidence reader")
	}
	if tx.laterQueryCalls != 0 {
		t.Fatalf("catalog predicate reader ran %d later evidence queries after the receipt completion error", tx.laterQueryCalls)
	}
	if bounded.nextCalls != 1 || bounded.scanCalls != 1 || bounded.thirdConsumed {
		t.Fatalf("receipt reader lifecycle = Next:%d Scan:%d third:%t, want one structural witness only", bounded.nextCalls, bounded.scanCalls, bounded.thirdConsumed)
	}
	if !rows.closed || rows.closeCalls != 1 || rows.Err() != original {
		t.Fatalf("receipt completion lifecycle = closed:%t close_calls:%d err:%v, want Close to expose original completion error", rows.closed, rows.closeCalls, rows.Err())
	}
	var got *pgconn.PgError
	if !errors.As(err, &got) || got != original {
		t.Fatalf("ReadAppACLR2CatalogPredicatesInTx() error = %v, want original PostgreSQL completion error", err)
	}
}

func TestAppACLR2ExactOneBodyReadersRejectOversizedBodiesBeforeTransfer(t *testing.T) {
	tests := []struct {
		name          string
		kind          string
		overflowField string
		read          func(context.Context, pgx.Tx) error
	}{
		{
			name:          "receipt body",
			kind:          "receipt",
			overflowField: "receipt",
			read: func(ctx context.Context, tx pgx.Tx) error {
				_, err := readAppACLR2ReceiptRowsInTx(ctx, tx)
				return err
			},
		},
		{
			name:          "M2 source-set body",
			kind:          "manifest",
			overflowField: "source",
			read: func(ctx context.Context, tx pgx.Tx) error {
				_, err := readAppACLR2ManifestRowsInTx(ctx, tx)
				return err
			},
		},
		{
			name:          "M2 privilege-set body",
			kind:          "manifest",
			overflowField: "privilege",
			read: func(ctx context.Context, tx pgx.Tx) error {
				_, err := readAppACLR2ManifestRowsInTx(ctx, tx)
				return err
			},
		},
		{
			name:          "M2 domain body",
			kind:          "manifest",
			overflowField: "domain",
			read: func(ctx context.Context, tx pgx.Tx) error {
				_, err := readAppACLR2ManifestRowsInTx(ctx, tx)
				return err
			},
		},
		{
			name:          "M2 control ACL body",
			kind:          "manifest",
			overflowField: "control",
			read: func(ctx context.Context, tx pgx.Tx) error {
				_, err := readAppACLR2ManifestRowsInTx(ctx, tx)
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := &appACLR2BoundedReaderRows{kind: tt.kind, count: 1, oversizedBody: tt.overflowField}
			tx := &appACLR2BoundedReaderTx{rows: rows}

			err := tt.read(context.Background(), tx)
			if rows.transferredOversizedBytes != 0 {
				t.Fatalf("reader transferred %d oversized body bytes before rejection", rows.transferredOversizedBytes)
			}
			if err == nil {
				t.Fatal("reader accepted an oversized body, want structural rejection")
			}
			query := strings.ToLower(tx.query)
			if !strings.Contains(query, "octet_length") || !strings.Contains(query, "case when") || !strings.Contains(query, "$1::integer") {
				t.Fatalf("reader query = %q, want an octet_length-guarded body projection", tx.query)
			}
			if !reflect.DeepEqual(tx.arguments, []any{appACLR2MaximumBodyBytes}) {
				t.Fatalf("reader body ceiling arguments = %#v, want %d", tx.arguments, appACLR2MaximumBodyBytes)
			}
		})
	}
}

func TestAppACLR2ExactOneDigestReadersRejectOversizedDigestsBeforeTransfer(t *testing.T) {
	tests := []struct {
		name         string
		kind         string
		digestField  string
		digestColumn string
		read         func(context.Context, pgx.Tx) error
	}{
		{
			name:         "receipt digest",
			kind:         "receipt",
			digestField:  "receipt",
			digestColumn: "receipt_digest",
			read: func(ctx context.Context, tx pgx.Tx) error {
				_, err := readAppACLR2ReceiptRowsInTx(ctx, tx)
				return err
			},
		},
		{
			name:         "M2 M1 manifest digest",
			kind:         "manifest",
			digestField:  "m1_manifest",
			digestColumn: "m1_manifest_digest",
			read: func(ctx context.Context, tx pgx.Tx) error {
				_, err := readAppACLR2ManifestRowsInTx(ctx, tx)
				return err
			},
		},
		{
			name:         "M2 M1 source-set digest",
			kind:         "manifest",
			digestField:  "m1_source",
			digestColumn: "m1_source_set_digest",
			read: func(ctx context.Context, tx pgx.Tx) error {
				_, err := readAppACLR2ManifestRowsInTx(ctx, tx)
				return err
			},
		},
		{
			name:         "M2 M1 privilege-set digest",
			kind:         "manifest",
			digestField:  "m1_privilege",
			digestColumn: "m1_privilege_set_digest",
			read: func(ctx context.Context, tx pgx.Tx) error {
				_, err := readAppACLR2ManifestRowsInTx(ctx, tx)
				return err
			},
		},
		{
			name:         "M2 R2 source-set digest",
			kind:         "manifest",
			digestField:  "r2_source",
			digestColumn: "r2_source_set_digest",
			read: func(ctx context.Context, tx pgx.Tx) error {
				_, err := readAppACLR2ManifestRowsInTx(ctx, tx)
				return err
			},
		},
		{
			name:         "M2 R2 privilege-set digest",
			kind:         "manifest",
			digestField:  "r2_privilege",
			digestColumn: "r2_privilege_set_digest",
			read: func(ctx context.Context, tx pgx.Tx) error {
				_, err := readAppACLR2ManifestRowsInTx(ctx, tx)
				return err
			},
		},
		{
			name:         "M2 domain digest",
			kind:         "manifest",
			digestField:  "domain",
			digestColumn: "domain_digest",
			read: func(ctx context.Context, tx pgx.Tx) error {
				_, err := readAppACLR2ManifestRowsInTx(ctx, tx)
				return err
			},
		},
		{
			name:         "M2 receipt digest",
			kind:         "manifest",
			digestField:  "receipt",
			digestColumn: "receipt_digest",
			read: func(ctx context.Context, tx pgx.Tx) error {
				_, err := readAppACLR2ManifestRowsInTx(ctx, tx)
				return err
			},
		},
		{
			name:         "M2 control ACL digest",
			kind:         "manifest",
			digestField:  "control",
			digestColumn: "control_acl_digest",
			read: func(ctx context.Context, tx pgx.Tx) error {
				_, err := readAppACLR2ManifestRowsInTx(ctx, tx)
				return err
			},
		},
		{
			name:         "M2 manifest digest",
			kind:         "manifest",
			digestField:  "manifest",
			digestColumn: "manifest_digest",
			read: func(ctx context.Context, tx pgx.Tx) error {
				_, err := readAppACLR2ManifestRowsInTx(ctx, tx)
				return err
			},
		},
		{
			name:         "M2 head manifest digest",
			kind:         "head",
			digestField:  "manifest",
			digestColumn: "manifest_digest",
			read: func(ctx context.Context, tx pgx.Tx) error {
				_, err := readAppACLR2ManifestHeadRowsInTx(ctx, tx)
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := &appACLR2BoundedReaderRows{kind: tt.kind, count: 1, oversizedDigest: tt.digestField}
			tx := &appACLR2BoundedReaderTx{rows: rows}

			err := tt.read(context.Background(), tx)
			if rows.transferredOversizedDigestBytes != 0 {
				t.Fatalf("reader transferred %d oversized digest bytes before rejection", rows.transferredOversizedDigestBytes)
			}
			if err == nil || !strings.Contains(err.Error(), "digest size") {
				t.Fatalf("reader error = %v, want observed digest-size rejection", err)
			}
			query := strings.ToLower(tx.query)
			if !strings.Contains(query, "octet_length("+tt.digestColumn+")") ||
				!strings.Contains(query, "then "+tt.digestColumn+" else null::bytea end") {
				t.Fatalf("reader query = %q, want a length-observed exact-32-byte projection for %s", tx.query, tt.digestColumn)
			}
		})
	}
}

func TestAppACLR2ExactOneReadersPreserveOperationalScanErrors(t *testing.T) {
	tests := []struct {
		name string
		kind string
		read func(context.Context, pgx.Tx) error
	}{
		{
			name: "receipt",
			kind: "receipt",
			read: func(ctx context.Context, tx pgx.Tx) error {
				_, err := readAppACLR2ReceiptRowsInTx(ctx, tx)
				return err
			},
		},
		{
			name: "M2 revisions",
			kind: "manifest",
			read: func(ctx context.Context, tx pgx.Tx) error {
				_, err := readAppACLR2ManifestRowsInTx(ctx, tx)
				return err
			},
		},
		{
			name: "M2 head",
			kind: "head",
			read: func(ctx context.Context, tx pgx.Tx) error {
				_, err := readAppACLR2ManifestHeadRowsInTx(ctx, tx)
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := &pgconn.PgError{Code: "42501", Message: "permission denied"}
			tx := &appACLR2BoundedReaderTx{rows: &appACLR2BoundedReaderRows{kind: tt.kind, count: 1, scanErr: original}}

			err := tt.read(context.Background(), tx)
			var got *pgconn.PgError
			if !errors.As(err, &got) || got != original {
				t.Fatalf("reader error = %v, want original PostgreSQL scan error", err)
			}
		})
	}
}

func TestReadAppACLR2CatalogPredicatesInTxStopsAfterExactOneReaderCompletionError(t *testing.T) {
	tests := []struct {
		name            string
		target          string
		kind            string
		reservedObjects []AppACLR2ReservedCatalogObjectV1
	}{
		{
			name:   "receipt",
			target: "receipt",
			kind:   "receipt",
			reservedObjects: []AppACLR2ReservedCatalogObjectV1{
				{OID: 1001, Kind: "relation", Schema: "public", Identity: "app_acl_r2_bootstrap_receipt", Detail: "r"},
				{OID: 2001, Kind: "relation", Schema: "public", Identity: "app_acl_r2_manifest_revisions", Detail: "r"},
			},
		},
		{
			name:   "M2 revisions",
			target: "manifest",
			kind:   "manifest",
			reservedObjects: []AppACLR2ReservedCatalogObjectV1{
				{OID: 2001, Kind: "relation", Schema: "public", Identity: "app_acl_r2_manifest_revisions", Detail: "r"},
				{OID: 2002, Kind: "relation", Schema: "public", Identity: "app_acl_r2_manifest_head", Detail: "r"},
			},
		},
		{
			name:            "M2 head",
			target:          "head",
			kind:            "head",
			reservedObjects: appACLR2CompletionPipelineM2ReservedObjects(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := &pgconn.PgError{Code: "57014", Message: "query canceled while completing"}
			bounded := &appACLR2BoundedReaderRows{kind: tt.kind, count: 2}
			rows := &appACLR2CompletionErrorRows{
				appACLR2BoundedReaderRows: bounded,
				completionErr:             original,
			}
			tx := &appACLR2CompletionPipelineTx{
				appACLR2PublicR1Tx: newAppACLR2PublicR1Tx(t, "center_runtime", "center_runtime", tt.reservedObjects),
				target:             tt.target,
				targetRows:         rows,
			}

			_, err := ReadAppACLR2CatalogPredicatesInTx(context.Background(), tx)
			if !tx.targetObserved {
				t.Fatal("catalog predicate reader did not reach the completion-error evidence reader")
			}
			if tx.laterQueryCalls != 0 {
				t.Fatalf("catalog predicate reader ran %d later evidence queries after the completion error", tx.laterQueryCalls)
			}
			if bounded.nextCalls != 2 || bounded.scanCalls != 2 || bounded.thirdConsumed {
				t.Fatalf("reader lifecycle = Next:%d Scan:%d third:%t, want exactly two successful rows", bounded.nextCalls, bounded.scanCalls, bounded.thirdConsumed)
			}
			if !rows.closed || rows.closeCalls != 1 || rows.Err() != original {
				t.Fatalf("reader completion lifecycle = closed:%t close_calls:%d err:%v, want Close to expose original completion error", rows.closed, rows.closeCalls, rows.Err())
			}
			var got *pgconn.PgError
			if !errors.As(err, &got) || got != original {
				t.Fatalf("ReadAppACLR2CatalogPredicatesInTx() error = %v, want original PostgreSQL completion error", err)
			}
		})
	}
}

func TestAppACLR2CatalogPredicateReaderRejectsIncompleteDependencies(t *testing.T) {
	_, err := readAppACLR2CatalogPredicatesInTxWithDependencies(context.Background(), &fakeAppACLR2CatalogPredicateTx{}, appACLR2CatalogPredicateReadDependencies{})
	if err == nil || !strings.Contains(err.Error(), "dependencies are incomplete") {
		t.Fatalf("readAppACLR2CatalogPredicatesInTxWithDependencies() error = %v, want incomplete-dependency rejection", err)
	}
}

func TestVerifyAppACLR2M2DefaultACLAbsenceUsesDirectMigratorOID(t *testing.T) {
	for _, tt := range []struct {
		name  string
		count int64
		want  string
	}{
		{name: "exact absence"},
		{name: "relevant default ACL", count: 1, want: "default ACL drift"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tx := &appACLR2M2DefaultACLTx{count: tt.count}
			err := verifyAppACLR2M2DefaultACLAbsenceInTx(context.Background(), tx, 21)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("verifyAppACLR2M2DefaultACLAbsenceInTx() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("verifyAppACLR2M2DefaultACLAbsenceInTx() error = %v, want %q rejection", err, tt.want)
			}
		})
	}
}

func TestAppACLR2CatalogPredicatesRejectM2DomainBodyNotBoundToReceipt(t *testing.T) {
	shape := validAppACLR2FinalizedCatalogShape(t)
	row := &shape.M2Revisions[0]
	domain, err := ParseCanonicalAppACLDomainR2BodyV1(row.Manifest.DomainBody)
	if err != nil {
		t.Fatalf("ParseCanonicalAppACLDomainR2BodyV1() error = %v", err)
	}
	domain.DatabaseOID++
	domainBody, err := CanonicalAppACLDomainR2BodyV1(domain)
	if err != nil {
		t.Fatalf("CanonicalAppACLDomainR2BodyV1() error = %v", err)
	}
	row.Manifest.DomainBody = domainBody
	row.Manifest.DomainDigest = sha256.Sum256(domainBody)
	manifestBody, err := CanonicalAppACLManifestR2BodyV1(row.Manifest)
	if err != nil {
		t.Fatalf("CanonicalAppACLManifestR2BodyV1() error = %v", err)
	}
	row.ManifestDigest = sha256.Sum256(manifestBody)
	shape.M2Heads[0].ManifestDigest = row.ManifestDigest

	predicates := evaluateAppACLR2CatalogShape(shape)
	if predicates.ExactM2 {
		t.Fatal("exact M2 predicate = true with a domain body not bound to the verified receipt")
	}
}

func TestAppACLR2CatalogPredicatesRejectIncompleteAndDriftedM2Shapes(t *testing.T) {
	valid := validAppACLR2FinalizedCatalogShape(t)
	tests := []struct {
		name   string
		mutate func(*appACLR2CatalogShape)
	}{
		{name: "one-sided M2 relation", mutate: func(shape *appACLR2CatalogShape) {
			shape.ReservedObjects = removeAppACLR2ReservedObject(shape.ReservedObjects, "relation|public|app_acl_r2_manifest_head|r")
			shape.ReservedObjects = removeAppACLR2ReservedObject(shape.ReservedObjects, "relation|public|app_acl_r2_manifest_head_pkey|i")
			shape.ReservedObjects = removeAppACLR2ReservedObject(shape.ReservedObjects, "trigger|public|app_acl_r2_manifest_head.app_acl_r2_manifest_head_immutable|user")
			shape.M2Heads = nil
		}},
		{name: "extra reserved object", mutate: func(shape *appACLR2CatalogShape) {
			shape.ReservedObjects = append(shape.ReservedObjects, AppACLR2ReservedCatalogObjectV1{OID: 9000, Kind: "relation", Schema: "public", Identity: "app_acl_r2_extra", Detail: "r"})
		}},
		{name: "wrong L2 owner", mutate: func(shape *appACLR2CatalogShape) {
			shape.L2EvidenceExact = false
		}},
		{name: "wrong M1 link", mutate: func(shape *appACLR2CatalogShape) {
			shape.M2Revisions[0].Manifest.M1ManifestDigest[0] ^= 0xff
		}},
		{name: "wrong M2 head", mutate: func(shape *appACLR2CatalogShape) {
			shape.M2Heads[0].ManifestDigest[0] ^= 0xff
		}},
		{name: "mixed M2 without L2", mutate: func(shape *appACLR2CatalogShape) {
			shape.ReservedObjects = appACLR2M2ReservedObjects()
			shape.L2Rows = nil
			shape.L2EvidenceExact = false
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shape := cloneAppACLR2CatalogShape(valid)
			tt.mutate(&shape)
			predicates := evaluateAppACLR2CatalogShape(shape)
			if predicates.ExactM2 {
				t.Fatal("exact M2 predicate = true, want false")
			}
		})
	}
}

func TestAppACLR2CatalogPredicateReaderBuildsExactFinalizedStateFromCallerTransaction(t *testing.T) {
	shape := validAppACLR2FinalizedCatalogShape(t)
	tx := &fakeAppACLR2CatalogPredicateTx{}
	got, err := readAppACLR2CatalogPredicatesInTxWithDependencies(context.Background(), tx, appACLR2CatalogPredicateReadDependencies{
		verifyFrozen: func(_ context.Context, gotTx pgx.Tx) (FrozenAppACLR1StateV1, error) {
			if gotTx != tx {
				t.Fatalf("frozen verifier transaction = %T %p, want caller transaction %p", gotTx, gotTx, tx)
			}
			return shape.FrozenState, nil
		},
		readReservedObjects: func(_ context.Context, gotTx pgx.Tx) ([]AppACLR2ReservedCatalogObjectV1, error) {
			if gotTx != tx {
				t.Fatalf("reserved-object transaction = %T %p, want caller transaction %p", gotTx, gotTx, tx)
			}
			return append([]AppACLR2ReservedCatalogObjectV1(nil), shape.ReservedObjects...), nil
		},
		readL2Rows: func(_ context.Context, gotTx pgx.Tx) ([]appACLR2ReceiptRowV1, error) {
			if gotTx != tx {
				t.Fatalf("L2 row transaction = %T %p, want caller transaction %p", gotTx, gotTx, tx)
			}
			return append([]appACLR2ReceiptRowV1(nil), shape.L2Rows...), nil
		},
		verifyL2: func(_ context.Context, gotTx pgx.Tx, frozen FrozenAppACLR1StateV1, row appACLR2ReceiptRowV1) error {
			if gotTx != tx {
				t.Fatalf("L2 verifier transaction = %T %p, want caller transaction %p", gotTx, gotTx, tx)
			}
			if !reflect.DeepEqual(frozen, shape.FrozenState) || !reflect.DeepEqual(row, shape.L2Rows[0]) {
				t.Fatal("L2 verifier inputs do not match caller evidence")
			}
			return nil
		},
		readM2Revisions: func(_ context.Context, gotTx pgx.Tx) ([]appACLR2ManifestRowV1, error) {
			if gotTx != tx {
				t.Fatalf("M2 revisions reader transaction = %T %p, want caller transaction %p", gotTx, gotTx, tx)
			}
			return append([]appACLR2ManifestRowV1(nil), shape.M2Revisions...), nil
		},
		readM2Heads: func(_ context.Context, gotTx pgx.Tx) ([]appACLR2ManifestHeadRowV1, error) {
			if gotTx != tx {
				t.Fatalf("M2 head reader transaction = %T %p, want caller transaction %p", gotTx, gotTx, tx)
			}
			return append([]appACLR2ManifestHeadRowV1(nil), shape.M2Heads...), nil
		},
		readM2ControlACL: func(_ context.Context, gotTx pgx.Tx, frozen FrozenAppACLR1StateV1) (AppACLControlACLBodyR2V1, error) {
			if gotTx != tx {
				t.Fatalf("M2 control ACL reader transaction = %T %p, want caller transaction %p", gotTx, gotTx, tx)
			}
			if !reflect.DeepEqual(frozen, shape.FrozenState) {
				t.Fatal("M2 control ACL reader frozen state does not match caller evidence")
			}
			return appACLR2CloneControlACL(shape.M2ControlACL), nil
		},
	})
	if err != nil {
		t.Fatalf("readAppACLR2CatalogPredicatesInTxWithDependencies() error = %v", err)
	}
	if !got.ExactFinalized() {
		t.Fatalf("catalog predicates = %#v, want exact finalized", got)
	}
}

func TestReadAppACLR2CatalogPredicatesInTxRejectsNilTransaction(t *testing.T) {
	_, err := ReadAppACLR2CatalogPredicatesInTx(context.Background(), nil)
	if err == nil {
		t.Fatal("ReadAppACLR2CatalogPredicatesInTx() error = nil, want transaction rejection")
	}
}

func TestAppACLR2CatalogPredicatesExactStateMethodsRejectMixedEvidence(t *testing.T) {
	tests := []struct {
		name       string
		predicates AppACLR2CatalogPredicates
	}{
		{
			name:       "R1 with L2 evidence",
			predicates: AppACLR2CatalogPredicates{ExactL1M1: true, ExactL2: true, L2Absent: true, M2Absent: true},
		},
		{
			name:       "prepared with absent L2",
			predicates: AppACLR2CatalogPredicates{ExactL1M1: true, ExactL2: true, L2Absent: true, M2Absent: true},
		},
		{
			name:       "finalized with absent M2",
			predicates: AppACLR2CatalogPredicates{ExactL1M1: true, ExactL2: true, ExactM2: true, M2Absent: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.predicates.ExactR1() || tt.predicates.ExactPrepared() || tt.predicates.ExactFinalized() {
				t.Fatalf("mixed predicates = %#v, want no exact state", tt.predicates)
			}
		})
	}
}

func TestAppACLR2M2ManifestReaderUsesRecordedAtTimestampMicroseconds(t *testing.T) {
	if !strings.Contains(appACLR2M2ManifestRevisionQuery, "extract(epoch from recorded_at)") {
		t.Fatalf("M2 revision query = %q, want recorded_at timestamp conversion", appACLR2M2ManifestRevisionQuery)
	}
}

func TestAppACLR2M2ManifestReaderCastsDirectMigratorOIDForPGXInt64Scan(t *testing.T) {
	var rawOID int64
	if err := pgtype.NewMap().Scan(pgtype.OIDOID, pgtype.BinaryFormatCode, []byte{0, 0, 0, 21}, &rawOID); err == nil {
		t.Fatal("pgx accepted a raw PostgreSQL oid scan into int64, want incompatibility")
	}

	var castOID int64
	if err := pgtype.NewMap().Scan(pgtype.Int8OID, pgtype.BinaryFormatCode, []byte{0, 0, 0, 0, 0, 0, 0, 21}, &castOID); err != nil {
		t.Fatalf("pgx scan of bigint into int64 error = %v", err)
	}
	if castOID != 21 {
		t.Fatalf("pgx bigint scan = %d, want 21", castOID)
	}
	if !strings.Contains(appACLR2M2ManifestRevisionQuery, "direct_migrator_oid::bigint") {
		t.Fatalf("M2 revision query = %q, want pgx-compatible direct_migrator_oid::bigint", appACLR2M2ManifestRevisionQuery)
	}
}

func TestAppACLR2M2CatalogQueriesCastNameArrays(t *testing.T) {
	source, err := os.ReadFile("app_acl_r2_catalog.go")
	if err != nil {
		t.Fatalf("read catalog source: %v", err)
	}
	for _, want := range []string{
		"rolname = any($1::name[])",
		"relation.relname = any($1::name[])",
		"relation.relname = any($4::name[])",
	} {
		if !strings.Contains(string(source), want) {
			t.Fatalf("catalog source is missing typed name-array query fragment %q", want)
		}
	}
}

func validAppACLR2FinalizedCatalogShape(t *testing.T) appACLR2CatalogShape {
	t.Helper()
	frozen := validFrozenAppACLR1StateFixture(t)
	bootstrap, surface := validAppACLR2CatalogSnapshotFixture(t, frozen)
	receipt, err := CompileAppACLR2BootstrapReceiptFromCatalogV1(bootstrap, surface, frozen)
	if err != nil {
		t.Fatalf("CompileAppACLR2BootstrapReceiptFromCatalogV1() error = %v", err)
	}
	receiptBody, err := CanonicalAppACLR2BootstrapReceiptBodyV1(receipt)
	if err != nil {
		t.Fatalf("CanonicalAppACLR2BootstrapReceiptBodyV1() error = %v", err)
	}
	directMigratorOID := uint32(0)
	for _, role := range receipt.Roles {
		if role.ControlRole == AppACLControlRoleDirectMigratorR2 {
			directMigratorOID = role.OID
		}
	}
	if directMigratorOID == 0 {
		t.Fatal("direct migrator OID = 0")
	}
	controlACLBody, err := CompileAppACLControlACLBodyR2V1(directMigratorOID)
	if err != nil {
		t.Fatalf("CompileAppACLControlACLBodyR2V1() error = %v", err)
	}
	manifest := AppACLManifestR2V1{
		ProtocolVersion:            2,
		ManifestRevision:           2,
		M1Revision:                 frozen.ManifestRevision,
		M1ManifestDigest:           frozen.ManifestDigest,
		M1SourceSetDigest:          frozen.SourceSetDigest,
		M1PrivilegeSetDigest:       frozen.PrivilegeSetDigest,
		M1MigratorCatalogRole:      frozen.DirectMigratorRole,
		DirectMigratorName:         frozen.DirectMigratorRole,
		DirectMigratorOID:          directMigratorOID,
		R2SourceSetBody:            append([]byte(nil), receipt.R2SourceBody...),
		R2SourceSetDigest:          receipt.R2SourceDigest,
		R2PrivilegeSetBody:         append([]byte(nil), receipt.R2PrivilegeBody...),
		R2PrivilegeSetDigest:       receipt.R2PrivilegeDigest,
		DomainBody:                 append([]byte(nil), receipt.DomainBody...),
		DomainDigest:               receipt.DomainDigest,
		ReceiptDigest:              sha256.Sum256(receiptBody),
		ControlACLBody:             controlACLBody,
		ControlACLDigest:           sha256.Sum256(controlACLBody),
		RecordedAtUnixMicroseconds: 1,
	}
	manifestBody, err := CanonicalAppACLManifestR2BodyV1(manifest)
	if err != nil {
		t.Fatalf("CanonicalAppACLManifestR2BodyV1() error = %v", err)
	}
	manifestDigest := sha256.Sum256(manifestBody)
	return appACLR2CatalogShape{
		FrozenExact:     true,
		FrozenState:     frozen,
		ReservedObjects: canonicalPG16AppACLR2FinalizedReservedObjectFixture(),
		L2Rows: []appACLR2ReceiptRowV1{{
			Singleton: true,
			Body:      receiptBody,
			Digest:    sha256.Sum256(receiptBody),
		}},
		L2EvidenceExact: true,
		M2Revisions: []appACLR2ManifestRowV1{{
			Manifest:       manifest,
			ManifestDigest: manifestDigest,
		}},
		M2Heads: []appACLR2ManifestHeadRowV1{{
			Singleton:        true,
			ProtocolVersion:  2,
			ManifestRevision: 2,
			ManifestDigest:   manifestDigest,
		}},
		M2ControlACL: appACLR2ControlACLContract(directMigratorOID),
	}
}

func canonicalPG16AppACLR2FinalizedReservedObjectFixture() []AppACLR2ReservedCatalogObjectV1 {
	return []AppACLR2ReservedCatalogObjectV1{
		{OID: 1001, Kind: "function", Schema: "record_platform_internal", Identity: "record_platform_internal.app_acl_r2_assert_bootstrap_receipt_insert(bytea, bytea)", Detail: "f"},
		{OID: 1002, Kind: "function", Schema: "record_platform_internal", Identity: "record_platform_internal.app_acl_r2_reject_bootstrap_receipt_mutation()", Detail: "f"},
		{OID: 2001, Kind: "function", Schema: "record_platform_internal", Identity: "record_platform_internal.app_acl_r2_reject_manifest_mutation()", Detail: "f"},
		{OID: 1003, Kind: "relation", Schema: "public", Identity: "app_acl_r2_bootstrap_receipt", Detail: "r"},
		{OID: 1004, Kind: "relation", Schema: "public", Identity: "app_acl_r2_bootstrap_receipt_pkey", Detail: "i"},
		{OID: 2002, Kind: "relation", Schema: "public", Identity: "app_acl_r2_manifest_head", Detail: "r"},
		{OID: 2003, Kind: "relation", Schema: "public", Identity: "app_acl_r2_manifest_head_pkey", Detail: "i"},
		{OID: 2004, Kind: "relation", Schema: "public", Identity: "app_acl_r2_manifest_revisions", Detail: "r"},
		{OID: 2005, Kind: "relation", Schema: "public", Identity: "app_acl_r2_manifest_revisions_pkey", Detail: "i"},
		{OID: 2006, Kind: "relation", Schema: "public", Identity: "app_acl_r2_manifest_revisions_protocol_version_manifest_rev_key", Detail: "i"},
		{OID: 1005, Kind: "trigger", Schema: "public", Identity: "app_acl_r2_bootstrap_receipt.app_acl_r2_bootstrap_receipt_immutable", Detail: "user"},
		{OID: 2007, Kind: "trigger", Schema: "public", Identity: "app_acl_r2_manifest_head.app_acl_r2_manifest_head_immutable", Detail: "user"},
		{OID: 2008, Kind: "trigger", Schema: "public", Identity: "app_acl_r2_manifest_revisions.app_acl_r2_manifest_revisions_immutable", Detail: "user"},
	}
}

func removeAppACLR2ReservedObject(objects []AppACLR2ReservedCatalogObjectV1, want string) []AppACLR2ReservedCatalogObjectV1 {
	result := make([]AppACLR2ReservedCatalogObjectV1, 0, len(objects))
	for _, object := range objects {
		key := object.Kind + "|" + object.Schema + "|" + object.Identity + "|" + object.Detail
		if key != want {
			result = append(result, object)
		}
	}
	return result
}

func cloneAppACLR2CatalogShape(value appACLR2CatalogShape) appACLR2CatalogShape {
	value.ReservedObjects = append([]AppACLR2ReservedCatalogObjectV1(nil), value.ReservedObjects...)
	value.L2Rows = append([]appACLR2ReceiptRowV1(nil), value.L2Rows...)
	for index := range value.L2Rows {
		value.L2Rows[index].Body = append([]byte(nil), value.L2Rows[index].Body...)
	}
	value.M2Revisions = append([]appACLR2ManifestRowV1(nil), value.M2Revisions...)
	for index := range value.M2Revisions {
		value.M2Revisions[index].Manifest.R2SourceSetBody = append([]byte(nil), value.M2Revisions[index].Manifest.R2SourceSetBody...)
		value.M2Revisions[index].Manifest.R2PrivilegeSetBody = append([]byte(nil), value.M2Revisions[index].Manifest.R2PrivilegeSetBody...)
		value.M2Revisions[index].Manifest.DomainBody = append([]byte(nil), value.M2Revisions[index].Manifest.DomainBody...)
		value.M2Revisions[index].Manifest.ControlACLBody = append([]byte(nil), value.M2Revisions[index].Manifest.ControlACLBody...)
	}
	value.M2Heads = append([]appACLR2ManifestHeadRowV1(nil), value.M2Heads...)
	value.M2ControlACL = appACLR2CloneControlACL(value.M2ControlACL)
	if !bytes.Equal(value.FrozenState.SourceSetBody, nil) {
		value.FrozenState = cloneFrozenAppACLR1State(value.FrozenState)
	}
	return value
}

type fakeAppACLR2CatalogPredicateTx struct{ pgx.Tx }

type appACLR2PublicBoundaryTx struct {
	pgx.Tx
	queryErr      error
	scanErr       error
	execErr       error
	execSQL       []string
	execCalls     int
	queryCalls    int
	queryRowCalls int
	beginCalls    int
	commitCalls   int
	rollbackCalls int
}

func (tx *appACLR2PublicBoundaryTx) Begin(context.Context) (pgx.Tx, error) {
	tx.beginCalls++
	return nil, fmt.Errorf("unexpected nested APP ACL R2 transaction")
}

func (tx *appACLR2PublicBoundaryTx) Commit(context.Context) error {
	tx.commitCalls++
	return fmt.Errorf("unexpected APP ACL R2 transaction commit")
}

func (tx *appACLR2PublicBoundaryTx) Rollback(context.Context) error {
	tx.rollbackCalls++
	return fmt.Errorf("unexpected APP ACL R2 transaction rollback")
}

func (tx *appACLR2PublicBoundaryTx) Exec(ctx context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	tx.execCalls++
	tx.execSQL = append(tx.execSQL, sql)
	if err := ctx.Err(); err != nil {
		return pgconn.CommandTag{}, err
	}
	if tx.execErr != nil {
		return pgconn.CommandTag{}, tx.execErr
	}
	return pgconn.NewCommandTag("SET"), nil
}

func (tx *appACLR2PublicBoundaryTx) Query(ctx context.Context, _ string, _ ...any) (pgx.Rows, error) {
	tx.queryCalls++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if tx.queryErr != nil {
		return nil, tx.queryErr
	}
	return nil, fmt.Errorf("unexpected APP ACL R2 public boundary query")
}

func (tx *appACLR2PublicBoundaryTx) QueryRow(ctx context.Context, _ string, _ ...any) pgx.Row {
	tx.queryRowCalls++
	if err := ctx.Err(); err != nil {
		return appACLR2PublicRow{err: err}
	}
	if tx.scanErr != nil {
		return appACLR2PublicRow{err: tx.scanErr}
	}
	return appACLR2PublicRow{values: []any{"houfeng_app"}}
}

type appACLR2PublicR1Tx struct {
	pgx.Tx
	sessionUser        string
	currentUser        string
	queryErrorFragment string
	queryError         error
	queries            []string
	queryRows          [][][]any
	queryRowValues     [][]any
	identityQueries    []string
	searchPath         string
	execSQL            []string
	catalogSearchPaths []string
	beginCalls         int
	commitCalls        int
	rollbackCalls      int
}

type appACLR2AbortAfterCatalogQueryTx struct {
	*appACLR2PublicR1Tx
	originalQueryFragment string
	originalErr           error
	abortedTxErr          error
	originalObserved      bool
	laterQueryCalls       int
}

func newAppACLR2PublicR1Tx(
	t *testing.T,
	sessionUser string,
	currentUser string,
	reservedObjects []AppACLR2ReservedCatalogObjectV1,
) *appACLR2PublicR1Tx {
	t.Helper()
	evidence, input, catalog, _ := validFrozenAppACLR1VerifyFixture(t)
	manifest := evidence.Manifests[0]

	manifestRows := [][]any{{
		int64(manifest.ManifestRevision),
		manifest.MigratorCatalogRole,
		appACLR2DigestBytes(manifest.PreviousManifestDigest),
		append([]byte(nil), manifest.CanonicalMigrationSet...),
		appACLR2DigestBytes(manifest.MigrationSetDigest),
		append([]byte(nil), manifest.CanonicalPrivilegeSet...),
		appACLR2DigestBytes(manifest.PrivilegeSetDigest),
		appACLR2DigestBytes(manifest.ManifestDigest),
	}}
	ledgerRows := make([][]any, 0, len(evidence.AppliedMigrations))
	for _, entry := range evidence.AppliedMigrations {
		ledgerRows = append(ledgerRows, []any{entry.Filename, hex.EncodeToString(entry.Checksum[:])})
	}
	roleRows := make([][]any, 0, len(catalog.Roles))
	for _, role := range catalog.Roles {
		roleRows = append(roleRows, []any{
			role.Name,
			role.Login,
			role.Inherit,
			role.Superuser,
			role.CreateDatabase,
			role.CreateRole,
			role.Replication,
			role.BypassRLS,
			role.TemporaryObjects,
			role.SchemaCreate,
		})
	}
	membershipRows := make([][]any, 0, len(catalog.Memberships))
	for _, membership := range catalog.Memberships {
		membershipRows = append(membershipRows, []any{membership.MemberRole, membership.ParentRole})
	}
	ownerRows := make([][]any, 0, len(catalog.Owners))
	for _, owner := range catalog.Owners {
		ownerRows = append(ownerRows, []any{string(owner.ObjectClass), owner.SchemaName, owner.ObjectIdentity, owner.OwnerRole})
	}
	directPrivilegeRows := make([][]any, 0, len(catalog.DirectPrivileges))
	for _, privilege := range catalog.DirectPrivileges {
		directPrivilegeRows = append(directPrivilegeRows, []any{
			string(privilege.ObjectClass),
			privilege.SchemaName,
			privilege.ObjectIdentity,
			privilege.ColumnName,
			privilege.Grantee,
			privilege.Privilege,
			privilege.GrantOption,
		})
	}
	effectivePrivilegeRows := make([][]any, 0, len(catalog.EffectivePrivileges))
	for _, privilege := range catalog.EffectivePrivileges {
		effectivePrivilegeRows = append(effectivePrivilegeRows, []any{
			privilege.Grantee,
			string(privilege.ObjectClass),
			privilege.SchemaName,
			privilege.ObjectIdentity,
			privilege.ColumnName,
			privilege.Privilege,
		})
	}
	columnACLRows := make([][]any, 0, len(catalog.ColumnACLs))
	for _, acl := range catalog.ColumnACLs {
		columnACLRows = append(columnACLRows, []any{acl.SchemaName, acl.RelationName, acl.ColumnName, acl.Grantee, acl.Privilege, acl.GrantOption})
	}
	defaultACLRows := make([][]any, 0, len(catalog.DefaultACLs))
	for _, acl := range catalog.DefaultACLs {
		defaultACLRows = append(defaultACLRows, []any{acl.OwnerRole, acl.SchemaName, acl.ObjectType, acl.Grantee, acl.Privilege, acl.GrantOption})
	}
	functionRows := make([][]any, 0, len(catalog.Functions))
	projectorRows := make([][]any, 0, len(input.ExpectedFunctions))
	for _, function := range catalog.Functions {
		functionRows = append(functionRows, []any{
			function.SchemaName,
			function.Name,
			function.IdentityArguments,
			function.OwnerRole,
			function.Kind,
			function.SecurityDefiner,
			append([]string(nil), function.Config...),
		})
		if function.SchemaName == "public" {
			for _, expected := range input.ExpectedFunctions {
				if function.Identity == expected.Identity {
					projectorRows = append(projectorRows, []any{function.Name, false})
				}
			}
		}
	}
	reservedRows := make([][]any, 0, len(reservedObjects))
	for _, object := range reservedObjects {
		reservedRows = append(reservedRows, []any{object.Kind, object.Schema, int64(object.OID), object.Identity, object.Detail})
	}

	return &appACLR2PublicR1Tx{
		sessionUser: sessionUser,
		currentUser: currentUser,
		searchPath:  "pg_catalog, public",
		queryRows: [][][]any{
			manifestRows,
			ledgerRows,
			roleRows,
			membershipRows,
			ownerRows,
			directPrivilegeRows,
			effectivePrivilegeRows,
			columnACLRows,
			defaultACLRows,
			functionRows,
			projectorRows,
			nil,
			reservedRows,
		},
		queryRowValues: [][]any{
			{evidence.DatabaseName},
			{pgtype.Int8{Int64: int64(evidence.Head.ManifestRevision), Valid: true}, appACLR2DigestBytes(evidence.Head.ManifestDigest)},
			{evidence.DatabaseName},
			{catalog.PGCryptoExtension.ExtensionName, catalog.PGCryptoExtension.SchemaName},
		},
	}
}

func (tx *appACLR2PublicR1Tx) Begin(context.Context) (pgx.Tx, error) {
	tx.beginCalls++
	return nil, fmt.Errorf("unexpected nested APP ACL R2 transaction")
}

func (tx *appACLR2PublicR1Tx) Commit(context.Context) error {
	tx.commitCalls++
	return fmt.Errorf("unexpected APP ACL R2 transaction commit")
}

func (tx *appACLR2PublicR1Tx) Rollback(context.Context) error {
	tx.rollbackCalls++
	return fmt.Errorf("unexpected APP ACL R2 transaction rollback")
}

func (tx *appACLR2PublicR1Tx) Exec(ctx context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	if err := ctx.Err(); err != nil {
		return pgconn.CommandTag{}, err
	}
	tx.execSQL = append(tx.execSQL, sql)
	if sql != "SET LOCAL search_path = pg_catalog, public" {
		return pgconn.CommandTag{}, fmt.Errorf("unexpected APP ACL R2 public Exec statement %q", sql)
	}
	tx.searchPath = "pg_catalog, public"
	return pgconn.NewCommandTag("SET"), nil
}

func (tx *appACLR2PublicR1Tx) Query(ctx context.Context, query string, _ ...any) (pgx.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if appACLR2QueryReadsConnectionIdentity(query) {
		tx.identityQueries = append(tx.identityQueries, query)
		return nil, fmt.Errorf("credential-neutral APP ACL R2 reader queried connection identity")
	}
	tx.catalogSearchPaths = append(tx.catalogSearchPaths, tx.searchPath)
	tx.queries = append(tx.queries, query)
	if tx.queryError != nil && strings.Contains(query, tx.queryErrorFragment) {
		return nil, tx.queryError
	}
	if len(tx.queryRows) == 0 {
		return nil, fmt.Errorf("unexpected APP ACL R2 public query")
	}
	rows := tx.queryRows[0]
	tx.queryRows = tx.queryRows[1:]
	return &appACLR2M2Rows{rows: rows}, nil
}

func (tx *appACLR2AbortAfterCatalogQueryTx) Query(ctx context.Context, query string, arguments ...any) (pgx.Rows, error) {
	if tx.originalObserved {
		tx.catalogSearchPaths = append(tx.catalogSearchPaths, tx.searchPath)
		tx.queries = append(tx.queries, query)
		tx.laterQueryCalls++
		return nil, tx.abortedTxErr
	}
	if strings.Contains(query, tx.originalQueryFragment) {
		tx.catalogSearchPaths = append(tx.catalogSearchPaths, tx.searchPath)
		tx.queries = append(tx.queries, query)
		tx.originalObserved = true
		return nil, tx.originalErr
	}
	return tx.appACLR2PublicR1Tx.Query(ctx, query, arguments...)
}

func (tx *appACLR2PublicR1Tx) QueryRow(ctx context.Context, query string, _ ...any) pgx.Row {
	if err := ctx.Err(); err != nil {
		return appACLR2PublicRow{err: err}
	}
	if appACLR2QueryReadsConnectionIdentity(query) {
		tx.identityQueries = append(tx.identityQueries, query)
		return appACLR2PublicRow{err: fmt.Errorf("credential-neutral APP ACL R2 reader queried connection identity")}
	}
	tx.catalogSearchPaths = append(tx.catalogSearchPaths, tx.searchPath)
	if len(tx.queryRowValues) == 0 {
		return appACLR2PublicRow{err: fmt.Errorf("unexpected APP ACL R2 public query row")}
	}
	values := tx.queryRowValues[0]
	tx.queryRowValues = tx.queryRowValues[1:]
	return appACLR2PublicRow{values: values}
}

func (tx *appACLR2PublicR1Tx) queried(want string) bool {
	for _, query := range tx.queries {
		if query == want {
			return true
		}
	}
	return false
}

func (tx *appACLR2PublicR1Tx) assertCallerOwnedAndComplete(t *testing.T) {
	t.Helper()
	if tx.beginCalls != 0 || tx.commitCalls != 0 || tx.rollbackCalls != 0 {
		t.Fatalf("public APP ACL R2 reader transaction ownership = begin:%d commit:%d rollback:%d, want all zero", tx.beginCalls, tx.commitCalls, tx.rollbackCalls)
	}
	if len(tx.identityQueries) != 0 {
		t.Fatalf("credential-neutral APP ACL R2 reader queried connection identity: %q", tx.identityQueries)
	}
	if len(tx.queryRows) != 0 || len(tx.queryRowValues) != 0 {
		t.Fatalf("public APP ACL R2 reader left scripted evidence unused: queries=%d query_rows=%d", len(tx.queryRows), len(tx.queryRowValues))
	}
}

func appACLR2QueryReadsConnectionIdentity(query string) bool {
	normalized := strings.ToLower(query)
	return strings.Contains(normalized, "session_user") || strings.Contains(normalized, "current_user")
}

type appACLR2PublicRow struct {
	values []any
	err    error
}

func (row appACLR2PublicRow) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	rows := &appACLR2M2Rows{rows: [][]any{row.values}}
	if !rows.Next() {
		return fmt.Errorf("APP ACL R2 public row has no values")
	}
	return rows.Scan(destinations...)
}

type appACLR2M2ColumnACLProbeTx struct {
	pgx.Tx
	columnACLRead bool
}

func (tx *appACLR2M2ColumnACLProbeTx) Query(_ context.Context, query string, _ ...any) (pgx.Rows, error) {
	switch {
	case strings.Contains(query, "attribute.attacl"):
		tx.columnACLRead = true
		return &scriptedAppACLR2ReceiptRows{rows: [][]any{{
			"app_acl_r2_manifest_head", "singleton", "platform_admin", "SELECT", false,
		}}}, nil
	case strings.Contains(query, "has_table_privilege"):
		return &scriptedAppACLR2ReceiptRows{rows: [][]any{
			{"app_acl_r2_manifest_head", true, false, false, false, false, false, false, false, false, false, false, false, false, false},
			{"app_acl_r2_manifest_revisions", true, false, false, false, false, false, false, false, false, false, false, false, false, false},
		}}, nil
	case strings.Contains(query, "aclexplode(relation.relacl)"):
		return &scriptedAppACLR2ReceiptRows{rows: [][]any{
			{"app_acl_r2_manifest_head", int64(20), "SELECT", false},
			{"app_acl_r2_manifest_revisions", int64(20), "SELECT", false},
		}}, nil
	case strings.Contains(query, "from pg_catalog.pg_class relation"):
		return &scriptedAppACLR2ReceiptRows{rows: [][]any{
			{"app_acl_r2_manifest_head", int64(21), "r"},
			{"app_acl_r2_manifest_revisions", int64(21), "r"},
		}}, nil
	default:
		return nil, fmt.Errorf("unexpected M2 relation query")
	}
}

type appACLR2M2DroppedColumnACLProbeTx struct {
	pgx.Tx
	query string
}

func (tx *appACLR2M2DroppedColumnACLProbeTx) Query(_ context.Context, query string, _ ...any) (pgx.Rows, error) {
	if !strings.Contains(query, "attribute.attacl") {
		return nil, fmt.Errorf("unexpected M2 dropped-column ACL query")
	}
	tx.query = query
	var rows [][]any
	if !strings.Contains(strings.ToLower(query), "attisdropped") {
		rows = [][]any{{
			"app_acl_r2_manifest_revisions",
			"........pg.dropped.21........",
			"platform_admin",
			"SELECT",
			false,
		}}
	}
	return &scriptedAppACLR2ReceiptRows{rows: rows}, nil
}

type appACLR2M2DroppedColumnResidueProbeTx struct {
	pgx.Tx
	query string
}

func (tx *appACLR2M2DroppedColumnResidueProbeTx) Query(_ context.Context, query string, _ ...any) (pgx.Rows, error) {
	if !strings.Contains(query, "attribute.attname") {
		return nil, fmt.Errorf("unexpected M2 dropped-column physical query")
	}
	tx.query = query
	rows := appACLR2M2PhysicalCatalogQueryRows()[1]
	if !strings.Contains(strings.ToLower(query), "attisdropped") {
		rows = append(rows, []any{
			"app_acl_r2_manifest_revisions",
			"........pg.dropped.21........",
			"-",
			false,
			"",
		})
	}
	return &appACLR2M2Rows{rows: rows}, nil
}

type appACLR2M2ACLQueryCaptureTx struct {
	pgx.Tx
	relationGrantQuery string
	relationGrantRows  [][]any
}

func (tx *appACLR2M2ACLQueryCaptureTx) Query(_ context.Context, query string, _ ...any) (pgx.Rows, error) {
	if !strings.Contains(query, "aclexplode(relation.relacl)") {
		return nil, fmt.Errorf("unexpected M2 relation grant query")
	}
	tx.relationGrantQuery = query
	return &scriptedAppACLR2ReceiptRows{rows: tx.relationGrantRows}, nil
}

type appACLR2M2ACLGrantorRow struct {
	name      string
	grantor   int64
	grantee   int64
	privilege string
	grantable bool
}

type appACLR2M2ACLGrantorRows struct {
	pgx.Rows
	rows  []appACLR2M2ACLGrantorRow
	index int
}

func (rows *appACLR2M2ACLGrantorRows) Close() {}

func (rows *appACLR2M2ACLGrantorRows) Err() error { return nil }

func (rows *appACLR2M2ACLGrantorRows) Next() bool {
	if rows.index >= len(rows.rows) {
		return false
	}
	rows.index++
	return true
}

func (rows *appACLR2M2ACLGrantorRows) Scan(destinations ...any) error {
	if rows.index == 0 || rows.index > len(rows.rows) {
		return fmt.Errorf("scan APP ACL R2 grantor row outside iteration")
	}
	row := rows.rows[rows.index-1]
	if row.name == "" {
		if len(destinations) == 3 {
			return scanScriptedAppACLR2ReceiptValues(destinations, []any{row.grantee, row.privilege, row.grantable})
		}
		return scanScriptedAppACLR2ReceiptValues(destinations, []any{row.grantor, row.grantee, row.privilege, row.grantable})
	}
	if len(destinations) == 4 {
		return scanScriptedAppACLR2ReceiptValues(destinations, []any{row.name, row.grantee, row.privilege, row.grantable})
	}
	return scanScriptedAppACLR2ReceiptValues(destinations, []any{row.name, row.grantor, row.grantee, row.privilege, row.grantable})
}

type appACLR2M2ACLGrantorSubstitutionRelationTx struct {
	pgx.Tx
	rows []appACLR2M2ACLGrantorRow
}

func (tx *appACLR2M2ACLGrantorSubstitutionRelationTx) Query(_ context.Context, query string, _ ...any) (pgx.Rows, error) {
	if !strings.Contains(query, "aclexplode(relation.relacl)") {
		return nil, fmt.Errorf("unexpected M2 relation grantor query")
	}
	return &appACLR2M2ACLGrantorRows{rows: tx.rows}, nil
}

type appACLR2M2ACLGrantorSubstitutionFunctionTx struct {
	*appACLR2M2FunctionVerifierTx
	rows []appACLR2M2ACLGrantorRow
}

func (tx *appACLR2M2ACLGrantorSubstitutionFunctionTx) Query(ctx context.Context, query string, arguments ...any) (pgx.Rows, error) {
	if strings.Contains(query, "aclexplode(procedure.proacl)") {
		return &appACLR2M2ACLGrantorRows{rows: tx.rows}, nil
	}
	return tx.appACLR2M2FunctionVerifierTx.Query(ctx, query, arguments...)
}

type appACLR2M2RelationEffectivePrivilegeTx struct {
	pgx.Tx
	rows     [][]any
	wantArgs []any
	query    string
}

func (tx *appACLR2M2RelationEffectivePrivilegeTx) Query(_ context.Context, query string, args ...any) (pgx.Rows, error) {
	if !strings.Contains(query, "has_table_privilege") {
		return nil, fmt.Errorf("unexpected M2 effective table privilege query")
	}
	tx.query = query
	if !reflect.DeepEqual(args, tx.wantArgs) {
		return nil, fmt.Errorf("M2 effective table privilege arguments = %#v, want %#v", args, tx.wantArgs)
	}
	return &appACLR2M2Rows{rows: tx.rows}, nil
}

func appACLR2M2RelationEffectivePrivilegeRow(name string, direct, runtime, admin [7]bool) []any {
	row := []any{name}
	for _, privileges := range [][7]bool{
		direct,
		runtime,
		admin,
	} {
		for _, privilege := range privileges {
			row = append(row, privilege)
		}
	}
	return row
}

type appACLR2BoundedCatalogTextTx struct {
	pgx.Tx
	rowQueue         []pgx.Rows
	functionIdentity []any
	queries          []string
	arguments        [][]any
	queryCalls       int
	laterQueryCalls  int
}

type appACLR2M2FunctionVerifierTx struct {
	*scriptedAppACLR2ReceiptTx
}

func newAppACLR2M2FunctionVerifierTx(tx *scriptedAppACLR2ReceiptTx) *appACLR2M2FunctionVerifierTx {
	return &appACLR2M2FunctionVerifierTx{scriptedAppACLR2ReceiptTx: tx}
}

func (tx *appACLR2M2FunctionVerifierTx) Query(ctx context.Context, query string, arguments ...any) (pgx.Rows, error) {
	if !strings.Contains(query, "pg_catalog.pg_get_functiondef") {
		return tx.scriptedAppACLR2ReceiptTx.Query(ctx, query, arguments...)
	}
	tx.queryTexts = append(tx.queryTexts, query)
	if len(tx.queries) == 0 {
		return nil, fmt.Errorf("unexpected APP ACL R2 helper profile query")
	}
	profileQuery := tx.queries[0]
	tx.queries = tx.queries[1:]
	if profileQuery.checkArgs != nil {
		if err := profileQuery.checkArgs(arguments); err != nil {
			return nil, err
		}
	}
	want := appACLR2M2FunctionProfile(0)
	return &appACLR2BoundedCatalogTextRows{
		kind:              "function",
		rawRows:           profileQuery.rows,
		definitionMaximum: len(want.Definition),
		sourceMaximum:     len(want.Source),
	}, nil
}

func (tx *appACLR2BoundedCatalogTextTx) Query(_ context.Context, query string, arguments ...any) (pgx.Rows, error) {
	tx.queryCalls++
	if tx.queryCalls > 1 {
		tx.laterQueryCalls++
	}
	tx.queries = append(tx.queries, query)
	tx.arguments = append(tx.arguments, append([]any(nil), arguments...))
	if len(tx.rowQueue) == 0 {
		return nil, fmt.Errorf("unexpected later bounded catalog text query")
	}
	rows := tx.rowQueue[0]
	tx.rowQueue = tx.rowQueue[1:]
	return rows, nil
}

func (tx *appACLR2BoundedCatalogTextTx) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	if len(tx.functionIdentity) == 0 {
		return scriptedAppACLR2ReceiptRow{err: fmt.Errorf("unexpected bounded catalog text QueryRow")}
	}
	return scriptedAppACLR2ReceiptRow{values: append([]any(nil), tx.functionIdentity...)}
}

type appACLR2BoundedCatalogTextRows struct {
	pgx.Rows
	kind                      string
	rawRows                   [][]any
	overflowField             string
	definitionMaximum         int
	sourceMaximum             int
	completionErr             error
	next                      int
	current                   []any
	scanCalls                 int
	transferredOversizedBytes int
	closed                    bool
	closeCalls                int
}

func (rows *appACLR2BoundedCatalogTextRows) Close() {
	if rows.closed {
		return
	}
	rows.closed = true
	rows.closeCalls++
}

func (rows *appACLR2BoundedCatalogTextRows) Err() error {
	if rows.closed {
		return rows.completionErr
	}
	return nil
}

func (rows *appACLR2BoundedCatalogTextRows) Next() bool {
	if rows.next >= len(rows.rawRows) {
		rows.Close()
		return false
	}
	rows.current = rows.rawRows[rows.next]
	rows.next++
	return true
}

func (rows *appACLR2BoundedCatalogTextRows) Scan(destinations ...any) error {
	if rows.current == nil {
		return fmt.Errorf("scan bounded catalog text row without Next")
	}
	rows.scanCalls++
	values, err := rows.scanValues(len(destinations))
	if err != nil {
		return err
	}
	if len(values) != len(destinations) {
		return fmt.Errorf("bounded %s row has %d values, scan has %d destinations", rows.kind, len(values), len(destinations))
	}
	for index, value := range values {
		if err := appACLR2AssignBoundedReaderValue(destinations[index], value); err != nil {
			return fmt.Errorf("assign bounded %s text value %d: %w", rows.kind, index, err)
		}
	}
	return nil
}

func (rows *appACLR2BoundedCatalogTextRows) scanValues(destinationCount int) ([]any, error) {
	raw := append([]any(nil), rows.current...)
	switch rows.kind {
	case "function":
		if len(raw) != 31 {
			return nil, fmt.Errorf("bounded function profile row has %d raw values", len(raw))
		}
		definition, _ := raw[29].(string)
		source, _ := raw[30].(string)
		switch destinationCount {
		case 31:
			_, raw[29] = rows.textProjection("definition", definition, rows.definitionMaximum, false, true)
			_, raw[30] = rows.textProjection("source", source, rows.sourceMaximum, false, true)
			return raw, nil
		case 33:
			definitionSize, definitionPayload := rows.textProjection("definition", definition, rows.definitionMaximum, true, true)
			sourceSize, sourcePayload := rows.textProjection("source", source, rows.sourceMaximum, true, true)
			values := make([]any, 0, 33)
			values = append(values, raw[:29]...)
			values = append(values, definitionSize, definitionPayload, sourceSize, sourcePayload)
			return values, nil
		}
	case "constraint":
		if len(raw) != 8 {
			return nil, fmt.Errorf("bounded constraint row has %d raw values", len(raw))
		}
		definition, _ := raw[2].(string)
		switch destinationCount {
		case 8:
			_, raw[2] = rows.textProjection("definition", definition, rows.definitionMaximum, false, true)
			return raw, nil
		case 9:
			definitionSize, definitionPayload := rows.textProjection("definition", definition, rows.definitionMaximum, true, true)
			values := make([]any, 0, 9)
			values = append(values, raw[:2]...)
			values = append(values, definitionSize, definitionPayload)
			values = append(values, raw[3:]...)
			return values, nil
		}
	case "trigger":
		if len(raw) != 19 {
			return nil, fmt.Errorf("bounded trigger row has %d raw values", len(raw))
		}
		internal, _ := raw[6].(bool)
		definition, _ := raw[12].(string)
		constraintDefinition, _ := raw[14].(string)
		switch destinationCount {
		case 19:
			_, raw[12] = rows.textProjection("trigger_definition", definition, rows.definitionMaximum, false, !internal)
			_, raw[14] = rows.textProjection("constraint_definition", constraintDefinition, rows.sourceMaximum, false, internal)
			return raw, nil
		case 21:
			definitionSize, definitionPayload := rows.textProjection("trigger_definition", definition, rows.definitionMaximum, true, !internal)
			constraintSize, constraintPayload := rows.textProjection("constraint_definition", constraintDefinition, rows.sourceMaximum, true, internal)
			values := make([]any, 0, 21)
			values = append(values, raw[:12]...)
			values = append(values, definitionSize, definitionPayload, raw[13], constraintSize, constraintPayload)
			values = append(values, raw[15:]...)
			return values, nil
		}
	default:
		return nil, fmt.Errorf("unknown bounded catalog text kind %q", rows.kind)
	}
	return nil, fmt.Errorf("bounded %s reader has unexpected destination count %d", rows.kind, destinationCount)
}

func (rows *appACLR2BoundedCatalogTextRows) textProjection(field, value string, maximum int, guarded, applicable bool) (int64, any) {
	if !applicable {
		if guarded {
			return 0, nil
		}
		return 0, value
	}
	if rows.overflowField == field {
		size := maximum + 1
		if guarded {
			return int64(size), nil
		}
		rows.transferredOversizedBytes += size
		return int64(size), strings.Repeat("x", size)
	}
	if guarded {
		return int64(len(value)), []byte(value)
	}
	return int64(len(value)), value
}

type appACLR2M2FunctionDefinitionProbeTx struct {
	pgx.Tx
	profileRead bool
}

func (tx *appACLR2M2FunctionDefinitionProbeTx) Query(_ context.Context, query string, _ ...any) (pgx.Rows, error) {
	if strings.Contains(query, "pg_get_functiondef") {
		tx.profileRead = true
		want := appACLR2M2FunctionProfile(0)
		return &appACLR2BoundedCatalogTextRows{
			kind:              "function",
			rawRows:           [][]any{appACLR2M2FunctionProfileRow("begin\n  return null;\nend;")},
			definitionMaximum: len(want.Definition),
			sourceMaximum:     len(want.Source),
		}, nil
	}
	if strings.Contains(query, "aclexplode(procedure.proacl)") {
		return &scriptedAppACLR2ReceiptRows{}, nil
	}
	return nil, fmt.Errorf("unexpected M2 function query")
}

func (tx *appACLR2M2FunctionDefinitionProbeTx) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	if strings.Contains(query, "has_function_privilege") {
		return scriptedAppACLR2ReceiptRow{values: []any{false, false}}
	}
	return scriptedAppACLR2ReceiptRow{values: []any{int64(500), int64(21), "f"}}
}

func appACLR2M2FunctionProfileRow(source string) []any {
	profile := appACLR2M2FunctionProfile(21)
	profile.Source = source
	return []any{
		profile.Schema,
		profile.Name,
		profile.IdentityArguments,
		profile.Identity,
		int64(profile.OwnerOID),
		profile.Kind,
		profile.Result,
		profile.Language,
		profile.Volatility,
		profile.Parallel,
		profile.SecurityDefiner,
		profile.Strict,
		profile.Leakproof,
		profile.ReturnsSet,
		profile.Cost,
		profile.Rows,
		int64(profile.SupportFunctionOID),
		int64(profile.VariadicTypeOID),
		int64(profile.ArgumentCount),
		int64(profile.ArgumentDefaultCount),
		profile.InputArgumentTypes,
		profile.AllArgumentTypesNull,
		profile.ArgumentModesNull,
		profile.ArgumentNamesNull,
		profile.ArgumentDefaultsNull,
		profile.TransformTypesNull,
		profile.BinaryNull,
		profile.SQLBodyNull,
		profile.Config,
		profile.Definition,
		profile.Source,
	}
}

type appACLR2M2TriggerDefinitionProbeTx struct {
	pgx.Tx
	definitionRead bool
}

func (tx *appACLR2M2TriggerDefinitionProbeTx) Query(_ context.Context, query string, _ ...any) (pgx.Rows, error) {
	if strings.Contains(query, "pg_get_triggerdef") {
		tx.definitionRead = true
		rows := [][]any{
			appACLR2M2TriggerProbeRow("app_acl_r2_manifest_head", "app_acl_r2_manifest_head_immutable", "CREATE TRIGGER altered"),
			appACLR2M2TriggerProbeRow("app_acl_r2_manifest_revisions", "app_acl_r2_manifest_revisions_immutable", "CREATE TRIGGER altered"),
		}
		return &appACLR2BoundedCatalogTextRows{
			kind:              "trigger",
			rawRows:           rows,
			definitionMaximum: appACLR2TestM2UserTriggerDefinitionMaximum(),
			sourceMaximum:     max(len(appACLR2LivePG16DesignPathHeadForeignKey), len(appACLR2LivePG16DesignPathRevForeignKey)),
		}, nil
	}
	return nil, fmt.Errorf("unexpected M2 trigger definition probe query")
}

type appACLR2M2TriggerInternalFKProbeTx struct {
	pgx.Tx
	definitionRead   bool
	includeExtraUser bool
	mutateRows       func([][]any) [][]any
	query            string
}

func (tx *appACLR2M2TriggerInternalFKProbeTx) Query(_ context.Context, query string, _ ...any) (pgx.Rows, error) {
	tx.query = query
	if !strings.Contains(query, "pg_get_triggerdef") {
		return nil, fmt.Errorf("unexpected M2 trigger probe query without pg_get_triggerdef")
	}
	tx.definitionRead = true
	rows := appACLR2M2ExactTriggerRows()
	if tx.includeExtraUser {
		rows = append(rows, appACLR2M2TriggerProbeRow("app_acl_r2_manifest_head", "app_acl_r2_manifest_head_extra", false, "CREATE TRIGGER app_acl_r2_manifest_head_extra BEFORE DELETE ON public.app_acl_r2_manifest_head FOR EACH STATEMENT EXECUTE FUNCTION record_platform_internal.app_acl_r2_reject_manifest_mutation()"))
	}
	if tx.mutateRows != nil {
		rows = tx.mutateRows(rows)
	}
	return &appACLR2BoundedCatalogTextRows{
		kind:              "trigger",
		rawRows:           rows,
		definitionMaximum: appACLR2TestM2UserTriggerDefinitionMaximum(),
		sourceMaximum:     max(len(appACLR2LivePG16DesignPathHeadForeignKey), len(appACLR2LivePG16DesignPathRevForeignKey)),
	}, nil
}

func appACLR2M2ExactTriggerRows() [][]any {
	return [][]any{
		appACLR2M2InternalTriggerProbeRow("app_acl_manifest_revisions", "RI_ConstraintTrigger_a_10001", "pg_catalog.RI_FKey_restrict_del()", 9, appACLR2LivePG16DesignPathRevForeignKey, "public.app_acl_r2_manifest_revisions", "public.app_acl_manifest_revisions", "public.app_acl_r2_manifest_revisions"),
		appACLR2M2InternalTriggerProbeRow("app_acl_manifest_revisions", "RI_ConstraintTrigger_a_10002", "pg_catalog.RI_FKey_noaction_upd()", 17, appACLR2LivePG16DesignPathRevForeignKey, "public.app_acl_r2_manifest_revisions", "public.app_acl_manifest_revisions", "public.app_acl_r2_manifest_revisions"),
		appACLR2M2InternalTriggerProbeRow("app_acl_r2_manifest_head", "RI_ConstraintTrigger_c_10003", "pg_catalog.RI_FKey_check_ins()", 5, appACLR2LivePG16DesignPathHeadForeignKey, "public.app_acl_r2_manifest_head", "public.app_acl_r2_manifest_revisions", "public.app_acl_r2_manifest_revisions"),
		appACLR2M2InternalTriggerProbeRow("app_acl_r2_manifest_head", "RI_ConstraintTrigger_c_10004", "pg_catalog.RI_FKey_check_upd()", 17, appACLR2LivePG16DesignPathHeadForeignKey, "public.app_acl_r2_manifest_head", "public.app_acl_r2_manifest_revisions", "public.app_acl_r2_manifest_revisions"),
		appACLR2M2TriggerProbeRow("app_acl_r2_manifest_head", "app_acl_r2_manifest_head_immutable", false, appACLR2M2TriggerDefinitionPG16("app_acl_r2_manifest_head")),
		appACLR2M2InternalTriggerProbeRow("app_acl_r2_manifest_revisions", "RI_ConstraintTrigger_a_10005", "pg_catalog.RI_FKey_restrict_del()", 9, appACLR2LivePG16DesignPathHeadForeignKey, "public.app_acl_r2_manifest_head", "public.app_acl_r2_manifest_revisions", "public.app_acl_r2_manifest_head"),
		appACLR2M2InternalTriggerProbeRow("app_acl_r2_manifest_revisions", "RI_ConstraintTrigger_a_10006", "pg_catalog.RI_FKey_noaction_upd()", 17, appACLR2LivePG16DesignPathHeadForeignKey, "public.app_acl_r2_manifest_head", "public.app_acl_r2_manifest_revisions", "public.app_acl_r2_manifest_head"),
		appACLR2M2InternalTriggerProbeRow("app_acl_r2_manifest_revisions", "RI_ConstraintTrigger_c_10007", "pg_catalog.RI_FKey_check_ins()", 5, appACLR2LivePG16DesignPathRevForeignKey, "public.app_acl_r2_manifest_revisions", "public.app_acl_manifest_revisions", "public.app_acl_manifest_revisions"),
		appACLR2M2InternalTriggerProbeRow("app_acl_r2_manifest_revisions", "RI_ConstraintTrigger_c_10008", "pg_catalog.RI_FKey_check_upd()", 17, appACLR2LivePG16DesignPathRevForeignKey, "public.app_acl_r2_manifest_revisions", "public.app_acl_manifest_revisions", "public.app_acl_manifest_revisions"),
		appACLR2M2TriggerProbeRow("app_acl_r2_manifest_revisions", "app_acl_r2_manifest_revisions_immutable", false, appACLR2M2TriggerDefinitionPG16("app_acl_r2_manifest_revisions")),
	}
}

func appACLR2TestM2UserTriggerDefinitionMaximum() int {
	return max(
		len(appACLR2M2TriggerDefinitionPG16("app_acl_r2_manifest_head")),
		len(appACLR2M2TriggerDefinitionPG16("app_acl_r2_manifest_revisions")),
	)
}

const (
	appACLR2M2TriggerProbeFunctionIdentity     = 2
	appACLR2M2TriggerProbeEnabled              = 5
	appACLR2M2TriggerProbeConstraintDefinition = 14
	appACLR2M2TriggerProbeBoundRelation        = 18
)

func appACLR2M2TriggerProbeRow(table, trigger string, extras ...any) []any {
	internal := false
	definition := ""
	for _, extra := range extras {
		switch value := extra.(type) {
		case bool:
			internal = value
		case string:
			definition = value
		}
	}
	row := []any{
		table,
		trigger,
		"record_platform_internal.app_acl_r2_reject_manifest_mutation()",
		int64(21),
		int64(21),
		true,
		internal,
		int64(appACLR2ReceiptTriggerTypeBeforeUpdateDeleteTruncateStatement),
		"",
		false,
		int64(0),
		"",
		definition,
		"",
		"",
		false,
		"",
		"",
		"",
	}
	return row
}

func appACLR2M2InternalTriggerProbeRow(
	table string,
	trigger string,
	functionIdentity string,
	triggerType int64,
	constraintDefinition string,
	constraintRelation string,
	referencedRelation string,
	boundRelation string,
) []any {
	return []any{
		table,
		trigger,
		functionIdentity,
		int64(21),
		int64(10),
		true,
		true,
		triggerType,
		"",
		false,
		int64(0),
		"",
		"",
		"f",
		constraintDefinition,
		true,
		constraintRelation,
		referencedRelation,
		boundRelation,
	}
}

func appACLR2M2PhysicalCatalogQueryRows() [][][]any {
	columns := make([][]any, 0, 24)
	for _, name := range []string{"app_acl_r2_manifest_head", "app_acl_r2_manifest_revisions"} {
		for _, column := range appACLR2M2RelationColumns(name) {
			columns = append(columns, []any{name, column.Name, column.Type, column.NotNull, column.DefaultExpression})
		}
	}
	constraintQueries := appACLR2M2ConstraintQueryRows(appACLR2M2ConstraintReaderRows())
	return [][][]any{
		{
			{"app_acl_r2_manifest_head", "p", false, false, false},
			{"app_acl_r2_manifest_revisions", "p", false, false, false},
		},
		columns,
		nil,
		constraintQueries[0],
		constraintQueries[1],
	}
}

func appACLR2M2ConstraintReaderRows() [][]any {
	rows := [][]any{
		{"app_acl_r2_manifest_head", "p", "PRIMARY KEY (singleton)", true, int64(1001), true, true, true},
		// PG16 stores FK conindid as the referenced unique/PK index OID, not zero.
		{"app_acl_r2_manifest_head", "f", "FOREIGN KEY (protocol_version, manifest_revision, manifest_digest) REFERENCES app_acl_r2_manifest_revisions(protocol_version, manifest_revision, manifest_digest) ON DELETE RESTRICT", true, int64(1003), false, true, true},
		{"app_acl_r2_manifest_head", "c", "CHECK (singleton)", true, int64(0), false, false, false},
		{"app_acl_r2_manifest_head", "c", "CHECK (protocol_version = 2)", true, int64(0), false, false, false},
		{"app_acl_r2_manifest_head", "c", "CHECK (manifest_revision = 2)", true, int64(0), false, false, false},
		{"app_acl_r2_manifest_head", "c", "CHECK (octet_length(manifest_digest) = 32)", true, int64(0), false, false, false},
		{"app_acl_r2_manifest_revisions", "p", "PRIMARY KEY (protocol_version, manifest_revision)", true, int64(1002), true, true, true},
		{"app_acl_r2_manifest_revisions", "u", "UNIQUE (protocol_version, manifest_revision, manifest_digest)", true, int64(1003), false, true, true},
		// M1 FK binds to unique (manifest_revision, manifest_digest) on app_acl_manifest_revisions.
		{"app_acl_r2_manifest_revisions", "f", "FOREIGN KEY (m1_revision, m1_manifest_digest) REFERENCES app_acl_manifest_revisions(manifest_revision, manifest_digest) ON DELETE RESTRICT", true, int64(2001), false, true, true},
	}
	for _, definition := range []string{
		"CHECK (protocol_version = 2)",
		"CHECK (manifest_revision = 2)",
		"CHECK (m1_revision = 1)",
		"CHECK (octet_length(m1_manifest_digest) = 32)",
		"CHECK (octet_length(m1_source_set_digest) = 32)",
		"CHECK (octet_length(m1_privilege_set_digest) = 32)",
		"CHECK (octet_length(r2_source_set_digest) = 32)",
		"CHECK (octet_length(r2_privilege_set_digest) = 32)",
		"CHECK (octet_length(domain_digest) = 32)",
		"CHECK (octet_length(receipt_digest) = 32)",
		"CHECK (octet_length(control_acl_digest) = 32)",
		"CHECK (octet_length(manifest_digest) = 32)",
		"CHECK (r2_source_set_digest = record_platform_internal.digest(r2_source_set_body, 'sha256'::text))",
		"CHECK (r2_privilege_set_digest = record_platform_internal.digest(r2_privilege_set_body, 'sha256'::text))",
		"CHECK (domain_digest = record_platform_internal.digest(domain_body, 'sha256'::text))",
		"CHECK (control_acl_digest = record_platform_internal.digest(control_acl_body, 'sha256'::text))",
		appACLR2LivePG16DesignPathManifestDigest,
	} {
		rows = append(rows, []any{"app_acl_r2_manifest_revisions", "c", definition, true, int64(0), false, false, false})
	}
	return rows
}

func appACLR2M2ConstraintRowsForRelation(name string) [][]any {
	var rows [][]any
	for _, row := range appACLR2M2ConstraintReaderRows() {
		if row[0] == name {
			rows = append(rows, append([]any(nil), row...))
		}
	}
	return rows
}

func appACLR2M2ConstraintQueryRows(rows [][]any) [][][]any {
	queries := make([][][]any, 0, 2)
	for _, name := range []string{"app_acl_r2_manifest_head", "app_acl_r2_manifest_revisions"} {
		var relationRows [][]any
		for _, row := range rows {
			if row[0] == name {
				relationRows = append(relationRows, row)
			}
		}
		queries = append(queries, relationRows)
	}
	return queries
}

func appACLR2M2ManifestReaderRow(row appACLR2ManifestRowV1) []any {
	manifest := row.Manifest
	return []any{
		int64(manifest.ProtocolVersion),
		int64(manifest.ManifestRevision),
		int64(manifest.M1Revision),
		int64(sha256.Size),
		appACLR2DigestBytes(manifest.M1ManifestDigest),
		int64(sha256.Size),
		appACLR2DigestBytes(manifest.M1SourceSetDigest),
		int64(sha256.Size),
		appACLR2DigestBytes(manifest.M1PrivilegeSetDigest),
		manifest.M1MigratorCatalogRole,
		manifest.DirectMigratorName,
		int64(manifest.DirectMigratorOID),
		int64(len(manifest.R2SourceSetBody)),
		append([]byte(nil), manifest.R2SourceSetBody...),
		int64(sha256.Size),
		appACLR2DigestBytes(manifest.R2SourceSetDigest),
		int64(len(manifest.R2PrivilegeSetBody)),
		append([]byte(nil), manifest.R2PrivilegeSetBody...),
		int64(sha256.Size),
		appACLR2DigestBytes(manifest.R2PrivilegeSetDigest),
		int64(len(manifest.DomainBody)),
		append([]byte(nil), manifest.DomainBody...),
		int64(sha256.Size),
		appACLR2DigestBytes(manifest.DomainDigest),
		int64(sha256.Size),
		appACLR2DigestBytes(manifest.ReceiptDigest),
		int64(len(manifest.ControlACLBody)),
		append([]byte(nil), manifest.ControlACLBody...),
		int64(sha256.Size),
		appACLR2DigestBytes(manifest.ControlACLDigest),
		int64(sha256.Size),
		appACLR2DigestBytes(row.ManifestDigest),
		manifest.RecordedAtUnixMicroseconds,
	}
}

func appACLR2M2HeadReaderRow(row appACLR2ManifestHeadRowV1) []any {
	return []any{row.Singleton, int64(row.ProtocolVersion), int64(row.ManifestRevision), int64(sha256.Size), appACLR2DigestBytes(row.ManifestDigest)}
}

func appACLR2DigestBytes(value [32]byte) []byte {
	return append([]byte(nil), value[:]...)
}

type appACLR2BoundedReaderTx struct {
	pgx.Tx
	rows      pgx.Rows
	query     string
	arguments []any
}

func (tx *appACLR2BoundedReaderTx) Query(_ context.Context, query string, arguments ...any) (pgx.Rows, error) {
	tx.query = query
	tx.arguments = append([]any(nil), arguments...)
	return tx.rows, nil
}

type appACLR2BoundedReaderRows struct {
	pgx.Rows
	kind                            string
	count                           int
	oversizedBody                   string
	oversizedDigest                 string
	scanErr                         error
	current                         int
	next                            int
	nextCalls                       int
	scanCalls                       int
	thirdConsumed                   bool
	transferredOversizedBytes       int
	transferredOversizedDigestBytes int
}

func (rows *appACLR2BoundedReaderRows) Close() {}

func (rows *appACLR2BoundedReaderRows) Err() error { return nil }

func (rows *appACLR2BoundedReaderRows) Next() bool {
	rows.nextCalls++
	if rows.next >= rows.count {
		return false
	}
	rows.current = rows.next
	rows.next++
	if rows.current >= 2 {
		rows.thirdConsumed = true
	}
	return true
}

func (rows *appACLR2BoundedReaderRows) Scan(destinations ...any) error {
	rows.scanCalls++
	if rows.scanErr != nil {
		return rows.scanErr
	}
	if rows.next == 0 {
		return fmt.Errorf("bounded reader scan without Next")
	}

	var values []any
	switch rows.kind {
	case "receipt":
		values = rows.receiptValues(len(destinations))
	case "manifest":
		values = rows.manifestValues(len(destinations))
	case "head":
		digestLength, digest := rows.digestValue("manifest", len(destinations) == 5)
		switch len(destinations) {
		case 4:
			values = []any{true, int64(2), int64(2), digest}
		case 5:
			values = []any{true, int64(2), int64(2), digestLength, digest}
		}
	default:
		return fmt.Errorf("unknown bounded reader kind %q", rows.kind)
	}
	if len(values) != len(destinations) {
		return fmt.Errorf("bounded %s reader has %d values, scan has %d destinations", rows.kind, len(values), len(destinations))
	}
	for index, value := range values {
		if err := appACLR2AssignBoundedReaderValue(destinations[index], value); err != nil {
			return fmt.Errorf("assign bounded %s reader value %d: %w", rows.kind, index, err)
		}
	}
	return nil
}

func (rows *appACLR2BoundedReaderRows) receiptValues(destinationCount int) []any {
	length, body := rows.bodyValue("receipt", destinationCount == 4 || destinationCount == 5)
	digestLength, digest := rows.digestValue("receipt", destinationCount == 5)
	switch destinationCount {
	case 3:
		return []any{true, body, digest}
	case 4:
		return []any{true, length, body, digest}
	case 5:
		return []any{true, length, body, digestLength, digest}
	default:
		return nil
	}
}

func (rows *appACLR2BoundedReaderRows) manifestValues(destinationCount int) []any {
	guarded := destinationCount == 33
	sourceLength, sourceBody := rows.bodyValue("source", destinationCount == 24 || guarded)
	privilegeLength, privilegeBody := rows.bodyValue("privilege", destinationCount == 24 || guarded)
	domainLength, domainBody := rows.bodyValue("domain", destinationCount == 24 || guarded)
	controlLength, controlBody := rows.bodyValue("control", destinationCount == 24 || guarded)
	m1ManifestLength, m1ManifestDigest := rows.digestValue("m1_manifest", guarded)
	m1SourceLength, m1SourceDigest := rows.digestValue("m1_source", guarded)
	m1PrivilegeLength, m1PrivilegeDigest := rows.digestValue("m1_privilege", guarded)
	r2SourceLength, r2SourceDigest := rows.digestValue("r2_source", guarded)
	r2PrivilegeLength, r2PrivilegeDigest := rows.digestValue("r2_privilege", guarded)
	domainDigestLength, domainDigest := rows.digestValue("domain", guarded)
	receiptDigestLength, receiptDigest := rows.digestValue("receipt", guarded)
	controlDigestLength, controlDigest := rows.digestValue("control", guarded)
	manifestDigestLength, manifestDigest := rows.digestValue("manifest", guarded)
	common := []any{
		int64(2), int64(2), int64(1),
		m1ManifestDigest, m1SourceDigest, m1PrivilegeDigest,
		"direct_migrator", "direct_migrator", int64(21),
	}
	switch destinationCount {
	case 20:
		return append(common,
			sourceBody, r2SourceDigest,
			privilegeBody, r2PrivilegeDigest,
			domainBody, domainDigest, receiptDigest,
			controlBody, controlDigest, manifestDigest, int64(1),
		)
	case 24:
		return append(common,
			sourceLength, sourceBody, r2SourceDigest,
			privilegeLength, privilegeBody, r2PrivilegeDigest,
			domainLength, domainBody, domainDigest, receiptDigest,
			controlLength, controlBody, controlDigest, manifestDigest, int64(1),
		)
	case 33:
		return []any{
			int64(2), int64(2), int64(1),
			m1ManifestLength, m1ManifestDigest,
			m1SourceLength, m1SourceDigest,
			m1PrivilegeLength, m1PrivilegeDigest,
			"direct_migrator", "direct_migrator", int64(21),
			sourceLength, sourceBody, r2SourceLength, r2SourceDigest,
			privilegeLength, privilegeBody, r2PrivilegeLength, r2PrivilegeDigest,
			domainLength, domainBody, domainDigestLength, domainDigest, receiptDigestLength, receiptDigest,
			controlLength, controlBody, controlDigestLength, controlDigest, manifestDigestLength, manifestDigest,
			int64(1),
		}
	default:
		return nil
	}
}

func (rows *appACLR2BoundedReaderRows) digestValue(field string, guarded bool) (int64, []byte) {
	if rows.oversizedDigest == field {
		if guarded {
			return sha256.Size + 1, nil
		}
		digest := make([]byte, sha256.Size+1)
		rows.transferredOversizedDigestBytes += len(digest)
		return int64(len(digest)), digest
	}
	digest := make([]byte, sha256.Size)
	return int64(len(digest)), digest
}

func (rows *appACLR2BoundedReaderRows) bodyValue(field string, guarded bool) (int64, []byte) {
	if rows.oversizedBody == field {
		if guarded {
			return int64(appACLR2MaximumBodyBytes) + 1, nil
		}
		body := make([]byte, appACLR2MaximumBodyBytes+1)
		rows.transferredOversizedBytes += len(body)
		return int64(len(body)), body
	}
	return 1, []byte{byte(rows.current + 1)}
}

// appACLR2CompletionErrorRows models the pgx v5.9.2 Rows lifecycle that is
// relevant to a bounded reader: Close drains query completion and only then
// makes a trailing server error available through Err. It deliberately never
// calls Next itself, because the query's server-side LIMIT 2 leaves no third
// row for the reader to consume.
type appACLR2CompletionErrorRows struct {
	*appACLR2BoundedReaderRows
	completionErr error
	closed        bool
	closeCalls    int
}

func (rows *appACLR2CompletionErrorRows) Close() {
	if rows.closed {
		return
	}
	rows.closed = true
	rows.closeCalls++
}

func (rows *appACLR2CompletionErrorRows) Err() error {
	if rows.closed {
		return rows.completionErr
	}
	return nil
}

type appACLR2CompletionPipelineTx struct {
	*appACLR2PublicR1Tx
	target          string
	targetRows      pgx.Rows
	targetObserved  bool
	laterQueryCalls int
}

func (tx *appACLR2CompletionPipelineTx) Query(ctx context.Context, query string, arguments ...any) (pgx.Rows, error) {
	if tx.targetObserved {
		tx.laterQueryCalls++
		return nil, fmt.Errorf("unexpected later APP ACL R2 evidence query")
	}

	normalized := strings.ToLower(query)
	isReceipt := strings.Contains(normalized, "from public.app_acl_r2_bootstrap_receipt")
	isRevisions := strings.Contains(normalized, "from public.app_acl_r2_manifest_revisions")
	isHead := strings.Contains(normalized, "from public.app_acl_r2_manifest_head")
	isAuthorityProbe := strings.Contains(normalized, "limit 0")
	isBoundedRead := strings.Contains(normalized, "limit 2")

	switch tx.target {
	case "receipt":
		if isReceipt && isAuthorityProbe {
			return &appACLR2M2Rows{}, nil
		}
		if isReceipt && isBoundedRead {
			tx.targetObserved = true
			return tx.targetRows, nil
		}
	case "manifest":
		if isRevisions && isAuthorityProbe {
			return &appACLR2M2Rows{}, nil
		}
		if isRevisions && isBoundedRead {
			tx.targetObserved = true
			return tx.targetRows, nil
		}
	case "head":
		if isRevisions && isAuthorityProbe {
			return &appACLR2M2Rows{}, nil
		}
		if isRevisions && isBoundedRead {
			return &appACLR2BoundedReaderRows{kind: "manifest"}, nil
		}
		if isHead && isAuthorityProbe {
			return &appACLR2M2Rows{}, nil
		}
		if isHead && isBoundedRead {
			tx.targetObserved = true
			return tx.targetRows, nil
		}
	}

	return tx.appACLR2PublicR1Tx.Query(ctx, query, arguments...)
}

func appACLR2CompletionPipelineM2ReservedObjects() []AppACLR2ReservedCatalogObjectV1 {
	objects := append([]AppACLR2ReservedCatalogObjectV1(nil), appACLR2M2ReservedObjects()...)
	for index := range objects {
		objects[index].OID = uint32(3000 + index)
	}
	return objects
}

func appACLR2AssignBoundedReaderValue(destination any, value any) error {
	target := reflect.ValueOf(destination)
	if target.Kind() != reflect.Pointer || target.IsNil() {
		return fmt.Errorf("destination %T is not a non-nil pointer", destination)
	}
	if value == nil {
		switch target.Elem().Kind() {
		case reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
			target.Elem().SetZero()
			return nil
		default:
			return fmt.Errorf("nil cannot scan into %T", destination)
		}
	}
	valueReflect := reflect.ValueOf(value)
	if !valueReflect.Type().AssignableTo(target.Elem().Type()) {
		return fmt.Errorf("value %T cannot scan into %T", value, destination)
	}
	if bytesValue, ok := value.([]byte); ok {
		target.Elem().SetBytes(append([]byte(nil), bytesValue...))
		return nil
	}
	target.Elem().Set(valueReflect)
	return nil
}

type appACLR2M2ScriptedQueryTx struct {
	pgx.Tx
	queryRows  [][][]any
	queryTexts []string
}

func (tx *appACLR2M2ScriptedQueryTx) Query(_ context.Context, query string, arguments ...any) (pgx.Rows, error) {
	tx.queryTexts = append(tx.queryTexts, query)
	if len(tx.queryRows) == 0 {
		return nil, fmt.Errorf("unexpected scripted M2 query")
	}
	rows := tx.queryRows[0]
	tx.queryRows = tx.queryRows[1:]
	if strings.Contains(query, "pg_catalog.pg_get_constraintdef") {
		if len(arguments) != 3 {
			return nil, fmt.Errorf("scripted M2 constraint query has %d arguments, want 3", len(arguments))
		}
		maximum, ok := arguments[1].(int)
		if !ok {
			return nil, fmt.Errorf("scripted M2 constraint definition maximum has type %T, want int", arguments[1])
		}
		return &appACLR2BoundedCatalogTextRows{
			kind:              "constraint",
			rawRows:           rows,
			definitionMaximum: maximum,
		}, nil
	}
	return &appACLR2M2Rows{rows: rows}, nil
}

type appACLR2M2Rows struct {
	pgx.Rows
	rows    [][]any
	current []any
	next    int
	err     error
}

func (rows *appACLR2M2Rows) Close() {
	rows.current = nil
	rows.next = len(rows.rows)
}

func (rows *appACLR2M2Rows) Err() error {
	return rows.err
}

func (rows *appACLR2M2Rows) Next() bool {
	if rows.next >= len(rows.rows) {
		rows.Close()
		return false
	}
	rows.current = rows.rows[rows.next]
	rows.next++
	return true
}

func (rows *appACLR2M2Rows) Scan(dest ...any) error {
	if rows.current == nil {
		return fmt.Errorf("scan scripted M2 row without Next")
	}
	if len(dest) != len(rows.current) {
		return fmt.Errorf("scripted M2 row has %d values, scan has %d destinations", len(rows.current), len(dest))
	}
	for index := range dest {
		target := reflect.ValueOf(dest[index])
		if target.Kind() != reflect.Pointer || target.IsNil() {
			return fmt.Errorf("scripted M2 scan destination %d is not a non-nil pointer", index)
		}
		value := reflect.ValueOf(rows.current[index])
		if !value.IsValid() || !value.Type().AssignableTo(target.Elem().Type()) {
			return fmt.Errorf("scripted M2 value %d of type %T cannot scan into %T", index, rows.current[index], dest[index])
		}
		if bytesValue, ok := rows.current[index].([]byte); ok {
			target.Elem().SetBytes(append([]byte(nil), bytesValue...))
			continue
		}
		target.Elem().Set(value)
	}
	return nil
}

type appACLR2M2DefaultACLTx struct {
	pgx.Tx
	count int64
}

func (tx *appACLR2M2DefaultACLTx) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	if len(args) != 1 {
		return scriptedAppACLR2ReceiptRow{err: fmt.Errorf("M2 default ACL argument count = %d, want 1", len(args))}
	}
	if oid, ok := args[0].(int64); !ok || oid != 21 {
		return scriptedAppACLR2ReceiptRow{err: fmt.Errorf("M2 default ACL argument = %#v, want direct migrator OID 21", args[0])}
	}
	return scriptedAppACLR2ReceiptRow{values: []any{tx.count}}
}
