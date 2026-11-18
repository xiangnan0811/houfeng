//go:build linux && amd64 && !cgo

package main

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

type runtimeMountFact struct {
	typ, source, destination, propagation string
	readonly, nonRecursive                bool
	readOnlyNonRecursive                  bool
}
type runtimeContainerFacts struct {
	rootfs, hostname, domainname, path, cwd, user             string
	args                                                      []any
	env                                                       map[string]string
	configEnv, imageEnv, overrideEnv                          []string
	mounts                                                    []runtimeMountFact
	ownerProbeSource                                          ownerProbeSourceIdentity
	hasOwnerProbeSource                                       bool
	hostnamePath, hostsPath, resolvPath, shmPath, sandboxPath string
	networkMode, ipcMode, cgroupnsMode                        string
	appArmorProfile, processLabel, mountLabel                 string
	memory, memorySwap, nanoCPUs, pidsLimit, shmSize          int64
	hasPidsLimit, readonlyRootfs, privileged, noNewPrivileges bool
	oomScoreAdj                                               int
	passwdRaw, groupRaw                                       []byte
	hasPasswd, hasGroup, sysInfoValid                         bool
	runtimeReady                                              bool
	sysInfo                                                   runtimeSysInfo
}

func runtimeSystemPaths(containerID string) (hostnamePath, hostsPath, resolvPath string) {
	root := filepath.Join(runtimeDockerDataRoot, "containers", containerID)
	return filepath.Join(root, "hostname"), filepath.Join(root, "hosts"), filepath.Join(root, "resolv.conf")
}

func inspectRuntimeSystemPaths(containerID string, item map[string]any, facts *runtimeContainerFacts, diagnostic *admitDiagnostic) error {
	if !runtimeContainerID(containerID) || facts == nil {
		return errGuardProtocol
	}
	hostname, hosts, resolv := runtimeSystemPaths(containerID)
	for _, pair := range []struct {
		key, expected, stage string
		target               *string
	}{
		{"HostnamePath", hostname, "ADMIT_HOSTNAME_PATH", &facts.hostnamePath},
		{"HostsPath", hosts, "ADMIT_HOSTS_PATH", &facts.hostsPath},
		{"ResolvConfPath", resolv, "ADMIT_RESOLV_PATH", &facts.resolvPath},
	} {
		value, ok := item[pair.key].(string)
		if !ok || value != "" && (!runtimePath(value) || value != pair.expected) {
			diagnostic.reject(pair.stage, errGuardProtocol)
			return errGuardProtocol
		}
		if value != "" {
			if err := runtimeMountIdentity(value, diagnostic); err != nil {
				diagnostic.reject("ADMIT_SYSTEM_PATH_IDENTITY", err)
				return err
			}
		}
		*pair.target = pair.expected
	}
	return nil
}

