package records

import (
	"errors"
	"testing"
	"time"

	"houfeng/internal/center/recordauth"
)

func TestDraftRoutingAcceptsRecordIdentityDomains(t *testing.T) {
	valid := DraftRouting{
		DraftID: "rdf_draft", ProjectID: recordauth.ProjectIDDefault,
		BaseRevisionID: "rrv_revision", AuthorID: "usr_0123456789abcdef01234567",
		UpdatedAt: time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC),
	}
	for _, recordID := range []string{
		"rec_legacy",
		"rec2_70000000000000000000000000000000",
		"rec2_80000000000000000000000000000000",
	} {
		routing := valid
		routing.RecordID = recordID
		if err := routing.Validate(); err != nil {
			t.Fatalf("DraftRouting.Validate(%q) error = %v", recordID, err)
		}
	}
	valid.RecordID = "rec2_70000000000000000000000000000000"
	for _, mutate := range []func(*DraftRouting){
		func(routing *DraftRouting) { routing.RecordID = "rec2_7000000000000000000000000000000g" },
		func(routing *DraftRouting) { routing.RecordID = "rrv_revision" },
		func(routing *DraftRouting) { routing.DraftID = valid.RecordID },
		func(routing *DraftRouting) { routing.BaseRevisionID = valid.RecordID },
	} {
		routing := valid
		mutate(&routing)
		if err := routing.Validate(); !errors.Is(err, ErrInvalidDraftCommand) {
			t.Fatalf("invalid DraftRouting.Validate() error = %v", err)
		}
	}
}

func TestSearchRebuildCheckpointAcceptsRecordIdentityDomains(t *testing.T) {
	for _, recordID := range []string{
		"rec_legacy",
		"rec2_70000000000000000000000000000000",
		"rec2_80000000000000000000000000000000",
	} {
		job := RevisionMutationSearchRebuildJob{ProjectID: "default", JobID: "rsj_rebuild", ResumeAfterRecordID: recordID}
		resources, err := CombineRevisionMutationResources(RevisionMutationResources{SearchRebuildJobs: []RevisionMutationSearchRebuildJob{job}})
		if err != nil || len(resources.SearchRebuildJobs) != 1 || resources.SearchRebuildJobs[0].ResumeAfterRecordID != recordID {
			t.Fatalf("rebuild resources = %#v, %v", resources.SearchRebuildJobs, err)
		}
	}
	for _, job := range []RevisionMutationSearchRebuildJob{
		{ProjectID: "default", JobID: "rsj_rebuild", ResumeAfterRecordID: "rec2_7000000000000000000000000000000g"},
		{ProjectID: "default", JobID: "rec2_70000000000000000000000000000000", ResumeAfterRecordID: "rec_legacy"},
	} {
		if _, err := CombineRevisionMutationResources(RevisionMutationResources{SearchRebuildJobs: []RevisionMutationSearchRebuildJob{job}}); !errors.Is(err, ErrInvalidRevisionMutationResources) {
			t.Fatalf("invalid rebuild resources error = %v", err)
		}
	}
}
