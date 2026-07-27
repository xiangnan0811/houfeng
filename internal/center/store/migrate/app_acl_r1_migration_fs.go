package migrate

import (
	"fmt"
	"io"
	"io/fs"
	"sort"
)

// newAppACLR1MigrationFS returns the exact frozen r1 source view over an
// embedded migration filesystem. Later embedded migrations remain invisible
// to r1 convergence and runtime admission rather than becoming an accidental
// r1 input.
func newAppACLR1MigrationFS(backing fs.FS) fs.FS {
	return appACLR1MigrationFS{backing: backing}
}

type appACLR1MigrationFS struct {
	backing fs.FS
}

func (view appACLR1MigrationFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	if view.backing == nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	if name == "." {
		info, err := fs.Stat(view.backing, name)
		if err != nil {
			return nil, err
		}
		entries, err := view.ReadDir(name)
		if err != nil {
			return nil, err
		}
		return &appACLR1MigrationDirectory{info: info, entries: entries}, nil
	}
	if !isAppACLR1MigrationSource(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return view.backing.Open(name)
}

func (view appACLR1MigrationFS) ReadFile(name string) ([]byte, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: fs.ErrInvalid}
	}
	if view.backing == nil || !isAppACLR1MigrationSource(name) {
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: fs.ErrNotExist}
	}
	return fs.ReadFile(view.backing, name)
}

func (view appACLR1MigrationFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrInvalid}
	}
	if name != "." || view.backing == nil {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}

	entries, err := fs.ReadDir(view.backing, name)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]fs.DirEntry, len(entries))
	for _, entry := range entries {
		byName[entry.Name()] = entry
	}

	filtered := make([]fs.DirEntry, 0, len(appACLR1MigrationSourceContract))
	for _, expected := range appACLR1MigrationSourceContract {
		entry, ok := byName[expected.Filename]
		if !ok || entry.IsDir() {
			return nil, fmt.Errorf("frozen r1 migration source %q is unavailable", expected.Filename)
		}
		filtered = append(filtered, entry)
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Name() < filtered[j].Name()
	})
	return filtered, nil
}

func isAppACLR1MigrationSource(name string) bool {
	for _, expected := range appACLR1MigrationSourceContract {
		if expected.Filename == name {
			return true
		}
	}
	return false
}

type appACLR1MigrationDirectory struct {
	info    fs.FileInfo
	entries []fs.DirEntry
	offset  int
}

func (directory *appACLR1MigrationDirectory) Stat() (fs.FileInfo, error) {
	return directory.info, nil
}

func (*appACLR1MigrationDirectory) Read([]byte) (int, error) {
	return 0, io.EOF
}

func (*appACLR1MigrationDirectory) Close() error {
	return nil
}

func (directory *appACLR1MigrationDirectory) ReadDir(count int) ([]fs.DirEntry, error) {
	if directory.offset >= len(directory.entries) {
		if count > 0 {
			return nil, io.EOF
		}
		return []fs.DirEntry{}, nil
	}
	end := len(directory.entries)
	if count > 0 {
		remaining := end - directory.offset
		if count < remaining {
			end = directory.offset + count
		}
	}
	entries := append([]fs.DirEntry(nil), directory.entries[directory.offset:end]...)
	directory.offset = end
	return entries, nil
}
