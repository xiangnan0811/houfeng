package recordauthority

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"houfeng/internal/center/recordplatform"
)

// The files were captured from the unchanged V1 producer; their keys are synthetic.
func copyHistoricalComposeFixture(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "authority")
	for _, directory := range []struct {
		name string
		mode os.FileMode
	}{{root, 0o755}, {filepath.Join(root, "public"), 0o755}, {filepath.Join(root, "private"), 0o700}} {
		if err := os.Mkdir(directory.name, directory.mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(directory.name, directory.mode); err != nil {
			t.Fatal(err)
		}
	}
	files := []struct {
		name, digest string
		mode         os.FileMode
	}{
		{"public/deployment-id", "5233bb998a6c81a6305377cbeedbd3bc1e0df287fafe9aa62963c8fcdead7cb2", 0o644},
		{"private/authority-key", "f16bf5dce7ac28a5f02e3d542aa89fb9b09826ec63def337c003d5f8e86d245e", 0o600},
		{"private/database-secret", "7ae6898a6dbf0cdfd7ca6c114c3d38a1681b8811496caa30cc98b2288329d611", 0o600},
		{"private/activation-ledger.jsonl", "fd8379061b05feec3d23462e1d6d35aef3d6aa0809c2626af927898a7674c834", 0o600},
		{"private/activation-receipt.json", "0912c33c05d3baa7cceed962b542b8cf4dfd2f12c80cb1fb979d5fc0e7012fde", 0o600},
	}
	for _, file := range files {
		body, err := os.ReadFile(filepath.Join("testdata", "legacy-v1", "state", file.name))
		if err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(body)
		if hex.EncodeToString(hash[:]) != file.digest {
			t.Fatalf("captured V1 file %s changed", file.name)
		}
		path := filepath.Join(root, file.name)
		if err := os.WriteFile(path, body, file.mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, file.mode); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestHistoricalV1UpgradePreservesGenesisAndSignsExactSuccessor(t *testing.T) {
	root := copyHistoricalComposeFixture(t)
	before, err := snapshotComposeStateFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	receiptBefore, err := os.ReadFile(filepath.Join(root, "private", "activation-receipt.json"))
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := LoadComposeState(root)
	if err != nil {
		t.Fatal(err)
	}
	if legacy.MembershipVersion != 1 || legacy.MembershipRotation != nil || len(legacy.Memberships) != 1 {
		t.Fatalf("historical state: %#v", legacy)
	}
	if err := VerifyActivationReceipt(root, legacy); err != nil {
		t.Fatal(err)
	}
	activation, err := legacy.ActivationCommand.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	captured, err := os.ReadFile(filepath.Join("testdata", "legacy-v1", "activation-command.bin"))
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(captured)
	if hex.EncodeToString(hash[:]) != "2c95851131fa38c49760e524f62ae287e43c2f8a7f50c4679b4c1b4d7ac2bdef" || !bytes.Equal(activation, captured) {
		t.Fatal("historical activation command changed")
	}

	current, err := PrepareComposeMembershipV2(root)
	if err != nil {
		t.Fatal(err)
	}
	if current.MembershipVersion != 2 || current.MembershipRotation == nil || len(current.Memberships) != 2 || current.Memberships[1].InstanceID != "compose-processor" || current.Memberships[1].InstanceKind != "worker" || !current.Memberships[1].QueueAdmitted {
		t.Fatalf("current membership: %#v", current.Memberships)
	}
	if current.ActivationCommand != legacy.ActivationCommand || current.DatabasePassword() != legacy.DatabasePassword() {
		t.Fatal("genesis changed during upgrade")
	}
	if err := VerifyActivationReceipt(root, current); err != nil {
		t.Fatalf("historical receipt after upgrade: %v", err)
	}
	after, err := snapshotComposeStateFiles(root)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("upgrade changed historical state files")
	}
	receiptAfter, err := os.ReadFile(filepath.Join(root, "private", "activation-receipt.json"))
	if err != nil || !bytes.Equal(receiptBefore, receiptAfter) {
		t.Fatal("upgrade changed historical receipt")
	}

	path := filepath.Join(root, "private", "membership-successor.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	assertFileMode(t, path, 0o600)
	var successor signedMembershipSuccessorV2
	if err := json.Unmarshal(raw, &successor); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(successor.Body)
	if err != nil {
		t.Fatal(err)
	}
	entryHash := testComposeDigestV1("HOUFENG-COMPOSE-RECORDS-AUTHORITY-MEMBERSHIP-LEDGER-ENTRY-V2", body)
	publicKey, err := base64.RawStdEncoding.DecodeString(successor.Body.AuthorityPublicKey)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := base64.RawStdEncoding.DecodeString(successor.Signature)
	if err != nil || successor.EntryHash != hex.EncodeToString(entryHash[:]) || !ed25519.Verify(publicKey, entryHash[:], signature) {
		t.Fatal("successor signature or framed digest is invalid")
	}
	if successor.Body.PreviousHash != hex.EncodeToString(legacy.ActivationCommand.WitnessedLedgerHash[:]) || successor.Body.BaseActivationBundleDigest != hex.EncodeToString(legacy.ActivationCommand.ActivationBundleDigest[:]) {
		t.Fatal("successor does not bind historical genesis")
	}
	rotation := current.MembershipRotation
	if rotation.ExpectedWitnessedLedgerHash != legacy.ActivationCommand.WitnessedLedgerHash || rotation.NextWitnessedLedgerHash != entryHash || rotation.NextIdentitySetEpoch != 2 || rotation.NextTrustRevision != 2 {
		t.Fatal("rotation chain mismatch")
	}
	identityJSON := `{"version":2,"deployment_id":"` + string(legacy.DeploymentID) + `","project_id":"default","memberships":[{"instance_id":"compose-center","instance_kind":"api","capability":"records.runtime","deployment_epoch":2,"fence_contract_version":1,"load_balancer_admitted":true,"queue_admitted":false},{"instance_id":"compose-processor","instance_kind":"worker","capability":"records.runtime","deployment_epoch":2,"fence_contract_version":1,"load_balancer_admitted":false,"queue_admitted":true}]}`
	identity := testComposeDigestV1("HOUFENG-COMPOSE-RECORDS-AUTHORITY-IDENTITY-SET-V2", []byte(identityJSON))
	mutationInput := append([]byte(nil), legacy.ActivationCommand.WitnessedLedgerHash[:]...)
	mutationInput = append(mutationInput, identity[:]...)
	mutationInput = append(mutationInput, legacy.ActivationCommand.ActivationBundleDigest[:]...)
	mutation := testComposeDigestV1("HOUFENG-COMPOSE-RECORDS-AUTHORITY-MEMBERSHIP-MUTATION-V2", mutationInput)
	trustInput := append([]byte(nil), legacy.ActivationCommand.TrustHeadHash[:]...)
	trustInput = append(trustInput, entryHash[:]...)
	trustInput = append(trustInput, publicKey...)
	trust := testComposeDigestV1("HOUFENG-COMPOSE-RECORDS-AUTHORITY-TRUST-HEAD-V2", trustInput)
	if rotation.NextIdentitySetDigest != identity || rotation.RotationMutationID != "tm-"+hex.EncodeToString(mutation[:]) || rotation.NextTrustHeadHash != trust {
		t.Fatal("rotation digest derivation differs from contract")
	}
	firstInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := PrepareComposeMembershipV2(root)
	if err != nil || !reflect.DeepEqual(current, retry) {
		t.Fatalf("retry: %v", err)
	}
	retryInfo, err := os.Stat(path)
	if err != nil || !os.SameFile(firstInfo, retryInfo) {
		t.Fatal("retry replaced successor")
	}
}

func TestPreviewMembershipV2IsReadOnlyAndMatchesPublication(t *testing.T) {
	root := copyHistoricalComposeFixture(t)
	type fileSnapshot struct {
		mode os.FileMode
		body string
	}
	snapshot := func() map[string]fileSnapshot {
		t.Helper()
		files := make(map[string]fileSnapshot)
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			var body []byte
			if info.Mode().IsRegular() {
				body, err = os.ReadFile(path)
				if err != nil {
					return err
				}
			}
			files[path] = fileSnapshot{mode: info.Mode(), body: string(body)}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return files
	}
	before := snapshot()
	preview, err := PreviewComposeMembershipV2(root)
	if err != nil {
		t.Fatal(err)
	}
	if preview.MembershipVersion != 2 || preview.MembershipRotation == nil {
		t.Fatal("preview did not derive the V2 rotation")
	}
	if !reflect.DeepEqual(before, snapshot()) {
		t.Fatal("preview changed authority files or directory modes")
	}
	if err := VerifyMembershipRotationReceipt(root, preview); !errors.Is(err, ErrComposeMembershipRotationReceiptInvalid) {
		t.Fatalf("unpublished preview accepted as durable: %v", err)
	}
	previewActivation, err := preview.ActivationCommand.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	previewRotation, err := preview.MembershipRotation.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	receipt := recordplatform.ProjectionCASReceiptDigestV1(previewRotation)
	if err := PersistMembershipRotationReceipt(root, preview, receipt[:]); !errors.Is(err, ErrComposeMembershipRotationReceiptInvalid) {
		t.Fatalf("unpublished preview accepted for receipt: %v", err)
	}
	prepared, err := PrepareComposeMembershipV2(root)
	if err != nil {
		t.Fatal(err)
	}
	activation, err := prepared.ActivationCommand.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	rotation, err := prepared.MembershipRotation.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(previewActivation, activation) || !bytes.Equal(previewRotation, rotation) || !reflect.DeepEqual(preview, prepared) {
		t.Fatal("published state differs from preview")
	}
	before = snapshot()
	second, err := PreviewComposeMembershipV2(root)
	if err != nil || !reflect.DeepEqual(second, prepared) || !reflect.DeepEqual(before, snapshot()) {
		t.Fatalf("preview of existing successor: %v", err)
	}
}

func TestPreviewMembershipV2RejectsCorruptPrivateReceipts(t *testing.T) {
	t.Run("activation", func(t *testing.T) {
		root := copyHistoricalComposeFixture(t)
		path := filepath.Join(root, "private", "activation-receipt.json")
		if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := PreviewComposeMembershipV2(root); !errors.Is(err, ErrComposeActivationReceiptInvalid) {
			t.Fatalf("corrupt activation receipt: %v", err)
		}
	})
	t.Run("rotation", func(t *testing.T) {
		root := copyHistoricalComposeFixture(t)
		if _, err := PrepareComposeMembershipV2(root); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, "private", "membership-rotation-receipt.json")
		if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := PreviewComposeMembershipV2(root); !errors.Is(err, ErrComposeMembershipRotationReceiptInvalid) {
			t.Fatalf("corrupt rotation receipt: %v", err)
		}
	})
}

func TestMembershipPrepareRejectsReceiptWithoutSuccessor(t *testing.T) {
	root := copyHistoricalComposeFixture(t)
	path := filepath.Join(root, "private", "membership-rotation-receipt.json")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareComposeMembershipV2(root); !errors.Is(err, ErrComposeStateInvalid) {
		t.Fatalf("orphan receipt: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "private", "membership-successor.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("successor unexpectedly published: %v", err)
	}
}

func TestMembershipSuccessorRejectsCorruptionWithoutFallback(t *testing.T) {
	cases := map[string]func([]byte) []byte{
		"truncated":   func(b []byte) []byte { return b[:len(b)-2] },
		"extra event": func(b []byte) []byte { return append(append([]byte(nil), b...), b...) },
		"duplicate field": func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"version":2`), []byte(`"version":2,"version":2`), 1)
		},
		"noncanonical": func(b []byte) []byte { return append([]byte(" "), b...) },
		"wrong chain": func(b []byte) []byte {
			var e signedMembershipSuccessorV2
			if json.Unmarshal(b, &e) != nil {
				panic("invalid fixture")
			}
			e.Body.PreviousHash = string(bytes.Repeat([]byte("0"), 64))
			out, _ := json.Marshal(e)
			return append(out, '\n')
		},
		"signature": func(b []byte) []byte {
			var e signedMembershipSuccessorV2
			if json.Unmarshal(b, &e) != nil {
				panic("invalid fixture")
			}
			e.Signature = base64.RawStdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))
			out, _ := json.Marshal(e)
			return append(out, '\n')
		},
		"wrong members": func(b []byte) []byte {
			var e signedMembershipSuccessorV2
			if json.Unmarshal(b, &e) != nil {
				panic("invalid fixture")
			}
			e.Body.Memberships[0], e.Body.Memberships[1] = e.Body.Memberships[1], e.Body.Memberships[0]
			out, _ := json.Marshal(e)
			return append(out, '\n')
		},
	}
	for name, tamper := range cases {
		t.Run(name, func(t *testing.T) {
			root := copyHistoricalComposeFixture(t)
			if _, err := PrepareComposeMembershipV2(root); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "private", "membership-successor.json")
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			corrupt := tamper(body)
			if err := os.WriteFile(path, corrupt, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadComposeState(root); !errors.Is(err, ErrComposeStateInvalid) {
				t.Fatalf("load: %v", err)
			}
			if _, err := PreviewComposeMembershipV2(root); !errors.Is(err, ErrComposeStateInvalid) {
				t.Fatalf("preview: %v", err)
			}
			if _, err := PrepareComposeMembershipV2(root); !errors.Is(err, ErrComposeStateInvalid) {
				t.Fatalf("prepare: %v", err)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(after, corrupt) {
				t.Fatal("corrupt successor replaced")
			}
		})
	}
}

func TestMembershipRotationReceiptRequiresExactCurrentStateAndIsImmutable(t *testing.T) {
	root := copyHistoricalComposeFixture(t)
	state, err := PrepareComposeMembershipV2(root)
	if err != nil {
		t.Fatal(err)
	}
	command, err := state.MembershipRotation.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	receipt := recordplatform.ProjectionCASReceiptDigestV1(command)
	if err := VerifyMembershipRotationReceipt(root, state); !errors.Is(err, ErrComposeMembershipRotationReceiptAbsent) {
		t.Fatalf("absent: %v", err)
	}
	bad := receipt
	bad[0] ^= 1
	if err := PersistMembershipRotationReceipt(root, state, bad[:]); !errors.Is(err, ErrComposeMembershipRotationReceiptInvalid) {
		t.Fatalf("wrong receipt: %v", err)
	}
	drift := state
	drift.Memberships = append([]ComposeMembership(nil), state.Memberships...)
	drift.Memberships[1].QueueAdmitted = false
	if err := PersistMembershipRotationReceipt(root, drift, receipt[:]); !errors.Is(err, ErrComposeMembershipRotationReceiptInvalid) {
		t.Fatalf("drift: %v", err)
	}
	drift = state
	rot := *state.MembershipRotation
	rot.NextWitnessedLedgerHash[0] ^= 1
	drift.MembershipRotation = &rot
	if err := PersistMembershipRotationReceipt(root, drift, receipt[:]); !errors.Is(err, ErrComposeMembershipRotationReceiptInvalid) {
		t.Fatalf("rotation drift: %v", err)
	}
	if err := PersistMembershipRotationReceipt(root, state, receipt[:]); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "private", "membership-rotation-receipt.json")
	assertFileMode(t, path, 0o600)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := PersistMembershipRotationReceipt(root, state, receipt[:]); err != nil {
		t.Fatal(err)
	}
	retry, err := os.Stat(path)
	if err != nil || !os.SameFile(info, retry) {
		t.Fatal("receipt retry replaced file")
	}
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyMembershipRotationReceipt(root, state); !errors.Is(err, ErrComposeMembershipRotationReceiptInvalid) {
		t.Fatalf("corrupt receipt: %v", err)
	}
	if err := PersistMembershipRotationReceipt(root, state, receipt[:]); !errors.Is(err, ErrComposeMembershipRotationReceiptInvalid) {
		t.Fatalf("corrupt retry: %v", err)
	}
}

func TestMembershipHeartbeatV2ExactWireAndRejectsDrift(t *testing.T) {
	root := copyHistoricalComposeFixture(t)
	state, err := PrepareComposeMembershipV2(root)
	if err != nil {
		t.Fatal(err)
	}
	issued := time.Unix(1_724_500_000, 987654321)
	raw, expires, err := MarshalMembershipHeartbeatCommandV2(state, issued)
	if err != nil {
		t.Fatal(err)
	}
	r := state.MembershipRotation
	if len(raw) != 240 || string(raw[:33]) != "HOUFENG-APP-PROJECTION-COMMAND-V1" || !bytes.Equal(raw[33:37], []byte{0, 1, 4, 9}) || string(raw[37:104]) != string(state.DeploymentID) || !bytes.Equal(raw[104:136], state.ActivationCommand.ActivationBundleDigest[:]) || binary.BigEndian.Uint64(raw[136:144]) != 2 || !bytes.Equal(raw[144:176], r.NextWitnessedLedgerHash[:]) || binary.BigEndian.Uint64(raw[176:184]) != 2 || !bytes.Equal(raw[184:216], r.NextIdentitySetDigest[:]) || binary.BigEndian.Uint64(raw[216:224]) != 1 || binary.BigEndian.Uint64(raw[224:232]) != uint64(issued.Unix()) || binary.BigEndian.Uint64(raw[232:240]) != uint64(issued.Unix()+90) || !expires.Equal(time.Unix(issued.Unix()+90, 0)) {
		t.Fatal("V2 heartbeat wire differs from contract")
	}
	if bytes.Contains(raw, []byte(state.DatabasePassword())) {
		t.Fatal("heartbeat contains credential")
	}
	for name, mutate := range map[string]func(*VerifiedComposeState){
		"genesis": func(s *VerifiedComposeState) { s.ActivationCommand.ActivationBundleDigest[0] ^= 1 },
		"rotation": func(s *VerifiedComposeState) {
			r := *s.MembershipRotation
			r.NextIdentitySetDigest[0] ^= 1
			s.MembershipRotation = &r
		},
		"member": func(s *VerifiedComposeState) {
			s.Memberships = append([]ComposeMembership(nil), s.Memberships...)
			s.Memberships[1].QueueAdmitted = false
		},
		"version": func(s *VerifiedComposeState) { s.MembershipVersion = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			drift := state
			mutate(&drift)
			if _, _, err := MarshalMembershipHeartbeatCommandV2(drift, issued); !errors.Is(err, ErrComposeHeartbeatInvalid) {
				t.Fatalf("drift: %v", err)
			}
		})
	}
	for _, invalid := range []time.Time{time.Time{}, time.Unix(0, 0), time.Unix(math.MaxInt64, 0)} {
		if _, _, err := MarshalMembershipHeartbeatCommandV2(state, invalid); !errors.Is(err, ErrComposeHeartbeatInvalid) {
			t.Fatalf("time: %v", err)
		}
	}
	if _, _, err := MarshalMembershipHeartbeatCommandV1(state, issued); !errors.Is(err, ErrComposeHeartbeatInvalid) {
		t.Fatalf("V1 codec accepted V2 state: %v", err)
	}
}
