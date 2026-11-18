package recordauthority

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"houfeng/internal/center/recordplatform"
)

const (
	composeMembershipSuccessorFilename = "membership-successor.json"
	composeMembershipReceiptFilename   = "membership-rotation-receipt.json"
	composeMembershipIdentityDomainV2  = "HOUFENG-COMPOSE-RECORDS-AUTHORITY-IDENTITY-SET-V2"
	composeMembershipMutationDomainV2  = "HOUFENG-COMPOSE-RECORDS-AUTHORITY-MEMBERSHIP-MUTATION-V2"
	composeMembershipLedgerDomainV2    = "HOUFENG-COMPOSE-RECORDS-AUTHORITY-MEMBERSHIP-LEDGER-ENTRY-V2"
	composeMembershipTrustDomainV2     = "HOUFENG-COMPOSE-RECORDS-AUTHORITY-TRUST-HEAD-V2"
)

var (
	ErrComposeMembershipRotationReceiptAbsent  = errors.New("Compose Records membership rotation receipt is absent")
	ErrComposeMembershipRotationReceiptInvalid = errors.New("Compose Records membership rotation receipt is invalid")
)

type membershipSuccessorBodyV2 struct {
	Version                    uint8                    `json:"version"`
	Sequence                   uint64                   `json:"sequence"`
	PreviousHash               string                   `json:"previous_hash"`
	Event                      string                   `json:"event"`
	DeploymentID               string                   `json:"deployment_id"`
	ProjectID                  string                   `json:"project_id"`
	Profile                    string                   `json:"profile"`
	RotationMutationID         string                   `json:"rotation_mutation_id"`
	AuthorityPublicKey         string                   `json:"authority_public_key"`
	DatabaseCredentialDigest   string                   `json:"database_credential_digest"`
	BaseActivationBundleDigest string                   `json:"base_activation_bundle_digest"`
	Memberships                []activationMembershipV1 `json:"memberships"`
}

type signedMembershipSuccessorV2 struct {
	Body      membershipSuccessorBodyV2 `json:"body"`
	EntryHash string                    `json:"entry_hash"`
	Signature string                    `json:"signature"`
}

func composeMembershipInventoryV2() []activationMembershipV1 {
	return []activationMembershipV1{
		{InstanceID: "compose-center", InstanceKind: "api", Capability: "records.runtime", DeploymentEpoch: 2, FenceContractVersion: 1, LoadBalancerAdmitted: true},
		{InstanceID: "compose-processor", InstanceKind: "worker", Capability: "records.runtime", DeploymentEpoch: 2, FenceContractVersion: 1, QueueAdmitted: true},
	}
}

func membershipIdentityDigestV2(deploymentID string) [sha256.Size]byte {
	body := mustComposeCanonicalJSON(struct {
		Version      uint8                    `json:"version"`
		DeploymentID string                   `json:"deployment_id"`
		ProjectID    string                   `json:"project_id"`
		Memberships  []activationMembershipV1 `json:"memberships"`
	}{2, deploymentID, string(recordplatform.ProjectIDDefault), composeMembershipInventoryV2()})
	return composeDigestV1(composeMembershipIdentityDomainV2, body)
}

func membershipSuccessorBody(genesis signedActivationLedgerEntryV1, activation recordplatform.ContractActivationProjectionCommandV1) membershipSuccessorBodyV2 {
	identity := membershipIdentityDigestV2(genesis.Body.DeploymentID)
	payload := make([]byte, 0, 3*sha256.Size)
	payload = append(payload, activation.WitnessedLedgerHash[:]...)
	payload = append(payload, identity[:]...)
	payload = append(payload, activation.ActivationBundleDigest[:]...)
	mutation := composeDigestV1(composeMembershipMutationDomainV2, payload)
	return membershipSuccessorBodyV2{
		Version: 2, Sequence: 2, PreviousHash: genesis.EntryHash,
		Event: "compose_membership_upgrade", DeploymentID: genesis.Body.DeploymentID,
		ProjectID: genesis.Body.ProjectID, Profile: genesis.Body.Profile,
		RotationMutationID:         "tm-" + hex.EncodeToString(mutation[:]),
		AuthorityPublicKey:         genesis.Body.AuthorityPublicKey,
		DatabaseCredentialDigest:   genesis.Body.DatabaseCredentialDigest,
		BaseActivationBundleDigest: hex.EncodeToString(activation.ActivationBundleDigest[:]),
		Memberships:                composeMembershipInventoryV2(),
	}
}

