package attachments

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
)

const (
	defaultContentLeaseDuration = time.Second
	defaultContentChunkBytes    = 32 * 1024
	maxContentLeaseDuration     = time.Second
)

var (
	ErrInvalidDownloadRequest           = errors.New("invalid attachment download request")
	ErrInvalidDownloadContent           = errors.New("invalid attachment download content")
	ErrInvalidContentVariant            = errors.New("invalid attachment content variant")
	ErrContentVariantUnavailable        = errors.New("attachment content variant unavailable")
	ErrContentUnavailable               = errors.New("attachment content unavailable")
	ErrInvalidContentRange              = errors.New("invalid attachment content range")
	ErrContentRangeNotSatisfiable       = errors.New("attachment content range not satisfiable")
	ErrContentPreviewTooLarge           = errors.New("attachment content preview too large")
	ErrContentDeliveryRevoked           = errors.New("attachment content delivery revoked")
	ErrContentDeliveryExpired     error = fmt.Errorf("%w: lease expired", ErrContentDeliveryRevoked)
)

// ContentRangeError preserves the selected immutable variant size after the
// request has passed authorization. The HTTP layer can therefore emit an
// exact 416 Content-Range without reusing the original attachment size.
type ContentRangeError struct {
	SizeBytes int64
	Cause     error
}

func NewContentRangeError(sizeBytes int64, cause error) error {
	if sizeBytes <= 0 || cause == nil {
		return ErrInvalidContentRange
	}
	return &ContentRangeError{SizeBytes: sizeBytes, Cause: cause}
}

func (err *ContentRangeError) Error() string {
	if err == nil || err.Cause == nil {
		return ErrInvalidContentRange.Error()
	}
	return err.Cause.Error()
}

func (err *ContentRangeError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Cause
}

func ContentRangeSize(err error) (int64, bool) {
	var rangeErr *ContentRangeError
	if !errors.As(err, &rangeErr) || rangeErr == nil || rangeErr.SizeBytes <= 0 {
		return 0, false
	}
	return rangeErr.SizeBytes, true
}

type ContentVariant string

const (
	ContentVariantOriginal ContentVariant = "original"
	ContentVariantPreview  ContentVariant = "preview"
)

func ParseContentVariant(raw string) (ContentVariant, error) {
	switch ContentVariant(raw) {
	case "", ContentVariantOriginal:
		return ContentVariantOriginal, nil
	case ContentVariantPreview:
		return ContentVariantPreview, nil
	default:
		return "", ErrInvalidContentVariant
	}
}

type ManagedPreviewContent struct {
	Object    ObjectVersion
	MediaType string
}

type AttachmentContent struct {
	ProjectID        string
	AttachmentID     string
	DraftID          string
	RecordID         string
	AuthorID         string
	State            UploadState
	DisplayName      string
	MediaType        string
	LogicalSizeBytes int64
	Original         ObjectVersion
	Preview          *ManagedPreviewContent
}

func (content AttachmentContent) Validate() error {
	if content.ProjectID != string(recordauth.ProjectIDDefault) ||
		ValidateAttachmentID(content.AttachmentID) != nil ||
		recordauth.ValidateActorUserID(content.AuthorID) != nil ||
		!validAttachmentText(content.DisplayName) || !validAttachmentText(content.MediaType) ||
		content.LogicalSizeBytes <= 0 || !knownDownloadUploadState(content.State) ||
		!validDownloadOwnerRoute(content.DraftID, content.RecordID) {
		return ErrInvalidDownloadContent
	}

	if content.State == UploadStateAvailable {
		if content.Original.Validate() != nil || content.Original.SizeBytes != content.LogicalSizeBytes {
			return ErrInvalidDownloadContent
		}
	} else if content.Original != (ObjectVersion{}) || content.Preview != nil {
		return ErrInvalidDownloadContent
	}
	if content.Preview != nil {
		if content.State != UploadStateAvailable || content.Preview.Object.Validate() != nil ||
			!managedPreviewMediaType(content.Preview.MediaType) {
			return ErrInvalidDownloadContent
		}
	}
	return nil
}

func knownDownloadUploadState(state UploadState) bool {
	switch state {
	case UploadStateCreated, UploadStateUploading, UploadStateQuarantined,
		UploadStateAvailable, UploadStateRejected, UploadStateExpired:
		return true
	default:
		return false
	}
}

func managedPreviewMediaType(mediaType string) bool {
	return mediaType == ManagedPreviewMediaTypePNG || mediaType == ManagedPreviewMediaTypeTextUTF8
}

type ContentLookup struct {
	ProjectID    string
	AttachmentID string
}

func (lookup ContentLookup) Validate() error {
	if lookup.ProjectID != string(recordauth.ProjectIDDefault) || ValidateAttachmentID(lookup.AttachmentID) != nil {
		return ErrInvalidDownloadRequest
	}
	return nil
}

type ContentAssertion struct {
	ProjectID    string
	AttachmentID string
	DraftID      string
	RecordID     string
	AuthorID     string
	Variant      ContentVariant
	Object       ObjectVersion
}

