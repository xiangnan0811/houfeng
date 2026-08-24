package recordauthority

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"houfeng/internal/center/recordplatform"
)

const (
	testLedgerEntryDomainV1        = "HOUFENG-COMPOSE-RECORDS-AUTHORITY-LEDGER-ENTRY-V1"
	testDatabaseCredentialDomainV1 = "HOUFENG-COMPOSE-RECORDS-AUTHORITY-DATABASE-CREDENTIAL-V1"
)

type testActivationMembershipV1 struct {
	InstanceID           string `json:"instance_id"`
	InstanceKind         string `json:"instance_kind"`
	Capability           string `json:"capability"`
	DeploymentEpoch      uint64 `json:"deployment_epoch"`
	FenceContractVersion uint64 `json:"fence_contract_version"`
	LoadBalancerAdmitted bool   `json:"load_balancer_admitted"`
	QueueAdmitted        bool   `json:"queue_admitted"`
}

type testActivationLedgerBodyV1 struct {
	Version                  uint8                        `json:"version"`
	Sequence                 uint64                       `json:"sequence"`
	PreviousHash             string                       `json:"previous_hash"`
	Event                    string                       `json:"event"`
	DeploymentID             string                       `json:"deployment_id"`
	ProjectID                string                       `json:"project_id"`
	Profile                  string                       `json:"profile"`
	ActivationMutationID     string                       `json:"activation_mutation_id"`
	AuthorityPublicKey       string                       `json:"authority_public_key"`
	DatabaseCredentialDigest string                       `json:"database_credential_digest"`
	Memberships              []testActivationMembershipV1 `json:"memberships"`
}

type testSignedActivationLedgerEntryV1 struct {
	Body      testActivationLedgerBodyV1 `json:"body"`
	EntryHash string                     `json:"entry_hash"`
	Signature string                     `json:"signature"`
}

