//go:build linux

package deploy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestOrdinaryNamespaceRecipeResolvesExactWritableBindSources(t *testing.T) {
	var recipe ordinaryNamespaceRecipe
	if err := json.Unmarshal([]byte(`{"environment":{"HOUFENG_ATTACHMENT_BLOB_BACKEND":"local"},"volumes":[{"type":"bind","source":"/srv/actual/blob","target":"/var/lib/houfeng/attachments"},{"type":"bind","source":"/srv/actual/identity","target":"/var/lib/houfeng/local-namespace-init"}]}`), &recipe); err != nil {
		t.Fatal(err)
	}
	got, err := ordinaryNamespaceMountsFromRecipe(recipe)
	if err != nil || got.backend != "local" || got.blobRoot != "/srv/actual/blob" || got.identityDir != "/srv/actual/identity" {
		t.Fatal("did not resolve the selected initializer's physical mounts")
	}
	recipe.Volumes[0].ReadOnly = true
	if _, err := ordinaryNamespaceMountsFromRecipe(recipe); err == nil {
		t.Fatal("accepted a read-only BlobRoot")
	}
}

func TestOrdinaryNamespaceBaselinePinsReleasedSkeleton(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root to create UID10001 fixture")
	}
	base := t.TempDir()
	root := filepath.Join(base, "blobs")
	digest := filepath.Join(root, "sha256")
	for _, path := range []string{root, digest} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(path, 10001, 10001); err != nil {
			t.Fatal(err)
		}
	}
	var recipe ordinaryNamespaceRecipe
	body, err := json.Marshal(map[string]any{"environment": map[string]string{"HOUFENG_ATTACHMENT_BLOB_BACKEND": "local"}, "volumes": []map[string]any{{"type": "bind", "source": root, "target": composeNamespaceRoot}, {"type": "bind", "source": filepath.Join(base, "identity"), "target": composeNamespaceIdentityDir}}})
	if err != nil || json.Unmarshal(body, &recipe) != nil {
		t.Fatal("invalid fixture recipe")
	}
	first, err := ordinaryCaptureNamespaceBaseline(recipe)
	if err != nil || !first.Check || first.RootInode == 0 || first.DigestInode == 0 {
		t.Fatal("released skeleton was not pinned")
	}
	withoutRoot := first
	withoutRoot.RootInode = 0
	withoutDigest := first
	withoutDigest.DigestInode = 0
	if ordinaryNamespaceBaselineMatches(withoutRoot) || ordinaryNamespaceBaselineMatches(withoutDigest) {
		t.Fatal("missing inode accepted in cold baseline")
	}
	if err := os.Rename(digest, filepath.Join(root, "old-sha256")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(digest, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(digest, 10001, 10001); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "old-sha256")); err != nil {
		t.Fatal(err)
	}
	second, err := ordinaryCaptureNamespaceBaseline(recipe)
	if err != nil || first == second {
		t.Fatal("replacement digest identity was not detected")
	}
}
