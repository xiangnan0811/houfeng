package migrate

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"strings"
	"testing"
)

func TestAppACLManifestPersistedV1BindsExactSiblingBodies(t *testing.T) {
	migrationBody, err := CanonicalMigrationSetBodyV1([]MigrationChecksumEntry{{
		Filename: "0001_initial_schema.sql",
		Checksum: checksumFromHex(t, strings.Repeat("01", 32)),
	}})
	if err != nil {
		t.Fatalf("CanonicalMigrationSetBodyV1() error = %v", err)
	}
	privilegeBody, err := CanonicalPrivilegeSetBodyV1(
		[]AppACLRoleBinding{
			{Subject: AppACLSubjectCenterRuntime, CatalogRole: "houfeng_center_runtime"},
			{Subject: AppACLSubjectPlatformAdmin, CatalogRole: "houfeng_platform_admin"},
		},
		[]AppACLPrivilege{{
			Subject:        AppACLSubjectCenterRuntime,
			ObjectClass:    AppACLObjectClassDatabase,
			ObjectIdentity: "houfeng",
			Privilege:      AppACLPrivilegeConnect,
		}},
	)
	if err != nil {
		t.Fatalf("CanonicalPrivilegeSetBodyV1() error = %v", err)
	}

	manifest, err := NewAppACLManifestPersistedV1(1, [32]byte{}, migrationBody, privilegeBody)
	if err != nil {
		t.Fatalf("NewAppACLManifestPersistedV1() error = %v", err)
	}
	if err := manifest.Validate(); err != nil {
		t.Fatalf("manifest.Validate() error = %v", err)
	}

	wantPreimage := []byte("HOUFENG-APP-ACL-MANIFEST-V1")
	wantPreimage = appendManifestUint64(wantPreimage, 1)
	wantPreimage = append(wantPreimage, make([]byte, 32)...)
	wantPreimage = appendManifestUint32(wantPreimage, uint32(len(migrationBody)))
	wantPreimage = append(wantPreimage, migrationBody...)
	migrationDigest := sha256.Sum256(migrationBody)
	wantPreimage = append(wantPreimage, migrationDigest[:]...)
	wantPreimage = appendManifestUint32(wantPreimage, uint32(len(privilegeBody)))
	wantPreimage = append(wantPreimage, privilegeBody...)
	privilegeDigest := sha256.Sum256(privilegeBody)
	wantPreimage = append(wantPreimage, privilegeDigest[:]...)
	wantDigest := sha256.Sum256(wantPreimage)
	if manifest.ManifestDigest != wantDigest {
		t.Fatalf("manifest digest = %x, want %x", manifest.ManifestDigest, wantDigest)
	}
}

func TestAppACLManifestPersistedV1RejectsDigestAndGenesisDrift(t *testing.T) {
	migrationBody, err := CanonicalMigrationSetBodyV1([]MigrationChecksumEntry{{
		Filename: "0001_initial_schema.sql",
		Checksum: checksumFromHex(t, strings.Repeat("01", 32)),
	}})
	if err != nil {
		t.Fatalf("CanonicalMigrationSetBodyV1() error = %v", err)
	}
	privilegeBody, err := CanonicalPrivilegeSetBodyV1(
		[]AppACLRoleBinding{
			{Subject: AppACLSubjectCenterRuntime, CatalogRole: "houfeng_center_runtime"},
			{Subject: AppACLSubjectPlatformAdmin, CatalogRole: "houfeng_platform_admin"},
		},
		nil,
	)
	if err != nil {
		t.Fatalf("CanonicalPrivilegeSetBodyV1() error = %v", err)
	}
	manifest, err := NewAppACLManifestPersistedV1(1, [32]byte{}, migrationBody, privilegeBody)
	if err != nil {
		t.Fatalf("NewAppACLManifestPersistedV1() error = %v", err)
	}

	mutatedSibling := manifest
	mutatedSibling.MigrationSetDigest[0] ^= 0xff
	mutatedManifest := manifest
	mutatedManifest.ManifestDigest[0] ^= 0xff
	nonGenesisPrevious := manifest
	nonGenesisPrevious.PreviousManifestDigest[0] = 1
	for _, candidate := range []AppACLManifestPersistedV1{mutatedSibling, mutatedManifest, nonGenesisPrevious} {
		if err := candidate.Validate(); err == nil {
			t.Fatal("manifest.Validate() error = nil, want contract-drift rejection")
		}
	}
}

