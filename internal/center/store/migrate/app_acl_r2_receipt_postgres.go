package migrate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

const (
	appACLR2ReceiptTriggerTypeBeforeUpdateDeleteTruncateStatement int64 = 58
	appACLR2ReceiptTriggerDefinitionPG16                                = "CREATE TRIGGER app_acl_r2_bootstrap_receipt_immutable " +
		"BEFORE DELETE OR UPDATE OR TRUNCATE ON public.app_acl_r2_bootstrap_receipt " +
		"FOR EACH STATEMENT EXECUTE FUNCTION record_platform_internal.app_acl_r2_reject_bootstrap_receipt_mutation()"
)

// AppACLR2CatalogRoleStateV1 records the exact transition-role attributes read
// from pg_authid/pg_roles in the bootstrap transaction.
type AppACLR2CatalogRoleStateV1 struct {
	ControlRole              AppACLControlRoleR2
	Name                     string
	OID                      uint32
	Login                    bool
	Inherit                  bool
	Superuser                bool
	CreateDatabase           bool
	CreateRole               bool
	Replication              bool
	BypassRLS                bool
	RecursiveMembershipCount uint16
}

type AppACLR2PGCryptoExtensionCatalogV1 struct {
	Name      string
	OID       uint32
	Schema    string
	SchemaOID uint32
	Version   string
	OwnerName string
	OwnerOID  uint32
}

type AppACLR2PGCryptoMemberCatalogV1 struct {
	AppACLR2ReceiptMemberV1
	ExtensionOID                            uint32
	ExtensionDependencyClass                string
	ExtensionDependencyObjectSubID          uint32
	ExtensionDependencyReferenceObjectSubID uint32
	ExtensionDependencyType                 string
	ExtensionDependencyCount                uint16
	RoutineKind                             string
	ACLIsDefault                            bool
}

type AppACLR2ReceiptHelperCatalogV1 struct {
	Schema               string
	Name                 string
	IdentityArguments    string
	Identity             string
	OwnerOID             uint32
	Kind                 string
	Result               string
	Language             string
	Volatility           string
	Parallel             string
	SecurityDefiner      bool
	Strict               bool
	Leakproof            bool
	ReturnsSet           bool
	Cost                 float64
	Rows                 float64
	SupportFunctionOID   uint32
	VariadicTypeOID      uint32
	ArgumentCount        uint16
	ArgumentDefaultCount uint16
	InputArgumentTypes   string
	AllArgumentTypesNull bool
	ArgumentModesNull    bool
	ArgumentNamesNull    bool
	ArgumentDefaultsNull bool
	TransformTypesNull   bool
	BinaryNull           bool
	SQLBodyNull          bool
	Config               []string
	Definition           string
	Source               string
}

type AppACLR2ReceiptTableColumnCatalogV1 struct {
	Name              string
	Type              string
	NotNull           bool
	DefaultExpression string
}

type AppACLR2ReceiptTableColumnACLCatalogV1 struct {
	ColumnName  string
	Grantee     string
	Privilege   string
	GrantOption bool
}

type AppACLR2ReceiptTableInheritanceCatalogV1 struct {
	ReceiptIsChild  bool
	ReceiptIsParent bool
}

type AppACLR2ReceiptTableConstraintCatalogV1 struct {
	Type         string
	Definition   string
	Validated    bool
	IndexOID     uint32
	IndexPrimary bool
	IndexUnique  bool
	IndexValid   bool
}

type AppACLR2ReceiptTableCatalogV1 struct {
	Schema      string
	Name        string
	OwnerOID    uint32
	Kind        string
	Persistence string
	Columns     []AppACLR2ReceiptTableColumnCatalogV1
	ColumnACLs  []AppACLR2ReceiptTableColumnACLCatalogV1
	Inheritance []AppACLR2ReceiptTableInheritanceCatalogV1
	Constraints []AppACLR2ReceiptTableConstraintCatalogV1
}

type AppACLR2ReservedCatalogObjectV1 struct {
	OID      uint32
	Kind     string
	Schema   string
	Identity string
	Detail   string
}

// AppACLR2BootstrapCatalogSnapshotV1 is the pre-existing local catalog proof
// read before the bootstrap-owned L2 surface is created.
type AppACLR2BootstrapCatalogSnapshotV1 struct {
	ServerVersionNum         uint32
	ServerVersion            string
	DatabaseOID              uint32
	DatabaseName             string
	PostgresSystemIdentifier string
	BootstrapDefaultACLCount uint16
	Domains                  []AppACLDomainR2V1
	Roles                    []AppACLR2CatalogRoleStateV1
	Extension                AppACLR2PGCryptoExtensionCatalogV1
	Members                  []AppACLR2PGCryptoMemberCatalogV1
}

// AppACLR2PostBootstrapCatalogSnapshotV1 is the constrained continuity
// snapshot shared by direct migrator and runtime readers. Its domain is
// persisted continuity evidence; it intentionally has no fresh physical
// system identifier field.
type AppACLR2PostBootstrapCatalogSnapshotV1 struct {
	ServerVersionNum         uint32
	ServerVersion            string
	DatabaseOID              uint32
	DatabaseName             string
	BootstrapDefaultACLCount uint16
	Domains                  []AppACLDomainR2V1
	Roles                    []AppACLR2CatalogRoleStateV1
	Extension                AppACLR2PGCryptoExtensionCatalogV1
	Members                  []AppACLR2PGCryptoMemberCatalogV1
}

// AppACLR2ReceiptCatalogSnapshotV1 is the freshly read bootstrap-owned L2
// surface after its identifier-dependent grants have been applied.
type AppACLR2ReceiptCatalogSnapshotV1 struct {
	Table           AppACLR2ReceiptTableCatalogV1
	ReservedObjects []AppACLR2ReservedCatalogObjectV1
	ACL             AppACLControlACLBodyR2V1
	Helpers         []AppACLR2ReceiptHelperCatalogV1
}

type appACLR2CatalogReadDependencies struct {
	readSnapshot func(context.Context, pgx.Tx, FrozenAppACLR1StateV1) (AppACLR2BootstrapCatalogSnapshotV1, error)
}

type appACLR2PostBootstrapCatalogReadDependencies struct {
	readSnapshot func(context.Context, pgx.Tx, FrozenAppACLR1StateV1) (AppACLR2PostBootstrapCatalogSnapshotV1, error)
}

// ReadAppACLR2BootstrapCatalogSnapshotInTx reads the bootstrap actor, domain,
// role, server, and pgcrypto member facts through the caller's transaction.
func ReadAppACLR2BootstrapCatalogSnapshotInTx(
	ctx context.Context,
	tx pgx.Tx,
	state FrozenAppACLR1StateV1,
) (AppACLR2BootstrapCatalogSnapshotV1, error) {
	return readAppACLR2BootstrapCatalogSnapshotInTxWithDependencies(ctx, tx, state, appACLR2CatalogReadDependencies{
		readSnapshot: readAppACLR2BootstrapCatalogSnapshotPostgres,
	})
}

// ReadAppACLR2PostBootstrapCatalogSnapshotInTx reads fresh catalog facts for
// post-bootstrap continuity. It never queries pg_control_system() and cannot
// present persisted system-identifier bytes as newly observed physical proof.
func ReadAppACLR2PostBootstrapCatalogSnapshotInTx(
	ctx context.Context,
	tx pgx.Tx,
	state FrozenAppACLR1StateV1,
) (AppACLR2PostBootstrapCatalogSnapshotV1, error) {
	return readAppACLR2PostBootstrapCatalogSnapshotInTxWithDependencies(ctx, tx, state, appACLR2PostBootstrapCatalogReadDependencies{
		readSnapshot: readAppACLR2PostBootstrapCatalogSnapshotPostgres,
	})
}

func readAppACLR2BootstrapCatalogSnapshotInTxWithDependencies(
	ctx context.Context,
	tx pgx.Tx,
	state FrozenAppACLR1StateV1,
	dependencies appACLR2CatalogReadDependencies,
) (AppACLR2BootstrapCatalogSnapshotV1, error) {
	if tx == nil {
		return AppACLR2BootstrapCatalogSnapshotV1{}, fmt.Errorf("APP ACL R2 catalog reader has no transaction")
	}
	if dependencies.readSnapshot == nil {
		return AppACLR2BootstrapCatalogSnapshotV1{}, fmt.Errorf("APP ACL R2 catalog reader dependency is missing")
	}
	return dependencies.readSnapshot(ctx, tx, state)
}

func readAppACLR2PostBootstrapCatalogSnapshotInTxWithDependencies(
	ctx context.Context,
	tx pgx.Tx,
	state FrozenAppACLR1StateV1,
	dependencies appACLR2PostBootstrapCatalogReadDependencies,
) (AppACLR2PostBootstrapCatalogSnapshotV1, error) {
	if tx == nil {
		return AppACLR2PostBootstrapCatalogSnapshotV1{}, fmt.Errorf("APP ACL R2 post-bootstrap catalog reader has no transaction")
	}
	if dependencies.readSnapshot == nil {
		return AppACLR2PostBootstrapCatalogSnapshotV1{}, fmt.Errorf("APP ACL R2 post-bootstrap catalog reader dependency is missing")
	}
	return dependencies.readSnapshot(ctx, tx, state)
}

// CompileAppACLR2BootstrapReceiptFromCatalogV1 converts a fresh exact catalog
// snapshot into the immutable receipt value. It never accepts caller-supplied
// source, privilege, domain, or L2 bytes without revalidating them.
func CompileAppACLR2BootstrapReceiptFromCatalogV1(
	snapshot AppACLR2BootstrapCatalogSnapshotV1,
	surface AppACLR2ReceiptCatalogSnapshotV1,
	frozen FrozenAppACLR1StateV1,
) (AppACLR2BootstrapReceiptV1, error) {
	if _, _, err := validateAppACLR2BootstrapCatalog(snapshot, frozen); err != nil {
		return AppACLR2BootstrapReceiptV1{}, err
	}
	return compileAppACLR2PostBootstrapReceiptFromCatalogV1(
		appACLR2PostBootstrapCatalogSnapshotFromBootstrap(snapshot),
		surface,
		frozen,
	)
}

