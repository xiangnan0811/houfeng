//go:build linux && amd64 && !cgo

package main

import "encoding/json"

type runtimeOCIDiagnosticFrame struct {
	actual, expected                   any
	key                                string
	cursor                             string
	index                              int
	hasCursor, entered, array, missing bool
}

type runtimeOCIPattern struct {
	stage    ownerProbeRuntimeStage
	segments [5]string
	length   int
}

func runtimeOCICost(remaining *int, cost int) bool {
	if cost > *remaining {
		return false
	}
	*remaining -= cost
	return true
}

func runtimeOCIClassifyPath(stack *[66]runtimeOCIDiagnosticFrame, depth int, work, bytes *int) ownerProbeRuntimeStage {
	patterns := [...]runtimeOCIPattern{
		{151, [5]string{"ociVersion"}, 1},
		{152, [5]string{"process"}, 1},
		{153, [5]string{"process", "terminal"}, 2},
		{154, [5]string{"process", "consoleSize"}, 2},
		{155, [5]string{"process", "user"}, 2},
		{156, [5]string{"process", "user", "uid"}, 3},
		{157, [5]string{"process", "user", "gid"}, 3},
		{158, [5]string{"process", "user", "umask"}, 3},
		{159, [5]string{"process", "user", "additionalGids"}, 3},
		{160, [5]string{"process", "user", "username"}, 3},
		{161, [5]string{"process", "args"}, 2},
		{162, [5]string{"process", "commandLine"}, 2},
		{163, [5]string{"process", "env"}, 2},
		{164, [5]string{"process", "cwd"}, 2},
		{165, [5]string{"process", "capabilities"}, 2},
		{166, [5]string{"process", "capabilities", "bounding"}, 3},
		{167, [5]string{"process", "capabilities", "effective"}, 3},
		{168, [5]string{"process", "capabilities", "inheritable"}, 3},
		{169, [5]string{"process", "capabilities", "permitted"}, 3},
		{170, [5]string{"process", "capabilities", "ambient"}, 3},
		{171, [5]string{"process", "rlimits"}, 2},
		{172, [5]string{"process", "noNewPrivileges"}, 2},
		{173, [5]string{"process", "apparmorProfile"}, 2},
		{174, [5]string{"process", "oomScoreAdj"}, 2},
		{175, [5]string{"process", "scheduler"}, 2},
		{176, [5]string{"process", "selinuxLabel"}, 2},
		{177, [5]string{"process", "ioPriority"}, 2},
		{178, [5]string{"process", "execCPUAffinity"}, 2},
		{179, [5]string{"root"}, 1},
		{180, [5]string{"root", "path"}, 2},
		{181, [5]string{"root", "readonly"}, 2},
		{182, [5]string{"hostname"}, 1},
		{183, [5]string{"domainname"}, 1},
		{184, [5]string{"mounts"}, 1},
		{185, [5]string{"mounts", "[]", "destination"}, 3},
		{186, [5]string{"mounts", "[]", "type"}, 3},
		{187, [5]string{"mounts", "[]", "source"}, 3},
		{188, [5]string{"mounts", "[]", "options"}, 3},
		{189, [5]string{"mounts", "[]", "uidMappings"}, 3},
		{190, [5]string{"mounts", "[]", "gidMappings"}, 3},
		{191, [5]string{"hooks"}, 1},
		{192, [5]string{"annotations"}, 1},
		{193, [5]string{"linux"}, 1},
		{194, [5]string{"linux", "uidMappings"}, 2},
		{195, [5]string{"linux", "gidMappings"}, 2},
		{196, [5]string{"linux", "sysctl"}, 2},
		{197, [5]string{"linux", "resources"}, 2},
		{198, [5]string{"linux", "resources", "devices"}, 3},
		{199, [5]string{"linux", "resources", "devices", "[]", "allow"}, 5},
		{200, [5]string{"linux", "resources", "devices", "[]", "type"}, 5},
		{201, [5]string{"linux", "resources", "devices", "[]", "major"}, 5},
		{202, [5]string{"linux", "resources", "devices", "[]", "minor"}, 5},
		{203, [5]string{"linux", "resources", "devices", "[]", "access"}, 5},
		{204, [5]string{"linux", "resources", "memory"}, 3},
		{205, [5]string{"linux", "resources", "memory", "limit"}, 4},
		{206, [5]string{"linux", "resources", "memory", "swap"}, 4},
		{207, [5]string{"linux", "resources", "cpu"}, 3},
		{208, [5]string{"linux", "resources", "cpu", "quota"}, 4},
		{209, [5]string{"linux", "resources", "cpu", "period"}, 4},
		{210, [5]string{"linux", "resources", "pids"}, 3},
		{211, [5]string{"linux", "resources", "pids", "limit"}, 4},
		{212, [5]string{"linux", "resources", "blockIO"}, 3},
		{213, [5]string{"linux", "resources", "hugepageLimits"}, 3},
		{214, [5]string{"linux", "resources", "network"}, 3},
		{215, [5]string{"linux", "resources", "rdma"}, 3},
		{216, [5]string{"linux", "resources", "unified"}, 3},
		{217, [5]string{"linux", "cgroupsPath"}, 2},
		{218, [5]string{"linux", "namespaces"}, 2},
		{219, [5]string{"linux", "namespaces", "[]", "type"}, 4},
		{220, [5]string{"linux", "namespaces", "[]", "path"}, 4},
		{221, [5]string{"linux", "devices"}, 2},
		{222, [5]string{"linux", "netDevices"}, 2},
		{223, [5]string{"linux", "seccomp"}, 2},
		{224, [5]string{"linux", "seccomp", "defaultAction"}, 3},
		{225, [5]string{"linux", "seccomp", "defaultErrnoRet"}, 3},
		{226, [5]string{"linux", "seccomp", "architectures"}, 3},
		{227, [5]string{"linux", "seccomp", "flags"}, 3},
		{228, [5]string{"linux", "seccomp", "listenerPath"}, 3},
		{229, [5]string{"linux", "seccomp", "listenerMetadata"}, 3},
		{230, [5]string{"linux", "seccomp", "syscalls"}, 3},
		{231, [5]string{"linux", "seccomp", "syscalls", "[]", "names"}, 5},
		{232, [5]string{"linux", "seccomp", "syscalls", "[]", "action"}, 5},
		{233, [5]string{"linux", "seccomp", "syscalls", "[]", "errnoRet"}, 5},
		{234, [5]string{"linux", "seccomp", "syscalls", "[]", "args"}, 5},
		{235, [5]string{"linux", "rootfsPropagation"}, 2},
		{236, [5]string{"linux", "maskedPaths"}, 2},
		{237, [5]string{"linux", "readonlyPaths"}, 2},
		{238, [5]string{"linux", "mountLabel"}, 2},
	}
	best, bestDepth := ownerProbeRuntimeStage(239), 0
	for index := range patterns {
		pattern := &patterns[index]
		if pattern.length > depth {
			continue
		}
		matched := true
		for i := range pattern.length {
			frame := &stack[i+1]
			literal := pattern.segments[i]
			if !runtimeOCICost(work, 1) || len(frame.key) > *bytes || !runtimeOCICost(bytes, len(frame.key)) || !runtimeOCICost(bytes, len(literal)) {
				return 240
			}
			if (literal == "[]") != frame.array || (!frame.array && frame.key != literal) {
				matched = false
				break
			}
		}
		if matched && pattern.length > bestDepth {
			best, bestDepth = pattern.stage, pattern.length
		}
	}
	return best
}

