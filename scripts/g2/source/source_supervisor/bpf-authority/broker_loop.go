//go:build linux && amd64 && !cgo

package main

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"golang.org/x/sys/unix"
)

// brokerLoopHandlers are the original-thread portions of notification and
// lifecycle handling. They must retain uncertain committed effects and must
// never treat disappearance of a pidfd as an observed successful exit.
type brokerLoopHandlers struct {
	notification func(*runtimeInvocation, seccompNotification) error
	deaths       func() error
}

type brokerControlConnection struct {
	pending    bool
	out        []byte
	settlement ownerProbeSettlementReceipt
}

func openBrokerControlListener(boot string) (int, error) {
	actual, err := fixedBootID()
	if err != nil || boot != actual {
		return -1, errGuardProtocol
	}
	fd, err := unix.Socket(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, 0)
	if err != nil {
		return -1, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = unix.Close(fd)
		}
	}()
	if err = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_PASSCRED, 1); err != nil {
		return -1, err
	}
	if err = unix.Bind(fd, &unix.SockaddrUnix{Name: "\x00g2-bpf-control-" + boot}); err != nil {
		return -1, err
	}
	if err = unix.Listen(fd, 128); err != nil {
		return -1, err
	}
	keep = true
	return fd, nil
}

func (b *authorityBroker) rejectReaderEvent() error {
	if e := b.reader.healthy(); e != nil {
		return g2d1CaptureReaderHealth(g2d1ReaderHealthStage(b.reader, true), b.failReaderObservation(e), e, &b.reader.healthSelector)
	}
	g2d1CaptureFailure("BROKER_READER_EVENT", errGuardProtocol)
	return errGuardProtocol
}

