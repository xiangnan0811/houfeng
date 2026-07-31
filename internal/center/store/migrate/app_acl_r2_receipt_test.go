package migrate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

const appACLR2PGCryptoMember16PG16IdentityLiteral = "record_platform_internal.pgp_armor_headers|text, OUT key text, OUT value text"
const appACLR2PGCryptoIdentitySetPG16DigestHex = "57e7ac6a986705d8fa1e5b2260c1836b74dffe1b33bee00d65d1b275284e8196"

const appACLR2GoldenDomainBodyHex = "484f5546454e472d4150502d41434c2d52322d444f4d41494e2d56310001004372642d31356361353865326332633764616133636132306634653063366638356166383432353463396136373566326262333039326663626430373339626631613138000b6170706c69636174696f6e0000000000000001000f706f7374677265735f73797374656d0011373236323338353937393033383238353600067932000b686f7566656e675f617070"
const appACLR2GoldenDomainDigestHex = "cda38896d1735a2dac68acd19e3d0ae19162f1c256efd2a96541903af0323c25"
const appACLR2GoldenDomainTrailingBodyHex = "484f5546454e472d4150502d41434c2d52322d444f4d41494e2d56310001004372642d31356361353865326332633764616133636132306634653063366638356166383432353463396136373566326262333039326663626430373339626631613138000b6170706c69636174696f6e0000000000000001000f706f7374677265735f73797374656d0011373236323338353937393033383238353600067932000b686f7566656e675f61707000"

const appACLR2GoldenL2ACLBodyHex = "484f5546454e472d4150502d41434c2d52322d4c322d41434c2d5631000100030100067075626c6963001c6170705f61636c5f72325f626f6f7473747261705f72656365697074010000000a0002020100030100070200187265636f72645f706c6174666f726d5f696e7465726e616c00517265636f72645f706c6174666f726d5f696e7465726e616c2e6170705f61636c5f72325f6173736572745f626f6f7473747261705f726563656970745f696e736572742862797465612c20627974656129010000000a0000010200187265636f72645f706c6174666f726d5f696e7465726e616c00477265636f72645f706c6174666f726d5f696e7465726e616c2e6170705f61636c5f72325f72656a6563745f626f6f7473747261705f726563656970745f6d75746174696f6e2829010000000a000001000100067075626c6963001c6170705f61636c5f72325f626f6f7473747261705f7265636569707400266170705f61636c5f72325f626f6f7473747261705f726563656970745f696d6d757461626c6500187265636f72645f706c6174666f726d5f696e7465726e616c00477265636f72645f706c6174666f726d5f696e7465726e616c2e6170705f61636c5f72325f72656a6563745f626f6f7473747261705f726563656970745f6d75746174696f6e28290000000a0000000a010002010101010202"
const appACLR2GoldenL2ACLDigestHex = "b228cc6316f673493d8942d0c8fcb94063cfcd1d850b2a5120a4a8380802db91"
const appACLR2GoldenL2ACLReversedGrantsBodyHex = "484f5546454e472d4150502d41434c2d52322d4c322d41434c2d5631000100030100067075626c6963001c6170705f61636c5f72325f626f6f7473747261705f72656365697074010000000a0002030100020100070200187265636f72645f706c6174666f726d5f696e7465726e616c00517265636f72645f706c6174666f726d5f696e7465726e616c2e6170705f61636c5f72325f6173736572745f626f6f7473747261705f726563656970745f696e736572742862797465612c20627974656129010000000a0000010200187265636f72645f706c6174666f726d5f696e7465726e616c00477265636f72645f706c6174666f726d5f696e7465726e616c2e6170705f61636c5f72325f72656a6563745f626f6f7473747261705f726563656970745f6d75746174696f6e2829010000000a000001000100067075626c6963001c6170705f61636c5f72325f626f6f7473747261705f7265636569707400266170705f61636c5f72325f626f6f7473747261705f726563656970745f696d6d757461626c6500187265636f72645f706c6174666f726d5f696e7465726e616c00477265636f72645f706c6174666f726d5f696e7465726e616c2e6170705f61636c5f72325f72656a6563745f626f6f7473747261705f726563656970745f6d75746174696f6e28290000000a0000000a010002010101010202"

