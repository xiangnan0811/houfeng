package migrate

import (
	"context"
	"crypto/sha256"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestAppACLR2ReceiptPostgresCatalogSnapshotCompilesExactReceipt(t *testing.T) {
	frozen := validFrozenAppACLR1StateFixture(t)
	snapshot, surface := validAppACLR2CatalogSnapshotFixture(t, frozen)
	snapshot.Roles[0].CreateDatabase = true
	snapshot.Roles[0].CreateRole = true
	snapshot.Roles[0].Replication = true
	snapshot.Roles[0].BypassRLS = true

	receipt, err := CompileAppACLR2BootstrapReceiptFromCatalogV1(snapshot, surface, frozen)
	if err != nil {
		t.Fatalf("CompileAppACLR2BootstrapReceiptFromCatalogV1() error = %v", err)
	}
	body, err := CanonicalAppACLR2BootstrapReceiptBodyV1(receipt)
	if err != nil {
		t.Fatalf("CanonicalAppACLR2BootstrapReceiptBodyV1() error = %v", err)
	}
	if _, err := ParseCanonicalAppACLR2BootstrapReceiptBodyV1(body); err != nil {
		t.Fatalf("ParseCanonicalAppACLR2BootstrapReceiptBodyV1() error = %v", err)
	}
	if err := VerifyAppACLR2BootstrapReceiptCatalogV1(receipt, snapshot, surface, frozen); err != nil {
		t.Fatalf("VerifyAppACLR2BootstrapReceiptCatalogV1() error = %v", err)
	}
}

func TestAppACLR2ReceiptPostgresCatalogSnapshotRejectsDrift(t *testing.T) {
	frozen := validFrozenAppACLR1StateFixture(t)
	validSnapshot, validSurface := validAppACLR2CatalogSnapshotFixture(t, frozen)
	tests := []struct {
		name   string
		mutate func(*AppACLR2BootstrapCatalogSnapshotV1, *AppACLR2ReceiptCatalogSnapshotV1, *FrozenAppACLR1StateV1)
		want   string
	}{
		{name: "wrong server major", mutate: func(value *AppACLR2BootstrapCatalogSnapshotV1, _ *AppACLR2ReceiptCatalogSnapshotV1, _ *FrozenAppACLR1StateV1) {
			value.ServerVersionNum = 150014
		}, want: "server_version_num"},
		{name: "unlisted PG16 patch", mutate: func(value *AppACLR2BootstrapCatalogSnapshotV1, _ *AppACLR2ReceiptCatalogSnapshotV1, _ *FrozenAppACLR1StateV1) {
			value.ServerVersionNum = 160001
		}, want: "server_version_num"},
		{name: "wrong bootstrap OID", mutate: func(value *AppACLR2BootstrapCatalogSnapshotV1, _ *AppACLR2ReceiptCatalogSnapshotV1, _ *FrozenAppACLR1StateV1) {
			value.Roles[0].OID = 11
		}, want: "OID 10"},
		{name: "bootstrap not superuser", mutate: func(value *AppACLR2BootstrapCatalogSnapshotV1, _ *AppACLR2ReceiptCatalogSnapshotV1, _ *FrozenAppACLR1StateV1) {
			value.Roles[0].Superuser = false
		}, want: "superuser"},
		{name: "missing domain", mutate: func(value *AppACLR2BootstrapCatalogSnapshotV1, _ *AppACLR2ReceiptCatalogSnapshotV1, _ *FrozenAppACLR1StateV1) {
			value.Domains = nil
		}, want: "domain"},
		{name: "changed domain database", mutate: func(value *AppACLR2BootstrapCatalogSnapshotV1, _ *AppACLR2ReceiptCatalogSnapshotV1, _ *FrozenAppACLR1StateV1) {
			value.Domains[0].DatabaseOID++
		}, want: "domain"},
		{name: "missing member", mutate: func(value *AppACLR2BootstrapCatalogSnapshotV1, _ *AppACLR2ReceiptCatalogSnapshotV1, _ *FrozenAppACLR1StateV1) {
			value.Members = value.Members[:len(value.Members)-1]
		}, want: "36"},
		{name: "equal cardinality substitution", mutate: func(value *AppACLR2BootstrapCatalogSnapshotV1, _ *AppACLR2ReceiptCatalogSnapshotV1, _ *FrozenAppACLR1StateV1) {
			value.Members[0].Name = "substituted"
		}, want: "identity set"},
		{name: "member namespace", mutate: func(value *AppACLR2BootstrapCatalogSnapshotV1, _ *AppACLR2ReceiptCatalogSnapshotV1, _ *FrozenAppACLR1StateV1) {
			value.Members[0].Schema = "public"
		}, want: "member"},
		{name: "member OID order", mutate: func(value *AppACLR2BootstrapCatalogSnapshotV1, _ *AppACLR2ReceiptCatalogSnapshotV1, _ *FrozenAppACLR1StateV1) {
			value.Members[0].OID = value.Members[1].OID
		}, want: "OID"},
		{name: "member identity arguments", mutate: func(value *AppACLR2BootstrapCatalogSnapshotV1, _ *AppACLR2ReceiptCatalogSnapshotV1, _ *FrozenAppACLR1StateV1) {
			value.Members[0].IdentityArguments = "text"
		}, want: "identity set"},
		{name: "member owner", mutate: func(value *AppACLR2BootstrapCatalogSnapshotV1, _ *AppACLR2ReceiptCatalogSnapshotV1, _ *FrozenAppACLR1StateV1) {
			value.Members[0].OwnerOID = 20
		}, want: "owner"},
		{name: "member dependency", mutate: func(value *AppACLR2BootstrapCatalogSnapshotV1, _ *AppACLR2ReceiptCatalogSnapshotV1, _ *FrozenAppACLR1StateV1) {
			value.Members[0].ExtensionDependencyType = "n"
		}, want: "dependency"},
		{name: "member dependency count", mutate: func(value *AppACLR2BootstrapCatalogSnapshotV1, _ *AppACLR2ReceiptCatalogSnapshotV1, _ *FrozenAppACLR1StateV1) {
			value.Members[0].ExtensionDependencyCount = 2
		}, want: "dependency"},
		{name: "member ACL", mutate: func(value *AppACLR2BootstrapCatalogSnapshotV1, _ *AppACLR2ReceiptCatalogSnapshotV1, _ *FrozenAppACLR1StateV1) {
			value.Members[0].ACLIsDefault = false
		}, want: "ACL"},
		{name: "extension version", mutate: func(value *AppACLR2BootstrapCatalogSnapshotV1, _ *AppACLR2ReceiptCatalogSnapshotV1, _ *FrozenAppACLR1StateV1) {
			value.Extension.Version = "1.2"
		}, want: "extension"},
		{name: "extension schema", mutate: func(value *AppACLR2BootstrapCatalogSnapshotV1, _ *AppACLR2ReceiptCatalogSnapshotV1, _ *FrozenAppACLR1StateV1) {
			value.Extension.Schema = "public"
		}, want: "extension"},
		{name: "extension owner", mutate: func(value *AppACLR2BootstrapCatalogSnapshotV1, _ *AppACLR2ReceiptCatalogSnapshotV1, _ *FrozenAppACLR1StateV1) {
			value.Extension.OwnerOID = 99
		}, want: "extension owner"},
		{name: "role attributes", mutate: func(value *AppACLR2BootstrapCatalogSnapshotV1, _ *AppACLR2ReceiptCatalogSnapshotV1, _ *FrozenAppACLR1StateV1) {
			value.Roles[2].Inherit = true
		}, want: "constrained"},
		{name: "role membership", mutate: func(value *AppACLR2BootstrapCatalogSnapshotV1, _ *AppACLR2ReceiptCatalogSnapshotV1, _ *FrozenAppACLR1StateV1) {
			value.Roles[2].RecursiveMembershipCount = 1
		}, want: "membership"},
		{name: "bootstrap default ACL", mutate: func(value *AppACLR2BootstrapCatalogSnapshotV1, _ *AppACLR2ReceiptCatalogSnapshotV1, _ *FrozenAppACLR1StateV1) {
			value.BootstrapDefaultACLCount = 1
		}, want: "default ACL"},
		{name: "application source", mutate: func(_ *AppACLR2BootstrapCatalogSnapshotV1, _ *AppACLR2ReceiptCatalogSnapshotV1, state *FrozenAppACLR1StateV1) {
			state.SourceSetDigest[0] ^= 0xff
		}, want: "frozen R1"},
		{name: "helper identity", mutate: func(_ *AppACLR2BootstrapCatalogSnapshotV1, value *AppACLR2ReceiptCatalogSnapshotV1, _ *FrozenAppACLR1StateV1) {
			value.Helpers[0].Identity += " "
		}, want: "helper"},
		{name: "helper structure", mutate: func(_ *AppACLR2BootstrapCatalogSnapshotV1, value *AppACLR2ReceiptCatalogSnapshotV1, _ *FrozenAppACLR1StateV1) {
			value.Helpers[0].Kind = "p"
		}, want: "helper"},
		{name: "helper definition", mutate: func(_ *AppACLR2BootstrapCatalogSnapshotV1, value *AppACLR2ReceiptCatalogSnapshotV1, _ *FrozenAppACLR1StateV1) {
			value.Helpers[1].Source = "begin return null; end;"
		}, want: "definition"},
		{name: "helper definition suffix", mutate: func(_ *AppACLR2BootstrapCatalogSnapshotV1, value *AppACLR2ReceiptCatalogSnapshotV1, _ *FrozenAppACLR1StateV1) {
			value.Helpers[0].Definition += " SECURITY DEFINER"
		}, want: "definition"},
		{name: "helper security definer catalog", mutate: func(_ *AppACLR2BootstrapCatalogSnapshotV1, value *AppACLR2ReceiptCatalogSnapshotV1, _ *FrozenAppACLR1StateV1) {
			value.Helpers[0].SecurityDefiner = true
		}, want: "security definer"},
		{name: "receipt table persistence", mutate: func(_ *AppACLR2BootstrapCatalogSnapshotV1, value *AppACLR2ReceiptCatalogSnapshotV1, _ *FrozenAppACLR1StateV1) {
			value.Table.Persistence = "u"
		}, want: "table"},
		{name: "receipt table singleton check", mutate: func(_ *AppACLR2BootstrapCatalogSnapshotV1, value *AppACLR2ReceiptCatalogSnapshotV1, _ *FrozenAppACLR1StateV1) {
			value.Table.Constraints[3].Definition = "CHECK (true)"
		}, want: "constraint"},
		{name: "receipt table digest check", mutate: func(_ *AppACLR2BootstrapCatalogSnapshotV1, value *AppACLR2ReceiptCatalogSnapshotV1, _ *FrozenAppACLR1StateV1) {
			value.Table.Constraints[1].Definition = "CHECK (true)"
		}, want: "constraint"},
		{name: "receipt table digest helper check", mutate: func(_ *AppACLR2BootstrapCatalogSnapshotV1, value *AppACLR2ReceiptCatalogSnapshotV1, _ *FrozenAppACLR1StateV1) {
			value.Table.Constraints[2].Definition = "CHECK (true)"
		}, want: "constraint"},
		{name: "receipt table primary index binding", mutate: func(_ *AppACLR2BootstrapCatalogSnapshotV1, value *AppACLR2ReceiptCatalogSnapshotV1, _ *FrozenAppACLR1StateV1) {
			value.Table.Constraints[4].IndexOID++
		}, want: "primary key index"},
		{name: "receipt table primary index validity", mutate: func(_ *AppACLR2BootstrapCatalogSnapshotV1, value *AppACLR2ReceiptCatalogSnapshotV1, _ *FrozenAppACLR1StateV1) {
			value.Table.Constraints[4].IndexValid = false
		}, want: "primary key index"},
		{name: "receipt table unvalidated check", mutate: func(_ *AppACLR2BootstrapCatalogSnapshotV1, value *AppACLR2ReceiptCatalogSnapshotV1, _ *FrozenAppACLR1StateV1) {
			value.Table.Constraints[3].Validated = false
		}, want: "not validated"},
		{name: "unknown reserved object", mutate: func(_ *AppACLR2BootstrapCatalogSnapshotV1, value *AppACLR2ReceiptCatalogSnapshotV1, _ *FrozenAppACLR1StateV1) {
			value.ReservedObjects = append(value.ReservedObjects, AppACLR2ReservedCatalogObjectV1{Kind: "relation", Schema: "third_party", Identity: "app_acl_r2_extra", Detail: "r"})
		}, want: "reserved"},
		{name: "unknown reserved function", mutate: func(_ *AppACLR2BootstrapCatalogSnapshotV1, value *AppACLR2ReceiptCatalogSnapshotV1, _ *FrozenAppACLR1StateV1) {
			value.ReservedObjects = append(value.ReservedObjects, AppACLR2ReservedCatalogObjectV1{Kind: "function", Schema: "third_party", Identity: "third_party.app_acl_r2_extra()", Detail: "f"})
		}, want: "reserved"},
		{name: "unknown reserved trigger", mutate: func(_ *AppACLR2BootstrapCatalogSnapshotV1, value *AppACLR2ReceiptCatalogSnapshotV1, _ *FrozenAppACLR1StateV1) {
			value.ReservedObjects = append(value.ReservedObjects, AppACLR2ReservedCatalogObjectV1{Kind: "trigger", Schema: "third_party", Identity: "other_table.app_acl_r2_extra", Detail: "user"})
		}, want: "reserved"},
		{name: "additional receipt trigger", mutate: func(_ *AppACLR2BootstrapCatalogSnapshotV1, value *AppACLR2ReceiptCatalogSnapshotV1, _ *FrozenAppACLR1StateV1) {
			value.ACL.Triggers = append(value.ACL.Triggers, value.ACL.Triggers[0])
		}, want: "L2 ACL"},
		{name: "direct receipt ACL", mutate: func(_ *AppACLR2BootstrapCatalogSnapshotV1, value *AppACLR2ReceiptCatalogSnapshotV1, _ *FrozenAppACLR1StateV1) {
			value.ACL.Objects[0].ExplicitGrants = value.ACL.Objects[0].ExplicitGrants[1:]
		}, want: "L2 ACL"},
		{name: "effective receipt ACL", mutate: func(_ *AppACLR2BootstrapCatalogSnapshotV1, value *AppACLR2ReceiptCatalogSnapshotV1, _ *FrozenAppACLR1StateV1) {
			value.ACL.Objects[0].EffectiveRelevantPrivilegeMask |= 0x08
		}, want: "L2 ACL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := cloneAppACLR2BootstrapCatalogSnapshot(validSnapshot)
			surface := cloneAppACLR2ReceiptCatalogSnapshot(validSurface)
			state := cloneFrozenAppACLR1State(frozen)
			tt.mutate(&snapshot, &surface, &state)
			if _, err := CompileAppACLR2BootstrapReceiptFromCatalogV1(snapshot, surface, state); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("CompileAppACLR2BootstrapReceiptFromCatalogV1() error = %v, want %q rejection", err, tt.want)
			}
		})
	}
}

