package recorddeletion

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
)

func TestBackupInventorySnapshotValidation(t *testing.T) {
	t.Parallel()

	digest := deletionTestDigest(0xab)
	zeroDigest := [32]byte{}
	empty := ManagedBackupSummary{}
	emptyWithRetention := ManagedBackupSummary{MaximumRetentionDays: 30}
	retained := ManagedBackupSummary{
		RetainedCopyCount: 2, MaximumRetentionDays: 30,
		LatestExpiresAt: time.Date(2026, time.November, 1, 12, 0, 0, 0, time.UTC),
	}
	expiryWithoutCopies := retained
	expiryWithoutCopies.RetainedCopyCount = 0
	missingDays := retained
	missingDays.MaximumRetentionDays = 0
	missingExpiry := retained
	missingExpiry.LatestExpiresAt = time.Time{}
	tests := []struct {
		name     string
		snapshot BackupInventorySnapshot
		valid    bool
	}{
		{"available known empty", BackupInventorySnapshot{BackupInventoryAvailable, &digest, &empty}, true},
		{"empty preserves retention policy", BackupInventorySnapshot{BackupInventoryAvailable, &digest, &emptyWithRetention}, true},
		{"available retained", BackupInventorySnapshot{BackupInventoryAvailable, &digest, &retained}, true},
		{"explicit unavailable", BackupInventorySnapshot{Availability: BackupInventoryUnavailable}, true},
		{"zero value", BackupInventorySnapshot{}, false},
		{"unknown availability", BackupInventorySnapshot{Availability: "unknown"}, false},
		{"noncanonical availability", BackupInventorySnapshot{Availability: "AVAILABLE"}, false},
		{"available no values", BackupInventorySnapshot{Availability: BackupInventoryAvailable}, false},
		{"available missing digest", BackupInventorySnapshot{Availability: BackupInventoryAvailable, ManagedBackup: &empty}, false},
		{"available missing summary", BackupInventorySnapshot{Availability: BackupInventoryAvailable, InventoryDigest: &digest}, false},
		{"available zero digest", BackupInventorySnapshot{BackupInventoryAvailable, &zeroDigest, &empty}, false},
		{"expiry without copies", BackupInventorySnapshot{BackupInventoryAvailable, &digest, &expiryWithoutCopies}, false},
		{"copies without days", BackupInventorySnapshot{BackupInventoryAvailable, &digest, &missingDays}, false},
		{"copies without expiry", BackupInventorySnapshot{BackupInventoryAvailable, &digest, &missingExpiry}, false},
		{"unavailable digest only", BackupInventorySnapshot{Availability: BackupInventoryUnavailable, InventoryDigest: &digest}, false},
		{"unavailable summary only", BackupInventorySnapshot{Availability: BackupInventoryUnavailable, ManagedBackup: &empty}, false},
		{"unavailable both values", BackupInventorySnapshot{BackupInventoryUnavailable, &digest, &empty}, false},
		{"unavailable zero digest pointer", BackupInventorySnapshot{Availability: BackupInventoryUnavailable, InventoryDigest: &zeroDigest}, false},
		{"unknown with available values", BackupInventorySnapshot{"unknown", &digest, &empty}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.snapshot.Validate()
			if tt.valid {
				if err != nil {
					t.Fatalf("Validate() = %v", err)
				}
			} else if !errors.Is(err, ErrDeletionSafetyUnavailable) {
				t.Fatalf("Validate() = %v, want ErrDeletionSafetyUnavailable", err)
			}
		})
	}
}

func TestUnavailableDeletionBackupInventorySourceChecksContextAndObject(t *testing.T) {
	t.Parallel()

	source := UnavailableDeletionBackupInventorySource{}
	object := issuerTestObject()
	got, err := source.Snapshot(context.Background(), object)
	if err != nil || got.Availability != BackupInventoryUnavailable || got.InventoryDigest != nil || got.ManagedBackup != nil {
		t.Fatalf("Snapshot() = %#v, %v, want explicit unavailable", got, err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, cancelExpired := context.WithDeadline(context.Background(), time.Unix(0, 1))
	defer cancelExpired()
	tests := []struct {
		name   string
		ctx    context.Context
		object recordplatform.ObjectRef
		want   error
	}{
		{"nil context", nil, object, ErrDeletionSafetyUnavailable},
		{"canceled", canceled, object, context.Canceled},
		{"expired", expired, object, context.DeadlineExceeded},
		{"empty object", context.Background(), recordplatform.ObjectRef{}, ErrDeletionSafetyUnavailable},
		{"wrong project", context.Background(), recordplatform.ObjectRef{ProjectID: "other", ObjectKind: "record", ObjectID: "rec_01"}, ErrDeletionSafetyUnavailable},
		{"wrong kind", context.Background(), recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "draft", ObjectID: "rec_01"}, ErrDeletionSafetyUnavailable},
		{"wrong record identity", context.Background(), recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: "rrv_01"}, ErrDeletionSafetyUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := source.Snapshot(tt.ctx, tt.object)
			if !errors.Is(err, tt.want) || got != (BackupInventorySnapshot{}) {
				t.Fatalf("Snapshot() = %#v, %v, want zero snapshot and %v", got, err, tt.want)
			}
			if tt.want == context.Canceled || tt.want == context.DeadlineExceeded {
				if err != tt.want {
					t.Fatalf("context error was changed: %v", err)
				}
			}
		})
	}
}

