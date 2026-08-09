package attachments

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func TestAttachmentIdentityValidatorsAcceptCanonicalIDs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		value    string
		validate func(string) error
	}{
		{name: "attachment", value: "att_0123456789abcdef", validate: ValidateAttachmentID},
		{name: "upload", value: "aup_0123456789abcdef", validate: ValidateUploadID},
		{name: "processor job", value: "apj_0123456789abcdef", validate: ValidateProcessorJobID},
		{name: "workspace", value: "cpw_0123456789abcdef", validate: ValidateWorkspaceID},
		{name: "gc pin", value: "bgp_0123456789abcdef", validate: ValidateBlobGCPinID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := tt.validate(tt.value); err != nil {
				t.Fatalf("validate(%q) error = %v", tt.value, err)
			}
		})
	}
}

func TestAttachmentIdentityValidatorsRejectNonCanonicalIDs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		value    string
		validate func(string) error
		want     error
	}{
		{name: "empty attachment", validate: ValidateAttachmentID, want: ErrInvalidAttachmentID},
		{name: "wrong attachment prefix", value: "aup_0123", validate: ValidateAttachmentID, want: ErrInvalidAttachmentID},
		{name: "uppercase suffix", value: "att_ABC", validate: ValidateAttachmentID, want: ErrInvalidAttachmentID},
		{name: "underscore suffix", value: "att_abc_def", validate: ValidateAttachmentID, want: ErrInvalidAttachmentID},
		{name: "attachment suffix too long", value: "att_" + strings.Repeat("a", 65), validate: ValidateAttachmentID, want: ErrInvalidAttachmentID},
		{name: "empty upload", validate: ValidateUploadID, want: ErrInvalidUploadID},
		{name: "wrong processor prefix", value: "job_0123", validate: ValidateProcessorJobID, want: ErrInvalidProcessorJobID},
		{name: "workspace whitespace", value: "cpw_abc ", validate: ValidateWorkspaceID, want: ErrInvalidWorkspaceID},
		{name: "gc pin punctuation", value: "bgp_abc-123", validate: ValidateBlobGCPinID, want: ErrInvalidBlobGCPinID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := tt.validate(tt.value); !errors.Is(err, tt.want) {
				t.Fatalf("validate(%q) error = %v, want %v", tt.value, err, tt.want)
			}
		})
	}
}

func TestDefaultLimitsMatchParentContract(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	if limits.MaxFileBytes != 50*MiB || limits.MaxRecordBytes != 500*MiB ||
		limits.MaxProjectBytes != 10*GiB || limits.WarningPercent != 80 ||
		limits.MaxInlineTextPreviewBytes != 5*MiB {
		t.Fatalf("DefaultLimits() = %#v", limits)
	}
	if err := limits.Validate(); err != nil {
		t.Fatalf("DefaultLimits().Validate() error = %v", err)
	}
}

func TestLimitsValidateHierarchyAndWarning(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*Limits)
	}{
		{name: "zero file", mutate: func(limits *Limits) { limits.MaxFileBytes = 0 }},
		{name: "negative record", mutate: func(limits *Limits) { limits.MaxRecordBytes = -1 }},
		{name: "file exceeds record", mutate: func(limits *Limits) { limits.MaxFileBytes = limits.MaxRecordBytes + 1 }},
		{name: "record exceeds project", mutate: func(limits *Limits) { limits.MaxRecordBytes = limits.MaxProjectBytes + 1 }},
		{name: "preview exceeds file", mutate: func(limits *Limits) { limits.MaxInlineTextPreviewBytes = limits.MaxFileBytes + 1 }},
		{name: "zero warning", mutate: func(limits *Limits) { limits.WarningPercent = 0 }},
		{name: "warning above one hundred", mutate: func(limits *Limits) { limits.WarningPercent = 101 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			limits := DefaultLimits()
			tt.mutate(&limits)
			if err := limits.Validate(); !errors.Is(err, ErrInvalidLimits) {
				t.Fatalf("Limits.Validate() error = %v, want ErrInvalidLimits", err)
			}
		})
	}
}

