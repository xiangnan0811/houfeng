package main

import (
	"context"
	"encoding/hex"
	"errors"
	"math"
	"slices"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/auth"
	"houfeng/internal/center/ids"
	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
	"houfeng/internal/center/store"
	"houfeng/internal/center/targets"
	"houfeng/internal/center/vpsassets"
)

var errSeed = errors.New("APP fixture unavailable")

type recordLocator struct {
	RecordID           string              `json:"record_id"`
	RevisionID         string              `json:"revision_id"`
	LockVersion        uint64              `json:"lock_version"`
	AuthorizationEpoch uint64              `json:"authorization_epoch"`
	SourceKind         records.SubjectKind `json:"source_kind"`
	SourceID           string              `json:"source_id"`
	ParentVPSID        *string             `json:"parent_vps_id"`
	SourceScope        sourceScopeWire     `json:"source_scope"`
	Closure            []sourceKeyWire     `json:"closure"`
}

type sourceScopeWire struct {
	Version         recordauth.ScopeVersion   `json:"version"`
	Kind            recordauth.VisibilityKind `json:"kind"`
	ProjectID       recordauth.ProjectID      `json:"project_id"`
	AllowedRoles    []recordauth.Role         `json:"allowed_roles"`
	AllowedGroupIDs []string                  `json:"allowed_group_ids"`
	PolicyVersion   uint64                    `json:"policy_version"`
	PolicyRevision  uint64                    `json:"policy_revision"`
	CanonicalHash   string                    `json:"canonical_hash"`
}

type sourceKeyWire struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type fixtureRecords struct {
	A recordLocator `json:"a"`
	B recordLocator `json:"b"`
}

type fixture struct {
	actorID string
	groupID string
	records fixtureRecords
}

type sourceFixture struct {
	kind        records.SubjectKind
	id          string
	parentVPSID *string
}

const (
	fixtureTitleA = "Isolated source A"
	fixtureTitleB = "Isolated source B"
	fixtureBodyA  = "# Isolated source A\n\nThis is the first source record.\n"
	fixtureBodyB  = "# Isolated source B\n\nThis is the second source record.\n"
)

