// SPDX-License-Identifier: GPL-2.0-only
#include <linux/anon_inodes.h>
#include <linux/bpf.h>
#include <linux/capability.h>
#include <linux/errno.h>
#include <linux/file.h>
#include <linux/fs.h>
#include <linux/miscdevice.h>
#include <linux/module.h>
#include <linux/mutex.h>
#include <linux/random.h>
#include <linux/slab.h>
#include <linux/string.h>
#include <linux/tracepoint.h>
#include <linux/uaccess.h>
#include <crypto/sha2.h>
#include "abi.h"
#include "early_boot.h"
#include "event_ring.h"
#include "task_refs.h"
#include "lineage.h"
#include "id_probes.h"
#include "cgroup_refs.h"
#include "audit_probe.h"

MODULE_LICENSE("GPL");
MODULE_DESCRIPTION("G2 isolated guest BPF authority guard");
MODULE_IMPORT_NS(BPF_INTERNAL);

#define G2_GUARD_UNSEALED 0U
#define G2_GUARD_SEALED 1U
#define G2_GUARD_DEAD 2U

struct g2_guard {
	struct mutex lifecycle;
	struct miscdevice device;
	struct g2_boot_identity boot;
	struct g2_observer_addresses observer;
	__u8 observer_bindings_sha256[32];
	struct task_struct *broker;
	struct file *control;
	struct task_struct *observer_task;
	struct file *observer_file;
	__u64 observer_cookie;
	struct bpf_link *links[3];
	__u32 link_ids[3];
	__u32 program_ids[3];
	__u8 epoch[16];
	__u8 bootstrap_digest[32];
	__u32 state;
	bool ever_opened;
	bool tracepoints_registered;
	bool fault_injected;
	bool fault_probe_unregistered;
	struct tracepoint *fork_tracepoint;
	struct tracepoint *exec_tracepoint;
	struct tracepoint *exit_tracepoint;
	struct g2_event_ring ring;
	struct g2_task_refs tasks;
	struct g2_lineage lineage;
	struct g2_id_probe_set ids;
	struct g2_cgroup_refs cgroups;
	struct g2_audit_probe audit;
};

static struct g2_guard guard;

static bool g2_control_owner(struct g2_guard *g, struct file *file)
{
	return READ_ONCE(g->control) == file && READ_ONCE(g->broker) == current &&
	       READ_ONCE(g->state) != G2_GUARD_DEAD;
}

static bool g2_observer_owner(struct g2_guard *g, struct file *file)
{
	return READ_ONCE(g->observer_file) == file &&
	       READ_ONCE(g->observer_task) == current;
}
static int g2_guard_open(struct inode *inode, struct file *file)
{
	struct g2_guard *g = &guard;
	int error;

	(void)inode;
	if (!capable(CAP_SYS_ADMIN) || task_pid_nr(current) != task_tgid_nr(current))
		return -EPERM;
	error = g2_boot_verify_broker(&g->boot);
	if (error)
		return error;
	mutex_lock(&g->lifecycle);
	if (g->ever_opened || g->state != G2_GUARD_UNSEALED) {
		error = -EBUSY;
		goto out;
	}
	error = g2_task_refs_set_broker(&g->tasks, current);
	if (error)
		goto out;
	error = g2_lineage_set_broker(&g->lineage, current, g->tasks.broker_cookie);
	if (error)
		goto out;
	g->ring.broker_cookie = g->tasks.broker_cookie;
	g->ring.broker_start_ns = current->start_boottime;
	g->ever_opened = true;
	g->broker = current;
	g->control = file;
	file->private_data = g;
out:
	mutex_unlock(&g->lifecycle);
	return error;
}

static void g2_tracepoints_unregister(struct g2_guard *g)
{
	if (!g->tracepoints_registered)
		return;
	tracepoint_probe_unregister(g->exit_tracepoint, g2_lineage_exit, &g->lineage);
	tracepoint_probe_unregister(g->exec_tracepoint, g2_lineage_exec, &g->lineage);
	tracepoint_probe_unregister(g->fork_tracepoint, g2_lineage_fork, &g->lineage);
	tracepoint_synchronize_unregister();
	g->tracepoints_registered = false;
}

static int g2_guard_release(struct inode *inode, struct file *file)
{
	struct g2_guard *g = file->private_data;
	unsigned long flags;

	(void)inode;
	mutex_lock(&g->lifecycle);
	if (g->control == file) {
		WRITE_ONCE(g->state, G2_GUARD_DEAD);
		WRITE_ONCE(g->control, NULL);
		WRITE_ONCE(g->broker, NULL);
		spin_lock_irqsave(&g->ring.lock, flags);
		g->ring.broker_retired = true;
		g2_event_ring_lower_phase_locked(&g->ring, 0);
		spin_unlock_irqrestore(&g->ring.lock, flags);
		wake_up_interruptible_poll(&g->ring.waiters, EPOLLIN | EPOLLERR);
	}
	mutex_unlock(&g->lifecycle);
	return 0;
}