// serveBrokerPoll runs only after the authenticated bootstrap has established
// the sealed authority, reader, audit stream, transcript, and listener. Socket
// clients can issue successive requests on the same credentialled connection.
func (b *authorityBroker) serveBrokerPoll(listener int, handlers brokerLoopHandlers) error {
	if b == nil || b.authority == nil || b.checkpoint == nil || b.checkpoint.lifecycle == nil || b.transcript == nil || b.join == nil || b.audit == nil || b.reader == nil || listener < 0 || handlers.notification == nil || handlers.deaths == nil || unix.Gettid() != os.Getpid() {
		g2d1CaptureFailure("BROKER_SERVE_INPUT", errGuardProtocol)
		return errGuardProtocol
	}
	connections := make(map[int]*brokerControlConnection)
	defer func() {
		if b.pendingSettlement != nil {
			b.pendingSettlement.followup(b, 4)
		}
		for fd := range connections {
			_ = unix.Close(fd)
		}
		if b.pendingObservation != nil && b.pendingObservation.credentialFD >= 0 {
			_ = unix.Close(b.pendingObservation.credentialFD)
			b.pendingObservation.credentialFD = -1
		}
		if b.pendingContainerProbe != nil {
			_ = unix.Close(b.pendingContainerProbe.pidfd)
			_ = unix.Close(b.pendingContainerProbe.eventFD)
			b.pendingContainerProbe = nil
		}
	}()
	pendingFD := -1
	var abandonedCheckpoint *pendingCheckpointRequest
	for {
		now, err := monoNowNS()
		if err != nil || now >= authorityGuestDeadlineNS {
			g2d1CaptureFailure("BROKER_DEADLINE", err)
			return errGuardProtocol
		}
		if b.selftestFD != nil && now >= b.selftestFD.deadline {
			b.checkpoint.markFault()
			g2d1CaptureFailure("BROKER_SELFTEST_DEADLINE", errGuardProtocol)
			return errGuardProtocol
		}
		if b.pendingObservation == nil || b.pendingObservation.armed {
			if err = b.reader.healthy(); err != nil {
				if errors.Is(err, unix.EINTR) {
					continue
				}
				return g2d1CaptureReaderHealth(g2d1ReaderHealthStage(b.reader, false), b.failReaderObservation(err), err, &b.reader.healthSelector)
			}
		} else if !pidfdSelects(b.reader.pidfd, b.reader.pid) {
			g2d1CaptureFailure("BROKER_READER_PIDFD_SELECT", errGuardProtocol)
			return errGuardProtocol
		}
		if err = drainGuardEvents(b.authority.guard, b.transcript, b.join, &b.checkpoint.cursor, b.checkpoint.lifecycle); err != nil {
			if b.join.fault != nil && b.join.fault.detector != "" {
				return g2d1CaptureReturned("BROKER_GUARD_DRAIN", b.reportDetectedObservation(), err)
			}
			b.checkpoint.markFault()
			g2d1CaptureFailure("BROKER_GUARD_DRAIN", err)
			return err
		}
		if err = drainAuditMessages(b.audit, b.join); err != nil {
			if b.join.fault != nil && b.join.fault.detector != "" {
				return g2d1CaptureReturned("BROKER_AUDIT_DRAIN", b.reportDetectedObservation(), err)
			}
			b.checkpoint.markFault()
			g2d1CaptureFailure("BROKER_AUDIT_DRAIN", err)
			return err
		}
		if err = b.join.drain(); err != nil {
			b.checkpoint.markFault()
			g2d1CaptureFailure("BROKER_AUDIT_JOIN", err)
			return err
		}
		if err = b.join.detectOmittedAudit(); err != nil {
			if b.join.fault != nil && b.join.fault.detector != "" {
				return g2d1CaptureReturned("BROKER_AUDIT_OMISSION", b.reportDetectedObservation(), err)
			}
			g2d1CaptureFailure("BROKER_AUDIT_OMISSION", err)
			return err
		}
		if err = b.advanceSelftestBaseline(); err != nil {
			b.checkpoint.markFault()
			g2d1CaptureFailure("BROKER_SELFTEST_BASELINE", err)
			return err
		}
		if b.pendingObservation != nil && b.pendingObservation.armed && b.pendingObservation.ackDelivered {
			if err = b.advanceReaderObservationAction(); err != nil {
				g2d1CaptureFailure("BROKER_READER_OBSERVATION", err)
				return err
			}
		}
		if abandonedCheckpoint != nil {
			done, e := b.drainAbandonedCheckpointReader(abandonedCheckpoint)
			if e != nil {
				b.checkpoint.markFault()
				g2d1CaptureFailure("BROKER_ABANDONED_READER", e)
				return e
			}
			if done {
				abandonedCheckpoint = nil
			}
		}
		if err = handlers.deaths(); err != nil {
			b.checkpoint.markFault()
			g2d1CaptureFailure("BROKER_SERVICE_DEATHS", err)
			return err
		}
		if b.effects != nil && b.effects.pending != nil && b.effects.pending.state == "response_settled" {
			if _, err = b.retireBrokerCreation(b.effects.pending); err != nil {
				g2d1CaptureFailure("BROKER_RETIRE_CREATION", err)
				return err
			}
		}
		deadAdmit := b.pendingAdmit != nil && b.pendingAdmit.principal.Terminal
		deadSettlement := b.pendingSettlement != nil && b.pendingSettlement.principal.Terminal
		deadCheckpoint := b.pendingCheckpoint != nil && b.pendingCheckpoint.principal.Terminal
		deadExecOp := b.pendingExecOp != nil && b.pendingExecOp.principal.Terminal
		deadContainerProbe := b.pendingContainerProbe != nil && b.pendingContainerProbe.principal.Terminal
		if deadAdmit || deadSettlement || deadCheckpoint || deadExecOp || deadContainerProbe {
			if pendingFD >= 0 {
				_ = unix.Close(pendingFD)
				delete(connections, pendingFD)
				pendingFD = -2
			}
			if deadCheckpoint {
				// serviceOwnerDeaths already recorded the failed checkpoint using
				// the accepted nonce and observed watermarks; never send an ACK.
				if b.pendingCheckpoint.readbackStarted && !b.pendingCheckpoint.readbackDone {
					abandonedCheckpoint = b.pendingCheckpoint
				}
				b.pendingCheckpoint = nil
				pendingFD = -1
			}
			if deadSettlement {
				b.pendingSettlement.followup(b, 3)
				b.pendingSettlement = nil
				pendingFD = -1
			}
			if deadAdmit {
				select {
				case result := <-b.pendingAdmit.result:
					if result.probe != nil {
						result.probe.close()
					}
					if result.secretRec != nil {
						result.secretRec.close()
					} else if result.initWait != nil {
						result.initWait.close()
					}
					_ = unix.Close(b.pendingAdmit.eventFD)
					b.pendingAdmit = nil
					pendingFD = -1
				default:
				}
			}
			if deadExecOp {
				if b.pendingExecOp.op == "END_ENDPOINT_TRANSITION" {
					b.closeEndpointTransitionGate()
				}
				_ = unix.Close(b.pendingExecOp.eventFD)
				b.pendingExecOp = nil
				pendingFD = -1
			}
			if deadContainerProbe {
				_ = unix.Close(b.pendingContainerProbe.pidfd)
				_ = unix.Close(b.pendingContainerProbe.eventFD)
				b.pendingContainerProbe = nil
				pendingFD = -1
			}
		}
		if pendingFD == -2 && b.pendingContainerProbe != nil {
			_ = unix.Close(b.pendingContainerProbe.pidfd)
			_ = unix.Close(b.pendingContainerProbe.eventFD)
			b.pendingContainerProbe = nil
			pendingFD = -1
		}
		if pendingFD != -1 && !deadAdmit && !deadSettlement && !deadCheckpoint && !deadExecOp && !deadContainerProbe {
			var connection *brokerControlConnection
			if pendingFD >= 0 {
				connection = connections[pendingFD]
				if connection == nil || !connection.pending {
					return errGuardProtocol
				}
			}
			var ack []byte
			var done bool
			var settlement *pendingBrokerSettlement
			start := b.pendingStart
			if b.pendingStart != nil {
				ack, done, err = b.finishPendingStart(pendingFD == -2, now)
			} else if b.pendingCheckpoint != nil {
				_, ack, done, err = b.advanceCheckpointRequest()
			} else if b.pendingAdmit != nil {
				_, ack, done, err = b.finishAdmitContainer()
			} else if b.pendingSettlement != nil {
				settlement = b.pendingSettlement
				_, ack, done, err = b.advanceBrokerSettlement()
			} else if b.pendingExecOp != nil {
				_, ack, done, err = b.finishPendingExecOp()
			} else if b.pendingContainerProbe != nil {
				ack, done, err = b.finishContainerSelftestProbe()
			} else if b.pendingObservation != nil && !b.pendingObservation.armed {
				ack, done, err = b.advanceReaderObservationArm()
			} else {
				return errGuardProtocol
			}
			if err != nil {
				if b.join.fault != nil && b.join.fault.detector != "" {
					return g2d1CaptureReturned("BROKER_PENDING_OPERATION", b.reportDetectedObservation(), err)
				}
				if b.reader.detection != nil {
					return g2d1CaptureReturned("BROKER_PENDING_READER", b.failReaderObservation(err), err)
				}
				b.checkpoint.markFault()
				g2d1CaptureFailure("BROKER_PENDING_OPERATION", err)
				return err
			}
			if done {
				if len(ack) == 0 && connection != nil {
					return errGuardProtocol
				}
				if start != nil {
					for fd := range start.waiters {
						if waiter := connections[fd]; waiter != nil {
							waiter.settlement = ownerProbeSettlementReceipt{}
							waiter.pending, waiter.out = false, ack
						}
					}
				} else if connection != nil {
					connection.pending, connection.out = false, ack
					connection.settlement = ownerProbeSettlementReceipt{}
					if settlement != nil && settlement.diagnosticEvents&(1<<3) != 0 {
						connection.settlement = settlement.diagnostic
					}
				}
				pendingFD = -1
			}
		}
		poll := []unix.PollFd{{Fd: int32(listener), Events: unix.POLLIN}, {Fd: int32(b.authority.guard.fd), Events: unix.POLLIN}, {Fd: int32(b.audit.fd), Events: unix.POLLIN}, {Fd: int32(b.reader.fd), Events: unix.POLLIN}, {Fd: int32(b.reader.pidfd), Events: unix.POLLIN}}
		if sfd := b.selftestBaselinePollFD(); sfd >= 0 {
			poll = append(poll, unix.PollFd{Fd: int32(sfd), Events: unix.POLLIN})
		}
		for fd, connection := range connections {
			events := int16(0)
			if len(connection.out) != 0 {
				events = unix.POLLOUT
			} else if !connection.pending {
				events = unix.POLLIN
			}
			poll = append(poll, unix.PollFd{Fd: int32(fd), Events: events})
		}
		for fd, inv := range b.runtimeInvocations {
			if inv == nil || fd < 0 {
				continue
			}
			events := int16(0)
			if inv.state == "settled" || inv.state == "failed" {
				events = unix.POLLIN
			} else if b.effects != nil && b.effects.pending == nil && !b.checkpoint.invalid && b.pendingObservation == nil && b.pendingCheckpoint == nil && b.checkpoint.active == nil && (b.selftestFD == nil || b.selftestFD.grantID == inv.grantID && b.join.settled() && !b.fixedProbeDrainPending(inv.grantID)) && !(b.selftestBaseline != nil && b.selftestBaseline.name == "attachment-replace" && b.selftestBaseline.grantID == inv.grantID && b.selftestBaseline.extraAttachmentID != 0 && b.selftestBaseline.replacementTransactionID == 0) {
				events = unix.POLLIN
			}
			poll = append(poll, unix.PollFd{Fd: int32(fd), Events: events})
		}
		for fd, actor := range b.probeListeners {
			if actor == nil || fd < 0 {
				continue
			}
			events := int16(0)
			if !b.checkpoint.invalid && b.pendingObservation == nil && b.pendingCheckpoint == nil && b.checkpoint.active == nil && (actor.name != "extra-fd-delays-removal" || b.effects != nil && b.effects.pending == nil) {
				events = unix.POLLIN
			}
			poll = append(poll, unix.PollFd{Fd: int32(fd), Events: events})
		}
		if b.pendingAdmit != nil {
			poll = append(poll, unix.PollFd{Fd: int32(b.pendingAdmit.eventFD), Events: unix.POLLIN})
		}
		if b.pendingContainerProbe != nil {
			poll = append(poll, unix.PollFd{Fd: int32(b.pendingContainerProbe.eventFD), Events: unix.POLLIN})
		}
		if b.pendingExecOp != nil {
			poll = append(poll, unix.PollFd{Fd: int32(b.pendingExecOp.eventFD), Events: unix.POLLIN})
		}
		if _, err = unix.Poll(poll, 100); err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			g2d1CaptureFailure("BROKER_POLL", err)
			return err
		}
		if poll[4].Revents != 0 || poll[3].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
			return b.rejectReaderEvent()
		}
		if poll[2].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 || poll[1].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
			b.checkpoint.markFault()
			g2d1CaptureFailure("BROKER_GUARD_AUDIT_EVENT", errGuardProtocol)
			return errGuardProtocol
		}
		if poll[0].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
			g2d1CaptureFailure("BROKER_LISTENER_EVENT", errGuardProtocol)
			return errGuardProtocol
		}
		if poll[0].Revents&unix.POLLIN != 0 {
			fd, _, e := unix.Accept4(listener, unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK)
			if e == nil {
				if len(connections) >= 128 || unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_PASSCRED, 1) != nil {
					_ = unix.Close(fd)
				} else {
					connections[fd] = &brokerControlConnection{}
				}
			} else if !errors.Is(e, unix.EAGAIN) && !errors.Is(e, unix.EINTR) {
				g2d1CaptureFailure("BROKER_ACCEPT", e)
				return e
			}
		}
		for _, item := range poll[5:] {
			fd := int(item.Fd)
			if connection := connections[fd]; connection != nil {
				if item.Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
					if b.pendingObservation != nil && b.pendingObservation.controlFD == fd && !b.pendingObservation.ackDelivered {
						return errGuardProtocol
					}
					if item.Revents&unix.POLLHUP != 0 && connection.pending && pendingFD == fd && b.pendingSettlement != nil {
						b.pendingSettlement.followup(b, 1)
					}
					if connection.pending && b.pendingStart != nil && b.pendingStart.waiters[fd] {
						delete(b.pendingStart.waiters, fd)
						if pendingFD == fd {
							pendingFD = -2
							for next := range b.pendingStart.waiters {
								pendingFD, b.pendingStart.fd = next, next
								break
							}
						}
					} else if connection.pending {
						pendingFD = -2
					}
					_ = unix.Close(fd)
					delete(connections, fd)
					continue
				}
				if item.Revents&unix.POLLOUT != 0 && len(connection.out) != 0 {
					if e := sendControl(fd, connection.out, nil); e != nil {
						if errors.Is(e, unix.EAGAIN) {
							continue
						}
						_ = unix.Close(fd)
						delete(connections, fd)
						continue
					}
					if b.pendingObservation != nil && b.pendingObservation.armed && b.pendingObservation.controlFD == fd {
						b.pendingObservation.ackDelivered = true
					}
					connection.settlement.send(4, 0, ownerProbeRemovalDiagnostic{})
					connection.settlement = ownerProbeSettlementReceipt{}
					connection.out = nil
				}
				if item.Revents&unix.POLLIN != 0 && !connection.pending && len(connection.out) == 0 {
					wasInvalid := b.checkpoint.invalid
					var controlDiagnostic brokerControlDiagnostic
					pending, e := b.dispatchBrokerControl(fd, &controlDiagnostic)
					if g2d1.registerFD == fd && g2d1.registered {
						if e != nil {
							g2d1Fail("ACK_SEND", e)
						} else if !pending {
							g2d1Mark("ACK_SEND_OK")
							g2d1.closedAfterACK = g2d1.owned
							g2d1Close()
						}
					}
					if b.pendingRevokeAndClose && e == nil {
						b.pendingRevokeAndClose = false
						return g2d1CaptureReturned("BROKER_REVOKE_CLOSE", b.executeRevokeAndCloseSequence(), nil)
					}
					if e != nil {
						if b.join.fault != nil && b.join.fault.detector != "" {
							return g2d1CaptureBrokerControl(&controlDiagnostic, brokerControlRouteJoinReport, b.reportDetectedObservation(), e)
						}
						faulted := b.checkpoint.faulted
						if faulted || !wasInvalid && b.checkpoint.invalid {
							route := brokerControlRouteNewInvalid
							if faulted {
								route = brokerControlRouteFaulted
							}
							return g2d1CaptureBrokerControl(&controlDiagnostic, route, e, e)
						}
						_ = unix.Close(fd)
						delete(connections, fd)
						continue
					}
					if pending {
						if pendingFD != -1 {
							if b.pendingStart == nil || !b.pendingStart.waiters[fd] {
								return errGuardProtocol
							}
							connection.pending = true
						} else {
							connection.pending, pendingFD = true, fd
						}
					}
				}
				continue
			}
			if actor := b.probeListeners[fd]; actor != nil && item.Revents&unix.POLLIN != 0 {
				n, e := receiveNotification(fd)
				if e != nil {
					if errors.Is(e, unix.EAGAIN) || errors.Is(e, unix.ENOENT) || errors.Is(e, unix.EINTR) {
						continue
					}
					g2d1CaptureFailure("BROKER_PROBE_RECEIVE", e)
					return e
				}
				if e = b.dispatchProbeNotification(actor, n); e != nil {
					if b.join.fault != nil && b.join.fault.detector != "" {
						return g2d1CaptureReturned("BROKER_PROBE_DISPATCH", b.reportDetectedObservation(), e)
					}
					b.checkpoint.markFault()
					g2d1CaptureFailure("BROKER_PROBE_DISPATCH", e)
					return e
				}
				continue
			}
			if inv := b.runtimeInvocations[fd]; inv != nil && item.Revents&unix.POLLIN != 0 {
				n, e := receiveNotification(fd)
				if e != nil {
					if errors.Is(e, unix.EAGAIN) || errors.Is(e, unix.ENOENT) || errors.Is(e, unix.EINTR) {
						continue
					}
					g2d1CaptureFailure("BROKER_RUNTIME_RECEIVE", e)
					return e
				}
				if inv.state == "settled" || inv.state == "failed" {
					e = b.denyRuntimeNotification(inv, n)
				} else {
					e = handlers.notification(inv, n)
				}
				if e != nil {
					if b.join.fault != nil && b.join.fault.detector != "" {
						return g2d1CaptureReturned("BROKER_RUNTIME_DISPATCH", b.reportDetectedObservation(), e)
					}
					b.checkpoint.markFault()
					g2d1CaptureFailure("BROKER_RUNTIME_DISPATCH", e)
					return e
				}
			}
		}
	}
}

