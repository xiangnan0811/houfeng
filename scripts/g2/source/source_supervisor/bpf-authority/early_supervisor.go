//go:build linux && amd64 && !cgo

package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

type earlyHelper struct {
	command *exec.Cmd
	pidfd   int
	reaped  bool
}

func startEarlyHelper(role string, files []*os.File) (*earlyHelper, error) {
	if role != "broker" && role != "audit-reader" && role != "terminal-observer" || os.Getpid() != 1 {
		return nil, errGuardProtocol
	}
	command := exec.Command("/g2-bpf-authority", role)
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin"}
	command.ExtraFiles = files
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	command.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: unix.SIGKILL}
	if err := command.Start(); err != nil {
		return nil, err
	}
	pidfd, err := unix.PidfdOpen(command.Process.Pid, 0)
	if err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, err
	}
	h := &earlyHelper{command: command, pidfd: pidfd}
	if err = earlyProbeSameExecutable(command.Process.Pid); err != nil {
		_ = h.terminate()
		return nil, err
	}
	return h, nil
}

func (h *earlyHelper) terminate() error {
	if h == nil || h.reaped {
		return nil
	}
	if err := unix.PidfdSendSignal(h.pidfd, unix.SIGKILL, nil, 0); err != nil && err != unix.ESRCH {
		return err
	}
	deadline, err := guestDeadline(5 * time.Second)
	if err != nil {
		return err
	}
	if err = earlyProbeWait(h.pidfd, unix.POLLIN, deadline); err != nil {
		return err
	}
	waitErr := h.command.Wait()
	h.reaped = true
	// An ExitError retains the actual signal status and proves direct wait.
	if waitErr != nil {
		if _, ok := waitErr.(*exec.ExitError); !ok {
			return waitErr
		}
	}
	return nil
}

func earlySocketPair() ([2]int, error) {
	pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, 0)
	if err != nil {
		return pair, err
	}
	for _, fd := range pair {
		if err = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_PASSCRED, 1); err != nil {
			_ = unix.Close(pair[0])
			_ = unix.Close(pair[1])
			return [2]int{-1, -1}, err
		}
	}
	return pair, nil
}

func earlyNonce() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	if guardZero(raw[:]) {
		return "", errGuardProtocol
	}
	return hex.EncodeToString(raw[:]), nil
}

var validEarlySealStages = map[string]bool{
	"seal-guard-loaded": true,
	"seal-inode-links":  true,
	"seal-bpf-link":     true,
	"seal-sealed":       true,
	"seal-audit-locked": true,
}

func parseEarlyCmdline() (string, string, error) {
	raw, err := os.ReadFile("/proc/cmdline")
	if err != nil || len(raw) == 0 || len(raw) > 4096 {
		g2d1EarlyCause("CMDLINE", err)
		return "", "", errGuardProtocol
	}
	fields := strings.Fields(strings.TrimSpace(string(raw)))
	var variant, caseSHA string
	seenVariant, seenCase := false, false
	for _, f := range fields {
		if strings.HasPrefix(f, "g2.bpf_test=") {
			if seenVariant {
				return "", "", errGuardProtocol
			}
			seenVariant = true
			variant = strings.TrimPrefix(f, "g2.bpf_test=")
			if !validEarlySealStages[variant] {
				return "", "", errGuardProtocol
			}
		} else if strings.HasPrefix(f, "g2.bpf_case=") {
			if seenCase {
				return "", "", errGuardProtocol
			}
			seenCase = true
			val, ok := digestString(strings.TrimPrefix(f, "g2.bpf_case="))
			if !ok {
				return "", "", errGuardProtocol
			}
			caseSHA = val
		} else if strings.HasPrefix(f, "g2.") {
			return "", "", errGuardProtocol
		}
	}
	if seenVariant != seenCase {
		return "", "", errGuardProtocol
	}
	return variant, caseSHA, nil
}