static bool g2_header_valid(const struct g2_guard_header *header)
{
	return header->version == G2_GUARD_VERSION && !header->reserved;
}

#if defined(CONFIG_MODULE_FORCE_UNLOAD) || defined(CONFIG_MODULE_FORCE_LOAD)
#error "G2 guard requires force loading and force unloading disabled"
#endif

static __u64 g2_probe_missed(struct g2_guard *g);

/* Caller holds the ring lock; no program pointer is exported. */
static bool g2_program_observed(struct g2_guard *g, struct bpf_prog *program)
{
	unsigned int i;

	for (i = 0; i < G2_ID_OBJECT_CAPACITY; i++) {
		struct g2_id_object *object = &g->ids.objects[i];

		if (object->pointer == program && object->class == G2_ID_PROGRAM &&
		    object->state == G2_ID_LIVE && object->id == program->aux->id &&
		    object->publisher_cookie == g->tasks.broker_cookie)
			return true;
	}
	return false;
}

static bool g2_link_currently_attached(struct bpf_link *link, const char *hook)
{
	struct bpf_tracing_link *tracing;
	struct bpf_prog *program;
	struct bpf_trampoline *tr;
	struct bpf_tramp_link *pos;
	bool found = false;

	if (!link || link->type != BPF_LINK_TYPE_TRACING || !link->id)
		return false;
	program = READ_ONCE(link->prog);
	if (!program || program->type != BPF_PROG_TYPE_LSM || !program->aux ||
	    !program->aux->id || !program->aux->attach_btf_id ||
	    !program->aux->attach_func_name ||
	    strcmp(program->aux->attach_func_name, hook))
		return false;
	tracing = container_of(link, struct bpf_tracing_link, link.link);
	tr = READ_ONCE(tracing->trampoline);
	if (tracing->attach_type != BPF_LSM_MAC || !tr || tracing->tgt_prog ||
	    hlist_unhashed(&tracing->link.tramp_hlist))
		return false;
	hlist_for_each_entry(pos, &tr->progs_hlist[BPF_TRAMP_MODIFY_RETURN], tramp_hlist) {
		if (pos == &tracing->link) {
			found = true;
			break;
		}
	}
	return found;
}

