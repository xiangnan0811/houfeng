/* SPDX-License-Identifier: GPL-2.0-only */
#ifndef G2_BPF_GUARD_TASK_REFS_H
#define G2_BPF_GUARD_TASK_REFS_H

#include <linux/errno.h>
#include <linux/fcntl.h>
#include <linux/file.h>
#include <linux/fdtable.h>
#include <linux/limits.h>
#include <linux/spinlock.h>
#include <linux/pid.h>
#include <linux/rcupdate.h>
#include <linux/sched.h>
#include <linux/sched/task.h>
#include <linux/types.h>
#include <linux/string.h>

#include "abi.h"

#define G2_GUARD_MAX_PINNED_TASKS 256

struct g2_task_ref {
	struct task_struct *task;
	__u64 cookie;
	__u32 pin_refs;
	__u32 internal_refs;
};

struct g2_task_refs {
	spinlock_t lock;
	struct g2_task_ref entries[G2_GUARD_MAX_PINNED_TASKS];
	__u64 next_cookie;
	__u64 broker_cookie;
};

static void g2_task_refs_init(struct g2_task_refs *refs)
{
	spin_lock_init(&refs->lock);
	memset(refs->entries, 0, sizeof(refs->entries));
	refs->next_cookie = 1;
	refs->broker_cookie = 0;
}

/* Caller holds lock. A new entry obtains its own task reference. */
static int g2_task_refs_insert_locked(struct g2_task_refs *refs,
		struct task_struct *task, __u64 *cookie, bool internal)
{
	struct g2_task_ref *empty = NULL;
	unsigned int i;

	for (i = 0; i < G2_GUARD_MAX_PINNED_TASKS; i++) {
		struct g2_task_ref *entry = &refs->entries[i];

		if (entry->task == task) {
			__u32 *count = internal ? &entry->internal_refs : &entry->pin_refs;

			if (*count == U32_MAX)
				return -EOVERFLOW;
			(*count)++;
			*cookie = entry->cookie;
			return 0;
		}
		if (!entry->task && !empty)
			empty = entry;
	}
	if (!empty)
		return -ENOSPC;
	if (!refs->next_cookie || refs->next_cookie == U64_MAX)
		return -EOVERFLOW;
	get_task_struct(task);
	empty->task = task;
	empty->cookie = refs->next_cookie++;
	if (internal)
		empty->internal_refs = 1;
	else
		empty->pin_refs = 1;
	*cookie = empty->cookie;
	return 0;
}

static int g2_task_refs_set_broker(struct g2_task_refs *refs,
		struct task_struct *task)
{
	unsigned long flags;
	__u64 cookie;
	int error;

	spin_lock_irqsave(&refs->lock, flags);
	if (refs->broker_cookie) {
		error = -EBUSY;
		goto out;
	}
	error = g2_task_refs_insert_locked(refs, task, &cookie, true);
	if (!error)
		refs->broker_cookie = cookie;
out:
	spin_unlock_irqrestore(&refs->lock, flags);
	return error;
}

static int g2_task_refs_pin(struct g2_task_refs *refs,
		struct g2_guard_pin_task *request)
{
	struct task_struct *task;
	struct pid *pid;
	__u64 cookie;
	__u32 tgid, tid;
	__u64 start;
	unsigned long flags;
	int error;

	if (!request->tid || request->tid > INT_MAX || request->pad ||
	    request->cookie || request->tgid || request->actual_tid ||
	    request->start_boottime_ns)
		return -EINVAL;
	pid = find_get_pid((pid_t)request->tid);
	if (!pid)
		return -ESRCH;
	task = get_pid_task(pid, PIDTYPE_PID);
	put_pid(pid);
	if (!task)
		return -ESRCH;
	tgid = task_tgid_nr(task);
	tid = task_pid_nr(task);
	start = task->start_boottime;
	if (!tgid || tid != request->tid || !start) {
		put_task_struct(task);
		return -ESRCH;
	}
	spin_lock_irqsave(&refs->lock, flags);
	error = g2_task_refs_insert_locked(refs, task, &cookie, false);
	spin_unlock_irqrestore(&refs->lock, flags);
	put_task_struct(task);
	if (error)
		return error;
	request->cookie = cookie;
	request->tgid = tgid;
	request->actual_tid = tid;
	request->start_boottime_ns = start;
	return 0;
}

