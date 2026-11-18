package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
	"houfeng/internal/center/sourceretirement"
)

// The unlocked candidate projection determines which W2 facts to prefetch.
// The proof is checked against fresh APP facts on the actual writer transaction.
func (repository *PostgresRecordRepository) prepareRevisionMutationAuthority(ctx context.Context, commands []records.RevisionCommitCommand) (*PreparedSourceAuthority, error) {
	if repository == nil || repository.authorization == nil || repository.platform == nil {
		return nil, ErrRecordSubjectUnavailable
	}
	resolver, ok := repository.authorization.resolver.(*RecordSubjectReadResolver)
	if !ok || resolver == nil {
		return nil, ErrRecordSubjectUnavailable
	}
	reader, ok := resolver.tombstones.(*WitnessedRecordSubjectTombstoneReader)
	if !ok || reader == nil {
		return nil, ErrRecordSubjectUnavailable
	}
	var candidate recordMutationResources
	err := repository.platform.runRecordPlatformTransaction(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly}, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		var err error
		candidate, err = discoverRevisionBatchResources(ctx, transaction.tx, repository.participants, commands, nil)
		return err
	})
	if err != nil {
		return nil, err
	}
	keys := make([]sourceretirement.SourceKey, 0, len(candidate.resources.Sources))
	for _, source := range candidate.resources.Sources {
		keys = append(keys, reader.sourceKey(recordauth.ProjectID(source.ProjectID), recordauth.SourceKind(source.SourceKind), source.SourceID))
	}
	return reader.Prefetch(ctx, keys)
}

func verifyRevisionPrefetchedRoutes(authority *PreparedSourceAuthority, resources recordMutationResources) error {
	if authority == nil || authority.reader == nil {
		return ErrRecordSubjectUnavailable
	}
	for _, source := range resources.resources.Sources {
		key := authority.reader.sourceKey(recordauth.ProjectID(source.ProjectID), recordauth.SourceKind(source.SourceKind), source.SourceID)
		if _, ok := authority.observations[key]; !ok {
			return &pgconn.PgError{Code: "40001", Message: "revision source route changed; restart with source prefetch"}
		}
	}
	return nil
}

func revisionTransactionSubjectResolver(tx pgx.Tx, authority *PreparedSourceAuthority) (*RecordSubjectReadResolver, error) {
	if authority == nil {
		return nil, ErrRecordSubjectUnavailable
	}
	live := transactionRecordSubjectSource{db: tx}
	registry, err := records.NewSubjectAdapterRegistry([]records.SubjectSourceAdapter{
		newVPSRecordSubjectAdapter(live),
		newMonitoringInstanceRecordSubjectAdapter(live),
		newTargetRecordSubjectAdapter(live),
	})
	if err != nil {
		return nil, err
	}
	return NewRecordSubjectReadResolver(registry, authority.ForTransaction(tx)), nil
}

func authorizeRevisionInput(ctx context.Context, resolver *RecordSubjectReadResolver, actor recordauth.ActorScope, input records.CompleteRevisionInput, capability recordauth.Capability) error {
	subjects := input.Subjects()
	sources := make([]recordauth.SourceAuthorization, 0, len(subjects))
	for _, subject := range subjects {
		identity, err := records.NewSubjectIdentitySnapshot(subject.Kind, subject.IdentitySnapshot)
		if err != nil {
			return err
		}
		resolved, err := resolver.Resolve(ctx, actor, RecordSubjectReadInput{
			Reference:        records.SubjectReference{RegistryVersion: subject.RegistryVersion, Kind: subject.Kind, Role: subject.Role, SourceID: subject.SourceID, Primary: subject.Primary},
			IdentitySnapshot: identity, CaptureAuthorization: subject.CaptureAuthorization,
		})
		if err != nil {
			return err
		}
		if resolved.CaptureAuthorization.Digest != subject.CaptureAuthorization.Digest {
			return &pgconn.PgError{Code: "40001", Message: "revision source authorization changed; restart transaction"}
		}
		sources = append(sources, resolved.CaptureAuthorization)
	}
	return records.AuthorizeRecordResource(actor, capability, records.RecordAuthorizationEvidence{
		ProjectID: actor.ProjectID, Visibility: input.VisibilityScope(), Sources: sources,
	})
}

