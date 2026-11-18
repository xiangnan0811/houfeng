/* SPDX-License-Identifier: GPL-2.0-only */
#ifndef G2_BPF_GUARD_CGROUP_REFS_H
#define G2_BPF_GUARD_CGROUP_REFS_H

#include <linux/cgroup.h>
#include <linux/errno.h>
#include <linux/file.h>
#include <linux/fs.h>
#include <linux/kernfs.h>
#include <linux/kprobes.h>
#include <linux/magic.h>
#include <linux/ptrace.h>
#include <linux/string.h>

#include "abi.h"
#include "event_ring.h"
#include "task_refs.h"

#define G2_CGROUP_CAPACITY 128
#define G2_CGROUP_MAXACTIVE 4096

struct g2_cgroup_entry {
	struct cgroup *cgroup;
	struct file *file;
	__u64 root_id;
	__u64 node_id;
	__u64 enrollment;
	__u64 grant_id;
	__u64 offline_seq;
	__u64 release_seq;
	bool retired;
};

struct g2_cgroup_refs {
	struct g2_event_ring *ring;
	struct g2_task_refs *tasks;
	struct g2_cgroup_entry entries[G2_CGROUP_CAPACITY];
	__u64 next_enrollment;
	__u64 default_root_id;
	struct kprobe offline;
	struct kretprobe release;
};

struct g2_cgroup_release_instance {
	__u64 root_id;
	__u64 node_id;
	__u64 task_cookie;
	__u64 task_start_ns;
	bool paired;
	bool valid;
};

/* The caller or the cgroup release callback holds a live cgroup reference. */
static int g2_cgroup_identity(struct cgroup *cgroup, __u64 *root, __u64 *node)
{
	struct kernfs_node *kn, *root_kn;
	struct cgroup_root *hierarchy;

	if (!cgroup)
		return -EINVAL;
	hierarchy = READ_ONCE(cgroup->root);
	kn = READ_ONCE(cgroup->kn);
	if (!hierarchy || !kn)
		return -EINVAL;
	root_kn = READ_ONCE(hierarchy->cgrp.kn);
	if (!root_kn)
		return -EINVAL;
	*root = READ_ONCE(root_kn->id);
	*node = READ_ONCE(kn->id);
	return *root && *node ? 0 : -EINVAL;
}

static void g2_cgroup_refs_init(struct g2_cgroup_refs *refs,
		struct g2_event_ring *ring, struct g2_task_refs *tasks)
{
	memset(refs, 0, sizeof(*refs));
	refs->ring = ring;
	refs->tasks = tasks;
	refs->next_enrollment = 1;
}

static int g2_cgroup_refs_enroll(struct g2_cgroup_refs *refs,
		struct g2_guard_enroll_cgroup *request)
{
	struct g2_event_ring *ring = refs->ring;
	struct g2_cgroup_entry *empty = NULL;
	struct cgroup *cgroup;
	struct file *file;
	struct inode *inode;
	unsigned long flags;
	__u64 root_id, node_id, start_sequence, cursor;
	unsigned int i;
	int error = 0;

