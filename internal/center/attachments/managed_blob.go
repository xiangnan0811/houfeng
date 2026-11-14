package attachments

import (
	"context"
	"errors"
	"io"
	"math"
	"strings"
	"sync"
	"time"
)

const managedBlobFinalizationTimeout = 5 * time.Second

type BlobWriter interface {
	Put(context.Context, WriteRequest, io.Reader) (WrittenBlob, error)
	NamespaceID() string
}

// TemporaryBlobWriter promotes an already versioned S3 upload through the same
// durable attempt lifecycle as Put. Local writers do not use this capability.
type TemporaryBlobWriter interface {
	PublishTemporaryVersion(context.Context, WriteRequest, TemporaryObjectVersion) (WrittenBlob, error)
}

type ManagedBlobWriter struct {
	blob         BlobStore
	temporary    TemporaryObjectStore
	local        *LocalBlobStore
	repository   ManagedWriteRepository
	backend      BackendKind
	instance     string
	registration sync.Mutex
	sequence     int64
}

func NewManagedBlobWriter(blob BlobStore, backend BackendKind, repository ManagedWriteRepository) (*ManagedBlobWriter, error) {
	if nilUploadServiceDependency(blob) || nilUploadServiceDependency(repository) || (backend != BackendKindLocal && backend != BackendKindS3) {
		return nil, ErrInvalidBlobStoreConfig
	}
	local, isLocal := blob.(*LocalBlobStore)
	if (backend == BackendKindLocal) != isLocal {
		return nil, ErrInvalidBlobStoreConfig
	}
	instance, err := newLocalUUID()
	if err != nil {
		return nil, err
	}
	temporary, _ := blob.(TemporaryObjectStore)
	return &ManagedBlobWriter{blob: blob, temporary: temporary, local: local, repository: repository, backend: backend, instance: instance}, nil
}

func (writer *ManagedBlobWriter) NamespaceID() string {
	if writer == nil || writer.local == nil {
		return ""
	}
	return writer.local.namespace.NamespaceID
}

func (writer *ManagedBlobWriter) Put(ctx context.Context, request WriteRequest, reader io.Reader) (WrittenBlob, error) {
	if nilUploadServiceDependency(reader) {
		return WrittenBlob{}, ErrInvalidBlobRequest
	}
	return writer.write(ctx, request, reader, TemporaryObjectVersion{})
}

func (writer *ManagedBlobWriter) PublishTemporaryVersion(ctx context.Context, request WriteRequest, source TemporaryObjectVersion) (WrittenBlob, error) {
	if writer == nil || writer.backend != BackendKindS3 || nilUploadServiceDependency(writer.temporary) || source.Validate() != nil || request.Content.TemporaryKey != source.Key {
		return WrittenBlob{}, ErrInvalidBlobRequest
	}
	return writer.write(ctx, request, nil, source)
}

