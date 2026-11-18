// SPDX-License-Identifier: GPL-2.0-only
/* The field declarations below are CO-RE descriptions, not kernel layouts. */
#include <linux/bpf.h>

#define SEC(name) __attribute__((section(name), used))
#define __uint(name, value) int (*name)[value]
#define __type(name, value) value *name
#define CORE __attribute__((preserve_access_index))
#define EPERM 1
#define CGROUP2_SUPER_MAGIC 0x63677270UL
#define KERNFS_DIR 1U
#define KERNFS_TYPE_MASK 0x0fU

struct super_block { unsigned long s_magic; unsigned int s_dev; } CORE;
struct inode { struct super_block *i_sb; void *i_private; unsigned long i_ino; } CORE;
struct dentry { struct inode *d_inode; } CORE;
struct file { struct inode *f_inode; } CORE;
struct mm_struct { struct file *exe_file; } CORE;
struct task_struct {
	struct task_struct *group_leader;
	struct mm_struct *mm;
	unsigned long long start_boottime;
	int tgid;
} CORE;
struct kernfs_root;
struct kernfs_elem_dir { struct kernfs_root *root; } CORE;
struct kernfs_node {
	struct kernfs_elem_dir dir;
	unsigned long long id;
	unsigned short flags;
} CORE;
struct kernfs_root { struct kernfs_node *kn; } CORE;

struct target_key { __u64 root_kn_id, kn_id; };
struct target_value { __u64 enrollment, grant_id; __u32 phase, retirement; };
struct retire_key { __u64 root_kn_id, kn_id; __u32 tgid, pad; __u64 leader_start_ns; };
struct retire_value { __u64 enrollment, grant_id, invocation_id, exe_dev, exe_ino; };

struct {
	__uint(type, BPF_MAP_TYPE_TASK_STORAGE);
	__uint(map_flags, BPF_F_NO_PREALLOC);
	__type(key, int);
	__type(value, __u64);
} broker_token SEC(".maps");
struct {
	__uint(type, BPF_MAP_TYPE_ARRAY);
	__uint(max_entries, 1);
	__uint(map_flags, 0);
	__type(key, __u32);
	__type(value, __u64);
} guard_liveness SEC(".maps");
struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__uint(max_entries, 128);
	__type(key, struct target_key);
	__type(value, struct target_value);
} realm_targets SEC(".maps");
struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__uint(max_entries, 4096);
	__type(key, struct retire_key);
	__type(value, struct retire_value);
} retire_tasks SEC(".maps");
static void *(* const map_lookup)(void *, const void *) = (void *)BPF_FUNC_map_lookup_elem;
static void *(* const task_storage_get)(void *, struct task_struct *, void *, __u64) = (void *)BPF_FUNC_task_storage_get;
static struct task_struct *(* const current_task)(void) = (void *)BPF_FUNC_get_current_task_btf;
static long (* const read_kernel)(void *, __u32, const void *) = (void *)BPF_FUNC_probe_read_kernel;
#define READ(dst, src) read_kernel(&(dst), sizeof(dst), __builtin_preserve_access_index(&(src)))

/* Return 1 for a cgroup2 directory, 0 for other filesystems, -1 on failure. */
static __attribute__((always_inline)) int target_identity(struct inode *inode, struct target_key *key)
{
	struct super_block *sb = 0;
	struct kernfs_node *node = 0, *root_node = 0;
	struct kernfs_root *root = 0;
	unsigned long magic = 0;
	unsigned short flags = 0;

	if (!inode || READ(sb, inode->i_sb) || !sb || READ(magic, sb->s_magic))
		return -1;
	if (magic != CGROUP2_SUPER_MAGIC)
		return 0;
	if (READ(node, inode->i_private) || !node || READ(flags, node->flags) ||
	    (flags & KERNFS_TYPE_MASK) != KERNFS_DIR ||
	    READ(key->kn_id, node->id) || !key->kn_id ||
	    READ(root, node->dir.root) || !root ||
	    READ(root_node, root->kn) || !root_node ||
	    READ(key->root_kn_id, root_node->id) || !key->root_kn_id)
		return -1;
	return 1;
}