// inspectRuntimeContainer obtains effective Docker facts independently over the
// authenticated daemon connection and compares them with the fixed admission.
// It is called only by the bounded I/O worker; no broker state is mutated.
func inspectRuntimeContainer(daemon *daemonRegistration, cid string, config map[string]any, deadline time.Time, diagnostic *admitDiagnostic) (*runtimeContainerFacts, error) {
	if !runtimeContainerID(cid) || config == nil || daemon == nil {
		diagnostic.reject("ADMIT_INSPECT_INPUT", errGuardProtocol)
		return nil, errGuardProtocol
	}
	sysInfo, err := probeRuntimeSysInfo(daemon.pid, diagnostic)
	if err != nil {
		diagnostic.reject("ADMIT_INSPECT_SYSINFO", err)
		return nil, err
	}
	imageID, ok := config["Image"].(string)
	if !ok || len(imageID) != 71 || !strings.HasPrefix(imageID, "sha256:") {
		diagnostic.reject("ADMIT_IMAGE_ID", errGuardProtocol)
		return nil, errGuardProtocol
	}
	if _, ok = digestString(imageID[7:]); !ok {
		diagnostic.reject("ADMIT_IMAGE_DIGEST", errGuardProtocol)
		return nil, errGuardProtocol
	}
	image, err := recoveryDockerGET(daemon, "/v1.41/images/"+imageID+"/json", deadline)
	if err != nil || image["Id"] != imageID {
		if err != nil {
			diagnostic.reject("ADMIT_IMAGE_GET", err)
		} else {
			diagnostic.reject("ADMIT_IMAGE_RESPONSE_ID", errGuardProtocol)
		}
		return nil, errGuardProtocol
	}
	imageConfig, ok := image["Config"].(map[string]any)
	if !ok {
		diagnostic.reject("ADMIT_IMAGE_CONFIG", errGuardProtocol)
		return nil, errGuardProtocol
	}
	imageEnvSlice, err := runtimeStrings(imageConfig["Env"])
	if err != nil {
		diagnostic.reject("ADMIT_IMAGE_ENV_STRINGS", err)
		return nil, err
	}
	env, err := recoveryEnvironment(imageConfig["Env"])
	if err != nil {
		diagnostic.reject("ADMIT_IMAGE_ENV", err)
		return nil, err
	}
	var overrideEnvSlice []string
	if overrides, exists := config["Env"]; exists {
		overrideEnvSlice, err = runtimeStrings(overrides)
		if err != nil {
			diagnostic.reject("ADMIT_OVERRIDE_ENV_STRINGS", err)
			return nil, err
		}
		values, e := recoveryEnvironment(overrides)
		if e != nil {
			diagnostic.reject("ADMIT_OVERRIDE_ENV", e)
			return nil, e
		}
		for k, v := range values {
			env[k] = v
		}
	}
	expectedMergedEnv := replaceOrAppendEnvValues(append([]string(nil), imageEnvSlice...), overrideEnvSlice)
	item, err := recoveryDockerGET(daemon, "/v1.41/containers/"+cid+"/json", deadline)
	if err != nil || item["Id"] != cid || item["Image"] != imageID {
		if err != nil {
			diagnostic.reject("ADMIT_CONTAINER_GET", err)
		} else {
			diagnostic.reject("ADMIT_CONTAINER_RESPONSE_ID", errGuardProtocol)
		}
		return nil, errGuardProtocol
	}
	state, ok := item["State"].(map[string]any)
	if !ok || state["Running"] != false || state["Pid"] != json.Number("0") {
		diagnostic.reject("ADMIT_CONTAINER_STATE", errGuardProtocol)
		return nil, errGuardProtocol
	}
	actual, ok := item["Config"].(map[string]any)
	if !ok || actual["Tty"] != false {
		diagnostic.reject("ADMIT_CONTAINER_CONFIG", errGuardProtocol)
		return nil, errGuardProtocol
	}
	for _, key := range []string{"User", "Entrypoint", "Cmd", "WorkingDir", "Labels"} {
		want, present := config[key]
		if !present {
			want = imageConfig[key]
		}
		if want != nil && !sameJSON(actual[key], want) {
			switch key {
			case "User":
				diagnostic.reject("ADMIT_CONFIG_USER", errGuardProtocol)
			case "Entrypoint":
				diagnostic.reject("ADMIT_CONFIG_ENTRYPOINT", errGuardProtocol)
			case "Cmd":
				diagnostic.reject("ADMIT_CONFIG_CMD", errGuardProtocol)
			case "WorkingDir":
				diagnostic.reject("ADMIT_CONFIG_WORKDIR", errGuardProtocol)
			case "Labels":
				diagnostic.reject("ADMIT_CONFIG_LABELS", errGuardProtocol)
			}
			return nil, errGuardProtocol
		}
	}
	actualEnvSlice, err := runtimeStrings(actual["Env"])
	if err != nil || !reflect.DeepEqual(actualEnvSlice, expectedMergedEnv) {
		if err != nil {
			diagnostic.reject("ADMIT_ACTUAL_ENV_STRINGS", err)
		} else {
			diagnostic.reject("ADMIT_ACTUAL_ENV_SEQUENCE", errGuardProtocol)
		}
		return nil, errGuardProtocol
	}
	actualEnv, err := recoveryEnvironment(actual["Env"])
	if err != nil || !reflect.DeepEqual(actualEnv, env) {
		if err != nil {
			diagnostic.reject("ADMIT_ACTUAL_ENV_PARSE", err)
		} else {
			diagnostic.reject("ADMIT_ACTUAL_ENV_VALUES", errGuardProtocol)
		}
		return nil, errGuardProtocol
	}
	if actual["Domainname"] != nil && actual["Domainname"] != "" || actual["OpenStdin"] != nil && actual["OpenStdin"] != false {
		diagnostic.reject("ADMIT_CONFIG_DOMAIN_STDIN", errGuardProtocol)
		return nil, errGuardProtocol
	}
	host, ok := item["HostConfig"].(map[string]any)
	if !ok {
		diagnostic.reject("ADMIT_HOST_CONFIG", errGuardProtocol)
		return nil, errGuardProtocol
	}
	wantHost, ok := config["HostConfig"].(map[string]any)
	if !ok {
		diagnostic.reject("ADMIT_WANT_HOST_CONFIG", errGuardProtocol)
		return nil, errGuardProtocol
	}
	for _, key := range []string{"NetworkMode", "ReadonlyRootfs", "Privileged", "PidsLimit", "Dns", "Memory", "NanoCpus", "Tmpfs"} {
		if want, exists := wantHost[key]; exists && !sameJSON(host[key], want) {
			switch key {
			case "NetworkMode":
				diagnostic.reject("ADMIT_HOST_NETWORK_MODE", errGuardProtocol)
			case "ReadonlyRootfs":
				diagnostic.reject("ADMIT_HOST_READONLY_ROOTFS", errGuardProtocol)
			case "Privileged":
				diagnostic.reject("ADMIT_HOST_PRIVILEGED", errGuardProtocol)
			case "PidsLimit":
				diagnostic.reject("ADMIT_HOST_PIDS_LIMIT", errGuardProtocol)
			case "Dns":
				diagnostic.reject("ADMIT_HOST_DNS", errGuardProtocol)
			case "Memory":
				diagnostic.reject("ADMIT_HOST_MEMORY", errGuardProtocol)
			case "NanoCpus":
				diagnostic.reject("ADMIT_HOST_NANO_CPUS", errGuardProtocol)
			case "Tmpfs":
				diagnostic.reject("ADMIT_HOST_TMPFS", errGuardProtocol)
			}
			return nil, errGuardProtocol
		}
	}
	securityOpt := any([]any{"no-new-privileges"})
	if host["ReadonlyRootfs"] != true || host["Privileged"] != false || host["CgroupParent"] != "" || !sameJSON(host["CapDrop"], []any{"ALL"}) || !sameJSON(host["SecurityOpt"], securityOpt) || !nilOrEmptyArray(host["CapAdd"]) || !nilOrEmptyArray(host["Devices"]) || !nilOrEmptyArray(host["DeviceRequests"]) || !nilOrEmptyArray(host["DeviceCgroupRules"]) || !nilOrEmptyArray(host["GroupAdd"]) || !nilOrEmptyArray(host["Ulimits"]) || !nilOrEmptyArray(host["Binds"]) || !nilOrEmptyArray(host["VolumesFrom"]) || !nilOrEmptyArray(host["Links"]) || !nilOrEmptyArray(host["ExtraHosts"]) {
		diagnostic.reject("ADMIT_HOST_SECURITY", errGuardProtocol)
		return nil, errGuardProtocol
	}
	if host["Init"] != nil && host["Init"] != false || host["PidMode"] != nil && host["PidMode"] != "" || host["UTSMode"] != nil && host["UTSMode"] != "" || host["UsernsMode"] != nil && host["UsernsMode"] != "" || host["Runtime"] != nil && host["Runtime"] != "" && host["Runtime"] != "g2-bpf" {
		diagnostic.reject("ADMIT_HOST_MODES_RUNTIME", errGuardProtocol)
		return nil, errGuardProtocol
	}
	if sysctls, exists := host["Sysctls"]; exists && sysctls != nil {
		if m, ok := sysctls.(map[string]any); !ok || len(m) != 0 {
			diagnostic.reject("ADMIT_HOST_SYSCTLS", errGuardProtocol)
			return nil, errGuardProtocol
		}
	}
	if annotations, exists := host["Annotations"]; exists && annotations != nil {
		if m, ok := annotations.(map[string]any); !ok || len(m) != 0 {
			diagnostic.reject("ADMIT_HOST_ANNOTATIONS", errGuardProtocol)
			return nil, errGuardProtocol
		}
	}
	if _, exists := wantHost["Tmpfs"]; !exists {
		if tmpfs, hasTmpfs := host["Tmpfs"]; hasTmpfs && tmpfs != nil {
			if m, ok := tmpfs.(map[string]any); !ok || len(m) != 0 {
				diagnostic.reject("ADMIT_HOST_EXTRA_TMPFS", errGuardProtocol)
				return nil, errGuardProtocol
			}
		}
	}
	var memVal, swapVal, nanoVal, pidsVal int64
	var hasPids bool
	if wMem, exists := wantHost["Memory"]; exists && wMem != nil {
		u, ok := exactNumber(wMem)
		if !ok || u == 0 {
			diagnostic.reject("ADMIT_WANT_MEMORY", errGuardProtocol)
			return nil, errGuardProtocol
		}
		memVal = int64(u)
		swapVal = memVal * 2
		if host["Memory"] != jsonI64(memVal) || host["MemorySwap"] != jsonI64(swapVal) {
			diagnostic.reject("ADMIT_MEMORY_SWAP", errGuardProtocol)
			return nil, errGuardProtocol
		}
	} else if (host["Memory"] != nil && host["Memory"] != json.Number("0")) || (host["MemorySwap"] != nil && host["MemorySwap"] != json.Number("0")) {
		diagnostic.reject("ADMIT_DEFAULT_MEMORY_SWAP", errGuardProtocol)
		return nil, errGuardProtocol
	}
	if wCPU, exists := wantHost["NanoCpus"]; exists && wCPU != nil {
		u, ok := exactNumber(wCPU)
		if !ok || u == 0 {
			diagnostic.reject("ADMIT_WANT_CPU", errGuardProtocol)
			return nil, errGuardProtocol
		}
		nanoVal = int64(u)
		if host["NanoCpus"] != jsonI64(nanoVal) {
			diagnostic.reject("ADMIT_CPU", errGuardProtocol)
			return nil, errGuardProtocol
		}
	} else if host["NanoCpus"] != nil && host["NanoCpus"] != json.Number("0") {
		diagnostic.reject("ADMIT_DEFAULT_CPU", errGuardProtocol)
		return nil, errGuardProtocol
	}
	if wPids, exists := wantHost["PidsLimit"]; exists && wPids != nil {
		u, ok := exactNumber(wPids)
		if !ok || u == 0 {
			diagnostic.reject("ADMIT_WANT_PIDS", errGuardProtocol)
			return nil, errGuardProtocol
		}
		pidsVal, hasPids = int64(u), true
		if host["PidsLimit"] != jsonI64(pidsVal) {
			diagnostic.reject("ADMIT_PIDS", errGuardProtocol)
			return nil, errGuardProtocol
		}
	} else if host["PidsLimit"] != nil && host["PidsLimit"] != json.Number("0") {
		diagnostic.reject("ADMIT_DEFAULT_PIDS", errGuardProtocol)
		return nil, errGuardProtocol
	}
	for _, zeroKey := range []string{"MemoryReservation", "CpuShares", "CpuPeriod", "CpuQuota", "CpuRealtimePeriod", "CpuRealtimeRuntime", "BlkioWeight", "OomScoreAdj"} {
		if host[zeroKey] != nil && host[zeroKey] != json.Number("0") {
			switch zeroKey {
			case "MemoryReservation":
				diagnostic.reject("ADMIT_ZERO_MEMORY_RESERVATION", errGuardProtocol)
			case "CpuShares":
				diagnostic.reject("ADMIT_ZERO_CPU_SHARES", errGuardProtocol)
			case "CpuPeriod":
				diagnostic.reject("ADMIT_ZERO_CPU_PERIOD", errGuardProtocol)
			case "CpuQuota":
				diagnostic.reject("ADMIT_ZERO_CPU_QUOTA", errGuardProtocol)
			case "CpuRealtimePeriod":
				diagnostic.reject("ADMIT_ZERO_CPU_REALTIME_PERIOD", errGuardProtocol)
			case "CpuRealtimeRuntime":
				diagnostic.reject("ADMIT_ZERO_CPU_REALTIME_RUNTIME", errGuardProtocol)
			case "BlkioWeight":
				diagnostic.reject("ADMIT_ZERO_BLKIO_WEIGHT", errGuardProtocol)
			case "OomScoreAdj":
				diagnostic.reject("ADMIT_ZERO_OOM_SCORE", errGuardProtocol)
			}
			return nil, errGuardProtocol
		}
	}
	for _, emptyStr := range []string{"CpusetCpus", "CpusetMems", "VolumeDriver"} {
		if host[emptyStr] != nil && host[emptyStr] != "" {
			switch emptyStr {
			case "CpusetCpus":
				diagnostic.reject("ADMIT_EMPTY_CPUSET_CPUS", errGuardProtocol)
			case "CpusetMems":
				diagnostic.reject("ADMIT_EMPTY_CPUSET_MEMS", errGuardProtocol)
			case "VolumeDriver":
				diagnostic.reject("ADMIT_EMPTY_VOLUME_DRIVER", errGuardProtocol)
			}
			return nil, errGuardProtocol
		}
	}
	if host["OomKillDisable"] != nil && host["OomKillDisable"] != false || host["MemorySwappiness"] != nil {
		diagnostic.reject("ADMIT_HOST_OOM_SWAPPINESS", errGuardProtocol)
		return nil, errGuardProtocol
	}
	if host["ShmSize"] != jsonI64(defaultShmSize) || host["IpcMode"] != "private" || host["CgroupnsMode"] != "private" {
		diagnostic.reject("ADMIT_HOST_SHM_IPC_CGROUPNS", errGuardProtocol)
		return nil, errGuardProtocol
	}
	expectedMasked := make([]any, 0, len(defaultLinuxMaskedPathsFromSysInfo(sysInfo)))
	for _, p := range defaultLinuxMaskedPathsFromSysInfo(sysInfo) {
		expectedMasked = append(expectedMasked, p)
	}
	expectedReadonly := make([]any, 0, len(defaultLinuxReadonlyPaths()))
	for _, p := range defaultLinuxReadonlyPaths() {
		expectedReadonly = append(expectedReadonly, p)
	}
	if !sameJSON(host["MaskedPaths"], expectedMasked) || !sameJSON(host["ReadonlyPaths"], expectedReadonly) {
		diagnostic.reject("ADMIT_HOST_MASKED_READONLY_PATHS", errGuardProtocol)
		return nil, errGuardProtocol
	}
	if ports, ok := host["PortBindings"].(map[string]any); ok {
		for _, value := range ports {
			if !nilOrEmptyArray(value) {
				diagnostic.reject("ADMIT_PORT_BINDING_VALUE", errGuardProtocol)
				return nil, errGuardProtocol
			}
		}
	} else if host["PortBindings"] != nil {
		diagnostic.reject("ADMIT_PORT_BINDINGS_TYPE", errGuardProtocol)
		return nil, errGuardProtocol
	}
	netMode, ok := host["NetworkMode"].(string)
	if !ok || netMode == "" {
		diagnostic.reject("ADMIT_NETWORK_MODE", errGuardProtocol)
		return nil, errGuardProtocol
	}
	appArmorProfile, _ := item["AppArmorProfile"].(string)
	processLabel, _ := item["ProcessLabel"].(string)
	mountLabel, _ := item["MountLabel"].(string)
	if processLabel != "" || mountLabel != "" {
		diagnostic.reject("ADMIT_SECURITY_LABELS", errGuardProtocol)
		return nil, errGuardProtocol
	}
	if sysInfo.appArmorSupported {
		if appArmorProfile != defaultAppArmorName && appArmorProfile != "" {
			diagnostic.reject("ADMIT_APPARMOR_SUPPORTED_PROFILE", errGuardProtocol)
			return nil, errGuardProtocol
		}
		appArmorProfile = defaultAppArmorName
	} else if appArmorProfile != "" {
		diagnostic.reject("ADMIT_APPARMOR_UNSUPPORTED_PROFILE", errGuardProtocol)
		return nil, errGuardProtocol
	}
	facts := &runtimeContainerFacts{
		env:             env,
		configEnv:       actualEnvSlice,
		imageEnv:        imageEnvSlice,
		overrideEnv:     overrideEnvSlice,
		networkMode:     netMode,
		ipcMode:         "private",
		cgroupnsMode:    "private",
		appArmorProfile: appArmorProfile,
		processLabel:    processLabel,
		mountLabel:      mountLabel,
		memory:          memVal,
		memorySwap:      swapVal,
		nanoCPUs:        nanoVal,
		pidsLimit:       pidsVal,
		hasPidsLimit:    hasPids,
		shmSize:         defaultShmSize,
		readonlyRootfs:  true,
		privileged:      false,
		noNewPrivileges: true,
		oomScoreAdj:     0,
		sysInfo:         sysInfo,
		sysInfoValid:    true,
	}
	facts.hostname, ok = actual["Hostname"].(string)
	if !ok || facts.hostname == "" || len(facts.hostname) > 64 {
		diagnostic.reject("ADMIT_HOSTNAME", errGuardProtocol)
		return nil, errGuardProtocol
	}
	facts.path, ok = item["Path"].(string)
	if !ok || facts.path == "" {
		diagnostic.reject("ADMIT_PROCESS_PATH", errGuardProtocol)
		return nil, errGuardProtocol
	}
	facts.args, ok = item["Args"].([]any)
	if !ok {
		diagnostic.reject("ADMIT_PROCESS_ARGS", errGuardProtocol)
		return nil, errGuardProtocol
	}
	facts.cwd, ok = actual["WorkingDir"].(string)
	if !ok {
		diagnostic.reject("ADMIT_PROCESS_WORKDIR", errGuardProtocol)
		return nil, errGuardProtocol
	}
	if facts.cwd == "" {
		facts.cwd = "/"
	}
	facts.user, ok = actual["User"].(string)
	if !ok {
		diagnostic.reject("ADMIT_PROCESS_USER", errGuardProtocol)
		return nil, errGuardProtocol
	}
	graph, ok := item["GraphDriver"].(map[string]any)
	if !ok || graph["Name"] != "overlay2" {
		diagnostic.reject("ADMIT_GRAPH_DRIVER", errGuardProtocol)
		return nil, errGuardProtocol
	}
	data, ok := graph["Data"].(map[string]any)
	if !ok {
		diagnostic.reject("ADMIT_GRAPH_DATA", errGuardProtocol)
		return nil, errGuardProtocol
	}
	facts.rootfs, ok = data["MergedDir"].(string)
	if !ok || !runtimePath(facts.rootfs) {
		diagnostic.reject("ADMIT_ROOTFS_PATH", errGuardProtocol)
		return nil, errGuardProtocol
	}
	if err := inspectRuntimeSystemPaths(cid, item, facts, diagnostic); err != nil {
		return nil, err
	}
	mounts, ok := item["Mounts"].([]any)
	if !ok {
		diagnostic.reject("ADMIT_MOUNTS_TYPE", errGuardProtocol)
		return nil, errGuardProtocol
	}
	seen := make(map[string]bool)
	for _, raw := range mounts {
		mount, ok := raw.(map[string]any)
		if !ok {
			diagnostic.reject("ADMIT_MOUNT_ITEM_TYPE", errGuardProtocol)
			return nil, errGuardProtocol
		}
		typ, _ := mount["Type"].(string)
		source, a := mount["Source"].(string)
		dest, d := mount["Destination"].(string)
		rw, r := mount["RW"].(bool)
		prop, _ := mount["Propagation"].(string)
		if !a || !d || !r || !runtimePath(source) || !runtimePath(dest) || seen[dest] || typ != "bind" && typ != "volume" {
			diagnostic.reject("ADMIT_MOUNT_ITEM_FIELDS", errGuardProtocol)
			return nil, errGuardProtocol
		}
		if typ == "bind" && prop == "" {
			prop = "rprivate"
		}
		if prop != "" && prop != "rprivate" {
			diagnostic.reject("ADMIT_MOUNT_PROPAGATION", errGuardProtocol)
			return nil, errGuardProtocol
		}
		seen[dest] = true
		facts.mounts = append(facts.mounts, runtimeMountFact{typ: typ, source: source, destination: dest, propagation: prop, readonly: !rw})
	}
	var requested []any
	if raw := wantHost["Mounts"]; raw != nil {
		var ok bool
		requested, ok = raw.([]any)
		if !ok {
			diagnostic.reject("ADMIT_REQUESTED_MOUNT_FIELDS", errGuardProtocol)
			return nil, errGuardProtocol
		}
	}
	if len(requested) != len(facts.mounts) {
		diagnostic.reject("ADMIT_UNREQUESTED_MOUNT", errGuardProtocol)
		return nil, errGuardProtocol
	}
	for _, raw := range requested {
		m, ok := raw.(map[string]any)
		if !ok || m["Type"] != "bind" || m["BindOptions"] != nil {
			diagnostic.reject("ADMIT_REQUESTED_MOUNT_FIELDS", errGuardProtocol)
			return nil, errGuardProtocol
		}
		matched := false
		for _, fact := range facts.mounts {
			if fact.typ == "bind" && fact.source == m["Source"] && fact.destination == m["Target"] && fact.readonly == (m["ReadOnly"] == true) {
				matched = true
			}
		}
		if !matched {
			diagnostic.reject("ADMIT_REQUESTED_MOUNT_MATCH", errGuardProtocol)
			return nil, errGuardProtocol
		}
	}
	for _, mount := range facts.mounts {
		if mount.source != ownerProbeSourcePath {
			continue
		}
		if len(facts.mounts) != 1 || mount.typ != "bind" || mount.destination != ownerProbeTargetPath || !mount.readonly {
			diagnostic.reject("ADMIT_SOURCE_MOUNT_IDENTITY", errGuardProtocol)
			return nil, errGuardProtocol
		}
		identity, err := readOwnerProbeSourceIdentity()
		if err != nil {
			diagnostic.reject("ADMIT_SOURCE_MOUNT_IDENTITY", err)
			return nil, err
		}
		facts.ownerProbeSource = identity
		facts.hasOwnerProbeSource = true
	}
	if err := verifyRuntimeMountSources(facts, diagnostic); err != nil {
		diagnostic.reject("ADMIT_MOUNT_IDENTITIES", err)
		return nil, err
	}
	if network, ok := item["NetworkSettings"].(map[string]any); ok {
		facts.sandboxPath, _ = network["SandboxKey"].(string)
	}
	facts.shmPath, _ = item["ShmPath"].(string)
	if facts.shmPath != "" {
		diagnostic.reject("ADMIT_SHM_PATH", errGuardProtocol)
		return nil, errGuardProtocol
	}
	return facts, nil
}