func TestValidateUploadStateTransition(t *testing.T) {
	t.Parallel()

	valid := [][2]UploadState{
		{UploadStateCreated, UploadStateUploading},
		{UploadStateCreated, UploadStateExpired},
		{UploadStateUploading, UploadStateQuarantined},
		{UploadStateUploading, UploadStateRejected},
		{UploadStateUploading, UploadStateExpired},
		{UploadStateQuarantined, UploadStateAvailable},
		{UploadStateQuarantined, UploadStateRejected},
		{UploadStateQuarantined, UploadStateExpired},
	}
	for _, transition := range valid {
		if err := ValidateUploadStateTransition(transition[0], transition[1]); err != nil {
			t.Errorf("ValidateUploadStateTransition(%q, %q) error = %v", transition[0], transition[1], err)
		}
	}

	invalid := [][2]UploadState{
		{UploadStateCreated, UploadStateAvailable},
		{UploadStateUploading, UploadStateAvailable},
		{UploadStateQuarantined, UploadStateUploading},
		{UploadStateAvailable, UploadStateRejected},
		{UploadStateRejected, UploadStateUploading},
		{UploadStateExpired, UploadStateCreated},
		{UploadStateCreated, UploadStateCreated},
		{"unknown", UploadStateUploading},
	}
	for _, transition := range invalid {
		if err := ValidateUploadStateTransition(transition[0], transition[1]); !errors.Is(err, ErrInvalidUploadStateTransition) {
			t.Errorf("ValidateUploadStateTransition(%q, %q) error = %v, want ErrInvalidUploadStateTransition", transition[0], transition[1], err)
		}
	}
}

func TestRequireArchiveScannerFailsClosed(t *testing.T) {
	t.Parallel()

	if err := RequireArchiveScanner(ScannerStatusHealthy); err != nil {
		t.Fatalf("RequireArchiveScanner(healthy) error = %v", err)
	}
	for _, status := range []ScannerStatus{ScannerStatusUnconfigured, ScannerStatusUnhealthy} {
		if err := RequireArchiveScanner(status); !errors.Is(err, ErrArchiveScannerUnavailable) {
			t.Errorf("RequireArchiveScanner(%q) error = %v, want ErrArchiveScannerUnavailable", status, err)
		}
	}
	if err := RequireArchiveScanner("unknown"); !errors.Is(err, ErrInvalidScannerStatus) {
		t.Fatalf("RequireArchiveScanner(unknown) error = %v, want ErrInvalidScannerStatus", err)
	}
}

func TestEvaluateUploadReservationQuota(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	decision, err := EvaluateUploadReservationQuota(
		QuotaUsage{LogicalBytes: 7 * GiB, ReservedBytes: 1000 * MiB, PhysicalBytes: 3 * GiB},
		460*MiB,
		40*MiB,
		limits,
	)
	if err != nil {
		t.Fatalf("EvaluateUploadReservationQuota() error = %v", err)
	}
	if decision.ProjectReservedBytes != 1040*MiB || decision.EffectiveRecordBytes != 500*MiB ||
		!decision.ProjectWarning {
		t.Fatalf("EvaluateUploadReservationQuota() = %#v", decision)
	}

	for _, test := range []struct {
		name                 string
		usage                QuotaUsage
		effectiveRecordBytes int64
		requestedBytes       int64
		wantScope            QuotaScope
		wantError            error
	}{
		{name: "file", requestedBytes: limits.MaxFileBytes + 1, wantScope: QuotaScopeFile, wantError: ErrQuotaExceeded},
		{name: "record", effectiveRecordBytes: limits.MaxRecordBytes - 1, requestedBytes: 2, wantScope: QuotaScopeRecord, wantError: ErrQuotaExceeded},
		{name: "project", usage: QuotaUsage{LogicalBytes: limits.MaxProjectBytes - 1}, requestedBytes: 2, wantScope: QuotaScopeProject, wantError: ErrQuotaExceeded},
		{name: "negative persisted usage", usage: QuotaUsage{LogicalBytes: -1}, requestedBytes: 1, wantError: ErrInvalidQuotaUsage},
		{name: "overflow", usage: QuotaUsage{LogicalBytes: math.MaxInt64}, requestedBytes: 1, wantError: ErrQuotaOverflow},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := EvaluateUploadReservationQuota(test.usage, test.effectiveRecordBytes, test.requestedBytes, limits)
			if !errors.Is(err, test.wantError) {
				t.Fatalf("EvaluateUploadReservationQuota() error = %v, want %v", err, test.wantError)
			}
			if test.wantScope != "" {
				var exceeded *QuotaExceededError
				if !errors.As(err, &exceeded) || exceeded.Scope != test.wantScope {
					t.Fatalf("EvaluateUploadReservationQuota() error = %#v, want scope %q", err, test.wantScope)
				}
			}
		})
	}
}

