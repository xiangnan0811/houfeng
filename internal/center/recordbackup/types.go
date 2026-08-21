package recordbackup

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"time"

	"houfeng/internal/center/recordreadiness"
)

var (
	ErrInvalidBackupRequest   = errors.New("invalid record backup request")
	ErrUnknownManifestVersion = errors.New("unknown record backup manifest version")
	ErrTamperedManifest       = errors.New("tampered record backup manifest")
	ErrBackupIncomplete       = errors.New("record backup incomplete")
	ErrBackupUnavailable      = errors.New("record backup unavailable")
	ErrBackupCleanupRequired  = errors.New("record backup cleanup required")
)

const (
	ManifestFormatV1                   = "houfeng-record-backup/v1"
	ManifestReaderVersionV1     uint32 = 1
	CapabilityContractVersionV1        = recordreadiness.CapabilityContractVersionV1
)

type Profile string

const (
	ProfileLocal Profile = "local"
	ProfileS3    Profile = "s3"
)

type Classification string

const (
	ClassificationDatabase Classification = "database"
	ClassificationObject   Classification = "object"
	ClassificationManifest Classification = "manifest"
)

type AdapterRef struct {
	kind    recordreadiness.CapabilityKind
	version uint32
}

func NewAdapterRef(kind recordreadiness.CapabilityKind, version uint32) (AdapterRef, error) {
	if kind == "" || version != CapabilityContractVersionV1 {
		return AdapterRef{}, ErrInvalidBackupRequest
	}
	return AdapterRef{kind: kind, version: version}, nil
}

func (ref AdapterRef) Kind() recordreadiness.CapabilityKind { return ref.kind }

func (ref AdapterRef) Version() uint32 { return ref.version }

type ArtifactRef struct {
	kind           string
	keyVersion     string
	digest         [sha256.Size]byte
	size           uint64
	classification Classification
}

func NewArtifactRef(
	kind string,
	keyVersion string,
	digest [sha256.Size]byte,
	size uint64,
	classification Classification,
) (ArtifactRef, error) {
	if !validArtifactKind(kind) || !validKeyVersion(keyVersion) || digest == ([sha256.Size]byte{}) {
		return ArtifactRef{}, ErrInvalidBackupRequest
	}
	switch classification {
	case ClassificationDatabase, ClassificationObject, ClassificationManifest:
	default:
		return ArtifactRef{}, ErrInvalidBackupRequest
	}
	return ArtifactRef{
		kind:           kind,
		keyVersion:     keyVersion,
		digest:         digest,
		size:           size,
		classification: classification,
	}, nil
}

func (ref ArtifactRef) Kind() string { return ref.kind }

func (ref ArtifactRef) KeyVersion() string { return ref.keyVersion }

func (ref ArtifactRef) Digest() [sha256.Size]byte { return ref.digest }

func (ref ArtifactRef) Size() uint64 { return ref.size }

func (ref ArtifactRef) Classification() Classification { return ref.classification }

type DeletionWatermark struct {
	sequence uint64
	digest   [sha256.Size]byte
}

func NewDeletionWatermark(sequence uint64, digest [sha256.Size]byte) (DeletionWatermark, error) {
	if sequence == 0 || digest == ([sha256.Size]byte{}) {
		return DeletionWatermark{}, ErrInvalidBackupRequest
	}
	return DeletionWatermark{sequence: sequence, digest: digest}, nil
}

func (mark DeletionWatermark) Sequence() uint64 { return mark.sequence }

func (mark DeletionWatermark) Digest() [sha256.Size]byte { return mark.digest }

type ManifestInput struct {
	BuildCommit     string
	BuildVersion    string
	MigrationDigest [sha256.Size]byte
	AppACLDigest    [sha256.Size]byte
	Adapters        []AdapterRef
	Database        ArtifactRef
	Objects         []ArtifactRef
	Deletion        DeletionWatermark
	CreatedAt       time.Time
	Profile         Profile
}

type Manifest struct {
	format           string
	minReaderVersion uint32
	buildCommit      string
	buildVersion     string
	migrationDigest  [sha256.Size]byte
	appACLDigest     [sha256.Size]byte
	adapters         []AdapterRef
	database         ArtifactRef
	objects          []ArtifactRef
	deletion         DeletionWatermark
	createdUnix      int64
	profile          Profile
	completionDigest [sha256.Size]byte
}

