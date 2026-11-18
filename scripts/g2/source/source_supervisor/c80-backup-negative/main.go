package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"houfeng/internal/center/recordbackup"
)

const (
	backupRoot    = "/var/lib/g2/managed-backups"
	stagedPath    = "staging/postgres_dump/db.v1"
	publishedPath = "published/postgres_dump/db.v1"
)

type stageError string

func (err stageError) Error() string { return string(err) }

type artifactResult struct {
	Path   string `json:"path"`
	Size   uint64 `json:"size"`
	SHA256 string `json:"sha256"`
}

type result struct {
	Version  uint64         `json:"version"`
	Status   string         `json:"status"`
	Artifact artifactResult `json:"artifact"`
}

func main() {
	os.Exit(mainStatus())
}

func mainStatus() (status int) {
	defer func() {
		if recover() != nil {
			_, _ = fmt.Fprintln(os.Stderr, "c80 backup negative: internal_unavailable")
			status = 1
		}
	}()
	os.Clearenv()
	out, err := run()
	if err != nil {
		code, ok := err.(stageError)
		if !ok {
			code = "internal_unavailable"
		}
		_, _ = fmt.Fprintln(os.Stderr, "c80 backup negative:", code)
		return 1
	}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "c80 backup negative: output_unavailable")
		return 1
	}
	return 0
}

