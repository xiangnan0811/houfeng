//go:build linux && amd64 && !cgo

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"

	"golang.org/x/sys/unix"
)

type brokerReadState struct {
	bootID, epoch, runToken string
	guard                   guardDevice
	transcript              *authorityTranscript
	checkpoint              *checkpointController
}

func controlACK(s *brokerReadState, p *controlPrincipal, requestID uint64, request []byte, status string, body map[string]any) ([]byte, error) {
	if s == nil || p == nil || requestID == 0 || body == nil {
		return nil, errControlFrame
	}
	digest := sha256.Sum256(request)
	ack := map[string]any{
		"version": json.Number("1"), "boot_id": s.bootID, "epoch": s.epoch, "run_token": s.runToken,
		"principal_id": jsonU64(p.ID), "request_id": jsonU64(requestID), "request_sha256": hex.EncodeToString(digest[:]),
		"status": status, "body": body,
	}
	return canonicalControl(ack)
}

func (s *brokerReadState) readStatus() (map[string]any, string) {
	var st guardStatus
	var err error
	if s.checkpoint != nil && s.checkpoint.active != nil {
		c := s.checkpoint
		var ready bool
		st, ready, err = pollAuditWatermark(c.guard, c.audit, c.transcript, c.join, &c.cursor, c.lifecycle)
		if err != nil {
			c.markFault()
		} else if !ready {
			err = unix.EAGAIN
		}
	} else {
		st, err = s.guard.status()
	}
	if errors.Is(err, unix.EAGAIN) {
		return map[string]any{"code": "BPF_AUTH_PENDING", "sequence": json.Number("0")}, "BPF_AUTH_PENDING"
	}
	if err != nil {
		return map[string]any{"code": "BPF_AUTH_UNAVAILABLE", "sequence": json.Number("0")}, "BPF_AUTH_UNAVAILABLE"
	}
	states := [...]string{"unsealed", "sealed", "dead"}
	if st.State >= uint32(len(states)) {
		return map[string]any{"code": "BPF_AUTH_UNAVAILABLE", "sequence": json.Number("0")}, "BPF_AUTH_UNAVAILABLE"
	}
	return map[string]any{"state": states[st.State], "sticky": jsonU64(uint64(st.Sticky)), "sequence": jsonU64(st.Sequence), "lost": jsonU64(st.Lost), "probe_missed": jsonU64(st.ProbeMissed)}, "OK"
}

// readEvidenceACK emits the longest whole prefix that fits in the actual ACK,
// including its envelope. It never truncates an individual record.
func (s *brokerReadState) readEvidenceACK(p *controlPrincipal, requestID uint64, raw []byte, body map[string]any) ([]byte, error) {
	if s.transcript == nil || !exactKeys(body, "after", "max_records") {
		return nil, errControlFrame
	}
	after, ok := exactNumber(body["after"])
	if !ok {
		return nil, errControlFrame
	}
	max, ok := exactNumber(body["max_records"])
	if !ok || max == 0 || max > 64 {
		return nil, errControlFrame
	}
	records, next, complete, err := s.transcript.recordsAfter(after, uint32(max))
	if err != nil {
		return nil, err
	}
	for {
		ack, e := controlACK(s, p, requestID, raw, "OK", map[string]any{"records": records, "next": jsonU64(next), "complete": complete})
		if e == nil {
			return ack, nil
		}
		if len(records) <= 1 {
			return nil, e
		}
		records = records[:len(records)-1]
		next--
		complete = false
	}
}

// handleReadControl consumes and responds to READ_STATUS/READ_EVIDENCE on an
// already credentialled connection. It performs no registration or policy
// effect and returns handled=false for another authorized operation.
func (s *brokerReadState) handleReadControl(fd int, p *controlPrincipal, raw []byte, cred *unix.Ucred, actual guardTask) (bool, error) {
	m, err := parseCanonicalControl(raw)
	if err != nil {
		return false, err
	}
	op, _ := m["operation"].(string)
	if op != "READ_STATUS" && op != "READ_EVIDENCE" {
		return false, nil
	}
	peer, err := unix.GetsockoptUcred(fd, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil || peer.Pid <= 0 || cred == nil || peer.Pid != cred.Pid || peer.Uid != cred.Uid || peer.Gid != cred.Gid {
		return true, errControlFrame
	}
	replay, err := p.authenticate(m, raw, cred, actual, s.bootID, s.epoch, s.runToken, nil)
	if err != nil {
		return true, err
	}
	if replay != nil {
		return true, sendControl(fd, replay.Ack, nil)
	}
	id, _ := exactNumber(m["request_id"])
	if id == 0 || id == math.MaxUint64 {
		return true, errControlFrame
	}
	body, ok := m["body"].(map[string]any)
	if !ok {
		return true, errControlFrame
	}
	var ack []byte
	if op == "READ_STATUS" {
		if !exactKeys(body) {
			return true, errControlFrame
		}
		response, status := s.readStatus()
		ack, err = controlACK(s, p, id, raw, status, response)
	} else {
		ack, err = s.readEvidenceACK(p, id, raw, body)
	}
	if err != nil {
		return true, err
	}
	if err = p.commitReply(id, raw, ack, nil); err != nil {
		return true, err
	}
	return true, sendControl(fd, ack, nil)
}