static int g2_verify_program_maps(struct g2_guard *g, struct bpf_prog *prog,
		unsigned int hook_index, __u64 **liveness_slot_out)
{
	struct bpf_prog_aux *aux;
	struct bpf_map **used;
	__u32 cnt, i, want_cnt;
	bool seen_token = false, seen_liveness = false;
	bool seen_targets = false, seen_retire = false;
	__u64 *slot = NULL;

	(void)g;
	if (!prog || !prog->aux || hook_index > 2)
		return -EINVAL;
	aux = prog->aux;
	want_cnt = hook_index == 2 ? 1U : 2U;
	cnt = READ_ONCE(aux->used_map_cnt);
	used = READ_ONCE(aux->used_maps);
	if (!used || cnt != want_cnt)
		return -EINVAL;
	for (i = 0; i < cnt; i++) {
		struct bpf_map *m = READ_ONCE(used[i]);

		if (!m || !m->ops)
			return -EINVAL;
		if (!strcmp(m->name, "broker_token")) {
			if (hook_index != 0 || seen_token ||
			    m->map_type != BPF_MAP_TYPE_TASK_STORAGE ||
			    m->key_size != 4 || m->value_size != 8 ||
			    !READ_ONCE(m->frozen))
				return -EINVAL;
			seen_token = true;
		} else if (!strcmp(m->name, "guard_liveness")) {
			__u32 zero_key = 0;
			__u64 *val;

			if (hook_index != 0 || seen_liveness ||
			    m->map_type != BPF_MAP_TYPE_ARRAY ||
			    m->key_size != 4 || m->value_size != 8 ||
			    m->max_entries != 1 || m->map_flags != 0 ||
			    !READ_ONCE(m->frozen) || !m->ops->map_lookup_elem)
				return -EINVAL;
			val = m->ops->map_lookup_elem(m, &zero_key);
			if (!val || READ_ONCE(*val) != 1ULL)
				return -EINVAL;
			slot = val;
			seen_liveness = true;
		} else if (!strcmp(m->name, "realm_targets")) {
			if (hook_index == 0 || seen_targets ||
			    m->map_type != BPF_MAP_TYPE_HASH ||
			    m->key_size != 16 || m->value_size != 24 ||
			    m->max_entries != 128 || m->map_flags != 0)
				return -EINVAL;
			seen_targets = true;
		} else if (!strcmp(m->name, "retire_tasks")) {
			if (hook_index != 1 || seen_retire ||
			    m->map_type != BPF_MAP_TYPE_HASH ||
			    m->key_size != 32 || m->value_size != 40 ||
			    m->max_entries != 4096 || m->map_flags != 0)
				return -EINVAL;
			seen_retire = true;
		} else {
			return -EINVAL;
		}
	}
	if (hook_index == 0) {
		if (!seen_token || !seen_liveness || !slot)
			return -EINVAL;
		if (liveness_slot_out)
			*liveness_slot_out = slot;
		return 0;
	}
	if (hook_index == 1)
		return (seen_targets && seen_retire) ? 0 : -EINVAL;
	return seen_targets ? 0 : -EINVAL;
}
static int g2_guard_seal(struct g2_guard *g, struct g2_guard_seal *request)
{
	static const char * const hooks[3] = {
		"bpf_lsm_bpf", "bpf_lsm_inode_rmdir", "bpf_lsm_inode_mkdir"
	};
	struct bpf_link *links[3] = {};
	__u8 epoch[16];
	unsigned long flags;
	__u64 *liveness_slot = NULL;
	unsigned int i, j;
	int error = -EINVAL;

	if (request->count != 3 || !memchr_inv(request->bootstrap_digest, 0, 32) ||
	    memchr_inv(request->epoch, 0, sizeof(request->epoch)) ||
	    memchr_inv(request->link_ids, 0, sizeof(request->link_ids)) ||
	    memchr_inv(request->program_ids, 0, sizeof(request->program_ids)))
		return -EINVAL;
	error = g2_boot_verify_broker(&g->boot);
	if (error)
		return error;
	error = -EINVAL;
	for (i = 0; i < 3; i++) {
		if (request->link_fds[i] < 0)
			goto out;
		for (j = 0; j < i; j++)
			if (request->link_fds[i] == request->link_fds[j])
				goto out;
		links[i] = bpf_link_get_from_fd(request->link_fds[i]);
		if (IS_ERR(links[i])) {
			error = PTR_ERR(links[i]);
			links[i] = NULL;
			goto out;
		}
		if (!g2_link_currently_attached(links[i], hooks[i]))
			goto out;
		for (j = 0; j < i; j++)
			if (links[i] == links[j] || links[i]->prog == links[j]->prog)
				goto out;
	}
	get_random_bytes(epoch, sizeof(epoch));
	if (!memchr_inv(epoch, 0, sizeof(epoch))) {
		error = -EIO;
		goto out;
	}
	mutex_lock(&g->lifecycle);
	if (g->state != G2_GUARD_UNSEALED || g->broker != current || !g->control ||
	    !g->observer_file || !g->observer_cookie) {
		error = -EPERM;
		goto out_unlock;
	}
	if (g2_verify_program_maps(g, links[0]->prog, 0, &liveness_slot) ||
	    g2_verify_program_maps(g, links[1]->prog, 1, NULL) ||
	    g2_verify_program_maps(g, links[2]->prog, 2, NULL)) {
		error = -EINVAL;
		goto out_unlock;
	}
	spin_lock_irqsave(&g->ring.lock, flags);
	if (g->ring.pending_pairs || g->ring.sticky || g2_probe_missed(g)) {
		error = g->ring.pending_pairs ? -EAGAIN : -EIO;
		goto out_ring;
	}
	for (i = 0; i < 3; i++) {
		if (!g2_program_observed(g, links[i]->prog)) {
			error = -EPERM;
			goto out_ring;
		}
	}
	if (!try_module_get(THIS_MODULE)) {
		error = -ENODEV;
		goto out_ring;
	}
	memcpy(g->epoch, epoch, sizeof(epoch));
	/* The trusted early broker independently verifies these manifest bytes. */
	memcpy(g->bootstrap_digest, request->bootstrap_digest, sizeof(g->bootstrap_digest));
	memcpy(request->epoch, epoch, sizeof(epoch));
	for (i = 0; i < 3; i++) {
		g->links[i] = links[i];
		g->link_ids[i] = links[i]->id;
		g->program_ids[i] = links[i]->prog->aux->id;
		request->link_ids[i] = links[i]->id;
		request->program_ids[i] = links[i]->prog->aux->id;
		links[i] = NULL;
	}
	g->ring.liveness_slot = liveness_slot;
	g->ring.sealed = true;
	WRITE_ONCE(g->state, G2_GUARD_SEALED);
	error = 0;
out_ring:
	spin_unlock_irqrestore(&g->ring.lock, flags);
out_unlock:
	mutex_unlock(&g->lifecycle);
out:
	for (i = 0; i < 3; i++)
		if (links[i])
			bpf_link_put(links[i]);
	return error;
}

