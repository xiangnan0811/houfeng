package migrate

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"testing"
)

func TestAppACLCurrentCacheExpiryFragmentHasNoAuthorizationDelta(t *testing.T) {
	fragment := ipQualityCacheExpiryAppACLCurrentMigrationFragment()
	if fragment.Migration != "0079_ip_quality_cache_expiry.sql" || len(fragment.Objects) != 0 ||
		len(fragment.Functions) != 0 || len(fragment.AuxiliaryPrivileges) != 0 {
		t.Fatalf("0079 must add only an ordinary index without managed objects or grants: %#v", fragment)
	}
	for _, database := range []string{"houfeng", "other_database"} {
		if privileges := fragment.Privileges(database); len(privileges) != 0 {
			t.Fatalf("0079 adds privileges for %s: %#v", database, privileges)
		}
	}
}

func TestAppACLCurrentC78IndependentGoldensPreserveC77(t *testing.T) {
	for _, tc := range []struct {
		name string
		body []byte
		sha  string
	}{
		{name: "migrations", body: appACLCurrentC78MigrationGolden, sha: "af2497ecde095a8f33e847dd21fd2f863764c86ff1a141aa40fac6ae6cb34ad3"},
		{name: "privileges", body: appACLCurrentC78PrivilegeGolden, sha: "3bda892781478202e876702943a66463e25345c205c9c125304b2b6ba1a1eb0c"},
	} {
		if got := fmt.Sprintf("%x", sha256.Sum256(tc.body)); got != tc.sha {
			t.Fatalf("independent pre-C79 C78 %s golden digest = %s, want %s", tc.name, got, tc.sha)
		}
	}
	entries, err := ParseCanonicalMigrationSetBodyV1(appACLCurrentC78MigrationGolden)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 79 || entries[78].Filename != "0078_ip_quality_collection_scope.sql" {
		t.Fatalf("independent C78 source oracle must end in exactly 0078: %#v", entries)
	}
	prefix, err := CanonicalMigrationSetBodyV1(entries[:78])
	if err != nil || !bytes.Equal(prefix, appACLCurrentC77MigrationGolden) {
		t.Fatalf("independent C78 source oracle changed the released C77 prefix: %v", err)
	}
	if !bytes.Equal(appACLCurrentC78PrivilegeGolden, appACLCurrentC77PrivilegeGolden) {
		t.Fatal("independent C78 must preserve C77 privileges exactly")
	}
}

func TestAppACLCurrentP78RequiresExactlyPreservedPrivileges(t *testing.T) {
	testAppACLCurrentPreservedPrivileges(t, appACLCurrentProfileP78, appACLCurrentC78PrivilegeGolden)
}

func TestAppACLCurrentP78RejectsPublishedSourceDriftBeforeBeginTx(t *testing.T) {
	fsys := appACLCurrentTransitionTestFS(t)
	file := fsys["0078_ip_quality_collection_scope.sql"]
	file.Data = append(append([]byte(nil), file.Data...), []byte("\n-- altered historical source\n")...)
	assertAppACLCurrentTransitionRejectedBeforeBeginTx(t, fsys, appACLCurrentMigrationFragments, appACLCurrentTransitionDefinitions,
		"predecessor migration source differs from released golden")
}
