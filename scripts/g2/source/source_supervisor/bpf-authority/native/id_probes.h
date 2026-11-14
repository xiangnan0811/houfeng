/* SPDX-License-Identifier: GPL-2.0-only */
#ifndef G2_BPF_GUARD_ID_PROBES_H
#define G2_BPF_GUARD_ID_PROBES_H

#include <linux/bpf.h>
#include <linux/errno.h>
#include <linux/filter.h>
#include <linux/kprobes.h>
#include <linux/ptrace.h>
#include <linux/string.h>
#include <linux/user_namespace.h>

#include "abi.h"
#include "event_ring.h"
#include "task_refs.h"
#include "observer_binding.h"

#define G2_ID_OBJECT_CAPACITY 512
#define G2_ID_PROBE_MAXACTIVE 4096
#define G2_ID_PROGRAM 1U
#define G2_ID_MAP 2U
#define G2_ID_LIVE 1U
#define G2_ID_REMOVING 2U

struct g2_id_object {
	const void *pointer;
	__u64 cookie;
	__u64 publisher_cookie;
	__u32 id;
	__u32 class;
	__u32 state;
	bool id_reuse_first;
};

struct g2_id_probe_set;
struct g2_id_probe {
	struct kretprobe probe;
	struct g2_id_probe_set *owner;
	__u32 class;
	bool removal;
};

struct g2_id_probe_set {
	struct g2_task_refs *tasks;
	struct g2_event_ring *ring;
	struct g2_id_object objects[G2_ID_OBJECT_CAPACITY];
	__u64 next_cookie;
	struct g2_id_probe probes[3];
	const struct g2_observer_addresses *binding;
	__u64 first_cookie, first_removal_sequence, next_transaction_id;
	__u32 removed_id, id_reuse_count;
	__u8 nonce[16];
	bool first_removed, id_reuse_used, id_reuse_armed, id_reuse_inflight;
};

struct g2_id_probe_instance {
	const void *pointer;
	__u64 cookie;
	__u64 task_cookie;
	__u64 task_start_ns;
	__u32 id;
	__u32 index;
	bool paired;
	bool emit;
	bool invalid;
	bool restore_cursor;
	__u32 saved_cursor;
};

static bool g2_id_reuse_shape(const struct bpf_map *map)
{
	return map && !memcmp(map->name, "g2_id_reuse\0\0\0\0\0", sizeof(map->name)) &&
		READ_ONCE(map->map_type) == BPF_MAP_TYPE_ARRAY &&
		READ_ONCE(map->key_size) == 4 && READ_ONCE(map->value_size) == 4 &&
		READ_ONCE(map->max_entries) == 1 && READ_ONCE(map->map_flags) == 0 &&
		READ_ONCE(map->map_extra) == 0 && !READ_ONCE(map->btf);
}

/* Ring lock held; map IDR lock is held by the caller in probe context. */
static int g2_id_cursor_record(struct g2_id_probe_set *set, __u32 operation,
		__u32 saved, s32 result, __u32 after)
{
	struct g2_guard_record record = {};
	struct g2_guard_selftest_id_cursor_detail detail = {};

	record.kind = 16;
	record.operation = operation;
	record.object_cookie = set->first_cookie;
	record.kernel_id = set->removed_id;
	record.task_cookie = set->ring->broker_cookie;
	record.task_start_ns = set->ring->broker_start_ns;
	record.result = result;
	detail.first_removal_sequence = set->first_removal_sequence;
	detail.transaction_id = set->next_transaction_id;
	detail.saved_cursor = saved;
	detail.requested_id = set->removed_id;
	detail.allocator_result = result;
	detail.cursor_after = after;
	memcpy(record.detail, &detail, sizeof(detail));
	return g2_event_ring_append_locked(set->ring, &record, NULL);
}

