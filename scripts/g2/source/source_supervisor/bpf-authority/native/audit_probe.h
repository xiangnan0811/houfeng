/* SPDX-License-Identifier: GPL-2.0-only */
#ifndef G2_BPF_GUARD_AUDIT_PROBE_H
#define G2_BPF_GUARD_AUDIT_PROBE_H

#include <linux/audit.h>
#include <linux/bpf.h>
#include <linux/kprobes.h>
#include <linux/mm.h>
#include <linux/ptrace.h>
#include <linux/sched/task.h>
#include <linux/skbuff.h>
#include <linux/stddef.h>
#include <linux/string.h>
#include <asm/syscall.h>
#include "pinned/kernel-audit.h"
#include "event_ring.h"
#include "id_probes.h"
#include "task_refs.h"
#include "lineage.h"

_Static_assert(sizeof(struct audit_stamp) == G2_OBS_LAYOUT_AUDIT_STAMP_SIZE, "stamp size");
_Static_assert(offsetof(struct audit_stamp, ctime.tv_sec) == G2_OBS_LAYOUT_STAMP_SECONDS_OFFSET, "stamp seconds");
_Static_assert(offsetof(struct audit_stamp, ctime.tv_nsec) == G2_OBS_LAYOUT_STAMP_NANOSECONDS_OFFSET, "stamp nanoseconds");
_Static_assert(offsetof(struct audit_stamp, serial) == G2_OBS_LAYOUT_STAMP_SERIAL_OFFSET, "stamp serial");
_Static_assert(sizeof_field(struct audit_stamp, serial) == G2_OBS_LAYOUT_STAMP_SERIAL_SIZE, "serial size");
_Static_assert(sizeof(struct audit_context) == G2_OBS_LAYOUT_AUDIT_CONTEXT_SIZE, "context size");
_Static_assert(offsetof(struct audit_context, context) == G2_OBS_LAYOUT_CONTEXT_CLASS_OFFSET, "context class");
_Static_assert(offsetof(struct audit_context, stamp) == G2_OBS_LAYOUT_CONTEXT_STAMP_OFFSET, "context stamp");
_Static_assert(offsetof(struct audit_context, major) == G2_OBS_LAYOUT_CONTEXT_MAJOR_OFFSET, "context major");
_Static_assert(sizeof_field(struct audit_context, major) == G2_OBS_LAYOUT_CONTEXT_MAJOR_SIZE, "major size");
_Static_assert(offsetof(struct audit_context, argv) == G2_OBS_LAYOUT_CONTEXT_ARGV_OFFSET, "context argv");
_Static_assert(sizeof_field(struct audit_context, argv) == G2_OBS_LAYOUT_CONTEXT_ARGV_SIZE, "argv size");
_Static_assert(offsetof(struct audit_context, return_code) == G2_OBS_LAYOUT_CONTEXT_RETURN_CODE_OFFSET, "context return_code");
_Static_assert(sizeof_field(struct audit_context, return_code) == G2_OBS_LAYOUT_CONTEXT_RETURN_CODE_SIZE, "return_code size");
_Static_assert(offsetof(struct audit_context, return_valid) == G2_OBS_LAYOUT_CONTEXT_RETURN_VALID_OFFSET, "context return_valid");
_Static_assert(sizeof_field(struct audit_context, return_valid) == G2_OBS_LAYOUT_CONTEXT_RETURN_VALID_SIZE, "return_valid size");
_Static_assert(offsetof(struct audit_context, arch) == G2_OBS_LAYOUT_CONTEXT_ARCH_OFFSET, "context arch");
_Static_assert(sizeof_field(struct audit_context, arch) == G2_OBS_LAYOUT_CONTEXT_ARCH_SIZE, "arch size");
_Static_assert(AUDIT_CTX_UNUSED == G2_OBS_LAYOUT_CONTEXT_UNUSED_VALUE &&
		AUDIT_CTX_SYSCALL == G2_OBS_LAYOUT_CONTEXT_SYSCALL_VALUE &&
		AUDIT_CTX_URING == G2_OBS_LAYOUT_CONTEXT_URING_VALUE, "context values");
