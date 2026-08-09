package attachments

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"

	"houfeng/internal/center/recordauth"
)

func TestRecordUploadedContentCommandRequiresPublishedIntent(t *testing.T) {
	t.Parallel()

	digest := sha256.Sum256([]byte("upload publication intent is required"))
	object := ObjectVersion{
		Key: "sha256/" + hexDigest(digest), VersionID: "upload-publication-v1",
		SHA256: digest, SizeBytes: int64(len("upload publication intent is required")),
	}
	command := RecordUploadedContentCommand{
		ProjectID: "default", UploadID: "aup_publicationintent1", AuthorID: "usr_publicationintent",
		Object: object,
	}
	if err := command.Validate(); !errors.Is(err, ErrInvalidAttachmentCommand) {
		t.Fatalf("zero publication intent validation error = %v, want ErrInvalidAttachmentCommand", err)
	}
}

func TestUploadServiceCreateAuthorizesDraftBeforeQuotaReservationAndReturnsLocalTarget(t *testing.T) {
	t.Parallel()

	fixture := newUploadServiceFixture(t, TransportKindLocal)
	result, err := fixture.service.CreateUpload(context.Background(), validCreateUploadRequest())
	if err != nil {
		t.Fatalf("CreateUpload() error = %v", err)
	}
	if !reflect.DeepEqual(fixture.events, []string{"authorize", "reserve"}) {
		t.Fatalf("CreateUpload() events = %#v, want authorize before reserve", fixture.events)
	}
	if result.UploadID != "aup_service1" || result.AttachmentID != "att_service1" ||
		result.State != UploadStateCreated || result.Target.TransportKind != TransportKindLocal ||
		result.Target.UploadID != "aup_service1" || result.Target.TemporaryObjectKey != "" ||
		result.Quota.Usage.ReservedBytes != int64(len(uploadServiceContent)) {
		t.Fatalf("CreateUpload() = %#v", result)
	}
}

func TestUploadServiceCreateArchiveDeclarationRequiresScannerBeforeAuthorizationReservationOrContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		displayName string
		mediaType   string
		configured  bool
		status      ScannerStatus
	}{
		{
			name:        "unconfigured extension declaration",
			displayName: "bundle.zip", mediaType: "text/plain",
		},
		{
			name:        "unconfigured media declaration",
			displayName: "notes.txt", mediaType: "application/x-tar",
			configured: true, status: ScannerStatusUnconfigured,
		},
		{
			name:        "unhealthy matching declaration",
			displayName: "bundle.zst", mediaType: "application/zstd",
			configured: true, status: ScannerStatusUnhealthy,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var readiness func(context.Context) (ScannerStatus, error)
			if tt.configured {
				readiness = func(context.Context) (ScannerStatus, error) {
					return tt.status, nil
				}
			}
			fixture := newUploadServiceFixtureWithArchiveScannerReadiness(t, TransportKindLocal, readiness)
			request := validCreateUploadRequest()
			request.DisplayName = tt.displayName
			request.MediaType = tt.mediaType

			_, err := fixture.service.CreateUpload(context.Background(), request)

			if !errors.Is(err, ErrArchiveScannerUnavailable) {
				t.Fatalf("CreateUpload() error = %v, want ErrArchiveScannerUnavailable", err)
			}
			if fixture.repository.reserveCalls != 0 {
				t.Fatalf("ReserveUpload calls = %d, want 0", fixture.repository.reserveCalls)
			}
			if len(fixture.events) != 0 {
				t.Fatalf("CreateUpload() events = %#v, want no authorization, reservation, or content action", fixture.events)
			}
			if fixture.blob.putCalls != 0 || fixture.blob.statCalls != 0 || fixture.blob.openCalls != 0 ||
				fixture.blob.openTemporaryCalls != 0 || fixture.blob.publishTemporaryCalls != 0 ||
				len(fixture.blob.deletedTemporaryVersions) != 0 {
				t.Fatal("CreateUpload() performed a content read or mutation before scanner readiness")
			}
		})
	}
}

func TestUploadServiceCreateChecksArchiveScannerReadinessLive(t *testing.T) {
	t.Parallel()

	status := ScannerStatusHealthy
	readinessCalls := 0
	fixture := newUploadServiceFixtureWithArchiveScannerReadiness(t, TransportKindLocal, func(context.Context) (ScannerStatus, error) {
		readinessCalls++
		return status, nil
	})
	request := validCreateUploadRequest()
	request.DisplayName = "bundle.tar"
	request.MediaType = "application/x-tar"

	if _, err := fixture.service.CreateUpload(context.Background(), request); err != nil {
		t.Fatalf("CreateUpload(healthy scanner) error = %v", err)
	}
	if readinessCalls != 1 || fixture.repository.reserveCalls != 1 ||
		!reflect.DeepEqual(fixture.events, []string{"authorize", "reserve"}) {
		t.Fatalf("CreateUpload(healthy scanner) readiness/reserve/events = %d/%d/%#v",
			readinessCalls, fixture.repository.reserveCalls, fixture.events)
	}

	status = ScannerStatusUnhealthy
	fixture.events = nil
	_, err := fixture.service.CreateUpload(context.Background(), request)
	if !errors.Is(err, ErrArchiveScannerUnavailable) {
		t.Fatalf("CreateUpload(unhealthy scanner) error = %v, want ErrArchiveScannerUnavailable", err)
	}
	if readinessCalls != 2 || fixture.repository.reserveCalls != 1 || len(fixture.events) != 0 {
		t.Fatalf("CreateUpload(unhealthy scanner) readiness/reserve/events = %d/%d/%#v",
			readinessCalls, fixture.repository.reserveCalls, fixture.events)
	}
}

