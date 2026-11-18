/* SPDX-License-Identifier: GPL-2.0-only */
#ifndef G2_BPF_GUARD_ABI_H
#define G2_BPF_GUARD_ABI_H

#include <linux/ioctl.h>
#include <linux/types.h>
#ifdef __KERNEL__
#include <linux/stddef.h>
#else
#include <stddef.h>
#endif

#define G2_GUARD_VERSION 5U
#define G2_GUARD_IOC_TYPE 0xB7
#define G2_GUARD_EVENT_SLOTS 64U
#define G2_GUARD_RING_CAPACITY 4096U

struct g2_guard_header {
	__u32 version;
	__u32 reserved;
};

struct g2_guard_target_key {
	__u64 root_kn_id;
	__u64 kn_id;
};

struct g2_guard_target_value {
	__u64 enrollment;
	__u64 grant_id;
	__u32 phase;
	__u32 retirement;
};

struct g2_guard_retire_key {
	__u64 root_kn_id;
	__u64 kn_id;
	__u32 tgid;
	__u32 pad;
	__u64 leader_start_ns;
};

struct g2_guard_retire_value {
	__u64 enrollment;
	__u64 grant_id;
	__u64 invocation_id;
	__u64 exe_dev;
	__u64 exe_ino;
};

struct g2_guard_program_snapshot {
	__u8 name[16];
	__u8 tag[8];
	__u64 load_time;
	__u32 created_by_uid;
	__u32 translated_insn_count;
	__u32 verified_insns;
	__u32 attach_btf_id;
	__u8 reserved[8];
};

struct g2_guard_map_snapshot {
	__u8 name[16];
	__u32 key_size;
	__u32 value_size;
	__u32 max_entries;
	__u32 map_flags;
	__u32 btf_id;
	__u32 btf_key_type_id;
	__u32 btf_value_type_id;
	__u32 pad;
	__u64 map_extra;
};

struct g2_guard_object_snapshot {
	__u32 type;
	__u32 reserved;
	union {
		struct g2_guard_program_snapshot program;
		struct g2_guard_map_snapshot map;
	} attributes;
};

struct g2_guard_record {
	__u64 sequence;
	__u32 kind;
	__u32 flags;
	__u64 object_cookie;
	__u64 kernel_id;
	__u64 root_cookie;
	__u64 cgroup_id;
	__u64 task_cookie;
	__u64 task_start_ns;
	__u64 audit_serial;
	__u32 operation;
	__s32 result;
	__u64 parent_cookie;
	__u8 detail[40];
	struct g2_guard_object_snapshot object_snapshot;
};

struct g2_guard_task_detail {
	__u32 tgid;
	__u32 tid;
	__u64 exe_dev;
	__u64 exe_ino;
	__u64 exec_generation;
	__s32 exit_code;
	__u32 pad;
};
struct g2_guard_raw_syscall_detail {
	__u32 tgid;
	__u32 tid;
	__u64 seconds;
	__u32 nanoseconds;
	__u32 pad;
	__u64 exe_dev;
	__u64 exe_ino;
};

struct g2_guard_seal {
	struct g2_guard_header header;
	__u32 count;
	__s32 link_fds[3];
	__u8 bootstrap_digest[32];
	__u8 epoch[16];
	__u32 link_ids[3];
	__u32 program_ids[3];
};

struct g2_guard_status {
	struct g2_guard_header header;
	__u8 epoch[16];
	__u32 state;
	__u32 sticky;
	__u32 authority_phase;
	__u32 pad;
	__u64 sequence;
	__u64 ack_cursor;
	__u64 observer_ack_cursor;
	__u64 lost;
	__u64 probe_missed;
	__u64 broker_cookie;
	__u64 observer_cookie;
	__u64 death_sequence;
	__u64 module_instance;
	__u64 module_dev;
	__u64 module_ino;
	__u64 module_size;
	__u32 link_ids[3];
	__u32 program_ids[3];
	__u32 link_attached[3];
	__u32 tail_pad;
	__u8 bootstrap_digest[32];
	__u8 observer_bindings_sha256[32];
};
struct g2_guard_pin_task {
	struct g2_guard_header header;
	__u32 tid;
	__u32 pad;
	__u64 cookie;
	__u32 tgid;
	__u32 actual_tid;
	__u64 start_boottime_ns;
};

