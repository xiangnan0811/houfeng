//go:build linux && amd64 && !cgo

package main

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

func auditReaderPID(fd int) (uint32, error) {
	b, err := readHeldProc(fmt.Sprintf("self/fdinfo/%d", fd), 65536)
	if err != nil || len(b) > 65536 {
		return 0, errGuardProtocol
	}
	var pid uint64
	found := false
	for _, line := range strings.Split(string(b), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok || key != "Pid" {
			continue
		}
		if found {
			return 0, errGuardProtocol
		}
		found = true
		pid, err = strconv.ParseUint(strings.TrimSpace(value), 10, 32)
		if err != nil {
			return 0, errGuardProtocol
		}
	}
	if !found || pid <= 1 || !pidfdSelects(fd, uint32(pid)) {
		return 0, errGuardProtocol
	}
	return uint32(pid), nil
}

func readerWait(fd int, events int16, peerfd int, deadline uint64) error {
	for {
		now, err := monoNowNS()
		if err != nil {
			return err
		}
		if now >= deadline {
			return unix.ETIMEDOUT
		}
		p := []unix.PollFd{{Fd: int32(fd), Events: events}}
		if peerfd >= 0 {
			p = append(p, unix.PollFd{Fd: int32(peerfd), Events: unix.POLLIN})
		}
		n, err := unix.Poll(p, int((deadline-now+999999)/1000000))
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return unix.ETIMEDOUT
		}
		if (len(p) > 1 && p[1].Revents != 0) || p[0].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
			return errGuardProtocol
		}
		if p[0].Revents&events != 0 {
			return nil
		}
	}
}

// readerQuery uses one unsubscribed NETLINK_AUDIT socket and records exactly
// the received kernel headers and payload, including the real multipart DONE.
func readerQuery(fd, brokerPIDFD int, port, seq uint32, kind uint16, deadline uint64) (map[string]any, error) {
	request := make([]byte, 16)
	binary.LittleEndian.PutUint32(request, 16)
	binary.LittleEndian.PutUint16(request[4:], kind)
	binary.LittleEndian.PutUint16(request[6:], unix.NLM_F_REQUEST)
	binary.LittleEndian.PutUint32(request[8:], seq)
	binary.LittleEndian.PutUint32(request[12:], port)
	for {
		err := unix.Sendto(fd, request, 0, &unix.SockaddrNetlink{Family: unix.AF_NETLINK})
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if errors.Is(err, unix.EAGAIN) {
			if err = readerWait(fd, unix.POLLOUT, brokerPIDFD, deadline); err != nil {
				return nil, err
			}
			continue
		}
		if err != nil {
			return nil, err
		}
		break
	}
	replies := []any{}
	complete := false
	for !complete {
		if err := readerWait(fd, unix.POLLIN, brokerPIDFD, deadline); err != nil {
			return nil, err
		}
		data := make([]byte, maxControlFrame+1)
		oob := make([]byte, 256)
		n, on, flags, sender, err := unix.Recvmsg(fd, data, oob, unix.MSG_CMSG_CLOEXEC)
		if errors.Is(err, unix.EINTR) || errors.Is(err, unix.EAGAIN) {
			continue
		}
		if err != nil {
			return nil, err
		}
		source, ok := sender.(*unix.SockaddrNetlink)
		if !ok || source.Pid != 0 || source.Groups != 0 || on != 0 || flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 || n == 0 || n > maxControlFrame {
			return nil, errGuardProtocol
		}
		data = data[:n]
		for off := 0; off < len(data); {
			if len(data)-off < 16 {
				return nil, errGuardProtocol
			}
			hdr := data[off:]
			length := binary.LittleEndian.Uint32(hdr)
			if length < 16 || uint64(length) > uint64(len(hdr)) {
				return nil, errGuardProtocol
			}
			typ := binary.LittleEndian.Uint16(hdr[4:])
			fl := binary.LittleEndian.Uint16(hdr[6:])
			s := binary.LittleEndian.Uint32(hdr[8:])
			hp := binary.LittleEndian.Uint32(hdr[12:])
			if complete || s != seq || fl&unix.NLM_F_DUMP_INTR != 0 || typ == unix.NLMSG_ERROR || typ == unix.NLMSG_OVERRUN {
				return nil, errGuardProtocol
			}
			payload := hdr[16:length]
			if kind == auditGetType {
				if typ != auditGetType || fl != 0 || len(payload) != 44 || len(replies) != 0 {
					return nil, errGuardProtocol
				}
				complete = true
			} else if typ == unix.NLMSG_DONE {
				if fl != unix.NLM_F_MULTI || len(payload) != 0 && !(len(payload) == 4 && guardU32(payload, 0) == 0) {
					return nil, errGuardProtocol
				}
				complete = true
			} else if typ != auditListRulesType || fl != unix.NLM_F_MULTI || !auditRuleFramed(payload) {
				return nil, errGuardProtocol
			}
			replies = append(replies, map[string]any{"source_pid": jsonU64(uint64(source.Pid)), "source_groups": jsonU64(uint64(source.Groups)), "length": jsonU64(uint64(length)), "type": jsonU64(uint64(typ)), "flags": jsonU64(uint64(fl)), "sequence": jsonU64(uint64(s)), "header_pid": jsonU64(uint64(hp)), "payload_hex": hex.EncodeToString(payload)})
			if len(replies) > 5 {
				return nil, errGuardProtocol
			}
			aligned := (int(length) + 3) &^ 3
			if aligned > len(data)-off {
				return nil, errGuardProtocol
			}
			off += aligned
		}
	}
	socketError, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_ERROR)
	if err != nil {
		return nil, err
	}
	if socketError != 0 {
		return nil, unix.Errno(socketError)
	}
	pending := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	if _, err = unix.Poll(pending, 0); err != nil {
		return nil, err
	}
	if pending[0].Revents != 0 {
		return nil, errGuardProtocol
	}
	value := map[string]any{"request_type": jsonU64(uint64(kind)), "request_sequence": jsonU64(uint64(seq)), "request_portid": jsonU64(uint64(port)), "replies": replies}
	if _, err := canonicalControl(value); err != nil {
		return nil, err
	}
	return value, nil
}

