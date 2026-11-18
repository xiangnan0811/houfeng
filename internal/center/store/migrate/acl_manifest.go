package migrate

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const canonicalMigrationSetMagic = "HOUFENG-APP-MIGRATION-SET-V1"
const canonicalPrivilegeSetMagic = "HOUFENG-APP-PRIVILEGE-SET-V1"
const canonicalPrivilegeSetMagicV2 = "HOUFENG-APP-PRIVILEGE-SET-V2"
const appACLManifestMagic = "HOUFENG-APP-ACL-MANIFEST-V1"
const maxCanonicalACLManifestBodyBytes = 4 * 1024 * 1024

// MigrationChecksumEntry is one filename/checksum pair in the application
// migration ledger. Its checksum is raw SHA-256 bytes, not a hex string.
type MigrationChecksumEntry struct {
	Filename string
	Checksum [32]byte
}

// CanonicalMigrationSetFromFS builds the complete canonical migration set for
// one embedded migration source.
func CanonicalMigrationSetFromFS(fsys fs.FS) ([]byte, error) {
	sources, err := migrationSources(fsys)
	if err != nil {
		return nil, err
	}
	entries := make([]MigrationChecksumEntry, 0, len(sources))
	for filename, source := range sources {
		checksumBytes, err := hex.DecodeString(source.checksum)
		if err != nil {
			return nil, fmt.Errorf("decode migration checksum for %q: %w", filename, err)
		}
		if len(checksumBytes) != 32 {
			return nil, fmt.Errorf("migration checksum for %q has length %d, want 32", filename, len(checksumBytes))
		}
		var checksum [32]byte
		copy(checksum[:], checksumBytes)
		entries = append(entries, MigrationChecksumEntry{Filename: filename, Checksum: checksum})
	}
	return CanonicalMigrationSetBodyV1(entries)
}

// CanonicalMigrationSetBodyV1 encodes the complete application migration set
// in its fixed v1 form. The output order is raw filename-byte order.
func CanonicalMigrationSetBodyV1(entries []MigrationChecksumEntry) ([]byte, error) {
	if len(entries) == 0 {
		return nil, fmt.Errorf("canonical migration set has no entries")
	}

	sorted := append([]MigrationChecksumEntry(nil), entries...)
	for _, entry := range sorted {
		if err := validateMigrationFilename(entry.Filename); err != nil {
			return nil, err
		}
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Filename < sorted[j].Filename
	})
	for i := 1; i < len(sorted); i++ {
		if sorted[i-1].Filename == sorted[i].Filename {
			return nil, fmt.Errorf("duplicate migration filename %q", sorted[i].Filename)
		}
	}

	size := len(canonicalMigrationSetMagic)
	for _, entry := range sorted {
		size += 4 + len(entry.Filename) + len(entry.Checksum)
	}
	body := make([]byte, 0, size)
	body = append(body, canonicalMigrationSetMagic...)
	for _, entry := range sorted {
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(entry.Filename)))
		body = append(body, length[:]...)
		body = append(body, entry.Filename...)
		body = append(body, entry.Checksum[:]...)
	}
	return body, nil
}

// ParseCanonicalMigrationSetBodyV1 accepts only fully canonical v1 migration
// set bytes. It rejects duplicate, unsorted, malformed, and trailing data.
func ParseCanonicalMigrationSetBodyV1(body []byte) ([]MigrationChecksumEntry, error) {
	if !bytes.HasPrefix(body, []byte(canonicalMigrationSetMagic)) {
		return nil, fmt.Errorf("invalid canonical migration set magic")
	}
	offset := len(canonicalMigrationSetMagic)
	if offset == len(body) {
		return nil, fmt.Errorf("canonical migration set has no entries")
	}

	entries := make([]MigrationChecksumEntry, 0)
	previousFilename := ""
	for offset < len(body) {
		if len(body)-offset < 4 {
			return nil, fmt.Errorf("truncated canonical migration filename length")
		}
		filenameLength := int(binary.BigEndian.Uint32(body[offset : offset+4]))
		offset += 4
		if filenameLength < 1 || filenameLength > 255 || len(body)-offset < filenameLength+32 {
			return nil, fmt.Errorf("invalid canonical migration filename length")
		}

		filename := string(body[offset : offset+filenameLength])
		offset += filenameLength
		if err := validateMigrationFilename(filename); err != nil {
			return nil, err
		}
		if previousFilename != "" && previousFilename >= filename {
			return nil, fmt.Errorf("canonical migration filenames are not strictly sorted")
		}

		var checksum [32]byte
		copy(checksum[:], body[offset:offset+len(checksum)])
		offset += len(checksum)
		entries = append(entries, MigrationChecksumEntry{Filename: filename, Checksum: checksum})
		previousFilename = filename
	}
	return entries, nil
}