func pid1ObserverRPC(fd int, observer *earlyHelper, boot string, reqID uint64, op string, body map[string]any, deadline time.Time) (map[string]any, error) {
	if observer == nil || !livePIDFD(observer.pidfd) {
		return nil, fmt.Errorf("early terminal observer pidfd not live: %w", errGuardProtocol)
	}
	nonce, err := earlyNonce()
	if err != nil {
		return nil, err
	}
	req := map[string]any{
		"version":    json.Number("1"),
		"boot_id":    boot,
		"epoch":      nil,
		"request_id": jsonU64(reqID),
		"nonce":      nonce,
		"operation":  op,
		"body":       body,
	}
	if err = earlyProbeSend(fd, req, nil, deadline); err != nil {
		return nil, err
	}
	resp, cred, _, err := earlyProbeReceive(fd, 0, deadline)
	if err != nil {
		return nil, err
	}
	if cred == nil || cred.Pid != int32(observer.command.Process.Pid) || cred.Uid != 0 || cred.Gid != 0 || !livePIDFD(observer.pidfd) {
		return nil, fmt.Errorf("early terminal observer response identity differs: %w", errGuardProtocol)
	}
	if !exactKeys(resp, "version", "boot_id", "epoch", "request_id", "nonce", "status", "body") || resp["version"] != json.Number("1") || resp["boot_id"] != boot || resp["request_id"] != jsonU64(reqID) || resp["nonce"] != nonce || resp["status"] != "OK" {
		return nil, errGuardProtocol
	}
	respBody, ok := resp["body"].(map[string]any)
	if !ok {
		return nil, errGuardProtocol
	}
	return respBody, nil
}