func rotationFromSuccessor(activation recordplatform.ContractActivationProjectionCommandV1, body membershipSuccessorBodyV2, entryHash [sha256.Size]byte, publicKey ed25519.PublicKey) recordplatform.DomainRotationProjectionCommandV1 {
	trustPayload := make([]byte, 0, 3*sha256.Size)
	trustPayload = append(trustPayload, activation.TrustHeadHash[:]...)
	trustPayload = append(trustPayload, entryHash[:]...)
	trustPayload = append(trustPayload, publicKey...)
	return recordplatform.DomainRotationProjectionCommandV1{
		DeploymentID: activation.DeploymentID, ActiveProfile: activation.ActiveProfile,
		RotationMutationID:              body.RotationMutationID,
		ExpectedWitnessedLedgerSequence: activation.WitnessedLedgerSequence,
		ExpectedWitnessedLedgerHash:     activation.WitnessedLedgerHash,
		ExpectedIdentitySetEpoch:        activation.IdentitySetEpoch, ExpectedIdentitySetDigest: activation.IdentitySetDigest,
		ExpectedAdapterPolicyGeneration: activation.AdapterPolicyGeneration, ExpectedAdapterPolicyDigest: activation.AdapterPolicyDigest,
		ExpectedMinimumFenceContractVersion: activation.MinimumFenceContractVersion,
		ExpectedTrustRevision:               activation.TrustRevision, ExpectedTrustHeadHash: activation.TrustHeadHash,
		NextWitnessedLedgerSequence: 2, NextWitnessedLedgerHash: entryHash,
		NextIdentitySetEpoch: 2, NextIdentitySetDigest: membershipIdentityDigestV2(activation.DeploymentID),
		NextAdapterPolicyGeneration: activation.AdapterPolicyGeneration, NextAdapterPolicyDigest: activation.AdapterPolicyDigest,
		NextMinimumFenceContractVersion: 1, NextTrustRevision: 2,
		NextTrustHeadHash: composeDigestV1(composeMembershipTrustDomainV2, trustPayload),
	}
}

func loadComposeMembershipSuccessor(root string, state VerifiedComposeState, genesis signedActivationLedgerEntryV1, publicKey ed25519.PublicKey) (VerifiedComposeState, error) {
	path := filepath.Join(root, composeStatePrivateDirectory, composeMembershipSuccessorFilename)
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return state, nil
	} else if err != nil {
		return VerifiedComposeState{}, fmt.Errorf("%w: successor metadata", ErrComposeStateInvalid)
	}
	raw, err := readBoundedComposeStateFile(path, composeMaximumLedgerBytes, 0o600)
	if err != nil {
		return VerifiedComposeState{}, fmt.Errorf("%w: successor encoding", ErrComposeStateInvalid)
	}
	return verifyComposeMembershipSuccessor(state, genesis, publicKey, raw)
}

