package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recorddeletion"
)

// issuerGraphInventorySQL binds the graph facts that survive independently of
// adapter summaries. Payload bodies and compressed bytes are never returned.
const issuerGraphInventorySQL = `
with snapshots as materialized (
  select s.* from public.evidence_snapshots s
  where s.record_id = $1 or exists (
    select 1 from public.comparison_result_dependencies d
    where d.project_id = 'default' and d.result_snapshot_id = s.snapshot_id and d.source_record_id = $1)
), dependencies as materialized (
  select d.* from public.comparison_result_dependencies d
  where d.project_id = 'default' and (d.source_record_id = $1 or exists (
    select 1 from public.evidence_snapshots s where s.snapshot_id = d.result_snapshot_id and s.record_id = $1))
), completions as materialized (
  select c.* from public.comparison_result_dependency_index_completions c
  where c.project_id = 'default' and (exists (
    select 1 from public.evidence_snapshots s where s.snapshot_id = c.result_snapshot_id and s.record_id = $1)
    or exists (select 1 from dependencies d where d.result_snapshot_id = c.result_snapshot_id))
), sets as (
  select jsonb_build_array(
    jsonb_build_array('records', coalesce((select jsonb_agg(jsonb_build_array(
      r.record_id, r.lifecycle, r.current_revision_id, r.lock_version, r.authorization_epoch,
      r.current_visibility_scope, encode(r.current_visibility_digest,'hex')) order by r.record_id collate "C")
      from public.records r where r.project_id='default' and r.record_id=$1),'[]'::jsonb)),
    jsonb_build_array('record_revisions', coalesce((select jsonb_agg(jsonb_build_array(
      r.revision_id,r.revision_no,encode(r.canonical_hash,'hex')) order by r.revision_id collate "C")
      from public.record_revisions r where r.project_id='default' and r.record_id=$1),'[]'::jsonb)),
    jsonb_build_array('record_drafts', coalesce((select jsonb_agg(jsonb_build_array(
      d.draft_id,d.base_revision_id,d.draft_version,encode(d.payload_hash,'hex'),encode(d.etag_digest,'hex'))
      order by d.draft_id collate "C") from public.record_drafts d where d.project_id='default' and d.record_id=$1),'[]'::jsonb)),
    jsonb_build_array('record_revision_subjects', coalesce((select jsonb_agg(jsonb_build_array(
      s.revision_id,s.ordinal,s.registry_version,s.subject_kind,s.source_id,s.relation_role,s.is_primary,encode(s.capture_authorization_digest,'hex'))
      order by s.revision_id collate "C",s.ordinal) from public.record_revision_subjects s
      join public.record_revisions r on r.revision_id=s.revision_id where r.project_id='default' and r.record_id=$1),'[]'::jsonb)),
    jsonb_build_array('record_revision_attachments',coalesce((select jsonb_agg(jsonb_build_array(
      a.revision_id,a.ordinal,a.attachment_id) order by a.revision_id collate "C",a.ordinal)
      from public.record_revision_attachments a where a.record_id=$1),'[]'::jsonb)),
    jsonb_build_array('record_revision_evidence',coalesce((select jsonb_agg(jsonb_build_array(
      e.revision_id,e.ordinal,e.snapshot_id) order by e.revision_id collate "C",e.ordinal)
      from public.record_revision_evidence e where e.record_id=$1),'[]'::jsonb)),
    jsonb_build_array('evidence_capture_intents',coalesce((select jsonb_agg(jsonb_build_array(
      i.intent_id,i.kind,i.schema_version,i.snapshot_id,encode(i.preview_digest,'hex'),encode(i.source_digest,'hex'),
      i.estimated_size_bytes,to_char(i.created_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
      to_char(i.valid_until at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')) order by i.intent_id collate "C")
      from public.evidence_capture_intents i where i.record_id=$1),'[]'::jsonb)),
    jsonb_build_array('evidence_snapshots',coalesce((select jsonb_agg(jsonb_build_array(
      s.snapshot_id,s.record_id,s.kind,s.schema_version,s.source_kind,s.source_id,encode(s.source_digest,'hex'),
      encode(s.capture_authorization_digest,'hex'),encode(s.canonical_hash,'hex'),s.logical_size_bytes,
      encode(s.payload_digest,'hex')) order by s.snapshot_id collate "C") from snapshots s),'[]'::jsonb)),
    jsonb_build_array('evidence_payloads',coalesce((select jsonb_agg(jsonb_build_array(
      encode(p.payload_digest,'hex'),p.payload_encoding,p.canonical_size_bytes,p.compressed_size_bytes)
      order by p.payload_digest) from public.evidence_payloads p where exists (
        select 1 from snapshots s where s.payload_digest=p.payload_digest)),'[]'::jsonb)),
    jsonb_build_array('evidence_copy_lineage',coalesce((select jsonb_agg(jsonb_build_array(
      l.snapshot_id,l.copied_from_snapshot_id,l.copy_reason,
      to_char(l.created_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')) order by l.snapshot_id collate "C")
      from public.evidence_copy_lineage l where exists (select 1 from snapshots s
        where s.snapshot_id=l.snapshot_id or s.snapshot_id=l.copied_from_snapshot_id)),'[]'::jsonb)),
    jsonb_build_array('comparison_result_dependencies',coalesce((select jsonb_agg(jsonb_build_array(
      d.result_snapshot_id,d.source_record_id,d.source_revision_id,d.source_snapshot_id,d.project_id,
      to_char(d.created_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')) order by d.result_snapshot_id collate "C",
      d.source_record_id collate "C",d.source_revision_id collate "C" nulls first,d.source_snapshot_id collate "C" nulls first)
      from dependencies d),'[]'::jsonb)),
    jsonb_build_array('comparison_result_dependency_index_completions',coalesce((select jsonb_agg(jsonb_build_array(
      c.result_snapshot_id,encode(c.payload_digest,'hex'),c.project_id,
      to_char(c.indexed_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'))
      order by c.result_snapshot_id collate "C") from completions c),'[]'::jsonb)),
    jsonb_build_array('comparison_index',encode($2::bytea,'hex'))
  ) as value
)
select pg_catalog.sha256(pg_catalog.convert_to(jsonb_build_array('HOUFENG-DELETION-GRAPH-V2',2,'default',$1,sets.value)::text,'UTF8'))
from sets`

