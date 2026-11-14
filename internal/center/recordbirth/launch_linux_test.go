//go:build linux

package recordbirth

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestLoadRuntimeLaunchBindingRejectsUnprotectedInput(t *testing.T) {
	_, canonical := launchOrdinaryVector()
	dir := t.TempDir()
	file := filepath.Join(dir, "binding.json")
	if err := os.WriteFile(file, []byte(canonical), 0o440); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	hardOriginal := filepath.Join(dir, "hard-original.json")
	if err := os.WriteFile(hardOriginal, []byte(canonical), 0o440); err != nil {
		t.Fatal(err)
	}
	hardLink := filepath.Join(dir, "hard-link.json")
	if err := os.Link(hardOriginal, hardLink); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(dir, "fifo")
	if err := unix.Mkfifo(fifo, 0o440); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		path string
		kind string
	}{
		{"empty path", "", "center"},
		{"relative path", "binding.json", "center"},
		{"root directory", "/", "center"},
		{"unclean path", dir + "/sub/../binding.json", "center"},
		{"missing file", filepath.Join(dir, "missing"), "center"},
		{"unprotected directory", file, "center"},
		{"symbolic link", link, "center"},
		{"multiple hard links", hardLink, "center"},
		{"fifo", fifo, "center"},
		{"proc magic link", "/proc/self/exe", "center"},
		{"wrong process kind", file, "worker"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, loader := range []struct {
				name string
				read func(string, string) (LaunchBinding, [32]byte, error)
			}{
				{"runtime", LoadRuntimeLaunchBinding},
				{"published", ReadPublishedLaunchBinding},
			} {
				t.Run(loader.name, func(t *testing.T) {
					binding, digest, err := loader.read(tc.path, tc.kind)
					if err == nil {
						t.Fatalf("accepted unsafe launch binding %+v with digest %x", binding, digest)
					}
					if binding != (LaunchBinding{}) || digest != ([32]byte{}) {
						t.Fatalf("returned launch authority on failure: %+v %x", binding, digest)
					}
				})
			}
		})
	}
}

func TestDigestExecutableRejectsFIFOWithoutWaitingForWriter(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "image-fifo")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := DigestExecutable(fifo); !errors.Is(err, ErrInvalid) {
		t.Fatalf("digesting a FIFO = %v", err)
	}
}
