//go:build linux && amd64 && !cgo

package main

import (
	"errors"
	"math"

	"golang.org/x/sys/unix"
)

// settleNotification records the actual one-shot response ioctl, including the
// kernel's known ENOENT terminal result. Unknown results remain unresolved.
func (b *authorityBroker) settleNotification(t *runtimeEffectTransaction, h *heldNotification, value int64, errno unix.Errno, sourceFD int) (returned any, undelivered bool, err error) {
	if b == nil || b.checkpoint == nil || b.effects == nil || t == nil || b.effects.transactions[t.id] != t || t.state != "effect_observed" || h == nil || h.memory == nil || h.notice.ID != t.notificationID || h.task.Cookie != t.requesterCookie || t.invocationID == 0 || errno > 4095 || errno != 0 && value != 0 || sourceFD >= 0 && (errno != 0 || value != 0) {
		return nil, false, errGuardProtocol
	}
	c := b.checkpoint
	if err = drainGuardEvents(c.guard, c.transcript, c.join, &c.cursor, c.lifecycle); err != nil {
		c.markFault()
		return nil, false, err
	}
	status, e := c.guard.statusObserved(2)
	if e != nil || status.State != 1 || status.Sticky != 0 || status.Lost != 0 || status.ProbeMissed != 0 || status.Sequence != c.cursor || status.AckCursor != c.cursor {
		c.markFault()
		return nil, false, errGuardProtocol
	}
	before := c.cursor
	operation := "SEND"
	var response any = map[string]any{"value": jsonI64(value), "error": jsonI64(-int64(errno)), "flags": jsonU64(0)}
	if sourceFD >= 0 {
		operation, response = "ADDFD_SEND", nil
	}
	body := map[string]any{"transaction_id": jsonU64(t.id), "invocation_id": jsonU64(t.invocationID), "requester_cookie": jsonU64(t.requesterCookie), "notification_id": jsonU64(t.notificationID), "operation": operation, "response": response, "ioctl_errno": jsonU64(uint64(unix.ENOENT)), "returned_fd": nil, "guard_before": jsonU64(before), "guard_after": jsonU64(math.MaxUint64)}
	if sourceFD >= 0 {
		body["returned_fd"] = jsonU64(math.MaxInt32)
	}
	if err = preflightRuntimeRecord(b, "notification_response", body); err != nil {
		c.markFault()
		return nil, false, err
	}
	var callErr error
	if sourceFD >= 0 {
		var fd int
		fd, callErr = injectNotificationFD(h.memory.listener, h.notice.ID, sourceFD)
		if callErr == nil {
			returned = jsonU64(uint64(fd))
		}
	} else {
		callErr = replyNotification(h.memory.listener, h.notice.ID, value, errno)
	}
	if b.diagnostic != nil && t.publication != nil && b.effects.objects != nil {
		var code unix.Errno
		_ = errors.As(callErr, &code)
		kind := uint64(1)
		if sourceFD >= 0 {
			kind = 2
		}
		o := b.effects.objects.byCookie[t.publication.ObjectCookie]
		b.diagnostic.objectFact(o, 18, 0, 0, kind, uint64(code))
		if callErr != nil && code == 0 {
			b.diagnostic.objectGap(o, 18)
		}
	}
	if callErr != nil && !errors.Is(callErr, unix.ENOENT) {
		c.markFault()
		return nil, false, callErr
	}
	undelivered = errors.Is(callErr, unix.ENOENT)
	if err = drainGuardEvents(c.guard, c.transcript, c.join, &c.cursor, c.lifecycle); err != nil {
		c.markFault()
		return nil, false, err
	}
	status, e = c.guard.statusObserved(3)
	if e != nil || status.State != 1 || status.Sticky != 0 || status.Lost != 0 || status.ProbeMissed != 0 || status.Sequence != c.cursor || status.AckCursor != c.cursor || c.cursor < before {
		c.markFault()
		return nil, false, errGuardProtocol
	}
	body["ioctl_errno"] = jsonU64(0)
	if undelivered {
		body["ioctl_errno"] = jsonU64(uint64(unix.ENOENT))
	}
	body["returned_fd"], body["guard_after"] = returned, jsonU64(c.cursor)
	if err = appendRuntimeRecord(b, "notification_response", body); err != nil {
		c.markFault()
		return nil, false, err
	}
	return returned, undelivered, nil
}
