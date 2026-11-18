//go:build linux

package deploy

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"houfeng/internal/center/sourceretirement"
)

func maintenanceTestConfig(t *testing.T) (string, SourceMaintenanceConfig) {
	t.Helper()
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	guard := filepath.Join(parent, "guard")
	identity, err := ProvisionMaintenanceGuard(guard)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(parent, "source-authority.json"), SourceMaintenanceConfig{
		GuardDirectory: guard, GuardIdentity: identity,
		Witness:           sourceretirement.DomainIdentity{ID: "rd-" + strings.Repeat("a", 64), SystemIdentifier: "123", DatabaseOID: 42, DatabaseName: "witness", Epoch: 1},
		ReaderDatabaseURL: "postgres://reader:secret@example.invalid:5432/witness?sslmode=require",
	}
}

func TestSourceMaintenanceConfigPublication(t *testing.T) {
	path, config := maintenanceTestConfig(t)
	if _, err := LoadSourceMaintenanceConfig(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing: %v", err)
	}
	if err := publishSourceMaintenanceConfig(path, config); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{
		"birth_installed": "false", "birth_lineage_id": `""`, "birth_protocol_version": "0", "birth_reader_database_url": `""`,
	} {
		if string(fields[name]) != want {
			t.Fatalf("initial publication omitted or changed %s", name)
		}
	}
	got, err := LoadSourceMaintenanceConfig(path)
	if err != nil || got != config {
		t.Fatalf("load: %v", err)
	}
	if err := publishSourceMaintenanceConfig(path, config); err != nil {
		t.Fatalf("exact replay: %v", err)
	}
	other := config
	other.ReaderDatabaseURL += "changed"
	if err := publishSourceMaintenanceConfig(path, other); err == nil {
		t.Fatal("conflicting publication accepted")
	}
	got, err = LoadSourceMaintenanceConfig(path)
	if err != nil || got != config {
		t.Fatalf("publication replaced config: %v", err)
	}
}

func TestSourceMaintenanceExplicitReplayCompletesLinkedPublication(t *testing.T) {
	path, config := maintenanceTestConfig(t)
	if err := publishSourceMaintenanceConfig(path, config); err != nil {
		t.Fatal(err)
	}
	temporary := filepath.Join(filepath.Dir(path), ".source-authority-"+strings.Repeat("a", 32))
	if err := os.Link(path, temporary); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSourceMaintenanceConfig(path); err == nil {
		t.Fatal("runtime accepted unfinished publication")
	}
	if err := finishSourceConfigPublication(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(temporary); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary remains: %v", err)
	}
	got, err := LoadSourceMaintenanceConfig(path)
	if err != nil || got != config {
		t.Fatalf("replay changed config: %v", err)
	}
}

