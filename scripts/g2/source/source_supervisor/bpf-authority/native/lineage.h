/* SPDX-License-Identifier: GPL-2.0-only */
#ifndef G2_BPF_GUARD_LINEAGE_H
#define G2_BPF_GUARD_LINEAGE_H

#include <linux/binfmts.h>
#include <linux/errno.h>
#include <linux/fs.h>
#include <linux/mm_types.h>
#include <linux/rcupdate.h>
#include <linux/sched.h>
#include <linux/spinlock.h>
#include <linux/string.h>
#include <linux/types.h>

#include "abi.h"
#include "event_ring.h"
#include "task_refs.h"

#define G2_LINEAGE_LIVE_MAX 256
#define G2_LINEAGE_TOMBSTONE_MAX 4096
#define G2_GUARD_TASK_FORK 9U
#define G2_GUARD_TASK_EXEC 10U
#define G2_GUARD_TASK_EXIT 11U
#define G2_GUARD_TASK_WATCH 12U
#define G2_GUARD_RAW_SYSCALL 13U
#define G2_GUARD_AUTHORITY_TRANSITION 14U
#define G2_GUARD_BROKER_EXIT 15U
struct g2_lineage_entry {
	struct task_struct *task;
	__u64 cookie;
	__u64 parent_cookie;
	__u64 start_ns;
	__u64 exe_dev;
	__u64 exe_ino;
	__u64 exec_generation;
	__u64 watch_sequence;
	__u32 tgid;
	__u32 tid;
	__u32 probe_kind;
};

struct g2_lineage_tombstone {
	__u64 cookie;
	__u64 parent_cookie;
	__u64 start_ns;
	__u64 exe_dev;
	__u64 exe_ino;
	__u64 exec_generation;
	__u32 tgid;
	__u32 tid;
	__s32 exit_code;
};

struct g2_lineage {
	spinlock_t lock;
	struct g2_task_refs *tasks;
	struct g2_event_ring *ring;
	struct g2_lineage_entry live[G2_LINEAGE_LIVE_MAX];
	struct g2_lineage_tombstone dead[G2_LINEAGE_TOMBSTONE_MAX];
	__u64 root_cookie;
	__u32 dead_count;
	struct task_struct *broker_task;
	__u64 broker_cookie;
	__u64 broker_start_ns;
	__u64 broker_exe_dev;
	__u64 broker_exe_ino;
	__u32 broker_tgid;
	__u32 broker_tid;
	bool broker_probe_watched;
	__u64 broker_watch_sequence;
	__u64 death_sequence;
};

static void g2_lineage_init(struct g2_lineage *lineage,
		struct g2_task_refs *tasks, struct g2_event_ring *ring)
{
	memset(lineage, 0, sizeof(*lineage));
	spin_lock_init(&lineage->lock);
	lineage->tasks = tasks;
	lineage->ring = ring;
}


/* task_lock pins the mm pointer; RCU pins the executable file dereference. */
static int g2_lineage_executable(struct task_struct *task, __u64 *dev, __u64 *ino)
{
	struct mm_struct *mm;
	struct file *file;
	struct inode *inode;
	int error = -ESRCH;

	task_lock(task);
	mm = task->mm;
	if (!mm)
		goto out_task;
	rcu_read_lock();
	file = rcu_dereference(mm->exe_file);
	if (file) {
		inode = file_inode(file);
		if (inode && inode->i_sb && inode->i_ino) {
			*dev = inode->i_sb->s_dev;
			*ino = inode->i_ino;
			error = 0;
		}
	}
	rcu_read_unlock();
out_task:
	task_unlock(task);
	return error;
}
static int g2_lineage_set_broker(struct g2_lineage *lineage,
		struct task_struct *task, __u64 cookie)
{
	unsigned long flags;
	__u64 dev = 0, ino = 0;
	int error;

