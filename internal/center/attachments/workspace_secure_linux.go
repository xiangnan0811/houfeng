//go:build linux

package attachments

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const secureWorkspaceProcFDPath = "/proc/self/fd"

type linuxSecureProcessorWorkspaceRoot struct {
	file *os.File
	path string
}

type linuxSecureProcessorWorkspace struct {
	file       *os.File
	sourceFile *os.File
	path       string
}

func openSecureWorkspaceRoot(root string) (secureProcessorWorkspaceRoot, error) {
	if validateWorkspaceRootConfiguration(root) != nil {
		return nil, ErrUnsafeProcessorWorkspace
	}
	fd, err := openOrCreatePrivateRoot(filepath.Clean(root))
	if err != nil {
		return nil, ErrUnsafeProcessorWorkspace
	}
	file := os.NewFile(uintptr(fd), filepath.Clean(root))
	if file == nil {
		_ = unix.Close(fd)
		return nil, ErrUnsafeProcessorWorkspace
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		_ = file.Close()
		return nil, ErrUnsafeProcessorWorkspace
	}
	entries, err := file.Readdirnames(-1)
	if err != nil {
		_ = file.Close()
		return nil, ErrUnsafeProcessorWorkspace
	}
	for _, entry := range entries {
		if ValidateWorkspaceID(entry) != nil {
			_ = file.Close()
			return nil, ErrUnsafeProcessorWorkspace
		}
	}
	if err := unix.Fchmod(fd, 0o700); err != nil {
		_ = file.Close()
		return nil, ErrUnsafeProcessorWorkspace
	}
	return &linuxSecureProcessorWorkspaceRoot{file: file, path: filepath.Clean(root)}, nil
}

// openOrCreatePrivateRoot walks the absolute root from a held descriptor. It
// never asks the kernel to resolve a path through the caller's namespace, so a
// hostile same-UID process cannot swap an ancestor between validation and use.
func openOrCreatePrivateRoot(root string) (int, error) {
	cleaned := filepath.Clean(root)
	if !filepath.IsAbs(cleaned) || cleaned == string(filepath.Separator) {
		return -1, unix.EINVAL
	}
	fd, err := openAt2(unix.AT_FDCWD, string(filepath.Separator), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW,
		unix.RESOLVE_NO_SYMLINKS)
	if err != nil {
		return -1, err
	}
	components := strings.Split(strings.TrimPrefix(cleaned, string(filepath.Separator)), string(filepath.Separator))
	for _, component := range components {
		if component == "" || component == "." {
			continue
		}
		next, openErr := openAt2(fd, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW,
			unix.RESOLVE_BENEATH|unix.RESOLVE_NO_SYMLINKS)
		if errors.Is(openErr, unix.ENOENT) {
			if mkdirErr := unix.Mkdirat(fd, component, 0o700); mkdirErr != nil && !errors.Is(mkdirErr, unix.EEXIST) {
				_ = unix.Close(fd)
				return -1, mkdirErr
			}
			next, openErr = openAt2(fd, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW,
				unix.RESOLVE_BENEATH|unix.RESOLVE_NO_SYMLINKS)
		}
		if openErr != nil {
			_ = unix.Close(fd)
			return -1, openErr
		}
		_ = unix.Close(fd)
		fd = next
	}
	return fd, nil
}

func (root *linuxSecureProcessorWorkspaceRoot) close() error {
	if root == nil || root.file == nil {
		return nil
	}
	err := root.file.Close()
	root.file = nil
	return err
}

