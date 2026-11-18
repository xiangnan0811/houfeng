//go:build linux && amd64 && !cgo

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestRuntimeInputDiagnostic(t *testing.T) {
	valid := unix.Stat_t{Mode: unix.S_IFREG | 0600, Uid: 0, Gid: 0, Nlink: 1, Size: 1}
	cases := []struct {
		name         string
		change       func(*unix.Stat_t)
		file, digest ownerProbeRuntimeStage
	}{
		{"valid-0600", func(*unix.Stat_t) {}, 0, 0},
		{"valid-0644", func(s *unix.Stat_t) { s.Mode = unix.S_IFREG | 0644 }, 0, 0},
		{"owner-write-special", func(s *unix.Stat_t) { s.Mode = unix.S_IFREG | 07700 }, 0, 0},
		{"type", func(s *unix.Stat_t) { s.Mode = unix.S_IFDIR | 0600 }, 84, 117},
		{"uid", func(s *unix.Stat_t) { s.Uid = 1 }, 85, 118},
		{"gid", func(s *unix.Stat_t) { s.Gid = 1 }, 86, 119},
		{"write", func(s *unix.Stat_t) { s.Mode |= 0020 }, 87, 120},
		{"links", func(s *unix.Stat_t) { s.Nlink = 2 }, 88, 0},
		{"size-zero", func(s *unix.Stat_t) { s.Size = 0 }, 89, 121},
		{"size-negative", func(s *unix.Stat_t) { s.Size = -1 }, 89, 121},
		{"size-high", func(s *unix.Stat_t) { s.Size = runtimeInputLimit + 1 }, 90, 122},
		{"first-type", func(s *unix.Stat_t) { s.Mode = unix.S_IFDIR | 0666; s.Uid = 1; s.Gid = 1; s.Nlink = 2; s.Size = 0 }, 84, 117},
		{"first-uid", func(s *unix.Stat_t) { s.Uid = 1; s.Gid = 1; s.Mode |= 0022; s.Size = 0 }, 85, 118},
		{"first-links", func(s *unix.Stat_t) { s.Nlink = 2; s.Size = 0 }, 88, 121},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := valid
			tc.change(&st)
			if got := runtimeHeldFileMetadataStage(st); got != tc.file {
				t.Fatalf("file stage=%d want=%d", got, tc.file)
			}
			if got := heldRegularDigestMetadataStage(st, runtimeInputLimit); got != tc.digest {
				t.Fatalf("digest stage=%d want=%d", got, tc.digest)
			}
		})
	}
	base := unix.Stat_t{Dev: 1, Ino: 2, Mode: unix.S_IFREG | 0600, Uid: 3, Gid: 4, Nlink: 5, Size: 6, Mtim: unix.Timespec{Sec: 7, Nsec: 8}, Ctim: unix.Timespec{Sec: 9, Nsec: 10}}
	fields := []struct {
		name   string
		change func(*unix.Stat_t)
		want   runtimeStatDifference
	}{
		{"dev", func(s *unix.Stat_t) { s.Dev++ }, runtimeStatDev},
		{"ino", func(s *unix.Stat_t) { s.Ino++ }, runtimeStatIno},
		{"mode", func(s *unix.Stat_t) { s.Mode++ }, runtimeStatMode},
		{"uid", func(s *unix.Stat_t) { s.Uid++ }, runtimeStatUID},
		{"gid", func(s *unix.Stat_t) { s.Gid++ }, runtimeStatGID},
		{"nlink", func(s *unix.Stat_t) { s.Nlink++ }, runtimeStatNlink},
		{"size", func(s *unix.Stat_t) { s.Size++ }, runtimeStatSize},
		{"mtime", func(s *unix.Stat_t) { s.Mtim.Nsec++ }, runtimeStatMtime},
		{"ctime", func(s *unix.Stat_t) { s.Ctim.Nsec++ }, runtimeStatCtime},
		{"multiple", func(s *unix.Stat_t) { s.Ino++; s.Size++; s.Ctim.Sec++ }, runtimeStatIno},
		{"atime-only", func(s *unix.Stat_t) { s.Atim.Sec++ }, runtimeStatSame},
	}
	original := base
	for _, tc := range fields {
		t.Run(tc.name, func(t *testing.T) {
			other := base
			tc.change(&other)
			before := other
			got := trustedStatDifference(base, other)
			if got != tc.want || sameTrustedStat(base, other) != (tc.want == runtimeStatSame) {
				t.Fatalf("difference=%d want=%d", got, tc.want)
			}
			if base != original || other != before {
				t.Fatal("comparison modified inputs")
			}
			var d runtimeInputDiagnostic
			d.rejectDifference(ownerProbeStageWrapperPrepareFileHeldDev, got)
			if got == runtimeStatSame && d.stage != 0 || got != runtimeStatSame && d.stage != ownerProbeStageWrapperPrepareFileHeldDev+ownerProbeRuntimeStage(got)-1 {
				t.Fatalf("difference stage=%d", d.stage)
			}
		})
	}
	bad := valid
	bad.Uid = 1
	var sink *runtimeInputDiagnostic
	sink.reject(runtimeHeldFileMetadataStage(bad))
	sink.rejectDifference(ownerProbeStageWrapperPrepareFileHeldDev, runtimeStatDev)
	if runtimeHeldFileMetadataStage(bad) != 85 {
		t.Fatal("nil sink changed policy")
	}
	var d runtimeInputDiagnostic
	d.reject(runtimeHeldFileMetadataStage(bad))
	d.rejectDifference(ownerProbeStageWrapperPrepareFileHeldDev, runtimeStatIno)
	d.reject(90)
	if d.stage != 85 {
		t.Fatalf("first rejection overwritten: %d", d.stage)
	}
	var empty runtimeInputDiagnostic
	empty.rejectDifference(ownerProbeStageWrapperPrepareFileHeldDev, runtimeStatSame)
	if empty.stage != 0 {
		t.Fatal("same metadata recorded")
	}
}

