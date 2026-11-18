//go:build linux

package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"houfeng/internal/center/attachments"
)

type localProtocolProcess struct {
	command         *exec.Cmd
	events, control *os.File
	output          bytes.Buffer
	once            sync.Once
	waitError       error
}

func startLocalProtocolProcess(t *testing.T, binary string, environment []string) *localProtocolProcess {
	t.Helper()
	eventRead, eventWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	controlRead, controlWrite, err := os.Pipe()
	if err != nil {
		eventRead.Close()
		eventWrite.Close()
		t.Fatal(err)
	}
	process := &localProtocolProcess{command: exec.Command(binary, "-test.run=^TestLocalBlobProcessHelper$", "-test.count=1"), events: eventRead, control: controlWrite}
	process.command.Env = append(os.Environ(), environment...)
	process.command.ExtraFiles = []*os.File{eventWrite, controlRead}
	process.command.Stdout = &process.output
	process.command.Stderr = &process.output
	if err := process.command.Start(); err != nil {
		eventRead.Close()
		eventWrite.Close()
		controlRead.Close()
		controlWrite.Close()
		t.Fatal(err)
	}
	eventWrite.Close()
	controlRead.Close()
	t.Cleanup(func() { process.wait(true) })
	return process
}
func (process *localProtocolProcess) wait(kill bool) error {
	process.once.Do(func() {
		if kill {
			_ = process.command.Process.Kill()
		}
		process.waitError = process.command.Wait()
		process.events.Close()
		process.control.Close()
	})
	return process.waitError
}
func (process *localProtocolProcess) await(t *testing.T, event byte) {
	t.Helper()
	if err := process.events.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var observed [1]byte
	if _, err := io.ReadFull(process.events, observed[:]); err != nil || observed[0] != event {
		process.wait(true)
		t.Fatalf("process barrier got=%q want=%q err=%v output=%s", observed, event, err, process.output.String())
	}
}
func (process *localProtocolProcess) release(t *testing.T) {
	t.Helper()
	if _, err := process.control.Write([]byte{'R'}); err != nil {
		t.Fatal(err)
	}
	if err := process.wait(false); err != nil {
		t.Fatalf("process release: %v\n%s", err, process.output.String())
	}
}
func (process *localProtocolProcess) kill(t *testing.T) {
	t.Helper()
	err := process.wait(true)
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ProcessState.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
		t.Fatalf("expected SIGKILL and reap: %v", err)
	}
}

type localProcessWriteFacts struct {
	obligationID, temporary, version, outcome string
	device, inode                             int64
	dispatched, created, returned             bool
}

func readLocalProcessWriteFacts(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, origin string) localProcessWriteFacts {
	t.Helper()
	var facts localProcessWriteFacts
	if err := fixture.db.QueryRow(ctx, `select obligation_id, temporary_key, coalesce(object_version,''), outcome,
		coalesce(temporary_device,-1), coalesce(temporary_inode,-1), dispatched_at is not null,
		temporary_created_at is not null, returned_at is not null
		from public.record_write_obligations where origin_id=$1`, origin).Scan(
		&facts.obligationID, &facts.temporary, &facts.version, &facts.outcome,
		&facts.device, &facts.inode, &facts.dispatched, &facts.created, &facts.returned,
	); err != nil {
		t.Fatalf("read local process write: %v", err)
	}
	return facts
}

func assertLocalProcessAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected physical absence at %s: %v", path, err)
	}
}

