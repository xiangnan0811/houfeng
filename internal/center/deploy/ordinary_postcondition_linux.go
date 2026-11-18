//go:build linux

package deploy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"path/filepath"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/recordauthority"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/recordplatform"
)

// The root observer independently checks the committed projection, full
// migration/privilege manifest, memberships, launch registry and files. A
// live lease is required for the final runtime proof, but restoration can resume
// after an otherwise valid participant heartbeat expires during an outage.
func ordinaryVerifyTargetPostconditions(ctx context.Context, observer *pgx.Conn, intent ordinaryUpgradeIntent, requireLiveLease bool) error {
	if ctx == nil || observer == nil || ordinaryValidateIntent(intent) != nil {
		return ordinaryUpgradePhase("postcondition-intent", errOrdinaryUpgrade)
	}
	if ordinaryObserverRequireCurrentSourceAbsent(ctx, observer) != nil {
		return ordinaryUpgradePhase("postcondition-source", errOrdinaryUpgrade)
	}
	activation, err := recordplatform.ParseContractActivationProjectionCommandV1(intent.Target.ActivationCommand)
	if err != nil {
		return errOrdinaryUpgrade
	}
	rotation, err := recordplatform.ParseDomainRotationProjectionCommandV1(intent.Target.RotationCommand)
	if err != nil {
		return errOrdinaryUpgrade
	}
	contract, err := ordinaryDecodeTargetContract(intent.Target.ContractWire)
	if err != nil || contract.SourceIdentitySHA256 != intent.Target.C81Identity {
		return errOrdinaryUpgrade
	}
	receipt := recordplatform.ProjectionCASReceiptDigestV1(intent.Target.RotationCommand)
	tx, err := observer.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return ordinaryUpgradePhase("postcondition-transaction", errOrdinaryUpgrade)
	}
	defer tx.Rollback(context.Background())
	var activationOK, rotationOK, manifestOK, transitionOK, launchOK, membershipOK bool
	err = tx.QueryRow(ctx, `select
		coalesce(s.deployment_id=$1 and s.active_profile='postgres_sync'
		and s.activation_sequence=1 and s.activation_mutation_id=$2
		and s.activation_plan_digest=$3 and s.activation_authorization_artifact_digest=$4
		and s.activation_bundle_digest=$5 and s.inventory_digest=$6 and s.approval_policy_digest=$7
		and s.activation_adapter_policy_generation=$8 and s.activation_adapter_policy_digest=$9
		and s.drain_receipt_digest=$10 and s.activation_domain_identity_epoch=$11
		and s.activation_domain_identity_set_digest=$12, false),
		coalesce(s.active_adapter_policy_generation=$13 and s.active_adapter_policy_digest=$14
		and s.active_domain_identity_epoch=$15 and s.active_domain_identity_set_digest=$16
		and s.witnessed_ledger_sequence=$17 and s.witnessed_ledger_hash=$18 and s.last_domain_identity_sequence=$17
		and s.last_domain_identity_entry_hash=$18 and s.trust_revision=$19 and s.trust_head_hash=$20
		and s.minimum_fence_contract_version=$21, false),
		coalesce(m.migrator_catalog_role=$22 and m.canonical_migration_set=$23 and m.canonical_privilege_set=$24
		and m.canonical_migration_set=(select convert_to('HOUFENG-APP-MIGRATION-SET-V1','UTF8') ||
		  string_agg(int4send(octet_length(name))||convert_to(name,'UTF8')||decode(checksum,'hex'),''::bytea order by name collate "C") from public.schema_migrations), false),
		coalesce(t.activation_command=$25 and t.rotation_command=$26 and t.rotation_receipt=$27
		and t.manifest_digest=m.manifest_digest and t.migration_set_digest=m.sorted_migration_set_digest, false),
		coalesce(l.opened and l.runtime_principal=$28 and l.worker_principal=$29 and l.manifest_digest=m.manifest_digest
		and l.revision in ($30::bigint,$30::bigint+1) and l.revision>0 and l.center_binding=$31 and l.processor_binding=$32, false),
		not exists(select 1 from public.source_authority_admission)
		and not exists(select 1 from record_platform_internal.record_birth_lineage)
		and not exists(select 1 from public.deployment_membership x where x.deployment_id=$1 and x.project_id='default' and x.instance_id not in ('compose-center','compose-processor'))
		and exists(select 1 from public.deployment_membership c join public.deployment_membership w on w.instance_id='compose-processor'
		  where c.instance_id='compose-center' and c.deployment_id=$1 and w.deployment_id=$1
		  and c.project_id='default' and w.project_id='default' and c.instance_kind='api' and w.instance_kind='worker'
		  and c.capability='records.runtime' and w.capability='records.runtime'
		  and c.deployment_epoch=2 and w.deployment_epoch=2 and c.fence_contract_version=1 and w.fence_contract_version=1
		  and c.load_balancer_admitted and not c.queue_admitted and not w.load_balancer_admitted and w.queue_admitted
		  and c.heartbeat_expires_at=w.heartbeat_expires_at and (not $33::boolean or c.heartbeat_expires_at>clock_timestamp()))
		from public.deployment_contract_state s
		join public.app_acl_manifest_head h on h.singleton
		join public.app_acl_manifest_revisions m using(manifest_revision,manifest_digest)
		join record_platform_internal.compose_membership_transition t on t.singleton
		join record_platform_internal.record_birth_launch_state l on l.singleton
		where s.project_id='default'`,
		activation.DeploymentID, activation.ActivationMutationID, activation.PlanDigest[:], activation.AuthorizationArtifactDigest[:],
		activation.ActivationBundleDigest[:], activation.InventoryDigest[:], activation.ApprovalPolicyDigest[:], int64(activation.AdapterPolicyGeneration), activation.AdapterPolicyDigest[:],
		activation.DrainReceiptDigest[:], int64(activation.IdentitySetEpoch), activation.IdentitySetDigest[:], int64(rotation.NextAdapterPolicyGeneration), rotation.NextAdapterPolicyDigest[:],
		int64(rotation.NextIdentitySetEpoch), rotation.NextIdentitySetDigest[:], int64(rotation.NextWitnessedLedgerSequence), rotation.NextWitnessedLedgerHash[:], int64(rotation.NextTrustRevision), rotation.NextTrustHeadHash[:],
		int64(rotation.NextMinimumFenceContractVersion), composeMigratorRole, contract.CanonicalMigrationSet, contract.CanonicalPrivilegeSet,
		intent.Target.ActivationCommand, intent.Target.RotationCommand, receipt[:], composeRuntimeRole, composeDeletionWorkerRole, intent.Predecessor.RegistryRevision, intent.Target.CenterLaunch, intent.Target.ProcessorLaunch, requireLiveLease).Scan(&activationOK, &rotationOK, &manifestOK, &transitionOK, &launchOK, &membershipOK)
	if err != nil {
		var pgError *pgconn.PgError
		if errors.As(err, &pgError) && len(pgError.Code) == 5 {
			valid := true
			for _, c := range pgError.Code {
				if (c < '0' || c > '9') && (c < 'A' || c > 'Z') {
					valid = false
				}
			}
			if valid {
				return ordinaryUpgradePhase("postcondition-query-sql-"+pgError.Code, errOrdinaryUpgrade)
			}
		}
		return ordinaryUpgradePhase("postcondition-query", errOrdinaryUpgrade)
	}
	for _, check := range []struct {
		name  string
		valid bool
	}{
		{"postcondition-activation", activationOK}, {"postcondition-rotation", rotationOK},
		{"postcondition-manifest", manifestOK}, {"postcondition-transition", transitionOK},
		{"postcondition-launch", launchOK}, {"postcondition-membership", membershipOK},
	} {
		if !check.valid {
			return ordinaryUpgradePhase(check.name, errOrdinaryUpgrade)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return ordinaryUpgradePhase("postcondition-commit", errOrdinaryUpgrade)
	}
	root := filepath.Join(intent.Request.WorkingDirectory, "data", "records-authority")
	state, err := recordauthority.LoadComposeState(root)
	if err != nil || state.MembershipRotation == nil || recordauthority.VerifyActivationReceipt(root, state) != nil || recordauthority.VerifyMembershipRotationReceipt(root, state) != nil {
		return ordinaryUpgradePhase("postcondition-authority", errOrdinaryUpgrade)
	}
	activationBytes, e1 := state.ActivationCommand.MarshalBinary()
	rotationBytes, e2 := state.MembershipRotation.MarshalBinary()
	if e1 != nil || e2 != nil || !bytes.Equal(activationBytes, intent.Target.ActivationCommand) || !bytes.Equal(rotationBytes, intent.Target.RotationCommand) {
		return ordinaryUpgradePhase("postcondition-commands", errOrdinaryUpgrade)
	}
	launchDir := filepath.Join(intent.Request.WorkingDirectory, "data", "center-config")
	for _, item := range []struct {
		name, kind string
		body       []byte
	}{{composeCenterBirthLaunchFile, "center", intent.Target.CenterLaunch}, {composeProcessorBirthLaunchFile, "content-processor", intent.Target.ProcessorLaunch}} {
		expected, err := recordbirth.DecodeLaunchBinding(item.body)
		value, digest, readErr := recordbirth.ReadPublishedLaunchBinding(filepath.Join(launchDir, item.name), item.kind)
		if err != nil || readErr != nil || value != expected || digest != sha256.Sum256(item.body) {
			return ordinaryUpgradePhase("postcondition-publication", errOrdinaryUpgrade)
		}
	}
	return nil
}
