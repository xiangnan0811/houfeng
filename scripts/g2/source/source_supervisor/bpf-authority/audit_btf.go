//go:build linux && amd64 && !cgo

package main

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"math"
	"os"

	"golang.org/x/sys/unix"
)

// validateObserverBinding checks the same immutable module section which the
// native code uses, before finit_module. Running BTF and build-ID bind that
// section to the actual guest kernel rather than to an installed filename.
func validateObserverBinding(module *os.File, manifest bootstrapManifest) error {
	image, err := elf.NewFile(module)
	if err != nil {
		return err
	}
	if image.Class != elf.ELFCLASS64 || image.Data != elf.ELFDATA2LSB || image.Machine != elf.EM_X86_64 || image.Type != elf.ET_REL {
		return errGuardProtocol
	}
	var section *elf.Section
	for _, candidate := range image.Sections {
		if candidate.Name == ".g2_observer_binding" {
			if section != nil {
				return errGuardProtocol
			}
			section = candidate
		}
	}
	if section == nil || section.Type != elf.SHT_PROGBITS || section.Flags&elf.SHF_WRITE != 0 || section.Size == 0 || section.Size > maxControlFrame {
		return errGuardProtocol
	}
	raw, err := section.Data()
	if err != nil || uint64(len(raw)) != section.Size || hashBytes(raw) != manifest.ObserverBindingsSHA256 {
		return errGuardProtocol
	}
	binding, decoded, err := validateObserverBindingMetadata(raw, manifest)
	if err != nil {
		return err
	}
	if err = validateRunningBuildID(decoded); err != nil {
		return err
	}
	btf, err := unix.Open("/sys/kernel/btf/vmlinux", unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(btf), "kernel-btf")
	defer file.Close()
	var before, after, named unix.Stat_t
	var filesystem unix.Statfs_t
	if unix.Fstat(btf, &before) != nil || unix.Fstatfs(btf, &filesystem) != nil || int64(filesystem.Type) != unix.SYSFS_MAGIC || before.Mode&unix.S_IFMT != unix.S_IFREG || before.Uid != 0 || before.Gid != 0 || before.Mode&0022 != 0 {
		return errGuardProtocol
	}
	body, err := io.ReadAll(io.LimitReader(file, 64<<20+1))
	if err != nil || len(body) < 24 || len(body) > 64<<20 || hashBytes(body) != binding["kernel_btf_sha256"] || unix.Fstat(btf, &after) != nil || unix.Lstat("/sys/kernel/btf/vmlinux", &named) != nil || !sameTrustedStat(before, after) || !sameTrustedStat(before, named) {
		return errGuardProtocol
	}
	return nil
}