SEC("lsm/bpf")
int g2_bpf(unsigned long long *ctx)
{
	int cmd = (int)ctx[0];
	int previous = (int)ctx[3];
	struct task_struct *task;
	__u64 *cookie, *phase;
	__u32 liveness_key = 0;
	__u64 current_phase;

	if (previous)
		return previous;
	task = current_task();
	cookie = task_storage_get(&broker_token, task, 0, 0);
	if (!cookie || !*cookie)
		return -EPERM;
	phase = map_lookup(&guard_liveness, &liveness_key);
	if (!phase)
		return -EPERM;
	current_phase = *phase;
	if (current_phase == 1) {
		switch (cmd) {
		case BPF_MAP_CREATE: case BPF_MAP_LOOKUP_ELEM: case BPF_MAP_UPDATE_ELEM:
		case BPF_MAP_DELETE_ELEM: case BPF_MAP_FREEZE: case BPF_PROG_LOAD:
		case BPF_PROG_QUERY: case BPF_PROG_GET_FD_BY_ID: case BPF_OBJ_GET_INFO_BY_FD:
		case BPF_PROG_ATTACH: case BPF_PROG_DETACH:
			return 0;
		default:
			return -EPERM;
		}
	}
	if (current_phase == 2) {
		switch (cmd) {
		case BPF_MAP_LOOKUP_ELEM: case BPF_MAP_UPDATE_ELEM:
		case BPF_MAP_DELETE_ELEM: case BPF_PROG_QUERY:
		case BPF_OBJ_GET_INFO_BY_FD: case BPF_PROG_DETACH:
			return 0;
		default:
			return -EPERM;
		}
	}
	return -EPERM;
}

SEC("lsm/inode_mkdir")
int g2_inode_mkdir(unsigned long long *ctx)
{
	struct target_key key = {};
	struct target_value *target;
	int previous = (int)ctx[3];
	int identified;

	if (previous)
		return previous;
	identified = target_identity((struct inode *)ctx[0], &key);
	if (identified < 0)
		return -EPERM;
	if (!identified)
		return 0;
	target = map_lookup(&realm_targets, &key);
	return target && target->phase != 4 ? -EPERM : 0;
}

SEC("lsm/inode_rmdir")
int g2_inode_rmdir(unsigned long long *ctx)
{
	struct target_key key = {};
	struct retire_key actor = {};
	struct target_value *target;
	struct retire_value *permit;
	struct dentry *entry = (struct dentry *)ctx[1];
	struct inode *inode = 0, *exe_inode = 0;
	struct super_block *exe_sb = 0;
	struct task_struct *task, *leader = 0;
	struct mm_struct *mm = 0;
	struct file *exe = 0;
	unsigned int dev = 0;
	unsigned long ino = 0;
	int previous = (int)ctx[2];
	int identified;

	if (previous)
		return previous;
	if (!entry || READ(inode, entry->d_inode) || !inode)
		return -EPERM;
	identified = target_identity(inode, &key);
	if (identified < 0)
		return -EPERM;
	if (!identified)
		return 0;
	target = map_lookup(&realm_targets, &key);
	if (!target)
		return 0;
	if ((target->phase != 2 && target->phase != 3) ||
	    (target->retirement != 1 && target->retirement != 2))
		return -EPERM;
	task = current_task();
	if (!task || READ(leader, task->group_leader) || !leader ||
	    READ(actor.tgid, leader->tgid) || !actor.tgid ||
	    READ(actor.leader_start_ns, leader->start_boottime) || !actor.leader_start_ns ||
	    READ(mm, task->mm) || !mm || READ(exe, mm->exe_file) || !exe ||
	    READ(exe_inode, exe->f_inode) || !exe_inode ||
	    READ(ino, exe_inode->i_ino) || !ino ||
	    READ(exe_sb, exe_inode->i_sb) || !exe_sb || READ(dev, exe_sb->s_dev))
		return -EPERM;
	actor.root_kn_id = key.root_kn_id;
	actor.kn_id = key.kn_id;
	permit = map_lookup(&retire_tasks, &actor);
	if (!permit || permit->enrollment != target->enrollment ||
	    permit->grant_id != target->grant_id || permit->exe_dev != dev ||
	    permit->exe_ino != ino || (actor.tgid == 1) != (permit->invocation_id == 0))
		return -EPERM;
	return 0;
}

char LICENSE[] SEC("license") = "GPL";