func run() (result, error) {
	if len(os.Args) != 2 || os.Args[1] != "produce" || os.Getuid() != 0 || os.Geteuid() != 0 ||
		os.Getgid() != 0 || os.Getegid() != 0 {
		return result{}, stageError("invocation_unavailable")
	}
	// Keep the calling thread alive so the kernel terminates this producer if
	// the supervising root parent dies.
	runtime.LockOSThread()
	parent := os.Getppid()
	if parent <= 1 || unix.Prctl(unix.PR_SET_PDEATHSIG, uintptr(unix.SIGKILL), 0, 0, 0) != nil || os.Getppid() != parent {
		return result{}, stageError("parent_unavailable")
	}
	req, err := readRequest()
	if err != nil {
		return result{}, stageError("private_input_unavailable")
	}
	dump, err := openDump(req)
	if err != nil {
		return result{}, stageError("dump_unavailable")
	}
	defer dump.Close()
	if checkRoots() != nil || !absent(backupRoot+"/staging/postgres_dump") ||
		!absent(backupRoot+"/published/postgres_dump") {
		return result{}, stageError("store_unavailable")
	}
	artifact, err := recordbackup.NewArtifactRef("postgres_dump", "db.v1", req.dumpDigest, req.dumpSize, recordbackup.ClassificationDatabase)
	if err != nil {
		return result{}, stageError("artifact_unavailable")
	}
	store, err := recordbackup.NewLocalStore(backupRoot)
	if err != nil {
		return result{}, stageError("store_unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := store.Stage(ctx, artifact, io.NewSectionReader(dump, 0, int64(req.dumpSize))); err != nil || ctx.Err() != nil {
		return result{}, stageError("stage_unavailable")
	}
	if syncDirectory(backupRoot+"/staging/postgres_dump") != nil || syncDirectory(backupRoot+"/staging") != nil {
		return result{}, stageError("stage_unavailable")
	}
	path, status := stagedPath, "staged"
	if req.mode == "published" {
		if err := store.Publish(ctx, artifact); err != nil || ctx.Err() != nil {
			return result{}, stageError("publish_unavailable")
		}
		if syncDirectory(backupRoot+"/staging/postgres_dump") != nil || syncDirectory(backupRoot+"/staging") != nil ||
			syncDirectory(backupRoot+"/published/postgres_dump") != nil || syncDirectory(backupRoot+"/published") != nil {
			return result{}, stageError("publish_unavailable")
		}
		path, status = publishedPath, "published"
	}
	size, digest, err := readArtifact(backupRoot+"/"+path, req.dumpSize)
	if err != nil || digest != req.dumpDigest || ctx.Err() != nil {
		return result{}, stageError("readback_unavailable")
	}
	return result{
		Version: 1, Status: status,
		Artifact: artifactResult{Path: path, Size: size, SHA256: hex.EncodeToString(digest[:])},
	}, nil
}

func openDump(req request) (*os.File, error) {
	file := os.NewFile(5, "c80-backup-negative-dump")
	if file == nil {
		return nil, errInput
	}
	fail := func() (*os.File, error) {
		_ = file.Close()
		return nil, errInput
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != int64(req.dumpSize) || !sealedMemfd(file) {
		return fail()
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || stat.Gid != 0 {
		return fail()
	}
	flags, err := unix.FcntlInt(file.Fd(), unix.F_GETFL, 0)
	if err != nil || flags&unix.O_ACCMODE != unix.O_RDONLY || flags&unix.O_PATH != 0 {
		return fail()
	}
	hash := sha256.New()
	n, err := io.Copy(hash, io.NewSectionReader(file, 0, int64(req.dumpSize)))
	if err != nil || n != int64(req.dumpSize) {
		return fail()
	}
	var digest [32]byte
	copy(digest[:], hash.Sum(nil))
	if digest != req.dumpDigest {
		return fail()
	}
	return file, nil
}

func absent(path string) bool {
	_, err := os.Lstat(path)
	return os.IsNotExist(err)
}

func checkRoots() error {
	for _, path := range []string{"/var", "/var/lib", "/var/lib/g2"} {
		if err := checkDirectory(path, false); err != nil {
			return err
		}
	}
	for _, path := range []string{backupRoot, backupRoot + "/staging", backupRoot + "/published", backupRoot + "/multipart", backupRoot + "/pins"} {
		if err := checkDirectory(path, true); err != nil {
			return err
		}
	}
	return nil
}

func checkDirectory(path string, private bool) error {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 ||
		(private && info.Mode().Perm() != 0700) || (!private && info.Mode().Perm()&0022 != 0) {
		return errors.New("directory unavailable")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || stat.Gid != 0 {
		return errors.New("directory unavailable")
	}
	return nil
}

func syncDirectory(path string) error {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return errors.New("directory sync unavailable")
	}
	defer unix.Close(fd)
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Mode&07777 != 0700 ||
		stat.Uid != 0 || stat.Gid != 0 || unix.Fsync(fd) != nil {
		return errors.New("directory sync unavailable")
	}
	return nil
}

func readArtifact(path string, expectedSize uint64) (uint64, [32]byte, error) {
	var digest [32]byte
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return 0, digest, errors.New("artifact unavailable")
	}
	file := os.NewFile(uintptr(fd), "c80-backup-negative-artifact")
	if file == nil {
		_ = unix.Close(fd)
		return 0, digest, errors.New("artifact unavailable")
	}
	defer file.Close()
	var before, after unix.Stat_t
	if unix.Fstat(fd, &before) != nil || before.Mode&unix.S_IFMT != unix.S_IFREG || before.Mode&07777 != 0600 ||
		before.Uid != 0 || before.Gid != 0 || before.Nlink != 1 || before.Size != int64(expectedSize) {
		return 0, digest, errors.New("artifact unavailable")
	}
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(file, int64(expectedSize)+1))
	if err != nil || n != int64(expectedSize) || unix.Fstat(fd, &after) != nil || before.Dev != after.Dev ||
		before.Ino != after.Ino || before.Size != after.Size || before.Mode != after.Mode || before.Uid != after.Uid ||
		before.Gid != after.Gid || before.Nlink != after.Nlink || before.Mtim != after.Mtim || before.Ctim != after.Ctim {
		return 0, digest, errors.New("artifact unavailable")
	}
	copy(digest[:], hash.Sum(nil))
	return uint64(n), digest, nil
}