func verifyComposeMembershipSuccessor(state VerifiedComposeState, genesis signedActivationLedgerEntryV1, publicKey ed25519.PublicKey, raw []byte) (VerifiedComposeState, error) {
	if len(raw) == 0 || raw[len(raw)-1] != '\n' || bytes.Count(raw, []byte{'\n'}) != 1 {
		return VerifiedComposeState{}, fmt.Errorf("%w: successor encoding", ErrComposeStateInvalid)
	}
	var entry signedMembershipSuccessorV2
	decoder := json.NewDecoder(bytes.NewReader(raw[:len(raw)-1]))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&entry); err != nil {
		return VerifiedComposeState{}, fmt.Errorf("%w: successor encoding", ErrComposeStateInvalid)
	}
	canonical, err := json.Marshal(entry)
	if err != nil || !bytes.Equal(append(canonical, '\n'), raw) || !bytes.Equal(mustComposeCanonicalJSON(entry.Body), mustComposeCanonicalJSON(membershipSuccessorBody(genesis, state.ActivationCommand))) {
		return VerifiedComposeState{}, fmt.Errorf("%w: successor closed contract", ErrComposeStateInvalid)
	}
	entryHash := composeDigestV1(composeMembershipLedgerDomainV2, mustComposeCanonicalJSON(entry.Body))
	signature, err := base64.RawStdEncoding.DecodeString(entry.Signature)
	if entry.EntryHash != hex.EncodeToString(entryHash[:]) || err != nil || len(signature) != ed25519.SignatureSize || entry.Signature != base64.RawStdEncoding.EncodeToString(signature) || !ed25519.Verify(publicKey, entryHash[:], signature) {
		return VerifiedComposeState{}, fmt.Errorf("%w: successor signature or chain", ErrComposeStateInvalid)
	}
	rotation := rotationFromSuccessor(state.ActivationCommand, entry.Body, entryHash, publicKey)
	if _, err := rotation.MarshalBinary(); err != nil {
		return VerifiedComposeState{}, fmt.Errorf("%w: successor projection", ErrComposeStateInvalid)
	}
	state.MembershipVersion = 2
	state.MembershipRotation = &rotation
	privateRotation := rotation
	state.verifiedMembershipRotation = &privateRotation
	state.Memberships = make([]ComposeMembership, 0, len(entry.Body.Memberships))
	for _, member := range entry.Body.Memberships {
		state.Memberships = append(state.Memberships, ComposeMembership{member.InstanceID, member.InstanceKind, member.Capability, member.DeploymentEpoch, member.FenceContractVersion, member.LoadBalancerAdmitted, member.QueueAdmitted})
	}
	return state, nil
}

func equalVerifiedComposeState(a, b VerifiedComposeState) bool {
	if a.DeploymentID != b.DeploymentID || a.ActivationCommand != b.ActivationCommand || a.MembershipVersion != b.MembershipVersion || a.databasePassword != b.databasePassword || a.genesisActivationCommand != b.genesisActivationCommand || len(a.Memberships) != len(b.Memberships) {
		return false
	}
	for i := range a.Memberships {
		if a.Memberships[i] != b.Memberships[i] {
			return false
		}
	}
	if (a.MembershipRotation == nil) != (b.MembershipRotation == nil) || (a.verifiedMembershipRotation == nil) != (b.verifiedMembershipRotation == nil) {
		return false
	}
	if a.MembershipRotation != nil && *a.MembershipRotation != *b.MembershipRotation {
		return false
	}
	return a.verifiedMembershipRotation == nil || *a.verifiedMembershipRotation == *b.verifiedMembershipRotation
}

// PreviewComposeMembershipV2 authenticates durable authority files and derives
// the exact prospective V2 state without creating or publishing any file.
// A preview is not evidence of a durable successor or an applied projection.
func PreviewComposeMembershipV2(root string) (VerifiedComposeState, error) {
	state, _, err := previewComposeMembershipV2(root)
	return state, err
}

// PrepareComposeMembershipV2 publishes the deterministic signed successor, or
// verifies an existing successor. Its presence does not prove database application.
func PrepareComposeMembershipV2(root string) (VerifiedComposeState, error) {
	preview, raw, err := previewComposeMembershipV2(root)
	if err != nil {
		return VerifiedComposeState{}, err
	}
	privateDir := filepath.Join(root, composeStatePrivateDirectory)
	if raw == nil {
		if err := syncDirectory(privateDir); err != nil {
			return VerifiedComposeState{}, fmt.Errorf("%w: sync successor", ErrComposeStateInvalid)
		}
		return preview, nil
	}
	if err := publishComposePrivateFile(privateDir, composeMembershipSuccessorFilename, raw); err != nil {
		return VerifiedComposeState{}, fmt.Errorf("%w: publish successor", ErrComposeStateInvalid)
	}
	loaded, err := LoadComposeState(root)
	if err != nil {
		return VerifiedComposeState{}, err
	}
	if !equalVerifiedComposeState(loaded, preview) {
		return VerifiedComposeState{}, ErrComposeStateInvalid
	}
	return loaded, nil
}