func TestCreateComposeStateAtomicallyPersistsStableSignedActivationProof(t *testing.T) {
	root := filepath.Join(t.TempDir(), "records-authority")
	random := append(bytes.Repeat([]byte{0x11}, 32), bytes.Repeat([]byte{0x22}, 32)...)
	random = append(random, bytes.Repeat([]byte{0x33}, 32)...)

	created, err := createComposeState(root, bytes.NewReader(random))
	if err != nil {
		t.Fatalf("createComposeState() error = %v", err)
	}
	loaded, err := LoadComposeState(root)
	if err != nil {
		t.Fatalf("LoadComposeState() error = %v", err)
	}
	if !reflect.DeepEqual(loaded, created) {
		t.Fatal("reloaded state differs from created state")
	}

	wantDeploymentID := recordplatform.DeploymentID("dp-" + strings.Repeat("11", 32))
	if loaded.DeploymentID != wantDeploymentID {
		t.Fatalf("DeploymentID = %q, want %q", loaded.DeploymentID, wantDeploymentID)
	}
	if loaded.DatabasePassword() != strings.Repeat("33", 32) {
		t.Fatalf("DatabasePassword length/value does not match the independent generated credential")
	}
	if loaded.ActivationCommand.DeploymentID != string(wantDeploymentID) ||
		loaded.ActivationCommand.ActiveProfile != recordplatform.ProjectionProfilePostgresSync ||
		loaded.ActivationCommand.WitnessedLedgerSequence != 1 ||
		loaded.ActivationCommand.TrustRevision != 1 ||
		loaded.ActivationCommand.AdapterPolicyGeneration != 1 ||
		loaded.ActivationCommand.IdentitySetEpoch != 1 ||
		loaded.ActivationCommand.MinimumFenceContractVersion != 1 {
		t.Fatalf("derived activation command = %#v", loaded.ActivationCommand)
	}
	commandBytes, err := loaded.ActivationCommand.MarshalBinary()
	if err != nil {
		t.Fatalf("derived activation command MarshalBinary() error = %v", err)
	}
	if _, err := recordplatform.ParseContractActivationProjectionCommandV1(commandBytes); err != nil {
		t.Fatalf("derived activation command is not accepted by the existing codec: %v", err)
	}

	assertFileMode(t, root, 0o755)
	assertFileMode(t, filepath.Join(root, "public"), 0o755)
	assertFileMode(t, filepath.Join(root, "public", "deployment-id"), 0o644)
	assertFileMode(t, filepath.Join(root, "private"), 0o700)
	for _, name := range []string{"authority-key", "database-secret", "activation-ledger.jsonl"} {
		assertFileMode(t, filepath.Join(root, "private", name), 0o600)
	}

	deploymentIDFile, err := os.ReadFile(filepath.Join(root, "public", "deployment-id"))
	if err != nil {
		t.Fatalf("read deployment-id: %v", err)
	}
	if string(deploymentIDFile) != string(wantDeploymentID)+"\n" {
		t.Fatalf("deployment-id = %q, want canonical ID plus newline", deploymentIDFile)
	}
	databaseSecretFile, err := os.ReadFile(filepath.Join(root, "private", "database-secret"))
	if err != nil {
		t.Fatalf("read database-secret: %v", err)
	}
	if string(databaseSecretFile) != strings.Repeat("33", 32)+"\n" {
		t.Fatal("database-secret does not contain the canonical independent credential")
	}

	keyFile, err := os.ReadFile(filepath.Join(root, "private", "authority-key"))
	if err != nil {
		t.Fatalf("read authority-key: %v", err)
	}
	block, remainder := pem.Decode(keyFile)
	if block == nil || block.Type != "PRIVATE KEY" || len(remainder) != 0 {
		t.Fatalf("authority-key is not one canonical PKCS#8 PEM block")
	}
	parsedKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		t.Fatalf("parse authority-key: %v", err)
	}
	privateKey, ok := parsedKey.(ed25519.PrivateKey)
	if !ok {
		t.Fatalf("authority-key type = %T, want Ed25519", parsedKey)
	}
	wantPrivateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x22}, ed25519.SeedSize))
	if !bytes.Equal(privateKey, wantPrivateKey) {
		t.Fatal("authority-key was not generated from the independent key material")
	}

	ledger, err := os.ReadFile(filepath.Join(root, "private", "activation-ledger.jsonl"))
	if err != nil {
		t.Fatalf("read activation ledger: %v", err)
	}
	if bytes.Count(ledger, []byte{'\n'}) != 1 || !bytes.HasSuffix(ledger, []byte{'\n'}) || len(ledger) > 16*1024 {
		t.Fatalf("activation ledger is not one bounded canonical JSONL entry: length=%d", len(ledger))
	}
	for _, forbidden := range []string{
		"plan_digest", "authorization_artifact_digest", "activation_bundle_digest",
		"trust_head_hash", "inventory_digest", "approval_policy_digest",
		"adapter_policy_digest", "drain_receipt_digest", "identity_set_digest",
	} {
		if bytes.Contains(ledger, []byte(forbidden)) {
			t.Fatalf("activation ledger persists caller-trusted projection field %q", forbidden)
		}
	}
	var entry testSignedActivationLedgerEntryV1
	if err := json.Unmarshal(bytes.TrimSuffix(ledger, []byte{'\n'}), &entry); err != nil {
		t.Fatalf("decode activation ledger: %v", err)
	}
	wantMembership := []testActivationMembershipV1{{
		InstanceID:           "compose-center",
		InstanceKind:         "api",
		Capability:           "records.runtime",
		DeploymentEpoch:      1,
		FenceContractVersion: 1,
		LoadBalancerAdmitted: true,
		QueueAdmitted:        false,
	}}
	if entry.Body.Version != 1 || entry.Body.Sequence != 1 ||
		entry.Body.PreviousHash != strings.Repeat("0", sha256.Size*2) ||
		entry.Body.Event != "compose_activation" || entry.Body.DeploymentID != string(wantDeploymentID) ||
		entry.Body.ProjectID != "default" || entry.Body.Profile != "postgres_sync" ||
		!reflect.DeepEqual(entry.Body.Memberships, wantMembership) {
		t.Fatalf("activation ledger body = %#v", entry.Body)
	}
	wantPublicKey := wantPrivateKey.Public().(ed25519.PublicKey)
	if entry.Body.AuthorityPublicKey != base64.RawStdEncoding.EncodeToString(wantPublicKey) {
		t.Fatal("activation ledger public key does not match authority-key")
	}
	wantCredentialDigest := testComposeDigestV1(testDatabaseCredentialDomainV1, []byte(strings.Repeat("33", 32)))
	if entry.Body.DatabaseCredentialDigest != hex.EncodeToString(wantCredentialDigest[:]) {
		t.Fatal("activation ledger does not bind the generated database credential")
	}
	body, err := json.Marshal(entry.Body)
	if err != nil {
		t.Fatalf("marshal activation ledger body: %v", err)
	}
	preimage := append([]byte(testLedgerEntryDomainV1), 0)
	var bodyLength [4]byte
	binary.BigEndian.PutUint32(bodyLength[:], uint32(len(body)))
	preimage = append(preimage, bodyLength[:]...)
	preimage = append(preimage, body...)
	wantHash := sha256.Sum256(preimage)
	if entry.EntryHash != hex.EncodeToString(wantHash[:]) {
		t.Fatalf("entry hash = %q, want %x", entry.EntryHash, wantHash)
	}
	signature, err := base64.RawStdEncoding.DecodeString(entry.Signature)
	if err != nil || !ed25519.Verify(wantPublicKey, wantHash[:], signature) {
		t.Fatal("activation ledger signature is not a valid Ed25519 signature over its entry hash")
	}

	before, err := snapshotComposeStateFiles(root)
	if err != nil {
		t.Fatalf("snapshot first state: %v", err)
	}
	if _, err := createComposeState(root, bytes.NewReader(bytes.Repeat([]byte{0xff}, 96))); !errors.Is(err, ErrComposeStateExists) {
		t.Fatalf("createComposeState() exact repeat error = %v, want ErrComposeStateExists", err)
	}
	after, err := snapshotComposeStateFiles(root)
	if err != nil {
		t.Fatalf("snapshot repeated state: %v", err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatal("repeated creation mutated durable authority state")
	}
}