func TestAppACLR2PostBootstrapCatalogContinuityRejectsPersistedAndFreshIdentityDrift(t *testing.T) {
	frozen := validFrozenAppACLR1StateFixture(t)
	bootstrap, surface := validAppACLR2CatalogSnapshotFixture(t, frozen)
	receipt, err := CompileAppACLR2BootstrapReceiptFromCatalogV1(bootstrap, surface, frozen)
	if err != nil {
		t.Fatalf("CompileAppACLR2BootstrapReceiptFromCatalogV1() error = %v", err)
	}

	continuity := appACLR2PostBootstrapCatalogSnapshotFixture(bootstrap)
	if err := VerifyAppACLR2PostBootstrapReceiptCatalogV1(receipt, continuity, surface, frozen); err != nil {
		t.Fatalf("VerifyAppACLR2PostBootstrapReceiptCatalogV1() error = %v", err)
	}
	if _, exists := reflect.TypeOf(continuity).FieldByName("PostgresSystemIdentifier"); exists {
		t.Fatal("post-bootstrap continuity snapshot carries a fresh or caller-supplied physical system identifier")
	}

	for _, tt := range []struct {
		name   string
		mutate func(*AppACLR2PostBootstrapCatalogSnapshotV1)
	}{
		{name: "receipt domain disagreement", mutate: func(snapshot *AppACLR2PostBootstrapCatalogSnapshotV1) {
			snapshot.Domains[0].PostgresSystemIdentifier = "72623859790382857"
		}},
		{name: "fresh database OID drift", mutate: func(snapshot *AppACLR2PostBootstrapCatalogSnapshotV1) {
			snapshot.DatabaseOID++
		}},
		{name: "fresh database name drift", mutate: func(snapshot *AppACLR2PostBootstrapCatalogSnapshotV1) {
			snapshot.DatabaseName = "other_database"
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := appACLR2PostBootstrapCatalogSnapshotFixture(bootstrap)
			tt.mutate(&snapshot)
			if err := VerifyAppACLR2PostBootstrapReceiptCatalogV1(receipt, snapshot, surface, frozen); err == nil {
				t.Fatal("VerifyAppACLR2PostBootstrapReceiptCatalogV1() error = nil, want continuity rejection")
			}
		})
	}
}

func TestAppACLR2PostBootstrapCatalogReaderDoesNotReadPhysicalSystemIdentifier(t *testing.T) {
	frozen := validFrozenAppACLR1StateFixture(t)
	tx := newScriptedAppACLR2BootstrapCatalogTx()
	tx.queryRows[0].values = []any{int64(160012), "16.12", int64(424242), "houfeng_app"}
	tx.queryRows = tx.queryRows[:3]

	snapshot, err := ReadAppACLR2PostBootstrapCatalogSnapshotInTx(context.Background(), tx, frozen)
	if err != nil {
		t.Fatalf("ReadAppACLR2PostBootstrapCatalogSnapshotInTx() error = %v", err)
	}
	if snapshot.DatabaseOID != 424242 || snapshot.DatabaseName != "houfeng_app" || len(snapshot.Domains) != 1 {
		t.Fatalf("post-bootstrap continuity snapshot = %#v, want fresh database OID/name and one persisted domain", snapshot)
	}
	if _, exists := reflect.TypeOf(snapshot).FieldByName("PostgresSystemIdentifier"); exists {
		t.Fatal("post-bootstrap catalog reader exposes a physical system identifier field")
	}
	for _, query := range append(tx.queryTexts, tx.queryRowTexts...) {
		if strings.Contains(strings.ToLower(query), "pg_control_system") {
			t.Fatalf("post-bootstrap catalog reader queried bootstrap-only pg_control_system(): %s", query)
		}
	}
	assertScriptedAppACLR2ReceiptQueriesAreIdentityNeutral(t, tx)
}

func TestAppACLR2SharedL2ContinuityVerifierDoesNotReadPhysicalSystemIdentifier(t *testing.T) {
	frozen := validFrozenAppACLR1StateFixture(t)
	catalogSeed := newScriptedAppACLR2BootstrapCatalogTx()
	catalogSeed.queryRows = catalogSeed.queryRows[:3]
	continuity, err := ReadAppACLR2PostBootstrapCatalogSnapshotInTx(context.Background(), catalogSeed, frozen)
	if err != nil {
		t.Fatalf("ReadAppACLR2PostBootstrapCatalogSnapshotInTx(seed) error = %v", err)
	}
	surfaceSeed := newScriptedAppACLR2ReceiptCatalogTx()
	surface, err := ReadAppACLR2ReceiptCatalogSnapshotInTx(context.Background(), surfaceSeed, frozen)
	if err != nil {
		t.Fatalf("ReadAppACLR2ReceiptCatalogSnapshotInTx(seed) error = %v", err)
	}
	receipt, err := compileAppACLR2PostBootstrapReceiptFromCatalogV1(continuity, surface, frozen)
	if err != nil {
		t.Fatalf("compileAppACLR2PostBootstrapReceiptFromCatalogV1(seed) error = %v", err)
	}
	body, err := CanonicalAppACLR2BootstrapReceiptBodyV1(receipt)
	if err != nil {
		t.Fatalf("CanonicalAppACLR2BootstrapReceiptBodyV1() error = %v", err)
	}
	postBootstrap := newScriptedAppACLR2BootstrapCatalogTx()
	receiptSurface := newScriptedAppACLR2ReceiptCatalogTx()
	tx := &scriptedAppACLR2ReceiptTx{
		queryRows: append(
			append([]scriptedAppACLR2ReceiptQueryRow(nil), postBootstrap.queryRows[:3]...),
			receiptSurface.queryRows...,
		),
		queries: append(
			append([]scriptedAppACLR2ReceiptQuery(nil), postBootstrap.queries...),
			receiptSurface.queries...,
		),
	}
	row := appACLR2ReceiptRowV1{Singleton: true, Body: body, Digest: sha256.Sum256(body)}
	if err := verifyAppACLR2L2EvidenceInTx(context.Background(), tx, frozen, row); err != nil {
		t.Fatalf("verifyAppACLR2L2EvidenceInTx() error = %v", err)
	}
	for _, query := range append(tx.queryTexts, tx.queryRowTexts...) {
		if strings.Contains(strings.ToLower(query), "pg_control_system") {
			t.Fatalf("shared L2 continuity verifier queried bootstrap-only pg_control_system(): %s", query)
		}
	}
	if len(tx.queries) != 0 || len(tx.queryRows) != 0 {
		t.Fatalf("shared L2 continuity verifier left %d query and %d query-row scripts unused", len(tx.queries), len(tx.queryRows))
	}
	assertScriptedAppACLR2ReceiptQueriesAreIdentityNeutral(t, tx)
}

func appACLR2PostBootstrapCatalogSnapshotFixture(
	bootstrap AppACLR2BootstrapCatalogSnapshotV1,
) AppACLR2PostBootstrapCatalogSnapshotV1 {
	return AppACLR2PostBootstrapCatalogSnapshotV1{
		ServerVersionNum:         bootstrap.ServerVersionNum,
		ServerVersion:            bootstrap.ServerVersion,
		DatabaseOID:              bootstrap.DatabaseOID,
		DatabaseName:             bootstrap.DatabaseName,
		BootstrapDefaultACLCount: bootstrap.BootstrapDefaultACLCount,
		Domains:                  append([]AppACLDomainR2V1(nil), bootstrap.Domains...),
		Roles:                    append([]AppACLR2CatalogRoleStateV1(nil), bootstrap.Roles...),
		Extension:                bootstrap.Extension,
		Members:                  append([]AppACLR2PGCryptoMemberCatalogV1(nil), bootstrap.Members...),
	}
}

func TestAppACLR2ReceiptPostgresCatalogReaderUsesCallerTransaction(t *testing.T) {
	tx := &fakeAppACLR2CatalogTx{}
	dependencies := appACLR2CatalogReadDependencies{
		readSnapshot: func(_ context.Context, gotTx pgx.Tx, state FrozenAppACLR1StateV1) (AppACLR2BootstrapCatalogSnapshotV1, error) {
			if gotTx != tx {
				t.Fatalf("snapshot transaction = %T %p, want caller transaction %p", gotTx, gotTx, tx)
			}
			if state.CenterRuntimeRole != "center_runtime" {
				t.Fatalf("frozen state center runtime = %q", state.CenterRuntimeRole)
			}
			return AppACLR2BootstrapCatalogSnapshotV1{}, nil
		},
	}
	state := FrozenAppACLR1StateV1{CenterRuntimeRole: "center_runtime"}
	if _, err := readAppACLR2BootstrapCatalogSnapshotInTxWithDependencies(context.Background(), tx, state, dependencies); err != nil {
		t.Fatalf("readAppACLR2BootstrapCatalogSnapshotInTxWithDependencies() error = %v", err)
	}
}

func TestAppACLR2TransitionRoleReaderCountsBootstrapMembership(t *testing.T) {
	const bootstrapName = "bootstrap_oid10"
	state := FrozenAppACLR1StateV1{
		DirectMigratorRole: "direct_migrator",
		CenterRuntimeRole:  "center_runtime",
		PlatformAdminRole:  "platform_admin",
	}
	tx := &scriptedAppACLR2ReceiptTx{queries: []scriptedAppACLR2ReceiptQuery{
		{rows: [][]any{
			{bootstrapName, int64(10), true, true, true, true, true, true, true},
			{"center_runtime", int64(20), true, false, false, false, false, false, false},
			{"direct_migrator", int64(21), true, false, false, false, false, false, false},
			{"platform_admin", int64(22), true, false, false, false, false, false, false},
		}},
		{
			checkArgs: func(args []any) error {
				if len(args) != 1 {
					return fmt.Errorf("membership query arguments = %d, want 1", len(args))
				}
				names, ok := args[0].([]string)
				if !ok {
					return fmt.Errorf("membership names have type %T, want []string", args[0])
				}
				for _, name := range names {
					if name == bootstrapName {
						return nil
					}
				}
				return fmt.Errorf("membership names omit bootstrap OID-10 role %q", bootstrapName)
			},
			rows: [][]any{{bootstrapName, "parent_role"}},
		},
	}}

	roles, err := readAppACLR2TransitionRolesInTx(context.Background(), tx, state)
	if err != nil {
		t.Fatalf("readAppACLR2TransitionRolesInTx() error = %v", err)
	}
	if len(roles) != 4 {
		t.Fatalf("readAppACLR2TransitionRolesInTx() role count = %d, want 4", len(roles))
	}
	if roles[0].Name != bootstrapName || roles[0].RecursiveMembershipCount != 1 {
		t.Fatalf("bootstrap role = %#v, want OID-10 bootstrap with one membership", roles[0])
	}
}

func TestAppACLR2ReceiptCatalogRejectsSecurityDefinerHelper(t *testing.T) {
	_, surface := validAppACLR2CatalogSnapshotFixture(t, validFrozenAppACLR1StateFixture(t))
	surface.Helpers[0].SecurityDefiner = true
	if err := validateAppACLR2ReceiptSurfaceCatalog(surface); err == nil || !strings.Contains(err.Error(), "security definer") {
		t.Fatalf("validateAppACLR2ReceiptSurfaceCatalog() error = %v, want security definer rejection", err)
	}
}