func TestUploadServiceCreateRejectsUnauthorizedDraftBeforeReservationOrObjectAction(t *testing.T) {
	t.Parallel()

	fixture := newUploadServiceFixture(t, TransportKindS3)
	fixture.authorizer.err = recordauth.ErrDenied
	_, err := fixture.service.CreateUpload(context.Background(), validCreateUploadRequest())
	if !errors.Is(err, recordauth.ErrDenied) {
		t.Fatalf("CreateUpload() error = %v, want recordauth.ErrDenied", err)
	}
	if !reflect.DeepEqual(fixture.events, []string{"authorize"}) {
		t.Fatalf("CreateUpload() unauthorized events = %#v", fixture.events)
	}
}

func TestUploadServiceCreatePersistsRandomS3TemporaryKeyBeforeReturningInstructions(t *testing.T) {
	t.Parallel()

	fixture := newUploadServiceFixture(t, TransportKindS3)
	result, err := fixture.service.CreateUpload(context.Background(), validCreateUploadRequest())
	if err != nil {
		t.Fatalf("CreateUpload() error = %v", err)
	}
	if !reflect.DeepEqual(fixture.events, []string{"authorize", "reserve", "prepare", "presign"}) {
		t.Fatalf("CreateUpload() events = %#v", fixture.events)
	}
	if fixture.repository.prepareCommands[0].CandidateTemporaryObjectKey != uploadServiceTemporaryKey1 {
		t.Fatalf("PrepareUpload() candidate key = %q", fixture.repository.prepareCommands[0].CandidateTemporaryObjectKey)
	}
	if fixture.blob.presignTemporaryKey != uploadServiceTemporaryKey1 ||
		fixture.blob.presignTTL <= 0 || fixture.blob.presignTTL > time.Hour {
		t.Fatalf("PresignTemporaryUpload() key/TTL = %q/%s, want persisted key and bounded positive TTL",
			fixture.blob.presignTemporaryKey, fixture.blob.presignTTL)
	}
	if result.State != UploadStateUploading || result.Target.TransportKind != TransportKindS3 ||
		result.Target.TemporaryObjectKey != uploadServiceTemporaryKey1 ||
		result.Target.UploadURL != uploadServicePresignedURL || result.Target.Method != "PUT" ||
		result.Target.RequiredHeaders == nil {
		t.Fatalf("CreateUpload() S3 target = %#v", result)
	}
}

func TestUploadServiceCreateTreatsSubSecondRemainingLifetimeAsExpiredBeforePresign(t *testing.T) {
	t.Parallel()

	fixture := newUploadServiceFixture(t, TransportKindS3)
	clockValues := []time.Time{
		uploadServiceNow,
		fixture.repository.preparation.ExpiresAt.Add(-500 * time.Millisecond),
	}
	fixture.service.now = func() time.Time {
		value := clockValues[0]
		clockValues = clockValues[1:]
		return value
	}

	_, err := fixture.service.CreateUpload(context.Background(), validCreateUploadRequest())
	if !errors.Is(err, ErrUploadExpired) {
		t.Fatalf("CreateUpload(sub-second remaining) error = %v, want ErrUploadExpired", err)
	}
	if !reflect.DeepEqual(fixture.events, []string{"authorize", "reserve", "prepare"}) ||
		len(fixture.repository.prepareCommands) != 1 {
		t.Fatalf("CreateUpload(sub-second remaining) events/prepare = %#v/%d, want persisted preparation",
			fixture.events, len(fixture.repository.prepareCommands))
	}
	if fixture.blob.presignCalls != 0 {
		t.Fatalf("PresignTemporaryUpload calls = %d, want 0", fixture.blob.presignCalls)
	}
}

func TestUploadServiceCreateRetryRecoversPersistedS3TemporaryKey(t *testing.T) {
	t.Parallel()

	fixture := newUploadServiceFixture(t, TransportKindS3)
	first, err := fixture.service.CreateUpload(context.Background(), validCreateUploadRequest())
	if err != nil {
		t.Fatalf("CreateUpload(first) error = %v", err)
	}
	fixture.events = nil
	fixture.repository.reservation.State = UploadStateUploading
	second, err := fixture.service.CreateUpload(context.Background(), validCreateUploadRequest())
	if err != nil {
		t.Fatalf("CreateUpload(retry) error = %v", err)
	}
	if !reflect.DeepEqual(first, second) || second.Target.TemporaryObjectKey != uploadServiceTemporaryKey1 {
		t.Fatalf("CreateUpload(first/retry) = %#v/%#v", first, second)
	}
	if !reflect.DeepEqual(fixture.events, []string{"authorize", "reserve", "prepare", "presign"}) {
		t.Fatalf("CreateUpload(retry) events = %#v", fixture.events)
	}
	if got := fixture.repository.prepareCommands[1].CandidateTemporaryObjectKey; got != uploadServiceTemporaryKey2 {
		t.Fatalf("CreateUpload(retry) candidate key = %q", got)
	}
}

func TestUploadServicePutContentReusesPersistedS3KeyBeforeBlobPutAndRecordsExactFinalIdentity(t *testing.T) {
	t.Parallel()

	fixture := newUploadServiceFixture(t, TransportKindS3)
	fixture.repository.preparation.TemporaryObjectKey = uploadServiceTemporaryKey1
	fixture.repository.preparation.State = UploadStateUploading
	fixture.temporaryKeys = []string{uploadServiceTemporaryKey2}

	result, err := fixture.service.PutContent(context.Background(), validPutUploadContentRequest())
	if err != nil {
		t.Fatalf("PutContent() error = %v", err)
	}
	if !reflect.DeepEqual(fixture.events, []string{"authorize", "prepare", "resolve_temp", "blob_put", "record_content"}) {
		t.Fatalf("PutContent() events = %#v, want persisted key before Blob Put", fixture.events)
	}
	if fixture.repository.prepareCommands[0].CandidateTemporaryObjectKey != uploadServiceTemporaryKey2 {
		t.Fatalf("PrepareUpload() retry candidate = %q", fixture.repository.prepareCommands[0].CandidateTemporaryObjectKey)
	}
	if fixture.blob.putRequest.TemporaryKey != uploadServiceTemporaryKey1 {
		t.Fatalf("BlobStore.Put() temporary key = %q, want persisted key", fixture.blob.putRequest.TemporaryKey)
	}
	if fixture.repository.recordContentCommands[0].TemporaryObjectKey != uploadServiceTemporaryKey1 ||
		fixture.repository.recordContentCommands[0].Object != uploadServiceObjectVersion() {
		t.Fatalf("RecordUploadedContent() = %#v", fixture.repository.recordContentCommands[0])
	}
	if fixture.repository.recordContentCommands[0].PublicationIntent.State != BlobPublicationStatePublished {
		t.Fatalf("RecordUploadedContent() publication intent = %#v, want published", fixture.repository.recordContentCommands[0].PublicationIntent)
	}
	if result.Object != uploadServiceObjectVersion() {
		t.Fatalf("PutContent() = %#v", result)
	}
}