func seedAPP(ctx context.Context, pool *pgxpool.Pool, deploymentID recordplatform.DeploymentID, caseName, variant string) (fixture, error) {
	var result fixture
	if variant != "" && (caseName != "source-vps" || variant != intersectingSourceFixture) {
		return result, errSeed
	}
	users := store.NewPostgresUserRepository(pool)
	count, err := users.CountUsers(ctx)
	if err != nil || count != 0 {
		return result, errSeed
	}
	username, err := ids.New("g2seed")
	if err != nil {
		return result, errSeed
	}
	password, err := ids.NewSecretToken("seed")
	if err != nil || auth.SeedInitialUser(ctx, users, username, password, "G2 fixture supervisor", time.Now) != nil {
		return result, errSeed
	}
	user, err := users.FindByUsername(ctx, username)
	if err != nil || user.Role != auth.RoleAdmin || !user.IsSupervisor || user.DisabledAt != nil ||
		recordauth.ValidateActorUserID(user.UserID) != nil {
		return result, errSeed
	}
	groupID, err := ids.New("rag")
	if err != nil {
		return result, errSeed
	}
	access := store.NewPostgresAccessAdminRepository(pool)
	group, err := access.CreateGroup(ctx, user.UserID, groupID, "G2 fixture operators", time.Now().UTC())
	if err != nil || group.GroupID != groupID || access.SetMember(ctx, user.UserID, groupID, user.UserID, true, time.Now().UTC()) != nil {
		return result, errSeed
	}
	groupIDs, err := store.NewPostgresRecordAuthorizationRepository(pool).ListActorGroupIDs(ctx, recordauth.ProjectIDDefault, user.UserID)
	if err != nil || len(groupIDs) != 1 || groupIDs[0] != groupID {
		return result, errSeed
	}
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID: user.UserID, Role: recordauth.RoleProjectAdmin,
		ProjectID: recordauth.ProjectIDDefault, GroupIDs: groupIDs,
	})
	if err != nil {
		return result, errSeed
	}

	vps := store.NewPostgresVPSAssetRepository(pool)
	monitoring := store.NewPostgresMonitoringInstanceRepository(pool)
	target := store.NewPostgresTargetRepository(pool)
	registry, err := records.NewSubjectAdapterRegistry([]records.SubjectSourceAdapter{
		store.NewVPSRecordSubjectAdapter(vps),
		store.NewMonitoringInstanceRecordSubjectAdapter(monitoring),
		store.NewTargetRecordSubjectAdapter(target),
	})
	if err != nil {
		return result, errSeed
	}
	gate, err := store.NewDeploymentMembershipAdmissionGate(store.DeploymentMembershipIdentity{
		InstanceID: "compose-center", DeploymentID: deploymentID,
		ProjectID: recordplatform.ProjectIDDefault, InstanceKind: "api", Capability: "records.runtime",
	})
	if err != nil {
		return result, errSeed
	}
	readResolver := store.NewRecordSubjectReadResolver(registry, nil)
	current := store.NewPostgresCurrentRecordAuthorizationSource(pool, readResolver, gate)
	repository, err := store.NewPostgresRecordRepository(pool, gate, nil)
	if err != nil {
		return result, errSeed
	}
	service, err := records.NewRevisionService(registry, current, repository)
	if err != nil {
		return result, errSeed
	}
	sourceA, err := createSource(ctx, vps, monitoring, target, caseName, "A")
	if err != nil {
		return result, errSeed
	}
	var sourceB sourceFixture
	if variant == intersectingSourceFixture {
		if sourceA.kind != records.SubjectKindVPS || sourceA.parentVPSID != nil {
			return result, errSeed
		}
		sourceB, err = createLinkedSource(ctx, monitoring, sourceA.id, "B")
	} else {
		sourceB, err = createSource(ctx, vps, monitoring, target, caseName, "B")
	}
	if err != nil {
		return result, errSeed
	}
	visibility, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{
		Version: recordauth.VisibilityScopeVersionV1, Kind: recordauth.VisibilityKindProject,
		ProjectID: recordauth.ProjectIDDefault, PolicyVersion: recordauth.PolicyVersionV1, PolicyRevision: 1,
	})
	if err != nil {
		return result, errSeed
	}
	result.records.A, err = saveFixtureRecord(ctx, service, actor, visibility, sourceA, fixtureTitleA, fixtureBodyA)
	if err != nil {
		return fixture{}, errSeed
	}
	result.records.B, err = saveFixtureRecord(ctx, service, actor, visibility, sourceB, fixtureTitleB, fixtureBodyB)
	if err != nil {
		return fixture{}, errSeed
	}
	if err := verifyFixture(ctx, pool, user.UserID, &result.records, variant); err != nil {
		return fixture{}, errSeed
	}
	result.records.A.SourceScope, err = observeSourceScope(ctx, current, actor, result.records.A)
	if err != nil {
		return fixture{}, errSeed
	}
	result.records.B.SourceScope, err = observeSourceScope(ctx, current, actor, result.records.B)
	if err != nil {
		return fixture{}, errSeed
	}
	result.actorID, result.groupID = user.UserID, groupID
	return result, nil
}

