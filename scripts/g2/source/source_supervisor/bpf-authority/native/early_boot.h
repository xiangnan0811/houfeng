/* SPDX-License-Identifier: GPL-2.0-only */
#ifndef G2_BPF_GUARD_EARLY_BOOT_H
#define G2_BPF_GUARD_EARLY_BOOT_H

#include <linux/cred.h>
#include <linux/errno.h>
#include <linux/fcntl.h>
#include <linux/file.h>
#include <linux/fs.h>
#include <linux/fs_struct.h>
#include <linux/magic.h>
#include <linux/mm_types.h>
#include <linux/mount.h>
#include <linux/path.h>
#include <linux/pid_namespace.h>
#include <linux/rcupdate.h>
#include <linux/sched.h>
#include <linux/sched/task.h>
#include <linux/sched/signal.h>
#include <linux/string.h>
#include <linux/shmem_fs.h>
#include <linux/random.h>
#include <asm/ptrace.h>
#include <asm/syscall.h>
#include <linux/bpf.h>
#include <linux/filter.h>
#include "observer_binding.h"
#include <linux/user_namespace.h>

#define G2_MODULE_SEALS (F_SEAL_WRITE | F_SEAL_GROW | F_SEAL_SHRINK | F_SEAL_SEAL)

struct g2_boot_identity {
	struct task_struct *init;
	struct path root;
	struct file *early;
	struct file *broker;
	struct file *module_file;
	__u64 module_instance;
	__u64 module_dev;
	__u64 module_ino;
	__u64 module_size;
};
/* Take a real executable file reference without an unexported task helper. */
static struct file *g2_boot_task_exe(struct task_struct *task)
{
	struct file *file = NULL;

	task_lock(task);
	if (task->mm) {
		rcu_read_lock();
		file = get_file_rcu(&task->mm->exe_file);
		rcu_read_unlock();
	}
	task_unlock(task);
	return file;
}

static int g2_boot_task_root(struct task_struct *task, struct path *root)
{
	int error = -ESRCH;

	task_lock(task);
	if (task->fs) {
		get_fs_root(task->fs, root);
		error = 0;
	}
	task_unlock(task);
	return error;
}

static bool g2_boot_root_valid(const struct path *root)
{
	struct inode *inode;
	struct super_block *sb;

	if (!root->dentry || !root->mnt || root->mnt->mnt_root != root->dentry)
		return false;
	inode = d_inode(root->dentry);
	sb = root->mnt->mnt_sb;
	return inode && S_ISDIR(inode->i_mode) &&
	       uid_eq(inode->i_uid, GLOBAL_ROOT_UID) &&
	       gid_eq(inode->i_gid, GLOBAL_ROOT_GID) && !(inode->i_mode & 0022) &&
	       sb && sb->s_type && !strcmp(sb->s_type->name, "rootfs") &&
	       (sb->s_magic == RAMFS_MAGIC || sb->s_magic == TMPFS_MAGIC);
}

static bool g2_boot_regular(struct file *file, const struct path *root,
		const char *name)
{
	struct inode *inode;
	struct dentry *dentry;
	size_t length = strlen(name);

	if (!file || file->f_path.mnt != root->mnt)
		return false;
	dentry = file->f_path.dentry;
	inode = file_inode(file);
	return dentry && inode && dentry->d_parent == root->dentry &&
	       !d_unhashed(dentry) && dentry->d_name.len == length &&
	       !memcmp(dentry->d_name.name, name, length) &&
	       S_ISREG(inode->i_mode) && (inode->i_mode & 07777) == 0500 &&
	       uid_eq(inode->i_uid, GLOBAL_ROOT_UID) &&
	       gid_eq(inode->i_gid, GLOBAL_ROOT_GID) && inode->i_nlink == 1;
}

static bool g2_boot_same_file(struct file *first, struct file *second)
{
	return first && second && path_equal(&first->f_path, &second->f_path) &&
	       file_inode(first) == file_inode(second);
}