// initializePID1Registry binds the live PID1 task to the systemd executable
// extracted and hashed from the original image. It is called only after the
// initramfs handoff; the earlier PID1 executable is not a systemd substitute.
func (b *authorityBroker) initializePID1Registry() (err error) {
	stage := "REGISTRY_STATE"
	defer func() {
		if err != nil {
			g2d1Fail(stage, err)
		}
	}()
	if b == nil || b.authority == nil || b.effects != nil {
		return errGuardProtocol
	}
	systemd := b.manifest.Executables["systemd"]
	if systemd.Package != "systemd" || systemd.Path != "/usr/lib/systemd/systemd" {
		return errGuardProtocol
	}
	stage = "REGISTRY_HASH"
	file, digest, err := openTrustedRegular(systemd.Path, 128<<20)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			g2d1Fail(stage, err)
		}
		_ = file.Close()
	}()
	if digest != systemd.SHA256 {
		return errGuardProtocol
	}
	stage = "REGISTRY_EXE"
	fd, err := unix.Openat(heldProcDirectory, "1/exe", unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			g2d1Fail(stage, err)
		}
		_ = unix.Close(fd)
	}()
	var expected, actual unix.Stat_t
	if e := unix.Fstat(int(file.Fd()), &expected); e != nil {
		g2d1Fail("REGISTRY_STAT", e)
		return errGuardProtocol
	}
	if e := unix.Fstat(fd, &actual); e != nil || !sameTrustedStat(expected, actual) {
		g2d1Fail("REGISTRY_STAT", e)
		return errGuardProtocol
	}
	stage = "REGISTRY_PIDFD"
	pidfd, err := unix.PidfdOpen(1, 0)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			g2d1Fail(stage, err)
		}
		_ = unix.Close(pidfd)
	}()
	stage = "REGISTRY_NATIVE"
	task, err := b.authority.guard.pinTask(1)
	if err != nil {
		return err
	}
	keep := false
	defer func() {
		if err != nil {
			g2d1Fail(stage, err)
		}
		if !keep {
			_ = b.authority.guard.dropTask(task.Cookie)
		}
	}()
	if task.TID != 1 || task.TGID != 1 || task.Cookie != b.parentTask.Cookie || task.StartBoottimeNS != b.parentTask.StartBoottimeNS || !pidfdSelects(pidfd, 1) {
		return errGuardProtocol
	}
	if e := verifyProcessExecutable(1, systemd.Path, systemd.SHA256); e != nil {
		g2d1Fail("REGISTRY_EXE_VERIFY", e)
		return errGuardProtocol
	}
	stage = "REGISTRY_EFFECTS"
	if err = b.initializeRuntimeEffects(task, fd, actual); err != nil {
		return err
	}
	b.checkpoint.lifecycle.runtimeGuard = b.observeRuntimeGuard
	keep = true
	return nil
}

// discoverBootstrapReader selects only PID1's unique live audit-reader child,
// holding a pidfd while checking the executable, argv and confinement. The
// subsequent PID1 handoff must independently deliver the same task and file.
func discoverBootstrapReader(manifest bootstrapManifest) (observedProcess, int, error) {
	var found observedProcess
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return found, -1, err
	}
	foundFD := -1
	for _, entry := range entries {
		pid, e := strconv.ParseUint(entry.Name(), 10, 32)
		if e != nil || pid <= 1 || strconv.FormatUint(pid, 10) != entry.Name() {
			continue
		}
		fd, e := unix.PidfdOpen(int(pid), 0)
		if e != nil {
			continue
		}
		process, e := observeProcess(uint32(pid), fd)
		if e != nil || process.PPID != 1 || process.UID != 0 || process.GID != 0 || process.ExecutableSHA256 != manifest.BrokerSHA256 || !exactArgumentVector(process.Argv, []string{"/g2-bpf-authority", "audit-reader"}) {
			_ = unix.Close(fd)
			continue
		}
		if foundFD >= 0 {
			_ = unix.Close(fd)
			_ = unix.Close(foundFD)
			return observedProcess{}, -1, errGuardProtocol
		}
		found, foundFD = process, fd
	}
	if foundFD < 0 || verifyAuditReaderConfinement(int(found.PID)) != nil || !livePIDFD(foundFD) {
		if foundFD >= 0 {
			_ = unix.Close(foundFD)
		}
		return observedProcess{}, -1, errGuardProtocol
	}
	return found, foundFD, nil
}

