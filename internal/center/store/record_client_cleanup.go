package store

import (
	"context"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/recordclientcleanup"
	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/records"
)

type PostgresRecordClientCleanupRepository struct {
	platform       *PostgresRecordPlatformRepository
	allocations    *PostgresRecordBirthRepository
	authorizations records.CurrentRecordAuthorizationSource
}

func NewPostgresRecordClientCleanupRepository(pool *pgxpool.Pool, gate AdmissionGate, authorizations records.CurrentRecordAuthorizationSource) *PostgresRecordClientCleanupRepository {
	return &PostgresRecordClientCleanupRepository{
		platform: NewPostgresRecordPlatformRepository(pool, gate), authorizations: authorizations,
		allocations: NewPostgresRecordBirthRepository(pool, gate, nil),
	}
}

type cleanupReconcileCandidate struct {
	recordID      string
	allocation    *recordbirth.Allocation
	current       records.CurrentRecordAuthorization
	authorization error
}

func (repository *PostgresRecordClientCleanupRepository) Reconcile(ctx context.Context, actor recordauth.ActorScope, request recordclientcleanup.ReconcileRequest) (recordclientcleanup.ReconcileResult, error) {
	result := recordclientcleanup.ReconcileResult{Allowed: []string{}, Pending: []string{}, Invalidate: []string{}}
	if err := request.Validate(); err != nil {
		return result, err
	}
	actor, err := cleanupActor(actor, request.BrowserID)
	if err != nil {
		return result, err
	}
	if repository == nil || repository.platform == nil || nilRecordSubjectDependency(repository.authorizations) {
		return result, ErrRecordSubjectUnavailable
	}

	// Resolve today's membership rather than trusting a stale session scope.
	// Registration rechecks it after the authorization lookups.
	var member recordauth.ActorScope
	err = repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		var memberErr error
		member, memberErr = cleanupCurrentMember(ctx, transaction.tx, actor)
		return memberErr
	})
	if err != nil {
		return recordclientcleanup.ReconcileResult{}, err
	}
	actor = member

	allocationIDs := make(map[string]string, len(request.AllocationReferences))
	ids := slices.Clone(request.RecordIDs)
	for _, reference := range request.AllocationReferences {
		allocationIDs[reference.RecordID] = reference.AllocationID
		ids = append(ids, reference.RecordID)
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	candidates := make([]cleanupReconcileCandidate, 0, len(ids))
	for _, recordID := range ids {
		candidate := cleanupReconcileCandidate{recordID: recordID}
		if allocationID, referenced := allocationIDs[recordID]; referenced {
			if repository.allocations == nil {
				return recordclientcleanup.ReconcileResult{}, ErrRecordSubjectUnavailable
			}
			allocation, lookupErr := repository.allocations.GetAllocation(ctx, actor, allocationID)
			if lookupErr != nil || allocation.Validate() != nil {
				// Missing, unknown, and foreign allocations do not establish that
				// the browser's unresolved publication may be discarded.
				return recordclientcleanup.ReconcileResult{}, ErrRecordSubjectUnavailable
			}
			if allocation.State == recordbirth.StatePending {
				// A certified allocation still awaiting W2 has no record ID yet.
				return recordclientcleanup.ReconcileResult{}, ErrRecordSubjectUnavailable
			}
			if allocation.AllocationID != allocationID || allocation.RecordID != recordID ||
				allocation.Request.ActorID != actor.UserID || allocation.Request.ProjectID != string(actor.ProjectID) {
				return recordclientcleanup.ReconcileResult{}, recordauth.ErrDenied
			}
			candidate.allocation = &allocation
			switch allocation.State {
			case recordbirth.StateAllocated, recordbirth.StatePreparing, recordbirth.StatePrepared, recordbirth.StateTombstoned:
				candidates = append(candidates, candidate)
				continue
			case recordbirth.StateFinished, recordbirth.StateConfirmed:
				// A completed allocation still needs current record authorization.
			default:
				// A consumed state cannot survive a valid root-creation commit.
				return recordclientcleanup.ReconcileResult{}, ErrRecordSubjectUnavailable
			}
		}
		candidate.current, candidate.authorization = repository.authorizations.ResolveCurrentRecordAuthorization(ctx, actor, recordID)
		if candidate.authorization == nil {
			candidate.authorization = records.AuthorizeRecordResource(actor, recordauth.CapabilityRecordRead, candidate.current.Evidence)
		}
		candidates = append(candidates, candidate)
	}

	err = repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		member, memberErr := cleanupCurrentMember(ctx, transaction.tx, actor)
		if memberErr != nil {
			return memberErr
		}
		if member.CanonicalHash() != actor.CanonicalHash() {
			return ErrRecordSubjectUnavailable
		}
		for _, candidate := range candidates {
			if candidate.allocation != nil {
				gated, rooted, fenced, err := cleanupDeletionState(ctx, transaction.tx, actor.ProjectID, candidate.recordID)
				if err != nil {
					return ErrRecordSubjectUnavailable
				}
				if gated || candidate.allocation.State == recordbirth.StateTombstoned {
					result.Invalidate = append(result.Invalidate, candidate.recordID)
					continue
				}
				switch candidate.allocation.State {
				case recordbirth.StateAllocated, recordbirth.StatePreparing, recordbirth.StatePrepared:
					if rooted {
						// The allocation may have completed since lookup. Retry to
						// apply current read authorization to its committed root.
						return ErrRecordSubjectUnavailable
					}
					result.Pending = append(result.Pending, candidate.recordID)
					continue
				default:
					if !rooted {
						return ErrRecordSubjectUnavailable
					}
					if errors.Is(candidate.authorization, records.ErrRecordNotFound) ||
						(errors.Is(candidate.authorization, records.ErrRecordDeletionReserved) && !fenced) {
						// A missing root or epoch alone is not proof of deletion.
						return ErrRecordSubjectUnavailable
					}
				}
			}
			if errors.Is(candidate.authorization, ErrRecordSubjectUnavailable) {
				return ErrRecordSubjectUnavailable
			}
			if candidate.authorization != nil && candidate.current.RecordID == "" {
				if errors.Is(candidate.authorization, recordauth.ErrDenied) || errors.Is(candidate.authorization, records.ErrRecordNotFound) ||
					errors.Is(candidate.authorization, records.ErrRecordDeletionReserved) || errors.Is(candidate.authorization, ErrRecordSubjectNotFound) {
					result.Invalidate = append(result.Invalidate, candidate.recordID)
					continue
				}
				return candidate.authorization
			}
			if candidate.authorization != nil && !errors.Is(candidate.authorization, recordauth.ErrDenied) {
				return candidate.authorization
			}
			if candidate.current.RecordID != candidate.recordID {
				return ErrRecordSubjectUnavailable
			}
			if err := assertRecordReadFence(ctx, transaction.tx, candidate.recordID); err != nil {
				if errors.Is(err, records.ErrRecordDeletionReserved) {
					if candidate.allocation != nil {
						gated, rooted, fenced, stateErr := cleanupDeletionState(ctx, transaction.tx, actor.ProjectID, candidate.recordID)
						if stateErr != nil {
							return ErrRecordSubjectUnavailable
						}
						if !gated && (!rooted || !fenced) {
							return ErrRecordSubjectUnavailable
						}
					}
					result.Invalidate = append(result.Invalidate, candidate.recordID)
					continue
				}
				return ErrRecordSubjectUnavailable
			}
			var unchanged bool
			if err := transaction.tx.QueryRow(ctx, `select exists(select 1 from public.records
				where project_id=$1 and record_id=$2 and current_revision_id=$3 and lock_version=$4
				and authorization_epoch=$5 and lifecycle=$6)`, actor.ProjectID, candidate.recordID,
				candidate.current.CurrentRevisionID, int64(candidate.current.LockVersion), int64(candidate.current.AuthorizationEpoch),
				candidate.current.Lifecycle).Scan(&unchanged); err != nil || !unchanged {
				return ErrRecordSubjectUnavailable
			}
			if errors.Is(candidate.authorization, recordauth.ErrDenied) {
				result.Invalidate = append(result.Invalidate, candidate.recordID)
			} else {
				result.Allowed = append(result.Allowed, candidate.recordID)
			}
		}
		// Targets contain metadata only. A gate that commits before or after
		// registration still has a root-independent event joined by record ID.
		targets := append(slices.Clone(result.Allowed), result.Pending...)
		slices.Sort(targets)
		_, err := transaction.tx.Exec(ctx, `insert into public.browser_record_cleanup_targets(user_id,browser_id,record_id,project_id)
			select $1,$2,record_id,$3 from unnest($4::text[]) as targets(record_id) order by record_id
			on conflict (user_id,browser_id,record_id) do nothing`, actor.UserID, request.BrowserID, actor.ProjectID, targets)
		return err
	})
	if err != nil {
		return recordclientcleanup.ReconcileResult{}, err
	}
	return result, nil
}

