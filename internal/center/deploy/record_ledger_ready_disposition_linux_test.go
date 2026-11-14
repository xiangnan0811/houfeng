//go:build linux

package deploy

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// These exercise the Go endpoint and real per-packet kernel credentials. Root's
// journal decisions and guest lifecycle still require their separate tests.
func readyDispositionTransport(t *testing.T, wrongPeer bool) (*ledgerMaintenanceChannel, *ledgerCallbackChannel, int) {
	t.Helper()
	sockets, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Close(sockets[0]); _ = unix.Close(sockets[1]) })
	peer := unix.Ucred{Pid: int32(os.Getpid()), Uid: uint32(os.Geteuid()), Gid: uint32(os.Getegid())}
	clientPeer := peer
	if wrongPeer {
		clientPeer.Pid++
	}
	client, err := ledgerCallbackSocket(sockets[0], clientPeer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.close)
	server, err := ledgerCallbackSocket(sockets[1], peer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.close)
	pidfd, err := unix.PidfdOpen(os.Getpid(), 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Close(pidfd) })
	return &ledgerMaintenanceChannel{realm: &ledgerRealmChannel{channel: client, pidfd: pidfd}}, server, sockets[1]
}

func readyDispositionRequest() ledgerMaintenanceCheckpoint {
	return ledgerMaintenanceCheckpoint{
		Family: "migration", RecoveryID: "recovery-test", ApplicationDigest: strings.Repeat("1", 64),
		PhaseGeneration: 7, Attempt: 0, Kind: "ready", RecordSHA256: strings.Repeat("2", 64),
	}
}

func readyDispositionReceiveRequest(ctx context.Context, server *ledgerCallbackChannel) (string, error) {
	body, err := server.receive(ctx)
	if err != nil {
		return "", err
	}
	var fields map[string]any
	if err := json.Unmarshal(body, &fields); err != nil {
		return "", err
	}
	nonce, ok := fields["nonce"].(string)
	decoded, decodeErr := hex.DecodeString(nonce)
	if !ok || decodeErr != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != nonce {
		return "", fmt.Errorf("invalid request nonce: %q", nonce)
	}
	want := map[string]any{
		"version": float64(1), "op": "maintenance_ready_disposition", "family": "migration",
		"recovery_id": "recovery-test", "application_digest": strings.Repeat("1", 64),
		"phase_generation": float64(7), "attempt": float64(0), "kind": "ready",
		"record_sha256": strings.Repeat("2", 64), "nonce": nonce,
	}
	if !reflect.DeepEqual(fields, want) {
		return "", fmt.Errorf("wrong disposition request: %s", body)
	}
	return nonce, nil
}

func readyDispositionResponse(nonce, disposition, witnessSequence, witnessDigest string) string {
	return fmt.Sprintf(`{"version":1,"op":"maintenance_ready_disposition_ack","family":"migration","recovery_id":"recovery-test","application_digest":"%s","phase_generation":7,"attempt":0,"kind":"ready","record_sha256":"%s","nonce":"%s","disposition":"%s","decision_sequence":12,"decision_digest":"%s","ready_witness_sequence":%s,"ready_witness_digest":%s}`,
		strings.Repeat("1", 64), strings.Repeat("2", 64), nonce, disposition, strings.Repeat("3", 64), witnessSequence, witnessDigest)
}