func TestReadDeletionBackupInventoryPreservesErrorsAndCancellation(t *testing.T) {
	t.Parallel()

	for _, providerErr := range []error{
		context.Canceled, context.DeadlineExceeded,
		errors.New("backup identity mismatch"), errors.New("backup parse failed"), errors.New("backup database unavailable"),
	} {
		t.Run(providerErr.Error(), func(t *testing.T) {
			source := issuerBackupInventorySourceFunc(func(context.Context, recordplatform.ObjectRef) (BackupInventorySnapshot, error) {
				// Even a simultaneous unavailable result cannot swallow the error.
				return BackupInventorySnapshot{Availability: BackupInventoryUnavailable}, providerErr
			})
			got, err := ReadDeletionBackupInventory(context.Background(), source, issuerTestObject())
			if err != providerErr || got != (BackupInventorySnapshot{}) {
				t.Fatalf("ReadDeletionBackupInventory() = %#v, %v, want original error %v", got, err, providerErr)
			}
		})
	}

	t.Run("already canceled never calls provider", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		source := issuerBackupInventorySourceFunc(func(context.Context, recordplatform.ObjectRef) (BackupInventorySnapshot, error) {
			t.Fatal("provider called after cancellation")
			return BackupInventorySnapshot{}, nil
		})
		if _, err := ReadDeletionBackupInventory(ctx, source, issuerTestObject()); err != context.Canceled {
			t.Fatalf("ReadDeletionBackupInventory() = %v, want context.Canceled", err)
		}
	})

	t.Run("canceled during provider never becomes unavailable", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		source := issuerBackupInventorySourceFunc(func(context.Context, recordplatform.ObjectRef) (BackupInventorySnapshot, error) {
			cancel()
			return BackupInventorySnapshot{Availability: BackupInventoryUnavailable}, nil
		})
		got, err := ReadDeletionBackupInventory(ctx, source, issuerTestObject())
		if err != context.Canceled || got != (BackupInventorySnapshot{}) {
			t.Fatalf("ReadDeletionBackupInventory() = %#v, %v", got, err)
		}
	})

	t.Run("provider error takes precedence over simultaneous cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		providerErr := errors.New("backup identity mismatch")
		source := issuerBackupInventorySourceFunc(func(context.Context, recordplatform.ObjectRef) (BackupInventorySnapshot, error) {
			cancel()
			return BackupInventorySnapshot{}, providerErr
		})
		if _, err := ReadDeletionBackupInventory(ctx, source, issuerTestObject()); err != providerErr {
			t.Fatalf("ReadDeletionBackupInventory() = %v, want unchanged provider error", err)
		}
	})
}