func TestUploadServicePutContentRecordsSurvivingS3VersionAndDoesNotReplaceIt(t *testing.T) {
	t.Parallel()

	fixture := newUploadServiceFixture(t, TransportKindS3)
	fixture.repository.preparation.TemporaryObjectKey = uploadServiceTemporaryKey1
	fixture.blob.resolveResult = TemporaryObjectVersion{
		Key: uploadServiceTemporaryKey1, VersionID: "surviving-temporary-v1",
	}
	fixture.blob.resolveErr = nil
	_, err := fixture.service.PutContent(context.Background(), validPutUploadContentRequest())
	if !errors.Is(err, ErrAttachmentConflict) {
		t.Fatalf("PutContent(surviving temporary) error = %v, want ErrAttachmentConflict", err)
	}
	if !reflect.DeepEqual(fixture.events, []string{"authorize", "prepare", "resolve_temp", "record_temp_version"}) {
		t.Fatalf("PutContent(surviving temporary) events = %#v", fixture.events)
	}
	if fixture.blob.putCalls != 0 || len(fixture.repository.recordContentCommands) != 0 ||
		len(fixture.repository.recordTemporaryVersionCommands) != 1 {
		t.Fatalf("PutContent(surviving temporary) calls = put %d record content/version %d/%d",
			fixture.blob.putCalls, len(fixture.repository.recordContentCommands),
			len(fixture.repository.recordTemporaryVersionCommands))
	}
	want := RecordTemporaryObjectVersionCommand{
		ProjectID: "default", UploadID: "aup_service1", AuthorID: uploadServiceAuthorID,
		TemporaryObjectKey: uploadServiceTemporaryKey1, TemporaryObjectVersion: "surviving-temporary-v1",
	}
	if got := fixture.repository.recordTemporaryVersionCommands[0]; got != want {
		t.Fatalf("RecordTemporaryObjectVersion() = %#v, want %#v", got, want)
	}
}

func TestUploadServicePutContentUsesDeclaredLengthAndSHA256ForLocalBlob(t *testing.T) {
	t.Parallel()

	fixture := newUploadServiceFixture(t, TransportKindLocal)
	request := validPutUploadContentRequest()
	result, err := fixture.service.PutContent(context.Background(), request)
	if err != nil {
		t.Fatalf("PutContent() error = %v", err)
	}
	if fixture.blob.putRequest.ExpectedSizeBytes != int64(len(uploadServiceContent)) ||
		fixture.blob.putRequest.ExpectedSHA256 != sha256.Sum256(uploadServiceContent) ||
		fixture.blob.putRequest.TemporaryKey != "" || !bytes.Equal(fixture.blob.putContent, uploadServiceContent) {
		t.Fatalf("BlobStore.Put() = (%#v, %q)", fixture.blob.putRequest, fixture.blob.putContent)
	}
	if result.Object != uploadServiceObjectVersion() {
		t.Fatalf("PutContent() = %#v", result)
	}
}

func TestUploadServicePutContentRejectsTypedNilReaderBeforeSideEffects(t *testing.T) {
	t.Parallel()

	fixture := newUploadServiceFixture(t, TransportKindLocal)
	request := validPutUploadContentRequest()
	var typedNil *bytes.Reader
	request.Content = typedNil

	err, panicValue := captureAttachmentCallPanic(func() error {
		_, err := fixture.service.PutContent(context.Background(), request)
		return err
	})
	if panicValue != nil {
		t.Fatalf("PutContent(typed-nil reader) panic = %v after events %#v; want ErrInvalidUploadServiceRequest before side effects",
			panicValue, fixture.events)
	}
	if !errors.Is(err, ErrInvalidUploadServiceRequest) {
		t.Fatalf("PutContent(typed-nil reader) error = %v, want ErrInvalidUploadServiceRequest", err)
	}
	if len(fixture.events) != 0 || fixture.repository.publicationPrepareCalls != 0 || fixture.blob.putCalls != 0 {
		t.Fatalf("PutContent(typed-nil reader) events/publication/put = %#v/%d/%d, want no side effects",
			fixture.events, fixture.repository.publicationPrepareCalls, fixture.blob.putCalls)
	}
}

func TestUploadServicePutContentRejectsFinalIdentityDriftBeforePersistence(t *testing.T) {
	t.Parallel()

	fixture := newUploadServiceFixture(t, TransportKindLocal)
	fixture.blob.putResult.SizeBytes++
	_, err := fixture.service.PutContent(context.Background(), validPutUploadContentRequest())
	if !errors.Is(err, ErrAttachmentConflict) {
		t.Fatalf("PutContent() error = %v, want ErrAttachmentConflict", err)
	}
	if len(fixture.repository.recordContentCommands) != 0 {
		t.Fatalf("PutContent() persisted drifted object: %#v", fixture.repository.recordContentCommands)
	}
}