/* Only an outstanding caller PIN authorizes lookup and descriptor import. */
static struct task_struct *g2_task_refs_get(struct g2_task_refs *refs,
		__u64 cookie)
{
	struct task_struct *task = NULL;
	unsigned int i;
	unsigned long flags;

	if (!cookie)
		return NULL;
	spin_lock_irqsave(&refs->lock, flags);
	for (i = 0; i < G2_GUARD_MAX_PINNED_TASKS; i++) {
		struct g2_task_ref *entry = &refs->entries[i];

		if (entry->task && entry->pin_refs && entry->cookie == cookie) {
			task = entry->task;
			get_task_struct(task);
			break;
		}
	}
	spin_unlock_irqrestore(&refs->lock, flags);
	return task;
}

static __u64 g2_task_refs_cookie(struct g2_task_refs *refs,
		struct task_struct *task)
{
	unsigned long flags;
	unsigned int i;
	__u64 cookie = 0;

	spin_lock_irqsave(&refs->lock, flags);
	for (i = 0; i < G2_GUARD_MAX_PINNED_TASKS; i++) {
		if (refs->entries[i].task == task) {
			cookie = refs->entries[i].cookie;
			break;
		}
	}
	spin_unlock_irqrestore(&refs->lock, flags);
	return cookie;
}

static int g2_task_refs_import(struct g2_task_refs *refs,
		struct g2_guard_import_fd *request)
{
	struct task_struct *task;
	struct file *file;
	unsigned int actual;
	int installed;

	if (!request->cookie || request->pad || request->installed_fd ||
	    request->output_pad || request->fd > INT_MAX)
		return -EINVAL;
	task = g2_task_refs_get(refs, request->cookie);
	if (!task)
		return -ESRCH;
	actual = request->fd;
	rcu_read_lock();
	file = task_lookup_next_fdget_rcu(task, &actual);
	rcu_read_unlock();
	put_task_struct(task);
	if (!file)
		return -EBADF;
	if (actual != request->fd) {
		fput(file);
		return -EBADF;
	}
	installed = receive_fd(file, NULL, O_CLOEXEC);
	fput(file);
	if (installed < 0)
		return installed;
	request->installed_fd = installed;
	return 0;
}

static int g2_task_refs_drop_kind(struct g2_task_refs *refs, __u64 cookie,
		bool internal)
{
	struct task_struct *task = NULL;
	unsigned int i;
	unsigned long flags;
	int error = -ESRCH;

	if (!cookie)
		return -EINVAL;
	spin_lock_irqsave(&refs->lock, flags);
	for (i = 0; i < G2_GUARD_MAX_PINNED_TASKS; i++) {
		struct g2_task_ref *entry = &refs->entries[i];

		if (!entry->task || entry->cookie != cookie)
			continue;
		__u32 *count = internal ? &entry->internal_refs : &entry->pin_refs;

		if (!*count || (internal && cookie == refs->broker_cookie && *count == 1)) {
			error = -EINVAL;
			break;
		}
		(*count)--;
		if (!entry->pin_refs && !entry->internal_refs) {
			task = entry->task;
			entry->task = NULL;
			entry->cookie = 0;
		}
		error = 0;
		break;
	}
	spin_unlock_irqrestore(&refs->lock, flags);
	if (task)
		put_task_struct(task);
	return error;
}

static int g2_task_refs_drop(struct g2_task_refs *refs, __u64 cookie)
{
	return g2_task_refs_drop_kind(refs, cookie, false);
}

static int g2_task_refs_drop_internal(struct g2_task_refs *refs, __u64 cookie)
{
	return g2_task_refs_drop_kind(refs, cookie, true);
}


#endif
