package attachments

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
)

const (
	downloadAuthorID = "usr_0123456789abcdef01234567"
	downloadOtherID  = "usr_abcdef0123456789abcdef01"
)

func TestDownloadServiceRequiresDraftAuthorAndRecordPolicyBeforeOpeningBytes(t *testing.T) {
	t.Parallel()

	t.Run("draft author", func(t *testing.T) {
		fixture := newDownloadServiceFixture(t, downloadDraftAttachment())
		request := validDownloadRequest(downloadActor(t, downloadAuthorID))

		delivery, err := fixture.service.Open(context.Background(), request)
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		defer func() { _ = delivery.Close(context.Background()) }()
		if fixture.authorizer.calls != 0 || fixture.leases.acquireCalls != 0 {
			t.Fatalf("draft Open() record authorization/lease calls = %d/%d, want 0/0",
				fixture.authorizer.calls, fixture.leases.acquireCalls)
		}
		if fixture.blob.openCalls != 1 || fixture.blob.openVersion != downloadOriginalObject() {
			t.Fatalf("draft Open() Blob call = %d/%#v", fixture.blob.openCalls, fixture.blob.openVersion)
		}

		denied := newDownloadServiceFixture(t, downloadDraftAttachment())
		_, err = denied.service.Open(context.Background(), validDownloadRequest(downloadActor(t, downloadOtherID)))
		if !errors.Is(err, recordauth.ErrDenied) {
			t.Fatalf("Open(other draft author) error = %v, want recordauth.ErrDenied", err)
		}
		if denied.repository.assertCalls != 0 || denied.blob.openCalls != 0 || denied.leases.acquireCalls != 0 {
			t.Fatalf("denied draft opened protected state: assert/blob/lease = %d/%d/%d",
				denied.repository.assertCalls, denied.blob.openCalls, denied.leases.acquireCalls)
		}
	})

	t.Run("record policy", func(t *testing.T) {
		fixture := newDownloadServiceFixture(t, downloadRecordAttachment())
		fixture.authorizer.err = recordauth.ErrDenied

		_, err := fixture.service.Open(context.Background(), validDownloadRequest(downloadActor(t, downloadAuthorID)))
		if !errors.Is(err, recordauth.ErrDenied) {
			t.Fatalf("Open(denied record) error = %v, want recordauth.ErrDenied", err)
		}
		if fixture.authorizer.calls != 1 || fixture.authorizer.recordID != "rec_download1" ||
			fixture.leases.acquireCalls != 0 || fixture.blob.openCalls != 0 {
			t.Fatalf("denied record calls = authorizer %d/%q lease %d Blob %d",
				fixture.authorizer.calls, fixture.authorizer.recordID,
				fixture.leases.acquireCalls, fixture.blob.openCalls)
		}

		fixture.authorizer.err = nil
		delivery, err := fixture.service.Open(context.Background(), validDownloadRequest(downloadActor(t, downloadAuthorID)))
		if err != nil {
			t.Fatalf("Open(authorized record) error = %v", err)
		}
		defer func() { _ = delivery.Close(context.Background()) }()
		if fixture.leases.acquireCalls != 1 || fixture.leases.assertCalls == 0 ||
			fixture.leases.object != downloadRecordObject() || fixture.repository.assertCalls == 0 {
			t.Fatalf("authorized record lease/assert calls = acquire %d lease assert %d content assert %d object %#v",
				fixture.leases.acquireCalls, fixture.leases.assertCalls,
				fixture.repository.assertCalls, fixture.leases.object)
		}
	})
}

func TestDownloadServiceSelectsClosedVariantAndResolvedRange(t *testing.T) {
	t.Parallel()

	fixture := newDownloadServiceFixture(t, downloadDraftAttachment())
	request := validDownloadRequest(downloadActor(t, downloadAuthorID))
	request.Variant = ContentVariantPreview
	request.HTTPRange = "bytes=2-4"

	delivery, err := fixture.service.Open(context.Background(), request)
	if err != nil {
		t.Fatalf("Open(preview range) error = %v", err)
	}
	defer func() { _ = delivery.Close(context.Background()) }()
	metadata := delivery.Metadata()
	if metadata.Variant != ContentVariantPreview || metadata.Object != downloadPreviewObject() ||
		metadata.MediaType != ManagedPreviewMediaTypeTextUTF8 || metadata.Range.Start != 2 ||
		metadata.Range.EndInclusive != 4 || !metadata.Range.Partial || metadata.Range.Length != 3 {
		t.Fatalf("Open(preview range) metadata = %#v", metadata)
	}
	if fixture.blob.openVersion != downloadPreviewObject() ||
		fixture.blob.openRange != ClosedByteRange(2, 4) {
		t.Fatalf("Blob.Open(preview range) = %#v/%#v", fixture.blob.openVersion, fixture.blob.openRange)
	}

	for _, raw := range []ContentVariant{"thumbnail", "raw", "PREVIEW", " original "} {
		request.Variant = raw
		if _, err := fixture.service.Open(context.Background(), request); !errors.Is(err, ErrInvalidContentVariant) {
			t.Errorf("Open(variant %q) error = %v, want ErrInvalidContentVariant", raw, err)
		}
	}

	missing := downloadDraftAttachment()
	missing.Preview = nil
	missingFixture := newDownloadServiceFixture(t, missing)
	request = validDownloadRequest(downloadActor(t, downloadAuthorID))
	request.Variant = ContentVariantPreview
	if _, err := missingFixture.service.Open(context.Background(), request); !errors.Is(err, ErrContentVariantUnavailable) {
		t.Fatalf("Open(missing preview) error = %v, want ErrContentVariantUnavailable", err)
	}
	if missingFixture.blob.openCalls != 0 {
		t.Fatal("Open(missing preview) opened the original as a fallback")
	}
}

func TestDownloadServiceRejectsOversizedManagedTextPreview(t *testing.T) {
	t.Parallel()

	attachment := downloadDraftAttachment()
	oversized := downloadObject(0x33, DefaultLimits().MaxInlineTextPreviewBytes+1, "preview-too-large-v1")
	attachment.Preview = &ManagedPreviewContent{Object: oversized, MediaType: ManagedPreviewMediaTypeTextUTF8}
	fixture := newDownloadServiceFixture(t, attachment)
	request := validDownloadRequest(downloadActor(t, downloadAuthorID))
	request.Variant = ContentVariantPreview

	metadata, metadataErr := fixture.service.GetMetadata(context.Background(), MetadataRequest{
		Actor: request.Actor, AttachmentID: request.AttachmentID,
	})
	if metadataErr != nil {
		t.Fatalf("GetMetadata(oversized text preview) error = %v, want original metadata", metadataErr)
	}
	if metadata.SizeBytes != attachment.LogicalSizeBytes || metadata.PreviewAvailable {
		t.Fatalf("GetMetadata(oversized text preview) = %#v, want original size and unavailable preview", metadata)
	}

	_, err := fixture.service.Open(context.Background(), request)
	if !errors.Is(err, ErrContentPreviewTooLarge) {
		t.Fatalf("Open(oversized text preview) error = %v, want ErrContentPreviewTooLarge", err)
	}
	if fixture.blob.openCalls != 0 {
		t.Fatal("Open(oversized text preview) opened bytes")
	}
}

func TestResolveHTTPContentRangeSupportsSingleClosedOpenAndSuffixRanges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		raw       string
		size      int64
		want      ResolvedContentRange
		wantRange ByteRange
		wantErr   error
	}{
		{name: "full", size: 10, want: ResolvedContentRange{Start: 0, EndInclusive: 9, Length: 10}, wantRange: FullByteRange()},
		{name: "closed", raw: "bytes=2-5", size: 10, want: ResolvedContentRange{Start: 2, EndInclusive: 5, Length: 4, Partial: true}, wantRange: ClosedByteRange(2, 5)},
		{name: "open", raw: "bytes=7-", size: 10, want: ResolvedContentRange{Start: 7, EndInclusive: 9, Length: 3, Partial: true}, wantRange: ClosedByteRange(7, 9)},
		{name: "suffix", raw: "bytes=-3", size: 10, want: ResolvedContentRange{Start: 7, EndInclusive: 9, Length: 3, Partial: true}, wantRange: ClosedByteRange(7, 9)},
		{name: "clamped end", raw: "bytes=7-99", size: 10, want: ResolvedContentRange{Start: 7, EndInclusive: 9, Length: 3, Partial: true}, wantRange: ClosedByteRange(7, 9)},
		{name: "unsatisfied", raw: "bytes=10-", size: 10, wantErr: ErrContentRangeNotSatisfiable},
		{name: "multiple", raw: "bytes=0-1,4-5", size: 10, wantErr: ErrInvalidContentRange},
		{name: "wrong unit", raw: "items=0-1", size: 10, wantErr: ErrInvalidContentRange},
		{name: "zero suffix", raw: "bytes=-0", size: 10, wantErr: ErrContentRangeNotSatisfiable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, blobRange, err := ResolveHTTPContentRange(test.raw, test.size)
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("ResolveHTTPContentRange() error = %v, want %v", err, test.wantErr)
				}
				return
			}
			if err != nil || got != test.want || blobRange != test.wantRange {
				t.Fatalf("ResolveHTTPContentRange() = %#v/%#v/%v, want %#v/%#v",
					got, blobRange, err, test.want, test.wantRange)
			}
		})
	}
}