func runtimeInputTestDir(t *testing.T) (string, int) {
	t.Helper()
	dir, err := os.MkdirTemp("/run/g2", "input-diagnostic-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	fd, err := unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Close(fd) })
	return dir, fd
}

func runtimeInputWriteFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRuntimeInputCustody(t *testing.T) {
	ownerRootPrerequisites(t)
	dir, dirFD := runtimeInputTestDir(t)
	payload := []byte("valid input payload")
	path := runtimeInputWriteFile(t, dir, "valid", payload)
	wantSum := sha256.Sum256(payload)
	wantHash := hex.EncodeToString(wantSum[:])
	beforeFDs, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	var d runtimeInputDiagnostic
	file, digest, err := runtimeHeldFileWithDiagnostic(dirFD, "valid", &d)
	if err != nil || d.stage != 0 || digest != wantHash || file == nil {
		t.Fatalf("valid: digest=%q stage=%d err=%v", digest, d.stage, err)
	}
	var held, named unix.Stat_t
	if err = unix.Fstat(int(file.Fd()), &held); err != nil {
		t.Fatal(err)
	}
	if err = unix.Stat(path, &named); err != nil || !sameTrustedStat(held, named) {
		t.Fatalf("held identity: %v", err)
	}
	if offset, e := unix.Seek(int(file.Fd()), 0, 1); e != nil || offset != 0 {
		t.Fatalf("initial offset=%d err=%v", offset, e)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	afterFDs, err := os.ReadDir("/proc/self/fd")
	if err != nil || len(beforeFDs) != len(afterFDs) {
		t.Fatalf("descriptor count %d -> %d: %v", len(beforeFDs), len(afterFDs), err)
	}
	ordinary, ordinaryHash, err := runtimeHeldFile(dirFD, "valid")
	if err != nil || ordinary == nil || ordinaryHash != wantHash {
		t.Fatalf("ordinary valid digest=%q err=%v", ordinaryHash, err)
	}
	if offset, e := unix.Seek(int(ordinary.Fd()), 0, 1); e != nil || offset != 0 {
		t.Fatalf("ordinary initial offset=%d err=%v", offset, e)
	}
	if err = ordinary.Close(); err != nil {
		t.Fatal(err)
	}
	if err = unix.Fstat(dirFD, &held); err != nil {
		t.Fatalf("directory fd closed: %v", err)
	}

	for _, tc := range []struct {
		name    string
		mode    os.FileMode
		content []byte
		link    bool
		stage   ownerProbeRuntimeStage
	}{
		{"empty", 0600, nil, false, 89},
		{"world-write", 0666, payload, false, 87},
		{"group-write", 0620, payload, false, 87},
		{"hardlink", 0600, payload, true, 88},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := runtimeInputWriteFile(t, dir, tc.name, tc.content)
			if err := os.Chmod(p, tc.mode); err != nil {
				t.Fatal(err)
			}
			st, err := os.Stat(p)
			if err != nil || st.Mode().Perm() != tc.mode {
				t.Fatalf("mode=%v err=%v", st, err)
			}
			if tc.link {
				if err := os.Link(p, p+"-link"); err != nil {
					t.Fatal(err)
				}
			}
			var diag runtimeInputDiagnostic
			got, _, err := runtimeHeldFileWithDiagnostic(dirFD, tc.name, &diag)
			if got != nil || err != errGuardProtocol || diag.stage != tc.stage {
				t.Fatalf("stage=%d file=%v err=%v", diag.stage, got, err)
			}
			got, _, err = runtimeHeldFile(dirFD, tc.name)
			if got != nil || err != errGuardProtocol {
				t.Fatalf("ordinary file=%v err=%v", got, err)
			}
		})
	}
	if err := os.Symlink("valid", filepath.Join(dir, "symlink")); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		stage ownerProbeRuntimeStage
		errno error
	}{
		{"", 80, errGuardProtocol}, {"bad/name", 80, errGuardProtocol}, {"missing", 81, unix.ENOENT}, {"symlink", 81, unix.ELOOP},
	} {
		var diag runtimeInputDiagnostic
		got, _, err := runtimeHeldFileWithDiagnostic(dirFD, tc.name, &diag)
		if got != nil || diag.stage != tc.stage || !errors.Is(err, tc.errno) {
			t.Fatalf("%q stage=%d err=%v", tc.name, diag.stage, err)
		}
		got, _, err = runtimeHeldFile(dirFD, tc.name)
		if got != nil || !errors.Is(err, tc.errno) {
			t.Fatalf("ordinary %q err=%v", tc.name, err)
		}
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if _, err = unix.Seek(fd, 3, 0); err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range []bool{true, false} {
		var diag runtimeInputDiagnostic
		var hash string
		var raw []byte
		if diagnostic {
			hash, _, err = heldRegularDigestWithDiagnostic(fd, runtimeInputLimit, &diag)
		} else {
			hash, _, err = heldRegularDigest(fd, runtimeInputLimit)
		}
		if err != nil || hash != wantHash || diag.stage != 0 {
			t.Fatalf("digest diagnostic=%v hash=%q stage=%d err=%v", diagnostic, hash, diag.stage, err)
		}
		if offset, e := unix.Seek(fd, 0, 1); e != nil || offset != 3 {
			t.Fatalf("digest shared offset=%d err=%v", offset, e)
		}
		if e := unix.Fstat(fd, &held); e != nil {
			t.Fatalf("digest closed caller fd: %v", e)
		}
		if diagnostic {
			raw, err = readHeldRegularWithDiagnostic(fd, runtimeInputLimit, &diag)
		} else {
			raw, err = readHeldRegular(fd, runtimeInputLimit)
		}
		if err != nil || !bytes.Equal(raw, payload) || diag.stage != 0 {
			t.Fatalf("read diagnostic=%v stage=%d err=%v", diagnostic, diag.stage, err)
		}
		if offset, e := unix.Seek(fd, 0, 1); e != nil || offset != 3 {
			t.Fatalf("shared offset=%d err=%v", offset, e)
		}
		if e := unix.Fstat(fd, &held); e != nil {
			t.Fatalf("caller fd closed: %v", e)
		}
	}
	var negative runtimeInputDiagnostic
	if _, _, err = heldRegularDigestWithDiagnostic(-1, runtimeInputLimit, &negative); err != errGuardProtocol || negative.stage != 115 {
		t.Fatalf("negative fd stage=%d err=%v", negative.stage, err)
	}
	if _, _, err = heldRegularDigest(-1, runtimeInputLimit); err != errGuardProtocol {
		t.Fatalf("ordinary negative fd err=%v", err)
	}
	var negativeRead runtimeInputDiagnostic
	if _, err = readHeldRegularWithDiagnostic(-1, runtimeInputLimit, &negativeRead); err != errGuardProtocol || negativeRead.stage != 115 {
		t.Fatalf("negative reader stage=%d err=%v", negativeRead.stage, err)
	}
	if _, err = readHeldRegular(-1, runtimeInputLimit); err != errGuardProtocol {
		t.Fatalf("ordinary negative reader err=%v", err)
	}
	dup, err := unix.FcntlInt(uintptr(fd), unix.F_DUPFD_CLOEXEC, 256)
	if err != nil {
		t.Fatal(err)
	}
	if err = unix.Close(dup); err != nil {
		t.Fatal(err)
	}
	var closed runtimeInputDiagnostic
	if _, _, err = heldRegularDigestWithDiagnostic(dup, runtimeInputLimit, &closed); err != errGuardProtocol || closed.stage != 116 {
		t.Fatalf("closed fd stage=%d err=%v", closed.stage, err)
	}
	dup, err = unix.FcntlInt(uintptr(fd), unix.F_DUPFD_CLOEXEC, 256)
	if err != nil {
		t.Fatal(err)
	}
	if err = unix.Close(dup); err != nil {
		t.Fatal(err)
	}
	var closedRead runtimeInputDiagnostic
	if _, err = readHeldRegularWithDiagnostic(dup, runtimeInputLimit, &closedRead); err != errGuardProtocol || closedRead.stage != 116 {
		t.Fatalf("closed reader stage=%d err=%v", closedRead.stage, err)
	}
	if err = unix.Fstat(dirFD, &held); err != nil {
		t.Fatalf("directory fd closed: %v", err)
	}
}