static bool g2_boot_module_memfd_valid(struct file *file, __u64 *dev,
		__u64 *ino, __u64 *size)
{
	struct inode *inode;
	struct super_block *sb;
	struct shmem_inode_info *info;
	loff_t isize;

	if (!file)
		return false;
	inode = file_inode(file);
	if (!inode || !S_ISREG(inode->i_mode) ||
	    !uid_eq(inode->i_uid, GLOBAL_ROOT_UID) ||
	    !gid_eq(inode->i_gid, GLOBAL_ROOT_GID) ||
	    (inode->i_mode & 07777) != 0400)
		return false;
	sb = inode->i_sb;
	if (!sb || sb->s_magic != TMPFS_MAGIC || !inode->i_mapping ||
	    !shmem_mapping(inode->i_mapping))
		return false;
	info = SHMEM_I(inode);
	if (!info || (READ_ONCE(info->seals) & G2_MODULE_SEALS) != G2_MODULE_SEALS)
		return false;
	isize = i_size_read(inode);
	if (isize <= 0 || isize > (128LL << 20) || !inode->i_ino)
		return false;
	if (dev)
		*dev = sb->s_dev;
	if (ino)
		*ino = inode->i_ino;
	if (size)
		*size = (__u64)isize;
	return true;
}

static bool g2_boot_module_still_valid(const struct g2_boot_identity *boot)
{
	__u64 dev = 0, ino = 0, size = 0;

	if (!boot->module_file || !boot->module_instance)
		return false;
	if (!g2_boot_module_memfd_valid(boot->module_file, &dev, &ino, &size))
		return false;
	return dev == boot->module_dev && ino == boot->module_ino &&
	       size == boot->module_size;
}

static void g2_boot_release(struct g2_boot_identity *boot)
{
	if (boot->module_file)
		fput(boot->module_file);
	if (boot->early)
		fput(boot->early);
	if (boot->broker)
		fput(boot->broker);
	if (boot->root.dentry)
		path_put(&boot->root);
	if (boot->init)
		put_task_struct(boot->init);
	memset(boot, 0, sizeof(*boot));
}
/* PID 1's trusted threads are the only userspace tasks in this interval. */
static int g2_boot_no_producers(struct g2_boot_identity *boot,
		const struct g2_observer_addresses *binding)
{
	struct task_struct *process, *thread;
	const struct bpf_prog *prog;
	const struct bpf_map *map;
	int next = 0, error = 0;

	rcu_read_lock();
	for_each_process_thread(process, thread) {
		if (!(READ_ONCE(thread->flags) & (PF_KTHREAD | PF_USER_WORKER)) &&
		    thread->group_leader != boot->init) {
			error = -EBUSY;
			break;
		}
	}
	rcu_read_unlock();
	if (error)
		return error;
	rcu_read_lock();
	while ((prog = idr_get_next((struct idr *)binding->prog_idr, &next)) != NULL) {
		const struct bpf_prog_aux *aux = READ_ONCE(prog->aux);
		const struct user_struct *user = aux ? READ_ONCE(aux->user) : NULL;
		__u32 type = READ_ONCE(prog->type);

		if (!aux || !user || READ_ONCE(aux->offload_requested) || READ_ONCE(aux->offload) ||
		    !uid_eq(READ_ONCE(user->uid), GLOBAL_ROOT_UID) ||
		    type == BPF_PROG_TYPE_LSM || type == BPF_PROG_TYPE_CGROUP_DEVICE) {
			error = -EBUSY;
			break;
		}
		if (next == INT_MAX)
			break;
		next++;
	}
	if (!error) {
		next = 0;
		while ((map = idr_get_next((struct idr *)binding->map_idr, &next)) != NULL) {
			__u32 type = READ_ONCE(map->map_type);

			if ((unsigned long)READ_ONCE(map->ops) == binding->map_offload_ops ||
			    type == BPF_MAP_TYPE_HASH || type == BPF_MAP_TYPE_TASK_STORAGE ||
			    (type == BPF_MAP_TYPE_ARRAY && strcmp(map->name, "__loader.map") != 0)) {
				error = -EBUSY;
				break;
			}
			if (next == INT_MAX)
				break;
			next++;
		}
	}
	rcu_read_unlock();
	return error;
}