// receiveBootstrapReader establishes the reader before the first BPF syscall.
// The returned task pin and reader client are retained for the broker lifetime.
func receiveBootstrapReader(bootstrapFD int, guard guardDevice, boot string, manifest bootstrapManifest) (*auditReaderClient, guardTask, observedProcess, string, error) {
	var empty guardTask
	var noProcess observedProcess
	deadline, err := guestDeadline(60 * time.Second)
	if err != nil {
		return nil, empty, noProcess, "", err
	}
	peer, err := unix.GetsockoptUcred(bootstrapFD, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil || peer == nil || peer.Pid != 1 || peer.Uid != 0 || peer.Gid != 0 {
		g2d1Fail("READER_HANDOFF", err)
		return nil, empty, noProcess, "", errGuardProtocol
	}
	frame, cred, _, err := earlyProbeReceive(bootstrapFD, 0, deadline)
	if err != nil || cred == nil || cred.Pid != 1 || cred.Uid != 0 || cred.Gid != 0 || !exactKeys(frame, "version", "boot_id", "kind", "nonce") || frame["version"] != json.Number("1") || frame["boot_id"] != boot || frame["kind"] != "reader_ready" {
		g2d1Fail("READER_HANDOFF", err)
		return nil, empty, noProcess, "", errGuardProtocol
	}
	nonce, ok := frame["nonce"].(string)
	if !ok || !validCheckpointNonce(nonce) {
		return nil, empty, noProcess, "", errGuardProtocol
	}
	process, pidfd, err := discoverBootstrapReader(manifest)
	if err != nil {
		return nil, empty, noProcess, "", err
	}
	defer unix.Close(pidfd)
	task, err := guard.pinTask(process.PID)
	if err != nil {
		return nil, empty, noProcess, "", err
	}
	keep := false
	defer func() {
		if !keep {
			_ = guard.dropTask(task.Cookie)
		}
	}()
	if task.TID != process.PID || task.TGID != process.PID || task.StartBoottimeNS/10_000_000 != process.StartTicks || !livePIDFD(pidfd) {
		return nil, empty, noProcess, "", errGuardProtocol
	}
	identity := map[string]any{"version": json.Number("1"), "boot_id": boot, "kind": "reader_identity", "reader_pid": jsonU64(uint64(process.PID)), "reader_tid": jsonU64(uint64(task.TID)), "reader_start_ns": jsonU64(task.StartBoottimeNS), "reader_exe_sha256": manifest.BrokerSHA256, "nonce": nonce}
	if err = earlyProbeSend(bootstrapFD, identity, nil, deadline); err != nil {
		return nil, empty, noProcess, "", err
	}
	handoff, sender, rights, err := earlyProbeReceive(bootstrapFD, 3, deadline)
	if err != nil {
		return nil, empty, noProcess, "", err
	}
	retained := false
	defer func() {
		if !retained {
			for _, fd := range rights {
				_ = unix.Close(fd)
			}
		}
	}()
	identity["kind"] = "audit_reader"
	if sender == nil || sender.Pid != 1 || sender.Uid != 0 || sender.Gid != 0 || !sameJSON(handoff, identity) || !pidfdSelects(rights[1], process.PID) || !livePIDFD(pidfd) {
		return nil, empty, noProcess, "", errGuardProtocol
	}
	digest, heldStat, err := heldRegularDigest(rights[2], 128<<20)
	if err != nil || digest != manifest.BrokerSHA256 {
		g2d1Fail("READER_HANDOFF", err)
		return nil, empty, noProcess, "", errGuardProtocol
	}
	actualFD, err := unix.Open(fmt.Sprintf("/proc/%d/exe", process.PID), unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, empty, noProcess, "", err
	}
	var actualStat unix.Stat_t
	err = unix.Fstat(actualFD, &actualStat)
	_ = unix.Close(actualFD)
	if err != nil || !sameTrustedStat(heldStat, actualStat) {
		g2d1Fail("READER_HANDOFF", err)
		return nil, empty, noProcess, "", errGuardProtocol
	}
	if e := verifyAuditReaderConfinement(int(process.PID)); e != nil {
		g2d1Fail("READER_HANDOFF", e)
		return nil, empty, noProcess, "", errGuardProtocol
	}
	if !pidfdSelects(rights[1], process.PID) {
		g2d1Fail("READER_HANDOFF", nil)
		return nil, empty, noProcess, "", errGuardProtocol
	}
	reader, err := newAuditReaderClient(rights[0], rights[1], process.PID, boot, task.Cookie)
	if err != nil {
		return nil, empty, noProcess, "", err
	}
	_ = unix.Close(rights[2])
	retained, keep = true, true
	return reader, task, process, nonce, nil
}

func discoverBootstrapObserver(manifest bootstrapManifest) (observedProcess, int, error) {
	var found observedProcess
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return found, -1, err
	}
	foundFD := -1
	for _, entry := range entries {
		pid, e := strconv.ParseUint(entry.Name(), 10, 32)
		if e != nil || pid <= 1 || strconv.FormatUint(pid, 10) != entry.Name() {
			continue
		}
		fd, e := unix.PidfdOpen(int(pid), 0)
		if e != nil {
			continue
		}
		process, e := observeProcess(uint32(pid), fd)
		if e != nil || process.PPID != 1 || process.UID != 0 || process.GID != 0 || process.ExecutableSHA256 != manifest.BrokerSHA256 || !exactArgumentVector(process.Argv, []string{"/g2-bpf-authority", "terminal-observer"}) {
			_ = unix.Close(fd)
			continue
		}
		if foundFD >= 0 {
			_ = unix.Close(fd)
			_ = unix.Close(foundFD)
			return observedProcess{}, -1, errGuardProtocol
		}
		found, foundFD = process, fd
	}
	if foundFD < 0 || verifyTerminalObserverConfinement(int(found.PID)) != nil || !livePIDFD(foundFD) {
		if foundFD >= 0 {
			_ = unix.Close(foundFD)
		}
		return observedProcess{}, -1, errGuardProtocol
	}
	return found, foundFD, nil
}