struct g2_audit_buffer_layout {
	struct sk_buff *skb;
	struct sk_buff_head skb_list;
	struct audit_context *ctx;
	struct audit_stamp stamp;
	gfp_t gfp_mask;
};
_Static_assert(sizeof(struct g2_audit_buffer_layout) == G2_OBS_LAYOUT_AUDIT_BUFFER_SIZE, "buffer size");
_Static_assert(offsetof(struct g2_audit_buffer_layout, ctx) == G2_OBS_LAYOUT_AUDIT_BUFFER_CTX_OFFSET, "buffer context");
_Static_assert(offsetof(struct g2_audit_buffer_layout, stamp) == G2_OBS_LAYOUT_AUDIT_BUFFER_STAMP_OFFSET, "buffer stamp");

#define G2_AUDIT_FRAMES 4096
#define G2_AUDIT_DEPTH 8
#define G2_AUDIT_NO_FRAME U32_MAX
#define G2_RAW_SYSCALL_FRAMES 256

struct g2_audit_frame {
	struct task_struct *task;
	__u64 generation, object_cookie, kernel_id, task_cookie, task_start_ns;
	__u64 seconds;
	__u32 nanoseconds, serial, operation, parent, depth;
	bool start_pending, start_seen, valid, syscall_context;
};

struct g2_raw_syscall_frame {
	struct task_struct *task;
	__u64 generation;
	__u64 task_cookie;
	__u64 task_start_ns;
	__u64 exe_dev;
	__u64 exe_ino;
	__u64 seconds;
	__u32 nanoseconds;
	__u32 serial;
	__u32 tgid;
	__u32 tid;
	__u32 command;
	long return_code;
	bool start_pending;
	bool start_seen;
	bool valid;
};

struct g2_audit_probe {
	struct kretprobe probe, start_probe, sys_bpf_probe;
	struct g2_event_ring *ring;
	struct g2_id_probe_set *ids;
	struct g2_task_refs *tasks;
	struct g2_lineage *lineage;
	const struct g2_observer_addresses *binding;
	struct g2_audit_frame frames[G2_AUDIT_FRAMES];
	struct g2_raw_syscall_frame raw_frames[G2_RAW_SYSCALL_FRAMES];
	__u64 next_generation;
};

struct g2_audit_instance {
	__u64 generation;
	struct audit_context *start_context;
	bool bad_start_flags;
	bool is_syscall;
	__u32 index;
	bool paired;
};
/* The ring lock protects every frame and its nesting relationship. */
static __u32 g2_audit_top(struct g2_audit_probe *audit, struct task_struct *task)
{
	__u32 i, found = G2_AUDIT_NO_FRAME, depth = 0;
	for (i = 0; i < G2_AUDIT_FRAMES; i++) {
		struct g2_audit_frame *f = &audit->frames[i];
		if (f->task == task && f->depth > depth) {
			found = i;
			depth = f->depth;
		}
	}
	return found;
}