func (root *linuxSecureProcessorWorkspaceRoot) openWorkspace(
	ctx context.Context,
	workspaceID string,
) (secureProcessorWorkspace, error) {
	if root == nil || root.file == nil || ctx == nil || ValidateWorkspaceID(workspaceID) != nil {
		return nil, ErrInvalidProcessorCommand
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fd, err := openAt2(int(root.file.Fd()), workspaceID,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW,
		unix.RESOLVE_BENEATH|unix.RESOLVE_NO_SYMLINKS)
	if errors.Is(err, unix.ENOENT) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := unix.Mkdirat(int(root.file.Fd()), workspaceID, 0o700); err != nil && !errors.Is(err, unix.EEXIST) {
			return nil, ErrUnsafeProcessorWorkspace
		}
		fd, err = openAt2(int(root.file.Fd()), workspaceID,
			unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW,
			unix.RESOLVE_BENEATH|unix.RESOLVE_NO_SYMLINKS)
	}
	if err != nil {
		return nil, ErrUnsafeProcessorWorkspace
	}
	file := os.NewFile(uintptr(fd), workspaceID)
	if file == nil {
		_ = unix.Close(fd)
		return nil, ErrUnsafeProcessorWorkspace
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		_ = file.Close()
		return nil, ErrUnsafeProcessorWorkspace
	}
	if err := unix.Fchmod(fd, 0o700); err != nil {
		_ = file.Close()
		return nil, ErrUnsafeProcessorWorkspace
	}
	return &linuxSecureProcessorWorkspace{
		file: file,
		path: secureWorkspaceProcFDPath + "/" + strconv.FormatUint(uint64(file.Fd()), 10),
	}, nil
}

func (root *linuxSecureProcessorWorkspaceRoot) removeWorkspace(
	ctx context.Context,
	workspaceID string,
) (int64, error) {
	if root == nil || root.file == nil || ctx == nil || ValidateWorkspaceID(workspaceID) != nil {
		return 0, ErrInvalidProcessorCommand
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	// Open the entry relative to the held root FD.  If it is a symlink or
	// another non-directory, remove only that entry; never follow it.
	fd, err := openAt2(int(root.file.Fd()), workspaceID,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW,
		unix.RESOLVE_BENEATH|unix.RESOLVE_NO_SYMLINKS)
	if errors.Is(err, unix.ENOENT) {
		return 0, nil
	}
	if err != nil {
		var stat unix.Stat_t
		if statErr := unix.Fstatat(int(root.file.Fd()), workspaceID, &stat, unix.AT_SYMLINK_NOFOLLOW); statErr != nil {
			if errors.Is(statErr, unix.ENOENT) {
				return 0, nil
			}
			return 0, ErrUnsafeProcessorWorkspace
		}
		switch stat.Mode & unix.S_IFMT {
		case unix.S_IFLNK, unix.S_IFREG:
			if err := ctx.Err(); err != nil {
				return 0, err
			}
			if unlinkErr := unix.Unlinkat(int(root.file.Fd()), workspaceID, 0); unlinkErr != nil && !errors.Is(unlinkErr, unix.ENOENT) {
				return 0, ErrUnsafeProcessorWorkspace
			}
			return 1, nil
		default:
			return 0, ErrUnsafeProcessorWorkspace
		}
	}
	workspace := os.NewFile(uintptr(fd), workspaceID)
	if workspace == nil {
		_ = unix.Close(fd)
		return 0, ErrUnsafeProcessorWorkspace
	}
	secure := &linuxSecureProcessorWorkspace{file: workspace, path: secureWorkspaceProcFDPath + "/" + strconv.FormatUint(uint64(workspace.Fd()), 10)}
	removed, removeErr := secure.removeTree(ctx)
	closeErr := secure.close()
	if removeErr != nil {
		return 0, removeErr
	}
	if closeErr != nil {
		return 0, ErrUnsafeProcessorWorkspace
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := unix.Unlinkat(int(root.file.Fd()), workspaceID, unix.AT_REMOVEDIR); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return removed, nil
		}
		// A hostile replacement can only be removed as an entry.  Never recurse
		// through it after the descriptor was closed.
		var stat unix.Stat_t
		if statErr := unix.Fstatat(int(root.file.Fd()), workspaceID, &stat, unix.AT_SYMLINK_NOFOLLOW); statErr == nil &&
			stat.Mode&unix.S_IFMT == unix.S_IFLNK {
			if unlinkErr := unix.Unlinkat(int(root.file.Fd()), workspaceID, 0); unlinkErr == nil || errors.Is(unlinkErr, unix.ENOENT) {
				return removed + 1, nil
			}
		}
		return 0, ErrUnsafeProcessorWorkspace
	}
	return removed, nil
}

func (workspace *linuxSecureProcessorWorkspace) close() error {
	if workspace == nil {
		return nil
	}
	var closeErr error
	if workspace.sourceFile != nil {
		closeErr = workspace.sourceFile.Close()
		workspace.sourceFile = nil
	}
	if workspace.file != nil {
		if err := workspace.file.Close(); closeErr == nil {
			closeErr = err
		}
		workspace.file = nil
	}
	return closeErr
}

