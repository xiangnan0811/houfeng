package store

import (
	"context"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/records"
)

// Lifecycle changes the retained root and search documents even though it
// does not create a revision. Discover their current routes before locking.
func discoverRecordLifecycleMutationResources(ctx context.Context, tx pgx.Tx, command records.RecordLifecycleCommand) (recordMutationResources, error) {
	resources, err := discoverRecordMutationUsers(ctx, tx, command.Actor.UserID, []string{command.Actor.UserID})
	if err != nil {
		return recordMutationResources{}, err
	}
	resources.Records = append(resources.Records, records.RevisionMutationRecord{ProjectID: string(command.Actor.ProjectID), RecordID: command.RecordID})
	resources.Idempotency = append(resources.Idempotency, records.RevisionMutationIdempotency{Key: command.Idempotency.Key})
	resources.Revisions = append(resources.Revisions, records.RevisionMutationRevision{RecordID: command.RecordID, RevisionID: command.CurrentRevisionID})
	resources.Projects = append(resources.Projects, records.RevisionMutationProject{ProjectID: string(command.Actor.ProjectID), Kind: records.RevisionMutationSearchCoordination})
	resources.Generations, err = readRevisionMutationRows[records.RevisionMutationGeneration](ctx, tx, `select project_id,generation,generation_state from public.record_search_generations where project_id=$1 and generation_state in ('published','building') order by generation`, command.Actor.ProjectID)
	if err != nil {
		return recordMutationResources{}, err
	}
	for _, generation := range resources.Generations {
		resources.Documents = append(resources.Documents, records.RevisionMutationDocument{ProjectID: generation.ProjectID, RecordID: command.RecordID, Generation: generation.Generation})
	}
	resources, err = discoverRecordMutationRoutes(ctx, tx, resources)
	if err != nil {
		return recordMutationResources{}, err
	}
	return normalizeRecordMutationDeclarations(resources)
}
