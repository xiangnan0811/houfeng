package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/ids"
)

// Discovery is a read-only hint. The coordinator repeats admission, target and
// ownership checks in a short transaction after acquiring the filesystem guard.
func (repository *PostgresAttachmentRepository) DiscoverLocalBlobGC(ctx context.Context, request attachments.BlobGCClaimRequest, namespaceID string) (*attachments.LocalBlobGCCandidate, error) {
	if ctx == nil || repository == nil || repository.beginTx == nil || request.Validate() != nil || request.BackendKind != attachments.BackendKindLocal || namespaceID == "" ||
		(request.Mode == attachments.BlobGCPurgeModePermanent && request.Object.NamespaceID != namespaceID) {
		return nil, attachments.ErrInvalidBlobGCRequest
	}
	tx, err := repository.beginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var result attachments.LocalBlobGCCandidate
	var digest []byte
	var priority int
	err = tx.QueryRow(ctx, `
		with candidates as (
			select 0 as priority, g.deletion_id, g.namespace_id, g.incarnation_id, g.blob_key,
			       coalesce(b.object_version,g.object_version,o.object_version,'') as object_version,
			       coalesce(b.size_bytes,g.size_bytes,o.size_bytes,0) as size_bytes,
			       coalesce(b.created_at,g.blob_created_at,o.created_at,g.created_at) as created_at, g.sha256_digest
			from public.blob_gc_deletions g
			join public.local_blob_namespaces n on n.namespace_id=g.namespace_id and n.current_incarnation_id=g.incarnation_id
			left join public.blob_objects b on b.namespace_id=g.namespace_id and b.blob_key=g.blob_key
			left join lateral (select object_version,size_bytes,created_at from public.record_write_obligations
			  where namespace_id=g.namespace_id and incarnation_id=g.incarnation_id and canonical_key=g.blob_key
			  order by (object_version is null),created_at desc,obligation_id limit 1) o on true
			where g.local_contract_version=2 and g.namespace_id=$1::uuid and n.admission_state='active'
			  and g.operation_id is null and g.deletion_state<>'completed'
			  and (g.owner_id is null or g.owner_id=$2 or g.lease_expires_at<=statement_timestamp())
			  and ($3='ordinary' or (g.blob_key=$4 and coalesce(b.object_version,g.object_version,o.object_version)=$5))
			union all
			select 1,'',b.namespace_id,n.current_incarnation_id,b.blob_key,b.object_version,b.size_bytes,b.created_at,b.sha256_digest
			from public.blob_objects b join public.local_blob_namespaces n on n.namespace_id=b.namespace_id
			where b.backend_kind='local' and b.namespace_id=$1::uuid and n.admission_state='active'
			  and (($3='ordinary' and b.created_at<=$6) or ($3='permanent' and b.blob_key=$4 and b.object_version=$5))
			  and not exists(select 1 from public.blob_gc_deletions g where g.blob_key=b.blob_key and g.deletion_state<>'completed')
			  and not exists(select 1 from public.record_attachments a where b.blob_key in (a.blob_key,a.preview_blob_key))
			  and not exists(select 1 from public.attachment_upload_parts p where p.sha256_digest=b.sha256_digest)
			  and not exists(select 1 from public.blob_gc_pins p where p.blob_key=b.blob_key and p.expires_at>statement_timestamp())
			  and not exists(select 1 from public.record_export_artifacts a where a.backend_kind='local' and a.blob_key=b.blob_key
			    and (a.namespace_id=b.namespace_id or a.namespace_id is null) and a.revoked_at is null and a.expires_at>statement_timestamp())
			  and not exists(select 1 from public.record_import_artifacts a where a.backend_kind='local' and a.blob_key=b.blob_key
			    and (a.namespace_id=b.namespace_id or a.namespace_id is null) and a.expires_at>statement_timestamp())
			  and not exists(select 1 from public.blob_publication_intents p where p.backend_kind='local' and p.blob_key=b.blob_key
			    and p.publication_state<>'completed')
			union all
			select distinct on (o.canonical_key) 2,'',o.namespace_id,o.incarnation_id,o.canonical_key,
			       coalesce(o.object_version,''),o.size_bytes,o.created_at,o.sha256_digest
			from public.record_write_obligations o join public.local_blob_namespaces n
			  on n.namespace_id=o.namespace_id and n.current_incarnation_id=o.incarnation_id
			where $3='ordinary' and o.namespace_id=$1::uuid and n.admission_state='active'
			  and o.operation_id is null and o.cleanup_completed_at is null and o.created_at<=$6
			  and (o.handed_off_at is not null or o.outcome in ('failed','local_excluded')
			    or exists(select 1 from public.attachment_uploads u where o.origin_kind='upload' and u.upload_id=o.origin_id
			      and (u.upload_state in ('available','rejected','expired') or u.expires_at<=statement_timestamp()))
			    or exists(select 1 from public.attachment_processor_jobs j where o.origin_kind='processor_preview' and j.processor_job_id=o.origin_id
			      and (j.processor_state in ('succeeded','rejected','expired') or j.expires_at<=statement_timestamp()))
			    or exists(select 1 from public.record_export_jobs j where o.origin_kind='export_artifact' and j.export_job_id=o.origin_id
			      and (j.job_state in ('revoked','failed','expired') or j.expires_at<=statement_timestamp()))
			    or exists(select 1 from public.record_import_jobs j where o.origin_kind in ('import_archive','import_attachment') and j.import_job_id=o.origin_id
			      and (j.job_state in ('applied','failed','expired') or j.expires_at<=statement_timestamp())))
			  and not exists(select 1 from public.blob_objects b where b.namespace_id=o.namespace_id and b.blob_key=o.canonical_key)
			  and not exists(select 1 from public.blob_gc_deletions g where g.blob_key=o.canonical_key and g.deletion_state<>'completed')
		)
		select deletion_id,namespace_id::text,incarnation_id::text,blob_key,object_version,size_bytes,created_at,sha256_digest,priority
		from candidates order by priority,created_at,blob_key limit 1`, namespaceID, request.OwnerID, request.Mode,
		request.Object.Key, request.Object.ObjectVersion, request.OrphanedBefore.UTC()).Scan(
		&result.Preparation.DeletionID, &result.Preparation.NamespaceID, &result.Preparation.IncarnationID,
		&result.Candidate.Object.Key, &result.Candidate.Object.ObjectVersion, &result.Candidate.Object.SizeBytes,
		&result.Candidate.CreatedAt, &digest, &priority)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("discover local Blob GC: %w", err)
	}
	if len(digest) != sha256.Size {
		return nil, attachments.ErrBlobGCConflict
	}
	copy(result.Candidate.Object.SHA256[:], digest)
	result.Candidate.Object.BackendKind = attachments.BackendKindLocal
	result.Candidate.Object.NamespaceID = result.Preparation.NamespaceID
	result.Preparation.Key = result.Candidate.Object.Key
	if result.Preparation.DeletionID == "" {
		if priority == 1 {
			result.Preparation.ExpectedVersion = &result.Candidate.Object.ObjectVersion
			if request.Mode == attachments.BlobGCPurgeModeOrdinary {
				cutoff := request.OrphanedBefore.UTC()
				result.Preparation.OrphanedBefore = &cutoff
			}
		}
		result.Preparation.DeletionID, err = repository.nextLocalCleanupID()
		if err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &result, nil
}