func receiveBootstrapObserver(bootstrapFD int, guard guardDevice, boot string, manifest bootstrapManifest) (int, int, guardTask, observedProcess, string, error) {
	var empty guardTask
	var noProcess observedProcess
	deadline, err := guestDeadline(60 * time.Second)
	if err != nil {
		return -1, -1, empty, noProcess, "", err
	}
	frame, cred, _, err := earlyProbeReceive(bootstrapFD, 0, deadline)
	if err != nil || cred == nil || cred.Pid != 1 || cred.Uid != 0 || cred.Gid != 0 || !exactKeys(frame, "version", "boot_id", "kind", "nonce") || frame["version"] != json.Number("1") || frame["boot_id"] != boot || frame["kind"] != "observer_ready" {
		g2d1Fail("OBSERVER_HANDOFF", err)
		return -1, -1, empty, noProcess, "", errGuardProtocol
	}
	nonce, ok := frame["nonce"].(string)
	if !ok || !validCheckpointNonce(nonce) {
		return -1, -1, empty, noProcess, "", errGuardProtocol
	}
	process, pidfd, err := discoverBootstrapObserver(manifest)
	if err != nil {
		return -1, -1, empty, noProcess, "", err
	}
	defer unix.Close(pidfd)
	task, err := guard.pinTask(process.PID)
	if err != nil {
		return -1, -1, empty, noProcess, "", err
	}
	keep := false
	defer func() {
		if !keep {
			_ = guard.dropTask(task.Cookie)
		}
	}()
	if task.TID != process.PID || task.TGID != process.PID || task.StartBoottimeNS/10_000_000 != process.StartTicks || !livePIDFD(pidfd) {
		return -1, -1, empty, noProcess, "", errGuardProtocol
	}
	identity := map[string]any{"version": json.Number("1"), "boot_id": boot, "kind": "observer_identity", "observer_pid": jsonU64(uint64(process.PID)), "observer_tid": jsonU64(uint64(task.TID)), "observer_start_ns": jsonU64(task.StartBoottimeNS), "observer_exe_sha256": manifest.BrokerSHA256, "nonce": nonce}
	if err = earlyProbeSend(bootstrapFD, identity, nil, deadline); err != nil {
		return -1, -1, empty, noProcess, "", err
	}
	handoff, sender, rights, err := earlyProbeReceive(bootstrapFD, 3, deadline)
	if err != nil {
		return -1, -1, empty, noProcess, "", err
	}
	retained := false
	defer func() {
		if !retained {
			for _, fd := range rights {
				_ = unix.Close(fd)
			}
		}
	}()
	identity["kind"] = "terminal_observer"
	if sender == nil || sender.Pid != 1 || sender.Uid != 0 || sender.Gid != 0 || !sameJSON(handoff, identity) || !pidfdSelects(rights[1], process.PID) || !livePIDFD(pidfd) {
		return -1, -1, empty, noProcess, "", errGuardProtocol
	}
	digest, heldStat, err := heldRegularDigest(rights[2], 128<<20)
	if err != nil || digest != manifest.BrokerSHA256 {
		g2d1Fail("OBSERVER_HANDOFF", err)
		return -1, -1, empty, noProcess, "", errGuardProtocol
	}
	actualFD, err := unix.Open(fmt.Sprintf("/proc/%d/exe", process.PID), unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, -1, empty, noProcess, "", err
	}
	var actualStat unix.Stat_t
	err = unix.Fstat(actualFD, &actualStat)
	_ = unix.Close(actualFD)
	if err != nil || !sameTrustedStat(heldStat, actualStat) {
		g2d1Fail("OBSERVER_HANDOFF", err)
		return -1, -1, empty, noProcess, "", errGuardProtocol
	}
	if e := verifyTerminalObserverConfinement(int(process.PID)); e != nil {
		g2d1Fail("OBSERVER_HANDOFF", e)
		return -1, -1, empty, noProcess, "", errGuardProtocol
	}
	if !pidfdSelects(rights[1], process.PID) {
		g2d1Fail("OBSERVER_HANDOFF", nil)
		return -1, -1, empty, noProcess, "", errGuardProtocol
	}
	_ = unix.Close(rights[2])
	obsGuardFD, err := guard.bindObserver(task.Cookie)
	if err != nil {
		return -1, -1, empty, noProcess, "", err
	}
	if err = earlyProbeSend(rights[0], map[string]any{
		"version": json.Number("1"),
		"boot_id": boot,
		"kind":    "observer_guard",
		"nonce":   nonce,
	}, []int{obsGuardFD}, deadline); err != nil {
		_ = unix.Close(obsGuardFD)
		return -1, -1, empty, noProcess, "", err
	}
	_ = unix.Close(obsGuardFD)
	ackFrame, obsCred, _, err := earlyProbeReceive(rights[0], 0, deadline)
	if err != nil || obsCred == nil || uint32(obsCred.Pid) != process.PID || obsCred.Uid != 0 || obsCred.Gid != 0 || !exactKeys(ackFrame, "version", "boot_id", "kind", "nonce") || ackFrame["version"] != json.Number("1") || ackFrame["boot_id"] != boot || ackFrame["kind"] != "observer_guard_ready" || ackFrame["nonce"] != nonce {
		g2d1Fail("OBSERVER_HANDOFF", err)
		return -1, -1, empty, noProcess, "", errGuardProtocol
	}
	retained, keep = true, true
	return rights[0], rights[1], task, process, nonce, nil
}

// bootstrapReaderConfiguration obtains a fresh, authenticated configuration
// readback while PID1 is waiting in the corresponding handoff phase.
func bootstrapReaderConfiguration(reader *auditReaderClient, final bool, drain func() error) (observedAuditStatus, map[string]any, error) {
	var empty observedAuditStatus
	if reader == nil {
		return empty, nil, errGuardProtocol
	}
	nonce, err := earlyNonce()
	if err != nil {
		return empty, nil, err
	}
	now, err := monoNowNS()
	if err != nil || now >= authorityGuestDeadlineNS {
		if !final {
			g2d1Fail("READER_CONFIGURATION", err)
		}
		return empty, nil, errGuardProtocol
	}
	deadline := now + uint64((5 * time.Second).Nanoseconds())
	if deadline < now || deadline > authorityGuestDeadlineNS {
		deadline = authorityGuestDeadlineNS
	}
	if err = reader.begin("configuration", nonce, deadline); err != nil {
		return empty, nil, err
	}
	for {
		if drain != nil {
			if err = drain(); err != nil {
				return empty, nil, err
			}
		}
		ready, e := reader.service()
		if e != nil {
			return empty, nil, e
		}
		if ready {
			status, evidence, e := reader.result()
			if e != nil {
				return empty, nil, e
			}
			if _, e = validateAuditConfiguration(evidence, final, 0); e != nil {
				return empty, nil, e
			}
			return status, evidence, nil
		}
		events := int16(unix.POLLIN)
		if !reader.sent {
			events |= unix.POLLOUT
		}
		poll := []unix.PollFd{{Fd: int32(reader.fd), Events: events}, {Fd: int32(reader.pidfd), Events: unix.POLLIN}}
		if _, e = unix.Poll(poll, 10); e != nil && !errors.Is(e, unix.EINTR) {
			return empty, nil, e
		}
		if poll[1].Revents != 0 || poll[0].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
			return empty, nil, errGuardProtocol
		}
	}
}

// bootstrapResolver consumes the recorder's actual syscall results and
// publication records before those records are moved into the transcript.
func bootstrapResolver(pid uint64, recorder *bootstrapRecorder) (*brokerAuditResolver, error) {
	if recorder == nil || recorder.executor == 0 {
		return nil, errGuardProtocol
	}
	resolver, err := newBrokerAuditResolver(pid, recorder.executor)
	if err != nil {
		return nil, err
	}
	commands := map[string]uint64{
		"MAP_CREATE": unix.BPF_MAP_CREATE, "MAP_LOOKUP_ELEM": unix.BPF_MAP_LOOKUP_ELEM,
		"MAP_UPDATE_ELEM": unix.BPF_MAP_UPDATE_ELEM, "MAP_DELETE_ELEM": unix.BPF_MAP_DELETE_ELEM,
		"MAP_FREEZE": unix.BPF_MAP_FREEZE, "PROG_LOAD": unix.BPF_PROG_LOAD,
		"PROG_QUERY": unix.BPF_PROG_QUERY, "PROG_GET_FD_BY_ID": unix.BPF_PROG_GET_FD_BY_ID,
		"OBJ_GET_INFO_BY_FD": unix.BPF_OBJ_GET_INFO_BY_FD, "PROG_ATTACH": unix.BPF_PROG_ATTACH,
		"PROG_DETACH": unix.BPF_PROG_DETACH, "BTF_LOAD": unix.BPF_BTF_LOAD, "LINK_CREATE": unix.BPF_LINK_CREATE,
	}
	for _, entry := range recorder.entries {
		body := entry.body
		if entry.kind == "transaction" && body["state"] == "effect_observed" {
			id, ok := exactNumber(body["id"])
			name, nameOK := body["command"].(string)
			number, known := commands[name]
			value, numberOK := body["result"].(json.Number)
			if !ok || !nameOK || !known || !numberOK {
				return nil, errGuardProtocol
			}
			result, e := strconv.ParseInt(string(value), 10, 64)
			if e != nil || resolver.recordTransaction(auditedTransaction{ID: id, ExecutorCookie: recorder.executor, Command: name, Number: number, Result: result}) != nil {
				return nil, errGuardProtocol
			}
		}
		if entry.kind == "object" && body["class"] == "program" && body["state"] == "live" {
			cookie, ok1 := exactNumber(body["cookie"])
			id, ok2 := exactNumber(body["kernel_id"])
			owner, ok3 := exactNumber(body["owner_transaction"])
			sequence, ok4 := exactNumber(body["publication_seq"])
			if !ok1 || !ok2 || !ok3 || !ok4 || resolver.recordObject(auditedObject{Cookie: cookie, ProgramID: id, LoadTransactionID: owner, PublicationSequence: sequence, LoadAuthorized: true}) != nil {
				return nil, errGuardProtocol
			}
		}
	}
	return resolver, nil
}

