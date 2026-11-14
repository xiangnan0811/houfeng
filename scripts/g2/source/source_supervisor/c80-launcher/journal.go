//go:build linux && amd64 && !cgo

package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

type rootJournalEvent struct {
	Version  uint64          `json:"version"`
	Sequence uint64          `json:"sequence"`
	Previous string          `json:"previous"`
	Kind     string          `json:"kind"`
	Fields   json.RawMessage `json:"fields"`
}

// A permit is made just before the supervisor finishes its observation. The
// completed, valid observation must also exist in the live root journal: a
// permit left behind by a failed final observation is not launch authority.
func verifyRootJournal(dir int, intent launchIntent) error {
	genesis, err := readGenesisAt(dir)
	if err != nil {
		return denied
	}
	app, err := readPrivateAt(dir, "realm-app.json", 0, 0)
	if err != nil || realmDigest(genesis, app) != intent.RealmDigest {
		return denied
	}
	all, err := readDirNames(dir)
	if err != nil {
		return denied
	}
	var names []string
	for _, name := range all {
		if strings.HasPrefix(name, "journal-") {
			names = append(names, name)
		}
	}
	if len(names) < 2 || len(names) > 65536 {
		return denied
	}
	previous := strings.Repeat("0", 64)
	stage := ""
	var stageGeneration uint64
	genesisSeen, appSeen, observed := false, false, false
	for i, name := range names {
		sequence := uint64(i + 1)
		if name != fmt.Sprintf("journal-%020d.json", sequence) {
			return denied
		}
		raw, err := readPrivateAt(dir, name, 0, 0)
		if err != nil {
			return denied
		}
		if _, err := exactObject(raw, "version", "sequence", "previous", "kind", "fields"); err != nil {
			return denied
		}
		var event rootJournalEvent
		if json.Unmarshal(raw, &event) != nil || event.Version != 1 || event.Sequence != sequence || event.Previous != previous {
			return denied
		}
		switch event.Kind {
		case "genesis":
			var fields struct {
				Digest string `json:"genesis_digest"`
			}
			if _, err := exactObject(event.Fields, "genesis_digest"); err != nil || json.Unmarshal(event.Fields, &fields) != nil || genesisSeen || sequence != 1 || fields.Digest != sha(genesis) {
				return denied
			}
			genesisSeen = true
		case "app_binding":
			var fields struct {
				Digest string `json:"app_digest"`
			}
			if _, err := exactObject(event.Fields, "app_digest"); err != nil || json.Unmarshal(event.Fields, &fields) != nil || !genesisSeen || appSeen || fields.Digest != sha(app) {
				return denied
			}
			appSeen = true
		case "phase_enter", "phase_exit":
			var fields struct {
				Phase      string `json:"phase"`
				Generation uint64 `json:"phase_generation"`
			}
			if _, err := exactObject(event.Fields, "phase", "phase_generation"); err != nil || json.Unmarshal(event.Fields, &fields) != nil || !shortText(fields.Phase) || fields.Generation == 0 {
				return denied
			}
			if event.Kind == "phase_enter" {
				if stage != "" || fields.Generation != stageGeneration+1 {
					return denied
				}
				stage, stageGeneration, observed = fields.Phase, fields.Generation, false
			} else {
				if stage != fields.Phase || stageGeneration != fields.Generation {
					return denied
				}
				stage, observed = "", false
			}
		case "observation":
			var fields struct {
				PhaseGeneration     uint64 `json:"phase_generation"`
				ObservationSequence uint64 `json:"observation_sequence"`
				RootsDigest         string `json:"roots_digest"`
				Valid               bool   `json:"valid"`
			}
			if _, err := exactObject(event.Fields, "phase_generation", "observation_sequence", "roots_digest", "valid"); err != nil || json.Unmarshal(event.Fields, &fields) != nil || !fields.Valid || fields.ObservationSequence == 0 || !validDigest(fields.RootsDigest) {
				return denied
			}
			if stage == "predecessor" && fields.PhaseGeneration == intent.PhaseGeneration {
				observed = true
			}
		case "admission", "registration", "launch", "exit":
			// These are supervisor-owned entries in the same hash chain.
		case "invalid":
			return denied
		default:
			return denied
		}
		previous = sha(raw)
	}
	if !genesisSeen || !appSeen || stage != "predecessor" || !observed {
		return denied
	}
	// A concurrently changing journal has no unambiguous observation tail.
	after, err := readDirNames(dir)
	if err != nil {
		return denied
	}
	var current []string
	for _, name := range after {
		if strings.HasPrefix(name, "journal-") {
			current = append(current, name)
		}
	}
	if strings.Join(names, "\n") != strings.Join(current, "\n") {
		return denied
	}
	return nil
}
