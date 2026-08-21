package recordbackup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"houfeng/internal/center/recordreadiness"
)

type encodedManifest struct {
	Format           string            `json:"format"`
	MinReaderVersion uint32            `json:"min_reader_version"`
	BuildCommit      string            `json:"build_commit"`
	BuildVersion     string            `json:"build_version"`
	MigrationDigest  string            `json:"migration_digest"`
	AppACLDigest     string            `json:"app_acl_digest"`
	Adapters         []encodedAdapter  `json:"adapters"`
	Database         encodedArtifact   `json:"database"`
	Objects          []encodedArtifact `json:"objects"`
	Deletion         encodedDeletion   `json:"deletion"`
	CreatedUnix      int64             `json:"created_unix"`
	Profile          Profile           `json:"profile"`
	CompletionDigest string            `json:"completion_digest,omitempty"`
}

type encodedAdapter struct {
	Kind    recordreadiness.CapabilityKind `json:"kind"`
	Version uint32                         `json:"version"`
}

type encodedArtifact struct {
	Kind           string         `json:"kind"`
	KeyVersion     string         `json:"key_version"`
	Digest         string         `json:"digest"`
	Size           uint64         `json:"size"`
	Classification Classification `json:"classification"`
}

type encodedDeletion struct {
	Sequence uint64 `json:"sequence"`
	Digest   string `json:"digest"`
}

func (manifest Manifest) Encode() ([]byte, error) {
	payload, err := manifest.canonicalPayload()
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("%w: encode", ErrInvalidBackupRequest)
	}
	digest := sha256.Sum256(body)
	payload.CompletionDigest = hex.EncodeToString(digest[:])
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("%w: encode", ErrInvalidBackupRequest)
	}
	return encoded, nil
}

func (manifest Manifest) canonicalPayload() (encodedManifest, error) {
	if manifest.format != ManifestFormatV1 || manifest.minReaderVersion != ManifestReaderVersionV1 {
		return encodedManifest{}, ErrUnknownManifestVersion
	}
	adapters := append([]AdapterRef(nil), manifest.adapters...)
	sort.Slice(adapters, func(i, j int) bool {
		return adapters[i].kind < adapters[j].kind
	})
	objects := append([]ArtifactRef(nil), manifest.objects...)
	sort.Slice(objects, func(i, j int) bool {
		if objects[i].kind == objects[j].kind {
			return objects[i].keyVersion < objects[j].keyVersion
		}
		return objects[i].kind < objects[j].kind
	})
	encodedAdapters := make([]encodedAdapter, 0, len(adapters))
	for _, adapter := range adapters {
		encodedAdapters = append(encodedAdapters, encodedAdapter{Kind: adapter.kind, Version: adapter.version})
	}
	encodedObjects := make([]encodedArtifact, 0, len(objects))
	for _, object := range objects {
		encodedObjects = append(encodedObjects, encodeArtifact(object))
	}
	return encodedManifest{
		Format:           ManifestFormatV1,
		MinReaderVersion: ManifestReaderVersionV1,
		BuildCommit:      manifest.buildCommit,
		BuildVersion:     manifest.buildVersion,
		MigrationDigest:  hex.EncodeToString(manifest.migrationDigest[:]),
		AppACLDigest:     hex.EncodeToString(manifest.appACLDigest[:]),
		Adapters:         encodedAdapters,
		Database:         encodeArtifact(manifest.database),
		Objects:          encodedObjects,
		Deletion: encodedDeletion{
			Sequence: manifest.deletion.sequence,
			Digest:   hex.EncodeToString(manifest.deletion.digest[:]),
		},
		CreatedUnix: manifest.createdUnix,
		Profile:     manifest.profile,
	}, nil
}

func encodeArtifact(artifact ArtifactRef) encodedArtifact {
	return encodedArtifact{
		Kind:           artifact.kind,
		KeyVersion:     artifact.keyVersion,
		Digest:         hex.EncodeToString(artifact.digest[:]),
		Size:           artifact.size,
		Classification: artifact.classification,
	}
}