func TestQuotaUsageSolidifyAndReleaseReservation(t *testing.T) {
	t.Parallel()

	usage := QuotaUsage{LogicalBytes: 10, ReservedBytes: 20, PhysicalBytes: 30}
	solidified, err := usage.SolidifyReservation(20, 15, 15)
	if err != nil {
		t.Fatalf("SolidifyReservation() error = %v", err)
	}
	if solidified != (QuotaUsage{LogicalBytes: 25, ReservedBytes: 0, PhysicalBytes: 45}) {
		t.Fatalf("SolidifyReservation() = %#v", solidified)
	}

	released, err := usage.ReleaseReservation(20)
	if err != nil {
		t.Fatalf("ReleaseReservation() error = %v", err)
	}
	if released != (QuotaUsage{LogicalBytes: 10, ReservedBytes: 0, PhysicalBytes: 30}) {
		t.Fatalf("ReleaseReservation() = %#v", released)
	}

	for _, test := range []struct {
		name string
		run  func() error
		want error
	}{
		{name: "solidify more than reserved", run: func() error {
			_, err := usage.SolidifyReservation(21, 15, 0)
			return err
		}, want: ErrInvalidQuotaUsage},
		{name: "release more than reserved", run: func() error {
			_, err := usage.ReleaseReservation(21)
			return err
		}, want: ErrInvalidQuotaUsage},
		{name: "logical overflow", run: func() error {
			_, err := (QuotaUsage{LogicalBytes: math.MaxInt64, ReservedBytes: 1}).SolidifyReservation(1, 1, 0)
			return err
		}, want: ErrQuotaOverflow},
		{name: "physical overflow", run: func() error {
			_, err := (QuotaUsage{ReservedBytes: 1, PhysicalBytes: math.MaxInt64}).SolidifyReservation(1, 1, 1)
			return err
		}, want: ErrQuotaOverflow},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := test.run(); !errors.Is(err, test.want) {
				t.Fatalf("quota mutation error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestReserveUploadCommandValidate(t *testing.T) {
	t.Parallel()

	valid := ReserveUploadCommand{
		ProjectID:         "default",
		UploadID:          "aup_reserve1",
		AttachmentID:      "att_reserve1",
		DraftID:           "rdf_reserve1",
		AuthorID:          "usr_reserve1",
		DisplayName:       "report.txt",
		MediaType:         "text/plain",
		TransportKind:     TransportKindLocal,
		DeclaredSizeBytes: 1024,
		ExpiresAt:         time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC),
		Limits:            DefaultLimits(),
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("ReserveUploadCommand.Validate() error = %v", err)
	}

	for _, test := range []struct {
		name   string
		mutate func(*ReserveUploadCommand)
	}{
		{name: "project", mutate: func(command *ReserveUploadCommand) { command.ProjectID = "other" }},
		{name: "upload id", mutate: func(command *ReserveUploadCommand) { command.UploadID = "upload_1" }},
		{name: "attachment id", mutate: func(command *ReserveUploadCommand) { command.AttachmentID = "attachment_1" }},
		{name: "draft id", mutate: func(command *ReserveUploadCommand) { command.DraftID = "draft_1" }},
		{name: "author id", mutate: func(command *ReserveUploadCommand) { command.AuthorID = "user_1" }},
		{name: "display name", mutate: func(command *ReserveUploadCommand) { command.DisplayName = "" }},
		{name: "media type", mutate: func(command *ReserveUploadCommand) { command.MediaType = "" }},
		{name: "transport", mutate: func(command *ReserveUploadCommand) { command.TransportKind = "unknown" }},
		{name: "declared bytes", mutate: func(command *ReserveUploadCommand) { command.DeclaredSizeBytes = 0 }},
		{name: "expires", mutate: func(command *ReserveUploadCommand) { command.ExpiresAt = time.Time{} }},
		{name: "limits", mutate: func(command *ReserveUploadCommand) { command.Limits.MaxFileBytes = 0 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			command := valid
			test.mutate(&command)
			if err := command.Validate(); !errors.Is(err, ErrInvalidAttachmentCommand) {
				t.Fatalf("ReserveUploadCommand.Validate() error = %v, want ErrInvalidAttachmentCommand", err)
			}
		})
	}
}

func TestCopyAttachmentCommandValidate(t *testing.T) {
	t.Parallel()

	valid := CopyAttachmentCommand{
		ProjectID:          "default",
		SourceRecordID:     "rec_source1",
		TargetRecordID:     "rec_target1",
		SourceAttachmentID: "att_source1",
		TargetAttachmentID: "att_target1",
		ActorID:            "usr_copy1",
		Limits:             DefaultLimits(),
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("CopyAttachmentCommand.Validate() error = %v", err)
	}

	for _, test := range []struct {
		name   string
		mutate func(*CopyAttachmentCommand)
	}{
		{name: "project", mutate: func(command *CopyAttachmentCommand) { command.ProjectID = "other" }},
		{name: "source record", mutate: func(command *CopyAttachmentCommand) { command.SourceRecordID = "record_source" }},
		{name: "target record", mutate: func(command *CopyAttachmentCommand) { command.TargetRecordID = "record_target" }},
		{name: "same record", mutate: func(command *CopyAttachmentCommand) { command.TargetRecordID = command.SourceRecordID }},
		{name: "source attachment", mutate: func(command *CopyAttachmentCommand) { command.SourceAttachmentID = "attachment_source" }},
		{name: "target attachment", mutate: func(command *CopyAttachmentCommand) { command.TargetAttachmentID = "attachment_target" }},
		{name: "same attachment", mutate: func(command *CopyAttachmentCommand) { command.TargetAttachmentID = command.SourceAttachmentID }},
		{name: "actor", mutate: func(command *CopyAttachmentCommand) { command.ActorID = "user_copy" }},
		{name: "limits", mutate: func(command *CopyAttachmentCommand) { command.Limits.MaxRecordBytes = 0 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			command := valid
			test.mutate(&command)
			if err := command.Validate(); !errors.Is(err, ErrInvalidAttachmentCommand) {
				t.Fatalf("CopyAttachmentCommand.Validate() error = %v, want ErrInvalidAttachmentCommand", err)
			}
		})
	}
}

func TestBlobGCPinCommandsValidate(t *testing.T) {
	t.Parallel()

	create := CreateBlobGCPinCommand{
		PinID:             "bgp_backup1",
		OwnerKind:         BlobGCPinOwnerBackupManifest,
		OwnerID:           "backup_2026-08-04",
		BlobKey:           "sha256/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		BlobObjectVersion: "local-v1",
		ExpiresAt:         time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC),
	}
	if err := create.Validate(); err != nil {
		t.Fatalf("CreateBlobGCPinCommand.Validate() error = %v", err)
	}
	for _, ownerKind := range []BlobGCPinOwnerKind{
		BlobGCPinOwnerBackupManifest,
		BlobGCPinOwnerRestoreAttempt,
		BlobGCPinOwnerImportPlan,
		BlobGCPinOwnerRevisionTransaction,
	} {
		command := create
		command.OwnerKind = ownerKind
		if err := command.Validate(); err != nil {
			t.Errorf("CreateBlobGCPinCommand.Validate(%q) error = %v", ownerKind, err)
		}
	}

	for _, test := range []struct {
		name   string
		mutate func(*CreateBlobGCPinCommand)
	}{
		{name: "pin id", mutate: func(command *CreateBlobGCPinCommand) { command.PinID = "pin_backup1" }},
		{name: "owner kind", mutate: func(command *CreateBlobGCPinCommand) { command.OwnerKind = "unknown" }},
		{name: "owner id", mutate: func(command *CreateBlobGCPinCommand) { command.OwnerID = "Backup 1" }},
		{name: "Blob key", mutate: func(command *CreateBlobGCPinCommand) { command.BlobKey = "sha256/short" }},
		{name: "Blob version", mutate: func(command *CreateBlobGCPinCommand) { command.BlobObjectVersion = "" }},
		{name: "expiry", mutate: func(command *CreateBlobGCPinCommand) { command.ExpiresAt = time.Time{} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			command := create
			test.mutate(&command)
			if err := command.Validate(); !errors.Is(err, ErrInvalidAttachmentCommand) {
				t.Fatalf("CreateBlobGCPinCommand.Validate() error = %v, want ErrInvalidAttachmentCommand", err)
			}
		})
	}

	release := ReleaseBlobGCPinCommand{
		PinID:             create.PinID,
		OwnerKind:         create.OwnerKind,
		OwnerID:           create.OwnerID,
		BlobKey:           create.BlobKey,
		BlobObjectVersion: create.BlobObjectVersion,
	}
	if err := release.Validate(); err != nil {
		t.Fatalf("ReleaseBlobGCPinCommand.Validate() error = %v", err)
	}
	protection := BlobProtectionCommand{BlobKey: create.BlobKey, BlobObjectVersion: create.BlobObjectVersion}
	if err := protection.Validate(); err != nil {
		t.Fatalf("BlobProtectionCommand.Validate() error = %v", err)
	}
}

func TestUploadLifecycleCommandsRequireConfiguredLimits(t *testing.T) {
	t.Parallel()

	digest := [32]byte{1}
	admit := AdmitUploadCommand{
		ProjectID: "default",
		UploadID:  "aup_admit1",
		AuthorID:  "usr_admit1",
		Blob: BlobObject{
			Key:           "sha256/0100000000000000000000000000000000000000000000000000000000000000",
			SHA256:        digest,
			ObjectVersion: "local-v1",
			SizeBytes:     1,
			BackendKind:   BackendKindLocal,
		},
		Limits: DefaultLimits(),
	}
	if err := admit.Validate(); err != nil {
		t.Fatalf("AdmitUploadCommand.Validate() error = %v", err)
	}
	admit.Limits = Limits{}
	if err := admit.Validate(); !errors.Is(err, ErrInvalidAttachmentCommand) {
		t.Fatalf("AdmitUploadCommand.Validate() limits error = %v", err)
	}

	fail := FailUploadCommand{
		ProjectID: "default", UploadID: "aup_fail1", AuthorID: "usr_fail1",
		TargetState: UploadStateRejected, Limits: DefaultLimits(),
	}
	if err := fail.Validate(); err != nil {
		t.Fatalf("FailUploadCommand.Validate() error = %v", err)
	}
	fail.Limits = Limits{}
	if err := fail.Validate(); !errors.Is(err, ErrInvalidAttachmentCommand) {
		t.Fatalf("FailUploadCommand.Validate() limits error = %v", err)
	}
}

func TestProjectQuotaSnapshotCommandValidate(t *testing.T) {
	t.Parallel()

	command := ProjectQuotaSnapshotCommand{ProjectID: "default", Limits: DefaultLimits()}
	if err := command.Validate(); err != nil {
		t.Fatalf("ProjectQuotaSnapshotCommand.Validate() error = %v", err)
	}
	command.ProjectID = "other"
	if err := command.Validate(); !errors.Is(err, ErrInvalidAttachmentCommand) {
		t.Fatalf("ProjectQuotaSnapshotCommand.Validate() project error = %v", err)
	}
	command = ProjectQuotaSnapshotCommand{ProjectID: "default"}
	if err := command.Validate(); !errors.Is(err, ErrInvalidAttachmentCommand) {
		t.Fatalf("ProjectQuotaSnapshotCommand.Validate() limits error = %v", err)
	}
}
