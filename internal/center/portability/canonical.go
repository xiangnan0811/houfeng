package portability

import (
	"bytes"
	"encoding/json"
	"fmt"
)

func canonicalManifestJSON(manifest ArchiveManifest) ([]byte, error) {
	if err := manifest.validate(); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(manifest)
	if err != nil || len(encoded) == 0 {
		return nil, fmt.Errorf("%w: canonical manifest", ErrInvalidArchive)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, encoded); err != nil {
		return nil, fmt.Errorf("%w: compact manifest", ErrInvalidArchive)
	}
	return compact.Bytes(), nil
}

func parseArchiveManifest(raw []byte) (ArchiveManifest, error) {
	var manifest ArchiveManifest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return ArchiveManifest{}, fmt.Errorf("%w: decode manifest", ErrInvalidArchive)
	}
	if err := manifest.validate(); err != nil {
		return ArchiveManifest{}, err
	}
	canonical, err := canonicalManifestJSON(manifest)
	if err != nil {
		return ArchiveManifest{}, err
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return ArchiveManifest{}, fmt.Errorf("%w: compact incoming manifest", ErrInvalidArchive)
	}
	if !bytes.Equal(canonical, compact.Bytes()) {
		return ArchiveManifest{}, fmt.Errorf("%w: non-canonical manifest", ErrInvalidArchive)
	}
	return manifest, nil
}
