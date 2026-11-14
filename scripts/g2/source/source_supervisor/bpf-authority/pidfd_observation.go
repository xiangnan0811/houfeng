//go:build linux && amd64 && !cgo

package main

import "golang.org/x/sys/unix"

type selectorReason uint8

const (
	selectorUnknown selectorReason = iota
	selectorInvalidFD
	selectorZeroPID
	selectorPrePollError
	selectorPrePollRejected
	selectorFDInfoRead
	selectorPIDMissing
	selectorPIDDuplicate
	selectorPIDParse
	selectorPIDMismatch
	selectorPostPollError
	selectorPostPollRejected
)

type selectorPollPhase uint8

const (
	selectorPollPre selectorPollPhase = iota
	selectorPollPost
)

type selectorObservation struct {
	reason    selectorReason
	pollFacts uint8
	err       error
}

func normalizeSelectorPoll(n int, revents int16) uint8 {
	var facts uint8
	if revents&unix.POLLIN != 0 {
		facts |= 1
	}
	if revents&unix.POLLNVAL != 0 {
		facts |= 2
	}
	if revents&unix.POLLHUP != 0 {
		facts |= 4
	}
	if revents&unix.POLLERR != 0 {
		facts |= 8
	}
	if revents & ^int16(unix.POLLIN|unix.POLLNVAL|unix.POLLHUP|unix.POLLERR) != 0 {
		facts |= 16
	}
	if n != 0 {
		facts |= 32
	}
	return facts
}
