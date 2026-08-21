package evidence

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestComparisonIntentSignerAccepts0400RegularFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeComparisonKey(t, filepath.Join(dir, "cmp_test"), 0400)
	signer, err := OpenComparisonIntentKeyring(dir, "cmp_test", nil)
	if err != nil {
		t.Fatalf("OpenComparisonIntentKeyring() error = %v", err)
	}
	now := time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC)
	claims := BuildComparisonIntentClaims(ComparisonIntentClaimsInput{
		Actor:           testActor(t),
		Items:           []ResolvedComparisonItem{{SnapshotID: "evs_fixeda", Kind: CommandAuditV1Key(), RevisionContext: RevisionContextNotApplicable}},
		BaselineIndex:   0,
		Alignment:       CoverageActual,
		RequestedWindow: testWindow(),
		Now:             now,
		KeyID:           "cmp_test",
	})
	intent, err := signer.Sign(claims)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	if intent.KeyID != "cmp_test" || intent.Token == "" {
		t.Fatalf("intent = %#v", intent)
	}
	got, err := signer.Verify(intent.Token, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if got.Purpose != ComparisonIntentPurpose || got.KeyID != "cmp_test" {
		t.Fatalf("verified claims = %#v", got)
	}
	expired, err := signer.Verify(intent.Token, now.Add(ComparisonIntentTTL+time.Second))
	if !errors.Is(err, ErrComparisonIntentExpired) || expired.Purpose != ComparisonIntentPurpose {
		t.Fatalf("expired verify = %#v, %v, want authentic claims plus expired", expired, err)
	}
}

func TestComparisonIntentSignerRejectsUnsafeKeyFiles(t *testing.T) {
	t.Parallel()

	key := []byte("comparison-intent-key-material-32b")
	tests := []struct {
		name string
		prep func(t *testing.T, dir string) (keyID string, reserved []string)
	}{
		{
			name: "writable mode",
			prep: func(t *testing.T, dir string) (string, []string) {
				writeComparisonKey(t, filepath.Join(dir, "cmp_test"), 0600)
				return "cmp_test", nil
			},
		},
		{
			name: "symlink",
			prep: func(t *testing.T, dir string) (string, []string) {
				target := filepath.Join(dir, "target")
				writeComparisonKey(t, target, 0400)
				if err := os.Symlink(target, filepath.Join(dir, "cmp_test")); err != nil {
					t.Fatalf("symlink: %v", err)
				}
				return "cmp_test", nil
			},
		},
		{
			name: "fifo device",
			prep: func(t *testing.T, dir string) (string, []string) {
				path := filepath.Join(dir, "cmp_test")
				if err := unix.Mkfifo(path, 0400); err != nil {
					t.Fatalf("mkfifo: %v", err)
				}
				return "cmp_test", nil
			},
		},
		{
			name: "hard-link",
			prep: func(t *testing.T, dir string) (string, []string) {
				path := filepath.Join(dir, "cmp_test")
				writeComparisonKey(t, path, 0400)
				if err := os.Link(path, filepath.Join(dir, "cmp_alias")); err != nil {
					t.Fatalf("hard link: %v", err)
				}
				return "cmp_test", nil
			},
		},
		{
			name: "reserved deletion key",
			prep: func(t *testing.T, dir string) (string, []string) {
				path := filepath.Join(dir, "cmp_test")
				writeComparisonKey(t, path, 0400)
				return "cmp_test", []string{path}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			keyID, reserved := tt.prep(t, dir)
			if _, err := OpenComparisonIntentKeyring(dir, keyID, reserved); err == nil {
				t.Fatalf("OpenComparisonIntentKeyring(%s) error = nil, want reject", tt.name)
			}
		})
	}
	_ = key
}

func TestComparisonIntentSignerRejectsForeignPurposeAndUnknownKey(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeComparisonKey(t, filepath.Join(dir, "cmp_test"), 0400)
	signer, err := OpenComparisonIntentKeyring(dir, "cmp_test", nil)
	if err != nil {
		t.Fatalf("OpenComparisonIntentKeyring() error = %v", err)
	}
	now := time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC)
	claims := BuildComparisonIntentClaims(ComparisonIntentClaimsInput{
		Actor:           testActor(t),
		Items:           []ResolvedComparisonItem{{SnapshotID: "evs_fixeda", Kind: CommandAuditV1Key(), RevisionContext: RevisionContextNotApplicable}},
		BaselineIndex:   0,
		Alignment:       CoverageActual,
		RequestedWindow: testWindow(),
		Now:             now,
		KeyID:           "cmp_test",
	})
	intent, err := signer.Sign(claims)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	tampered := stringsReplacePurpose(intent.Token, "deletion-confirm/v1")
	if _, err := signer.Verify(tampered, now); err == nil {
		t.Fatal("Verify(foreign purpose) error = nil, want reject")
	}
	otherDir := t.TempDir()
	writeComparisonKey(t, filepath.Join(otherDir, "cmp_other"), 0400)
	other, err := OpenComparisonIntentKeyring(otherDir, "cmp_other", nil)
	if err != nil {
		t.Fatalf("OpenComparisonIntentKeyring(other) error = %v", err)
	}
	if _, err := other.Verify(intent.Token, now); err == nil {
		t.Fatal("Verify(unknown key) error = nil, want reject")
	}
}

func TestComparisonIntentSignerRejectsTokenAfterKeyRotationOnReopen(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "cmp_test")
	writeComparisonKey(t, path, 0400)
	signer, err := OpenComparisonIntentKeyring(dir, "cmp_test", nil)
	if err != nil {
		t.Fatalf("OpenComparisonIntentKeyring() error = %v", err)
	}
	now := time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC)
	claims := BuildComparisonIntentClaims(ComparisonIntentClaimsInput{
		Actor:           testActor(t),
		Items:           []ResolvedComparisonItem{{SnapshotID: "evs_fixeda", Kind: CommandAuditV1Key(), RevisionContext: RevisionContextNotApplicable}},
		BaselineIndex:   0,
		Alignment:       CoverageActual,
		RequestedWindow: testWindow(),
		Now:             now,
		KeyID:           "cmp_test",
	})
	intent, err := signer.Sign(claims)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove old key: %v", err)
	}
	if err := os.WriteFile(path, []byte("comparison-intent-key-rotated-32bxx"), 0600); err != nil {
		t.Fatalf("rotate key: %v", err)
	}
	if err := os.Chmod(path, 0400); err != nil {
		t.Fatalf("chmod rotated key: %v", err)
	}
	reopened, err := OpenComparisonIntentKeyring(dir, "cmp_test", nil)
	if err != nil {
		t.Fatalf("OpenComparisonIntentKeyring(rotated) error = %v", err)
	}
	if _, err := reopened.Verify(intent.Token, now); !errors.Is(err, ErrComparisonIntentInvalid) {
		t.Fatalf("Verify after rotation error = %v, want %v", err, ErrComparisonIntentInvalid)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("revoke key: %v", err)
	}
	if _, err := OpenComparisonIntentKeyring(dir, "cmp_test", nil); !errors.Is(err, ErrComparisonIntentUnavailable) {
		t.Fatalf("OpenComparisonIntentKeyring(revoked) error = %v, want %v", err, ErrComparisonIntentUnavailable)
	}
}

func writeComparisonKey(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte("comparison-intent-key-material-32b"), 0600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod key: %v", err)
	}
}

func stringsReplacePurpose(token, purpose string) string {
	_ = purpose
	if token == "" {
		return "cmp1.forged.payload.mac"
	}
	return token + ".forged"
}
