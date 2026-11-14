//go:build linux && amd64 && !cgo

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func genesisFixture(t *testing.T, size int) []byte {
	t.Helper()
	prefix := `{"run_token":"` + runToken + `","boot_id":"fixture-boot","supervisor_pid":123,"supervisor_starttime":456,"padding":"`
	suffix := `"}`
	if size < len(prefix)+len(suffix) {
		t.Fatal("genesis fixture size too small")
	}
	return []byte(prefix + strings.Repeat("x", size-len(prefix)-len(suffix)) + suffix)
}

func genesisDirectory(t *testing.T) (string, int) {
	t.Helper()
	path := t.TempDir()
	if err := os.Chmod(path, 0700); err != nil {
		t.Fatal(err)
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Close(fd) })
	return path, fd
}

func writeGenesisFixtureFile(t *testing.T, dir, name string, data []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestGenesisBounds(t *testing.T) {
	for _, size := range []int{maxFrame + 1, maxGenesis, maxGenesis + 1} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			dir, fd := genesisDirectory(t)
			raw := genesisFixture(t, size)
			writeGenesisFixtureFile(t, dir, "realm-genesis.json", raw)

			read, readErr := readGenesisAt(fd)
			var facts genesisFacts
			parseErr := genesisDocument(raw, &facts)
			if size > maxGenesis {
				if !errors.Is(readErr, denied) || !errors.Is(parseErr, denied) {
					t.Fatalf("oversized genesis: read error %v, parse error %v", readErr, parseErr)
				}
			} else {
				if readErr != nil || !bytes.Equal(read, raw) {
					t.Fatalf("genesis read: error %v, size %d", readErr, len(read))
				}
				if parseErr != nil || facts.RunToken != runToken || facts.BootID != "fixture-boot" || facts.SupervisorPID != 123 || facts.SupervisorStarttime != 456 {
					t.Fatalf("genesis projection: %+v, error %v", facts, parseErr)
				}
			}
			if _, err := readPrivateAt(fd, "realm-genesis.json", 0, 0); !errors.Is(err, denied) {
				t.Fatalf("generic reader accepted %d bytes: %v", size, err)
			}
			if err := realmDocument(raw, &facts); !errors.Is(err, denied) {
				t.Fatalf("generic document accepted %d bytes: %v", size, err)
			}
			if err := checkJSON(raw, true); !errors.Is(err, denied) {
				t.Fatalf("generic JSON accepted %d bytes: %v", size, err)
			}
		})
	}

	t.Run("generic limit", func(t *testing.T) {
		dir, fd := genesisDirectory(t)
		raw := genesisFixture(t, maxFrame)
		writeGenesisFixtureFile(t, dir, "realm-app.json", raw)
		read, err := readPrivateAt(fd, "realm-app.json", 0, 0)
		var facts genesisFacts
		if err != nil || !bytes.Equal(read, raw) || realmDocument(raw, &facts) != nil || checkJSON(raw, true) != nil {
			t.Fatalf("generic limit rejected: %v", err)
		}
	})

	padding := strings.Repeat("x", maxFrame)
	invalid := map[string][]byte{
		"nested duplicate": []byte(`{"padding":"` + padding + `","nested":{"a":1,"a":2}}`),
		"invalid utf8":      append([]byte(`{"padding":"`+padding), []byte{0xff, '"', '}'}...),
		"second value":      []byte(`{"padding":"` + padding + `"} {}`),
		"depth":             []byte(`{"padding":"` + padding + `","nested":` + strings.Repeat("[", 65) + `0` + strings.Repeat("]", 65) + `}`),
	}
	for name, raw := range invalid {
		t.Run(name, func(t *testing.T) {
			var facts genesisFacts
			if len(raw) <= maxFrame || len(raw) > maxGenesis {
				t.Fatal("invalid fixture outside genesis-only range")
			}
			if err := genesisDocument(raw, &facts); !errors.Is(err, denied) {
				t.Fatalf("invalid genesis accepted: %v", err)
			}
		})
	}
	t.Run("null semantics", func(t *testing.T) {
		raw := []byte(`{"padding":"` + padding + `","optional":null}`)
		var facts genesisFacts
		if err := genesisDocument(raw, &facts); err != nil {
			t.Fatalf("genesis null rejected: %v", err)
		}
		if err := checkJSONBounded(raw, false, maxGenesis); !errors.Is(err, denied) {
			t.Fatalf("disallowed null accepted: %v", err)
		}
		if err := genesisDocument([]byte("null"), &facts); err != nil {
			t.Fatalf("top-level null rejected: %v", err)
		}
	})
}