	if (!task || !cookie)
		return -EINVAL;
	error = g2_lineage_executable(task, &dev, &ino);
	if (error)
		return error;
	spin_lock_irqsave(&lineage->lock, flags);
	if (lineage->broker_task) {
		spin_unlock_irqrestore(&lineage->lock, flags);
		return -EBUSY;
	}
	get_task_struct(task);
	lineage->broker_task = task;
	lineage->broker_cookie = cookie;
	lineage->broker_start_ns = task->start_boottime;
	lineage->broker_exe_dev = dev;
	lineage->broker_exe_ino = ino;
	lineage->broker_tgid = task_tgid_nr(task);
	lineage->broker_tid = task_pid_nr(task);
	spin_unlock_irqrestore(&lineage->lock, flags);
	return 0;
}

static int g2_lineage_emit(struct g2_lineage *lineage,
		const struct g2_lineage_entry *entry, __u32 kind, __s32 exit_code,
		__u64 *sequence)
{
	struct g2_guard_task_detail detail = {
		.tgid = entry->tgid,
		.tid = entry->tid,
		.exe_dev = entry->exe_dev,
		.exe_ino = entry->exe_ino,
		.exec_generation = entry->exec_generation,
		.exit_code = exit_code,
	};
	struct g2_guard_record record = {
		.kind = kind,
		.task_cookie = entry->cookie,
		.task_start_ns = entry->start_ns,
		.parent_cookie = entry->parent_cookie,
	};

	memcpy(record.detail, &detail, sizeof(detail));
	return g2_event_ring_append(lineage->ring, &record, sequence);
}

/* Existing broker PIN is required. Adds a separate reference for the watch. */
static int g2_lineage_watch(struct g2_lineage *lineage, __u64 cookie,
		__u64 *sequence)
{
	struct task_struct *task, *parent;
	struct g2_lineage_entry value = {}, *empty = NULL;
	unsigned long flags, task_flags;
	__u64 held_cookie;
	unsigned int i;
	int error;

	task = g2_task_refs_get(lineage->tasks, cookie);
	if (!task)
		return -ESRCH;
	value.task = task;
	value.cookie = cookie;
	value.tgid = task_tgid_nr(task);
	value.tid = task_pid_nr(task);
	value.start_ns = task->start_boottime;
	value.exec_generation = 1;
	if (!value.tgid || !value.tid || !value.start_ns ||
	    g2_lineage_executable(task, &value.exe_dev, &value.exe_ino)) {
		error = -ESRCH;
		goto out_task;
	}
	rcu_read_lock();
	parent = rcu_dereference(task->real_parent);
	if (parent)
		get_task_struct(parent);
	rcu_read_unlock();
	if (!parent) {
		error = -ESRCH;
		goto out_task;
	}
	spin_lock_irqsave(&lineage->lock, flags);
	if (lineage->root_cookie && lineage->root_cookie != cookie) {
		error = -EPERM;
		goto out_unlock;
	}
	for (i = 0; i < G2_LINEAGE_LIVE_MAX; i++) {
		struct g2_lineage_entry *entry = &lineage->live[i];

		if (entry->task == task) {
			if (entry->cookie != cookie || !entry->watch_sequence) {
				error = -EINVAL;
				goto out_unlock;
			}
			*sequence = entry->watch_sequence;
			error = 0;
			goto out_unlock;
		}
		if (!entry->task && !empty)
			empty = entry;
	}
	if (lineage->root_cookie) {
		error = -ESRCH;
		goto out_unlock;
	}
	if (!empty) {
		g2_event_ring_invalidate(lineage->ring);
		error = -ENOSPC;
		goto out_unlock;
	}
	spin_lock_irqsave(&lineage->tasks->lock, task_flags);
	error = g2_task_refs_insert_locked(lineage->tasks, parent,
			&value.parent_cookie, true);
	if (!error)
		error = g2_task_refs_insert_locked(lineage->tasks, task, &held_cookie, true);
	spin_unlock_irqrestore(&lineage->tasks->lock, task_flags);
	if (error || held_cookie != cookie) {
		g2_event_ring_invalidate(lineage->ring);
		if (!error)
			error = -EINVAL;
		goto out_unlock;
	}
	/* Install before publication; overflow permanently invalidates evidence. */
	*empty = value;
	lineage->root_cookie = cookie;
	error = g2_lineage_emit(lineage, empty, G2_GUARD_TASK_WATCH, 0,
			&empty->watch_sequence);
	if (!error)
		*sequence = empty->watch_sequence;
out_unlock:
	spin_unlock_irqrestore(&lineage->lock, flags);
	put_task_struct(parent);
out_task:
	put_task_struct(task);
	return error;
}
static int g2_lineage_watch_probe(struct g2_lineage *lineage,
		struct g2_guard_watch_probe *request)
{
	struct task_struct *task = NULL, *parent = NULL;
	struct g2_lineage_entry value = {}, *empty = NULL;
	unsigned long flags, task_flags;
	__u64 held_cookie;
	unsigned int i;
	int error;