// A missing epoch also causes the shared read fence to reject a read, but it
// is not by itself evidence that an allocated record was deleted.
func cleanupDeletionState(ctx context.Context, tx pgx.Tx, projectID recordauth.ProjectID, recordID string) (gated, rooted, fenced bool, err error) {
	err = tx.QueryRow(ctx, `select
		exists(select 1 from public.record_deletion_gates where project_id=$1 and record_id=$2),
		exists(select 1 from public.records where project_id=$1 and record_id=$2),
		exists(select 1 from public.deletion_reservations where project_id=$1 and object_kind='record'
			and object_id=$2 and state in ('fenced','committed')) or
		exists(select 1 from public.deletion_fence_leases where project_id=$1 and object_kind='record'
			and object_id=$2 and expires_at>clock_timestamp())`, projectID, recordID).Scan(&gated, &rooted, &fenced)
	return gated, rooted, fenced, err
}

func cleanupCurrentMember(ctx context.Context, tx pgx.Tx, actor recordauth.ActorScope) (recordauth.ActorScope, error) {
	member, err := NewPostgresCollaborationMembershipReader().ReadMemberActor(ctx, tx, actor.ProjectID, actor.UserID)
	if errors.Is(err, recordcollaboration.ErrMembershipDenied) {
		return recordauth.ActorScope{}, recordauth.ErrDenied
	}
	if err != nil {
		return recordauth.ActorScope{}, ErrRecordSubjectUnavailable
	}
	return member, nil
}