func readerWitnessPages(fd int, boot, variant string, id uint64, nonce, role string, raw []byte, deadline time.Time) error {
	if len(raw) == 0 || len(raw) > 32768 {
		return errGuardProtocol
	}
	pages := (len(raw) + 8191) / 8192
	for i := range pages {
		end := (i + 1) * 8192
		if end > len(raw) {
			end = len(raw)
		}
		frame := map[string]any{"version": json.Number("1"), "boot_id": boot, "kind": "reader_witness_chunk", "variant": variant, "request_id": jsonU64(id), "nonce": nonce, "role": role, "page_index": jsonU64(uint64(i)), "page_count": jsonU64(uint64(pages)), "total_bytes": jsonU64(uint64(len(raw))), "total_sha256": hashBytes(raw), "data_b64": base64.StdEncoding.EncodeToString(raw[i*8192 : end])}
		if err := earlyProbeSend(fd, frame, nil, deadline); err != nil {
			return err
		}
	}
	return nil
}

func readerWitnessEvent(fd int, boot, variant string, id uint64, nonce, check string, request, original, sent []byte, result any, sentRights int, endpointClosed any, queries any) error {
	deadline, err := guestDeadline(5 * time.Second)
	if err != nil {
		return err
	}
	if len(original)+len(sent) > 32768 {
		return errGuardProtocol
	}
	if variant == "reader-frame" || variant == "reader-done" || variant == "reader-replay" || variant == "reader-credential" {
		if len(original) != 0 {
			if err = readerWitnessPages(fd, boot, variant, id, nonce, "original_reply", original, deadline); err != nil {
				return err
			}
		}
		if len(sent) != 0 {
			if err = readerWitnessPages(fd, boot, variant, id, nonce, "sent_reply", sent, deadline); err != nil {
				return err
			}
		}
	}
	var origSHA, origBytes, sentSHA, sentBytes any
	if len(original) != 0 {
		origSHA, origBytes = hashBytes(original), jsonU64(uint64(len(original)))
	}
	if len(sent) != 0 {
		sentSHA, sentBytes = hashBytes(sent), jsonU64(uint64(len(sent)))
	}
	frame := map[string]any{"version": json.Number("1"), "boot_id": boot, "kind": "reader_witness_event", "variant": variant, "request_id": jsonU64(id), "nonce": nonce, "check": check, "request_sha256": hashBytes(request), "request_bytes": jsonU64(uint64(len(request))), "original_reply_sha256": origSHA, "original_reply_bytes": origBytes, "sent_reply_sha256": sentSHA, "sent_reply_bytes": sentBytes, "send_result": result, "sent_rights": jsonU64(uint64(sentRights)), "endpoint_closed": endpointClosed, "queries": queries}
	return earlyProbeSend(fd, frame, nil, deadline)
}

