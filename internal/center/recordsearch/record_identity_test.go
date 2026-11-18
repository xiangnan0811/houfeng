package recordsearch

import (
	"errors"
	"testing"
)

func TestSearchProjectionAcceptsRecordIdentityDomains(t *testing.T) {
	for _, recordID := range []string{
		"rec_legacy",
		"rec2_70000000000000000000000000000000",
		"rec2_80000000000000000000000000000000",
	} {
		values := testProjectionFacts()
		values.RecordID = recordID
		facts, err := NormalizeDocumentFacts(values)
		if err != nil || facts.RecordID() != recordID {
			t.Fatalf("NormalizeDocumentFacts(%q) = %q, %v", recordID, facts.RecordID(), err)
		}
	}
	values := testProjectionFacts()
	values.RecordID = "rec2_70000000000000000000000000000000"
	values.CurrentRevisionID = values.RecordID
	if _, err := NormalizeDocumentFacts(values); !errors.Is(err, ErrInvalidProjection) {
		t.Fatalf("record as revision error = %v", err)
	}
	values.CurrentRevisionID = "rrv_revision"
	values.RecordID = "rec2_7000000000000000000000000000000g"
	if _, err := NormalizeDocumentFacts(values); !errors.Is(err, ErrInvalidProjection) {
		t.Fatalf("invalid record error = %v", err)
	}
}
