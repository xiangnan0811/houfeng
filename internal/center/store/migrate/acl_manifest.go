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
)

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
	sortedBindings, err := canonicalRoleBindings(bindings)
	if err != nil {
		return nil, err
	}
	sortedPrivileges, err := canonicalPrivileges(privileges)
	if err != nil {
		return nil, err
	}

	size := len(canonicalPrivilegeSetMagic) + 4 + 4
	for _, binding := range sortedBindings {
		size += 4 + len(binding.Subject) + 4 + len(binding.CatalogRole)
	}
	for _, privilege := range sortedPrivileges {
		size += 4 + len(privilege.Subject) + 4 + len(privilege.ObjectClass) +
			4 + len(privilege.SchemaName) + 4 + len(privilege.ObjectIdentity) +
			4 + len(privilege.ColumnName) + 4 + len(privilege.Privilege) + 1
	}

	body := make([]byte, 0, size)
	body = append(body, canonicalPrivilegeSetMagic...)
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
	if !bytes.HasPrefix(body, []byte(canonicalPrivilegeSetMagic)) {
		return AppACLPrivilegeSet{}, fmt.Errorf("invalid canonical privilege set magic")
	}
	offset := len(canonicalPrivilegeSetMagic)
	bindingCount, err := readCanonicalUint32(body, &offset, "role binding count")
	if err != nil {
		return AppACLPrivilegeSet{}, err
	}
	if bindingCount != 2 {
		return AppACLPrivilegeSet{}, fmt.Errorf("canonical privilege set has %d role bindings, want 2", bindingCount)
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
	if _, err := canonicalRoleBindings(bindings); err != nil {
		return AppACLPrivilegeSet{}, err
	}
	if bindings[0].Subject != AppACLSubjectCenterRuntime || bindings[1].Subject != AppACLSubjectPlatformAdmin {
		return AppACLPrivilegeSet{}, fmt.Errorf("canonical privilege role bindings are not sorted")
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
			return AppACLPrivilegeSet{}, fmt.Errorf("v1 privilege grant option must be false")
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
	canonical, err := canonicalPrivileges(privileges)
	if err != nil {
		return AppACLPrivilegeSet{}, err
	}
	if len(canonical) != len(privileges) {
		return AppACLPrivilegeSet{}, fmt.Errorf("canonical privilege set privilege count changed")
	}
	for index := range privileges {
		if compareAppACLPrivilege(privileges[index], canonical[index]) != 0 {
			return AppACLPrivilegeSet{}, fmt.Errorf("canonical privilege tuples are not sorted")
		}
	}
	return AppACLPrivilegeSet{RoleBindings: bindings, Privileges: privileges}, nil
}

func canonicalRoleBindings(bindings []AppACLRoleBinding) ([]AppACLRoleBinding, error) {
	if len(bindings) != 2 {
		return nil, fmt.Errorf("canonical privilege set requires two role bindings")
	}
	sorted := append([]AppACLRoleBinding(nil), bindings...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Subject < sorted[j].Subject
	})
	if sorted[0].Subject != AppACLSubjectCenterRuntime || sorted[1].Subject != AppACLSubjectPlatformAdmin {
		return nil, fmt.Errorf("canonical privilege set has invalid semantic role bindings")
	}
	if !validCatalogRoleName(sorted[0].CatalogRole) || !validCatalogRoleName(sorted[1].CatalogRole) {
		return nil, fmt.Errorf("canonical privilege set has invalid catalog role name")
	}
	if sorted[0].CatalogRole == sorted[1].CatalogRole {
		return nil, fmt.Errorf("canonical privilege set reuses a catalog role")
	}
	return sorted, nil
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
	if privilege.Subject != AppACLSubjectCenterRuntime && privilege.Subject != AppACLSubjectPlatformAdmin {
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
		if privilege.SchemaName != "" || privilege.ColumnName != "" || !validFunctionIdentity(privilege.ObjectIdentity) || privilege.Privilege != AppACLPrivilegeExecute {
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
	const suffix = "(bytea)"
	if !strings.HasPrefix(identity, prefix) || !strings.HasSuffix(identity, suffix) {
		return false
	}
	return validBareCatalogName(strings.TrimSuffix(strings.TrimPrefix(identity, prefix), suffix))
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
	for _, pair := range [][2]string{
		{string(left.Subject), string(right.Subject)},
		{string(left.ObjectClass), string(right.ObjectClass)},
		{left.SchemaName, right.SchemaName},
		{left.ObjectIdentity, right.ObjectIdentity},
		{left.ColumnName, right.ColumnName},
		{string(left.Privilege), string(right.Privilege)},
	} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
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
	length, err := readCanonicalUint32(body, offset, field+" length")
	if err != nil {
		return "", err
	}
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
	if _, err := ParseCanonicalPrivilegeSetBodyV1(manifest.CanonicalPrivilegeSet); err != nil {
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