func DecodeManifest(payload []byte) (Manifest, error) {
	var encoded encodedManifest
	if err := json.Unmarshal(payload, &encoded); err != nil {
		return Manifest{}, fmt.Errorf("%w: json", ErrTamperedManifest)
	}
	if encoded.Format != ManifestFormatV1 || encoded.MinReaderVersion != ManifestReaderVersionV1 {
		return Manifest{}, ErrUnknownManifestVersion
	}
	manifest, err := manifestFromEncoded(encoded)
	if err != nil {
		return Manifest{}, err
	}
	reencoded, err := manifest.Encode()
	if err != nil {
		return Manifest{}, err
	}
	var expected encodedManifest
	if err := json.Unmarshal(reencoded, &expected); err != nil {
		return Manifest{}, fmt.Errorf("%w: json", ErrTamperedManifest)
	}
	if encoded.CompletionDigest != expected.CompletionDigest || encoded.Database.Digest != expected.Database.Digest {
		return Manifest{}, ErrTamperedManifest
	}
	digest, err := parseDigest(expected.CompletionDigest)
	if err != nil {
		return Manifest{}, err
	}
	manifest.completionDigest = digest
	return manifest, nil
}

func manifestFromEncoded(encoded encodedManifest) (Manifest, error) {
	migration, err := parseDigest(encoded.MigrationDigest)
	if err != nil {
		return Manifest{}, err
	}
	acl, err := parseDigest(encoded.AppACLDigest)
	if err != nil {
		return Manifest{}, err
	}
	database, err := artifactFromEncoded(encoded.Database)
	if err != nil {
		return Manifest{}, err
	}
	objects := make([]ArtifactRef, 0, len(encoded.Objects))
	for _, object := range encoded.Objects {
		ref, err := artifactFromEncoded(object)
		if err != nil {
			return Manifest{}, err
		}
		objects = append(objects, ref)
	}
	adapters := make([]AdapterRef, 0, len(encoded.Adapters))
	for _, adapter := range encoded.Adapters {
		ref, err := NewAdapterRef(adapter.Kind, adapter.Version)
		if err != nil {
			return Manifest{}, err
		}
		adapters = append(adapters, ref)
	}
	deletionDigest, err := parseDigest(encoded.Deletion.Digest)
	if err != nil {
		return Manifest{}, err
	}
	deletion, err := NewDeletionWatermark(encoded.Deletion.Sequence, deletionDigest)
	if err != nil {
		return Manifest{}, err
	}
	return NewManifest(ManifestInput{
		BuildCommit:     encoded.BuildCommit,
		BuildVersion:    encoded.BuildVersion,
		MigrationDigest: migration,
		AppACLDigest:    acl,
		Adapters:        adapters,
		Database:        database,
		Objects:         objects,
		Deletion:        deletion,
		CreatedAt:       unixTime(encoded.CreatedUnix),
		Profile:         encoded.Profile,
	})
}

func artifactFromEncoded(encoded encodedArtifact) (ArtifactRef, error) {
	digest, err := parseDigest(encoded.Digest)
	if err != nil {
		return ArtifactRef{}, err
	}
	return NewArtifactRef(encoded.Kind, encoded.KeyVersion, digest, encoded.Size, encoded.Classification)
}

func parseDigest(value string) ([sha256.Size]byte, error) {
	var digest [sha256.Size]byte
	raw, err := hex.DecodeString(value)
	if err != nil || len(raw) != sha256.Size {
		return digest, ErrTamperedManifest
	}
	copy(digest[:], raw)
	return digest, nil
}

func bindCompletion(manifest Manifest) (Manifest, error) {
	encoded, err := manifest.Encode()
	if err != nil {
		return Manifest{}, err
	}
	var payload encodedManifest
	if err := json.Unmarshal(encoded, &payload); err != nil {
		return Manifest{}, fmt.Errorf("%w: json", ErrInvalidBackupRequest)
	}
	digest, err := parseDigest(payload.CompletionDigest)
	if err != nil {
		return Manifest{}, err
	}
	manifest.completionDigest = digest
	return manifest, nil
}