// previewComposeMembershipV2 returns successor bytes only when publication is
// still necessary. Both preview and prepare use the same verified derivation.
func previewComposeMembershipV2(root string) (VerifiedComposeState, []byte, error) {
	state, err := LoadComposeState(root)
	if err != nil {
		return VerifiedComposeState{}, nil, err
	}
	if err := VerifyActivationReceipt(root, state); err != nil && !errors.Is(err, ErrComposeActivationReceiptAbsent) {
		return VerifiedComposeState{}, nil, err
	}
	if state.MembershipVersion == 2 {
		if err := VerifyMembershipRotationReceipt(root, state); err != nil && !errors.Is(err, ErrComposeMembershipRotationReceiptAbsent) {
			return VerifiedComposeState{}, nil, err
		}
		return state, nil, nil
	}
	privateDir := filepath.Join(root, composeStatePrivateDirectory)
	receiptPath := filepath.Join(privateDir, composeMembershipReceiptFilename)
	if _, err := os.Lstat(receiptPath); err == nil || !errors.Is(err, os.ErrNotExist) {
		return VerifiedComposeState{}, nil, fmt.Errorf("%w: receipt without successor", ErrComposeStateInvalid)
	}
	keyPEM, err := readBoundedComposeStateFile(filepath.Join(privateDir, composeAuthorityKeyFilename), 1024, 0o600)
	if err != nil {
		return VerifiedComposeState{}, nil, err
	}
	block, rest := pem.Decode(keyPEM)
	if block == nil || block.Type != "PRIVATE KEY" || len(rest) != 0 {
		return VerifiedComposeState{}, nil, ErrComposeStateInvalid
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return VerifiedComposeState{}, nil, ErrComposeStateInvalid
	}
	privateKey, ok := key.(ed25519.PrivateKey)
	if !ok || len(privateKey) != ed25519.PrivateKeySize {
		return VerifiedComposeState{}, nil, ErrComposeStateInvalid
	}
	canonicalDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil || !bytes.Equal(keyPEM, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: canonicalDER})) {
		return VerifiedComposeState{}, nil, ErrComposeStateInvalid
	}
	publicKey := privateKey.Public().(ed25519.PublicKey)
	ledger, err := readBoundedComposeStateFile(filepath.Join(privateDir, composeActivationLedgerFilename), composeMaximumLedgerBytes, 0o600)
	if err != nil {
		return VerifiedComposeState{}, nil, err
	}
	genesis, err := parseAndVerifyActivationLedgerV1(ledger, state.DeploymentID, publicKey, state.databasePassword)
	if err != nil {
		return VerifiedComposeState{}, nil, err
	}
	verifiedGenesis, err := verifiedComposeStateFromEntry(genesis, state.databasePassword)
	if err != nil || !equalVerifiedComposeState(state, verifiedGenesis) {
		return VerifiedComposeState{}, nil, ErrComposeStateInvalid
	}
	body := membershipSuccessorBody(genesis, state.ActivationCommand)
	entryHash := composeDigestV1(composeMembershipLedgerDomainV2, mustComposeCanonicalJSON(body))
	entry := signedMembershipSuccessorV2{body, hex.EncodeToString(entryHash[:]), base64.RawStdEncoding.EncodeToString(ed25519.Sign(privateKey, entryHash[:]))}
	raw := append(mustComposeCanonicalJSON(entry), '\n')
	preview, err := verifyComposeMembershipSuccessor(state, genesis, publicKey, raw)
	if err != nil {
		return VerifiedComposeState{}, nil, err
	}
	return preview, raw, nil
}