/* Called synchronously by the actual init task during finit_module. */
static int g2_boot_capture(struct g2_boot_identity *boot)
{
	struct pt_regs *regs;
	unsigned long args[6] = {};
	struct file *exe, *mod_file;
	int error;

	memset(boot, 0, sizeof(*boot));
	if (task_pid_nr(current) != 1 || task_tgid_nr(current) != 1 ||
	    task_active_pid_ns(current) != &init_pid_ns ||
	    current_user_ns() != &init_user_ns ||
	    !uid_eq(current_uid(), GLOBAL_ROOT_UID) ||
	    !gid_eq(current_gid(), GLOBAL_ROOT_GID))
		return -EPERM;
	regs = task_pt_regs(current);
	if (!regs || syscall_get_nr(current, regs) != 313 ||
	    syscall_get_arch(current) != AUDIT_ARCH_X86_64)
		return -EPERM;
	syscall_get_arguments(current, regs, args);
	if ((int)args[0] < 0 || (unsigned int)args[2] != 0)
		return -EPERM;
	mod_file = fget((unsigned int)args[0]);
	if (!mod_file)
		return -EBADF;
	boot->module_file = mod_file;
	if (!g2_boot_module_memfd_valid(mod_file, &boot->module_dev,
			&boot->module_ino, &boot->module_size)) {
		error = -EPERM;
		goto fail;
	}
	get_random_bytes(&boot->module_instance, sizeof(boot->module_instance));
	if (!boot->module_instance)
		boot->module_instance = 1;
	error = g2_boot_task_root(current, &boot->root);
	if (error)
		goto fail;
	if (!g2_boot_root_valid(&boot->root)) {
		error = -EPERM;
		goto fail;
	}
	boot->early = file_open_root(&boot->root, "g2-early-init",
			O_RDONLY | O_NOFOLLOW | O_CLOEXEC, 0);
	if (IS_ERR(boot->early)) {
		error = PTR_ERR(boot->early);
		boot->early = NULL;
		goto fail;
	}
	boot->broker = file_open_root(&boot->root, "g2-bpf-authority",
			O_RDONLY | O_NOFOLLOW | O_CLOEXEC, 0);
	if (IS_ERR(boot->broker)) {
		error = PTR_ERR(boot->broker);
		boot->broker = NULL;
		goto fail;
	}
	if (!g2_boot_regular(boot->early, &boot->root, "g2-early-init") ||
	    !g2_boot_regular(boot->broker, &boot->root, "g2-bpf-authority")) {
		error = -EPERM;
		goto fail;
	}
	exe = g2_boot_task_exe(current);
	if (!exe) {
		error = -ESRCH;
		goto fail;
	}
	error = g2_boot_same_file(exe, boot->early) ? 0 : -EPERM;
	fput(exe);
	if (error)
		goto fail;
	get_task_struct(current);
	boot->init = current;
	return 0;
fail:
	g2_boot_release(boot);
	return error;
}

static int g2_boot_verify_observer_task(struct g2_boot_identity *boot,
		struct task_struct *task)
{
	struct task_struct *parent;
	struct file *early_exe = NULL, *task_exe = NULL;
	struct file *early_named = NULL, *broker_named = NULL;
	struct path init_root = {}, task_root = {};
	const struct cred *cred;
	int error = -EPERM;