func TestAppACLR2ReceiptDomainGoldenVector(t *testing.T) {
	wantBody := mustDecodeAppACLR2LiteralHex(t, appACLR2GoldenDomainBodyHex)
	wantDigest := mustDecodeAppACLR2LiteralDigest(t, appACLR2GoldenDomainDigestHex)
	domain := appACLR2GoldenDomainFixture()

	body, err := CanonicalAppACLDomainR2BodyV1(domain)
	if err != nil {
		t.Fatalf("CanonicalAppACLDomainR2BodyV1() error = %v", err)
	}
	if !bytes.Equal(body, wantBody) {
		t.Fatalf("domain body = %x, want literal %x", body, wantBody)
	}
	if got := sha256.Sum256(wantBody); got != wantDigest {
		t.Fatalf("literal domain SHA-256 = %x, want documented %x", got, wantDigest)
	}
	parsed, err := ParseCanonicalAppACLDomainR2BodyV1(wantBody)
	if err != nil {
		t.Fatalf("ParseCanonicalAppACLDomainR2BodyV1(literal) error = %v", err)
	}
	reencoded, err := CanonicalAppACLDomainR2BodyV1(parsed)
	if err != nil {
		t.Fatalf("CanonicalAppACLDomainR2BodyV1(parsed) error = %v", err)
	}
	if !bytes.Equal(reencoded, wantBody) {
		t.Fatalf("re-encoded domain body = %x, want literal %x", reencoded, wantBody)
	}
	if _, err := ParseCanonicalAppACLDomainR2BodyV1(mustDecodeAppACLR2LiteralHex(t, appACLR2GoldenDomainTrailingBodyHex)); err == nil || !strings.Contains(err.Error(), "trailing") {
		t.Fatalf("ParseCanonicalAppACLDomainR2BodyV1(trailing literal) error = %v, want trailing-byte rejection", err)
	}
}

func TestAppACLR2ReceiptL2ACLGoldenVector(t *testing.T) {
	wantBody := mustDecodeAppACLR2LiteralHex(t, appACLR2GoldenL2ACLBodyHex)
	wantDigest := mustDecodeAppACLR2LiteralDigest(t, appACLR2GoldenL2ACLDigestHex)

	body, err := CanonicalAppACLL2ACLBodyR2V1(appACLR2GoldenL2ACLFixture())
	if err != nil {
		t.Fatalf("CanonicalAppACLL2ACLBodyR2V1() error = %v", err)
	}
	if !bytes.Equal(body, wantBody) {
		t.Fatalf("L2 ACL body = %x, want literal %x", body, wantBody)
	}
	if got := sha256.Sum256(wantBody); got != wantDigest {
		t.Fatalf("literal L2 ACL SHA-256 = %x, want documented %x", got, wantDigest)
	}
	parsed, err := ParseCanonicalAppACLL2ACLBodyR2V1(wantBody)
	if err != nil {
		t.Fatalf("ParseCanonicalAppACLL2ACLBodyR2V1(literal) error = %v", err)
	}
	reencoded, err := CanonicalAppACLL2ACLBodyR2V1(parsed)
	if err != nil {
		t.Fatalf("CanonicalAppACLL2ACLBodyR2V1(parsed) error = %v", err)
	}
	if !bytes.Equal(reencoded, wantBody) {
		t.Fatalf("re-encoded L2 ACL body = %x, want literal %x", reencoded, wantBody)
	}
	if _, err := ParseCanonicalAppACLL2ACLBodyR2V1(mustDecodeAppACLR2LiteralHex(t, appACLR2GoldenL2ACLReversedGrantsBodyHex)); err == nil || !strings.Contains(err.Error(), "strictly ordered") {
		t.Fatalf("ParseCanonicalAppACLL2ACLBodyR2V1(reversed literal) error = %v, want canonical-order rejection", err)
	}
}