static __u64 g2_probe_missed(struct g2_guard *g)
{
	__u64 missed = 0, value;
	unsigned int i;

#define G2_COUNT_MISSED(expr) do { \
	value = (__u64)READ_ONCE(expr); \
	if (U64_MAX - missed < value) missed = U64_MAX; \
	else missed += value; \
} while (0)
	for (i = 0; i < ARRAY_SIZE(g->ids.probes); i++) {
		G2_COUNT_MISSED(g->ids.probes[i].probe.nmissed);
		G2_COUNT_MISSED(g->ids.probes[i].probe.kp.nmissed);
	}
	G2_COUNT_MISSED(g->cgroups.offline.nmissed);
	G2_COUNT_MISSED(g->cgroups.release.nmissed);
	G2_COUNT_MISSED(g->cgroups.release.kp.nmissed);
	G2_COUNT_MISSED(g->audit.probe.nmissed);
	G2_COUNT_MISSED(g->audit.probe.kp.nmissed);
	G2_COUNT_MISSED(g->audit.start_probe.nmissed);
	G2_COUNT_MISSED(g->audit.start_probe.kp.nmissed);
	G2_COUNT_MISSED(g->audit.sys_bpf_probe.nmissed);
	G2_COUNT_MISSED(g->audit.sys_bpf_probe.kp.nmissed);
#undef G2_COUNT_MISSED
	return missed;
}

static int g2_guard_status(struct g2_guard *g, struct g2_guard_status *request)
{
	static const char * const hooks[3] = {
		"bpf_lsm_bpf", "bpf_lsm_inode_rmdir", "bpf_lsm_inode_mkdir"
	};
	unsigned long flags;
	__u64 missed;
	unsigned int i;
	int error = 0;

	if (memchr_inv((char *)request + sizeof(request->header), 0,
			sizeof(*request) - sizeof(request->header)))
		return -EINVAL;
	if (!g2_boot_module_still_valid(&g->boot))
		g2_event_ring_invalidate(&g->ring);
	spin_lock_irqsave(&g->ring.lock, flags);
	missed = g2_probe_missed(g);
	if (missed)
		g2_event_ring_invalidate_locked(&g->ring);
	if (g->ring.pending_pairs) {
		error = -EAGAIN;
		goto out;
	}
	memcpy(request->epoch, g->epoch, sizeof(request->epoch));
	request->state = READ_ONCE(g->state);
	request->sticky = g->ring.sticky;
	request->authority_phase = g->ring.authority_phase;
	request->pad = 0;
	request->sequence = g->ring.sequence;
	request->ack_cursor = g->ring.ack_cursor;
	request->observer_ack_cursor = g->ring.observer_ack_cursor;
	request->lost = g->ring.lost;
	request->probe_missed = missed;
	request->broker_cookie = g->tasks.broker_cookie;
	request->observer_cookie = g->observer_cookie;
	request->death_sequence = READ_ONCE(g->lineage.death_sequence);
	request->module_instance = g->boot.module_instance;
	request->module_dev = g->boot.module_dev;
	request->module_ino = g->boot.module_ino;
	request->module_size = g->boot.module_size;
	memcpy(request->link_ids, g->link_ids, sizeof(request->link_ids));
	memcpy(request->program_ids, g->program_ids, sizeof(request->program_ids));
	for (i = 0; i < 3; i++) {
		request->link_attached[i] =
			(g->links[i] && g2_link_currently_attached(g->links[i], hooks[i])) ? 1U : 0U;
	}
	request->tail_pad = 0;
	memcpy(request->bootstrap_digest, g->bootstrap_digest, sizeof(request->bootstrap_digest));
	memcpy(request->observer_bindings_sha256, g->observer_bindings_sha256,
			sizeof(request->observer_bindings_sha256));
out:
	spin_unlock_irqrestore(&g->ring.lock, flags);
	return error;
}

static int g2_observer_release(struct inode *inode, struct file *file)
{
	struct g2_guard *g = file->private_data;

	(void)inode;
	if (!g)
		return 0;
	mutex_lock(&g->lifecycle);
	if (g->observer_file == file) {
		WRITE_ONCE(g->observer_file, NULL);
		WRITE_ONCE(g->observer_task, NULL);
		g2_event_ring_invalidate(&g->ring);
	}
	mutex_unlock(&g->lifecycle);
	return 0;
}