func readIssuerGraphInventory(ctx context.Context, tx pgx.Tx, recordID string, comparisonIndex [32]byte) ([32]byte, error) {
	var result [32]byte
	if ctx == nil || tx == nil || comparisonIndex == ([32]byte{}) {
		return result, recorddeletion.ErrDeletionSafetyUnavailable
	}
	var digest []byte
	if err := tx.QueryRow(ctx, issuerGraphInventorySQL, recordID, comparisonIndex[:]).Scan(&digest); err != nil {
		return result, fmt.Errorf("read issuer graph inventory: %w", err)
	}
	if len(digest) != len(result) {
		return result, recorddeletion.ErrDeletionSafetyUnavailable
	}
	copy(result[:], digest)
	return result, nil
}

// The route union mirrors the deletion inventory lock, including durable
// operation handoffs after an original business owner has gone away.
const issuerInventoryRoutesSQL = `
with operations as materialized (
  select g.operation_id from public.record_deletion_gates g where g.project_id='default' and g.record_id=$1
), obligations as materialized (
  select o.* from public.record_write_obligations o where o.project_id='default' and (
    o.record_id=$1 or o.operation_id in (select operation_id from operations)
    or exists (select 1 from public.record_write_obligation_targets t where t.obligation_id=o.obligation_id and t.record_id=$1)
    or (o.origin_kind in ('upload','processor_preview') and exists (
      select 1 from public.record_drafts d where d.record_id=$1
        and d.draft_id=(convert_from(o.registration_command,'UTF8')::jsonb)->>'draft_id'))
    or (o.origin_kind='upload' and exists (select 1 from public.attachment_uploads u
      join public.record_attachments a on a.attachment_id=u.attachment_id
      left join public.record_drafts d on d.draft_id=a.draft_id
      where u.upload_id=o.origin_id and (a.record_id=$1 or d.record_id=$1)))
    or (o.origin_kind='processor_preview' and exists (select 1 from public.attachment_processor_jobs j
      join public.record_attachments a on a.attachment_id=j.attachment_id
      left join public.record_drafts d on d.draft_id=a.draft_id
      where j.processor_job_id=o.origin_id and (a.record_id=$1 or d.record_id=$1)))
    or (o.origin_kind='export_artifact' and exists (select 1 from public.record_export_jobs j
      where j.export_job_id=o.origin_id and j.project_id='default' and j.record_id=$1))
    or (o.origin_kind in ('import_archive','import_attachment') and exists (
      select 1 from public.record_import_plans p join public.record_import_entity_mappings m on m.import_plan_id=p.import_plan_id
      where p.import_job_id=o.origin_id and m.entity_kind='record' and m.target_id=$1))
  )
), uploads as materialized (
  select u.* from public.attachment_uploads u where u.project_id='default' and (
    exists (select 1 from public.record_attachments a left join public.record_drafts d on d.draft_id=a.draft_id
      where a.attachment_id=u.attachment_id and (a.record_id=$1 or d.record_id=$1))
    or exists (select 1 from obligations o where o.origin_kind='upload' and o.origin_id=u.upload_id)
    or exists (select 1 from public.attachment_processor_jobs j join obligations o
      on o.origin_kind='processor_preview' and o.origin_id=j.processor_job_id where j.upload_id=u.upload_id))
), jobs as materialized (
  select j.* from public.attachment_processor_jobs j where j.upload_id in (select upload_id from uploads)
    or exists (select 1 from obligations o where o.origin_kind='processor_preview' and o.origin_id=j.processor_job_id)
), workspaces as materialized (
  select w.* from public.content_processor_workspaces w where w.processor_job_id in (select processor_job_id from jobs)
)
`