func TestAppACLR2ReceiptPGCryptoIdentityInventoryMatchesFixedDigest(t *testing.T) {
	want := mustDecodeAppACLR2LiteralDigest(t, appACLR2PGCryptoIdentitySetDigestHex)
	if got := appACLR2PGCryptoIdentitySetDigest(appACLR2PGCryptoIdentityContract[:]); got != want {
		t.Fatalf("pgcrypto identity inventory digest = %x, want fixed %x", got, want)
	}
}

func TestAppACLR2ReceiptPGCryptoMember16UsesPG16FullOUTSignature(t *testing.T) {
	const memberIndex = 16
	if got := appACLR2PGCryptoIdentityContract[memberIndex]; got != appACLR2PGCryptoMember16PG16IdentityLiteral {
		t.Fatalf("pgcrypto member %d = %q, want PostgreSQL 16 identity %q", memberIndex, got, appACLR2PGCryptoMember16PG16IdentityLiteral)
	}
}

func TestAppACLR2ReceiptPGCryptoIdentityInventoryMatchesPG16LiteralDigest(t *testing.T) {
	want := mustDecodeAppACLR2LiteralDigest(t, appACLR2PGCryptoIdentitySetPG16DigestHex)
	if got := appACLR2PGCryptoIdentitySetDigest(appACLR2PGCryptoIdentityContract[:]); got != want {
		t.Fatalf("pgcrypto identity inventory digest = %x, want PostgreSQL 16 literal %x", got, want)
	}
}

func TestAppACLR2ReceiptRoundTripAndNestedTamperRejection(t *testing.T) {
	receipt := validAppACLR2BootstrapReceiptFixture(t)
	body, err := CanonicalAppACLR2BootstrapReceiptBodyV1(receipt)
	if err != nil {
		t.Fatalf("CanonicalAppACLR2BootstrapReceiptBodyV1() error = %v", err)
	}
	parsed, err := ParseCanonicalAppACLR2BootstrapReceiptBodyV1(body)
	if err != nil {
		t.Fatalf("ParseCanonicalAppACLR2BootstrapReceiptBodyV1() error = %v", err)
	}
	reencoded, err := CanonicalAppACLR2BootstrapReceiptBodyV1(parsed)
	if err != nil {
		t.Fatalf("CanonicalAppACLR2BootstrapReceiptBodyV1(parsed) error = %v", err)
	}
	if !bytes.Equal(reencoded, body) {
		t.Fatalf("receipt round trip changed bytes")
	}
	digest, err := AppACLR2BootstrapReceiptDigestV1(body)
	if err != nil {
		t.Fatalf("AppACLR2BootstrapReceiptDigestV1() error = %v", err)
	}
	if digest != sha256.Sum256(body) {
		t.Fatalf("receipt digest = %x, want SHA-256 of canonical body", digest)
	}

	tests := []struct {
		name   string
		mutate func(*AppACLR2BootstrapReceiptV1)
		want   string
	}{
		{name: "domain digest", mutate: func(value *AppACLR2BootstrapReceiptV1) { value.DomainDigest[0] ^= 0xff }, want: "domain digest"},
		{name: "L2 ACL digest", mutate: func(value *AppACLR2BootstrapReceiptV1) { value.L2ACLDigest[0] ^= 0xff }, want: "L2 ACL digest"},
		{name: "R1 source digest", mutate: func(value *AppACLR2BootstrapReceiptV1) { value.R1SourceDigest[0] ^= 0xff }, want: "R1 source"},
		{name: "R2 source digest", mutate: func(value *AppACLR2BootstrapReceiptV1) { value.R2SourceDigest[0] ^= 0xff }, want: "R2 source"},
		{name: "R1 privilege digest", mutate: func(value *AppACLR2BootstrapReceiptV1) { value.R1PrivilegeDigest[0] ^= 0xff }, want: "R1 privilege"},
		{name: "R2 privilege digest", mutate: func(value *AppACLR2BootstrapReceiptV1) { value.R2PrivilegeDigest[0] ^= 0xff }, want: "R2 privilege"},
		{name: "nested body swap", mutate: func(value *AppACLR2BootstrapReceiptV1) {
			value.DomainBody, value.L2ACLBody = value.L2ACLBody, value.DomainBody
			value.DomainDigest, value.L2ACLDigest = value.L2ACLDigest, value.DomainDigest
		}, want: "domain"},
		{name: "member schema", mutate: func(value *AppACLR2BootstrapReceiptV1) { value.Members[0].Schema = "public" }, want: "member"},
		{name: "member OID order", mutate: func(value *AppACLR2BootstrapReceiptV1) {
			value.Members[0], value.Members[1] = value.Members[1], value.Members[0]
		}, want: "strictly"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mutated := cloneAppACLR2BootstrapReceiptFixture(receipt)
			tt.mutate(&mutated)
			if _, err := CanonicalAppACLR2BootstrapReceiptBodyV1(mutated); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("CanonicalAppACLR2BootstrapReceiptBodyV1() error = %v, want %q rejection", err, tt.want)
			}
		})
	}

	trailing := append(append([]byte(nil), body...), 0)
	if _, err := ParseCanonicalAppACLR2BootstrapReceiptBodyV1(trailing); err == nil || !strings.Contains(err.Error(), "trailing") {
		t.Fatalf("ParseCanonicalAppACLR2BootstrapReceiptBodyV1(trailing) error = %v, want trailing-byte rejection", err)
	}
	malformedLength := append([]byte(nil), body...)
	firstBodyLengthOffset := len(appACLR2BootstrapReceiptMagic) + 2 + 2 + 2
	copy(malformedLength[firstBodyLengthOffset:firstBodyLengthOffset+4], []byte{0xff, 0xff, 0xff, 0xff})
	if _, err := ParseCanonicalAppACLR2BootstrapReceiptBodyV1(malformedLength); err == nil {
		t.Fatal("ParseCanonicalAppACLR2BootstrapReceiptBodyV1(oversized nested length) error = nil")
	}
}