func TestSourceMaintenanceConfigRejectsUnsafeAndMalformed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(t *testing.T, path string, data []byte)
	}{
		{"mode", func(t *testing.T, path string, _ []byte) {
			if err := os.Chmod(path, 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{"symlink", func(t *testing.T, path string, data []byte) {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			target := path + ".target"
			if err := os.WriteFile(target, data, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
		}},
		{"duplicate", func(t *testing.T, path string, data []byte) {
			data = append([]byte(`{"guard_directory":"duplicate",`), data[1:]...)
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"unknown", func(t *testing.T, path string, data []byte) {
			data = append([]byte(`{"unexpected":true,`), data[1:]...)
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"case", func(t *testing.T, path string, data []byte) {
			data = []byte(strings.Replace(string(data), `"guard_directory"`, `"GUARD_DIRECTORY"`, 1))
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"trailing", func(t *testing.T, path string, data []byte) {
			data = append(data, []byte(` {}`)...)
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path, config := maintenanceTestConfig(t)
			data, err := json.Marshal(sourceWire(config))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			tc.mutate(t, path, data)
			if _, err := LoadSourceMaintenanceConfig(path); err == nil || errors.Is(err, os.ErrNotExist) {
				t.Fatalf("unsafe file accepted or reported absent: %v", err)
			}
		})
	}
}

func TestSourceMaintenanceConfigRequiresPrivateParentOnlyWhenPresent(t *testing.T) {
	path, config := maintenanceTestConfig(t)
	if err := os.Chmod(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSourceMaintenanceConfig(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("absent: %v", err)
	}
	data, err := json.Marshal(sourceWire(config))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSourceMaintenanceConfig(path); err == nil {
		t.Fatal("accepted nonprivate parent")
	}
}

func installedMaintenanceTestConfig(config SourceMaintenanceConfig) SourceMaintenanceConfig {
	config.BirthInstalled = true
	config.BirthLineageID = "bl-" + strings.Repeat("a", 64)
	config.BirthProtocolVersion = 1
	config.BirthReaderDatabaseURL = "postgres://birth_reader:birth-secret@example.invalid:5432/witness?sslmode=require"
	return config
}

func replaceMaintenanceTestConfig(t *testing.T, path string, guard, expected, next SourceMaintenanceConfig) error {
	t.Helper()
	return WithMaintenanceGuard(t.Context(), guard.GuardDirectory, guard.GuardIdentity, true, func() error {
		return publishSourceMaintenanceConfigReplacement(path, expected, next)
	})
}

func TestSourceMaintenanceConfigRequiresExplicitBirthWire(t *testing.T) {
	fields := []string{
		"guard_directory", "guard_identity", "witness", "reader_database_url",
		"birth_installed", "birth_lineage_id", "birth_protocol_version", "birth_reader_database_url",
	}
	birthFields := fields[4:]
	for _, field := range fields {
		t.Run("missing_"+field, func(t *testing.T) {
			path, config := maintenanceTestConfig(t)
			if err := publishSourceMaintenanceConfig(path, config); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var object map[string]json.RawMessage
			if err := json.Unmarshal(data, &object); err != nil {
				t.Fatal(err)
			}
			delete(object, field)
			data, err = json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadSourceMaintenanceConfig(path); !errors.Is(err, errSourceMaintenance) {
				t.Fatalf("omitted field accepted or reported absent: %v", err)
			}
		})
	}
	for _, field := range birthFields {
		for _, mutation := range []string{"duplicate", "null"} {
			t.Run(mutation+"_"+field, func(t *testing.T) {
				path, config := maintenanceTestConfig(t)
				if err := publishSourceMaintenanceConfig(path, config); err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var object map[string]json.RawMessage
				if err := json.Unmarshal(data, &object); err != nil {
					t.Fatal(err)
				}
				if mutation == "duplicate" {
					prefix := []byte(`{"` + field + `":` + string(object[field]) + `,`)
					data = append(prefix, data[1:]...)
				} else {
					object[field] = json.RawMessage("null")
					data, err = json.Marshal(object)
					if err != nil {
						t.Fatal(err)
					}
				}
				if err := os.WriteFile(path, data, 0o600); err != nil {
					t.Fatal(err)
				}
				if _, err := LoadSourceMaintenanceConfig(path); !errors.Is(err, errSourceMaintenance) {
					t.Fatalf("invalid field accepted or reported absent: %v", err)
				}
			})
		}
	}
}

func TestSourceMaintenanceConfigBirthValidation(t *testing.T) {
	cases := []struct {
		name   string
		valid  bool
		mutate func(*SourceMaintenanceConfig)
	}{
		{"installed", true, func(*SourceMaintenanceConfig) {}},
		{"never_installed", true, func(c *SourceMaintenanceConfig) {
			c.BirthInstalled, c.BirthLineageID, c.BirthProtocolVersion, c.BirthReaderDatabaseURL = false, "", 0, ""
		}},
		{"disabled_lineage", false, func(c *SourceMaintenanceConfig) {
			c.BirthInstalled, c.BirthProtocolVersion, c.BirthReaderDatabaseURL = false, 0, ""
		}},
		{"disabled_protocol", false, func(c *SourceMaintenanceConfig) {
			c.BirthInstalled, c.BirthLineageID, c.BirthReaderDatabaseURL = false, "", ""
		}},
		{"disabled_reader", false, func(c *SourceMaintenanceConfig) {
			c.BirthInstalled, c.BirthLineageID, c.BirthProtocolVersion = false, "", 0
		}},
		{"missing_lineage", false, func(c *SourceMaintenanceConfig) { c.BirthLineageID = "" }},
		{"short_lineage", false, func(c *SourceMaintenanceConfig) { c.BirthLineageID = "bl-" + strings.Repeat("a", 63) }},
		{"uppercase_lineage", false, func(c *SourceMaintenanceConfig) { c.BirthLineageID = "bl-" + strings.Repeat("A", 64) }},
		{"invalid_lineage", false, func(c *SourceMaintenanceConfig) { c.BirthLineageID = "bl-" + strings.Repeat("g", 64) }},
		{"wrong_prefix", false, func(c *SourceMaintenanceConfig) { c.BirthLineageID = "rd-" + strings.Repeat("a", 64) }},
		{"missing_protocol", false, func(c *SourceMaintenanceConfig) { c.BirthProtocolVersion = 0 }},
		{"unknown_protocol", false, func(c *SourceMaintenanceConfig) { c.BirthProtocolVersion = 2 }},
		{"missing_reader", false, func(c *SourceMaintenanceConfig) { c.BirthReaderDatabaseURL = "" }},
		{"reader_without_port", false, func(c *SourceMaintenanceConfig) {
			c.BirthReaderDatabaseURL = "postgres://birth:secret@example.invalid/witness?sslmode=require"
		}},
		{"reader_without_tls_mode", false, func(c *SourceMaintenanceConfig) {
			c.BirthReaderDatabaseURL = "postgres://birth:secret@example.invalid:5432/witness"
		}},
		{"reader_role_override", false, func(c *SourceMaintenanceConfig) { c.BirthReaderDatabaseURL += "&user=other" }},
		{"source_reader_invalid", false, func(c *SourceMaintenanceConfig) {
			c.ReaderDatabaseURL = "postgres://reader:secret@example.invalid/witness"
		}},
		{"same_direct_role", false, func(c *SourceMaintenanceConfig) {
			c.BirthReaderDatabaseURL = "postgres://reader:other-secret@other.invalid:5433/other?sslmode=require"
		}},
		{"same_escaped_direct_role", false, func(c *SourceMaintenanceConfig) {
			c.BirthReaderDatabaseURL = "postgres://%72eader:other-secret@other.invalid:5433/other?sslmode=require"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path, config := maintenanceTestConfig(t)
			config = installedMaintenanceTestConfig(config)
			tc.mutate(&config)
			if err := publishSourceMaintenanceConfig(path, config); err != nil {
				t.Fatal(err)
			}
			got, err := LoadSourceMaintenanceConfig(path)
			if tc.valid {
				if err != nil || got != config {
					t.Fatalf("valid expectation rejected: %v", err)
				}
			} else if !errors.Is(err, errSourceMaintenance) {
				t.Fatalf("invalid expectation accepted or reported absent: %v", err)
			}
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Fatal("configuration error exposed a credential")
			}
		})
	}
}

func TestSourceMaintenanceConfigReplacementAndReplay(t *testing.T) {
	path, original := maintenanceTestConfig(t)
	if err := publishSourceMaintenanceConfig(path, original); err != nil {
		t.Fatal(err)
	}
	oldData, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	oldFile, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer oldFile.Close()
	oldInfo, err := oldFile.Stat()
	if err != nil {
		t.Fatal(err)
	}
	installed := installedMaintenanceTestConfig(original)
	if err := replaceMaintenanceTestConfig(t, path, original, original, installed); err != nil {
		t.Fatalf("install expectation: %v", err)
	}
	got, err := LoadSourceMaintenanceConfig(path)
	if err != nil || got != installed {
		t.Fatalf("load installed expectation: %v", err)
	}
	newInfo, err := os.Stat(path)
	if err != nil || os.SameFile(oldInfo, newInfo) {
		t.Fatalf("replacement did not publish a new inode: %v", err)
	}
	oldAfter, err := io.ReadAll(oldFile)
	if err != nil || !bytes.Equal(oldData, oldAfter) {
		t.Fatalf("replacement changed the previously opened file: %v", err)
	}
	// Simulate retrying after a rename whose result was not observed.
	if err := replaceMaintenanceTestConfig(t, path, original, original, installed); err != nil {
		t.Fatalf("replay replacement: %v", err)
	}
	if err := replaceMaintenanceTestConfig(t, path, original, installed, installed); err != nil {
		t.Fatalf("replay current expectation: %v", err)
	}
	replayedInfo, err := os.Stat(path)
	if err != nil || !os.SameFile(newInfo, replayedInfo) {
		t.Fatalf("exact replay replaced the inode: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*SourceMaintenanceConfig)
	}{
		{"clear_installed", func(c *SourceMaintenanceConfig) {
			c.BirthInstalled, c.BirthLineageID, c.BirthProtocolVersion, c.BirthReaderDatabaseURL = false, "", 0, ""
		}},
		{"change_lineage", func(c *SourceMaintenanceConfig) { c.BirthLineageID = "bl-" + strings.Repeat("b", 64) }},
		{"change_protocol", func(c *SourceMaintenanceConfig) { c.BirthProtocolVersion = 2 }},
		{"change_guard_directory", func(c *SourceMaintenanceConfig) { c.GuardDirectory += "-other" }},
		{"change_guard_identity", func(c *SourceMaintenanceConfig) { c.GuardIdentity.Nonce[0] ^= 0xff }},
		{"change_witness", func(c *SourceMaintenanceConfig) { c.Witness.Epoch++ }},
		{"reuse_source_reader_role", func(c *SourceMaintenanceConfig) {
			c.BirthReaderDatabaseURL = "postgres://reader:other-secret@other.invalid:5432/witness?sslmode=require"
		}},
		{"oversized", func(c *SourceMaintenanceConfig) {
			c.BirthReaderDatabaseURL = "postgres://birth_reader:" + strings.Repeat("s", sourceConfigLimit) + "@example.invalid:5432/witness?sslmode=require"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			next := installed
			tc.mutate(&next)
			if err := replaceMaintenanceTestConfig(t, path, original, installed, next); !errors.Is(err, errSourceMaintenance) {
				t.Fatalf("invalid replacement accepted: %v", err)
			}
			got, err := LoadSourceMaintenanceConfig(path)
			if err != nil || got != installed {
				t.Fatalf("invalid replacement altered protected state: %v", err)
			}
		})
	}
	staleNext := installed
	staleNext.BirthReaderDatabaseURL = "postgres://birth_reader:rotated@example.invalid:5432/witness?sslmode=require"
	if err := replaceMaintenanceTestConfig(t, path, original, original, staleNext); !errors.Is(err, errSourceMaintenance) {
		t.Fatalf("stale expected configuration accepted: %v", err)
	}
	got, err = LoadSourceMaintenanceConfig(path)
	if err != nil || got != installed {
		t.Fatalf("stale replacement altered protected state: %v", err)
	}

	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatal(err)
	}
	var before, after unix.Stat_t
	if err := unix.Stat(path, &before); err != nil {
		t.Fatal(err)
	}
	rotated := installed
	rotated.ReaderDatabaseURL = "postgres://source_next:rotated@example.invalid:5432/witness?sslmode=require"
	rotated.BirthReaderDatabaseURL = "postgres://birth_next:rotated@example.invalid:5432/witness?sslmode=require"
	if err := replaceMaintenanceTestConfig(t, path, original, installed, rotated); err != nil {
		t.Fatalf("rotate direct readers: %v", err)
	}
	got, err = LoadSourceMaintenanceConfig(path)
	if err != nil || got != rotated {
		t.Fatalf("load rotated readers: %v", err)
	}
	if err := unix.Stat(path, &after); err != nil {
		t.Fatal(err)
	}
	if after.Uid != uint32(os.Geteuid()) || after.Uid != before.Uid || after.Gid != before.Gid || after.Mode&0o7777 != 0o400 {
		t.Fatal("replacement changed protected ownership or mode")
	}
	if err := replaceMaintenanceTestConfig(t, path, original, installed, rotated); err != nil {
		t.Fatalf("replay rotated readers: %v", err)
	}
}

func TestSourceMaintenanceConfigReplacementRejectsUnsafePaths(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*testing.T, string)
	}{
		{"file_mode", func(t *testing.T, path string) {
			if err := os.Chmod(path, 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{"parent_mode", func(t *testing.T, path string) {
			if err := os.Chmod(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
		}},
		{"missing_file", func(t *testing.T, path string) {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}},
		{"file_symlink", func(t *testing.T, path string) {
			if err := os.Rename(path, path+".original"); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(path+".original", path); err != nil {
				t.Fatal(err)
			}
		}},
		{"hard_link", func(t *testing.T, path string) {
			if err := os.Link(path, path+".link"); err != nil {
				t.Fatal(err)
			}
		}},
		{"parent_symlink", func(t *testing.T, path string) {
			parent := filepath.Dir(path)
			moved := parent + "-moved"
			if err := os.Rename(parent, moved); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := os.Remove(parent); err != nil {
					t.Errorf("remove parent symlink: %v", err)
				}
				if err := os.Rename(moved, parent); err != nil {
					t.Errorf("restore fixture parent: %v", err)
				}
			})
			if err := os.Symlink(moved, parent); err != nil {
				t.Fatal(err)
			}
		}},
	}
	if os.Geteuid() == 0 {
		cases = append(cases, struct {
			name   string
			mutate func(*testing.T, string)
		}{"foreign_owner", func(t *testing.T, path string) {
			if err := os.Chown(path, 65534, -1); err != nil {
				if errors.Is(err, os.ErrPermission) {
					t.Skip("fixture cannot assign a different file owner")
				}
				t.Fatal(err)
			}
		}})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path, config := maintenanceTestConfig(t)
			if err := publishSourceMaintenanceConfig(path, config); err != nil {
				t.Fatal(err)
			}
			originalData, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			next := installedMaintenanceTestConfig(config)
			var replayErr error
			err = WithMaintenanceGuard(t.Context(), config.GuardDirectory, config.GuardIdentity, true, func() error {
				tc.mutate(t, path)
				replayErr = publishSourceMaintenanceConfigReplacement(path, config, config)
				return publishSourceMaintenanceConfigReplacement(path, config, next)
			})
			if !errors.Is(replayErr, errSourceMaintenance) {
				t.Fatalf("unsafe exact replay accepted: %v", replayErr)
			}
			if !errors.Is(err, errSourceMaintenance) {
				t.Fatalf("unsafe replacement accepted: %v", err)
			}
			if tc.name == "foreign_owner" {
				if err := os.Chown(path, os.Geteuid(), -1); err != nil {
					t.Fatal(err)
				}
			}
			if tc.name != "missing_file" {
				data, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(data, originalData) {
					t.Fatalf("unsafe replacement changed protected bytes: %v", err)
				}
			}
		})
	}
}