static bool g2_id_reuse_live(const struct g2_id_probe_set *set, __u32 id)
{
	unsigned int i;
	for (i = 0; i < G2_ID_OBJECT_CAPACITY; i++)
		if (set->objects[i].state && set->objects[i].class == G2_ID_MAP && set->objects[i].id == id)
			return true;
	return false;
}

static bool g2_id_read(const void *pointer, __u32 class, __u32 *id)
{
	if (!pointer || !id)
		return false;
	if (class == G2_ID_PROGRAM) {
		const struct bpf_prog *prog = pointer;

		if (!prog->aux)
			return false;
		*id = READ_ONCE(prog->aux->id);
	} else if (class == G2_ID_MAP) {
		const struct bpf_map *map = pointer;

		*id = READ_ONCE(map->id);
	} else {
		return false;
	}
	return true;
}

/* Only the verified allocator return may dereference these objects: its
 * caller still owns the newly allocated object and aux/user/BTF references.
 * Registry entries never confer a dereference lifetime. */
static bool g2_id_snapshot(struct g2_id_probe_set *set, const void *pointer,
		__u32 class, struct g2_guard_object_snapshot *snapshot)
{
	if (class == G2_ID_PROGRAM) {
		const struct bpf_prog *prog = pointer;
		const struct bpf_prog_aux *aux = READ_ONCE(prog->aux);
		const struct user_struct *user;
		struct g2_guard_program_snapshot *out = &snapshot->attributes.program;

		if (!aux || READ_ONCE(aux->offload_requested) || READ_ONCE(aux->offload))
			return false;
		user = READ_ONCE(aux->user);
		if (!user)
			return false;
		snapshot->type = READ_ONCE(prog->type);
		if (snapshot->type != BPF_PROG_TYPE_LSM &&
		    snapshot->type != BPF_PROG_TYPE_CGROUP_DEVICE)
			return false;
		if (copy_from_kernel_nofault(out->name, aux->name, sizeof(out->name)) ||
		    copy_from_kernel_nofault(out->tag, prog->tag, sizeof(out->tag)))
			return false;
		out->load_time = READ_ONCE(aux->load_time);
		out->created_by_uid = from_kuid_munged(&init_user_ns, READ_ONCE(user->uid));
		/* bpf_prog_insn_size(prog) is prog->len * sizeof(bpf_insn).
		 * Keep the final verifier/runtime count, not the original input. */
		out->translated_insn_count = READ_ONCE(prog->len);
		out->verified_insns = READ_ONCE(aux->verified_insns);
		out->attach_btf_id = READ_ONCE(aux->attach_btf_id);
		return out->translated_insn_count != 0;
	}
	if (class == G2_ID_MAP) {
		const struct bpf_map *map = pointer;
		const struct btf *btf = READ_ONCE(map->btf);
		struct g2_guard_map_snapshot *out = &snapshot->attributes.map;

		snapshot->type = READ_ONCE(map->map_type);
		if ((unsigned long)READ_ONCE(map->ops) == set->binding->map_offload_ops ||
		    (snapshot->type != BPF_MAP_TYPE_HASH && snapshot->type != BPF_MAP_TYPE_ARRAY &&
		     snapshot->type != BPF_MAP_TYPE_TASK_STORAGE))
			return false;
		if (copy_from_kernel_nofault(out->name, map->name, sizeof(out->name)))
			return false;
		out->key_size = READ_ONCE(map->key_size);
		out->value_size = READ_ONCE(map->value_size);
		out->max_entries = READ_ONCE(map->max_entries);
		out->map_flags = READ_ONCE(map->map_flags);
		out->map_extra = READ_ONCE(map->map_extra);
		if (btf) {
			/* Private struct btf is bound to the actual selected BTF;
			 * same-version btf_obj_id is exactly READ_ONCE(btf->id). */
			out->btf_id = READ_ONCE(*(__u32 *)((const char *)btf + G2_OBS_LAYOUT_BTF_ID_OFFSET));
			out->btf_key_type_id = READ_ONCE(map->btf_key_type_id);
			out->btf_value_type_id = READ_ONCE(map->btf_value_type_id);
		}
		return true;
	}
	return false;
}