func createSource(ctx context.Context, vps *store.PostgresVPSAssetRepository, monitoring *store.PostgresMonitoringInstanceRepository, target *store.PostgresTargetRepository, caseName, suffix string) (sourceFixture, error) {
	var result sourceFixture
	switch caseName {
	case "source-vps", "source-recovery", "source-monitoring-instance":
		asset, err := vps.CreateVPSAsset(ctx, vpsassets.CreateInput{
			DisplayName: "G2 VPS " + suffix, ProviderName: "G2 provider", Region: "G2 region",
			LifecycleStatus: vpsassets.LifecycleActive, UsageStatus: vpsassets.UsageInUse,
		})
		if err != nil || !records.ValidSubjectSourceID(records.SubjectKindVPS, asset.VPSID) {
			return result, errSeed
		}
		if caseName != "source-monitoring-instance" {
			return sourceFixture{kind: records.SubjectKindVPS, id: asset.VPSID}, nil
		}
		return createLinkedSource(ctx, monitoring, asset.VPSID, suffix)
	case "source-target":
		item, err := target.CreateTarget(ctx, targets.CreateTargetInput{
			Name: "G2 target " + suffix, TargetType: targets.TargetTypeService,
			Host: "g2-" + suffix + ".example.test", RunStatus: targets.RunStatusEnabled,
			ExecutionMonitoringInstanceLabels: []string{}, Labels: []string{},
		})
		if err != nil || !records.ValidSubjectSourceID(records.SubjectKindTarget, item.TargetID) {
			return result, errSeed
		}
		return sourceFixture{kind: records.SubjectKindTarget, id: item.TargetID}, nil
	default:
		return result, errSeed
	}
}

func createLinkedSource(ctx context.Context, monitoring *store.PostgresMonitoringInstanceRepository, vpsID, suffix string) (sourceFixture, error) {
	var result sourceFixture
	instance, link, err := monitoring.CreateLinkedMonitoringInstance(ctx, vpsID, monitoringinstances.CreateInput{
		DisplayName: "G2 monitoring " + suffix,
		LifecycleStatus: monitoringinstances.LifecyclePendingEnrollment,
		Labels: []string{},
	}, "")
	if err != nil || instance.VPSID != vpsID || link.VPSID != vpsID ||
		link.MonitoringInstanceID != instance.MonitoringInstanceID || link.UnlinkedAt != nil ||
		!records.ValidSubjectSourceID(records.SubjectKindMonitoringInstance, instance.MonitoringInstanceID) {
		return result, errSeed
	}
	return sourceFixture{kind: records.SubjectKindMonitoringInstance, id: instance.MonitoringInstanceID, parentVPSID: &vpsID}, nil
}

func saveFixtureRecord(ctx context.Context, service *records.RevisionService, actor recordauth.ActorScope, visibility recordauth.VisibilityScope, source sourceFixture, title, body string) (recordLocator, error) {
	var result recordLocator
	recordID, err := ids.New("rec")
	if err != nil {
		return result, errSeed
	}
	key, err := ids.New("g2seed")
	if err != nil {
		return result, errSeed
	}
	commit, err := service.SaveRevision(ctx, records.RevisionSaveRequest{
		Actor: actor, RecordID: recordID,
		Values: records.CompleteRevisionValues{
			Title: title, BodyMarkdown: body, MarkdownDialectVersion: records.MarkdownDialectVersionV1,
			RecordType: records.RecordTypeNote, ImpactLevel: "informational",
			VisibilityScope: visibility, OwnerID: actor.UserID,
		},
		SubjectReferences: []records.SubjectReference{{
			RegistryVersion: records.SubjectRegistryVersionV1, Kind: source.kind,
			Role: records.RelationRoleAffected, SourceID: source.id, Primary: true,
		}},
		ActivityKind:   records.DomainActivityRecordCreated,
		IdempotencyKey: key, IdempotencyOwnerID: "g2_c80_initializer",
		OwnerLeaseDuration: 30 * time.Second, IdempotencyTTL: 24 * time.Hour, OutboxTTL: 24 * time.Hour,
	})
	if err != nil || commit.RecordID != recordID || !records.ValidRevisionID(commit.RevisionID) ||
		!commit.Created || commit.Replayed || commit.RevisionNo != 1 || commit.LockVersion == 0 ||
		commit.AuthorizationEpoch == 0 || commit.Lifecycle != records.LifecycleActive {
		return result, errSeed
	}
	return recordLocator{
		RecordID: commit.RecordID, RevisionID: commit.RevisionID,
		LockVersion: commit.LockVersion, AuthorizationEpoch: commit.AuthorizationEpoch,
		SourceKind: source.kind, SourceID: source.id, ParentVPSID: source.parentVPSID,
	}, nil
}