func compileAppACLR2PostBootstrapReceiptFromCatalogV1(
	snapshot AppACLR2PostBootstrapCatalogSnapshotV1,
	surface AppACLR2ReceiptCatalogSnapshotV1,
	frozen FrozenAppACLR1StateV1,
) (AppACLR2BootstrapReceiptV1, error) {
	if err := validateFrozenAppACLR1StateForReceipt(frozen); err != nil {
		return AppACLR2BootstrapReceiptV1{}, err
	}
	domain, roles, err := validateAppACLR2PostBootstrapCatalog(snapshot, frozen)
	if err != nil {
		return AppACLR2BootstrapReceiptV1{}, err
	}
	if err := validateAppACLR2ReceiptSurfaceCatalog(surface); err != nil {
		return AppACLR2BootstrapReceiptV1{}, err
	}

	domainBody, err := CanonicalAppACLDomainR2BodyV1(domain)
	if err != nil {
		return AppACLR2BootstrapReceiptV1{}, fmt.Errorf("encode APP ACL R2 domain catalog evidence: %w", err)
	}
	l2ACLBody, err := CanonicalAppACLL2ACLBodyR2V1(surface.ACL)
	if err != nil {
		return AppACLR2BootstrapReceiptV1{}, fmt.Errorf("encode APP ACL R2 L2 ACL catalog evidence: %w", err)
	}
	r2SourceBody, err := CompileAppACLSourceSetR2V1()
	if err != nil {
		return AppACLR2BootstrapReceiptV1{}, fmt.Errorf("compile APP ACL R2 source set: %w", err)
	}
	r2PrivilegeBody, err := CompileAppACLPrivilegeSetR2V1(domain.DatabaseName, []AppACLRoleBindingR2V1{
		{Subject: AppACLSubjectCenterRuntimeR2, CatalogRole: roles[AppACLControlRoleCenterRuntimeR2].Name},
		{Subject: AppACLSubjectDirectMigratorR2, CatalogRole: roles[AppACLControlRoleDirectMigratorR2].Name},
		{Subject: AppACLSubjectPlatformAdminR2, CatalogRole: roles[AppACLControlRolePlatformAdminR2].Name},
	})
	if err != nil {
		return AppACLR2BootstrapReceiptV1{}, fmt.Errorf("compile APP ACL R2 privilege set: %w", err)
	}
	sourceEvidence, err := ReadAppACLR2SourceEvidenceV1()
	if err != nil {
		return AppACLR2BootstrapReceiptV1{}, err
	}
	identityDigest, err := appACLR2DigestFromHex(appACLR2PGCryptoIdentitySetDigestHex)
	if err != nil {
		return AppACLR2BootstrapReceiptV1{}, fmt.Errorf("decode fixed pgcrypto identity-set digest: %w", err)
	}

	receiptRoles := make([]AppACLR2ReceiptRoleV1, len(snapshot.Roles))
	for index, role := range snapshot.Roles {
		receiptRoles[index] = AppACLR2ReceiptRoleV1{
			ControlRole:              role.ControlRole,
			Name:                     role.Name,
			OID:                      role.OID,
			Login:                    role.Login,
			Inherit:                  role.Inherit,
			Superuser:                role.Superuser,
			RecursiveMembershipCount: role.RecursiveMembershipCount,
		}
	}
	members := make([]AppACLR2ReceiptMemberV1, len(snapshot.Members))
	for index := range snapshot.Members {
		members[index] = snapshot.Members[index].AppACLR2ReceiptMemberV1
	}
	helpers := make([]AppACLR2ReceiptHelperFunctionV1, len(surface.Helpers))
	for index, helper := range surface.Helpers {
		helpers[index] = AppACLR2ReceiptHelperFunctionV1{Schema: helper.Schema, Identity: helper.Identity, OwnerOID: helper.OwnerOID}
	}
	receipt := AppACLR2BootstrapReceiptV1{
		R1SourceBody:             append([]byte(nil), frozen.SourceSetBody...),
		R1SourceDigest:           frozen.SourceSetDigest,
		R1PrivilegeBody:          append([]byte(nil), frozen.PrivilegeSetBody...),
		R1PrivilegeDigest:        frozen.PrivilegeSetDigest,
		R2SourceBody:             r2SourceBody,
		R2SourceDigest:           sha256.Sum256(r2SourceBody),
		R2PrivilegeBody:          r2PrivilegeBody,
		R2PrivilegeDigest:        sha256.Sum256(r2PrivilegeBody),
		R20052FullFileSHA256:     sourceEvidence.FullFileSHA256,
		R2BootstrapSectionSHA256: sourceEvidence.BootstrapSectionSHA256,
		R2FinalizeSectionSHA256:  sourceEvidence.FinalizeSectionSHA256,
		DomainBody:               domainBody,
		DomainDigest:             sha256.Sum256(domainBody),
		Roles:                    receiptRoles,
		ServerVersionNum:         snapshot.ServerVersionNum,
		ServerVersion:            snapshot.ServerVersion,
		ExtensionName:            snapshot.Extension.Name,
		ExtensionOID:             snapshot.Extension.OID,
		ExtensionSchema:          snapshot.Extension.Schema,
		ExtensionVersion:         snapshot.Extension.Version,
		ExtensionOwnerName:       snapshot.Extension.OwnerName,
		ExtensionOwnerOID:        snapshot.Extension.OwnerOID,
		IdentitySetSHA256:        identityDigest,
		Members:                  members,
		ReceiptSchema:            "public",
		ReceiptTable:             "app_acl_r2_bootstrap_receipt",
		ReceiptOwnerOID:          10,
		Singleton:                true,
		HelperFunctions:          helpers,
		ReceiptTriggers:          append([]AppACLControlTriggerR2V1(nil), surface.ACL.Triggers...),
		L2ACLBody:                l2ACLBody,
		L2ACLDigest:              sha256.Sum256(l2ACLBody),
	}
	if err := validateAppACLR2BootstrapReceipt(receipt); err != nil {
		return AppACLR2BootstrapReceiptV1{}, fmt.Errorf("validate compiled APP ACL R2 receipt: %w", err)
	}
	return receipt, nil
}

// VerifyAppACLR2BootstrapReceiptCatalogV1 compares persisted receipt evidence
// with a fresh compilation from the same locked catalog snapshot.
func VerifyAppACLR2BootstrapReceiptCatalogV1(
	receipt AppACLR2BootstrapReceiptV1,
	snapshot AppACLR2BootstrapCatalogSnapshotV1,
	surface AppACLR2ReceiptCatalogSnapshotV1,
	frozen FrozenAppACLR1StateV1,
) error {
	expected, err := CompileAppACLR2BootstrapReceiptFromCatalogV1(snapshot, surface, frozen)
	if err != nil {
		return fmt.Errorf("compile fresh APP ACL R2 receipt catalog evidence: %w", err)
	}
	actualBody, err := CanonicalAppACLR2BootstrapReceiptBodyV1(receipt)
	if err != nil {
		return fmt.Errorf("validate persisted APP ACL R2 receipt: %w", err)
	}
	expectedBody, err := CanonicalAppACLR2BootstrapReceiptBodyV1(expected)
	if err != nil {
		return fmt.Errorf("encode expected APP ACL R2 receipt: %w", err)
	}
	if !bytes.Equal(actualBody, expectedBody) {
		return fmt.Errorf("APP ACL R2 receipt does not match fresh catalog evidence")
	}
	return nil
}

// VerifyAppACLR2PostBootstrapReceiptCatalogV1 proves receipt/domain
// continuity against fresh non-physical catalog facts. Equal system identifiers
// here are persisted receipt/domain evidence only, never a fresh physical
// system assertion.
func VerifyAppACLR2PostBootstrapReceiptCatalogV1(
	receipt AppACLR2BootstrapReceiptV1,
	snapshot AppACLR2PostBootstrapCatalogSnapshotV1,
	surface AppACLR2ReceiptCatalogSnapshotV1,
	frozen FrozenAppACLR1StateV1,
) error {
	expected, err := compileAppACLR2PostBootstrapReceiptFromCatalogV1(snapshot, surface, frozen)
	if err != nil {
		return fmt.Errorf("compile constrained APP ACL R2 receipt continuity evidence: %w", err)
	}
	actualBody, err := CanonicalAppACLR2BootstrapReceiptBodyV1(receipt)
	if err != nil {
		return fmt.Errorf("validate persisted APP ACL R2 receipt: %w", err)
	}
	expectedBody, err := CanonicalAppACLR2BootstrapReceiptBodyV1(expected)
	if err != nil {
		return fmt.Errorf("encode constrained APP ACL R2 receipt continuity evidence: %w", err)
	}
	if !bytes.Equal(actualBody, expectedBody) {
		return fmt.Errorf("APP ACL R2 receipt does not match constrained post-bootstrap continuity evidence")
	}
	return nil
}

func appACLR2PostBootstrapCatalogSnapshotFromBootstrap(
	snapshot AppACLR2BootstrapCatalogSnapshotV1,
) AppACLR2PostBootstrapCatalogSnapshotV1 {
	return AppACLR2PostBootstrapCatalogSnapshotV1{
		ServerVersionNum:         snapshot.ServerVersionNum,
		ServerVersion:            snapshot.ServerVersion,
		DatabaseOID:              snapshot.DatabaseOID,
		DatabaseName:             snapshot.DatabaseName,
		BootstrapDefaultACLCount: snapshot.BootstrapDefaultACLCount,
		Domains:                  append([]AppACLDomainR2V1(nil), snapshot.Domains...),
		Roles:                    append([]AppACLR2CatalogRoleStateV1(nil), snapshot.Roles...),
		Extension:                snapshot.Extension,
		Members:                  append([]AppACLR2PGCryptoMemberCatalogV1(nil), snapshot.Members...),
	}
}

func validateFrozenAppACLR1StateForReceipt(state FrozenAppACLR1StateV1) error {
	if !validCatalogRoleName(state.DatabaseName) || state.ManifestRevision != 1 || state.ManifestDigest == [32]byte{} {
		return fmt.Errorf("frozen R1 state identity is invalid")
	}
	expectedSourceBody, err := CanonicalMigrationSetBodyV1(appACLR1MigrationSourceContract[:])
	if err != nil {
		return fmt.Errorf("build frozen R1 source contract: %w", err)
	}
	if !bytes.Equal(state.SourceSetBody, expectedSourceBody) || state.SourceSetDigest != sha256.Sum256(state.SourceSetBody) {
		return fmt.Errorf("frozen R1 source evidence does not match the 52-source contract")
	}
	privilegeSet, err := ParseCanonicalPrivilegeSetBodyV1(state.PrivilegeSetBody)
	if err != nil || len(privilegeSet.Privileges) != appACLEffectiveCatalogR1PrivilegeCount {
		return fmt.Errorf("frozen R1 privilege evidence is invalid: %w", err)
	}
	wantPrivilegeBody, err := CompileAppACLPrivilegeSetR1(state.DatabaseName, []AppACLRoleBinding{
		{Subject: AppACLSubjectCenterRuntime, CatalogRole: state.CenterRuntimeRole},
		{Subject: AppACLSubjectPlatformAdmin, CatalogRole: state.PlatformAdminRole},
	})
	if err != nil {
		return fmt.Errorf("compile frozen R1 privilege evidence: %w", err)
	}
	if !bytes.Equal(state.PrivilegeSetBody, wantPrivilegeBody) || state.PrivilegeSetDigest != sha256.Sum256(state.PrivilegeSetBody) {
		return fmt.Errorf("frozen R1 privilege evidence does not match the 204-tuple contract")
	}
	if !validCatalogRoleName(state.DirectMigratorRole) || state.DirectMigratorRole == state.CenterRuntimeRole || state.DirectMigratorRole == state.PlatformAdminRole {
		return fmt.Errorf("frozen R1 direct migrator binding is invalid")
	}
	return nil
}

func validateAppACLR2BootstrapCatalog(
	snapshot AppACLR2BootstrapCatalogSnapshotV1,
	frozen FrozenAppACLR1StateV1,
) (AppACLDomainR2V1, map[AppACLControlRoleR2]AppACLR2CatalogRoleStateV1, error) {
	domain, roles, err := validateAppACLR2PostBootstrapCatalog(
		appACLR2PostBootstrapCatalogSnapshotFromBootstrap(snapshot),
		frozen,
	)
	if err != nil {
		return AppACLDomainR2V1{}, nil, err
	}
	if domain.PostgresSystemIdentifier != snapshot.PostgresSystemIdentifier {
		return AppACLDomainR2V1{}, nil, fmt.Errorf("APP ACL R2 bootstrap live system identifier does not match the immutable domain")
	}
	return domain, roles, nil
}