func emitEarlySealGapResult(obsStartupFD int, boot string, variant string, caseSHA string, broker, observer *earlyHelper, pid1Cookie uint64, deadline time.Time) error {
	if pid1Cookie == 0 {
		return fmt.Errorf("early terminal PID1 pin evidence missing: %w", errGuardProtocol)
	}
	if err := unix.PidfdSendSignal(broker.pidfd, unix.SIGKILL, nil, 0); err != nil && err != unix.ESRCH {
		return err
	}
	if err := earlyProbeWait(broker.pidfd, unix.POLLIN, deadline); err != nil {
		return fmt.Errorf("early terminal broker pidfd exit not observed: %w", err)
	}
	if !time.Now().Before(deadline) {
		return fmt.Errorf("early terminal broker pidfd exit observed after deadline: %w", unix.ETIMEDOUT)
	}
	waitErr := broker.command.Wait()
	broker.reaped = true
	if waitErr != nil {
		if _, ok := waitErr.(*exec.ExitError); !ok {
			return waitErr
		}
	}
	if broker.command.ProcessState == nil {
		return fmt.Errorf("early terminal direct wait status missing: %w", errGuardProtocol)
	}
	status, ok := broker.command.ProcessState.Sys().(syscall.WaitStatus)
	if !ok {
		return fmt.Errorf("early terminal direct wait status invalid: %w", errGuardProtocol)
	}
	finBody, err := pid1ObserverRPC(obsStartupFD, observer, boot, 2, "FINISH", map[string]any{}, deadline)
	if err != nil {
		return err
	}
	end, endOK := exactNumber(finBody["end_sequence"])
	terminalSHA, shaOK := digestString(finBody["terminal_sha256"])
	authAfter, authOK := finBody["authority_after"].(map[string]any)
	authBefore, beforeOK := earlyObserverAuthBefore.(map[string]any)
	begin, beginOK := exactNumber(earlyObserverBeginSeq)
	if !exactKeys(finBody, "end_sequence", "terminal_sha256", "authority_after", "losses", "outcome", "fault_evidence") || finBody["fault_evidence"] != nil || !endOK || !shaOK || !authOK || !beforeOK || !beginOK || end < begin || finBody["outcome"] != "early_no_handoff" {
		return fmt.Errorf("early terminal FINISH evidence invalid: %w", errGuardProtocol)
	}
	brokerCookie, cookieOK := exactNumber(authAfter["broker_cookie"])
	beforeBroker, beforeBrokerOK := exactNumber(authBefore["broker_cookie"])
	observerCookie, observerOK := exactNumber(authAfter["observer_cookie"])
	beforeObserver, beforeObserverOK := exactNumber(authBefore["observer_cookie"])
	instance, instanceOK := exactNumber(authAfter["module_instance"])
	beforeInstance, beforeInstanceOK := exactNumber(authBefore["module_instance"])
	deathSeq, deathOK := exactNumber(authAfter["death_sequence"])
	if !cookieOK || brokerCookie == 0 || !beforeBrokerOK || beforeBroker != brokerCookie || !observerOK || observerCookie == 0 || !beforeObserverOK || beforeObserver != observerCookie || !instanceOK || instance == 0 || !beforeInstanceOK || beforeInstance != instance || !deathOK || deathSeq == 0 {
		return fmt.Errorf("early terminal native status identity or death sequence missing: %w", errGuardProtocol)
	}
	allRecords := make([]any, 0)
	var after uint64
	tail := strings.Repeat("0", 64)
	reqSeq := uint64(3)
	for after < end {
		count := end - after
		if count > 64 {
			count = 64
		}
		page, e := pid1ObserverRPC(obsStartupFD, observer, boot, reqSeq, "READ", map[string]any{
			"after": jsonU64(after), "max_records": jsonU64(count),
		}, deadline)
		if e != nil {
			return e
		}
		reqSeq++
		recs, recOK := page["records"].([]any)
		next, nextOK := exactNumber(page["next"])
		_, completeOK := page["complete"].(bool)
		if !exactKeys(page, "records", "next", "complete") || !recOK || !nextOK || !completeOK || len(recs) == 0 || uint64(len(recs)) > count || next != after+uint64(len(recs)) {
			return fmt.Errorf("early terminal READ cursor incomplete: %w", errGuardProtocol)
		}
		for _, item := range recs {
			record, valid := item.(map[string]any)
			if !valid || !exactKeys(record, "version", "boot_id", "epoch", "sequence", "previous", "kind", "body") || record["version"] != json.Number("1") || record["boot_id"] != boot || record["sequence"] != jsonU64(after+1) || record["previous"] != tail {
				return fmt.Errorf("early terminal record chain invalid: %w", errGuardProtocol)
			}
			tail, e = observerTranscriptHash(record)
			if e != nil {
				return e
			}
			allRecords = append(allRecords, record)
			after++
		}
	}
	if tail != terminalSHA {
		return fmt.Errorf("early terminal FINISH digest differs from record chain: %w", errGuardProtocol)
	}
	var exit map[string]any
	for _, item := range allRecords {
		rm := item.(map[string]any)
		if rm["kind"] != "guard" {
			continue
		}
		body, ok := rm["body"].(map[string]any)
		if !ok {
			return fmt.Errorf("early terminal guard body invalid: %w", errGuardProtocol)
		}
		gr, ok := body["record"].(map[string]any)
		if !ok {
			return fmt.Errorf("early terminal guard record invalid: %w", errGuardProtocol)
		}
		if gr["kind"] == json.Number("15") && gr["task_cookie"] == jsonU64(brokerCookie) {
			if exit != nil {
				return fmt.Errorf("early terminal broker exit duplicated: %w", errGuardProtocol)
			}
			exit = gr
		}
	}
	if exit == nil {
		return fmt.Errorf("early terminal native broker exit missing: %w", errGuardProtocol)
	}
	detail, detailOK := exit["detail"].(map[string]any)
	start, startOK := exactNumber(exit["task_start_ns"])
	exitCode, exitOK := exit["result"].(json.Number)
	detailCode, detailCodeOK := detail["exit_code"].(json.Number)
	pid := jsonU64(uint64(broker.command.Process.Pid))
	if !detailOK || !startOK || start == 0 || exit["sequence"] != jsonU64(deathSeq) || detail["tgid"] != pid || detail["tid"] != pid || !exitOK || !detailCodeOK || detailCode != exitCode || exitCode != jsonI64(int64(status)) {
		return fmt.Errorf("early terminal broker native exit differs from direct wait: %w", errGuardProtocol)
	}
	brokerExit := map[string]any{
		"task_cookie": jsonU64(brokerCookie), "tgid": pid, "tid": pid,
		"start_ns": jsonU64(start), "pidfd_exited": true,
		"guard_exit_sequence": exit["sequence"], "guard_exit_code": exit["result"],
		"direct_wait": map[string]any{"waiter_cookie": jsonU64(pid1Cookie), "wait_status": jsonI64(int64(status))},
	}
	termObs := map[string]any{
		"version":            json.Number("1"),
		"variant":            variant,
		"boot_id":            boot,
		"epoch":              authAfter["epoch"],
		"module_instance":    authAfter["module_instance"],
		"observer_principal": jsonU64(3),
		"authority_before":   earlyObserverAuthBefore,
		"authority_after":    authAfter,
		"begin_sequence":     earlyObserverBeginSeq,
		"end_sequence":       finBody["end_sequence"],
		"terminal_sha256":    finBody["terminal_sha256"],
		"broker_exit":        brokerExit,
		"probe_exits":        []any{},
		"records":            allRecords,
		"losses":             finBody["losses"],
		"outcome":            finBody["outcome"],
		"fault_evidence":     finBody["fault_evidence"],
	}
	payload := map[string]any{
		"version":              json.Number("1"),
		"run_token":            authorityRunToken,
		"case_sha256":          caseSHA,
		"seal_stage":           variant,
		"init_handoff":         false,
		"terminal_observation": termObs,
	}
	if err := writeEarlyResult(os.Stdout, payload, caseSHA, variant); err != nil {
		return err
	}
	// Wait in no-handoff state within deadline for host owned QMP quit
	for time.Until(deadline) > 0 {
		time.Sleep(100 * time.Millisecond)
	}
	return errGuardProtocol
}