func validateMigrationFilename(filename string) error {
	if len(filename) < 1 || len(filename) > 255 || !utf8.ValidString(filename) {
		return fmt.Errorf("invalid migration filename")
	}
	if !strings.HasSuffix(filename, ".sql") || strings.ContainsAny(filename, "/\\\x00") {
		return fmt.Errorf("invalid migration filename %q", filename)
	}
	return nil
}

// AppACLSubject is a semantic subject in the application ACL manifest.
type AppACLSubject string

const (
	AppACLSubjectCenterRuntime AppACLSubject = "center_runtime"
	AppACLSubjectPlatformAdmin AppACLSubject = "platform_admin"
	// AppACLSubjectRecordDeletionWorker is the mandatory third subject of
	// the v2 privilege body. It never appears in a v1 body.
	AppACLSubjectRecordDeletionWorker AppACLSubject = "record_deletion_worker"
)

// AppACLPrivilegeSetVersion identifies one canonical privilege-body format.
type AppACLPrivilegeSetVersion uint8

const (
	AppACLPrivilegeSetV1 AppACLPrivilegeSetVersion = 1
	AppACLPrivilegeSetV2 AppACLPrivilegeSetVersion = 2
)

// appACLPrivilegeSetSubjectsV1 and appACLPrivilegeSetSubjectsV2 are the exact
// binding subjects, in canonical byte order, admitted by each body version.
var appACLPrivilegeSetSubjectsV1 = []AppACLSubject{
	AppACLSubjectCenterRuntime,
	AppACLSubjectPlatformAdmin,
}

var appACLPrivilegeSetSubjectsV2 = []AppACLSubject{
	AppACLSubjectCenterRuntime,
	AppACLSubjectPlatformAdmin,
	AppACLSubjectRecordDeletionWorker,
}

// AppACLObjectClass identifies the PostgreSQL catalog surface in a privilege
// tuple. Column entries remain decodable for catalog checks but v1 grants none.
type AppACLObjectClass string

const (
	AppACLObjectClassDatabase AppACLObjectClass = "database"
	AppACLObjectClassSchema   AppACLObjectClass = "schema"
	AppACLObjectClassTable    AppACLObjectClass = "table"
	AppACLObjectClassView     AppACLObjectClass = "view"
	AppACLObjectClassColumn   AppACLObjectClass = "column"
	AppACLObjectClassSequence AppACLObjectClass = "sequence"
	AppACLObjectClassFunction AppACLObjectClass = "function"
)

// AppACLPrivilegeKind is one closed PostgreSQL privilege token.
type AppACLPrivilegeKind string

const (
	AppACLPrivilegeConnect AppACLPrivilegeKind = "CONNECT"
	AppACLPrivilegeUsage   AppACLPrivilegeKind = "USAGE"
	AppACLPrivilegeSelect  AppACLPrivilegeKind = "SELECT"
	AppACLPrivilegeInsert  AppACLPrivilegeKind = "INSERT"
	AppACLPrivilegeUpdate  AppACLPrivilegeKind = "UPDATE"
	AppACLPrivilegeDelete  AppACLPrivilegeKind = "DELETE"
	AppACLPrivilegeExecute AppACLPrivilegeKind = "EXECUTE"
)

// AppACLRoleBinding pins a semantic manifest subject to one catalog role.
type AppACLRoleBinding struct {
	Subject     AppACLSubject
	CatalogRole string
}

// AppACLPrivilege is one exact catalog privilege tuple. Function identities
// are stored wholly in ObjectIdentity (for example, public.name(bytea)).
type AppACLPrivilege struct {
	Subject        AppACLSubject
	ObjectClass    AppACLObjectClass
	SchemaName     string
	ObjectIdentity string
	ColumnName     string
	Privilege      AppACLPrivilegeKind
	GrantOption    bool
}

// AppACLPrivilegeSet is the fully decoded canonical privilege-set body.
type AppACLPrivilegeSet struct {
	RoleBindings []AppACLRoleBinding
	Privileges   []AppACLPrivilege
}

// CanonicalPrivilegeSetBodyV1 serializes the fixed application privilege set.
// It accepts only the two semantic subjects, normalizes ordering, and rejects
// grants that cannot be represented by the v1 catalog contract.
func CanonicalPrivilegeSetBodyV1(bindings []AppACLRoleBinding, privileges []AppACLPrivilege) ([]byte, error) {
	return encodeCanonicalPrivilegeSetBody(canonicalPrivilegeSetMagic, appACLPrivilegeSetSubjectsV1, bindings, privileges)
}

// CanonicalPrivilegeSetBodyV2 serializes the mandatory-worker privilege set.
// It uses the v1 framing under a distinct magic and accepts exactly the
// runtime, platform-admin and record-deletion-worker bindings.
func CanonicalPrivilegeSetBodyV2(bindings []AppACLRoleBinding, privileges []AppACLPrivilege) ([]byte, error) {
	return encodeCanonicalPrivilegeSetBody(canonicalPrivilegeSetMagicV2, appACLPrivilegeSetSubjectsV2, bindings, privileges)
}

