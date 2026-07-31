package migrate

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	appaclr2migrations "houfeng/db/appaclr2/migrations"
)

const appACLR2DomainMagic = "HOUFENG-APP-ACL-R2-DOMAIN-V1"
const appACLR2L2ACLMagic = "HOUFENG-APP-ACL-R2-L2-ACL-V1"
const appACLR2BootstrapReceiptMagic = "HOUFENG-APP-ACL-R2-BOOTSTRAP-RECEIPT-V1"
const appACLR2BootstrapReceiptProtocol uint16 = 2
const appACLR2PGCryptoIdentitySetDigestHex = "57e7ac6a986705d8fa1e5b2260c1836b74dffe1b33bee00d65d1b275284e8196"

const appACLR2MigrationName = "0052_app_acl_r2_privileged_transition.sql"
const appACLR2BootstrapBeginMarker = "HOUFENG-APP-ACL-R2-BOOTSTRAP-BEGIN"
const appACLR2BootstrapEndMarker = "HOUFENG-APP-ACL-R2-BOOTSTRAP-END"
const appACLR2FinalizeBeginMarker = "HOUFENG-APP-ACL-R2-FINALIZE-BEGIN"
const appACLR2FinalizeEndMarker = "HOUFENG-APP-ACL-R2-FINALIZE-END"

var appACLR2PGCryptoIdentityContract = [...]string{
	"record_platform_internal.armor|bytea",
	"record_platform_internal.armor|bytea, text[], text[]",
	"record_platform_internal.crypt|text, text",
	"record_platform_internal.dearmor|text",
	"record_platform_internal.decrypt|bytea, bytea, text",
	"record_platform_internal.decrypt_iv|bytea, bytea, bytea, text",
	"record_platform_internal.digest|bytea, text",
	"record_platform_internal.digest|text, text",
	"record_platform_internal.encrypt|bytea, bytea, text",
	"record_platform_internal.encrypt_iv|bytea, bytea, bytea, text",
	"record_platform_internal.gen_random_bytes|integer",
	"record_platform_internal.gen_random_uuid|",
	"record_platform_internal.gen_salt|text",
	"record_platform_internal.gen_salt|text, integer",
	"record_platform_internal.hmac|bytea, bytea, text",
	"record_platform_internal.hmac|text, text, text",
	"record_platform_internal.pgp_armor_headers|text, OUT key text, OUT value text",
	"record_platform_internal.pgp_key_id|bytea",
	"record_platform_internal.pgp_pub_decrypt|bytea, bytea",
	"record_platform_internal.pgp_pub_decrypt|bytea, bytea, text",
	"record_platform_internal.pgp_pub_decrypt|bytea, bytea, text, text",
	"record_platform_internal.pgp_pub_decrypt_bytea|bytea, bytea",
	"record_platform_internal.pgp_pub_decrypt_bytea|bytea, bytea, text",
	"record_platform_internal.pgp_pub_decrypt_bytea|bytea, bytea, text, text",
	"record_platform_internal.pgp_pub_encrypt|text, bytea",
	"record_platform_internal.pgp_pub_encrypt|text, bytea, text",
	"record_platform_internal.pgp_pub_encrypt_bytea|bytea, bytea",
	"record_platform_internal.pgp_pub_encrypt_bytea|bytea, bytea, text",
	"record_platform_internal.pgp_sym_decrypt|bytea, text",
	"record_platform_internal.pgp_sym_decrypt|bytea, text, text",
	"record_platform_internal.pgp_sym_decrypt_bytea|bytea, text",
	"record_platform_internal.pgp_sym_decrypt_bytea|bytea, text, text",
	"record_platform_internal.pgp_sym_encrypt|text, text",
	"record_platform_internal.pgp_sym_encrypt|text, text, text",
	"record_platform_internal.pgp_sym_encrypt_bytea|bytea, text",
	"record_platform_internal.pgp_sym_encrypt_bytea|bytea, text, text",
}

// AppACLDomainR2V1 is the immutable application-domain identity nested in L2
// and M2 evidence. It contains catalog identity only, never a DSN or path.
type AppACLDomainR2V1 struct {
	DomainID                 string
	DomainKind               string
	IdentityEpoch            uint64
	IdentityMode             string
	PostgresSystemIdentifier string
	DatabaseOID              uint32
	DatabaseName             string
}

// AppACLR2SourceEvidenceV1 binds the isolated full source and both inclusive
// marker ranges used by bootstrap and finalization.
type AppACLR2SourceEvidenceV1 struct {
	FullFileSHA256         [32]byte
	BootstrapSectionSHA256 [32]byte
	FinalizeSectionSHA256  [32]byte
}

// AppACLR2ReceiptRoleV1 is one fixed control-role fact in receipt order.
type AppACLR2ReceiptRoleV1 struct {
	ControlRole              AppACLControlRoleR2
	Name                     string
	OID                      uint32
	Login                    bool
	Inherit                  bool
	Superuser                bool
	RecursiveMembershipCount uint16
}

// AppACLR2ReceiptMemberV1 is one extension member, ordered by PostgreSQL OID.
type AppACLR2ReceiptMemberV1 struct {
	OID               uint32
	Schema            string
	Name              string
	IdentityArguments string
	OwnerName         string
	OwnerOID          uint32
}

// AppACLR2ReceiptHelperFunctionV1 records one bootstrap helper identity.
type AppACLR2ReceiptHelperFunctionV1 struct {
	Schema   string
	Identity string
	OwnerOID uint32
}

// AppACLR2BootstrapReceiptV1 is the complete decoded immutable L2 receipt.
type AppACLR2BootstrapReceiptV1 struct {
	R1SourceBody             []byte
	R1SourceDigest           [32]byte
	R1PrivilegeBody          []byte
	R1PrivilegeDigest        [32]byte
	R2SourceBody             []byte
	R2SourceDigest           [32]byte
	R2PrivilegeBody          []byte
	R2PrivilegeDigest        [32]byte
	R20052FullFileSHA256     [32]byte
	R2BootstrapSectionSHA256 [32]byte
	R2FinalizeSectionSHA256  [32]byte
	DomainBody               []byte
	DomainDigest             [32]byte
	Roles                    []AppACLR2ReceiptRoleV1
	ServerVersionNum         uint32
	ServerVersion            string
	ExtensionName            string
	ExtensionOID             uint32
	ExtensionSchema          string
	ExtensionVersion         string
	ExtensionOwnerName       string
	ExtensionOwnerOID        uint32
	IdentitySetSHA256        [32]byte
	Members                  []AppACLR2ReceiptMemberV1
	ReceiptSchema            string
	ReceiptTable             string
	ReceiptOwnerOID          uint32
	Singleton                bool
	HelperFunctions          []AppACLR2ReceiptHelperFunctionV1
	ReceiptTriggers          []AppACLControlTriggerR2V1
	L2ACLBody                []byte
	L2ACLDigest              [32]byte
}

// CanonicalAppACLDomainR2BodyV1 emits the fixed application-domain grammar.
func CanonicalAppACLDomainR2BodyV1(domain AppACLDomainR2V1) ([]byte, error) {
	if err := validateAppACLR2Domain(domain); err != nil {
		return nil, err
	}
	body := make([]byte, 0, 256)
	body = append(body, appACLR2DomainMagic...)
	body = appendAppACLR2Uint16(body, appACLR2CodecVersion)
	body = appendAppACLR2String16(body, domain.DomainID)
	body = appendAppACLR2String16(body, domain.DomainKind)
	body = appendAppACLR2Uint64(body, domain.IdentityEpoch)
	body = appendAppACLR2String16(body, domain.IdentityMode)
	body = appendAppACLR2String16(body, domain.PostgresSystemIdentifier)
	body = appendAppACLR2Uint32(body, domain.DatabaseOID)
	body = appendAppACLR2String16(body, domain.DatabaseName)
	return body, nil
}