func (workspace *linuxSecureProcessorWorkspace) commandSourcePath() string {
	if workspace == nil {
		return ""
	}
	return filepath.Join(workspace.path, processorWorkspaceSourceName)
}

func (workspace *linuxSecureProcessorWorkspace) sourceSize(ctx context.Context) (int64, error) {
	file, err := workspace.openHeldOrNamedSource(ctx)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return 0, ErrInvalidPreviewContent
	}
	return int64(stat.Size), nil
}

func (workspace *linuxSecureProcessorWorkspace) readSourceBounded(ctx context.Context, limit int64) ([]byte, error) {
	file, err := workspace.openHeldOrNamedSource(ctx)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return readRegularFileBoundedReader(ctx, file, limit)
}

func (workspace *linuxSecureProcessorWorkspace) openHeldOrNamedSource(ctx context.Context) (*os.File, error) {
	if workspace == nil || workspace.file == nil || ctx == nil {
		return nil, ErrInvalidProcessorCommand
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if workspace.sourceFile != nil {
		fd, err := unix.Dup(int(workspace.sourceFile.Fd()))
		if err != nil {
			return nil, ErrInvalidPreviewContent
		}
		if _, err := unix.Seek(fd, 0, 0); err != nil {
			_ = unix.Close(fd)
			return nil, ErrInvalidPreviewContent
		}
		file := os.NewFile(uintptr(fd), processorWorkspaceSourceName)
		if file == nil {
			_ = unix.Close(fd)
			return nil, ErrInvalidPreviewContent
		}
		return file, nil
	}
	return workspace.openSource(ctx, unix.O_RDONLY)
}

func (workspace *linuxSecureProcessorWorkspace) materializeSource(
	ctx context.Context,
	source io.Reader,
	expected BlobObject,
	maxSourceBytes int64,
) error {
	if ctx == nil || source == nil || maxSourceBytes <= 0 {
		return ErrInvalidProcessorCommand
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	fd, err := openAt2(int(workspace.file.Fd()), processorWorkspaceSourceName,
		unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW,
		unix.RESOLVE_BENEATH|unix.RESOLVE_NO_SYMLINKS)
	if err != nil {
		return ErrUnsafeProcessorWorkspace
	}
	file := os.NewFile(uintptr(fd), processorWorkspaceSourceName)
	if file == nil {
		_ = unix.Close(fd)
		return ErrUnsafeProcessorWorkspace
	}
	if err := unix.Fchmod(fd, 0o600); err != nil {
		_ = file.Close()
		return ErrUnsafeProcessorWorkspace
	}
	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hasher), io.LimitReader(&contextReader{ctx: ctx, reader: source}, maxSourceBytes+1))
	if copyErr == nil {
		copyErr = file.Sync()
	}
	if contextError := ctx.Err(); contextError != nil {
		_ = file.Close()
		_ = unix.Unlinkat(int(workspace.file.Fd()), processorWorkspaceSourceName, 0)
		return contextError
	}
	if copyErr != nil {
		_ = file.Close()
		_ = unix.Unlinkat(int(workspace.file.Fd()), processorWorkspaceSourceName, 0)
		return ErrInvalidPreviewContent
	}
	if written > maxSourceBytes {
		_ = file.Close()
		_ = unix.Unlinkat(int(workspace.file.Fd()), processorWorkspaceSourceName, 0)
		return ErrPreviewLimitExceeded
	}
	if written != expected.SizeBytes || !equalHash(hasher, expected.SHA256) {
		_ = file.Close()
		_ = unix.Unlinkat(int(workspace.file.Fd()), processorWorkspaceSourceName, 0)
		return ErrInvalidPreviewContent
	}
	// Keep the validated inode anchored, but hand only a read-only descriptor
	// to preview readers and external commands.  Re-opening through the held
	// workspace descriptor and comparing file identity closes the replacement
	// window between the write/validation handle and the read handle.
	readFile, readErr := workspace.openValidatedReadSource(file)
	if readErr != nil {
		_ = file.Close()
		_ = unix.Unlinkat(int(workspace.file.Fd()), processorWorkspaceSourceName, 0)
		return readErr
	}
	if closeErr := file.Close(); closeErr != nil {
		_ = readFile.Close()
		_ = unix.Unlinkat(int(workspace.file.Fd()), processorWorkspaceSourceName, 0)
		return ErrUnsafeProcessorWorkspace
	}
	workspace.sourceFile = readFile
	return nil
}