func TestAppACLR2ReceiptCatalogReaderMapsExactSurfaceWithoutCallerIdentity(t *testing.T) {
	state := FrozenAppACLR1StateV1{
		DirectMigratorRole: "direct_migrator",
		CenterRuntimeRole:  "center_runtime",
		PlatformAdminRole:  "platform_admin",
	}
	tx := newScriptedAppACLR2ReceiptCatalogTx()
	surface, err := ReadAppACLR2ReceiptCatalogSnapshotInTx(context.Background(), tx, state)
	if err != nil {
		t.Fatalf("ReadAppACLR2ReceiptCatalogSnapshotInTx() error = %v", err)
	}
	if err := validateAppACLR2ReceiptSurfaceCatalog(surface); err != nil {
		t.Fatalf("validateAppACLR2ReceiptSurfaceCatalog() error = %v", err)
	}
	if !reflect.DeepEqual(surface.Table, appACLR2ReceiptTableCatalogFixture()) {
		t.Fatalf("receipt table catalog = %#v, want test-owned literal fixture %#v", surface.Table, appACLR2ReceiptTableCatalogFixture())
	}
	if len(tx.queries) != 0 || len(tx.queryRows) != 0 {
		t.Fatalf("receipt catalog reader left %d query and %d query-row scripts unused", len(tx.queries), len(tx.queryRows))
	}
	assertScriptedAppACLR2ReceiptQueriesAreIdentityNeutral(t, tx)
}

func TestAppACLR2ReceiptCatalogReaderRejectsColumnACLAndInheritanceDrift(t *testing.T) {
	state := FrozenAppACLR1StateV1{
		DirectMigratorRole: "direct_migrator",
		CenterRuntimeRole:  "center_runtime",
		PlatformAdminRole:  "platform_admin",
	}
	tests := []struct {
		name                 string
		columnACLRows        [][]any
		inheritanceRows      [][]any
		want                 string
		wantColumnACLQuery   bool
		wantInheritanceQuery bool
		wantQueryFragments   []string
	}{
		{
			name:               "platform admin column SELECT",
			columnACLRows:      [][]any{{"receipt_body", "platform_admin", "SELECT", false}},
			want:               "column ACL",
			wantColumnACLQuery: true,
			wantQueryFragments: []string{
				"pg_catalog.aclexplode(attribute.attacl)",
				"attribute.attacl is not null",
				"pg_catalog.cardinality(attribute.attacl) > 0",
			},
		},
		{
			name:                 "receipt inherits parent",
			inheritanceRows:      [][]any{{true, false}},
			want:                 "inherits from a parent",
			wantInheritanceQuery: true,
			wantQueryFragments: []string{
				"from pg_catalog.pg_inherits inheritance",
				"inheritance.inhrelid = $1::pg_catalog.oid",
				"inheritance.inhparent = $1::pg_catalog.oid",
			},
		},
		{
			name:                 "receipt has inheritance child",
			inheritanceRows:      [][]any{{false, true}},
			want:                 "has an inheritance child",
			wantInheritanceQuery: true,
			wantQueryFragments: []string{
				"from pg_catalog.pg_inherits inheritance",
				"inheritance.inhrelid = $1::pg_catalog.oid",
				"inheritance.inhparent = $1::pg_catalog.oid",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newScriptedAppACLR2ReceiptCatalogTx()
			tx.columnACLRows = tt.columnACLRows
			tx.inheritanceRows = tt.inheritanceRows

			surface, err := ReadAppACLR2ReceiptCatalogSnapshotInTx(context.Background(), tx, state)
			if err != nil {
				t.Fatalf("ReadAppACLR2ReceiptCatalogSnapshotInTx() error = %v", err)
			}
			if err := validateAppACLR2ReceiptSurfaceCatalog(surface); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validateAppACLR2ReceiptSurfaceCatalog() error = %v, want %q rejection", err, tt.want)
			}
			if tt.wantColumnACLQuery && tx.columnACLQueryCount != 1 {
				t.Fatalf("receipt column ACL query count = %d, want 1", tx.columnACLQueryCount)
			}
			if tt.wantInheritanceQuery && tx.inheritanceQueryCount != 1 {
				t.Fatalf("receipt inheritance query count = %d, want 1", tx.inheritanceQueryCount)
			}
			queries := strings.ToLower(strings.Join(tx.queryTexts, "\n"))
			for _, want := range tt.wantQueryFragments {
				if !strings.Contains(queries, want) {
					t.Fatalf("receipt catalog queries = %q, want fragment %q", queries, want)
				}
			}
			if len(tx.queries) != 0 || len(tx.queryRows) != 0 {
				t.Fatalf("receipt catalog reader left %d query and %d query-row scripts unused", len(tx.queries), len(tx.queryRows))
			}
			assertScriptedAppACLR2ReceiptQueriesAreIdentityNeutral(t, tx)
		})
	}
}

func TestAppACLR2BootstrapCatalogReaderMapsIdentityNeutralSnapshot(t *testing.T) {
	state := FrozenAppACLR1StateV1{
		DirectMigratorRole: "direct_migrator",
		CenterRuntimeRole:  "center_runtime",
		PlatformAdminRole:  "platform_admin",
	}
	tx := newScriptedAppACLR2BootstrapCatalogTx()
	snapshot, err := readAppACLR2BootstrapCatalogSnapshotPostgres(context.Background(), tx, state)
	if err != nil {
		t.Fatalf("readAppACLR2BootstrapCatalogSnapshotPostgres() error = %v", err)
	}
	if snapshot.ServerVersionNum != 160012 || snapshot.DatabaseName != "houfeng_app" || len(snapshot.Roles) != 4 || len(snapshot.Members) != 36 {
		t.Fatalf("bootstrap catalog snapshot = %#v, want test-owned literal server, database, roles, and 36-member inventory", snapshot)
	}
	if len(tx.queries) != 0 || len(tx.queryRows) != 0 {
		t.Fatalf("bootstrap catalog reader left %d query and %d query-row scripts unused", len(tx.queries), len(tx.queryRows))
	}
	assertScriptedAppACLR2ReceiptQueriesAreIdentityNeutral(t, tx)
}

func TestAppACLR2BootstrapLiveCatalogReaderCallsPGControlAndRejectsDomainMismatch(t *testing.T) {
	frozen := validFrozenAppACLR1StateFixture(t)
	for _, tt := range []struct {
		name       string
		liveSystem string
		wantErr    bool
	}{
		{name: "exact live binding", liveSystem: "72623859790382856"},
		{name: "live domain mismatch", liveSystem: "72623859790382857", wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tx := newScriptedAppACLR2BootstrapCatalogTx()
			tx.queryRows[3].values = []any{tt.liveSystem}
			snapshot, err := ReadAppACLR2BootstrapCatalogSnapshotInTx(context.Background(), tx, frozen)
			if err != nil {
				t.Fatalf("ReadAppACLR2BootstrapCatalogSnapshotInTx() error = %v", err)
			}
			_, _, err = validateAppACLR2BootstrapCatalog(snapshot, frozen)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateAppACLR2BootstrapCatalog() error = %v, want error=%t", err, tt.wantErr)
			}
			queries := strings.ToLower(strings.Join(tx.queryRowTexts, "\n"))
			if !strings.Contains(queries, "pg_control_system()") {
				t.Fatalf("bootstrap live catalog reader queries = %q, want direct pg_control_system() call", queries)
			}
			if len(tx.queries) != 0 || len(tx.queryRows) != 0 {
				t.Fatalf("bootstrap live catalog reader left %d query and %d query-row scripts unused", len(tx.queries), len(tx.queryRows))
			}
		})
	}
}

func TestAppACLR2BootstrapCatalogReaderScansAndValidatesExactPGCryptoMembers(t *testing.T) {
	frozen := validFrozenAppACLR1StateFixture(t)
	tx := newScriptedAppACLR2BootstrapCatalogTx()
	snapshot, err := readAppACLR2BootstrapCatalogSnapshotPostgres(context.Background(), tx, frozen)
	if err != nil {
		t.Fatalf("readAppACLR2BootstrapCatalogSnapshotPostgres() error = %v", err)
	}
	if _, _, err := validateAppACLR2BootstrapCatalog(snapshot, frozen); err != nil {
		t.Fatalf("validateAppACLR2BootstrapCatalog() error = %v", err)
	}

	member16 := snapshot.Members[16]
	member16Identity := member16.Schema + "." + member16.Name + "|" + member16.IdentityArguments
	if member16.OID != 7017 || member16.Schema != "record_platform_internal" || member16.Name != "pgp_armor_headers" ||
		member16Identity != appACLR2PGCryptoMember16PG16IdentityLiteral {
		t.Fatalf("pgcrypto member 16 = %#v, want PostgreSQL 16 full-OUT pgp_armor_headers identity", member16)
	}
	for _, tt := range []struct {
		name              string
		identityArguments string
	}{
		{name: "input only", identityArguments: "text"},
		{name: "OUT name substitution", identityArguments: "text, OUT header text, OUT value text"},
		{name: "OUT mode substitution", identityArguments: "text, INOUT key text, OUT value text"},
		{name: "OUT type substitution", identityArguments: "text, OUT key bytea, OUT value text"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mutated := snapshot
			mutated.Members = append([]AppACLR2PGCryptoMemberCatalogV1(nil), snapshot.Members...)
			mutated.Members[16].IdentityArguments = tt.identityArguments
			if _, _, err := validateAppACLR2BootstrapCatalog(mutated, frozen); err == nil || !strings.Contains(err.Error(), "identity set") {
				t.Fatalf("validateAppACLR2BootstrapCatalog() error = %v, want exact full-OUT identity rejection", err)
			}
		})
	}

	first := snapshot.Members[0]
	if first.OID != 7001 || first.Schema != "record_platform_internal" || first.Name != "armor" || first.IdentityArguments != "bytea" ||
		first.OwnerName != "bootstrap_oid10" || first.OwnerOID != 10 || first.ExtensionOID != 500 || first.ExtensionDependencyType != "e" ||
		first.ExtensionDependencyCount != 1 || first.ExtensionDependencyClass != "pg_catalog.pg_proc" || first.ExtensionDependencyObjectSubID != 0 ||
		first.ExtensionDependencyReferenceObjectSubID != 0 || first.RoutineKind != "f" || !first.ACLIsDefault {
		t.Fatalf("first pgcrypto member = %#v, want every literal OID/identity/owner/dependency/ACL fact", first)
	}
}

func TestAppACLR2ReceiptPostgresCatalogReadersUseValidCoalesceSyntax(t *testing.T) {
	frozen := validFrozenAppACLR1StateFixture(t)
	bootstrapTx := newScriptedAppACLR2BootstrapCatalogTx()
	if _, err := readAppACLR2BootstrapCatalogSnapshotPostgres(context.Background(), bootstrapTx, frozen); err != nil {
		t.Fatalf("readAppACLR2BootstrapCatalogSnapshotPostgres() error = %v", err)
	}
	receiptTx := newScriptedAppACLR2ReceiptCatalogTx()
	if _, err := ReadAppACLR2ReceiptCatalogSnapshotInTx(context.Background(), receiptTx, frozen); err != nil {
		t.Fatalf("ReadAppACLR2ReceiptCatalogSnapshotInTx() error = %v", err)
	}
	for _, tx := range []*scriptedAppACLR2ReceiptTx{bootstrapTx, receiptTx} {
		queries := append([]string(nil), tx.queryTexts...)
		queries = append(queries, tx.queryRowTexts...)
		for _, query := range queries {
			if strings.Contains(strings.ToLower(query), "pg_catalog.coalesce") {
				t.Fatalf("APP ACL R2 catalog query schema-qualifies SQL COALESCE syntax: %s", query)
			}
		}
	}
}

