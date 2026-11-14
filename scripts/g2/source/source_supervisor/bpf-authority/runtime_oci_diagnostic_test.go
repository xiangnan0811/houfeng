//go:build linux && amd64 && !cgo

package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestRuntimeOCIDifferenceDiagnostic(t *testing.T) {
	cases := []struct {
		name             string
		actual, expected map[string]any
		want             ownerProbeRuntimeStage
	}{
		{"equal", map[string]any{"root": map[string]any{"path": "x"}}, map[string]any{"root": map[string]any{"path": "x"}}, 39},
		{"root-path", map[string]any{"root": map[string]any{"path": "secret-a"}}, map[string]any{"root": map[string]any{"path": "secret-b"}}, 180},
		{"uid", map[string]any{"process": map[string]any{"user": map[string]any{"uid": json.Number("1")}}}, map[string]any{"process": map[string]any{"user": map[string]any{"uid": json.Number("2")}}}, 156},
		{"env", map[string]any{"process": map[string]any{"env": []any{"a"}}}, map[string]any{"process": map[string]any{"env": []any{"b"}}}, 163},
		{"capability", map[string]any{"process": map[string]any{"capabilities": map[string]any{"bounding": []any{"a"}}}}, map[string]any{"process": map[string]any{"capabilities": map[string]any{"bounding": []any{"b"}}}}, 166},
		{"mount-options", map[string]any{"mounts": []any{map[string]any{"options": []any{"ro"}}}}, map[string]any{"mounts": []any{map[string]any{"options": []any{"rw"}}}}, 188},
		{"namespace", map[string]any{"linux": map[string]any{"namespaces": []any{map[string]any{"path": "a"}}}}, map[string]any{"linux": map[string]any{"namespaces": []any{map[string]any{"path": "b"}}}}, 220},
		{"device", map[string]any{"linux": map[string]any{"resources": map[string]any{"devices": []any{map[string]any{"allow": true}}}}}, map[string]any{"linux": map[string]any{"resources": map[string]any{"devices": []any{map[string]any{"allow": false}}}}}, 199},
		{"memory", map[string]any{"linux": map[string]any{"resources": map[string]any{"memory": map[string]any{"limit": json.Number("1")}}}}, map[string]any{"linux": map[string]any{"resources": map[string]any{"memory": map[string]any{"limit": json.Number("2")}}}}, 205},
		{"pids", map[string]any{"linux": map[string]any{"resources": map[string]any{"pids": map[string]any{"limit": json.Number("1")}}}}, map[string]any{"linux": map[string]any{"resources": map[string]any{"pids": map[string]any{"limit": json.Number("2")}}}}, 211},
		{"seccomp-names", map[string]any{"linux": map[string]any{"seccomp": map[string]any{"syscalls": []any{map[string]any{"names": []any{"x"}}}}}}, map[string]any{"linux": map[string]any{"seccomp": map[string]any{"syscalls": []any{map[string]any{"names": []any{"y"}}}}}}, 231},
		{"seccomp-args", map[string]any{"linux": map[string]any{"seccomp": map[string]any{"syscalls": []any{map[string]any{"args": []any{"x"}}}}}}, map[string]any{"linux": map[string]any{"seccomp": map[string]any{"syscalls": []any{map[string]any{"args": []any{"y"}}}}}}, 234},
		{"annotations", map[string]any{"annotations": map[string]any{"private": "a"}}, map[string]any{"annotations": map[string]any{"private": "b"}}, 192},
		{"root-order", map[string]any{"root": true, "hostname": true}, map[string]any{"root": false, "hostname": false}, 182},
		{"child-order", map[string]any{"process": map[string]any{"user": false, "env": false}}, map[string]any{"process": map[string]any{"user": true, "env": true}}, 163},
		{"array-order", map[string]any{"mounts": []any{map[string]any{"source": "a"}, map[string]any{"options": "a"}}}, map[string]any{"mounts": []any{map[string]any{"source": "b"}, map[string]any{"options": "b"}}}, 187},
		{"missing-null", map[string]any{"process": map[string]any{"user": nil}}, map[string]any{"process": map[string]any{}}, 155},
		{"nil-containers-equal", map[string]any{"root": map[string]any(nil), "mounts": []any(nil)}, map[string]any{"root": map[string]any(nil), "mounts": []any(nil)}, 39},
		{"array-length", map[string]any{"process": map[string]any{"args": []any{"x"}}}, map[string]any{"process": map[string]any{"args": []any{}}}, 161},
		{"unsupported-expected", map[string]any{"root": nil}, map[string]any{"root": 1}, 239},
		{"nil-map", map[string]any{"root": map[string]any(nil)}, map[string]any{"root": map[string]any{}}, 179},
		{"nil-slice", map[string]any{"mounts": []any(nil)}, map[string]any{"mounts": []any{}}, 184},
		{"typed-nil", map[string]any{"root": map[string]any(nil)}, map[string]any{"root": nil}, 179},
		{"number-text", map[string]any{"process": map[string]any{"user": map[string]any{"uid": json.Number("1")}}}, map[string]any{"process": map[string]any{"user": map[string]any{"uid": json.Number("1.0")}}}, 156},
		{"number-type", map[string]any{"process": map[string]any{"user": map[string]any{"uid": json.Number("1")}}}, map[string]any{"process": map[string]any{"user": map[string]any{"uid": "1"}}}, 156},
		{"unknown-root", map[string]any{"unknown": "secret"}, map[string]any{"unknown": "other"}, 239},
		{"unknown-child", map[string]any{"process": map[string]any{"unknown": true}}, map[string]any{"process": map[string]any{"unknown": false}}, 152},
		{"literal-brackets", map[string]any{"mounts": map[string]any{"[]": map[string]any{"options": true}}}, map[string]any{"mounts": map[string]any{"[]": map[string]any{"options": false}}}, 184},
		{"unsupported", map[string]any{"root": 1}, map[string]any{"root": 1}, 239},
		{"empty-key", map[string]any{"": true, "root": true}, map[string]any{"": false, "root": false}, 239},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for range 8 {
				got := classifyRuntimeSpecMismatch(tc.actual, tc.expected)
				if got.stage != tc.want || got.mountOptions != (ownerProbeMountOptionsDiagnostic{}) {
					t.Fatalf("got diagnostic %+v, want stage %d and no witness", got, tc.want)
				}
			}
		})
	}
	original := map[string]any{"process": map[string]any{"env": []any{"private", "x"}}}
	copyBefore := map[string]any{"process": map[string]any{"env": []any{"private", "x"}}}
	other := map[string]any{"process": map[string]any{"env": []any{"private", "y"}}}
	first := map[string]any{}
	first["root"] = false
	first["hostname"] = false
	second := map[string]any{}
	second["hostname"] = true
	second["root"] = true
	for range 16 {
		if got := classifyRuntimeSpecMismatch(first, second); got != (ownerProbeRuntimeDiagnostic{stage: 182}) {
			t.Fatalf("map order: %+v", got)
		}
	}
	prefix := strings.Repeat("k", 149795)
	shortKeys := map[string]any{prefix + "a": nil, prefix + "b": nil}
	if got := classifyRuntimeSpecMismatch(shortKeys, shortKeys); got != (ownerProbeRuntimeDiagnostic{stage: 39}) {
		t.Fatalf("long key within budget: %+v", got)
	}
	prefix = strings.Repeat("k", 149796)
	longKeys := map[string]any{prefix + "a": nil, prefix + "b": nil}
	if got := classifyRuntimeSpecMismatch(longKeys, longKeys); got != (ownerProbeRuntimeDiagnostic{stage: 240}) {
		t.Fatalf("long key byte budget: %+v", got)
	}
	otherBefore := map[string]any{"process": map[string]any{"env": []any{"private", "y"}}}
	if got := classifyRuntimeSpecMismatch(original, other); got != (ownerProbeRuntimeDiagnostic{stage: 163}) || !reflect.DeepEqual(original, copyBefore) || !reflect.DeepEqual(other, otherBefore) {
		t.Fatal("classification changed inputs or returned wrong category")
	}
	large := make(map[string]any)
	for i := range 65 {
		large[fmt.Sprintf("k%02d", i)] = nil
	}
	if got := classifyRuntimeSpecMismatch(large, large); got != (ownerProbeRuntimeDiagnostic{stage: 240}) {
		t.Fatalf("map limit: %+v", got)
	}
	for _, tc := range []struct{ a, b map[string]any }{
		{map[string]any{"root": large}, map[string]any{"root": nil}},
		{map[string]any{"root": true}, map[string]any{"root": large}},
		{map[string]any{"root": large}, map[string]any{}},
	} {
		if got := classifyRuntimeSpecMismatch(tc.a, tc.b); got != (ownerProbeRuntimeDiagnostic{stage: 240}) {
			t.Fatalf("mixed map limit: %+v", got)
		}
	}
	long := strings.Repeat("x", 2097153)
	if got := classifyRuntimeSpecMismatch(map[string]any{"root": long}, map[string]any{"root": long}); got != (ownerProbeRuntimeDiagnostic{stage: 240}) {
		t.Fatalf("byte limit: %+v", got)
	}
	wide := make(map[string]any)
	for i := range 64 {
		wide[fmt.Sprintf("prefix-%02d", i)] = nil
	}
	many := []any{wide, wide, wide, wide}
	if got := classifyRuntimeSpecMismatch(map[string]any{"root": many}, map[string]any{"root": many}); got != (ownerProbeRuntimeDiagnostic{stage: 240}) {
		t.Fatalf("work limit: %+v", got)
	}
	var a, b any = "a", "b"
	for range 64 {
		a = []any{a}
		b = []any{b}
	}
	if got := classifyRuntimeSpecMismatch(map[string]any{"root": a}, map[string]any{"root": b}); got != (ownerProbeRuntimeDiagnostic{stage: 179}) {
		t.Fatalf("depth 65: %+v", got)
	}
	a, b = []any{a}, []any{b}
	if got := classifyRuntimeSpecMismatch(map[string]any{"root": a}, map[string]any{"root": b}); got != (ownerProbeRuntimeDiagnostic{stage: 241}) {
		t.Fatalf("depth 66: %+v", got)
	}
}