const issuerProcessorInventorySQL = issuerInventoryRoutesSQL + `
select pg_catalog.sha256(pg_catalog.convert_to(jsonb_build_array('HOUFENG-DELETION-PROCESSOR-V2',2,'default',$1,
 jsonb_build_array(
  jsonb_build_array('attachment_uploads',coalesce((select jsonb_agg(jsonb_build_array(
    u.upload_id,u.upload_state,u.attachment_id,u.origin_draft_id,u.temporary_object_key,u.temporary_object_version)
    order by u.upload_id collate "C") from uploads u),'[]'::jsonb)),
  jsonb_build_array('attachment_processor_jobs',coalesce((select jsonb_agg(jsonb_build_array(
    j.processor_job_id,j.upload_id,j.attachment_id,j.processor_state,j.owner_id,j.owner_generation,
    to_char(j.lease_expires_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),j.attempt,
    encode(p.sha256_digest,'hex'),p.object_version,p.size_bytes,p.namespace_id,p.obligation_id)
    order by j.processor_job_id collate "C") from jobs j
    left join public.attachment_upload_parts p on p.upload_id=j.upload_id and p.part_number=1),'[]'::jsonb)),
  jsonb_build_array('content_processor_workspaces',coalesce((select jsonb_agg(jsonb_build_array(
    w.workspace_id,w.processor_job_id,w.attempt,w.workspace_state,encode(w.workspace_path_digest,'hex'),
    to_char(w.expires_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'))
    order by w.workspace_id collate "C") from workspaces w),'[]'::jsonb)),
  jsonb_build_array('content_workspace_purge_receipts',coalesce((select jsonb_agg(jsonb_build_array(
    r.workspace_id,encode(r.removed_surface_digest,'hex'),encode(r.receipt_digest,'hex'),r.removed_row_count,
    to_char(r.verified_absent_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
    to_char(r.created_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'))
    order by r.workspace_id collate "C") from public.content_workspace_purge_receipts r
    where r.workspace_id in (select workspace_id from workspaces)),'[]'::jsonb))
 ))::text,'UTF8'))`

func readIssuerProcessorInventory(ctx context.Context, tx pgx.Tx, recordID string) ([32]byte, error) {
	var result [32]byte
	if ctx == nil || tx == nil {
		return result, recorddeletion.ErrDeletionSafetyUnavailable
	}
	var digest []byte
	if err := tx.QueryRow(ctx, issuerProcessorInventorySQL, recordID).Scan(&digest); err != nil {
		return result, fmt.Errorf("read issuer processor inventory: %w", err)
	}
	if len(digest) != len(result) {
		return result, recorddeletion.ErrDeletionSafetyUnavailable
	}
	copy(result[:], digest)
	return result, nil
}