func TestAppACLR2ReceiptTableConstraintCatalogQueryUsesNonKeywordAlias(t *testing.T) {
	tx := &scriptedAppACLR2ReceiptTx{
		queryRows: []scriptedAppACLR2ReceiptQueryRow{{values: appACLR2ReceiptTableRelationRow()}},
		queries: []scriptedAppACLR2ReceiptQuery{
			{rows: appACLR2ReceiptTableColumnRows()},
			{rows: appACLR2ReceiptTableConstraintRows()},
		},
	}
	if _, err := readAppACLR2ReceiptTableCatalogInTx(context.Background(), tx); err != nil {
		t.Fatalf("readAppACLR2ReceiptTableCatalogInTx() error = %v", err)
	}

	const selector = "from pg_catalog.pg_constraint"
	constraintQuery := ""
	for _, query := range tx.queryTexts {
		normalized := strings.Join(strings.Fields(strings.ToLower(query)), " ")
		if strings.Contains(normalized, selector) {
			constraintQuery = normalized
			break
		}
	}
	if constraintQuery == "" {
		t.Fatalf("receipt table catalog queries contain no %q selector: %#v", selector, tx.queryTexts)
	}

	const alias = "constraint_catalog"
	for _, want := range []string{
		selector + " " + alias + " left join pg_catalog.pg_index index_catalog",
		"select " + alias + ".contype::text",
		"pg_catalog.pg_get_constraintdef(" + alias + ".oid, true)",
		alias + ".convalidated",
		alias + ".conindid::bigint",
		"index_catalog.indexrelid = " + alias + ".conindid",
		"index_catalog.indrelid = " + alias + ".conrelid",
		"where " + alias + ".conrelid = $1::pg_catalog.oid",
		"order by " + alias + ".contype",
	} {
		if !strings.Contains(constraintQuery, want) {
			t.Fatalf("receipt constraint catalog query = %q, want parseable alias fragment %q", constraintQuery, want)
		}
	}
	if got := strings.Count(constraintQuery, alias+"."); got != 9 {
		t.Fatalf("receipt constraint catalog query alias references = %d, want all 9 references qualified by %q: %q", got, alias, constraintQuery)
	}
	if strings.Contains(constraintQuery, "constraint.") {
		t.Fatalf("receipt constraint catalog query uses reserved PostgreSQL alias constraint: %q", constraintQuery)
	}
}

func TestAppACLR2BootstrapCatalogReaderRejectsPGCryptoRoutineKindDrift(t *testing.T) {
	frozen := validFrozenAppACLR1StateFixture(t)
	memberRows := appACLR2PGCryptoMemberCatalogRows()
	memberRows[0][10] = "p"
	tx := newScriptedAppACLR2BootstrapCatalogTxWithPGCryptoRows(memberRows)
	snapshot, err := readAppACLR2BootstrapCatalogSnapshotPostgres(context.Background(), tx, frozen)
	if err != nil {
		t.Fatalf("readAppACLR2BootstrapCatalogSnapshotPostgres() error = %v", err)
	}
	if _, _, err := validateAppACLR2BootstrapCatalog(snapshot, frozen); err == nil || !strings.Contains(err.Error(), "routine kind") {
		t.Fatalf("validateAppACLR2BootstrapCatalog() error = %v, want routine-kind rejection", err)
	}
}

func TestAppACLR2BootstrapCatalogReaderRetainsExtraNonProcedurePGCryptoMemberForRejection(t *testing.T) {
	frozen := validFrozenAppACLR1StateFixture(t)
	memberRows := appACLR2PGCryptoMemberCatalogRows()
	memberRows = append(memberRows, []any{
		"pg_catalog.pg_class", int64(8001), int64(0), int64(0), "e", int64(500), int64(1),
		"", "", "", "", "", int64(0), true,
	})
	tx := newScriptedAppACLR2BootstrapCatalogTxWithPGCryptoRows(memberRows)
	snapshot, err := readAppACLR2BootstrapCatalogSnapshotPostgres(context.Background(), tx, frozen)
	if err != nil {
		t.Fatalf("readAppACLR2BootstrapCatalogSnapshotPostgres() error = %v", err)
	}
	if len(snapshot.Members) != 37 {
		t.Fatalf("scanned pgcrypto member count = %d, want extra non-procedure dependency retained", len(snapshot.Members))
	}
	extra := snapshot.Members[36]
	if extra.ExtensionDependencyClass != "pg_catalog.pg_class" || extra.OID != 8001 || extra.ExtensionOID != 500 || extra.ExtensionDependencyType != "e" {
		t.Fatalf("extra non-procedure pgcrypto member = %#v, want literal dependency/class facts retained", extra)
	}
	if _, _, err := validateAppACLR2BootstrapCatalog(snapshot, frozen); err == nil || !strings.Contains(err.Error(), "36") {
		t.Fatalf("validateAppACLR2BootstrapCatalog() error = %v, want exact-inventory rejection", err)
	}
}

func TestAppACLR2ReceiptTableCatalogReaderMapsConstraintIndexMetadata(t *testing.T) {
	tests := []struct {
		name   string
		mutate func([][]any)
		want   string
	}{
		{name: "exact catalog"},
		{name: "detached primary index", mutate: func(rows [][]any) { rows[4][3] = int64(2004) }, want: "primary key index"},
		{name: "nonprimary primary index", mutate: func(rows [][]any) { rows[4][4] = false }, want: "primary key index"},
		{name: "nonunique primary index", mutate: func(rows [][]any) { rows[4][5] = false }, want: "primary key index"},
		{name: "invalid primary index", mutate: func(rows [][]any) { rows[4][6] = false }, want: "primary key index"},
		{name: "unvalidated singleton check", mutate: func(rows [][]any) { rows[3][2] = false }, want: "not validated"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			constraintRows := appACLR2ReceiptTableConstraintRows()
			if tt.mutate != nil {
				tt.mutate(constraintRows)
			}
			tx := &scriptedAppACLR2ReceiptTx{
				queryRows: []scriptedAppACLR2ReceiptQueryRow{{values: appACLR2ReceiptTableRelationRow()}},
				queries: []scriptedAppACLR2ReceiptQuery{
					{rows: appACLR2ReceiptTableColumnRows()},
					{rows: constraintRows},
				},
			}
			table, err := readAppACLR2ReceiptTableCatalogInTx(context.Background(), tx)
			if err != nil {
				t.Fatalf("readAppACLR2ReceiptTableCatalogInTx() error = %v", err)
			}
			if tt.want == "" && !reflect.DeepEqual(table, appACLR2ReceiptTableCatalogFixture()) {
				t.Fatalf("receipt table catalog = %#v, want test-owned literal fixture %#v", table, appACLR2ReceiptTableCatalogFixture())
			}
			err = validateAppACLR2ReceiptTableCatalog(table, 1004)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("validateAppACLR2ReceiptTableCatalog() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validateAppACLR2ReceiptTableCatalog() error = %v, want %q rejection", err, tt.want)
			}
		})
	}
}

func TestAppACLR2ReceiptHelperReaderMapsSecurityDefiner(t *testing.T) {
	tests := []struct {
		name            string
		securityDefiner bool
		want            string
	}{
		{name: "exact catalog"},
		{name: "security definer", securityDefiner: true, want: "security definer"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			helperRows := appACLR2ReceiptHelperRows()
			helperRows[1][10] = tt.securityDefiner
			tx := &scriptedAppACLR2ReceiptTx{queries: []scriptedAppACLR2ReceiptQuery{{rows: helperRows}}}
			helpers, err := readAppACLR2ReceiptHelpersInTx(context.Background(), tx)
			if err != nil {
				t.Fatalf("readAppACLR2ReceiptHelpersInTx() error = %v", err)
			}
			_, surface := validAppACLR2CatalogSnapshotFixture(t, validFrozenAppACLR1StateFixture(t))
			surface.Helpers = helpers
			err = validateAppACLR2ReceiptSurfaceCatalog(surface)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("validateAppACLR2ReceiptSurfaceCatalog() error = %v", err)
				}
				if helpers[1].SecurityDefiner {
					t.Fatal("receipt helper reader changed false pg_proc.prosecdef to true")
				}
				return
			}
			if !helpers[1].SecurityDefiner {
				t.Fatal("receipt helper reader dropped true pg_proc.prosecdef")
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validateAppACLR2ReceiptSurfaceCatalog() error = %v, want %q rejection", err, tt.want)
			}
		})
	}
}

func TestAppACLR2ReceiptHelperReaderRejectsExactDefinitionDrift(t *testing.T) {
	tests := []struct {
		name   string
		mutate func([][]any)
	}{
		{name: "exact catalog"},
		{name: "strict", mutate: func(rows [][]any) { rows[0][11] = true }},
		{name: "leakproof", mutate: func(rows [][]any) { rows[0][12] = true }},
		{name: "returns set", mutate: func(rows [][]any) { rows[0][13] = true }},
		{name: "nondefault cost", mutate: func(rows [][]any) { rows[0][14] = float64(101) }},
		{name: "support function", mutate: func(rows [][]any) { rows[0][16] = int64(9001) }},
		{name: "argument names", mutate: func(rows [][]any) { rows[0][23] = false }},
		{name: "SQL body", mutate: func(rows [][]any) { rows[0][27] = false }},
		{name: "definition suffix", mutate: func(rows [][]any) { rows[0][29] = rows[0][29].(string) + " STRICT" }},
		{name: "source literal case", mutate: func(rows [][]any) {
			rows[0][30] = strings.Replace(rows[0][30].(string), "'sha256'", "'SHA256'", 1)
		}},
		{name: "source whitespace", mutate: func(rows [][]any) { rows[1][30] = "\n" + rows[1][30].(string) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			helperRows := appACLR2ReceiptHelperRows()
			if tt.mutate != nil {
				tt.mutate(helperRows)
			}
			tx := &scriptedAppACLR2ReceiptTx{queries: []scriptedAppACLR2ReceiptQuery{{rows: helperRows}}}
			helpers, err := readAppACLR2ReceiptHelpersInTx(context.Background(), tx)
			if err != nil {
				t.Fatalf("readAppACLR2ReceiptHelpersInTx() error = %v", err)
			}
			_, surface := validAppACLR2CatalogSnapshotFixture(t, validFrozenAppACLR1StateFixture(t))
			surface.Helpers = helpers
			err = validateAppACLR2ReceiptSurfaceCatalog(surface)
			if tt.mutate == nil {
				if err != nil {
					t.Fatalf("validateAppACLR2ReceiptSurfaceCatalog() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "helper") {
				t.Fatalf("validateAppACLR2ReceiptSurfaceCatalog() error = %v, want exact helper-definition rejection", err)
			}
		})
	}
}

func TestAppACLR2ReservedCatalogObjectReaderRetainsAdditionalPrefixedObjects(t *testing.T) {
	tests := []struct {
		name  string
		extra AppACLR2ReservedCatalogObjectV1
	}{
		{name: "relation", extra: AppACLR2ReservedCatalogObjectV1{OID: 1101, Kind: "relation", Schema: "third_party", Identity: "app_acl_r2_extra", Detail: "r"}},
		{name: "function", extra: AppACLR2ReservedCatalogObjectV1{OID: 1102, Kind: "function", Schema: "third_party", Identity: "third_party.app_acl_r2_extra()", Detail: "f"}},
		{name: "trigger", extra: AppACLR2ReservedCatalogObjectV1{OID: 1103, Kind: "trigger", Schema: "third_party", Identity: "other_table.app_acl_r2_extra", Detail: "user"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := appACLR2ReservedCatalogObjectRows()
			rows = append(rows, appACLR2ReservedCatalogObjectRow(tt.extra))
			tx := &scriptedAppACLR2ReceiptTx{queries: []scriptedAppACLR2ReceiptQuery{{rows: rows}}}
			objects, err := readAppACLR2ReservedCatalogObjectsInTx(context.Background(), tx)
			if err != nil {
				t.Fatalf("readAppACLR2ReservedCatalogObjectsInTx() error = %v", err)
			}
			if got := objects[len(objects)-1]; got != tt.extra {
				t.Fatalf("extra reserved catalog object = %#v, want %#v", got, tt.extra)
			}
			_, surface := validAppACLR2CatalogSnapshotFixture(t, validFrozenAppACLR1StateFixture(t))
			surface.ReservedObjects = objects
			if err := validateAppACLR2ReceiptSurfaceCatalog(surface); err == nil || !strings.Contains(err.Error(), "reserved") {
				t.Fatalf("validateAppACLR2ReceiptSurfaceCatalog() error = %v, want reserved-object rejection", err)
			}
		})
	}
}

func TestAppACLR2ReceiptACLReaderRejectsReturnedInternalTrigger(t *testing.T) {
	state := FrozenAppACLR1StateV1{
		DirectMigratorRole: "direct_migrator",
		CenterRuntimeRole:  "center_runtime",
		PlatformAdminRole:  "platform_admin",
	}
	triggerRows := appACLR2ReceiptTriggerRows()
	triggerRows = append(triggerRows, []any{
		"public", "app_acl_r2_bootstrap_receipt", "receipt_fk_internal", "pg_catalog", "pg_catalog.RI_FKey_noaction_del()",
		int64(10), int64(10), true, true, int64(58), "", false, "CREATE CONSTRAINT TRIGGER receipt_fk_internal AFTER DELETE ON public.app_acl_r2_bootstrap_receipt FOR EACH ROW EXECUTE FUNCTION pg_catalog.RI_FKey_noaction_del()",
	})
	tx := &scriptedAppACLR2ReceiptTx{queries: []scriptedAppACLR2ReceiptQuery{
		{rows: appACLR2ReceiptObjectRows()},
		{rows: appACLR2ReceiptGrantRows()},
		{rows: appACLR2ReceiptEffectivePrivilegeRows()},
		{rows: triggerRows},
	}}
	if _, err := readAppACLR2ReceiptACLInTx(context.Background(), tx, state); err == nil || !strings.Contains(err.Error(), "internal") {
		t.Fatalf("readAppACLR2ReceiptACLInTx() error = %v, want returned-internal-trigger rejection", err)
	}
}

func TestAppACLR2ReceiptACLReaderRejectsUpdateOfColumnTrigger(t *testing.T) {
	state := FrozenAppACLR1StateV1{
		DirectMigratorRole: "direct_migrator",
		CenterRuntimeRole:  "center_runtime",
		PlatformAdminRole:  "platform_admin",
	}
	triggerRows := [][]any{{
		"public",
		"app_acl_r2_bootstrap_receipt",
		"app_acl_r2_bootstrap_receipt_immutable",
		"record_platform_internal",
		"record_platform_internal.app_acl_r2_reject_bootstrap_receipt_mutation()",
		int64(10),
		int64(10),
		true,
		false,
		int64(58),
		"1",
		false,
		"CREATE TRIGGER app_acl_r2_bootstrap_receipt_immutable BEFORE DELETE OR UPDATE OF singleton OR TRUNCATE ON public.app_acl_r2_bootstrap_receipt FOR EACH STATEMENT EXECUTE FUNCTION record_platform_internal.app_acl_r2_reject_bootstrap_receipt_mutation()",
	}}
	tx := &scriptedAppACLR2ReceiptTx{queries: []scriptedAppACLR2ReceiptQuery{
		{rows: appACLR2ReceiptObjectRows()},
		{rows: appACLR2ReceiptGrantRows()},
		{rows: appACLR2ReceiptEffectivePrivilegeRows()},
		{rows: triggerRows},
	}}
	_, err := readAppACLR2ReceiptACLInTx(context.Background(), tx, state)
	if len(tx.queryTexts) != 4 {
		t.Fatalf("readAppACLR2ReceiptACLInTx() query count = %d, want 4", len(tx.queryTexts))
	}
	query := strings.Join(strings.Fields(tx.queryTexts[3]), " ")
	const scanOrder = "trigger.tgtype::integer, trigger.tgattr::text, trigger.tgqual is not null, pg_catalog.pg_get_triggerdef(trigger.oid, false)"
	if !strings.Contains(query, scanOrder) {
		t.Fatalf("receipt trigger catalog query = %q, want scan-order fragment %q", query, scanOrder)
	}
	if err == nil || !strings.Contains(err.Error(), "UPDATE OF") {
		t.Fatalf("readAppACLR2ReceiptACLInTx() error = %v, want UPDATE OF column rejection", err)
	}
}

func TestAppACLR2ReceiptTriggerCatalogRejectsSemanticDrift(t *testing.T) {
	trigger := appACLR2GoldenL2ACLFixture().Triggers[0]
	const definition = "CREATE TRIGGER app_acl_r2_bootstrap_receipt_immutable BEFORE DELETE OR UPDATE OR TRUNCATE ON public.app_acl_r2_bootstrap_receipt FOR EACH STATEMENT EXECUTE FUNCTION record_platform_internal.app_acl_r2_reject_bootstrap_receipt_mutation()"
	if err := validateAppACLR2ReceiptTriggerCatalog(trigger, false, 58, "", false, definition); err != nil {
		t.Fatalf("validateAppACLR2ReceiptTriggerCatalog() error = %v", err)
	}
	for _, tt := range []struct {
		name              string
		internal          bool
		triggerType       int64
		triggerAttributes string
		hasQualification  bool
		definition        string
		want              string
	}{
		{name: "internal", internal: true, triggerType: 58, definition: definition},
		{name: "update only", triggerType: 18, definition: definition},
		{name: "qualified", triggerType: 58, hasQualification: true, definition: definition},
		{name: "definition", triggerType: 58, definition: "CREATE TRIGGER app_acl_r2_bootstrap_receipt_immutable BEFORE UPDATE ON public.app_acl_r2_bootstrap_receipt FOR EACH STATEMENT EXECUTE FUNCTION record_platform_internal.app_acl_r2_reject_bootstrap_receipt_mutation()"},
		{name: "update of column", triggerType: 58, triggerAttributes: "1", definition: "CREATE TRIGGER app_acl_r2_bootstrap_receipt_immutable BEFORE DELETE OR UPDATE OF singleton OR TRUNCATE ON public.app_acl_r2_bootstrap_receipt FOR EACH STATEMENT EXECUTE FUNCTION record_platform_internal.app_acl_r2_reject_bootstrap_receipt_mutation()", want: "UPDATE OF"},
		{name: "reordered events", triggerType: 58, definition: "CREATE TRIGGER app_acl_r2_bootstrap_receipt_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON public.app_acl_r2_bootstrap_receipt FOR EACH STATEMENT EXECUTE FUNCTION record_platform_internal.app_acl_r2_reject_bootstrap_receipt_mutation()"},
		{name: "extra event", triggerType: 58, definition: "CREATE TRIGGER app_acl_r2_bootstrap_receipt_immutable BEFORE INSERT OR DELETE OR UPDATE OR TRUNCATE ON public.app_acl_r2_bootstrap_receipt FOR EACH STATEMENT EXECUTE FUNCTION record_platform_internal.app_acl_r2_reject_bootstrap_receipt_mutation()"},
		{name: "keyword case", triggerType: 58, definition: strings.Replace(definition, "CREATE TRIGGER", "create trigger", 1)},
		{name: "source whitespace", triggerType: 58, definition: strings.Replace(definition, " BEFORE ", "  BEFORE ", 1)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAppACLR2ReceiptTriggerCatalog(trigger, tt.internal, tt.triggerType, tt.triggerAttributes, tt.hasQualification, tt.definition)
			if err == nil {
				t.Fatal("validateAppACLR2ReceiptTriggerCatalog() error = nil, want rejection")
			}
			if tt.want != "" && !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validateAppACLR2ReceiptTriggerCatalog() error = %v, want %q rejection", err, tt.want)
			}
		})
	}
}

