//go:build linux

package main

import "context"

// Genesis is read from the actual databases after the real migration and
// provisioning APIs return. In particular, a source-authority generation is
// not an application enrollment, a retirement, or a ledger entry.
func checkW1Genesis(ctx context.Context, db *domainDB) error {
	if db.verifyPhysical(ctx) != nil {
		return errDomain
	}
	var valid bool
	err := db.pool.QueryRow(ctx, `
		select not exists (select 1 from public.deletion_ledger_entries)
		  and not exists (select 1 from public.deletion_ledger_checkpoints)
		  and not exists (select 1 from public.record_platform_domain_attestations)
		  and (select count(*)=1 and bool_and(singleton and sequence=0
		       and entry_hash=decode(repeat('00',32),'hex')) from public.deletion_ledger_head)`).Scan(&valid)
	if err != nil || !valid || db.verifyPhysical(ctx) != nil {
		return errDomain
	}
	return nil
}

func checkW2Genesis(ctx context.Context, db *domainDB) error {
	if db.verifyPhysical(ctx) != nil {
		return errDomain
	}
	var valid bool
	err := db.pool.QueryRow(ctx, `
		select not exists (select 1 from public.deletion_witness_entries)
		  and (select count(*)=1 and bool_and(singleton and sequence=0
		       and entry_hash=decode(repeat('00',32),'hex')) from public.deletion_witness_head)
		  and not exists (select 1 from public.recovery_trust_witness_entries)
		  and (select count(*)=1 and bool_and(singleton and trust_revision=0
		       and entry_hash=decode(repeat('00',32),'hex')) from public.recovery_trust_witness_head)
		  and not exists (select 1 from public.record_platform_domain_attestations)
		  and (select count(*)=1 and bool_and(singleton and generation=1 and mode='active'
		       and recovery_id is null and coverage_seal is null) from public.source_authority_state)
		  and (select count(*)=3 and count(*) filter (where generation=1 and recovery_id is null
		       and (principal::text,purpose) in (('g2_w2_source_reader','reader'),
		       ('g2_w2_source_producer','producer'),('g2_w2_source_provisioner','provisioner')))=3
		       from public.source_authority_principals)
		  and not exists (select 1 from public.source_application_enrollments)
		  and not exists (select 1 from public.source_application_activations)
		  and not exists (select 1 from public.source_retirement_prepares)
		  and not exists (select 1 from public.source_retirement_terminals)
		  and not exists (select 1 from public.source_retirement_unknown)
		  and (select count(*)=1 and bool_and(singleton and sequence=0
		       and entry_hash=decode(repeat('00',32),'hex')) from public.source_retirement_head)
		  and not exists (select 1 from public.source_authority_recoveries)
		  and not exists (select 1 from public.source_authority_credentials)
		  and not exists (select 1 from public.source_authority_handoff_events)
		  and not exists (select 1 from public.record_birth_keys)
		  and not exists (select 1 from public.record_birth_lineage)
		  and not exists (select 1 from public.record_birth_bindings)
		  and not exists (select 1 from public.record_birth_enabled)
		  and not exists (select 1 from public.record_birth_principals)
		  and not exists (select 1 from public.record_birth_requests)
		  and not exists (select 1 from public.record_birth_events)
		  and not exists (select 1 from public.record_birth_request_coverage)
		  and (select count(*)=1 and bool_and(singleton and sequence=0
		       and receipt_hash=decode(repeat('00',32),'hex')) from public.record_birth_head)
		  and (select count(*)=1 and bool_and(singleton and sequence=0
		       and checkpoint_hash=decode(repeat('00',32),'hex')) from public.record_birth_checkpoint_anchor)`).Scan(&valid)
	if err != nil || !valid || db.verifyPhysical(ctx) != nil {
		return errDomain
	}
	return nil
}

func checkControlGenesis(ctx context.Context, db *domainDB) error {
	if db.verifyPhysical(ctx) != nil {
		return errDomain
	}
	var valid bool
	err := db.pool.QueryRow(ctx, `
		select not exists (select 1 from public.record_platform_domain_attestations)
		  and not exists (select 1 from public.record_platform_s3_witness_identities)
		  and not exists (select 1 from public.record_platform_s3_witness_attestations)
		  and not exists (select 1 from public.recovery_trust_entries)
		  and (select count(*)=1 and bool_and(singleton and trust_revision=0
		       and entry_hash=decode(repeat('00',32),'hex')) from public.recovery_trust_head)
		  and not exists (select 1 from public.recovery_point_manifests)
		  and not exists (select 1 from public.recovery_inventory)
		  and not exists (select 1 from public.backup_attempts)
		  and not exists (select 1 from public.backup_workspaces)
		  and not exists (select 1 from public.restore_attempts)
		  and not exists (select 1 from public.restore_workspaces)
		  and not exists (select 1 from public.recovery_purge_receipts)`).Scan(&valid)
	if err != nil || !valid || db.verifyPhysical(ctx) != nil {
		return errDomain
	}
	return nil
}