// canonicalPrivilegeSetBodyForBindings selects the only body version that
// can represent the supplied binding set: two bindings are the historical v1
// contract, three bindings are the mandatory-worker v2 contract.
func canonicalPrivilegeSetBodyForBindings(bindings []AppACLRoleBinding, privileges []AppACLPrivilege) ([]byte, error) {
	switch len(bindings) {
	case len(appACLPrivilegeSetSubjectsV1):
		return CanonicalPrivilegeSetBodyV1(bindings, privileges)
	case len(appACLPrivilegeSetSubjectsV2):
		return CanonicalPrivilegeSetBodyV2(bindings, privileges)
	default:
		return nil, fmt.Errorf("canonical privilege set has %d role bindings, want 2 (v1) or 3 (v2)", len(bindings))
	}
}

func encodeCanonicalPrivilegeSetBody(
	magic string,
	subjects []AppACLSubject,
	bindings []AppACLRoleBinding,
	privileges []AppACLPrivilege,
) ([]byte, error) {
	sortedBindings, err := canonicalRoleBindingsForSubjects(bindings, subjects)
	if err != nil {
		return nil, err
	}
	sortedPrivileges, err := canonicalPrivileges(privileges)
	if err != nil {
		return nil, err
	}
	for _, privilege := range sortedPrivileges {
		if !appACLSubjectAdmitted(privilege.Subject, subjects) {
			return nil, fmt.Errorf("ACL subject %q is not bound by this privilege set version", privilege.Subject)
		}
		if magic == canonicalPrivilegeSetMagic && privilege.ObjectClass == AppACLObjectClassFunction &&
			!validFunctionIdentity(privilege.ObjectIdentity) {
			return nil, fmt.Errorf("function ACL identity requires privilege set v2")
		}
	}

	size := len(magic) + 4 + 4
	for _, binding := range sortedBindings {
		size += 4 + len(binding.Subject) + 4 + len(binding.CatalogRole)
	}
	for _, privilege := range sortedPrivileges {
		size += 4 + len(privilege.Subject) + 4 + len(privilege.ObjectClass) +
			4 + len(privilege.SchemaName) + 4 + len(privilege.ObjectIdentity) +
			4 + len(privilege.ColumnName) + 4 + len(privilege.Privilege) + 1
	}

	body := make([]byte, 0, size)
	body = append(body, magic...)
	body = appendUint32(body, uint32(len(sortedBindings)))
	for _, binding := range sortedBindings {
		body = appendLengthPrefixedString(body, string(binding.Subject))
		body = appendLengthPrefixedString(body, binding.CatalogRole)
	}
	body = appendUint32(body, uint32(len(sortedPrivileges)))
	for _, privilege := range sortedPrivileges {
		body = appendLengthPrefixedString(body, string(privilege.Subject))
		body = appendLengthPrefixedString(body, string(privilege.ObjectClass))
		body = appendLengthPrefixedString(body, privilege.SchemaName)
		body = appendLengthPrefixedString(body, privilege.ObjectIdentity)
		body = appendLengthPrefixedString(body, privilege.ColumnName)
		body = appendLengthPrefixedString(body, string(privilege.Privilege))
		body = append(body, 0)
	}
	return body, nil
}

// ParseCanonicalPrivilegeSetBodyV1 accepts only canonical v1 privilege body
// bytes. It rejects duplicate or non-sorted tuples and any trailing bytes.
func ParseCanonicalPrivilegeSetBodyV1(body []byte) (AppACLPrivilegeSet, error) {
	return parseCanonicalPrivilegeSetBody(body, canonicalPrivilegeSetMagic, appACLPrivilegeSetSubjectsV1)
}

// ParseCanonicalPrivilegeSetBodyV2 accepts only canonical v2 privilege body
// bytes: exactly the runtime, platform-admin and record-deletion-worker
// bindings in that order, distinct roles, no grant option, no trailing bytes.
func ParseCanonicalPrivilegeSetBodyV2(body []byte) (AppACLPrivilegeSet, error) {
	return parseCanonicalPrivilegeSetBody(body, canonicalPrivilegeSetMagicV2, appACLPrivilegeSetSubjectsV2)
}

// parseCanonicalPrivilegeSetBodyAnyVersion dispatches strictly on the body
// magic. It is the only reader for persisted manifest bodies whose version is
// not fixed by the caller.
func parseCanonicalPrivilegeSetBodyAnyVersion(body []byte) (AppACLPrivilegeSet, AppACLPrivilegeSetVersion, error) {
	switch {
	case bytes.HasPrefix(body, []byte(canonicalPrivilegeSetMagic)):
		set, err := ParseCanonicalPrivilegeSetBodyV1(body)
		return set, AppACLPrivilegeSetV1, err
	case bytes.HasPrefix(body, []byte(canonicalPrivilegeSetMagicV2)):
		set, err := ParseCanonicalPrivilegeSetBodyV2(body)
		return set, AppACLPrivilegeSetV2, err
	default:
		return AppACLPrivilegeSet{}, 0, fmt.Errorf("invalid canonical privilege set magic")
	}
}