func TestUploadServiceCompleteVerifiesExactVersionAndBytesThenEnqueuesWithoutScanning(t *testing.T) {
	t.Parallel()

	fixture := newUploadServiceFixture(t, TransportKindS3)
	result, err := fixture.service.CompleteUpload(context.Background(), validCompleteUploadRequest())
	if err != nil {
		t.Fatalf("CompleteUpload() error = %v", err)
	}
	if !reflect.DeepEqual(fixture.events, []string{"authorize", "load_completion", "blob_stat", "blob_open", "complete_enqueue"}) {
		t.Fatalf("CompleteUpload() events = %#v", fixture.events)
	}
	if fixture.blob.statVersion != uploadServiceObjectVersion() || fixture.blob.openVersion != uploadServiceObjectVersion() {
		t.Fatalf("CompleteUpload() verified stat/open = %#v/%#v", fixture.blob.statVersion, fixture.blob.openVersion)
	}
	command := fixture.repository.completeCommands[0]
	if command.Object != uploadServiceObjectVersion() || command.ActualSizeBytes != int64(len(uploadServiceContent)) ||
		command.ActualSHA256 != sha256.Sum256(uploadServiceContent) ||
		command.TemporaryObjectKey != uploadServiceTemporaryKey1 || command.TemporaryObjectVersion != "" ||
		command.ProcessorJobID != "apj_service1" || command.ProcessorProfile != ProcessorProfileText ||
		command.CompletionFingerprint == [sha256.Size]byte{} {
		t.Fatalf("CompleteUploadAndEnqueue() = %#v", command)
	}
	if result.State != UploadStateQuarantined {
		t.Fatalf("CompleteUpload() = %#v", result)
	}
}

func TestUploadServiceCompleteDirectS3PublishesExactTemporaryThenCleansUpAfterCommit(t *testing.T) {
	t.Parallel()

	fixture := newUploadServiceFixture(t, TransportKindS3)
	fixture.repository.completion.HasObject = false
	fixture.repository.completion.Object = ObjectVersion{}
	fixture.repository.completion.TemporaryObjectVersion = ""
	fixture.blob.resolveResult = TemporaryObjectVersion{
		Key: uploadServiceTemporaryKey1, VersionID: "direct-temporary-v1",
	}
	fixture.blob.resolveErr = nil
	fixture.blob.openTemporaryContent = append([]byte(nil), uploadServiceContent...)

	result, err := fixture.service.CompleteUpload(context.Background(), validCompleteUploadRequest())
	if err != nil {
		t.Fatalf("CompleteUpload(direct S3) error = %v", err)
	}
	wantEvents := []string{
		"authorize", "load_completion", "resolve_temp", "record_temp_version",
		"open_temp", "publish_temp", "complete_enqueue", "delete_temp",
	}
	if !reflect.DeepEqual(fixture.events, wantEvents) {
		t.Fatalf("CompleteUpload(direct S3) events = %#v, want %#v", fixture.events, wantEvents)
	}
	if result.State != UploadStateQuarantined || len(fixture.repository.completeCommands) != 1 {
		t.Fatalf("CompleteUpload(direct S3) = %#v, commands %#v", result, fixture.repository.completeCommands)
	}
	command := fixture.repository.completeCommands[0]
	if command.Object != uploadServiceObjectVersion() || command.TemporaryObjectKey != uploadServiceTemporaryKey1 ||
		command.TemporaryObjectVersion != "direct-temporary-v1" ||
		command.ActualSHA256 != sha256.Sum256(uploadServiceContent) {
		t.Fatalf("CompleteUploadAndEnqueue(direct S3) = %#v", command)
	}
	if command.PublicationIntent.State != BlobPublicationStatePublished {
		t.Fatalf("CompleteUploadAndEnqueue(direct S3) publication intent = %#v, want published", command.PublicationIntent)
	}
	if !reflect.DeepEqual(fixture.blob.deletedTemporaryVersions, []TemporaryObjectVersion{{
		Key: uploadServiceTemporaryKey1, VersionID: "direct-temporary-v1",
	}}) {
		t.Fatalf("DeleteTemporaryVersion() = %#v", fixture.blob.deletedTemporaryVersions)
	}
}

func TestUploadServiceCompleteDirectS3DatabaseFailurePreservesTemporaryForRetry(t *testing.T) {
	t.Parallel()

	fixture := newUploadServiceFixture(t, TransportKindS3)
	fixture.repository.completion.HasObject = false
	fixture.repository.completion.Object = ObjectVersion{}
	fixture.blob.resolveResult = TemporaryObjectVersion{
		Key: uploadServiceTemporaryKey1, VersionID: "direct-temporary-v1",
	}
	fixture.blob.resolveErr = nil
	fixture.blob.openTemporaryContent = append([]byte(nil), uploadServiceContent...)
	fixture.repository.completeErr = errors.New("injected completion transaction failure")

	if _, err := fixture.service.CompleteUpload(context.Background(), validCompleteUploadRequest()); err == nil {
		t.Fatal("CompleteUpload(database failure) error = nil")
	}
	if len(fixture.blob.deletedTemporaryVersions) != 0 {
		t.Fatalf("CompleteUpload(database failure) deleted temporary = %#v", fixture.blob.deletedTemporaryVersions)
	}
	if fixture.repository.completion.TemporaryObjectVersion != "direct-temporary-v1" {
		t.Fatalf("persisted temporary version = %q", fixture.repository.completion.TemporaryObjectVersion)
	}

	fixture.events = nil
	fixture.repository.completeErr = nil
	if _, err := fixture.service.CompleteUpload(context.Background(), validCompleteUploadRequest()); err != nil {
		t.Fatalf("CompleteUpload(retry) error = %v", err)
	}
	if fixture.blob.publishTemporaryCalls != 2 || len(fixture.blob.deletedTemporaryVersions) != 1 {
		t.Fatalf("CompleteUpload(retry) publish/delete calls = %d/%d",
			fixture.blob.publishTemporaryCalls, len(fixture.blob.deletedTemporaryVersions))
	}
}

