package recordauthority

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"houfeng/internal/center/recordplatform"
)

const (
	composeStatePublicDirectory       = "public"
	composeStatePrivateDirectory      = "private"
	composeDeploymentIDFilename       = "deployment-id"
	composeAuthorityKeyFilename       = "authority-key"
	composeDatabaseSecretFilename     = "database-secret"
	composeActivationLedgerFilename   = "activation-ledger.jsonl"
	composeActivationReceiptFilename  = "activation-receipt.json"
	composeLedgerEntryDomainV1        = "HOUFENG-COMPOSE-RECORDS-AUTHORITY-LEDGER-ENTRY-V1"
	composeDatabaseCredentialDomainV1 = "HOUFENG-COMPOSE-RECORDS-AUTHORITY-DATABASE-CREDENTIAL-V1"
	composeActivationMutationDomainV1 = "HOUFENG-COMPOSE-RECORDS-AUTHORITY-ACTIVATION-MUTATION-V1"
	composePlanDomainV1               = "HOUFENG-COMPOSE-RECORDS-AUTHORITY-PLAN-V1"
	composeAuthorizationDomainV1      = "HOUFENG-COMPOSE-RECORDS-AUTHORITY-AUTHORIZATION-V1"
	composeActivationBundleDomainV1   = "HOUFENG-COMPOSE-RECORDS-AUTHORITY-ACTIVATION-BUNDLE-V1"
	composeTrustHeadDomainV1          = "HOUFENG-COMPOSE-RECORDS-AUTHORITY-TRUST-HEAD-V1"
	composeInventoryDomainV1          = "HOUFENG-COMPOSE-RECORDS-AUTHORITY-INVENTORY-V1"
	composeApprovalPolicyDomainV1     = "HOUFENG-COMPOSE-RECORDS-AUTHORITY-APPROVAL-POLICY-V1"
	composeAdapterPolicyDomainV1      = "HOUFENG-COMPOSE-RECORDS-AUTHORITY-ADAPTER-POLICY-V1"
	composeDrainReceiptDomainV1       = "HOUFENG-COMPOSE-RECORDS-AUTHORITY-DRAIN-RECEIPT-V1"
	composeIdentitySetDomainV1        = "HOUFENG-COMPOSE-RECORDS-AUTHORITY-IDENTITY-SET-V1"
	composeMaximumLedgerBytes         = 16 * 1024
)

var (
	ErrComposeStateExists  = errors.New("Compose Records authority state already exists")
	ErrComposeStateAbsent  = errors.New("Compose Records authority state is absent")
	ErrComposeStateInvalid = errors.New("Compose Records authority state is invalid")

	ErrComposeActivationReceiptAbsent  = errors.New("Compose Records activation receipt is absent")
	ErrComposeActivationReceiptInvalid = errors.New("Compose Records activation receipt is invalid")
)

// ComposeMembership is the closed membership inventory activated by the
// single-host Compose profile. It deliberately contains no free-form operator
// input.
type ComposeMembership struct {
	InstanceID           string
	InstanceKind         string
	Capability           string
	DeploymentEpoch      uint64
	FenceContractVersion uint64
	LoadBalancerAdmitted bool
	QueueAdmitted        bool
}

// VerifiedComposeState contains only values recovered from a completely
// verified durable state. The private key remains internal to verification.
type VerifiedComposeState struct {
	DeploymentID      recordplatform.DeploymentID
	ActivationCommand recordplatform.ContractActivationProjectionCommandV1
	Memberships       []ComposeMembership
	databasePassword  string
}

// DatabasePassword returns the constrained authority role credential. Callers
// must keep it scoped to db-init and the authority process.
func (state VerifiedComposeState) DatabasePassword() string {
	return state.databasePassword
}

type activationMembershipV1 struct {
	InstanceID           string `json:"instance_id"`
	InstanceKind         string `json:"instance_kind"`
	Capability           string `json:"capability"`
	DeploymentEpoch      uint64 `json:"deployment_epoch"`
	FenceContractVersion uint64 `json:"fence_contract_version"`
	LoadBalancerAdmitted bool   `json:"load_balancer_admitted"`
	QueueAdmitted        bool   `json:"queue_admitted"`
}