static long g2_observer_ioctl(struct file *file, unsigned int command, unsigned long arg)
{
	struct g2_guard *g = file->private_data;
	void __user *user = (void __user *)arg;
	struct g2_guard_header *header;
	void *buffer;
	size_t size;
	int error;

	if (!g || !g2_observer_owner(g, file))
		return -EPERM;
	switch (command) {
	case G2_GUARD_STATUS: size = sizeof(struct g2_guard_status); break;
	case G2_GUARD_READ_EVENTS: size = sizeof(struct g2_guard_read_events); break;
	default: return -EPERM;
	}
	buffer = kvzalloc(size, GFP_KERNEL);
	if (!buffer)
		return -ENOMEM;
	if (copy_from_user(buffer, user, size)) {
		error = -EFAULT;
		goto out;
	}
	header = buffer;
	if (!g2_header_valid(header)) {
		error = -EINVAL;
		goto out;
	}
	if (g2_probe_missed(g))
		g2_event_ring_invalidate(&g->ring);
	if (command == G2_GUARD_STATUS)
		error = g2_guard_status(g, buffer);
	else
		error = g2_event_ring_prepare_role(&g->ring, buffer, true);
	if (!error && copy_to_user(user, buffer, size))
		error = -EFAULT;
	if (!error && command == G2_GUARD_READ_EVENTS)
		g2_event_ring_returned_role(&g->ring, buffer, true);
out:
	kvfree(buffer);
	return error;
}

static __poll_t g2_observer_poll(struct file *file, poll_table *wait)
{
	struct g2_guard *g = file->private_data;

	if (!g || !g2_observer_owner(g, file))
		return EPOLLERR;
	if (g2_probe_missed(g))
		g2_event_ring_invalidate(&g->ring);
	return g2_event_ring_poll_role(&g->ring, file, wait, true);
}

#ifdef CONFIG_COMPAT
static long g2_guard_compat_ioctl(struct file *file, unsigned int command,
		unsigned long arg)
{
	(void)file;
	(void)command;
	(void)arg;
	return -EINVAL;
}
#endif
static const struct file_operations g2_observer_fops = {
	.owner = THIS_MODULE,
	.release = g2_observer_release,
	.unlocked_ioctl = g2_observer_ioctl,
#ifdef CONFIG_COMPAT
	.compat_ioctl = g2_guard_compat_ioctl,
#endif
	.poll = g2_observer_poll,
	.llseek = no_llseek,
};

static int g2_guard_bind_observer(struct g2_guard *g,
		struct g2_guard_bind_observer *request)
{
	struct task_struct *task = NULL;
	struct file *obs_file = NULL;
	unsigned long flags, task_flags;
	__u64 held_cookie = 0;
	int fd = -1, error;

	if (!request->cookie || request->installed_fd || request->pad ||
	    request->cookie == g->tasks.broker_cookie)
		return -EINVAL;
	error = g2_boot_verify_broker(&g->boot);
	if (error)
		return error;
	task = g2_task_refs_get(&g->tasks, request->cookie);
	if (!task)
		return -ESRCH;
	error = g2_boot_verify_observer_task(&g->boot, task);
	if (error)
		goto out_task;
	mutex_lock(&g->lifecycle);
	if (g->state != G2_GUARD_UNSEALED || g->observer_file || g->observer_cookie ||
	    g->broker != current || !g->control) {
		error = -EBUSY;
		goto out_unlock;
	}
	spin_lock_irqsave(&g->tasks.lock, task_flags);
	error = g2_task_refs_insert_locked(&g->tasks, task, &held_cookie, true);
	spin_unlock_irqrestore(&g->tasks.lock, task_flags);
	if (error || held_cookie != request->cookie) {
		if (!error)
			error = -EINVAL;
		goto out_unlock;
	}
	obs_file = anon_inode_getfile("[g2_bpf_observer]", &g2_observer_fops, g,
			O_RDWR | O_CLOEXEC);
	if (IS_ERR(obs_file)) {
		error = PTR_ERR(obs_file);
		obs_file = NULL;
		goto out_unlock;
	}
	fd = receive_fd(obs_file, NULL, O_CLOEXEC);
	fput(obs_file);
	if (fd < 0) {
		error = fd;
		goto out_unlock;
	}
	get_task_struct(task);
	WRITE_ONCE(g->observer_task, task);
	WRITE_ONCE(g->observer_file, obs_file);
	g->observer_cookie = request->cookie;
	spin_lock_irqsave(&g->ring.lock, flags);
	g->ring.observer_bound = true;
	spin_unlock_irqrestore(&g->ring.lock, flags);
	request->installed_fd = fd;
	error = 0;
out_unlock:
	mutex_unlock(&g->lifecycle);
out_task:
	put_task_struct(task);
	return error;
}