// ParseCanonicalAppACLDomainR2BodyV1 requires the fixed values, strict EOF,
// and byte-identical re-encoding.
func ParseCanonicalAppACLDomainR2BodyV1(body []byte) (AppACLDomainR2V1, error) {
	if len(body) > appACLR2MaximumBodyBytes || !bytes.HasPrefix(body, []byte(appACLR2DomainMagic)) {
		return AppACLDomainR2V1{}, fmt.Errorf("invalid APP ACL R2 domain magic or size")
	}
	decoder := appACLR2Decoder{body: body, offset: len(appACLR2DomainMagic)}
	version, err := decoder.uint16("domain version")
	if err != nil {
		return AppACLDomainR2V1{}, err
	}
	if version != appACLR2CodecVersion {
		return AppACLDomainR2V1{}, fmt.Errorf("APP ACL R2 domain version is %d, want %d", version, appACLR2CodecVersion)
	}
	domain := AppACLDomainR2V1{}
	if domain.DomainID, err = decoder.string16(1, 128, "domain ID"); err != nil {
		return AppACLDomainR2V1{}, err
	}
	if domain.DomainKind, err = decoder.string16(1, 32, "domain kind"); err != nil {
		return AppACLDomainR2V1{}, err
	}
	if domain.IdentityEpoch, err = decoder.uint64("identity epoch"); err != nil {
		return AppACLDomainR2V1{}, err
	}
	if domain.IdentityMode, err = decoder.string16(1, 32, "identity mode"); err != nil {
		return AppACLDomainR2V1{}, err
	}
	if domain.PostgresSystemIdentifier, err = decoder.string16(1, 128, "PostgreSQL system identifier"); err != nil {
		return AppACLDomainR2V1{}, err
	}
	if domain.DatabaseOID, err = decoder.uint32("database OID"); err != nil {
		return AppACLDomainR2V1{}, err
	}
	if domain.DatabaseName, err = decoder.string16(1, 63, "database name"); err != nil {
		return AppACLDomainR2V1{}, err
	}
	if err := decoder.requireEOF("domain"); err != nil {
		return AppACLDomainR2V1{}, err
	}
	reencoded, err := CanonicalAppACLDomainR2BodyV1(domain)
	if err != nil {
		return AppACLDomainR2V1{}, err
	}
	if !bytes.Equal(reencoded, body) {
		return AppACLDomainR2V1{}, fmt.Errorf("APP ACL R2 domain is not byte-canonical")
	}
	return domain, nil
}

// AppACLDomainR2DigestV1 validates then hashes one canonical domain body.
func AppACLDomainR2DigestV1(body []byte) ([32]byte, error) {
	if _, err := ParseCanonicalAppACLDomainR2BodyV1(body); err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(body), nil
}

func validateAppACLR2Domain(domain AppACLDomainR2V1) error {
	if len(domain.DomainID) != 67 || !strings.HasPrefix(domain.DomainID, "rd-") || !appACLR2LowerHex(domain.DomainID[3:]) {
		return fmt.Errorf("invalid APP ACL R2 domain ID")
	}
	if domain.DomainKind != "application" || domain.IdentityEpoch != 1 || domain.IdentityMode != "postgres_system" {
		return fmt.Errorf("APP ACL R2 domain is not the fixed application epoch-1 PostgreSQL identity")
	}
	systemIdentifier, err := strconv.ParseUint(domain.PostgresSystemIdentifier, 10, 64)
	if err != nil || systemIdentifier == 0 || strconv.FormatUint(systemIdentifier, 10) != domain.PostgresSystemIdentifier {
		return fmt.Errorf("invalid APP ACL R2 PostgreSQL system identifier")
	}
	if domain.DatabaseOID == 0 || !validAppACLR2RoleName(domain.DatabaseName) {
		return fmt.Errorf("invalid APP ACL R2 database identity")
	}
	return nil
}

func appACLR2LowerHex(value string) bool {
	for _, character := range value {
		if character < '0' || character > '9' {
			if character < 'a' || character > 'f' {
				return false
			}
		}
	}
	return true
}

// CanonicalAppACLL2ACLBodyR2V1 emits only the fixed bootstrap-owned receipt
// table, helper, trigger, grant, and default-ACL absence contract.
func CanonicalAppACLL2ACLBodyR2V1(value AppACLControlACLBodyR2V1) ([]byte, error) {
	if err := validateAppACLR2L2ACL(value); err != nil {
		return nil, err
	}
	return encodeAppACLR2L2ACL(value), nil
}

func encodeAppACLR2L2ACL(value AppACLControlACLBodyR2V1) []byte {
	body := make([]byte, 0, 640)
	body = append(body, appACLR2L2ACLMagic...)
	body = appendAppACLR2Uint16(body, appACLR2CodecVersion)
	body = appendAppACLR2Uint16(body, uint16(len(value.Objects)))
	for _, object := range value.Objects {
		body = append(body, byte(object.Kind))
		body = appendAppACLR2String16(body, object.Schema)
		body = appendAppACLR2String16(body, object.Identity)
		body = append(body, byte(object.OwnerRole))
		body = appendAppACLR2Uint32(body, object.OwnerOID)
		body = appendAppACLR2Uint16(body, uint16(len(object.ExplicitGrants)))
		for _, grant := range object.ExplicitGrants {
			body = append(body, byte(grant.GranteeRole), byte(grant.Privilege), 0)
		}
		body = append(body, object.EffectiveRelevantPrivilegeMask)
	}
	body = appendAppACLR2Uint16(body, uint16(len(value.Triggers)))
	for _, trigger := range value.Triggers {
		body = appendAppACLR2String16(body, trigger.TableSchema)
		body = appendAppACLR2String16(body, trigger.TableName)
		body = appendAppACLR2String16(body, trigger.TriggerName)
		body = appendAppACLR2String16(body, trigger.FunctionSchema)
		body = appendAppACLR2String16(body, trigger.FunctionIdentity)
		body = appendAppACLR2Uint32(body, trigger.TableOwnerOID)
		body = appendAppACLR2Uint32(body, trigger.FunctionOwnerOID)
		body = append(body, appACLR2BoolByte(trigger.Enabled))
	}
	body = appendAppACLR2Uint16(body, uint16(len(value.DefaultACLAssertions)))
	for _, assertion := range value.DefaultACLAssertions {
		body = append(body, byte(assertion.OwnerRole), byte(assertion.Kind), byte(assertion.Namespace))
	}
	return body
}