	if (!request->cookie || request->pad || request->sequence ||
	    (request->probe_kind != 1 && request->probe_kind != 2))
		return -EINVAL;
	if (request->probe_kind == 2) {
		__u64 dev = 0, ino = 0;

		if (request->cookie != lineage->broker_cookie ||
		    current != lineage->broker_task ||
		    task_pid_nr(current) != task_tgid_nr(current))
			return -EPERM;
		if (g2_lineage_executable(current, &dev, &ino) ||
		    dev != lineage->broker_exe_dev ||
		    ino != lineage->broker_exe_ino)
			return -EPERM;
		spin_lock_irqsave(&lineage->lock, flags);
		if (lineage->broker_probe_watched) {
			spin_unlock_irqrestore(&lineage->lock, flags);
			return -EBUSY;
		}
		lineage->broker_probe_watched = true;
		lineage->broker_watch_sequence = READ_ONCE(lineage->ring->sequence);
		if (!lineage->broker_watch_sequence)
			lineage->broker_watch_sequence = 1;
		request->sequence = lineage->broker_watch_sequence;
		spin_unlock_irqrestore(&lineage->lock, flags);
		return 0;
	}
	if (request->cookie == lineage->broker_cookie)
		return -EPERM;
	task = g2_task_refs_get(lineage->tasks, request->cookie);
	if (!task)
		return -ESRCH;
	value.task = task;
	value.cookie = request->cookie;
	value.tgid = task_tgid_nr(task);
	value.tid = task_pid_nr(task);
	value.start_ns = task->start_boottime;
	value.exec_generation = 1;
	value.probe_kind = 1;
	if (!value.tgid || !value.tid || !value.start_ns ||
	    g2_lineage_executable(task, &value.exe_dev, &value.exe_ino)) {
		error = -ESRCH;
		goto out_probe_task;
	}
	rcu_read_lock();
	parent = rcu_dereference(task->real_parent);
	if (parent)
		get_task_struct(parent);
	rcu_read_unlock();
	if (!parent) {
		error = -ESRCH;
		goto out_probe_task;
	}
	spin_lock_irqsave(&lineage->lock, flags);
	for (i = 0; i < G2_LINEAGE_LIVE_MAX; i++) {
		struct g2_lineage_entry *entry = &lineage->live[i];

		if (entry->task == task) {
			if (request->probe_kind == 1 && entry->probe_kind == 0 &&
			    entry->cookie == request->cookie && entry->tgid == value.tgid &&
			    entry->tid == value.tid && entry->start_ns == value.start_ns &&
			    entry->exe_dev == value.exe_dev && entry->exe_ino == value.exe_ino) {
				entry->probe_kind = 1;
				request->sequence = entry->watch_sequence;
				error = 0;
			} else {
				error = -EBUSY;
			}
			goto out_probe_unlock;
		}
		if (!entry->task && !empty)
			empty = entry;
	}
	if (!empty || lineage->dead_count == G2_LINEAGE_TOMBSTONE_MAX) {
		g2_event_ring_invalidate(lineage->ring);
		error = -ENOSPC;
		goto out_probe_unlock;
	}
	spin_lock_irqsave(&lineage->tasks->lock, task_flags);
	error = g2_task_refs_insert_locked(lineage->tasks, parent,
			&value.parent_cookie, true);
	if (!error)
		error = g2_task_refs_insert_locked(lineage->tasks, task,
				&held_cookie, true);
	spin_unlock_irqrestore(&lineage->tasks->lock, task_flags);
	if (error || held_cookie != request->cookie) {
		g2_event_ring_invalidate(lineage->ring);
		if (!error)
			error = -EINVAL;
		goto out_probe_unlock;
	}
	*empty = value;
	error = g2_lineage_emit(lineage, empty, G2_GUARD_TASK_WATCH, 0,
			&empty->watch_sequence);
	if (!error)
		request->sequence = empty->watch_sequence;
out_probe_unlock:
	spin_unlock_irqrestore(&lineage->lock, flags);
	put_task_struct(parent);
out_probe_task:
	put_task_struct(task);
	return error;
}