/* Object registry is protected by the event ring's irq-safe lock. */
static int g2_id_entry(struct kretprobe_instance *instance, struct pt_regs *regs)
{
	struct g2_id_probe *probe = container_of(get_kretprobe(instance),
			struct g2_id_probe, probe);
	struct g2_id_probe_set *set = probe->owner;
	struct g2_id_probe_instance *call = (void *)instance->data;
	struct g2_event_ring *ring = set->ring;
	const void *pointer;
	unsigned long flags;
	__u32 id = 0;
	unsigned int i;
	__u32 class = probe->class;

	if (!probe->removal) {
		if (regs->di == set->binding->prog_idr)
			class = G2_ID_PROGRAM;
		else if (regs->di == set->binding->map_idr)
			class = G2_ID_MAP;
		else
			return 1;
		pointer = (const void *)regs->si;
	} else {
		pointer = (const void *)regs->di;
	}

	memset(call, 0, sizeof(*call));
	if (g2_event_pair_begin(ring))
		return 1;
	call->paired = true;
	call->pointer = pointer;
	call->id = class;
	if (!probe->removal &&
	    ((__u32)regs->dx != 1 || (__u32)regs->cx != INT_MAX ||
	     (__u32)regs->r8 != GFP_ATOMIC)) {
		call->invalid = true;
		g2_event_ring_invalidate(ring);
	}
	call->task_cookie = g2_task_refs_cookie(set->tasks, current);
	if (call->task_cookie)
		call->task_start_ns = current->start_boottime;
	if (!pointer) {
		call->invalid = true;
		g2_event_ring_invalidate(ring);
		return 0;
	}
	if (!probe->removal) {
		if (class == G2_ID_MAP && !call->invalid &&
		    call->task_cookie == ring->broker_cookie &&
		    *(unsigned long *)regs->sp == set->binding->map_alloc_return &&
		    g2_id_reuse_shape(pointer)) {
			struct idr *idr = (struct idr *)set->binding->map_idr;
			spin_lock_irqsave(&ring->lock, flags);
			if (set->id_reuse_armed) {
				if (ring->authority_phase != 1 || g2_id_reuse_live(set, set->removed_id) ||
				    idr_find(idr, set->removed_id)) {
					g2_event_ring_invalidate_locked(ring);
				} else {
					call->saved_cursor = idr_get_cursor(idr);
					call->restore_cursor = true;
					set->id_reuse_armed = false;
					set->id_reuse_inflight = true;
					idr_set_cursor(idr, set->removed_id);
					g2_id_cursor_record(set, 2, call->saved_cursor, 0, idr_get_cursor(idr));
				}
			}
			spin_unlock_irqrestore(&ring->lock, flags);
		}
		return 0;
	}
	if (!g2_id_read(pointer, probe->class, &id)) {
		g2_event_ring_invalidate(ring);
		return 0;
	}
	if (!id)
		return 0;
	spin_lock_irqsave(&ring->lock, flags);
	for (i = 0; i < G2_ID_OBJECT_CAPACITY; i++) {
		struct g2_id_object *object = &set->objects[i];

		if (object->pointer != pointer || object->class != probe->class)
			continue;
		if (object->state != G2_ID_LIVE || object->id != id) {
			g2_event_ring_invalidate_locked(ring);
			goto out;
		}
		object->state = G2_ID_REMOVING;
		call->cookie = object->cookie;
		call->id = id;
		call->index = i;
		call->emit = true;
		goto out;
	}
	if (probe->class == G2_ID_PROGRAM) {
		const struct bpf_prog *prog = pointer;
		__u32 type = READ_ONCE(prog->type);

		if (type != BPF_PROG_TYPE_LSM && type != BPF_PROG_TYPE_CGROUP_DEVICE)
			goto out;
	} else if (probe->class == G2_ID_MAP) {
		const struct bpf_map *map = pointer;
		__u32 type = READ_ONCE(map->map_type);

		if ((unsigned long)READ_ONCE(map->ops) == set->binding->map_offload_ops ||
		    (type != BPF_MAP_TYPE_HASH && type != BPF_MAP_TYPE_ARRAY &&
		     type != BPF_MAP_TYPE_TASK_STORAGE) ||
		    (type == BPF_MAP_TYPE_ARRAY && strcmp(map->name, "__loader.map") == 0))
			goto out;
	}
	/* A nonzero, previously unseen object is not an authorized incarnation. */
	g2_event_ring_invalidate_locked(ring);
out:
	spin_unlock_irqrestore(&ring->lock, flags);
	return 0;
}