type activationLedgerBodyV1 struct {
	Version                  uint8                    `json:"version"`
	Sequence                 uint64                   `json:"sequence"`
	PreviousHash             string                   `json:"previous_hash"`
	Event                    string                   `json:"event"`
	DeploymentID             string                   `json:"deployment_id"`
	ProjectID                string                   `json:"project_id"`
	Profile                  string                   `json:"profile"`
	ActivationMutationID     string                   `json:"activation_mutation_id"`
	AuthorityPublicKey       string                   `json:"authority_public_key"`
	DatabaseCredentialDigest string                   `json:"database_credential_digest"`
	Memberships              []activationMembershipV1 `json:"memberships"`
}

type signedActivationLedgerEntryV1 struct {
	Body      activationLedgerBodyV1 `json:"body"`
	EntryHash string                 `json:"entry_hash"`
	Signature string                 `json:"signature"`
}

type activationReceiptV1 struct {
	Version                 uint8  `json:"version"`
	ProjectionCommandDigest string `json:"projection_command_digest"`
	CASReceipt              string `json:"cas_receipt"`
}

func composeMembershipInventoryV1() []activationMembershipV1 {
	return []activationMembershipV1{{
		InstanceID:           "compose-center",
		InstanceKind:         "api",
		Capability:           "records.runtime",
		DeploymentEpoch:      1,
		FenceContractVersion: 1,
		LoadBalancerAdmitted: true,
		QueueAdmitted:        false,
	}}
}

// CreateComposeState generates the closed Compose authority state from the
// operating system CSPRNG and publishes the directory with one atomic rename.
func CreateComposeState(root string) (VerifiedComposeState, error) {
	return createComposeState(root, rand.Reader)
}

func createComposeState(root string, randomness io.Reader) (VerifiedComposeState, error) {
	if !validComposeStateRoot(root) || randomness == nil {
		return VerifiedComposeState{}, ErrComposeStateInvalid
	}
	if _, err := os.Lstat(root); err == nil {
		return VerifiedComposeState{}, ErrComposeStateExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return VerifiedComposeState{}, ErrComposeStateInvalid
	}

	deploymentEntropy := make([]byte, sha256.Size)
	keySeed := make([]byte, ed25519.SeedSize)
	databaseEntropy := make([]byte, sha256.Size)
	for _, destination := range [][]byte{deploymentEntropy, keySeed, databaseEntropy} {
		if _, err := io.ReadFull(randomness, destination); err != nil {
			return VerifiedComposeState{}, fmt.Errorf("%w: generate independent authority material", ErrComposeStateInvalid)
		}
	}

	deploymentID := recordplatform.DeploymentID("dp-" + hex.EncodeToString(deploymentEntropy))
	privateKey := ed25519.NewKeyFromSeed(keySeed)
	publicKey := privateKey.Public().(ed25519.PublicKey)
	databasePassword := hex.EncodeToString(databaseEntropy)
	entry, ledger, err := newSignedActivationLedgerEntryV1(deploymentID, publicKey, privateKey, databasePassword)
	if err != nil {
		return VerifiedComposeState{}, fmt.Errorf("%w: construct activation ledger", ErrComposeStateInvalid)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return VerifiedComposeState{}, fmt.Errorf("%w: encode authority key", ErrComposeStateInvalid)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	if len(keyPEM) == 0 {
		return VerifiedComposeState{}, fmt.Errorf("%w: encode authority key", ErrComposeStateInvalid)
	}

	parent := filepath.Dir(root)
	temporaryRoot, err := os.MkdirTemp(parent, "."+filepath.Base(root)+".tmp-")
	if err != nil {
		return VerifiedComposeState{}, fmt.Errorf("%w: create temporary authority state", ErrComposeStateInvalid)
	}
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(temporaryRoot)
		}
	}()
	if err := os.Chmod(temporaryRoot, 0o755); err != nil {
		return VerifiedComposeState{}, fmt.Errorf("%w: set authority root permissions", ErrComposeStateInvalid)
	}
	publicDirectory := filepath.Join(temporaryRoot, composeStatePublicDirectory)
	privateDirectory := filepath.Join(temporaryRoot, composeStatePrivateDirectory)
	if err := os.Mkdir(publicDirectory, 0o755); err != nil {
		return VerifiedComposeState{}, fmt.Errorf("%w: create public authority directory", ErrComposeStateInvalid)
	}
	if err := os.Mkdir(privateDirectory, 0o700); err != nil {
		return VerifiedComposeState{}, fmt.Errorf("%w: create private authority directory", ErrComposeStateInvalid)
	}
	files := []struct {
		path string
		mode os.FileMode
		body []byte
	}{
		{path: filepath.Join(publicDirectory, composeDeploymentIDFilename), mode: 0o644, body: []byte(string(deploymentID) + "\n")},
		{path: filepath.Join(privateDirectory, composeAuthorityKeyFilename), mode: 0o600, body: keyPEM},
		{path: filepath.Join(privateDirectory, composeDatabaseSecretFilename), mode: 0o600, body: []byte(databasePassword + "\n")},
		{path: filepath.Join(privateDirectory, composeActivationLedgerFilename), mode: 0o600, body: ledger},
	}
	for _, file := range files {
		if err := writeSyncedFile(file.path, file.body, file.mode); err != nil {
			return VerifiedComposeState{}, fmt.Errorf("%w: write authority state file", ErrComposeStateInvalid)
		}
	}
	for _, directory := range []string{publicDirectory, privateDirectory, temporaryRoot} {
		if err := syncDirectory(directory); err != nil {
			return VerifiedComposeState{}, fmt.Errorf("%w: sync authority state", ErrComposeStateInvalid)
		}
	}
	if err := os.Rename(temporaryRoot, root); err != nil {
		if _, statErr := os.Lstat(root); statErr == nil {
			return VerifiedComposeState{}, ErrComposeStateExists
		}
		return VerifiedComposeState{}, fmt.Errorf("%w: publish authority state", ErrComposeStateInvalid)
	}
	published = true
	if err := syncDirectory(parent); err != nil {
		return VerifiedComposeState{}, fmt.Errorf("%w: sync authority state parent", ErrComposeStateInvalid)
	}
	return verifiedComposeStateFromEntry(entry, databasePassword)
}