func appACLR2GoldenDomainFixture() AppACLDomainR2V1 {
	return AppACLDomainR2V1{
		DomainID:                 "rd-15ca58e2c2c7daa3ca20f4e0c6f85af84254c9a675f2bb3092fcbd0739bf1a18",
		DomainKind:               "application",
		IdentityEpoch:            1,
		IdentityMode:             "postgres_system",
		PostgresSystemIdentifier: "72623859790382856",
		DatabaseOID:              424242,
		DatabaseName:             "houfeng_app",
	}
}

func appACLR2GoldenL2ACLFixture() AppACLControlACLBodyR2V1 {
	const internalSchema = "record_platform_internal"
	const receiptTable = "app_acl_r2_bootstrap_receipt"
	const assertIdentity = internalSchema + ".app_acl_r2_assert_bootstrap_receipt_insert(bytea, bytea)"
	const rejectIdentity = internalSchema + ".app_acl_r2_reject_bootstrap_receipt_mutation()"
	return AppACLControlACLBodyR2V1{
		Objects: []AppACLControlObjectR2V1{
			{
				Kind: AppACLControlObjectTableR2, Schema: "public", Identity: receiptTable,
				OwnerRole: AppACLControlRoleBootstrapSuperuserR2, OwnerOID: 10,
				ExplicitGrants: []AppACLControlGrantR2V1{
					{GranteeRole: AppACLControlRoleDirectMigratorR2, Privilege: AppACLControlPrivilegeSelectR2},
					{GranteeRole: AppACLControlRoleCenterRuntimeR2, Privilege: AppACLControlPrivilegeSelectR2},
				},
				EffectiveRelevantPrivilegeMask: 0x07,
			},
			{
				Kind: AppACLControlObjectFunctionR2, Schema: internalSchema, Identity: assertIdentity,
				OwnerRole: AppACLControlRoleBootstrapSuperuserR2, OwnerOID: 10,
				EffectiveRelevantPrivilegeMask: 0x01,
			},
			{
				Kind: AppACLControlObjectFunctionR2, Schema: internalSchema, Identity: rejectIdentity,
				OwnerRole: AppACLControlRoleBootstrapSuperuserR2, OwnerOID: 10,
				EffectiveRelevantPrivilegeMask: 0x01,
			},
		},
		Triggers: []AppACLControlTriggerR2V1{{
			TableSchema: "public", TableName: receiptTable, TriggerName: "app_acl_r2_bootstrap_receipt_immutable",
			FunctionSchema: internalSchema, FunctionIdentity: rejectIdentity,
			TableOwnerOID: 10, FunctionOwnerOID: 10, Enabled: true,
		}},
		DefaultACLAssertions: []AppACLDefaultACLAssertionR2V1{
			{OwnerRole: AppACLControlRoleBootstrapSuperuserR2, Kind: AppACLControlObjectTableR2, Namespace: AppACLDefaultACLNamespacePublicR2},
			{OwnerRole: AppACLControlRoleBootstrapSuperuserR2, Kind: AppACLControlObjectFunctionR2, Namespace: AppACLDefaultACLNamespaceRecordPlatformInternalR2},
		},
	}
}