// readerWithoutDone changes only the delivered copy of an already validated
// configuration readback; the original remains available to the witness.
func readerWithoutDone(queries []any) ([]any, error) {
	if len(queries) != 3 {
		return nil, errGuardProtocol
	}
	list, ok := queries[1].(map[string]any)
	if !ok || list["request_type"] != jsonU64(auditListRulesType) {
		return nil, errGuardProtocol
	}
	replies, ok := list["replies"].([]any)
	if !ok || len(replies) < 2 {
		return nil, errGuardProtocol
	}
	last, ok := replies[len(replies)-1].(map[string]any)
	if !ok || last["type"] != jsonU64(unix.NLMSG_DONE) {
		return nil, errGuardProtocol
	}
	listCopy := make(map[string]any, len(list))
	for key, value := range list {
		listCopy[key] = value
	}
	listCopy["replies"] = append([]any(nil), replies[:len(replies)-1]...)
	result := append([]any(nil), queries...)
	result[1] = listCopy
	return result, nil
}

func readerSelftestArm(queryFD, witnessFD int, observerPID uint32, observerPIDFD int, boot string, frame map[string]any, rights []int) (string, error) {
	if len(rights) != 1 || !exactKeys(frame, "version", "boot_id", "kind", "request_id", "nonce", "variant", "case_sha256") || frame["version"] != json.Number("1") || frame["boot_id"] != boot || frame["kind"] != "reader_selftest_arm" {
		return "", errGuardProtocol
	}
	id, idOK := exactNumber(frame["request_id"])
	nonce, nonceOK := frame["nonce"].(string)
	variant, variantOK := frame["variant"].(string)
	caseSHA, shaOK := digestString(frame["case_sha256"])
	if !idOK || id == 0 || !nonceOK || !validCheckpointNonce(nonce) || !variantOK || !shaOK {
		return "", errGuardProtocol
	}
	switch variant {
	case "reader-death", "reader-endpoint", "reader-timeout", "reader-replay", "reader-credential", "reader-extra-fd", "reader-frame", "reader-done":
	default:
		return "", errGuardProtocol
	}
	raw, err := readHeldRegular(rights[0], 4096)
	if err != nil || hashBytes(raw) != caseSHA {
		return "", errGuardProtocol
	}
	caseFacts, err := readCanonicalLine(raw)
	if err != nil || caseFacts["version"] != json.Number("1") || caseFacts["run_token"] != authorityRunToken || caseFacts["case"] != "realm-negative" || caseFacts["scenario"] != "bpf-authorization" || caseFacts["bpf_variant"] != variant {
		return "", errGuardProtocol
	}
	deadline, err := guestDeadline(5 * time.Second)
	if err != nil {
		return "", err
	}
	arm := map[string]any{"version": json.Number("1"), "boot_id": boot, "kind": "reader_witness_arm", "request_id": jsonU64(id), "nonce": nonce, "variant": variant, "case_sha256": caseSHA}
	var sentRights []int
	if variant == "reader-death" {
		sentRights = []int{queryFD}
	}
	if err = earlyProbeSend(witnessFD, arm, sentRights, deadline); err != nil {
		return "", err
	}
	ack, cred, _, err := earlyProbeReceive(witnessFD, 0, deadline)
	if err != nil || cred == nil || uint32(cred.Pid) != observerPID || cred.Uid != 0 || cred.Gid != 0 || !pidfdSelects(observerPIDFD, observerPID) || !exactKeys(ack, "version", "boot_id", "kind", "request_id", "nonce", "variant", "case_sha256", "status") || ack["version"] != json.Number("1") || ack["boot_id"] != boot || ack["kind"] != "reader_witness_arm_ack" || ack["request_id"] != jsonU64(id) || ack["nonce"] != nonce || ack["variant"] != variant || ack["case_sha256"] != caseSHA || ack["status"] != "OK" {
		return "", errGuardProtocol
	}
	sentRights = nil
	if variant == "reader-credential" {
		sentRights = []int{queryFD}
	}
	if err = earlyProbeSend(queryFD, map[string]any{"version": json.Number("1"), "boot_id": boot, "kind": "reader_test_armed", "request_id": jsonU64(id), "nonce": nonce}, sentRights, deadline); err != nil {
		return "", err
	}
	return variant, nil
}

