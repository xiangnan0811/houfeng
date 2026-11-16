package migrate

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// These guards are required only by the undelivered C81 successor. Keeping
// their catalog check outside the frozen R1 snapshot preserves predecessor
// projections while making current convergence and runtime admission inspect
// the actual triggers in the same transaction as the rest of the catalog.
type comparisonInputTriggerContract struct {
	table, name, function string
	typeBits              int
	columns               []string
}

var comparisonInputTriggers = []comparisonInputTriggerContract{
	{"evidence_snapshots", "comparison_snapshot_writer_guard", "comparison_evidence_row_guard", 15, []string{}},
	{"evidence_copy_lineage", "comparison_lineage_writer_guard", "comparison_evidence_row_guard", 15, []string{}},
	{"record_revision_evidence", "comparison_reference_writer_guard", "comparison_evidence_row_guard", 15, []string{}},
	{"evidence_payloads", "comparison_payload_cleanup_guard", "comparison_evidence_row_guard", 11, []string{}},
	{"records", "comparison_record_update_guard", "comparison_root_epoch_guard", 19, []string{}},
	{"records", "comparison_record_delete_guard", "comparison_root_epoch_guard", 11, []string{}},
	{"content_delivery_epochs", "comparison_epoch_insert_delete_guard", "comparison_root_epoch_guard", 15, []string{}},
	{"content_delivery_epochs", "comparison_epoch_update_guard", "comparison_root_epoch_guard", 19, []string{"delivery_epoch", "object_id", "object_kind", "project_id"}},
	{"record_drafts", "record_draft_launch_guard", "record_draft_launch_guard", 31, []string{}},
	{"record_draft_checkpoints", "record_draft_checkpoint_launch_guard", "record_draft_launch_guard", 15, []string{}},
	{"source_legacy_retirement_bindings", "source_legacy_retirement_immutable", "reject_immutable_mutation", 27, []string{}},
}

func requiresComparisonInputTriggers(input appACLEffectiveCatalogVerifierInput) bool {
	for _, function := range input.Contract.ExpectedFunctions {
		if function.SchemaName == "public" && function.Identity == "record_comparison_index_write(bytea, bytea)" {
			return true
		}
	}
	return false
}

func verifyComparisonInputTriggerCatalog(ctx context.Context, tx pgx.Tx, owner string) error {
	for _, expected := range comparisonInputTriggers {
		var matches bool
		err := tx.QueryRow(ctx, `select exists (
			select 1 from pg_catalog.pg_trigger t
			join pg_catalog.pg_class relation on relation.oid=t.tgrelid
			join pg_catalog.pg_namespace relation_schema on relation_schema.oid=relation.relnamespace
			join pg_catalog.pg_proc p on p.oid=t.tgfoid
			join pg_catalog.pg_namespace function_schema on function_schema.oid=p.pronamespace
			where relation_schema.nspname='public' and relation.relname=$1 and t.tgname=$2
			  and relation.relkind in ('r','p') and pg_catalog.pg_get_userbyid(relation.relowner)=$6
			  and function_schema.nspname='record_platform_internal' and p.proname=$3
			  and p.pronargs=0 and p.prokind='f' and p.prosecdef and p.prorettype='pg_catalog.trigger'::regtype
			  and pg_catalog.pg_get_userbyid(p.proowner)=$6
			  and not t.tgisinternal and t.tgenabled='O' and t.tgtype::integer=$4
			  and t.tgconstraint=0 and t.tgparentid=0 and not t.tgdeferrable and not t.tginitdeferred
			  and t.tgqual is null and t.tgnargs=0 and octet_length(t.tgargs)=0
			  and t.tgoldtable is null and t.tgnewtable is null
			  and coalesce((select array_agg(a.attname::text order by a.attname::text)
			    from unnest(t.tgattr::smallint[]) x(attnum)
			    join pg_catalog.pg_attribute a on a.attrelid=t.tgrelid and a.attnum=x.attnum), '{}'::text[])=$5::text[]
		)`, expected.table, expected.name, expected.function, expected.typeBits, expected.columns, owner).Scan(&matches)
		if err != nil {
			return fmt.Errorf("read comparison input trigger %s.%s: %w", expected.table, expected.name, err)
		}
		if !matches {
			return fmt.Errorf("comparison input trigger %s.%s is missing, disabled, or has a different definition", expected.table, expected.name)
		}
	}
	if err := verifyBirthCheckpointTriggerCatalog(ctx, tx, owner); err != nil {
		return err
	}
	if err := verifyPortabilityGuardTriggerCatalog(ctx, tx, owner); err != nil {
		return err
	}
	return verifyRecordDeletionAuthorityCatalog(ctx, tx, owner)
}