func validateAppACLR2PostBootstrapCatalog(
	snapshot AppACLR2PostBootstrapCatalogSnapshotV1,
	frozen FrozenAppACLR1StateV1,
) (AppACLDomainR2V1, map[AppACLControlRoleR2]AppACLR2CatalogRoleStateV1, error) {
	if !appACLR2AllowedServerVersion(snapshot.ServerVersionNum) {
		return AppACLDomainR2V1{}, nil, fmt.Errorf("APP ACL R2 server_version_num %d is not allowed", snapshot.ServerVersionNum)
	}
	if !validAppACLR2Text(snapshot.ServerVersion, 1, 32) {
		return AppACLDomainR2V1{}, nil, fmt.Errorf("APP ACL R2 server version is invalid")
	}
	if len(snapshot.Roles) != 4 {
		return AppACLDomainR2V1{}, nil, fmt.Errorf("APP ACL R2 catalog has %d transition roles, want 4", len(snapshot.Roles))
	}
	roles := make(map[AppACLControlRoleR2]AppACLR2CatalogRoleStateV1, len(snapshot.Roles))
	seenNames := make(map[string]struct{}, len(snapshot.Roles))
	seenOIDs := make(map[uint32]struct{}, len(snapshot.Roles))
	for index, role := range snapshot.Roles {
		wantTag := AppACLControlRoleR2(index + 1)
		if role.ControlRole != wantTag || !validAppACLR2RoleName(role.Name) || role.OID == 0 {
			return AppACLDomainR2V1{}, nil, fmt.Errorf("APP ACL R2 catalog role %d has invalid identity", index)
		}
		if role.RecursiveMembershipCount != 0 {
			return AppACLDomainR2V1{}, nil, fmt.Errorf("APP ACL R2 role %q has forbidden membership", role.Name)
		}
		if _, exists := seenNames[role.Name]; exists {
			return AppACLDomainR2V1{}, nil, fmt.Errorf("APP ACL R2 catalog role name %q is duplicated", role.Name)
		}
		if _, exists := seenOIDs[role.OID]; exists {
			return AppACLDomainR2V1{}, nil, fmt.Errorf("APP ACL R2 catalog role OID %d is duplicated", role.OID)
		}
		seenNames[role.Name] = struct{}{}
		seenOIDs[role.OID] = struct{}{}
		roles[role.ControlRole] = role
	}
	bootstrap := roles[AppACLControlRoleBootstrapSuperuserR2]
	if bootstrap.OID != 10 {
		return AppACLDomainR2V1{}, nil, fmt.Errorf("APP ACL R2 bootstrap role must have OID 10")
	}
	if !bootstrap.Login || !bootstrap.Inherit || !bootstrap.Superuser {
		return AppACLDomainR2V1{}, nil, fmt.Errorf("APP ACL R2 bootstrap role must be LOGIN INHERIT superuser")
	}
	wantNames := map[AppACLControlRoleR2]string{
		AppACLControlRoleDirectMigratorR2: frozen.DirectMigratorRole,
		AppACLControlRoleCenterRuntimeR2:  frozen.CenterRuntimeRole,
		AppACLControlRolePlatformAdminR2:  frozen.PlatformAdminRole,
	}
	for tag, name := range wantNames {
		role := roles[tag]
		if role.Name != name {
			return AppACLDomainR2V1{}, nil, fmt.Errorf("APP ACL R2 catalog role %d does not match frozen R1 binding", tag)
		}
		if !role.Login || role.Inherit || role.Superuser || role.CreateDatabase || role.CreateRole || role.Replication || role.BypassRLS {
			return AppACLDomainR2V1{}, nil, fmt.Errorf("APP ACL R2 role %q is not a constrained direct role", role.Name)
		}
	}
	if snapshot.BootstrapDefaultACLCount != 0 {
		return AppACLDomainR2V1{}, nil, fmt.Errorf("APP ACL R2 bootstrap owner has %d forbidden default ACL rows", snapshot.BootstrapDefaultACLCount)
	}
	if len(snapshot.Domains) != 1 {
		return AppACLDomainR2V1{}, nil, fmt.Errorf("APP ACL R2 catalog has %d application domain rows, want 1", len(snapshot.Domains))
	}
	domain := snapshot.Domains[0]
	if err := validateAppACLR2Domain(domain); err != nil {
		return AppACLDomainR2V1{}, nil, fmt.Errorf("validate APP ACL R2 catalog domain: %w", err)
	}
	if domain.DatabaseOID != snapshot.DatabaseOID || domain.DatabaseName != snapshot.DatabaseName || domain.DatabaseName != frozen.DatabaseName {
		return AppACLDomainR2V1{}, nil, fmt.Errorf("APP ACL R2 catalog domain does not match the local database identity")
	}
	extension := snapshot.Extension
	if extension.Name != "pgcrypto" || extension.OID == 0 || extension.Schema != "record_platform_internal" || extension.SchemaOID == 0 || extension.Version != "1.3" {
		return AppACLDomainR2V1{}, nil, fmt.Errorf("APP ACL R2 pgcrypto extension catalog facts are invalid")
	}
	direct := roles[AppACLControlRoleDirectMigratorR2]
	if extension.OwnerName != direct.Name || extension.OwnerOID != direct.OID {
		return AppACLDomainR2V1{}, nil, fmt.Errorf("APP ACL R2 pgcrypto extension owner does not match direct migrator")
	}
	if err := validateAppACLR2PGCryptoCatalogMembers(snapshot.Members, extension, bootstrap); err != nil {
		return AppACLDomainR2V1{}, nil, err
	}
	return domain, roles, nil
}

func validateAppACLR2PGCryptoCatalogMembers(
	members []AppACLR2PGCryptoMemberCatalogV1,
	extension AppACLR2PGCryptoExtensionCatalogV1,
	bootstrap AppACLR2CatalogRoleStateV1,
) error {
	if len(members) != len(appACLR2PGCryptoIdentityContract) {
		return fmt.Errorf("APP ACL R2 pgcrypto catalog has %d members, want 36", len(members))
	}
	identities := make([]string, 0, len(members))
	for index, member := range members {
		if member.OID == 0 || index > 0 && members[index-1].OID >= member.OID {
			return fmt.Errorf("APP ACL R2 pgcrypto member OIDs are not strictly ordered")
		}
		if member.ExtensionDependencyClass != "pg_catalog.pg_proc" || member.ExtensionDependencyObjectSubID != 0 || member.ExtensionDependencyReferenceObjectSubID != 0 {
			return fmt.Errorf("APP ACL R2 pgcrypto member %d extension dependency class is invalid", index)
		}
		if member.RoutineKind != "f" {
			return fmt.Errorf("APP ACL R2 pgcrypto member %d routine kind %q is invalid", index, member.RoutineKind)
		}
		if member.Schema != extension.Schema || member.ExtensionOID != extension.OID {
			return fmt.Errorf("APP ACL R2 pgcrypto member %d has invalid member namespace or extension OID", index)
		}
		if member.OwnerName != bootstrap.Name || member.OwnerOID != 10 || member.OwnerOID != bootstrap.OID {
			return fmt.Errorf("APP ACL R2 pgcrypto member %d owner does not match bootstrap OID 10", index)
		}
		if member.ExtensionDependencyCount != 1 || member.ExtensionDependencyType != "e" {
			return fmt.Errorf("APP ACL R2 pgcrypto member %d extension dependency is invalid", index)
		}
		if !member.ACLIsDefault {
			return fmt.Errorf("APP ACL R2 pgcrypto member %d ACL is not the default catalog baseline", index)
		}
		identities = append(identities, member.Schema+"."+member.Name+"|"+member.IdentityArguments)
	}
	sort.Strings(identities)
	wantIdentities := append([]string(nil), appACLR2PGCryptoIdentityContract[:]...)
	sort.Strings(wantIdentities)
	for index := range identities {
		if identities[index] != wantIdentities[index] {
			return fmt.Errorf("APP ACL R2 pgcrypto identity set does not match fixed member %d", index)
		}
	}
	wantDigest, err := appACLR2DigestFromHex(appACLR2PGCryptoIdentitySetDigestHex)
	if err != nil {
		return err
	}
	if appACLR2PGCryptoIdentitySetDigest(identities) != wantDigest {
		return fmt.Errorf("APP ACL R2 pgcrypto identity set does not match fixed digest")
	}
	return nil
}