func NewManifest(input ManifestInput) (Manifest, error) {
	if input.BuildCommit == "" || input.BuildVersion == "" ||
		input.MigrationDigest == ([sha256.Size]byte{}) ||
		input.AppACLDigest == ([sha256.Size]byte{}) ||
		input.CreatedAt.IsZero() ||
		(input.Profile != ProfileLocal && input.Profile != ProfileS3) ||
		input.Database.classification != ClassificationDatabase ||
		input.Deletion.sequence == 0 {
		return Manifest{}, ErrInvalidBackupRequest
	}
	for _, object := range input.Objects {
		if object.classification != ClassificationObject {
			return Manifest{}, ErrInvalidBackupRequest
		}
	}
	return Manifest{
		format:           ManifestFormatV1,
		minReaderVersion: ManifestReaderVersionV1,
		buildCommit:      input.BuildCommit,
		buildVersion:     input.BuildVersion,
		migrationDigest:  input.MigrationDigest,
		appACLDigest:     input.AppACLDigest,
		adapters:         append([]AdapterRef(nil), input.Adapters...),
		database:         input.Database,
		objects:          append([]ArtifactRef(nil), input.Objects...),
		deletion:         input.Deletion,
		createdUnix:      input.CreatedAt.UTC().Unix(),
		profile:          input.Profile,
	}, nil
}

func (manifest Manifest) Format() string { return manifest.format }

func (manifest Manifest) MinReaderVersion() uint32 { return manifest.minReaderVersion }

func (manifest Manifest) BuildCommit() string { return manifest.buildCommit }

func (manifest Manifest) BuildVersion() string { return manifest.buildVersion }

func (manifest Manifest) MigrationDigest() [sha256.Size]byte { return manifest.migrationDigest }

func (manifest Manifest) AppACLDigest() [sha256.Size]byte { return manifest.appACLDigest }

func (manifest Manifest) Database() ArtifactRef { return manifest.database }

func (manifest Manifest) Deletion() DeletionWatermark { return manifest.deletion }

func (manifest Manifest) Profile() Profile { return manifest.profile }

func (manifest Manifest) CompletionDigest() [sha256.Size]byte { return manifest.completionDigest }

func (manifest Manifest) Adapters() []AdapterRef {
	return append([]AdapterRef(nil), manifest.adapters...)
}

func (manifest Manifest) Objects() []ArtifactRef {
	return append([]ArtifactRef(nil), manifest.objects...)
}

type Plan struct {
	artifacts []ArtifactRef
}

func (plan Plan) Artifacts() []ArtifactRef {
	return append([]ArtifactRef(nil), plan.artifacts...)
}

type CleanupReceipt struct {
	abortedArtifacts   []string
	abortedMultipart   int
	releasedPins       int
	releasedWorkspaces int
}

func (receipt CleanupReceipt) AbortedArtifacts() []string {
	return append([]string(nil), receipt.abortedArtifacts...)
}

func (receipt CleanupReceipt) AbortedMultipart() int { return receipt.abortedMultipart }

func (receipt CleanupReceipt) ReleasedPins() int { return receipt.releasedPins }

func (receipt CleanupReceipt) ReleasedWorkspaces() int { return receipt.releasedWorkspaces }

type ArtifactStore interface {
	Stage(context.Context, ArtifactRef, io.Reader) error
	Publish(context.Context, ArtifactRef) error
	Abort(context.Context, ArtifactRef) error
	AbortMultipart(context.Context, ArtifactRef) error
	ReleasePin(context.Context, ArtifactRef) error
	ReleaseWorkspace(context.Context) error
}

type DatabaseSource interface {
	Dump(context.Context) (io.ReadCloser, ArtifactRef, error)
}

type ObjectInventory interface {
	List(context.Context) ([]ArtifactRef, error)
	Open(context.Context, ArtifactRef) (io.ReadCloser, error)
}

type Options struct {
	Store    ArtifactStore
	Database DatabaseSource
	Objects  ObjectInventory
	Now      func() time.Time
	Build    BuildIdentity
}

type BuildIdentity struct {
	Commit          string
	Version         string
	MigrationDigest [sha256.Size]byte
	AppACLDigest    [sha256.Size]byte
	Adapters        []AdapterRef
	Deletion        DeletionWatermark
	Profile         Profile
}

type Request struct {
	Profile Profile
}

func validArtifactKind(kind string) bool {
	switch kind {
	case "postgres_dump", "record_attachments", "record_evidence", "record_portability", "manifest":
		return true
	default:
		return false
	}
}

func validKeyVersion(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_':
		default:
			return false
		}
	}
	return true
}