static int g2_audit_entry(struct kretprobe_instance *instance, struct pt_regs *regs)
{
	struct g2_audit_probe *audit = container_of(get_kretprobe(instance), struct g2_audit_probe, probe);
	struct g2_audit_instance *call = (void *)instance->data;
	struct g2_event_ring *ring = audit->ring;
	struct g2_guard_record record = { .kind = 7 };
	struct g2_audit_frame *frame;
	const struct bpf_prog *prog = (void *)regs->di;
	unsigned long flags;
	__u32 i, parent, index = G2_AUDIT_NO_FRAME, id = 0;
	__u64 task_cookie = 0;
	int task_error;

	memset(call, 0, sizeof(*call));
	if (g2_event_pair_begin(ring)) return 1;
	call->paired = true;
	call->index = G2_AUDIT_NO_FRAME;
	if (!prog || (__u32)regs->si > 1 || !g2_id_read(prog, G2_ID_PROGRAM, &id) || !id) {
		g2_event_ring_invalidate(ring);
		return 0;
	}
	if (READ_ONCE(prog->type) != BPF_PROG_TYPE_LSM &&
	    READ_ONCE(prog->type) != BPF_PROG_TYPE_CGROUP_DEVICE) {
		spin_lock_irqsave(&ring->lock, flags);
		if (ring->pending_pairs)
			ring->pending_pairs--;
		else
			g2_event_ring_invalidate_locked(ring);
		spin_unlock_irqrestore(&ring->lock, flags);
		wake_up_interruptible_poll(&ring->waiters, EPOLLPRI | EPOLLERR);
		return 1;
	}
	spin_lock_irqsave(&audit->tasks->lock, flags);
	task_error = g2_task_refs_insert_locked(audit->tasks, current, &task_cookie, true);
	spin_unlock_irqrestore(&audit->tasks->lock, flags);
	if (task_error) {
		g2_event_ring_invalidate(ring);
		return 0;
	}
	spin_lock_irqsave(&ring->lock, flags);
	parent = g2_audit_top(audit, current);
	if ((parent != G2_AUDIT_NO_FRAME && audit->frames[parent].depth >= G2_AUDIT_DEPTH) ||
	    !audit->next_generation || audit->next_generation == U64_MAX) goto invalid;
	for (i = 0; i < G2_AUDIT_FRAMES; i++)
		if (!audit->frames[i].task) { index = i; break; }
	if (index == G2_AUDIT_NO_FRAME) goto invalid;
	for (i = 0; i < G2_ID_OBJECT_CAPACITY; i++) {
		struct g2_id_object *object = &audit->ids->objects[i];
		if (object->pointer == prog && object->class == G2_ID_PROGRAM &&
		    object->state == G2_ID_LIVE && object->id == id) {
			record.object_cookie = object->cookie;
			break;
		}
	}
	if (!record.object_cookie) goto invalid;
	frame = &audit->frames[index];
	memset(frame, 0, sizeof(*frame));
	get_task_struct(current);
	frame->task = current;
	frame->generation = audit->next_generation++;
	frame->object_cookie = record.object_cookie;
	frame->kernel_id = id;
	frame->task_cookie = task_cookie;
	frame->task_start_ns = task_cookie ? current->start_boottime : 0;
	frame->operation = (__u32)regs->si;
	frame->parent = parent;
	frame->depth = parent == G2_AUDIT_NO_FRAME ? 1 : audit->frames[parent].depth + 1;
	frame->valid = true;
	call->index = index;
	call->generation = frame->generation;
	record.kernel_id = id;
	record.task_cookie = frame->task_cookie;
	record.task_start_ns = frame->task_start_ns;
	record.operation = frame->operation;
	g2_event_ring_append_locked(ring, &record, NULL);
	goto out;
invalid:
	ring->sticky = 1;
out:
	spin_unlock_irqrestore(&ring->lock, flags);
	if (call->index == G2_AUDIT_NO_FRAME)
		g2_task_refs_drop_kind(audit->tasks, task_cookie, true);
	wake_up_interruptible_poll(&ring->waiters, EPOLLIN | EPOLLERR);
	return 0;
}

static __u32 g2_raw_syscall_find(struct g2_audit_probe *audit,
		struct task_struct *task)
{
	__u32 i;
	for (i = 0; i < G2_RAW_SYSCALL_FRAMES; i++)
		if (audit->raw_frames[i].task == task)
			return i;
	return G2_AUDIT_NO_FRAME;
}