func TestUploadServiceCompleteDirectS3RejectsReplacedTemporaryVersion(t *testing.T) {
	t.Parallel()

	fixture := newUploadServiceFixture(t, TransportKindS3)
	fixture.repository.completion.HasObject = false
	fixture.repository.completion.Object = ObjectVersion{}
	fixture.repository.completion.TemporaryObjectVersion = "direct-temporary-v1"
	fixture.blob.resolveResult = TemporaryObjectVersion{
		Key: uploadServiceTemporaryKey1, VersionID: "replacement-temporary-v2",
	}
	fixture.blob.resolveErr = nil

	_, err := fixture.service.CompleteUpload(context.Background(), validCompleteUploadRequest())
	if !errors.Is(err, ErrAttachmentConflict) {
		t.Fatalf("CompleteUpload(replaced temporary) error = %v, want ErrAttachmentConflict", err)
	}
	if fixture.blob.openTemporaryCalls != 0 || fixture.blob.publishTemporaryCalls != 0 ||
		len(fixture.repository.completeCommands) != 0 || len(fixture.blob.deletedTemporaryVersions) != 0 {
		t.Fatal("CompleteUpload(replaced temporary) performed read, publish, DB completion, or cleanup")
	}
}

func TestUploadServiceCompleteReplayFinishesDirectS3CleanupWithoutReverification(t *testing.T) {
	t.Parallel()

	fixture := newUploadServiceFixture(t, TransportKindS3)
	fixture.repository.completion.State = UploadStateQuarantined
	fixture.repository.completion.TemporaryObjectVersion = "direct-temporary-v1"
	fixture.repository.completion.ExpiresAt = uploadServiceNow.Add(-time.Hour)
	fixture.blob.deleteTemporaryErr = errors.New("temporary cleanup unavailable")

	if _, err := fixture.service.CompleteUpload(context.Background(), validCompleteUploadRequest()); err == nil {
		t.Fatal("CompleteUpload(cleanup failure) error = nil")
	}
	if fixture.blob.statCalls != 0 || fixture.blob.openCalls != 0 || fixture.blob.openTemporaryCalls != 0 ||
		fixture.blob.publishTemporaryCalls != 0 || len(fixture.repository.completeCommands) != 1 {
		t.Fatal("CompleteUpload(cleanup failure replay) repeated object verification/publication")
	}

	fixture.events = nil
	fixture.blob.deleteTemporaryErr = nil
	result, err := fixture.service.CompleteUpload(context.Background(), validCompleteUploadRequest())
	if err != nil {
		t.Fatalf("CompleteUpload(cleanup retry) error = %v", err)
	}
	if result.State != UploadStateQuarantined || !reflect.DeepEqual(fixture.events, []string{
		"authorize", "load_completion", "complete_enqueue", "delete_temp",
	}) {
		t.Fatalf("CompleteUpload(cleanup retry) = %#v, events %#v", result, fixture.events)
	}
}

func TestUploadServiceCompleteRejectsServerSideHashMismatchWithoutEnqueue(t *testing.T) {
	t.Parallel()

	fixture := newUploadServiceFixture(t, TransportKindLocal)
	fixture.blob.openContent = []byte("tampered-content")
	_, err := fixture.service.CompleteUpload(context.Background(), validCompleteUploadRequest())
	if !errors.Is(err, ErrBlobSizeMismatch) && !errors.Is(err, ErrBlobHashMismatch) {
		t.Fatalf("CompleteUpload() error = %v, want byte verification error", err)
	}
	if len(fixture.repository.completeCommands) != 0 {
		t.Fatalf("CompleteUpload() enqueued mismatched bytes: %#v", fixture.repository.completeCommands)
	}
}

func TestUploadServiceCompleteIsIdempotentWithStableFingerprintAndJobIdentity(t *testing.T) {
	t.Parallel()

	fixture := newUploadServiceFixture(t, TransportKindLocal)
	first, err := fixture.service.CompleteUpload(context.Background(), validCompleteUploadRequest())
	if err != nil {
		t.Fatalf("CompleteUpload(first) error = %v", err)
	}
	fixture.repository.content.State = UploadStateQuarantined
	fixture.repository.content.ExpiresAt = uploadServiceNow.Add(-time.Hour)
	fixture.repository.completion.ExpiresAt = uploadServiceNow.Add(-time.Hour)
	second, err := fixture.service.CompleteUpload(context.Background(), validCompleteUploadRequest())
	if err != nil {
		t.Fatalf("CompleteUpload(replay) error = %v", err)
	}
	if first != second || len(fixture.repository.completeCommands) != 2 {
		t.Fatalf("CompleteUpload() first/replay = %#v/%#v", first, second)
	}
	left, right := fixture.repository.completeCommands[0], fixture.repository.completeCommands[1]
	if left.CompletionFingerprint != right.CompletionFingerprint || left.ProcessorJobID != right.ProcessorJobID {
		t.Fatalf("CompleteUpload() replay commands = %#v/%#v", left, right)
	}
	if fixture.blob.statCalls != 1 || fixture.blob.openCalls != 1 {
		t.Fatalf("CompleteUpload() replay reverified committed bytes: stat/open = %d/%d", fixture.blob.statCalls, fixture.blob.openCalls)
	}
}

func TestUploadServiceExpiredSessionFailsClosedBeforeBlobMutationOrEnqueue(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		run  func(*uploadServiceFixture) error
	}{
		{name: "put", run: func(fixture *uploadServiceFixture) error {
			fixture.repository.preparation.ExpiresAt = uploadServiceNow.Add(-time.Second)
			_, err := fixture.service.PutContent(context.Background(), validPutUploadContentRequest())
			return err
		}},
		{name: "complete", run: func(fixture *uploadServiceFixture) error {
			fixture.repository.completion.ExpiresAt = uploadServiceNow.Add(-time.Second)
			_, err := fixture.service.CompleteUpload(context.Background(), validCompleteUploadRequest())
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := newUploadServiceFixture(t, TransportKindS3)
			err := test.run(fixture)
			if !errors.Is(err, ErrUploadExpired) {
				t.Fatalf("expired %s error = %v, want ErrUploadExpired", test.name, err)
			}
			if fixture.blob.putCalls != 0 || fixture.blob.statCalls != 0 || fixture.blob.openCalls != 0 ||
				len(fixture.repository.recordContentCommands) != 0 || len(fixture.repository.completeCommands) != 0 {
				t.Fatalf("expired %s performed object/enqueue actions", test.name)
			}
		})
	}
}