// Read the current record through the pinned authorization source, which
// resolves its stored subject against the real live source adapter. Only the
// resulting current source scope is eligible to become a later final floor.
func observeSourceScope(ctx context.Context, current records.CurrentRecordAuthorizationSource, actor recordauth.ActorScope, locator recordLocator) (sourceScopeWire, error) {
	var result sourceScopeWire
	observed, err := current.ResolveCurrentRecordAuthorization(ctx, actor.Clone(), locator.RecordID)
	if err != nil || observed.RecordID != locator.RecordID || observed.CurrentRevisionID != locator.RevisionID ||
		observed.LockVersion != locator.LockVersion || observed.AuthorizationEpoch != locator.AuthorizationEpoch ||
		observed.Lifecycle != records.LifecycleActive || observed.Evidence.ProjectID != actor.ProjectID ||
		len(observed.Evidence.Sources) != 1 {
		return result, errSeed
	}
	observedSource := observed.Evidence.Sources[0]
	canonicalSource, err := recordauth.NormalizeSourceAuthorization(observedSource)
	if err != nil || canonicalSource.Digest != observedSource.Digest ||
		observedSource.Kind != recordauth.SourceKind(locator.SourceKind) || observedSource.SourceID != locator.SourceID ||
		observedSource.State != recordauth.SourceStateLive || observedSource.CurrentScope == nil || canonicalSource.CurrentScope == nil {
		return result, errSeed
	}
	observedScope := *observedSource.CurrentScope
	scope, err := recordauth.NormalizeVisibilityScope(observedScope)
	if err != nil || scope.ProjectID != actor.ProjectID || scope.CanonicalHash != observedScope.CanonicalHash ||
		scope.CanonicalHash == ([32]byte{}) || scope.CanonicalHash != canonicalSource.CurrentScope.CanonicalHash ||
		!slices.Equal(scope.AllowedRoles, observedScope.AllowedRoles) ||
		!slices.Equal(scope.AllowedGroupIDs, observedScope.AllowedGroupIDs) ||
		scope.PolicyVersion > math.MaxInt64 || scope.PolicyRevision > math.MaxInt64 ||
		len(scope.AllowedRoles) > 1024 || len(scope.AllowedGroupIDs) > 1024 {
		return result, errSeed
	}
	if size := len(scope.CanonicalBytes()); size == 0 || size > 4096 {
		return result, errSeed
	}
	return sourceScopeWire{
		Version: scope.Version, Kind: scope.Kind, ProjectID: scope.ProjectID,
		AllowedRoles:    append([]recordauth.Role{}, scope.AllowedRoles...),
		AllowedGroupIDs: append([]string{}, scope.AllowedGroupIDs...),
		PolicyVersion:   scope.PolicyVersion, PolicyRevision: scope.PolicyRevision,
		CanonicalHash: hex.EncodeToString(scope.CanonicalHash[:]),
	}, nil
}