func TestAppACLR2PGCryptoMemberCatalogReaderEnumeratesEveryExtensionDependencyClass(t *testing.T) {
	tx := &recordingAppACLR2PGCryptoMemberCatalogTx{}
	if _, err := readAppACLR2PGCryptoMembersInTx(context.Background(), tx); err == nil {
		t.Fatal("readAppACLR2PGCryptoMembersInTx() error = nil, want query sentinel")
	}

	query := strings.Join(strings.Fields(tx.query), " ")
	for _, want := range []string{
		"from pg_catalog.pg_extension extension join pg_catalog.pg_depend dependency",
		"dependency.refclassid = 'pg_catalog.pg_extension'::pg_catalog.regclass",
		"dependency.refobjid = extension.oid",
		"left join pg_catalog.pg_class member_class",
		"left join pg_catalog.pg_namespace class_namespace",
		"left join pg_catalog.pg_proc procedure",
		"dependency.classid = 'pg_catalog.pg_proc'::pg_catalog.regclass",
		"procedure.prokind::text",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("pgcrypto member dependency query = %q, want cross-catalog enumeration fragment %q", query, want)
		}
	}
	if strings.Contains(query, "from pg_catalog.pg_proc procedure join") {
		t.Fatalf("pgcrypto member dependency query must not root enumeration at pg_proc: %q", query)
	}
	if err := validateAppACLR2PGCryptoMemberCatalogIdentityFormatterQuery(query); err != nil {
		t.Fatal(err)
	}
}

const appACLR2PGCryptoIdentityFormatter = "pg_catalog.pg_get_function_identity_arguments(procedure.oid)"
const appACLR2PGCryptoIdentityFormatterProjection = "coalesce(" + appACLR2PGCryptoIdentityFormatter + ", '')"
const appACLR2PGCryptoMemberCatalogDirectOuterSelectPrefix = "select coalesce(class_namespace.nspname::text || '.' || member_class.relname::text, ''), " +
	"dependency.objid::bigint, dependency.objsubid::bigint, dependency.refobjsubid::bigint, dependency.deptype::text, extension.oid::bigint, " +
	"(select pg_catalog.count(*)::bigint from pg_catalog.pg_depend extension_dependency where extension_dependency.classid = dependency.classid and extension_dependency.refclassid = 'pg_catalog.pg_extension'::pg_catalog.regclass and extension_dependency.objid = dependency.objid), " +
	"coalesce(namespace.nspname::text, ''), coalesce(procedure.proname::text, ''), "
const appACLR2PGCryptoMemberCatalogDirectOuterSelectSuffix = ", coalesce(procedure.prokind::text, ''), coalesce(owner.rolname, ''), coalesce(owner.oid::bigint, 0), procedure.proacl is null"
const appACLR2PGCryptoMemberCatalogOuterSelectProjection = appACLR2PGCryptoMemberCatalogDirectOuterSelectPrefix +
	appACLR2PGCryptoIdentityFormatterProjection + appACLR2PGCryptoMemberCatalogDirectOuterSelectSuffix

type appACLR2PGCryptoMemberCatalogQueryRejectionRule string

const (
	appACLR2PGCryptoMemberCatalogQueryDirectOuterProjection appACLR2PGCryptoMemberCatalogQueryRejectionRule = "direct-outer-projection"
	appACLR2PGCryptoMemberCatalogQueryForbiddenFormatter    appACLR2PGCryptoMemberCatalogQueryRejectionRule = "forbidden-formatter"
	appACLR2PGCryptoMemberCatalogQueryForbiddenTransform    appACLR2PGCryptoMemberCatalogQueryRejectionRule = "forbidden-transform"
	appACLR2PGCryptoMemberCatalogQueryForbiddenSpecialCase  appACLR2PGCryptoMemberCatalogQueryRejectionRule = "forbidden-special-case"
	appACLR2PGCryptoMemberCatalogQueryExactProjection       appACLR2PGCryptoMemberCatalogQueryRejectionRule = "exact-identity-projection"
)

type appACLR2PGCryptoMemberCatalogQueryValidationError struct {
	Rule  appACLR2PGCryptoMemberCatalogQueryRejectionRule
	Query string
}

func (err appACLR2PGCryptoMemberCatalogQueryValidationError) Error() string {
	return fmt.Sprintf("pgcrypto member dependency query rejected by %s: %q", err.Rule, err.Query)
}

func TestAppACLR2PGCryptoMemberCatalogQueryRejectsFullProductionMutationsByRule(t *testing.T) {
	productionQuery := appACLR2PGCryptoMemberCatalogRecordedQuery(t)
	for _, tt := range []struct {
		name   string
		mutate func(*testing.T, string) string
		want   appACLR2PGCryptoMemberCatalogQueryRejectionRule
	}{
		{
			name: "regexp replace around formatter",
			mutate: func(t *testing.T, query string) string {
				return appACLR2PGCryptoMemberCatalogReplaceIdentityProjection(t, query, "regexp_replace("+appACLR2PGCryptoIdentityFormatterProjection+", ' OUT [^,]+', '', 'g')")
			},
			want: appACLR2PGCryptoMemberCatalogQueryForbiddenTransform,
		},
		{
			name: "conditional formatter handling",
			mutate: func(t *testing.T, query string) string {
				return appACLR2PGCryptoMemberCatalogReplaceIdentityProjection(t, query, "case when procedure.proname = 'other' then '' else "+appACLR2PGCryptoIdentityFormatterProjection+" end")
			},
			want: appACLR2PGCryptoMemberCatalogQueryForbiddenSpecialCase,
		},
		{
			name: "pgp armor headers special OUT stripping",
			mutate: func(t *testing.T, query string) string {
				return appACLR2PGCryptoMemberCatalogReplaceIdentityProjection(t, query, "case when procedure.proname = 'pgp_armor_headers' then regexp_replace('text, OUT key text, OUT value text', ' OUT [^,]+', '', 'g') else "+appACLR2PGCryptoIdentityFormatterProjection+" end")
			},
			want: appACLR2PGCryptoMemberCatalogQueryForbiddenSpecialCase,
		},
		{
			name: "pg get function arguments",
			mutate: func(t *testing.T, query string) string {
				return appACLR2PGCryptoMemberCatalogReplaceIdentityProjection(t, query, "coalesce(pg_catalog.pg_get_function_arguments(procedure.oid), '')")
			},
			want: appACLR2PGCryptoMemberCatalogQueryForbiddenFormatter,
		},
		{
			name: "oidvectortypes proargtypes",
			mutate: func(t *testing.T, query string) string {
				return appACLR2PGCryptoMemberCatalogReplaceIdentityProjection(t, query, "coalesce(pg_catalog.oidvectortypes(procedure.proargtypes), '')")
			},
			want: appACLR2PGCryptoMemberCatalogQueryForbiddenFormatter,
		},
		{
			name: "proargtypes",
			mutate: func(t *testing.T, query string) string {
				return appACLR2PGCryptoMemberCatalogReplaceIdentityProjection(t, query, "coalesce(procedure.proargtypes::text, '')")
			},
			want: appACLR2PGCryptoMemberCatalogQueryForbiddenFormatter,
		},
		{
			name: "proallargtypes",
			mutate: func(t *testing.T, query string) string {
				return appACLR2PGCryptoMemberCatalogReplaceIdentityProjection(t, query, "coalesce(procedure.proallargtypes::text, '')")
			},
			want: appACLR2PGCryptoMemberCatalogQueryForbiddenFormatter,
		},
		{
			name: "proargmodes",
			mutate: func(t *testing.T, query string) string {
				return appACLR2PGCryptoMemberCatalogReplaceIdentityProjection(t, query, "coalesce(procedure.proargmodes::text, '')")
			},
			want: appACLR2PGCryptoMemberCatalogQueryForbiddenFormatter,
		},
		{
			name: "proargnames",
			mutate: func(t *testing.T, query string) string {
				return appACLR2PGCryptoMemberCatalogReplaceIdentityProjection(t, query, "coalesce(procedure.proargnames::text, '')")
			},
			want: appACLR2PGCryptoMemberCatalogQueryForbiddenFormatter,
		},
		{
			name:   "nested CTE split part",
			mutate: appACLR2PGCryptoMemberCatalogNestedCTEOUTStrippingQuery,
			want:   appACLR2PGCryptoMemberCatalogQueryDirectOuterProjection,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			appACLR2PGCryptoMemberCatalogAssertQueryRejectedByRule(t, tt.mutate(t, productionQuery), tt.want)
		})
	}
}