// LoadComposeState verifies file shape, canonical encoding, the complete
// one-entry hash chain, and its Ed25519 signature before deriving the existing
// projection command.
func LoadComposeState(root string) (VerifiedComposeState, error) {
	if !validComposeStateRoot(root) {
		return VerifiedComposeState{}, ErrComposeStateInvalid
	}
	if _, err := os.Lstat(root); errors.Is(err, os.ErrNotExist) {
		return VerifiedComposeState{}, ErrComposeStateAbsent
	} else if err != nil {
		return VerifiedComposeState{}, ErrComposeStateInvalid
	}
	if err := requireComposeStateMode(root, os.ModeDir, 0o755); err != nil {
		return VerifiedComposeState{}, err
	}
	publicDirectory := filepath.Join(root, composeStatePublicDirectory)
	privateDirectory := filepath.Join(root, composeStatePrivateDirectory)
	if err := requireComposeStateMode(publicDirectory, os.ModeDir, 0o755); err != nil {
		return VerifiedComposeState{}, err
	}
	if err := requireComposeStateMode(privateDirectory, os.ModeDir, 0o700); err != nil {
		return VerifiedComposeState{}, err
	}

	deploymentIDRaw, err := readBoundedComposeStateFile(filepath.Join(publicDirectory, composeDeploymentIDFilename), 68, 0o644)
	if err != nil {
		return VerifiedComposeState{}, err
	}
	if len(deploymentIDRaw) != 68 || deploymentIDRaw[len(deploymentIDRaw)-1] != '\n' {
		return VerifiedComposeState{}, fmt.Errorf("%w: deployment identity encoding", ErrComposeStateInvalid)
	}
	deploymentID := recordplatform.DeploymentID(string(deploymentIDRaw[:len(deploymentIDRaw)-1]))
	if err := recordplatform.ValidateDeploymentID(deploymentID); err != nil {
		return VerifiedComposeState{}, fmt.Errorf("%w: deployment identity", ErrComposeStateInvalid)
	}

	keyPEM, err := readBoundedComposeStateFile(filepath.Join(privateDirectory, composeAuthorityKeyFilename), 1024, 0o600)
	if err != nil {
		return VerifiedComposeState{}, err
	}
	block, remainder := pem.Decode(keyPEM)
	if block == nil || block.Type != "PRIVATE KEY" || len(remainder) != 0 {
		return VerifiedComposeState{}, fmt.Errorf("%w: authority key encoding", ErrComposeStateInvalid)
	}
	parsedKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return VerifiedComposeState{}, fmt.Errorf("%w: authority key", ErrComposeStateInvalid)
	}
	privateKey, ok := parsedKey.(ed25519.PrivateKey)
	if !ok || len(privateKey) != ed25519.PrivateKeySize {
		return VerifiedComposeState{}, fmt.Errorf("%w: authority key type", ErrComposeStateInvalid)
	}
	canonicalDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil || !bytes.Equal(keyPEM, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: canonicalDER})) {
		return VerifiedComposeState{}, fmt.Errorf("%w: authority key is not canonical", ErrComposeStateInvalid)
	}
	publicKey := privateKey.Public().(ed25519.PublicKey)

	databaseSecretRaw, err := readBoundedComposeStateFile(filepath.Join(privateDirectory, composeDatabaseSecretFilename), 65, 0o600)
	if err != nil {
		return VerifiedComposeState{}, err
	}
	if len(databaseSecretRaw) != 65 || databaseSecretRaw[64] != '\n' {
		return VerifiedComposeState{}, fmt.Errorf("%w: database credential encoding", ErrComposeStateInvalid)
	}
	databasePassword := string(databaseSecretRaw[:64])
	decodedPassword, err := hex.DecodeString(databasePassword)
	if err != nil || len(decodedPassword) != sha256.Size || databasePassword != strings.ToLower(databasePassword) {
		return VerifiedComposeState{}, fmt.Errorf("%w: database credential", ErrComposeStateInvalid)
	}

	ledger, err := readBoundedComposeStateFile(filepath.Join(privateDirectory, composeActivationLedgerFilename), composeMaximumLedgerBytes, 0o600)
	if err != nil {
		return VerifiedComposeState{}, err
	}
	entry, err := parseAndVerifyActivationLedgerV1(ledger, deploymentID, publicKey, databasePassword)
	if err != nil {
		return VerifiedComposeState{}, err
	}
	return verifiedComposeStateFromEntry(entry, databasePassword)
}

