package attachments

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func TestImportedAvailableAttachmentValidateRejectsArchiveClaims(t *testing.T) {
	t.Parallel()
	item := mustImportedAvailableAttachment(t, "att_imported00001", "notes.txt", []byte("hello notes\n"))
	item.MediaType = ""
	if item.Validate() == nil {
		t.Fatal("empty media type validated")
	}
}

func TestImportBlobGCObservationValidation(t *testing.T) {
	t.Parallel()
	item := mustImportedAvailableAttachment(t, "att_imported00001", "notes.txt", []byte("hello notes\n"))
	if _, err := NewImportBlobGCObservation(item.Object.SHA256, -1); err == nil {
		t.Fatal("negative history count accepted")
	}
	for _, count := range []int64{0, 1, 9223372036854775807} {
		observation, err := NewImportBlobGCObservation(item.Object.SHA256, count)
		if err != nil || observation.DeletionCount() != count || observation.Validate(item.Object) != nil {
			t.Fatalf("valid observation %d rejected: %v", count, err)
		}
	}
	for name, mutate := range map[string]func(*ImportedAvailableAttachment){
		"zero_token": func(item *ImportedAvailableAttachment) { item.GCObservation = ImportBlobGCObservation{} },
		"wrong_digest_token": func(item *ImportedAvailableAttachment) {
			item.GCObservation, _ = NewImportBlobGCObservation(sha256.Sum256([]byte("other")), 0)
		},
		"noncanonical_key": func(item *ImportedAvailableAttachment) { item.Object.Key = "sha256/not-the-digest" },
		"invalid_object":   func(item *ImportedAvailableAttachment) { item.Object.VersionID = "" },
	} {
		t.Run(name, func(t *testing.T) {
			bad := item
			mutate(&bad)
			if bad.Validate() == nil {
				t.Fatal("invalid import accepted")
			}
			if bad.GCObservation.Validate(bad.Object) == nil {
				t.Fatal("invalid observation/object pair accepted")
			}
		})
	}
	encoded, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	if _, exists := fields["GCObservation"]; exists {
		t.Fatal("internal observation serialized")
	}
}

func mustImportedAvailableAttachment(t *testing.T, attachmentID, displayName string, payload []byte) ImportedAvailableAttachment {
	t.Helper()
	digest := sha256.Sum256(payload)
	observation, err := NewImportBlobGCObservation(digest, 0)
	if err != nil {
		t.Fatal(err)
	}
	item := ImportedAvailableAttachment{
		AttachmentID: attachmentID, DisplayName: displayName, MediaType: "text/plain", LogicalSizeBytes: int64(len(payload)),
		Object:      ObjectVersion{Key: "sha256/" + hex.EncodeToString(digest[:]), VersionID: "import-v1", SHA256: digest, SizeBytes: int64(len(payload))},
		BackendKind: BackendKindLocal, CreatedBy: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", GCObservation: observation,
	}
	if err := item.Validate(); err != nil {
		t.Fatalf("fixture Validate() = %v", err)
	}
	return item
}
