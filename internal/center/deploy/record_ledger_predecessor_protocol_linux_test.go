//go:build linux

package deploy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// Exercise the result protocol over an actual credentialled socket. This is
// transport evidence only; the real APP and guest lifecycle have separate checks.
func TestLedgerPredecessorOutputTransport(t *testing.T) {
	sockets, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(sockets[0])
	defer unix.Close(sockets[1])
	peer := unix.Ucred{Pid: int32(os.Getpid()), Uid: uint32(os.Geteuid()), Gid: uint32(os.Getegid())}
	sender, err := ledgerCallbackSocket(sockets[0], peer)
	if err != nil {
		t.Fatal(err)
	}
	defer sender.close()
	receiver, err := ledgerCallbackSocket(sockets[1], peer)
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.close()
	dir, err := unix.Open(t.TempDir(), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(dir)
	const frame = "{\"version\":1,\"phase_generation\":2,\"status\":\"unavailable\",\"operation_id\":\"\",\"reason\":\"admission_unavailable\"}\n"
	digest := sha256.Sum256([]byte(frame))
	digestText := hex.EncodeToString(digest[:])
	call := ledgerPredecessorCall{fence: &RecordLedgerFence{control: dir}, child: &ledgerPredecessorChild{channel: sender}, intent: ledgerPredecessorIntent{PhaseGeneration: 2}, resultDigest: digestText}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// The result-written request can precede the final stdout byte. No ACK or
	// receipt may be emitted while only the incomplete line has arrived.
	if complete, err := call.finishOutput(ctx, []byte(strings.TrimSuffix(frame, "\n"))); err != nil || complete {
		t.Fatalf("partial line acknowledged: %v %v", complete, err)
	}
	buffer := make([]byte, 128)
	if _, _, err := unix.Recvfrom(sockets[1], buffer, unix.MSG_DONTWAIT); err != unix.EAGAIN {
		t.Fatalf("unexpected pre-line ACK: %v", err)
	}
	if _, found, err := fenceReadFile(dir, ledgerPredecessorName("result-written", 2)); err != nil || found {
		t.Fatalf("premature receipt: %v %v", found, err)
	}
	if complete, err := call.finishOutput(ctx, []byte(frame)); err != nil || !complete {
		t.Fatalf("complete line failed: %v %v", complete, err)
	}
	body, err := receiver.receive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var ack map[string]any
	if json.Unmarshal(body, &ack) != nil || len(ack) != 3 || ack["op"] != "result_written_ack" || ack["phase_generation"] != float64(2) || ack["result_digest"] != digestText {
		t.Fatalf("incorrect ACK: %s", body)
	}
	var saved ledgerPredecessorWrittenResult
	found, err := ledgerPredecessorRead(dir, "result-written", 2, &saved)
	if err != nil || !found || saved.ResultDigest != digestText || saved.Result.Status != "unavailable" || saved.Result.Reason != "admission_unavailable" {
		t.Fatalf("original output not durably bound: %#v %v", saved, err)
	}
	carrier, original, captured, err := ledgerPredecessorReadStdout(dir, call.intent)
	if err != nil || !captured || string(carrier.StdoutBytes) != frame || original != saved.Result || carrier.ResultDigest != digestText {
		t.Fatalf("captured stdout differs from emitted bytes: captured=%v err=%v", captured, err)
	}
}

func TestLedgerPredecessorOutputRejectsInvalidFrames(t *testing.T) {
	const valid = "{\"version\":1,\"phase_generation\":2,\"status\":\"unavailable\",\"operation_id\":\"\",\"reason\":\"admission_unavailable\"}\n"
	cases := map[string]string{
		"extra line":           valid + valid,
		"null operation":       strings.Replace(valid, `"operation_id":""`, `"operation_id":null`, 1),
		"wrong phase":          strings.Replace(valid, `"phase_generation":2`, `"phase_generation":3`, 1),
		"unknown field":        strings.Replace(valid, `"version":1`, `"version":1,"extra":true`, 1),
		"duplicate field":      strings.Replace(valid, `"version":1`, `"version":1,"version":1`, 1),
		"unobserved commit":    strings.Replace(strings.Replace(valid, `"status":"unavailable"`, `"status":"committed"`, 1), `"reason":"admission_unavailable"`, `"reason":"app_reserved"`, 1),
		"unobserved rejection": strings.Replace(strings.Replace(valid, `"status":"unavailable"`, `"status":"rejected"`, 1), `"reason":"admission_unavailable"`, `"reason":"source_denied"`, 1),
	}
	for name, frame := range cases {
		t.Run(name, func(t *testing.T) {
			digest := sha256.Sum256([]byte(frame))
			call := ledgerPredecessorCall{intent: ledgerPredecessorIntent{PhaseGeneration: 2}, resultDigest: hex.EncodeToString(digest[:])}
			if complete, err := call.finishOutput(context.Background(), []byte(frame)); err == nil || complete {
				t.Fatalf("invalid frame acknowledged: %v %v", complete, err)
			}
		})
	}
	t.Run("wrong byte digest", func(t *testing.T) {
		call := ledgerPredecessorCall{intent: ledgerPredecessorIntent{PhaseGeneration: 2}, resultDigest: strings.Repeat("1", 64)}
		if complete, err := call.finishOutput(context.Background(), []byte(valid)); err == nil || complete {
			t.Fatalf("wrong digest acknowledged: %v %v", complete, err)
		}
	})
}

func TestLedgerPredecessorStdoutRecovery(t *testing.T) {
	dir, err := unix.Open(t.TempDir(), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(dir)
	intent := ledgerPredecessorIntent{PhaseGeneration: 2, RecoveryID: "original-rid"}
	// A legal maximum-size frame has noncanonical whitespace that must survive
	// storage exactly; its base64 carrier exceeds the 16KiB wire-frame limit.
	const line = "{\"reason\":\"admission_unavailable\",\"operation_id\":\"\",\"status\":\"unavailable\",\"phase_generation\":2,\"version\":1}\n"
	frame := []byte(strings.Repeat(" ", ledgerCallbackFrameLimit-len(line)) + line)
	digest := ledgerPredecessorHash(frame)
	result, err := ledgerPredecessorParseOutput(frame, 2)
	if err != nil {
		t.Fatal(err)
	}
	written := ledgerPredecessorWrittenResult{Version: 1, PhaseGeneration: 2, RecoveryID: intent.RecoveryID, ResultDigest: digest, Result: result}
	if _, err := ledgerPublishExact(dir, ledgerPredecessorName("result-written", 2), written); err != nil {
		t.Fatal(err)
	}
	fence := &RecordLedgerFence{control: dir}
	if _, err := ledgerResolvePredecessor(context.Background(), fence, intent, ledgerPredecessorEffectBinding{}); err == nil {
		t.Fatal("result-written without original stdout was admitted")
	}
	if err := ledgerPredecessorPublishStdout(dir, intent, digest, frame); err != nil {
		t.Fatal(err)
	}
	carrier, original, found, err := ledgerPredecessorReadStdout(dir, intent)
	if err != nil || !found || string(carrier.StdoutBytes) != string(frame) || original != result {
		t.Fatalf("maximum-size original frame did not survive: found=%v err=%v", found, err)
	}
	resolution, err := ledgerResolvePredecessor(context.Background(), fence, intent, ledgerPredecessorEffectBinding{})
	if err != nil || resolution.Outcome != "preview_not_created" {
		t.Fatalf("valid no-effect terminal evidence rejected: %#v %v", resolution, err)
	}
	if err := ledgerPredecessorPublishStdout(dir, intent, ledgerPredecessorHash([]byte(line)), []byte(line)); err == nil {
		t.Fatal("semantically equal re-encoding overwrote original stdout")
	}
	if err := unix.Unlinkat(dir, ledgerPredecessorName("result-written", 2), 0); err != nil {
		t.Fatal(err)
	}
	resolution, err = ledgerResolvePredecessor(context.Background(), fence, intent, ledgerPredecessorEffectBinding{})
	if err != nil || resolution.Outcome != "preview_not_created" {
		t.Fatalf("carrier-only publication prefix rejected: %#v %v", resolution, err)
	}
	wrongIntent := intent
	wrongIntent.RecoveryID = "different-rid"
	if _, _, _, err := ledgerPredecessorReadStdout(dir, wrongIntent); err == nil {
		t.Fatal("original stdout accepted for a different recovery identity")
	}
}