static int g2_sys_bpf_entry(struct kretprobe_instance *instance, struct pt_regs *regs)
{
	struct g2_audit_probe *audit = container_of(get_kretprobe(instance), struct g2_audit_probe, sys_bpf_probe);
	struct g2_event_ring *ring = audit->ring;
	struct pt_regs *uregs;
	unsigned long args[6] = {};
	__u64 cookie = 0, start_ns = 0, exe_dev = 0, exe_ino = 0;
	__u32 tgid = 0, tid = 0, i, slot = G2_AUDIT_NO_FRAME;
	struct g2_raw_syscall_frame *frame;
	unsigned long flags;

	if (!regs || syscall_get_arch(current) != AUDIT_ARCH_X86_64)
		return 1;
	if (!g2_lineage_watched_syscall_actor(audit->lineage, current, &cookie,
			&start_ns, &tgid, &tid, &exe_dev, &exe_ino))
		return 1;
	uregs = (struct pt_regs *)regs->di;
	if (!uregs || uregs != task_pt_regs(current) || syscall_get_nr(current, uregs) != 321) {
		g2_event_ring_invalidate(ring);
		return 1;
	}
	syscall_get_arguments(current, uregs, args);
	spin_lock_irqsave(&ring->lock, flags);
	if (g2_raw_syscall_find(audit, current) != G2_AUDIT_NO_FRAME ||
	    !audit->next_generation || audit->next_generation == U64_MAX) {
		g2_event_ring_invalidate_locked(ring);
		spin_unlock_irqrestore(&ring->lock, flags);
		wake_up_interruptible_poll(&ring->waiters, EPOLLERR);
		return 1;
	}
	for (i = 0; i < G2_RAW_SYSCALL_FRAMES; i++) {
		if (!audit->raw_frames[i].task) {
			slot = i;
			break;
		}
	}
	if (slot == G2_AUDIT_NO_FRAME) {
		g2_event_ring_invalidate_locked(ring);
		spin_unlock_irqrestore(&ring->lock, flags);
		wake_up_interruptible_poll(&ring->waiters, EPOLLERR);
		return 1;
	}
	frame = &audit->raw_frames[slot];
	memset(frame, 0, sizeof(*frame));
	frame->task = current;
	frame->generation = audit->next_generation++;
	frame->task_cookie = cookie;
	frame->task_start_ns = start_ns;
	frame->exe_dev = exe_dev;
	frame->exe_ino = exe_ino;
	frame->tgid = tgid;
	frame->tid = tid;
	frame->command = (__u32)args[0];
	frame->valid = args[0] <= U32_MAX;
	if (!frame->valid)
		g2_event_ring_invalidate_locked(ring);
	spin_unlock_irqrestore(&ring->lock, flags);
	return 0;
}

static int g2_sys_bpf_return(struct kretprobe_instance *instance, struct pt_regs *regs)
{
	struct g2_audit_probe *audit = container_of(get_kretprobe(instance), struct g2_audit_probe, sys_bpf_probe);
	struct g2_event_ring *ring = audit->ring;
	struct g2_raw_syscall_frame *frame;
	unsigned long flags;
	long ret;
	__u32 slot;

	if (!regs)
		return 0;
	ret = regs_return_value(regs);
	spin_lock_irqsave(&ring->lock, flags);
	slot = g2_raw_syscall_find(audit, current);
	if (slot == G2_AUDIT_NO_FRAME) {
		g2_event_ring_invalidate_locked(ring);
		spin_unlock_irqrestore(&ring->lock, flags);
		wake_up_interruptible_poll(&ring->waiters, EPOLLERR);
		return 0;
	}
	frame = &audit->raw_frames[slot];
	if (frame->start_pending || frame->start_seen || !frame->valid ||
	    ret < S32_MIN || ret > S32_MAX) {
		g2_event_ring_invalidate_locked(ring);
		frame->valid = false;
	} else {
		frame->return_code = ret;
	}
	spin_unlock_irqrestore(&ring->lock, flags);
	return 0;
}
static int g2_audit_start_entry(struct kretprobe_instance *instance, struct pt_regs *regs)
{
	struct g2_audit_probe *audit = container_of(get_kretprobe(instance), struct g2_audit_probe, start_probe);
	struct g2_audit_instance *call = (void *)instance->data;
	struct g2_event_ring *ring = audit->ring;
	unsigned long flags;
	__u32 top, type = (__u32)regs->dx;

	memset(call, 0, sizeof(*call));
	if (type == AUDIT_BPF) {
		spin_lock_irqsave(&ring->lock, flags);
		top = g2_audit_top(audit, current);
		if (top == G2_AUDIT_NO_FRAME) {
			spin_unlock_irqrestore(&ring->lock, flags);
			return 1;
		}
		if (audit->frames[top].start_pending || audit->frames[top].start_seen) {
			g2_event_ring_invalidate_locked(ring);
			spin_unlock_irqrestore(&ring->lock, flags);
			return 1;
		}
		audit->frames[top].start_pending = true;
		call->index = top;
		call->generation = audit->frames[top].generation;
		call->start_context = (void *)regs->di;
		call->bad_start_flags = (__u32)regs->si != GFP_ATOMIC;
		call->is_syscall = false;
		spin_unlock_irqrestore(&ring->lock, flags);
		return 0;
	}
	if (type == 1300) {
		spin_lock_irqsave(&ring->lock, flags);
		top = g2_raw_syscall_find(audit, current);
		if (top == G2_AUDIT_NO_FRAME) {
			spin_unlock_irqrestore(&ring->lock, flags);
			return 1;
		}
		if (audit->raw_frames[top].start_pending || audit->raw_frames[top].start_seen) {
			g2_event_ring_invalidate_locked(ring);
			spin_unlock_irqrestore(&ring->lock, flags);
			return 1;
		}
		audit->raw_frames[top].start_pending = true;
		call->index = top;
		call->generation = audit->raw_frames[top].generation;
		call->start_context = (void *)regs->di;
		call->bad_start_flags = false;
		call->is_syscall = true;
		spin_unlock_irqrestore(&ring->lock, flags);
		return 0;
	}
	return 1;
}