func TestUploadServicePreservesStableRepositoryConflicts(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		run  func(*uploadServiceFixture) error
	}{
		{name: "prepare", run: func(fixture *uploadServiceFixture) error {
			fixture.repository.prepareErr = ErrAttachmentConflict
			_, err := fixture.service.PutContent(context.Background(), validPutUploadContentRequest())
			return err
		}},
		{name: "record content", run: func(fixture *uploadServiceFixture) error {
			fixture.repository.recordContentErr = ErrAttachmentConflict
			_, err := fixture.service.PutContent(context.Background(), validPutUploadContentRequest())
			return err
		}},
		{name: "complete", run: func(fixture *uploadServiceFixture) error {
			fixture.repository.completeErr = ErrAttachmentConflict
			_, err := fixture.service.CompleteUpload(context.Background(), validCompleteUploadRequest())
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := newUploadServiceFixture(t, TransportKindLocal)
			if err := test.run(fixture); !errors.Is(err, ErrAttachmentConflict) {
				t.Fatalf("%s error = %v, want ErrAttachmentConflict", test.name, err)
			}
		})
	}
}

const (
	uploadServiceTemporaryKey1 = "temporary/1111111111111111111111111111111111111111111111111111111111111111"
	uploadServiceTemporaryKey2 = "temporary/2222222222222222222222222222222222222222222222222222222222222222"
	uploadServiceAuthorID      = "usr_0123456789abcdef01234567"
	uploadServicePresignedURL  = "https://objects.example.test/private-upload?X-Amz-Credential=runtime"
)

var (
	uploadServiceNow     = time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	uploadServiceContent = []byte("verified attachment bytes")
)

type uploadServiceFixture struct {
	service       *UploadService
	authorizer    *uploadServiceAuthorizerStub
	repository    *uploadServiceRepositoryStub
	blob          *uploadServiceBlobStub
	events        []string
	temporaryKeys []string
}

func newUploadServiceFixture(t *testing.T, transport TransportKind) *uploadServiceFixture {
	t.Helper()
	return newUploadServiceFixtureWithArchiveScannerReadiness(t, transport, nil)
}

func newUploadServiceFixtureWithArchiveScannerReadiness(
	t *testing.T,
	transport TransportKind,
	archiveScannerReadiness func(context.Context) (ScannerStatus, error),
) *uploadServiceFixture {
	t.Helper()
	fixture := &uploadServiceFixture{}
	fixture.authorizer = &uploadServiceAuthorizerStub{events: &fixture.events}
	fixture.repository = &uploadServiceRepositoryStub{
		events: &fixture.events,
		reservation: UploadReservationResult{
			UploadID: "aup_service1", AttachmentID: "att_service1", State: UploadStateCreated,
			Quota: QuotaSnapshot{Usage: QuotaUsage{ReservedBytes: int64(len(uploadServiceContent))}},
		},
		preparation: UploadPreparation{
			ProjectID: "default", UploadID: "aup_service1", AttachmentID: "att_service1",
			DraftID: "rdf_service1", AuthorID: uploadServiceAuthorID, State: UploadStateUploading,
			TransportKind: transport, DeclaredSizeBytes: int64(len(uploadServiceContent)),
			MediaType: "text/plain", ExpiresAt: uploadServiceNow.Add(time.Hour),
		},
		content: UploadedContent{
			ProjectID: "default", UploadID: "aup_service1", AttachmentID: "att_service1",
			DraftID: "rdf_service1", AuthorID: uploadServiceAuthorID, State: UploadStateUploading,
			TransportKind: transport, MediaType: "text/plain", ExpiresAt: uploadServiceNow.Add(time.Hour),
			TemporaryObjectKey: uploadServiceTemporaryKey1, Object: uploadServiceObjectVersion(),
		},
		completeResult: UploadMutationResult{UploadID: "aup_service1", AttachmentID: "att_service1", State: UploadStateQuarantined},
	}
	fixture.repository.completion = UploadCompletionPreparation{
		UploadPreparation: UploadPreparation{
			ProjectID: "default", UploadID: "aup_service1", AttachmentID: "att_service1",
			DraftID: "rdf_service1", AuthorID: uploadServiceAuthorID, State: UploadStateUploading,
			TransportKind: transport, DeclaredSizeBytes: int64(len(uploadServiceContent)),
			MediaType: "text/plain", ExpiresAt: uploadServiceNow.Add(time.Hour),
			TemporaryObjectKey: uploadServiceTemporaryKey1,
		},
		Object: uploadServiceObjectVersion(), HasObject: true,
	}
	if transport == TransportKindLocal {
		fixture.repository.content.TemporaryObjectKey = ""
		fixture.repository.completion.TemporaryObjectKey = ""
	}
	fixture.blob = &uploadServiceBlobStub{
		events:                 &fixture.events,
		presignURL:             uploadServicePresignedURL,
		presignMethod:          "PUT",
		presignRequiredHeaders: []string{},
		putResult:              uploadServiceObjectVersion(),
		statResult:             ObjectInfo{Version: uploadServiceObjectVersion()},
		openContent:            append([]byte(nil), uploadServiceContent...),
		resolveErr:             ErrBlobNotFound,
		publishTemporaryResult: uploadServiceObjectVersion(),
	}
	fixture.temporaryKeys = []string{uploadServiceTemporaryKey1, uploadServiceTemporaryKey2}
	service, err := NewUploadService(fixture.authorizer, fixture.repository, fixture.blob, UploadServiceOptions{
		TransportKind:           transport,
		Limits:                  DefaultLimits(),
		Now:                     func() time.Time { return uploadServiceNow },
		ArchiveScannerReadiness: archiveScannerReadiness,
		NewTemporaryObjectKey: func() (string, error) {
			if len(fixture.temporaryKeys) == 0 {
				return "", errors.New("temporary key sequence exhausted")
			}
			key := fixture.temporaryKeys[0]
			fixture.temporaryKeys = fixture.temporaryKeys[1:]
			return key, nil
		},
		ProcessorMaxAttempts: 3,
	})
	if err != nil {
		t.Fatalf("NewUploadService() error = %v", err)
	}
	fixture.service = service
	return fixture
}

