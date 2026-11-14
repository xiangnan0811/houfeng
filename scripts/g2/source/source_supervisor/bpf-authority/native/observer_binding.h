/* SPDX-License-Identifier: GPL-2.0-only */
#ifndef G2_BPF_GUARD_OBSERVER_BINDING_H
#define G2_BPF_GUARD_OBSERVER_BINDING_H

#include <linux/errno.h>
#include <linux/idr.h>
#include <linux/mm.h>
#include <linux/overflow.h>
#include <linux/string.h>
#include "observer_bindings.generated.h"
#include <linux/bpf.h>
#include <linux/filter.h>
#include <linux/sched/user.h>
#include <linux/uidgid.h>

_Static_assert(sizeof(struct bpf_prog) == G2_OBS_LAYOUT_BPF_PROG_STRUCT_SIZE, "bpf_prog size differs");
_Static_assert(offsetof(struct bpf_prog, type) == G2_OBS_LAYOUT_BPF_PROG_TYPE_OFFSET, "bpf_prog.type offset differs");
_Static_assert(sizeof(((struct bpf_prog *)0)->type) == G2_OBS_LAYOUT_BPF_PROG_TYPE_SIZE, "bpf_prog.type size differs");
_Static_assert(offsetof(struct bpf_prog, len) == G2_OBS_LAYOUT_BPF_PROG_LEN_OFFSET, "bpf_prog.len offset differs");
_Static_assert(sizeof(((struct bpf_prog *)0)->len) == G2_OBS_LAYOUT_BPF_PROG_LEN_SIZE, "bpf_prog.len size differs");
_Static_assert(offsetof(struct bpf_prog, tag) == G2_OBS_LAYOUT_BPF_PROG_TAG_OFFSET, "bpf_prog.tag offset differs");
_Static_assert(sizeof(((struct bpf_prog *)0)->tag) == G2_OBS_LAYOUT_BPF_PROG_TAG_SIZE, "bpf_prog.tag size differs");
_Static_assert(offsetof(struct bpf_prog, aux) == G2_OBS_LAYOUT_BPF_PROG_AUX_OFFSET, "bpf_prog.aux offset differs");
_Static_assert(sizeof(((struct bpf_prog *)0)->aux) == G2_OBS_LAYOUT_BPF_PROG_AUX_SIZE, "bpf_prog.aux size differs");
_Static_assert(sizeof(struct bpf_prog_aux) == G2_OBS_LAYOUT_BPF_PROG_AUX_STRUCT_SIZE, "bpf_prog_aux size differs");
_Static_assert(offsetof(struct bpf_prog_aux, name) == G2_OBS_LAYOUT_BPF_PROG_AUX_NAME_OFFSET, "bpf_prog_aux.name offset differs");
_Static_assert(sizeof(((struct bpf_prog_aux *)0)->name) == G2_OBS_LAYOUT_BPF_PROG_AUX_NAME_SIZE, "bpf_prog_aux.name size differs");
_Static_assert(offsetof(struct bpf_prog_aux, load_time) == G2_OBS_LAYOUT_BPF_PROG_AUX_LOAD_TIME_OFFSET, "bpf_prog_aux.load_time offset differs");
_Static_assert(sizeof(((struct bpf_prog_aux *)0)->load_time) == G2_OBS_LAYOUT_BPF_PROG_AUX_LOAD_TIME_SIZE, "bpf_prog_aux.load_time size differs");
_Static_assert(offsetof(struct bpf_prog_aux, user) == G2_OBS_LAYOUT_BPF_PROG_AUX_USER_OFFSET, "bpf_prog_aux.user offset differs");
_Static_assert(sizeof(((struct bpf_prog_aux *)0)->user) == G2_OBS_LAYOUT_BPF_PROG_AUX_USER_SIZE, "bpf_prog_aux.user size differs");
_Static_assert(offsetof(struct bpf_prog_aux, verified_insns) == G2_OBS_LAYOUT_BPF_PROG_AUX_VERIFIED_INSNS_OFFSET, "bpf_prog_aux.verified_insns offset differs");
_Static_assert(sizeof(((struct bpf_prog_aux *)0)->verified_insns) == G2_OBS_LAYOUT_BPF_PROG_AUX_VERIFIED_INSNS_SIZE, "bpf_prog_aux.verified_insns size differs");
_Static_assert(offsetof(struct bpf_prog_aux, attach_btf_id) == G2_OBS_LAYOUT_BPF_PROG_AUX_ATTACH_BTF_ID_OFFSET, "bpf_prog_aux.attach_btf_id offset differs");
_Static_assert(sizeof(((struct bpf_prog_aux *)0)->attach_btf_id) == G2_OBS_LAYOUT_BPF_PROG_AUX_ATTACH_BTF_ID_SIZE, "bpf_prog_aux.attach_btf_id size differs");
_Static_assert(offsetof(struct bpf_prog_aux, offload) == G2_OBS_LAYOUT_BPF_PROG_AUX_OFFLOAD_OFFSET, "bpf_prog_aux.offload offset differs");
_Static_assert(sizeof(((struct bpf_prog_aux *)0)->offload) == G2_OBS_LAYOUT_BPF_PROG_AUX_OFFLOAD_SIZE, "bpf_prog_aux.offload size differs");
_Static_assert(offsetof(struct bpf_prog_aux, offload_requested) == G2_OBS_LAYOUT_BPF_PROG_AUX_OFFLOAD_REQUESTED_OFFSET, "offload request offset differs");
_Static_assert(sizeof(((struct bpf_prog_aux *)0)->offload_requested) == G2_OBS_LAYOUT_BPF_PROG_AUX_OFFLOAD_REQUESTED_SIZE, "offload request width differs");
_Static_assert(sizeof(struct user_struct) == G2_OBS_LAYOUT_USER_STRUCT_STRUCT_SIZE, "user_struct size differs");
_Static_assert(offsetof(struct user_struct, uid) == G2_OBS_LAYOUT_USER_STRUCT_UID_OFFSET, "user_struct.uid offset differs");
_Static_assert(sizeof(((struct user_struct *)0)->uid) == G2_OBS_LAYOUT_USER_STRUCT_UID_SIZE, "user_struct.uid size differs");
_Static_assert(sizeof(struct bpf_map) == G2_OBS_LAYOUT_BPF_MAP_STRUCT_SIZE, "bpf_map size differs");
_Static_assert(offsetof(struct bpf_map, ops) == G2_OBS_LAYOUT_BPF_MAP_OPS_OFFSET, "bpf_map.ops offset differs");
_Static_assert(sizeof(((struct bpf_map *)0)->ops) == G2_OBS_LAYOUT_BPF_MAP_OPS_SIZE, "bpf_map.ops size differs");
_Static_assert(offsetof(struct bpf_map, map_type) == G2_OBS_LAYOUT_BPF_MAP_MAP_TYPE_OFFSET, "bpf_map.map_type offset differs");
_Static_assert(sizeof(((struct bpf_map *)0)->map_type) == G2_OBS_LAYOUT_BPF_MAP_MAP_TYPE_SIZE, "bpf_map.map_type size differs");
_Static_assert(offsetof(struct bpf_map, name) == G2_OBS_LAYOUT_BPF_MAP_NAME_OFFSET, "bpf_map.name offset differs");
_Static_assert(sizeof(((struct bpf_map *)0)->name) == G2_OBS_LAYOUT_BPF_MAP_NAME_SIZE, "bpf_map.name size differs");
_Static_assert(offsetof(struct bpf_map, key_size) == G2_OBS_LAYOUT_BPF_MAP_KEY_SIZE_OFFSET, "bpf_map.key_size offset differs");
_Static_assert(sizeof(((struct bpf_map *)0)->key_size) == G2_OBS_LAYOUT_BPF_MAP_KEY_SIZE_SIZE, "bpf_map.key_size size differs");
_Static_assert(offsetof(struct bpf_map, value_size) == G2_OBS_LAYOUT_BPF_MAP_VALUE_SIZE_OFFSET, "bpf_map.value_size offset differs");
_Static_assert(sizeof(((struct bpf_map *)0)->value_size) == G2_OBS_LAYOUT_BPF_MAP_VALUE_SIZE_SIZE, "bpf_map.value_size size differs");
_Static_assert(offsetof(struct bpf_map, max_entries) == G2_OBS_LAYOUT_BPF_MAP_MAX_ENTRIES_OFFSET, "bpf_map.max_entries offset differs");
_Static_assert(sizeof(((struct bpf_map *)0)->max_entries) == G2_OBS_LAYOUT_BPF_MAP_MAX_ENTRIES_SIZE, "bpf_map.max_entries size differs");
_Static_assert(offsetof(struct bpf_map, map_flags) == G2_OBS_LAYOUT_BPF_MAP_MAP_FLAGS_OFFSET, "bpf_map.map_flags offset differs");
_Static_assert(sizeof(((struct bpf_map *)0)->map_flags) == G2_OBS_LAYOUT_BPF_MAP_MAP_FLAGS_SIZE, "bpf_map.map_flags size differs");
_Static_assert(offsetof(struct bpf_map, map_extra) == G2_OBS_LAYOUT_BPF_MAP_MAP_EXTRA_OFFSET, "bpf_map.map_extra offset differs");
_Static_assert(sizeof(((struct bpf_map *)0)->map_extra) == G2_OBS_LAYOUT_BPF_MAP_MAP_EXTRA_SIZE, "bpf_map.map_extra size differs");
_Static_assert(offsetof(struct bpf_map, btf) == G2_OBS_LAYOUT_BPF_MAP_BTF_OFFSET, "bpf_map.btf offset differs");
_Static_assert(sizeof(((struct bpf_map *)0)->btf) == G2_OBS_LAYOUT_BPF_MAP_BTF_SIZE, "bpf_map.btf size differs");
_Static_assert(offsetof(struct bpf_map, btf_key_type_id) == G2_OBS_LAYOUT_BPF_MAP_BTF_KEY_TYPE_ID_OFFSET, "bpf_map.btf_key_type_id offset differs");
_Static_assert(sizeof(((struct bpf_map *)0)->btf_key_type_id) == G2_OBS_LAYOUT_BPF_MAP_BTF_KEY_TYPE_ID_SIZE, "bpf_map.btf_key_type_id size differs");
_Static_assert(offsetof(struct bpf_map, btf_value_type_id) == G2_OBS_LAYOUT_BPF_MAP_BTF_VALUE_TYPE_ID_OFFSET, "bpf_map.btf_value_type_id offset differs");
_Static_assert(sizeof(((struct bpf_map *)0)->btf_value_type_id) == G2_OBS_LAYOUT_BPF_MAP_BTF_VALUE_TYPE_ID_SIZE, "bpf_map.btf_value_type_id size differs");
_Static_assert(sizeof(kuid_t) == G2_OBS_LAYOUT_KUID_STRUCT_SIZE, "kuid size differs");
_Static_assert(offsetof(kuid_t, val) == G2_OBS_LAYOUT_KUID_VAL_OFFSET, "kuid value differs");
_Static_assert(sizeof(((kuid_t *)0)->val) == G2_OBS_LAYOUT_KUID_VAL_SIZE, "kuid value width differs");