// PersistActivationReceipt validates the database projector result against the
// exact locally derived command, then publishes a canonical receipt without
// replacing any existing mismatched or corrupt proof.
func PersistActivationReceipt(root string, state VerifiedComposeState, receipt []byte) error {
	canonicalState, command, expectedReceipt, err := composeReceiptInputs(root, state)
	if err != nil {
		return err
	}
	_ = canonicalState
	if len(receipt) != sha256.Size || subtle.ConstantTimeCompare(receipt, expectedReceipt[:]) != 1 {
		return ErrComposeActivationReceiptInvalid
	}
	if err := VerifyActivationReceipt(root, state); err == nil {
		return nil
	} else if !errors.Is(err, ErrComposeActivationReceiptAbsent) {
		return err
	}

	commandDigest := sha256.Sum256(command)
	document := activationReceiptV1{
		Version:                 1,
		ProjectionCommandDigest: hex.EncodeToString(commandDigest[:]),
		CASReceipt:              hex.EncodeToString(expectedReceipt[:]),
	}
	body, err := json.Marshal(document)
	if err != nil {
		return ErrComposeActivationReceiptInvalid
	}
	body = append(body, '\n')
	privateDirectory := filepath.Join(root, composeStatePrivateDirectory)
	temporary, err := os.CreateTemp(privateDirectory, ".activation-receipt.tmp-")
	if err != nil {
		return fmt.Errorf("%w: create temporary receipt", ErrComposeActivationReceiptInvalid)
	}
	temporaryPath := temporary.Name()
	linked := false
	defer func() {
		_ = temporary.Close()
		if !linked {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("%w: secure temporary receipt", ErrComposeActivationReceiptInvalid)
	}
	if _, err := temporary.Write(body); err != nil {
		return fmt.Errorf("%w: write temporary receipt", ErrComposeActivationReceiptInvalid)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("%w: sync temporary receipt", ErrComposeActivationReceiptInvalid)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("%w: close temporary receipt", ErrComposeActivationReceiptInvalid)
	}
	receiptPath := filepath.Join(privateDirectory, composeActivationReceiptFilename)
	if err := os.Link(temporaryPath, receiptPath); err != nil {
		if verifyErr := VerifyActivationReceipt(root, state); verifyErr == nil {
			return nil
		}
		return fmt.Errorf("%w: publish receipt", ErrComposeActivationReceiptInvalid)
	}
	linked = true
	if err := os.Remove(temporaryPath); err != nil {
		return fmt.Errorf("%w: finalize receipt", ErrComposeActivationReceiptInvalid)
	}
	if err := syncDirectory(privateDirectory); err != nil {
		return fmt.Errorf("%w: sync receipt", ErrComposeActivationReceiptInvalid)
	}
	return nil
}

// VerifyActivationReceipt proves that the durable receipt is canonical and
// binds the exact activation projection derived from the signed ledger.
func VerifyActivationReceipt(root string, state VerifiedComposeState) error {
	_, command, expectedReceipt, err := composeReceiptInputs(root, state)
	if err != nil {
		return err
	}
	receiptPath := filepath.Join(root, composeStatePrivateDirectory, composeActivationReceiptFilename)
	if _, err := os.Lstat(receiptPath); errors.Is(err, os.ErrNotExist) {
		return ErrComposeActivationReceiptAbsent
	} else if err != nil {
		return ErrComposeActivationReceiptInvalid
	}
	body, err := readBoundedComposeStateFile(receiptPath, 512, 0o600)
	if err != nil {
		return ErrComposeActivationReceiptInvalid
	}
	if len(body) == 0 || body[len(body)-1] != '\n' || bytes.Count(body, []byte{'\n'}) != 1 {
		return ErrComposeActivationReceiptInvalid
	}
	var document activationReceiptV1
	decoder := json.NewDecoder(bytes.NewReader(body[:len(body)-1]))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return ErrComposeActivationReceiptInvalid
	}
	canonical, err := json.Marshal(document)
	if err != nil || !bytes.Equal(append(canonical, '\n'), body) || document.Version != 1 {
		return ErrComposeActivationReceiptInvalid
	}
	commandDigest := sha256.Sum256(command)
	if document.ProjectionCommandDigest != hex.EncodeToString(commandDigest[:]) ||
		document.CASReceipt != hex.EncodeToString(expectedReceipt[:]) {
		return ErrComposeActivationReceiptInvalid
	}
	return nil
}