func validCreateUploadRequest() CreateUploadRequest {
	return CreateUploadRequest{
		Actor:             uploadServiceActor(),
		UploadID:          "aup_service1",
		AttachmentID:      "att_service1",
		DraftID:           "rdf_service1",
		DisplayName:       "report.txt",
		MediaType:         "text/plain",
		DeclaredSizeBytes: int64(len(uploadServiceContent)),
		ExpiresAt:         uploadServiceNow.Add(time.Hour),
	}
}

func validPutUploadContentRequest() PutUploadContentRequest {
	return PutUploadContentRequest{
		Actor:          uploadServiceActor(),
		DraftID:        "rdf_service1",
		UploadID:       "aup_service1",
		ExpectedSHA256: sha256.Sum256(uploadServiceContent),
		Content:        bytes.NewReader(uploadServiceContent),
	}
}

func validCompleteUploadRequest() CompleteUploadRequest {
	return CompleteUploadRequest{Actor: uploadServiceActor(), DraftID: "rdf_service1", UploadID: "aup_service1"}
}

func uploadServiceActor() recordauth.ActorScope {
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID: uploadServiceAuthorID, Role: recordauth.RoleProjectAdmin, ProjectID: recordauth.ProjectIDDefault,
	})
	if err != nil {
		panic(err)
	}
	return actor
}

func uploadServiceObjectVersion() ObjectVersion {
	digest := sha256.Sum256(uploadServiceContent)
	return ObjectVersion{
		Key: "sha256/" + hexDigest(digest), VersionID: "local-v1", SHA256: digest, SizeBytes: int64(len(uploadServiceContent)),
	}
}

type uploadServiceAuthorizerStub struct {
	events *[]string
	err    error
}

func (stub *uploadServiceAuthorizerStub) AuthorizeDraftAttachmentUpload(
	_ context.Context,
	actor recordauth.ActorScope,
	draftID string,
) error {
	*stub.events = append(*stub.events, "authorize")
	if actor.UserID != uploadServiceAuthorID || actor.ProjectID != recordauth.ProjectIDDefault || draftID != "rdf_service1" {
		return recordauth.ErrDenied
	}
	return stub.err
}

type uploadServiceRepositoryStub struct {
	events                         *[]string
	reserveCalls                   int
	reservation                    UploadReservationResult
	preparation                    UploadPreparation
	content                        UploadedContent
	completion                     UploadCompletionPreparation
	completeResult                 UploadMutationResult
	prepareErr                     error
	recordContentErr               error
	loadContentErr                 error
	completeErr                    error
	publicationIntent              BlobPublicationIntent
	publicationPrepareCalls        int
	publicationVersionCalls        int
	prepareCommands                []PrepareUploadCommand
	recordContentCommands          []RecordUploadedContentCommand
	completeCommands               []CompleteUploadAndEnqueueCommand
	recordTemporaryVersionCommands []RecordTemporaryObjectVersionCommand
}

func (stub *uploadServiceRepositoryStub) ReserveUpload(
	_ context.Context,
	command ReserveUploadCommand,
) (UploadReservationResult, error) {
	*stub.events = append(*stub.events, "reserve")
	stub.reserveCalls++
	if command.AuthorID != uploadServiceAuthorID || command.DraftID != "rdf_service1" {
		return UploadReservationResult{}, ErrAttachmentOwnerNotFound
	}
	return stub.reservation, nil
}

func (stub *uploadServiceRepositoryStub) PrepareBlobPublication(
	_ context.Context,
	request BlobPublicationPrepareRequest,
) (BlobPublicationIntent, error) {
	stub.publicationPrepareCalls++
	if stub.publicationIntent != (BlobPublicationIntent{}) {
		return stub.publicationIntent, nil
	}
	return BlobPublicationIntent{
		PublicationID: "bpi_service1", ProjectID: request.ProjectID,
		OwnerKind: request.OwnerKind, OwnerID: request.OwnerID, OwnerGeneration: request.OwnerGeneration,
		Target: request.Target, State: BlobPublicationStatePrepared, PublishExpiresAt: request.PublishExpiresAt,
	}, nil
}

func (stub *uploadServiceRepositoryStub) RecordBlobPublicationVersion(
	_ context.Context,
	request BlobPublicationVersionRequest,
) (BlobPublicationIntent, error) {
	stub.publicationVersionCalls++
	intent := request.Intent
	if intent.State == BlobPublicationStatePublished {
		return intent, nil
	}
	intent.ObjectVersion = request.Object.VersionID
	intent.State = BlobPublicationStatePublished
	stub.publicationIntent = intent
	return intent, nil
}

func (stub *uploadServiceRepositoryStub) PrepareUpload(
	_ context.Context,
	command PrepareUploadCommand,
) (UploadPreparation, error) {
	*stub.events = append(*stub.events, "prepare")
	stub.prepareCommands = append(stub.prepareCommands, command)
	if stub.prepareErr != nil {
		return UploadPreparation{}, stub.prepareErr
	}
	if stub.preparation.TransportKind == TransportKindS3 && stub.preparation.TemporaryObjectKey == "" {
		stub.preparation.TemporaryObjectKey = command.CandidateTemporaryObjectKey
	}
	return stub.preparation, nil
}

func (stub *uploadServiceRepositoryStub) RecordUploadedContent(
	_ context.Context,
	command RecordUploadedContentCommand,
) (UploadedContent, error) {
	*stub.events = append(*stub.events, "record_content")
	stub.recordContentCommands = append(stub.recordContentCommands, command)
	if stub.recordContentErr != nil {
		return UploadedContent{}, stub.recordContentErr
	}
	result := stub.content
	result.TemporaryObjectKey = command.TemporaryObjectKey
	result.Object = command.Object
	return result, nil
}