// classifyRuntimeSpecMismatch observes the first difference after the policy's
// complete comparison has already rejected the candidate.
func classifyRuntimeSpecMismatch(actual, expected map[string]any) ownerProbeRuntimeDiagnostic {
	var stack [66]runtimeOCIDiagnosticFrame
	stack[0].actual, stack[0].expected = actual, expected
	work, bytes, depth := 32768, 4194304, 0
	for {
		f := &stack[depth]
		if !f.entered {
			if !runtimeOCICost(&work, 1) {
				return ownerProbeRuntimeDiagnostic{stage: 240}
			}
			if m, ok := f.actual.(map[string]any); ok && len(m) > 64 {
				return ownerProbeRuntimeDiagnostic{stage: 240}
			}
			if m, ok := f.expected.(map[string]any); ok && len(m) > 64 {
				return ownerProbeRuntimeDiagnostic{stage: 240}
			}
			f.entered = true
			if f.missing {
				stage := runtimeOCIClassifyPath(&stack, depth, &work, &bytes)
				result := ownerProbeRuntimeDiagnostic{stage: stage}
				if stage == ownerProbeStageBrokerOCIMountOptions {
					result.mountOptions = runtimeMountOptionsDiagnostic(&stack, depth)
				}
				return result
			}
			switch f.expected.(type) {
			case nil, bool, string, json.Number, []any, map[string]any:
			default:
				return ownerProbeRuntimeDiagnostic{stage: 239}
			}
			different := false
			switch a := f.actual.(type) {
			case nil:
				different = f.expected != nil
			case bool:
				b, ok := f.expected.(bool)
				different = !ok || a != b
			case string:
				b, ok := f.expected.(string)
				if !ok {
					different = true
				} else {
					if !runtimeOCICost(&bytes, len(a)) || !runtimeOCICost(&bytes, len(b)) {
						return ownerProbeRuntimeDiagnostic{stage: 240}
					}
					different = a != b
				}
			case json.Number:
				b, ok := f.expected.(json.Number)
				if !ok {
					different = true
				} else {
					if !runtimeOCICost(&bytes, len(a)) || !runtimeOCICost(&bytes, len(b)) {
						return ownerProbeRuntimeDiagnostic{stage: 240}
					}
					different = a != b
				}
			case []any:
				b, ok := f.expected.([]any)
				different = !ok || (a == nil) != (b == nil) || len(a) != len(b)
			case map[string]any:
				b, ok := f.expected.(map[string]any)
				different = !ok || (a == nil) != (b == nil)
			default:
				return ownerProbeRuntimeDiagnostic{stage: 239}
			}
			if different {
				stage := runtimeOCIClassifyPath(&stack, depth, &work, &bytes)
				result := ownerProbeRuntimeDiagnostic{stage: stage}
				if stage == ownerProbeStageBrokerOCIMountOptions {
					result.mountOptions = runtimeMountOptionsDiagnostic(&stack, depth)
				}
				return result
			}
		}
		var a, b any
		var key string
		var array, missing, found bool
		switch value := f.actual.(type) {
		case []any:
			other := f.expected.([]any)
			if f.index < len(value) {
				a, b = value[f.index], other[f.index]
				f.index++
				array, found = true, true
			}
		case map[string]any:
			other := f.expected.(map[string]any)
			for k := range value {
				if !runtimeOCICost(&work, 1) || len(k) > bytes/2 || !runtimeOCICost(&bytes, 2*len(k)) {
					return ownerProbeRuntimeDiagnostic{stage: 240}
				}
				if (!f.hasCursor || k > f.cursor) && (!found || k < key) {
					key, found = k, true
				}
			}
			for k := range other {
				if !runtimeOCICost(&work, 1) || len(k) > bytes/2 || !runtimeOCICost(&bytes, 2*len(k)) {
					return ownerProbeRuntimeDiagnostic{stage: 240}
				}
				if (!f.hasCursor || k > f.cursor) && (!found || k < key) {
					key, found = k, true
				}
			}
			if found {
				if len(key) > bytes/2 || !runtimeOCICost(&bytes, 2*len(key)) {
					return ownerProbeRuntimeDiagnostic{stage: 240}
				}
				var aok, bok bool
				a, aok = value[key]
				b, bok = other[key]
				missing = aok != bok
				f.cursor, f.hasCursor = key, true
			}
		}
		if found {
			if depth == 65 {
				return ownerProbeRuntimeDiagnostic{stage: 241}
			}
			depth++
			stack[depth] = runtimeOCIDiagnosticFrame{actual: a, expected: b, key: key, array: array, missing: missing}
			continue
		}
		if depth == 0 {
			return ownerProbeRuntimeDiagnostic{stage: ownerProbeStageBrokerBundleCompare}
		}
		depth--
	}
}