static bool g2_lineage_watched_syscall_actor(struct g2_lineage *lineage,
		struct task_struct *task, __u64 *cookie, __u64 *start_ns,
		__u32 *tgid, __u32 *tid, __u64 *exe_dev, __u64 *exe_ino)
{
	unsigned long flags;
	unsigned int i;
	bool found = false;

	spin_lock_irqsave(&lineage->lock, flags);
	if (lineage->broker_probe_watched && lineage->broker_task == task) {
		*cookie = lineage->broker_cookie;
		*start_ns = lineage->broker_start_ns;
		*tgid = lineage->broker_tgid;
		*tid = lineage->broker_tid;
		*exe_dev = lineage->broker_exe_dev;
		*exe_ino = lineage->broker_exe_ino;
		found = true;
		goto out;
	}
	for (i = 0; i < G2_LINEAGE_LIVE_MAX; i++) {
		struct g2_lineage_entry *entry = &lineage->live[i];

		if (entry->task == task && entry->probe_kind == 1) {
			*cookie = entry->cookie;
			*start_ns = entry->start_ns;
			*tgid = entry->tgid;
			*tid = entry->tid;
			*exe_dev = entry->exe_dev;
			*exe_ino = entry->exe_ino;
			found = true;
			break;
		}
	}
out:
	spin_unlock_irqrestore(&lineage->lock, flags);
	return found;
}


/* sched_process_fork callback: the child has not yet been made runnable. */
static void g2_lineage_fork(void *data, struct task_struct *parent,
		struct task_struct *child)
{
	struct g2_lineage *lineage = data;
	struct g2_lineage_entry *source = NULL, *empty = NULL;
	struct g2_lineage_entry value = {};
	unsigned long flags, task_flags;
	unsigned int i;
	int error;

	spin_lock_irqsave(&lineage->lock, flags);
	for (i = 0; i < G2_LINEAGE_LIVE_MAX; i++) {
		if (lineage->live[i].task == parent)
			source = &lineage->live[i];
		if (!lineage->live[i].task && !empty)
			empty = &lineage->live[i];
	}
	if (!source)
		goto out;
	if (!empty || lineage->dead_count == G2_LINEAGE_TOMBSTONE_MAX) {
		g2_event_ring_invalidate(lineage->ring);
		goto out;
	}
	value.task = child;
	value.parent_cookie = source->cookie;
	value.start_ns = child->start_boottime;
	value.tgid = task_tgid_nr(child);
	value.tid = task_pid_nr(child);
	value.exe_dev = source->exe_dev;
	value.exe_ino = source->exe_ino;
	value.exec_generation = source->exec_generation;
	if (!value.start_ns || !value.tgid || !value.tid) {
		g2_event_ring_invalidate(lineage->ring);
		goto out;
	}
	spin_lock_irqsave(&lineage->tasks->lock, task_flags);
	error = g2_task_refs_insert_locked(lineage->tasks, child, &value.cookie, true);
	spin_unlock_irqrestore(&lineage->tasks->lock, task_flags);
	if (error) {
		g2_event_ring_invalidate(lineage->ring);
		goto out;
	}
	value.probe_kind = source->probe_kind;
	*empty = value;
	g2_lineage_emit(lineage, empty, G2_GUARD_TASK_FORK, 0, &empty->watch_sequence);
out:
	spin_unlock_irqrestore(&lineage->lock, flags);
}

