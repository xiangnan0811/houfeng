package store_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/http/handlers"
	"houfeng/internal/center/http/sessionctx"
	"houfeng/internal/center/portability"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/recordidentity"
	"houfeng/internal/center/records"
	"houfeng/internal/center/store"
)

type importBirthFaultAuthority struct {
	recordbirth.Authority
	outcomes []string
	reserves int
	confirms int
	reached  chan struct{}
	resume   chan struct{}
}

func (authority *importBirthFaultAuthority) Reserve(ctx context.Context, binding recordbirth.Binding, request recordbirth.Request, proof []byte) ([]byte, error) {
	authority.reserves++
	return authority.Authority.Reserve(ctx, binding, request, proof)
}

func (authority *importBirthFaultAuthority) Confirm(ctx context.Context, binding recordbirth.Binding, reservationID string, commitment [32]byte) ([]byte, error) {
	authority.confirms++
	outcome := "success"
	if len(authority.outcomes) > 0 {
		outcome, authority.outcomes = authority.outcomes[0], authority.outcomes[1:]
	}
	if authority.reached != nil {
		close(authority.reached)
		select {
		case <-authority.resume:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if outcome == "fail" {
		return nil, recordbirth.ErrUnavailable
	}
	receipt, err := authority.Authority.Confirm(ctx, binding, reservationID, commitment)
	if err == nil && outcome == "lost" {
		return nil, recordbirth.ErrUnavailable
	}
	return receipt, err
}

type importBirthCommitFault struct {
	records.RevisionCommitStore
	loseResponse bool
	calls        int
}

func (commit *importBirthCommitFault) CommitRevisionsFinishing(ctx context.Context, commands []records.RevisionCommitCommand, finish records.RevisionCommitFinish) ([]records.RevisionCommitResult, error) {
	commit.calls++
	results, err := commit.RevisionCommitStore.CommitRevisionsFinishing(ctx, commands, finish)
	if err == nil && commit.loseResponse {
		commit.loseResponse = false
		return nil, errors.New("APP commit acknowledgement lost")
	}
	return results, err
}

// The S3 boundary is in memory; APP obligations, import jobs, revisions and W2
// receipts use the real restricted PostgreSQL paths.
type importBirthReplayBlob struct {
	contents map[attachments.ObjectVersion][]byte
	writes   int
	reads    int
}

func (blob *importBirthReplayBlob) Put(_ context.Context, request attachments.PutRequest, reader io.Reader) (attachments.ObjectVersion, error) {
	payload, err := io.ReadAll(reader)
	if err != nil {
		return attachments.ObjectVersion{}, err
	}
	digest := sha256.Sum256(payload)
	if digest != request.ExpectedSHA256 || int64(len(payload)) != request.ExpectedSizeBytes {
		return attachments.ObjectVersion{}, attachments.ErrBlobHashMismatch
	}
	blob.writes++
	version := attachments.ObjectVersion{
		Key: fmt.Sprintf("sha256/%x", digest), VersionID: fmt.Sprintf("test-version-%d", blob.writes),
		SHA256: digest, SizeBytes: int64(len(payload)),
	}
	blob.contents[version] = append([]byte(nil), payload...)
	return version, nil
}

func (blob *importBirthReplayBlob) Open(_ context.Context, version attachments.ObjectVersion, _ attachments.ByteRange) (io.ReadCloser, error) {
	blob.reads++
	payload, ok := blob.contents[version]
	if !ok {
		return nil, attachments.ErrBlobNotFound
	}
	return io.NopCloser(bytes.NewReader(payload)), nil
}

func (blob *importBirthReplayBlob) Stat(_ context.Context, version attachments.ObjectVersion) (attachments.ObjectInfo, error) {
	if _, ok := blob.contents[version]; !ok {
		return attachments.ObjectInfo{}, attachments.ErrBlobNotFound
	}
	return attachments.ObjectInfo{Version: version}, nil
}

func (blob *importBirthReplayBlob) Delete(_ context.Context, version attachments.ObjectVersion) (attachments.DeletionReceipt, error) {
	_, found := blob.contents[version]
	delete(blob.contents, version)
	return attachments.DeletionReceipt{Version: version, Deleted: found}, nil
}

type importBirthReplayAllocations struct {
	*store.PostgresRecordBirthRepository
	reached     chan struct{}
	resume      chan struct{}
	unavailable bool
}

func (reader *importBirthReplayAllocations) GetAllocation(ctx context.Context, actor recordauth.ActorScope, allocationID string) (recordbirth.Allocation, error) {
	if reader.unavailable {
		return recordbirth.Allocation{}, recordbirth.ErrUnavailable
	}
	allocation, err := reader.PostgresRecordBirthRepository.GetAllocation(ctx, actor, allocationID)
	if reader.reached != nil {
		close(reader.reached)
		select {
		case <-reader.resume:
		case <-ctx.Done():
			return recordbirth.Allocation{}, ctx.Err()
		}
	}
	return allocation, err
}

func importBirthReplayHTTP(t *testing.T, ctx context.Context, service *portability.Service, methodPath string, actor recordauth.ActorScope, body []byte, contentType, key string, pending bool, result any) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, methodPath, bytes.NewReader(body))
	request.Header.Set("Content-Type", contentType)
	if key != "" {
		request.Header.Set("Idempotency-Key", key)
	}
	request = request.WithContext(sessionctx.WithActorScope(ctx, actor))
	response := httptest.NewRecorder()
	handlers.RecordPortability(service).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("import replay HTTP status=%d body=%s", response.Code, response.Body.String())
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &fields); err != nil {
		t.Fatal(err)
	}
	status, present := fields["certification_pending"]
	if pending && (!present || string(status) != "true") || !pending && present {
		t.Fatalf("import replay HTTP pending=%t body=%s", pending, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), result); err != nil {
		t.Fatal(err)
	}
}