func TestCreateComposeStateRandomFailureLeavesNoPartialState(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "records-authority")

	if _, err := createComposeState(root, bytes.NewReader(bytes.Repeat([]byte{0x11}, 32))); err == nil {
		t.Fatal("createComposeState() error = nil, want random-source failure")
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("authority root exists after failed atomic generation: %v", err)
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatalf("ReadDir(parent): %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed generation left temporary state: %#v", entries)
	}
}

func TestLoadComposeStateRejectsIncompleteAndHostileStateWithoutLeakingMaterial(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, root string)
	}{
		{
			name: "missing private file is corrupt rather than absent",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				if err := os.Remove(filepath.Join(root, "private", "authority-key")); err != nil {
					t.Fatalf("remove authority key: %v", err)
				}
			},
		},
		{
			name: "truncated ledger",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				path := filepath.Join(root, "private", "activation-ledger.jsonl")
				body, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("read ledger: %v", err)
				}
				if err := os.WriteFile(path, body[:len(body)/2], 0o600); err != nil {
					t.Fatalf("truncate ledger: %v", err)
				}
			},
		},
		{
			name: "extra ledger entry",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				path := filepath.Join(root, "private", "activation-ledger.jsonl")
				body, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("read ledger: %v", err)
				}
				if err := os.WriteFile(path, append(body, body...), 0o600); err != nil {
					t.Fatalf("extend ledger: %v", err)
				}
			},
		},
		{
			name: "mismatched public deployment identity",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				body := []byte("dp-" + strings.Repeat("aa", 32) + "\n")
				if err := os.WriteFile(filepath.Join(root, "public", "deployment-id"), body, 0o644); err != nil {
					t.Fatalf("replace deployment ID: %v", err)
				}
			},
		},
		{
			name: "different canonical database credential",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				body := []byte(strings.Repeat("44", 32) + "\n")
				if err := os.WriteFile(filepath.Join(root, "private", "database-secret"), body, 0o600); err != nil {
					t.Fatalf("replace database credential: %v", err)
				}
			},
		},
		{
			name: "noncanonical database credential",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				body := []byte(strings.Repeat("A", 64) + "\n")
				if err := os.WriteFile(filepath.Join(root, "private", "database-secret"), body, 0o600); err != nil {
					t.Fatalf("replace database credential: %v", err)
				}
			},
		},
		{
			name: "private file mode widened",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				if err := os.Chmod(filepath.Join(root, "private", "database-secret"), 0o640); err != nil {
					t.Fatalf("widen database credential mode: %v", err)
				}
			},
		},
		{
			name: "ledger symlink",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				path := filepath.Join(root, "private", "activation-ledger.jsonl")
				if err := os.Remove(path); err != nil {
					t.Fatalf("remove ledger: %v", err)
				}
				if err := os.Symlink(filepath.Join(root, "private", "database-secret"), path); err != nil {
					t.Fatalf("symlink ledger: %v", err)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "records-authority")
			random := append(bytes.Repeat([]byte{0x11}, 32), bytes.Repeat([]byte{0x22}, 32)...)
			random = append(random, bytes.Repeat([]byte{0x33}, 32)...)
			if _, err := createComposeState(root, bytes.NewReader(random)); err != nil {
				t.Fatalf("createComposeState(): %v", err)
			}
			tc.mutate(t, root)

			_, err := LoadComposeState(root)
			if !errors.Is(err, ErrComposeStateInvalid) {
				t.Fatalf("LoadComposeState() error = %v, want ErrComposeStateInvalid", err)
			}
			message := err.Error()
			for _, secret := range []string{
				strings.Repeat("33", 32), strings.Repeat("22", 32),
				"dp-" + strings.Repeat("11", 32), root,
			} {
				if strings.Contains(message, secret) {
					t.Fatalf("LoadComposeState() error leaks durable material/path: %q", message)
				}
			}
		})
	}

	absentRoot := filepath.Join(t.TempDir(), "absent")
	if _, err := LoadComposeState(absentRoot); !errors.Is(err, ErrComposeStateAbsent) {
		t.Fatalf("LoadComposeState(absent) error = %v, want ErrComposeStateAbsent", err)
	}
}

