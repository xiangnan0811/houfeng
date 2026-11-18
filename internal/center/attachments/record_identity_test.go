package attachments

import (
	"errors"
	"testing"
)

func TestRecordOwnedContentAcceptsRecordIdentityDomains(t *testing.T) {
	for _, recordID := range []string{
		"rec_download1",
		"rec2_70000000000000000000000000000000",
		"rec2_80000000000000000000000000000000",
	} {
		t.Run(recordID, func(t *testing.T) {
			content := downloadRecordAttachment()
			content.RecordID = recordID
			if err := content.Validate(); err != nil {
				t.Fatalf("AttachmentContent.Validate() error = %v", err)
			}
			assertion := ContentAssertion{
				ProjectID: content.ProjectID, AttachmentID: content.AttachmentID,
				RecordID: recordID, AuthorID: content.AuthorID,
				Variant: ContentVariantOriginal, Object: content.Original,
			}
			if err := assertion.Validate(); err != nil {
				t.Fatalf("ContentAssertion.Validate() error = %v", err)
			}
			assertion.RecordID = ""
			assertion.DraftID = recordID
			if err := assertion.Validate(); !errors.Is(err, ErrInvalidDownloadRequest) {
				t.Fatalf("record as draft error = %v", err)
			}
		})
	}
	invalid := downloadRecordAttachment()
	invalid.RecordID = "rec2_7000000000000000000000000000000g"
	if err := invalid.Validate(); !errors.Is(err, ErrInvalidDownloadContent) {
		t.Fatalf("invalid record owner error = %v", err)
	}
}
