//go:build linux && amd64 && !cgo

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

type recoveryContainer struct {
	id     string
	config map[string]any
}

type recoveryBinding struct {
	containers                                      map[string]recoveryContainer
	uid, gid                                        uint32
	controlFD, seedFD, caseFD                       int
	intentSHA, usedSHA, credentialSHA, originalBoot string
}

func recoveryCanonical(b *bytes.Buffer, value any, depth, limit int) error {
	if depth > 64 || b.Len() > limit {
		return errControlFrame
	}
	switch v := value.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		if v {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case string:
		if err := appendJSONString(b, v, limit); err != nil {
			return err
		}
	case json.Number:
		s := string(v)
		if strings.ContainsAny(s, ".eE+") || s == "-0" {
			return errControlFrame
		}
		if strings.HasPrefix(s, "-") {
			if _, err := strconv.ParseInt(s, 10, 64); err != nil {
				return errControlFrame
			}
		} else if _, err := strconv.ParseUint(s, 10, 64); err != nil {
			return errControlFrame
		}
		b.WriteString(s)
	case []any:
		b.WriteByte('[')
		for i, item := range v {
			if i != 0 {
				b.WriteByte(',')
			}
			if err := recoveryCanonical(b, item, depth+1, limit); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		b.WriteByte('{')
		for i, key := range keys {
			if i != 0 {
				b.WriteByte(',')
			}
			if err := appendJSONString(b, key, limit); err != nil {
				return err
			}
			b.WriteByte(':')
			if err := recoveryCanonical(b, v[key], depth+1, limit); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	default:
		return errControlFrame
	}
	if b.Len() > limit {
		return errControlFrame
	}
	return nil
}

func recoveryDocument(raw []byte, limit int) (map[string]any, error) {
	if len(raw) == 0 || len(raw) > limit {
		return nil, errControlFrame
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	v, err := readJSONValue(d, 0)
	if err != nil {
		return nil, err
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, errControlFrame
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, errControlFrame
	}
	var encoded bytes.Buffer
	if err = recoveryCanonical(&encoded, m, 0, limit); err != nil || !bytes.Equal(raw, encoded.Bytes()) {
		return nil, errControlFrame
	}
	return m, nil
}

func recoveryRead(dir int, name string, limit int64) ([]byte, map[string]any, error) {
	raw, st, err := readAtRegular(dir, name, limit)
	if err != nil || st.Mode&07777 != 0600 || st.Nlink != 1 || st.Uid != 0 || st.Gid != 0 {
		return nil, nil, errGuardProtocol
	}
	value, err := recoveryDocument(raw, int(limit))
	if err != nil {
		return nil, nil, err
	}
	return raw, value, nil
}

func recoveryRealmDigest(genesis, app []byte) string {
	h := sha256.New()
	for _, value := range [][]byte{[]byte("HOUFENG-C80-REALM-V1"), genesis, app} {
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(value)))
		_, _ = h.Write(length[:])
		_, _ = h.Write(value)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// recoveryJournal verifies the original held journal and its raw hash chain.
// Its owner remains closed; this proof only selects the recovery ceremony.
func recoveryJournal(dir int, intentSHA string, genesisRaw, appRaw []byte) error {
	fd, err := unix.Openat(dir, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), "realm-control")
	if file == nil {
		_ = unix.Close(fd)
		return errGuardProtocol
	}
	defer file.Close()
	names, err := file.Readdirnames(-1)
	if err != nil || len(names) > 16384 {
		return errGuardProtocol
	}
	var journal []string
	for _, name := range names {
		if strings.HasPrefix(name, "journal-") {
			journal = append(journal, name)
		}
	}
	sort.Strings(journal)
	if len(journal) < 2 {
		return errGuardProtocol
	}
	keys := map[string][]string{
		"genesis": {"genesis_digest"}, "app_binding": {"app_digest"},
		"phase_enter": {"phase", "phase_generation"}, "phase_exit": {"phase", "phase_generation"},
		"admission": {"name", "phase_generation", "action_digest"}, "registration": {"name", "identity_digest"},
		"launch": {"name", "pid", "starttime", "executable_sha256"}, "exit": {"name", "pid", "starttime", "status", "pidfd_exited"},
		"observation": {"phase_generation", "observation_sequence", "roots_digest", "valid"},
	}
	previous := strings.Repeat("0", 64)
	for index, name := range journal {
		if name != fmt.Sprintf("journal-%020d.json", index+1) {
			return errGuardProtocol
		}
		raw, event, err := recoveryRead(dir, name, maxControlFrame)
		if err != nil {
			return err
		}
		if !exactKeys(event, "version", "sequence", "previous", "kind", "fields") || event["version"] != json.Number("1") || event["sequence"] != jsonU64(uint64(index+1)) || event["previous"] != previous {
			return errGuardProtocol
		}
		kind, ok := event["kind"].(string)
		if !ok {
			return errGuardProtocol
		}
		fields, ok := event["fields"].(map[string]any)
		if !ok {
			return errGuardProtocol
		}
		want, ok := keys[kind]
		if !ok || !exactKeys(fields, want...) {
			return errGuardProtocol
		}
		if index == 0 && (kind != "genesis" || fields["genesis_digest"] != hashBytes(genesisRaw)) {
			return errGuardProtocol
		}
		if index == 1 && (kind != "app_binding" || fields["app_digest"] != hashBytes(appRaw)) {
			return errGuardProtocol
		}
		if index > 0 && kind == "genesis" || index > 1 && kind == "app_binding" {
			return errGuardProtocol
		}
		if index == len(journal)-1 && (kind != "registration" || fields["name"] != "vm-kill-ready" || fields["identity_digest"] != intentSHA) {
			return errGuardProtocol
		}
		previous = hashBytes(raw)
	}
	return nil
}

func validateRecoveryIntent(intent, app map[string]any) error {
	application, ok := intent["application"].(map[string]any)
	if !ok || !exactKeys(application, "ID", "SystemIdentifier", "DatabaseName", "DatabaseOID", "Epoch") {
		return errGuardProtocol
	}
	for _, key := range []string{"ID", "SystemIdentifier", "DatabaseName"} {
		value, ok := application[key].(string)
		if !ok || len(value) == 0 || len(value) > 256 {
			return errGuardProtocol
		}
	}
	for _, key := range []string{"DatabaseOID", "Epoch"} {
		value, ok := exactNumber(application[key])
		if !ok || value == 0 || value > uint64(^uint64(0)>>1) || key == "DatabaseOID" && value > 0xffffffff {
			return errGuardProtocol
		}
	}
	rid, ok := intent["recovery_id"].(string)
	if !ok || len(rid) != 27 || !strings.HasPrefix(rid, "g2-") {
		return errGuardProtocol
	}
	for _, c := range rid[3:] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return errGuardProtocol
		}
	}
	phase, ok := intent["phase"].(map[string]any)
	if !ok || !exactKeys(phase, "version", "generation", "previous", "recovery_id", "application", "phase") || phase["version"] != json.Number("1") || phase["recovery_id"] != rid || phase["phase"] != "quiescent" || !sameJSON(phase["application"], application) {
		return errGuardProtocol
	}
	generation, ok := exactNumber(phase["generation"])
	if !ok || generation == 0 || generation > uint64(^uint64(0)>>1) {
		return errGuardProtocol
	}
	previous, ok := phase["previous"].([]any)
	if !ok || len(previous) != 32 {
		return errGuardProtocol
	}
	for _, item := range previous {
		n, ok := exactNumber(item)
		if !ok || n > 255 {
			return errGuardProtocol
		}
	}
	source, ok := intent["source"].(map[string]any)
	if !ok || !exactKeys(source, "deployment_id", "project_id", "kind", "id") || source["project_id"] != "default" || !sameJSON(source["deployment_id"], app["deployment_id"]) {
		return errGuardProtocol
	}
	kind, ok := source["kind"].(string)
	if !ok || kind != "vps" && kind != "monitoring_instance" && kind != "target" {
		return errGuardProtocol
	}
	prefix := map[string]string{"vps": "vps_", "monitoring_instance": "mi_", "target": "tg_"}[kind]
	value, ok := source["id"].(string)
	if !ok || !strings.HasPrefix(value, prefix) || len(value) != len(prefix)+16 {
		return errGuardProtocol
	}
	for _, c := range value[len(prefix):] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return errGuardProtocol
		}
	}
	operation, ok := intent["source_operation_id"].(string)
	if !ok || operation == "" {
		return errGuardProtocol
	}
	deployment, ok := app["deployment_id"].(string)
	if !ok || deployment == "" {
		return errGuardProtocol
	}
	sourceKey := hashBytes([]byte(deployment + "\x00default\x00" + operation))
	control, ok := intent["control_files"].(map[string]any)
	if !ok || len(control) == 0 {
		return errGuardProtocol
	}
	for name, digest := range control {
		if !strings.HasPrefix(name, "phase-") && !strings.HasPrefix(name, "predecessor-") && !strings.HasPrefix(name, "source-") || strings.Contains(name, "/") || strings.HasPrefix(name, "source-handoff-") {
			return errGuardProtocol
		}
		if _, ok := digestString(digest); !ok {
			return errGuardProtocol
		}
	}
	for _, prefix := range []string{"source-preview-", "source-intent-", "source-witnessed-", "source-anchored-"} {
		name := prefix + sourceKey + ".json"
		if _, ok := control[name]; !ok {
			return errGuardProtocol
		}
		for candidate := range control {
			if strings.HasPrefix(candidate, prefix) && candidate != name {
				return errGuardProtocol
			}
		}
	}
	if _, ok := control[fmt.Sprintf("phase-%020d.json", generation)]; !ok {
		return errGuardProtocol
	}
	return nil
}