func appACLR2PGCryptoMemberCatalogRecordedQuery(t *testing.T) string {
	t.Helper()
	tx := &recordingAppACLR2PGCryptoMemberCatalogTx{}
	if _, err := readAppACLR2PGCryptoMembersInTx(context.Background(), tx); err == nil {
		t.Fatal("readAppACLR2PGCryptoMembersInTx() error = nil, want query sentinel")
	}
	return strings.Join(strings.Fields(strings.ToLower(tx.query)), " ")
}

func appACLR2PGCryptoMemberCatalogReplaceIdentityProjection(t *testing.T, query, replacement string) string {
	t.Helper()
	mutated := strings.Replace(query, appACLR2PGCryptoIdentityFormatterProjection, replacement, 1)
	if mutated == query {
		t.Fatalf("pgcrypto member query did not contain formatter projection %q: %q", appACLR2PGCryptoIdentityFormatterProjection, query)
	}
	return mutated
}

func appACLR2PGCryptoMemberCatalogNestedCTEOUTStrippingQuery(t *testing.T, query string) string {
	t.Helper()
	return "with raw (extension_dependency_class, oid, object_sub_id, reference_object_sub_id, dependency_type, extension_oid, extension_dependency_count, schema_name, procedure_name, identity_arguments, routine_kind, owner_name, owner_oid, acl_is_default) as (" +
		query + ") select extension_dependency_class, oid, object_sub_id, reference_object_sub_id, dependency_type, extension_oid, extension_dependency_count, schema_name, procedure_name, split_part(identity_arguments, ',', 1), routine_kind, owner_name, owner_oid, acl_is_default from raw"
}

func appACLR2PGCryptoMemberCatalogAssertQueryRejectedByRule(t *testing.T, query string, want appACLR2PGCryptoMemberCatalogQueryRejectionRule) {
	t.Helper()
	err := validateAppACLR2PGCryptoMemberCatalogIdentityFormatterQuery(query)
	if err == nil {
		t.Fatalf("pgcrypto member query accepted mutation, want %s rejection: %q", want, query)
	}
	classified, ok := err.(appACLR2PGCryptoMemberCatalogQueryValidationError)
	if !ok {
		t.Fatalf("pgcrypto member query rejection is not classified as %s: %v", want, err)
	}
	if classified.Rule != want {
		t.Fatalf("pgcrypto member query rejection rule = %s, want %s: %v", classified.Rule, want, err)
	}
}

func validateAppACLR2PGCryptoMemberCatalogIdentityFormatterQuery(query string) error {
	query = strings.Join(strings.Fields(strings.ToLower(query)), " ")
	if !strings.HasPrefix(query, appACLR2PGCryptoMemberCatalogDirectOuterSelectPrefix) {
		return appACLR2PGCryptoMemberCatalogQueryValidationError{Rule: appACLR2PGCryptoMemberCatalogQueryDirectOuterProjection, Query: query}
	}
	for _, forbidden := range []string{
		"case when ",
		"pgp_armor_headers",
	} {
		if strings.Contains(query, forbidden) {
			return appACLR2PGCryptoMemberCatalogQueryValidationError{Rule: appACLR2PGCryptoMemberCatalogQueryForbiddenSpecialCase, Query: query}
		}
	}
	for _, forbidden := range []string{
		"regexp_replace(",
		"replace(",
		"trim(",
		"btrim(",
		"ltrim(",
		"rtrim(",
		"substring(",
		"substr(",
	} {
		if strings.Contains(query, forbidden) {
			return appACLR2PGCryptoMemberCatalogQueryValidationError{Rule: appACLR2PGCryptoMemberCatalogQueryForbiddenTransform, Query: query}
		}
	}
	for _, forbidden := range []string{
		"pg_get_function_arguments",
		"oidvectortypes",
		"proargtypes",
		"proallargtypes",
		"proargmodes",
		"proargnames",
	} {
		if strings.Contains(query, forbidden) {
			return appACLR2PGCryptoMemberCatalogQueryValidationError{Rule: appACLR2PGCryptoMemberCatalogQueryForbiddenFormatter, Query: query}
		}
	}
	if strings.Count(query, appACLR2PGCryptoIdentityFormatter) != 1 || strings.Count(query, appACLR2PGCryptoMemberCatalogOuterSelectProjection) != 1 {
		return appACLR2PGCryptoMemberCatalogQueryValidationError{Rule: appACLR2PGCryptoMemberCatalogQueryExactProjection, Query: query}
	}
	return nil
}

func validAppACLR2CatalogSnapshotFixture(t *testing.T, frozen FrozenAppACLR1StateV1) (AppACLR2BootstrapCatalogSnapshotV1, AppACLR2ReceiptCatalogSnapshotV1) {
	t.Helper()
	receipt := validAppACLR2BootstrapReceiptFixture(t)
	roles := make([]AppACLR2CatalogRoleStateV1, len(receipt.Roles))
	for index, role := range receipt.Roles {
		roles[index] = AppACLR2CatalogRoleStateV1{
			ControlRole: role.ControlRole, Name: role.Name, OID: role.OID,
			Login: role.Login, Inherit: role.Inherit, Superuser: role.Superuser,
		}
	}
	members := make([]AppACLR2PGCryptoMemberCatalogV1, len(receipt.Members))
	for index, member := range receipt.Members {
		members[index] = AppACLR2PGCryptoMemberCatalogV1{
			AppACLR2ReceiptMemberV1: member,
			ExtensionOID:            500, ExtensionDependencyClass: "pg_catalog.pg_proc",
			ExtensionDependencyType: "e", ExtensionDependencyCount: 1, RoutineKind: "f", ACLIsDefault: true,
		}
	}
	return AppACLR2BootstrapCatalogSnapshotV1{
			ServerVersionNum: receipt.ServerVersionNum, ServerVersion: receipt.ServerVersion,
			DatabaseOID: 424242, DatabaseName: "houfeng_app", PostgresSystemIdentifier: "72623859790382856",
			Domains: []AppACLDomainR2V1{appACLR2GoldenDomainFixture()},
			Roles:   roles,
			Extension: AppACLR2PGCryptoExtensionCatalogV1{
				Name: receipt.ExtensionName, OID: receipt.ExtensionOID, Schema: receipt.ExtensionSchema,
				SchemaOID: 600, Version: receipt.ExtensionVersion,
				OwnerName: receipt.ExtensionOwnerName, OwnerOID: receipt.ExtensionOwnerOID,
			},
			Members: members,
		}, AppACLR2ReceiptCatalogSnapshotV1{
			Table:           appACLR2ReceiptTableCatalogFixture(),
			ReservedObjects: appACLR2ReservedCatalogObjectFixture(),
			ACL:             appACLR2GoldenL2ACLFixture(),
			Helpers:         appACLR2ReceiptHelperCatalogFixture(),
		}
}

func appACLR2ReceiptHelperCatalogFixture() []AppACLR2ReceiptHelperCatalogV1 {
	return []AppACLR2ReceiptHelperCatalogV1{
		{
			Schema: "record_platform_internal", Name: "app_acl_r2_assert_bootstrap_receipt_insert",
			IdentityArguments: "bytea, bytea", Identity: "record_platform_internal.app_acl_r2_assert_bootstrap_receipt_insert(bytea, bytea)",
			OwnerOID: 10, Kind: "f", Result: "boolean", Language: "sql", Volatility: "i", Parallel: "s",
			Cost: 100, ArgumentCount: 2, InputArgumentTypes: "17 17",
			AllArgumentTypesNull: true, ArgumentModesNull: true, ArgumentNamesNull: true, ArgumentDefaultsNull: true,
			TransformTypesNull: true, BinaryNull: true, SQLBodyNull: true,
			Config: []string{"search_path=pg_catalog"},
			Definition: `CREATE OR REPLACE FUNCTION record_platform_internal.app_acl_r2_assert_bootstrap_receipt_insert(bytea, bytea)
 RETURNS boolean
 LANGUAGE sql
 IMMUTABLE PARALLEL SAFE
 SET search_path TO 'pg_catalog'
AS $function$
  select pg_catalog.octet_length($1) between 1 and 4194304
    and pg_catalog.octet_length($2) = 32
    and record_platform_internal.digest($1, 'sha256') = $2
$function$
`,
			Source: `
  select pg_catalog.octet_length($1) between 1 and 4194304
    and pg_catalog.octet_length($2) = 32
    and record_platform_internal.digest($1, 'sha256') = $2
`,
		},
		{
			Schema: "record_platform_internal", Name: "app_acl_r2_reject_bootstrap_receipt_mutation",
			IdentityArguments: "", Identity: "record_platform_internal.app_acl_r2_reject_bootstrap_receipt_mutation()",
			OwnerOID: 10, Kind: "f", Result: "trigger", Language: "plpgsql", Volatility: "v", Parallel: "u",
			Cost: 100, InputArgumentTypes: "",
			AllArgumentTypesNull: true, ArgumentModesNull: true, ArgumentNamesNull: true, ArgumentDefaultsNull: true,
			TransformTypesNull: true, BinaryNull: true, SQLBodyNull: true,
			Config: []string{"search_path=pg_catalog"},
			Definition: `CREATE OR REPLACE FUNCTION record_platform_internal.app_acl_r2_reject_bootstrap_receipt_mutation()
 RETURNS trigger
 LANGUAGE plpgsql
 SET search_path TO 'pg_catalog'
AS $function$
begin
  raise sqlstate '55000'
    using message = 'app_acl_r2_bootstrap_receipt is immutable';
end;
$function$
`,
			Source: `
begin
  raise sqlstate '55000'
    using message = 'app_acl_r2_bootstrap_receipt is immutable';
end;
`,
		},
	}
}

func appACLR2ReservedCatalogObjectFixture() []AppACLR2ReservedCatalogObjectV1 {
	return []AppACLR2ReservedCatalogObjectV1{
		{OID: 1001, Kind: "function", Schema: "record_platform_internal", Identity: "record_platform_internal.app_acl_r2_assert_bootstrap_receipt_insert(bytea, bytea)", Detail: "f"},
		{OID: 1002, Kind: "function", Schema: "record_platform_internal", Identity: "record_platform_internal.app_acl_r2_reject_bootstrap_receipt_mutation()", Detail: "f"},
		{OID: 1003, Kind: "relation", Schema: "public", Identity: "app_acl_r2_bootstrap_receipt", Detail: "r"},
		{OID: 1004, Kind: "relation", Schema: "public", Identity: "app_acl_r2_bootstrap_receipt_pkey", Detail: "i"},
		{OID: 1005, Kind: "trigger", Schema: "public", Identity: "app_acl_r2_bootstrap_receipt.app_acl_r2_bootstrap_receipt_immutable", Detail: "user"},
	}
}

func appACLR2ReceiptTableCatalogFixture() AppACLR2ReceiptTableCatalogV1 {
	return AppACLR2ReceiptTableCatalogV1{
		Schema: "public", Name: "app_acl_r2_bootstrap_receipt", OwnerOID: 10, Kind: "r", Persistence: "p",
		Columns: []AppACLR2ReceiptTableColumnCatalogV1{
			{Name: "singleton", Type: "boolean", NotNull: true, DefaultExpression: "true"},
			{Name: "receipt_body", Type: "bytea", NotNull: true},
			{Name: "receipt_digest", Type: "bytea", NotNull: true},
		},
		Constraints: []AppACLR2ReceiptTableConstraintCatalogV1{
			{Type: "c", Definition: "CHECK ((octet_length(receipt_body) >= 1) AND (octet_length(receipt_body) <= 4194304))", Validated: true},
			{Type: "c", Definition: "CHECK (octet_length(receipt_digest) = 32)", Validated: true},
			{Type: "c", Definition: "CHECK (record_platform_internal.app_acl_r2_assert_bootstrap_receipt_insert(receipt_body, receipt_digest))", Validated: true},
			{Type: "c", Definition: "CHECK (singleton)", Validated: true},
			{Type: "p", Definition: "PRIMARY KEY (singleton)", Validated: true, IndexOID: 1004, IndexPrimary: true, IndexUnique: true, IndexValid: true},
		},
	}
}

