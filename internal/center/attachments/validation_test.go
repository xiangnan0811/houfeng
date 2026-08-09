package attachments

import (
	"errors"
	"testing"
)

func TestNormalizeAttachmentReferencesPreservesOrderAndIsDefensive(t *testing.T) {
	t.Parallel()

	values := []AttachmentReference{
		{AttachmentID: "att_1111111111111111"},
		{AttachmentID: "att_2222222222222222"},
	}
	got, err := NormalizeAttachmentReferences(values)
	if err != nil {
		t.Fatalf("NormalizeAttachmentReferences() error = %v", err)
	}
	if len(got) != 2 || got[0].AttachmentID != values[0].AttachmentID || got[1].AttachmentID != values[1].AttachmentID {
		t.Fatalf("NormalizeAttachmentReferences() = %#v", got)
	}

	values[0].AttachmentID = "att_3333333333333333"
	if got[0].AttachmentID != "att_1111111111111111" {
		t.Fatalf("normalized references changed through input mutation: %#v", got)
	}
	got[1].AttachmentID = "att_4444444444444444"
	again, err := NormalizeAttachmentReferences([]AttachmentReference{
		{AttachmentID: "att_1111111111111111"},
		{AttachmentID: "att_2222222222222222"},
	})
	if err != nil || again[1].AttachmentID != "att_2222222222222222" {
		t.Fatalf("normalization retained caller mutation: %#v, %v", again, err)
	}
}

func TestNormalizeAttachmentReferencesReturnsNonNilEmptySlice(t *testing.T) {
	t.Parallel()

	got, err := NormalizeAttachmentReferences(nil)
	if err != nil {
		t.Fatalf("NormalizeAttachmentReferences(nil) error = %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("NormalizeAttachmentReferences(nil) = %#v, want non-nil empty slice", got)
	}
}

func TestNormalizeAttachmentReferencesRejectsInvalidAndDuplicateIDs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		values []AttachmentReference
	}{
		{name: "invalid", values: []AttachmentReference{{AttachmentID: "attachment_1"}}},
		{name: "duplicate", values: []AttachmentReference{
			{AttachmentID: "att_1111111111111111"},
			{AttachmentID: "att_1111111111111111"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NormalizeAttachmentReferences(tt.values); !errors.Is(err, ErrInvalidAttachmentReferences) {
				t.Fatalf("NormalizeAttachmentReferences() error = %v, want ErrInvalidAttachmentReferences", err)
			}
		})
	}
}