// writeEarlyResult emits the LF-terminated aggregate as bounded control frames.
func writeEarlyResult(w io.Writer, payload map[string]any, caseSHA, variant string) error {
	const resultLimit = 64 * 1024 * 1024
	payloadRaw, err := canonicalWithLimit(payload, resultLimit-1)
	if err != nil {
		return err
	}
	payloadRaw = append(payloadRaw, '\n')
	payloadSHA := hashBytes(payloadRaw)
	const chunkSize = 8192
	chunks := (len(payloadRaw) + chunkSize - 1) / chunkSize
	if chunks == 0 || chunks > 8192 {
		return errControlFrame
	}
	beginFrame, err := canonicalControl(map[string]any{
		"version":     json.Number("1"),
		"run_token":   authorityRunToken,
		"case_sha256": caseSHA,
		"variant":     variant,
		"bytes":       jsonU64(uint64(len(payloadRaw))),
		"sha256":      payloadSHA,
		"chunks":      jsonU64(uint64(chunks)),
	})
	if err != nil {
		return err
	}
	beginFrame = append([]byte("G2_BPF_EARLY_BEGIN "), append(beginFrame, '\n')...)
	if n, err := w.Write(beginFrame); err != nil {
		return err
	} else if n != len(beginFrame) {
		return io.ErrShortWrite
	}
	for i := range chunks {
		startOff := i * chunkSize
		endOff := startOff + chunkSize
		if endOff > len(payloadRaw) {
			endOff = len(payloadRaw)
		}
		b64 := base64.StdEncoding.EncodeToString(payloadRaw[startOff:endOff])
		chunkFrame, e := canonicalControl(map[string]any{
			"sequence": jsonU64(uint64(i)),
			"data":     b64,
		})
		if e != nil {
			return e
		}
		chunkFrame = append([]byte("G2_BPF_EARLY_CHUNK "), append(chunkFrame, '\n')...)
		if n, e := w.Write(chunkFrame); e != nil {
			return e
		} else if n != len(chunkFrame) {
			return io.ErrShortWrite
		}
	}
	endFrame, err := canonicalControl(map[string]any{
		"version":     json.Number("1"),
		"run_token":   authorityRunToken,
		"case_sha256": caseSHA,
		"bytes":       jsonU64(uint64(len(payloadRaw))),
		"sha256":      payloadSHA,
		"chunks":      jsonU64(uint64(chunks)),
	})
	if err != nil {
		return err
	}
	endFrame = append([]byte("G2_BPF_EARLY_END "), append(endFrame, '\n')...)
	if n, err := w.Write(endFrame); err != nil {
		return err
	} else if n != len(endFrame) {
		return io.ErrShortWrite
	}
	return nil
}

var (
	earlyObserverAuthBefore any
	earlyObserverBeginSeq   any = jsonU64(0)
)