func composeReceiptInputs(
	root string,
	state VerifiedComposeState,
) (VerifiedComposeState, []byte, [sha256.Size]byte, error) {
	canonicalState, err := LoadComposeState(root)
	if err != nil {
		return VerifiedComposeState{}, nil, [sha256.Size]byte{}, err
	}
	canonicalCommand, err := canonicalState.ActivationCommand.MarshalBinary()
	if err != nil {
		return VerifiedComposeState{}, nil, [sha256.Size]byte{}, ErrComposeActivationReceiptInvalid
	}
	providedCommand, err := state.ActivationCommand.MarshalBinary()
	if err != nil || state.DeploymentID != canonicalState.DeploymentID || !bytes.Equal(providedCommand, canonicalCommand) {
		return VerifiedComposeState{}, nil, [sha256.Size]byte{}, ErrComposeActivationReceiptInvalid
	}
	return canonicalState, canonicalCommand, recordplatform.ProjectionCASReceiptDigestV1(canonicalCommand), nil
}

func newSignedActivationLedgerEntryV1(
	deploymentID recordplatform.DeploymentID,
	publicKey ed25519.PublicKey,
	privateKey ed25519.PrivateKey,
	databasePassword string,
) (signedActivationLedgerEntryV1, []byte, error) {
	mutationInput, err := json.Marshal(struct {
		Version            uint8  `json:"version"`
		DeploymentID       string `json:"deployment_id"`
		AuthorityPublicKey string `json:"authority_public_key"`
	}{Version: 1, DeploymentID: string(deploymentID), AuthorityPublicKey: base64.RawStdEncoding.EncodeToString(publicKey)})
	if err != nil {
		return signedActivationLedgerEntryV1{}, nil, err
	}
	mutationDigest := composeDigestV1(composeActivationMutationDomainV1, mutationInput)
	databaseCredentialDigest := composeDigestV1(composeDatabaseCredentialDomainV1, []byte(databasePassword))
	body := activationLedgerBodyV1{
		Version:                  1,
		Sequence:                 1,
		PreviousHash:             strings.Repeat("0", sha256.Size*2),
		Event:                    "compose_activation",
		DeploymentID:             string(deploymentID),
		ProjectID:                string(recordplatform.ProjectIDDefault),
		Profile:                  "postgres_sync",
		ActivationMutationID:     "tm-" + hex.EncodeToString(mutationDigest[:]),
		AuthorityPublicKey:       base64.RawStdEncoding.EncodeToString(publicKey),
		DatabaseCredentialDigest: hex.EncodeToString(databaseCredentialDigest[:]),
		Memberships:              composeMembershipInventoryV1(),
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return signedActivationLedgerEntryV1{}, nil, err
	}
	entryHash := composeDigestV1(composeLedgerEntryDomainV1, bodyBytes)
	entry := signedActivationLedgerEntryV1{
		Body:      body,
		EntryHash: hex.EncodeToString(entryHash[:]),
		Signature: base64.RawStdEncoding.EncodeToString(ed25519.Sign(privateKey, entryHash[:])),
	}
	ledger, err := json.Marshal(entry)
	if err != nil {
		return signedActivationLedgerEntryV1{}, nil, err
	}
	ledger = append(ledger, '\n')
	if len(ledger) > composeMaximumLedgerBytes {
		return signedActivationLedgerEntryV1{}, nil, fmt.Errorf("activation ledger exceeds bound")
	}
	return entry, ledger, nil
}