func newScriptedAppACLR2ReceiptCatalogTx() *scriptedAppACLR2ReceiptTx {
	return &scriptedAppACLR2ReceiptTx{
		queryRows: []scriptedAppACLR2ReceiptQueryRow{
			{values: appACLR2ReceiptTableRelationRow()},
			{values: []any{int64(0)}},
		},
		queries: []scriptedAppACLR2ReceiptQuery{
			{rows: appACLR2ReceiptTableColumnRows()},
			{rows: appACLR2ReceiptTableConstraintRows()},
			{rows: appACLR2ReservedCatalogObjectRows()},
			{rows: appACLR2ReceiptObjectRows()},
			{rows: appACLR2ReceiptGrantRows()},
			{rows: appACLR2ReceiptEffectivePrivilegeRows()},
			{rows: appACLR2ReceiptTriggerRows()},
			{rows: appACLR2ReceiptHelperRows()},
		},
	}
}

func newScriptedAppACLR2BootstrapCatalogTx() *scriptedAppACLR2ReceiptTx {
	return newScriptedAppACLR2BootstrapCatalogTxWithPGCryptoRows(appACLR2PGCryptoMemberCatalogRows())
}

func newScriptedAppACLR2BootstrapCatalogTxWithPGCryptoRows(memberRows [][]any) *scriptedAppACLR2ReceiptTx {
	return &scriptedAppACLR2ReceiptTx{
		queryRows: []scriptedAppACLR2ReceiptQueryRow{
			{values: []any{int64(160012), "16.12", int64(424242), "houfeng_app"}},
			{values: []any{int64(0)}},
			{values: []any{"pgcrypto", int64(500), "record_platform_internal", int64(600), "1.3", "direct_migrator", int64(21)}},
			{values: []any{"72623859790382856"}},
		},
		queries: []scriptedAppACLR2ReceiptQuery{
			{rows: [][]any{{
				"rd-15ca58e2c2c7daa3ca20f4e0c6f85af84254c9a675f2bb3092fcbd0739bf1a18",
				"application",
				int64(1),
				"postgres_system",
				"72623859790382856",
				int64(424242),
				"houfeng_app",
			}}},
			{rows: [][]any{
				{"bootstrap_oid10", int64(10), true, true, true, true, true, true, true},
				{"center_runtime", int64(20), true, false, false, false, false, false, false},
				{"direct_migrator", int64(21), true, false, false, false, false, false, false},
				{"platform_admin", int64(22), true, false, false, false, false, false, false},
			}},
			{rows: nil},
			{rows: memberRows},
		},
	}
}

func appACLR2PGCryptoMemberCatalogRows() [][]any {
	return [][]any{
		{"pg_catalog.pg_proc", int64(7001), int64(0), int64(0), "e", int64(500), int64(1), "record_platform_internal", "armor", "bytea", "f", "bootstrap_oid10", int64(10), true},
		{"pg_catalog.pg_proc", int64(7002), int64(0), int64(0), "e", int64(500), int64(1), "record_platform_internal", "armor", "bytea, text[], text[]", "f", "bootstrap_oid10", int64(10), true},
		{"pg_catalog.pg_proc", int64(7003), int64(0), int64(0), "e", int64(500), int64(1), "record_platform_internal", "crypt", "text, text", "f", "bootstrap_oid10", int64(10), true},
		{"pg_catalog.pg_proc", int64(7004), int64(0), int64(0), "e", int64(500), int64(1), "record_platform_internal", "dearmor", "text", "f", "bootstrap_oid10", int64(10), true},
		{"pg_catalog.pg_proc", int64(7005), int64(0), int64(0), "e", int64(500), int64(1), "record_platform_internal", "decrypt", "bytea, bytea, text", "f", "bootstrap_oid10", int64(10), true},
		{"pg_catalog.pg_proc", int64(7006), int64(0), int64(0), "e", int64(500), int64(1), "record_platform_internal", "decrypt_iv", "bytea, bytea, bytea, text", "f", "bootstrap_oid10", int64(10), true},
		{"pg_catalog.pg_proc", int64(7007), int64(0), int64(0), "e", int64(500), int64(1), "record_platform_internal", "digest", "bytea, text", "f", "bootstrap_oid10", int64(10), true},
		{"pg_catalog.pg_proc", int64(7008), int64(0), int64(0), "e", int64(500), int64(1), "record_platform_internal", "digest", "text, text", "f", "bootstrap_oid10", int64(10), true},
		{"pg_catalog.pg_proc", int64(7009), int64(0), int64(0), "e", int64(500), int64(1), "record_platform_internal", "encrypt", "bytea, bytea, text", "f", "bootstrap_oid10", int64(10), true},
		{"pg_catalog.pg_proc", int64(7010), int64(0), int64(0), "e", int64(500), int64(1), "record_platform_internal", "encrypt_iv", "bytea, bytea, bytea, text", "f", "bootstrap_oid10", int64(10), true},
		{"pg_catalog.pg_proc", int64(7011), int64(0), int64(0), "e", int64(500), int64(1), "record_platform_internal", "gen_random_bytes", "integer", "f", "bootstrap_oid10", int64(10), true},
		{"pg_catalog.pg_proc", int64(7012), int64(0), int64(0), "e", int64(500), int64(1), "record_platform_internal", "gen_random_uuid", "", "f", "bootstrap_oid10", int64(10), true},
		{"pg_catalog.pg_proc", int64(7013), int64(0), int64(0), "e", int64(500), int64(1), "record_platform_internal", "gen_salt", "text", "f", "bootstrap_oid10", int64(10), true},
		{"pg_catalog.pg_proc", int64(7014), int64(0), int64(0), "e", int64(500), int64(1), "record_platform_internal", "gen_salt", "text, integer", "f", "bootstrap_oid10", int64(10), true},
		{"pg_catalog.pg_proc", int64(7015), int64(0), int64(0), "e", int64(500), int64(1), "record_platform_internal", "hmac", "bytea, bytea, text", "f", "bootstrap_oid10", int64(10), true},
		{"pg_catalog.pg_proc", int64(7016), int64(0), int64(0), "e", int64(500), int64(1), "record_platform_internal", "hmac", "text, text, text", "f", "bootstrap_oid10", int64(10), true},
		{"pg_catalog.pg_proc", int64(7017), int64(0), int64(0), "e", int64(500), int64(1), "record_platform_internal", "pgp_armor_headers", "text, OUT key text, OUT value text", "f", "bootstrap_oid10", int64(10), true},
		{"pg_catalog.pg_proc", int64(7018), int64(0), int64(0), "e", int64(500), int64(1), "record_platform_internal", "pgp_key_id", "bytea", "f", "bootstrap_oid10", int64(10), true},
		{"pg_catalog.pg_proc", int64(7019), int64(0), int64(0), "e", int64(500), int64(1), "record_platform_internal", "pgp_pub_decrypt", "bytea, bytea", "f", "bootstrap_oid10", int64(10), true},
		{"pg_catalog.pg_proc", int64(7020), int64(0), int64(0), "e", int64(500), int64(1), "record_platform_internal", "pgp_pub_decrypt", "bytea, bytea, text", "f", "bootstrap_oid10", int64(10), true},
		{"pg_catalog.pg_proc", int64(7021), int64(0), int64(0), "e", int64(500), int64(1), "record_platform_internal", "pgp_pub_decrypt", "bytea, bytea, text, text", "f", "bootstrap_oid10", int64(10), true},
		{"pg_catalog.pg_proc", int64(7022), int64(0), int64(0), "e", int64(500), int64(1), "record_platform_internal", "pgp_pub_decrypt_bytea", "bytea, bytea", "f", "bootstrap_oid10", int64(10), true},
		{"pg_catalog.pg_proc", int64(7023), int64(0), int64(0), "e", int64(500), int64(1), "record_platform_internal", "pgp_pub_decrypt_bytea", "bytea, bytea, text", "f", "bootstrap_oid10", int64(10), true},
		{"pg_catalog.pg_proc", int64(7024), int64(0), int64(0), "e", int64(500), int64(1), "record_platform_internal", "pgp_pub_decrypt_bytea", "bytea, bytea, text, text", "f", "bootstrap_oid10", int64(10), true},
		{"pg_catalog.pg_proc", int64(7025), int64(0), int64(0), "e", int64(500), int64(1), "record_platform_internal", "pgp_pub_encrypt", "text, bytea", "f", "bootstrap_oid10", int64(10), true},
		{"pg_catalog.pg_proc", int64(7026), int64(0), int64(0), "e", int64(500), int64(1), "record_platform_internal", "pgp_pub_encrypt", "text, bytea, text", "f", "bootstrap_oid10", int64(10), true},
		{"pg_catalog.pg_proc", int64(7027), int64(0), int64(0), "e", int64(500), int64(1), "record_platform_internal", "pgp_pub_encrypt_bytea", "bytea, bytea", "f", "bootstrap_oid10", int64(10), true},
		{"pg_catalog.pg_proc", int64(7028), int64(0), int64(0), "e", int64(500), int64(1), "record_platform_internal", "pgp_pub_encrypt_bytea", "bytea, bytea, text", "f", "bootstrap_oid10", int64(10), true},
		{"pg_catalog.pg_proc", int64(7029), int64(0), int64(0), "e", int64(500), int64(1), "record_platform_internal", "pgp_sym_decrypt", "bytea, text", "f", "bootstrap_oid10", int64(10), true},
		{"pg_catalog.pg_proc", int64(7030), int64(0), int64(0), "e", int64(500), int64(1), "record_platform_internal", "pgp_sym_decrypt", "bytea, text, text", "f", "bootstrap_oid10", int64(10), true},
		{"pg_catalog.pg_proc", int64(7031), int64(0), int64(0), "e", int64(500), int64(1), "record_platform_internal", "pgp_sym_decrypt_bytea", "bytea, text", "f", "bootstrap_oid10", int64(10), true},
		{"pg_catalog.pg_proc", int64(7032), int64(0), int64(0), "e", int64(500), int64(1), "record_platform_internal", "pgp_sym_decrypt_bytea", "bytea, text, text", "f", "bootstrap_oid10", int64(10), true},
		{"pg_catalog.pg_proc", int64(7033), int64(0), int64(0), "e", int64(500), int64(1), "record_platform_internal", "pgp_sym_encrypt", "text, text", "f", "bootstrap_oid10", int64(10), true},
		{"pg_catalog.pg_proc", int64(7034), int64(0), int64(0), "e", int64(500), int64(1), "record_platform_internal", "pgp_sym_encrypt", "text, text, text", "f", "bootstrap_oid10", int64(10), true},
		{"pg_catalog.pg_proc", int64(7035), int64(0), int64(0), "e", int64(500), int64(1), "record_platform_internal", "pgp_sym_encrypt_bytea", "bytea, text", "f", "bootstrap_oid10", int64(10), true},
		{"pg_catalog.pg_proc", int64(7036), int64(0), int64(0), "e", int64(500), int64(1), "record_platform_internal", "pgp_sym_encrypt_bytea", "bytea, text, text", "f", "bootstrap_oid10", int64(10), true},
	}
}

func assertScriptedAppACLR2ReceiptQueriesAreIdentityNeutral(t *testing.T, tx *scriptedAppACLR2ReceiptTx) {
	t.Helper()
	queries := append([]string(nil), tx.queryTexts...)
	queries = append(queries, tx.queryRowTexts...)
	for _, query := range queries {
		for _, forbidden := range []string{"session_user", "current_user"} {
			if strings.Contains(strings.ToLower(query), forbidden) {
				t.Fatalf("credential-neutral receipt catalog reader queried %q", forbidden)
			}
		}
	}
}

func appACLR2ReceiptTableRelationRow() []any {
	return []any{"public", "app_acl_r2_bootstrap_receipt", int64(1003), int64(10), "r", "p"}
}

func appACLR2ReceiptTableColumnRows() [][]any {
	return [][]any{
		{"singleton", "boolean", true, "true"},
		{"receipt_body", "bytea", true, ""},
		{"receipt_digest", "bytea", true, ""},
	}
}

func appACLR2ReceiptTableConstraintRows() [][]any {
	return [][]any{
		{"c", "CHECK ((octet_length(receipt_body) >= 1) AND (octet_length(receipt_body) <= 4194304))", true, int64(0), false, false, false},
		{"c", "CHECK (octet_length(receipt_digest) = 32)", true, int64(0), false, false, false},
		{"c", "CHECK (record_platform_internal.app_acl_r2_assert_bootstrap_receipt_insert(receipt_body, receipt_digest))", true, int64(0), false, false, false},
		{"c", "CHECK (singleton)", true, int64(0), false, false, false},
		{"p", "PRIMARY KEY (singleton)", true, int64(1004), true, true, true},
	}
}

func appACLR2ReservedCatalogObjectRows() [][]any {
	objects := appACLR2ReservedCatalogObjectFixture()
	rows := make([][]any, 0, len(objects))
	for _, object := range objects {
		rows = append(rows, appACLR2ReservedCatalogObjectRow(object))
	}
	return rows
}