func TestRuntimePrepareDiagnostic(t *testing.T) {
	ownerRootPrerequisites(t)
	dir, dirFD := runtimeInputTestDir(t)
	cases := []struct {
		name, raw string
		stage     ownerProbeRuntimeStage
		jsonErr   bool
	}{
		{"normal", `{"process":{"args":["/usr/bin/id","-u","postgres"]}}`, 0, false},
		{"no-args", `{"process":{}}`, 0, false},
		{"non-array", `{"process":{"args":1}}`, 0, false},
		{"other-args", `{"process":{"args":["other"]}}`, 0, false},
		{"missing-process", `{}`, 149, false},
		{"number-process", `{"process":1}`, 149, false},
		{"malformed", `{`, 148, true},
		{"duplicate", `{"process":{},"process":{}}`, 148, true},
		{"number-format", `{"process":{},"n":1e0}`, 148, true},
		{"array", `[]`, 148, true},
		{"selftest", `{"process":{"args":["/opt/g2/g2-container-bpf-probe","selftest","container-raw-denial"]}}`, 150, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := runtimeInputWriteFile(t, dir, tc.name, []byte(tc.raw))
			var before, after unix.Stat_t
			if err := unix.Stat(path, &before); err != nil {
				t.Fatal(err)
			}
			var diag runtimeInputDiagnostic
			err := prepareContainerSelftestBundle(dirFD, dir, tc.name, &diag)
			if diag.stage != tc.stage {
				t.Fatalf("stage=%d want=%d err=%v", diag.stage, tc.stage, err)
			}
			if tc.jsonErr {
				_, expected := runtimeJSON([]byte(tc.raw))
				if expected == nil || err != expected {
					t.Fatalf("parser error=%v prepare error=%v", expected, err)
				}
			} else if tc.stage == 0 && err != nil || tc.stage != 0 && err != errGuardProtocol {
				t.Fatalf("stage=%d err=%v", tc.stage, err)
			}
			if err := unix.Stat(path, &after); err != nil || !sameTrustedStat(before, after) {
				t.Fatalf("source identity changed: %v", err)
			}
			raw, err := os.ReadFile(path)
			if err != nil || string(raw) != tc.raw {
				t.Fatalf("source changed: %v", err)
			}
			if err := unix.Fstat(dirFD, &after); err != nil {
				t.Fatalf("directory fd closed: %v", err)
			}
		})
	}
	var missing runtimeInputDiagnostic
	if err := prepareContainerSelftestBundle(dirFD, dir, "missing", &missing); !errors.Is(err, unix.ENOENT) || missing.stage != 81 {
		t.Fatalf("open stage=%d err=%v", missing.stage, err)
	}
	p := runtimeInputWriteFile(t, dir, "writable", []byte(`{}`))
	if err := os.Chmod(p, 0666); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(p); err != nil || st.Mode().Perm() != 0666 {
		t.Fatalf("writable mode=%v err=%v", st, err)
	}
	var writable runtimeInputDiagnostic
	if err := prepareContainerSelftestBundle(dirFD, dir, "writable", &writable); err != errGuardProtocol || writable.stage != 87 {
		t.Fatalf("metadata stage=%d err=%v", writable.stage, err)
	}
}