	if (request->cgroup_fd < 0 || request->pad || !request->grant_id ||
	    request->root_cookie || request->kernfs_id || request->enrollment)
		return -EINVAL;
	spin_lock_irqsave(&ring->lock, flags);
	start_sequence = ring->sequence;
	spin_unlock_irqrestore(&ring->lock, flags);
	file = fget(request->cgroup_fd);
	if (!file)
		return -EBADF;
	cgroup = cgroup_get_from_fd(request->cgroup_fd);
	if (IS_ERR(cgroup)) {
		error = PTR_ERR(cgroup);
		goto out_file;
	}
	inode = file_inode(file);
	if (!inode || !S_ISDIR(inode->i_mode) || !inode->i_sb ||
	    inode->i_sb->s_magic != CGROUP2_SUPER_MAGIC ||
	    cgroup->root != &cgrp_dfl_root || inode->i_private != cgroup->kn ||
	    g2_cgroup_identity(cgroup, &root_id, &node_id)) {
		error = -EINVAL;
		goto out_cgroup;
	}
	spin_lock_irqsave(&ring->lock, flags);
	if (ring->pending_pairs) {
		error = -EAGAIN;
		goto out_unlock;
	}
	if (ring->sticky || start_sequence < ring->ack_cursor ||
	    ring->sequence - start_sequence > G2_GUARD_RING_CAPACITY ||
	    (refs->default_root_id && refs->default_root_id != root_id)) {
		ring->sticky = 1;
		error = -EIO;
		goto out_unlock;
	}
	/* Observe any offline which raced the live fd acquisition. */
	for (cursor = start_sequence; cursor < ring->sequence;) {
		struct g2_guard_record *event;

		cursor++;
		event = &ring->records[(cursor - 1) % G2_GUARD_RING_CAPACITY];
		if (event->sequence != cursor) {
			ring->sticky = 1;
			error = -EIO;
			goto out_unlock;
		}
		if (event->kind == 5 && event->root_cookie == root_id &&
		    event->cgroup_id == node_id) {
			error = -ESTALE;
			goto out_unlock;
		}
	}
	for (i = 0; i < G2_CGROUP_CAPACITY; i++) {
		struct g2_cgroup_entry *entry = &refs->entries[i];

		if (!entry->enrollment) {
			if (!empty)
				empty = entry;
			continue;
		}
		if ((entry->root_id == root_id && entry->node_id == node_id) ||
		    entry->grant_id == request->grant_id) {
			if (entry->retired || entry->offline_seq || entry->release_seq ||
			    entry->root_id != root_id || entry->node_id != node_id ||
			    entry->cgroup != cgroup || entry->grant_id != request->grant_id) {
				error = -ESTALE;
				goto out_unlock;
			}
			request->root_cookie = root_id;
			request->kernfs_id = node_id;
			request->enrollment = entry->enrollment;
			goto out_unlock;
		}
	}
	if (!empty || !refs->next_enrollment || refs->next_enrollment == U64_MAX) {
		error = -ENOSPC;
		goto out_unlock;
	}
	refs->default_root_id = root_id;
	empty->cgroup = cgroup;
	empty->file = file;
	empty->root_id = root_id;
	empty->node_id = node_id;
	empty->grant_id = request->grant_id;
	empty->enrollment = refs->next_enrollment++;
	request->root_cookie = root_id;
	request->kernfs_id = node_id;
	request->enrollment = empty->enrollment;
	cgroup = NULL;
	file = NULL;
out_unlock:
	spin_unlock_irqrestore(&ring->lock, flags);
	if (cgroup)
		cgroup_put(cgroup);
out_file:
	if (file)
		fput(file);
	return error;
out_cgroup:
	cgroup_put(cgroup);
	goto out_file;
}

static int g2_cgroup_refs_retire(struct g2_cgroup_refs *refs,
		struct g2_guard_retire_cgroup *request)
{
	struct g2_event_ring *ring = refs->ring;
	struct cgroup *cgroup = NULL;
	struct file *file = NULL;
	unsigned long flags;
	unsigned int i;
	int error = -ENOENT;

	if (!request->enrollment || request->observed_state || request->pad ||
	    request->sequence)
		return -EINVAL;
	spin_lock_irqsave(&ring->lock, flags);
	if (ring->sticky || ring->pending_pairs) {
		error = ring->sticky ? -EIO : -EAGAIN;
		goto out_retire_unlock;
	}
	for (i = 0; i < G2_CGROUP_CAPACITY; i++) {
		struct g2_cgroup_entry *entry = &refs->entries[i];

		if (entry->enrollment != request->enrollment)
			continue;
		if (entry->release_seq) {
			request->observed_state = 2;
			request->sequence = entry->release_seq;
			entry->retired = true;
			cgroup = entry->cgroup;
			file = entry->file;
			entry->cgroup = NULL;
			entry->file = NULL;
		} else if (entry->offline_seq) {
			request->observed_state = 1;
			request->sequence = entry->offline_seq;
		} else {
			request->observed_state = 0;
			request->sequence = ring->sequence;
		}
		error = 0;
		break;
	}
out_retire_unlock:
	spin_unlock_irqrestore(&ring->lock, flags);
	if (cgroup)
		cgroup_put(cgroup);
	if (file)
		fput(file);
	return error;
}

static int g2_cgroup_offline(struct kprobe *probe, struct pt_regs *regs)
{
	struct g2_cgroup_refs *refs = container_of(probe, struct g2_cgroup_refs, offline);
	struct g2_event_ring *ring = refs->ring;
	struct cgroup *cgroup = (void *)regs->di;
	struct g2_guard_record record = { .kind = 5 };
	unsigned long flags;
	unsigned int i;
	__u64 sequence = 0;

	if (g2_cgroup_identity(cgroup, &record.root_cookie, &record.cgroup_id)) {
		g2_event_ring_invalidate(ring);
		return 0;
	}
	record.task_cookie = g2_task_refs_cookie(refs->tasks, current);
	if (record.task_cookie)
		record.task_start_ns = current->start_boottime;
	spin_lock_irqsave(&ring->lock, flags);
	if (!g2_event_ring_append_locked(ring, &record, &sequence)) {
		for (i = 0; i < G2_CGROUP_CAPACITY; i++) {
			struct g2_cgroup_entry *entry = &refs->entries[i];

			if (entry->enrollment && entry->root_id == record.root_cookie &&
			    entry->node_id == record.cgroup_id) {
				if (entry->offline_seq || entry->cgroup != cgroup)
					g2_event_ring_invalidate_locked(ring);
				else
					entry->offline_seq = sequence;
				break;
			}
		}
	}
	spin_unlock_irqrestore(&ring->lock, flags);
	wake_up_interruptible_poll(&ring->waiters, EPOLLIN | EPOLLERR);
	return 0;
}