func verifyPortabilityGuardTriggerCatalog(ctx context.Context, tx pgx.Tx, owner string) error {
	type contract struct {
		table, name, function string
		bits                  int
		deferred              bool
	}
	var contracts []contract
	for _, table := range []string{
		"record_export_jobs", "record_export_artifacts", "record_import_jobs", "record_import_artifacts",
		"record_import_plans", "record_import_entity_mappings", "record_attachments", "attachment_upload_parts",
		"blob_gc_pins", "blob_objects", "blob_publication_intents", "record_write_obligations", "record_write_obligation_targets",
	} {
		contracts = append(contracts, contract{table, "record_portability_v1_reference_guard", "record_portability_v1_reference_guard", 31, false},
			contract{table, "record_portability_v1_reference_check", "record_portability_v1_reference_check", 25, true})
	}
	for _, table := range []string{"attachment_uploads", "attachment_processor_jobs", "record_drafts"} {
		contracts = append(contracts, contract{table, "record_portability_v1_parent_guard", "record_portability_v1_parent_guard", 27, false},
			contract{table, "record_portability_v1_parent_check", "record_portability_v1_parent_check", 25, true})
	}
	contracts = append(contracts,
		contract{"local_blob_gc_obligations", "record_portability_v1_link_guard", "record_portability_v1_link_guard", 31, false},
		contract{"local_blob_gc_obligations", "record_portability_v1_control_check", "record_portability_v1_control_check", 29, true},
		contract{"blob_gc_deletions", "record_portability_v1_control_check", "record_portability_v1_control_check", 29, true})
	contracts = append(contracts,
		contract{"record_portability_cleanup_preparations", "record_portability_cleanup_preparation_immutable", "local_blob_immutable_identity", 27, false},
		contract{"record_portability_cleanup_targets", "record_portability_cleanup_target_immutable", "local_blob_immutable_identity", 27, false},
		contract{"record_portability_cleanup_preparations", "record_portability_cleanup_preparation_no_truncate", "local_blob_immutable_identity", 34, false},
		contract{"record_portability_cleanup_targets", "record_portability_cleanup_target_no_truncate", "local_blob_immutable_identity", 34, false},
		contract{"record_portability_v1_preparations", "record_portability_v1_preparation_immutable", "local_blob_immutable_identity", 58, false},
		contract{"record_portability_v1_artifacts", "record_portability_v1_artifact_immutable", "local_blob_immutable_identity", 58, false},
		contract{"record_portability_v1_obligations", "record_portability_v1_obligation_immutable", "local_blob_immutable_identity", 58, false},
		contract{"record_portability_v1_targets", "record_portability_v1_target_no_truncate", "local_blob_immutable_identity", 34, false},
		contract{"record_portability_v1_targets", "record_portability_v1_target_guard", "record_portability_v1_target_guard", 27, false},
		contract{"blob_gc_deletions", "record_portability_v1_gc_transition_guard", "record_portability_v1_gc_transition_guard", 23, false})
	for _, expected := range contracts {
		var valid bool
		err := tx.QueryRow(ctx, `select exists (
			select 1 from pg_catalog.pg_trigger t
			join pg_catalog.pg_class c on c.oid=t.tgrelid
			join pg_catalog.pg_namespace n on n.oid=c.relnamespace
			join pg_catalog.pg_proc p on p.oid=t.tgfoid
			join pg_catalog.pg_namespace pn on pn.oid=p.pronamespace
			left join pg_catalog.pg_constraint k on k.oid=t.tgconstraint
			where n.nspname='public' and c.relname=$1 and t.tgname=$2 and p.proname=$3
			  and pn.nspname='record_platform_internal' and p.pronargs=0 and p.prosecdef=(p.proname<>'local_blob_immutable_identity')
			  and p.prorettype='pg_catalog.trigger'::regtype and pg_catalog.pg_get_userbyid(p.proowner)=$6
			  and pg_catalog.pg_get_userbyid(c.relowner)=$6 and t.tgtype::integer=$4
			  and not t.tgisinternal and t.tgenabled='O' and t.tgparentid=0
			  and t.tgdeferrable=$5 and t.tginitdeferred=$5 and t.tgqual is null
			  and t.tgnargs=0 and octet_length(t.tgargs)=0 and t.tgoldtable is null and t.tgnewtable is null
			  and t.tgattr::text='' and (($5 and k.contype='t' and k.conrelid=c.oid
			    and k.condeferrable and k.condeferred) or (not $5 and t.tgconstraint=0))
		)`, expected.table, expected.name, expected.function, expected.bits, expected.deferred, owner).Scan(&valid)
		if err != nil {
			return fmt.Errorf("read portability trigger %s.%s: %w", expected.table, expected.name, err)
		}
		if !valid {
			return fmt.Errorf("portability trigger %s.%s is missing, disabled, or has a different definition", expected.table, expected.name)
		}
	}
	return nil
}

// The request checkpoint chain is authority-bearing even after its allocation
// advances or is tombstoned. Admission must reject a disabled or replaced
// immutability trigger before a runtime or worker can act on the chain.
func verifyBirthCheckpointTriggerCatalog(ctx context.Context, tx pgx.Tx, owner string) error {
	var valid bool
	err := tx.QueryRow(ctx, `select exists (
		select 1 from pg_catalog.pg_trigger t
		join pg_catalog.pg_class c on c.oid=t.tgrelid
		join pg_catalog.pg_namespace n on n.oid=c.relnamespace
		join pg_catalog.pg_proc f on f.oid=t.tgfoid
		join pg_catalog.pg_namespace fn on fn.oid=f.pronamespace
		where n.nspname='record_platform_internal' and c.relname='record_birth_checkpoints'
		  and c.relkind='r' and pg_catalog.pg_get_userbyid(c.relowner)=$1
		  and t.tgname='record_birth_checkpoints_immutable' and t.tgtype=58
		  and not t.tgisinternal and t.tgenabled='O' and t.tgconstraint=0
		  and t.tgparentid=0 and not t.tgdeferrable and not t.tginitdeferred
		  and t.tgqual is null and t.tgnargs=0 and octet_length(t.tgargs)=0
		  and t.tgattr=''::int2vector and t.tgoldtable is null and t.tgnewtable is null
		  and fn.nspname='record_platform_internal' and f.proname='reject_source_retirement_mutation'
		  and f.pronargs=0 and f.prokind='f' and f.prosecdef
		  and f.prorettype='pg_catalog.trigger'::regtype and pg_catalog.pg_get_userbyid(f.proowner)=$1
	)`, owner).Scan(&valid)
	if err != nil {
		return fmt.Errorf("read birth checkpoint trigger catalog: %w", err)
	}
	if !valid {
		return fmt.Errorf("birth checkpoint trigger is missing, disabled, or has a different definition")
	}
	return nil
}
