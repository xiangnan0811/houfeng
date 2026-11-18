//go:build linux && amd64 && !cgo

package main

import (
	"bufio"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestAdmitHeaderDiagnosticAndDeferredSelection(t *testing.T) {
	for _, tc := range []struct {
		name       string
		input      string
		readFailed bool
	}{
		{name: "truncated stream", input: "HTTP/1.1 200 OK\r\nX-Private: private-header-47b9\r\n", readFailed: true},
		{name: "invalid framing", input: "HTTP/1.1 200 OK private-header-47b9\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			if _, err := io.WriteString(writer, tc.input); err != nil {
				writer.Close()
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			_, rejected, observed := boundedDockerHeadersDiagnostic(bufio.NewReader(reader))
			if rejected != errGuardProtocol {
				t.Fatalf("production rejection = %v", rejected)
			}
			if tc.readFailed {
				if !errors.Is(observed, io.EOF) {
					t.Fatalf("read cause = %v, want EOF", observed)
				}
			} else if observed != nil {
				t.Fatalf("invalid framing reported read cause %v", observed)
			}

			original := g2d1
			defer func() { g2d1 = original }()
			outputReader, outputWriter, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer outputReader.Close()
			defer outputWriter.Close()
			g2d1.fd, g2d1.role, g2d1.owned = int(outputWriter.Fd()), 'B', false
			g2d1.count, g2d1.failed, g2d1.captured = 0, false, false
			principal := &controlPrincipal{}
			result := make(chan admitIOResult, 1)
			result <- admitIOResult{
				err: rejected,
				diagnostic: admitDiagnostic{
					stage:    "ADMIT_PROBE_ATTACH_HEADERS",
					observed: observed,
				},
			}
			broker := &authorityBroker{pendingAdmit: &pendingAdmit{principal: principal, eventFD: -1, result: result}}
			gotPrincipal, _, done, gotErr := broker.finishAdmitContainer()
			if gotPrincipal != principal || !done || gotErr != rejected {
				t.Fatalf("admission result: principal=%p done=%t err=%v", gotPrincipal, done, gotErr)
			}
			g2d1CaptureFailure("BROKER_PENDING_OPERATION", gotErr)
			wantCause := observed
			if wantCause == nil {
				wantCause = rejected
			}
			if !g2d1.captured || g2d1.capturedStage != "ADMIT_PROBE_ATTACH_HEADERS" || g2d1.capturedErr != wantCause {
				t.Fatalf("deferred selection: captured=%t stage=%q cause=%v", g2d1.captured, g2d1.capturedStage, g2d1.capturedErr)
			}
			poll := []unix.PollFd{{Fd: int32(outputReader.Fd()), Events: unix.POLLIN}}
			if n, err := unix.Poll(poll, 0); err != nil || n != 0 {
				t.Fatalf("premature output: ready=%d err=%v", n, err)
			}
			g2d1TerminalFail(gotErr)
			if err := outputWriter.Close(); err != nil {
				t.Fatal(err)
			}
			out, err := io.ReadAll(outputReader)
			if err != nil || !strings.HasPrefix(string(out), "G2D1 B ADMIT_PROBE_ATTACH_HEADERS ") || len(out) > 128 || strings.Contains(string(out), "private-header-47b9") || g2d1.count != 1 {
				t.Fatalf("terminal output: %q count=%d err=%v", out, g2d1.count, err)
			}
		})
	}
}

func TestRuntimeMountRejectsDeviceType(t *testing.T) {
	diagnostic := admitDiagnostic{stage: "ADMIT_INSPECT_RUNTIME_CONTAINER"}
	err := runtimeMountIdentity("/dev/null", &diagnostic)
	if err != errGuardProtocol || diagnostic.stage != "ADMIT_MOUNT_TYPE" || diagnostic.observed != errGuardProtocol {
		t.Fatalf("device rejection: stage=%q observed=%v returned=%v", diagnostic.stage, diagnostic.observed, err)
	}
}

func TestRuntimeInspectionFirstRejectionDeferred(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	original := g2d1
	defer func() { g2d1 = original }()
	g2d1.fd, g2d1.role, g2d1.owned = int(writer.Fd()), 'B', false
	g2d1.count, g2d1.failed, g2d1.captured = 0, false, false

	diagnostic := admitDiagnostic{stage: "ADMIT_INSPECT_RUNTIME_CONTAINER"}
	diagnostic.reject("UNOBSERVED", nil)
	if diagnostic.stage != "ADMIT_INSPECT_RUNTIME_CONTAINER" || diagnostic.observed != nil {
		t.Fatal("nil observation claimed the rejection")
	}
	const secret = "private-diagnostic-input-47b9"
	_, inspectionErr := inspectRuntimeContainer(nil, secret, nil, time.Time{}, &diagnostic)
	if inspectionErr != errGuardProtocol || diagnostic.stage != "ADMIT_INSPECT_INPUT" || diagnostic.observed != errGuardProtocol {
		t.Fatalf("inspection rejection: stage=%q observed=%v returned=%v", diagnostic.stage, diagnostic.observed, inspectionErr)
	}
	diagnostic = admitDiagnostic{stage: "ADMIT_INSPECT_RUNTIME_CONTAINER"}
	facts := &runtimeContainerFacts{rootfs: "/dev/null/" + secret}
	err = verifyMountAndRootfsIdentities(facts, &diagnostic)
	if err == nil || diagnostic.stage != "ADMIT_MOUNT_PARENT" || diagnostic.observed != err {
		t.Fatalf("mount rejection: stage=%q observed=%v returned=%v", diagnostic.stage, diagnostic.observed, err)
	}
	_, inspectionErr = inspectRuntimeContainer(nil, secret, nil, time.Time{}, &diagnostic)
	if inspectionErr != errGuardProtocol || diagnostic.stage != "ADMIT_MOUNT_PARENT" || diagnostic.observed != err {
		t.Fatalf("first rejection lost: stage=%q observed=%v inspection=%v", diagnostic.stage, diagnostic.observed, inspectionErr)
	}
	if g2d1.failed || g2d1.captured || g2d1.count != 0 {
		t.Fatal("inspection captured a global failure")
	}
	principal := &controlPrincipal{}
	result := make(chan admitIOResult, 1)
	result <- admitIOResult{err: err, diagnostic: diagnostic}
	broker := &authorityBroker{pendingAdmit: &pendingAdmit{principal: principal, eventFD: -1, result: result}}
	gotPrincipal, _, done, gotErr := broker.finishAdmitContainer()
	if gotPrincipal != principal || !done || gotErr != err || !g2d1.captured || g2d1.capturedErr != err {
		t.Fatalf("finish: principal=%p done=%t err=%v captured=%t", gotPrincipal, done, gotErr, g2d1.captured)
	}
	poll := []unix.PollFd{{Fd: int32(reader.Fd()), Events: unix.POLLIN}}
	if n, e := unix.Poll(poll, 0); e != nil || n != 0 {
		t.Fatalf("premature output: ready=%d err=%v", n, e)
	}
	g2d1CaptureFailure("OUTER_FAILURE", inspectionErr)
	g2d1TerminalFail(inspectionErr)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(reader)
	if err != nil || !strings.HasPrefix(string(out), "G2D1 B ADMIT_MOUNT_PARENT ") || len(out) > 128 || strings.Contains(string(out), secret) || g2d1.count != 1 {
		t.Fatalf("terminal output: %q count=%d err=%v", out, g2d1.count, err)
	}
}