// ParseCanonicalAppACLL2ACLBodyR2V1 rejects malformed counts, ordering,
// grants, nested identities, and trailing bytes.
func ParseCanonicalAppACLL2ACLBodyR2V1(body []byte) (AppACLControlACLBodyR2V1, error) {
	if len(body) > appACLR2MaximumBodyBytes || !bytes.HasPrefix(body, []byte(appACLR2L2ACLMagic)) {
		return AppACLControlACLBodyR2V1{}, fmt.Errorf("invalid APP ACL R2 L2 ACL magic or size")
	}
	decoder := appACLR2Decoder{body: body, offset: len(appACLR2L2ACLMagic)}
	version, err := decoder.uint16("L2 ACL version")
	if err != nil {
		return AppACLControlACLBodyR2V1{}, err
	}
	if version != appACLR2CodecVersion {
		return AppACLControlACLBodyR2V1{}, fmt.Errorf("APP ACL R2 L2 ACL version is %d, want %d", version, appACLR2CodecVersion)
	}
	objectCount, err := decoder.uint16("L2 object count")
	if err != nil {
		return AppACLControlACLBodyR2V1{}, err
	}
	if objectCount != 3 {
		return AppACLControlACLBodyR2V1{}, fmt.Errorf("APP ACL R2 L2 ACL has %d objects, want 3", objectCount)
	}
	objects := make([]AppACLControlObjectR2V1, 0, objectCount)
	for objectIndex := range int(objectCount) {
		kind, err := decoder.uint8("L2 object kind")
		if err != nil {
			return AppACLControlACLBodyR2V1{}, err
		}
		schema, err := decoder.string16(1, 63, "L2 object schema")
		if err != nil {
			return AppACLControlACLBodyR2V1{}, err
		}
		identity, err := decoder.string16(1, 1024, "L2 object identity")
		if err != nil {
			return AppACLControlACLBodyR2V1{}, err
		}
		ownerRole, err := decoder.uint8("L2 object owner role")
		if err != nil {
			return AppACLControlACLBodyR2V1{}, err
		}
		ownerOID, err := decoder.uint32("L2 object owner OID")
		if err != nil {
			return AppACLControlACLBodyR2V1{}, err
		}
		grantCount, err := decoder.uint16("L2 object grant count")
		if err != nil {
			return AppACLControlACLBodyR2V1{}, err
		}
		if grantCount > 5 {
			return AppACLControlACLBodyR2V1{}, fmt.Errorf("APP ACL R2 L2 object %d has too many grants", objectIndex)
		}
		grants := make([]AppACLControlGrantR2V1, 0, grantCount)
		for grantIndex := range int(grantCount) {
			grantee, err := decoder.uint8("L2 grant grantee")
			if err != nil {
				return AppACLControlACLBodyR2V1{}, err
			}
			privilege, err := decoder.uint8("L2 grant privilege")
			if err != nil {
				return AppACLControlACLBodyR2V1{}, err
			}
			grantOption, err := decoder.uint8("L2 grant option")
			if err != nil {
				return AppACLControlACLBodyR2V1{}, err
			}
			if grantOption != 0 {
				return AppACLControlACLBodyR2V1{}, fmt.Errorf("APP ACL R2 L2 grant option must be zero")
			}
			grant := AppACLControlGrantR2V1{GranteeRole: AppACLControlRoleR2(grantee), Privilege: AppACLControlPrivilegeR2(privilege)}
			if grantIndex > 0 && compareAppACLR2ControlGrant(grants[grantIndex-1], grant) >= 0 {
				return AppACLControlACLBodyR2V1{}, fmt.Errorf("APP ACL R2 L2 grants are not strictly ordered")
			}
			grants = append(grants, grant)
		}
		mask, err := decoder.uint8("L2 effective relevant privilege mask")
		if err != nil {
			return AppACLControlACLBodyR2V1{}, err
		}
		object := AppACLControlObjectR2V1{
			Kind: AppACLControlObjectKindR2(kind), Schema: schema, Identity: identity,
			OwnerRole: AppACLControlRoleR2(ownerRole), OwnerOID: ownerOID,
			ExplicitGrants: grants, EffectiveRelevantPrivilegeMask: mask,
		}
		if objectIndex > 0 && compareAppACLR2ControlObject(objects[objectIndex-1], object) >= 0 {
			return AppACLControlACLBodyR2V1{}, fmt.Errorf("APP ACL R2 L2 objects are not strictly ordered")
		}
		objects = append(objects, object)
	}
	triggerCount, err := decoder.uint16("L2 trigger count")
	if err != nil {
		return AppACLControlACLBodyR2V1{}, err
	}
	if triggerCount != 1 {
		return AppACLControlACLBodyR2V1{}, fmt.Errorf("APP ACL R2 L2 ACL has %d triggers, want 1", triggerCount)
	}
	triggers := make([]AppACLControlTriggerR2V1, 0, triggerCount)
	for range int(triggerCount) {
		trigger, err := decodeAppACLR2ControlTrigger(&decoder)
		if err != nil {
			return AppACLControlACLBodyR2V1{}, err
		}
		triggers = append(triggers, trigger)
	}
	assertionCount, err := decoder.uint16("L2 default ACL assertion count")
	if err != nil {
		return AppACLControlACLBodyR2V1{}, err
	}
	if assertionCount != 2 {
		return AppACLControlACLBodyR2V1{}, fmt.Errorf("APP ACL R2 L2 ACL has %d default assertions, want 2", assertionCount)
	}
	assertions := make([]AppACLDefaultACLAssertionR2V1, 0, assertionCount)
	for assertionIndex := range int(assertionCount) {
		ownerRole, err := decoder.uint8("L2 default ACL owner role")
		if err != nil {
			return AppACLControlACLBodyR2V1{}, err
		}
		kind, err := decoder.uint8("L2 default ACL object kind")
		if err != nil {
			return AppACLControlACLBodyR2V1{}, err
		}
		namespace, err := decoder.uint8("L2 default ACL namespace")
		if err != nil {
			return AppACLControlACLBodyR2V1{}, err
		}
		assertion := AppACLDefaultACLAssertionR2V1{OwnerRole: AppACLControlRoleR2(ownerRole), Kind: AppACLControlObjectKindR2(kind), Namespace: AppACLDefaultACLNamespaceR2(namespace)}
		if assertionIndex > 0 && compareAppACLR2DefaultACLAssertion(assertions[assertionIndex-1], assertion) >= 0 {
			return AppACLControlACLBodyR2V1{}, fmt.Errorf("APP ACL R2 L2 default ACL assertions are not strictly ordered")
		}
		assertions = append(assertions, assertion)
	}
	if err := decoder.requireEOF("L2 ACL"); err != nil {
		return AppACLControlACLBodyR2V1{}, err
	}
	value := AppACLControlACLBodyR2V1{Objects: objects, Triggers: triggers, DefaultACLAssertions: assertions}
	reencoded, err := CanonicalAppACLL2ACLBodyR2V1(value)
	if err != nil {
		return AppACLControlACLBodyR2V1{}, err
	}
	if !bytes.Equal(reencoded, body) {
		return AppACLControlACLBodyR2V1{}, fmt.Errorf("APP ACL R2 L2 ACL is not byte-canonical")
	}
	return value, nil
}

func decodeAppACLR2ControlTrigger(decoder *appACLR2Decoder) (AppACLControlTriggerR2V1, error) {
	var trigger AppACLControlTriggerR2V1
	var err error
	if trigger.TableSchema, err = decoder.string16(1, 63, "trigger table schema"); err != nil {
		return AppACLControlTriggerR2V1{}, err
	}
	if trigger.TableName, err = decoder.string16(1, 63, "trigger table name"); err != nil {
		return AppACLControlTriggerR2V1{}, err
	}
	if trigger.TriggerName, err = decoder.string16(1, 63, "trigger name"); err != nil {
		return AppACLControlTriggerR2V1{}, err
	}
	if trigger.FunctionSchema, err = decoder.string16(1, 63, "trigger function schema"); err != nil {
		return AppACLControlTriggerR2V1{}, err
	}
	if trigger.FunctionIdentity, err = decoder.string16(1, 1024, "trigger function identity"); err != nil {
		return AppACLControlTriggerR2V1{}, err
	}
	if trigger.TableOwnerOID, err = decoder.uint32("trigger table owner OID"); err != nil {
		return AppACLControlTriggerR2V1{}, err
	}
	if trigger.FunctionOwnerOID, err = decoder.uint32("trigger function owner OID"); err != nil {
		return AppACLControlTriggerR2V1{}, err
	}
	enabled, err := decoder.uint8("trigger enabled")
	if err != nil {
		return AppACLControlTriggerR2V1{}, err
	}
	if enabled != 1 {
		return AppACLControlTriggerR2V1{}, fmt.Errorf("APP ACL R2 trigger enabled byte must be one")
	}
	trigger.Enabled = true
	return trigger, nil
}