func TestLedgerReadyDispositionCredentialledTransport(t *testing.T) {
	for _, disposition := range []string{"witnessed", "unwitnessed_retired"} {
		t.Run(disposition, func(t *testing.T) {
			channel, server, socket := readyDispositionTransport(t, false)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			serverDone := make(chan error, 1)
			go func() {
				var previous string
				for range 2 {
					nonce, err := readyDispositionReceiveRequest(ctx, server)
					if err != nil {
						serverDone <- err
						return
					}
					if nonce == previous {
						serverDone <- fmt.Errorf("disposition retry reused nonce")
						return
					}
					previous = nonce
					sequence, digest := "null", "null"
					if disposition == "witnessed" {
						sequence, digest = "9", `"`+strings.Repeat("4", 64)+`"`
					}
					frame := readyDispositionResponse(nonce, disposition, sequence, digest)
					if _, err := unix.SendmsgN(socket, []byte(frame), nil, nil, 0); err != nil {
						serverDone <- err
						return
					}
				}
				serverDone <- nil
			}()
			for range 2 {
				result, err := channel.ReadyDisposition(ctx, readyDispositionRequest())
				if err != nil || result.Disposition != disposition || result.DecisionSequence != 12 ||
					result.DecisionDigest != strings.Repeat("3", 64) || !channel.Healthy() {
					t.Fatalf("valid disposition: %#v, %v", result, err)
				}
				if disposition == "witnessed" {
					if result.ReadyWitnessSequence == nil || *result.ReadyWitnessSequence != 9 ||
						result.ReadyWitnessDigest == nil || *result.ReadyWitnessDigest != strings.Repeat("4", 64) {
						t.Fatalf("wrong witness reference: %#v", result)
					}
				} else if result.ReadyWitnessSequence != nil || result.ReadyWitnessDigest != nil {
					t.Fatalf("retired disposition contains witness: %#v", result)
				}
			}
			if err := <-serverDone; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestLedgerReadyDispositionRejectsInvalidTransport(t *testing.T) {
	type testCase struct {
		name      string
		mutate    func(string, string) string
		fd        bool
		wrongPeer bool
	}
	replace := func(old, replacement string) func(string, string) string {
		return func(frame, _ string) string { return strings.Replace(frame, old, replacement, 1) }
	}
	cases := []testCase{
		{name: "wrong nonce", mutate: func(frame, nonce string) string { return strings.Replace(frame, nonce, strings.Repeat("5", 64), 1) }},
		{name: "wrong record", mutate: replace(strings.Repeat("2", 64), strings.Repeat("5", 64))},
		{name: "wrong generation", mutate: replace(`"phase_generation":7`, `"phase_generation":8`)},
		{name: "wrong recovery", mutate: replace(`"recovery_id":"recovery-test"`, `"recovery_id":"other"`)},
		{name: "unknown field", mutate: replace(`"version":1`, `"version":1,"extra":true`)},
		{name: "duplicate field", mutate: replace(`"version":1`, `"version":1,"version":1`)},
		{name: "missing field", mutate: replace(`,"decision_sequence":12`, "")},
		{name: "trailing data", mutate: func(frame, _ string) string { return frame + `{}` }},
		{name: "malformed JSON", mutate: func(frame, _ string) string { return frame[:len(frame)-1] }},
		{name: "null decision digest", mutate: replace(`"decision_digest":"`+strings.Repeat("3", 64)+`"`, `"decision_digest":null`)},
		{name: "zero decision sequence", mutate: replace(`"decision_sequence":12`, `"decision_sequence":0`)},
		{name: "boolean decision sequence", mutate: replace(`"decision_sequence":12`, `"decision_sequence":true`)},
		{name: "invalid disposition", mutate: replace(`"disposition":"witnessed"`, `"disposition":"unknown"`)},
		{name: "missing witness reference", mutate: replace(`"ready_witness_sequence":9`, `"ready_witness_sequence":null`)},
		{name: "null witness digest", mutate: replace(`"ready_witness_digest":"`+strings.Repeat("4", 64)+`"`, `"ready_witness_digest":null`)},
		{name: "zero witness", mutate: replace(`"ready_witness_sequence":9`, `"ready_witness_sequence":0`)},
		{name: "future witness", mutate: replace(`"ready_witness_sequence":9`, `"ready_witness_sequence":12`)},
		{name: "zero witness digest", mutate: replace(strings.Repeat("4", 64), strings.Repeat("0", 64))},
		{name: "unwitnessed with witness", mutate: replace(`"disposition":"witnessed"`, `"disposition":"unwitnessed_retired"`)},
		{name: "unwitnessed with only witness digest", mutate: func(frame, _ string) string {
			frame = strings.Replace(frame, `"disposition":"witnessed"`, `"disposition":"unwitnessed_retired"`, 1)
			return strings.Replace(frame, `"ready_witness_sequence":9`, `"ready_witness_sequence":null`, 1)
		}},
		{name: "unwitnessed with only witness sequence", mutate: func(frame, _ string) string {
			frame = strings.Replace(frame, `"disposition":"witnessed"`, `"disposition":"unwitnessed_retired"`, 1)
			return strings.Replace(frame, `"ready_witness_digest":"`+strings.Repeat("4", 64)+`"`, `"ready_witness_digest":null`, 1)
		}},
		{name: "received descriptor", fd: true},
		{name: "wrong peer credentials", wrongPeer: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			channel, server, socket := readyDispositionTransport(t, tc.wrongPeer)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			serverDone := make(chan error, 1)
			go func() {
				nonce, err := readyDispositionReceiveRequest(ctx, server)
				if err != nil {
					serverDone <- err
					return
				}
				frame := readyDispositionResponse(nonce, "witnessed", "9", `"`+strings.Repeat("4", 64)+`"`)
				if tc.mutate != nil {
					frame = tc.mutate(frame, nonce)
				}
				var rights []byte
				if tc.fd {
					fd, openErr := unix.Open("/dev/null", unix.O_RDONLY|unix.O_CLOEXEC, 0)
					if openErr != nil {
						serverDone <- openErr
						return
					}
					defer unix.Close(fd)
					rights = unix.UnixRights(fd)
				}
				_, err = unix.SendmsgN(socket, []byte(frame), rights, nil, 0)
				serverDone <- err
			}()
			if result, err := channel.ReadyDisposition(ctx, readyDispositionRequest()); err == nil {
				t.Fatalf("invalid response accepted: %#v", result)
			}
			if err := <-serverDone; err != nil {
				t.Fatal(err)
			}
			if channel.Healthy() {
				t.Fatal("failed exchange left channel healthy")
			}
			if _, err := channel.ReadyDisposition(ctx, readyDispositionRequest()); err == nil {
				t.Fatal("failed channel accepted a subsequent request")
			}
			var buffer [1024]byte
			if _, _, err := unix.Recvfrom(socket, buffer[:], unix.MSG_DONTWAIT); err != unix.EAGAIN {
				t.Fatalf("failed channel emitted a subsequent packet: %v", err)
			}
		})
	}
}