static int g2_audit_start_return(struct kretprobe_instance *instance, struct pt_regs *regs)
{
	struct g2_audit_probe *audit = container_of(get_kretprobe(instance), struct g2_audit_probe, start_probe);
	struct g2_audit_instance *call = (void *)instance->data;
	struct g2_event_ring *ring = audit->ring;
	struct audit_context *ctx = NULL;
	struct audit_stamp stamp = {}, ctx_stamp = {};
	const char *buffer = (void *)regs_return_value(regs);
	struct g2_audit_frame *frame;
	struct g2_raw_syscall_frame *raw_frame;
	unsigned long flags;
	int context = AUDIT_CTX_UNUSED;
	bool valid;

	if (call->is_syscall) {
		int major = 0, return_valid = 0, arch = 0;
		unsigned long argv[4] = {};
		long return_code = 0;

		valid = buffer && call->start_context &&
			!copy_from_kernel_nofault(&ctx, buffer + G2_OBS_LAYOUT_AUDIT_BUFFER_CTX_OFFSET, sizeof(ctx)) &&
			ctx == call->start_context &&
			!copy_from_kernel_nofault(&stamp, buffer + G2_OBS_LAYOUT_AUDIT_BUFFER_STAMP_OFFSET, sizeof(stamp)) &&
			!copy_from_kernel_nofault(&context, &ctx->context, sizeof(context)) &&
			context == AUDIT_CTX_SYSCALL &&
			!copy_from_kernel_nofault(&ctx_stamp, &ctx->stamp, sizeof(ctx_stamp)) &&
			ctx_stamp.ctime.tv_sec == stamp.ctime.tv_sec &&
			ctx_stamp.ctime.tv_nsec == stamp.ctime.tv_nsec &&
			ctx_stamp.serial == stamp.serial &&
			!copy_from_kernel_nofault(&major, &ctx->major, sizeof(major)) &&
			major == 321 &&
			!copy_from_kernel_nofault(argv, ctx->argv, sizeof(argv)) &&
			!copy_from_kernel_nofault(&return_code, &ctx->return_code, sizeof(return_code)) &&
			!copy_from_kernel_nofault(&return_valid, &ctx->return_valid, sizeof(return_valid)) &&
			return_valid != 0 &&
			!copy_from_kernel_nofault(&arch, &ctx->arch, sizeof(arch)) &&
			(__u32)arch == AUDIT_ARCH_X86_64 &&
			stamp.ctime.tv_sec >= 0 && stamp.ctime.tv_nsec >= 0 &&
			stamp.ctime.tv_nsec < 1000000000 && stamp.serial;
		spin_lock_irqsave(&ring->lock, flags);
		raw_frame = &audit->raw_frames[call->index];
		if (raw_frame->task != current || raw_frame->generation != call->generation ||
		    !raw_frame->start_pending || raw_frame->start_seen) {
			g2_event_ring_invalidate_locked(ring);
		} else {
			struct g2_guard_record record = { .kind = G2_GUARD_RAW_SYSCALL };
			struct g2_guard_raw_syscall_detail detail = {};

			raw_frame->start_pending = false;
			raw_frame->start_seen = true;
			valid = valid && argv[0] == (unsigned long)raw_frame->command &&
				return_code == raw_frame->return_code &&
				return_code >= S32_MIN && return_code <= S32_MAX;
			raw_frame->valid = raw_frame->valid && valid;
			if (!raw_frame->valid) {
				g2_event_ring_invalidate_locked(ring);
			} else {
				detail.tgid = raw_frame->tgid;
				detail.tid = raw_frame->tid;
				detail.seconds = (__u64)stamp.ctime.tv_sec;
				detail.nanoseconds = (__u32)stamp.ctime.tv_nsec;
				detail.exe_dev = raw_frame->exe_dev;
				detail.exe_ino = raw_frame->exe_ino;
				record.task_cookie = raw_frame->task_cookie;
				record.task_start_ns = raw_frame->task_start_ns;
				record.audit_serial = stamp.serial;
				record.operation = raw_frame->command;
				record.result = (__s32)return_code;
				memcpy(record.detail, &detail, sizeof(detail));
				g2_event_ring_append_locked(ring, &record, NULL);
			}
			memset(raw_frame, 0, sizeof(*raw_frame));
		}
		spin_unlock_irqrestore(&ring->lock, flags);
		wake_up_interruptible_poll(&ring->waiters, EPOLLIN | EPOLLERR);
		return 0;
	}

	valid = (unsigned long)get_kretprobe_retaddr(instance) == audit->binding->audit_start_return &&
		!call->bad_start_flags && buffer &&
		!copy_from_kernel_nofault(&ctx, buffer + G2_OBS_LAYOUT_AUDIT_BUFFER_CTX_OFFSET, sizeof(ctx)) &&
		ctx == call->start_context &&
		!copy_from_kernel_nofault(&stamp, buffer + G2_OBS_LAYOUT_AUDIT_BUFFER_STAMP_OFFSET, sizeof(stamp));
	if (valid && ctx) {
		valid = !copy_from_kernel_nofault(&context, &ctx->context, sizeof(context));
		if (valid && context == AUDIT_CTX_SYSCALL)
			valid = !copy_from_kernel_nofault(&ctx_stamp, &ctx->stamp, sizeof(ctx_stamp)) &&
				ctx_stamp.ctime.tv_sec == stamp.ctime.tv_sec &&
				ctx_stamp.ctime.tv_nsec == stamp.ctime.tv_nsec && ctx_stamp.serial == stamp.serial;
		else if (valid && context != AUDIT_CTX_UNUSED)
			valid = false;
	}
	valid = valid && stamp.ctime.tv_sec >= 0 && stamp.ctime.tv_nsec >= 0 &&
		stamp.ctime.tv_nsec < 1000000000 && stamp.serial;
	spin_lock_irqsave(&ring->lock, flags);
	frame = &audit->frames[call->index];
	if (frame->task != current || frame->generation != call->generation ||
	    g2_audit_top(audit, current) != call->index || !frame->start_pending || frame->start_seen) {
		g2_event_ring_invalidate_locked(ring);
	} else {
		frame->start_pending = false;
		frame->start_seen = true;
		frame->valid = frame->valid && valid;
		if (!valid) g2_event_ring_invalidate_locked(ring);
		frame->seconds = stamp.ctime.tv_sec;
		frame->nanoseconds = stamp.ctime.tv_nsec;
		frame->serial = stamp.serial;
		frame->syscall_context = ctx && context == AUDIT_CTX_SYSCALL;
	}
	spin_unlock_irqrestore(&ring->lock, flags);
	return 0;
}