// AppACLL2ACLDigestV1 validates then hashes one canonical L2 ACL body.
func AppACLL2ACLDigestV1(body []byte) ([32]byte, error) {
	if _, err := ParseCanonicalAppACLL2ACLBodyR2V1(body); err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(body), nil
}

func validateAppACLR2L2ACL(value AppACLControlACLBodyR2V1) error {
	if len(value.Objects) != 3 || len(value.Triggers) != 1 || len(value.DefaultACLAssertions) != 2 {
		return fmt.Errorf("APP ACL R2 L2 ACL count is %d/%d/%d, want 3/1/2", len(value.Objects), len(value.Triggers), len(value.DefaultACLAssertions))
	}
	for index, object := range value.Objects {
		if err := validateAppACLR2ControlObject(object); err != nil {
			return fmt.Errorf("validate APP ACL R2 L2 object %d: %w", index, err)
		}
		if index > 0 && compareAppACLR2ControlObject(value.Objects[index-1], object) >= 0 {
			return fmt.Errorf("APP ACL R2 L2 objects are not strictly ordered")
		}
	}
	for index, trigger := range value.Triggers {
		if err := validateAppACLR2ControlTrigger(trigger); err != nil {
			return fmt.Errorf("validate APP ACL R2 L2 trigger %d: %w", index, err)
		}
	}
	for index, assertion := range value.DefaultACLAssertions {
		if err := validateAppACLR2DefaultACLAssertion(assertion); err != nil {
			return fmt.Errorf("validate APP ACL R2 L2 default ACL assertion %d: %w", index, err)
		}
		if index > 0 && compareAppACLR2DefaultACLAssertion(value.DefaultACLAssertions[index-1], assertion) >= 0 {
			return fmt.Errorf("APP ACL R2 L2 default ACL assertions are not strictly ordered")
		}
	}
	if !equalAppACLR2ControlACL(value, appACLR2L2ACLContract()) {
		return fmt.Errorf("APP ACL R2 L2 ACL does not match the fixed receipt contract")
	}
	return nil
}