func authorizeCurrentRevisionRecord(ctx context.Context, tx pgx.Tx, resolver *RecordSubjectReadResolver, actor recordauth.ActorScope, recordID string, capability recordauth.Capability) error {
	loader := &PostgresCurrentRecordAuthorizationSource{db: tx, resolver: resolver}
	snapshot, err := loader.loadCurrentRecordAuthorizationSnapshot(ctx, recordID)
	if err != nil {
		return err
	}
	normalized, err := normalizeCurrentRecordAuthorizationSnapshot(recordID, actor.ProjectID, snapshot)
	if err != nil {
		return err
	}
	sources, err := loader.resolveRecordAuthorizationSubjects(ctx, actor, normalized.projectID, normalized.subjects)
	if err != nil {
		return err
	}
	return records.AuthorizeRecordResource(actor, capability, records.RecordAuthorizationEvidence{
		ProjectID: normalized.projectID, Visibility: normalized.visibility, Sources: sources,
	})
}

// Authorization runs after acquisition and compares the actor with the exact
// fingerprint scope. Cross-record participant sources are checked separately.
func authorizeRevisionMutationBatch(ctx context.Context, tx pgx.Tx, authority *PreparedSourceAuthority, commands []records.RevisionCommitCommand, plan revisionMutationBatchPlan) error {
	if len(plan.commands) != len(commands) || len(plan.replays) != len(commands) || len(plan.noops) != len(commands) {
		return records.ErrInvalidRevisionMutationResources
	}
	resolver, err := revisionTransactionSubjectResolver(tx, authority)
	if err != nil {
		return err
	}
	members := &postgresCollaborationMembershipReader{}
	seenKeys := make(map[recordplatform.IdempotencyKey]bool, len(commands))
	futureByRevision := make(map[string]records.CompleteRevisionInput, len(commands))
	futureByRecord := make(map[string]records.CompleteRevisionInput, len(commands))
	for index, command := range commands {
		actor, err := members.ReadMemberActor(ctx, tx, command.Actor.ProjectID, command.Actor.UserID)
		if err != nil || actor.CanonicalHash() != command.Actor.CanonicalHash() {
			return recordauth.ErrDenied
		}
		if seenKeys[command.Idempotency.Key] {
			continue
		}
		seenKeys[command.Idempotency.Key] = true
		if plan.replays[index] {
			if err := authorizeCurrentRevisionRecord(ctx, tx, resolver, actor, command.RecordID, recordauth.CapabilityRecordRead); err != nil {
				return err
			}
			continue
		}
		capability := recordauth.CapabilityRecordCreate
		if command.Idempotency.Key.OperationKind == recordplatform.OperationKindRecordUpdate {
			capability = recordauth.CapabilityRecordUpdate
			if previous, ok := futureByRevision[command.BaseRevisionID]; ok {
				if err := authorizeRevisionInput(ctx, resolver, actor, previous, capability); err != nil {
					return err
				}
			} else if err := authorizeCurrentRevisionRecord(ctx, tx, resolver, actor, command.RecordID, capability); err != nil {
				return err
			}
		}
		if err := authorizeRevisionInput(ctx, resolver, actor, command.Input, capability); err != nil {
			return err
		}
		for _, record := range plan.commands[index].resources.Records {
			if record.RecordID == command.RecordID {
				continue
			}
			if previous, ok := futureByRecord[record.RecordID]; ok {
				if err := authorizeRevisionInput(ctx, resolver, actor, previous, recordauth.CapabilityRecordRead); err != nil {
					return err
				}
			} else if err := authorizeCurrentRevisionRecord(ctx, tx, resolver, actor, record.RecordID, recordauth.CapabilityRecordRead); err != nil {
				return fmt.Errorf("authorize revision source record: %w", err)
			}
		}
		if !plan.noops[index] {
			revisionID, err := command.RevisionID()
			if err != nil {
				return err
			}
			futureByRevision[revisionID] = command.Input
			futureByRecord[command.RecordID] = command.Input
		}
	}
	return nil
}