func TestContentRangeErrorPreservesSelectedSizeAndSentinel(t *testing.T) {
	t.Parallel()

	err := NewContentRangeError(7, ErrContentRangeNotSatisfiable)
	if !errors.Is(err, ErrContentRangeNotSatisfiable) {
		t.Fatalf("NewContentRangeError() = %v, want ErrContentRangeNotSatisfiable", err)
	}
	if size, ok := ContentRangeSize(err); !ok || size != 7 {
		t.Fatalf("ContentRangeSize() = %d/%t, want 7/true", size, ok)
	}
	if invalid := NewContentRangeError(0, ErrInvalidContentRange); !errors.Is(invalid, ErrInvalidContentRange) {
		t.Fatalf("NewContentRangeError(invalid) = %v, want ErrInvalidContentRange", invalid)
	}
}

func TestDownloadResponseMetadataUsesSafeFilenameAndAllowlistedMediaType(t *testing.T) {
	t.Parallel()

	disposition := SafeContentDisposition(ContentVariantOriginal, "../报告\r\n\".svg")
	for _, forbidden := range []string{"\r", "\n", "../", "\".svg\""} {
		if strings.Contains(disposition, forbidden) {
			t.Fatalf("SafeContentDisposition() = %q, contains %q", disposition, forbidden)
		}
	}
	if !strings.HasPrefix(disposition, "attachment; filename=\"") ||
		!strings.Contains(disposition, "filename*=UTF-8''") {
		t.Fatalf("SafeContentDisposition() = %q, want ASCII and UTF-8 parameters", disposition)
	}
	if got := AllowlistedContentType(ContentVariantOriginal, "image/svg+xml"); got != "application/octet-stream" {
		t.Fatalf("AllowlistedContentType(active original) = %q", got)
	}
	if got := AllowlistedContentType(ContentVariantPreview, ManagedPreviewMediaTypeTextUTF8); got != ManagedPreviewMediaTypeTextUTF8 {
		t.Fatalf("AllowlistedContentType(text preview) = %q", got)
	}
	if got := AllowlistedContentType(ContentVariantPreview, "text/html"); got != "application/octet-stream" {
		t.Fatalf("AllowlistedContentType(active preview) = %q", got)
	}
}

func TestContentDeliveryAssertsBeforeEveryWriteAndStopsAfterFence(t *testing.T) {
	t.Parallel()

	fixture := newDownloadServiceFixture(t, downloadRecordAttachment())
	fixture.repository.assertErrAt = 4 // two open assertions, first write, then fence before the second write
	fixture.repository.assertErr = recordplatform.ErrLostOwnerLease
	request := validDownloadRequest(downloadActor(t, downloadAuthorID))

	delivery, err := fixture.service.Open(context.Background(), request)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	var output bytes.Buffer
	written, err := delivery.WriteTo(context.Background(), &output)
	if !errors.Is(err, ErrContentDeliveryRevoked) {
		t.Fatalf("WriteTo() error = %v, want ErrContentDeliveryRevoked", err)
	}
	if written != 4 || output.String() != "0123" {
		t.Fatalf("WriteTo() wrote %d bytes %q after fence, want only first chunk", written, output.String())
	}
	if closeErr := delivery.Close(context.Background()); closeErr != nil {
		t.Fatalf("Close() error = %v", closeErr)
	}
	if fixture.blob.reader.closeCalls != 1 || fixture.leases.releaseCalls != 1 {
		t.Fatalf("Close() reader/lease releases = %d/%d, want 1/1",
			fixture.blob.reader.closeCalls, fixture.leases.releaseCalls)
	}
	if closeErr := delivery.Close(context.Background()); closeErr != nil ||
		fixture.blob.reader.closeCalls != 1 || fixture.leases.releaseCalls != 1 {
		t.Fatalf("Close() replay = %v reader/lease %d/%d",
			closeErr, fixture.blob.reader.closeCalls, fixture.leases.releaseCalls)
	}
}

func TestContentDeliveryAllowsOnlyChunkLinearizedBeforeConcurrentFence(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.August, 9, 8, 0, 0, 0, time.UTC)
	clock := &downloadTestClock{current: base}
	leases := &linearizingFenceDownloadLease{
		firstAssertionObserved:    make(chan struct{}),
		allowFirstAssertionReturn: make(chan struct{}),
	}
	delivery := newFinalWriteBoundaryDelivery(clock, leases, []byte("0123456789"))
	writer := &recordingDownloadWriter{}
	type writeResult struct {
		written int64
		err     error
	}
	result := make(chan writeResult, 1)
	go func() {
		written, err := delivery.WriteTo(context.Background(), writer)
		result <- writeResult{written: written, err: err}
	}()

	waitForDownloadSignal(t, leases.firstAssertionObserved, "pre-fence serving assertion")
	leases.commitFence()
	close(leases.allowFirstAssertionReturn)

	got := <-result
	if !errors.Is(got.err, ErrContentDeliveryRevoked) || !errors.Is(got.err, recordplatform.ErrLostOwnerLease) {
		t.Fatalf("WriteTo() error = %v, want revoked lost-owner fence failure", got.err)
	}
	if got.written != 4 || writer.calls != 1 || writer.output.String() != "0123" {
		t.Fatalf("WriteTo() across concurrent fence = %d bytes, writer calls/output %d/%q, want 4/1/first chunk",
			got.written, writer.calls, writer.output.String())
	}
	if calls := leases.assertionCallCount(); calls != 2 {
		t.Fatalf("serving assertion calls = %d, want first pre-fence and second post-fence assertions", calls)
	}
	if err := delivery.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestContentDeliveryRenewsServingLeaseForSlowReader(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.August, 7, 8, 0, 0, 0, time.UTC)
	clock := &downloadTestClock{current: base}
	reader := &advancingDownloadReader{
		content: []byte("0123456789"), clock: clock, advance: 6 * time.Millisecond,
	}
	lease := recordplatform.ServingLeaseV1{
		Object: downloadRecordObject(),
		Owner: recordplatform.OwnerLease{
			OwnerID: "attachment_delivery_slow", Generation: 1, ExpiresAt: base.Add(10 * time.Millisecond),
		},
		CapturedEpoch: 3,
	}
	repository := &downloadRepositoryStub{attachment: downloadRecordAttachment()}
	authorizer := &downloadAuthorizerStub{}
	leases := &downloadLeaseStub{serving: lease, clock: clock}
	blob := &downloadBlobStub{content: []byte("0123456789"), openReader: reader}
	service, err := NewDownloadService(repository, authorizer, leases, blob, DownloadServiceOptions{
		Now: clock.Now, LeaseDuration: 10 * time.Millisecond, ChunkBytes: 4,
		Limits: DefaultLimits(), NewLeaseOwnerID: func() (string, error) { return lease.Owner.OwnerID, nil },
	})
	if err != nil {
		t.Fatalf("NewDownloadService() error = %v", err)
	}
	delivery, err := service.Open(context.Background(), validDownloadRequest(downloadActor(t, downloadAuthorID)))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = delivery.Close(context.Background()) }()

	var output bytes.Buffer
	written, err := delivery.WriteTo(context.Background(), &output)
	if err != nil {
		t.Fatalf("WriteTo() error = %v, want successful renewal across slow reads", err)
	}
	if written != 10 || output.String() != "0123456789" {
		t.Fatalf("WriteTo() = %d/%q, want 10/full content", written, output.String())
	}
	if leases.renewCalls == 0 {
		t.Fatal("WriteTo() never renewed the serving lease for a slow reader")
	}
}

