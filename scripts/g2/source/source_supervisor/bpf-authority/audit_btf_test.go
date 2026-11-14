//go:build linux && amd64 && !cgo

package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
)

// Canonical observer-bindings.json from source build attempt 109. The fixed
// expectations below are independent of the validator's pin table.
const observerBindingAttempt109 = `{"allocator_windows":[{"bytes":"48c7c714b00984e82b28f50041b820080000b9ffffff7f4c89feba0100000048c7c760008183e8cc55e9004189c485c07e044189473448c7c714b00984e86529f50065ff0d","delta":-15291895,"relocations":[3,34,57]},{"bytes":"48c7c714b00984e83334f5008b733448c7c7600081834989c4e8616ae900c74334000000004c89e648c7c714b00984e8ab34f5005b415c5d31","delta":-15296943,"relocations":[3,18,43]}],"anchor":{"name":"idr_alloc_cyclic","type":"T"},"calls":[{"bytes":"e8408af4ff","call_delta":-15314357,"callee":"audit_log_start","owner":"bpf_audit_prog.part.0","return_delta":-15314352},{"bytes":"e80183f4ff","call_delta":-15314310,"callee":"audit_log_format","owner":"bpf_audit_prog.part.0","return_delta":-15314305},{"bytes":"e8c980f4ff","call_delta":-15314302,"callee":"audit_log_end","owner":"bpf_audit_prog.part.0","return_delta":-15314297},{"bytes":"e807eaffff","call_delta":-15308796,"callee":"bpf_audit_prog.part.0","owner":"bpf_prog_put_deferred","return_delta":-15308791},{"bytes":"e8cc55e900","call_delta":-15291857,"callee":"idr_alloc_cyclic","owner":"map_create","return_delta":-15291852},{"bytes":"e8a74ce900","call_delta":-15289516,"callee":"idr_alloc_cyclic","owner":"bpf_prog_load","return_delta":-15289511},{"bytes":"e83c9effff","call_delta":-15289393,"callee":"bpf_audit_prog.part.0","owner":"bpf_prog_load","return_delta":-15289388}],"config_sha256":"cf8454bc2e67effab87a5bff3c39042f2d8d106b6a777c4302e52f50d23b7400","core":{"bss_end_delta":35963504,"bss_start_delta":31076976,"data_end_delta":25831920,"data_start_delta":21283440,"text_end_delta":4506224,"text_start_delta":-18562448},"gnu_build_id":"00a89b4c4179276e34c5b676e5e455747b3deab3","headers_manifest_sha256":"e3a255c540801b0b70de6705b6a34a02ae9bdde9a9348cb047217b019a0fe14f","kernel_btf_sha256":"8c10e7e174e89c92f1d2708f12bb5268dc381e57e867397534c4e145348622e0","kernel_release":"6.8.0-147-generic","kernel_sha256":"41ba36d860aa0b0cd995f4ca8856d80249b244577e704929b55be13fbd1ebd2f","layout":{"audit_buffer_ctx_offset":32,"audit_buffer_size":72,"audit_buffer_stamp_offset":40,"audit_context_size":1200,"audit_stamp_size":24,"bpf_map_btf_key_type_id_offset":68,"bpf_map_btf_key_type_id_size":4,"bpf_map_btf_offset":80,"bpf_map_btf_size":8,"bpf_map_btf_value_type_id_offset":72,"bpf_map_btf_value_type_id_size":4,"bpf_map_key_size_offset":28,"bpf_map_key_size_size":4,"bpf_map_map_extra_offset":40,"bpf_map_map_extra_size":8,"bpf_map_map_flags_offset":48,"bpf_map_map_flags_size":4,"bpf_map_map_type_offset":24,"bpf_map_map_type_size":4,"bpf_map_max_entries_offset":36,"bpf_map_max_entries_size":4,"bpf_map_name_offset":96,"bpf_map_name_size":16,"bpf_map_ops_offset":0,"bpf_map_ops_size":8,"bpf_map_struct_size":320,"bpf_map_value_size_offset":32,"bpf_map_value_size_size":4,"bpf_prog_aux_attach_btf_id_offset":48,"bpf_prog_aux_attach_btf_id_size":4,"bpf_prog_aux_load_time_offset":928,"bpf_prog_aux_load_time_size":8,"bpf_prog_aux_name_offset":960,"bpf_prog_aux_name_size":16,"bpf_prog_aux_offload_offset":992,"bpf_prog_aux_offload_requested_offset":138,"bpf_prog_aux_offload_requested_size":1,"bpf_prog_aux_offload_size":8,"bpf_prog_aux_offset":56,"bpf_prog_aux_size":8,"bpf_prog_aux_struct_size":1112,"bpf_prog_aux_user_offset":920,"bpf_prog_aux_user_size":8,"bpf_prog_aux_verified_insns_offset":936,"bpf_prog_aux_verified_insns_size":4,"bpf_prog_len_offset":12,"bpf_prog_len_size":4,"bpf_prog_struct_size":72,"bpf_prog_tag_offset":20,"bpf_prog_tag_size":8,"bpf_prog_type_offset":4,"bpf_prog_type_size":4,"btf_id_offset":88,"btf_id_size":4,"btf_struct_size":216,"context_arch_offset":952,"context_arch_size":4,"context_argv_offset":48,"context_argv_size":32,"context_class_offset":4,"context_major_offset":40,"context_major_size":4,"context_return_code_offset":80,"context_return_code_size":8,"context_return_valid_offset":96,"context_return_valid_size":4,"context_stamp_offset":16,"context_syscall_value":1,"context_unused_value":0,"context_uring_value":2,"idr_base_offset":16,"idr_next_offset":20,"idr_size":24,"kuid_struct_size":4,"kuid_val_offset":0,"kuid_val_size":4,"spinlock_size":4,"stamp_nanoseconds_offset":8,"stamp_seconds_offset":0,"stamp_serial_offset":16,"stamp_serial_size":4,"user_struct_struct_size":144,"user_struct_uid_offset":80,"user_struct_uid_size":4},"symbols":[{"delta":-15276512,"name":"__x64_sys_bpf","type":"T"},{"delta":774720,"name":"_raw_spin_lock_bh","type":"T"},{"delta":772752,"name":"_raw_spin_lock_irqsave","type":"T"},{"delta":775088,"name":"_raw_spin_unlock_bh","type":"T"},{"delta":772912,"name":"_raw_spin_unlock_irqrestore","type":"T"},{"delta":-16067760,"name":"audit_log_end","type":"T"},{"delta":-16067200,"name":"audit_log_format","type":"T"},{"delta":-16065392,"name":"audit_log_start","type":"T"},{"delta":-15314416,"name":"bpf_audit_prog.part.0","type":"t"},{"delta":-15296976,"name":"bpf_map_free_id","type":"T"},{"delta":4744752,"name":"bpf_map_offload_ops","type":"D"},{"delta":-15284224,"name":"bpf_prog_free_id","type":"T"},{"delta":-15291344,"name":"bpf_prog_load","type":"t"},{"delta":-15308848,"name":"bpf_prog_put_deferred","type":"t"},{"delta":-14817616,"name":"cgroup_bpf_offline","type":"T"},{"delta":-14824576,"name":"cgroup_bpf_release","type":"t"},{"delta":208,"name":"idr_remove","type":"T"},{"delta":-15292688,"name":"map_create","type":"t"},{"delta":23446224,"name":"map_idr","type":"d"},{"delta":32404100,"name":"map_idr_lock","type":"b"},{"delta":23446256,"name":"prog_idr","type":"d"}],"symvers_sha256":"8e7eb5798789b9610c324b3d9b13373d88c30ae17bb9e41e622efd05e0277a87","system_map_sha256":"2cbb8be5b2501a60c5b2e29371bf18a5eb4b468bb4485206a45bf178b1191a11","version":1}`

