package recordreadiness

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func findRepositoryTestNames(root string, names []string) (map[string]string, error) {
	present := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if shouldSkipRepositoryCorpusDir(root, path, entry) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		payload, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, testName := range names {
			if bytes.Contains(payload, []byte("func "+testName+"(")) {
				present[testName] = path
			}
		}
		return nil
	})
	return present, err
}

func shouldSkipRepositoryCorpusDir(root, path string, entry fs.DirEntry) bool {
	switch entry.Name() {
	case ".git", "node_modules", "web", "bin", "dist":
		return true
	case "tmp":
		return filepath.Clean(path) == filepath.Join(filepath.Clean(root), "tmp")
	default:
		return false
	}
}
