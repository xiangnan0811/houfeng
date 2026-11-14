//go:build linux && amd64 && !cgo

package main

import (
	"encoding/json"
	"strconv"
)

// validateRuntimeDeviceRules reconstructs the pinned Docker default OCI rules.
// WithDevices appends the original default slice to itself; runc then appends
// its own fixed defaults. Any custom node or rule must fail before BPF load.
func validateRuntimeDeviceRules(linux map[string]any) error {
	if devices, exists := linux["devices"]; exists && !nilOrEmptyArray(devices) {
		return errGuardProtocol
	}
	resources, ok := linux["resources"].(map[string]any)
	if !ok {
		return errGuardProtocol
	}
	rules, ok := resources["devices"].([]any)
	if !ok || len(rules) != 16 {
		return errGuardProtocol
	}
	type rule struct {
		allow        bool
		typ          string
		major, minor int64
	}
	expected := []rule{{false, "", -1, -1}, {true, "c", 1, 5}, {true, "c", 1, 3}, {true, "c", 1, 9}, {true, "c", 1, 8}, {true, "c", 5, 0}, {true, "c", 5, 1}, {false, "c", 10, 229}}
	for index, raw := range rules {
		row, ok := raw.(map[string]any)
		if !ok {
			return errGuardProtocol
		}
		want := expected[index%len(expected)]
		if want.typ == "" {
			if !exactKeys(row, "allow", "access") || row["allow"] != false || row["access"] != "rwm" {
				return errGuardProtocol
			}
			continue
		}
		if !exactKeys(row, "allow", "type", "major", "minor", "access") || row["allow"] != want.allow || row["type"] != want.typ || row["major"] != json.Number(strconv.FormatInt(want.major, 10)) || row["minor"] != json.Number(strconv.FormatInt(want.minor, 10)) || row["access"] != "rwm" {
			return errGuardProtocol
		}
	}
	return nil
}