func TestRuntimeOCIMountOptionsDiagnostic(t *testing.T) {
	makeSpec := func(destination any, options any, present bool) map[string]any {
		m := map[string]any{"destination": destination}
		if present {
			m["options"] = options
		}
		return map[string]any{"mounts": []any{m}}
	}
	ro := []any{"rbind", "ro", "rprivate"}
	rro := []any{"rbind", "rro", "rprivate"}
	proc := []any{"nosuid", "noexec", "nodev"}
	ordered := []any{"nodev", "noexec", "nosuid"}
	for _, tc := range []struct {
		name        string
		destination any
		a, b        any
		ap, bp      bool
		want        ownerProbeMountOptionsDiagnostic
	}{
		{"pgdata-ro", "/var/lib/postgresql/data", ro, rro, true, true, ownerProbeMountOptionsDiagnostic{11, 12, 13, 6}},
		{"pgdata-rro", "/var/lib/postgresql/data", rro, ro, true, true, ownerProbeMountOptionsDiagnostic{11, 13, 12, 6}},
		{"proc-order", "/proc", ordered, proc, true, true, ownerProbeMountOptionsDiagnostic{1, 15, 6, 5}},
		{"missing", "/proc", nil, proc, false, true, ownerProbeMountOptionsDiagnostic{1, 1, 6, 1}},
		{"null", "/proc", nil, proc, true, true, ownerProbeMountOptionsDiagnostic{1, 2, 6, 2}},
		{"nil-array", "/proc", []any(nil), []any{}, true, true, ownerProbeMountOptionsDiagnostic{1, 3, 5, 3}},
		{"type", "/proc", "private", proc, true, true, ownerProbeMountOptionsDiagnostic{1, 4, 6, 2}},
		{"length", "/proc", []any{"one"}, proc, true, true, ownerProbeMountOptionsDiagnostic{1, 15, 6, 4}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := makeSpec(tc.destination, tc.a, tc.ap), makeSpec(tc.destination, tc.b, tc.bp)
			beforeA, _ := json.Marshal(a)
			beforeB, _ := json.Marshal(b)
			got := classifyRuntimeSpecMismatch(a, b)
			if got.stage != 188 || got.mountOptions != tc.want {
				t.Fatalf("got %+v want %+v", got, tc.want)
			}
			afterA, _ := json.Marshal(a)
			afterB, _ := json.Marshal(b)
			if string(beforeA) != string(afterA) || string(beforeB) != string(afterB) {
				t.Fatal("modified input")
			}
		})
	}
	for i, destination := range []string{"/proc", "/dev", "/dev/pts", "/sys", "/sys/fs/cgroup", "/dev/mqueue", "/dev/shm", "/etc/hostname", "/etc/hosts", "/etc/resolv.conf", "/var/lib/postgresql/data", "private"} {
		got := classifyRuntimeSpecMismatch(makeSpec(destination, ro, true), makeSpec(destination, rro, true))
		if got.stage != 188 || got.mountOptions.mount != uint8(i+1) {
			t.Fatalf("destination %d: %+v", i, got)
		}
	}
	for _, pair := range [][2]any{{"unknown-a", "unknown-b"}, {nil, nil}, {strings.Repeat("x", 65), strings.Repeat("x", 65)}} {
		got := classifyRuntimeSpecMismatch(makeSpec(pair[0], ro, true), makeSpec(pair[1], rro, true))
		if got.mountOptions != (ownerProbeMountOptionsDiagnostic{}) {
			t.Fatalf("untrusted destination: %+v", got)
		}
	}
	got := classifyRuntimeSpecMismatch(makeSpec(strings.Repeat("x", 64), ro, true), makeSpec(strings.Repeat("x", 64), rro, true))
	if got.mountOptions.mount != 12 {
		t.Fatalf("64-byte destination: %+v", got)
	}
	missingDestination := map[string]any{"mounts": []any{map[string]any{"options": ro}}}
	if got := classifyRuntimeSpecMismatch(missingDestination, map[string]any{"mounts": []any{map[string]any{"options": rro}}}); got.stage != 188 || got.mountOptions != (ownerProbeMountOptionsDiagnostic{}) {
		t.Fatalf("missing destination: %+v", got)
	}
	sixteen := make([]any, 16)
	for i := range sixteen {
		sixteen[i] = strings.Repeat("x", 64)
	}
	if got := runtimeMountOptionsSignature(sixteen, true); got != 15 {
		t.Fatalf("16-by-64 boundary: %d", got)
	}
	for _, tc := range []struct {
		value   any
		present bool
		want    uint8
	}{
		{nil, false, 1}, {nil, true, 2}, {[]any(nil), true, 3}, {false, true, 4}, {[]any{}, true, 5},
		{[]any{"nosuid", "noexec", "nodev"}, true, 6}, {[]any{"nosuid", "strictatime", "mode=755", "size=65536k"}, true, 7},
		{[]any{"nosuid", "noexec", "newinstance", "ptmxmode=0666", "mode=0620", "gid=5"}, true, 8},
		{[]any{"nosuid", "noexec", "nodev", "ro"}, true, 9}, {[]any{"ro", "nosuid", "noexec", "nodev"}, true, 10},
		{[]any{"nosuid", "noexec", "nodev", "mode=1777", "size=67108864"}, true, 11}, {ro, true, 12}, {rro, true, 13},
		{[]any{"rbind", "rprivate"}, true, 14}, {[]any{strings.Repeat("a", 64)}, true, 15},
		{[]any{false}, true, 16}, {[]any{false, strings.Repeat("a", 65)}, true, 17}, {make([]any, 17), true, 17},
	} {
		if got := runtimeMountOptionsSignature(tc.value, tc.present); got != tc.want {
			t.Fatalf("signature %d want %d", got, tc.want)
		}
	}
	for _, tc := range []struct {
		a, b   any
		ap, bp bool
		want   uint8
	}{
		{nil, nil, false, false, 9}, {nil, nil, false, true, 1}, {[]any(nil), []any{}, true, true, 3},
		{[]any{"a", "a", "b"}, []any{"a", "b", "a"}, true, true, 5},
		{[]any{"a", "a", "b"}, []any{"a", "b", "b"}, true, true, 6},
		{[]any{false}, []any{"a"}, true, true, 7}, {[]any{false, strings.Repeat("x", 65)}, []any{"a"}, true, true, 8},
		{"a", "b", true, true, 9},
	} {
		a, b := runtimeMountOptionsSignature(tc.a, tc.ap), runtimeMountOptionsSignature(tc.b, tc.bp)
		if got := runtimeMountOptionsDifference(tc.a, tc.b, tc.ap, tc.bp, a, b); got != tc.want {
			t.Fatalf("difference %d want %d", got, tc.want)
		}
	}
	var stack [66]runtimeOCIDiagnosticFrame
	if got := runtimeMountOptionsDiagnostic(&stack, 3); got != (ownerProbeMountOptionsDiagnostic{}) {
		t.Fatalf("bad stack: %+v", got)
	}
	stack[1] = runtimeOCIDiagnosticFrame{key: "mounts", actual: []any{1}, expected: []any{1}}
	stack[2] = runtimeOCIDiagnosticFrame{array: true, actual: map[string]any{"destination": "/proc"}, expected: map[string]any{"destination": "/proc"}}
	stack[3] = runtimeOCIDiagnosticFrame{key: "options"}
	if got := runtimeMountOptionsDiagnostic(&stack, 3); got != (ownerProbeMountOptionsDiagnostic{1, 1, 1, 9}) {
		t.Fatalf("both missing: %+v", got)
	}
	stack[2].missing = true
	if got := runtimeMountOptionsDiagnostic(&stack, 3); got != (ownerProbeMountOptionsDiagnostic{}) {
		t.Fatalf("missing mount: %+v", got)
	}
	stack[2].missing = false
	stack[2].actual = map[string]any(nil)
	if got := runtimeMountOptionsDiagnostic(&stack, 3); got != (ownerProbeMountOptionsDiagnostic{}) {
		t.Fatalf("nil mount: %+v", got)
	}
	first := map[string]any{"mounts": []any{map[string]any{"destination": "/proc", "options": ordered}, map[string]any{"destination": "/dev", "options": ro}}}
	second := map[string]any{"mounts": []any{map[string]any{"options": proc, "destination": "/proc"}, map[string]any{"options": rro, "destination": "/dev"}}}
	for range 8 {
		if got := classifyRuntimeSpecMismatch(first, second); got.mountOptions != (ownerProbeMountOptionsDiagnostic{1, 15, 6, 5}) {
			t.Fatalf("first mount: %+v", got)
		}
	}
}