static int g2_audit_return(struct kretprobe_instance *instance, struct pt_regs *regs)
{
	struct g2_audit_probe *audit = container_of(get_kretprobe(instance), struct g2_audit_probe, probe);
	struct g2_audit_instance *call = (void *)instance->data;
	struct g2_event_ring *ring = audit->ring;
	struct g2_guard_record record = { .kind = 8 };
	struct task_struct *release = NULL;
	__u64 release_cookie = 0;
	struct g2_audit_frame *frame;
	unsigned long flags, ret = (unsigned long)get_kretprobe_retaddr(instance);

	(void)regs;
	spin_lock_irqsave(&ring->lock, flags);
	if (!call->paired || !ring->pending_pairs) { g2_event_ring_invalidate_locked(ring); goto out; }
	if (call->index == G2_AUDIT_NO_FRAME) goto complete;
	frame = &audit->frames[call->index];
	if (frame->task != current || frame->generation != call->generation ||
	    g2_audit_top(audit, current) != call->index || frame->start_pending ||
	    !frame->start_seen || !frame->valid ||
	    ret != (frame->operation ? audit->binding->unload_audit_return : audit->binding->load_audit_return)) {
		g2_event_ring_invalidate_locked(ring);
		/* Retain any slot still referenced by a nested or start probe. */
		if (frame->task != current || frame->generation != call->generation ||
		    frame->start_pending || g2_audit_top(audit, current) != call->index)
			goto complete;
	} else {
		record.object_cookie = frame->object_cookie;
		record.kernel_id = frame->kernel_id;
		record.task_cookie = frame->task_cookie;
		record.task_start_ns = frame->task_start_ns;
		record.operation = frame->operation;
		record.audit_serial = frame->serial;
		if (frame->syscall_context) record.flags = 1;
		memcpy(record.detail, &frame->seconds, sizeof(frame->seconds));
		memcpy(record.detail + 8, &frame->nanoseconds, sizeof(frame->nanoseconds));
		g2_event_pair_append_locked(ring, &record, NULL);
	}
	release = frame->task;
	release_cookie = frame->task_cookie;
	memset(frame, 0, sizeof(*frame));
complete:
	ring->pending_pairs--;
out:
	spin_unlock_irqrestore(&ring->lock, flags);
	if (release) put_task_struct(release);
	if (release_cookie && g2_task_refs_drop_kind(audit->tasks, release_cookie, true))
		g2_event_ring_invalidate(ring);
	wake_up_interruptible_poll(&ring->waiters, EPOLLPRI | EPOLLIN | EPOLLERR);
	return 0;
}