func assertLocalProcessInode(t *testing.T, file *os.File, namespace attachments.LocalNamespace, facts localProcessWriteFacts, initialized bool, content []byte) os.FileInfo {
	t.Helper()
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
		t.Fatal(err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&0o7777 != 0o600 || stat.Uid != namespace.OwnerUID || stat.Gid != namespace.OwnerGID || stat.Dev != namespace.Device {
		t.Fatalf("temporary physical identity = %#v", stat)
	}
	if facts.created && (stat.Dev != uint64(facts.device) || stat.Ino != uint64(facts.inode)) {
		t.Fatalf("temporary inode %d/%d differs from recorded %d/%d", stat.Dev, stat.Ino, facts.device, facts.inode)
	}
	var attribute [128]byte
	n, err := unix.Fgetxattr(int(file.Fd()), "user.houfeng.blob-materialization", attribute[:])
	value := ""
	if err == nil {
		value = string(attribute[:n])
	}
	if initialized {
		if err != nil || value != "local-v2-"+facts.obligationID {
			t.Fatalf("temporary materialization identity = %q, %v", value, err)
		}
	} else if !errors.Is(err, unix.ENODATA) {
		t.Fatalf("uninitialized temporary unexpectedly has materialization identity: %q, %v", value, err)
	}
	got, err := io.ReadAll(file)
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("temporary bytes = %q, %v; want %q", got, err, content)
	}
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func assertLocalProcessAnonymousTemporary(t *testing.T, process *localProtocolProcess, root string, namespace attachments.LocalNamespace, facts localProcessWriteFacts, initialized bool) {
	t.Helper()
	directory := fmt.Sprintf("/proc/%d/fd", process.command.Process.Pid)
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	prefix := filepath.Join(root, "sha256") + string(os.PathSeparator)
	var matches int
	for _, entry := range entries {
		path := filepath.Join(directory, entry.Name())
		target, err := os.Readlink(path)
		if err != nil || !strings.HasPrefix(target, prefix) || !strings.HasSuffix(target, " (deleted)") {
			continue
		}
		file, err := os.Open(path)
		if err != nil {
			t.Fatalf("open anonymous writer descriptor: %v", err)
		}
		var stat unix.Stat_t
		if err := unix.Fstat(int(file.Fd()), &stat); err != nil || stat.Nlink != 0 {
			file.Close()
			t.Fatalf("temporary writer descriptor is not anonymous: %#v %v", stat, err)
		}
		assertLocalProcessInode(t, file, namespace, facts, initialized, nil)
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		matches++
	}
	if matches != 1 {
		t.Fatalf("anonymous temporary descriptor count = %d, want 1", matches)
	}
}

type localProcessSettlement struct {
	obligationOutcome, obligationReceipt, settlementID, settledAt string
	taskOutcome, taskReceipt, completedAt                         string
	generation, physicalBytes, quotaVersion                       int64
	blobAbsent                                                    bool
}

func readLocalProcessSettlement(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, obligationID, deletionID string) localProcessSettlement {
	t.Helper()
	var result localProcessSettlement
	if err := fixture.db.QueryRow(ctx, `select coalesce(o.cleanup_outcome,''),coalesce(encode(o.cleanup_receipt,'hex'),''),
		o.quota_settlement_id,coalesce(o.quota_settled_at::text,''),coalesce(g.physical_delete_result,''),
		coalesce(encode(g.receipt_digest,'hex'),''),coalesce(g.completed_at::text,''),coalesce(g.owner_generation,0),
		coalesce((select q.physical_bytes from public.attachment_quota_accounts q where q.project_id='default'),0),
		coalesce((select q.quota_version from public.attachment_quota_accounts q where q.project_id='default'),-1),
		not exists(select 1 from public.blob_objects b where b.blob_key=o.canonical_key)
		from public.record_write_obligations o join public.blob_gc_deletions g on g.deletion_id=$2
		where o.obligation_id=$1 and g.deletion_state='completed'`, obligationID, deletionID).Scan(
		&result.obligationOutcome, &result.obligationReceipt, &result.settlementID, &result.settledAt,
		&result.taskOutcome, &result.taskReceipt, &result.completedAt, &result.generation,
		&result.physicalBytes, &result.quotaVersion, &result.blobAbsent,
	); err != nil {
		t.Fatalf("read local process settlement: %v", err)
	}
	return result
}

func TestPostgresIntegrationLocalBlobProcessRecovery(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	repositoryRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "attachments-process.test")
	build := exec.Command("go", "test", "-c", "-o", binary, "./internal/center/attachments")
	build.Dir = repositoryRoot
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build local process helper: %v\n%s", err, output)
	}
	root := filepath.Join(t.TempDir(), "blob-root")
	blob, err := newPostgresLocalBlobStore(t, ctx, fixture, root)
	if err != nil {
		t.Fatal(err)
	}
	var fs unix.Statfs_t
	if err := unix.Statfs(root, &fs); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("HOUFENG_RECORDS_REQUIRE_BTRFS") == "1" && fs.Type != unix.BTRFS_SUPER_MAGIC {
		t.Fatalf("BlobRoot filesystem=%x, want btrfs", fs.Type)
	}
	t.Logf("actual BlobRoot filesystem type=%x", fs.Type)
	owner := fixture.openDirectRolePool(t, ctx, fixture.migrator, "local-process-cleanup-owner", 1)
	base := []string{"HOUFENG_LOCAL_PROCESS_ROOT=" + root, "HOUFENG_LOCAL_PROCESS_DATABASE_URL=" + contentProcessorCrashDatabaseURL(fixture.directRoleConfig(t, fixture.runtime, "local-process"))}
	launch := func(mode string, extras ...string) *localProtocolProcess {
		environment := append(append([]string{}, base...), "HOUFENG_LOCAL_PROCESS_MODE="+mode)
		return startLocalProtocolProcess(t, binary, append(environment, extras...))
	}
	t.Run("two_process_same_key_wait_cancel_and_independent_key", func(t *testing.T) {
		firstKey, secondKey := "sha256/"+strings.Repeat("1", 64), "sha256/"+strings.Repeat("2", 64)
		holder := launch("hold", "HOUFENG_LOCAL_PROCESS_KEY="+firstKey)
		holder.await(t, 'H')
		for range 2 {
			waiter := launch("cancel-wait", "HOUFENG_LOCAL_PROCESS_KEY="+firstKey)
			waiter.await(t, 'W')
			waiter.release(t)
		}
		independent := launch("hold", "HOUFENG_LOCAL_PROCESS_KEY="+secondKey)
		independent.await(t, 'H')
		independent.release(t)
		holder.release(t)
		next := launch("hold", "HOUFENG_LOCAL_PROCESS_KEY="+firstKey)
		next.await(t, 'H')
		next.release(t)
	})
	stages := []struct {
		cutpoint      string
		anonymous     bool
		initialized   bool
		recorded      bool
		temporaryName bool
		contentCopied bool
		finalPresent  bool
		returned      bool
		outcome       string
	}{
		{cutpoint: "after-temp-open", anonymous: true, outcome: "already_absent"},
		{cutpoint: "after-temp-identity", anonymous: true, initialized: true, outcome: "already_absent"},
		{cutpoint: "after-temp-registration", anonymous: true, initialized: true, recorded: true, outcome: "already_absent"},
		{cutpoint: "after-temp-link", initialized: true, recorded: true, temporaryName: true, outcome: "already_absent"},
		{cutpoint: "after-temp-create", initialized: true, recorded: true, temporaryName: true, outcome: "already_absent"},
		{cutpoint: "after-copy", initialized: true, recorded: true, temporaryName: true, contentCopied: true, outcome: "already_absent"},
		{cutpoint: "after-file-sync", initialized: true, recorded: true, temporaryName: true, contentCopied: true, outcome: "already_absent"},
		{cutpoint: "before-publish", initialized: true, recorded: true, temporaryName: true, contentCopied: true, outcome: "already_absent"},
		{cutpoint: "after-publish", initialized: true, recorded: true, temporaryName: true, contentCopied: true, finalPresent: true, outcome: "deleted"},
		{cutpoint: "after-delete", initialized: true, recorded: true, contentCopied: true, finalPresent: true, returned: true, outcome: "already_absent"},
	}
	for index, stage := range stages {
		t.Run(stage.cutpoint, func(t *testing.T) {
			content := []byte("durable process crash " + stage.cutpoint)
			digest := sha256.Sum256(content)
			key := "sha256/" + hex.EncodeToString(digest[:])
			jobID := fmt.Sprintf("rij_process%d", index)
			if _, err := fixture.db.Exec(ctx, `insert into public.record_import_jobs(import_job_id,actor_id,idempotency_key,job_state,identity_classification,archive_digest,destination_subject_kind,destination_subject_source_id,expires_at)
				values($1,'usr_aaaaaaaaaaaaaaaaaaaaaaaa',$1,'uploaded','unknown',$2,'vps',$3,clock_timestamp()+interval '1 hour')`, jobID, digest[:], testStoreRecordVPSID); err != nil {
				t.Fatal(err)
			}
			writer := launch("put", "HOUFENG_LOCAL_PROCESS_JOB="+jobID, "HOUFENG_LOCAL_PROCESS_CONTENT="+string(content), "HOUFENG_LOCAL_PROCESS_CUTPOINT="+stage.cutpoint)
			if stage.returned {
				if err := writer.wait(false); err != nil {
					t.Fatalf("seed process Put: %v\n%s", err, writer.output.String())
				}
			} else {
				writer.await(t, 'B')
			}
			facts := readLocalProcessWriteFacts(t, ctx, fixture, jobID)
			if !facts.dispatched || facts.temporary != "sha256/.blob-tmp-"+facts.obligationID || facts.created != stage.recorded || facts.returned != stage.returned {
				t.Fatalf("write facts at %s = %#v", stage.cutpoint, facts)
			}
			if stage.recorded {
				if facts.device < 0 || facts.inode <= 0 || uint64(facts.device) != blob.Namespace().Device {
					t.Fatalf("invalid recorded temporary inode at %s: %#v", stage.cutpoint, facts)
				}
			} else if facts.device != -1 || facts.inode != -1 {
				t.Fatalf("temporary identity recorded too early at %s: %#v", stage.cutpoint, facts)
			}
			if stage.returned {
				if facts.outcome != "materialized" || facts.version != "local-v2-"+facts.obligationID {
					t.Fatalf("missing exact materialized return: %#v", facts)
				}
			} else if facts.outcome != "registered" || facts.version != "" {
				t.Fatalf("interrupted write fabricated a return: %#v", facts)
			}
			inspect := func(checkAnonymous bool) {
				t.Helper()
				temporaryPath := filepath.Join(root, facts.temporary)
				var temporaryInfo os.FileInfo
				if stage.temporaryName {
					file, err := os.Open(temporaryPath)
					if err != nil {
						t.Fatalf("open named temporary: %v", err)
					}
					var want []byte
					if stage.contentCopied {
						want = content
					}
					temporaryInfo = assertLocalProcessInode(t, file, blob.Namespace(), facts, true, want)
					if err := file.Close(); err != nil {
						t.Fatal(err)
					}
				} else {
					assertLocalProcessAbsent(t, temporaryPath)
				}
				if stage.anonymous && checkAnonymous {
					assertLocalProcessAnonymousTemporary(t, writer, root, blob.Namespace(), facts, stage.initialized)
				}
				finalPath := filepath.Join(root, key)
				if !stage.finalPresent {
					assertLocalProcessAbsent(t, finalPath)
					return
				}
				finalInfo, err := os.Stat(finalPath)
				if err != nil {
					t.Fatalf("missing published final: %v", err)
				}
				if temporaryInfo != nil && !os.SameFile(finalInfo, temporaryInfo) {
					t.Fatal("temporary and final names do not share the registered inode")
				}
				actual, err := os.ReadFile(finalPath)
				if err != nil || !bytes.Equal(actual, content) {
					t.Fatalf("published final bytes = %q, %v", actual, err)
				}
			}
			inspect(!stage.returned)
			if !stage.returned {
				waiter := launch("cancel-wait", "HOUFENG_LOCAL_PROCESS_KEY="+key)
				waiter.await(t, 'W')
				waiter.release(t)
				writer.kill(t)
				if afterKill := readLocalProcessWriteFacts(t, ctx, fixture, jobID); afterKill != facts {
					t.Fatalf("SIGKILL changed durable write facts: before %#v after %#v", facts, afterKill)
				}
				inspect(false)
			} else {
				if _, err := fixture.db.Exec(ctx, `insert into public.blob_objects(blob_key,sha256_digest,object_version,size_bytes,backend_kind,namespace_id) values($1,$2,$3,$4,'local',$5)`, key, digest[:], facts.version, len(content), blob.Namespace().NamespaceID); err != nil {
					t.Fatal(err)
				}
				if _, err := fixture.db.Exec(ctx, `insert into public.attachment_quota_accounts(project_id,physical_bytes) values('default',$1) on conflict(project_id) do update set physical_bytes=excluded.physical_bytes`, len(content)); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := fixture.db.Exec(ctx, `delete from public.record_import_jobs where import_job_id=$1`, jobID); err != nil {
				t.Fatal(err)
			}
			request := attachments.LocalCleanupPreparation{DeletionID: fmt.Sprintf("bgd_process%d", index), NamespaceID: blob.Namespace().NamespaceID, IncarnationID: blob.Namespace().IncarnationID, Key: key}
			body, err := json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			var physicalBefore, quotaVersionBefore int64
			if err := fixture.db.QueryRow(ctx, `select
				coalesce((select physical_bytes from public.attachment_quota_accounts where project_id='default'),0),
				coalesce((select quota_version from public.attachment_quota_accounts where project_id='default'),-1)`).Scan(&physicalBefore, &quotaVersionBefore); err != nil {
				t.Fatal(err)
			}
			wantPhysicalBefore, wantQuotaVersion := int64(0), quotaVersionBefore
			if stage.returned {
				wantPhysicalBefore = int64(len(content))
				wantQuotaVersion++
			}
			if physicalBefore != wantPhysicalBefore {
				t.Fatalf("physical quota before cleanup = %d, want %d", physicalBefore, wantPhysicalBefore)
			}
			if stage.returned {
				cleanup := launch("cleanup", "HOUFENG_LOCAL_PROCESS_CLEANUP="+string(body), "HOUFENG_LOCAL_PROCESS_OWNER=recovery_first", "HOUFENG_LOCAL_PROCESS_CUTPOINT=after-delete")
				cleanup.await(t, 'B')
				cleanup.kill(t)
				assertLocalProcessAbsent(t, filepath.Join(root, key))
				assertLocalProcessAbsent(t, filepath.Join(root, facts.temporary))
				var pending bool
				if err := fixture.db.QueryRow(ctx, `select g.deletion_state='claimed' and g.receipt_digest is null
					and o.cleanup_completed_at is null and o.quota_settled_at is null
					and (select q.physical_bytes from public.attachment_quota_accounts q where q.project_id='default')=$3
					and exists(select 1 from public.blob_objects b where b.blob_key=$4)
					from public.blob_gc_deletions g join public.record_write_obligations o on o.obligation_id=$2 where g.deletion_id=$1`, request.DeletionID, facts.obligationID, len(content), key).Scan(&pending); err != nil || !pending {
					t.Fatalf("physical deletion prematurely settled receipt or quota: %v %v", pending, err)
				}
				if _, err := owner.Exec(ctx, `update public.blob_gc_deletions set lease_expires_at=clock_timestamp()-interval '1 second' where deletion_id=$1`, request.DeletionID); err != nil {
					t.Fatal(err)
				}
			}
			var first localProcessSettlement
			for attempt := range 2 {
				recovery := launch("cleanup", "HOUFENG_LOCAL_PROCESS_CLEANUP="+string(body), fmt.Sprintf("HOUFENG_LOCAL_PROCESS_OWNER=recovery_%d", attempt))
				if err := recovery.wait(false); err != nil {
					t.Fatalf("recovery process %d: %v\n%s", attempt, err, recovery.output.String())
				}
				assertLocalProcessAbsent(t, filepath.Join(root, key))
				assertLocalProcessAbsent(t, filepath.Join(root, facts.temporary))
				assertNoLocalBlobTemporaryResidue(t, root)
				settlement := readLocalProcessSettlement(t, ctx, fixture, facts.obligationID, request.DeletionID)
				if settlement.obligationOutcome != stage.outcome || settlement.taskOutcome != stage.outcome || settlement.settlementID != facts.obligationID || settlement.settledAt == "" || settlement.completedAt == "" || len(settlement.taskReceipt) != 64 || settlement.obligationReceipt != settlement.taskReceipt || settlement.physicalBytes != 0 || settlement.quotaVersion != wantQuotaVersion || settlement.generation < 1 || (stage.returned && settlement.generation < 2) || !settlement.blobAbsent {
					t.Fatalf("incomplete durable settlement: %#v", settlement)
				}
				if attempt == 0 {
					first = settlement
				} else if settlement != first {
					t.Fatalf("replay changed receipt or quota settlement: first %#v replay %#v", first, settlement)
				}
				if after := readLocalProcessWriteFacts(t, ctx, fixture, jobID); after != facts {
					t.Fatalf("recovery changed registered write identity: before %#v after %#v", facts, after)
				}
			}
		})
	}
	t.Run("preexisting_temporary_collision_fails_closed", func(t *testing.T) {
		content := []byte("managed attempt with foreign temporary collision")
		foreignContent := []byte("foreign bytes must survive collision and cleanup")
		digest := sha256.Sum256(content)
		key := "sha256/" + hex.EncodeToString(digest[:])
		const jobID = "rij_processcollision"
		if _, err := fixture.db.Exec(ctx, `insert into public.record_import_jobs(import_job_id,actor_id,idempotency_key,job_state,identity_classification,archive_digest,destination_subject_kind,destination_subject_source_id,expires_at)
			values($1,'usr_aaaaaaaaaaaaaaaaaaaaaaaa',$1,'uploaded','unknown',$2,'vps',$3,clock_timestamp()+interval '1 hour')`, jobID, digest[:], testStoreRecordVPSID); err != nil {
			t.Fatal(err)
		}
		writer := launch("put-collision", "HOUFENG_LOCAL_PROCESS_JOB="+jobID, "HOUFENG_LOCAL_PROCESS_CONTENT="+string(content), "HOUFENG_LOCAL_PROCESS_CUTPOINT=after-temp-registration")
		writer.await(t, 'B')
		before := readLocalProcessWriteFacts(t, ctx, fixture, jobID)
		if !before.dispatched || !before.created || before.temporary != "sha256/.blob-tmp-"+before.obligationID || before.device < 0 || before.inode <= 0 || before.returned || before.version != "" || before.outcome != "registered" {
			t.Fatalf("collision registration = %#v", before)
		}
		assertLocalProcessAnonymousTemporary(t, writer, root, blob.Namespace(), before, true)
		temporaryPath := filepath.Join(root, before.temporary)
		assertLocalProcessAbsent(t, temporaryPath)
		assertLocalProcessAbsent(t, filepath.Join(root, key))
		foreign, err := os.OpenFile(temporaryPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := foreign.Write(foreignContent); err != nil {
			foreign.Close()
			t.Fatal(err)
		}
		// Matching xattr alone must not turn a foreign inode into this attempt.
		if err := unix.Fsetxattr(int(foreign.Fd()), "user.houfeng.blob-materialization", []byte("local-v2-"+before.obligationID), unix.XATTR_CREATE); err != nil {
			foreign.Close()
			t.Fatal(err)
		}
		if err := foreign.Sync(); err != nil {
			foreign.Close()
			t.Fatal(err)
		}
		var foreignStat unix.Stat_t
		if err := unix.Fstat(int(foreign.Fd()), &foreignStat); err != nil {
			foreign.Close()
			t.Fatal(err)
		}
		foreignInfo, err := foreign.Stat()
		if err != nil {
			foreign.Close()
			t.Fatal(err)
		}
		if foreignStat.Dev != uint64(before.device) || foreignStat.Ino == uint64(before.inode) {
			foreign.Close()
			t.Fatalf("collision does not have a distinct inode: %#v, write %#v", foreignStat, before)
		}
		if err := foreign.Close(); err != nil {
			t.Fatal(err)
		}
		directory, err := os.Open(filepath.Join(root, "sha256"))
		if err != nil {
			t.Fatal(err)
		}
		defer directory.Close()
		if err := directory.Sync(); err != nil {
			t.Fatal(err)
		}
		assertForeign := func() {
			t.Helper()
			file, err := os.Open(temporaryPath)
			if err != nil {
				t.Fatal(err)
			}
			unbound := before
			unbound.created = false
			observed := assertLocalProcessInode(t, file, blob.Namespace(), unbound, true, foreignContent)
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
			if !os.SameFile(observed, foreignInfo) {
				t.Fatal("foreign temporary inode was replaced")
			}
		}
		writer.release(t)
		afterReturn := readLocalProcessWriteFacts(t, ctx, fixture, jobID)
		expected := before
		expected.outcome, expected.returned = "failed", true
		if afterReturn != expected {
			t.Fatalf("collision write return = %#v, want %#v", afterReturn, expected)
		}
		assertForeign()
		assertLocalProcessAbsent(t, filepath.Join(root, key))
		if _, err := fixture.db.Exec(ctx, `delete from public.record_import_jobs where import_job_id=$1`, jobID); err != nil {
			t.Fatal(err)
		}
		request := attachments.LocalCleanupPreparation{DeletionID: "bgd_processcollision", NamespaceID: blob.Namespace().NamespaceID, IncarnationID: blob.Namespace().IncarnationID, Key: key}
		body, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		var quotaVersionBefore int64
		if err := fixture.db.QueryRow(ctx, `select coalesce((select quota_version from public.attachment_quota_accounts where project_id='default'),-1)`).Scan(&quotaVersionBefore); err != nil {
			t.Fatal(err)
		}
		cleanup := launch("cleanup-collision", "HOUFENG_LOCAL_PROCESS_CLEANUP="+string(body), "HOUFENG_LOCAL_PROCESS_OWNER=collision_first")
		if err := cleanup.wait(false); err != nil {
			t.Fatalf("foreign temporary cleanup: %v\n%s", err, cleanup.output.String())
		}
		assertForeign()
		assertLocalProcessAbsent(t, filepath.Join(root, key))
		var pending bool
		if err := fixture.db.QueryRow(ctx, `select g.deletion_state='claimed' and g.receipt_digest is null and g.final_identity_bound and not g.final_was_present
			and o.cleanup_completed_at is null and o.cleanup_receipt is null and o.quota_settled_at is null
			from public.blob_gc_deletions g join public.record_write_obligations o on o.obligation_id=$2 where g.deletion_id=$1`, request.DeletionID, before.obligationID).Scan(&pending); err != nil || !pending {
			t.Fatalf("foreign inode must leave cleanup pending: %t %v", pending, err)
		}
		// This file was created by the test, and its exact inode was just checked.
		if err := os.Remove(temporaryPath); err != nil {
			t.Fatal(err)
		}
		if err := directory.Sync(); err != nil {
			t.Fatal(err)
		}
		if _, err := owner.Exec(ctx, `update public.blob_gc_deletions set lease_expires_at=clock_timestamp()-interval '1 second' where deletion_id=$1`, request.DeletionID); err != nil {
			t.Fatal(err)
		}
		var first localProcessSettlement
		for attempt := range 2 {
			recovery := launch("cleanup", "HOUFENG_LOCAL_PROCESS_CLEANUP="+string(body), fmt.Sprintf("HOUFENG_LOCAL_PROCESS_OWNER=collision_recovery_%d", attempt))
			if err := recovery.wait(false); err != nil {
				t.Fatalf("collision recovery %d: %v\n%s", attempt, err, recovery.output.String())
			}
			assertLocalProcessAbsent(t, temporaryPath)
			assertLocalProcessAbsent(t, filepath.Join(root, key))
			assertNoLocalBlobTemporaryResidue(t, root)
			settlement := readLocalProcessSettlement(t, ctx, fixture, before.obligationID, request.DeletionID)
			if settlement.obligationOutcome != "already_absent" || settlement.taskOutcome != "already_absent" || settlement.settlementID != before.obligationID || settlement.settledAt == "" || settlement.completedAt == "" || len(settlement.taskReceipt) != 64 || settlement.obligationReceipt != settlement.taskReceipt || settlement.physicalBytes != 0 || settlement.quotaVersion != quotaVersionBefore || settlement.generation < 2 || !settlement.blobAbsent {
				t.Fatalf("collision recovery settlement = %#v", settlement)
			}
			if attempt == 0 {
				first = settlement
			} else if settlement != first {
				t.Fatalf("collision replay changed settlement: first %#v replay %#v", first, settlement)
			}
		}
	})
}