func waitImportBirthReplayBlocked(t *testing.T, ctx context.Context, fixture store.ImportBirthReplayFixture, blockerPID int32) {
	t.Helper()
	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		err := fixture.AppOwner.QueryRow(waitCtx, `select exists(select 1 from pg_catalog.pg_stat_activity
			where datname=current_database() and $1=any(pg_catalog.pg_blocking_pids(pid)))`, blockerPID).Scan(&waiting)
		if err != nil {
			t.Fatalf("observe import membership lock wait: %v", err)
		}
		if waiting {
			return
		}
		select {
		case <-ticker.C:
		case <-waitCtx.Done():
			t.Fatalf("import did not wait for membership row: %v", waitCtx.Err())
		}
	}
}

func TestPostgresIntegrationImportAppliedBirthConfirmationReplay(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	fixture := store.NewImportBirthReplayFixture(t, ctx)
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID: "usr_000000000000000000000001", Role: recordauth.RoleProjectAdmin, ProjectID: recordauth.ProjectIDDefault,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.AppOwner.Exec(ctx, `insert into public.record_access_groups(group_id,display_name) values('rag_importreplay','Import replay')`); err != nil {
		t.Fatal(err)
	}
	destination := importDestinationReference(records.SubjectKindVPS, fixture.Destination)

	for _, scenario := range []struct {
		name          string
		loseCommit    bool
		outcomes      []string
		firstIsDryRun bool
	}{
		{name: "appcommitlost", loseCommit: true, outcomes: []string{"fail", "success"}},
		{name: "confirmfailed", outcomes: []string{"fail", "fail", "success"}, firstIsDryRun: true},
		{name: "confirmlost", outcomes: []string{"lost", "fail", "success"}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			fixture.Refresh()
			authority := &importBirthFaultAuthority{Authority: fixture.Authority, outcomes: append([]string(nil), scenario.outcomes...)}
			birthApplication := store.NewPostgresRecordBirthRepository(fixture.Runtime, fixture.Gate, nil)
			birth, err := recordbirth.NewCoordinator(birthApplication, fixture.Reader, authority)
			if err != nil {
				t.Fatal(err)
			}
			birthRepository := store.NewPostgresRecordBirthRepository(fixture.Runtime, fixture.Gate, birth)
			subjects, err := records.NewSubjectAdapterRegistry([]records.SubjectSourceAdapter{
				store.NewVPSRecordSubjectAdapter(store.NewPostgresVPSAssetRepository(fixture.Runtime)),
			})
			if err != nil {
				t.Fatal(err)
			}
			resolver := store.NewRecordSubjectReadResolver(subjects, fixture.Tombstones)
			baseCurrent := store.NewPostgresCurrentRecordAuthorizationSource(fixture.Runtime, resolver, fixture.Gate)
			revisionStore, err := store.NewPostgresRecordRepository(fixture.Runtime, fixture.Gate, nil, baseCurrent, birth)
			if err != nil {
				t.Fatal(err)
			}
			commits := &importBirthCommitFault{RevisionCommitStore: revisionStore, loseResponse: scenario.loseCommit}
			revisions, err := records.NewRevisionService(subjects, baseCurrent, commits, birth)
			if err != nil {
				t.Fatal(err)
			}
			application, err := records.NewApplication(importDestinationReadStub{}, revisions, importDestinationLifecycleStub{}, importDestinationDraftStub{}, records.ApplicationOptions{
				IdempotencyOwnerID: "import-birth-replay", OwnerLeaseDuration: time.Minute, IdempotencyTTL: time.Hour, OutboxTTL: time.Hour,
			})
			if err != nil {
				t.Fatal(err)
			}
			imports := store.NewPostgresRecordPortabilityRepository(fixture.Runtime, fixture.Gate, fixture.Tombstones, birth)
			blob := &importBirthReplayBlob{contents: make(map[attachments.ObjectVersion][]byte)}
			writer, err := attachments.NewManagedBlobWriter(blob, attachments.BackendKindS3, store.NewPostgresLocalBlobRepository(fixture.Runtime, fixture.Tombstones, birth))
			if err != nil {
				t.Fatal(err)
			}
			allocations := &importBirthReplayAllocations{PostgresRecordBirthRepository: birthRepository}
			service, err := portability.NewService(portability.Options{
				Enabled: true, BackendKind: "s3", Documents: importDestinationDocumentSource{}, Jobs: imports,
				Imports: imports, Importer: application, BirthCoordinator: birth, BirthAllocations: allocations,
				ImportAuthorization: baseCurrent, Subjects: subjects, Staging: portability.NewLeasedBlobStore(blob, writer),
				Rebuilder: portability.NewAuthoritativeProjectionRebuilder(),
			})
			if err != nil {
				t.Fatal(err)
			}
			archive := importDestinationArchive(t, "rec_source"+scenario.name, "Certification replay "+scenario.name)
			request := portability.DryRunRequest{Actor: actor, IdempotencyKey: "birth-replay-" + scenario.name, Archive: archive, DestinationSubject: destination}
			preview, err := service.DryRun(ctx, request)
			if err != nil {
				t.Fatalf("DryRun: %v", err)
			}
			plan, err := imports.LoadImportPlan(ctx, preview.PlanID)
			if err != nil || len(plan.Remaps) != 1 || plan.Remaps[0].BirthAllocationID == "" {
				t.Fatalf("persisted plan = %+v, %v", plan, err)
			}
			allocation, err := birthRepository.GetAllocation(ctx, actor, plan.Remaps[0].BirthAllocationID)
			if err != nil || allocation.Domain != recordidentity.Certifiable {
				t.Fatalf("certifiable allocation = %+v, %v", allocation, err)
			}
			applyRequest := portability.ApplyRequest{Actor: actor, PlanID: preview.PlanID, LockVersion: preview.LockVersion}
			first, err := service.Apply(ctx, applyRequest)
			if scenario.loseCommit {
				if err == nil || authority.confirms != 0 {
					t.Fatalf("lost APP commit: result=%+v error=%v confirms=%d", first, err, authority.confirms)
				}
			} else if err != nil || !first.CertificationPending || !reflect.DeepEqual(first.RecordIDs, []string{allocation.RecordID}) {
				t.Fatalf("uncertain initial confirmation: result=%+v error=%v", first, err)
			}
			committed, err := birthRepository.CommittedCreation(ctx, allocation.RecordID)
			if err != nil || committed.State != recordbirth.StateFinished {
				t.Fatalf("committed pending birth = %+v, %v", committed, err)
			}
			if _, err := birth.ReadCertification(ctx, allocation.RecordID); err == nil {
				t.Fatal("unconfirmed APP birth was certified")
			}
			beforeWrites, beforeReads, beforeReserves, beforeCommits := blob.writes, blob.reads, authority.reserves, commits.calls
			beforeConfirms := authority.confirms
			allocations.unavailable = true
			unknownApply, err := service.Apply(ctx, applyRequest)
			if err != nil || !unknownApply.CertificationPending || !reflect.DeepEqual(unknownApply.RecordIDs, []string{allocation.RecordID}) {
				t.Fatalf("unknown allocation Apply = %+v, %v", unknownApply, err)
			}
			unknownDryRun, err := service.DryRun(ctx, request)
			if err != nil || !unknownDryRun.CertificationPending || unknownDryRun.PlanID != preview.PlanID {
				t.Fatalf("unknown allocation DryRun = %+v, %v", unknownDryRun, err)
			}
			allocations.unavailable = false
			if authority.confirms != beforeConfirms {
				t.Fatal("unknown allocation attempted confirmation")
			}
			revoked := actor.Clone()
			revoked.Role = recordauth.RoleViewer
			if _, err := service.Apply(ctx, portability.ApplyRequest{Actor: revoked, PlanID: preview.PlanID, LockVersion: preview.LockVersion}); !errors.Is(err, portability.ErrExportUnauthorized) {
				t.Fatalf("revoked Apply = %v", err)
			}
			revokedRequest := request
			revokedRequest.Actor = revoked
			if _, err := service.DryRun(ctx, revokedRequest); !errors.Is(err, portability.ErrExportUnauthorized) {
				t.Fatalf("revoked DryRun = %v", err)
			}
			handler := handlers.RecordPortability(service)
			revokedHTTP := httptest.NewRequest(http.MethodPost, "/api/record-imports/"+preview.PlanID+"/apply", bytes.NewReader([]byte(fmt.Sprintf(`{"lock_version":%d}`, preview.LockVersion))))
			revokedHTTP.Header.Set("Content-Type", "application/json")
			revokedHTTP = revokedHTTP.WithContext(sessionctx.WithActorScope(ctx, revoked))
			deniedResponse := httptest.NewRecorder()
			handler.ServeHTTP(deniedResponse, revokedHTTP)
			assertImportDestinationHTTPError(t, deniedResponse, http.StatusNotFound, "resource_not_found")
			revokedDryHTTP := httptest.NewRequest(http.MethodPost, "/api/record-imports/dry-run?destination_subject_kind=vps&destination_subject_id="+fixture.Destination, bytes.NewReader(archive))
			revokedDryHTTP.Header.Set("Content-Type", "application/zip")
			revokedDryHTTP.Header.Set("Idempotency-Key", request.IdempotencyKey)
			revokedDryHTTP = revokedDryHTTP.WithContext(sessionctx.WithActorScope(ctx, revoked))
			deniedDryResponse := httptest.NewRecorder()
			handler.ServeHTTP(deniedDryResponse, revokedDryHTTP)
			assertImportDestinationHTTPError(t, deniedDryResponse, http.StatusNotFound, "resource_not_found")
			if authority.confirms != beforeConfirms {
				t.Fatal("revoked actor attempted confirmation")
			}

			// A current group change must not replace the request's original scope.
			if _, err := fixture.AppOwner.Exec(ctx, `insert into public.record_access_group_members(group_id,user_id) values('rag_importreplay',$1)`, actor.UserID); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := fixture.AppOwner.Exec(context.Background(), `update public.users set disabled_at=null where user_id=$1`, actor.UserID); err != nil {
					t.Errorf("restore fixture actor: %v", err)
				}
				if _, err := fixture.AppOwner.Exec(context.Background(), `delete from public.record_access_group_members where group_id='rag_importreplay' and user_id=$1`, actor.UserID); err != nil {
					t.Errorf("remove fixture membership: %v", err)
				}
			})
			changed := actor.Clone()
			changed.GroupIDs = []string{"rag_importreplay"}
			changed, err = recordauth.NormalizeActorScope(changed)
			if err != nil || changed.CanonicalHash() == allocation.Request.ActorScopeDigest {
				t.Fatalf("changed actor scope: %+v %v", changed, err)
			}
			applyRequest.Actor, request.Actor = changed, changed

			// Membership rows use the protected lock command. A changed, still
			// authorized scope must replay without replacing the original binding.
			allocations.unavailable = true
			groupApply, err := service.Apply(ctx, applyRequest)
			if err != nil || !groupApply.CertificationPending || !reflect.DeepEqual(groupApply.RecordIDs, []string{allocation.RecordID}) {
				t.Fatalf("authorized grouped Apply: result=%+v error=%v", groupApply, err)
			}
			groupDryRun, err := service.DryRun(ctx, request)
			if err != nil || !groupDryRun.CertificationPending || groupDryRun.PlanID != preview.PlanID {
				t.Fatalf("authorized grouped DryRun: result=%+v error=%v", groupDryRun, err)
			}
			allocations.unavailable = false
			if authority.confirms != beforeConfirms {
				t.Fatal("unknown grouped allocation attempted confirmation")
			}

			// Change actual APP user or group membership while each route waits
			// after allocation lookup. A stale request scope must fail closed.
			type authorizationOutcome struct {
				err       error
				disclosed bool
			}
			// The actor row is changed in a real PG transaction. Observe the
			// replay waiting on that row before allowing the change to commit.
			blocker, err := fixture.AppOwner.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer blocker.Rollback(context.Background())
			if _, err := blocker.Exec(ctx, `update public.users set disabled_at=clock_timestamp() where user_id=$1`, actor.UserID); err != nil {
				t.Fatal(err)
			}
			var blockerPID int32
			if err := blocker.QueryRow(ctx, `select pg_backend_pid()`).Scan(&blockerPID); err != nil {
				t.Fatal(err)
			}
			waitingResult := make(chan authorizationOutcome, 1)
			go func() {
				value, err := service.Apply(ctx, applyRequest)
				waitingResult <- authorizationOutcome{err: err, disclosed: value.PlanID != "" || len(value.RecordIDs) != 0}
			}()
			waitImportBirthReplayBlocked(t, ctx, fixture, blockerPID)
			if err := blocker.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			var afterRowWait authorizationOutcome
			select {
			case afterRowWait = <-waitingResult:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if !errors.Is(afterRowWait.err, portability.ErrExportUnauthorized) || afterRowWait.disclosed || authority.confirms != beforeConfirms {
				t.Fatalf("replay after membership row wait: error=%v, disclosed=%t, confirms=%d/%d", afterRowWait.err, afterRowWait.disclosed, authority.confirms, beforeConfirms)
			}
			if _, err := fixture.AppOwner.Exec(ctx, `update public.users set disabled_at=null where user_id=$1`, actor.UserID); err != nil {
				t.Fatal(err)
			}

			for _, attempt := range []struct{ operation, revoked string }{
				{operation: "Apply", revoked: "user"}, {operation: "DryRun", revoked: "user"},
				{operation: "Apply", revoked: "group"}, {operation: "DryRun", revoked: "group"},
			} {
				allocations.reached, allocations.resume = make(chan struct{}), make(chan struct{})
				result := make(chan authorizationOutcome, 1)
				go func(operation string) {
					if operation == "Apply" {
						value, err := service.Apply(ctx, applyRequest)
						result <- authorizationOutcome{err: err, disclosed: value.PlanID != "" || len(value.RecordIDs) != 0}
						return
					}
					value, err := service.DryRun(ctx, request)
					result <- authorizationOutcome{err: err, disclosed: value.PlanID != ""}
				}(attempt.operation)
				select {
				case <-allocations.reached:
				case early := <-result:
					t.Fatalf("%s/%s did not reach allocation wait: error=%v, disclosed=%t", attempt.operation, attempt.revoked, early.err, early.disclosed)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				revokeSQL := `update public.users set disabled_at=clock_timestamp() where user_id=$1`
				if attempt.revoked == "group" {
					revokeSQL = `delete from public.record_access_group_members where group_id='rag_importreplay' and user_id=$1`
				}
				if _, err := fixture.AppOwner.Exec(ctx, revokeSQL, actor.UserID); err != nil {
					close(allocations.resume)
					t.Fatal(err)
				}
				close(allocations.resume)
				var blocked authorizationOutcome
				select {
				case blocked = <-result:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				allocations.reached, allocations.resume = nil, nil
				if !errors.Is(blocked.err, portability.ErrExportUnauthorized) || blocked.disclosed {
					t.Fatalf("%s/%s replay after revocation: error=%v, disclosed=%t", attempt.operation, attempt.revoked, blocked.err, blocked.disclosed)
				}
				if authority.confirms != beforeConfirms {
					t.Fatalf("%s/%s confirmed after revocation", attempt.operation, attempt.revoked)
				}
				restoreSQL := `update public.users set disabled_at=null where user_id=$1`
				if attempt.revoked == "group" {
					restoreSQL = `insert into public.record_access_group_members(group_id,user_id) values('rag_importreplay',$1)`
				}
				if _, err := fixture.AppOwner.Exec(ctx, restoreSQL, actor.UserID); err != nil {
					t.Fatal(err)
				}
			}

			// A terminal W2 fact may complete while permission is revoked, but
			// the applied result must be reauthorized before it is disclosed.
			authority.outcomes = append([]string{"lost"}, authority.outcomes...)
			authority.reached, authority.resume = make(chan struct{}), make(chan struct{})
			type replayOutcome struct {
				result portability.ApplyResult
				err    error
			}
			outcome := make(chan replayOutcome, 1)
			go func() {
				result, err := service.Apply(ctx, applyRequest)
				outcome <- replayOutcome{result: result, err: err}
			}()
			select {
			case <-authority.reached:
			case early := <-outcome:
				t.Fatalf("confirmation did not reach W2: %+v, %v", early.result, early.err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if _, err := fixture.AppOwner.Exec(ctx, `update public.users set disabled_at=clock_timestamp() where user_id=$1`, actor.UserID); err != nil {
				close(authority.resume)
				t.Fatal(err)
			}
			close(authority.resume)
			var revokedDuringConfirm replayOutcome
			select {
			case revokedDuringConfirm = <-outcome:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			authority.reached, authority.resume = nil, nil
			if !errors.Is(revokedDuringConfirm.err, portability.ErrExportUnauthorized) || len(revokedDuringConfirm.result.RecordIDs) != 0 {
				t.Fatalf("revoked result disclosed after W2 wait: %+v, %v", revokedDuringConfirm.result, revokedDuringConfirm.err)
			}
			var terminalEvents int
			if err := fixture.Witness.QueryRow(ctx, `select count(*) from public.record_birth_events where reservation_id=$1 and kind='created'`, allocation.ReservationID).Scan(&terminalEvents); err != nil || terminalEvents != 1 {
				t.Fatalf("terminal W2 fact after revocation: events=%d error=%v", terminalEvents, err)
			}
			if _, err := service.DryRun(ctx, request); !errors.Is(err, portability.ErrExportUnauthorized) {
				t.Fatalf("disabled actor DryRun = %v", err)
			}
			if _, err := fixture.AppOwner.Exec(ctx, `update public.users set disabled_at=null where user_id=$1`, actor.UserID); err != nil {
				t.Fatal(err)
			}
			if authority.confirms != beforeConfirms+1 {
				t.Fatalf("revoked confirmation attempts=%d, want %d", authority.confirms, beforeConfirms+1)
			}

			applyPath := "/api/record-imports/" + preview.PlanID + "/apply"
			applyBody := []byte(fmt.Sprintf(`{"lock_version":%d}`, preview.LockVersion))
			dryRunPath := "/api/record-imports/dry-run?destination_subject_kind=vps&destination_subject_id=" + fixture.Destination
			if scenario.firstIsDryRun {
				var retry portability.ImportPlanView
				importBirthReplayHTTP(t, ctx, service, dryRunPath, changed, archive, "application/zip", request.IdempotencyKey, true, &retry)
				if retry.PlanID != preview.PlanID || retry.JobState != "applied" || !retry.CertificationPending {
					t.Fatalf("pending DryRun replay = %+v", retry)
				}
				var applied portability.ApplyResult
				importBirthReplayHTTP(t, ctx, service, applyPath, changed, applyBody, "application/json", "", false, &applied)
				if applied.CertificationPending || !reflect.DeepEqual(applied.RecordIDs, []string{allocation.RecordID}) {
					t.Fatalf("confirmed Apply replay = %+v", applied)
				}
			} else {
				var applied portability.ApplyResult
				importBirthReplayHTTP(t, ctx, service, applyPath, changed, applyBody, "application/json", "", true, &applied)
				if !applied.CertificationPending || !reflect.DeepEqual(applied.RecordIDs, []string{allocation.RecordID}) {
					t.Fatalf("pending Apply replay = %+v", applied)
				}
				var retry portability.ImportPlanView
				importBirthReplayHTTP(t, ctx, service, dryRunPath, changed, archive, "application/zip", request.IdempotencyKey, false, &retry)
				if retry.PlanID != preview.PlanID || retry.JobState != "applied" || retry.CertificationPending {
					t.Fatalf("confirmed DryRun replay = %+v", retry)
				}
			}
			confirmed, err := birthRepository.GetAllocation(ctx, changed, allocation.AllocationID)
			if err != nil || confirmed.State != recordbirth.StateConfirmed || confirmed.Request != allocation.Request {
				t.Fatalf("confirmed original allocation = %+v, %v", confirmed, err)
			}
			if _, err := birth.ReadCertification(ctx, allocation.RecordID); err != nil {
				t.Fatalf("confirmed birth verification: %v", err)
			}
			confirmedCalls := authority.confirms
			for range 2 {
				applied, err := service.Apply(ctx, applyRequest)
				if err != nil || applied.CertificationPending || !reflect.DeepEqual(applied.RecordIDs, []string{allocation.RecordID}) {
					t.Fatalf("repeated Apply replay = %+v, %v", applied, err)
				}
				retry, err := service.DryRun(ctx, request)
				if err != nil || retry.CertificationPending || retry.PlanID != preview.PlanID {
					t.Fatalf("repeated DryRun replay = %+v, %v", retry, err)
				}
			}
			if authority.confirms != confirmedCalls {
				t.Fatal("confirmed birth was sent to W2 again")
			}
			if blob.writes != beforeWrites || blob.reads != beforeReads || authority.reserves != beforeReserves || commits.calls != beforeCommits {
				t.Fatalf("replay repeated work: blob puts=%d opens=%d reserves=%d commits=%d", blob.writes-beforeWrites, blob.reads-beforeReads, authority.reserves-beforeReserves, commits.calls-beforeCommits)
			}
			var roots, revisionsCount, createdEvents int
			if err := fixture.AppOwner.QueryRow(ctx, `select (select count(*) from public.records where record_id=$1),
				(select count(*) from public.record_revisions where record_id=$1)`, allocation.RecordID).Scan(&roots, &revisionsCount); err != nil {
				t.Fatal(err)
			}
			if err := fixture.Witness.QueryRow(ctx, `select count(*) from public.record_birth_events where reservation_id=$1 and kind='created'`, allocation.ReservationID).Scan(&createdEvents); err != nil {
				t.Fatal(err)
			}
			if roots != 1 || revisionsCount != 1 || createdEvents != 1 {
				t.Fatalf("replay duplicated durable effects: roots=%d revisions=%d W2 created=%d", roots, revisionsCount, createdEvents)
			}
		})
	}
}