type bootstrapAuditDrain struct {
	wake  int
	facts chan auditFact
	done  chan error
}

// The multicast subscription is drained while the original broker thread is
// executing bootstrap BPF syscalls. The fixed queue is bounded by the existing
// audit join's 4096-entry limit; an overflow fails bootstrap rather than
// dropping a fact or increasing the public evidence budget.
func startBootstrapAuditDrain(a *auditMulticast) (*bootstrapAuditDrain, error) {
	if a == nil || a.fd < 0 {
		return nil, errGuardProtocol
	}
	wake, err := unix.Eventfd(0, unix.EFD_CLOEXEC|unix.EFD_NONBLOCK)
	if err != nil {
		return nil, err
	}
	d := &bootstrapAuditDrain{wake: wake, facts: make(chan auditFact, 4096), done: make(chan error, 1)}
	go func() {
		defer close(d.facts)
		for {
			facts, e := a.receive()
			if e != nil && !auditReceiveAgain(e) {
				d.done <- e
				return
			}
			for _, fact := range facts {
				select {
				case d.facts <- fact:
				default:
					d.done <- errGuardProtocol
					return
				}
			}
			poll := []unix.PollFd{{Fd: int32(a.fd), Events: unix.POLLIN}, {Fd: int32(wake), Events: unix.POLLIN}}
			if _, e = unix.Poll(poll, 100); e != nil {
				if errors.Is(e, unix.EINTR) {
					continue
				}
				d.done <- e
				return
			}
			if poll[0].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 || poll[1].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
				d.done <- errGuardProtocol
				return
			}
			if poll[1].Revents&unix.POLLIN != 0 {
				d.done <- nil
				return
			}
		}
	}()
	return d, nil
}

func (d *bootstrapAuditDrain) finish(join *auditJoin) error {
	if d == nil || d.wake < 0 || join == nil {
		return errGuardProtocol
	}
	var signal [8]byte
	guardPut64(signal[:], 0, 1)
	_, err := unix.Write(d.wake, signal[:])
	if err != nil && !errors.Is(err, unix.EAGAIN) {
		return err
	}
	err = <-d.done
	_ = unix.Close(d.wake)
	d.wake = -1
	if err != nil {
		return err
	}
	for fact := range d.facts {
		if err = join.observeFact(fact); err != nil {
			return err
		}
	}
	return nil
}