struct g2_guard_import_fd {
	struct g2_guard_header header;
	__u64 cookie;
	__u32 fd;
	__u32 pad;
	__s32 installed_fd;
	__u32 output_pad;
};

struct g2_guard_drop_task {
	struct g2_guard_header header;
	__u64 cookie;
};

struct g2_guard_enroll_cgroup {
	struct g2_guard_header header;
	__s32 cgroup_fd;
	__u32 pad;
	__u64 grant_id;
	__u64 root_cookie;
	__u64 kernfs_id;
	__u64 enrollment;
};

struct g2_guard_retire_cgroup {
	struct g2_guard_header header;
	__u64 enrollment;
	__u32 observed_state;
	__u32 pad;
	__u64 sequence;
};

struct g2_guard_read_events {
	struct g2_guard_header header;
	__u32 max_records;
	__u32 pad;
	__u64 after;
	__u64 ack;
	__u64 first;
	__u64 last;
	__u32 count;
	__u32 sticky;
	struct g2_guard_record records[G2_GUARD_EVENT_SLOTS];
};

struct g2_guard_watch_daemon {
	struct g2_guard_header header;
	__u64 cookie;
	__u64 sequence;
};
struct g2_guard_bind_observer {
	struct g2_guard_header header;
	__u64 cookie;
	__s32 installed_fd;
	__u32 pad;
};

struct g2_guard_revoke {
	struct g2_guard_header header;
};

struct g2_guard_watch_probe {
	struct g2_guard_header header;
	__u64 cookie;
	__u32 probe_kind;
	__u32 pad;
	__u64 sequence;
};

struct g2_guard_selftest_observation_fault {
	struct g2_guard_header header;
	__u32 variant;
	__u32 pad;
};

struct g2_guard_selftest_id_reuse {
	struct g2_guard_header header;
	__u64 first_object_cookie;
	__u64 first_removal_sequence;
	__u64 next_transaction_id;
	__u8 nonce[16];
};

struct g2_guard_selftest_id_cursor_detail {
	__u64 first_removal_sequence;
	__u64 transaction_id;
	__u32 saved_cursor;
	__u32 requested_id;
	__s32 allocator_result;
	__u32 cursor_after;
	__u64 pad;
};

