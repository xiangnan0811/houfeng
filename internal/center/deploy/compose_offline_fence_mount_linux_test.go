//go:build linux

package deploy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestComposeOfflineFenceMountSharing(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		a, b    fenceMount
		overlap bool
	}{
		{fenceMount{Type: "bind", Source: root}, fenceMount{Type: "bind", Source: child}, true},
		{fenceMount{Type: "volume", Name: "v", Source: child}, fenceMount{Type: "volume", Name: "v", Source: child}, true},
		{fenceMount{Type: "volume", Name: "v", Source: child}, fenceMount{Type: "bind", Source: root}, true},
		{fenceMount{Type: "tmpfs"}, fenceMount{Type: "bind", Source: root}, false},
	}
	for _, tc := range cases {
		got, err := fenceMountsOverlap(tc.a, tc.b)
		if err != nil || got != tc.overlap {
			t.Fatalf("mount overlap = %v, %v; want %v", got, err, tc.overlap)
		}
	}
}