func (stub *uploadServiceRepositoryStub) RecordTemporaryObjectVersion(
	_ context.Context,
	command RecordTemporaryObjectVersionCommand,
) (UploadPreparation, error) {
	*stub.events = append(*stub.events, "record_temp_version")
	stub.recordTemporaryVersionCommands = append(stub.recordTemporaryVersionCommands, command)
	result := stub.preparation
	result.TemporaryObjectKey = command.TemporaryObjectKey
	result.TemporaryObjectVersion = command.TemporaryObjectVersion
	stub.completion.UploadPreparation = result
	return result, nil
}

func (stub *uploadServiceRepositoryStub) GetUploadedContent(
	_ context.Context,
	_ UploadMutationCommand,
) (UploadedContent, error) {
	*stub.events = append(*stub.events, "load_content")
	return stub.content, stub.loadContentErr
}

func (stub *uploadServiceRepositoryStub) GetUploadCompletionPreparation(
	_ context.Context,
	_ UploadMutationCommand,
) (UploadCompletionPreparation, error) {
	*stub.events = append(*stub.events, "load_completion")
	return stub.completion, stub.loadContentErr
}

func (stub *uploadServiceRepositoryStub) CompleteUploadAndEnqueue(
	_ context.Context,
	command CompleteUploadAndEnqueueCommand,
) (UploadMutationResult, error) {
	*stub.events = append(*stub.events, "complete_enqueue")
	stub.completeCommands = append(stub.completeCommands, command)
	if stub.completeErr == nil {
		stub.completion.State = stub.completeResult.State
		stub.completion.Object = command.Object
		stub.completion.HasObject = true
	}
	return stub.completeResult, stub.completeErr
}

type uploadServiceBlobStub struct {
	events                   *[]string
	presignTemporaryKey      string
	presignTTL               time.Duration
	presignCalls             int
	presignURL               string
	presignMethod            string
	presignRequiredHeaders   []string
	presignErr               error
	putRequest               PutRequest
	putContent               []byte
	putResult                ObjectVersion
	putErr                   error
	putCalls                 int
	statVersion              ObjectVersion
	statResult               ObjectInfo
	statErr                  error
	statCalls                int
	openVersion              ObjectVersion
	openContent              []byte
	openErr                  error
	openCalls                int
	resolveResult            TemporaryObjectVersion
	resolveErr               error
	openTemporaryContent     []byte
	openTemporaryErr         error
	openTemporaryCalls       int
	publishTemporaryResult   ObjectVersion
	publishTemporaryErr      error
	publishTemporaryCalls    int
	deletedTemporaryVersions []TemporaryObjectVersion
	deleteTemporaryErr       error
}

func (stub *uploadServiceBlobStub) PresignTemporaryUpload(
	_ context.Context,
	temporaryObjectKey string,
	ttl time.Duration,
) (string, string, []string, error) {
	*stub.events = append(*stub.events, "presign")
	stub.presignCalls++
	stub.presignTemporaryKey = temporaryObjectKey
	stub.presignTTL = ttl
	requiredHeaders := make([]string, len(stub.presignRequiredHeaders))
	copy(requiredHeaders, stub.presignRequiredHeaders)
	return stub.presignURL, stub.presignMethod, requiredHeaders, stub.presignErr
}

func (stub *uploadServiceBlobStub) Put(_ context.Context, request PutRequest, reader io.Reader) (ObjectVersion, error) {
	*stub.events = append(*stub.events, "blob_put")
	stub.putCalls++
	stub.putRequest = request
	stub.putContent, _ = io.ReadAll(reader)
	return stub.putResult, stub.putErr
}

func (stub *uploadServiceBlobStub) Stat(_ context.Context, version ObjectVersion) (ObjectInfo, error) {
	*stub.events = append(*stub.events, "blob_stat")
	stub.statCalls++
	stub.statVersion = version
	return stub.statResult, stub.statErr
}

func (stub *uploadServiceBlobStub) Open(_ context.Context, version ObjectVersion, _ ByteRange) (io.ReadCloser, error) {
	*stub.events = append(*stub.events, "blob_open")
	stub.openCalls++
	stub.openVersion = version
	if stub.openErr != nil {
		return nil, stub.openErr
	}
	return io.NopCloser(bytes.NewReader(stub.openContent)), nil
}

func (*uploadServiceBlobStub) Delete(context.Context, ObjectVersion) (DeletionReceipt, error) {
	panic("UploadService must not delete content")
}

func (stub *uploadServiceBlobStub) ResolveTemporaryVersion(
	_ context.Context,
	key string,
) (TemporaryObjectVersion, error) {
	*stub.events = append(*stub.events, "resolve_temp")
	if stub.resolveResult.Key != "" && stub.resolveResult.Key != key {
		return TemporaryObjectVersion{}, ErrBlobVersionMismatch
	}
	return stub.resolveResult, stub.resolveErr
}

func (stub *uploadServiceBlobStub) OpenTemporaryVersion(
	_ context.Context,
	_ TemporaryObjectReadRequest,
) (io.ReadCloser, error) {
	*stub.events = append(*stub.events, "open_temp")
	stub.openTemporaryCalls++
	if stub.openTemporaryErr != nil {
		return nil, stub.openTemporaryErr
	}
	return io.NopCloser(bytes.NewReader(stub.openTemporaryContent)), nil
}

func (stub *uploadServiceBlobStub) PublishTemporaryVersion(
	_ context.Context,
	_ TemporaryObjectPublishRequest,
) (ObjectVersion, error) {
	*stub.events = append(*stub.events, "publish_temp")
	stub.publishTemporaryCalls++
	return stub.publishTemporaryResult, stub.publishTemporaryErr
}

func (stub *uploadServiceBlobStub) DeleteTemporaryVersion(
	_ context.Context,
	version TemporaryObjectVersion,
) error {
	*stub.events = append(*stub.events, "delete_temp")
	stub.deletedTemporaryVersions = append(stub.deletedTemporaryVersions, version)
	return stub.deleteTemporaryErr
}

func captureAttachmentCallPanic(call func() error) (err error, panicValue any) {
	defer func() {
		panicValue = recover()
	}()
	return call(), nil
}