func TestDownloadServiceUsesOneSecondDefaultContentLease(t *testing.T) {
	t.Parallel()

	fixture := newDownloadServiceFixture(t, downloadRecordAttachment())
	service, err := NewDownloadService(fixture.repository, fixture.authorizer, fixture.leases, fixture.blob, DownloadServiceOptions{})
	if err != nil {
		t.Fatalf("NewDownloadService() error = %v", err)
	}
	if service.leaseDuration != time.Second {
		t.Fatalf("default content lease duration = %s, want 1s", service.leaseDuration)
	}
}

func TestDownloadServiceRejectsContentLeaseLongerThanOneSecond(t *testing.T) {
	t.Parallel()

	fixture := newDownloadServiceFixture(t, downloadRecordAttachment())
	service, err := NewDownloadService(
		fixture.repository,
		fixture.authorizer,
		fixture.leases,
		fixture.blob,
		DownloadServiceOptions{LeaseDuration: time.Second + time.Nanosecond},
	)
	if service != nil || !errors.Is(err, ErrInvalidDownloadRequest) {
		t.Fatalf("NewDownloadService(lease > 1s) = %#v, %v, want nil ErrInvalidDownloadRequest", service, err)
	}
}

func TestContentDeliveryRenewsWhileReaderRemainsBlockedAcrossLease(t *testing.T) {
	t.Parallel()

	const leaseDuration = 90 * time.Millisecond
	service, reader, leases := newBackgroundRenewalDownloadFixture(t, leaseDuration, nil, false)
	delivery, err := service.Open(context.Background(), validDownloadRequest(downloadActor(t, downloadAuthorID)))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	result := make(chan error, 1)
	go func() {
		_, writeErr := delivery.WriteTo(context.Background(), io.Discard)
		result <- writeErr
	}()
	waitForDownloadSignal(t, reader.started, "blocked content read")

	for renewal := 1; renewal <= 2; renewal++ {
		select {
		case <-leases.renewed:
		case <-time.After(2 * leaseDuration):
			_ = delivery.Close(context.Background())
			<-result
			t.Fatalf("blocked content read observed %d background renewals, want at least 2", renewal-1)
		}
	}
	select {
	case writeErr := <-result:
		_ = delivery.Close(context.Background())
		t.Fatalf("WriteTo() returned while its reader was still blocked: %v", writeErr)
	default:
	}
	if err := delivery.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	<-result
}

func TestContentDeliveryRenewalFailureClosesBlockedReaderBeforeExpiry(t *testing.T) {
	t.Parallel()

	const leaseDuration = 300 * time.Millisecond
	service, reader, _ := newBackgroundRenewalDownloadFixture(
		t,
		leaseDuration,
		recordplatform.ErrLostOwnerLease,
		false,
	)
	delivery, err := service.Open(context.Background(), validDownloadRequest(downloadActor(t, downloadAuthorID)))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = delivery.Close(context.Background()) }()
	result := make(chan error, 1)
	go func() {
		_, writeErr := delivery.WriteTo(context.Background(), io.Discard)
		result <- writeErr
	}()
	waitForDownloadSignal(t, reader.started, "blocked content read")

	expiry := time.NewTimer(leaseDuration)
	defer expiry.Stop()
	select {
	case <-reader.released:
	case <-expiry.C:
		_ = delivery.Close(context.Background())
		<-result
		t.Fatal("renewal failure did not close the blocked reader before lease expiry")
	}
	select {
	case writeErr := <-result:
		if !errors.Is(writeErr, ErrContentDeliveryRevoked) || !errors.Is(writeErr, recordplatform.ErrLostOwnerLease) {
			t.Fatalf("WriteTo() error = %v, want revoked lost-owner renewal failure", writeErr)
		}
	case <-time.After(time.Second):
		t.Fatal("WriteTo() remained blocked after renewal failure closed its reader")
	}
}

func TestContentDeliveryDoesNotStartWriteAfterBackgroundRenewalRevokes(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.August, 7, 8, 0, 0, 0, time.UTC)
	clock := &downloadTestClock{current: base}
	leases := &finalAssertionDownloadLease{
		clock:                clock,
		advanceAtAssertion:   4 * time.Millisecond,
		assertionStarted:     make(chan struct{}),
		allowAssertionReturn: make(chan struct{}),
		renewStarted:         make(chan struct{}),
		renewErr:             recordplatform.ErrLostOwnerLease,
	}
	delivery := newFinalWriteBoundaryDelivery(clock, leases, []byte("0123456789"))
	writer := &recordingDownloadWriter{}
	type writeResult struct {
		written int64
		err     error
	}
	result := make(chan writeResult, 1)
	go func() {
		written, err := delivery.WriteTo(context.Background(), writer)
		result <- writeResult{written: written, err: err}
	}()
	waitForDownloadSignal(t, leases.assertionStarted, "final serving assertion")

	renewalDone := make(chan struct{})
	go delivery.runRenewal(make(chan struct{}), renewalDone)
	waitForDownloadSignal(t, leases.renewStarted, "background renewal failure")
	waitForDownloadSignal(t, renewalDone, "background renewal revocation")
	close(leases.allowAssertionReturn)

	got := <-result
	if !errors.Is(got.err, ErrContentDeliveryRevoked) || !errors.Is(got.err, recordplatform.ErrLostOwnerLease) {
		t.Fatalf("WriteTo() error = %v, want revoked lost-owner renewal failure", got.err)
	}
	if got.written != 0 || writer.calls != 0 || writer.output.Len() != 0 {
		t.Fatalf("WriteTo() after renewal revocation = %d bytes, writer calls/bytes %d/%d, want 0/0/0",
			got.written, writer.calls, writer.output.Len())
	}
	if err := delivery.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestContentDeliveryDoesNotStartWriteAfterCloseLinearizes(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.August, 7, 8, 0, 0, 0, time.UTC)
	clock := &downloadTestClock{current: base}
	leases := &finalAssertionDownloadLease{
		clock:                clock,
		assertionStarted:     make(chan struct{}),
		allowAssertionReturn: make(chan struct{}),
		renewStarted:         make(chan struct{}),
	}
	delivery := newFinalWriteBoundaryDelivery(clock, leases, []byte("0123456789"))
	writer := &recordingDownloadWriter{}
	type writeResult struct {
		written int64
		err     error
	}
	result := make(chan writeResult, 1)
	go func() {
		written, err := delivery.WriteTo(context.Background(), writer)
		result <- writeResult{written: written, err: err}
	}()
	waitForDownloadSignal(t, leases.assertionStarted, "final serving assertion")

	if err := delivery.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	close(leases.allowAssertionReturn)
	got := <-result
	if !errors.Is(got.err, ErrContentDeliveryRevoked) {
		t.Fatalf("WriteTo() error = %v, want ErrContentDeliveryRevoked", got.err)
	}
	if got.written != 0 || writer.calls != 0 || writer.output.Len() != 0 {
		t.Fatalf("WriteTo() after Close = %d bytes, writer calls/bytes %d/%d, want 0/0/0",
			got.written, writer.calls, writer.output.Len())
	}
}

func TestContentDeliveryWritesAllChunksWhenNotRevoked(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.August, 7, 8, 0, 0, 0, time.UTC)
	clock := &downloadTestClock{current: base}
	delivery := newFinalWriteBoundaryDelivery(clock, statelessDownloadLeaseRepository{}, []byte("0123456789"))
	writer := &recordingDownloadWriter{}

	written, err := delivery.WriteTo(context.Background(), writer)
	if err != nil {
		t.Fatalf("WriteTo() error = %v, want nil", err)
	}
	if written != 10 || writer.calls != 3 || writer.output.String() != "0123456789" {
		t.Fatalf("WriteTo() = %d bytes, writer calls/output %d/%q, want 10/3/full content",
			written, writer.calls, writer.output.String())
	}
	if err := delivery.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestContentDeliveryReentrantCloseDoesNotDeadlockWriter(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.August, 7, 8, 0, 0, 0, time.UTC)
	clock := &downloadTestClock{current: base}
	leases := &downloadLeaseStub{}
	delivery := newFinalWriteBoundaryDelivery(clock, leases, []byte("0123456789"))
	writer := &reentrantCloseDownloadWriter{
		delivery:  delivery,
		closeDone: make(chan struct{}),
	}

	written, err := delivery.WriteTo(context.Background(), writer)
	waitForDownloadSignal(t, writer.closeDone, "reentrant Close cleanup")
	if writer.closeBlocked {
		t.Fatal("writer.Write blocked waiting for reentrant Close")
	}
	if writer.closeErr != nil {
		t.Fatalf("reentrant Close() error = %v", writer.closeErr)
	}
	if !errors.Is(err, ErrContentDeliveryRevoked) {
		t.Fatalf("WriteTo() error = %v, want ErrContentDeliveryRevoked", err)
	}
	if written != 4 || writer.calls != 1 || leases.releaseCalls != 1 {
		t.Fatalf("reentrant Close WriteTo/release = %d bytes, %d writer calls, %d releases, want 4/1/1",
			written, writer.calls, leases.releaseCalls)
	}
}