func (assertion ContentAssertion) Validate() error {
	if (ContentLookup{ProjectID: assertion.ProjectID, AttachmentID: assertion.AttachmentID}).Validate() != nil ||
		recordauth.ValidateActorUserID(assertion.AuthorID) != nil || assertion.Object.Validate() != nil ||
		!validDownloadOwnerRoute(assertion.DraftID, assertion.RecordID) {
		return ErrInvalidDownloadRequest
	}
	if _, err := ParseContentVariant(string(assertion.Variant)); err != nil || assertion.Variant == "" {
		return ErrInvalidDownloadRequest
	}
	return nil
}

func validDownloadOwnerRoute(draftID, recordID string) bool {
	if (draftID == "") == (recordID == "") {
		return false
	}
	if draftID != "" {
		return validPrefixedID(draftID, "rdf_")
	}
	return validPrefixedID(recordID, "rec_")
}

type DownloadRepository interface {
	GetAttachmentForDownload(context.Context, ContentLookup) (AttachmentContent, error)
	AssertAttachmentContent(context.Context, ContentAssertion) error
}

type RecordDownloadAuthorizer interface {
	AuthorizeRecordAttachmentRead(context.Context, recordauth.ActorScope, string) error
}

type ContentLeaseRepository interface {
	AcquireServingLease(context.Context, recordplatform.ObjectRef, recordplatform.LeaseClaimInputV1) (recordplatform.ServingLeaseV1, error)
	RenewServingLease(context.Context, recordplatform.ServingLeaseV1, time.Duration) (recordplatform.ServingLeaseV1, error)
	AssertServingLease(context.Context, recordplatform.ServingLeaseV1) error
	ReleaseObjectContentLease(context.Context, recordplatform.ObjectRef, recordplatform.OwnerLease) error
}

type DownloadServiceOptions struct {
	Now             func() time.Time
	LeaseDuration   time.Duration
	ChunkBytes      int
	Limits          Limits
	NewLeaseOwnerID func() (string, error)
}

type DownloadService struct {
	repository      DownloadRepository
	authorizer      RecordDownloadAuthorizer
	leases          ContentLeaseRepository
	blob            BlobStore
	now             func() time.Time
	leaseDuration   time.Duration
	chunkBytes      int
	limits          Limits
	newLeaseOwnerID func() (string, error)
}