// bootstrapBrokerAuthority establishes the real reader and audit evidence,
// seals the three links, and completes PID1's immutable audit handoff before
// publishing the control socket. No caller-provided identity becomes a trust
// anchor; both the broker and reader remain the original held tasks.
func bootstrapBrokerAuthority(bootstrapFD int) (_ *authorityBroker, _ int, err error) {
	if bootstrapFD < 0 || os.Getppid() != 1 || unix.Gettid() != os.Getpid() || os.Geteuid() != 0 {
		g2d1Fail("BROKER_IDENTITY", nil)
		return nil, -1, errGuardProtocol
	}
	manifestFile, manifestSHA, err := openTrustedRegular("/g2-bootstrap.json", maxControlFrame)
	if err != nil {
		g2d1Fail("BROKER_MANIFEST_OPEN", err)
		return nil, -1, err
	}
	raw, err := io.ReadAll(io.LimitReader(manifestFile, maxControlFrame+1))
	_ = manifestFile.Close()
	if err != nil || len(raw) == 0 || len(raw) > maxControlFrame {
		g2d1Fail("BROKER_MANIFEST_READ", err)
		return nil, -1, errGuardProtocol
	}
	manifest, err := parseBootstrapManifest(raw)
	if err != nil {
		g2d1Fail("BROKER_MANIFEST_PARSE", err)
		return nil, -1, err
	}
	if digest, e := procExecutableHash(os.Getpid()); e != nil || digest != manifest.BrokerSHA256 {
		g2d1Fail("BROKER_SELF_HASH", e)
		return nil, -1, errGuardProtocol
	}
	if digest, e := procExecutableHash(1); e != nil || digest != manifest.BrokerSHA256 {
		g2d1Fail("BROKER_PARENT_HASH", e)
		return nil, -1, errGuardProtocol
	}
	boot, err := fixedBootID()
	if err != nil {
		g2d1Fail("BROKER_BOOT_ID", err)
		return nil, -1, err
	}
	guardFD, err := unix.Open("/dev/g2_bpf_guard", unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		g2d1Fail("BROKER_GUARD_OPEN", err)
		return nil, -1, err
	}
	guard := guardDevice{fd: guardFD}
	parent, err := guard.pinTask(1)
	if err != nil || parent.TID != 1 || parent.TGID != 1 {
		g2d1Fail("BROKER_PARENT_PIN", err)
		_ = unix.Close(guardFD)
		return nil, -1, errGuardProtocol
	}
	reader, readerTask, readerProcess, nonce, err := receiveBootstrapReader(bootstrapFD, guard, boot, manifest)
	if err != nil {
		g2d1Fail("READER_HANDOFF", err)
		_ = unix.Close(guardFD)
		return nil, -1, err
	}
	obsSocketFD, obsPIDFD, obsTask, obsProcess, _, err := receiveBootstrapObserver(bootstrapFD, guard, boot, manifest)
	if err != nil {
		g2d1Fail("OBSERVER_HANDOFF", err)
		_ = reader.close()
		_ = unix.Close(guardFD)
		return nil, -1, err
	}
	deadline, err := guestDeadline(60 * time.Second)
	if err != nil {
		g2d1Fail("STAGE_CONFIG_DEADLINE", err)
		_ = unix.Close(obsSocketFD)
		_ = unix.Close(obsPIDFD)
		_ = reader.close()
		_ = unix.Close(guardFD)
		return nil, -1, err
	}
	stageCfg, cfgCred, _, err := earlyProbeReceive(bootstrapFD, 0, deadline)
	if err != nil || cfgCred == nil || cfgCred.Pid != 1 || !exactKeys(stageCfg, "version", "boot_id", "kind", "seal_stage", "nonce") || stageCfg["version"] != json.Number("1") || stageCfg["boot_id"] != boot || stageCfg["kind"] != "seal_stage_config" || stageCfg["nonce"] != nonce {
		g2d1Fail("STAGE_CONFIG", err)
		_ = unix.Close(obsSocketFD)
		_ = unix.Close(obsPIDFD)
		_ = reader.close()
		_ = unix.Close(guardFD)
		return nil, -1, errGuardProtocol
	}
	sealStageTarget, _ := stageCfg["seal_stage"].(string)
	maybePauseAtSealStage := func(stage string) error {
		if sealStageTarget == "" || sealStageTarget != stage {
			return nil
		}
		if e := earlyProbeSend(bootstrapFD, map[string]any{
			"version":     json.Number("1"),
			"boot_id":     boot,
			"kind":        "seal_stage_reached",
			"stage":       stage,
			"nonce":       nonce,
			"pid1_cookie": jsonU64(parent.Cookie),
		}, nil, deadline); e != nil {
			return e
		}
		for time.Until(deadline) > 0 {
			time.Sleep(100 * time.Millisecond)
		}
		return errGuardProtocol
	}
	if err = maybePauseAtSealStage("seal-guard-loaded"); err != nil {
		g2d1Fail("STAGE_GUARD_LOADED", err)
		_ = unix.Close(obsSocketFD)
		_ = unix.Close(obsPIDFD)
		_ = reader.close()
		_ = unix.Close(guardFD)
		return nil, -1, err
	}
	initialStatus, initialEvidence, err := bootstrapReaderConfiguration(reader, false, nil)
	if err != nil {
		g2d1Fail("READER_CONFIGURATION", err)
		_ = unix.Close(obsSocketFD)
		_ = unix.Close(obsPIDFD)
		_ = reader.close()
		_ = unix.Close(guardFD)
		return nil, -1, err
	}
	if err = dropBrokerAuditControl(); err != nil {
		g2d1Fail("AUDIT_DROP", err)
		_ = unix.Close(obsSocketFD)
		_ = unix.Close(obsPIDFD)
		_ = reader.close()
		_ = unix.Close(guardFD)
		return nil, -1, err
	}
	audit, err := openAuditMulticast()
	if err != nil {
		g2d1Fail("AUDIT_OPEN_DRAIN", err)
		_ = unix.Close(obsSocketFD)
		_ = unix.Close(obsPIDFD)
		_ = reader.close()
		_ = unix.Close(guardFD)
		return nil, -1, err
	}
	drain, err := startBootstrapAuditDrain(audit)
	if err != nil {
		g2d1Fail("AUDIT_OPEN_DRAIN", err)
		_ = audit.close()
		_ = unix.Close(obsSocketFD)
		_ = unix.Close(obsPIDFD)
		_ = reader.close()
		_ = unix.Close(guardFD)
		return nil, -1, err
	}
	guardStBefore, err := guard.status()
	if err != nil {
		g2d1Fail("GUARD_STATUS", err)
		_ = audit.close()
		_ = unix.Close(obsSocketFD)
		_ = unix.Close(obsPIDFD)
		_ = reader.close()
		_ = unix.Close(guardFD)
		return nil, -1, err
	}
	readerBody := map[string]any{"id": jsonU64(2), "role": "BOOTSTRAP_AUDIT_READER", "task_cookie": jsonU64(readerTask.Cookie), "tgid": jsonU64(uint64(readerTask.TGID)), "tid": jsonU64(uint64(readerTask.TID)), "start_ns": jsonU64(readerTask.StartBoottimeNS), "parent_cookie": jsonU64(parent.Cookie), "exe_sha256": readerProcess.ExecutableSHA256, "script_sha256": nil, "argv_sha256": readerProcess.ArgvSHA256, "uid": jsonU64(uint64(readerProcess.UID)), "gid": jsonU64(uint64(readerProcess.GID)), "cgroup_sha256": readerProcess.CgroupSHA256, "registration": "bootstrap", "terminal": nil}
	observerBody := map[string]any{"id": jsonU64(3), "role": "BOOTSTRAP_TERMINAL_OBSERVER", "task_cookie": jsonU64(obsTask.Cookie), "tgid": jsonU64(uint64(obsTask.TGID)), "tid": jsonU64(uint64(obsTask.TID)), "start_ns": jsonU64(obsTask.StartBoottimeNS), "parent_cookie": jsonU64(parent.Cookie), "exe_sha256": obsProcess.ExecutableSHA256, "script_sha256": nil, "argv_sha256": obsProcess.ArgvSHA256, "uid": jsonU64(uint64(obsProcess.UID)), "gid": jsonU64(uint64(obsProcess.GID)), "cgroup_sha256": obsProcess.CgroupSHA256, "registration": "bootstrap", "terminal": nil}
	moduleInstanceBody := map[string]any{
		"instance":      jsonU64(guardStBefore.ModuleInstance),
		"loader_cookie": jsonU64(parent.Cookie),
		"module_sha256": manifest.GuardSHA256,
		"load_file": map[string]any{
			"dev":  jsonU64(guardStBefore.ModuleDev),
			"ino":  jsonU64(guardStBefore.ModuleIno),
			"size": jsonU64(guardStBefore.ModuleSize),
		},
		"observer_bindings_sha256": manifest.ObserverBindingsSHA256,
	}
	authority, err := loadAndSealPolicy(manifest, manifestSHA, guard, func(rec *bootstrapRecorder) error {
		if e := rec.add("principal", readerBody); e != nil {
			return e
		}
		if e := rec.add("principal", observerBody); e != nil {
			return e
		}
		if e := rec.add("module_instance", moduleInstanceBody); e != nil {
			return e
		}
		return rec.add("audit_readback", initialEvidence)
	}, maybePauseAtSealStage)
	if err != nil {
		g2d1Fail("POLICY_LOAD_SEAL", err)
		_ = audit.close()
		_ = unix.Close(obsSocketFD)
		_ = unix.Close(obsPIDFD)
		_ = reader.close()
		return nil, -1, err
	}
	epoch := hex.EncodeToString(authority.seal.Epoch[:])
	transcript := newAuthorityTranscript(boot, epoch)
	self, err := observeProcess(uint32(os.Getpid()), authority.brokerPIDFD)
	if err != nil || self.PPID != 1 || self.ExecutableSHA256 != manifest.BrokerSHA256 {
		g2d1Fail("TRANSCRIPT_AUDIT_JOIN", err)
		return nil, -1, errGuardProtocol
	}
	selfTask, err := guard.pinTask(uint32(os.Getpid()))
	if err != nil || selfTask.Cookie != authority.brokerCookie || selfTask.TGID != uint32(os.Getpid()) || selfTask.TID != uint32(os.Getpid()) {
		g2d1Fail("TRANSCRIPT_AUDIT_JOIN", err)
		return nil, -1, errGuardProtocol
	}
	b := &authorityBroker{manifest: manifest, bootID: boot, epoch: epoch, authority: authority, transcript: transcript, audit: audit, reader: reader, observerSocketFD: obsSocketFD, observerPIDFD: obsPIDFD, observerCookie: obsTask.Cookie, parentTask: parent, principals: make(map[uint64]*controlPrincipal), launches: make(map[uint64]*launchRegistration), daemons: make(map[uint64]*daemonRegistration), grants: make(map[uint64]*brokerGrant), nextPrincipal: 4, programLogScratch: make([]byte, 131072)}
	principal := &controlPrincipal{ID: 1, TaskCookie: selfTask.Cookie, StartNS: selfTask.StartBoottimeNS, Role: "BOOTSTRAP_BROKER", PIDFD: authority.brokerPIDFD, PID: uint32(os.Getpid()), UID: 0, GID: 0, NextRequest: 1}
	if err = b.appendPrincipal(principal, self, parent.Cookie, "", "bootstrap"); err != nil {
		g2d1Fail("TRANSCRIPT_AUDIT_JOIN", err)
		return nil, -1, err
	}
	b.resolver, err = bootstrapResolver(uint64(os.Getpid()), authority.recorder)
	if err != nil {
		g2d1Fail("TRANSCRIPT_AUDIT_JOIN", err)
		return nil, -1, err
	}
	if err = authority.recorder.publish(transcript); err != nil {
		g2d1Fail("TRANSCRIPT_AUDIT_JOIN", err)
		return nil, -1, err
	}
	b.join = newAuditJoin(transcript, b.resolver)
	for _, record := range authority.recorder.guardRecords {
		if record.Kind == 7 || record.Kind == 8 || record.Kind == 13 {
			if e := b.join.observeGuard(record); e != nil {
				g2d1Fail("TRANSCRIPT_AUDIT_JOIN", e)
				return nil, -1, errGuardProtocol
			}
		}
	}
	if err = drain.finish(b.join); err != nil {
		g2d1Fail("TRANSCRIPT_AUDIT_JOIN", err)
		return nil, -1, err
	}
	lifecycle := &checkpointLifecycle{objects: make(map[uint64]*deferredObject), cgroups: make(map[cgroupKey]*deferredCgroup), lineage: &daemonLineage{}, retiringTasks: make(map[uint64]bool)}
	b.checkpoint = &checkpointController{guard: guard, audit: audit, transcript: transcript, join: b.join, lifecycle: lifecycle, cursor: authority.guardCursor, auditBaselineSet: true, auditLost: initialStatus.lost}
	b.reads = &brokerReadState{bootID: boot, epoch: epoch, runToken: authorityRunToken, guard: guard, transcript: transcript, checkpoint: b.checkpoint}
	actor := auditControlActor{TaskCookie: parent.Cookie, TGID: 1, TID: 1, StartNS: parent.StartBoottimeNS, ExeSHA256: manifest.BrokerSHA256}
	for range 2 {
		if err = b.join.expectControl(auditControlExpectation{Actor: actor, Phase: "final_rules", Change: "add_rule", RuleList: []uint32{4}}); err != nil {
			g2d1Fail("TRANSCRIPT_AUDIT_JOIN", err)
			return nil, -1, err
		}
	}
	oldEnabled, newEnabled := uint32(1), uint32(2)
	if err = b.join.expectControl(auditControlExpectation{Actor: actor, Phase: "final_lock", Change: "enabled", OldValue: &oldEnabled, NewValue: &newEnabled}); err != nil {
		g2d1Fail("TRANSCRIPT_AUDIT_JOIN", err)
		return nil, -1, err
	}
	deadline, err = guestDeadline(60 * time.Second)
	if err != nil {
		g2d1Fail("POLICY_SEALED_DEADLINE", err)
		return nil, -1, err
	}
	if err = earlyProbeSend(bootstrapFD, map[string]any{"version": json.Number("1"), "boot_id": boot, "kind": "policy_sealed", "nonce": nonce, "pid1_cookie": jsonU64(parent.Cookie)}, nil, deadline); err != nil {
		g2d1Fail("POLICY_SEALED_SEND", err)
		return nil, -1, err
	}
	if err = b.finishBootstrapAudit(bootstrapFD, nonce, deadline); err != nil {
		return nil, -1, err
	}
	listener, err := openBrokerControlListener(boot)
	if err != nil {
		return nil, -1, err
	}
	return b, listener, nil
}

