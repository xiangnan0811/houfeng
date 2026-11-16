/* SPDX-License-Identifier: GPL-2.0-only */
#ifndef G2_BPF_GUARD_EVENT_RING_H
#define G2_BPF_GUARD_EVENT_RING_H

#include <linux/errno.h>
#include <linux/limits.h>
#include <linux/ktime.h>
#include <linux/poll.h>
#include <linux/spinlock.h>
#include <linux/string.h>
#include <linux/types.h>
#include <linux/wait.h>

#include "abi.h"

/* Producers run in probe/tracepoint context; the ring never overwrites data. */
struct g2_event_ring {
	spinlock_t lock;
	wait_queue_head_t waiters;
	struct g2_guard_record records[G2_GUARD_RING_CAPACITY];
	__u64 sequence;
	__u64 ack_cursor;
	__u64 returned_cursor;
	__u64 observer_ack_cursor;
	__u64 observer_returned_cursor;
	__u64 lost;
	__u32 pending_pairs;
	bool sealed;
	bool observer_bound;
	bool broker_retired;
	bool fault_hold_ack;
	bool fault_drop_one_pair;
	__u32 sticky;
	__u32 authority_phase;
	__u64 *liveness_slot;
	__u64 broker_cookie;
	__u64 broker_start_ns;
};

static void g2_event_ring_init(struct g2_event_ring *ring)
{
	memset(ring, 0, sizeof(*ring));
	spin_lock_init(&ring->lock);
	init_waitqueue_head(&ring->waiters);
	ring->authority_phase = 1;
}

/* Caller holds ring->lock. Only 1->2, 1->0 and 2->0 are transitions;
 * enum values do not have numerical authority ordering. */
static int g2_event_ring_append_locked(struct g2_event_ring *ring,
		const struct g2_guard_record *record, __u64 *sequence);

static void g2_event_ring_lower_phase_locked(struct g2_event_ring *ring,
		__u32 target_phase)
{
	struct g2_guard_record record = {};

	if (!((ring->authority_phase == 1 &&
	       (target_phase == 2 || target_phase == 0)) ||
	      (ring->authority_phase == 2 && target_phase == 0)))
		return;
	ring->authority_phase = target_phase;
	if (ring->liveness_slot)
		WRITE_ONCE(*ring->liveness_slot, (__u64)target_phase);
	if (ring->broker_cookie) {
		record.kind = 14;
		record.task_cookie = ring->broker_cookie;
		record.task_start_ns = ring->broker_start_ns;
		record.operation = target_phase;
		g2_event_ring_append_locked(ring, &record, NULL);
	}
}

static void g2_event_ring_invalidate_locked(struct g2_event_ring *ring)
{
	ring->sticky = 1;
	if (ring->authority_phase == 1)
		g2_event_ring_lower_phase_locked(ring, 2);
}

static void g2_event_ring_invalidate(struct g2_event_ring *ring)
{
	unsigned long flags;

	spin_lock_irqsave(&ring->lock, flags);
	g2_event_ring_invalidate_locked(ring);
	spin_unlock_irqrestore(&ring->lock, flags);
	wake_up_interruptible_poll(&ring->waiters, EPOLLERR);
}

static __u64 g2_event_ring_effective_ack_locked(const struct g2_event_ring *ring)
{
	if (ring->fault_hold_ack) {
		if (ring->observer_bound)
			return ring->ack_cursor < ring->observer_ack_cursor ?
			       ring->ack_cursor : ring->observer_ack_cursor;
		return ring->ack_cursor;
	}
	if (ring->broker_retired) {
		if (ring->observer_bound)
			return ring->observer_ack_cursor;
		return ring->ack_cursor;
	}
	if (ring->observer_bound)
		return ring->ack_cursor < ring->observer_ack_cursor ?
		       ring->ack_cursor : ring->observer_ack_cursor;
	return ring->ack_cursor;
}

/* Caller holds ring->lock. */
static int g2_event_ring_append_locked(struct g2_event_ring *ring,
		const struct g2_guard_record *record, __u64 *sequence)
{
	struct g2_guard_record *slot;
	__u64 next, min_ack;

	if ((record->kind == 1 || record->kind == 3) ?
	    (!record->object_snapshot.type || record->object_snapshot.reserved ||
	     (record->kind == 1 ?
	      memchr_inv(record->object_snapshot.attributes.program.reserved, 0, 8) != NULL :
	      record->object_snapshot.attributes.map.pad != 0)) :
	    memchr_inv(&record->object_snapshot, 0, sizeof(record->object_snapshot)) != NULL) {
		g2_event_ring_invalidate_locked(ring);
		return -EINVAL;
	}
	min_ack = g2_event_ring_effective_ack_locked(ring);
	if (ring->sequence == U64_MAX ||
	    ring->sequence - min_ack >= G2_GUARD_RING_CAPACITY) {
		if (ring->lost != U64_MAX)
			ring->lost++;
		ring->sticky = 1;
		/* If a broker is known, the attempted transition record is lost too. */
		if (ring->authority_phase == 1)
			g2_event_ring_lower_phase_locked(ring, 2);
		return -ENOSPC;
	}
	next = ++ring->sequence;
	slot = &ring->records[(next - 1) % G2_GUARD_RING_CAPACITY];
	*slot = *record;
	if (slot->kind == 2 || slot->kind == 4 || slot->kind == 5 || slot->kind == 6) {
		__u64 observed = ktime_get_ns();
		unsigned int i;

		memset(slot->detail, 0, sizeof(slot->detail));
		for (i = 0; i < sizeof(observed); i++)
			slot->detail[i] = (__u8)(observed >> (8 * i));
	}
	if (!ring->sealed && slot->kind >= 1 && slot->kind <= 8)
		slot->flags |= 4;
	slot->sequence = next;
	if (sequence)
		*sequence = next;
	return 0;
}