func TestContentDeliveryTerminalTransitionDoesNotWaitForBlockedWriter(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.August, 7, 8, 0, 0, 0, time.UTC)
	clock := &downloadTestClock{current: base}
	leases := &downloadLeaseStub{
		clock: clock, renewErr: recordplatform.ErrLostOwnerLease,
	}
	delivery := newFinalWriteBoundaryDelivery(clock, leases, []byte("0123456789"))
	writer := &blockingContentDownloadWriter{
		started: make(chan struct{}), allowReturn: make(chan struct{}),
	}
	type writeResult struct {
		written int64
		err     error
	}
	result := make(chan writeResult, 1)
	go func() {
		written, err := delivery.WriteTo(context.Background(), writer)
		result <- writeResult{written: written, err: err}
	}()
	waitForDownloadSignal(t, writer.started, "blocked writer")
	clock.mu.Lock()
	clock.current = clock.current.Add(4 * time.Millisecond)
	clock.mu.Unlock()

	renewalDone := make(chan struct{})
	go delivery.runRenewal(make(chan struct{}), renewalDone)
	select {
	case <-renewalDone:
	case <-time.After(time.Second):
		close(writer.allowReturn)
		<-result
		<-renewalDone
		_ = delivery.Close(context.Background())
		t.Fatal("renewal revocation waited for blocked writer.Write")
	}

	closeResult := make(chan error, 1)
	go func() { closeResult <- delivery.Close(context.Background()) }()
	select {
	case closeErr := <-closeResult:
		if closeErr != nil {
			close(writer.allowReturn)
			<-result
			t.Fatalf("Close() error = %v", closeErr)
		}
	case <-time.After(time.Second):
		close(writer.allowReturn)
		<-result
		<-closeResult
		t.Fatal("Close() waited for blocked writer.Write")
	}
	if calls, _ := writer.snapshot(); calls != 1 {
		close(writer.allowReturn)
		<-result
		t.Fatalf("writer calls while blocked = %d, want 1", calls)
	}

	close(writer.allowReturn)
	got := <-result
	if !errors.Is(got.err, ErrContentDeliveryRevoked) || !errors.Is(got.err, recordplatform.ErrLostOwnerLease) {
		t.Fatalf("WriteTo() error = %v, want revoked lost-owner renewal failure", got.err)
	}
	calls, output := writer.snapshot()
	if got.written != 4 || calls != 1 || output != "0123" {
		t.Fatalf("WriteTo() after blocked writer revocation = %d bytes, writer calls/output %d/%q, want 4/1/first chunk",
			got.written, calls, output)
	}
}

func TestContentDeliveryCloseCancelsInFlightBackgroundRenewal(t *testing.T) {
	t.Parallel()

	const leaseDuration = 600 * time.Millisecond
	service, _, leases := newBackgroundRenewalDownloadFixture(t, leaseDuration, nil, true)
	delivery, err := service.Open(context.Background(), validDownloadRequest(downloadActor(t, downloadAuthorID)))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	waitForDownloadSignal(t, leases.renewStarted, "background renewal")

	closeResult := make(chan error, 1)
	go func() { closeResult <- delivery.Close(context.Background()) }()
	waitForDownloadSignal(t, leases.renewCanceled, "in-flight renewal cancellation")
	select {
	case closeErr := <-closeResult:
		if closeErr != nil {
			t.Fatalf("Close() error = %v", closeErr)
		}
	case <-time.After(leaseDuration / 2):
		t.Fatal("Close() did not finish after canceling the in-flight renewal")
	}
	if leases.releaseCalls() != 1 {
		t.Fatalf("Close() release calls = %d, want 1", leases.releaseCalls())
	}
}

func TestContentDeliveryStopsBeforeWritingAfterLeaseRenewalFailure(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.August, 7, 8, 0, 0, 0, time.UTC)
	clock := &downloadTestClock{current: base}
	reader := &advancingDownloadReader{
		content: []byte("0123456789"), clock: clock,
		advances: []time.Duration{time.Millisecond, 6 * time.Millisecond},
	}
	lease := recordplatform.ServingLeaseV1{
		Object: downloadRecordObject(),
		Owner: recordplatform.OwnerLease{
			OwnerID: "attachment_delivery_failure", Generation: 1, ExpiresAt: base.Add(10 * time.Millisecond),
		},
		CapturedEpoch: 3,
	}
	repository := &downloadRepositoryStub{attachment: downloadRecordAttachment()}
	authorizer := &downloadAuthorizerStub{}
	leases := &downloadLeaseStub{serving: lease, clock: clock, renewErr: recordplatform.ErrLostOwnerLease}
	blob := &downloadBlobStub{content: []byte("0123456789"), openReader: reader}
	service, err := NewDownloadService(repository, authorizer, leases, blob, DownloadServiceOptions{
		Now: clock.Now, LeaseDuration: 10 * time.Millisecond, ChunkBytes: 4,
		Limits: DefaultLimits(), NewLeaseOwnerID: func() (string, error) { return lease.Owner.OwnerID, nil },
	})
	if err != nil {
		t.Fatalf("NewDownloadService() error = %v", err)
	}
	delivery, err := service.Open(context.Background(), validDownloadRequest(downloadActor(t, downloadAuthorID)))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = delivery.Close(context.Background()) }()

	var output bytes.Buffer
	written, err := delivery.WriteTo(context.Background(), &output)
	if !errors.Is(err, ErrContentDeliveryRevoked) {
		t.Fatalf("WriteTo() error = %v, want ErrContentDeliveryRevoked", err)
	}
	if written != 4 || output.String() != "0123" {
		t.Fatalf("WriteTo() = %d/%q, want one chunk before renewal failure", written, output.String())
	}
}

func TestContentDeliveryRejectsRenewedOwnerDriftBeforeWriting(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.August, 7, 8, 0, 0, 0, time.UTC)
	clock := &downloadTestClock{current: base}
	reader := &advancingDownloadReader{
		content: []byte("0123456789"), clock: clock,
		advances: []time.Duration{time.Millisecond, 6 * time.Millisecond},
	}
	lease := recordplatform.ServingLeaseV1{
		Object: downloadRecordObject(),
		Owner: recordplatform.OwnerLease{
			OwnerID: "attachment_delivery_drift", Generation: 1, ExpiresAt: base.Add(10 * time.Millisecond),
		},
		CapturedEpoch: 3,
	}
	repository := &downloadRepositoryStub{attachment: downloadRecordAttachment()}
	authorizer := &downloadAuthorizerStub{}
	leases := &downloadLeaseStub{serving: lease, clock: clock, renewGeneration: 2}
	blob := &downloadBlobStub{content: []byte("0123456789"), openReader: reader}
	service, err := NewDownloadService(repository, authorizer, leases, blob, DownloadServiceOptions{
		Now: clock.Now, LeaseDuration: 10 * time.Millisecond, ChunkBytes: 4,
		Limits: DefaultLimits(), NewLeaseOwnerID: func() (string, error) { return lease.Owner.OwnerID, nil },
	})
	if err != nil {
		t.Fatalf("NewDownloadService() error = %v", err)
	}
	delivery, err := service.Open(context.Background(), validDownloadRequest(downloadActor(t, downloadAuthorID)))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = delivery.Close(context.Background()) }()

	var output bytes.Buffer
	written, err := delivery.WriteTo(context.Background(), &output)
	if !errors.Is(err, ErrContentDeliveryRevoked) {
		t.Fatalf("WriteTo() error = %v, want ErrContentDeliveryRevoked", err)
	}
	if written != 4 || output.String() != "0123" {
		t.Fatalf("WriteTo() = %d/%q, want one chunk before owner drift", written, output.String())
	}
}