func (writer *ManagedBlobWriter) write(ctx context.Context, request WriteRequest, reader io.Reader, source TemporaryObjectVersion) (WrittenBlob, error) {
	var result WrittenBlob
	if ctx == nil || writer == nil || request.Content.Validate() != nil || request.ProjectID == "" || request.Origin.ID == "" || request.Origin.Generation <= 0 {
		return result, ErrInvalidBlobRequest
	}
	obligation, err := writer.register(ctx, request)
	if err != nil {
		return result, err
	}
	if obligation.DispatchedAt != nil || obligation.Outcome != "registered" || obligation.HandedOffAt != nil || obligation.CleanupOwnerID != nil {
		return result, ErrWriteOutcomeUnknown
	}
	dispatchID, err := newLocalUUID()
	if err != nil {
		return result, err
	}
	identity := WriteIdentity{ObligationID: obligation.ID, WriterInstance: writer.instance}
	run := func(ctx context.Context, guard *LocalKeyGuard) error {
		dispatch := WriteDispatch{WriteIdentity: identity, DispatchID: dispatchID}
		if guard != nil {
			dispatch.AnchorInode = &guard.anchor.Inode
			dispatch.AnchorNonce = &guard.anchor.Nonce
		}
		dispatched, err := writer.repository.DispatchWrite(ctx, dispatch)
		if err != nil {
			if errors.Is(err, ErrBlobGCProtected) {
				return err
			}
			resolved, resolveErr := writer.resolve(ctx, identity)
			if resolveErr != nil || !sameWriteDispatch(resolved, obligation, dispatchID) {
				return errors.Join(ErrWriteOutcomeUnknown, err, resolveErr)
			}
			dispatched = resolved
		}
		if !sameWriteDispatch(dispatched, obligation, dispatchID) || dispatched.ReturnedAt != nil || dispatched.CleanupOwnerID != nil || dispatched.HandedOffAt != nil {
			return ErrWriteOutcomeUnknown
		}
		obligation = dispatched
		var version ObjectVersion
		var writeErr error
		if guard != nil {
			version, writeErr = writer.local.putGuarded(ctx, guard, obligation, request.Content, reader, func(device, inode uint64) error {
				finalCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), managedBlobFinalizationTimeout)
				defer cancel()
				created, err := writer.repository.RecordWriteTemporary(finalCtx, WriteTemporaryCreation{WriteIdentity: identity, DispatchID: dispatchID, Device: device, Inode: inode})
				if err != nil {
					created, err = writer.repository.ResolveWrite(finalCtx, identity)
				}
				if err != nil || !sameWriteDispatch(created, obligation, dispatchID) || created.TemporaryDevice == nil || created.TemporaryInode == nil || *created.TemporaryDevice != device || *created.TemporaryInode != inode {
					return errors.Join(ErrWriteOutcomeUnknown, err)
				}
				obligation = created
				return nil
			})
		} else if source.VersionID != "" {
			version, writeErr = writer.temporary.PublishTemporaryVersion(ctx, TemporaryObjectPublishRequest{Version: source, ExpectedSHA256: request.Content.ExpectedSHA256, ExpectedSizeBytes: request.Content.ExpectedSizeBytes})
		} else {
			version, writeErr = writer.blob.Put(ctx, request.Content, reader)
		}
		returned := WriteReturn{WriteIdentity: identity, DispatchID: dispatchID, Outcome: "materialized", Key: obligation.Key, SHA256: obligation.SHA256, Size: obligation.Size}
		if version.Validate() == nil && version.Key == obligation.Key && version.SHA256 == request.Content.ExpectedSHA256 && version.SizeBytes == obligation.Size {
			returned.Version = &version.VersionID
		} else if writeErr == nil {
			writeErr = ErrBlobVersionMismatch
		}
		if writeErr != nil {
			returned.Outcome = "failed"
			if writer.backend == BackendKindS3 {
				returned.Outcome = "remote_unknown"
			}
		}
		finalCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), managedBlobFinalizationTimeout)
		defer cancel()
		persisted, err := writer.repository.RecordWriteReturn(finalCtx, returned)
		if err != nil {
			resolved, resolveErr := writer.repository.ResolveWrite(finalCtx, identity)
			if resolveErr != nil || !sameWriteReturn(resolved, obligation, returned) {
				return errors.Join(ErrWriteOutcomeUnknown, writeErr, err, resolveErr)
			}
			persisted = resolved
		}
		if !sameWriteReturn(persisted, obligation, returned) {
			return errors.Join(ErrWriteOutcomeUnknown, writeErr)
		}
		if writeErr != nil {
			return writeErr
		}
		if guard != nil {
			if err := writer.local.removeKnownTemporary(finalCtx, guard, persisted); err != nil {
				return err
			}
		}
		result = WrittenBlob{Version: version, ObligationID: obligation.ID}
		if obligation.NamespaceID != nil {
			result.NamespaceID = *obligation.NamespaceID
		}
		return nil
	}
	if writer.local != nil {
		err = writer.local.WithKeyGuard(ctx, obligation.Key, run)
	} else {
		err = run(ctx, nil)
	}
	return result, err
}