func TestReadDeletionBackupInventoryValidatesAndOwnsProviderValues(t *testing.T) {
	t.Parallel()

	object := issuerTestObject()
	original := issuerTestAvailableBackup()
	source := issuerBackupInventorySourceFunc(func(ctx context.Context, got recordplatform.ObjectRef) (BackupInventorySnapshot, error) {
		if ctx == nil || got != object {
			t.Fatalf("Snapshot request = %v, %#v", ctx, got)
		}
		return original, nil
	})
	got, err := ReadDeletionBackupInventory(context.Background(), source, object)
	if err != nil || !reflect.DeepEqual(got, original) {
		t.Fatalf("ReadDeletionBackupInventory() = %#v, %v, want %#v", got, err, original)
	}
	*original.InventoryDigest = deletionTestDigest(99)
	original.ManagedBackup.RetainedCopyCount++
	if *got.InventoryDigest == *original.InventoryDigest || got.ManagedBackup.RetainedCopyCount == original.ManagedBackup.RetainedCopyCount {
		t.Fatal("provider mutations changed the returned snapshot")
	}

	var typedNil *UnavailableDeletionBackupInventorySource
	var nilFunction issuerBackupInventorySourceFunc
	for _, source := range []DeletionBackupInventorySource{nil, typedNil, nilFunction} {
		if _, err := ReadDeletionBackupInventory(context.Background(), source, object); !errors.Is(err, ErrDeletionSafetyUnavailable) {
			t.Fatalf("nil source error = %v, want ErrDeletionSafetyUnavailable", err)
		}
	}
	invalid := issuerBackupInventorySourceFunc(func(context.Context, recordplatform.ObjectRef) (BackupInventorySnapshot, error) {
		return BackupInventorySnapshot{}, nil
	})
	if _, err := ReadDeletionBackupInventory(context.Background(), invalid, object); !errors.Is(err, ErrDeletionSafetyUnavailable) {
		t.Fatalf("invalid provider snapshot error = %v, want ErrDeletionSafetyUnavailable", err)
	}
	got, err = ReadDeletionBackupInventory(context.Background(), UnavailableDeletionBackupInventorySource{}, object)
	if err != nil || got.Availability != BackupInventoryUnavailable || got.ManagedBackup != nil || got.InventoryDigest != nil {
		t.Fatalf("explicit unavailable provider = %#v, %v", got, err)
	}
}