func validateAppACLR2ReceiptSurfaceCatalog(surface AppACLR2ReceiptCatalogSnapshotV1) error {
	primaryKeyIndexOID, err := validateAppACLR2ReservedCatalogObjects(surface.ReservedObjects)
	if err != nil {
		return err
	}
	if err := validateAppACLR2ReceiptTableCatalog(surface.Table, primaryKeyIndexOID); err != nil {
		return err
	}
	if _, err := CanonicalAppACLL2ACLBodyR2V1(surface.ACL); err != nil {
		return fmt.Errorf("APP ACL R2 L2 ACL catalog drift: %w", err)
	}
	want := []AppACLR2ReceiptHelperCatalogV1{
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
	if len(surface.Helpers) != len(want) {
		return fmt.Errorf("APP ACL R2 receipt helper catalog has %d rows, want 2", len(surface.Helpers))
	}
	for index := range want {
		got := surface.Helpers[index]
		if got.SecurityDefiner {
			return fmt.Errorf("APP ACL R2 receipt helper %d has security definer drift", index)
		}
		if !appACLR2ReceiptHelperCatalogEqual(got, want[index]) {
			return fmt.Errorf("APP ACL R2 receipt helper %d has exact catalog definition drift", index)
		}
	}
	return nil
}

func appACLR2ReceiptHelperCatalogEqual(left, right AppACLR2ReceiptHelperCatalogV1) bool {
	if left.Schema != right.Schema || left.Name != right.Name || left.IdentityArguments != right.IdentityArguments || left.Identity != right.Identity ||
		left.OwnerOID != right.OwnerOID || left.Kind != right.Kind || left.Result != right.Result || left.Language != right.Language ||
		left.Volatility != right.Volatility || left.Parallel != right.Parallel || left.SecurityDefiner != right.SecurityDefiner ||
		left.Strict != right.Strict || left.Leakproof != right.Leakproof || left.ReturnsSet != right.ReturnsSet || left.Cost != right.Cost ||
		left.Rows != right.Rows || left.SupportFunctionOID != right.SupportFunctionOID || left.VariadicTypeOID != right.VariadicTypeOID ||
		left.ArgumentCount != right.ArgumentCount || left.ArgumentDefaultCount != right.ArgumentDefaultCount || left.InputArgumentTypes != right.InputArgumentTypes ||
		left.AllArgumentTypesNull != right.AllArgumentTypesNull || left.ArgumentModesNull != right.ArgumentModesNull || left.ArgumentNamesNull != right.ArgumentNamesNull ||
		left.ArgumentDefaultsNull != right.ArgumentDefaultsNull || left.TransformTypesNull != right.TransformTypesNull || left.BinaryNull != right.BinaryNull ||
		left.SQLBodyNull != right.SQLBodyNull || left.Definition != right.Definition || left.Source != right.Source || len(left.Config) != len(right.Config) {
		return false
	}
	for index := range left.Config {
		if left.Config[index] != right.Config[index] {
			return false
		}
	}
	return true
}

func validateAppACLR2ReservedCatalogObjects(objects []AppACLR2ReservedCatalogObjectV1) (uint32, error) {
	want := []AppACLR2ReservedCatalogObjectV1{
		{Kind: "function", Schema: "record_platform_internal", Identity: "record_platform_internal.app_acl_r2_assert_bootstrap_receipt_insert(bytea, bytea)", Detail: "f"},
		{Kind: "function", Schema: "record_platform_internal", Identity: "record_platform_internal.app_acl_r2_reject_bootstrap_receipt_mutation()", Detail: "f"},
		{Kind: "relation", Schema: "public", Identity: "app_acl_r2_bootstrap_receipt", Detail: "r"},
		{Kind: "relation", Schema: "public", Identity: "app_acl_r2_bootstrap_receipt_pkey", Detail: "i"},
		{Kind: "trigger", Schema: "public", Identity: "app_acl_r2_bootstrap_receipt.app_acl_r2_bootstrap_receipt_immutable", Detail: "user"},
	}
	if len(objects) != len(want) {
		return 0, fmt.Errorf("APP ACL R2 reserved catalog inventory has %d objects, want %d", len(objects), len(want))
	}
	observed := make(map[string]AppACLR2ReservedCatalogObjectV1, len(objects))
	observedOIDs := make(map[uint32]string, len(objects))
	for _, object := range objects {
		if object.OID == 0 {
			return 0, fmt.Errorf("APP ACL R2 reserved catalog inventory has object without an OID")
		}
		key := object.Kind + "|" + object.Schema + "|" + object.Identity + "|" + object.Detail
		if _, exists := observed[key]; exists {
			return 0, fmt.Errorf("APP ACL R2 reserved catalog inventory has duplicate object %q", key)
		}
		if existing, exists := observedOIDs[object.OID]; exists {
			return 0, fmt.Errorf("APP ACL R2 reserved catalog inventory reuses OID %d for %q and %q", object.OID, existing, key)
		}
		observed[key] = object
		observedOIDs[object.OID] = key
	}
	for _, object := range want {
		key := object.Kind + "|" + object.Schema + "|" + object.Identity + "|" + object.Detail
		if _, exists := observed[key]; !exists {
			return 0, fmt.Errorf("APP ACL R2 reserved catalog inventory is missing expected object %q", key)
		}
	}
	primaryKeyIndex := observed["relation|public|app_acl_r2_bootstrap_receipt_pkey|i"]
	return primaryKeyIndex.OID, nil
}

func validateAppACLR2ReceiptTableCatalog(table AppACLR2ReceiptTableCatalogV1, primaryKeyIndexOID uint32) error {
	if table.Schema != "public" || table.Name != "app_acl_r2_bootstrap_receipt" || table.OwnerOID != 10 || table.Kind != "r" || table.Persistence != "p" {
		return fmt.Errorf("APP ACL R2 receipt table has catalog identity or persistence drift")
	}
	if primaryKeyIndexOID == 0 {
		return fmt.Errorf("APP ACL R2 receipt table primary key index is missing from the reserved catalog inventory")
	}
	for _, inheritance := range table.Inheritance {
		switch {
		case inheritance.ReceiptIsChild && !inheritance.ReceiptIsParent:
			return fmt.Errorf("APP ACL R2 receipt table inherits from a parent")
		case !inheritance.ReceiptIsChild && inheritance.ReceiptIsParent:
			return fmt.Errorf("APP ACL R2 receipt table has an inheritance child")
		default:
			return fmt.Errorf("APP ACL R2 receipt table has invalid inheritance catalog evidence")
		}
	}
	if len(table.ColumnACLs) != 0 {
		columnACL := table.ColumnACLs[0]
		return fmt.Errorf("APP ACL R2 receipt table has column ACL drift on %q for grantee %q", columnACL.ColumnName, columnACL.Grantee)
	}
	wantColumns := []AppACLR2ReceiptTableColumnCatalogV1{
		{Name: "singleton", Type: "boolean", NotNull: true, DefaultExpression: "true"},
		{Name: "receipt_body", Type: "bytea", NotNull: true},
		{Name: "receipt_digest", Type: "bytea", NotNull: true},
	}
	if len(table.Columns) != len(wantColumns) {
		return fmt.Errorf("APP ACL R2 receipt table has %d columns, want %d", len(table.Columns), len(wantColumns))
	}
	for index, want := range wantColumns {
		got := table.Columns[index]
		if got.Name != want.Name || got.Type != want.Type || got.NotNull != want.NotNull || appACLR2NormalizeCatalogDefinition(got.DefaultExpression) != want.DefaultExpression {
			return fmt.Errorf("APP ACL R2 receipt table column %d has shape drift", index)
		}
	}
	if len(table.Constraints) != 5 {
		return fmt.Errorf("APP ACL R2 receipt table has %d constraints, want 5", len(table.Constraints))
	}
	wantConstraints := map[string]bool{
		"primary key":                 false,
		"singleton check":             false,
		"receipt body length check":   false,
		"receipt digest length check": false,
		"receipt digest check":        false,
	}
	for _, constraint := range table.Constraints {
		key, err := appACLR2ReceiptTableConstraintKey(constraint)
		if err != nil {
			return err
		}
		if !constraint.Validated {
			return fmt.Errorf("APP ACL R2 receipt table %s is not validated", key)
		}
		if key == "primary key" {
			if constraint.IndexOID != primaryKeyIndexOID || !constraint.IndexPrimary || !constraint.IndexUnique || !constraint.IndexValid {
				return fmt.Errorf("APP ACL R2 receipt table primary key index has catalog drift")
			}
		} else if constraint.IndexOID != 0 || constraint.IndexPrimary || constraint.IndexUnique || constraint.IndexValid {
			return fmt.Errorf("APP ACL R2 receipt table %s has unexpected index metadata", key)
		}
		if seen, exists := wantConstraints[key]; !exists || seen {
			return fmt.Errorf("APP ACL R2 receipt table has unexpected or duplicate %s", key)
		}
		wantConstraints[key] = true
	}
	for key, seen := range wantConstraints {
		if !seen {
			return fmt.Errorf("APP ACL R2 receipt table is missing %s", key)
		}
	}
	return nil
}

func appACLR2ReceiptTableConstraintKey(constraint AppACLR2ReceiptTableConstraintCatalogV1) (string, error) {
	normalized := appACLR2NormalizeCatalogDefinition(constraint.Definition)
	switch constraint.Type {
	case "p":
		if strings.NewReplacer("(", "", ")", "").Replace(normalized) == "primarykeysingleton" {
			return "primary key", nil
		}
	case "c":
		if !strings.HasPrefix(normalized, "check(") || !strings.HasSuffix(normalized, ")") {
			break
		}
		expression := normalized[len("check(") : len(normalized)-1]
		expression = strings.ReplaceAll(expression, "(", "")
		expression = strings.ReplaceAll(expression, ")", "")
		switch expression {
		case "singleton":
			return "singleton check", nil
		case "octet_lengthreceipt_body>=1andoctet_lengthreceipt_body<=4194304":
			return "receipt body length check", nil
		case "octet_lengthreceipt_digest=32":
			return "receipt digest length check", nil
		case "record_platform_internal.app_acl_r2_assert_bootstrap_receipt_insertreceipt_body,receipt_digest":
			return "receipt digest check", nil
		}
	}
	return "", fmt.Errorf("APP ACL R2 receipt table has unexpected constraint %q", constraint.Definition)
}

func appACLR2NormalizeCatalogDefinition(value string) string {
	value = strings.ToLower(strings.Join(strings.Fields(value), ""))
	return strings.ReplaceAll(value, "pg_catalog.", "")
}

func readAppACLR2BootstrapCatalogSnapshotPostgres(
	ctx context.Context,
	tx pgx.Tx,
	state FrozenAppACLR1StateV1,
) (AppACLR2BootstrapCatalogSnapshotV1, error) {
	postBootstrap, err := readAppACLR2PostBootstrapCatalogSnapshotPostgres(ctx, tx, state)
	if err != nil {
		return AppACLR2BootstrapCatalogSnapshotV1{}, err
	}
	systemIdentifier, err := readAppACLR2BootstrapLiveSystemIdentifierInTx(ctx, tx)
	if err != nil {
		return AppACLR2BootstrapCatalogSnapshotV1{}, err
	}
	return appACLR2BootstrapCatalogSnapshotFromPostBootstrap(postBootstrap, systemIdentifier), nil
}

func readAppACLR2PostBootstrapCatalogSnapshotPostgres(
	ctx context.Context,
	tx pgx.Tx,
	state FrozenAppACLR1StateV1,
) (snapshot AppACLR2PostBootstrapCatalogSnapshotV1, err error) {
	var serverVersion, databaseOID int64
	if err := tx.QueryRow(ctx, `
		select pg_catalog.current_setting('server_version_num')::bigint,
		       pg_catalog.current_setting('server_version')::text,
		       database.oid::bigint,
		       database.datname::text
		from pg_catalog.pg_database database
		where database.datname = pg_catalog.current_database()
	`).Scan(
		&serverVersion,
		&snapshot.ServerVersion,
		&databaseOID,
		&snapshot.DatabaseName,
	); err != nil {
		return AppACLR2PostBootstrapCatalogSnapshotV1{}, fmt.Errorf("read APP ACL R2 post-bootstrap server and database identity: %w", err)
	}
	if snapshot.ServerVersionNum, err = appACLR2CatalogUint32(serverVersion, "server_version_num"); err != nil {
		return AppACLR2PostBootstrapCatalogSnapshotV1{}, err
	}
	if snapshot.DatabaseOID, err = appACLR2CatalogUint32(databaseOID, "database OID"); err != nil {
		return AppACLR2PostBootstrapCatalogSnapshotV1{}, err
	}
	if snapshot.Domains, err = readAppACLR2DomainsInTx(ctx, tx); err != nil {
		return AppACLR2PostBootstrapCatalogSnapshotV1{}, err
	}
	if snapshot.BootstrapDefaultACLCount, err = readAppACLR2BootstrapDefaultACLCountInTx(ctx, tx); err != nil {
		return AppACLR2PostBootstrapCatalogSnapshotV1{}, err
	}
	if snapshot.Roles, err = readAppACLR2TransitionRolesInTx(ctx, tx, state); err != nil {
		return AppACLR2PostBootstrapCatalogSnapshotV1{}, err
	}
	if snapshot.Extension, err = readAppACLR2PGCryptoExtensionInTx(ctx, tx); err != nil {
		return AppACLR2PostBootstrapCatalogSnapshotV1{}, err
	}
	if snapshot.Members, err = readAppACLR2PGCryptoMembersInTx(ctx, tx); err != nil {
		return AppACLR2PostBootstrapCatalogSnapshotV1{}, err
	}
	return snapshot, nil
}

func readAppACLR2BootstrapLiveSystemIdentifierInTx(ctx context.Context, tx pgx.Tx) (string, error) {
	var systemIdentifier string
	if err := tx.QueryRow(ctx, `
		select control.system_identifier::text
		from pg_catalog.pg_control_system() control
	`).Scan(&systemIdentifier); err != nil {
		return "", fmt.Errorf("read APP ACL R2 bootstrap live system identifier: %w", err)
	}
	return systemIdentifier, nil
}

func appACLR2BootstrapCatalogSnapshotFromPostBootstrap(
	snapshot AppACLR2PostBootstrapCatalogSnapshotV1,
	systemIdentifier string,
) AppACLR2BootstrapCatalogSnapshotV1 {
	return AppACLR2BootstrapCatalogSnapshotV1{
		ServerVersionNum:         snapshot.ServerVersionNum,
		ServerVersion:            snapshot.ServerVersion,
		DatabaseOID:              snapshot.DatabaseOID,
		DatabaseName:             snapshot.DatabaseName,
		PostgresSystemIdentifier: systemIdentifier,
		BootstrapDefaultACLCount: snapshot.BootstrapDefaultACLCount,
		Domains:                  append([]AppACLDomainR2V1(nil), snapshot.Domains...),
		Roles:                    append([]AppACLR2CatalogRoleStateV1(nil), snapshot.Roles...),
		Extension:                snapshot.Extension,
		Members:                  append([]AppACLR2PGCryptoMemberCatalogV1(nil), snapshot.Members...),
	}
}

func readAppACLR2DomainsInTx(ctx context.Context, tx pgx.Tx) ([]AppACLDomainR2V1, error) {
	rows, err := tx.Query(ctx, `
		select domain_id,
		       domain_kind,
		       identity_epoch,
		       identity_mode,
		       coalesce(postgres_system_identifier, ''),
		       database_oid::bigint,
		       database_name
		from public.record_platform_domain_identity
		where domain_kind = 'application'
		order by provisioned_at, domain_id
	`)
	if err != nil {
		return nil, fmt.Errorf("read APP ACL R2 application domain identity: %w", err)
	}
	defer rows.Close()
	domains := make([]AppACLDomainR2V1, 0, 1)
	for rows.Next() {
		var domain AppACLDomainR2V1
		var epoch, oid int64
		if err := rows.Scan(&domain.DomainID, &domain.DomainKind, &epoch, &domain.IdentityMode, &domain.PostgresSystemIdentifier, &oid, &domain.DatabaseName); err != nil {
			return nil, fmt.Errorf("scan APP ACL R2 application domain identity: %w", err)
		}
		if epoch < 0 {
			return nil, fmt.Errorf("APP ACL R2 application domain epoch is negative")
		}
		domain.IdentityEpoch = uint64(epoch)
		if domain.DatabaseOID, err = appACLR2CatalogUint32(oid, "domain database OID"); err != nil {
			return nil, err
		}
		domains = append(domains, domain)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate APP ACL R2 application domain identity: %w", err)
	}
	return domains, nil
}

func readAppACLR2BootstrapDefaultACLCountInTx(ctx context.Context, tx pgx.Tx) (uint16, error) {
	var count int64
	if err := tx.QueryRow(ctx, `
		select pg_catalog.count(*)::bigint
		from pg_catalog.pg_default_acl default_acl
		left join pg_catalog.pg_namespace namespace on namespace.oid = default_acl.defaclnamespace
		where default_acl.defaclrole = 10
		  and ((default_acl.defaclobjtype = 'r'
		        and (default_acl.defaclnamespace = 0 or namespace.nspname = 'public'))
		    or (default_acl.defaclobjtype = 'f'
		        and (default_acl.defaclnamespace = 0 or namespace.nspname = 'record_platform_internal')))
	`).Scan(&count); err != nil {
		return 0, fmt.Errorf("read APP ACL R2 bootstrap default ACL catalog: %w", err)
	}
	if count < 0 || count > int64(^uint16(0)) {
		return 0, fmt.Errorf("APP ACL R2 bootstrap default ACL count is outside uint16 bounds")
	}
	return uint16(count), nil
}

func readAppACLR2TransitionRolesInTx(
	ctx context.Context,
	tx pgx.Tx,
	state FrozenAppACLR1StateV1,
) ([]AppACLR2CatalogRoleStateV1, error) {
	names := []string{state.DirectMigratorRole, state.CenterRuntimeRole, state.PlatformAdminRole}
	rows, err := tx.Query(ctx, `
		select role.rolname,
		       role.oid::bigint,
		       role.rolcanlogin,
		       role.rolinherit,
		       role.rolsuper,
		       role.rolcreatedb,
		       role.rolcreaterole,
		       role.rolreplication,
		       role.rolbypassrls
		from pg_catalog.pg_roles role
		where role.oid = 10
		   or role.rolname = any($1::name[])
		order by role.rolname
	`, names)
	if err != nil {
		return nil, fmt.Errorf("read APP ACL R2 transition role attributes: %w", err)
	}
	defer rows.Close()
	byName := make(map[string]AppACLR2CatalogRoleStateV1, len(names))
	for rows.Next() {
		var role AppACLR2CatalogRoleStateV1
		var oid int64
		if err := rows.Scan(&role.Name, &oid, &role.Login, &role.Inherit, &role.Superuser, &role.CreateDatabase, &role.CreateRole, &role.Replication, &role.BypassRLS); err != nil {
			return nil, fmt.Errorf("scan APP ACL R2 transition role attributes: %w", err)
		}
		if role.OID, err = appACLR2CatalogUint32(oid, "transition role OID"); err != nil {
			return nil, err
		}
		byName[role.Name] = role
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate APP ACL R2 transition role attributes: %w", err)
	}
	var bootstrap AppACLR2CatalogRoleStateV1
	bootstrapFound := false
	for _, role := range byName {
		if role.OID == 10 {
			bootstrap = role
			bootstrapFound = true
			break
		}
	}
	if !bootstrapFound {
		return nil, fmt.Errorf("APP ACL R2 bootstrap role with OID 10 is missing")
	}
	membershipNames := append([]string{bootstrap.Name}, names...)
	memberships, err := readAppACLEffectiveCatalogMembershipsR1(ctx, tx, membershipNames)
	if err != nil {
		return nil, err
	}
	for _, membership := range memberships {
		for _, name := range []string{membership.MemberRole, membership.ParentRole} {
			role, exists := byName[name]
			if !exists {
				continue
			}
			if role.RecursiveMembershipCount == ^uint16(0) {
				return nil, fmt.Errorf("APP ACL R2 role %q has too many recursive memberships", name)
			}
			role.RecursiveMembershipCount++
			byName[name] = role
		}
	}
	bootstrap = byName[bootstrap.Name]
	roles := make([]AppACLR2CatalogRoleStateV1, len(names)+1)
	bootstrap.ControlRole = AppACLControlRoleBootstrapSuperuserR2
	roles[0] = bootstrap
	for index, name := range names {
		role, exists := byName[name]
		if !exists {
			return nil, fmt.Errorf("APP ACL R2 transition role %q is missing", name)
		}
		role.ControlRole = AppACLControlRoleR2(index + 2)
		roles[index+1] = role
	}
	return roles, nil
}

func readAppACLR2PGCryptoExtensionInTx(ctx context.Context, tx pgx.Tx) (AppACLR2PGCryptoExtensionCatalogV1, error) {
	var extension AppACLR2PGCryptoExtensionCatalogV1
	var oid, schemaOID, ownerOID int64
	err := tx.QueryRow(ctx, `
		select extension.extname::text,
		       extension.oid::bigint,
		       namespace.nspname::text,
		       namespace.oid::bigint,
		       extension.extversion::text,
		       owner.rolname,
		       owner.oid::bigint
		from pg_catalog.pg_extension extension
		join pg_catalog.pg_namespace namespace on namespace.oid = extension.extnamespace
		join pg_catalog.pg_roles owner on owner.oid = extension.extowner
		where extension.extname = 'pgcrypto'
	`).Scan(&extension.Name, &oid, &extension.Schema, &schemaOID, &extension.Version, &extension.OwnerName, &ownerOID)
	if err != nil {
		return AppACLR2PGCryptoExtensionCatalogV1{}, fmt.Errorf("read APP ACL R2 pgcrypto extension catalog: %w", err)
	}
	if extension.OID, err = appACLR2CatalogUint32(oid, "pgcrypto extension OID"); err != nil {
		return AppACLR2PGCryptoExtensionCatalogV1{}, err
	}
	if extension.SchemaOID, err = appACLR2CatalogUint32(schemaOID, "pgcrypto schema OID"); err != nil {
		return AppACLR2PGCryptoExtensionCatalogV1{}, err
	}
	if extension.OwnerOID, err = appACLR2CatalogUint32(ownerOID, "pgcrypto extension owner OID"); err != nil {
		return AppACLR2PGCryptoExtensionCatalogV1{}, err
	}
	return extension, nil
}

func readAppACLR2PGCryptoMembersInTx(ctx context.Context, tx pgx.Tx) ([]AppACLR2PGCryptoMemberCatalogV1, error) {
	rows, err := tx.Query(ctx, `
		select coalesce(class_namespace.nspname::text || '.' || member_class.relname::text, ''),
		       dependency.objid::bigint,
		       dependency.objsubid::bigint,
		       dependency.refobjsubid::bigint,
		       dependency.deptype::text,
		       extension.oid::bigint,
		       (select pg_catalog.count(*)::bigint
		          from pg_catalog.pg_depend extension_dependency
		         where extension_dependency.classid = dependency.classid
		           and extension_dependency.refclassid = 'pg_catalog.pg_extension'::pg_catalog.regclass
		           and extension_dependency.objid = dependency.objid),
		       coalesce(namespace.nspname::text, ''),
		       coalesce(procedure.proname::text, ''),
		       coalesce(pg_catalog.pg_get_function_identity_arguments(procedure.oid), ''),
		       coalesce(procedure.prokind::text, ''),
		       coalesce(owner.rolname, ''),
		       coalesce(owner.oid::bigint, 0),
		       procedure.proacl is null
		from pg_catalog.pg_extension extension
		join pg_catalog.pg_depend dependency
		  on dependency.refclassid = 'pg_catalog.pg_extension'::pg_catalog.regclass
		 and dependency.refobjid = extension.oid
		left join pg_catalog.pg_class member_class on member_class.oid = dependency.classid
		left join pg_catalog.pg_namespace class_namespace on class_namespace.oid = member_class.relnamespace
		left join pg_catalog.pg_proc procedure
		  on dependency.classid = 'pg_catalog.pg_proc'::pg_catalog.regclass
		 and dependency.objid = procedure.oid
		 and dependency.objsubid = 0
		left join pg_catalog.pg_namespace namespace on namespace.oid = procedure.pronamespace
		left join pg_catalog.pg_roles owner on owner.oid = procedure.proowner
		where extension.extname = 'pgcrypto'
		order by dependency.classid, dependency.objid, dependency.objsubid, dependency.refobjsubid, dependency.deptype
	`)
	if err != nil {
		return nil, fmt.Errorf("read APP ACL R2 pgcrypto member catalog: %w", err)
	}
	defer rows.Close()
	members := make([]AppACLR2PGCryptoMemberCatalogV1, 0, len(appACLR2PGCryptoIdentityContract))
	for rows.Next() {
		var member AppACLR2PGCryptoMemberCatalogV1
		var oid, objectSubID, referenceObjectSubID, ownerOID, extensionOID, dependencyCount int64
		if err := rows.Scan(
			&member.ExtensionDependencyClass,
			&oid,
			&objectSubID,
			&referenceObjectSubID,
			&member.ExtensionDependencyType,
			&extensionOID,
			&dependencyCount,
			&member.Schema,
			&member.Name,
			&member.IdentityArguments,
			&member.RoutineKind,
			&member.OwnerName,
			&ownerOID,
			&member.ACLIsDefault,
		); err != nil {
			return nil, fmt.Errorf("scan APP ACL R2 pgcrypto member catalog: %w", err)
		}
		if member.OID, err = appACLR2CatalogUint32(oid, "pgcrypto member OID"); err != nil {
			return nil, err
		}
		if member.ExtensionDependencyObjectSubID, err = appACLR2CatalogOptionalOID(objectSubID, "pgcrypto member dependency object sub-ID"); err != nil {
			return nil, err
		}
		if member.ExtensionDependencyReferenceObjectSubID, err = appACLR2CatalogOptionalOID(referenceObjectSubID, "pgcrypto member dependency reference object sub-ID"); err != nil {
			return nil, err
		}
		if member.OwnerOID, err = appACLR2CatalogOptionalOID(ownerOID, "pgcrypto member owner OID"); err != nil {
			return nil, err
		}
		if member.ExtensionOID, err = appACLR2CatalogUint32(extensionOID, "pgcrypto member extension OID"); err != nil {
			return nil, err
		}
		if dependencyCount < 0 || dependencyCount > int64(^uint16(0)) {
			return nil, fmt.Errorf("pgcrypto member dependency count is outside uint16 bounds")
		}
		member.ExtensionDependencyCount = uint16(dependencyCount)
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate APP ACL R2 pgcrypto member catalog: %w", err)
	}
	return members, nil
}

func appACLR2CatalogUint32(value int64, field string) (uint32, error) {
	if value < 1 || value > int64(^uint32(0)) {
		return 0, fmt.Errorf("APP ACL R2 %s is outside uint32 bounds", field)
	}
	return uint32(value), nil
}

func appACLR2CatalogOptionalOID(value int64, field string) (uint32, error) {
	if value < 0 || value > int64(^uint32(0)) {
		return 0, fmt.Errorf("APP ACL R2 %s is outside uint32 bounds", field)
	}
	return uint32(value), nil
}

// ReadAppACLR2ReceiptCatalogSnapshotInTx reads the post-DDL receipt surface
// without opening or completing a transaction.
func ReadAppACLR2ReceiptCatalogSnapshotInTx(
	ctx context.Context,
	tx pgx.Tx,
	state FrozenAppACLR1StateV1,
) (AppACLR2ReceiptCatalogSnapshotV1, error) {
	if tx == nil {
		return AppACLR2ReceiptCatalogSnapshotV1{}, fmt.Errorf("APP ACL R2 receipt catalog reader has no transaction")
	}
	table, err := readAppACLR2ReceiptTableCatalogInTx(ctx, tx)
	if err != nil {
		return AppACLR2ReceiptCatalogSnapshotV1{}, err
	}
	reservedObjects, err := readAppACLR2ReservedCatalogObjectsInTx(ctx, tx)
	if err != nil {
		return AppACLR2ReceiptCatalogSnapshotV1{}, err
	}
	acl, err := readAppACLR2ReceiptACLInTx(ctx, tx, state)
	if err != nil {
		return AppACLR2ReceiptCatalogSnapshotV1{}, err
	}
	helpers, err := readAppACLR2ReceiptHelpersInTx(ctx, tx)
	if err != nil {
		return AppACLR2ReceiptCatalogSnapshotV1{}, err
	}
	return AppACLR2ReceiptCatalogSnapshotV1{Table: table, ReservedObjects: reservedObjects, ACL: acl, Helpers: helpers}, nil
}

func readAppACLR2ReservedCatalogObjectsInTx(ctx context.Context, tx pgx.Tx) ([]AppACLR2ReservedCatalogObjectV1, error) {
	rows, err := tx.Query(ctx, `
		select object_kind, schema_name, object_oid, object_identity, object_detail
		from (
			select 'relation'::text as object_kind,
			       namespace.nspname::text as schema_name,
			       relation.oid::bigint as object_oid,
			       relation.relname::text as object_identity,
			       relation.relkind::text as object_detail
			from pg_catalog.pg_class relation
			join pg_catalog.pg_namespace namespace on namespace.oid = relation.relnamespace
			where relation.relname like 'app\_acl\_r2\_%' escape '\'
			union all
			select 'function'::text,
			       namespace.nspname::text,
			       procedure.oid::bigint,
			       namespace.nspname::text || '.' || procedure.proname::text || '(' || pg_catalog.pg_get_function_identity_arguments(procedure.oid) || ')',
			       procedure.prokind::text
			from pg_catalog.pg_proc procedure
			join pg_catalog.pg_namespace namespace on namespace.oid = procedure.pronamespace
			where procedure.proname like 'app\_acl\_r2\_%' escape '\'
			union all
			select 'trigger'::text,
			       table_namespace.nspname::text,
			       trigger.oid::bigint,
			       table_relation.relname::text || '.' || trigger.tgname::text,
			       case when trigger.tgisinternal then 'internal' else 'user' end
			from pg_catalog.pg_trigger trigger
			join pg_catalog.pg_class table_relation on table_relation.oid = trigger.tgrelid
			join pg_catalog.pg_namespace table_namespace on table_namespace.oid = table_relation.relnamespace
			where trigger.tgname like 'app\_acl\_r2\_%' escape '\'
		) catalog
		order by object_kind, schema_name, object_identity, object_detail
	`)
	if err != nil {
		return nil, fmt.Errorf("read APP ACL R2 reserved catalog objects: %w", err)
	}
	defer rows.Close()
	objects := make([]AppACLR2ReservedCatalogObjectV1, 0, 5)
	for rows.Next() {
		var object AppACLR2ReservedCatalogObjectV1
		var oid int64
		if err := rows.Scan(&object.Kind, &object.Schema, &oid, &object.Identity, &object.Detail); err != nil {
			return nil, fmt.Errorf("scan APP ACL R2 reserved catalog object: %w", err)
		}
		if object.OID, err = appACLR2CatalogUint32(oid, "reserved catalog object OID"); err != nil {
			return nil, err
		}
		objects = append(objects, object)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate APP ACL R2 reserved catalog objects: %w", err)
	}
	return objects, nil
}

func readAppACLR2ReceiptTableCatalogInTx(ctx context.Context, tx pgx.Tx) (AppACLR2ReceiptTableCatalogV1, error) {
	var table AppACLR2ReceiptTableCatalogV1
	var relationOID, ownerOID int64
	if err := tx.QueryRow(ctx, `
		select namespace.nspname::text,
		       relation.relname::text,
		       relation.oid::bigint,
		       relation.relowner::bigint,
		       relation.relkind::text,
		       relation.relpersistence::text
		from pg_catalog.pg_class relation
		join pg_catalog.pg_namespace namespace on namespace.oid = relation.relnamespace
		where namespace.nspname = 'public'
		  and relation.relname = 'app_acl_r2_bootstrap_receipt'
	`).Scan(&table.Schema, &table.Name, &relationOID, &ownerOID, &table.Kind, &table.Persistence); err != nil {
		return AppACLR2ReceiptTableCatalogV1{}, fmt.Errorf("read APP ACL R2 receipt table catalog: %w", err)
	}
	owner, err := appACLR2CatalogUint32(ownerOID, "receipt table owner OID")
	if err != nil {
		return AppACLR2ReceiptTableCatalogV1{}, err
	}
	table.OwnerOID = owner
	columnRows, err := tx.Query(ctx, `
		select attribute.attname::text,
		       pg_catalog.format_type(attribute.atttypid, attribute.atttypmod),
		       attribute.attnotnull,
		       coalesce(pg_catalog.pg_get_expr(default_value.adbin, default_value.adrelid), '')
		from pg_catalog.pg_attribute attribute
		left join pg_catalog.pg_attrdef default_value
		  on default_value.adrelid = attribute.attrelid
		 and default_value.adnum = attribute.attnum
		where attribute.attrelid = $1::pg_catalog.oid
		  and attribute.attnum > 0
		  and not attribute.attisdropped
		order by attribute.attnum
	`, relationOID)
	if err != nil {
		return AppACLR2ReceiptTableCatalogV1{}, fmt.Errorf("read APP ACL R2 receipt table columns: %w", err)
	}
	defer columnRows.Close()
	for columnRows.Next() {
		var column AppACLR2ReceiptTableColumnCatalogV1
		if err := columnRows.Scan(&column.Name, &column.Type, &column.NotNull, &column.DefaultExpression); err != nil {
			return AppACLR2ReceiptTableCatalogV1{}, fmt.Errorf("scan APP ACL R2 receipt table column: %w", err)
		}
		table.Columns = append(table.Columns, column)
	}
	if err := columnRows.Err(); err != nil {
		return AppACLR2ReceiptTableCatalogV1{}, fmt.Errorf("iterate APP ACL R2 receipt table columns: %w", err)
	}
	columnACLRows, err := tx.Query(ctx, `
		select attribute.attname::text,
		       case when acl_entry.grantee = 0 then 'PUBLIC' else grantee.rolname end,
		       acl_entry.privilege_type,
		       acl_entry.is_grantable
		from pg_catalog.pg_attribute attribute
		join pg_catalog.pg_class relation on relation.oid = attribute.attrelid
		cross join lateral pg_catalog.aclexplode(attribute.attacl)
		  as acl_entry(grantor, grantee, privilege_type, is_grantable)
		left join pg_catalog.pg_roles grantee on grantee.oid = acl_entry.grantee
		where relation.oid = $1::pg_catalog.oid
		  and attribute.attnum > 0
		  and not attribute.attisdropped
		  and attribute.attacl is not null
		  and pg_catalog.cardinality(attribute.attacl) > 0
		order by attribute.attnum, grantee.rolname nulls first, acl_entry.privilege_type, acl_entry.is_grantable
	`, relationOID)
	if err != nil {
		return AppACLR2ReceiptTableCatalogV1{}, fmt.Errorf("read APP ACL R2 receipt table column ACLs: %w", err)
	}
	defer columnACLRows.Close()
	for columnACLRows.Next() {
		var columnACL AppACLR2ReceiptTableColumnACLCatalogV1
		if err := columnACLRows.Scan(&columnACL.ColumnName, &columnACL.Grantee, &columnACL.Privilege, &columnACL.GrantOption); err != nil {
			return AppACLR2ReceiptTableCatalogV1{}, fmt.Errorf("scan APP ACL R2 receipt table column ACL: %w", err)
		}
		table.ColumnACLs = append(table.ColumnACLs, columnACL)
	}
	if err := columnACLRows.Err(); err != nil {
		return AppACLR2ReceiptTableCatalogV1{}, fmt.Errorf("iterate APP ACL R2 receipt table column ACLs: %w", err)
	}
	inheritanceRows, err := tx.Query(ctx, `
		select inheritance.inhrelid = $1::pg_catalog.oid,
		       inheritance.inhparent = $1::pg_catalog.oid
		from pg_catalog.pg_inherits inheritance
		where inheritance.inhrelid = $1::pg_catalog.oid
		   or inheritance.inhparent = $1::pg_catalog.oid
		order by inheritance.inhrelid, inheritance.inhparent
	`, relationOID)
	if err != nil {
		return AppACLR2ReceiptTableCatalogV1{}, fmt.Errorf("read APP ACL R2 receipt table inheritance: %w", err)
	}
	defer inheritanceRows.Close()
	for inheritanceRows.Next() {
		var inheritance AppACLR2ReceiptTableInheritanceCatalogV1
		if err := inheritanceRows.Scan(&inheritance.ReceiptIsChild, &inheritance.ReceiptIsParent); err != nil {
			return AppACLR2ReceiptTableCatalogV1{}, fmt.Errorf("scan APP ACL R2 receipt table inheritance: %w", err)
		}
		table.Inheritance = append(table.Inheritance, inheritance)
	}
	if err := inheritanceRows.Err(); err != nil {
		return AppACLR2ReceiptTableCatalogV1{}, fmt.Errorf("iterate APP ACL R2 receipt table inheritance: %w", err)
	}
	constraintRows, err := tx.Query(ctx, `
		select constraint_catalog.contype::text,
		       pg_catalog.pg_get_constraintdef(constraint_catalog.oid, true),
		       constraint_catalog.convalidated,
		       constraint_catalog.conindid::bigint,
		       coalesce(index_catalog.indisprimary, false),
		       coalesce(index_catalog.indisunique, false),
		       coalesce(index_catalog.indisvalid, false)
		from pg_catalog.pg_constraint constraint_catalog
		left join pg_catalog.pg_index index_catalog
		  on index_catalog.indexrelid = constraint_catalog.conindid
		 and index_catalog.indrelid = constraint_catalog.conrelid
		where constraint_catalog.conrelid = $1::pg_catalog.oid
		order by constraint_catalog.contype, pg_catalog.pg_get_constraintdef(constraint_catalog.oid, true)
	`, relationOID)
	if err != nil {
		return AppACLR2ReceiptTableCatalogV1{}, fmt.Errorf("read APP ACL R2 receipt table constraints: %w", err)
	}
	defer constraintRows.Close()
	for constraintRows.Next() {
		var constraint AppACLR2ReceiptTableConstraintCatalogV1
		var indexOID int64
		if err := constraintRows.Scan(
			&constraint.Type,
			&constraint.Definition,
			&constraint.Validated,
			&indexOID,
			&constraint.IndexPrimary,
			&constraint.IndexUnique,
			&constraint.IndexValid,
		); err != nil {
			return AppACLR2ReceiptTableCatalogV1{}, fmt.Errorf("scan APP ACL R2 receipt table constraint: %w", err)
		}
		if constraint.IndexOID, err = appACLR2CatalogOptionalOID(indexOID, "receipt table constraint index OID"); err != nil {
			return AppACLR2ReceiptTableCatalogV1{}, err
		}
		table.Constraints = append(table.Constraints, constraint)
	}
	if err := constraintRows.Err(); err != nil {
		return AppACLR2ReceiptTableCatalogV1{}, fmt.Errorf("iterate APP ACL R2 receipt table constraints: %w", err)
	}
	return table, nil
}

func readAppACLR2ReceiptACLInTx(ctx context.Context, tx pgx.Tx, state FrozenAppACLR1StateV1) (AppACLControlACLBodyR2V1, error) {
	objects := appACLR2L2ACLContract().Objects
	for index := range objects {
		objects[index].ExplicitGrants = nil
		objects[index].EffectiveRelevantPrivilegeMask = 0
	}
	rows, err := tx.Query(ctx, `
		select object_kind, schema_name, object_identity, owner_oid, object_oid
		from (
			select 1::integer as object_kind,
			       namespace.nspname::text as schema_name,
			       relation.relname::text as object_identity,
			       relation.relowner::bigint as owner_oid,
			       relation.oid::bigint as object_oid
			from pg_catalog.pg_class relation
			join pg_catalog.pg_namespace namespace on namespace.oid = relation.relnamespace
			where namespace.nspname = 'public'
			  and relation.relname = 'app_acl_r2_bootstrap_receipt'
			  and relation.relkind = 'r'
			union all
			select 2,
			       namespace.nspname::text,
			       namespace.nspname::text || '.' || procedure.proname::text || '(' || pg_catalog.pg_get_function_identity_arguments(procedure.oid) || ')',
			       procedure.proowner::bigint,
			       procedure.oid::bigint
			from pg_catalog.pg_proc procedure
			join pg_catalog.pg_namespace namespace on namespace.oid = procedure.pronamespace
			where namespace.nspname = 'record_platform_internal'
			  and procedure.proname in ('app_acl_r2_assert_bootstrap_receipt_insert', 'app_acl_r2_reject_bootstrap_receipt_mutation')
		) object_catalog
		order by object_kind, schema_name, object_identity
	`)
	if err != nil {
		return AppACLControlACLBodyR2V1{}, fmt.Errorf("read APP ACL R2 receipt object catalog: %w", err)
	}
	defer rows.Close()
	type receiptObjectCatalog struct {
		OID      uint32
		OwnerOID uint32
	}
	expected := make(map[string]int, len(objects))
	for index := range objects {
		expected[fmt.Sprintf("%d|%s|%s", objects[index].Kind, objects[index].Schema, objects[index].Identity)] = index
	}
	observed := make(map[string]receiptObjectCatalog, len(objects))
	var objectCatalogErr error
	for rows.Next() {
		// Keep consuming rows so a completion error can take precedence over row-local drift.
		if objectCatalogErr != nil {
			continue
		}
		var kind int
		var schema, identity string
		var objectOID, ownerOID int64
		if err := rows.Scan(&kind, &schema, &identity, &ownerOID, &objectOID); err != nil {
			objectCatalogErr = fmt.Errorf("scan APP ACL R2 receipt object catalog: %w", err)
			continue
		}
		object, err := appACLR2CatalogUint32(objectOID, "receipt object OID")
		if err != nil {
			objectCatalogErr = err
			continue
		}
		owner, err := appACLR2CatalogUint32(ownerOID, "receipt object owner OID")
		if err != nil {
			objectCatalogErr = err
			continue
		}
		key := fmt.Sprintf("%d|%s|%s", kind, schema, identity)
		_, exists := expected[key]
		if !exists {
			objectCatalogErr = fmt.Errorf("APP ACL R2 receipt object identity %q is unexpected", identity)
			continue
		}
		if _, exists := observed[key]; exists {
			objectCatalogErr = fmt.Errorf("APP ACL R2 receipt object identity %q is duplicate", identity)
			continue
		}
		observed[key] = receiptObjectCatalog{OID: object, OwnerOID: owner}
	}
	if err := rows.Err(); err != nil {
		return AppACLControlACLBodyR2V1{}, fmt.Errorf("iterate APP ACL R2 receipt object catalog: %w", err)
	}
	if objectCatalogErr != nil {
		return AppACLControlACLBodyR2V1{}, objectCatalogErr
	}
	assertHelperKey := fmt.Sprintf("%d|%s|%s", objects[1].Kind, objects[1].Schema, objects[1].Identity)
	rejectHelperKey := fmt.Sprintf("%d|%s|%s", objects[2].Kind, objects[2].Schema, objects[2].Identity)
	assertHelperCatalog := observed[assertHelperKey]
	rejectHelperCatalog := observed[rejectHelperKey]
	var assertHelperOID, rejectHelperOID any
	if assertHelperCatalog.OwnerOID == objects[1].OwnerOID {
		assertHelperOID = int64(assertHelperCatalog.OID)
	}
	if rejectHelperCatalog.OwnerOID == objects[2].OwnerOID {
		rejectHelperOID = int64(rejectHelperCatalog.OID)
	}
	for index := range objects {
		key := fmt.Sprintf("%d|%s|%s", objects[index].Kind, objects[index].Schema, objects[index].Identity)
		catalog, exists := observed[key]
		if !exists {
			return AppACLControlACLBodyR2V1{}, fmt.Errorf("APP ACL R2 receipt object %s.%s is missing", objects[index].Schema, objects[index].Identity)
		}
		objects[index].OwnerOID = catalog.OwnerOID
	}

	roleTags := map[string]AppACLControlRoleR2{
		state.DirectMigratorRole: AppACLControlRoleDirectMigratorR2,
		state.CenterRuntimeRole:  AppACLControlRoleCenterRuntimeR2,
		state.PlatformAdminRole:  AppACLControlRolePlatformAdminR2,
		"PUBLIC":                 AppACLControlRolePublicR2,
	}
	grantRows, err := tx.Query(ctx, `
		select object_kind, object_identity, grantee_name, privilege_type, is_grantable
		from (
			select 1::integer as object_kind,
			       relation.relname::text as object_identity,
			       case when acl.grantee = 0 then 'PUBLIC' else grantee.rolname end as grantee_name,
			       acl.privilege_type,
			       acl.is_grantable
			from pg_catalog.pg_class relation
			join pg_catalog.pg_namespace namespace on namespace.oid = relation.relnamespace
			cross join lateral pg_catalog.aclexplode(relation.relacl) acl
			left join pg_catalog.pg_roles grantee on grantee.oid = acl.grantee
			where namespace.nspname = 'public'
			  and relation.relname = 'app_acl_r2_bootstrap_receipt'
			  and acl.grantee <> relation.relowner
			union all
			select 2,
			       namespace.nspname::text || '.' || procedure.proname::text || '(' || pg_catalog.pg_get_function_identity_arguments(procedure.oid) || ')',
			       case when acl.grantee = 0 then 'PUBLIC' else grantee.rolname end,
			       acl.privilege_type,
			       acl.is_grantable
			from pg_catalog.pg_proc procedure
			join pg_catalog.pg_namespace namespace on namespace.oid = procedure.pronamespace
			cross join lateral pg_catalog.aclexplode(procedure.proacl) acl
			left join pg_catalog.pg_roles grantee on grantee.oid = acl.grantee
			where namespace.nspname = 'record_platform_internal'
			  and procedure.proname in ('app_acl_r2_assert_bootstrap_receipt_insert', 'app_acl_r2_reject_bootstrap_receipt_mutation')
			  and acl.grantee <> procedure.proowner
		) grants
		order by object_kind, object_identity, grantee_name, privilege_type, is_grantable
	`)
	if err != nil {
		return AppACLControlACLBodyR2V1{}, fmt.Errorf("read APP ACL R2 receipt direct ACL: %w", err)
	}
	defer grantRows.Close()
	for grantRows.Next() {
		var kind int
		var identity, grantee, privilege string
		var grantOption bool
		if err := grantRows.Scan(&kind, &identity, &grantee, &privilege, &grantOption); err != nil {
			return AppACLControlACLBodyR2V1{}, fmt.Errorf("scan APP ACL R2 receipt direct ACL: %w", err)
		}
		tag, exists := roleTags[grantee]
		if !exists {
			return AppACLControlACLBodyR2V1{}, fmt.Errorf("APP ACL R2 receipt ACL has unknown grantee %q", grantee)
		}
		var privilegeTag AppACLControlPrivilegeR2
		switch privilege {
		case "SELECT":
			privilegeTag = AppACLControlPrivilegeSelectR2
		case "EXECUTE":
			privilegeTag = AppACLControlPrivilegeExecuteR2
		default:
			return AppACLControlACLBodyR2V1{}, fmt.Errorf("APP ACL R2 receipt ACL has unexpected privilege %q", privilege)
		}
		matched := false
		for index := range objects {
			if int(objects[index].Kind) == kind && objects[index].Identity == identity {
				objects[index].ExplicitGrants = append(objects[index].ExplicitGrants, AppACLControlGrantR2V1{GranteeRole: tag, Privilege: privilegeTag, GrantOption: grantOption})
				matched = true
				break
			}
		}
		if !matched {
			return AppACLControlACLBodyR2V1{}, fmt.Errorf("APP ACL R2 receipt ACL refers to unknown object %q", identity)
		}
	}
	if err := grantRows.Err(); err != nil {
		return AppACLControlACLBodyR2V1{}, fmt.Errorf("iterate APP ACL R2 receipt direct ACL: %w", err)
	}
	for index := range objects {
		sort.Slice(objects[index].ExplicitGrants, func(left, right int) bool {
			return compareAppACLR2ControlGrant(objects[index].ExplicitGrants[left], objects[index].ExplicitGrants[right]) < 0
		})
	}

	roleNames := []string{state.DirectMigratorRole, state.CenterRuntimeRole, state.PlatformAdminRole}
	effectiveRows, err := tx.Query(ctx, `
		select role.rolname,
		       pg_catalog.has_table_privilege(role.oid, 'public.app_acl_r2_bootstrap_receipt'::pg_catalog.regclass, 'SELECT'),
		       pg_catalog.has_table_privilege(role.oid, 'public.app_acl_r2_bootstrap_receipt'::pg_catalog.regclass, 'INSERT'),
		       pg_catalog.has_table_privilege(role.oid, 'public.app_acl_r2_bootstrap_receipt'::pg_catalog.regclass, 'UPDATE'),
		       pg_catalog.has_table_privilege(role.oid, 'public.app_acl_r2_bootstrap_receipt'::pg_catalog.regclass, 'DELETE'),
		       pg_catalog.has_table_privilege(role.oid, 'public.app_acl_r2_bootstrap_receipt'::pg_catalog.regclass, 'TRUNCATE'),
		       pg_catalog.has_table_privilege(role.oid, 'public.app_acl_r2_bootstrap_receipt'::pg_catalog.regclass, 'REFERENCES'),
		       pg_catalog.has_table_privilege(role.oid, 'public.app_acl_r2_bootstrap_receipt'::pg_catalog.regclass, 'TRIGGER'),
		       coalesce(pg_catalog.has_function_privilege(role.oid, $2::pg_catalog.oid, 'EXECUTE'), false),
		       coalesce(pg_catalog.has_function_privilege(role.oid, $3::pg_catalog.oid, 'EXECUTE'), false)
		from pg_catalog.pg_roles role
		where role.rolname = any($1::name[])
		union all
		select bootstrap.rolname,
		       true,
		       true,
		       true,
		       true,
		       true,
		       true,
		       true,
		       true,
		       true
		from pg_catalog.pg_roles bootstrap
		where bootstrap.oid = 10
		order by 1
	`, roleNames, assertHelperOID, rejectHelperOID)
	if err != nil {
		return AppACLControlACLBodyR2V1{}, fmt.Errorf("read APP ACL R2 receipt effective ACL: %w", err)
	}
	defer effectiveRows.Close()
	bootstrapName := ""
	for effectiveRows.Next() {
		var roleName string
		var tableSelect, tableInsert, tableUpdate, tableDelete, tableTruncate, tableReferences, tableTrigger bool
		var assertExecute, rejectExecute bool
		if err := effectiveRows.Scan(
			&roleName,
			&tableSelect,
			&tableInsert,
			&tableUpdate,
			&tableDelete,
			&tableTruncate,
			&tableReferences,
			&tableTrigger,
			&assertExecute,
			&rejectExecute,
		); err != nil {
			return AppACLControlACLBodyR2V1{}, fmt.Errorf("scan APP ACL R2 receipt effective ACL: %w", err)
		}
		var tag AppACLControlRoleR2
		if roleName == state.DirectMigratorRole {
			tag = AppACLControlRoleDirectMigratorR2
		} else if roleName == state.CenterRuntimeRole {
			tag = AppACLControlRoleCenterRuntimeR2
		} else if roleName == state.PlatformAdminRole {
			tag = AppACLControlRolePlatformAdminR2
		} else {
			bootstrapName = roleName
			tag = AppACLControlRoleBootstrapSuperuserR2
		}
		if tag != AppACLControlRoleBootstrapSuperuserR2 && (tableInsert || tableUpdate || tableDelete || tableTruncate || tableReferences || tableTrigger) {
			return AppACLControlACLBodyR2V1{}, fmt.Errorf("APP ACL R2 receipt has effective non-SELECT table privilege for role %q", roleName)
		}
		bit := uint8(1 << (tag - 1))
		if tableSelect {
			objects[0].EffectiveRelevantPrivilegeMask |= bit
		}
		if assertExecute {
			objects[1].EffectiveRelevantPrivilegeMask |= bit
		}
		if rejectExecute {
			objects[2].EffectiveRelevantPrivilegeMask |= bit
		}
	}
	if err := effectiveRows.Err(); err != nil {
		return AppACLControlACLBodyR2V1{}, fmt.Errorf("iterate APP ACL R2 receipt effective ACL: %w", err)
	}
	if bootstrapName == "" {
		return AppACLControlACLBodyR2V1{}, fmt.Errorf("APP ACL R2 receipt bootstrap OID 10 role is missing")
	}
	for index := range objects {
		for _, grant := range objects[index].ExplicitGrants {
			if grant.GranteeRole == AppACLControlRolePublicR2 {
				objects[index].EffectiveRelevantPrivilegeMask |= 1 << (AppACLControlRolePublicR2 - 1)
			}
		}
	}

	triggerRows, err := tx.Query(ctx, `
		select table_namespace.nspname::text,
		       table_relation.relname::text,
		       trigger.tgname::text,
		       function_namespace.nspname::text,
		       function_namespace.nspname::text || '.' || function.proname::text || '(' || pg_catalog.pg_get_function_identity_arguments(function.oid) || ')',
		       table_relation.relowner::bigint,
		       function.proowner::bigint,
		       trigger.tgenabled = 'O',
		       trigger.tgisinternal,
		       trigger.tgtype::integer,
		       trigger.tgattr::text,
		       trigger.tgqual is not null,
		       pg_catalog.pg_get_triggerdef(trigger.oid, false)
		from pg_catalog.pg_trigger trigger
		join pg_catalog.pg_class table_relation on table_relation.oid = trigger.tgrelid
		join pg_catalog.pg_namespace table_namespace on table_namespace.oid = table_relation.relnamespace
		join pg_catalog.pg_proc function on function.oid = trigger.tgfoid
		join pg_catalog.pg_namespace function_namespace on function_namespace.oid = function.pronamespace
		where table_namespace.nspname = 'public'
		  and table_relation.relname = 'app_acl_r2_bootstrap_receipt'
		order by trigger.tgname
	`)
	if err != nil {
		return AppACLControlACLBodyR2V1{}, fmt.Errorf("read APP ACL R2 receipt trigger catalog: %w", err)
	}
	defer triggerRows.Close()
	triggers := make([]AppACLControlTriggerR2V1, 0, 1)
	for triggerRows.Next() {
		var trigger AppACLControlTriggerR2V1
		var tableOwner, functionOwner int64
		var internal, hasQualification bool
		var triggerType int64
		var triggerAttributes, definition string
		if err := triggerRows.Scan(&trigger.TableSchema, &trigger.TableName, &trigger.TriggerName, &trigger.FunctionSchema, &trigger.FunctionIdentity, &tableOwner, &functionOwner, &trigger.Enabled, &internal, &triggerType, &triggerAttributes, &hasQualification, &definition); err != nil {
			return AppACLControlACLBodyR2V1{}, fmt.Errorf("scan APP ACL R2 receipt trigger catalog: %w", err)
		}
		if trigger.TableOwnerOID, err = appACLR2CatalogUint32(tableOwner, "receipt trigger table owner OID"); err != nil {
			return AppACLControlACLBodyR2V1{}, err
		}
		if trigger.FunctionOwnerOID, err = appACLR2CatalogUint32(functionOwner, "receipt trigger function owner OID"); err != nil {
			return AppACLControlACLBodyR2V1{}, err
		}
		if err := validateAppACLR2ReceiptTriggerCatalog(trigger, internal, triggerType, triggerAttributes, hasQualification, definition); err != nil {
			return AppACLControlACLBodyR2V1{}, err
		}
		triggers = append(triggers, trigger)
	}
	if err := triggerRows.Err(); err != nil {
		return AppACLControlACLBodyR2V1{}, fmt.Errorf("iterate APP ACL R2 receipt trigger catalog: %w", err)
	}
	var defaultACLCount int64
	if err := tx.QueryRow(ctx, `
		select pg_catalog.count(*)::bigint
		from pg_catalog.pg_default_acl default_acl
		left join pg_catalog.pg_namespace namespace on namespace.oid = default_acl.defaclnamespace
		where default_acl.defaclrole = 10
		  and ((default_acl.defaclobjtype = 'r'
		        and (default_acl.defaclnamespace = 0 or namespace.nspname = 'public'))
		    or (default_acl.defaclobjtype = 'f'
		        and (default_acl.defaclnamespace = 0 or namespace.nspname = 'record_platform_internal')))
	`).Scan(&defaultACLCount); err != nil {
		return AppACLControlACLBodyR2V1{}, fmt.Errorf("read APP ACL R2 receipt default ACL catalog: %w", err)
	}
	if defaultACLCount != 0 {
		return AppACLControlACLBodyR2V1{}, fmt.Errorf("APP ACL R2 receipt bootstrap owner has default ACL drift")
	}
	return AppACLControlACLBodyR2V1{
		Objects:              objects,
		Triggers:             triggers,
		DefaultACLAssertions: append([]AppACLDefaultACLAssertionR2V1(nil), appACLR2L2ACLContract().DefaultACLAssertions...),
	}, nil
}

func validateAppACLR2ReceiptTriggerCatalog(
	trigger AppACLControlTriggerR2V1,
	internal bool,
	triggerType int64,
	triggerAttributes string,
	hasQualification bool,
	definition string,
) error {
	if internal {
		return fmt.Errorf("APP ACL R2 receipt trigger %q is internal", trigger.TriggerName)
	}
	if triggerType != appACLR2ReceiptTriggerTypeBeforeUpdateDeleteTruncateStatement {
		return fmt.Errorf("APP ACL R2 receipt trigger %q does not have BEFORE UPDATE/DELETE/TRUNCATE statement semantics", trigger.TriggerName)
	}
	if triggerAttributes != "" {
		return fmt.Errorf("APP ACL R2 receipt trigger %q has UPDATE OF columns in pg_trigger.tgattr", trigger.TriggerName)
	}
	if hasQualification {
		return fmt.Errorf("APP ACL R2 receipt trigger %q has a qualification", trigger.TriggerName)
	}
	if definition != appACLR2ReceiptTriggerDefinitionPG16 {
		return fmt.Errorf("APP ACL R2 receipt trigger %q definition does not match the fixed PostgreSQL 16 catalog definition", trigger.TriggerName)
	}
	return nil
}

func readAppACLR2ReceiptHelpersInTx(ctx context.Context, tx pgx.Tx) ([]AppACLR2ReceiptHelperCatalogV1, error) {
	rows, err := tx.Query(ctx, `
		select namespace.nspname::text,
		       procedure.proname::text,
		       pg_catalog.pg_get_function_identity_arguments(procedure.oid),
		       namespace.nspname::text || '.' || procedure.proname::text || '(' || pg_catalog.pg_get_function_identity_arguments(procedure.oid) || ')',
		       procedure.proowner::bigint,
		       procedure.prokind::text,
		       pg_catalog.pg_get_function_result(procedure.oid),
		       language.lanname::text,
		       procedure.provolatile::text,
		       procedure.proparallel::text,
		       procedure.prosecdef,
		       procedure.proisstrict,
		       procedure.proleakproof,
		       procedure.proretset,
		       procedure.procost::double precision,
		       procedure.prorows::double precision,
		       procedure.prosupport::pg_catalog.oid::bigint,
		       procedure.provariadic::bigint,
		       procedure.pronargs::bigint,
		       procedure.pronargdefaults::bigint,
		       procedure.proargtypes::text,
		       procedure.proallargtypes is null,
		       procedure.proargmodes is null,
		       procedure.proargnames is null,
		       procedure.proargdefaults is null,
		       procedure.protrftypes is null,
		       procedure.probin is null,
		       procedure.prosqlbody is null,
		       coalesce(procedure.proconfig, '{}'::text[]),
		       pg_catalog.pg_get_functiondef(procedure.oid),
		       procedure.prosrc::text
		from pg_catalog.pg_proc procedure
		join pg_catalog.pg_namespace namespace on namespace.oid = procedure.pronamespace
		join pg_catalog.pg_language language on language.oid = procedure.prolang
		where namespace.nspname = 'record_platform_internal'
		  and procedure.proname in ('app_acl_r2_assert_bootstrap_receipt_insert', 'app_acl_r2_reject_bootstrap_receipt_mutation')
		order by procedure.proname, pg_catalog.pg_get_function_identity_arguments(procedure.oid)
	`)
	if err != nil {
		return nil, fmt.Errorf("read APP ACL R2 receipt helper catalog: %w", err)
	}
	defer rows.Close()
	helpers := make([]AppACLR2ReceiptHelperCatalogV1, 0, 2)
	for rows.Next() {
		var helper AppACLR2ReceiptHelperCatalogV1
		var ownerOID, supportFunctionOID, variadicTypeOID, argumentCount, argumentDefaultCount int64
		if err := rows.Scan(
			&helper.Schema,
			&helper.Name,
			&helper.IdentityArguments,
			&helper.Identity,
			&ownerOID,
			&helper.Kind,
			&helper.Result,
			&helper.Language,
			&helper.Volatility,
			&helper.Parallel,
			&helper.SecurityDefiner,
			&helper.Strict,
			&helper.Leakproof,
			&helper.ReturnsSet,
			&helper.Cost,
			&helper.Rows,
			&supportFunctionOID,
			&variadicTypeOID,
			&argumentCount,
			&argumentDefaultCount,
			&helper.InputArgumentTypes,
			&helper.AllArgumentTypesNull,
			&helper.ArgumentModesNull,
			&helper.ArgumentNamesNull,
			&helper.ArgumentDefaultsNull,
			&helper.TransformTypesNull,
			&helper.BinaryNull,
			&helper.SQLBodyNull,
			&helper.Config,
			&helper.Definition,
			&helper.Source,
		); err != nil {
			return nil, fmt.Errorf("scan APP ACL R2 receipt helper catalog: %w", err)
		}
		if helper.OwnerOID, err = appACLR2CatalogUint32(ownerOID, "receipt helper owner OID"); err != nil {
			return nil, err
		}
		if helper.SupportFunctionOID, err = appACLR2CatalogOptionalOID(supportFunctionOID, "receipt helper support function OID"); err != nil {
			return nil, err
		}
		if helper.VariadicTypeOID, err = appACLR2CatalogOptionalOID(variadicTypeOID, "receipt helper variadic type OID"); err != nil {
			return nil, err
		}
		if argumentCount < 0 || argumentCount > int64(^uint16(0)) {
			return nil, fmt.Errorf("APP ACL R2 receipt helper argument count is outside uint16 bounds")
		}
		helper.ArgumentCount = uint16(argumentCount)
		if argumentDefaultCount < 0 || argumentDefaultCount > int64(^uint16(0)) {
			return nil, fmt.Errorf("APP ACL R2 receipt helper default argument count is outside uint16 bounds")
		}
		helper.ArgumentDefaultCount = uint16(argumentDefaultCount)
		helpers = append(helpers, helper)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate APP ACL R2 receipt helper catalog: %w", err)
	}
	return helpers, nil
}