static int g2_guard_revoke(struct g2_guard *g, struct g2_guard_revoke *request)
{
	unsigned long flags;

	(void)request;
	spin_lock_irqsave(&g->ring.lock, flags);
	if (g->ring.authority_phase != 1) {
		spin_unlock_irqrestore(&g->ring.lock, flags);
		return -EBUSY;
	}
	g2_event_ring_lower_phase_locked(&g->ring, 2);
	spin_unlock_irqrestore(&g->ring.lock, flags);
	wake_up_interruptible_poll(&g->ring.waiters, EPOLLIN | EPOLLERR);
	return 0;
}

static int g2_guard_observation_fault(struct g2_guard *g,
		struct g2_guard_selftest_observation_fault *request)
{
	unsigned long flags;

	if (request->pad || request->variant < 1 || request->variant > 3)
		return -EINVAL;
	mutex_lock(&g->lifecycle);
	if (g->state != G2_GUARD_SEALED || g->fault_injected) {
		mutex_unlock(&g->lifecycle);
		return -EBUSY;
	}
	g->fault_injected = true;
	if (request->variant == 1) {
		if (!g->fault_probe_unregistered) {
			unregister_kretprobe(&g->audit.probe);
			g->fault_probe_unregistered = true;
		}
		spin_lock_irqsave(&g->ring.lock, flags);
		g2_event_ring_invalidate_locked(&g->ring);
		spin_unlock_irqrestore(&g->ring.lock, flags);
		wake_up_interruptible_poll(&g->ring.waiters, EPOLLIN | EPOLLERR);
	} else if (request->variant == 2) {
		spin_lock_irqsave(&g->ring.lock, flags);
		g->ring.fault_drop_one_pair = true;
		g2_event_ring_lower_phase_locked(&g->ring, 2);
		spin_unlock_irqrestore(&g->ring.lock, flags);
		wake_up_interruptible_poll(&g->ring.waiters, EPOLLIN | EPOLLERR);
	} else if (request->variant == 3) {
		spin_lock_irqsave(&g->ring.lock, flags);
		g->ring.fault_hold_ack = true;
		spin_unlock_irqrestore(&g->ring.lock, flags);
	}
	mutex_unlock(&g->lifecycle);
	return 0;
}

static int g2_guard_selftest_id_reuse(struct g2_guard *g,
		struct g2_guard_selftest_id_reuse *request)
{
	struct g2_id_probe_set *set = &g->ids;
	unsigned long flags;
	int error = -EINVAL;

	if (!request->first_object_cookie || !request->first_removal_sequence ||
	    !request->next_transaction_id || !memchr_inv(request->nonce, 0, sizeof(request->nonce)))
		return -EINVAL;
	spin_lock_irqsave(&g->ring.lock, flags);
	if (READ_ONCE(g->state) != G2_GUARD_SEALED || g->ring.authority_phase != 1 ||
	    set->id_reuse_used || set->id_reuse_count != 1 || !set->first_removed ||
	    set->first_cookie != request->first_object_cookie ||
	    set->first_removal_sequence != request->first_removal_sequence ||
	    !set->removed_id || g2_id_reuse_live(set, set->removed_id))
		goto out;
	rcu_read_lock();
	if (idr_find((struct idr *)set->binding->map_idr, set->removed_id)) {
		rcu_read_unlock();
		goto out;
	}
	rcu_read_unlock();
	set->id_reuse_used = true;
	set->next_transaction_id = request->next_transaction_id;
	memcpy(set->nonce, request->nonce, sizeof(set->nonce));
	error = g2_id_cursor_record(set, 1, 0, 0, 0);
	if (!error)
		set->id_reuse_armed = true;
out:
	spin_unlock_irqrestore(&g->ring.lock, flags);
	wake_up_interruptible_poll(&g->ring.waiters, EPOLLIN | EPOLLERR);
	return error;
}