func validAppACLR2BootstrapReceiptFixture(t *testing.T) AppACLR2BootstrapReceiptV1 {
	t.Helper()
	domainBody := mustDecodeAppACLR2LiteralHex(t, appACLR2GoldenDomainBodyHex)
	domainDigest := mustDecodeAppACLR2LiteralDigest(t, appACLR2GoldenDomainDigestHex)
	l2Body := mustDecodeAppACLR2LiteralHex(t, appACLR2GoldenL2ACLBodyHex)
	l2Digest := mustDecodeAppACLR2LiteralDigest(t, appACLR2GoldenL2ACLDigestHex)
	r1SourceBody, err := CanonicalMigrationSetBodyV1(appACLR1MigrationSourceContract[:])
	if err != nil {
		t.Fatalf("CanonicalMigrationSetBodyV1(frozen R1) error = %v", err)
	}
	r1PrivilegeBody, err := CompileAppACLPrivilegeSetR1("houfeng_app", []AppACLRoleBinding{
		{Subject: AppACLSubjectCenterRuntime, CatalogRole: "center_runtime"},
		{Subject: AppACLSubjectPlatformAdmin, CatalogRole: "platform_admin"},
	})
	if err != nil {
		t.Fatalf("CompileAppACLPrivilegeSetR1() error = %v", err)
	}
	r2SourceBody, err := CompileAppACLSourceSetR2V1()
	if err != nil {
		t.Fatalf("CompileAppACLSourceSetR2V1() error = %v", err)
	}
	r2PrivilegeBody, err := CompileAppACLPrivilegeSetR2V1("houfeng_app", []AppACLRoleBindingR2V1{
		{Subject: AppACLSubjectCenterRuntimeR2, CatalogRole: "center_runtime"},
		{Subject: AppACLSubjectDirectMigratorR2, CatalogRole: "direct_migrator"},
		{Subject: AppACLSubjectPlatformAdminR2, CatalogRole: "platform_admin"},
	})
	if err != nil {
		t.Fatalf("CompileAppACLPrivilegeSetR2V1() error = %v", err)
	}
	sourceEvidence, err := ReadAppACLR2SourceEvidenceV1()
	if err != nil {
		t.Fatalf("ReadAppACLR2SourceEvidenceV1() error = %v", err)
	}
	members := make([]AppACLR2ReceiptMemberV1, 0, len(appACLR2PGCryptoIdentityContract))
	for index, identity := range appACLR2PGCryptoIdentityContract {
		name, arguments, ok := strings.Cut(identity, "|")
		if !ok {
			t.Fatalf("invalid test pgcrypto identity %q", identity)
		}
		name = strings.TrimPrefix(name, "record_platform_internal.")
		members = append(members, AppACLR2ReceiptMemberV1{
			OID: uint32(1000 + index), Schema: "record_platform_internal", Name: name,
			IdentityArguments: arguments, OwnerName: "postgres", OwnerOID: 10,
		})
	}
	return AppACLR2BootstrapReceiptV1{
		R1SourceBody: r1SourceBody, R1SourceDigest: sha256.Sum256(r1SourceBody),
		R1PrivilegeBody: r1PrivilegeBody, R1PrivilegeDigest: sha256.Sum256(r1PrivilegeBody),
		R2SourceBody: r2SourceBody, R2SourceDigest: sha256.Sum256(r2SourceBody),
		R2PrivilegeBody: r2PrivilegeBody, R2PrivilegeDigest: sha256.Sum256(r2PrivilegeBody),
		R20052FullFileSHA256:     sourceEvidence.FullFileSHA256,
		R2BootstrapSectionSHA256: sourceEvidence.BootstrapSectionSHA256,
		R2FinalizeSectionSHA256:  sourceEvidence.FinalizeSectionSHA256,
		DomainBody:               domainBody, DomainDigest: domainDigest,
		Roles: []AppACLR2ReceiptRoleV1{
			{ControlRole: AppACLControlRoleBootstrapSuperuserR2, Name: "postgres", OID: 10, Login: true, Inherit: true, Superuser: true},
			{ControlRole: AppACLControlRoleDirectMigratorR2, Name: "direct_migrator", OID: 20, Login: true},
			{ControlRole: AppACLControlRoleCenterRuntimeR2, Name: "center_runtime", OID: 30, Login: true},
			{ControlRole: AppACLControlRolePlatformAdminR2, Name: "platform_admin", OID: 40, Login: true},
		},
		ServerVersionNum: 160006, ServerVersion: "16.6",
		ExtensionName: "pgcrypto", ExtensionOID: 500, ExtensionSchema: "record_platform_internal", ExtensionVersion: "1.3",
		ExtensionOwnerName: "direct_migrator", ExtensionOwnerOID: 20,
		IdentitySetSHA256: mustDecodeAppACLR2LiteralDigest(t, appACLR2PGCryptoIdentitySetDigestHex),
		Members:           members,
		ReceiptSchema:     "public", ReceiptTable: "app_acl_r2_bootstrap_receipt", ReceiptOwnerOID: 10, Singleton: true,
		HelperFunctions: []AppACLR2ReceiptHelperFunctionV1{
			{Schema: "record_platform_internal", Identity: "record_platform_internal.app_acl_r2_assert_bootstrap_receipt_insert(bytea, bytea)", OwnerOID: 10},
			{Schema: "record_platform_internal", Identity: "record_platform_internal.app_acl_r2_reject_bootstrap_receipt_mutation()", OwnerOID: 10},
		},
		ReceiptTriggers: append([]AppACLControlTriggerR2V1(nil), appACLR2GoldenL2ACLFixture().Triggers...),
		L2ACLBody:       l2Body, L2ACLDigest: l2Digest,
	}
}