static int g2_event_ring_append(struct g2_event_ring *ring,
		const struct g2_guard_record *record, __u64 *sequence)
{
	unsigned long flags;
	int error;

	spin_lock_irqsave(&ring->lock, flags);
	error = g2_event_ring_append_locked(ring, record, sequence);
	spin_unlock_irqrestore(&ring->lock, flags);
	wake_up_interruptible_poll(&ring->waiters, EPOLLIN | EPOLLERR);
	return error;
}

/* Pair count and completion records share the STATUS/ring lock. */
static int g2_event_pair_begin(struct g2_event_ring *ring)
{
	unsigned long flags;
	int error = 0;

	spin_lock_irqsave(&ring->lock, flags);
	if (ring->pending_pairs == U32_MAX) {
		g2_event_ring_invalidate_locked(ring);
		error = -EOVERFLOW;
	} else {
		ring->pending_pairs++;
	}
	spin_unlock_irqrestore(&ring->lock, flags);
	if (error)
		wake_up_interruptible_poll(&ring->waiters, EPOLLERR);
	return error;
}

/* A null record completes a real no-event pair, such as a zero-ID removal. */
static int g2_event_pair_append_locked(struct g2_event_ring *ring,
		const struct g2_guard_record *record, __u64 *sequence)
{
	if (ring->fault_drop_one_pair) {
		ring->fault_drop_one_pair = false;
		if (ring->lost != U64_MAX)
			ring->lost++;
		g2_event_ring_invalidate_locked(ring);
		return -EIO;
	}
	return g2_event_ring_append_locked(ring, record, sequence);
}

static __poll_t g2_event_ring_poll_role(struct g2_event_ring *ring,
		struct file *file, poll_table *wait, bool is_observer)
{
	unsigned long flags;
	__poll_t ready = 0;
	__u64 ack;

	poll_wait(file, &ring->waiters, wait);
	spin_lock_irqsave(&ring->lock, flags);
	ack = is_observer ? ring->observer_ack_cursor : ring->ack_cursor;
	if (!ring->pending_pairs)
		ready |= EPOLLPRI;
	if (ring->sequence > ack)
		ready |= EPOLLIN | EPOLLRDNORM;
	if (ring->sticky && !is_observer)
		ready |= EPOLLERR;
	spin_unlock_irqrestore(&ring->lock, flags);
	return ready;
}

/* The caller must mark returned_cursor only after the entire copy_to_user. */
static int g2_event_ring_prepare_role(struct g2_event_ring *ring,
		struct g2_guard_read_events *request, bool is_observer)
{
	unsigned long flags;
	__u64 cursor, last;
	__u64 *ack_ptr, *ret_ptr;
	unsigned int count = 0;
	int error = 0;

	if (!request->max_records || request->max_records > G2_GUARD_EVENT_SLOTS ||
	    request->pad || request->first || request->last || request->count ||
	    request->sticky || memchr_inv(request->records, 0, sizeof(request->records)))
		return -EINVAL;
	spin_lock_irqsave(&ring->lock, flags);
	ack_ptr = is_observer ? &ring->observer_ack_cursor : &ring->ack_cursor;
	ret_ptr = is_observer ? &ring->observer_returned_cursor : &ring->returned_cursor;
	if (request->ack < *ack_ptr ||
	    (request->ack != *ack_ptr && request->ack != *ret_ptr) ||
	    request->after < request->ack || request->after > ring->sequence ||
	    (request->after != request->ack && request->after != *ret_ptr)) {
		g2_event_ring_invalidate_locked(ring);
		error = -EINVAL;
		goto out;
	}
	if (!ring->fault_hold_ack)
		*ack_ptr = request->ack;
	cursor = request->after;
	last = ring->sequence;
	while (cursor < last && count < request->max_records) {
		const struct g2_guard_record *slot;

		cursor++;
		slot = &ring->records[(cursor - 1) % G2_GUARD_RING_CAPACITY];
		if (slot->sequence != cursor) {
			g2_event_ring_invalidate_locked(ring);
			error = -EIO;
			goto out;
		}
		request->records[count++] = *slot;
	}
	request->count = count;
	request->first = count ? request->records[0].sequence : 0;
	request->last = count ? cursor : 0;
	request->sticky = ring->sticky;
out:
	spin_unlock_irqrestore(&ring->lock, flags);
	return error;
}

static void g2_event_ring_returned_role(struct g2_event_ring *ring,
		const struct g2_guard_read_events *request, bool is_observer)
{
	unsigned long flags;
	__u64 *ret_ptr;

	if (!request->count)
		return;
	spin_lock_irqsave(&ring->lock, flags);
	ret_ptr = is_observer ? &ring->observer_returned_cursor : &ring->returned_cursor;
	if (request->last > *ret_ptr)
		*ret_ptr = request->last;
	spin_unlock_irqrestore(&ring->lock, flags);
}
#endif