func (writer *ManagedBlobWriter) register(ctx context.Context, request WriteRequest) (WriteObligation, error) {
	// Preserve registration order even when callers concurrently reach SQL.
	// The mutex ends before a key guard or physical reader is acquired.
	writer.registration.Lock()
	defer writer.registration.Unlock()
	if err := ctx.Err(); err != nil {
		return WriteObligation{}, err
	}
	if writer.sequence == math.MaxInt64 {
		return WriteObligation{}, ErrInvalidBlobRequest
	}
	writer.sequence++
	id, err := newLocalUUID()
	if err != nil {
		return WriteObligation{}, err
	}
	registration := WriteRegistration{WriteOrigin: request.Origin, ObligationID: "rwo_" + strings.ReplaceAll(id, "-", ""), WriterInstance: writer.instance, RequestSeq: writer.sequence,
		ProjectID: request.ProjectID, BackendKind: writer.backend, Key: "sha256/" + hexDigest(request.Content.ExpectedSHA256), SHA256: hexDigest(request.Content.ExpectedSHA256), Size: request.Content.ExpectedSizeBytes}
	if writer.local != nil {
		n := writer.local.Namespace()
		registration.NamespaceID = &n.NamespaceID
		registration.IncarnationID = &n.IncarnationID
	}
	obligation, err := writer.repository.RegisterWrite(ctx, registration)
	if err != nil {
		if errors.Is(err, ErrBlobGCProtected) {
			return WriteObligation{}, err
		}
		finalCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), managedBlobFinalizationTimeout)
		defer cancel()
		// Exact registration replay checks all origin bytes as well as the ID.
		obligation, err = writer.repository.RegisterWrite(finalCtx, registration)
		if err != nil {
			return WriteObligation{}, errors.Join(ErrWriteOutcomeUnknown, err)
		}
	}
	if obligation.ID != registration.ObligationID || obligation.WriterInstance != writer.instance || obligation.RequestSeq != registration.RequestSeq || obligation.ProjectID != registration.ProjectID || obligation.BackendKind != registration.BackendKind || obligation.Key != registration.Key || obligation.SHA256 != registration.SHA256 || obligation.Size != registration.Size || !sameOptionalString(obligation.NamespaceID, registration.NamespaceID) || !sameOptionalString(obligation.IncarnationID, registration.IncarnationID) {
		return WriteObligation{}, ErrWriteOutcomeUnknown
	}
	return obligation, nil
}

func (writer *ManagedBlobWriter) resolve(ctx context.Context, identity WriteIdentity) (WriteObligation, error) {
	finalCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), managedBlobFinalizationTimeout)
	defer cancel()
	return writer.repository.ResolveWrite(finalCtx, identity)
}

func sameOptionalString(left, right *string) bool {
	return (left == nil && right == nil) || (left != nil && right != nil && *left == *right)
}
func sameWriteDispatch(actual, expected WriteObligation, dispatch string) bool {
	return actual.ID == expected.ID && actual.WriterInstance == expected.WriterInstance && actual.RequestSeq == expected.RequestSeq && actual.ProjectID == expected.ProjectID && actual.BackendKind == expected.BackendKind && actual.Key == expected.Key && actual.SHA256 == expected.SHA256 && actual.Size == expected.Size && sameOptionalString(actual.NamespaceID, expected.NamespaceID) && sameOptionalString(actual.IncarnationID, expected.IncarnationID) && sameOptionalString(actual.TemporaryKey, expected.TemporaryKey) && actual.DispatchID != nil && *actual.DispatchID == dispatch && actual.DispatchedAt != nil
}
func sameWriteReturn(actual, expected WriteObligation, returned WriteReturn) bool {
	return sameWriteDispatch(actual, expected, returned.DispatchID) && actual.ReturnedAt != nil && actual.Outcome == returned.Outcome && sameOptionalString(actual.Version, returned.Version)
}