func cloneAppACLR2BootstrapReceiptFixture(value AppACLR2BootstrapReceiptV1) AppACLR2BootstrapReceiptV1 {
	value.R1SourceBody = append([]byte(nil), value.R1SourceBody...)
	value.R1PrivilegeBody = append([]byte(nil), value.R1PrivilegeBody...)
	value.R2SourceBody = append([]byte(nil), value.R2SourceBody...)
	value.R2PrivilegeBody = append([]byte(nil), value.R2PrivilegeBody...)
	value.DomainBody = append([]byte(nil), value.DomainBody...)
	value.L2ACLBody = append([]byte(nil), value.L2ACLBody...)
	value.Roles = append([]AppACLR2ReceiptRoleV1(nil), value.Roles...)
	value.Members = append([]AppACLR2ReceiptMemberV1(nil), value.Members...)
	value.HelperFunctions = append([]AppACLR2ReceiptHelperFunctionV1(nil), value.HelperFunctions...)
	value.ReceiptTriggers = append([]AppACLControlTriggerR2V1(nil), value.ReceiptTriggers...)
	return value
}

func mustDecodeAppACLR2LiteralHex(t *testing.T, value string) []byte {
	t.Helper()
	decoded, err := hex.DecodeString(value)
	if err != nil {
		t.Fatalf("decode literal hex: %v", err)
	}
	return decoded
}

func mustDecodeAppACLR2LiteralDigest(t *testing.T, value string) [32]byte {
	t.Helper()
	decoded := mustDecodeAppACLR2LiteralHex(t, value)
	if len(decoded) != sha256.Size {
		t.Fatalf("literal digest length = %d, want %d", len(decoded), sha256.Size)
	}
	var digest [32]byte
	copy(digest[:], decoded)
	return digest
}