func appendManifestUint64(body []byte, value uint64) []byte {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	return append(body, encoded[:]...)
}

func appendManifestUint32(body []byte, value uint32) []byte {
	var encoded [4]byte
	binary.BigEndian.PutUint32(encoded[:], value)
	return append(body, encoded[:]...)
}

func TestAppACLManifestPersistedV1DigestFixtureUsesDifferentBodyLengths(t *testing.T) {
	left := appendManifestUint32(nil, 1)
	right := appendManifestUint32(nil, 256)
	if bytes.Equal(left, right) {
		t.Fatalf("length encodings unexpectedly collide: %x", left)
	}
}

func TestValidateAppACLManifestChainV1RequiresContiguousLinkedHead(t *testing.T) {
	migrationBody, err := CanonicalMigrationSetBodyV1([]MigrationChecksumEntry{{
		Filename: "0001_initial_schema.sql",
		Checksum: checksumFromHex(t, strings.Repeat("01", 32)),
	}})
	if err != nil {
		t.Fatalf("CanonicalMigrationSetBodyV1() error = %v", err)
	}
	privilegeBody, err := CanonicalPrivilegeSetBodyV1(
		[]AppACLRoleBinding{
			{Subject: AppACLSubjectCenterRuntime, CatalogRole: "houfeng_center_runtime"},
			{Subject: AppACLSubjectPlatformAdmin, CatalogRole: "houfeng_platform_admin"},
		},
		nil,
	)
	if err != nil {
		t.Fatalf("CanonicalPrivilegeSetBodyV1() error = %v", err)
	}
	first, err := NewAppACLManifestPersistedV1(1, [32]byte{}, migrationBody, privilegeBody)
	if err != nil {
		t.Fatalf("NewAppACLManifestPersistedV1(first) error = %v", err)
	}
	second, err := NewAppACLManifestPersistedV1(2, first.ManifestDigest, migrationBody, privilegeBody)
	if err != nil {
		t.Fatalf("NewAppACLManifestPersistedV1(second) error = %v", err)
	}
	head := AppACLManifestHeadV1{ManifestRevision: second.ManifestRevision, ManifestDigest: second.ManifestDigest}
	if err := ValidateAppACLManifestChainV1([]AppACLManifestPersistedV1{first, second}, head); err != nil {
		t.Fatalf("ValidateAppACLManifestChainV1() error = %v", err)
	}

	gap, err := NewAppACLManifestPersistedV1(3, first.ManifestDigest, migrationBody, privilegeBody)
	if err != nil {
		t.Fatalf("NewAppACLManifestPersistedV1(gap) error = %v", err)
	}
	if err := ValidateAppACLManifestChainV1([]AppACLManifestPersistedV1{first, gap}, head); err == nil {
		t.Fatal("ValidateAppACLManifestChainV1() error = nil for revision gap")
	}
	var unexpectedPrevious [32]byte
	unexpectedPrevious[0] = 1
	wrongPredecessor, err := NewAppACLManifestPersistedV1(2, unexpectedPrevious, migrationBody, privilegeBody)
	if err != nil {
		t.Fatalf("NewAppACLManifestPersistedV1(wrong predecessor) error = %v", err)
	}
	if err := ValidateAppACLManifestChainV1([]AppACLManifestPersistedV1{first, wrongPredecessor}, head); err == nil {
		t.Fatal("ValidateAppACLManifestChainV1() error = nil for previous digest drift")
	}
	wrongHead := head
	wrongHead.ManifestRevision--
	if err := ValidateAppACLManifestChainV1([]AppACLManifestPersistedV1{first, second}, wrongHead); err == nil {
		t.Fatal("ValidateAppACLManifestChainV1() error = nil for stale manifest head")
	}
}