static long g2_guard_ioctl(struct file *file, unsigned int command, unsigned long arg)
{
	struct g2_guard *g = file->private_data;
	void __user *user = (void __user *)arg;
	struct g2_guard_header *header;
	void *buffer;
	size_t size;
	int error;

	if (!g || !g2_control_owner(g, file))
		return -EPERM;
	switch (command) {
	case G2_GUARD_SEAL: size = sizeof(struct g2_guard_seal); break;
	case G2_GUARD_STATUS: size = sizeof(struct g2_guard_status); break;
	case G2_GUARD_PIN_TASK: size = sizeof(struct g2_guard_pin_task); break;
	case G2_GUARD_IMPORT_FD: size = sizeof(struct g2_guard_import_fd); break;
	case G2_GUARD_DROP_TASK: size = sizeof(struct g2_guard_drop_task); break;
	case G2_GUARD_ENROLL_CGROUP: size = sizeof(struct g2_guard_enroll_cgroup); break;
	case G2_GUARD_RETIRE_CGROUP: size = sizeof(struct g2_guard_retire_cgroup); break;
	case G2_GUARD_READ_EVENTS: size = sizeof(struct g2_guard_read_events); break;
	case G2_GUARD_WATCH_DAEMON: size = sizeof(struct g2_guard_watch_daemon); break;
	case G2_GUARD_BIND_OBSERVER: size = sizeof(struct g2_guard_bind_observer); break;
	case G2_GUARD_REVOKE: size = sizeof(struct g2_guard_revoke); break;
	case G2_GUARD_WATCH_PROBE: size = sizeof(struct g2_guard_watch_probe); break;
	case G2_GUARD_SELFTEST_OBSERVATION_FAULT:
		size = sizeof(struct g2_guard_selftest_observation_fault);
		break;
	case G2_GUARD_SELFTEST_ID_REUSE:
		size = sizeof(struct g2_guard_selftest_id_reuse);
		break;
	default: return -EINVAL;
	}
	buffer = kvzalloc(size, GFP_KERNEL);
	if (!buffer)
		return -ENOMEM;
	if (copy_from_user(buffer, user, size)) {
		error = -EFAULT;
		goto out;
	}
	header = buffer;
	if (!g2_header_valid(header)) {
		error = -EINVAL;
		goto out;
	}
	if (command == G2_GUARD_BIND_OBSERVER) {
		if (READ_ONCE(g->state) != G2_GUARD_UNSEALED) {
			error = -EPERM;
			goto out;
		}
	} else if (command != G2_GUARD_SEAL && command != G2_GUARD_STATUS &&
	    command != G2_GUARD_PIN_TASK && command != G2_GUARD_READ_EVENTS &&
	    READ_ONCE(g->state) != G2_GUARD_SEALED) {
		error = -EPERM;
		goto out;
	}
	if (g2_probe_missed(g))
		g2_event_ring_invalidate(&g->ring);
	switch (command) {
	case G2_GUARD_SEAL:
		error = g2_guard_seal(g, buffer);
		break;
	case G2_GUARD_STATUS:
		error = g2_guard_status(g, buffer);
		break;
	case G2_GUARD_PIN_TASK:
		error = g2_task_refs_pin(&g->tasks, buffer);
		break;
	case G2_GUARD_IMPORT_FD:
		error = g2_task_refs_import(&g->tasks, buffer);
		break;
	case G2_GUARD_DROP_TASK:
		error = g2_task_refs_drop(&g->tasks, ((struct g2_guard_drop_task *)buffer)->cookie);
		break;
	case G2_GUARD_ENROLL_CGROUP:
		error = g2_cgroup_refs_enroll(&g->cgroups, buffer);
		break;
	case G2_GUARD_RETIRE_CGROUP:
		error = g2_cgroup_refs_retire(&g->cgroups, buffer);
		break;
	case G2_GUARD_READ_EVENTS:
		error = g2_event_ring_prepare_role(&g->ring, buffer, false);
		break;
	case G2_GUARD_WATCH_DAEMON: {
		struct g2_guard_watch_daemon *request = buffer;

		if (!request->cookie || request->sequence)
			error = -EINVAL;
		else
			error = g2_lineage_watch(&g->lineage, request->cookie, &request->sequence);
		break;
	}
	case G2_GUARD_BIND_OBSERVER:
		error = g2_guard_bind_observer(g, buffer);
		break;
	case G2_GUARD_REVOKE:
		error = g2_guard_revoke(g, buffer);
		break;
	case G2_GUARD_WATCH_PROBE:
		error = g2_lineage_watch_probe(&g->lineage, buffer);
		break;
	case G2_GUARD_SELFTEST_OBSERVATION_FAULT:
		error = g2_guard_observation_fault(g, buffer);
		break;
	case G2_GUARD_SELFTEST_ID_REUSE:
		error = g2_guard_selftest_id_reuse(g, buffer);
		break;
	default:
		error = -EINVAL;
	}
	if (!error && copy_to_user(user, buffer, size))
		error = -EFAULT;
	if (!error && command == G2_GUARD_READ_EVENTS)
		g2_event_ring_returned_role(&g->ring, buffer, false);
out:
	kvfree(buffer);
	return error;
}