func writeGenesisJournal(t *testing.T, dir string, app []byte, genesisDigest string) {
	t.Helper()
	fields := []struct {
		kind   string
		fields any
	}{
		{"genesis", map[string]any{"genesis_digest": genesisDigest}},
		{"app_binding", map[string]any{"app_digest": sha(app)}},
		{"phase_enter", map[string]any{"phase": "predecessor", "phase_generation": 1}},
		{"observation", map[string]any{"phase_generation": 1, "observation_sequence": 1, "roots_digest": strings.Repeat("a", 64), "valid": true}},
	}
	previous := strings.Repeat("0", 64)
	for i, item := range fields {
		event := map[string]any{"version": 1, "sequence": i + 1, "previous": previous, "kind": item.kind, "fields": item.fields}
		raw, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		writeGenesisFixtureFile(t, dir, fmt.Sprintf("journal-%020d.json", i+1), raw)
		previous = sha(raw)
	}
}

func TestGenesisJournalBinding(t *testing.T) {
	for _, size := range []int{maxFrame + 1, maxGenesis} {
		for _, scenario := range []string{"valid", "wrong first digest", "changed genesis"} {
			t.Run(fmt.Sprintf("%d/%s", size, scenario), func(t *testing.T) {
				dir, fd := genesisDirectory(t)
				genesis := genesisFixture(t, size)
				app := []byte(`{"run_token":"` + runToken + `"}`)
				writeGenesisFixtureFile(t, dir, "realm-genesis.json", genesis)
				writeGenesisFixtureFile(t, dir, "realm-app.json", app)
				digest := sha(genesis)
				if scenario == "wrong first digest" {
					digest = strings.Repeat("b", 64)
				}
				writeGenesisJournal(t, dir, app, digest)
				if scenario == "changed genesis" {
					genesis = bytes.Replace(genesis, []byte("fixture-boot"), []byte("fixture-booX"), 1)
					writeGenesisFixtureFile(t, dir, "realm-genesis.json", genesis)
				}
				intent := launchIntent{PhaseGeneration: 1, RealmDigest: realmDigest(genesis, app)}
				err := verifyRootJournal(fd, intent)
				if scenario == "valid" && err != nil {
					t.Fatalf("valid journal rejected: %v", err)
				}
				if scenario != "valid" && !errors.Is(err, denied) {
					t.Fatalf("invalid journal accepted: %v", err)
				}
			})
		}
	}
	t.Run("oversized genesis", func(t *testing.T) {
		dir, fd := genesisDirectory(t)
		genesis := genesisFixture(t, maxGenesis+1)
		app := []byte(`{"run_token":"` + runToken + `"}`)
		writeGenesisFixtureFile(t, dir, "realm-genesis.json", genesis)
		writeGenesisFixtureFile(t, dir, "realm-app.json", app)
		writeGenesisJournal(t, dir, app, sha(genesis))
		intent := launchIntent{PhaseGeneration: 1, RealmDigest: realmDigest(genesis, app)}
		if err := verifyRootJournal(fd, intent); !errors.Is(err, denied) {
			t.Fatalf("oversized genesis journal accepted: %v", err)
		}
	})
}