// verifyRecoveryBinding binds the held original journal and four prearmed
// containers without reissuing the original source-realm capability.
func verifyRecoveryBinding(dir, seed, caseFD int, manifest bootstrapManifest, boot, intentSHA, usedSHA, originalBoot string) (*recoveryBinding, map[string]any, error) {
	var held, named unix.Stat_t
	if unix.Fstat(dir, &held) != nil || held.Mode&unix.S_IFMT != unix.S_IFDIR || held.Mode&07777 != 0700 || held.Uid != 0 || held.Gid != 0 {
		return nil, nil, errGuardProtocol
	}
	if unix.Lstat("/var/lib/g2/realm-control", &named) != nil || !sameTrustedStat(held, named) {
		return nil, nil, errGuardProtocol
	}
	caseFacts, err := verifySourceCarrier(seed, caseFD, manifest)
	if err != nil {
		return nil, nil, err
	}
	if caseFacts["case"] != "source-recovery" || caseFacts["scenario"] != "vm-kill" {
		return nil, nil, errGuardProtocol
	}
	genesisRaw, genesis, err := recoveryRead(dir, "realm-genesis.json", 512<<10)
	if err != nil {
		return nil, nil, err
	}
	appRaw, app, err := recoveryRead(dir, "realm-app.json", maxControlFrame)
	if err != nil {
		return nil, nil, err
	}
	if !exactKeys(app, "version", "run_token", "application", "deployment_id", "project_id", "domains", "physical", "guard", "roots_digest") || app["version"] != json.Number("1") || app["project_id"] != "default" {
		return nil, nil, errGuardProtocol
	}
	if _, ok := app["application"].(map[string]any); !ok {
		return nil, nil, errGuardProtocol
	}
	if value, ok := app["deployment_id"].(string); !ok || value == "" {
		return nil, nil, errGuardProtocol
	}
	if _, ok := app["physical"].(map[string]any); !ok {
		return nil, nil, errGuardProtocol
	}
	intentRaw, intent, err := recoveryRead(dir, "reboot-intent.json", maxControlFrame)
	if err != nil {
		return nil, nil, err
	}
	usedRaw, used, err := recoveryRead(dir, "reboot-observer-used.json", maxControlFrame)
	if err != nil {
		return nil, nil, err
	}
	if hashBytes(intentRaw) != intentSHA || hashBytes(usedRaw) != usedSHA || !exactKeys(used, "version", "run_token", "boot_id", "intent_sha256") || used["version"] != json.Number("1") || used["run_token"] != authorityRunToken || used["boot_id"] != boot || used["intent_sha256"] != intentSHA {
		return nil, nil, errGuardProtocol
	}
	if !exactKeys(intent, "version", "run_token", "case", "scenario", "original_boot_id", "realm_digest", "original_bpf_tail", "genesis_sha256", "app_binding_sha256", "application", "deployment_id", "source", "source_operation_id", "phase", "recovery_id", "control_files", "projection", "records", "fixture_records", "app_host", "containers", "postgres_uid", "postgres_gid") || intent["version"] != json.Number("1") || intent["run_token"] != authorityRunToken || intent["case"] != "source-recovery" || intent["scenario"] != "vm-kill" || intent["original_boot_id"] != originalBoot || originalBoot == boot || genesis["boot_id"] != originalBoot || genesis["case"] != "source-recovery" || genesis["scenario"] != "vm-kill" || genesis["run_token"] != authorityRunToken || app["run_token"] != authorityRunToken || !sameJSON(intent["application"], app["application"]) || !sameJSON(intent["deployment_id"], app["deployment_id"]) || intent["genesis_sha256"] != hashBytes(genesisRaw) || intent["app_binding_sha256"] != hashBytes(appRaw) || intent["realm_digest"] != recoveryRealmDigest(genesisRaw, appRaw) {
		return nil, nil, errGuardProtocol
	}
	if _, ok := digestString(intent["original_bpf_tail"]); !ok {
		return nil, nil, errGuardProtocol
	}
	if err = validateRecoveryIntent(intent, app); err != nil {
		return nil, nil, err
	}
	credentialRaw, credential, err := recoveryRead(dir, "reboot-credential.json", maxControlFrame)
	if err != nil {
		return nil, nil, err
	}
	if !exactKeys(credential, "version", "run_token", "case", "scenario", "application", "source_operation_id", "realm_digest", "app_volume", "postgres_password") || credential["version"] != json.Number("1") || credential["run_token"] != authorityRunToken || credential["case"] != "source-recovery" || credential["scenario"] != "vm-kill" || !sameJSON(credential["application"], intent["application"]) || !sameJSON(credential["source_operation_id"], intent["source_operation_id"]) || credential["realm_digest"] != intent["realm_digest"] {
		return nil, nil, errGuardProtocol
	}
	if _, ok := digestString(credential["postgres_password"]); !ok {
		return nil, nil, errGuardProtocol
	}
	volumes, ok := genesis["volumes"].(map[string]any)
	if !ok {
		return nil, nil, errGuardProtocol
	}
	volume, ok := volumes["app"].(map[string]any)
	if !ok {
		return nil, nil, errGuardProtocol
	}
	disk, ok := volume["disk"].(map[string]any)
	if !ok {
		return nil, nil, errGuardProtocol
	}
	appVolume, ok := credential["app_volume"].(map[string]any)
	if !ok || !exactKeys(appVolume, "serial", "mount_ino", "pgdata_ino") || appVolume["serial"] != disk["serial"] || appVolume["mount_ino"] != volume["mount_ino"] || appVolume["pgdata_ino"] != volume["pgdata_ino"] {
		return nil, nil, errGuardProtocol
	}
	sums, _, err := readAtRegular(seed, "SHA256SUMS", 65536)
	if err != nil || genesis["seed_manifest_sha256"] != hashBytes(sums) {
		return nil, nil, errGuardProtocol
	}
	supervisor, _, err := readAtRegular(seed, "supervisor-manifest.json", 65536)
	if err != nil || genesis["supervisor_manifest_sha256"] != hashBytes(supervisor) {
		return nil, nil, errGuardProtocol
	}
	if err = recoveryJournal(dir, intentSHA, genesisRaw, appRaw); err != nil {
		return nil, nil, err
	}
	uid, ok := exactNumber(intent["postgres_uid"])
	if !ok || uid == 0 || uid > 0xffffffff {
		return nil, nil, errGuardProtocol
	}
	gid, ok := exactNumber(intent["postgres_gid"])
	if !ok || gid == 0 || gid > 0xffffffff {
		return nil, nil, errGuardProtocol
	}
	containers, ok := intent["containers"].(map[string]any)
	if !ok || !exactKeys(containers, "app", "w1", "w2", "control") {
		return nil, nil, errGuardProtocol
	}
	result := &recoveryBinding{containers: make(map[string]recoveryContainer), intentSHA: intentSHA, usedSHA: usedSHA, credentialSHA: hashBytes(credentialRaw), originalBoot: originalBoot, uid: uint32(uid), gid: uint32(gid), controlFD: dir, seedFD: seed, caseFD: caseFD}
	seen := make(map[string]bool)
	for _, name := range []string{"app", "w1", "w2", "control"} {
		entry, ok := containers[name].(map[string]any)
		if !ok || !exactKeys(entry, "id", "config") {
			return nil, nil, errGuardProtocol
		}
		id, ok := entry["id"].(string)
		if !ok || !runtimeContainerID(id) || seen[id] {
			return nil, nil, errGuardProtocol
		}
		seen[id] = true
		config, ok := entry["config"].(map[string]any)
		if !ok {
			return nil, nil, errGuardProtocol
		}
		expected, e := postgresTemplate(name, false, uint32(uid), uint32(gid))
		if e != nil || !sameTemplate(config, expected) {
			return nil, nil, errGuardProtocol
		}
		result.containers[name] = recoveryContainer{id: id, config: config}
	}
	var after, namedAfter unix.Stat_t
	if unix.Fstat(dir, &after) != nil || unix.Lstat("/var/lib/g2/realm-control", &namedAfter) != nil || !sameTrustedStat(held, after) || !sameTrustedStat(after, namedAfter) {
		return nil, nil, errGuardProtocol
	}
	return result, caseFacts, nil
}