func appACLR2ReservedCatalogObjectRow(object AppACLR2ReservedCatalogObjectV1) []any {
	return []any{object.Kind, object.Schema, int64(object.OID), object.Identity, object.Detail}
}

func appACLR2ReceiptObjectRows() [][]any {
	return [][]any{
		{1, "public", "app_acl_r2_bootstrap_receipt", int64(10)},
		{2, "record_platform_internal", "record_platform_internal.app_acl_r2_assert_bootstrap_receipt_insert(bytea, bytea)", int64(10)},
		{2, "record_platform_internal", "record_platform_internal.app_acl_r2_reject_bootstrap_receipt_mutation()", int64(10)},
	}
}

func appACLR2ReceiptGrantRows() [][]any {
	return [][]any{
		{1, "app_acl_r2_bootstrap_receipt", "direct_migrator", "SELECT", false},
		{1, "app_acl_r2_bootstrap_receipt", "center_runtime", "SELECT", false},
	}
}

func appACLR2ReceiptEffectivePrivilegeRows() [][]any {
	return [][]any{
		{"bootstrap_oid10", true, true, true, true, true, true, true, true, true},
		{"center_runtime", true, false, false, false, false, false, false, false, false},
		{"direct_migrator", true, false, false, false, false, false, false, false, false},
		{"platform_admin", false, false, false, false, false, false, false, false, false},
	}
}

func appACLR2ReceiptTriggerRows() [][]any {
	return [][]any{{
		"public",
		"app_acl_r2_bootstrap_receipt",
		"app_acl_r2_bootstrap_receipt_immutable",
		"record_platform_internal",
		"record_platform_internal.app_acl_r2_reject_bootstrap_receipt_mutation()",
		int64(10),
		int64(10),
		true,
		false,
		int64(58),
		"",
		false,
		"CREATE TRIGGER app_acl_r2_bootstrap_receipt_immutable BEFORE DELETE OR UPDATE OR TRUNCATE ON public.app_acl_r2_bootstrap_receipt FOR EACH STATEMENT EXECUTE FUNCTION record_platform_internal.app_acl_r2_reject_bootstrap_receipt_mutation()",
	}}
}

func appACLR2ReceiptHelperRows() [][]any {
	return [][]any{
		{
			"record_platform_internal",
			"app_acl_r2_assert_bootstrap_receipt_insert",
			"bytea, bytea",
			"record_platform_internal.app_acl_r2_assert_bootstrap_receipt_insert(bytea, bytea)",
			int64(10),
			"f",
			"boolean",
			"sql",
			"i",
			"s",
			false,
			false,
			false,
			false,
			float64(100),
			float64(0),
			int64(0),
			int64(0),
			int64(2),
			int64(0),
			"17 17",
			true,
			true,
			true,
			true,
			true,
			true,
			true,
			[]string{"search_path=pg_catalog"},
			`CREATE OR REPLACE FUNCTION record_platform_internal.app_acl_r2_assert_bootstrap_receipt_insert(bytea, bytea)
 RETURNS boolean
 LANGUAGE sql
 IMMUTABLE PARALLEL SAFE
 SET search_path TO 'pg_catalog'
AS $function$
  select pg_catalog.octet_length($1) between 1 and 4194304
    and pg_catalog.octet_length($2) = 32
    and record_platform_internal.digest($1, 'sha256') = $2
$function$
`,
			`
  select pg_catalog.octet_length($1) between 1 and 4194304
    and pg_catalog.octet_length($2) = 32
    and record_platform_internal.digest($1, 'sha256') = $2
`,
		},
		{
			"record_platform_internal",
			"app_acl_r2_reject_bootstrap_receipt_mutation",
			"",
			"record_platform_internal.app_acl_r2_reject_bootstrap_receipt_mutation()",
			int64(10),
			"f",
			"trigger",
			"plpgsql",
			"v",
			"u",
			false,
			false,
			false,
			false,
			float64(100),
			float64(0),
			int64(0),
			int64(0),
			int64(0),
			int64(0),
			"",
			true,
			true,
			true,
			true,
			true,
			true,
			true,
			[]string{"search_path=pg_catalog"},
			`CREATE OR REPLACE FUNCTION record_platform_internal.app_acl_r2_reject_bootstrap_receipt_mutation()
 RETURNS trigger
 LANGUAGE plpgsql
 SET search_path TO 'pg_catalog'
AS $function$
begin
  raise sqlstate '55000'
    using message = 'app_acl_r2_bootstrap_receipt is immutable';
end;
$function$
`,
			`
begin
  raise sqlstate '55000'
    using message = 'app_acl_r2_bootstrap_receipt is immutable';
end;
`,
		},
	}
}

func cloneAppACLR2BootstrapCatalogSnapshot(value AppACLR2BootstrapCatalogSnapshotV1) AppACLR2BootstrapCatalogSnapshotV1 {
	value.Domains = append([]AppACLDomainR2V1(nil), value.Domains...)
	value.Roles = append([]AppACLR2CatalogRoleStateV1(nil), value.Roles...)
	value.Members = append([]AppACLR2PGCryptoMemberCatalogV1(nil), value.Members...)
	return value
}

func cloneAppACLR2ReceiptCatalogSnapshot(value AppACLR2ReceiptCatalogSnapshotV1) AppACLR2ReceiptCatalogSnapshotV1 {
	value.Table.Columns = append([]AppACLR2ReceiptTableColumnCatalogV1(nil), value.Table.Columns...)
	value.Table.ColumnACLs = append([]AppACLR2ReceiptTableColumnACLCatalogV1(nil), value.Table.ColumnACLs...)
	value.Table.Inheritance = append([]AppACLR2ReceiptTableInheritanceCatalogV1(nil), value.Table.Inheritance...)
	value.Table.Constraints = append([]AppACLR2ReceiptTableConstraintCatalogV1(nil), value.Table.Constraints...)
	value.ReservedObjects = append([]AppACLR2ReservedCatalogObjectV1(nil), value.ReservedObjects...)
	value.ACL = appACLR2CloneControlACL(value.ACL)
	value.Helpers = append([]AppACLR2ReceiptHelperCatalogV1(nil), value.Helpers...)
	for index := range value.Helpers {
		value.Helpers[index].Config = append([]string(nil), value.Helpers[index].Config...)
	}
	return value
}

func appACLR2CloneControlACL(value AppACLControlACLBodyR2V1) AppACLControlACLBodyR2V1 {
	value.Objects = append([]AppACLControlObjectR2V1(nil), value.Objects...)
	for index := range value.Objects {
		value.Objects[index].ExplicitGrants = append([]AppACLControlGrantR2V1(nil), value.Objects[index].ExplicitGrants...)
	}
	value.Triggers = append([]AppACLControlTriggerR2V1(nil), value.Triggers...)
	value.DefaultACLAssertions = append([]AppACLDefaultACLAssertionR2V1(nil), value.DefaultACLAssertions...)
	return value
}

type fakeAppACLR2CatalogTx struct{ pgx.Tx }

type recordingAppACLR2PGCryptoMemberCatalogTx struct {
	pgx.Tx
	query string
}

func (tx *recordingAppACLR2PGCryptoMemberCatalogTx) Query(_ context.Context, query string, _ ...any) (pgx.Rows, error) {
	tx.query = query
	return nil, pgx.ErrNoRows
}

type scriptedAppACLR2ReceiptQuery struct {
	checkArgs func([]any) error
	rows      [][]any
}

type scriptedAppACLR2ReceiptQueryRow struct {
	checkArgs func([]any) error
	values    []any
}

type scriptedAppACLR2ReceiptTx struct {
	pgx.Tx
	queries               []scriptedAppACLR2ReceiptQuery
	queryRows             []scriptedAppACLR2ReceiptQueryRow
	columnACLRows         [][]any
	inheritanceRows       [][]any
	columnACLQueryCount   int
	inheritanceQueryCount int
	queryTexts            []string
	queryRowTexts         []string
}

func (tx *scriptedAppACLR2ReceiptTx) Query(_ context.Context, queryText string, args ...any) (pgx.Rows, error) {
	tx.queryTexts = append(tx.queryTexts, queryText)
	normalizedQuery := strings.ToLower(queryText)
	switch {
	case strings.Contains(normalizedQuery, "attribute.attacl"):
		if err := assertScriptedAppACLR2ReceiptRelationQueryArguments(args); err != nil {
			return nil, err
		}
		tx.columnACLQueryCount++
		return &scriptedAppACLR2ReceiptRows{rows: tx.columnACLRows}, nil
	case strings.Contains(normalizedQuery, "pg_catalog.pg_inherits"):
		if err := assertScriptedAppACLR2ReceiptRelationQueryArguments(args); err != nil {
			return nil, err
		}
		tx.inheritanceQueryCount++
		return &scriptedAppACLR2ReceiptRows{rows: tx.inheritanceRows}, nil
	}
	if len(tx.queries) == 0 {
		return nil, fmt.Errorf("unexpected APP ACL R2 receipt query")
	}
	query := tx.queries[0]
	tx.queries = tx.queries[1:]
	if query.checkArgs != nil {
		if err := query.checkArgs(args); err != nil {
			return nil, err
		}
	}
	return &scriptedAppACLR2ReceiptRows{rows: query.rows}, nil
}

func assertScriptedAppACLR2ReceiptRelationQueryArguments(args []any) error {
	if len(args) != 1 {
		return fmt.Errorf("receipt relation query arguments = %d, want 1", len(args))
	}
	if relationOID, ok := args[0].(int64); !ok || relationOID != 1003 {
		return fmt.Errorf("receipt relation query argument = %#v, want receipt OID 1003", args[0])
	}
	return nil
}

func (tx *scriptedAppACLR2ReceiptTx) QueryRow(_ context.Context, queryText string, args ...any) pgx.Row {
	tx.queryRowTexts = append(tx.queryRowTexts, queryText)
	if len(tx.queryRows) == 0 {
		return scriptedAppACLR2ReceiptRow{err: fmt.Errorf("unexpected APP ACL R2 receipt query row")}
	}
	query := tx.queryRows[0]
	tx.queryRows = tx.queryRows[1:]
	if query.checkArgs != nil {
		if err := query.checkArgs(args); err != nil {
			return scriptedAppACLR2ReceiptRow{err: err}
		}
	}
	return scriptedAppACLR2ReceiptRow{values: query.values}
}

type scriptedAppACLR2ReceiptRow struct {
	values []any
	err    error
}

func (row scriptedAppACLR2ReceiptRow) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	return scanScriptedAppACLR2ReceiptValues(destinations, row.values)
}

type scriptedAppACLR2ReceiptRows struct {
	rows  [][]any
	index int
}

func (rows *scriptedAppACLR2ReceiptRows) Close()                                       {}
func (rows *scriptedAppACLR2ReceiptRows) Err() error                                   { return nil }
func (rows *scriptedAppACLR2ReceiptRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (rows *scriptedAppACLR2ReceiptRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (rows *scriptedAppACLR2ReceiptRows) Values() ([]any, error)                       { return nil, nil }
func (rows *scriptedAppACLR2ReceiptRows) RawValues() [][]byte                          { return nil }
func (rows *scriptedAppACLR2ReceiptRows) Conn() *pgx.Conn                              { return nil }

func (rows *scriptedAppACLR2ReceiptRows) Next() bool {
	if rows.index >= len(rows.rows) {
		return false
	}
	rows.index++
	return true
}

func (rows *scriptedAppACLR2ReceiptRows) Scan(destinations ...any) error {
	if rows.index == 0 || rows.index > len(rows.rows) {
		return fmt.Errorf("scan APP ACL R2 receipt row outside iteration")
	}
	return scanScriptedAppACLR2ReceiptValues(destinations, rows.rows[rows.index-1])
}

func scanScriptedAppACLR2ReceiptValues(destinations []any, values []any) error {
	if len(destinations) != len(values) {
		return fmt.Errorf("scan destination count = %d, want %d", len(destinations), len(values))
	}
	for index, value := range values {
		switch destination := destinations[index].(type) {
		case *int:
			got, ok := value.(int)
			if !ok {
				return fmt.Errorf("value %d has type %T, want int", index, value)
			}
			*destination = got
		case *bool:
			got, ok := value.(bool)
			if !ok {
				return fmt.Errorf("value %d has type %T, want bool", index, value)
			}
			*destination = got
		case *int64:
			got, ok := value.(int64)
			if !ok {
				return fmt.Errorf("value %d has type %T, want int64", index, value)
			}
			*destination = got
		case *float64:
			got, ok := value.(float64)
			if !ok {
				return fmt.Errorf("value %d has type %T, want float64", index, value)
			}
			*destination = got
		case *string:
			got, ok := value.(string)
			if !ok {
				return fmt.Errorf("value %d has type %T, want string", index, value)
			}
			*destination = got
		case *[]string:
			got, ok := value.([]string)
			if !ok {
				return fmt.Errorf("value %d has type %T, want []string", index, value)
			}
			*destination = append((*destination)[:0], got...)
		default:
			return fmt.Errorf("unsupported scan destination %d of type %T", index, destination)
		}
	}
	return nil
}