const observerKernelAttempt109 = "41ba36d860aa0b0cd995f4ca8856d80249b244577e704929b55be13fbd1ebd2f"

func TestObserverBindingMetadata(t *testing.T) {
	manifest := bootstrapManifest{KernelSHA256: observerKernelAttempt109}
	if got := hashBytes([]byte(observerBindingAttempt109)); got != "2103717c45b725607f300678be8d479cc9a89d7456b875437cb860edfaf58179" {
		t.Fatalf("fixture hash = %s", got)
	}
	binding, buildID, err := validateObserverBindingMetadata([]byte(observerBindingAttempt109), manifest)
	if err != nil || binding == nil || hex.EncodeToString(buildID) != "00a89b4c4179276e34c5b676e5e455747b3deab3" {
		t.Fatalf("authentic metadata: binding=%v buildID=%x err=%v", binding != nil, buildID, err)
	}

	type mutation struct {
		name  string
		apply func(map[string]any)
	}
	layout := func(m map[string]any) map[string]any { return m["layout"].(map[string]any) }
	window := func(m map[string]any, i int) map[string]any {
		return m["allocator_windows"].([]any)[i].(map[string]any)
	}
	owner := func(m map[string]any, name string) map[string]any {
		for _, entry := range m["symbols"].([]any) {
			s := entry.(map[string]any)
			if s["name"] == name {
				return s
			}
		}
		panic("fixture owner missing")
	}
	cases := []mutation{
		{"missing top", func(m map[string]any) { delete(m, "allocator_windows") }},
		{"extra top", func(m map[string]any) { m["extra"] = true }},
		{"extra layout", func(m map[string]any) { layout(m)["extra"] = json.Number("1") }},
		{"extra window", func(m map[string]any) { window(m, 1)["extra"] = true }},
		{"one window", func(m map[string]any) { m["allocator_windows"] = m["allocator_windows"].([]any)[:1] }},
		{"swapped windows", func(m map[string]any) { w := m["allocator_windows"].([]any); w[0], w[1] = w[1], w[0] }},
		{"window bytes", func(m map[string]any) { window(m, 0)["bytes"] = "00" }},
		{"window uppercase", func(m map[string]any) {
			window(m, 1)["bytes"] = string(bytes.ToUpper([]byte(window(m, 1)["bytes"].(string))))
		}},
		{"window delta", func(m map[string]any) { window(m, 0)["delta"] = json.Number("-15291894") }},
		{"window delta bool", func(m map[string]any) { window(m, 0)["delta"] = true }},
		{"window delta fraction", func(m map[string]any) { window(m, 0)["delta"] = json.Number("1.5") }},
		{"window delta overflow", func(m map[string]any) { window(m, 0)["delta"] = json.Number("9223372036854775808") }},
		{"window delta negative zero", func(m map[string]any) {
			owner(m, "map_create")["delta"] = json.Number("-793")
			window(m, 0)["delta"] = json.Number("-0")
		}},
		{"owner delta negative zero", func(m map[string]any) {
			owner(m, "map_create")["delta"] = json.Number("-0")
			window(m, 0)["delta"] = json.Number("793")
		}},
		{"relocation changed", func(m map[string]any) { window(m, 0)["relocations"].([]any)[1] = json.Number("35") }},
		{"relocation out of bounds", func(m map[string]any) { window(m, 1)["relocations"].([]any)[2] = json.Number("1000") }},
		{"relocation negative", func(m map[string]any) { window(m, 1)["relocations"].([]any)[0] = json.Number("-1") }},
		{"relocation bool", func(m map[string]any) { window(m, 1)["relocations"].([]any)[0] = true }},
		{"owner delta fraction", func(m map[string]any) { owner(m, "map_create")["delta"] = json.Number("-15292688.5") }},
		{"owner delta outside int64", func(m map[string]any) { owner(m, "map_create")["delta"] = json.Number("-9223372036854775809") }},
		{"relocation fraction", func(m map[string]any) { window(m, 1)["relocations"].([]any)[0] = json.Number("3.5") }},
		{"relocation overflow", func(m map[string]any) { window(m, 1)["relocations"].([]any)[0] = json.Number("18446744073709551616") }},
		{"owner wrong type", func(m map[string]any) { owner(m, "map_create")["type"] = "T" }},
		{"free owner wrong type", func(m map[string]any) { owner(m, "bpf_map_free_id")["type"] = "t" }},
		{"owner extra key", func(m map[string]any) { owner(m, "map_create")["extra"] = true }},
		{"owner delta missing", func(m map[string]any) { delete(owner(m, "map_create"), "delta") }},
		{"owner delta changed", func(m map[string]any) { owner(m, "map_create")["delta"] = json.Number("-15292687") }},
		{"owner delta bool", func(m map[string]any) { owner(m, "map_create")["delta"] = true }},
		{"owner delta overflow addition", func(m map[string]any) { owner(m, "map_create")["delta"] = json.Number("9223372036854775807") }},
	}
	for _, key := range []string{"idr_size", "idr_base_offset", "idr_next_offset", "spinlock_size"} {
		key := key
		cases = append(cases, mutation{"missing layout " + key, func(m map[string]any) { delete(layout(m), key) }})
	}
	for _, index := range []int{0, 1} {
		for _, key := range []string{"delta", "bytes", "relocations"} {
			index, key := index, key
			cases = append(cases, mutation{fmt.Sprintf("missing window %d %s", index, key), func(m map[string]any) { delete(window(m, index), key) }})
		}
	}
	for _, name := range []string{"map_create", "bpf_map_free_id"} {
		name := name
		cases = append(cases,
			mutation{"missing owner " + name, func(m map[string]any) { owner(m, name)["name"] = "other" }},
			mutation{"duplicate owner " + name, func(m map[string]any) { m["symbols"] = append(m["symbols"].([]any), owner(m, name)) }},
		)
	}
	for _, key := range []string{"idr_size", "idr_base_offset", "idr_next_offset", "spinlock_size"} {
		for _, v := range []struct {
			name  string
			value any
		}{
			{"wrong", json.Number("123")}, {"bool", true}, {"negative", json.Number("-1")},
			{"fraction", json.Number("1.5")}, {"overflow", json.Number("4294967296")},
		} {
			key, v := key, v
			cases = append(cases, mutation{"layout " + key + " " + v.name, func(m map[string]any) { layout(m)[key] = v.value }})
		}
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decoder := json.NewDecoder(bytes.NewBufferString(observerBindingAttempt109))
			decoder.UseNumber()
			var value map[string]any
			if err := decoder.Decode(&value); err != nil {
				t.Fatal(err)
			}
			tc.apply(value)
			raw, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := validateObserverBindingMetadata(raw, manifest); err == nil {
				t.Fatal("forged binding accepted")
			}
		})
	}
	t.Run("wrong manifest kernel", func(t *testing.T) {
		if _, _, err := validateObserverBindingMetadata([]byte(observerBindingAttempt109), bootstrapManifest{KernelSHA256: "wrong"}); err == nil {
			t.Fatal("wrong kernel accepted")
		}
	})
}