// validateObserverBindingMetadata validates the canonical module binding without
// consulting the running kernel. The caller still must validate that kernel.
func validateObserverBindingMetadata(raw []byte, manifest bootstrapManifest) (map[string]any, []byte, error) {
	binding, err := parseCanonicalControl(raw)
	if err != nil || !exactKeys(binding, "version", "kernel_release", "kernel_sha256", "system_map_sha256", "config_sha256", "kernel_btf_sha256", "gnu_build_id", "headers_manifest_sha256", "symvers_sha256", "anchor", "core", "symbols", "calls", "layout", "allocator_windows") || binding["version"] != json.Number("1") || binding["kernel_release"] != "6.8.0-147-generic" || binding["kernel_sha256"] != manifest.KernelSHA256 {
		return nil, nil, errGuardProtocol
	}
	for _, key := range []string{"kernel_sha256", "system_map_sha256", "config_sha256", "kernel_btf_sha256", "headers_manifest_sha256", "symvers_sha256"} {
		if _, ok := digestString(binding[key]); !ok {
			return nil, nil, errGuardProtocol
		}
	}
	layout, ok := binding["layout"].(map[string]any)
	if !ok || !exactKeys(layout,
		"idr_size", "idr_base_offset", "idr_next_offset", "spinlock_size", "audit_buffer_size", "audit_buffer_ctx_offset", "audit_buffer_stamp_offset", "audit_stamp_size", "stamp_seconds_offset", "stamp_nanoseconds_offset", "stamp_serial_offset", "stamp_serial_size", "audit_context_size", "context_class_offset", "context_stamp_offset",
		"context_major_offset", "context_major_size", "context_argv_offset", "context_argv_size", "context_return_code_offset", "context_return_code_size", "context_return_valid_offset", "context_return_valid_size", "context_arch_offset", "context_arch_size",
		"context_unused_value", "context_syscall_value", "context_uring_value",
		"bpf_prog_struct_size", "bpf_prog_aux_struct_size", "user_struct_struct_size", "bpf_map_struct_size", "btf_struct_size", "kuid_struct_size",
		"bpf_prog_len_offset", "bpf_prog_len_size", "bpf_prog_aux_load_time_offset", "bpf_prog_aux_load_time_size", "bpf_prog_aux_verified_insns_offset", "bpf_prog_aux_verified_insns_size", "bpf_prog_aux_attach_btf_id_offset", "bpf_prog_aux_attach_btf_id_size", "kuid_val_offset", "kuid_val_size",
		"bpf_map_key_size_offset", "bpf_map_key_size_size", "bpf_map_value_size_offset", "bpf_map_value_size_size", "bpf_map_max_entries_offset", "bpf_map_max_entries_size", "bpf_map_map_flags_offset", "bpf_map_map_flags_size", "bpf_map_map_extra_offset", "bpf_map_map_extra_size", "bpf_map_btf_key_type_id_offset", "bpf_map_btf_key_type_id_size", "bpf_map_btf_value_type_id_offset", "bpf_map_btf_value_type_id_size", "btf_id_offset", "btf_id_size",
		"bpf_prog_aux_offload_requested_offset", "bpf_prog_aux_offload_requested_size",
		"bpf_prog_type_offset", "bpf_prog_type_size", "bpf_map_map_type_offset", "bpf_map_map_type_size", "bpf_prog_aux_offset", "bpf_prog_aux_size", "bpf_prog_aux_user_offset", "bpf_prog_aux_user_size", "bpf_prog_aux_offload_offset", "bpf_prog_aux_offload_size", "bpf_map_ops_offset", "bpf_map_ops_size", "bpf_map_btf_offset", "bpf_map_btf_size", "user_struct_uid_offset", "user_struct_uid_size", "bpf_prog_tag_offset", "bpf_prog_tag_size", "bpf_prog_aux_name_offset", "bpf_prog_aux_name_size", "bpf_map_name_offset", "bpf_map_name_size") {
		return nil, nil, errGuardProtocol
	}
	for _, value := range layout {
		n, valid := exactNumber(value)
		if !valid || n > 0xffffffff {
			return nil, nil, errGuardProtocol
		}
	}
	for key, size := range map[string]uint64{
		"idr_size": 24, "idr_base_offset": 16, "idr_next_offset": 20, "spinlock_size": 4,
		"context_major_size": 4, "context_argv_size": 32, "context_return_code_size": 8, "context_return_valid_size": 4, "context_arch_size": 4,
		"kuid_struct_size": 4, "kuid_val_size": 4, "user_struct_uid_size": 4,
		"bpf_prog_len_size": 4, "bpf_prog_type_size": 4, "bpf_prog_tag_size": 8,
		"bpf_prog_aux_offload_requested_size": 1,
		"bpf_prog_aux_size":                   8, "bpf_prog_aux_load_time_size": 8, "bpf_prog_aux_verified_insns_size": 4, "bpf_prog_aux_attach_btf_id_size": 4, "bpf_prog_aux_user_size": 8, "bpf_prog_aux_offload_size": 8, "bpf_prog_aux_name_size": 16,
		"bpf_map_map_type_size": 4, "bpf_map_key_size_size": 4, "bpf_map_value_size_size": 4, "bpf_map_max_entries_size": 4, "bpf_map_map_flags_size": 4, "bpf_map_map_extra_size": 8, "bpf_map_btf_key_type_id_size": 4, "bpf_map_btf_value_type_id_size": 4, "bpf_map_ops_size": 8, "bpf_map_btf_size": 8, "bpf_map_name_size": 16, "btf_id_size": 4,
	} {
		n, _ := exactNumber(layout[key])
		if n != size {
			return nil, nil, errGuardProtocol
		}
	}
	anchor, ok := binding["anchor"].(map[string]any)
	if !ok || !exactKeys(anchor, "name", "type") || anchor["name"] != "idr_alloc_cyclic" || anchor["type"] != "T" {
		return nil, nil, errGuardProtocol
	}
	if !validateAllocatorWindows(binding) {
		return nil, nil, errGuardProtocol
	}
	buildID, ok := binding["gnu_build_id"].(string)
	if !ok || len(buildID) != 40 {
		return nil, nil, errGuardProtocol
	}
	decoded, err := hex.DecodeString(buildID)
	if err != nil || hex.EncodeToString(decoded) != buildID {
		return nil, nil, errGuardProtocol
	}
	return binding, decoded, nil
}