static int g2_cgroup_release_entry(struct kretprobe_instance *instance,
		struct pt_regs *regs)
{
	struct kretprobe *probe = get_kretprobe(instance);
	struct g2_cgroup_refs *refs = container_of(probe, struct g2_cgroup_refs, release);
	struct g2_cgroup_release_instance *call = (void *)instance->data;
	struct work_struct *work = (void *)regs->di;
	struct cgroup *cgroup;

	memset(call, 0, sizeof(*call));
	if (g2_event_pair_begin(refs->ring))
		return 1;
	call->paired = true;
	if (!work) {
		g2_event_ring_invalidate(refs->ring);
		return 0;
	}
	cgroup = container_of(work, struct cgroup, bpf.release_work);
	if (g2_cgroup_identity(cgroup, &call->root_id, &call->node_id)) {
		g2_event_ring_invalidate(refs->ring);
		return 0;
	}
	call->task_cookie = g2_task_refs_cookie(refs->tasks, current);
	if (call->task_cookie)
		call->task_start_ns = current->start_boottime;
	call->valid = true;
	return 0;
}

static int g2_cgroup_release_return(struct kretprobe_instance *instance,
		struct pt_regs *regs)
{
	struct kretprobe *probe = get_kretprobe(instance);
	struct g2_cgroup_refs *refs = container_of(probe, struct g2_cgroup_refs, release);
	struct g2_cgroup_release_instance *call = (void *)instance->data;
	struct g2_event_ring *ring = refs->ring;
	struct g2_guard_record record = { .kind = 6 };
	unsigned long flags;
	unsigned int i;
	__u64 sequence = 0;

	(void)regs;
	if (!call->paired) {
		g2_event_ring_invalidate(ring);
		return 0;
	}
	spin_lock_irqsave(&ring->lock, flags);
	if (!ring->pending_pairs) {
		g2_event_ring_invalidate_locked(ring);
		goto out;
	}
	if (call->valid) {
		record.root_cookie = call->root_id;
		record.cgroup_id = call->node_id;
		record.task_cookie = call->task_cookie;
		record.task_start_ns = call->task_start_ns;
		if (!g2_event_pair_append_locked(ring, &record, &sequence)) {
			for (i = 0; i < G2_CGROUP_CAPACITY; i++) {
				struct g2_cgroup_entry *entry = &refs->entries[i];

				if (entry->enrollment && entry->root_id == call->root_id &&
				    entry->node_id == call->node_id) {
					if (!entry->offline_seq || entry->release_seq)
						g2_event_ring_invalidate_locked(ring);
					else
						entry->release_seq = sequence;
					break;
				}
			}
		}
	}
	ring->pending_pairs--;
out:
	spin_unlock_irqrestore(&ring->lock, flags);
	wake_up_interruptible_poll(&ring->waiters, EPOLLPRI | EPOLLIN | EPOLLERR);
	return 0;
}

static int g2_cgroup_probes_register(struct g2_cgroup_refs *refs)
{
	int error;

	refs->offline.symbol_name = "cgroup_bpf_offline";
	refs->offline.pre_handler = g2_cgroup_offline;
	error = register_kprobe(&refs->offline);
	if (error)
		return error;
	refs->release.kp.symbol_name = "cgroup_bpf_release";
	refs->release.entry_handler = g2_cgroup_release_entry;
	refs->release.handler = g2_cgroup_release_return;
	refs->release.data_size = sizeof(struct g2_cgroup_release_instance);
	refs->release.maxactive = G2_CGROUP_MAXACTIVE;
	error = register_kretprobe(&refs->release);
	if (error)
		unregister_kprobe(&refs->offline);
	return error;
}

static void g2_cgroup_probes_unregister(struct g2_cgroup_refs *refs)
{
	unregister_kretprobe(&refs->release);
	unregister_kprobe(&refs->offline);
}

#endif