func cleanupActor(actor recordauth.ActorScope, browserID string) (recordauth.ActorScope, error) {
	if err := recordclientcleanup.ValidateBrowserID(browserID); err != nil {
		return recordauth.ActorScope{}, err
	}
	return recordauth.NormalizeActorScope(actor)
}

func (repository *PostgresRecordClientCleanupRepository) ValidateCursor(ctx context.Context, actor recordauth.ActorScope, browserID, cursor string) error {
	actor, err := cleanupActor(actor, browserID)
	if err != nil {
		return err
	}
	var value int64
	if cursor != "" {
		value, err = recordclientcleanup.PositiveBigint(cursor)
		if err != nil {
			return err
		}
	}
	if repository == nil || repository.platform == nil {
		return ErrRecordPlatformAdmissionUnavailable
	}
	return repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		if cursor == "" {
			return nil
		}
		var known bool
		err := transaction.tx.QueryRow(ctx, `select exists(select 1 from public.browser_record_cleanup_events e
			join public.browser_record_cleanup_targets t on t.project_id=e.project_id and t.record_id=e.record_id
			where t.user_id=$1 and t.browser_id=$2 and t.project_id=$3 and e.cursor=$4)`, actor.UserID, browserID, actor.ProjectID, value).Scan(&known)
		if err != nil {
			return err
		}
		if !known {
			return recordclientcleanup.ErrReconcileRequired
		}
		return nil
	})
}

func (repository *PostgresRecordClientCleanupRepository) Next(ctx context.Context, actor recordauth.ActorScope, browserID string) (*recordclientcleanup.Event, error) {
	actor, err := cleanupActor(actor, browserID)
	if err != nil {
		return nil, err
	}
	if repository == nil || repository.platform == nil {
		return nil, ErrRecordPlatformAdmissionUnavailable
	}
	var event *recordclientcleanup.Event
	err = repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		current := recordclientcleanup.Event{Version: 1}
		err := transaction.tx.QueryRow(ctx, `select e.cursor::text,e.operation_id,e.record_id,e.cleanup_epoch::text,e.scopes
			from public.browser_record_cleanup_events e
			join public.browser_record_cleanup_targets t on t.project_id=e.project_id and t.record_id=e.record_id
			where t.user_id=$1 and t.browser_id=$2 and t.project_id=$3
			and not exists(select 1 from public.browser_record_cleanup_receipts r
				where r.user_id=t.user_id and r.browser_id=t.browser_id and r.event_cursor=e.cursor)
			order by e.cursor limit 1`, actor.UserID, browserID, actor.ProjectID).Scan(&current.Cursor, &current.OperationID, &current.RecordID, &current.CleanupEpoch, &current.Scopes)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := current.Validate(); err != nil {
			return err
		}
		event = &current
		return nil
	})
	return event, err
}

func (repository *PostgresRecordClientCleanupRepository) Acknowledge(ctx context.Context, actor recordauth.ActorScope, browserID string, ack recordclientcleanup.Acknowledgement) error {
	actor, err := cleanupActor(actor, browserID)
	if err != nil {
		return err
	}
	if err := ack.Validate(); err != nil {
		return err
	}
	if repository == nil || repository.platform == nil {
		return ErrRecordPlatformAdmissionUnavailable
	}
	cursor, _ := recordclientcleanup.PositiveBigint(ack.Cursor)
	epoch, _ := recordclientcleanup.PositiveBigint(ack.CleanupEpoch)
	return repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		var targeted bool
		err := transaction.tx.QueryRow(ctx, `with target as (
			select t.user_id,t.browser_id,e.cursor,e.operation_id,e.record_id,e.cleanup_epoch
			from public.browser_record_cleanup_events e join public.browser_record_cleanup_targets t
			on t.project_id=e.project_id and t.record_id=e.record_id
			where t.user_id=$1 and t.browser_id=$2 and t.project_id=$3
			and e.cursor=$4 and e.operation_id=$5 and e.record_id=$6 and e.cleanup_epoch=$7
		), inserted as (
			insert into public.browser_record_cleanup_receipts(user_id,browser_id,event_cursor,operation_id,record_id,cleanup_epoch)
			select user_id,browser_id,cursor,operation_id,record_id,cleanup_epoch from target
			on conflict (user_id,browser_id,event_cursor) do nothing returning event_cursor
		) select exists(select 1 from target)`, actor.UserID, browserID, actor.ProjectID, cursor, ack.OperationID, ack.RecordID, epoch).Scan(&targeted)
		if err != nil {
			return err
		}
		if !targeted {
			return recordclientcleanup.ErrInvalidAcknowledgement
		}
		return nil
	})
}

var _ recordclientcleanup.Repository = (*PostgresRecordClientCleanupRepository)(nil)
