//go:build linux && amd64 && !cgo

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

func hashBytes(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func readHeldRegular(fd int, max int64) ([]byte, error) {
	return readHeldRegularWithDiagnostic(fd, max, nil)
}

func readHeldRegularWithDiagnostic(fd int, max int64, d *runtimeInputDiagnostic) ([]byte, error) {
	_, before, err := heldRegularDigestWithDiagnostic(fd, max, d)
	if err != nil {
		return nil, err
	}
	data := make([]byte, int(before.Size))
	offset := 0
	for offset < len(data) {
		n, e := unix.Pread(fd, data[offset:], int64(offset))
		if e != nil {
			d.reject(ownerProbeStageWrapperPrepareReadDataRead)
			return nil, errGuardProtocol
		}
		if n <= 0 {
			d.reject(ownerProbeStageWrapperPrepareReadDataCount)
			return nil, errGuardProtocol
		}
		offset += n
	}
	var after unix.Stat_t
	if unix.Fstat(fd, &after) != nil {
		d.reject(ownerProbeStageWrapperPrepareReadDataStat)
		return nil, errGuardProtocol
	}
	if difference := trustedStatDifference(before, after); difference != runtimeStatSame {
		d.rejectDifference(ownerProbeStageWrapperPrepareReadDataAfterDev, difference)
		return nil, errGuardProtocol
	}
	return data, nil
}

func readAtRegular(dir int, name string, max int64) ([]byte, unix.Stat_t, error) {
	var st unix.Stat_t
	if name == "" || strings.Contains(name, "/") || name == "." || name == ".." {
		return nil, st, errGuardProtocol
	}
	fd, err := unix.Openat(dir, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, st, err
	}
	defer unix.Close(fd)
	data, err := readHeldRegular(fd, max)
	if err != nil {
		return nil, st, err
	}
	var named unix.Stat_t
	if unix.Fstat(fd, &st) != nil || unix.Fstatat(dir, name, &named, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameTrustedStat(st, named) {
		return nil, unix.Stat_t{}, errGuardProtocol
	}
	return data, st, nil
}

func readCanonicalLine(raw []byte) (map[string]any, error) {
	if len(raw) < 2 || raw[len(raw)-1] != '\n' {
		return nil, errControlFrame
	}
	return parseCanonicalControl(raw[:len(raw)-1])
}

func carrierMount(fd int, target, fsType, serial string) (unix.Stat_t, error) {
	var st unix.Stat_t
	var fs unix.Statfs_t
	if unix.Fstat(fd, &st) != nil || unix.Fstatfs(fd, &fs) != nil || st.Mode&unix.S_IFMT != unix.S_IFDIR || fs.Flags&unix.ST_RDONLY == 0 {
		return st, errGuardProtocol
	}
	magic := int64(unix.EXT4_SUPER_MAGIC)
	if fsType == "vfat" {
		magic = unix.MSDOS_SUPER_MAGIC
	}
	if int64(fs.Type) != magic {
		return st, errGuardProtocol
	}
	mounts, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil || len(mounts) == 0 || len(mounts) > 4<<20 {
		return st, errGuardProtocol
	}
	device := fmt.Sprintf("%d:%d", unix.Major(uint64(st.Dev)), unix.Minor(uint64(st.Dev)))
	found := false
	for _, line := range strings.Split(string(mounts), "\n") {
		if line == "" {
			continue
		}
		left, right, ok := strings.Cut(line, " - ")
		if !ok {
			return st, errGuardProtocol
		}
		a, b := strings.Fields(left), strings.Fields(right)
		if len(a) < 6 || len(b) < 3 {
			return st, errGuardProtocol
		}
		if a[4] != target {
			continue
		}
		if found || a[2] != device || b[0] != fsType {
			return st, errGuardProtocol
		}
		found = true
		opts := "," + a[5] + ","
		for _, opt := range []string{"ro", "nosuid", "nodev", "noexec"} {
			if !strings.Contains(opts, ","+opt+",") {
				return st, errGuardProtocol
			}
		}
		if fsType == "ext4" {
			super := "," + b[2] + ","
			if !strings.Contains(super, ",noload,") && !strings.Contains(super, ",norecovery,") {
				return st, errGuardProtocol
			}
		}
	}
	if !found {
		return st, errGuardProtocol
	}
	link := "/dev/disk/by-id/virtio-" + serial
	if fsType == "vfat" {
		link += "-part1"
	}
	block, err := unix.Open(link, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return st, err
	}
	defer unix.Close(block)
	var blockStat unix.Stat_t
	if unix.Fstat(block, &blockStat) != nil || blockStat.Mode&unix.S_IFMT != unix.S_IFBLK || blockStat.Rdev != uint64(st.Dev) {
		return st, errGuardProtocol
	}
	ro, err := unix.IoctlGetInt(block, unix.BLKROGET)
	if err != nil || ro != 1 {
		return st, errGuardProtocol
	}
	sys := "/sys/dev/block/" + device
	if fsType == "vfat" {
		part, err := os.ReadFile(sys + "/partition")
		if err != nil || strings.TrimSpace(string(part)) != "1" {
			return st, errGuardProtocol
		}
		resolved, err := filepath.EvalSymlinks(sys)
		if err != nil {
			return st, err
		}
		sys = filepath.Dir(resolved)
	}
	value, err := os.ReadFile(sys + "/serial")
	if err != nil || len(value) > 128 || strings.TrimSpace(string(value)) != serial {
		return st, errGuardProtocol
	}
	return st, nil
}

func seedGeometry(fd int) (string, uint64, error) {
	block, err := unix.Open("/dev/disk/by-id/virtio-G2SEED", unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return "", 0, err
	}
	defer unix.Close(block)
	var mounted, device unix.Stat_t
	if unix.Fstat(fd, &mounted) != nil || unix.Fstat(block, &device) != nil || device.Rdev != uint64(mounted.Dev) {
		return "", 0, errGuardProtocol
	}
	var size uint64
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(block), unix.BLKGETSIZE64, uintptr(unsafe.Pointer(&size)))
	if errno != 0 || size == 0 {
		return "", 0, errGuardProtocol
	}
	buf := make([]byte, 256)
	n, err := unix.Pread(block, buf, 1024)
	if err != nil || n != len(buf) || buf[56] != 0x53 || buf[57] != 0xef {
		return "", 0, errGuardProtocol
	}
	if !bytes.Equal(bytes.TrimRight(buf[120:136], "\x00"), []byte("G2SEED")) {
		return "", 0, errGuardProtocol
	}
	uuid := hex.EncodeToString(buf[104:120])
	if len(uuid) != 32 || uuid == strings.Repeat("0", 32) {
		return "", 0, errGuardProtocol
	}
	return uuid[:8] + "-" + uuid[8:12] + "-" + uuid[12:16] + "-" + uuid[16:20] + "-" + uuid[20:], size, nil
}

func verifySourceCarrier(seedFD, caseFD int, manifest bootstrapManifest) (map[string]any, error) {
	seed, err := carrierMount(seedFD, "/mnt/g2-seed", "ext4", "G2SEED")
	if err != nil {
		return nil, err
	}
	cidata, err := unix.Open("/mnt/g2-cidata", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	defer unix.Close(cidata)
	cidataStat, err := carrierMount(cidata, "/mnt/g2-cidata", "vfat", "G2CIDATA")
	if err != nil || seed.Dev == cidataStat.Dev {
		return nil, errGuardProtocol
	}
	caseRaw, err := readHeldRegular(caseFD, 4096)
	if err != nil {
		return nil, err
	}
	caseNamed, caseStat, err := readAtRegular(seedFD, "case.json", 4096)
	if err != nil || !bytes.Equal(caseRaw, caseNamed) {
		return nil, errGuardProtocol
	}
	var caseHeld unix.Stat_t
	if unix.Fstat(caseFD, &caseHeld) != nil || !sameTrustedStat(caseHeld, caseStat) {
		return nil, errGuardProtocol
	}
	value, err := readCanonicalLine(caseRaw)
	if err != nil || !exactKeys(value, "version", "run_token", "case", "scenario", "bpf_variant", "qemu_sha256", "boot_inputs", "bpf_bootstrap") || value["version"] != json.Number("1") || value["run_token"] != authorityRunToken {
		return nil, errGuardProtocol
	}
	if value["bpf_variant"] != nil {
		if _, ok := manifestText(value["bpf_variant"], 128); !ok {
			return nil, errGuardProtocol
		}
	}
	inputs, ok := value["boot_inputs"].(map[string]any)
	if !ok || !exactKeys(inputs, "version", "base_sha256", "kernel_release", "kernel_path", "kernel_bytes", "kernel_sha256", "original_initrd_path", "original_initrd_sha256", "initrd_path", "initrd_bytes", "initrd_sha256", "bootstrap_manifest_sha256", "observer_bindings_sha256", "command_line") || inputs["version"] != json.Number("1") || inputs["kernel_release"] != "6.8.0-147-generic" || inputs["kernel_sha256"] != manifest.KernelSHA256 || inputs["original_initrd_sha256"] != manifest.OriginalInitrdSHA256 || inputs["observer_bindings_sha256"] != manifest.ObserverBindingsSHA256 {
		return nil, errGuardProtocol
	}
	bootstrap, ok := value["bpf_bootstrap"].(map[string]any)
	if !ok {
		return nil, errGuardProtocol
	}
	bootstrapRaw, err := canonicalControl(bootstrap)
	if err != nil || hashBytes(bootstrapRaw) != inputs["bootstrap_manifest_sha256"] {
		return nil, errGuardProtocol
	}
	parsed, err := parseBootstrapManifest(bootstrapRaw)
	if err != nil || !reflect.DeepEqual(parsed, manifest) {
		return nil, errGuardProtocol
	}
	for _, key := range []string{"case", "scenario"} {
		if _, ok := manifestText(value[key], 128); !ok {
			return nil, errGuardProtocol
		}
	}
	if _, ok := digestString(value["qemu_sha256"]); !ok {
		return nil, errGuardProtocol
	}
	sums, _, err := readAtRegular(seedFD, "SHA256SUMS", 65536)
	if err != nil {
		return nil, err
	}
	entries := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSuffix(string(sums), "\n"), "\n") {
		if len(line) < 67 || line[64:66] != "  " {
			return nil, errGuardProtocol
		}
		key := line[66:]
		if key == "" || strings.Contains(key, "/") {
			return nil, errGuardProtocol
		}
		digest, ok := digestString(line[:64])
		if !ok {
			return nil, errGuardProtocol
		}
		if _, exists := entries[key]; exists {
			return nil, errGuardProtocol
		}
		entries[key] = digest
	}
	if entries["case.json"] != hashBytes(caseRaw) || entries["source_guest.py"] != manifest.Scripts["source_guest"] {
		return nil, errGuardProtocol
	}
	script, _, err := readAtRegular(seedFD, "source_guest.py", 16<<20)
	if err != nil || hashBytes(script) != manifest.Scripts["source_guest"] {
		return nil, errGuardProtocol
	}
	browserRaw, _, err := readAtRegular(seedFD, "browser-selection.json", 65536)
	if err != nil || hashBytes(browserRaw) != entries["browser-selection.json"] {
		return nil, errGuardProtocol
	}
	browser, err := readCanonicalLine(browserRaw)
	if err != nil || !exactKeys(browser, "version", "run_token", "selection_id", "attempt_id", "receipt_sha256", "manifest_sha256", "rootfs_sha256", "image_id", "image_archive_sha256", "program_sha256", "bundle_sha256") || browser["version"] != json.Number("1") || browser["run_token"] != authorityRunToken || browser["image_archive_sha256"] != entries["browser-image.tar"] {
		return nil, errGuardProtocol
	}
	for _, key := range []string{"receipt_sha256", "manifest_sha256", "rootfs_sha256", "image_archive_sha256", "program_sha256", "bundle_sha256"} {
		if _, ok := digestString(browser[key]); !ok {
			return nil, errGuardProtocol
		}
	}
	for _, key := range []string{"selection_id", "attempt_id"} {
		id, ok := browser[key].(string)
		if !ok || len(id) != 20 {
			return nil, errGuardProtocol
		}
		for _, c := range id {
			if c < '0' || c > '9' {
				return nil, errGuardProtocol
			}
		}
	}
	image, ok := browser["image_id"].(string)
	if !ok || len(image) != 71 || !strings.HasPrefix(image, "sha256:") {
		return nil, errGuardProtocol
	}
	if _, ok = digestString(image[7:]); !ok {
		return nil, errGuardProtocol
	}
	value["_broker_browser_image"] = image
	supervisor, _, err := readAtRegular(seedFD, "supervisor-manifest.json", 65536)
	if err != nil || entries["supervisor-manifest.json"] != hashBytes(supervisor) {
		return nil, errGuardProtocol
	}
	descriptorRaw, _, err := readAtRegular(cidata, "seed-descriptor.json", maxControlFrame)
	if err != nil {
		return nil, err
	}
	descriptor, err := readCanonicalLine(descriptorRaw)
	if err != nil || !exactKeys(descriptor, "version", "run_token", "case", "scenario", "payload_case_sha256", "payload_manifest_sha256", "seed_uuid", "seed_size", "host_launch_sha256", "host_argv_sha256", "qemu_sha256") || descriptor["version"] != json.Number("1") {
		return nil, errGuardProtocol
	}
	for _, key := range []string{"run_token", "case", "scenario", "qemu_sha256"} {
		if descriptor[key] != value[key] {
			return nil, errGuardProtocol
		}
	}
	if descriptor["payload_case_sha256"] != hashBytes(caseRaw) || descriptor["payload_manifest_sha256"] != hashBytes(sums) {
		return nil, errGuardProtocol
	}
	for _, key := range []string{"host_launch_sha256", "host_argv_sha256"} {
		if _, ok := digestString(descriptor[key]); !ok {
			return nil, errGuardProtocol
		}
	}
	uuid, size, err := seedGeometry(seedFD)
	if err != nil || descriptor["seed_uuid"] != uuid {
		return nil, errGuardProtocol
	}
	declaredSize, ok := exactNumber(descriptor["seed_size"])
	if !ok || declaredSize != size {
		return nil, errGuardProtocol
	}
	var named unix.Stat_t
	if unix.Stat("/mnt/g2-seed", &named) != nil || named.Dev != seed.Dev || named.Ino != seed.Ino {
		return nil, errGuardProtocol
	}
	return value, nil
}