_Static_assert(sizeof(struct idr) == G2_OBS_LAYOUT_IDR_SIZE, "IDR layout differs");
_Static_assert(offsetof(struct idr, idr_base) == G2_OBS_LAYOUT_IDR_BASE_OFFSET, "IDR base differs");
_Static_assert(offsetof(struct idr, idr_next) == G2_OBS_LAYOUT_IDR_NEXT_OFFSET, "IDR cursor differs");
_Static_assert(sizeof(((struct idr *)0)->idr_base) == 4 && sizeof(((struct idr *)0)->idr_next) == 4, "IDR scalar width differs");
_Static_assert(sizeof(spinlock_t) == G2_OBS_LAYOUT_SPINLOCK_SIZE && __alignof__(spinlock_t) == 4, "map IDR lock layout differs");

struct g2_observer_addresses {
	unsigned long text_start, text_end, data_start, data_end, bss_start, bss_end;
	unsigned long prog_idr, map_idr, map_idr_lock, prog_free, map_free;
	unsigned long map_offload_ops;
	unsigned long cgroup_offline, cgroup_release;
	unsigned long split, audit_start, sys_bpf;
	unsigned long prog_alloc_return, map_alloc_return, audit_start_return;
	unsigned long load_audit_return, unload_audit_return;
};