static int g2_id_return(struct kretprobe_instance *instance, struct pt_regs *regs)
{
	struct g2_id_probe *probe = container_of(get_kretprobe(instance),
			struct g2_id_probe, probe);
	struct g2_id_probe_set *set = probe->owner;
	struct g2_id_probe_instance *call = (void *)instance->data;
	struct g2_event_ring *ring = set->ring;
	struct g2_guard_record record = {};
	unsigned long flags;
	unsigned int i, empty = G2_ID_OBJECT_CAPACITY;
	__u32 id, class = call->id;
	s32 result;
	record.task_cookie = call->task_cookie;
	record.task_start_ns = call->task_start_ns;

	if (call->restore_cursor)
		idr_set_cursor((struct idr *)set->binding->map_idr, call->saved_cursor);
	if (!call->paired) {
		g2_event_ring_invalidate(ring);
		return 0;
	}
	spin_lock_irqsave(&ring->lock, flags);
	if (call->restore_cursor) {
		result = (s32)(u32)regs_return_value(regs);
		set->id_reuse_inflight = false;
		g2_id_cursor_record(set, 3, call->saved_cursor, result,
				idr_get_cursor((struct idr *)set->binding->map_idr));
	}
	if (!ring->pending_pairs) {
		g2_event_ring_invalidate_locked(ring);
		goto out;
	}
	if (probe->removal) {
		struct g2_id_object *object;

		if (!call->emit)
			goto complete;
		object = &set->objects[call->index];
		if (object->state != G2_ID_REMOVING || object->cookie != call->cookie ||
		    object->pointer != call->pointer || object->id != call->id ||
		    object->class != probe->class) {
			g2_event_ring_invalidate_locked(ring);
			goto complete;
		}
		record.kind = probe->class == G2_ID_PROGRAM ? 2 : 4;
		record.object_cookie = call->cookie;
		record.kernel_id = call->id;
		if (object->id_reuse_first) {
			__u64 sequence = 0;
			if (!g2_event_pair_append_locked(ring, &record, &sequence)) {
				set->first_cookie = object->cookie;
				set->first_removal_sequence = sequence;
				set->removed_id = object->id;
				set->first_removed = true;
			}
		} else {
			g2_event_pair_append_locked(ring, &record, NULL);
		}
		memset(object, 0, sizeof(*object));
		goto complete;
	}
	if (call->invalid)
		goto complete;
	if ((unsigned long)get_kretprobe_retaddr(instance) !=
	    (class == G2_ID_PROGRAM ? set->binding->prog_alloc_return :
	     set->binding->map_alloc_return)) {
		g2_event_ring_invalidate_locked(ring);
		goto complete;
	}
	result = (s32)(u32)regs_return_value(regs);
	if (result < 0)
		goto complete;
	if (!result || result == INT_MAX || !set->next_cookie || set->next_cookie == U64_MAX) {
		g2_event_ring_invalidate_locked(ring);
		goto complete;
	}
	id = (__u32)result;
	if (!g2_id_snapshot(set, call->pointer, class, &record.object_snapshot)) {
		g2_event_ring_invalidate_locked(ring);
		goto complete;
	}
	if (class == G2_ID_MAP && !strcmp(((const struct bpf_map *)call->pointer)->name, "g2_id_reuse")) {
		if (!g2_id_reuse_shape(call->pointer) || call->task_cookie != ring->broker_cookie ||
		    set->id_reuse_count >= 2 ||
		    (set->id_reuse_count == 0 && (set->id_reuse_used || call->restore_cursor)) ||
		    (set->id_reuse_count == 1 && (!set->id_reuse_used || !call->restore_cursor))) {
			g2_event_ring_invalidate_locked(ring);
			goto complete;
		}
	}
	for (i = 0; i < G2_ID_OBJECT_CAPACITY; i++) {
		struct g2_id_object *object = &set->objects[i];

		if (!object->state) {
			if (empty == G2_ID_OBJECT_CAPACITY)
				empty = i;
			continue;
		}
		if ((object->pointer == call->pointer && object->class == class) ||
		    (object->class == class && object->id == id)) {
			g2_event_ring_invalidate_locked(ring);
			goto complete;
		}
	}
	if (empty == G2_ID_OBJECT_CAPACITY) {
		g2_event_ring_invalidate_locked(ring);
		goto complete;
	}
	set->objects[empty].pointer = call->pointer;
	set->objects[empty].cookie = set->next_cookie++;
	set->objects[empty].publisher_cookie = call->task_cookie;
	set->objects[empty].id = id;
	set->objects[empty].class = class;
	set->objects[empty].state = G2_ID_LIVE;
	if (class == G2_ID_MAP && g2_id_reuse_shape(call->pointer)) {
		set->objects[empty].id_reuse_first = set->id_reuse_count == 0;
		set->id_reuse_count++;
	}
	record.kind = class == G2_ID_PROGRAM ? 1 : 3;
	record.object_cookie = set->objects[empty].cookie;
	record.kernel_id = id;
	g2_event_pair_append_locked(ring, &record, NULL);
complete:
	ring->pending_pairs--;
out:
	spin_unlock_irqrestore(&ring->lock, flags);
	wake_up_interruptible_poll(&ring->waiters, EPOLLPRI | EPOLLIN | EPOLLERR);
	return 0;
}