const issuerObligationInventorySQL = issuerInventoryRoutesSQL + `
, operation_ids as materialized (
  select operation_id from operations union select operation_id from obligations where operation_id is not null
), export_jobs as materialized (
  select j.* from public.record_export_jobs j where j.project_id='default' and (j.record_id=$1 or exists (
    select 1 from obligations o where o.origin_kind='export_artifact' and o.origin_id=j.export_job_id))
), import_jobs as materialized (
  select j.* from public.record_import_jobs j where j.project_id='default' and (exists (
    select 1 from public.record_import_plans p join public.record_import_entity_mappings m on m.import_plan_id=p.import_plan_id
    where p.import_job_id=j.import_job_id and m.entity_kind='record' and m.target_id=$1)
    or exists (select 1 from obligations o where o.origin_kind in ('import_archive','import_attachment') and o.origin_id=j.import_job_id))
), gc as materialized (
  select g.* from public.blob_gc_deletions g where g.operation_id in (select operation_id from operation_ids)
    or exists (select 1 from obligations o where o.backend_kind=g.backend_kind
      and o.namespace_id is not distinct from g.namespace_id and o.canonical_key=g.blob_key)
), recovery as materialized (select public.local_blob_recovery_inventory() as inventory),
 recovery_rows as materialized (
  select t->>'name' as name,r as fact from recovery,
    lateral jsonb_array_elements(recovery.inventory->'tables') t,
    lateral jsonb_array_elements(t->'rows') r
  where t->>'name' in ('attachment_purge_preparations','attachment_purge_targets',
    'record_portability_cleanup_preparations','record_portability_cleanup_targets','local_blob_gc_obligations')
 )
select pg_catalog.sha256(pg_catalog.convert_to(jsonb_build_array('HOUFENG-DELETION-OBLIGATION-V2',2,'default',$1,
 jsonb_build_array(
  jsonb_build_array('record_write_obligations',coalesce((select jsonb_agg(jsonb_build_array(
    o.obligation_id,o.writer_instance,o.request_seq,o.writer_principal,o.project_id,o.origin_kind,o.origin_id,o.origin_generation,
    o.origin_owner_id,o.origin_owner_generation,to_char(o.origin_owner_expires_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
    encode(o.plan_digest,'hex'),encode(o.archive_digest,'hex'),o.namespace_id,o.incarnation_id,o.backend_kind,o.canonical_key,
    encode(o.sha256_digest,'hex'),o.size_bytes,o.publication_id,o.record_id,o.record_epoch,o.operation_id,
    encode(pg_catalog.sha256(o.registration_command),'hex'),o.temporary_key,o.dispatch_id,
    to_char(o.dispatched_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),o.temporary_device,o.temporary_inode,
    to_char(o.temporary_created_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),o.object_version,o.outcome,
    to_char(o.returned_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),encode(pg_catalog.sha256(o.return_command),'hex'),
    to_char(o.handed_off_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
    to_char(o.published_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),o.cleanup_owner_id,o.cleanup_generation,
    to_char(o.cleanup_expires_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),o.cleanup_dispatch_id,o.cleanup_outcome,
    encode(o.cleanup_receipt,'hex'),to_char(o.cleanup_completed_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
    o.quota_settlement_id,to_char(o.quota_settled_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
    to_char(o.created_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'))
    order by o.obligation_id collate "C") from obligations o),'[]'::jsonb)),
  jsonb_build_array('record_write_obligation_targets',coalesce((select jsonb_agg(jsonb_build_array(
    t.obligation_id,t.record_id,t.attachment_id,encode(t.plan_digest,'hex'),t.record_epoch,
    to_char(t.bound_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
    to_char(t.handed_off_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'))
    order by t.obligation_id collate "C",t.record_id collate "C") from public.record_write_obligation_targets t
    where t.obligation_id in (select obligation_id from obligations)),'[]'::jsonb)),
  jsonb_build_array('attachment_upload_parts',coalesce((select jsonb_agg(jsonb_build_array(
    p.upload_id,p.part_number,p.size_bytes,encode(p.sha256_digest,'hex'),p.object_version,p.namespace_id,p.obligation_id,
    to_char(p.created_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'))
    order by p.upload_id collate "C",p.part_number) from public.attachment_upload_parts p
    where p.upload_id in (select upload_id from uploads) or p.obligation_id in (select obligation_id from obligations)),'[]'::jsonb)),
  jsonb_build_array('record_export_jobs',coalesce((select jsonb_agg(jsonb_build_array(
    j.export_job_id,j.project_id,j.actor_id,j.idempotency_key,j.export_kind,j.export_mode,j.job_state,j.failure_code,j.lock_version,
    encode(j.request_fingerprint,'hex'),encode(j.inventory_digest,'hex'),j.authorization_epoch,j.record_id,j.revision_id,
    j.owner_id,j.owner_generation,to_char(j.lease_expires_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
    to_char(j.expires_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
    to_char(j.created_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
    to_char(j.updated_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'))
    order by j.export_job_id collate "C") from export_jobs j),'[]'::jsonb)),
  jsonb_build_array('record_export_artifacts',coalesce((select jsonb_agg(jsonb_build_array(
    a.export_artifact_id,a.export_job_id,a.artifact_kind,a.content_type,a.backend_kind,a.blob_key,a.object_version_id,
    a.namespace_id,a.obligation_id,encode(a.sha256,'hex'),a.byte_size,
    to_char(a.expires_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
    to_char(a.revoked_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
    to_char(a.created_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'))
    order by a.export_artifact_id collate "C") from public.record_export_artifacts a
    where a.export_job_id in (select export_job_id from export_jobs) or a.obligation_id in (select obligation_id from obligations)),'[]'::jsonb)),
  jsonb_build_array('record_import_jobs',coalesce((select jsonb_agg(jsonb_build_array(
    j.import_job_id,j.project_id,j.actor_id,j.idempotency_key,j.job_state,j.failure_code,j.identity_classification,
    encode(j.archive_digest,'hex'),j.lock_version,j.owner_id,j.owner_generation,
    to_char(j.lease_expires_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
    to_char(j.expires_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
    to_char(j.created_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
    to_char(j.updated_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'))
    order by j.import_job_id collate "C") from import_jobs j),'[]'::jsonb)),
  jsonb_build_array('record_import_plans',coalesce((select jsonb_agg(jsonb_build_array(
    p.import_plan_id,p.import_job_id,encode(p.plan_digest,'hex'),p.object_count,p.remap_count,
    to_char(p.expires_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
    to_char(p.created_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'))
    order by p.import_plan_id collate "C") from public.record_import_plans p
    where p.import_job_id in (select import_job_id from import_jobs)),'[]'::jsonb)),
  jsonb_build_array('record_import_entity_mappings',coalesce((select jsonb_agg(jsonb_build_array(
    m.import_plan_id,m.entity_kind,m.source_id,encode(m.source_identity_digest,'hex'),m.target_id,
    to_char(m.created_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'))
    order by m.import_plan_id collate "C",m.entity_kind collate "C",m.source_identity_digest)
    from public.record_import_entity_mappings m join public.record_import_plans p on p.import_plan_id=m.import_plan_id
    where p.import_job_id in (select import_job_id from import_jobs)),'[]'::jsonb)),
  jsonb_build_array('record_import_artifacts',coalesce((select jsonb_agg(jsonb_build_array(
    a.import_artifact_id,a.import_job_id,a.artifact_role,a.backend_kind,a.blob_key,a.object_version_id,a.namespace_id,a.obligation_id,
    encode(a.sha256,'hex'),a.byte_size,to_char(a.expires_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
    to_char(a.created_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'))
    order by a.import_artifact_id collate "C") from public.record_import_artifacts a
    where a.import_job_id in (select import_job_id from import_jobs) or a.obligation_id in (select obligation_id from obligations)),'[]'::jsonb)),
  jsonb_build_array('blob_gc_deletions',coalesce((select jsonb_agg(jsonb_build_array(
    g.deletion_id,g.project_id,g.purge_mode,g.blob_key,encode(g.sha256_digest,'hex'),g.object_version,g.size_bytes,g.backend_kind,
    to_char(g.blob_created_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),g.deletion_state,g.owner_id,g.owner_generation,g.attempt,
    to_char(g.lease_expires_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
    to_char(g.retry_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),g.physical_delete_result,encode(g.receipt_digest,'hex'),
    to_char(g.completed_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),g.namespace_id,g.operation_id,g.local_contract_version,
    g.incarnation_id,g.cleanup_dispatch_id,encode(g.inventory_digest,'hex'),g.final_identity_bound,g.final_was_present,g.final_device,g.final_inode,
    encode(pg_catalog.sha256(g.completion_command),'hex'),to_char(g.created_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
    to_char(g.updated_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'))
    order by g.deletion_id collate "C") from gc g),'[]'::jsonb)),
  jsonb_build_array('attachment_purge_preparations',coalesce((select jsonb_agg(jsonb_build_array(
    fact->'operation_id',fact->'record_id',fact->'namespace_id',fact->'incarnation_id',substr(fact->>'surface_digest',3),
    fact->'logical_rows',fact->'upload_part_rows',fact->'workspace_count',substr(fact->>'workspace_digest',3),
    to_char((fact->>'prepared_at')::timestamptz at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'))
    order by fact->>'operation_id' collate "C") from recovery_rows
    where name='attachment_purge_preparations' and fact->>'operation_id' in (select operation_id from operation_ids)),'[]'::jsonb)),
  jsonb_build_array('attachment_purge_targets',coalesce((select jsonb_agg(jsonb_build_array(
    fact->'operation_id',fact->'deletion_id',fact->'blob_key',fact->'metadata_present')
    order by fact->>'operation_id' collate "C",fact->>'blob_key' collate "C") from recovery_rows
    where name='attachment_purge_targets' and fact->>'operation_id' in (select operation_id from operation_ids)),'[]'::jsonb)),
  jsonb_build_array('record_portability_cleanup_preparations',coalesce((select jsonb_agg(jsonb_build_array(
    fact->'operation_id',fact->'record_id',fact->'namespace_id',fact->'incarnation_id',substr(fact->>'surface_digest',3),
    fact->'removed_rows',to_char((fact->>'prepared_at')::timestamptz at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'))
    order by fact->>'operation_id' collate "C") from recovery_rows
    where name='record_portability_cleanup_preparations' and fact->>'operation_id' in (select operation_id from operation_ids)),'[]'::jsonb)),
  jsonb_build_array('record_portability_cleanup_targets',coalesce((select jsonb_agg(jsonb_build_array(
    fact->'operation_id',fact->'deletion_id',fact->'blob_key')
    order by fact->>'operation_id' collate "C",fact->>'blob_key' collate "C") from recovery_rows
    where name='record_portability_cleanup_targets' and fact->>'operation_id' in (select operation_id from operation_ids)),'[]'::jsonb)),
  jsonb_build_array('local_blob_gc_obligations',coalesce((select jsonb_agg(jsonb_build_array(
    fact->'deletion_id',fact->'obligation_id',fact->'seal_obligation')
    order by fact->>'deletion_id' collate "C",fact->>'obligation_id' collate "C") from recovery_rows
    where name='local_blob_gc_obligations' and (fact->>'deletion_id' in (select deletion_id from gc)
      or fact->>'obligation_id' in (select obligation_id from obligations))),'[]'::jsonb))
 ))::text,'UTF8'))`

func readIssuerInventories(ctx context.Context, tx pgx.Tx, recordID string, comparisonIndex [32]byte) (graph, processor, obligation [32]byte, err error) {
	if ctx == nil || tx == nil {
		err = recorddeletion.ErrDeletionSafetyUnavailable
		return
	}
	if _, err = tx.Exec(ctx, `set local bytea_output = 'hex'`); err != nil {
		return
	}
	graph, err = readIssuerGraphInventory(ctx, tx, recordID, comparisonIndex)
	if err != nil {
		return
	}
	processor, err = readIssuerProcessorInventory(ctx, tx, recordID)
	if err != nil {
		return
	}
	var digest []byte
	if err = tx.QueryRow(ctx, issuerObligationInventorySQL, recordID).Scan(&digest); err != nil {
		err = fmt.Errorf("read issuer obligation inventory: %w", err)
		return
	}
	if len(digest) != len(obligation) {
		err = recorddeletion.ErrDeletionSafetyUnavailable
		return
	}
	copy(obligation[:], digest)
	return
}