static int g2_observer_add(unsigned long base, long long delta, unsigned long *out)
{
	unsigned long magnitude;

	if (delta < 0) {
		magnitude = (unsigned long)(-(delta + 1)) + 1;
		if (check_sub_overflow(base, magnitude, out))
			return -EINVAL;
	} else if (check_add_overflow(base, (unsigned long)delta, out)) {
		return -EINVAL;
	}
	return *out >= 0xffff800000000000UL ? 0 : -EINVAL;
}

static bool g2_observer_range(unsigned long start, unsigned long end,
		unsigned long address, size_t size)
{
	return start < end && address >= start && address < end && size <= end - address;
}

static int g2_observer_binding_init(struct g2_observer_addresses *a)
{
	static const long long text_deltas[] = {
		G2_OBS_BPF_MAP_FREE_ID_DELTA, G2_OBS_BPF_PROG_FREE_ID_DELTA,
		G2_OBS_BPF_PROG_LOAD_DELTA, G2_OBS_BPF_PROG_PUT_DEFERRED_DELTA,
		G2_OBS_CGROUP_BPF_OFFLINE_DELTA, G2_OBS_CGROUP_BPF_RELEASE_DELTA,
		G2_OBS_BPF_AUDIT_PROG_PART_0_DELTA, G2_OBS_MAP_CREATE_DELTA,
		G2_OBS_AUDIT_LOG_START_DELTA, G2_OBS_AUDIT_LOG_FORMAT_DELTA,
		G2_OBS_AUDIT_LOG_END_DELTA, G2_OBS_IDR_ALLOC_CYCLIC_DELTA,
		G2_OBS___X64_SYS_BPF_DELTA,
	};
	unsigned long anchor = (unsigned long)&idr_alloc_cyclic, address, target, ret;
	struct idr snapshot;
	unsigned int i, idrs = 0, splits = 0, starts = 0, formats = 0, ends = 0;
	unsigned char bytes[5], byte;
	s32 displacement;

	memset(a, 0, sizeof(*a));
#define G2_RELOC(field, delta) do { if (g2_observer_add(anchor, (delta), &a->field)) return -EINVAL; } while (0)
	G2_RELOC(text_start, G2_OBS_CORE_TEXT_START_DELTA);
	G2_RELOC(text_end, G2_OBS_CORE_TEXT_END_DELTA);
	G2_RELOC(data_start, G2_OBS_CORE_DATA_START_DELTA);
	G2_RELOC(data_end, G2_OBS_CORE_DATA_END_DELTA);
	G2_RELOC(bss_start, G2_OBS_CORE_BSS_START_DELTA);
	G2_RELOC(bss_end, G2_OBS_CORE_BSS_END_DELTA);
	G2_RELOC(prog_idr, G2_OBS_PROG_IDR_DELTA);
	G2_RELOC(map_idr, G2_OBS_MAP_IDR_DELTA);
	G2_RELOC(map_idr_lock, G2_OBS_MAP_IDR_LOCK_DELTA);
	G2_RELOC(prog_free, G2_OBS_BPF_PROG_FREE_ID_DELTA);
	G2_RELOC(map_offload_ops, G2_OBS_BPF_MAP_OFFLOAD_OPS_DELTA);
	G2_RELOC(map_free, G2_OBS_BPF_MAP_FREE_ID_DELTA);
	G2_RELOC(split, G2_OBS_BPF_AUDIT_PROG_PART_0_DELTA);
	G2_RELOC(cgroup_offline, G2_OBS_CGROUP_BPF_OFFLINE_DELTA);
	G2_RELOC(cgroup_release, G2_OBS_CGROUP_BPF_RELEASE_DELTA);
	G2_RELOC(audit_start, G2_OBS_AUDIT_LOG_START_DELTA);
	G2_RELOC(sys_bpf, G2_OBS___X64_SYS_BPF_DELTA);
#undef G2_RELOC
	if (!g2_observer_range(a->text_start, a->text_end, anchor, 1) ||
	    !g2_observer_range(a->data_start, a->data_end, a->prog_idr, sizeof(struct idr)) ||
	    !g2_observer_range(a->data_start, a->data_end, a->map_idr, sizeof(struct idr)) ||
	    !g2_observer_range(a->bss_start, a->bss_end, a->map_idr_lock, sizeof(spinlock_t)) ||
	    !IS_ALIGNED(a->map_idr_lock, __alignof__(spinlock_t)) ||
	    !IS_ALIGNED(a->prog_idr, __alignof__(struct idr)) ||
	    !IS_ALIGNED(a->map_idr, __alignof__(struct idr)) || a->prog_idr == a->map_idr ||
	    !g2_observer_range(a->text_start, a->data_end, a->map_offload_ops, sizeof(struct bpf_map_ops)) ||
	    !IS_ALIGNED(a->map_offload_ops, __alignof__(struct bpf_map_ops)) ||
	    copy_from_kernel_nofault(&snapshot, (void *)a->prog_idr, sizeof(snapshot)) ||
	    copy_from_kernel_nofault(&snapshot, (void *)a->map_idr, sizeof(snapshot)))
		return -EINVAL;
	for (i = 0; i < ARRAY_SIZE(text_deltas); i++) {
		if (g2_observer_add(anchor, text_deltas[i], &address) ||
		    !g2_observer_range(a->text_start, a->text_end, address, 1) ||
		    copy_from_kernel_nofault(&byte, (void *)address, 1))
			return -EINVAL;
	}
	for (i = 0; i < ARRAY_SIZE(g2_observer_windows); i++) {
		const struct g2_observer_window *window = &g2_observer_windows[i];
		unsigned char expected[80], actual[80];
		unsigned int j;
		if (window->size > sizeof(expected) ||
		    g2_observer_add(anchor, window->delta, &address) ||
		    !g2_observer_range(a->text_start, a->text_end, address, window->size) ||
		    copy_from_kernel_nofault(actual, (void *)address, window->size))
			return -EINVAL;
		memcpy(expected, window->bytes, window->size);
		for (j = 0; j < ARRAY_SIZE(window->reloc); j++) {
			u32 value = (u32)(j == 1 ? a->map_idr : a->map_idr_lock);
			if (window->reloc[j] > window->size - sizeof(value))
				return -EINVAL;
			memcpy(expected + window->reloc[j], &value, sizeof(value));
		}
		if (memcmp(actual, expected, window->size))
			return -EINVAL;
	}
	for (i = 0; i < G2_OBSERVER_CALL_COUNT; i++) {
		const struct g2_observer_call *call = &g2_observer_calls[i];
		if (g2_observer_add(anchor, call->call_delta, &address) ||
		    g2_observer_add(anchor, call->return_delta, &ret) ||
		    g2_observer_add(anchor, call->callee_delta, &target) ||
		    !g2_observer_range(a->text_start, a->text_end, address, 5) ||
		    !g2_observer_range(a->text_start, a->text_end, target, 1) ||
		    ret != address + 5 || copy_from_kernel_nofault(bytes, (void *)address, 5) ||
		    memcmp(bytes, call->bytes, 5) || bytes[0] != 0xe8)
			return -EINVAL;
		memcpy(&displacement, bytes + 1, sizeof(displacement));
		if (g2_observer_add(ret, displacement, &address) || address != target)
			return -EINVAL;
		if (call->callee_delta == G2_OBS_IDR_ALLOC_CYCLIC_DELTA) {
			if (target != anchor) return -EINVAL;
			if (call->call_delta >= G2_OBS_MAP_CREATE_DELTA && call->call_delta < G2_OBS_BPF_PROG_LOAD_DELTA && !a->map_alloc_return)
				a->map_alloc_return = ret;
			else if (call->call_delta >= G2_OBS_BPF_PROG_LOAD_DELTA && !a->prog_alloc_return)
				a->prog_alloc_return = ret;
			else return -EINVAL;
			idrs++;
		} else if (call->callee_delta == G2_OBS_BPF_AUDIT_PROG_PART_0_DELTA) {
			if (call->call_delta >= G2_OBS_BPF_PROG_PUT_DEFERRED_DELTA && call->call_delta < G2_OBS_MAP_CREATE_DELTA && !a->unload_audit_return)
				a->unload_audit_return = ret;
			else if (call->call_delta >= G2_OBS_BPF_PROG_LOAD_DELTA && !a->load_audit_return)
				a->load_audit_return = ret;
			else return -EINVAL;
			splits++;
		} else if (call->callee_delta == G2_OBS_AUDIT_LOG_START_DELTA) {
			if (starts++) return -EINVAL;
			a->audit_start_return = ret;
		} else if (call->callee_delta == G2_OBS_AUDIT_LOG_FORMAT_DELTA) {
			formats++;
		} else if (call->callee_delta == G2_OBS_AUDIT_LOG_END_DELTA) {
			ends++;
		} else return -EINVAL;
	}
	if (idrs != 2 || splits != 2 || starts != 1 || formats != 1 || ends != 1 ||
	    !g2_observer_range(a->text_start, a->text_end, a->prog_free, 1) ||
	    !g2_observer_range(a->text_start, a->text_end, a->map_free, 1) ||
	    !g2_observer_range(a->text_start, a->text_end, a->split, 1) ||
	    !g2_observer_range(a->text_start, a->text_end, a->audit_start, 1) ||
	    !g2_observer_range(a->text_start, a->text_end, a->sys_bpf, 1) ||
	    copy_from_kernel_nofault(&byte, (void *)a->prog_free, 1) ||
	    copy_from_kernel_nofault(&byte, (void *)a->map_free, 1) ||
	    copy_from_kernel_nofault(&byte, (void *)a->sys_bpf, 1))
		return -EINVAL;
	return 0;
}
#endif
