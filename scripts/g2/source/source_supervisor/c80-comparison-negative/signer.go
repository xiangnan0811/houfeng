package main

import (
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"syscall"

	"houfeng/internal/center/evidence"
)

const signerParent = "/var/lib/g2/control/maintenance/comparison-negative"

// The production signer requires a private regular key file. This one-shot
// fixture creates its own random key beneath the supervisor's fixed directory;
// the token and key never leave the process or appear in its output.
func openSigner() (*evidence.FileComparisonIntentSigner, func() error, error) {
	info, err := os.Lstat(signerParent)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 || info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, errors.New("signer parent unavailable")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 10002 || stat.Gid != 10002 {
		return nil, nil, errors.New("signer parent unavailable")
	}
	dir, err := os.MkdirTemp(signerParent, "signer-")
	if err != nil {
		return nil, nil, errors.New("signer directory unavailable")
	}
	keyPath := filepath.Join(dir, "comparison")
	cleanup := func() error {
		keyErr := os.Remove(keyPath)
		if keyErr != nil && !os.IsNotExist(keyErr) {
			return errors.New("signer cleanup unavailable")
		}
		if err := os.Remove(dir); err != nil {
			return errors.New("signer cleanup unavailable")
		}
		return nil
	}
	fail := func() (*evidence.FileComparisonIntentSigner, func() error, error) {
		_ = cleanup()
		return nil, nil, errors.New("signer unavailable")
	}
	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		return fail()
	}
	file, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fail()
	}
	n, writeErr := file.Write(key[:])
	for i := range key {
		key[i] = 0
	}
	modeErr := file.Chmod(0400)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || n != 32 || modeErr != nil || syncErr != nil || closeErr != nil {
		return fail()
	}
	signer, err := evidence.OpenComparisonIntentKeyring(dir, "comparison", nil)
	if err != nil {
		return fail()
	}
	return signer, cleanup, nil
}