func (b *authorityBroker) finishBootstrapAudit(fd int, nonce string, deadline time.Time) error {
	if b == nil || b.checkpoint == nil || b.join == nil || b.reader == nil || !validCheckpointNonce(nonce) {
		return errGuardProtocol
	}
	drain := func() error {
		_, _, err := pollAuditWatermark(b.checkpoint.guard, b.audit, b.transcript, b.join, &b.checkpoint.cursor, b.checkpoint.lifecycle)
		return err
	}
	for {
		if time.Until(deadline) <= 0 || !pidfdSelects(b.reader.pidfd, b.reader.pid) {
			return errGuardProtocol
		}
		if err := drain(); err != nil {
			return err
		}
		poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}, {Fd: int32(b.audit.fd), Events: unix.POLLIN}, {Fd: int32(b.reader.pidfd), Events: unix.POLLIN}}
		if _, err := unix.Poll(poll, 100); err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return err
		}
		if poll[2].Revents != 0 || poll[0].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 || poll[1].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
			return errGuardProtocol
		}
		if poll[0].Revents&unix.POLLIN != 0 {
			frame, cred, _, err := earlyProbeReceive(fd, 0, deadline)
			if err != nil || cred == nil || cred.Pid != 1 || cred.Uid != 0 || cred.Gid != 0 || !exactKeys(frame, "version", "boot_id", "kind", "nonce") || frame["version"] != json.Number("1") || frame["boot_id"] != b.bootID || frame["kind"] != "audit_finalized" || frame["nonce"] != nonce {
				return errGuardProtocol
			}
			break
		}
	}
	status, evidence, err := bootstrapReaderConfiguration(b.reader, true, drain)
	if err != nil || status.lost != b.checkpoint.auditLost {
		return errGuardProtocol
	}
	sequence, _, err := b.transcript.append("audit_readback", evidence)
	if err != nil {
		return err
	}
	for {
		if time.Until(deadline) <= 0 || !pidfdSelects(b.reader.pidfd, b.reader.pid) {
			return errGuardProtocol
		}
		_, ready, e := pollAuditWatermark(b.checkpoint.guard, b.audit, b.transcript, b.join, &b.checkpoint.cursor, b.checkpoint.lifecycle)
		if e != nil {
			return e
		}
		if ready && b.join.auditControlsSettled() && len(b.resolver.transactions) == 0 {
			break
		}
		poll := []unix.PollFd{{Fd: int32(b.audit.fd), Events: unix.POLLIN}, {Fd: int32(b.reader.pidfd), Events: unix.POLLIN}}
		if _, e = unix.Poll(poll, 10); e != nil && !errors.Is(e, unix.EINTR) {
			return e
		}
		if poll[1].Revents != 0 || poll[0].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
			return errGuardProtocol
		}
	}
	return earlyProbeSend(fd, map[string]any{"version": json.Number("1"), "boot_id": b.bootID, "kind": "audit_handoff_ready", "nonce": nonce, "readback_sequence": jsonU64(sequence)}, nil, deadline)
}

// installBrokerLineage pins each allowed executable before WATCH_DAEMON emits
// its first event. Every role is bound to its fixed package or installed copy.
func (b *authorityBroker) installBrokerLineage() error {
	if b == nil || b.checkpoint == nil || b.checkpoint.lifecycle == nil {
		return errGuardProtocol
	}
	if b.checkpoint.invalid || b.checkpoint.faulted {
		return errGuardProtocol
	}
	current := b.checkpoint.lifecycle.lineage
	if current == nil {
		return errGuardProtocol
	}
	if len(current.executables) != 0 {
		return nil
	}
	if current.root != 0 || len(current.nodes) != 0 {
		return errGuardProtocol
	}
	lineage := &daemonLineage{}
	inputs := []struct{ role, path, hash string }{
		{"helper", "/opt/g2/g2-bpf-authority", b.manifest.BrokerSHA256},
		{"wrapper", "/opt/g2/g2-bpf-runtime", b.manifest.BrokerSHA256},
		{"containerd_launcher", containerdLauncherPath, b.manifest.BrokerSHA256},
	}
	for _, role := range []string{"dockerd", "containerd", "shim", "runc"} {
		executable := b.manifest.Executables[role]
		inputs = append(inputs, struct{ role, path, hash string }{role, executable.Path, executable.SHA256})
	}
	keys := make(map[string]executableKey, len(inputs))
	closeCandidate := func() {
		for _, executable := range lineage.executables {
			_ = executable.file.Close()
		}
	}
	for _, input := range inputs {
		key, err := lineage.addExecutable(input.role, input.path, input.hash)
		if err != nil {
			closeCandidate()
			return err
		}
		keys[input.role] = key
	}
	launcher, target := keys["containerd_launcher"], keys["containerd"]
	if launcher == target || launcher == keys["helper"] || launcher == keys["wrapper"] {
		closeCandidate()
		return errGuardProtocol
	}
	_, _, err := b.transcript.append("containerd_launcher", map[string]any{
		"path": containerdLauncherPath, "exe_sha256": b.manifest.BrokerSHA256,
		"exe_dev": jsonU64(launcher.dev), "exe_ino": jsonU64(launcher.ino),
		"target_path": "/usr/bin/containerd", "target_sha256": b.manifest.Executables["containerd"].SHA256,
		"target_dev": jsonU64(target.dev), "target_ino": jsonU64(target.ino),
	})
	if err != nil {
		b.checkpoint.markFault()
		closeCandidate()
		return err
	}
	b.checkpoint.lifecycle.lineage = lineage
	return nil
}

func runAuthorityBroker(bootstrapFD int) (err error) {
	b, listener, err := bootstrapBrokerAuthority(bootstrapFD)
	if err != nil {
		return err
	}
	_ = unix.Close(bootstrapFD)
	defer unix.Close(listener)
	defer b.reader.close()
	defer unix.Close(b.observerSocketFD)
	defer unix.Close(b.observerPIDFD)
	defer b.audit.close()
	defer unix.Close(b.authority.guard.fd)
	defer func() {
		if err != nil {
			b.checkpoint.markFault()
			b.closeOwnedOnFault()
		}
	}()
	nullFD, err := unix.Open("/dev/null", unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer unix.Close(nullFD)
	var actual, named unix.Stat_t
	if unix.Fstat(nullFD, &actual) != nil || unix.Lstat("/dev/null", &named) != nil || actual.Mode&unix.S_IFMT != unix.S_IFCHR || named.Mode&unix.S_IFMT != unix.S_IFCHR || actual.Dev != named.Dev || actual.Ino != named.Ino || actual.Rdev != unix.Mkdev(1, 3) {
		return errGuardProtocol
	}
	if _, err = b.recordBootstrapDevNull(nullFD); err != nil {
		return err
	}
	err = b.serveBrokerPoll(listener, brokerLoopHandlers{
		notification: func(inv *runtimeInvocation, n seccompNotification) error {
			return b.dispatchBrokerNotification(inv, n, nullFD)
		},
		deaths: func() error {
			if b.effects == nil {
				return nil
			}
			return b.serviceRuntimeDeaths()
		},
	})
	if err != nil {
		g2d1CaptureFailure("BROKER_SERVE_RETURN", err)
	}
	return err
}