func runEarlyInit() (err error) {
	stage := "EARLY_KERNEL"
	defer func() {
		if err != nil && g2d1.role == 'E' {
			g2d1Fail(stage, err)
		}
	}()
	var audit *earlyAuditControl
	manifest, _, moduleMemFile, err := prepareEarlyKernel(&audit)
	if err != nil {
		return err
	}
	defer moduleMemFile.Close()
	defer audit.close()
	stage = "CMDLINE"
	earlyVariant, earlyCaseSHA, err := parseEarlyCmdline()
	if err != nil {
		return err
	}
	stage = "DEADLINE"
	deadline, err := guestDeadline(60 * time.Second)
	if err != nil {
		return err
	}
	stage = "BOOT_ID"
	boot, err := fixedBootID()
	if err != nil {
		return err
	}
	stage = "NONCE"
	nonce, err := earlyNonce()
	if err != nil {
		return err
	}
	stage = "BROKER_SOCKET"
	bootstrap, err := earlySocketPair()
	if err != nil {
		return err
	}
	parentSocket := os.NewFile(uintptr(bootstrap[0]), "early-bootstrap-parent")
	childSocket := os.NewFile(uintptr(bootstrap[1]), "early-bootstrap-child")
	defer parentSocket.Close()
	defer childSocket.Close()
	stage = "BROKER_START"
	broker, err := startEarlyHelper("broker", []*os.File{childSocket})
	if err != nil {
		return err
	}
	_ = childSocket.Close()
	defer unix.Close(broker.pidfd)
	var reader, observer *earlyHelper
	handedOff := false
	defer func() {
		if !handedOff {
			if reader != nil {
				if cleanup := reader.terminate(); err == nil {
					err = cleanup
				}
			}
			if observer != nil {
				if cleanup := observer.terminate(); err == nil {
					err = cleanup
				}
			}
			if cleanup := broker.terminate(); err == nil {
				err = cleanup
			}
		}
	}()
	stage = "BROKER_IDENTITY"
	brokerExeFD, err := unix.Open(fmt.Sprintf("/proc/%d/exe", broker.command.Process.Pid), unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	brokerExe := os.NewFile(uintptr(brokerExeFD), "held-broker-executable")
	defer brokerExe.Close()
	brokerHash, _, err := heldRegularDigest(brokerExeFD, 128<<20)
	if err != nil || brokerHash != manifest.BrokerSHA256 {
		g2d1EarlyCause(stage, err)
		return errGuardProtocol
	}
	stage = "READER_SOCKET"
	query, err := earlySocketPair()
	if err != nil {
		return err
	}
	queryParent := os.NewFile(uintptr(query[0]), "audit-reader-query")
	queryChild := os.NewFile(uintptr(query[1]), "audit-reader-child")
	defer queryParent.Close()
	defer queryChild.Close()
	stage = "WITNESS_SOCKET"
	witness, err := earlySocketPair()
	if err != nil {
		return err
	}
	witnessReader := os.NewFile(uintptr(witness[0]), "reader-witness")
	witnessObserver := os.NewFile(uintptr(witness[1]), "observer-witness")
	defer witnessReader.Close()
	defer witnessObserver.Close()
	stage = "READER_BROKER_PIDFD"
	brokerPIDFD, err := unix.FcntlInt(uintptr(broker.pidfd), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return err
	}
	brokerPIDFile := os.NewFile(uintptr(brokerPIDFD), "broker-pidfd-for-reader")
	defer brokerPIDFile.Close()
	stage = "READER_START"
	reader, err = startEarlyHelper("audit-reader", []*os.File{queryChild, brokerPIDFile, brokerExe, witnessReader})
	if err != nil {
		return err
	}
	_ = queryChild.Close()
	_ = witnessReader.Close()
	_ = brokerPIDFile.Close()
	defer unix.Close(reader.pidfd)
	stage = "READER_INIT_SEND"
	if err = earlyProbeSend(query[0], map[string]any{"version": json.Number("1"), "boot_id": boot, "kind": "reader_init", "nonce": nonce}, nil, deadline); err != nil {
		return err
	}
	stage = "READER_READY"
	frame, cred, _, err := earlyProbeReceive(query[0], 0, deadline)
	if err != nil || cred.Pid != int32(reader.command.Process.Pid) || cred.Uid != 0 || cred.Gid != 0 || !exactKeys(frame, "version", "boot_id", "kind", "nonce") || frame["version"] != json.Number("1") || frame["boot_id"] != boot || frame["kind"] != "reader_ready" || frame["nonce"] != nonce {
		g2d1EarlyCause(stage, err)
		return errGuardProtocol
	}
	stage = "READER_CONFINEMENT"
	if err = verifyAuditReaderConfinement(reader.command.Process.Pid); err != nil {
		return err
	}
	stage = "READER_READY_SEND"
	if err = earlyProbeSend(bootstrap[0], frame, nil, deadline); err != nil {
		return err
	}
	stage = "READER_IDENTITY"
	identity, cred, _, err := earlyProbeReceive(bootstrap[0], 0, deadline)
	if err != nil || cred.Pid != int32(broker.command.Process.Pid) || cred.Uid != 0 || cred.Gid != 0 || !exactKeys(identity, "version", "boot_id", "kind", "reader_pid", "reader_tid", "reader_start_ns", "reader_exe_sha256", "nonce") || identity["version"] != json.Number("1") || identity["boot_id"] != boot || identity["kind"] != "reader_identity" || identity["nonce"] != nonce || identity["reader_pid"] != json.Number(fmt.Sprint(reader.command.Process.Pid)) || identity["reader_tid"] != identity["reader_pid"] || identity["reader_exe_sha256"] != manifest.BrokerSHA256 {
		g2d1EarlyCause(stage, err)
		return errGuardProtocol
	}
	start, ok := exactNumber(identity["reader_start_ns"])
	if !ok || start == 0 {
		return errGuardProtocol
	}
	ticks, err := procStartTicks(reader.command.Process.Pid)
	if err != nil || start/10_000_000 != ticks || !livePIDFD(reader.pidfd) || !livePIDFD(broker.pidfd) {
		g2d1EarlyCause(stage, err)
		return errGuardProtocol
	}
	readerExeFD, err := unix.Open(fmt.Sprintf("/proc/%d/exe", reader.command.Process.Pid), unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(readerExeFD)
	readerHash, _, err := heldRegularDigest(readerExeFD, 128<<20)
	if err != nil || readerHash != manifest.BrokerSHA256 {
		g2d1EarlyCause(stage, err)
		return errGuardProtocol
	}
	stage = "READER_BIND_SEND"
	identity["kind"] = "audit_reader"
	if err = earlyProbeSend(bootstrap[0], identity, []int{query[0], reader.pidfd, readerExeFD}, deadline); err != nil {
		return err
	}
	_ = queryParent.Close()

	// Start terminal-observer with two distinct seqpacket pairs
	stage = "OBSERVER_STARTUP_SOCKET"
	obsStartup, err := earlySocketPair()
	if err != nil {
		return err
	}
	obsStartupParent := os.NewFile(uintptr(obsStartup[0]), "observer-startup-parent")
	obsStartupChild := os.NewFile(uintptr(obsStartup[1]), "observer-startup-child")
	defer obsStartupParent.Close()
	defer obsStartupChild.Close()

	stage = "OBSERVER_BROKER_SOCKET"
	obsBroker, err := earlySocketPair()
	if err != nil {
		return err
	}
	obsBrokerParent := os.NewFile(uintptr(obsBroker[0]), "observer-broker-parent")
	obsBrokerChild := os.NewFile(uintptr(obsBroker[1]), "observer-broker-child")
	defer obsBrokerParent.Close()
	defer obsBrokerChild.Close()

	stage = "OBSERVER_INPUT_FDS"
	brokerPIDFD2, err := unix.FcntlInt(uintptr(broker.pidfd), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return err
	}
	brokerPIDFile2 := os.NewFile(uintptr(brokerPIDFD2), "broker-pidfd-for-observer")
	defer brokerPIDFile2.Close()
	readerPIDCopy, err := unix.FcntlInt(uintptr(reader.pidfd), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return err
	}
	readerPIDFile := os.NewFile(uintptr(readerPIDCopy), "reader-pidfd-for-observer")
	defer readerPIDFile.Close()
	readerExeCopy, err := unix.FcntlInt(uintptr(readerExeFD), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return err
	}
	readerExeFile := os.NewFile(uintptr(readerExeCopy), "reader-executable-for-observer")
	defer readerExeFile.Close()
	stage = "OBSERVER_START"
	observer, err = startEarlyHelper("terminal-observer", []*os.File{obsStartupChild, obsBrokerChild, brokerPIDFile2, brokerExe, moduleMemFile, readerPIDFile, readerExeFile, witnessObserver})
	if err != nil {
		return err
	}
	_ = obsStartupChild.Close()
	_ = obsBrokerChild.Close()
	_ = brokerPIDFile2.Close()
	_ = readerPIDFile.Close()
	_ = readerExeFile.Close()
	defer unix.Close(observer.pidfd)

	stage = "OBSERVER_NONCE"
	obsNonce, err := earlyNonce()
	if err != nil {
		return err
	}
	stage = "OBSERVER_INIT_SEND"
	if err = earlyProbeSend(obsStartup[0], map[string]any{
		"version":                  json.Number("1"),
		"boot_id":                  boot,
		"kind":                     "observer_init",
		"nonce":                    obsNonce,
		"module_sha256":            manifest.GuardSHA256,
		"observer_bindings_sha256": manifest.ObserverBindingsSHA256,
	}, nil, deadline); err != nil {
		return err
	}
	stage = "OBSERVER_READY"
	obsFrame, obsCred, _, err := earlyProbeReceive(obsStartup[0], 0, deadline)
	if err != nil || obsCred.Pid != int32(observer.command.Process.Pid) || obsCred.Uid != 0 || obsCred.Gid != 0 || !exactKeys(obsFrame, "version", "boot_id", "kind", "nonce") || obsFrame["version"] != json.Number("1") || obsFrame["boot_id"] != boot || obsFrame["kind"] != "observer_ready" || obsFrame["nonce"] != obsNonce {
		g2d1EarlyCause(stage, err)
		return errGuardProtocol
	}
	stage = "OBSERVER_CONFINEMENT"
	if err = verifyTerminalObserverConfinement(observer.command.Process.Pid); err != nil {
		return err
	}
	stage = "OBSERVER_EXECUTABLE"
	observerPID := uint32(observer.command.Process.Pid)
	if !pidfdSelects(observer.pidfd, observerPID) {
		return errGuardProtocol
	}
	obsAcquisitionTicks, err := procStartTicks(observer.command.Process.Pid)
	if err != nil || obsAcquisitionTicks == 0 {
		g2d1EarlyCause(stage, err)
		return errGuardProtocol
	}
	obsExeFD, err := unix.Open(fmt.Sprintf("/proc/%d/exe", observer.command.Process.Pid), unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(obsExeFD)
	obsHash, _, err := heldRegularDigest(obsExeFD, 128<<20)
	if err != nil || obsHash != manifest.BrokerSHA256 {
		g2d1EarlyCause(stage, err)
		return errGuardProtocol
	}
	obsAfterTicks, err := procStartTicks(observer.command.Process.Pid)
	if err != nil || obsAfterTicks != obsAcquisitionTicks || !pidfdSelects(observer.pidfd, observerPID) {
		g2d1EarlyCause(stage, err)
		return errGuardProtocol
	}
	if err = verifyTerminalObserverConfinement(observer.command.Process.Pid); err != nil {
		return err
	}
	if !pidfdSelects(observer.pidfd, observerPID) {
		return errGuardProtocol
	}
	// PID1 authenticates the observer to the reader after its confinement is
	// established; SO_PEERCRED alone identifies only the socket creator.
	stage = "OBSERVER_WITNESS_SEND"
	if err = earlyProbeSend(witness[1], map[string]any{"version": json.Number("1"), "boot_id": boot, "kind": "reader_witness_peer", "pid": jsonU64(uint64(observer.command.Process.Pid)), "nonce": nonce}, []int{observer.pidfd, obsExeFD}, deadline); err != nil {
		return err
	}
	_ = witnessObserver.Close()
	stage = "OBSERVER_READY_SEND"
	if err = earlyProbeSend(bootstrap[0], obsFrame, nil, deadline); err != nil {
		return err
	}
	stage = "OBSERVER_IDENTITY"
	obsIdentity, cred, _, err := earlyProbeReceive(bootstrap[0], 0, deadline)
	if err != nil || cred.Pid != int32(broker.command.Process.Pid) || cred.Uid != 0 || cred.Gid != 0 || !exactKeys(obsIdentity, "version", "boot_id", "kind", "observer_pid", "observer_tid", "observer_start_ns", "observer_exe_sha256", "nonce") || obsIdentity["version"] != json.Number("1") || obsIdentity["boot_id"] != boot || obsIdentity["kind"] != "observer_identity" || obsIdentity["nonce"] != obsNonce || obsIdentity["observer_pid"] != json.Number(fmt.Sprint(observer.command.Process.Pid)) || obsIdentity["observer_tid"] != obsIdentity["observer_pid"] || obsIdentity["observer_exe_sha256"] != manifest.BrokerSHA256 {
		g2d1EarlyCause(stage, err)
		return errGuardProtocol
	}
	obsStart, ok := exactNumber(obsIdentity["observer_start_ns"])
	if !ok || obsStart == 0 {
		return errGuardProtocol
	}
	obsTicks, err := procStartTicks(observer.command.Process.Pid)
	if err != nil || obsTicks != obsAcquisitionTicks || obsStart/10_000_000 != obsTicks || !pidfdSelects(observer.pidfd, observerPID) || !livePIDFD(broker.pidfd) {
		g2d1EarlyCause(stage, err)
		return errGuardProtocol
	}
	obsHash, _, err = heldRegularDigest(obsExeFD, 128<<20)
	if err != nil || obsHash != manifest.BrokerSHA256 {
		g2d1EarlyCause(stage, err)
		return errGuardProtocol
	}
	stage = "OBSERVER_BIND_SEND"
	obsIdentity["kind"] = "terminal_observer"
	if err = earlyProbeSend(bootstrap[0], obsIdentity, []int{obsBroker[0], observer.pidfd, obsExeFD}, deadline); err != nil {
		return err
	}
	_ = obsBrokerParent.Close()

	stage = "OBSERVER_ARM"
	if earlyVariant != "" {
		armResp, e := pid1ObserverRPC(obsStartup[0], observer, boot, 1, "ARM", map[string]any{
			"variant":     earlyVariant,
			"case_sha256": earlyCaseSHA,
		}, deadline)
		if e != nil {
			return e
		}
		earlyObserverAuthBefore = armResp["authority_before"]
		earlyObserverBeginSeq = armResp["begin_sequence"]
	}
	stage = "SEAL_CONFIG_SEND"
	if err = earlyProbeSend(bootstrap[0], map[string]any{
		"version":    json.Number("1"),
		"boot_id":    boot,
		"kind":       "seal_stage_config",
		"seal_stage": earlyVariant,
		"nonce":      nonce,
	}, nil, deadline); err != nil {
		return err
	}
	stage = "SEAL_EXCHANGE"
	var pid1Cookie uint64
	for {
		msg, cred, _, e := earlyProbeReceive(bootstrap[0], 0, deadline)
		if e != nil || cred == nil || cred.Pid != int32(broker.command.Process.Pid) || cred.Uid != 0 || cred.Gid != 0 || msg["version"] != json.Number("1") || msg["boot_id"] != boot {
			g2d1EarlyCause(stage, e)
			return errGuardProtocol
		}
		cookie, ok := exactNumber(msg["pid1_cookie"])
		if !ok || cookie == 0 || msg["nonce"] != nonce {
			return fmt.Errorf("early startup PID1 pin identity missing: %w", errGuardProtocol)
		}
		if msg["kind"] == "seal_stage_reached" {
			if !exactKeys(msg, "version", "boot_id", "kind", "stage", "nonce", "pid1_cookie") || earlyVariant == "" || msg["stage"] != earlyVariant {
				return errGuardProtocol
			}
			return emitEarlySealGapResult(obsStartup[0], boot, earlyVariant, earlyCaseSHA, broker, observer, cookie, deadline)
		}
		if msg["kind"] == "policy_sealed" && exactKeys(msg, "version", "boot_id", "kind", "nonce", "pid1_cookie") {
			pid1Cookie = cookie
			break
		}
		return errGuardProtocol
	}
	stage = "AUDIT_FINALIZE"
	if err = audit.finalize(); err != nil {
		return err
	}
	if earlyVariant == "seal-audit-locked" {
		return emitEarlySealGapResult(obsStartup[0], boot, earlyVariant, earlyCaseSHA, broker, observer, pid1Cookie, deadline)
	}
	stage = "AUDIT_HANDOFF_SEND"
	if err = earlyProbeSend(bootstrap[0], map[string]any{"version": json.Number("1"), "boot_id": boot, "kind": "audit_finalized", "nonce": nonce}, nil, deadline); err != nil {
		return err
	}
	stage = "AUDIT_HANDOFF_ACK"
	ack, cred, _, err := earlyProbeReceive(bootstrap[0], 0, deadline)
	if err != nil || cred.Pid != int32(broker.command.Process.Pid) || cred.Uid != 0 || cred.Gid != 0 || !exactKeys(ack, "version", "boot_id", "kind", "nonce", "readback_sequence") || ack["version"] != json.Number("1") || ack["boot_id"] != boot || ack["kind"] != "audit_handoff_ready" || ack["nonce"] != nonce {
		g2d1EarlyCause(stage, err)
		return errGuardProtocol
	}
	sequence, ok := exactNumber(ack["readback_sequence"])
	if !ok || sequence == 0 || !livePIDFD(reader.pidfd) || !livePIDFD(observer.pidfd) || !livePIDFD(broker.pidfd) {
		return errGuardProtocol
	}
	stage = "HANDOFF_CONFINEMENT"
	if err = verifyAuditReaderConfinement(reader.command.Process.Pid); err != nil {
		return err
	}
	if err = verifyTerminalObserverConfinement(observer.command.Process.Pid); err != nil {
		return err
	}
	audit.close()
	_ = obsStartupParent.Close()
	_ = parentSocket.Close()
	_ = brokerExe.Close()
	_ = moduleMemFile.Close()
	handedOff = true
	stage = "INIT_EXEC"
	err = syscall.Exec("/init", []string{"/init"}, os.Environ())
	handedOff = false
	return err
}