#define G2_GUARD_SEAL _IOWR(G2_GUARD_IOC_TYPE, 1, struct g2_guard_seal)
#define G2_GUARD_STATUS _IOWR(G2_GUARD_IOC_TYPE, 2, struct g2_guard_status)
#define G2_GUARD_PIN_TASK _IOWR(G2_GUARD_IOC_TYPE, 3, struct g2_guard_pin_task)
#define G2_GUARD_IMPORT_FD _IOWR(G2_GUARD_IOC_TYPE, 4, struct g2_guard_import_fd)
#define G2_GUARD_DROP_TASK _IOWR(G2_GUARD_IOC_TYPE, 5, struct g2_guard_drop_task)
#define G2_GUARD_ENROLL_CGROUP _IOWR(G2_GUARD_IOC_TYPE, 6, struct g2_guard_enroll_cgroup)
#define G2_GUARD_RETIRE_CGROUP _IOWR(G2_GUARD_IOC_TYPE, 7, struct g2_guard_retire_cgroup)
#define G2_GUARD_READ_EVENTS _IOWR(G2_GUARD_IOC_TYPE, 8, struct g2_guard_read_events)
#define G2_GUARD_WATCH_DAEMON _IOWR(G2_GUARD_IOC_TYPE, 9, struct g2_guard_watch_daemon)
#define G2_GUARD_BIND_OBSERVER _IOWR(G2_GUARD_IOC_TYPE, 10, struct g2_guard_bind_observer)
#define G2_GUARD_REVOKE _IOWR(G2_GUARD_IOC_TYPE, 11, struct g2_guard_revoke)
#define G2_GUARD_WATCH_PROBE _IOWR(G2_GUARD_IOC_TYPE, 12, struct g2_guard_watch_probe)
#define G2_GUARD_SELFTEST_OBSERVATION_FAULT _IOWR(G2_GUARD_IOC_TYPE, 13, struct g2_guard_selftest_observation_fault)
#define G2_GUARD_SELFTEST_ID_REUSE _IOWR(G2_GUARD_IOC_TYPE, 14, struct g2_guard_selftest_id_reuse)
#define G2_ABI_SIZE(type, size) _Static_assert(sizeof(struct type) == (size), #type " size")
#define G2_ABI_OFFSET(type, field, offset) _Static_assert(offsetof(struct type, field) == (offset), #type "." #field " offset")
G2_ABI_SIZE(g2_guard_target_key, 16);
G2_ABI_OFFSET(g2_guard_target_key, kn_id, 8);
G2_ABI_SIZE(g2_guard_target_value, 24);
G2_ABI_OFFSET(g2_guard_target_value, phase, 16);
G2_ABI_OFFSET(g2_guard_target_value, retirement, 20);
G2_ABI_SIZE(g2_guard_retire_key, 32);
G2_ABI_OFFSET(g2_guard_retire_key, tgid, 16);
G2_ABI_OFFSET(g2_guard_retire_key, leader_start_ns, 24);
G2_ABI_SIZE(g2_guard_retire_value, 40);
G2_ABI_OFFSET(g2_guard_retire_value, invocation_id, 16);
G2_ABI_OFFSET(g2_guard_retire_value, exe_dev, 24);
G2_ABI_OFFSET(g2_guard_retire_value, exe_ino, 32);
G2_ABI_SIZE(g2_guard_header, 8);
G2_ABI_SIZE(g2_guard_program_snapshot, 56);
G2_ABI_OFFSET(g2_guard_program_snapshot, load_time, 24);
G2_ABI_OFFSET(g2_guard_program_snapshot, created_by_uid, 32);
G2_ABI_OFFSET(g2_guard_program_snapshot, translated_insn_count, 36);
G2_ABI_OFFSET(g2_guard_program_snapshot, verified_insns, 40);
G2_ABI_OFFSET(g2_guard_program_snapshot, attach_btf_id, 44);
G2_ABI_OFFSET(g2_guard_program_snapshot, reserved, 48);
G2_ABI_SIZE(g2_guard_map_snapshot, 56);
G2_ABI_OFFSET(g2_guard_map_snapshot, key_size, 16);
G2_ABI_OFFSET(g2_guard_map_snapshot, btf_id, 32);
G2_ABI_OFFSET(g2_guard_map_snapshot, pad, 44);
G2_ABI_OFFSET(g2_guard_map_snapshot, map_extra, 48);
G2_ABI_SIZE(g2_guard_object_snapshot, 64);
G2_ABI_OFFSET(g2_guard_object_snapshot, attributes, 8);
G2_ABI_SIZE(g2_guard_record, 192);
G2_ABI_OFFSET(g2_guard_record, object_snapshot, 128);
G2_ABI_OFFSET(g2_guard_record, sequence, 0);
G2_ABI_OFFSET(g2_guard_record, kind, 8);
G2_ABI_OFFSET(g2_guard_record, flags, 12);
G2_ABI_OFFSET(g2_guard_record, kernel_id, 24);
G2_ABI_OFFSET(g2_guard_record, root_cookie, 32);
G2_ABI_OFFSET(g2_guard_record, cgroup_id, 40);
G2_ABI_OFFSET(g2_guard_record, task_cookie, 48);
G2_ABI_OFFSET(g2_guard_record, task_start_ns, 56);
G2_ABI_OFFSET(g2_guard_record, result, 76);
G2_ABI_OFFSET(g2_guard_record, object_cookie, 16);
G2_ABI_OFFSET(g2_guard_record, audit_serial, 64);
G2_ABI_OFFSET(g2_guard_record, operation, 72);
G2_ABI_OFFSET(g2_guard_record, parent_cookie, 80);
G2_ABI_OFFSET(g2_guard_record, detail, 88);
G2_ABI_SIZE(g2_guard_task_detail, 40);
G2_ABI_OFFSET(g2_guard_task_detail, tgid, 0);
G2_ABI_OFFSET(g2_guard_task_detail, tid, 4);
G2_ABI_OFFSET(g2_guard_task_detail, exe_ino, 16);
G2_ABI_OFFSET(g2_guard_task_detail, pad, 36);
G2_ABI_OFFSET(g2_guard_task_detail, exe_dev, 8);
G2_ABI_OFFSET(g2_guard_task_detail, exec_generation, 24);
G2_ABI_OFFSET(g2_guard_task_detail, exit_code, 32);
G2_ABI_SIZE(g2_guard_raw_syscall_detail, 40);
G2_ABI_OFFSET(g2_guard_raw_syscall_detail, tgid, 0);
G2_ABI_OFFSET(g2_guard_raw_syscall_detail, tid, 4);
G2_ABI_OFFSET(g2_guard_raw_syscall_detail, seconds, 8);
G2_ABI_OFFSET(g2_guard_raw_syscall_detail, nanoseconds, 16);
G2_ABI_OFFSET(g2_guard_raw_syscall_detail, pad, 20);
G2_ABI_OFFSET(g2_guard_raw_syscall_detail, exe_dev, 24);
G2_ABI_OFFSET(g2_guard_raw_syscall_detail, exe_ino, 32);
G2_ABI_SIZE(g2_guard_seal, 96);
G2_ABI_OFFSET(g2_guard_seal, count, 8);
G2_ABI_OFFSET(g2_guard_seal, link_fds, 12);
G2_ABI_OFFSET(g2_guard_seal, bootstrap_digest, 24);
G2_ABI_OFFSET(g2_guard_seal, epoch, 56);
G2_ABI_OFFSET(g2_guard_seal, link_ids, 72);
G2_ABI_OFFSET(g2_guard_seal, program_ids, 84);
G2_ABI_SIZE(g2_guard_status, 240);
G2_ABI_OFFSET(g2_guard_status, epoch, 8);
G2_ABI_OFFSET(g2_guard_status, state, 24);
G2_ABI_OFFSET(g2_guard_status, sticky, 28);
G2_ABI_OFFSET(g2_guard_status, authority_phase, 32);
G2_ABI_OFFSET(g2_guard_status, pad, 36);
G2_ABI_OFFSET(g2_guard_status, sequence, 40);
G2_ABI_OFFSET(g2_guard_status, ack_cursor, 48);
G2_ABI_OFFSET(g2_guard_status, observer_ack_cursor, 56);
G2_ABI_OFFSET(g2_guard_status, lost, 64);
G2_ABI_OFFSET(g2_guard_status, probe_missed, 72);
G2_ABI_OFFSET(g2_guard_status, broker_cookie, 80);
G2_ABI_OFFSET(g2_guard_status, observer_cookie, 88);
G2_ABI_OFFSET(g2_guard_status, death_sequence, 96);
G2_ABI_OFFSET(g2_guard_status, module_instance, 104);
G2_ABI_OFFSET(g2_guard_status, module_dev, 112);
G2_ABI_OFFSET(g2_guard_status, module_ino, 120);
G2_ABI_OFFSET(g2_guard_status, module_size, 128);
G2_ABI_OFFSET(g2_guard_status, link_ids, 136);
G2_ABI_OFFSET(g2_guard_status, program_ids, 148);
G2_ABI_OFFSET(g2_guard_status, link_attached, 160);
G2_ABI_OFFSET(g2_guard_status, tail_pad, 172);
G2_ABI_OFFSET(g2_guard_status, bootstrap_digest, 176);
G2_ABI_OFFSET(g2_guard_status, observer_bindings_sha256, 208);
G2_ABI_SIZE(g2_guard_pin_task, 40);
G2_ABI_OFFSET(g2_guard_pin_task, tid, 8);
G2_ABI_OFFSET(g2_guard_pin_task, pad, 12);
G2_ABI_OFFSET(g2_guard_pin_task, cookie, 16);
G2_ABI_OFFSET(g2_guard_pin_task, tgid, 24);
G2_ABI_OFFSET(g2_guard_pin_task, actual_tid, 28);
G2_ABI_OFFSET(g2_guard_pin_task, start_boottime_ns, 32);
G2_ABI_SIZE(g2_guard_import_fd, 32);
G2_ABI_OFFSET(g2_guard_import_fd, cookie, 8);
G2_ABI_OFFSET(g2_guard_import_fd, fd, 16);
G2_ABI_OFFSET(g2_guard_import_fd, pad, 20);
G2_ABI_OFFSET(g2_guard_import_fd, output_pad, 28);
G2_ABI_OFFSET(g2_guard_import_fd, installed_fd, 24);
G2_ABI_SIZE(g2_guard_drop_task, 16);
G2_ABI_OFFSET(g2_guard_drop_task, cookie, 8);
G2_ABI_SIZE(g2_guard_enroll_cgroup, 48);
G2_ABI_OFFSET(g2_guard_enroll_cgroup, cgroup_fd, 8);
G2_ABI_OFFSET(g2_guard_enroll_cgroup, grant_id, 16);
G2_ABI_OFFSET(g2_guard_enroll_cgroup, root_cookie, 24);
G2_ABI_OFFSET(g2_guard_enroll_cgroup, kernfs_id, 32);
G2_ABI_OFFSET(g2_guard_enroll_cgroup, pad, 12);
G2_ABI_OFFSET(g2_guard_enroll_cgroup, enrollment, 40);
G2_ABI_SIZE(g2_guard_retire_cgroup, 32);
G2_ABI_OFFSET(g2_guard_retire_cgroup, enrollment, 8);
G2_ABI_OFFSET(g2_guard_retire_cgroup, observed_state, 16);
G2_ABI_OFFSET(g2_guard_retire_cgroup, pad, 20);
G2_ABI_OFFSET(g2_guard_retire_cgroup, sequence, 24);
G2_ABI_SIZE(g2_guard_read_events, 12344);
G2_ABI_OFFSET(g2_guard_read_events, max_records, 8);
G2_ABI_OFFSET(g2_guard_read_events, after, 16);
G2_ABI_OFFSET(g2_guard_read_events, ack, 24);
G2_ABI_OFFSET(g2_guard_read_events, first, 32);
G2_ABI_OFFSET(g2_guard_read_events, last, 40);
G2_ABI_OFFSET(g2_guard_read_events, count, 48);
G2_ABI_OFFSET(g2_guard_read_events, records, 56);
G2_ABI_OFFSET(g2_guard_read_events, pad, 12);
G2_ABI_OFFSET(g2_guard_read_events, sticky, 52);
G2_ABI_SIZE(g2_guard_watch_daemon, 24);
G2_ABI_OFFSET(g2_guard_watch_daemon, cookie, 8);
G2_ABI_OFFSET(g2_guard_watch_daemon, sequence, 16);
G2_ABI_SIZE(g2_guard_bind_observer, 24);
G2_ABI_OFFSET(g2_guard_bind_observer, cookie, 8);
G2_ABI_OFFSET(g2_guard_bind_observer, installed_fd, 16);
G2_ABI_OFFSET(g2_guard_bind_observer, pad, 20);
G2_ABI_SIZE(g2_guard_revoke, 8);
G2_ABI_SIZE(g2_guard_watch_probe, 32);
G2_ABI_OFFSET(g2_guard_watch_probe, cookie, 8);
G2_ABI_OFFSET(g2_guard_watch_probe, probe_kind, 16);
G2_ABI_OFFSET(g2_guard_watch_probe, pad, 20);
G2_ABI_OFFSET(g2_guard_watch_probe, sequence, 24);
G2_ABI_SIZE(g2_guard_selftest_observation_fault, 16);
G2_ABI_OFFSET(g2_guard_selftest_observation_fault, variant, 8);
G2_ABI_OFFSET(g2_guard_selftest_observation_fault, pad, 12);
G2_ABI_SIZE(g2_guard_selftest_id_reuse, 48);
G2_ABI_OFFSET(g2_guard_selftest_id_reuse, header, 0);
_Static_assert(_IOC_NR(G2_GUARD_SELFTEST_ID_REUSE) == 14 && _IOC_SIZE(G2_GUARD_SELFTEST_ID_REUSE) == 48, "selftest ID reuse ioctl differs");
G2_ABI_OFFSET(g2_guard_selftest_id_reuse, first_object_cookie, 8);
G2_ABI_OFFSET(g2_guard_selftest_id_reuse, first_removal_sequence, 16);
G2_ABI_OFFSET(g2_guard_selftest_id_reuse, next_transaction_id, 24);
G2_ABI_OFFSET(g2_guard_selftest_id_reuse, nonce, 32);
G2_ABI_SIZE(g2_guard_selftest_id_cursor_detail, 40);
G2_ABI_OFFSET(g2_guard_selftest_id_cursor_detail, first_removal_sequence, 0);
G2_ABI_OFFSET(g2_guard_selftest_id_cursor_detail, transaction_id, 8);
G2_ABI_OFFSET(g2_guard_selftest_id_cursor_detail, saved_cursor, 16);
G2_ABI_OFFSET(g2_guard_selftest_id_cursor_detail, requested_id, 20);
G2_ABI_OFFSET(g2_guard_selftest_id_cursor_detail, allocator_result, 24);
G2_ABI_OFFSET(g2_guard_selftest_id_cursor_detail, cursor_after, 28);
G2_ABI_OFFSET(g2_guard_selftest_id_cursor_detail, pad, 32);
#undef G2_ABI_SIZE
#undef G2_ABI_OFFSET

#endif