func TestContentDeliveryAssertionUsesLockedLeaseSnapshot(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 7, 8, 0, 0, 0, time.UTC)
	serving := recordplatform.ServingLeaseV1{
		Object: downloadRecordObject(),
		Owner: recordplatform.OwnerLease{
			OwnerID: "attachment_delivery_assert_snapshot", Generation: 1, ExpiresAt: now.Add(time.Hour),
		},
		CapturedEpoch: 3,
	}
	delivery := &ContentDelivery{
		repository: statelessDownloadRepository{}, leases: statelessDownloadLeaseRepository{},
		now: func() time.Time { return now },
		assertion: ContentAssertion{
			ProjectID: "default", AttachmentID: "att_download1", DraftID: "rdf_download1",
			AuthorID: downloadAuthorID, Variant: ContentVariantOriginal, Object: downloadOriginalObject(),
		},
		serving: &serving, expiresAt: serving.Owner.ExpiresAt,
	}

	started := make(chan struct{})
	done := make(chan struct{})
	go func() {
		close(started)
		for index := 0; index < 10_000; index++ {
			renewed := serving
			renewed.Owner.ExpiresAt = now.Add(time.Hour + time.Duration(index+1)*time.Nanosecond)
			delivery.mu.Lock()
			delivery.serving = &renewed
			delivery.expiresAt = renewed.Owner.ExpiresAt
			delivery.mu.Unlock()
		}
		close(done)
	}()
	<-started
	for {
		select {
		case <-done:
			return
		default:
			if err := delivery.assert(context.Background()); err != nil {
				t.Fatalf("assert() error = %v, want nil", err)
			}
		}
	}
}

func TestContentDeliveryCloseWaitsForRenewalAndReleasesLatestLease(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.August, 7, 8, 0, 0, 0, time.UTC)
	original := recordplatform.ServingLeaseV1{
		Object: downloadRecordObject(),
		Owner: recordplatform.OwnerLease{
			OwnerID: "attachment_delivery_close_renewal", Generation: 1, ExpiresAt: base.Add(10 * time.Millisecond),
		},
		CapturedEpoch: 3,
	}
	renewed := original
	renewed.Owner.ExpiresAt = base.Add(16 * time.Millisecond)
	leases := &blockingRenewDownloadLease{
		renewed: renewed, renewStarted: make(chan struct{}), allowRenew: make(chan struct{}),
		releaseCalled: make(chan struct{}),
	}
	delivery := &ContentDelivery{
		leases: leases, now: func() time.Time { return base.Add(6 * time.Millisecond) },
		serving: &original, expiresAt: original.Owner.ExpiresAt, leaseDuration: 10 * time.Millisecond,
	}

	renewResult := make(chan error, 1)
	go func() { renewResult <- delivery.renewIfNeeded(context.Background()) }()
	<-leases.renewStarted
	closeResult := make(chan error, 1)
	go func() { closeResult <- delivery.Close(context.Background()) }()

	select {
	case <-leases.releaseCalled:
		close(leases.allowRenew)
		<-renewResult
		<-closeResult
		t.Fatal("Close() released a stale lease while renewal was still in flight")
	case <-time.After(25 * time.Millisecond):
	}
	close(leases.allowRenew)
	if err := <-renewResult; err != nil {
		t.Fatalf("renewIfNeeded() error = %v, want nil", err)
	}
	if err := <-closeResult; err != nil {
		t.Fatalf("Close() error = %v, want nil", err)
	}
	if leases.released != renewed.Owner {
		t.Fatalf("Close() released owner %#v, want renewed owner %#v", leases.released, renewed.Owner)
	}
}

func TestContentDeliveryEnforcesDeclaredRangeFraming(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		content    []byte
		wantErr    error
		wantBytes  int
		wantOutput string
	}{
		{name: "short reader", content: []byte("01234567"), wantErr: io.ErrUnexpectedEOF, wantBytes: 8, wantOutput: "01234567"},
		{name: "exact reader", content: []byte("0123456789"), wantBytes: 10, wantOutput: "0123456789"},
		{name: "over reader", content: []byte("0123456789x"), wantErr: ErrInvalidDownloadContent, wantBytes: 8, wantOutput: "01234567"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newDownloadServiceFixture(t, downloadDraftAttachment())
			fixture.blob.content = tt.content
			delivery, err := fixture.service.Open(context.Background(), validDownloadRequest(downloadActor(t, downloadAuthorID)))
			if err != nil {
				t.Fatalf("Open() error = %v", err)
			}
			defer func() { _ = delivery.Close(context.Background()) }()
			var output bytes.Buffer
			written, err := delivery.WriteTo(context.Background(), &output)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("WriteTo() error = %v, want nil", err)
				}
			} else if !errors.Is(err, tt.wantErr) {
				t.Fatalf("WriteTo() error = %v, want %v", err, tt.wantErr)
			}
			if written != int64(tt.wantBytes) || output.String() != tt.wantOutput {
				t.Fatalf("WriteTo() = %d/%q, want %d/%q", written, output.String(), tt.wantBytes, tt.wantOutput)
			}
		})
	}
}

func TestContentDeliveryCloseUnblocksBlockedReader(t *testing.T) {
	t.Parallel()

	reader := &blockingDownloadReader{started: make(chan struct{}), released: make(chan struct{})}
	now := time.Date(2026, time.August, 7, 8, 0, 0, 0, time.UTC)
	fixture := newDownloadServiceFixture(t, downloadDraftAttachment())
	delivery := &ContentDelivery{
		repository: fixture.repository, authorizer: fixture.authorizer, leases: fixture.leases,
		actor: downloadActor(t, downloadAuthorID), now: func() time.Time { return now },
		assertion: ContentAssertion{
			ProjectID: "default", AttachmentID: "att_download1", DraftID: "rdf_download1",
			AuthorID: downloadAuthorID, Variant: ContentVariantOriginal, Object: downloadOriginalObject(),
		},
		expiresAt: now.Add(time.Minute), chunkBytes: 4,
		metadata: DownloadMetadata{
			AttachmentID: "att_download1", Variant: ContentVariantOriginal,
			Object: downloadOriginalObject(), Range: ResolvedContentRange{Length: 1},
		},
		reader: reader,
	}

	result := make(chan error, 1)
	go func() {
		_, err := delivery.WriteTo(context.Background(), io.Discard)
		result <- err
	}()
	select {
	case <-reader.started:
	case <-time.After(time.Second):
		t.Fatal("WriteTo() did not reach blocked reader")
	}
	if err := delivery.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	select {
	case err := <-result:
		if !errors.Is(err, ErrContentDeliveryRevoked) {
			t.Fatalf("WriteTo() error = %v, want ErrContentDeliveryRevoked", err)
		}
	case <-time.After(time.Second):
		t.Fatal("WriteTo() remained blocked after Close()")
	}
}

func TestContentDeliveryContextCancellationUnblocksBlockedReader(t *testing.T) {
	t.Parallel()

	reader := &blockingDownloadReader{started: make(chan struct{}), released: make(chan struct{})}
	now := time.Date(2026, time.August, 7, 8, 0, 0, 0, time.UTC)
	fixture := newDownloadServiceFixture(t, downloadDraftAttachment())
	delivery := &ContentDelivery{
		repository: fixture.repository, authorizer: fixture.authorizer, leases: fixture.leases,
		actor: downloadActor(t, downloadAuthorID), now: func() time.Time { return now },
		assertion: ContentAssertion{
			ProjectID: "default", AttachmentID: "att_download1", DraftID: "rdf_download1",
			AuthorID: downloadAuthorID, Variant: ContentVariantOriginal, Object: downloadOriginalObject(),
		},
		expiresAt: now.Add(time.Minute), chunkBytes: 4,
		metadata: DownloadMetadata{
			AttachmentID: "att_download1", Variant: ContentVariantOriginal,
			Object: downloadOriginalObject(), Range: ResolvedContentRange{Length: 1},
		},
		reader: reader,
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := delivery.WriteTo(ctx, io.Discard)
		result <- err
	}()
	select {
	case <-reader.started:
	case <-time.After(time.Second):
		t.Fatal("WriteTo() did not reach blocked reader")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, ErrContentDeliveryRevoked) || !errors.Is(err, context.Canceled) {
			t.Fatalf("WriteTo(cancelled) error = %v, want revoked context cancellation", err)
		}
	case <-time.After(time.Second):
		_ = delivery.Close(context.Background())
		t.Fatal("WriteTo() remained blocked after context cancellation")
	}
	if err := delivery.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

type downloadServiceFixture struct {
	service    *DownloadService
	repository *downloadRepositoryStub
	authorizer *downloadAuthorizerStub
	leases     *downloadLeaseStub
	blob       *downloadBlobStub
}