func testComposeDigestV1(domain string, payload []byte) [sha256.Size]byte {
	preimage := append([]byte(domain), 0)
	var payloadLength [4]byte
	binary.BigEndian.PutUint32(payloadLength[:], uint32(len(payload)))
	preimage = append(preimage, payloadLength[:]...)
	preimage = append(preimage, payload...)
	return sha256.Sum256(preimage)
}

func TestPersistActivationReceiptIsExactAtomicAndNonMutating(t *testing.T) {
	root := filepath.Join(t.TempDir(), "records-authority")
	random := append(bytes.Repeat([]byte{0x11}, 32), bytes.Repeat([]byte{0x22}, 32)...)
	random = append(random, bytes.Repeat([]byte{0x33}, 32)...)
	state, err := createComposeState(root, bytes.NewReader(random))
	if err != nil {
		t.Fatalf("createComposeState(): %v", err)
	}
	command, err := state.ActivationCommand.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary(): %v", err)
	}
	receipt := recordplatform.ProjectionCASReceiptDigestV1(command)
	receiptPath := filepath.Join(root, "private", "activation-receipt.json")

	if err := VerifyActivationReceipt(root, state); !errors.Is(err, ErrComposeActivationReceiptAbsent) {
		t.Fatalf("VerifyActivationReceipt() missing error = %v", err)
	}
	wrong := receipt
	wrong[0] ^= 0xff
	if err := PersistActivationReceipt(root, state, wrong[:]); !errors.Is(err, ErrComposeActivationReceiptInvalid) {
		t.Fatalf("PersistActivationReceipt(wrong) error = %v", err)
	}
	if _, err := os.Stat(receiptPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("wrong receipt created a file: %v", err)
	}

	if err := PersistActivationReceipt(root, state, receipt[:]); err != nil {
		t.Fatalf("PersistActivationReceipt() error = %v", err)
	}
	if err := VerifyActivationReceipt(root, state); err != nil {
		t.Fatalf("VerifyActivationReceipt() error = %v", err)
	}
	assertFileMode(t, receiptPath, 0o600)
	commandDigest := sha256.Sum256(command)
	wantReceipt := []byte(`{"version":1,"projection_command_digest":"` + hex.EncodeToString(commandDigest[:]) + `","cas_receipt":"` + hex.EncodeToString(receipt[:]) + `"}` + "\n")
	gotReceipt, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatalf("read receipt: %v", err)
	}
	if !bytes.Equal(gotReceipt, wantReceipt) {
		t.Fatalf("receipt file is not canonical: %q", gotReceipt)
	}
	beforeInfo, err := os.Stat(receiptPath)
	if err != nil {
		t.Fatalf("stat receipt: %v", err)
	}
	if err := PersistActivationReceipt(root, state, receipt[:]); err != nil {
		t.Fatalf("PersistActivationReceipt() repeat error = %v", err)
	}
	afterInfo, err := os.Stat(receiptPath)
	if err != nil {
		t.Fatalf("stat repeated receipt: %v", err)
	}
	if !afterInfo.ModTime().Equal(beforeInfo.ModTime()) {
		t.Fatal("exact receipt repeat rewrote the durable receipt")
	}

	corrupt := append([]byte(nil), gotReceipt...)
	corrupt[len(corrupt)-3] ^= 1
	if err := os.WriteFile(receiptPath, corrupt, 0o600); err != nil {
		t.Fatalf("corrupt receipt: %v", err)
	}
	if err := VerifyActivationReceipt(root, state); !errors.Is(err, ErrComposeActivationReceiptInvalid) {
		t.Fatalf("VerifyActivationReceipt(corrupt) error = %v", err)
	}
	if err := PersistActivationReceipt(root, state, receipt[:]); !errors.Is(err, ErrComposeActivationReceiptInvalid) {
		t.Fatalf("PersistActivationReceipt(existing corrupt) error = %v", err)
	}
	afterCorrupt, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatalf("read corrupt receipt: %v", err)
	}
	if !bytes.Equal(afterCorrupt, corrupt) {
		t.Fatal("persist overwrote an existing corrupt receipt")
	}
}

func assertFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", filepath.Base(path), err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("mode %s = %04o, want %04o", filepath.Base(path), got, want)
	}
}

func snapshotComposeStateFiles(root string) (map[string][]byte, error) {
	result := make(map[string][]byte)
	for _, name := range []string{
		filepath.Join("public", "deployment-id"),
		filepath.Join("private", "authority-key"),
		filepath.Join("private", "database-secret"),
		filepath.Join("private", "activation-ledger.jsonl"),
	} {
		body, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			return nil, err
		}
		result[name] = body
	}
	return result, nil
}