func appACLR2L2ACLContract() AppACLControlACLBodyR2V1 {
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
			{Kind: AppACLControlObjectFunctionR2, Schema: internalSchema, Identity: assertIdentity, OwnerRole: AppACLControlRoleBootstrapSuperuserR2, OwnerOID: 10, EffectiveRelevantPrivilegeMask: 0x01},
			{Kind: AppACLControlObjectFunctionR2, Schema: internalSchema, Identity: rejectIdentity, OwnerRole: AppACLControlRoleBootstrapSuperuserR2, OwnerOID: 10, EffectiveRelevantPrivilegeMask: 0x01},
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

// ReadAppACLR2SourceEvidenceV1 hashes the complete isolated SQL and both
// unique inclusive marker ranges. It does not read a path outside embed.FS.
func ReadAppACLR2SourceEvidenceV1() (AppACLR2SourceEvidenceV1, error) {
	payload, err := fs.ReadFile(appaclr2migrations.FS, appACLR2MigrationName)
	if err != nil {
		return AppACLR2SourceEvidenceV1{}, fmt.Errorf("read isolated APP ACL R2 source: %w", err)
	}
	bootstrap, err := appACLR2SourceSection(payload, appACLR2BootstrapBeginMarker, appACLR2BootstrapEndMarker)
	if err != nil {
		return AppACLR2SourceEvidenceV1{}, fmt.Errorf("read APP ACL R2 bootstrap section: %w", err)
	}
	finalize, err := appACLR2SourceSection(payload, appACLR2FinalizeBeginMarker, appACLR2FinalizeEndMarker)
	if err != nil {
		return AppACLR2SourceEvidenceV1{}, fmt.Errorf("read APP ACL R2 finalize section: %w", err)
	}
	return AppACLR2SourceEvidenceV1{
		FullFileSHA256:         sha256.Sum256(payload),
		BootstrapSectionSHA256: sha256.Sum256(bootstrap),
		FinalizeSectionSHA256:  sha256.Sum256(finalize),
	}, nil
}

func appACLR2SourceSection(payload []byte, beginMarker, endMarker string) ([]byte, error) {
	if bytes.Count(payload, []byte(beginMarker)) != 1 || bytes.Count(payload, []byte(endMarker)) != 1 {
		return nil, fmt.Errorf("source markers are not unique")
	}
	begin := bytes.Index(payload, []byte(beginMarker))
	endStart := bytes.Index(payload, []byte(endMarker))
	if begin < 0 || endStart < begin {
		return nil, fmt.Errorf("source marker order is invalid")
	}
	end := endStart + len(endMarker)
	if end >= len(payload) || payload[end] != '\n' {
		return nil, fmt.Errorf("source END marker has no terminal LF")
	}
	return payload[begin : end+1], nil
}

// CanonicalAppACLR2BootstrapReceiptBodyV1 validates every nested value and
// emits the immutable receipt preimage.
func CanonicalAppACLR2BootstrapReceiptBodyV1(receipt AppACLR2BootstrapReceiptV1) ([]byte, error) {
	if err := validateAppACLR2BootstrapReceipt(receipt); err != nil {
		return nil, err
	}
	body := make([]byte, 0, 16*1024)
	body = append(body, appACLR2BootstrapReceiptMagic...)
	body = appendAppACLR2Uint16(body, appACLR2CodecVersion)
	body = appendAppACLR2Uint16(body, appACLR2BootstrapReceiptProtocol)
	body = appendAppACLR2Uint16(body, 52)
	body = appendAppACLR2Body32(body, receipt.R1SourceBody)
	body = append(body, receipt.R1SourceDigest[:]...)
	body = appendAppACLR2Uint16(body, 204)
	body = appendAppACLR2Body32(body, receipt.R1PrivilegeBody)
	body = append(body, receipt.R1PrivilegeDigest[:]...)
	body = appendAppACLR2Uint16(body, 53)
	body = appendAppACLR2Body32(body, receipt.R2SourceBody)
	body = append(body, receipt.R2SourceDigest[:]...)
	body = appendAppACLR2Uint16(body, 206)
	body = appendAppACLR2Body32(body, receipt.R2PrivilegeBody)
	body = append(body, receipt.R2PrivilegeDigest[:]...)
	body = append(body, receipt.R20052FullFileSHA256[:]...)
	body = append(body, receipt.R2BootstrapSectionSHA256[:]...)
	body = append(body, receipt.R2FinalizeSectionSHA256[:]...)
	body = appendAppACLR2Body32(body, receipt.DomainBody)
	body = append(body, receipt.DomainDigest[:]...)
	body = appendAppACLR2Uint16(body, uint16(len(receipt.Roles)))
	for _, role := range receipt.Roles {
		body = append(body, byte(role.ControlRole))
		body = appendAppACLR2String16(body, role.Name)
		body = appendAppACLR2Uint32(body, role.OID)
		body = append(body, appACLR2ReceiptRoleFlags(role))
		body = appendAppACLR2Uint16(body, role.RecursiveMembershipCount)
	}
	body = appendAppACLR2Uint32(body, receipt.ServerVersionNum)
	body = appendAppACLR2String16(body, receipt.ServerVersion)
	body = appendAppACLR2String16(body, receipt.ExtensionName)
	body = appendAppACLR2Uint32(body, receipt.ExtensionOID)
	body = appendAppACLR2String16(body, receipt.ExtensionSchema)
	body = appendAppACLR2String16(body, receipt.ExtensionVersion)
	body = appendAppACLR2String16(body, receipt.ExtensionOwnerName)
	body = appendAppACLR2Uint32(body, receipt.ExtensionOwnerOID)
	body = append(body, receipt.IdentitySetSHA256[:]...)
	body = appendAppACLR2Uint16(body, uint16(len(receipt.Members)))
	for _, member := range receipt.Members {
		body = appendAppACLR2Uint32(body, member.OID)
		body = appendAppACLR2String16(body, member.Schema)
		body = appendAppACLR2String16(body, member.Name)
		body = appendAppACLR2String16(body, member.IdentityArguments)
		body = appendAppACLR2String16(body, member.OwnerName)
		body = appendAppACLR2Uint32(body, member.OwnerOID)
	}
	body = appendAppACLR2String16(body, receipt.ReceiptSchema)
	body = appendAppACLR2String16(body, receipt.ReceiptTable)
	body = appendAppACLR2Uint32(body, receipt.ReceiptOwnerOID)
	body = append(body, appACLR2BoolByte(receipt.Singleton))
	body = appendAppACLR2Uint16(body, uint16(len(receipt.HelperFunctions)))
	for _, helper := range receipt.HelperFunctions {
		body = appendAppACLR2String16(body, helper.Schema)
		body = appendAppACLR2String16(body, helper.Identity)
		body = appendAppACLR2Uint32(body, helper.OwnerOID)
	}
	body = appendAppACLR2Uint16(body, uint16(len(receipt.ReceiptTriggers)))
	for _, trigger := range receipt.ReceiptTriggers {
		body = appendAppACLR2String16(body, trigger.TableSchema)
		body = appendAppACLR2String16(body, trigger.TableName)
		body = appendAppACLR2String16(body, trigger.TriggerName)
		body = appendAppACLR2String16(body, trigger.FunctionSchema)
		body = appendAppACLR2String16(body, trigger.FunctionIdentity)
		body = appendAppACLR2Uint32(body, trigger.TableOwnerOID)
		body = appendAppACLR2Uint32(body, trigger.FunctionOwnerOID)
		body = append(body, appACLR2BoolByte(trigger.Enabled))
	}
	body = appendAppACLR2Body32(body, receipt.L2ACLBody)
	body = append(body, receipt.L2ACLDigest[:]...)
	if len(body) > appACLR2MaximumBodyBytes {
		return nil, fmt.Errorf("APP ACL R2 receipt exceeds %d bytes", appACLR2MaximumBodyBytes)
	}
	return body, nil
}

// ParseCanonicalAppACLR2BootstrapReceiptBodyV1 performs bounded allocation,
// nested parser/digest validation, strict EOF, and byte-canonical re-encoding.
func ParseCanonicalAppACLR2BootstrapReceiptBodyV1(body []byte) (AppACLR2BootstrapReceiptV1, error) {
	if len(body) > appACLR2MaximumBodyBytes || !bytes.HasPrefix(body, []byte(appACLR2BootstrapReceiptMagic)) {
		return AppACLR2BootstrapReceiptV1{}, fmt.Errorf("invalid APP ACL R2 bootstrap receipt magic or size")
	}
	decoder := appACLR2Decoder{body: body, offset: len(appACLR2BootstrapReceiptMagic)}
	version, err := decoder.uint16("receipt version")
	if err != nil {
		return AppACLR2BootstrapReceiptV1{}, err
	}
	if version != appACLR2CodecVersion {
		return AppACLR2BootstrapReceiptV1{}, fmt.Errorf("APP ACL R2 receipt version is %d, want %d", version, appACLR2CodecVersion)
	}
	protocol, err := decoder.uint16("receipt protocol")
	if err != nil {
		return AppACLR2BootstrapReceiptV1{}, err
	}
	if protocol != appACLR2BootstrapReceiptProtocol {
		return AppACLR2BootstrapReceiptV1{}, fmt.Errorf("APP ACL R2 receipt protocol is %d, want %d", protocol, appACLR2BootstrapReceiptProtocol)
	}
	var receipt AppACLR2BootstrapReceiptV1
	if err := decodeAppACLR2ReceiptNestedSets(&decoder, &receipt); err != nil {
		return AppACLR2BootstrapReceiptV1{}, err
	}
	if receipt.R20052FullFileSHA256, err = decoder.digest("0052 full-file digest"); err != nil {
		return AppACLR2BootstrapReceiptV1{}, err
	}
	if receipt.R2BootstrapSectionSHA256, err = decoder.digest("bootstrap section digest"); err != nil {
		return AppACLR2BootstrapReceiptV1{}, err
	}
	if receipt.R2FinalizeSectionSHA256, err = decoder.digest("finalize section digest"); err != nil {
		return AppACLR2BootstrapReceiptV1{}, err
	}
	if receipt.DomainBody, err = decoder.body32(1, appACLR2MaximumBodyBytes, "domain body"); err != nil {
		return AppACLR2BootstrapReceiptV1{}, err
	}
	if receipt.DomainDigest, err = decoder.digest("domain digest"); err != nil {
		return AppACLR2BootstrapReceiptV1{}, err
	}
	roleCount, err := decoder.uint16("receipt role count")
	if err != nil {
		return AppACLR2BootstrapReceiptV1{}, err
	}
	if roleCount != 4 {
		return AppACLR2BootstrapReceiptV1{}, fmt.Errorf("APP ACL R2 receipt has %d roles, want 4", roleCount)
	}
	receipt.Roles = make([]AppACLR2ReceiptRoleV1, 0, roleCount)
	for range int(roleCount) {
		role, err := decodeAppACLR2ReceiptRole(&decoder)
		if err != nil {
			return AppACLR2BootstrapReceiptV1{}, err
		}
		receipt.Roles = append(receipt.Roles, role)
	}
	if receipt.ServerVersionNum, err = decoder.uint32("server_version_num"); err != nil {
		return AppACLR2BootstrapReceiptV1{}, err
	}
	if receipt.ServerVersion, err = decoder.string16(1, 32, "server version"); err != nil {
		return AppACLR2BootstrapReceiptV1{}, err
	}
	if receipt.ExtensionName, err = decoder.string16(1, 63, "extension name"); err != nil {
		return AppACLR2BootstrapReceiptV1{}, err
	}
	if receipt.ExtensionOID, err = decoder.uint32("extension OID"); err != nil {
		return AppACLR2BootstrapReceiptV1{}, err
	}
	if receipt.ExtensionSchema, err = decoder.string16(1, 63, "extension schema"); err != nil {
		return AppACLR2BootstrapReceiptV1{}, err
	}
	if receipt.ExtensionVersion, err = decoder.string16(1, 16, "extension version"); err != nil {
		return AppACLR2BootstrapReceiptV1{}, err
	}
	if receipt.ExtensionOwnerName, err = decoder.string16(1, 63, "extension owner name"); err != nil {
		return AppACLR2BootstrapReceiptV1{}, err
	}
	if receipt.ExtensionOwnerOID, err = decoder.uint32("extension owner OID"); err != nil {
		return AppACLR2BootstrapReceiptV1{}, err
	}
	if receipt.IdentitySetSHA256, err = decoder.digest("identity-set digest"); err != nil {
		return AppACLR2BootstrapReceiptV1{}, err
	}
	memberCount, err := decoder.uint16("extension member count")
	if err != nil {
		return AppACLR2BootstrapReceiptV1{}, err
	}
	if memberCount != uint16(len(appACLR2PGCryptoIdentityContract)) {
		return AppACLR2BootstrapReceiptV1{}, fmt.Errorf("APP ACL R2 receipt has %d extension members, want 36", memberCount)
	}
	receipt.Members = make([]AppACLR2ReceiptMemberV1, 0, memberCount)
	for range int(memberCount) {
		member, err := decodeAppACLR2ReceiptMember(&decoder)
		if err != nil {
			return AppACLR2BootstrapReceiptV1{}, err
		}
		receipt.Members = append(receipt.Members, member)
	}
	if receipt.ReceiptSchema, err = decoder.string16(1, 63, "receipt schema"); err != nil {
		return AppACLR2BootstrapReceiptV1{}, err
	}
	if receipt.ReceiptTable, err = decoder.string16(1, 63, "receipt table"); err != nil {
		return AppACLR2BootstrapReceiptV1{}, err
	}
	if receipt.ReceiptOwnerOID, err = decoder.uint32("receipt owner OID"); err != nil {
		return AppACLR2BootstrapReceiptV1{}, err
	}
	singleton, err := decoder.uint8("receipt singleton")
	if err != nil {
		return AppACLR2BootstrapReceiptV1{}, err
	}
	if singleton != 1 {
		return AppACLR2BootstrapReceiptV1{}, fmt.Errorf("APP ACL R2 receipt singleton byte must be one")
	}
	receipt.Singleton = true
	helperCount, err := decoder.uint16("receipt helper count")
	if err != nil {
		return AppACLR2BootstrapReceiptV1{}, err
	}
	if helperCount != 2 {
		return AppACLR2BootstrapReceiptV1{}, fmt.Errorf("APP ACL R2 receipt has %d helpers, want 2", helperCount)
	}
	receipt.HelperFunctions = make([]AppACLR2ReceiptHelperFunctionV1, 0, helperCount)
	for range int(helperCount) {
		helper, err := decodeAppACLR2ReceiptHelper(&decoder)
		if err != nil {
			return AppACLR2BootstrapReceiptV1{}, err
		}
		receipt.HelperFunctions = append(receipt.HelperFunctions, helper)
	}
	triggerCount, err := decoder.uint16("receipt trigger count")
	if err != nil {
		return AppACLR2BootstrapReceiptV1{}, err
	}
	if triggerCount != 1 {
		return AppACLR2BootstrapReceiptV1{}, fmt.Errorf("APP ACL R2 receipt has %d triggers, want 1", triggerCount)
	}
	trigger, err := decodeAppACLR2ControlTrigger(&decoder)
	if err != nil {
		return AppACLR2BootstrapReceiptV1{}, err
	}
	receipt.ReceiptTriggers = []AppACLControlTriggerR2V1{trigger}
	if receipt.L2ACLBody, err = decoder.body32(1, appACLR2MaximumBodyBytes, "L2 ACL body"); err != nil {
		return AppACLR2BootstrapReceiptV1{}, err
	}
	if receipt.L2ACLDigest, err = decoder.digest("L2 ACL digest"); err != nil {
		return AppACLR2BootstrapReceiptV1{}, err
	}
	if err := decoder.requireEOF("bootstrap receipt"); err != nil {
		return AppACLR2BootstrapReceiptV1{}, err
	}
	reencoded, err := CanonicalAppACLR2BootstrapReceiptBodyV1(receipt)
	if err != nil {
		return AppACLR2BootstrapReceiptV1{}, err
	}
	if !bytes.Equal(reencoded, body) {
		return AppACLR2BootstrapReceiptV1{}, fmt.Errorf("APP ACL R2 bootstrap receipt is not byte-canonical")
	}
	return receipt, nil
}

func decodeAppACLR2ReceiptNestedSets(decoder *appACLR2Decoder, receipt *AppACLR2BootstrapReceiptV1) error {
	sets := []struct {
		name      string
		wantCount uint16
		body      *[]byte
		digest    *[32]byte
	}{
		{name: "R1 source", wantCount: 52, body: &receipt.R1SourceBody, digest: &receipt.R1SourceDigest},
		{name: "R1 privilege", wantCount: 204, body: &receipt.R1PrivilegeBody, digest: &receipt.R1PrivilegeDigest},
		{name: "R2 source", wantCount: 53, body: &receipt.R2SourceBody, digest: &receipt.R2SourceDigest},
		{name: "R2 privilege", wantCount: 206, body: &receipt.R2PrivilegeBody, digest: &receipt.R2PrivilegeDigest},
	}
	for _, set := range sets {
		count, err := decoder.uint16(set.name + " count")
		if err != nil {
			return err
		}
		if count != set.wantCount {
			return fmt.Errorf("APP ACL R2 receipt %s count is %d, want %d", set.name, count, set.wantCount)
		}
		*set.body, err = decoder.body32(1, appACLR2MaximumBodyBytes, set.name+" body")
		if err != nil {
			return err
		}
		*set.digest, err = decoder.digest(set.name + " digest")
		if err != nil {
			return err
		}
	}
	return nil
}

func decodeAppACLR2ReceiptRole(decoder *appACLR2Decoder) (AppACLR2ReceiptRoleV1, error) {
	controlRole, err := decoder.uint8("receipt control role")
	if err != nil {
		return AppACLR2ReceiptRoleV1{}, err
	}
	name, err := decoder.string16(1, 63, "receipt role name")
	if err != nil {
		return AppACLR2ReceiptRoleV1{}, err
	}
	oid, err := decoder.uint32("receipt role OID")
	if err != nil {
		return AppACLR2ReceiptRoleV1{}, err
	}
	flags, err := decoder.uint8("receipt role flags")
	if err != nil {
		return AppACLR2ReceiptRoleV1{}, err
	}
	if flags&^byte(0x07) != 0 {
		return AppACLR2ReceiptRoleV1{}, fmt.Errorf("APP ACL R2 receipt role flags use unknown bits")
	}
	membershipCount, err := decoder.uint16("receipt recursive membership count")
	if err != nil {
		return AppACLR2ReceiptRoleV1{}, err
	}
	return AppACLR2ReceiptRoleV1{
		ControlRole: AppACLControlRoleR2(controlRole), Name: name, OID: oid,
		Login: flags&0x01 != 0, Inherit: flags&0x02 != 0, Superuser: flags&0x04 != 0,
		RecursiveMembershipCount: membershipCount,
	}, nil
}

func decodeAppACLR2ReceiptMember(decoder *appACLR2Decoder) (AppACLR2ReceiptMemberV1, error) {
	var member AppACLR2ReceiptMemberV1
	var err error
	if member.OID, err = decoder.uint32("extension member OID"); err != nil {
		return AppACLR2ReceiptMemberV1{}, err
	}
	if member.Schema, err = decoder.string16(1, 63, "extension member schema"); err != nil {
		return AppACLR2ReceiptMemberV1{}, err
	}
	if member.Name, err = decoder.string16(1, 63, "extension member name"); err != nil {
		return AppACLR2ReceiptMemberV1{}, err
	}
	if member.IdentityArguments, err = decoder.string16(0, 512, "extension member identity arguments"); err != nil {
		return AppACLR2ReceiptMemberV1{}, err
	}
	if member.OwnerName, err = decoder.string16(1, 63, "extension member owner name"); err != nil {
		return AppACLR2ReceiptMemberV1{}, err
	}
	if member.OwnerOID, err = decoder.uint32("extension member owner OID"); err != nil {
		return AppACLR2ReceiptMemberV1{}, err
	}
	return member, nil
}

func decodeAppACLR2ReceiptHelper(decoder *appACLR2Decoder) (AppACLR2ReceiptHelperFunctionV1, error) {
	var helper AppACLR2ReceiptHelperFunctionV1
	var err error
	if helper.Schema, err = decoder.string16(1, 63, "receipt helper schema"); err != nil {
		return AppACLR2ReceiptHelperFunctionV1{}, err
	}
	if helper.Identity, err = decoder.string16(1, 1024, "receipt helper identity"); err != nil {
		return AppACLR2ReceiptHelperFunctionV1{}, err
	}
	if helper.OwnerOID, err = decoder.uint32("receipt helper owner OID"); err != nil {
		return AppACLR2ReceiptHelperFunctionV1{}, err
	}
	return helper, nil
}

// AppACLR2BootstrapReceiptDigestV1 validates then hashes a receipt body.
func AppACLR2BootstrapReceiptDigestV1(body []byte) ([32]byte, error) {
	if _, err := ParseCanonicalAppACLR2BootstrapReceiptBodyV1(body); err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(body), nil
}

func validateAppACLR2BootstrapReceipt(receipt AppACLR2BootstrapReceiptV1) error {
	domain, err := validateAppACLR2ReceiptNestedBodies(receipt)
	if err != nil {
		return err
	}
	roles, err := validateAppACLR2ReceiptRoles(receipt.Roles)
	if err != nil {
		return err
	}
	if err := validateAppACLR2ReceiptBindings(receipt, domain, roles); err != nil {
		return err
	}
	if !appACLR2AllowedServerVersion(receipt.ServerVersionNum) || !validAppACLR2Text(receipt.ServerVersion, 1, 32) {
		return fmt.Errorf("APP ACL R2 receipt has unsupported server_version_num or server version")
	}
	if receipt.ExtensionName != "pgcrypto" || receipt.ExtensionOID == 0 || receipt.ExtensionSchema != "record_platform_internal" || receipt.ExtensionVersion != "1.3" {
		return fmt.Errorf("APP ACL R2 receipt extension facts do not match pgcrypto 1.3")
	}
	directRole := roles[AppACLControlRoleDirectMigratorR2]
	if receipt.ExtensionOwnerName != directRole.Name || receipt.ExtensionOwnerOID != directRole.OID {
		return fmt.Errorf("APP ACL R2 receipt extension owner does not match direct migrator")
	}
	wantIdentityDigest, err := appACLR2DigestFromHex(appACLR2PGCryptoIdentitySetDigestHex)
	if err != nil {
		return err
	}
	if receipt.IdentitySetSHA256 != wantIdentityDigest {
		return fmt.Errorf("APP ACL R2 receipt identity-set digest does not match the fixed pgcrypto contract")
	}
	if err := validateAppACLR2ReceiptMembers(receipt.Members, roles[AppACLControlRoleBootstrapSuperuserR2]); err != nil {
		return err
	}
	if receipt.ReceiptSchema != "public" || receipt.ReceiptTable != "app_acl_r2_bootstrap_receipt" || receipt.ReceiptOwnerOID != 10 || !receipt.Singleton {
		return fmt.Errorf("APP ACL R2 receipt singleton identity is invalid")
	}
	if err := validateAppACLR2ReceiptHelpers(receipt.HelperFunctions); err != nil {
		return err
	}
	wantTrigger := appACLR2L2ACLContract().Triggers
	if len(receipt.ReceiptTriggers) != 1 || len(wantTrigger) != 1 || receipt.ReceiptTriggers[0] != wantTrigger[0] {
		return fmt.Errorf("APP ACL R2 receipt trigger does not match the fixed contract")
	}
	return nil
}

func validateAppACLR2ReceiptNestedBodies(receipt AppACLR2BootstrapReceiptV1) (AppACLDomainR2V1, error) {
	if sha256.Sum256(receipt.R1SourceBody) != receipt.R1SourceDigest {
		return AppACLDomainR2V1{}, fmt.Errorf("APP ACL R2 receipt R1 source digest mismatch")
	}
	r1Sources, err := ParseCanonicalMigrationSetBodyV1(receipt.R1SourceBody)
	if err != nil || len(r1Sources) != len(appACLR1MigrationSourceContract) {
		return AppACLDomainR2V1{}, fmt.Errorf("parse APP ACL R2 receipt R1 source body: %w", err)
	}
	for index := range r1Sources {
		if r1Sources[index] != appACLR1MigrationSourceContract[index] {
			return AppACLDomainR2V1{}, fmt.Errorf("APP ACL R2 receipt R1 source entry %d does not match frozen R1", index)
		}
	}
	if sha256.Sum256(receipt.R1PrivilegeBody) != receipt.R1PrivilegeDigest {
		return AppACLDomainR2V1{}, fmt.Errorf("APP ACL R2 receipt R1 privilege digest mismatch")
	}
	if _, err := ParseCanonicalPrivilegeSetBodyV1(receipt.R1PrivilegeBody); err != nil {
		return AppACLDomainR2V1{}, fmt.Errorf("parse APP ACL R2 receipt R1 privilege body: %w", err)
	}
	if sha256.Sum256(receipt.R2SourceBody) != receipt.R2SourceDigest {
		return AppACLDomainR2V1{}, fmt.Errorf("APP ACL R2 receipt R2 source digest mismatch")
	}
	if _, err := ParseCanonicalAppACLSourceSetR2BodyV1(receipt.R2SourceBody); err != nil {
		return AppACLDomainR2V1{}, fmt.Errorf("parse APP ACL R2 receipt R2 source body: %w", err)
	}
	if sha256.Sum256(receipt.R2PrivilegeBody) != receipt.R2PrivilegeDigest {
		return AppACLDomainR2V1{}, fmt.Errorf("APP ACL R2 receipt R2 privilege digest mismatch")
	}
	if _, err := ParseCanonicalAppACLPrivilegeSetR2BodyV1(receipt.R2PrivilegeBody); err != nil {
		return AppACLDomainR2V1{}, fmt.Errorf("parse APP ACL R2 receipt R2 privilege body: %w", err)
	}
	sourceEvidence, err := ReadAppACLR2SourceEvidenceV1()
	if err != nil {
		return AppACLDomainR2V1{}, err
	}
	if receipt.R20052FullFileSHA256 != sourceEvidence.FullFileSHA256 || receipt.R2BootstrapSectionSHA256 != sourceEvidence.BootstrapSectionSHA256 || receipt.R2FinalizeSectionSHA256 != sourceEvidence.FinalizeSectionSHA256 {
		return AppACLDomainR2V1{}, fmt.Errorf("APP ACL R2 receipt isolated source evidence does not match embedded 0052")
	}
	if sha256.Sum256(receipt.DomainBody) != receipt.DomainDigest {
		return AppACLDomainR2V1{}, fmt.Errorf("APP ACL R2 receipt domain digest mismatch")
	}
	domain, err := ParseCanonicalAppACLDomainR2BodyV1(receipt.DomainBody)
	if err != nil {
		return AppACLDomainR2V1{}, fmt.Errorf("parse APP ACL R2 receipt domain body: %w", err)
	}
	if sha256.Sum256(receipt.L2ACLBody) != receipt.L2ACLDigest {
		return AppACLDomainR2V1{}, fmt.Errorf("APP ACL R2 receipt L2 ACL digest mismatch")
	}
	l2ACL, err := ParseCanonicalAppACLL2ACLBodyR2V1(receipt.L2ACLBody)
	if err != nil {
		return AppACLDomainR2V1{}, fmt.Errorf("parse APP ACL R2 receipt L2 ACL body: %w", err)
	}
	if !equalAppACLR2ControlACL(l2ACL, appACLR2L2ACLContract()) {
		return AppACLDomainR2V1{}, fmt.Errorf("APP ACL R2 receipt L2 ACL does not match fixed contract")
	}
	return domain, nil
}

func validateAppACLR2ReceiptRoles(roles []AppACLR2ReceiptRoleV1) (map[AppACLControlRoleR2]AppACLR2ReceiptRoleV1, error) {
	if len(roles) != 4 {
		return nil, fmt.Errorf("APP ACL R2 receipt has %d roles, want 4", len(roles))
	}
	result := make(map[AppACLControlRoleR2]AppACLR2ReceiptRoleV1, len(roles))
	seenNames := make(map[string]struct{}, len(roles))
	seenOIDs := make(map[uint32]struct{}, len(roles))
	for index, role := range roles {
		wantTag := AppACLControlRoleR2(index + 1)
		if role.ControlRole != wantTag || !validAppACLR2RoleName(role.Name) || role.OID == 0 {
			return nil, fmt.Errorf("APP ACL R2 receipt role %d has invalid identity", index)
		}
		if role.RecursiveMembershipCount != 0 {
			return nil, fmt.Errorf("APP ACL R2 receipt role %q has recursive membership", role.Name)
		}
		if _, duplicate := seenNames[role.Name]; duplicate {
			return nil, fmt.Errorf("APP ACL R2 receipt role name %q is duplicated", role.Name)
		}
		if _, duplicate := seenOIDs[role.OID]; duplicate {
			return nil, fmt.Errorf("APP ACL R2 receipt role OID %d is duplicated", role.OID)
		}
		seenNames[role.Name] = struct{}{}
		seenOIDs[role.OID] = struct{}{}
		result[role.ControlRole] = role
	}
	bootstrap := roles[0]
	if bootstrap.OID != 10 || !bootstrap.Login || !bootstrap.Inherit || !bootstrap.Superuser {
		return nil, fmt.Errorf("APP ACL R2 bootstrap role must be OID 10 LOGIN INHERIT SUPERUSER")
	}
	for _, role := range roles[1:] {
		if !role.Login || role.Inherit || role.Superuser {
			return nil, fmt.Errorf("APP ACL R2 role %q must be constrained LOGIN NOINHERIT NOSUPERUSER", role.Name)
		}
	}
	return result, nil
}

func validateAppACLR2ReceiptBindings(receipt AppACLR2BootstrapReceiptV1, domain AppACLDomainR2V1, roles map[AppACLControlRoleR2]AppACLR2ReceiptRoleV1) error {
	r1, err := ParseCanonicalPrivilegeSetBodyV1(receipt.R1PrivilegeBody)
	if err != nil {
		return err
	}
	wantR1Bindings := []AppACLRoleBinding{
		{Subject: AppACLSubjectCenterRuntime, CatalogRole: roles[AppACLControlRoleCenterRuntimeR2].Name},
		{Subject: AppACLSubjectPlatformAdmin, CatalogRole: roles[AppACLControlRolePlatformAdminR2].Name},
	}
	wantR1, err := CompileAppACLPrivilegeSetR1(domain.DatabaseName, wantR1Bindings)
	if err != nil {
		return fmt.Errorf("compile APP ACL R2 receipt frozen R1 privilege contract: %w", err)
	}
	if !bytes.Equal(wantR1, receipt.R1PrivilegeBody) || len(r1.Privileges) != 204 {
		return fmt.Errorf("APP ACL R2 receipt R1 privilege body does not match frozen R1")
	}
	wantR2Bindings := []AppACLRoleBindingR2V1{
		{Subject: AppACLSubjectCenterRuntimeR2, CatalogRole: roles[AppACLControlRoleCenterRuntimeR2].Name},
		{Subject: AppACLSubjectDirectMigratorR2, CatalogRole: roles[AppACLControlRoleDirectMigratorR2].Name},
		{Subject: AppACLSubjectPlatformAdminR2, CatalogRole: roles[AppACLControlRolePlatformAdminR2].Name},
	}
	wantR2, err := CompileAppACLPrivilegeSetR2V1(domain.DatabaseName, wantR2Bindings)
	if err != nil {
		return fmt.Errorf("compile APP ACL R2 receipt R2 privilege contract: %w", err)
	}
	if !bytes.Equal(wantR2, receipt.R2PrivilegeBody) {
		return fmt.Errorf("APP ACL R2 receipt R2 privilege body does not match fixed R2")
	}
	return nil
}

func validateAppACLR2ReceiptMembers(members []AppACLR2ReceiptMemberV1, bootstrap AppACLR2ReceiptRoleV1) error {
	if len(members) != len(appACLR2PGCryptoIdentityContract) {
		return fmt.Errorf("APP ACL R2 receipt has %d pgcrypto members, want 36", len(members))
	}
	identities := make([]string, 0, len(members))
	for index, member := range members {
		if member.OID == 0 || index > 0 && members[index-1].OID >= member.OID {
			return fmt.Errorf("APP ACL R2 receipt member OIDs are not strictly ordered")
		}
		if member.Schema != "record_platform_internal" || !validAppACLR2RoleName(member.Name) || !validAppACLR2Text(member.IdentityArguments, 0, 512) {
			return fmt.Errorf("APP ACL R2 receipt member %d has invalid identity", index)
		}
		if member.OwnerOID != 10 || member.OwnerOID != bootstrap.OID || member.OwnerName != bootstrap.Name {
			return fmt.Errorf("APP ACL R2 receipt member %d owner does not match bootstrap OID 10", index)
		}
		identities = append(identities, member.Schema+"."+member.Name+"|"+member.IdentityArguments)
	}
	sort.Strings(identities)
	wantIdentities := append([]string(nil), appACLR2PGCryptoIdentityContract[:]...)
	sort.Strings(wantIdentities)
	for index, identity := range identities {
		if identity != wantIdentities[index] {
			return fmt.Errorf("APP ACL R2 receipt pgcrypto identity set does not match fixed member %d", index)
		}
	}
	wantDigest, err := appACLR2DigestFromHex(appACLR2PGCryptoIdentitySetDigestHex)
	if err != nil {
		return fmt.Errorf("decode fixed APP ACL R2 pgcrypto identity-set digest: %w", err)
	}
	if appACLR2PGCryptoIdentitySetDigest(identities) != wantDigest {
		return fmt.Errorf("APP ACL R2 receipt pgcrypto identity-set digest does not match the fixed contract")
	}
	return nil
}

func appACLR2PGCryptoIdentitySetDigest(identities []string) [32]byte {
	sortedIdentities := append([]string(nil), identities...)
	sort.Strings(sortedIdentities)
	return sha256.Sum256([]byte(strings.Join(sortedIdentities, "\n") + "\n"))
}

func validateAppACLR2ReceiptHelpers(helpers []AppACLR2ReceiptHelperFunctionV1) error {
	want := []AppACLR2ReceiptHelperFunctionV1{
		{Schema: "record_platform_internal", Identity: "record_platform_internal.app_acl_r2_assert_bootstrap_receipt_insert(bytea, bytea)", OwnerOID: 10},
		{Schema: "record_platform_internal", Identity: "record_platform_internal.app_acl_r2_reject_bootstrap_receipt_mutation()", OwnerOID: 10},
	}
	if len(helpers) != len(want) {
		return fmt.Errorf("APP ACL R2 receipt has %d helper functions, want 2", len(helpers))
	}
	for index := range helpers {
		if helpers[index] != want[index] {
			return fmt.Errorf("APP ACL R2 receipt helper %d does not match fixed identity", index)
		}
	}
	return nil
}

func appACLR2AllowedServerVersion(version uint32) bool {
	return version == 160000 || version == 160006 || version == 160012
}

func appACLR2ReceiptRoleFlags(role AppACLR2ReceiptRoleV1) byte {
	var flags byte
	if role.Login {
		flags |= 0x01
	}
	if role.Inherit {
		flags |= 0x02
	}
	if role.Superuser {
		flags |= 0x04
	}
	return flags
}

func appACLR2BoolByte(value bool) byte {
	if value {
		return 1
	}
	return 0
}