func newDownloadServiceFixture(t *testing.T, attachment AttachmentContent) downloadServiceFixture {
	t.Helper()
	now := time.Date(2026, time.August, 7, 8, 0, 0, 0, time.UTC)
	repository := &downloadRepositoryStub{attachment: attachment}
	authorizer := &downloadAuthorizerStub{}
	leases := &downloadLeaseStub{serving: recordplatform.ServingLeaseV1{
		Object: downloadRecordObject(),
		Owner: recordplatform.OwnerLease{
			OwnerID: "attachment_delivery_test", Generation: 1, ExpiresAt: now.Add(time.Second),
		},
		CapturedEpoch: 3,
	}}
	blob := &downloadBlobStub{content: []byte("0123456789"), reader: &downloadReadCloser{Reader: bytes.NewReader([]byte("0123456789"))}}
	service, err := NewDownloadService(repository, authorizer, leases, blob, DownloadServiceOptions{
		Now: func() time.Time { return now }, LeaseDuration: time.Second, ChunkBytes: 4,
		Limits: DefaultLimits(), NewLeaseOwnerID: func() (string, error) { return "attachment_delivery_test", nil },
	})
	if err != nil {
		t.Fatalf("NewDownloadService() error = %v", err)
	}
	return downloadServiceFixture{service: service, repository: repository, authorizer: authorizer, leases: leases, blob: blob}
}

func newBackgroundRenewalDownloadFixture(
	t *testing.T,
	leaseDuration time.Duration,
	renewErr error,
	blockRenewUntilCanceled bool,
) (*DownloadService, *blockingDownloadReader, *backgroundRenewalDownloadLease) {
	t.Helper()
	ownerID := "attachment_delivery_background"
	leases := &backgroundRenewalDownloadLease{
		serving: recordplatform.ServingLeaseV1{
			Object: downloadRecordObject(),
			Owner: recordplatform.OwnerLease{
				OwnerID: ownerID, Generation: 1, ExpiresAt: time.Now().Add(leaseDuration),
			},
			CapturedEpoch: 3,
		},
		renewErr:                renewErr,
		blockRenewUntilCanceled: blockRenewUntilCanceled,
		renewStarted:            make(chan struct{}),
		renewCanceled:           make(chan struct{}),
		renewed:                 make(chan struct{}, 8),
	}
	reader := &blockingDownloadReader{started: make(chan struct{}), released: make(chan struct{})}
	service, err := NewDownloadService(
		&downloadRepositoryStub{attachment: downloadRecordAttachment()},
		&downloadAuthorizerStub{},
		leases,
		&downloadBlobStub{content: []byte("0123456789"), openReader: reader},
		DownloadServiceOptions{
			Now: time.Now, LeaseDuration: leaseDuration, ChunkBytes: 4,
			Limits: DefaultLimits(), NewLeaseOwnerID: func() (string, error) { return ownerID, nil },
		},
	)
	if err != nil {
		t.Fatalf("NewDownloadService() error = %v", err)
	}
	return service, reader, leases
}

func waitForDownloadSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func validDownloadRequest(actor recordauth.ActorScope) DownloadRequest {
	return DownloadRequest{Actor: actor, AttachmentID: "att_download1", Variant: ContentVariantOriginal}
}

func downloadDraftAttachment() AttachmentContent {
	preview := ManagedPreviewContent{Object: downloadPreviewObject(), MediaType: ManagedPreviewMediaTypeTextUTF8}
	return AttachmentContent{
		ProjectID: "default", AttachmentID: "att_download1", DraftID: "rdf_download1",
		AuthorID: downloadAuthorID, State: UploadStateAvailable, DisplayName: "notes.txt",
		MediaType: "text/plain", LogicalSizeBytes: 10, Original: downloadOriginalObject(), Preview: &preview,
	}
}

func downloadRecordAttachment() AttachmentContent {
	attachment := downloadDraftAttachment()
	attachment.DraftID = ""
	attachment.RecordID = "rec_download1"
	return attachment
}

func downloadOriginalObject() ObjectVersion { return downloadObject(0x11, 10, "original-v1") }
func downloadPreviewObject() ObjectVersion  { return downloadObject(0x22, 10, "preview-v1") }

func downloadObject(fill byte, size int64, version string) ObjectVersion {
	digest := [sha256.Size]byte{}
	for index := range digest {
		digest[index] = fill
	}
	return ObjectVersion{Key: "sha256/" + strings.Repeat(string([]byte{hexDigit(fill >> 4), hexDigit(fill & 0x0f)}), sha256.Size), VersionID: version, SHA256: digest, SizeBytes: size}
}

func hexDigit(value byte) byte {
	if value < 10 {
		return '0' + value
	}
	return 'a' + value - 10
}

func downloadRecordObject() recordplatform.ObjectRef {
	return recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: "rec_download1"}
}

func downloadActor(t *testing.T, userID string) recordauth.ActorScope {
	t.Helper()
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID: userID, Role: recordauth.RoleViewer, ProjectID: recordauth.ProjectIDDefault,
	})
	if err != nil {
		t.Fatalf("NormalizeActorScope() error = %v", err)
	}
	return actor
}

type downloadRepositoryStub struct {
	attachment    AttachmentContent
	getErr        error
	assertCalls   int
	assertErrAt   int
	assertErr     error
	lastAssertion ContentAssertion
}

type statelessDownloadRepository struct{}

func (statelessDownloadRepository) GetAttachmentForDownload(context.Context, ContentLookup) (AttachmentContent, error) {
	return AttachmentContent{}, errors.New("unexpected GetAttachmentForDownload")
}

func (statelessDownloadRepository) AssertAttachmentContent(context.Context, ContentAssertion) error {
	return nil
}

type statelessDownloadLeaseRepository struct{}

func (statelessDownloadLeaseRepository) AcquireServingLease(context.Context, recordplatform.ObjectRef, recordplatform.LeaseClaimInputV1) (recordplatform.ServingLeaseV1, error) {
	return recordplatform.ServingLeaseV1{}, errors.New("unexpected AcquireServingLease")
}

func (statelessDownloadLeaseRepository) RenewServingLease(context.Context, recordplatform.ServingLeaseV1, time.Duration) (recordplatform.ServingLeaseV1, error) {
	return recordplatform.ServingLeaseV1{}, errors.New("unexpected RenewServingLease")
}

func (statelessDownloadLeaseRepository) AssertServingLease(context.Context, recordplatform.ServingLeaseV1) error {
	return nil
}

func (statelessDownloadLeaseRepository) ReleaseObjectContentLease(context.Context, recordplatform.ObjectRef, recordplatform.OwnerLease) error {
	return nil
}

type blockingRenewDownloadLease struct {
	renewed       recordplatform.ServingLeaseV1
	renewStarted  chan struct{}
	allowRenew    chan struct{}
	releaseCalled chan struct{}
	released      recordplatform.OwnerLease
}

type finalAssertionDownloadLease struct {
	clock                *downloadTestClock
	advanceAtAssertion   time.Duration
	assertionStarted     chan struct{}
	allowAssertionReturn chan struct{}
	renewStarted         chan struct{}
	renewErr             error
	assertionOnce        sync.Once
	renewOnce            sync.Once
}

type linearizingFenceDownloadLease struct {
	mu                        sync.Mutex
	fenced                    bool
	assertionCalls            int
	firstAssertionObserved    chan struct{}
	allowFirstAssertionReturn chan struct{}
	firstAssertionOnce        sync.Once
}

type recordingDownloadWriter struct {
	calls  int
	output bytes.Buffer
}

var errReentrantCloseBlocked = errors.New("reentrant Close blocked by writer gate")

type reentrantCloseDownloadWriter struct {
	delivery     *ContentDelivery
	closeDone    chan struct{}
	calls        int
	closeErr     error
	closeBlocked bool
}

type blockingContentDownloadWriter struct {
	mu          sync.Mutex
	started     chan struct{}
	allowReturn chan struct{}
	startOnce   sync.Once
	calls       int
	output      bytes.Buffer
}

func newFinalWriteBoundaryDelivery(
	clock *downloadTestClock,
	leases ContentLeaseRepository,
	content []byte,
) *ContentDelivery {
	expiresAt := clock.Now().Add(10 * time.Millisecond)
	serving := recordplatform.ServingLeaseV1{
		Object: downloadRecordObject(),
		Owner: recordplatform.OwnerLease{
			OwnerID: "attachment_delivery_write_boundary", Generation: 1, ExpiresAt: expiresAt,
		},
		CapturedEpoch: 3,
	}
	return &ContentDelivery{
		repository: statelessDownloadRepository{}, leases: leases, now: clock.Now,
		assertion: ContentAssertion{
			ProjectID: "default", AttachmentID: "att_download1", DraftID: "rdf_download1",
			AuthorID: downloadAuthorID, Variant: ContentVariantOriginal, Object: downloadOriginalObject(),
		},
		serving: &serving, expiresAt: expiresAt, leaseDuration: 10 * time.Millisecond, chunkBytes: 4,
		metadata: DownloadMetadata{
			AttachmentID: "att_download1", Variant: ContentVariantOriginal,
			Object: downloadOriginalObject(), Range: ResolvedContentRange{Length: int64(len(content))},
		},
		reader: &downloadReadCloser{Reader: bytes.NewReader(content)},
	}
}