func (workspace *linuxSecureProcessorWorkspace) openValidatedReadSource(writer *os.File) (*os.File, error) {
	if workspace == nil || workspace.file == nil || writer == nil {
		return nil, ErrInvalidProcessorCommand
	}
	fd, err := openAt2(int(workspace.file.Fd()), processorWorkspaceSourceName,
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		unix.RESOLVE_BENEATH|unix.RESOLVE_NO_SYMLINKS)
	if err != nil {
		return nil, ErrUnsafeProcessorWorkspace
	}
	readFile := os.NewFile(uintptr(fd), processorWorkspaceSourceName)
	if readFile == nil {
		_ = unix.Close(fd)
		return nil, ErrUnsafeProcessorWorkspace
	}
	writerInfo, writerErr := writer.Stat()
	readInfo, readErr := readFile.Stat()
	if writerErr != nil || readErr != nil || !writerInfo.Mode().IsRegular() || !readInfo.Mode().IsRegular() ||
		!os.SameFile(writerInfo, readInfo) {
		_ = readFile.Close()
		return nil, ErrUnsafeProcessorWorkspace
	}
	return readFile, nil
}

func (workspace *linuxSecureProcessorWorkspace) openSource(ctx context.Context, extraFlags int) (*os.File, error) {
	if workspace == nil || workspace.file == nil || ctx == nil {
		return nil, ErrInvalidProcessorCommand
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fd, err := openAt2(int(workspace.file.Fd()), processorWorkspaceSourceName,
		extraFlags|unix.O_NOFOLLOW,
		unix.RESOLVE_BENEATH|unix.RESOLVE_NO_SYMLINKS)
	if err != nil {
		return nil, ErrInvalidPreviewContent
	}
	file := os.NewFile(uintptr(fd), processorWorkspaceSourceName)
	if file == nil {
		_ = unix.Close(fd)
		return nil, ErrInvalidPreviewContent
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG {
		_ = file.Close()
		return nil, ErrInvalidPreviewContent
	}
	return file, nil
}

func (workspace *linuxSecureProcessorWorkspace) ensurePreviewAbsent(ctx context.Context) error {
	if workspace == nil || workspace.file == nil || ctx == nil {
		return ErrInvalidProcessorCommand
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var stat unix.Stat_t
	err := unix.Fstatat(int(workspace.file.Fd()), processorWorkspacePreviewName+".png", &stat, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return ErrInvalidPreviewContent
	}
	return ErrInvalidPreviewContent
}

func (workspace *linuxSecureProcessorWorkspace) preparePreviewDirectories(ctx context.Context, config PreviewConfig) error {
	if workspace == nil || workspace.file == nil || ctx == nil {
		return ErrInvalidProcessorCommand
	}
	for _, name := range []string{
		processorWorkspaceCacheName,
		processorWorkspaceConfigName,
		processorWorkspaceTempName,
		previewCommandRootDirectoryName,
	} {
		directory, err := workspace.ensureDirectory(ctx, int(workspace.file.Fd()), name)
		if err != nil {
			return ErrInvalidPreviewContent
		}
		if err := directory.Close(); err != nil {
			return ErrInvalidPreviewContent
		}
	}
	commandRoot, err := workspace.openDirectory(ctx, int(workspace.file.Fd()), previewCommandRootDirectoryName)
	if err != nil {
		return ErrInvalidPreviewContent
	}
	defer commandRoot.Close()
	workingDirectory, err := workspace.ensureDirectory(ctx, int(commandRoot.Fd()), previewCommandWorkingDirectoryName)
	if err != nil {
		return ErrInvalidPreviewContent
	}
	if err := workingDirectory.Close(); err != nil {
		return ErrInvalidPreviewContent
	}
	fontConfig, err := buildPrivateFontConfig(workspace.path)
	if err != nil {
		return ErrInvalidPreviewContent
	}
	fd, err := openAt2(int(workspace.file.Fd()), processorWorkspaceFontConfigName,
		unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW,
		unix.RESOLVE_BENEATH|unix.RESOLVE_NO_SYMLINKS)
	if err != nil {
		return ErrInvalidPreviewContent
	}
	file := os.NewFile(uintptr(fd), processorWorkspaceFontConfigName)
	if file == nil {
		_ = unix.Close(fd)
		return ErrInvalidPreviewContent
	}
	defer file.Close()
	if err := unix.Fchmod(fd, 0o600); err != nil {
		return ErrInvalidPreviewContent
	}
	if err := writeAndSyncPrivateFile(ctx, file, fontConfig); err != nil {
		_ = unix.Unlinkat(int(workspace.file.Fd()), processorWorkspaceFontConfigName, 0)
		return ErrInvalidPreviewContent
	}
	return nil
}

func (workspace *linuxSecureProcessorWorkspace) ensureDirectory(ctx context.Context, parentFD int, name string) (*os.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := unix.Mkdirat(parentFD, name, 0o700); err != nil && !errors.Is(err, unix.EEXIST) {
		return nil, err
	}
	return workspace.openDirectory(ctx, parentFD, name)
}

func (workspace *linuxSecureProcessorWorkspace) openDirectory(ctx context.Context, parentFD int, name string) (*os.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fd, err := openAt2(parentFD, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW,
		unix.RESOLVE_BENEATH|unix.RESOLVE_NO_SYMLINKS)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), name)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("invalid directory file")
	}
	if err := unix.Fchmod(fd, 0o700); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func (workspace *linuxSecureProcessorWorkspace) removeTree(ctx context.Context) (int64, error) {
	if workspace == nil || workspace.file == nil || ctx == nil {
		return 0, ErrInvalidProcessorCommand
	}
	return removeDirectoryEntries(ctx, int(workspace.file.Fd()))
}

func removeDirectoryEntries(ctx context.Context, dirFD int) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	dupFD, err := unix.Dup(dirFD)
	if err != nil {
		return 0, ErrUnsafeProcessorWorkspace
	}
	directory := os.NewFile(uintptr(dupFD), "workspace-read")
	if directory == nil {
		return 0, ErrUnsafeProcessorWorkspace
	}
	defer directory.Close()
	var removed int64
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		entries, readErr := directory.Readdirnames(1)
		if len(entries) == 0 && errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return 0, ErrUnsafeProcessorWorkspace
		}
		if len(entries) == 0 {
			continue
		}
		name := entries[0]
		var stat unix.Stat_t
		statErr := unix.Fstatat(dirFD, name, &stat, unix.AT_SYMLINK_NOFOLLOW)
		if errors.Is(statErr, unix.ENOENT) {
			continue
		}
		if statErr != nil {
			return 0, ErrUnsafeProcessorWorkspace
		}
		mode := stat.Mode & unix.S_IFMT
		switch mode {
		case unix.S_IFDIR:
			childFD, openErr := openAt2(dirFD, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW,
				unix.RESOLVE_BENEATH|unix.RESOLVE_NO_SYMLINKS)
			if openErr != nil {
				if errors.Is(openErr, unix.ENOENT) {
					continue
				}
				return 0, ErrUnsafeProcessorWorkspace
			}
			childRemoved, childErr := removeDirectoryEntries(ctx, childFD)
			_ = unix.Close(childFD)
			if childErr != nil {
				return 0, childErr
			}
			if err := ctx.Err(); err != nil {
				return 0, err
			}
			if err := unix.Unlinkat(dirFD, name, unix.AT_REMOVEDIR); err != nil && !errors.Is(err, unix.ENOENT) {
				return 0, ErrUnsafeProcessorWorkspace
			}
			removed += childRemoved
		case unix.S_IFREG, unix.S_IFLNK:
			if err := unix.Unlinkat(dirFD, name, 0); err != nil && !errors.Is(err, unix.ENOENT) {
				return 0, ErrUnsafeProcessorWorkspace
			}
			removed++
		default:
			return 0, ErrUnsafeProcessorWorkspace
		}
	}
	return removed, nil
}

func openAt2(dirFD int, path string, flags int, resolve uint64) (int, error) {
	if path == "" || strings.Contains(path, "\x00") {
		return -1, unix.EINVAL
	}
	return unix.Openat2(dirFD, path, &unix.OpenHow{Flags: uint64(flags), Resolve: resolve})
}