func runtimeMountOptionsDiagnostic(stack *[66]runtimeOCIDiagnosticFrame, depth int) ownerProbeMountOptionsDiagnostic {
	var result ownerProbeMountOptionsDiagnostic
	if depth < 3 || stack[1].array || stack[1].key != "mounts" || !stack[2].array || stack[2].missing || stack[3].array || stack[3].key != "options" {
		return result
	}
	if _, ok := stack[1].actual.([]any); !ok {
		return result
	}
	if _, ok := stack[1].expected.([]any); !ok {
		return result
	}
	a, aok := stack[2].actual.(map[string]any)
	b, bok := stack[2].expected.(map[string]any)
	if !aok || !bok || a == nil || b == nil {
		return result
	}
	ad, aok := a["destination"]
	bd, bok := b["destination"]
	as, asok := ad.(string)
	bs, bsok := bd.(string)
	if !aok || !bok || !asok || !bsok || len(as) > 64 || len(bs) > 64 || as != bs {
		return result
	}
	switch as {
	case "/proc":
		result.mount = ownerProbeMountProc
	case "/dev":
		result.mount = ownerProbeMountDev
	case "/dev/pts":
		result.mount = ownerProbeMountDevPts
	case "/sys":
		result.mount = ownerProbeMountSys
	case "/sys/fs/cgroup":
		result.mount = ownerProbeMountSysCgroup
	case "/dev/mqueue":
		result.mount = ownerProbeMountDevMqueue
	case "/dev/shm":
		result.mount = ownerProbeMountDevShm
	case "/etc/hostname":
		result.mount = ownerProbeMountHostname
	case "/etc/hosts":
		result.mount = ownerProbeMountHosts
	case "/etc/resolv.conf":
		result.mount = ownerProbeMountResolv
	case "/var/lib/postgresql/data":
		result.mount = ownerProbeMountPGDATA
	default:
		result.mount = ownerProbeMountOther
	}
	av, ap := a["options"]
	bv, bp := b["options"]
	result.actual = runtimeMountOptionsSignature(av, ap)
	result.expected = runtimeMountOptionsSignature(bv, bp)
	result.difference = runtimeMountOptionsDifference(av, bv, ap, bp, result.actual, result.expected)
	return result
}