func parseAndVerifyActivationLedgerV1(
	ledger []byte,
	deploymentID recordplatform.DeploymentID,
	publicKey ed25519.PublicKey,
	databasePassword string,
) (signedActivationLedgerEntryV1, error) {
	if len(ledger) == 0 || len(ledger) > composeMaximumLedgerBytes ||
		bytes.Count(ledger, []byte{'\n'}) != 1 || ledger[len(ledger)-1] != '\n' {
		return signedActivationLedgerEntryV1{}, fmt.Errorf("%w: activation ledger bounds", ErrComposeStateInvalid)
	}
	var entry signedActivationLedgerEntryV1
	decoder := json.NewDecoder(bytes.NewReader(ledger[:len(ledger)-1]))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&entry); err != nil {
		return signedActivationLedgerEntryV1{}, fmt.Errorf("%w: activation ledger encoding", ErrComposeStateInvalid)
	}
	canonical, err := json.Marshal(entry)
	if err != nil || !bytes.Equal(append(canonical, '\n'), ledger) {
		return signedActivationLedgerEntryV1{}, fmt.Errorf("%w: activation ledger is not canonical", ErrComposeStateInvalid)
	}
	wantMemberships := composeMembershipInventoryV1()
	wantCredentialDigest := composeDigestV1(composeDatabaseCredentialDomainV1, []byte(databasePassword))
	if entry.Body.Version != 1 || entry.Body.Sequence != 1 ||
		entry.Body.PreviousHash != strings.Repeat("0", sha256.Size*2) ||
		entry.Body.Event != "compose_activation" || entry.Body.DeploymentID != string(deploymentID) ||
		entry.Body.ProjectID != string(recordplatform.ProjectIDDefault) || entry.Body.Profile != "postgres_sync" ||
		len(entry.Body.Memberships) != len(wantMemberships) || entry.Body.Memberships[0] != wantMemberships[0] ||
		entry.Body.AuthorityPublicKey != base64.RawStdEncoding.EncodeToString(publicKey) ||
		entry.Body.DatabaseCredentialDigest != hex.EncodeToString(wantCredentialDigest[:]) {
		return signedActivationLedgerEntryV1{}, fmt.Errorf("%w: activation ledger closed contract", ErrComposeStateInvalid)
	}
	mutationInput, err := json.Marshal(struct {
		Version            uint8  `json:"version"`
		DeploymentID       string `json:"deployment_id"`
		AuthorityPublicKey string `json:"authority_public_key"`
	}{Version: 1, DeploymentID: string(deploymentID), AuthorityPublicKey: entry.Body.AuthorityPublicKey})
	if err != nil {
		return signedActivationLedgerEntryV1{}, fmt.Errorf("%w: activation mutation", ErrComposeStateInvalid)
	}
	mutationDigest := composeDigestV1(composeActivationMutationDomainV1, mutationInput)
	if entry.Body.ActivationMutationID != "tm-"+hex.EncodeToString(mutationDigest[:]) {
		return signedActivationLedgerEntryV1{}, fmt.Errorf("%w: activation mutation", ErrComposeStateInvalid)
	}
	bodyBytes, err := json.Marshal(entry.Body)
	if err != nil {
		return signedActivationLedgerEntryV1{}, fmt.Errorf("%w: activation ledger body", ErrComposeStateInvalid)
	}
	entryHash := composeDigestV1(composeLedgerEntryDomainV1, bodyBytes)
	if entry.EntryHash != hex.EncodeToString(entryHash[:]) {
		return signedActivationLedgerEntryV1{}, fmt.Errorf("%w: activation ledger hash chain", ErrComposeStateInvalid)
	}
	signature, err := base64.RawStdEncoding.DecodeString(entry.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize || !ed25519.Verify(publicKey, entryHash[:], signature) {
		return signedActivationLedgerEntryV1{}, fmt.Errorf("%w: activation ledger signature", ErrComposeStateInvalid)
	}
	return entry, nil
}