static int g2_audit_probe_register(struct g2_audit_probe *audit,
		struct g2_event_ring *ring, struct g2_id_probe_set *ids,
		struct g2_task_refs *tasks, struct g2_lineage *lineage,
		const struct g2_observer_addresses *binding)
{
	int error;
	memset(audit, 0, sizeof(*audit));
	audit->ring = ring;
	audit->ids = ids;
	audit->tasks = tasks;
	audit->lineage = lineage;
	audit->binding = binding;
	audit->next_generation = 1;
	audit->probe.kp.addr = (kprobe_opcode_t *)binding->split;
	audit->probe.entry_handler = g2_audit_entry;
	audit->probe.handler = g2_audit_return;
	audit->probe.data_size = sizeof(struct g2_audit_instance);
	audit->probe.maxactive = 4096;
	audit->start_probe.kp.addr = (kprobe_opcode_t *)binding->audit_start;
	audit->start_probe.entry_handler = g2_audit_start_entry;
	audit->start_probe.handler = g2_audit_start_return;
	audit->start_probe.data_size = sizeof(struct g2_audit_instance);
	audit->start_probe.maxactive = 4096;
	audit->sys_bpf_probe.kp.addr = (kprobe_opcode_t *)binding->sys_bpf;
	audit->sys_bpf_probe.entry_handler = g2_sys_bpf_entry;
	audit->sys_bpf_probe.handler = g2_sys_bpf_return;
	audit->sys_bpf_probe.data_size = 0;
	audit->sys_bpf_probe.maxactive = 4096;
	error = register_kretprobe(&audit->probe);
	if (error) return error;
	error = register_kretprobe(&audit->start_probe);
	if (error) {
		unregister_kretprobe(&audit->probe);
		return error;
	}
	error = register_kretprobe(&audit->sys_bpf_probe);
	if (error) {
		unregister_kretprobe(&audit->start_probe);
		unregister_kretprobe(&audit->probe);
	}
	return error;
}

static void g2_audit_probe_unregister(struct g2_audit_probe *audit)
{
	unsigned int i;
	unregister_kretprobe(&audit->sys_bpf_probe);
	unregister_kretprobe(&audit->start_probe);
	unregister_kretprobe(&audit->probe);
	for (i = 0; i < G2_AUDIT_FRAMES; i++) {
		if (!audit->frames[i].task)
			continue;
		g2_task_refs_drop_kind(audit->tasks, audit->frames[i].task_cookie, true);
		put_task_struct(audit->frames[i].task);
	}
}
#endif
