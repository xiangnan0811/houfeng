package recordbirth

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"

	"houfeng/internal/center/sourceretirement"
)

const maxLaunchBindingBytes = 16384

// LaunchBinding is the public, secret-free launch state published by the
// protected initializer. It is not an admission assertion by itself.
type LaunchBinding struct {
	ProtocolVersion        uint64
	ProcessKind            string
	DeploymentID           string
	SourceGeneration       uint64
	BirthInstalled         bool
	Witness                sourceretirement.DomainIdentity
	BirthBinding           Binding
	ActivationBundleDigest [32]byte
	BinaryDigest           [32]byte
}

type launchWitnessJSON struct {
	ID               string `json:"id"`
	SystemIdentifier string `json:"system_identifier"`
	DatabaseOID      uint32 `json:"database_oid"`
	DatabaseName     string `json:"database_name"`
	Epoch            uint64 `json:"epoch"`
}

type launchBindingJSON struct {
	ProtocolVersion        uint64               `json:"protocol_version"`
	ProcessKind            string               `json:"process_kind"`
	DeploymentID           string               `json:"deployment_id"`
	SourceGeneration       uint64               `json:"source_generation"`
	BirthInstalled         bool                 `json:"birth_installed"`
	Witness                launchWitnessJSON    `json:"witness"`
	BirthBinding           postgresBirthBinding `json:"birth_binding"`
	ActivationBundleDigest string               `json:"activation_bundle_digest"`
	BinaryDigest           string               `json:"binary_digest"`
}

func validateLaunchBinding(value LaunchBinding) error {
	if value.ProtocolVersion != 1 || (value.ProcessKind != "center" && value.ProcessKind != "content-processor") ||
		!hexIdentity(value.DeploymentID, "dp-", 64) || value.SourceGeneration > math.MaxInt64 ||
		value.ActivationBundleDigest == ([32]byte{}) || value.BinaryDigest == ([32]byte{}) {
		return fmt.Errorf("%w: invalid launch binding", ErrInvalid)
	}
	if value.SourceGeneration == 0 {
		if value.BirthInstalled || value.Witness != (sourceretirement.DomainIdentity{}) {
			return fmt.Errorf("%w: invalid never-enrolled launch binding", ErrInvalid)
		}
	} else if !validPostgresPhysicalIdentity(postgresPhysicalIdentity{
		ID: value.Witness.ID, SystemIdentifier: value.Witness.SystemIdentifier,
		DatabaseOID: value.Witness.DatabaseOID, DatabaseName: value.Witness.DatabaseName,
		Epoch: value.Witness.Epoch,
	}) {
		return fmt.Errorf("%w: invalid launch witness identity", ErrInvalid)
	}
	if value.BirthInstalled {
		if value.SourceGeneration == 0 || value.BirthBinding.Validate() != nil ||
			value.BirthBinding.AuthorityID != value.Witness.ID ||
			value.BirthBinding.DeploymentID != value.DeploymentID ||
			value.BirthBinding.Generation != value.SourceGeneration {
			return fmt.Errorf("%w: launch birth binding mismatch", ErrInvalid)
		}
	} else if value.BirthBinding != (Binding{}) {
		return fmt.Errorf("%w: unexpected launch birth binding", ErrInvalid)
	}
	return nil
}

// EncodeLaunchBinding encodes all fields in their fixed canonical JSON order.
// A zero cutover is represented by an empty string, not an all-zero digest.
func EncodeLaunchBinding(value LaunchBinding) ([]byte, error) {
	if err := validateLaunchBinding(value); err != nil {
		return nil, err
	}
	birth := postgresBinding(value.BirthBinding)
	if value.BirthBinding.CutoverDigest == ([32]byte{}) {
		birth.CutoverDigest = ""
	}
	encoded, err := json.Marshal(launchBindingJSON{
		ProtocolVersion: value.ProtocolVersion, ProcessKind: value.ProcessKind,
		DeploymentID: value.DeploymentID, SourceGeneration: value.SourceGeneration,
		BirthInstalled: value.BirthInstalled,
		Witness: launchWitnessJSON{
			ID: value.Witness.ID, SystemIdentifier: value.Witness.SystemIdentifier,
			DatabaseOID: value.Witness.DatabaseOID, DatabaseName: value.Witness.DatabaseName,
			Epoch: value.Witness.Epoch,
		},
		BirthBinding:           birth,
		ActivationBundleDigest: hex.EncodeToString(value.ActivationBundleDigest[:]),
		BinaryDigest:           hex.EncodeToString(value.BinaryDigest[:]),
	})
	if err != nil {
		return nil, fmt.Errorf("%w: encode launch binding", ErrInvalid)
	}
	return encoded, nil
}