static __poll_t g2_guard_poll(struct file *file, poll_table *wait)
{
	struct g2_guard *g = file->private_data;

	if (!g || !g2_control_owner(g, file))
		return EPOLLERR;
	if (g2_probe_missed(g))
		g2_event_ring_invalidate(&g->ring);
	return g2_event_ring_poll_role(&g->ring, file, wait, false);
}

static const struct file_operations g2_guard_fops = {
	.owner = THIS_MODULE,
	.open = g2_guard_open,
	.release = g2_guard_release,
	.unlocked_ioctl = g2_guard_ioctl,
#ifdef CONFIG_COMPAT
	.compat_ioctl = g2_guard_compat_ioctl,
#endif
	.poll = g2_guard_poll,
	.llseek = no_llseek,
};

static void g2_find_tracepoint(struct tracepoint *tracepoint, void *unused)
{
	struct g2_guard *g = &guard;

	(void)unused;
	if (!strcmp(tracepoint->name, "sched_process_fork"))
		g->fork_tracepoint = tracepoint;
	else if (!strcmp(tracepoint->name, "sched_process_exec"))
		g->exec_tracepoint = tracepoint;
	else if (!strcmp(tracepoint->name, "sched_process_exit"))
		g->exit_tracepoint = tracepoint;
}

static int g2_tracepoints_register(struct g2_guard *g)
{
	int error;

	for_each_kernel_tracepoint(g2_find_tracepoint, NULL);
	if (!g->fork_tracepoint || !g->exec_tracepoint || !g->exit_tracepoint)
		return -ENOENT;
	error = tracepoint_probe_register(g->fork_tracepoint, g2_lineage_fork, &g->lineage);
	if (error)
		return error;
	error = tracepoint_probe_register(g->exec_tracepoint, g2_lineage_exec, &g->lineage);
	if (error)
		goto out_fork;
	error = tracepoint_probe_register(g->exit_tracepoint, g2_lineage_exit, &g->lineage);
	if (error)
		goto out_exec;
	g->tracepoints_registered = true;
	return 0;
out_exec:
	tracepoint_probe_unregister(g->exec_tracepoint, g2_lineage_exec, &g->lineage);
out_fork:
	tracepoint_probe_unregister(g->fork_tracepoint, g2_lineage_fork, &g->lineage);
	tracepoint_synchronize_unregister();
	return error;
}

static int __init g2_guard_init(void)
{
	struct g2_guard *g = &guard;
	int error;

	mutex_init(&g->lifecycle);
	error = g2_boot_capture(&g->boot);
	if (error)
		return error;
	error = g2_observer_binding_init(&g->observer);
	if (error)
		goto out_boot;
	sha256(g2_observer_binding, G2_OBSERVER_BINDING_SIZE, g->observer_bindings_sha256);
	error = g2_boot_no_producers(&g->boot, &g->observer);
	if (error)
		goto out_boot;
	g2_event_ring_init(&g->ring);
	g2_task_refs_init(&g->tasks);
	g2_lineage_init(&g->lineage, &g->tasks, &g->ring);
	g2_cgroup_refs_init(&g->cgroups, &g->ring, &g->tasks);
	error = g2_id_probes_register(&g->ids, &g->ring, &g->tasks, &g->observer);
	if (error)
		goto out_boot;
	error = g2_cgroup_probes_register(&g->cgroups);
	if (error)
		goto out_ids;
	if ((unsigned long)g->cgroups.offline.addr != g->observer.cgroup_offline ||
	    (unsigned long)g->cgroups.release.kp.addr != g->observer.cgroup_release) {
		error = -EINVAL;
		goto out_cgroups;
	}
	error = g2_audit_probe_register(&g->audit, &g->ring, &g->ids, &g->tasks, &g->lineage, &g->observer);
	if (error)
		goto out_cgroups;
	error = g2_tracepoints_register(g);
	if (error)
		goto out_audit;
	error = g2_boot_no_producers(&g->boot, &g->observer);
	if (error || g2_probe_missed(g) || g->ring.sticky || g->ring.pending_pairs) {
		if (!error)
			error = -EIO;
		goto out_tracepoints;
	}
	g->device.minor = MISC_DYNAMIC_MINOR;
	g->device.name = "g2_bpf_guard";
	g->device.fops = &g2_guard_fops;
	g->device.mode = 0600;
	error = misc_register(&g->device);
	if (error)
		goto out_tracepoints;
	return 0;
out_tracepoints:
	g2_tracepoints_unregister(g);
out_audit:
	g2_audit_probe_unregister(&g->audit);
out_cgroups:
	g2_cgroup_probes_unregister(&g->cgroups);
out_ids:
	g2_id_probes_unregister(&g->ids);
out_boot:
	g2_boot_release(&g->boot);
	return error;
}

module_init(g2_guard_init);