func parseCanonicalPrivilegeSetBody(body []byte, magic string, subjects []AppACLSubject) (AppACLPrivilegeSet, error) {
	if !bytes.HasPrefix(body, []byte(magic)) {
		return AppACLPrivilegeSet{}, fmt.Errorf("invalid canonical privilege set magic")
	}
	offset := len(magic)
	bindingCount, err := readCanonicalUint32(body, &offset, "role binding count")
	if err != nil {
		return AppACLPrivilegeSet{}, err
	}
	if bindingCount != uint32(len(subjects)) {
		return AppACLPrivilegeSet{}, fmt.Errorf("canonical privilege set has %d role bindings, want %d", bindingCount, len(subjects))
	}
	bindings := make([]AppACLRoleBinding, 0, bindingCount)
	for range bindingCount {
		subject, err := readCanonicalString(body, &offset, 64, "role binding subject")
		if err != nil {
			return AppACLPrivilegeSet{}, err
		}
		catalogRole, err := readCanonicalString(body, &offset, 63, "catalog role")
		if err != nil {
			return AppACLPrivilegeSet{}, err
		}
		bindings = append(bindings, AppACLRoleBinding{Subject: AppACLSubject(subject), CatalogRole: catalogRole})
	}
	if _, err := canonicalRoleBindingsForSubjects(bindings, subjects); err != nil {
		return AppACLPrivilegeSet{}, err
	}
	for index, binding := range bindings {
		if binding.Subject != subjects[index] {
			return AppACLPrivilegeSet{}, fmt.Errorf("canonical privilege role bindings are not sorted")
		}
	}

	privilegeCount, err := readCanonicalUint32(body, &offset, "privilege count")
	if err != nil {
		return AppACLPrivilegeSet{}, err
	}
	if privilegeCount > 65536 {
		return AppACLPrivilegeSet{}, fmt.Errorf("canonical privilege set has too many privileges")
	}
	privileges := make([]AppACLPrivilege, 0, privilegeCount)
	for range privilegeCount {
		subject, err := readCanonicalString(body, &offset, 64, "privilege subject")
		if err != nil {
			return AppACLPrivilegeSet{}, err
		}
		objectClass, err := readCanonicalString(body, &offset, 64, "privilege object class")
		if err != nil {
			return AppACLPrivilegeSet{}, err
		}
		schemaName, err := readCanonicalString(body, &offset, 63, "privilege schema name")
		if err != nil {
			return AppACLPrivilegeSet{}, err
		}
		objectIdentity, err := readCanonicalString(body, &offset, 256, "privilege object identity")
		if err != nil {
			return AppACLPrivilegeSet{}, err
		}
		columnName, err := readCanonicalString(body, &offset, 63, "privilege column name")
		if err != nil {
			return AppACLPrivilegeSet{}, err
		}
		privilegeKind, err := readCanonicalString(body, &offset, 16, "privilege kind")
		if err != nil {
			return AppACLPrivilegeSet{}, err
		}
		if offset >= len(body) {
			return AppACLPrivilegeSet{}, fmt.Errorf("truncated privilege grant option")
		}
		grantOption := body[offset]
		offset++
		if grantOption != 0 {
			return AppACLPrivilegeSet{}, fmt.Errorf("privilege grant option must be false")
		}
		privileges = append(privileges, AppACLPrivilege{
			Subject:        AppACLSubject(subject),
			ObjectClass:    AppACLObjectClass(objectClass),
			SchemaName:     schemaName,
			ObjectIdentity: objectIdentity,
			ColumnName:     columnName,
			Privilege:      AppACLPrivilegeKind(privilegeKind),
		})
	}
	if offset != len(body) {
		return AppACLPrivilegeSet{}, fmt.Errorf("canonical privilege set has trailing bytes")
	}
	// Parsed tuples must already be canonical. Validate their strict order in
	// place instead of allocating and sorting a second copy for comparison.
	for index, privilege := range privileges {
		if err := validateAppACLPrivilege(privilege); err != nil {
			return AppACLPrivilegeSet{}, err
		}
		if !appACLSubjectAdmitted(privilege.Subject, subjects) {
			return AppACLPrivilegeSet{}, fmt.Errorf("ACL subject %q is not bound by this privilege set version", privilege.Subject)
		}
		if magic == canonicalPrivilegeSetMagic && privilege.ObjectClass == AppACLObjectClassFunction &&
			!validFunctionIdentity(privilege.ObjectIdentity) {
			return AppACLPrivilegeSet{}, fmt.Errorf("function ACL identity requires privilege set v2")
		}
		if index == 0 {
			continue
		}
		switch compareAppACLPrivilege(privileges[index-1], privilege) {
		case 0:
			return AppACLPrivilegeSet{}, fmt.Errorf("duplicate canonical privilege tuple")
		case 1:
			return AppACLPrivilegeSet{}, fmt.Errorf("canonical privilege tuples are not sorted")
		}
	}
	return AppACLPrivilegeSet{RoleBindings: bindings, Privileges: privileges}, nil
}