func launchDigest(text string, emptyOK bool) ([32]byte, error) {
	var digest [32]byte
	if emptyOK && text == "" {
		return digest, nil
	}
	if len(text) != 64 {
		return digest, ErrInvalid
	}
	if _, err := hex.Decode(digest[:], []byte(text)); err != nil {
		return [32]byte{}, ErrInvalid
	}
	return digest, nil
}

// DecodeLaunchBinding accepts only the exact compact canonical JSON form.
func DecodeLaunchBinding(raw []byte) (LaunchBinding, error) {
	if len(raw) == 0 || len(raw) > maxLaunchBindingBytes {
		return LaunchBinding{}, fmt.Errorf("%w: launch binding size", ErrInvalid)
	}
	var wire launchBindingJSON
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return LaunchBinding{}, fmt.Errorf("%w: decode launch binding", ErrInvalid)
	}
	cutover, err := launchDigest(wire.BirthBinding.CutoverDigest, true)
	if err != nil {
		return LaunchBinding{}, fmt.Errorf("%w: invalid launch cutover digest", ErrInvalid)
	}
	activation, err := launchDigest(wire.ActivationBundleDigest, false)
	if err != nil {
		return LaunchBinding{}, fmt.Errorf("%w: invalid launch activation digest", ErrInvalid)
	}
	binary, err := launchDigest(wire.BinaryDigest, false)
	if err != nil {
		return LaunchBinding{}, fmt.Errorf("%w: invalid launch binary digest", ErrInvalid)
	}
	value := LaunchBinding{
		ProtocolVersion: wire.ProtocolVersion, ProcessKind: wire.ProcessKind,
		DeploymentID: wire.DeploymentID, SourceGeneration: wire.SourceGeneration,
		BirthInstalled: wire.BirthInstalled,
		Witness: sourceretirement.DomainIdentity{
			ID: wire.Witness.ID, SystemIdentifier: wire.Witness.SystemIdentifier,
			DatabaseOID: wire.Witness.DatabaseOID, DatabaseName: wire.Witness.DatabaseName,
			Epoch: wire.Witness.Epoch,
		},
		BirthBinding: Binding{
			AuthorityID: wire.BirthBinding.AuthorityID, LineageID: wire.BirthBinding.LineageID,
			DeploymentID: wire.BirthBinding.DeploymentID, ProjectID: wire.BirthBinding.ProjectID,
			Generation: wire.BirthBinding.Generation, KeyID: wire.BirthBinding.KeyID,
			CutoverDigest: cutover,
		},
		ActivationBundleDigest: activation, BinaryDigest: binary,
	}
	canonical, err := EncodeLaunchBinding(value)
	if err != nil || !bytes.Equal(raw, canonical) {
		return LaunchBinding{}, fmt.Errorf("%w: noncanonical launch binding", ErrInvalid)
	}
	return value, nil
}

// DigestExecutable streams the contents of a regular file and rejects a changed
// inode, size, modification time, mode, or path binding during the read.
func DigestExecutable(path string) ([32]byte, error) {
	file, err := openExecutableForDigest(path)
	if err != nil {
		return [32]byte{}, fmt.Errorf("open executable: %w", err)
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() < 0 {
		return [32]byte{}, fmt.Errorf("%w: executable is not a regular file", ErrInvalid)
	}
	hash := sha256.New()
	// Read at most the observed size even if another process keeps appending.
	n, err := io.Copy(hash, io.LimitReader(file, before.Size()))
	if err != nil {
		return [32]byte{}, fmt.Errorf("read executable: %w", err)
	}
	after, err := file.Stat()
	if err != nil || !sameExecutableInfo(before, after) || n != before.Size() {
		return [32]byte{}, fmt.Errorf("%w: executable changed during read", ErrInvalid)
	}
	named, err := os.Stat(path)
	if err != nil || !sameExecutableInfo(before, named) {
		return [32]byte{}, fmt.Errorf("%w: executable path changed during read", ErrInvalid)
	}
	var digest [32]byte
	copy(digest[:], hash.Sum(nil))
	return digest, nil
}

func sameExecutableInfo(a, b os.FileInfo) bool {
	return os.SameFile(a, b) && a.Mode() == b.Mode() && a.Size() == b.Size() &&
		a.ModTime().Equal(b.ModTime()) && executableMetadataUnchanged(a, b)
}