func TestBackupInventoryJSONDistinguishesKnownEmptyFromUnknown(t *testing.T) {
	t.Parallel()

	digest := [32]byte{}
	for index := range digest {
		digest[index] = byte(0xa0 + index)
	}
	empty := ManagedBackupSummary{MaximumRetentionDays: 30}
	retained := ManagedBackupSummary{
		RetainedCopyCount: 2, MaximumRetentionDays: 30,
		LatestExpiresAt: time.Date(2026, time.November, 1, 14, 0, 0, 123000000, time.FixedZone("plus-two", 2*60*60)),
	}
	tests := []struct {
		name     string
		snapshot BackupInventorySnapshot
		want     string
	}{
		{
			name:     "unknown",
			snapshot: BackupInventorySnapshot{Availability: BackupInventoryUnavailable},
			want:     `{"availability":"unavailable","inventory_digest":null,"managed_backup":null}`,
		},
		{
			name:     "known empty retains days",
			snapshot: BackupInventorySnapshot{BackupInventoryAvailable, &digest, &empty},
			want:     `{"availability":"available","inventory_digest":"a0a1a2a3a4a5a6a7a8a9aaabacadaeafb0b1b2b3b4b5b6b7b8b9babbbcbdbebf","managed_backup":{"retained_copy_count":0,"maximum_retention_days":30,"latest_expires_at":null}}`,
		},
		{
			name:     "known retained UTC expiry",
			snapshot: BackupInventorySnapshot{BackupInventoryAvailable, &digest, &retained},
			want:     `{"availability":"available","inventory_digest":"a0a1a2a3a4a5a6a7a8a9aaabacadaeafb0b1b2b3b4b5b6b7b8b9babbbcbdbebf","managed_backup":{"retained_copy_count":2,"maximum_retention_days":30,"latest_expires_at":"2026-11-01T12:00:00.123Z"}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.snapshot)
			if err != nil || string(got) != tt.want {
				t.Fatalf("Marshal() = %s, %v; want %s", got, err, tt.want)
			}
		})
	}
	if _, err := json.Marshal(BackupInventorySnapshot{}); !errors.Is(err, ErrDeletionSafetyUnavailable) {
		t.Fatalf("invalid snapshot Marshal() = %v, want ErrDeletionSafetyUnavailable", err)
	}
}

func TestIssuerPreviewSummaryJSONDisclosesOnlineOnlyAndUnknownBackups(t *testing.T) {
	t.Parallel()

	summary := issuerTestSummary(BackupInventorySnapshot{Availability: BackupInventoryUnavailable})
	got, err := json.Marshal(summary)
	want := `{"online_purge_scopes":["record_core","record_attachments","record_evidence","record_markdown_client","record_search","record_activity_projection","record_comparison","record_collaboration","record_portability"],"surviving_copies":[],"backup_inventory":{"availability":"unavailable","inventory_digest":null,"managed_backup":null},"completion_scope":"online_only","disclosure":"Backup inventory is unknown. This operation clears online data only; it does not promise deletion of backups."}`
	if err != nil || string(got) != want {
		t.Fatalf("Marshal() = %s, %v; want %s", got, err, want)
	}

	summary.BackupInventory = issuerTestAvailableBackup()
	summary.SurvivingCopies = []SurvivingCopySummary{
		KnownSurvivingCopySummary(AdapterNameRecordAttachments, SurvivingCopyKindOtherRecord, 2),
		UnknownSurvivingCopySummary(AdapterNameRecordMarkdownClient, SurvivingCopyKindOfflineBrowserBuffer),
	}
	got, err = json.Marshal(summary)
	if err != nil {
		t.Fatalf("known summary Marshal() = %v", err)
	}
	for _, required := range []string{
		`"scope":"record_attachments","kind":"other_record","count_status":"known","copy_count":2`,
		`"scope":"record_markdown_client","kind":"offline_browser_buffer","count_status":"unknown","copy_count":null`,
		`"completion_scope":"online_only"`,
		`"disclosure":"This operation clears online data only; it does not promise deletion of backups."`,
	} {
		if !strings.Contains(string(got), required) {
			t.Fatalf("known summary JSON %s lacks %s", got, required)
		}
	}
	if strings.Contains(string(got), "ledger_health") || strings.Contains(string(got), "Backup inventory is unknown") {
		t.Fatalf("known summary misrepresents ledger or inventory: %s", got)
	}
}

func TestIssuerPreviewSummaryRejectsInvalidDisclosures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*IssuerPreviewSummary)
		want   error
	}{
		{"missing completion scope", func(s *IssuerPreviewSummary) { s.CompletionScope = "" }, ErrInvalidDeletionPreview},
		{"backup completion promise", func(s *IssuerPreviewSummary) { s.CompletionScope = "all_copies" }, ErrInvalidDeletionPreview},
		{"missing adapter", func(s *IssuerPreviewSummary) { s.OnlinePurgeScopes = s.OnlinePurgeScopes[:8] }, ErrInvalidDeletionPreview},
		{"nil surviving copies", func(s *IssuerPreviewSummary) { s.SurvivingCopies = nil }, ErrInvalidDeletionPreview},
		{"unknown inventory status", func(s *IssuerPreviewSummary) { s.BackupInventory.Availability = "unknown" }, ErrDeletionSafetyUnavailable},
		{"wrong unknown owner", func(s *IssuerPreviewSummary) {
			s.SurvivingCopies = []SurvivingCopySummary{UnknownSurvivingCopySummary(AdapterNameRecordAttachments, SurvivingCopyKindOfflineBrowserBuffer)}
		}, ErrInvalidDeletionPreview},
		{"duplicate surviving kind", func(s *IssuerPreviewSummary) {
			copy := KnownSurvivingCopySummary(AdapterNameRecordAttachments, SurvivingCopyKindOtherRecord, 1)
			s.SurvivingCopies = []SurvivingCopySummary{copy, copy}
		}, ErrInvalidDeletionPreview},
		{"out of order surviving owner", func(s *IssuerPreviewSummary) {
			s.SurvivingCopies = []SurvivingCopySummary{
				UnknownSurvivingCopySummary(AdapterNameRecordMarkdownClient, SurvivingCopyKindOfflineBrowserBuffer),
				KnownSurvivingCopySummary(AdapterNameRecordAttachments, SurvivingCopyKindOtherRecord, 1),
			}
		}, ErrInvalidDeletionPreview},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary := issuerTestSummary(BackupInventorySnapshot{Availability: BackupInventoryUnavailable})
			tt.mutate(&summary)
			if err := summary.Validate(); !errors.Is(err, tt.want) {
				t.Fatalf("Validate() = %v, want %v", err, tt.want)
			}
			if _, err := json.Marshal(summary); !errors.Is(err, tt.want) {
				t.Fatalf("Marshal() = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestIssuerPreviewBindingDetectsOnlineAndBackupDrift(t *testing.T) {
	t.Parallel()

	baseline := issuerTestBinding(t)
	expected, err := DigestIssuerPreviewBinding(baseline)
	if err != nil {
		t.Fatalf("DigestIssuerPreviewBinding() = %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*IssuerPreviewBinding)
	}{
		{"actor", func(b *IssuerPreviewBinding) { b.Actor = deletionTestActor(t, "usr_bbbbbbbbbbbbbbbbbbbbbbbb") }},
		{"actor groups", func(b *IssuerPreviewBinding) { b.Actor.GroupIDs = []string{"rag_alpha"} }},
		{"record identity", func(b *IssuerPreviewBinding) { b.Object.ObjectID = "rec_02" }},
		{"revision", func(b *IssuerPreviewBinding) { b.CurrentRevisionID = "rrv_02" }},
		{"lock version", func(b *IssuerPreviewBinding) { b.LockVersion++ }},
		{"authorization epoch", func(b *IssuerPreviewBinding) { b.AuthorizationEpoch++ }},
		{"delivery epoch", func(b *IssuerPreviewBinding) { b.ContentDeliveryEpoch++ }},
		{"lifecycle", func(b *IssuerPreviewBinding) { b.Lifecycle = "trashed" }},
		{"authorization", func(b *IssuerPreviewBinding) {
			visibility := b.Authorization.Visibility
			visibility.PolicyRevision++
			normalized, err := recordauth.NormalizeVisibilityScope(visibility)
			if err != nil {
				t.Fatalf("NormalizeVisibilityScope() = %v", err)
			}
			b.Authorization.Visibility = normalized
		}},
		{"source authorization", func(b *IssuerPreviewBinding) {
			source := b.Authorization.Sources[0]
			current := *source.CurrentScope
			current.PolicyRevision++
			normalized, err := recordauth.NormalizeVisibilityScope(current)
			if err != nil {
				t.Fatalf("NormalizeVisibilityScope() = %v", err)
			}
			source.CurrentScope = &normalized
			source, err = recordauth.NormalizeSourceAuthorization(source)
			if err != nil {
				t.Fatalf("NormalizeSourceAuthorization() = %v", err)
			}
			b.Authorization.Sources = []recordauth.SourceAuthorization{source}
		}},
		{"readiness", func(b *IssuerPreviewBinding) { b.AdapterReadinessDigest[0]++ }},
		{"adapter impact", func(b *IssuerPreviewBinding) { b.AdapterPreviewDigest[0]++ }},
		{"dependency inventory", func(b *IssuerPreviewBinding) { b.DependencyGraphDigest[0]++ }},
		{"processor inventory", func(b *IssuerPreviewBinding) { b.ProcessorInventoryDigest[0]++ }},
		{"obligation inventory", func(b *IssuerPreviewBinding) { b.ObligationInventoryDigest[0]++ }},
		{"available to unavailable", func(b *IssuerPreviewBinding) {
			b.BackupInventory = BackupInventorySnapshot{Availability: BackupInventoryUnavailable}
		}},
		{"backup inventory digest", func(b *IssuerPreviewBinding) { b.BackupInventory.InventoryDigest[0]++ }},
		{"backup count", func(b *IssuerPreviewBinding) { b.BackupInventory.ManagedBackup.RetainedCopyCount++ }},
		{"backup days", func(b *IssuerPreviewBinding) { b.BackupInventory.ManagedBackup.MaximumRetentionDays++ }},
		{"backup expiry", func(b *IssuerPreviewBinding) {
			b.BackupInventory.ManagedBackup.LatestExpiresAt = b.BackupInventory.ManagedBackup.LatestExpiresAt.Add(time.Nanosecond)
		}},
		{"known empty", func(b *IssuerPreviewBinding) { b.BackupInventory.ManagedBackup = &ManagedBackupSummary{} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			current := issuerTestBinding(t)
			tt.mutate(&current)
			if err := CheckIssuerPreviewBinding(expected, current); !errors.Is(err, ErrDeletionPreviewStale) {
				t.Fatalf("CheckIssuerPreviewBinding() = %v, want ErrDeletionPreviewStale", err)
			}
		})
	}

	t.Run("unavailable to available", func(t *testing.T) {
		unknown := issuerTestBinding(t)
		unknown.BackupInventory = BackupInventorySnapshot{Availability: BackupInventoryUnavailable}
		unknownDigest, err := DigestIssuerPreviewBinding(unknown)
		if err != nil {
			t.Fatalf("DigestIssuerPreviewBinding() = %v", err)
		}
		if err := CheckIssuerPreviewBinding(unknownDigest, baseline); err != ErrDeletionPreviewStale {
			t.Fatalf("CheckIssuerPreviewBinding() = %v, want ErrDeletionPreviewStale", err)
		}
		knownEmpty := issuerTestBinding(t)
		knownEmpty.BackupInventory.ManagedBackup = &ManagedBackupSummary{}
		if err := CheckIssuerPreviewBinding(unknownDigest, knownEmpty); err != ErrDeletionPreviewStale {
			t.Fatalf("unknown to known empty = %v, want ErrDeletionPreviewStale", err)
		}
	})
}

func TestIssuerPreviewBindingReplaysSameValuesAndCanonicalEquivalents(t *testing.T) {
	t.Parallel()

	for _, availability := range []BackupInventoryAvailability{BackupInventoryAvailable, BackupInventoryUnavailable} {
		t.Run(string(availability), func(t *testing.T) {
			binding := issuerTestBinding(t)
			binding.Actor.GroupIDs = []string{"rag_beta", "rag_alpha", "rag_beta"}
			if availability == BackupInventoryUnavailable {
				binding.BackupInventory = BackupInventorySnapshot{Availability: availability}
			}
			expected, err := DigestIssuerPreviewBinding(binding)
			if err != nil {
				t.Fatalf("DigestIssuerPreviewBinding() = %v", err)
			}
			if err := CheckIssuerPreviewBinding(expected, binding); err != nil {
				t.Fatalf("same-value replay = %v", err)
			}
			binding.Actor.GroupIDs = []string{"rag_alpha", "rag_beta"}
			if binding.BackupInventory.ManagedBackup != nil {
				binding.BackupInventory.ManagedBackup.LatestExpiresAt = binding.BackupInventory.ManagedBackup.LatestExpiresAt.In(time.FixedZone("plus-two", 2*60*60))
			}
			if err := CheckIssuerPreviewBinding(expected, binding); err != nil {
				t.Fatalf("canonical actor/UTC replay = %v", err)
			}
		})
	}
	t.Run("known empty replay and retention drift", func(t *testing.T) {
		binding := issuerTestBinding(t)
		binding.BackupInventory.ManagedBackup = &ManagedBackupSummary{MaximumRetentionDays: 30}
		expected, err := DigestIssuerPreviewBinding(binding)
		if err != nil {
			t.Fatalf("DigestIssuerPreviewBinding() = %v", err)
		}
		sameValues := issuerTestBinding(t)
		sameValues.BackupInventory.ManagedBackup = &ManagedBackupSummary{MaximumRetentionDays: 30}
		if err := CheckIssuerPreviewBinding(expected, sameValues); err != nil {
			t.Fatalf("known-empty same-value replay = %v", err)
		}
		sameValues.BackupInventory.ManagedBackup.MaximumRetentionDays++
		if err := CheckIssuerPreviewBinding(expected, sameValues); err != ErrDeletionPreviewStale {
			t.Fatalf("known-empty retention drift = %v, want ErrDeletionPreviewStale", err)
		}
	})

	binding := issuerTestBinding(t)
	record := deletionTestRecordSnapshot(t)
	legacy := digestPreviewBinding(binding.Actor, binding.Object, record, binding.AdapterReadinessDigest, binding.AdapterPreviewDigest, deletionTestWitnessHead(1))
	if err := CheckIssuerPreviewBinding(legacy, binding); err != ErrDeletionPreviewStale {
		t.Fatalf("legacy digest interpreted as V2: %v", err)
	}
}

func TestIssuerPreviewBindingFailsClosedOnInvalidFacts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*IssuerPreviewBinding)
		want   error
	}{
		{"missing actor", func(b *IssuerPreviewBinding) { b.Actor = recordauth.ActorScope{} }, ErrInvalidDeletionPreview},
		{"invalid actor identity", func(b *IssuerPreviewBinding) { b.Actor.UserID = "" }, recordauth.ErrDenied},
		{"revoked actor", func(b *IssuerPreviewBinding) { b.Actor.Role = recordauth.RoleViewer }, recordauth.ErrDenied},
		{"invalid source authorization", func(b *IssuerPreviewBinding) { b.Authorization.Sources = nil }, recordauth.ErrDenied},
		{"project mismatch", func(b *IssuerPreviewBinding) { b.Authorization.ProjectID = "other" }, ErrInvalidDeletionPreview},
		{"invalid object", func(b *IssuerPreviewBinding) { b.Object.ObjectKind = "draft" }, ErrInvalidDeletionPreview},
		{"invalid revision", func(b *IssuerPreviewBinding) { b.CurrentRevisionID = "rec_01" }, ErrInvalidDeletionPreview},
		{"archived", func(b *IssuerPreviewBinding) { b.Lifecycle = "archived" }, ErrInvalidDeletionPreview},
		{"unknown lifecycle", func(b *IssuerPreviewBinding) { b.Lifecycle = "" }, ErrInvalidDeletionPreview},
		{"missing CAS", func(b *IssuerPreviewBinding) { b.LockVersion = 0 }, ErrInvalidDeletionPreview},
		{"missing authorization epoch", func(b *IssuerPreviewBinding) { b.AuthorizationEpoch = 0 }, ErrInvalidDeletionPreview},
		{"unknown readiness", func(b *IssuerPreviewBinding) { b.AdapterReadinessDigest = [32]byte{} }, ErrInvalidDeletionPreview},
		{"unknown adapter coverage", func(b *IssuerPreviewBinding) { b.AdapterPreviewDigest = [32]byte{} }, ErrInvalidDeletionPreview},
		{"unknown dependencies", func(b *IssuerPreviewBinding) { b.DependencyGraphDigest = [32]byte{} }, ErrInvalidDeletionPreview},
		{"unknown processors", func(b *IssuerPreviewBinding) { b.ProcessorInventoryDigest = [32]byte{} }, ErrInvalidDeletionPreview},
		{"unknown obligations", func(b *IssuerPreviewBinding) { b.ObligationInventoryDigest = [32]byte{} }, ErrInvalidDeletionPreview},
		{"missing backup contract", func(b *IssuerPreviewBinding) { b.BackupInventory = BackupInventorySnapshot{} }, ErrDeletionSafetyUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			binding := issuerTestBinding(t)
			tt.mutate(&binding)
			digest, err := DigestIssuerPreviewBinding(binding)
			if !errors.Is(err, tt.want) || digest != ([sha256.Size]byte{}) {
				t.Fatalf("DigestIssuerPreviewBinding() = %x, %v, want zero and %v", digest, err, tt.want)
			}
			if err := CheckIssuerPreviewBinding(deletionTestDigest(1), binding); !errors.Is(err, tt.want) {
				t.Fatalf("CheckIssuerPreviewBinding() = %v, want %v rather than a stale or successful comparison", err, tt.want)
			}
		})
	}
	if err := CheckIssuerPreviewBinding([32]byte{}, issuerTestBinding(t)); !errors.Is(err, ErrInvalidDeletionPreview) {
		t.Fatalf("empty stored commitment = %v, want ErrInvalidDeletionPreview", err)
	}
}

func issuerTestObject() recordplatform.ObjectRef {
	return recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: "rec_01"}
}

func issuerTestAvailableBackup() BackupInventorySnapshot {
	digest := deletionTestDigest(0xab)
	backup := ManagedBackupSummary{
		RetainedCopyCount: 2, MaximumRetentionDays: 30,
		LatestExpiresAt: time.Date(2026, time.November, 1, 12, 0, 0, 0, time.UTC),
	}
	return BackupInventorySnapshot{BackupInventoryAvailable, &digest, &backup}
}

func issuerTestSummary(backup BackupInventorySnapshot) IssuerPreviewSummary {
	return IssuerPreviewSummary{
		OnlinePurgeScopes: RequiredAdapterNames(), SurvivingCopies: []SurvivingCopySummary{},
		BackupInventory: backup, CompletionScope: IssuerCompletionScopeOnlineOnly,
	}
}

func issuerTestBinding(t *testing.T) IssuerPreviewBinding {
	t.Helper()
	record := deletionTestRecordSnapshot(t)
	return IssuerPreviewBinding{
		Actor: deletionTestActor(t, "usr_aaaaaaaaaaaaaaaaaaaaaaaa"), Object: issuerTestObject(), Lifecycle: "active",
		CurrentRevisionID: record.CurrentRevisionID, LockVersion: record.LockVersion,
		AuthorizationEpoch: record.AuthorizationEpoch, ContentDeliveryEpoch: record.ContentDeliveryEpoch,
		Authorization:          record.Authorization,
		AdapterReadinessDigest: deletionTestDigest(21), AdapterPreviewDigest: deletionTestDigest(22),
		DependencyGraphDigest: record.DependencyGraphDigest, ProcessorInventoryDigest: record.ProcessorInventoryDigest,
		ObligationInventoryDigest: deletionTestDigest(23), BackupInventory: issuerTestAvailableBackup(),
	}
}

type issuerBackupInventorySourceFunc func(context.Context, recordplatform.ObjectRef) (BackupInventorySnapshot, error)

func (source issuerBackupInventorySourceFunc) Snapshot(ctx context.Context, object recordplatform.ObjectRef) (BackupInventorySnapshot, error) {
	return source(ctx, object)
}

func TestDigestIssuerAdapterSnapshotsMatchesRegistry(t *testing.T) {
	registry, adapters := deletionTestRegistry(t)
	count := uint64(3)
	deletionTestAdapterStub(t, adapters, AdapterNameRecordAttachments).preview.SurvivingCopies = []AdapterSurvivingCopy{
		{Kind: SurvivingCopyKindOtherRecord, CountStatus: SurvivingCopyCountKnown, CopyCount: &count},
	}
	deletionTestAdapterStub(t, adapters, AdapterNameRecordMarkdownClient).preview.SurvivingCopies = []AdapterSurvivingCopy{
		UnknownAdapterSurvivingCopy(SurvivingCopyKindOfflineBrowserBuffer),
	}
	health := make(map[AdapterName]AdapterHealthSnapshot)
	previews := make(map[AdapterName]AdapterPreviewSnapshot)
	for _, adapter := range adapters {
		health[adapter.name] = adapter.health
		previews[adapter.name] = adapter.preview
	}
	wantReadiness, err := registry.RequireReady(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wantPreview, err := registry.previewSnapshot(context.Background(), deletionTestPreviewTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	readiness, preview, summary, err := DigestIssuerAdapterSnapshots(health, previews)
	if err != nil || readiness != wantReadiness.Digest() || preview != wantPreview.digest ||
		!reflect.DeepEqual(summary.OnlinePurgeScopes, wantPreview.onlinePurgeScopes) ||
		!reflect.DeepEqual(summary.SurvivingCopies, wantPreview.survivingCopies) || summary.CompletionScope != IssuerCompletionScopeOnlineOnly {
		t.Fatalf("issuer snapshots readiness=%x preview=%x summary=%+v error=%v", readiness, preview, summary, err)
	}
	count++
	if *summary.SurvivingCopies[0].CopyCount != 3 {
		t.Fatal("issuer summary aliases adapter count")
	}
}

func TestDigestIssuerAdapterSnapshotsRejectsInvalidInputs(t *testing.T) {
	_, adapters := deletionTestRegistry(t)
	for _, tc := range []struct {
		name   string
		mutate func(map[AdapterName]AdapterHealthSnapshot, map[AdapterName]AdapterPreviewSnapshot)
		want   error
	}{
		{"missing health", func(h map[AdapterName]AdapterHealthSnapshot, _ map[AdapterName]AdapterPreviewSnapshot) {
			delete(h, AdapterNameRecordCore)
		}, ErrDeletionSafetyUnavailable},
		{"extra health", func(h map[AdapterName]AdapterHealthSnapshot, _ map[AdapterName]AdapterPreviewSnapshot) {
			h["extra"] = h[AdapterNameRecordCore]
		}, ErrDeletionSafetyUnavailable},
		{"invalid health", func(h map[AdapterName]AdapterHealthSnapshot, _ map[AdapterName]AdapterPreviewSnapshot) {
			h[AdapterNameRecordCore] = AdapterHealthSnapshot{}
		}, ErrDeletionSafetyUnavailable},
		{"unhealthy", func(h map[AdapterName]AdapterHealthSnapshot, _ map[AdapterName]AdapterPreviewSnapshot) {
			h[AdapterNameRecordCore] = mustHealthSnapshot(t, false, 1)
		}, ErrDeletionSafetyUnavailable},
		{"missing preview", func(_ map[AdapterName]AdapterHealthSnapshot, p map[AdapterName]AdapterPreviewSnapshot) {
			delete(p, AdapterNameRecordCore)
		}, ErrInvalidDeletionPreview},
		{"extra preview", func(_ map[AdapterName]AdapterHealthSnapshot, p map[AdapterName]AdapterPreviewSnapshot) {
			p["extra"] = p[AdapterNameRecordCore]
		}, ErrInvalidDeletionPreview},
		{"invalid preview", func(_ map[AdapterName]AdapterHealthSnapshot, p map[AdapterName]AdapterPreviewSnapshot) {
			p[AdapterNameRecordCore] = AdapterPreviewSnapshot{}
		}, ErrInvalidDeletionPreview},
		{"wrong surviving copy owner", func(_ map[AdapterName]AdapterHealthSnapshot, p map[AdapterName]AdapterPreviewSnapshot) {
			v := p[AdapterNameRecordCore]
			v.SurvivingCopies = []AdapterSurvivingCopy{UnknownAdapterSurvivingCopy(SurvivingCopyKindOfflineBrowserBuffer)}
			p[AdapterNameRecordCore] = v
		}, ErrInvalidDeletionPreview},
	} {
		t.Run(tc.name, func(t *testing.T) {
			health := make(map[AdapterName]AdapterHealthSnapshot)
			previews := make(map[AdapterName]AdapterPreviewSnapshot)
			for _, adapter := range adapters {
				health[adapter.name], previews[adapter.name] = adapter.health, adapter.preview
			}
			tc.mutate(health, previews)
			readiness, preview, summary, err := DigestIssuerAdapterSnapshots(health, previews)
			if !errors.Is(err, tc.want) || readiness != ([32]byte{}) || preview != ([32]byte{}) || !reflect.DeepEqual(summary, IssuerPreviewSummary{}) {
				t.Fatalf("readiness=%x preview=%x summary=%+v error=%v, want %v", readiness, preview, summary, err, tc.want)
			}
		})
	}
}