// canonicalRoleBindings normalizes the historical two-binding v1 set.
func canonicalRoleBindings(bindings []AppACLRoleBinding) ([]AppACLRoleBinding, error) {
	return canonicalRoleBindingsForSubjects(bindings, appACLPrivilegeSetSubjectsV1)
}

// canonicalAppACLRoleBindings normalizes either a v1 or a v2 binding set,
// chosen solely by its exact size.
func canonicalAppACLRoleBindings(bindings []AppACLRoleBinding) ([]AppACLRoleBinding, error) {
	switch len(bindings) {
	case len(appACLPrivilegeSetSubjectsV1):
		return canonicalRoleBindingsForSubjects(bindings, appACLPrivilegeSetSubjectsV1)
	case len(appACLPrivilegeSetSubjectsV2):
		return canonicalRoleBindingsForSubjects(bindings, appACLPrivilegeSetSubjectsV2)
	default:
		return nil, fmt.Errorf("canonical privilege set requires two (v1) or three (v2) role bindings")
	}
}

func canonicalRoleBindingsForSubjects(bindings []AppACLRoleBinding, subjects []AppACLSubject) ([]AppACLRoleBinding, error) {
	if len(bindings) != len(subjects) {
		if len(subjects) == len(appACLPrivilegeSetSubjectsV1) {
			return nil, fmt.Errorf("canonical privilege set requires two role bindings")
		}
		return nil, fmt.Errorf("canonical privilege set requires three role bindings")
	}
	sorted := append([]AppACLRoleBinding(nil), bindings...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Subject < sorted[j].Subject
	})
	for index, binding := range sorted {
		if binding.Subject != subjects[index] {
			return nil, fmt.Errorf("canonical privilege set has invalid semantic role bindings")
		}
	}
	for _, binding := range sorted {
		if !validCatalogRoleName(binding.CatalogRole) {
			return nil, fmt.Errorf("canonical privilege set has invalid catalog role name")
		}
	}
	for left := range sorted {
		for right := left + 1; right < len(sorted); right++ {
			if sorted[left].CatalogRole == sorted[right].CatalogRole {
				return nil, fmt.Errorf("canonical privilege set reuses a catalog role")
			}
		}
	}
	return sorted, nil
}

func appACLSubjectAdmitted(subject AppACLSubject, subjects []AppACLSubject) bool {
	for _, admitted := range subjects {
		if subject == admitted {
			return true
		}
	}
	return false
}

func canonicalPrivileges(privileges []AppACLPrivilege) ([]AppACLPrivilege, error) {
	sorted := append([]AppACLPrivilege(nil), privileges...)
	for _, privilege := range sorted {
		if err := validateAppACLPrivilege(privilege); err != nil {
			return nil, err
		}
	}
	sort.Slice(sorted, func(i, j int) bool {
		return compareAppACLPrivilege(sorted[i], sorted[j]) < 0
	})
	for index := 1; index < len(sorted); index++ {
		if compareAppACLPrivilege(sorted[index-1], sorted[index]) == 0 {
			return nil, fmt.Errorf("duplicate canonical privilege tuple")
		}
	}
	return sorted, nil
}

func validateAppACLPrivilege(privilege AppACLPrivilege) error {
	if privilege.Subject != AppACLSubjectCenterRuntime && privilege.Subject != AppACLSubjectPlatformAdmin &&
		privilege.Subject != AppACLSubjectRecordDeletionWorker {
		return fmt.Errorf("unknown ACL subject %q", privilege.Subject)
	}
	if privilege.GrantOption {
		return fmt.Errorf("ACL grant option is not supported")
	}
	switch privilege.ObjectClass {
	case AppACLObjectClassDatabase:
		if privilege.SchemaName != "" || privilege.ColumnName != "" || !validBareCatalogName(privilege.ObjectIdentity) || privilege.Privilege != AppACLPrivilegeConnect {
			return fmt.Errorf("invalid database ACL tuple")
		}
	case AppACLObjectClassSchema:
		if privilege.SchemaName != "" || privilege.ColumnName != "" || privilege.ObjectIdentity != "public" || privilege.Privilege != AppACLPrivilegeUsage {
			return fmt.Errorf("invalid schema ACL tuple")
		}
	case AppACLObjectClassTable:
		if !validRelationACLShape(privilege) || !oneOfPrivilege(privilege.Privilege, AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeUpdate, AppACLPrivilegeDelete) {
			return fmt.Errorf("invalid table ACL tuple")
		}
	case AppACLObjectClassView:
		if !validRelationACLShape(privilege) || privilege.Privilege != AppACLPrivilegeSelect {
			return fmt.Errorf("invalid view ACL tuple")
		}
	case AppACLObjectClassSequence:
		if !validRelationACLShape(privilege) || !oneOfPrivilege(privilege.Privilege, AppACLPrivilegeUsage, AppACLPrivilegeSelect) {
			return fmt.Errorf("invalid sequence ACL tuple")
		}
	case AppACLObjectClassFunction:
		if privilege.SchemaName != "" || privilege.ColumnName != "" ||
			(!validFunctionIdentity(privilege.ObjectIdentity) && !validSuccessorFunctionIdentityV2(privilege.ObjectIdentity)) ||
			privilege.Privilege != AppACLPrivilegeExecute {
			return fmt.Errorf("invalid function ACL tuple")
		}
	case AppACLObjectClassColumn:
		return fmt.Errorf("v1 ACL manifest must not contain column grants")
	default:
		return fmt.Errorf("unknown ACL object class %q", privilege.ObjectClass)
	}
	return nil
}