// Verification is a separate read-only snapshot. It checks the stored body,
// current CAS and subject, and both complete source-graph components. It never
// rewrites ordinary record-created outbox events.
func verifyFixture(ctx context.Context, pool *pgxpool.Pool, actorID string, pair *fixtureRecords, variant string) error {
	if pair.A.RecordID == pair.B.RecordID || pair.A.RevisionID == pair.B.RevisionID {
		return errSeed
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return errSeed
	}
	defer tx.Rollback(ctx)
	for _, item := range []struct {
		locator recordLocator
		title   string
		body    string
	}{{pair.A, fixtureTitleA, fixtureBodyA}, {pair.B, fixtureTitleB, fixtureBodyB}} {
		var project, revisionID, lifecycle, title, body, author string
		var lockVersion, authorizationEpoch, revisionNo int64
		err := tx.QueryRow(ctx, `
			select r.project_id, r.current_revision_id, r.lifecycle, r.lock_version, r.authorization_epoch,
			       v.revision_no, v.title, v.body_markdown, v.author_id
			from public.records r join public.record_revisions v
			  on v.record_id=r.record_id and v.revision_id=r.current_revision_id
			where r.record_id=$1`, item.locator.RecordID).Scan(
			&project, &revisionID, &lifecycle, &lockVersion, &authorizationEpoch,
			&revisionNo, &title, &body, &author)
		if err != nil || project != "default" || revisionID != item.locator.RevisionID || lifecycle != "active" ||
			lockVersion <= 0 || uint64(lockVersion) != item.locator.LockVersion ||
			authorizationEpoch <= 0 || uint64(authorizationEpoch) != item.locator.AuthorizationEpoch ||
			revisionNo != 1 || title != item.title || body != item.body || author != actorID {
			return errSeed
		}
		var subjects, matchingSubjects, outbox int64
		if tx.QueryRow(ctx, `
			select (select count(*) from public.record_revision_subjects where revision_id=$1),
			       (select count(*) from public.record_revision_subjects
			        where revision_id=$1 and registry_version=1 and subject_kind=$2
			          and relation_role='affected' and source_id=$3 and is_primary),
			       (select count(*) from public.record_outbox
			        where project_id='default' and subject_kind='record' and subject_id=$4 and event_kind='record_created')`,
			item.locator.RevisionID, string(item.locator.SourceKind), item.locator.SourceID, item.locator.RecordID,
		).Scan(&subjects, &matchingSubjects, &outbox) != nil || subjects != 1 || matchingSubjects != 1 || outbox != 1 {
			return errSeed
		}
	}
	var additionalA []sourceKey
	if variant == intersectingSourceFixture {
		if pair.A.SourceKind != records.SubjectKindVPS || pair.A.ParentVPSID != nil ||
			pair.B.SourceKind != records.SubjectKindMonitoringInstance || pair.B.ParentVPSID == nil ||
			*pair.B.ParentVPSID != pair.A.SourceID {
			return errSeed
		}
		additionalA = []sourceKey{{kind: string(pair.B.SourceKind), id: pair.B.SourceID}}
	} else if variant != "" {
		return errSeed
	}
	left, err := readClosure(ctx, tx, pair.A, additionalA)
	if err != nil {
		return errSeed
	}
	right, err := readClosure(ctx, tx, pair.B, nil)
	if err != nil {
		return errSeed
	}
	if variant == intersectingSourceFixture {
		if len(left) != 2 || len(right) != 2 {
			return errSeed
		}
		for key := range left {
			if _, overlaps := right[key]; !overlaps {
				return errSeed
			}
		}
	} else {
		for key := range left {
			if _, overlaps := right[key]; overlaps {
				return errSeed
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return errSeed
	}
	pair.A.Closure = sourceClosureWire(left)
	pair.B.Closure = sourceClosureWire(right)
	return nil
}

type sourceKey struct {
	kind string
	id   string
}

func sourceClosureWire(keys map[sourceKey]struct{}) []sourceKeyWire {
	result := make([]sourceKeyWire, 0, len(keys))
	for key := range keys {
		result = append(result, sourceKeyWire{Kind: key.kind, ID: key.id})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Kind != result[j].Kind {
			return result[i].Kind < result[j].Kind
		}
		return result[i].ID < result[j].ID
	})
	return result
}

// Keep this historical-edge graph aligned with the fixed C80 predecessor's
// source guard. Independently require every expected root and real VPS link;
// only the named negative fixture supplies its one additional graph member.
func readClosure(ctx context.Context, tx pgx.Tx, item recordLocator, additional []sourceKey) (map[sourceKey]struct{}, error) {
	expected := map[sourceKey]struct{}{{kind: string(item.SourceKind), id: item.SourceID}: {}}
	if item.ParentVPSID != nil {
		if item.SourceKind != records.SubjectKindMonitoringInstance ||
			!records.ValidSubjectSourceID(records.SubjectKindVPS, *item.ParentVPSID) {
			return nil, errSeed
		}
		expected[sourceKey{kind: "vps", id: *item.ParentVPSID}] = struct{}{}
	} else if item.SourceKind == records.SubjectKindMonitoringInstance {
		return nil, errSeed
	}
	for _, key := range additional {
		if !records.ValidSubjectSourceID(records.SubjectKind(key.kind), key.id) {
			return nil, errSeed
		}
		expected[key] = struct{}{}
	}
	var exists bool
	switch item.SourceKind {
	case records.SubjectKindVPS:
		err := tx.QueryRow(ctx, `select exists(select 1 from public.vps_assets where vps_id=$1)`, item.SourceID).Scan(&exists)
		if err != nil || !exists {
			return nil, errSeed
		}
	case records.SubjectKindMonitoringInstance:
		err := tx.QueryRow(ctx, `select exists(select 1 from public.monitoring_instances m
			join public.vps_assets v on v.vps_id=m.vps_id
			join public.vps_monitoring_instance_links l on l.vps_id=v.vps_id and l.monitoring_instance_id=m.monitoring_instance_id
			where m.monitoring_instance_id=$1 and m.vps_id=$2 and l.unlinked_at is null)`, item.SourceID, *item.ParentVPSID).Scan(&exists)
		if err != nil || !exists {
			return nil, errSeed
		}
	case records.SubjectKindTarget:
		err := tx.QueryRow(ctx, `select exists(select 1 from public.targets where target_id=$1)`, item.SourceID).Scan(&exists)
		if err != nil || !exists {
			return nil, errSeed
		}
	default:
		return nil, errSeed
	}
	rows, err := tx.Query(ctx, `
		with recursive edges(parent_kind, parent_id, child_kind, child_id) as materialized (
		  select 'vps'::text, vps_id, 'monitoring_instance'::text, monitoring_instance_id from public.monitoring_instances
		  union select 'vps', vps_id, 'monitoring_instance', monitoring_instance_id from public.vps_monitoring_instance_links
		  union select 'vps', vps_id, 'target', target_id from public.asset_service_associations where target_id is not null
		  union select 'vps', vps_id, 'target', target_id from public.asset_domain_associations where target_id is not null
		  union select 'vps', action.vps_id, effect.resource_kind, effect.resource_id
		    from public.vps_maintenance_effects effect join public.vps_maintenance_actions action on action.action_id=effect.action_id
		), adjacency(from_kind, from_id, to_kind, to_id) as (
		  select parent_kind, parent_id, child_kind, child_id from edges
		  union all select child_kind, child_id, parent_kind, parent_id from edges
		), component(kind, id) as (
		  select $1::text, $2::text
		  union select adjacency.to_kind, adjacency.to_id from component
		    join adjacency on adjacency.from_kind=component.kind and adjacency.from_id=component.id
		)
		select kind, id from component order by kind collate "C", id collate "C" limit 1025`, string(item.SourceKind), item.SourceID)
	if err != nil {
		return nil, errSeed
	}
	defer rows.Close()
	actual := make(map[sourceKey]struct{})
	for rows.Next() {
		var key sourceKey
		if rows.Scan(&key.kind, &key.id) != nil || !records.ValidSubjectSourceID(records.SubjectKind(key.kind), key.id) {
			return nil, errSeed
		}
		actual[key] = struct{}{}
		if len(actual) > 1024 {
			return nil, errSeed
		}
	}
	if rows.Err() != nil || len(actual) != len(expected) {
		return nil, errSeed
	}
	for key := range expected {
		if _, ok := actual[key]; !ok {
			return nil, errSeed
		}
	}
	return actual, nil
}