// publishComposePrivateFile never replaces an existing file. The caller must
// re-read and validate the destination, including when another writer won.
func publishComposePrivateFile(dir, name string, body []byte) error {
	file, err := os.CreateTemp(dir, ".membership.tmp-")
	if err != nil {
		return err
	}
	path := file.Name()
	defer os.Remove(path)
	defer file.Close()
	if err := file.Chmod(0o600); err != nil {
		return err
	}
	if n, err := file.Write(body); err != nil {
		return err
	} else if n != len(body) {
		return errors.New("short private file write")
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Link(path, filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	return syncDirectory(dir)
}

func membershipReceiptInputs(root string, state VerifiedComposeState) ([]byte, [sha256.Size]byte, error) {
	canonical, err := LoadComposeState(root)
	if err != nil {
		return nil, [sha256.Size]byte{}, err
	}
	if canonical.MembershipVersion != 2 || canonical.MembershipRotation == nil || !equalVerifiedComposeState(state, canonical) {
		return nil, [sha256.Size]byte{}, ErrComposeMembershipRotationReceiptInvalid
	}
	command, err := canonical.MembershipRotation.MarshalBinary()
	if err != nil {
		return nil, [sha256.Size]byte{}, ErrComposeMembershipRotationReceiptInvalid
	}
	return command, recordplatform.ProjectionCASReceiptDigestV1(command), nil
}

// PersistMembershipRotationReceipt checks correspondence to the canonical
// command. The caller must establish successful database commit before calling.
func PersistMembershipRotationReceipt(root string, state VerifiedComposeState, receipt []byte) error {
	command, expected, err := membershipReceiptInputs(root, state)
	if err != nil {
		return err
	}
	if len(receipt) != sha256.Size || subtle.ConstantTimeCompare(receipt, expected[:]) != 1 {
		return ErrComposeMembershipRotationReceiptInvalid
	}
	if err := VerifyMembershipRotationReceipt(root, state); err == nil {
		if err := syncDirectory(filepath.Join(root, composeStatePrivateDirectory)); err != nil {
			return ErrComposeMembershipRotationReceiptInvalid
		}
		return nil
	} else if !errors.Is(err, ErrComposeMembershipRotationReceiptAbsent) {
		return err
	}
	digest := sha256.Sum256(command)
	document := activationReceiptV1{1, hex.EncodeToString(digest[:]), hex.EncodeToString(expected[:])}
	body := append(mustComposeCanonicalJSON(document), '\n')
	if err := publishComposePrivateFile(filepath.Join(root, composeStatePrivateDirectory), composeMembershipReceiptFilename, body); err != nil {
		return ErrComposeMembershipRotationReceiptInvalid
	}
	return VerifyMembershipRotationReceipt(root, state)
}

// VerifyMembershipRotationReceipt checks durable canonical command correspondence;
// it does not establish that a database transaction committed or remains current.
func VerifyMembershipRotationReceipt(root string, state VerifiedComposeState) error {
	command, expected, err := membershipReceiptInputs(root, state)
	if err != nil {
		return err
	}
	path := filepath.Join(root, composeStatePrivateDirectory, composeMembershipReceiptFilename)
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return ErrComposeMembershipRotationReceiptAbsent
	} else if err != nil {
		return ErrComposeMembershipRotationReceiptInvalid
	}
	body, err := readBoundedComposeStateFile(path, 512, 0o600)
	if err != nil || len(body) == 0 || body[len(body)-1] != '\n' || bytes.Count(body, []byte{'\n'}) != 1 {
		return ErrComposeMembershipRotationReceiptInvalid
	}
	var document activationReceiptV1
	decoder := json.NewDecoder(bytes.NewReader(body[:len(body)-1]))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return ErrComposeMembershipRotationReceiptInvalid
	}
	canonical, err := json.Marshal(document)
	digest := sha256.Sum256(command)
	if err != nil || !bytes.Equal(append(canonical, '\n'), body) || document.Version != 1 || document.ProjectionCommandDigest != hex.EncodeToString(digest[:]) || document.CASReceipt != hex.EncodeToString(expected[:]) {
		return ErrComposeMembershipRotationReceiptInvalid
	}
	return nil
}