func (stub *finalAssertionDownloadLease) AcquireServingLease(context.Context, recordplatform.ObjectRef, recordplatform.LeaseClaimInputV1) (recordplatform.ServingLeaseV1, error) {
	return recordplatform.ServingLeaseV1{}, errors.New("unexpected AcquireServingLease")
}

func (stub *finalAssertionDownloadLease) RenewServingLease(context.Context, recordplatform.ServingLeaseV1, time.Duration) (recordplatform.ServingLeaseV1, error) {
	stub.renewOnce.Do(func() { close(stub.renewStarted) })
	return recordplatform.ServingLeaseV1{}, stub.renewErr
}

func (stub *finalAssertionDownloadLease) AssertServingLease(context.Context, recordplatform.ServingLeaseV1) error {
	stub.assertionOnce.Do(func() {
		if stub.clock != nil && stub.advanceAtAssertion != 0 {
			stub.clock.mu.Lock()
			stub.clock.current = stub.clock.current.Add(stub.advanceAtAssertion)
			stub.clock.mu.Unlock()
		}
		close(stub.assertionStarted)
	})
	<-stub.allowAssertionReturn
	return nil
}

func (*finalAssertionDownloadLease) ReleaseObjectContentLease(context.Context, recordplatform.ObjectRef, recordplatform.OwnerLease) error {
	return nil
}

func (*linearizingFenceDownloadLease) AcquireServingLease(context.Context, recordplatform.ObjectRef, recordplatform.LeaseClaimInputV1) (recordplatform.ServingLeaseV1, error) {
	return recordplatform.ServingLeaseV1{}, errors.New("unexpected AcquireServingLease")
}

func (*linearizingFenceDownloadLease) RenewServingLease(context.Context, recordplatform.ServingLeaseV1, time.Duration) (recordplatform.ServingLeaseV1, error) {
	return recordplatform.ServingLeaseV1{}, errors.New("unexpected RenewServingLease")
}

func (stub *linearizingFenceDownloadLease) AssertServingLease(context.Context, recordplatform.ServingLeaseV1) error {
	stub.mu.Lock()
	stub.assertionCalls++
	firstAssertion := stub.assertionCalls == 1
	fenced := stub.fenced
	if firstAssertion {
		stub.firstAssertionOnce.Do(func() { close(stub.firstAssertionObserved) })
	}
	stub.mu.Unlock()
	if firstAssertion {
		<-stub.allowFirstAssertionReturn
	}
	if fenced {
		return recordplatform.ErrLostOwnerLease
	}
	return nil
}

func (*linearizingFenceDownloadLease) ReleaseObjectContentLease(context.Context, recordplatform.ObjectRef, recordplatform.OwnerLease) error {
	return nil
}

func (stub *linearizingFenceDownloadLease) commitFence() {
	stub.mu.Lock()
	stub.fenced = true
	stub.mu.Unlock()
}

func (stub *linearizingFenceDownloadLease) assertionCallCount() int {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	return stub.assertionCalls
}

func (writer *recordingDownloadWriter) Write(payload []byte) (int, error) {
	writer.calls++
	return writer.output.Write(payload)
}

func (writer *reentrantCloseDownloadWriter) Write(payload []byte) (int, error) {
	writer.calls++
	go func() {
		writer.closeErr = writer.delivery.Close(context.Background())
		close(writer.closeDone)
	}()
	select {
	case <-writer.closeDone:
		return len(payload), writer.closeErr
	case <-time.After(time.Second):
		writer.closeBlocked = true
		return 0, errReentrantCloseBlocked
	}
}

func (writer *blockingContentDownloadWriter) Write(payload []byte) (int, error) {
	writer.mu.Lock()
	writer.calls++
	writer.startOnce.Do(func() { close(writer.started) })
	writer.mu.Unlock()
	<-writer.allowReturn
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return writer.output.Write(payload)
}

func (writer *blockingContentDownloadWriter) snapshot() (int, string) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return writer.calls, writer.output.String()
}

type backgroundRenewalDownloadLease struct {
	mu                      sync.Mutex
	serving                 recordplatform.ServingLeaseV1
	renewErr                error
	blockRenewUntilCanceled bool
	renewStartOnce          sync.Once
	renewCancelOnce         sync.Once
	renewStarted            chan struct{}
	renewCanceled           chan struct{}
	renewed                 chan struct{}
	releases                int
}

func (stub *backgroundRenewalDownloadLease) AcquireServingLease(
	_ context.Context,
	_ recordplatform.ObjectRef,
	_ recordplatform.LeaseClaimInputV1,
) (recordplatform.ServingLeaseV1, error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	return stub.serving, nil
}

func (stub *backgroundRenewalDownloadLease) RenewServingLease(
	ctx context.Context,
	serving recordplatform.ServingLeaseV1,
	duration time.Duration,
) (recordplatform.ServingLeaseV1, error) {
	stub.renewStartOnce.Do(func() { close(stub.renewStarted) })
	if stub.blockRenewUntilCanceled {
		<-ctx.Done()
		stub.renewCancelOnce.Do(func() { close(stub.renewCanceled) })
		return recordplatform.ServingLeaseV1{}, ctx.Err()
	}
	select {
	case stub.renewed <- struct{}{}:
	default:
	}
	if stub.renewErr != nil {
		return recordplatform.ServingLeaseV1{}, stub.renewErr
	}
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if serving != stub.serving {
		return recordplatform.ServingLeaseV1{}, recordplatform.ErrLostOwnerLease
	}
	stub.serving.Owner.ExpiresAt = time.Now().Add(duration)
	return stub.serving, nil
}

func (*backgroundRenewalDownloadLease) AssertServingLease(context.Context, recordplatform.ServingLeaseV1) error {
	return nil
}

func (stub *backgroundRenewalDownloadLease) ReleaseObjectContentLease(
	_ context.Context,
	_ recordplatform.ObjectRef,
	owner recordplatform.OwnerLease,
) error {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if owner != stub.serving.Owner {
		return recordplatform.ErrLostOwnerLease
	}
	stub.releases++
	return nil
}

func (stub *backgroundRenewalDownloadLease) releaseCalls() int {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	return stub.releases
}

func (stub *blockingRenewDownloadLease) AcquireServingLease(context.Context, recordplatform.ObjectRef, recordplatform.LeaseClaimInputV1) (recordplatform.ServingLeaseV1, error) {
	return recordplatform.ServingLeaseV1{}, errors.New("unexpected AcquireServingLease")
}

func (stub *blockingRenewDownloadLease) RenewServingLease(context.Context, recordplatform.ServingLeaseV1, time.Duration) (recordplatform.ServingLeaseV1, error) {
	close(stub.renewStarted)
	<-stub.allowRenew
	return stub.renewed, nil
}

func (stub *blockingRenewDownloadLease) AssertServingLease(context.Context, recordplatform.ServingLeaseV1) error {
	return nil
}

func (stub *blockingRenewDownloadLease) ReleaseObjectContentLease(_ context.Context, _ recordplatform.ObjectRef, owner recordplatform.OwnerLease) error {
	stub.released = owner
	close(stub.releaseCalled)
	return nil
}

func (stub *downloadRepositoryStub) GetAttachmentForDownload(context.Context, ContentLookup) (AttachmentContent, error) {
	return stub.attachment, stub.getErr
}

func (stub *downloadRepositoryStub) AssertAttachmentContent(_ context.Context, assertion ContentAssertion) error {
	stub.assertCalls++
	stub.lastAssertion = assertion
	if stub.assertErrAt > 0 && stub.assertCalls >= stub.assertErrAt {
		return stub.assertErr
	}
	return nil
}

type downloadAuthorizerStub struct {
	calls    int
	recordID string
	err      error
}

func (stub *downloadAuthorizerStub) AuthorizeRecordAttachmentRead(_ context.Context, _ recordauth.ActorScope, recordID string) error {
	stub.calls++
	stub.recordID = recordID
	return stub.err
}

type downloadLeaseStub struct {
	serving         recordplatform.ServingLeaseV1
	clock           *downloadTestClock
	object          recordplatform.ObjectRef
	acquireCalls    int
	assertCalls     int
	renewCalls      int
	releaseCalls    int
	assertErr       error
	renewErr        error
	renewGeneration uint64
}