func verifiedComposeStateFromEntry(entry signedActivationLedgerEntryV1, databasePassword string) (VerifiedComposeState, error) {
	bodyBytes, err := json.Marshal(entry.Body)
	if err != nil {
		return VerifiedComposeState{}, fmt.Errorf("%w: derive activation body", ErrComposeStateInvalid)
	}
	entryHashBytes, err := hex.DecodeString(entry.EntryHash)
	if err != nil || len(entryHashBytes) != sha256.Size {
		return VerifiedComposeState{}, fmt.Errorf("%w: derive activation entry hash", ErrComposeStateInvalid)
	}
	var entryHash [sha256.Size]byte
	copy(entryHash[:], entryHashBytes)
	publicKey, err := base64.RawStdEncoding.DecodeString(entry.Body.AuthorityPublicKey)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return VerifiedComposeState{}, fmt.Errorf("%w: derive authority public key", ErrComposeStateInvalid)
	}

	planBody := mustComposeCanonicalJSON(struct {
		Version      uint8  `json:"version"`
		DeploymentID string `json:"deployment_id"`
		ProjectID    string `json:"project_id"`
		Profile      string `json:"profile"`
	}{1, entry.Body.DeploymentID, entry.Body.ProjectID, entry.Body.Profile})
	authorizationBody := mustComposeCanonicalJSON(struct {
		Version   uint8  `json:"version"`
		Algorithm string `json:"algorithm"`
		PublicKey string `json:"public_key"`
	}{1, "ed25519", entry.Body.AuthorityPublicKey})
	inventoryBody := mustComposeCanonicalJSON(entry.Body.Memberships)
	approvalBody := mustComposeCanonicalJSON(struct {
		Version             uint8  `json:"version"`
		RequiredAlgorithm   string `json:"required_algorithm"`
		RequiredSignatures  uint8  `json:"required_signatures"`
		ExactInventory      bool   `json:"exact_inventory"`
		SingleHostAuthority bool   `json:"single_host_authority"`
	}{1, "ed25519", 1, true, true})
	adapterBody := mustComposeCanonicalJSON(struct {
		Version uint8  `json:"version"`
		Profile string `json:"profile"`
	}{1, entry.Body.Profile})
	drainBody := mustComposeCanonicalJSON(struct {
		Version uint8  `json:"version"`
		State   string `json:"state"`
	}{1, "not_required_for_fresh_activation"})
	identityBody := mustComposeCanonicalJSON(struct {
		Version      uint8                    `json:"version"`
		DeploymentID string                   `json:"deployment_id"`
		ProjectID    string                   `json:"project_id"`
		Memberships  []activationMembershipV1 `json:"memberships"`
	}{1, entry.Body.DeploymentID, entry.Body.ProjectID, entry.Body.Memberships})
	trustBody := make([]byte, 0, len(entryHash)+len(publicKey))
	trustBody = append(trustBody, entryHash[:]...)
	trustBody = append(trustBody, publicKey...)

	command := recordplatform.ContractActivationProjectionCommandV1{
		DeploymentID:                entry.Body.DeploymentID,
		ActiveProfile:               recordplatform.ProjectionProfilePostgresSync,
		ActivationMutationID:        entry.Body.ActivationMutationID,
		WitnessedLedgerSequence:     entry.Body.Sequence,
		WitnessedLedgerHash:         entryHash,
		PlanDigest:                  composeDigestV1(composePlanDomainV1, planBody),
		AuthorizationArtifactDigest: composeDigestV1(composeAuthorizationDomainV1, authorizationBody),
		ActivationBundleDigest:      composeDigestV1(composeActivationBundleDomainV1, bodyBytes),
		TrustRevision:               1,
		TrustHeadHash:               composeDigestV1(composeTrustHeadDomainV1, trustBody),
		InventoryDigest:             composeDigestV1(composeInventoryDomainV1, inventoryBody),
		ApprovalPolicyDigest:        composeDigestV1(composeApprovalPolicyDomainV1, approvalBody),
		AdapterPolicyGeneration:     1,
		AdapterPolicyDigest:         composeDigestV1(composeAdapterPolicyDomainV1, adapterBody),
		DrainReceiptDigest:          composeDigestV1(composeDrainReceiptDomainV1, drainBody),
		IdentitySetEpoch:            1,
		IdentitySetDigest:           composeDigestV1(composeIdentitySetDomainV1, identityBody),
		MinimumFenceContractVersion: 1,
	}
	if _, err := command.MarshalBinary(); err != nil {
		return VerifiedComposeState{}, fmt.Errorf("%w: derived activation projection", ErrComposeStateInvalid)
	}
	memberships := make([]ComposeMembership, len(entry.Body.Memberships))
	for index, membership := range entry.Body.Memberships {
		memberships[index] = ComposeMembership{
			InstanceID: membership.InstanceID, InstanceKind: membership.InstanceKind,
			Capability: membership.Capability, DeploymentEpoch: membership.DeploymentEpoch,
			FenceContractVersion: membership.FenceContractVersion,
			LoadBalancerAdmitted: membership.LoadBalancerAdmitted, QueueAdmitted: membership.QueueAdmitted,
		}
	}
	return VerifiedComposeState{
		DeploymentID:      recordplatform.DeploymentID(entry.Body.DeploymentID),
		ActivationCommand: command,
		Memberships:       memberships,
		databasePassword:  databasePassword,
	}, nil
}