func NewDownloadService(
	repository DownloadRepository,
	authorizer RecordDownloadAuthorizer,
	leases ContentLeaseRepository,
	blob BlobStore,
	options DownloadServiceOptions,
) (*DownloadService, error) {
	if nilDownloadDependency(repository) || nilDownloadDependency(authorizer) ||
		nilDownloadDependency(leases) || nilDownloadDependency(blob) {
		return nil, fmt.Errorf("%w: dependency", ErrInvalidDownloadRequest)
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.LeaseDuration == 0 {
		options.LeaseDuration = defaultContentLeaseDuration
	}
	if options.ChunkBytes == 0 {
		options.ChunkBytes = defaultContentChunkBytes
	}
	if options.Limits == (Limits{}) {
		options.Limits = DefaultLimits()
	}
	if options.NewLeaseOwnerID == nil {
		options.NewLeaseOwnerID = newContentLeaseOwnerID
	}
	if options.LeaseDuration < time.Microsecond || options.LeaseDuration > maxContentLeaseDuration ||
		options.ChunkBytes < 1 || int64(options.ChunkBytes) > MiB || options.Limits.Validate() != nil {
		return nil, fmt.Errorf("%w: options", ErrInvalidDownloadRequest)
	}
	return &DownloadService{
		repository: repository, authorizer: authorizer, leases: leases, blob: blob,
		now: options.Now, leaseDuration: options.LeaseDuration, chunkBytes: options.ChunkBytes,
		limits: options.Limits, newLeaseOwnerID: options.NewLeaseOwnerID,
	}, nil
}

type MetadataRequest struct {
	Actor        recordauth.ActorScope
	AttachmentID string
}

type AttachmentMetadata struct {
	AttachmentID     string
	State            UploadState
	DisplayName      string
	MediaType        string
	SizeBytes        int64
	PreviewAvailable bool
}

func (service *DownloadService) GetMetadata(
	ctx context.Context,
	request MetadataRequest,
) (AttachmentMetadata, error) {
	actor, content, err := service.loadAuthorized(ctx, request.Actor, request.AttachmentID)
	if err != nil {
		return AttachmentMetadata{}, err
	}
	_ = actor
	previewAvailable := content.State == UploadStateAvailable && content.Preview != nil
	if previewAvailable && content.Preview.MediaType == ManagedPreviewMediaTypeTextUTF8 &&
		content.Preview.Object.SizeBytes > service.limits.MaxInlineTextPreviewBytes {
		previewAvailable = false
	}
	return AttachmentMetadata{
		AttachmentID:     content.AttachmentID,
		State:            content.State,
		DisplayName:      content.DisplayName,
		MediaType:        AllowlistedContentType(ContentVariantOriginal, content.MediaType),
		SizeBytes:        content.LogicalSizeBytes,
		PreviewAvailable: previewAvailable,
	}, nil
}

type DownloadRequest struct {
	Actor        recordauth.ActorScope
	AttachmentID string
	Variant      ContentVariant
	HTTPRange    string
}

type ResolvedContentRange struct {
	Start        int64
	EndInclusive int64
	Length       int64
	Partial      bool
}

type DownloadMetadata struct {
	AttachmentID string
	Variant      ContentVariant
	DisplayName  string
	MediaType    string
	Object       ObjectVersion
	Range        ResolvedContentRange
}

// ContentStream is the narrow HTTP-facing stream contract. Implementations
// must perform their authorization/lease assertion before each write.
type ContentStream interface {
	Metadata() DownloadMetadata
	WriteTo(context.Context, io.Writer) (int64, error)
	Close(context.Context) error
}

func (service *DownloadService) Open(
	ctx context.Context,
	request DownloadRequest,
) (ContentStream, error) {
	if ctx == nil || service == nil {
		return nil, ErrInvalidDownloadRequest
	}
	variant, err := ParseContentVariant(string(request.Variant))
	if err != nil {
		return nil, err
	}
	actor, content, err := service.loadAuthorized(ctx, request.Actor, request.AttachmentID)
	if err != nil {
		return nil, err
	}
	if content.State != UploadStateAvailable {
		return nil, ErrContentUnavailable
	}

	selected, mediaType, err := service.selectContent(content, variant)
	if err != nil {
		return nil, err
	}
	resolvedRange, blobRange, err := ResolveHTTPContentRange(request.HTTPRange, selected.SizeBytes)
	if err != nil {
		return nil, NewContentRangeError(selected.SizeBytes, err)
	}
	assertion := ContentAssertion{
		ProjectID: content.ProjectID, AttachmentID: content.AttachmentID,
		DraftID: content.DraftID, RecordID: content.RecordID, AuthorID: content.AuthorID,
		Variant: variant, Object: selected,
	}
	if assertion.Validate() != nil {
		return nil, ErrInvalidDownloadContent
	}

	var serving *recordplatform.ServingLeaseV1
	expiresAt := service.now().Add(service.leaseDuration)
	if content.RecordID != "" {
		ownerID, ownerErr := service.newLeaseOwnerID()
		claim := recordplatform.LeaseClaimInputV1{OwnerID: ownerID, LeaseDuration: service.leaseDuration}
		if ownerErr != nil || claim.Validate() != nil {
			return nil, fmt.Errorf("%w: lease identity", ErrInvalidDownloadRequest)
		}
		object := recordObject(content)
		acquired, acquireErr := service.leases.AcquireServingLease(ctx, object, claim)
		if acquireErr != nil {
			return nil, acquireErr
		}
		if acquired.Validate() != nil || acquired.Object != object || acquired.Owner.OwnerID != ownerID {
			_ = service.releaseServingLease(ctx, acquired)
			return nil, ErrInvalidDownloadContent
		}
		serving = &acquired
		expiresAt = acquired.Owner.ExpiresAt
	}

	delivery := &ContentDelivery{
		repository: service.repository, authorizer: service.authorizer, leases: service.leases,
		actor: actor, recordID: content.RecordID, now: service.now,
		assertion: assertion, serving: serving, expiresAt: expiresAt, chunkBytes: service.chunkBytes,
		leaseDuration: service.leaseDuration,
		closeDone:     make(chan struct{}),
		metadata: DownloadMetadata{
			AttachmentID: content.AttachmentID, Variant: variant, DisplayName: content.DisplayName,
			MediaType: AllowlistedContentType(variant, mediaType), Object: selected, Range: resolvedRange,
		},
	}
	if err := delivery.assert(ctx); err != nil {
		_ = delivery.Close(ctx)
		return nil, err
	}
	info, err := service.blob.Stat(ctx, selected)
	if err != nil {
		_ = delivery.Close(ctx)
		return nil, err
	}
	if info.Version != selected {
		_ = delivery.Close(ctx)
		return nil, ErrInvalidDownloadContent
	}
	reader, err := service.blob.Open(ctx, selected, blobRange)
	if err != nil {
		_ = delivery.Close(ctx)
		return nil, err
	}
	if nilDownloadDependency(reader) {
		_ = delivery.Close(ctx)
		return nil, ErrInvalidDownloadContent
	}
	delivery.reader = reader
	if err := delivery.assert(ctx); err != nil {
		_ = delivery.Close(ctx)
		return nil, err
	}
	delivery.startRenewal()
	if err := delivery.deliveryError(); err != nil {
		_ = delivery.Close(ctx)
		return nil, err
	}
	_ = actor
	return delivery, nil
}

func (service *DownloadService) loadAuthorized(
	ctx context.Context,
	actor recordauth.ActorScope,
	attachmentID string,
) (recordauth.ActorScope, AttachmentContent, error) {
	if ctx == nil || service == nil || nilDownloadDependency(service.repository) ||
		nilDownloadDependency(service.authorizer) || nilDownloadDependency(service.leases) ||
		nilDownloadDependency(service.blob) {
		return recordauth.ActorScope{}, AttachmentContent{}, ErrInvalidDownloadRequest
	}
	normalized, err := recordauth.NormalizeActorScope(actor)
	if err != nil || attachmentID == "" {
		return recordauth.ActorScope{}, AttachmentContent{}, ErrInvalidDownloadRequest
	}
	lookup := ContentLookup{ProjectID: string(normalized.ProjectID), AttachmentID: attachmentID}
	if lookup.Validate() != nil {
		return recordauth.ActorScope{}, AttachmentContent{}, ErrInvalidDownloadRequest
	}
	content, err := service.repository.GetAttachmentForDownload(ctx, lookup)
	if err != nil {
		return recordauth.ActorScope{}, AttachmentContent{}, err
	}
	if content.Validate() != nil || content.ProjectID != string(normalized.ProjectID) ||
		content.AttachmentID != attachmentID {
		return recordauth.ActorScope{}, AttachmentContent{}, ErrInvalidDownloadContent
	}
	if content.DraftID != "" {
		if normalized.UserID != content.AuthorID {
			return recordauth.ActorScope{}, AttachmentContent{}, recordauth.ErrDenied
		}
	} else if err := service.authorizer.AuthorizeRecordAttachmentRead(
		ctx, normalized.Clone(), content.RecordID,
	); err != nil {
		return recordauth.ActorScope{}, AttachmentContent{}, err
	}
	return normalized, content, nil
}

func (service *DownloadService) selectContent(
	content AttachmentContent,
	variant ContentVariant,
) (ObjectVersion, string, error) {
	switch variant {
	case ContentVariantOriginal:
		return content.Original, content.MediaType, nil
	case ContentVariantPreview:
		if content.Preview == nil {
			return ObjectVersion{}, "", ErrContentVariantUnavailable
		}
		if content.Preview.MediaType == ManagedPreviewMediaTypeTextUTF8 &&
			content.Preview.Object.SizeBytes > service.limits.MaxInlineTextPreviewBytes {
			return ObjectVersion{}, "", ErrContentPreviewTooLarge
		}
		return content.Preview.Object, content.Preview.MediaType, nil
	default:
		return ObjectVersion{}, "", ErrInvalidContentVariant
	}
}

func (service *DownloadService) releaseServingLease(
	ctx context.Context,
	serving recordplatform.ServingLeaseV1,
) error {
	if serving.Validate() != nil {
		return nil
	}
	cleanupCtx, cancel := downloadCleanupContext(ctx)
	defer cancel()
	err := service.leases.ReleaseObjectContentLease(cleanupCtx, serving.Object, serving.Owner)
	if errors.Is(err, recordplatform.ErrLostOwnerLease) {
		return nil
	}
	return err
}

type ContentDelivery struct {
	mu              sync.Mutex
	repository      DownloadRepository
	authorizer      RecordDownloadAuthorizer
	leases          ContentLeaseRepository
	actor           recordauth.ActorScope
	recordID        string
	now             func() time.Time
	assertion       ContentAssertion
	serving         *recordplatform.ServingLeaseV1
	expiresAt       time.Time
	leaseDuration   time.Duration
	chunkBytes      int
	metadata        DownloadMetadata
	reader          io.ReadCloser
	readerCloseOnce sync.Once
	readerCloseErr  error
	renewalStop     chan struct{}
	renewalDone     chan struct{}
	renewalCancel   context.CancelFunc
	renewalSequence uint64
	activeRenewal   chan struct{}
	terminalErr     error
	closeDone       chan struct{}
	closeErr        error
	closed          bool
	streamed        bool
	writeActive     bool
}

func (delivery *ContentDelivery) Metadata() DownloadMetadata {
	if delivery == nil {
		return DownloadMetadata{}
	}
	return delivery.metadata
}

func (delivery *ContentDelivery) WriteTo(ctx context.Context, writer io.Writer) (int64, error) {
	if ctx == nil || writer == nil || delivery == nil {
		return 0, ErrInvalidDownloadRequest
	}
	delivery.mu.Lock()
	if delivery.closed || delivery.streamed || nilDownloadDependency(delivery.reader) {
		delivery.mu.Unlock()
		return 0, ErrInvalidDownloadRequest
	}
	delivery.streamed = true
	reader := delivery.reader
	delivery.mu.Unlock()
	if err := delivery.deliveryError(); err != nil {
		return 0, err
	}

	if delivery.metadata.Range.Length <= 0 || delivery.chunkBytes < 1 || int64(delivery.chunkBytes) > MiB {
		return 0, ErrInvalidDownloadContent
	}
	buffer := make([]byte, delivery.chunkBytes+1)
	var written int64
	remaining := delivery.metadata.Range.Length
	for remaining > 0 {
		if err := ctx.Err(); err != nil {
			return written, fmt.Errorf("%w: %w", ErrContentDeliveryRevoked, err)
		}
		if err := delivery.deliveryError(); err != nil {
			return written, err
		}
		if err := delivery.renewIfNeeded(ctx); err != nil {
			return written, err
		}
		readSize := int64(delivery.chunkBytes)
		if readSize > remaining {
			readSize = remaining
		}
		// Read one extra byte on the final bounded read so a malformed Blob
		// reader cannot silently over-deliver past the HTTP range.
		if remaining <= int64(delivery.chunkBytes) {
			readSize++
		}
		read, readErr := readContentWithContext(ctx, reader, buffer[:int(readSize)], delivery.closeReader)
		if err := delivery.deliveryError(); err != nil {
			return written, err
		}
		if read < 0 || read > len(buffer) || int64(read) > remaining {
			return written, ErrInvalidDownloadContent
		}
		if read > 0 {
			if err := delivery.renewIfNeeded(ctx); err != nil {
				return written, err
			}
			if err := delivery.assert(ctx); err != nil {
				return written, err
			}
			count, writeErr := delivery.write(ctx, writer, buffer[:read])
			if count < 0 || count > read {
				return written, ErrInvalidDownloadContent
			}
			written += int64(count)
			remaining -= int64(read)
			if writeErr != nil {
				return written, writeErr
			}
			if count != read {
				return written, io.ErrShortWrite
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return written, io.ErrUnexpectedEOF
			}
			return written, readErr
		}
		if read == 0 {
			return written, io.ErrNoProgress
		}
	}
	// A reader may return the exact final bytes with a nil error. Probe once
	// more so an extra byte is rejected instead of being silently ignored.
	var probe [1]byte
	read, readErr := readContentWithContext(ctx, reader, probe[:], delivery.closeReader)
	if err := delivery.deliveryError(); err != nil {
		return written, err
	}
	if read < 0 || read > len(probe) {
		return written, ErrInvalidDownloadContent
	}
	if read > 0 {
		return written, ErrInvalidDownloadContent
	}
	if readErr == nil {
		return written, io.ErrNoProgress
	}
	if errors.Is(readErr, io.EOF) {
		return written, nil
	}
	return written, readErr
}

func (delivery *ContentDelivery) write(ctx context.Context, writer io.Writer, payload []byte) (int, error) {
	if err := delivery.beginWrite(ctx); err != nil {
		return 0, err
	}
	defer delivery.finishWrite()
	return writer.Write(payload)
}

func (delivery *ContentDelivery) beginWrite(ctx context.Context) error {
	delivery.mu.Lock()
	defer delivery.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %w", ErrContentDeliveryRevoked, err)
	}
	if delivery.terminalErr != nil {
		return delivery.terminalErr
	}
	if delivery.closed {
		return ErrContentDeliveryRevoked
	}
	if delivery.writeActive {
		return ErrInvalidDownloadContent
	}
	delivery.writeActive = true
	return nil
}