func (repository *PostgresAttachmentRepository) DiscoverLocalPublicationCleanup(ctx context.Context, request attachments.BlobPublicationCleanupClaimRequest, namespaceID string) (*attachments.LocalCleanupPreparation, error) {
	if ctx == nil || repository == nil || repository.beginTx == nil || request.Validate() != nil || request.BackendKind != attachments.BackendKindLocal || namespaceID == "" {
		return nil, attachments.ErrInvalidBlobPublicationRequest
	}
	tx, err := repository.beginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var result attachments.LocalCleanupPreparation
	var publicationID string
	err = tx.QueryRow(ctx, `
		select coalesce(g.deletion_id,''),p.namespace_id::text,n.current_incarnation_id::text,p.blob_key,p.publication_id
		from public.blob_publication_intents p join public.local_blob_namespaces n on n.namespace_id=p.namespace_id
		left join public.blob_gc_deletions g on g.namespace_id=p.namespace_id and g.incarnation_id=n.current_incarnation_id
		  and g.blob_key=p.blob_key and g.local_contract_version=2 and g.deletion_state<>'completed'
		where p.project_id=$1 and p.backend_kind='local' and p.namespace_id=$2::uuid and n.admission_state='active'
		  and p.publication_state<>'completed' and p.publish_expires_at<=statement_timestamp()
		  and g.operation_id is null
		  and (g.owner_id is null or g.owner_id=$3 or g.lease_expires_at<=statement_timestamp())
		order by p.publish_expires_at,p.publication_id limit 1`, request.ProjectID, namespaceID, request.CleanupOwnerID).Scan(
		&result.DeletionID, &result.NamespaceID, &result.IncarnationID, &result.Key, &publicationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("discover local publication cleanup: %w", err)
	}
	result.PublicationID = &publicationID
	if result.DeletionID == "" {
		result.DeletionID, err = repository.nextLocalCleanupID()
		if err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &result, nil
}

func (repository *PostgresAttachmentRepository) nextLocalCleanupID() (string, error) {
	if repository.newBlobGCDeletionID != nil {
		return repository.newBlobGCDeletionID()
	}
	return ids.New("bgd")
}

var _ attachments.LocalBlobGCDiscovery = (*PostgresAttachmentRepository)(nil)
var _ attachments.LocalPublicationCleanupDiscovery = (*PostgresAttachmentRepository)(nil)