func validRelationACLShape(privilege AppACLPrivilege) bool {
	return privilege.SchemaName == "public" && privilege.ColumnName == "" && validBareCatalogName(privilege.ObjectIdentity)
}

func validFunctionIdentity(identity string) bool {
	const prefix = "public."
	if !strings.HasPrefix(identity, prefix) {
		return false
	}
	// Preserve the public bytea grammar and admit only the approved pure
	// parsers used by source constraints. The catalog contract still checks
	// each exact identity; this does not admit arbitrary overloads.
	if identity == "public.houfeng_parse_host_address(text)" ||
		identity == "public.record_text_set_is_canonical(text[])" {
		return true
	}
	const suffix = "(bytea)"
	if !strings.HasSuffix(identity, suffix) {
		return false
	}
	return validBareCatalogName(strings.TrimSuffix(strings.TrimPrefix(identity, prefix), suffix))
}

// These exact command entrypoints belong only to the mandatory-worker successor body.
// Keep the historical V1 grammar unchanged and reject unlisted overloads.
func validSuccessorFunctionIdentityV2(identity string) bool {
	switch identity {
	case "public.source_application_admit()",
		"public.record_birth_current_binding()",
		"public.record_birth_continuity_binding()",
		"public.source_maintenance_status()",
		"public.source_legacy_retirement_blocked(text, text)",
		"public.record_birth_launch_inspect(bigint, bytea)",
		"public.record_processor_worker_admit(text, text, text, text)",
		"public.record_revision_blob_fence()",
		"public.local_blob_recovery_inventory()",
		"public.record_comparison_index_write(bytea, bytea)",
		"public.record_collaboration_start_inventory(text, bigint)",
		"public.record_collaboration_delivery_inventory(text, bigint)",
		"public.source_retirement_preview(text, text, text)",
		"public.source_retirement_commit(bigint, text, text, text, text, text, bytea, bytea, bytea, bytea)",
		"public.source_retirement_install_pin(bigint, text, bigint, bytea, bytea, bytea, bigint)":
		return true
	default:
		return false
	}
}

func validBareCatalogName(value string) bool {
	if len(value) < 1 || len(value) > 63 || !utf8.ValidString(value) {
		return false
	}
	for index, runeValue := range value {
		if (index == 0 && !(runeValue == '_' || runeValue >= 'a' && runeValue <= 'z')) ||
			(index > 0 && !(runeValue == '_' || runeValue >= 'a' && runeValue <= 'z' || runeValue >= '0' && runeValue <= '9')) {
			return false
		}
	}
	return true
}

func validCatalogRoleName(value string) bool {
	if len(value) < 1 || len(value) > 63 || !utf8.ValidString(value) || !norm.NFC.IsNormalString(value) {
		return false
	}
	for _, runeValue := range value {
		if runeValue == 0 || unicode.IsControl(runeValue) {
			return false
		}
	}
	return true
}

func oneOfPrivilege(got AppACLPrivilegeKind, allowed ...AppACLPrivilegeKind) bool {
	for _, value := range allowed {
		if got == value {
			return true
		}
	}
	return false
}

func compareAppACLPrivilege(left, right AppACLPrivilege) int {
	if order := strings.Compare(string(left.Subject), string(right.Subject)); order != 0 {
		return order
	}
	if order := strings.Compare(string(left.ObjectClass), string(right.ObjectClass)); order != 0 {
		return order
	}
	if order := strings.Compare(left.SchemaName, right.SchemaName); order != 0 {
		return order
	}
	if order := strings.Compare(left.ObjectIdentity, right.ObjectIdentity); order != 0 {
		return order
	}
	if order := strings.Compare(left.ColumnName, right.ColumnName); order != 0 {
		return order
	}
	if order := strings.Compare(string(left.Privilege), string(right.Privilege)); order != 0 {
		return order
	}
	if left.GrantOption == right.GrantOption {
		return 0
	}
	if !left.GrantOption {
		return -1
	}
	return 1
}