	if (!boot->init || !task || task_pid_nr(task) != task_tgid_nr(task) ||
	    task_active_pid_ns(task) != &init_pid_ns ||
	    !g2_boot_module_still_valid(boot))
		return -EPERM;
	rcu_read_lock();
	parent = rcu_dereference(task->real_parent);
	cred = __task_cred(task);
	if (parent != boot->init || !cred || cred->user_ns != &init_user_ns ||
	    !uid_eq(cred->uid, GLOBAL_ROOT_UID) ||
	    !gid_eq(cred->gid, GLOBAL_ROOT_GID)) {
		rcu_read_unlock();
		return -EPERM;
	}
	rcu_read_unlock();
	if (task_pid_nr(boot->init) != 1 || task_tgid_nr(boot->init) != 1 ||
	    g2_boot_task_root(boot->init, &init_root) ||
	    g2_boot_task_root(task, &task_root))
		goto out;
	if (!path_equal(&init_root, &boot->root) ||
	    !path_equal(&task_root, &boot->root) || !g2_boot_root_valid(&boot->root))
		goto out;
	early_exe = g2_boot_task_exe(boot->init);
	task_exe = g2_boot_task_exe(task);
	if (!g2_boot_same_file(early_exe, boot->early) ||
	    !g2_boot_same_file(task_exe, boot->broker))
		goto out;
	early_named = file_open_root(&boot->root, "g2-early-init",
			O_RDONLY | O_NOFOLLOW | O_CLOEXEC, 0);
	if (IS_ERR(early_named)) {
		early_named = NULL;
		goto out;
	}
	broker_named = file_open_root(&boot->root, "g2-bpf-authority",
			O_RDONLY | O_NOFOLLOW | O_CLOEXEC, 0);
	if (IS_ERR(broker_named)) {
		broker_named = NULL;
		goto out;
	}
	if (!g2_boot_regular(early_named, &boot->root, "g2-early-init") ||
	    !g2_boot_regular(broker_named, &boot->root, "g2-bpf-authority") ||
	    !g2_boot_same_file(early_named, boot->early) ||
	    !g2_boot_same_file(broker_named, boot->broker))
		goto out;
	error = 0;
out:
	if (init_root.dentry)
		path_put(&init_root);
	if (task_root.dentry)
		path_put(&task_root);
	if (early_exe)
		fput(early_exe);
	if (task_exe)
		fput(task_exe);
	if (early_named)
		fput(early_named);
	if (broker_named)
		fput(broker_named);
	return error;
}

/* Open and SEAL must occur before the held PID 1 executes the original init. */
static int g2_boot_verify_broker(struct g2_boot_identity *boot)
{
	struct task_struct *parent;
	struct file *early_exe = NULL, *broker_exe = NULL;
	struct file *early_named = NULL, *broker_named = NULL;
	struct path init_root = {}, broker_root = {};
	int error = -EPERM;

	if (!boot->init || task_pid_nr(current) != task_tgid_nr(current) ||
	    task_active_pid_ns(current) != &init_pid_ns ||
	    current_user_ns() != &init_user_ns ||
	    !uid_eq(current_uid(), GLOBAL_ROOT_UID) ||
	    !gid_eq(current_gid(), GLOBAL_ROOT_GID) ||
	    !g2_boot_module_still_valid(boot))
		return -EPERM;
	rcu_read_lock();
	parent = rcu_dereference(current->real_parent);
	if (parent != boot->init) {
		rcu_read_unlock();
		return -EPERM;
	}
	rcu_read_unlock();
	if (task_pid_nr(boot->init) != 1 || task_tgid_nr(boot->init) != 1 ||
	    g2_boot_task_root(boot->init, &init_root) ||
	    g2_boot_task_root(current, &broker_root))
		goto out;
	if (!path_equal(&init_root, &boot->root) ||
	    !path_equal(&broker_root, &boot->root) || !g2_boot_root_valid(&boot->root))
		goto out;
	early_exe = g2_boot_task_exe(boot->init);
	broker_exe = g2_boot_task_exe(current);
	if (!g2_boot_same_file(early_exe, boot->early) ||
	    !g2_boot_same_file(broker_exe, boot->broker))
		goto out;
	early_named = file_open_root(&boot->root, "g2-early-init",
			O_RDONLY | O_NOFOLLOW | O_CLOEXEC, 0);
	if (IS_ERR(early_named)) {
		early_named = NULL;
		goto out;
	}
	broker_named = file_open_root(&boot->root, "g2-bpf-authority",
			O_RDONLY | O_NOFOLLOW | O_CLOEXEC, 0);
	if (IS_ERR(broker_named)) {
		broker_named = NULL;
		goto out;
	}
	if (!g2_boot_regular(early_named, &boot->root, "g2-early-init") ||
	    !g2_boot_regular(broker_named, &boot->root, "g2-bpf-authority") ||
	    !g2_boot_same_file(early_named, boot->early) ||
	    !g2_boot_same_file(broker_named, boot->broker))
		goto out;
	error = 0;
out:
	if (init_root.dentry)
		path_put(&init_root);
	if (broker_root.dentry)
		path_put(&broker_root);
	if (early_exe)
		fput(early_exe);
	if (broker_exe)
		fput(broker_exe);
	if (early_named)
		fput(early_named);
	if (broker_named)
		fput(broker_named);
	return error;
}

#endif