// runAuditReader owns the query, broker identity, and private witness descriptors.
func runAuditReader(queryFD, brokerPIDFD, brokerExeFD, witnessFD int) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer unix.Close(queryFD)
	defer unix.Close(brokerPIDFD)
	defer unix.Close(brokerExeFD)
	defer unix.Close(witnessFD)
	if os.Getppid() != 1 || unix.Gettid() != os.Getpid() || queryFD < 0 || brokerPIDFD < 0 || brokerExeFD < 0 || witnessFD < 0 {
		return errGuardProtocol
	}
	for _, name := range []string{"user", "pid", "net"} {
		var initial, own unix.Stat_t
		if unix.Stat("/proc/1/ns/"+name, &initial) != nil || unix.Stat("/proc/self/ns/"+name, &own) != nil || initial.Dev != own.Dev || initial.Ino != own.Ino {
			return errGuardProtocol
		}
	}
	boot, err := fixedBootID()
	if err != nil {
		return err
	}
	brokerPID, err := auditReaderPID(brokerPIDFD)
	if err != nil {
		return err
	}
	self, err := procExecutableHash(os.Getpid())
	if err != nil {
		return err
	}
	held, _, err := heldRegularDigest(brokerExeFD, 128<<20)
	if err != nil || held != self {
		return errGuardProtocol
	}
	brokerHash, err := procExecutableHash(int(brokerPID))
	if err != nil || brokerHash != held {
		return errGuardProtocol
	}
	broker, err := observeProcess(brokerPID, brokerPIDFD)
	if err != nil || broker.PPID != 1 || broker.ExecutableSHA256 != held {
		return errGuardProtocol
	}
	parentHash, err := procExecutableHash(1)
	if err != nil || parentHash != self {
		return errGuardProtocol
	}
	typ, err := unix.GetsockoptInt(queryFD, unix.SOL_SOCKET, unix.SO_TYPE)
	if err != nil || typ != unix.SOCK_SEQPACKET {
		return errGuardProtocol
	}
	creator, err := unix.GetsockoptUcred(queryFD, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil || creator == nil || creator.Pid != 1 || creator.Uid != 0 || creator.Gid != 0 {
		return errGuardProtocol
	}
	if err = unix.SetsockoptInt(queryFD, unix.SOL_SOCKET, unix.SO_PASSCRED, 1); err != nil {
		return err
	}
	if err = unix.SetNonblock(queryFD, true); err != nil {
		return err
	}
	deadlineTime, err := guestDeadline(5 * time.Second)
	if err != nil {
		return err
	}
	if err = waitFD(queryFD, unix.POLLIN, deadlineTime); err != nil {
		return err
	}
	raw, cred, _, err := receiveControl(queryFD, 0)
	if err != nil {
		return err
	}
	init, err := parseCanonicalControl(raw)
	if err != nil || cred == nil || cred.Pid != 1 || cred.Uid != 0 || cred.Gid != 0 || !exactKeys(init, "version", "boot_id", "kind", "nonce") || init["version"] != json.Number("1") || init["boot_id"] != boot || init["kind"] != "reader_init" {
		return errGuardProtocol
	}
	nonce, ok := init["nonce"].(string)
	if !ok || !validCheckpointNonce(nonce) {
		return errGuardProtocol
	}
	if err = confineAuditReader(); err != nil {
		return err
	}
	ready, err := canonicalControl(map[string]any{"version": json.Number("1"), "boot_id": boot, "kind": "reader_ready", "nonce": nonce})
	if err != nil {
		return err
	}
	if err = waitFD(queryFD, unix.POLLOUT, deadlineTime); err != nil {
		return err
	}
	if err = sendControl(queryFD, ready, nil); err != nil {
		return err
	}
	// PID1 authenticates the original observer and supplies its held executable
	// after confinement; SO_PEERCRED alone identifies only the socket creator.
	if typ, e := unix.GetsockoptInt(witnessFD, unix.SOL_SOCKET, unix.SO_TYPE); e != nil || typ != unix.SOCK_SEQPACKET {
		g2d1Fail("WITNESS_SOCKET", e)
		return errGuardProtocol
	}
	if creator, e := unix.GetsockoptUcred(witnessFD, unix.SOL_SOCKET, unix.SO_PEERCRED); e != nil || creator == nil || creator.Pid != 1 || creator.Uid != 0 || creator.Gid != 0 {
		g2d1Fail("WITNESS_CREDENTIALS", e)
		return errGuardProtocol
	}
	witnessDeadline, err := guestDeadline(30 * time.Second)
	if err != nil {
		g2d1Fail("WITNESS_DEADLINE", err)
		return err
	}
	peerFrame, peerCred, peerRights, err := earlyProbeReceive(witnessFD, 2, witnessDeadline)
	if err != nil {
		g2d1Fail("WITNESS_RECEIVE", err)
		return err
	}
	observerPIDFD, observerExeFD := peerRights[0], peerRights[1]
	defer unix.Close(observerPIDFD)
	defer unix.Close(observerExeFD)
	flags, flagsErr := unix.FcntlInt(uintptr(observerExeFD), unix.F_GETFL, 0)
	observerPID, valid := exactNumber(peerFrame["pid"])
	if peerCred == nil || peerCred.Pid != 1 || peerCred.Uid != 0 || peerCred.Gid != 0 || !exactKeys(peerFrame, "version", "boot_id", "kind", "pid", "nonce") || peerFrame["version"] != json.Number("1") || peerFrame["boot_id"] != boot || peerFrame["kind"] != "reader_witness_peer" || peerFrame["nonce"] != nonce || !valid || observerPID <= 1 || observerPID > math.MaxUint32 || !pidfdSelects(observerPIDFD, uint32(observerPID)) || flagsErr != nil || flags&unix.O_ACCMODE != unix.O_RDONLY {
		g2d1Fail("WITNESS_FRAME_PIDFD", nil)
		return errGuardProtocol
	}
	observer, err := observeProcessWithHeldExecutable(uint32(observerPID), observerPIDFD, observerExeFD)
	if err != nil || observer.PPID != 1 || observer.UID != 0 || observer.GID != 0 || observer.ExecutableSHA256 != held || !exactArgumentVector(observer.Argv, []string{"/g2-bpf-authority", "terminal-observer"}) {
		g2d1Fail("OBSERVER_IDENTITY", err)
		return errGuardProtocol
	}
	if err = verifyTerminalObserverConfinement(int(observerPID)); err != nil {
		g2d1Fail("OBSERVER_CONFINEMENT", err)
		return errGuardProtocol
	}
	if !pidfdSelects(observerPIDFD, uint32(observerPID)) {
		return errGuardProtocol
	}
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, unix.NETLINK_AUDIT)
	if err != nil {
		g2d1Fail("NETLINK_SOCKET", err)
		return err
	}
	defer unix.Close(fd)
	if err = unix.Bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		g2d1Fail("NETLINK_BIND", err)
		return err
	}
	address, err := unix.Getsockname(fd)
	local, ok := address.(*unix.SockaddrNetlink)
	if err != nil || !ok || local.Pid == 0 || local.Groups != 0 {
		g2d1Fail("NETLINK_ADDRESS", err)
		return errGuardProtocol
	}
	var nextID uint64 = 1
	var sequence uint32
	var previousDeadline uint64
	var previous, response []byte
	var armedVariant string
	var armedOnce bool
	for {
		// A long-lived idle reader must still observe broker death, without a
		// timeout that would turn inactivity into a new authorization.
		p := []unix.PollFd{{Fd: int32(queryFD), Events: unix.POLLIN}, {Fd: int32(brokerPIDFD), Events: unix.POLLIN}}
		for {
			n, e := unix.Poll(p, -1)
			if errors.Is(e, unix.EINTR) {
				continue
			}
			if e != nil {
				g2d1CaptureFailure("READER_POLL", e)
				return e
			}
			if n == 0 {
				continue
			}
			break
		}
		if p[1].Revents != 0 {
			if p[1].Revents&unix.POLLIN != 0 {
				g2d1CaptureFailure("READER_BROKER_PIDFD_POLLIN", errGuardProtocol)
			} else {
				g2d1CaptureFailure("READER_BROKER_PIDFD_EVENT", errGuardProtocol)
			}
			return errGuardProtocol
		}
		if p[0].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
			g2d1CaptureFailure(g2d1ReaderQueryEventStage(p[0].Revents), errGuardProtocol)
			return errGuardProtocol
		}
		if p[0].Revents&unix.POLLIN == 0 {
			g2d1CaptureFailure("READER_QUERY_NO_POLLIN", errGuardProtocol)
			return errGuardProtocol
		}
		if !pidfdSelects(brokerPIDFD, brokerPID) {
			g2d1CaptureFailure("READER_BROKER_PIDFD_SELECT", errGuardProtocol)
			return errGuardProtocol
		}
		currentBoot, bootErr := fixedBootID()
		if bootErr != nil {
			g2d1CaptureFailure("READER_BOOT_ID_READ", bootErr)
			return errGuardProtocol
		}
		if currentBoot != boot {
			g2d1CaptureFailure("READER_BOOT_ID_MISMATCH", errGuardProtocol)
			return errGuardProtocol
		}
		var rights []int
		raw, cred, rights, err = receiveControl(queryFD, -1)
		if err != nil {
			g2d1CaptureFailure("READER_RECEIVE", err)
			return err
		}
		req, e := parseCanonicalControl(raw)
		if e != nil || cred == nil || cred.Pid != int32(brokerPID) || cred.Uid != 0 || cred.Gid != 0 {
			for _, right := range rights {
				_ = unix.Close(right)
			}
			g2d1CaptureFailure("READER_FRAME_CREDENTIALS", e)
			return errGuardProtocol
		}
		if req["kind"] == "reader_selftest_arm" {
			if armedOnce {
				for _, right := range rights {
					_ = unix.Close(right)
				}
				g2d1CaptureFailure("READER_ARM_REPLAY", errGuardProtocol)
				return errGuardProtocol
			}
			armedVariant, err = readerSelftestArm(queryFD, witnessFD, uint32(observerPID), observerPIDFD, boot, req, rights)
			for _, right := range rights {
				_ = unix.Close(right)
			}
			if err != nil {
				g2d1CaptureFailure("READER_ARM", err)
				return err
			}
			armedOnce = true
			continue
		}
		if len(rights) != 0 {
			for _, right := range rights {
				_ = unix.Close(right)
			}
			g2d1CaptureFailure("READER_UNEXPECTED_RIGHTS", errGuardProtocol)
			return errGuardProtocol
		}
		if !exactKeys(req, "version", "boot_id", "request_id", "nonce", "check") || req["version"] != json.Number("1") || req["boot_id"] != boot {
			g2d1CaptureFailure("READER_REQUEST_FRAME", errGuardProtocol)
			return errGuardProtocol
		}
		id, valid := exactNumber(req["request_id"])
		n, ok := req["nonce"].(string)
		check, ck := req["check"].(string)
		if !valid || id == 0 || id == math.MaxUint64 || !ok || !validCheckpointNonce(n) || !ck || check != "status" && check != "configuration" {
			g2d1CaptureFailure("READER_REQUEST_FIELDS", errGuardProtocol)
			return errGuardProtocol
		}
		if id == nextID-1 && bytes.Equal(previous, raw) && len(response) > 0 {
			if err = readerWait(queryFD, unix.POLLOUT, brokerPIDFD, previousDeadline); err != nil {
				g2d1CaptureFailure("READER_REPLAY_WAIT", err)
				return err
			}
			if err = sendControl(queryFD, response, nil); err != nil {
				g2d1CaptureFailure("READER_REPLAY_SEND", err)
				return err
			}
			continue
		}
		if id != nextID {
			g2d1CaptureFailure("READER_REQUEST_SEQUENCE", errGuardProtocol)
			return errGuardProtocol
		}
		now, e := monoNowNS()
		if e != nil || now >= authorityGuestDeadlineNS {
			g2d1CaptureFailure("READER_REQUEST_DEADLINE", e)
			return errGuardProtocol
		}
		deadline := now + uint64((5 * time.Second).Nanoseconds())
		if deadline > authorityGuestDeadlineNS {
			deadline = authorityGuestDeadlineNS
		}
		if armedVariant == "reader-endpoint" || armedVariant == "reader-timeout" && check == "status" {
			variant := armedVariant
			var closed any
			if variant == "reader-endpoint" {
				if err = unix.Shutdown(queryFD, unix.SHUT_RDWR); err != nil {
					g2d1CaptureFailure("READER_ENDPOINT_SHUTDOWN", err)
					return err
				}
				closed = true
			}
			if err = readerWitnessEvent(witnessFD, boot, variant, id, n, check, raw, nil, nil, nil, 0, closed, nil); err != nil {
				g2d1CaptureFailure("READER_WITNESS_EVENT", err)
				return err
			}
			return g2d1CaptureReturned("READER_WITNESS_WAIT", readerWait(witnessFD, unix.POLLIN, brokerPIDFD, authorityGuestDeadlineNS), nil)
		}
		kinds := []uint16{auditGetType}
		if check == "configuration" {
			kinds = []uint16{auditGetType, auditListRulesType, auditGetType}
		}
		queries := make([]any, 0, len(kinds))
		for _, kind := range kinds {
			if sequence == math.MaxUint32 {
				g2d1CaptureFailure("READER_NETLINK_SEQUENCE", errGuardProtocol)
				return errGuardProtocol
			}
			sequence++
			q, e := readerQuery(fd, brokerPIDFD, local.Pid, sequence, kind, deadline)
			if e != nil {
				g2d1Fail("NETLINK_QUERY", e)
				return e
			}
			queries = append(queries, q)
		}
		if _, e = validateAuditQueries(queries, check); e != nil {
			g2d1Fail("NETLINK_QUERY_VALIDATE", e)
			return e
		}
		evidence := map[string]any{"reader_cookie": jsonU64(math.MaxUint64), "request_id": jsonU64(id), "nonce": n, "check": check, "queries": queries}
		full := map[string]any{"version": json.Number("1"), "boot_id": boot, "epoch": strings.Repeat("f", 32), "sequence": jsonU64(math.MaxUint64), "previous": strings.Repeat("f", 64), "kind": "audit_readback", "body": evidence}
		if _, e = canonicalControl(full); e != nil {
			g2d1CaptureFailure("READER_EVIDENCE_FRAME", e)
			return e
		}
		priorResponse := response
		response, e = canonicalControl(map[string]any{"version": json.Number("1"), "boot_id": boot, "request_id": jsonU64(id), "nonce": n, "check": check, "queries": queries})
		if e != nil {
			g2d1CaptureFailure("READER_RESPONSE_FRAME", e)
			return e
		}
		variant := armedVariant
		if variant == "reader-done" && check != "configuration" || variant == "reader-timeout" && check != "status" || variant == "reader-death" {
			variant = ""
		}
		original := response
		var sentRights []int
		switch variant {
		case "reader-replay":
			if len(priorResponse) == 0 {
				g2d1CaptureFailure("READER_VARIANT_REPLAY", errGuardProtocol)
				return errGuardProtocol
			}
			response = priorResponse
		case "reader-frame":
			response = append(append([]byte(nil), response...), ' ')
		case "reader-extra-fd":
			sentRights = []int{brokerExeFD}
		case "reader-done":
			modified, e := readerWithoutDone(queries)
			if e != nil {
				g2d1CaptureFailure("READER_VARIANT_DONE", e)
				return e
			}
			response, e = canonicalControl(map[string]any{"version": json.Number("1"), "boot_id": boot, "request_id": jsonU64(id), "nonce": n, "check": check, "queries": modified})
			if e != nil {
				g2d1CaptureFailure("READER_VARIANT_FRAME", e)
				return e
			}
		case "reader-credential":
			if err = readerWitnessEvent(witnessFD, boot, variant, id, n, check, raw, original, response, nil, 0, nil, queries); err != nil {
				g2d1CaptureFailure("READER_WITNESS_EVENT", err)
				return err
			}
			return g2d1CaptureReturned("READER_WITNESS_WAIT", readerWait(witnessFD, unix.POLLIN, brokerPIDFD, authorityGuestDeadlineNS), nil)
		}
		if err = readerWait(queryFD, unix.POLLOUT, brokerPIDFD, deadline); err != nil {
			g2d1CaptureFailure("READER_SEND_WAIT", err)
			return err
		}
		sendErr := sendControl(queryFD, response, sentRights)
		if variant != "" {
			var result any = jsonU64(0)
			if sendErr != nil {
				var errno unix.Errno
				if errors.As(sendErr, &errno) {
					result = jsonU64(uint64(errno))
				} else {
					result = sendErr.Error()
				}
			}
			if err = readerWitnessEvent(witnessFD, boot, variant, id, n, check, raw, original, response, result, len(sentRights), nil, queries); err != nil {
				g2d1CaptureFailure("READER_WITNESS_EVENT", err)
				return err
			}
			armedVariant = ""
		}
		if sendErr != nil {
			g2d1CaptureFailure("READER_SEND", sendErr)
			return sendErr
		}
		previous = append(previous[:0], raw...)
		previousDeadline = deadline
		nextID++
	}
}