func appendUint32(body []byte, value uint32) []byte {
	var encoded [4]byte
	binary.BigEndian.PutUint32(encoded[:], value)
	return append(body, encoded[:]...)
}

func appendLengthPrefixedString(body []byte, value string) []byte {
	body = appendUint32(body, uint32(len(value)))
	return append(body, value...)
}

func readCanonicalUint32(body []byte, offset *int, field string) (uint32, error) {
	if len(body)-*offset < 4 {
		return 0, fmt.Errorf("truncated %s", field)
	}
	value := binary.BigEndian.Uint32(body[*offset : *offset+4])
	*offset += 4
	return value, nil
}

func readCanonicalString(body []byte, offset *int, maximumLength int, field string) (string, error) {
	// Keep diagnostic formatting on the error path: this decoder runs once
	// for every string in every manifest tuple.
	if len(body)-*offset < 4 {
		return "", fmt.Errorf("truncated %s length", field)
	}
	length := binary.BigEndian.Uint32(body[*offset : *offset+4])
	*offset += 4
	if length > uint32(maximumLength) || len(body)-*offset < int(length) {
		return "", fmt.Errorf("invalid %s length", field)
	}
	value := string(body[*offset : *offset+int(length)])
	*offset += int(length)
	if !utf8.ValidString(value) {
		return "", fmt.Errorf("invalid %s UTF-8", field)
	}
	return value, nil
}

// AppACLManifestPersistedV1 mirrors the content-bearing fields of one
// app_acl_manifest_revisions row. PostgreSQL stores the same field order in
// its manifest_digest CHECK constraint.
type AppACLManifestPersistedV1 struct {
	ManifestRevision       uint64
	MigratorCatalogRole    string
	PreviousManifestDigest [32]byte
	CanonicalMigrationSet  []byte
	MigrationSetDigest     [32]byte
	CanonicalPrivilegeSet  []byte
	PrivilegeSetDigest     [32]byte
	ManifestDigest         [32]byte
}

// AppACLManifestHeadV1 mirrors the non-null pair held by
// app_acl_manifest_head after the first manifest revision is committed.
type AppACLManifestHeadV1 struct {
	ManifestRevision uint64
	ManifestDigest   [32]byte
}

// NewAppACLManifestPersistedV1 produces a fully bound manifest value from two
// canonical body bytes. It never derives the revision from migration names.
func NewAppACLManifestPersistedV1(
	manifestRevision uint64,
	migratorCatalogRole string,
	previousManifestDigest [32]byte,
	canonicalMigrationSet []byte,
	canonicalPrivilegeSet []byte,
) (AppACLManifestPersistedV1, error) {
	manifest := AppACLManifestPersistedV1{
		ManifestRevision:       manifestRevision,
		MigratorCatalogRole:    migratorCatalogRole,
		PreviousManifestDigest: previousManifestDigest,
		CanonicalMigrationSet:  append([]byte(nil), canonicalMigrationSet...),
		CanonicalPrivilegeSet:  append([]byte(nil), canonicalPrivilegeSet...),
	}
	manifest.MigrationSetDigest = sha256.Sum256(manifest.CanonicalMigrationSet)
	manifest.PrivilegeSetDigest = sha256.Sum256(manifest.CanonicalPrivilegeSet)
	if err := manifest.validateFields(); err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	manifest.ManifestDigest = manifest.computedDigest()
	return manifest, nil
}

// Validate checks both sibling digests and the SQL-compatible manifest
// preimage. It is safe to use before writing or trusting a persisted row.
func (manifest AppACLManifestPersistedV1) Validate() error {
	if err := manifest.validateFields(); err != nil {
		return err
	}
	if manifest.ManifestDigest != manifest.computedDigest() {
		return fmt.Errorf("app ACL manifest digest does not match canonical fields")
	}
	return nil
}