static void g2_lineage_exec(void *data, struct task_struct *task,
		pid_t old_pid, struct linux_binprm *bprm)
{
	struct g2_lineage *lineage = data;
	__u64 dev = 0, ino = 0;
	unsigned long flags;
	unsigned int i;
	int error;

	(void)old_pid;
	(void)bprm;
	spin_lock_irqsave(&lineage->lock, flags);
	for (i = 0; i < G2_LINEAGE_LIVE_MAX; i++)
		if (lineage->live[i].task == task)
			break;
	spin_unlock_irqrestore(&lineage->lock, flags);
	if (i == G2_LINEAGE_LIVE_MAX)
		return;
	error = g2_lineage_executable(task, &dev, &ino);
	spin_lock_irqsave(&lineage->lock, flags);
	for (i = 0; i < G2_LINEAGE_LIVE_MAX; i++) {
		struct g2_lineage_entry *entry = &lineage->live[i];

		if (entry->task != task)
			continue;
		if (error || entry->exec_generation == U64_MAX) {
			g2_event_ring_invalidate(lineage->ring);
			break;
		}
		entry->tgid = task_tgid_nr(task);
		entry->tid = task_pid_nr(task);
		entry->start_ns = task->start_boottime;
		entry->exe_dev = dev;
		entry->exe_ino = ino;
		entry->exec_generation++;
		if (!entry->tgid || !entry->tid || !entry->start_ns) {
			g2_event_ring_invalidate(lineage->ring);
			break;
		}
		g2_lineage_emit(lineage, entry, G2_GUARD_TASK_EXEC, 0, NULL);
		break;
	}
	spin_unlock_irqrestore(&lineage->lock, flags);
}

static void g2_lineage_exit(void *data, struct task_struct *task)
{
	struct g2_lineage *lineage = data;
	unsigned long flags;
	__u64 cookie = 0;
	unsigned int i;

	spin_lock_irqsave(&lineage->lock, flags);
	if (lineage->broker_task == task && !lineage->death_sequence) {
		unsigned long ring_flags;
		struct g2_guard_task_detail detail = {
			.tgid = lineage->broker_tgid,
			.tid = lineage->broker_tid,
			.exe_dev = lineage->broker_exe_dev,
			.exe_ino = lineage->broker_exe_ino,
			.exec_generation = 1,
			.exit_code = task->exit_code,
		};
		struct g2_guard_record record = {
			.kind = G2_GUARD_BROKER_EXIT,
			.task_cookie = lineage->broker_cookie,
			.task_start_ns = lineage->broker_start_ns,
			.result = task->exit_code,
		};
		__u64 seq = 0;

		memcpy(record.detail, &detail, sizeof(detail));
		spin_lock_irqsave(&lineage->ring->lock, ring_flags);
		lineage->ring->broker_retired = true;
		g2_event_ring_lower_phase_locked(lineage->ring, 0);
		if (!g2_event_ring_append_locked(lineage->ring, &record, &seq))
			lineage->death_sequence = seq;
		spin_unlock_irqrestore(&lineage->ring->lock, ring_flags);
		wake_up_interruptible_poll(&lineage->ring->waiters, EPOLLIN | EPOLLERR);
	}
	for (i = 0; i < G2_LINEAGE_LIVE_MAX; i++) {
		struct g2_lineage_entry *entry = &lineage->live[i];
		struct g2_lineage_tombstone *dead;

		if (entry->task != task)
			continue;
		if (lineage->dead_count == G2_LINEAGE_TOMBSTONE_MAX) {
			g2_event_ring_invalidate(lineage->ring);
			break;
		}
		dead = &lineage->dead[lineage->dead_count++];
		dead->cookie = entry->cookie;
		dead->parent_cookie = entry->parent_cookie;
		dead->start_ns = entry->start_ns;
		dead->exe_dev = entry->exe_dev;
		dead->exe_ino = entry->exe_ino;
		dead->exec_generation = entry->exec_generation;
		dead->tgid = entry->tgid;
		dead->tid = entry->tid;
		dead->exit_code = task->exit_code;
		g2_lineage_emit(lineage, entry, G2_GUARD_TASK_EXIT,
				task->exit_code, NULL);
		cookie = entry->cookie;
		memset(entry, 0, sizeof(*entry));
		break;
	}
	spin_unlock_irqrestore(&lineage->lock, flags);
	if (cookie && g2_task_refs_drop_internal(lineage->tasks, cookie))
		g2_event_ring_invalidate(lineage->ring);
}

#endif