func runtimeMountOptionsSignature(value any, present bool) uint8 {
	if !present {
		return ownerProbeOptionMissing
	}
	if value == nil {
		return ownerProbeOptionNull
	}
	array, ok := value.([]any)
	if !ok {
		return ownerProbeOptionOtherType
	}
	if array == nil {
		return ownerProbeOptionNilArray
	}
	if len(array) > 16 {
		return ownerProbeOptionLimit
	}
	if len(array) == 0 {
		return ownerProbeOptionEmpty
	}
	nonString, oversized := false, false
	for _, value := range array {
		if s, ok := value.(string); !ok {
			nonString = true
		} else if len(s) > 64 {
			oversized = true
		}
	}
	if oversized {
		return ownerProbeOptionLimit
	}
	if nonString {
		return ownerProbeOptionNonStringArray
	}
	patterns := [...][6]string{
		{"nosuid", "noexec", "nodev"},
		{"nosuid", "strictatime", "mode=755", "size=65536k"},
		{"nosuid", "noexec", "newinstance", "ptmxmode=0666", "mode=0620", "gid=5"},
		{"nosuid", "noexec", "nodev", "ro"},
		{"ro", "nosuid", "noexec", "nodev"},
		{"nosuid", "noexec", "nodev", "mode=1777", "size=67108864"},
		{"rbind", "ro", "rprivate"},
		{"rbind", "rro", "rprivate"},
		{"rbind", "rprivate"},
	}
	lengths := [...]int{3, 4, 6, 4, 4, 5, 3, 3, 2}
	for i := range patterns {
		if len(array) != lengths[i] {
			continue
		}
		match := true
		for j, v := range array {
			if v.(string) != patterns[i][j] {
				match = false
				break
			}
		}
		if match {
			return uint8(i) + ownerProbeOptionNosuidNoexecNodev
		}
	}
	return ownerProbeOptionOtherStringArray
}

func runtimeMountOptionsType(value any) uint8 {
	switch value.(type) {
	case nil:
		return 0
	case bool:
		return 1
	case string:
		return 2
	case json.Number:
		return 3
	case []any:
		return 4
	case map[string]any:
		return 5
	default:
		return 6
	}
}

func runtimeMountOptionsDifference(actual, expected any, actualPresent, expectedPresent bool, actualSignature, expectedSignature uint8) uint8 {
	if actualPresent != expectedPresent {
		return ownerProbeDifferenceMissing
	}
	if !actualPresent {
		return ownerProbeDifferenceOther
	}
	if runtimeMountOptionsType(actual) != runtimeMountOptionsType(expected) {
		return ownerProbeDifferenceType
	}
	a, ok := actual.([]any)
	if !ok {
		return ownerProbeDifferenceOther
	}
	b := expected.([]any)
	if (a == nil) != (b == nil) {
		return ownerProbeDifferenceNilArray
	}
	if a == nil {
		return ownerProbeDifferenceOther
	}
	if actualSignature == ownerProbeOptionLimit || expectedSignature == ownerProbeOptionLimit {
		return ownerProbeDifferenceLimit
	}
	if actualSignature == ownerProbeOptionNonStringArray || expectedSignature == ownerProbeOptionNonStringArray {
		return ownerProbeDifferenceNonString
	}
	if len(a) != len(b) {
		return ownerProbeDifferenceLength
	}
	same := true
	for i := range a {
		if a[i].(string) != b[i].(string) {
			same = false
			break
		}
	}
	if same {
		return ownerProbeDifferenceOther
	}
	var matched [16]bool
	for _, item := range a {
		found := false
		for i, candidate := range b {
			if !matched[i] && item.(string) == candidate.(string) {
				matched[i] = true
				found = true
				break
			}
		}
		if !found {
			return ownerProbeDifferenceValue
		}
	}
	return ownerProbeDifferenceOrder
}