// validateAllocatorWindows binds each pinned instruction window to its unique
// named symbol; the native observer independently validates the kernel image.
func validateAllocatorWindows(binding map[string]any) bool {
	type windowPin struct {
		name, kind, bytes string
		offset            int64
		relocations       [3]uint64
	}
	pins := [...]windowPin{
		{"map_create", "t", "48c7c714b00984e82b28f50041b820080000b9ffffff7f4c89feba0100000048c7c760008183e8cc55e9004189c485c07e044189473448c7c714b00984e86529f50065ff0d", 0x319, [3]uint64{3, 34, 57}},
		{"bpf_map_free_id", "T", "48c7c714b00984e83334f5008b733448c7c7600081834989c4e8616ae900c74334000000004c89e648c7c714b00984e8ab34f5005b415c5d31", 0x21, [3]uint64{3, 18, 43}},
	}
	symbols, ok := binding["symbols"].([]any)
	if !ok {
		return false
	}
	windows, ok := binding["allocator_windows"].([]any)
	if !ok || len(windows) != len(pins) {
		return false
	}
	for i, pin := range pins {
		var ownerDelta int64
		found := false
		for _, item := range symbols {
			symbol, ok := item.(map[string]any)
			if !ok || symbol["name"] != pin.name {
				continue
			}
			if found || !exactKeys(symbol, "name", "type", "delta") || symbol["type"] != pin.kind {
				return false
			}
			number, ok := symbol["delta"].(json.Number)
			if !ok {
				return false
			}
			var err error
			ownerDelta, err = number.Int64()
			if err != nil || ownerDelta > math.MaxInt64-pin.offset {
				return false
			}
			found = true
		}
		window, ok := windows[i].(map[string]any)
		if !found || !ok || !exactKeys(window, "delta", "bytes", "relocations") || window["bytes"] != pin.bytes {
			return false
		}
		number, ok := window["delta"].(json.Number)
		if !ok {
			return false
		}
		delta, err := number.Int64()
		if err != nil || delta != ownerDelta+pin.offset {
			return false
		}
		relocations, ok := window["relocations"].([]any)
		if !ok || len(relocations) != len(pin.relocations) {
			return false
		}
		for j, relocation := range relocations {
			offset, valid := exactNumber(relocation)
			if !valid || offset != pin.relocations[j] || offset+4 > uint64(len(pin.bytes)/2) {
				return false
			}
		}
	}
	return true
}

func validateRunningBuildID(expected []byte) error {
	fd, err := unix.Open("/sys/kernel/notes", unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), "kernel-notes")
	defer file.Close()
	var before, after, named unix.Stat_t
	var filesystem unix.Statfs_t
	if unix.Fstat(fd, &before) != nil || unix.Fstatfs(fd, &filesystem) != nil || int64(filesystem.Type) != unix.SYSFS_MAGIC || before.Mode&unix.S_IFMT != unix.S_IFREG || before.Uid != 0 || before.Gid != 0 || before.Mode&0022 != 0 {
		return errGuardProtocol
	}
	raw, err := io.ReadAll(io.LimitReader(file, 1<<20+1))
	if err != nil || len(raw) == 0 || len(raw) > 1<<20 {
		return errGuardProtocol
	}
	count := 0
	for cursor := 0; cursor < len(raw); {
		if len(raw)-cursor < 12 {
			return errGuardProtocol
		}
		names := uint64(binary.LittleEndian.Uint32(raw[cursor:]))
		desc := uint64(binary.LittleEndian.Uint32(raw[cursor+4:]))
		kind := binary.LittleEndian.Uint32(raw[cursor+8:])
		start := uint64(cursor + 12)
		data := start + (names+3)&^uint64(3)
		end := data + (desc+3)&^uint64(3)
		if end > uint64(len(raw)) || start+names > data || data+desc > end {
			return errGuardProtocol
		}
		if bytes.Equal(raw[start:start+names], []byte("GNU\x00")) && kind == 3 {
			count++
			if !bytes.Equal(raw[data:data+desc], expected) {
				return errGuardProtocol
			}
		}
		cursor = int(end)
	}
	if count != 1 || unix.Fstat(fd, &after) != nil || unix.Lstat("/sys/kernel/notes", &named) != nil || !sameTrustedStat(before, after) || !sameTrustedStat(before, named) {
		return errGuardProtocol
	}
	return nil
}