// ValidateAppACLManifestChainV1 verifies the complete, ordered immutable
// manifest chain and its persisted head pointer. Callers must supply every
// revision in manifest_revision order; a partial, reordered, or far-tail
// result is rejected rather than treated as an equivalent chain.
func ValidateAppACLManifestChainV1(manifests []AppACLManifestPersistedV1, head AppACLManifestHeadV1) error {
	if len(manifests) == 0 {
		return fmt.Errorf("app ACL manifest chain has no revisions")
	}
	if len(manifests) > 999999 {
		return fmt.Errorf("app ACL manifest chain has too many revisions")
	}

	var previousVersion AppACLPrivilegeSetVersion
	for index, manifest := range manifests {
		expectedRevision := uint64(index + 1)
		if manifest.ManifestRevision != expectedRevision {
			return fmt.Errorf("app ACL manifest chain revision %d has manifest revision %d", expectedRevision, manifest.ManifestRevision)
		}
		if err := manifest.Validate(); err != nil {
			return fmt.Errorf("validate app ACL manifest revision %d: %w", expectedRevision, err)
		}
		if index > 0 && manifest.PreviousManifestDigest != manifests[index-1].ManifestDigest {
			return fmt.Errorf("app ACL manifest revision %d does not bind the previous manifest digest", expectedRevision)
		}
		// Privilege bodies may only advance from v1 to v2. A v2 revision
		// followed by a v1 revision would silently drop the mandatory
		// record-deletion worker binding.
		_, version, err := parseCanonicalPrivilegeSetBodyAnyVersion(manifest.CanonicalPrivilegeSet)
		if err != nil {
			return fmt.Errorf("parse app ACL manifest revision %d privilege set: %w", expectedRevision, err)
		}
		if version < previousVersion {
			return fmt.Errorf("app ACL manifest revision %d regresses privilege set version %d to %d", expectedRevision, previousVersion, version)
		}
		previousVersion = version
	}

	latest := manifests[len(manifests)-1]
	if head.ManifestRevision != latest.ManifestRevision || head.ManifestDigest != latest.ManifestDigest {
		return fmt.Errorf("app ACL manifest head does not match latest revision")
	}
	return nil
}

func (manifest AppACLManifestPersistedV1) validateFields() error {
	if manifest.ManifestRevision < 1 || manifest.ManifestRevision > 999999 {
		return fmt.Errorf("app ACL manifest revision %d is outside v1 bounds", manifest.ManifestRevision)
	}
	if manifest.ManifestRevision == 1 && manifest.PreviousManifestDigest != [32]byte{} {
		return fmt.Errorf("app ACL manifest genesis revision has a previous digest")
	}
	if manifest.ManifestRevision > 1 && manifest.PreviousManifestDigest == [32]byte{} {
		return fmt.Errorf("app ACL manifest non-genesis revision has no previous digest")
	}
	if !validCatalogRoleName(manifest.MigratorCatalogRole) {
		return fmt.Errorf("app ACL manifest has invalid migrator catalog role")
	}
	if len(manifest.CanonicalMigrationSet) < 1 || len(manifest.CanonicalMigrationSet) > maxCanonicalACLManifestBodyBytes {
		return fmt.Errorf("canonical migration set size is outside v1 bounds")
	}
	if len(manifest.CanonicalPrivilegeSet) < 1 || len(manifest.CanonicalPrivilegeSet) > maxCanonicalACLManifestBodyBytes {
		return fmt.Errorf("canonical privilege set size is outside v1 bounds")
	}
	if _, err := ParseCanonicalMigrationSetBodyV1(manifest.CanonicalMigrationSet); err != nil {
		return fmt.Errorf("parse canonical migration set: %w", err)
	}
	if _, _, err := parseCanonicalPrivilegeSetBodyAnyVersion(manifest.CanonicalPrivilegeSet); err != nil {
		return fmt.Errorf("parse canonical privilege set: %w", err)
	}
	if manifest.MigrationSetDigest != sha256.Sum256(manifest.CanonicalMigrationSet) {
		return fmt.Errorf("migration set sibling digest does not match canonical bytes")
	}
	if manifest.PrivilegeSetDigest != sha256.Sum256(manifest.CanonicalPrivilegeSet) {
		return fmt.Errorf("privilege set sibling digest does not match canonical bytes")
	}
	return nil
}

func (manifest AppACLManifestPersistedV1) computedDigest() [32]byte {
	preimage := make([]byte, 0,
		len(appACLManifestMagic)+8+4+len(manifest.MigratorCatalogRole)+len(manifest.PreviousManifestDigest)+4+len(manifest.CanonicalMigrationSet)+len(manifest.MigrationSetDigest)+4+len(manifest.CanonicalPrivilegeSet)+len(manifest.PrivilegeSetDigest))
	preimage = append(preimage, appACLManifestMagic...)
	preimage = appendACLManifestUint64(preimage, manifest.ManifestRevision)
	preimage = appendUint32(preimage, uint32(len(manifest.MigratorCatalogRole)))
	preimage = append(preimage, manifest.MigratorCatalogRole...)
	preimage = append(preimage, manifest.PreviousManifestDigest[:]...)
	preimage = appendUint32(preimage, uint32(len(manifest.CanonicalMigrationSet)))
	preimage = append(preimage, manifest.CanonicalMigrationSet...)
	preimage = append(preimage, manifest.MigrationSetDigest[:]...)
	preimage = appendUint32(preimage, uint32(len(manifest.CanonicalPrivilegeSet)))
	preimage = append(preimage, manifest.CanonicalPrivilegeSet...)
	preimage = append(preimage, manifest.PrivilegeSetDigest[:]...)
	return sha256.Sum256(preimage)
}

func appendACLManifestUint64(body []byte, value uint64) []byte {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	return append(body, encoded[:]...)
}