func (delivery *ContentDelivery) finishWrite() {
	delivery.mu.Lock()
	delivery.writeActive = false
	delivery.mu.Unlock()
}

func readContentWithContext(
	ctx context.Context,
	reader io.ReadCloser,
	buffer []byte,
	closeReader func() error,
) (int, error) {
	if ctx == nil || nilDownloadDependency(reader) || len(buffer) == 0 || closeReader == nil {
		return 0, ErrInvalidDownloadRequest
	}
	stopClose := context.AfterFunc(ctx, func() { _ = closeReader() })
	read, err := reader.Read(buffer)
	stopClose()
	if contextErr := ctx.Err(); contextErr != nil {
		return read, fmt.Errorf("%w: %w", ErrContentDeliveryRevoked, contextErr)
	}
	return read, err
}

func (delivery *ContentDelivery) startRenewal() {
	if delivery == nil {
		return
	}
	delivery.mu.Lock()
	if delivery.closed || delivery.terminalErr != nil || delivery.serving == nil || delivery.renewalDone != nil {
		delivery.mu.Unlock()
		return
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	delivery.renewalStop = stop
	delivery.renewalDone = done
	delivery.mu.Unlock()
	go delivery.runRenewal(stop, done)
}

func (delivery *ContentDelivery) runRenewal(stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	for {
		delay, err := delivery.nextRenewalDelay()
		if err != nil {
			if !delivery.isClosed() {
				delivery.revoke(err)
			}
			return
		}
		timer := time.NewTimer(delay)
		select {
		case <-stop:
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
		if err := delivery.renewIfNeeded(context.Background()); err != nil {
			if !delivery.isClosed() {
				delivery.revoke(err)
			}
			return
		}
	}
}

func (delivery *ContentDelivery) nextRenewalDelay() (time.Duration, error) {
	delivery.mu.Lock()
	defer delivery.mu.Unlock()
	if delivery.closed {
		return 0, ErrContentDeliveryRevoked
	}
	if delivery.terminalErr != nil {
		return 0, delivery.terminalErr
	}
	if delivery.serving == nil || delivery.now == nil || delivery.leaseDuration < time.Microsecond || delivery.expiresAt.IsZero() {
		return 0, ErrInvalidDownloadContent
	}
	now := delivery.now()
	if !now.Before(delivery.expiresAt) {
		return 0, ErrContentDeliveryExpired
	}
	delay := delivery.expiresAt.Sub(now) - contentRenewalLead(delivery.leaseDuration)
	if delay < 0 {
		return 0, nil
	}
	return delay, nil
}

// renewIfNeeded extends a record stream only when its database-observed lease
// is near expiry. State snapshots are taken under the mutex, while database I/O
// runs outside it so Close can cancel the operation and unblock the reader.
func (delivery *ContentDelivery) renewIfNeeded(ctx context.Context) error {
	if delivery == nil || ctx == nil {
		return ErrInvalidDownloadRequest
	}
	for {
		delivery.mu.Lock()
		active := delivery.activeRenewal
		if active == nil {
			break
		}
		delivery.mu.Unlock()
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: %w", ErrContentDeliveryRevoked, ctx.Err())
		case <-active:
		}
	}
	if delivery.closed {
		delivery.mu.Unlock()
		return ErrContentDeliveryRevoked
	}
	if delivery.terminalErr != nil {
		err := delivery.terminalErr
		delivery.mu.Unlock()
		return err
	}
	if delivery.now == nil {
		delivery.mu.Unlock()
		return ErrInvalidDownloadRequest
	}
	serving := delivery.serving
	expiresAt := delivery.expiresAt
	leaseDuration := delivery.leaseDuration
	leases := delivery.leases
	if serving == nil {
		delivery.mu.Unlock()
		return nil
	}
	if nilDownloadDependency(leases) || leaseDuration < time.Microsecond {
		delivery.mu.Unlock()
		return ErrInvalidDownloadContent
	}
	now := delivery.now()
	lead := contentRenewalLead(leaseDuration)
	if now.Before(expiresAt) && now.Before(expiresAt.Add(-lead)) {
		delivery.mu.Unlock()
		return nil
	}
	if !now.Before(expiresAt) {
		delivery.mu.Unlock()
		delivery.revoke(ErrContentDeliveryExpired)
		return ErrContentDeliveryExpired
	}
	remaining := expiresAt.Sub(now)
	timeout := renewalTimeout(remaining)
	if timeout <= 0 {
		delivery.mu.Unlock()
		delivery.revoke(ErrContentDeliveryExpired)
		return ErrContentDeliveryExpired
	}
	delivery.renewalSequence++
	sequence := delivery.renewalSequence
	active := make(chan struct{})
	delivery.activeRenewal = active
	renewContext, cancel := context.WithTimeout(ctx, timeout)
	delivery.renewalCancel = cancel
	delivery.mu.Unlock()

	renewed, err := leases.RenewServingLease(renewContext, *serving, leaseDuration)
	cancel()
	delivery.mu.Lock()
	if delivery.renewalSequence == sequence {
		delivery.renewalCancel = nil
	}
	if delivery.activeRenewal == active {
		delivery.activeRenewal = nil
		close(active)
	}
	closed := delivery.closed
	current := delivery.serving
	if err != nil {
		delivery.mu.Unlock()
		renewErr := fmt.Errorf("%w: %w", ErrContentDeliveryRevoked, err)
		if !closed {
			delivery.revoke(renewErr)
		}
		return renewErr
	}
	if current == nil || *current != *serving || renewed.Validate() != nil || renewed.Object != serving.Object ||
		renewed.Owner.OwnerID != serving.Owner.OwnerID ||
		renewed.Owner.Generation != serving.Owner.Generation ||
		renewed.CapturedEpoch != serving.CapturedEpoch ||
		delivery.now == nil || !renewed.Owner.ExpiresAt.After(delivery.now()) {
		delivery.mu.Unlock()
		if !closed {
			delivery.revoke(ErrContentDeliveryRevoked)
		}
		return ErrContentDeliveryRevoked
	}
	delivery.serving = &renewed
	delivery.expiresAt = renewed.Owner.ExpiresAt
	delivery.mu.Unlock()
	return nil
}

func contentRenewalLead(duration time.Duration) time.Duration {
	lead := duration * 2 / 3
	if lead < time.Microsecond {
		return time.Microsecond
	}
	return lead
}

func renewalTimeout(remaining time.Duration) time.Duration {
	if remaining <= 0 {
		return 0
	}
	return remaining / 2
}

func (delivery *ContentDelivery) revoke(err error) {
	if delivery == nil || err == nil {
		return
	}
	delivery.mu.Lock()
	if delivery.closed || delivery.terminalErr != nil {
		delivery.mu.Unlock()
		return
	}
	delivery.terminalErr = err
	delivery.mu.Unlock()
	_ = delivery.closeReader()
}

func (delivery *ContentDelivery) deliveryError() error {
	if delivery == nil {
		return ErrInvalidDownloadRequest
	}
	delivery.mu.Lock()
	defer delivery.mu.Unlock()
	if delivery.terminalErr != nil {
		return delivery.terminalErr
	}
	if delivery.closed {
		return ErrContentDeliveryRevoked
	}
	return nil
}

func (delivery *ContentDelivery) isClosed() bool {
	if delivery == nil {
		return true
	}
	delivery.mu.Lock()
	defer delivery.mu.Unlock()
	return delivery.closed

}

func (delivery *ContentDelivery) closeReader() error {
	if delivery == nil {
		return nil
	}
	delivery.readerCloseOnce.Do(func() {
		delivery.mu.Lock()
		reader := delivery.reader
		delivery.mu.Unlock()
		if !nilDownloadDependency(reader) {
			delivery.readerCloseErr = reader.Close()
		}
	})
	return delivery.readerCloseErr
}

func (delivery *ContentDelivery) assert(ctx context.Context) error {
	if delivery == nil || ctx == nil {
		return ErrInvalidDownloadRequest
	}
	delivery.mu.Lock()
	closed := delivery.closed
	terminalErr := delivery.terminalErr
	now := delivery.now
	expiresAt := delivery.expiresAt
	serving := delivery.serving
	repository := delivery.repository
	authorizer := delivery.authorizer
	leases := delivery.leases
	recordID := delivery.recordID
	actor := delivery.actor
	assertion := delivery.assertion
	delivery.mu.Unlock()
	if now == nil || expiresAt.IsZero() || nilDownloadDependency(repository) ||
		(recordID != "" && nilDownloadDependency(authorizer)) ||
		(serving != nil && nilDownloadDependency(leases)) {
		return ErrInvalidDownloadRequest
	}
	if closed {
		return ErrContentDeliveryRevoked
	}
	if terminalErr != nil {
		return terminalErr
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %w", ErrContentDeliveryRevoked, err)
	}
	if !now().Before(expiresAt) {
		return ErrContentDeliveryExpired
	}
	if err := repository.AssertAttachmentContent(ctx, assertion); err != nil {
		return fmt.Errorf("%w: %w", ErrContentDeliveryRevoked, err)
	}
	if recordID != "" {
		if err := authorizer.AuthorizeRecordAttachmentRead(ctx, actor.Clone(), recordID); err != nil {
			return fmt.Errorf("%w: %w", ErrContentDeliveryRevoked, err)
		}
	}
	if serving != nil {
		return delivery.assertServingLease(ctx, leases, *serving)
	}
	return nil
}

func (delivery *ContentDelivery) assertServingLease(
	ctx context.Context,
	leases ContentLeaseRepository,
	serving recordplatform.ServingLeaseV1,
) error {
	for attempt := 0; attempt < 3; attempt++ {
		if err := leases.AssertServingLease(ctx, serving); err == nil {
			return nil
		} else {
			delivery.mu.Lock()
			closed := delivery.closed
			terminalErr := delivery.terminalErr
			current := delivery.serving
			delivery.mu.Unlock()
			if closed {
				return ErrContentDeliveryRevoked
			}
			if terminalErr != nil {
				return terminalErr
			}
			if current == nil || *current == serving {
				return fmt.Errorf("%w: %w", ErrContentDeliveryRevoked, err)
			}
			serving = *current
		}
	}
	return ErrContentDeliveryRevoked
}

func (delivery *ContentDelivery) Close(ctx context.Context) error {
	if delivery == nil {
		return nil
	}
	delivery.mu.Lock()
	if delivery.closeDone == nil {
		delivery.closeDone = make(chan struct{})
	}
	if delivery.closed {
		done := delivery.closeDone
		delivery.mu.Unlock()
		<-done
		delivery.mu.Lock()
		err := delivery.closeErr
		delivery.mu.Unlock()
		return err
	}
	delivery.closed = true
	stop := delivery.renewalStop
	done := delivery.renewalDone
	activeRenewal := delivery.activeRenewal
	cancelRenewal := delivery.renewalCancel
	closeDone := delivery.closeDone
	if stop != nil {
		close(stop)
		delivery.renewalStop = nil
	}
	delivery.mu.Unlock()

	if cancelRenewal != nil {
		cancelRenewal()
	}
	closeErr := delivery.closeReader()
	if activeRenewal != nil {
		<-activeRenewal
	}
	if done != nil {
		<-done
	}
	delivery.mu.Lock()
	serving := delivery.serving
	leases := delivery.leases
	delivery.mu.Unlock()
	if serving != nil && !nilDownloadDependency(leases) {
		cleanupCtx, cancel := downloadCleanupContext(ctx)
		releaseErr := leases.ReleaseObjectContentLease(
			cleanupCtx, serving.Object, serving.Owner,
		)
		cancel()
		if errors.Is(releaseErr, recordplatform.ErrLostOwnerLease) {
			releaseErr = nil
		}
		closeErr = errors.Join(closeErr, releaseErr)
	}
	delivery.mu.Lock()
	delivery.closeErr = closeErr
	close(closeDone)
	delivery.mu.Unlock()
	return closeErr
}

func ResolveHTTPContentRange(raw string, sizeBytes int64) (ResolvedContentRange, ByteRange, error) {
	if sizeBytes <= 0 {
		return ResolvedContentRange{}, ByteRange{}, ErrInvalidContentRange
	}
	if raw == "" {
		return ResolvedContentRange{Start: 0, EndInclusive: sizeBytes - 1, Length: sizeBytes},
			FullByteRange(), nil
	}
	if raw != strings.TrimSpace(raw) || !strings.HasPrefix(raw, "bytes=") || strings.Contains(raw, ",") {
		return ResolvedContentRange{}, ByteRange{}, ErrInvalidContentRange
	}
	value := strings.TrimPrefix(raw, "bytes=")
	if strings.Count(value, "-") != 1 {
		return ResolvedContentRange{}, ByteRange{}, ErrInvalidContentRange
	}
	parts := strings.SplitN(value, "-", 2)
	if parts[0] == "" {
		suffix, err := parseContentRangeInteger(parts[1])
		if err != nil {
			return ResolvedContentRange{}, ByteRange{}, err
		}
		if suffix <= 0 {
			return ResolvedContentRange{}, ByteRange{}, ErrContentRangeNotSatisfiable
		}
		if suffix > sizeBytes {
			suffix = sizeBytes
		}
		start := sizeBytes - suffix
		return partialContentRange(start, sizeBytes-1)
	}
	start, err := parseContentRangeInteger(parts[0])
	if err != nil {
		return ResolvedContentRange{}, ByteRange{}, err
	}
	if start >= sizeBytes {
		return ResolvedContentRange{}, ByteRange{}, ErrContentRangeNotSatisfiable
	}
	end := sizeBytes - 1
	if parts[1] != "" {
		end, err = parseContentRangeInteger(parts[1])
		if err != nil {
			return ResolvedContentRange{}, ByteRange{}, err
		}
		if end < start {
			return ResolvedContentRange{}, ByteRange{}, ErrContentRangeNotSatisfiable
		}
		if end >= sizeBytes {
			end = sizeBytes - 1
		}
	}
	return partialContentRange(start, end)
}

func parseContentRangeInteger(raw string) (int64, error) {
	if raw == "" {
		return 0, ErrInvalidContentRange
	}
	for _, character := range raw {
		if character < '0' || character > '9' {
			return 0, ErrInvalidContentRange
		}
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, ErrInvalidContentRange
	}
	return value, nil
}

func partialContentRange(start, end int64) (ResolvedContentRange, ByteRange, error) {
	if start < 0 || end < start {
		return ResolvedContentRange{}, ByteRange{}, ErrContentRangeNotSatisfiable
	}
	resolved := ResolvedContentRange{
		Start: start, EndInclusive: end, Length: end - start + 1, Partial: true,
	}
	return resolved, ClosedByteRange(start, end), nil
}

func AllowlistedContentType(variant ContentVariant, raw string) string {
	const fallback = "application/octet-stream"
	if variant == ContentVariantPreview {
		if managedPreviewMediaType(raw) {
			return raw
		}
		return fallback
	}
	mediaType, parameters, err := mime.ParseMediaType(raw)
	if err != nil {
		return fallback
	}
	mediaType = strings.ToLower(mediaType)
	if len(parameters) > 0 {
		charset, ok := parameters["charset"]
		if len(parameters) != 1 || !ok || !strings.EqualFold(charset, "utf-8") ||
			!strings.HasPrefix(mediaType, "text/") {
			return fallback
		}
	}
	switch mediaType {
	case "application/octet-stream", "application/pdf", "application/zip", "application/gzip",
		"application/x-gzip", "application/x-tar", "application/zstd", "application/json",
		"application/yaml", "application/toml", "image/png", "image/jpeg", "image/webp",
		"text/plain", "text/markdown", "text/csv", "text/tab-separated-values", "text/yaml",
		"text/x-diff", "text/x-patch":
		if len(parameters) == 1 {
			return mediaType + "; charset=utf-8"
		}
		return mediaType
	default:
		return fallback
	}
}

func SafeContentDisposition(variant ContentVariant, displayName string) string {
	disposition := "attachment"
	if variant == ContentVariantPreview {
		disposition = "inline"
	}
	safeName := sanitizeContentFilename(displayName)
	return disposition + "; filename=\"" + asciiContentFilename(safeName) +
		"\"; filename*=UTF-8''" + url.PathEscape(safeName)
}

func sanitizeContentFilename(value string) string {
	if !utf8.ValidString(value) {
		return "attachment"
	}
	var builder strings.Builder
	runes := 0
	for _, character := range value {
		if runes >= 180 {
			break
		}
		runes++
		if unicode.IsControl(character) || character == '/' || character == '\\' ||
			character == '"' || character == ';' || character == ':' {
			builder.WriteByte('_')
			continue
		}
		builder.WriteRune(character)
	}
	name := strings.Trim(builder.String(), " .")
	if name == "" || name == "." || name == ".." {
		return "attachment"
	}
	return name
}

func asciiContentFilename(value string) string {
	var builder strings.Builder
	for _, character := range value {
		if character >= 0x20 && character <= 0x7e && character != '"' && character != '\\' {
			builder.WriteRune(character)
		} else {
			builder.WriteByte('_')
		}
	}
	name := strings.Trim(builder.String(), " .")
	if name == "" {
		return "attachment"
	}
	return name
}

func recordObject(content AttachmentContent) recordplatform.ObjectRef {
	return recordplatform.ObjectRef{
		ProjectID: content.ProjectID, ObjectKind: "record", ObjectID: content.RecordID,
	}
}

func newContentLeaseOwnerID() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return "attachment_delivery_" + hex.EncodeToString(random[:]), nil
}

func downloadCleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
}

func nilDownloadDependency(value any) bool {
	if value == nil {
		return true
	}
	kind := reflect.ValueOf(value).Kind()
	switch kind {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflect.ValueOf(value).IsNil()
	default:
		return false
	}
}