static int g2_id_probes_register(struct g2_id_probe_set *set,
		struct g2_event_ring *ring, struct g2_task_refs *tasks,
		const struct g2_observer_addresses *binding)
{
	unsigned int i;
	int error;

	memset(set, 0, sizeof(*set));
	set->ring = ring;
	set->tasks = tasks;
	set->binding = binding;
	set->next_cookie = 1;
	for (i = 0; i < ARRAY_SIZE(set->probes); i++) {
		struct g2_id_probe *probe = &set->probes[i];

		probe->owner = set;
		probe->class = i == 1 ? G2_ID_PROGRAM : G2_ID_MAP;
		probe->removal = i != 0;
		probe->probe.kp.addr = (kprobe_opcode_t *)(i == 0 ?
			(unsigned long)&idr_alloc_cyclic : i == 1 ? binding->prog_free : binding->map_free);
		probe->probe.entry_handler = g2_id_entry;
		probe->probe.handler = g2_id_return;
		probe->probe.data_size = sizeof(struct g2_id_probe_instance);
		probe->probe.maxactive = G2_ID_PROBE_MAXACTIVE;
		error = register_kretprobe(&probe->probe);
		if (error)
			goto rollback;
	}
	return 0;
rollback:
	while (i)
		unregister_kretprobe(&set->probes[--i].probe);
	return error;
}

static void g2_id_probes_unregister(struct g2_id_probe_set *set)
{
	unsigned int i;

	for (i = ARRAY_SIZE(set->probes); i; i--)
		unregister_kretprobe(&set->probes[i - 1].probe);
}

#endif