func (stub *downloadLeaseStub) AcquireServingLease(_ context.Context, object recordplatform.ObjectRef, _ recordplatform.LeaseClaimInputV1) (recordplatform.ServingLeaseV1, error) {
	stub.acquireCalls++
	stub.object = object
	return stub.serving, nil
}

func (stub *downloadLeaseStub) AssertServingLease(context.Context, recordplatform.ServingLeaseV1) error {
	stub.assertCalls++
	return stub.assertErr
}

func (stub *downloadLeaseStub) RenewServingLease(_ context.Context, serving recordplatform.ServingLeaseV1, duration time.Duration) (recordplatform.ServingLeaseV1, error) {
	stub.renewCalls++
	if stub.renewErr != nil {
		return recordplatform.ServingLeaseV1{}, stub.renewErr
	}
	if stub.renewGeneration != 0 {
		serving.Owner.Generation = stub.renewGeneration
	}
	if stub.clock != nil {
		serving.Owner.ExpiresAt = stub.clock.Now().Add(duration)
	}
	return serving, nil
}

func (stub *downloadLeaseStub) ReleaseObjectContentLease(context.Context, recordplatform.ObjectRef, recordplatform.OwnerLease) error {
	stub.releaseCalls++
	return nil
}

type downloadBlobStub struct {
	content     []byte
	openReader  io.ReadCloser
	reader      *downloadReadCloser
	openCalls   int
	openVersion ObjectVersion
	openRange   ByteRange
}

func (stub *downloadBlobStub) Put(context.Context, PutRequest, io.Reader) (ObjectVersion, error) {
	return ObjectVersion{}, errors.New("unexpected Put")
}

func (stub *downloadBlobStub) Open(_ context.Context, version ObjectVersion, byteRange ByteRange) (io.ReadCloser, error) {
	stub.openCalls++
	stub.openVersion = version
	stub.openRange = byteRange
	if stub.openReader != nil {
		return stub.openReader, nil
	}
	start, end := int64(0), int64(len(stub.content)-1)
	if byteRange.kind == byteRangeKindClosed {
		start, end = byteRange.Start, byteRange.EndInclusive
	}
	stub.reader = &downloadReadCloser{Reader: bytes.NewReader(stub.content[start : end+1])}
	return stub.reader, nil
}

func (stub *downloadBlobStub) Stat(_ context.Context, version ObjectVersion) (ObjectInfo, error) {
	return ObjectInfo{Version: version}, nil
}

func (stub *downloadBlobStub) Delete(context.Context, ObjectVersion) (DeletionReceipt, error) {
	return DeletionReceipt{}, errors.New("unexpected Delete")
}

type downloadReadCloser struct {
	*bytes.Reader
	closeCalls int
}

type downloadTestClock struct {
	mu      sync.Mutex
	current time.Time
}

func (clock *downloadTestClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.current
}

type advancingDownloadReader struct {
	content   []byte
	index     int
	clock     *downloadTestClock
	advance   time.Duration
	advances  []time.Duration
	readCalls int
}

func (reader *advancingDownloadReader) Read(payload []byte) (int, error) {
	advance := reader.advance
	if reader.readCalls < len(reader.advances) {
		advance = reader.advances[reader.readCalls]
	}
	reader.readCalls++
	if reader.clock != nil {
		reader.clock.mu.Lock()
		reader.clock.current = reader.clock.current.Add(advance)
		reader.clock.mu.Unlock()
	}
	if reader.index >= len(reader.content) {
		return 0, io.EOF
	}
	count := copy(payload, reader.content[reader.index:])
	reader.index += count
	return count, nil
}

func (*advancingDownloadReader) Close() error { return nil }

var errBlockingDownloadReaderClosed = errors.New("blocking download reader closed")

type blockingDownloadReader struct {
	started     chan struct{}
	released    chan struct{}
	startOnce   sync.Once
	releaseOnce sync.Once
}

func (reader *blockingDownloadReader) Read([]byte) (int, error) {
	reader.startOnce.Do(func() { close(reader.started) })
	<-reader.released
	return 0, errBlockingDownloadReaderClosed
}

func (reader *blockingDownloadReader) Close() error {
	reader.releaseOnce.Do(func() { close(reader.released) })
	return nil
}

func (reader *downloadReadCloser) Close() error {
	reader.closeCalls++
	return nil
}

var _ DownloadRepository = (*downloadRepositoryStub)(nil)
var _ RecordDownloadAuthorizer = (*downloadAuthorizerStub)(nil)
var _ ContentLeaseRepository = (*downloadLeaseStub)(nil)
var _ BlobStore = (*downloadBlobStub)(nil)

func TestContentVariantParserDefaultsToOriginalAndRejectsOpenVocabulary(t *testing.T) {
	t.Parallel()

	for raw, want := range map[string]ContentVariant{"": ContentVariantOriginal, "original": ContentVariantOriginal, "preview": ContentVariantPreview} {
		got, err := ParseContentVariant(raw)
		if err != nil || got != want {
			t.Errorf("ParseContentVariant(%q) = %q, %v, want %q", raw, got, err, want)
		}
	}
	if got, err := ParseContentVariant("thumbnail"); got != "" || !errors.Is(err, ErrInvalidContentVariant) {
		t.Fatalf("ParseContentVariant(thumbnail) = %q, %v", got, err)
	}
}

func TestAttachmentContentValidationIsOwnerAndObjectExact(t *testing.T) {
	t.Parallel()

	valid := downloadDraftAttachment()
	if err := valid.Validate(); err != nil {
		t.Fatalf("AttachmentContent.Validate() error = %v", err)
	}
	mutations := []func(*AttachmentContent){
		func(value *AttachmentContent) { value.RecordID = "rec_download1" },
		func(value *AttachmentContent) { value.RecordID = "not-a-record-id" },
		func(value *AttachmentContent) { value.DraftID = "not-a-draft-id" },
		func(value *AttachmentContent) { value.AuthorID = "other" },
		func(value *AttachmentContent) { value.State = UploadState("unknown") },
		func(value *AttachmentContent) { value.Original.SizeBytes++ },
		func(value *AttachmentContent) { value.Preview.MediaType = "text/html" },
	}
	for index, mutate := range mutations {
		candidate := valid
		preview := *valid.Preview
		candidate.Preview = &preview
		mutate(&candidate)
		if err := candidate.Validate(); !errors.Is(err, ErrInvalidDownloadContent) {
			t.Errorf("mutation %d Validate() error = %v, want ErrInvalidDownloadContent", index, err)
		}
	}
}

func TestContentAssertionValidationRequiresExactlyOneValidOwnerRoute(t *testing.T) {
	t.Parallel()

	valid := ContentAssertion{
		ProjectID: "default", AttachmentID: "att_download1", DraftID: "rdf_download1",
		AuthorID: downloadAuthorID, Variant: ContentVariantOriginal, Object: downloadOriginalObject(),
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("ContentAssertion.Validate() error = %v", err)
	}
	mutations := []func(*ContentAssertion){
		func(value *ContentAssertion) { value.RecordID = "rec_download1" },
		func(value *ContentAssertion) { value.RecordID = "not-a-record-id" },
		func(value *ContentAssertion) { value.DraftID = "not-a-draft-id" },
		func(value *ContentAssertion) { value.DraftID = "" },
	}
	for index, mutate := range mutations {
		candidate := valid
		mutate(&candidate)
		if err := candidate.Validate(); !errors.Is(err, ErrInvalidDownloadRequest) {
			t.Errorf("mutation %d Validate() error = %v, want ErrInvalidDownloadRequest", index, err)
		}
	}
}

func TestDownloadMetadataIsDefensive(t *testing.T) {
	t.Parallel()

	fixture := newDownloadServiceFixture(t, downloadDraftAttachment())
	first, err := fixture.service.GetMetadata(context.Background(), MetadataRequest{
		Actor: downloadActor(t, downloadAuthorID), AttachmentID: "att_download1",
	})
	if err != nil {
		t.Fatalf("GetMetadata() error = %v", err)
	}
	second, err := fixture.service.GetMetadata(context.Background(), MetadataRequest{
		Actor: downloadActor(t, downloadAuthorID), AttachmentID: "att_download1",
	})
	if err != nil || !reflect.DeepEqual(first, second) || !first.PreviewAvailable || first.SizeBytes != 10 {
		t.Fatalf("GetMetadata() = %#v / %#v / %v", first, second, err)
	}
}