func composeDigestV1(domain string, payload []byte) [sha256.Size]byte {
	preimage := make([]byte, 0, len(domain)+1+4+len(payload))
	preimage = append(preimage, domain...)
	preimage = append(preimage, 0)
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(payload)))
	preimage = append(preimage, length[:]...)
	preimage = append(preimage, payload...)
	return sha256.Sum256(preimage)
}

func mustComposeCanonicalJSON(value any) []byte {
	body, err := json.Marshal(value)
	if err != nil {
		panic("recordauthority: fixed canonical JSON cannot fail")
	}
	return body
}

func validComposeStateRoot(root string) bool {
	if root == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return false
	}
	parent := filepath.Dir(root)
	return parent != root && filepath.Base(root) != "." && filepath.Base(root) != string(filepath.Separator)
}

func writeSyncedFile(path string, body []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(body); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func requireComposeStateMode(path string, kind os.FileMode, permissions os.FileMode) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: authority state is incomplete", ErrComposeStateInvalid)
	}
	if err != nil || info.Mode()&os.ModeType != kind || info.Mode().Perm() != permissions {
		return fmt.Errorf("%w: authority state permissions", ErrComposeStateInvalid)
	}
	return nil
}

func readBoundedComposeStateFile(path string, maximum int64, permissions os.FileMode) ([]byte, error) {
	if err := requireComposeStateMode(path, 0, permissions); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("%w: read authority state file", ErrComposeStateInvalid)
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || int64(len(body)) > maximum {
		return nil, fmt.Errorf("%w: authority state file bounds", ErrComposeStateInvalid)
	}
	return body, nil
}
