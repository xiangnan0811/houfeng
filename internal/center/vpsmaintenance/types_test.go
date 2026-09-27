package vpsmaintenance

import (
	"errors"
	"testing"
)

func TestSelectMaintenanceConsentAndStalePreview(t *testing.T) {
	r := Review{VPSID: "v1", Lifecycle: "active", Resources: []Resource{
		{Kind: "monitoring_instance", ResourceID: "m1", Eligible: true},
		{Kind: "target", ResourceID: "exclusive", Eligible: true},
		{Kind: "target", ResourceID: "shared", Shared: true, Eligible: true},
		{Kind: "target", ResourceID: "paused", Eligible: false},
	}}
	i := StartInput{Reason: "maintenance", PreviewDigest: r.Digest()}
	selected, err := Select(r, i)
	if err != nil || len(selected) != 2 {
		t.Fatalf("default selected=%v error=%v", selected, err)
	}
	i.ConfirmedSharedTargetIDs = []string{"shared"}
	selected, err = Select(r, i)
	if err != nil || len(selected) != 3 {
		t.Fatalf("confirmed selected=%v error=%v", selected, err)
	}
	i.ConfirmedSharedTargetIDs = []string{"exclusive"}
	if _, err = Select(r, i); !errors.Is(err, ErrInvalid) {
		t.Fatalf("non-shared confirmation=%v", err)
	}
	i.ConfirmedSharedTargetIDs = nil
	r.Resources[1].Shared = true
	if _, err = Select(r, i); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed ownership=%v", err)
	}
	i.PreviewDigest = r.Digest()
	r.Resources[0].ControlRevision++
	if _, err = Select(r, i); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed manual control=%v", err)
	}
}
