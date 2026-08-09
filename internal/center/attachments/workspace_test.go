package attachments

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestContentProcessorWorkspacePrivateDerivedRegisterMaterializePurge(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "private-root")
	source := []byte("hello preview")
	claim := workspaceTestClaim(source, ProcessorProfileText)
	repository := newFakeProcessorWorkspaceRepository()
	repository.onRegister = func(registration ProcessorWorkspaceRegistration) {
		workspacePath := filepath.Join(root, registration.WorkspaceID)
		if _, err := os.Lstat(workspacePath); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("workspace existed before durable registration: %v", err)
		}
	}
	repository.onMaterialize = func(workspace ProcessorWorkspaceTransition) {
		workspacePath := filepath.Join(root, workspace.WorkspaceID)
		info, err := os.Stat(workspacePath)
		if err != nil {
			t.Fatalf("stat materialized workspace: %v", err)
		}
		if got := info.Mode().Perm(); got != 0o700 {
			t.Fatalf("workspace mode = %o, want 700", got)
		}
		sourceInfo, err := os.Stat(filepath.Join(workspacePath, processorWorkspaceSourceName))
		if err != nil {
			t.Fatalf("stat materialized source: %v", err)
		}
		if got := sourceInfo.Mode().Perm(); got != 0o600 {
			t.Fatalf("source mode = %o, want 600", got)
		}
	}
	preview := newPreviewProcessor(PreviewConfig{
		MaxSourceBytes: 1024, MaxOutputBytes: 1024, MaxImagePixels: 1,
		PDFInfoBinary: "/configured/pdfinfo", PDFToPPMBinary: "/configured/pdftoppm",
	}, nil)
	workspace, err := NewContentProcessorWorkspace(ContentProcessorWorkspaceConfig{
		Root: root, MaxSourceBytes: 1024, CleanupTimeout: time.Second,
	}, repository, preview)
	if err != nil {
		t.Fatalf("NewContentProcessorWorkspace() error = %v", err)
	}
	artifact, receipt, err := workspace.Process(context.Background(), ProcessorWorkspaceProcessRequest{
		Claim: claim, WorkspaceID: "cpw_private1", ExpiresAt: claim.LeaseExpiresAt,
		Source: bytes.NewReader(source),
	})
	if err != nil {
		t.Fatalf("ContentProcessorWorkspace.Process() error = %v", err)
	}
	if got := string(artifact.Bytes); got != string(source) {
		t.Fatalf("preview bytes = %q, want %q", got, source)
	}
	if receipt.WorkspaceID != "cpw_private1" || receipt.RemovedRowCount != 1 {
		t.Fatalf("purge receipt = %#v", receipt)
	}
	if repository.registerCalls != 1 || repository.materializeCalls != 1 ||
		repository.beginPurgeCalls != 1 || repository.completePurgeCalls != 1 {
		t.Fatalf("repository calls = register %d materialize %d begin purge %d complete purge %d",
			repository.registerCalls, repository.materializeCalls,
			repository.beginPurgeCalls, repository.completePurgeCalls)
	}
	absWorkspace, err := filepath.Abs(filepath.Join(root, "cpw_private1"))
	if err != nil {
		t.Fatal(err)
	}
	wantPathDigest := sha256.Sum256([]byte(filepath.Clean(absWorkspace)))
	if repository.registration.WorkspacePathDigest != wantPathDigest {
		t.Fatalf("registered path digest = %x, want %x", repository.registration.WorkspacePathDigest, wantPathDigest)
	}
	if _, err := os.Lstat(absWorkspace); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("workspace residue after success: %v", err)
	}
	rootInfo, err := os.Stat(root)
	if err != nil {
		t.Fatalf("stat root: %v", err)
	}
	if got := rootInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("root mode = %o, want 700", got)
	}
	formatted := strings.ToLower(receipt.String())
	if strings.Contains(formatted, strings.ToLower(root)) || strings.Contains(formatted, "hello preview") ||
		strings.Contains(formatted, processorWorkspaceSourceName) {
		t.Fatalf("receipt contains content-bearing data: %q", formatted)
	}
}

func TestContentProcessorWorkspaceRejectsRelativeAndBroadRoots(t *testing.T) {
	t.Parallel()

	filesystemRoot := filepath.VolumeName(os.TempDir()) + string(filepath.Separator)
	for _, tt := range []struct {
		name string
		root string
	}{
		{name: "relative", root: "relative-workspace"},
		{name: "current directory", root: "."},
		{name: "filesystem root", root: filesystemRoot},
		{name: "temporary directory", root: os.TempDir()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewContentProcessorWorkspace(ContentProcessorWorkspaceConfig{
				Root: tt.root, MaxSourceBytes: 1024, CleanupTimeout: time.Second,
			}, newFakeProcessorWorkspaceRepository(), workspaceTestPreviewProcessor()); !errors.Is(err, ErrInvalidProcessorCommand) {
				t.Fatalf("NewContentProcessorWorkspace(%q) error = %v, want ErrInvalidProcessorCommand", tt.root, err)
			}
		})
	}
}

func TestContentProcessorWorkspaceRejectsNonDedicatedRootWithoutChmod(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "dedicated-root")
	if err := os.Mkdir(root, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "unrelated-entry"), []byte("must remain"), 0o600); err != nil {
		t.Fatal(err)
	}
	workspace, err := NewContentProcessorWorkspace(ContentProcessorWorkspaceConfig{
		Root: root, MaxSourceBytes: 1024, CleanupTimeout: time.Second,
	}, newFakeProcessorWorkspaceRepository(), workspaceTestPreviewProcessor())
	if err != nil {
		t.Fatalf("NewContentProcessorWorkspace() error = %v, want deferred content validation", err)
	}
	claim := workspaceTestClaim([]byte("source"), ProcessorProfileText)
	_, _, err = workspace.Process(context.Background(), ProcessorWorkspaceProcessRequest{
		Claim: claim, WorkspaceID: "cpw_broadroot1", ExpiresAt: claim.LeaseExpiresAt,
		Source: bytes.NewReader([]byte("source")),
	})
	if !errors.Is(err, ErrUnsafeProcessorWorkspace) {
		t.Fatalf("Process(non-dedicated root) error = %v, want ErrUnsafeProcessorWorkspace", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o750 {
		t.Fatalf("non-dedicated root mode = %o, want unchanged 750", got)
	}
	if _, err := os.Stat(filepath.Join(root, "unrelated-entry")); err != nil {
		t.Fatalf("unrelated root entry changed or disappeared: %v", err)
	}
}

func TestContentProcessorWorkspacePurgeUsesHeldRootAfterRootReplacement(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("descriptor-anchored workspace helpers are Linux-specific")
	}
	t.Parallel()

	base := t.TempDir()
	root := filepath.Join(base, "private-root")
	originalRoot := filepath.Join(base, "private-root-original")
	repository := newFakeProcessorWorkspaceRepository()
	repository.onMaterialize = func(ProcessorWorkspaceTransition) {
		if err := os.Rename(root, originalRoot); err != nil {
			t.Fatalf("replace held root: rename original: %v", err)
		}
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatalf("replace held root: create replacement: %v", err)
		}
	}
	workspace, err := NewContentProcessorWorkspace(ContentProcessorWorkspaceConfig{
		Root: root, MaxSourceBytes: 1024, CleanupTimeout: time.Second,
	}, repository, workspaceTestPreviewProcessor())
	if err != nil {
		t.Fatal(err)
	}
	source := []byte("held root source")
	claim := workspaceTestClaim(source, ProcessorProfileText)
	if _, _, err := workspace.Process(context.Background(), ProcessorWorkspaceProcessRequest{
		Claim: claim, WorkspaceID: "cpw_heldroot1", ExpiresAt: claim.LeaseExpiresAt,
		Source: bytes.NewReader(source),
	}); err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(originalRoot, "cpw_heldroot1")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("original root workspace residue after purge: %v", err)
	}
}

func TestContentProcessorWorkspacePurgesMismatchedRegistrationReadback(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "private-root")
	source := []byte("registered source")
	claim := workspaceTestClaim(source, ProcessorProfileText)
	repository := newFakeProcessorWorkspaceRepository()
	mismatched := ProcessorWorkspace{
		WorkspaceID: "cpw_readback1", ProcessorJobID: "apj_mismatched1", Attempt: claim.Attempt,
		State:               ProcessorWorkspaceStateRegistered,
		WorkspacePathDigest: sha256.Sum256([]byte("mismatched path")), ExpiresAt: claim.LeaseExpiresAt,
	}
	repository.registerReadback = &mismatched
	workspace, err := NewContentProcessorWorkspace(ContentProcessorWorkspaceConfig{
		Root: root, MaxSourceBytes: 1024, CleanupTimeout: time.Second,
	}, repository, workspaceTestPreviewProcessor())
	if err != nil {
		t.Fatalf("NewContentProcessorWorkspace() error = %v", err)
	}
	_, receipt, err := workspace.Process(context.Background(), ProcessorWorkspaceProcessRequest{
		Claim: claim, WorkspaceID: "cpw_readback1", ExpiresAt: claim.LeaseExpiresAt,
		Source: bytes.NewReader(source),
	})
	if !errors.Is(err, ErrAttachmentConflict) {
		t.Fatalf("Process(mismatched registration readback) error = %v, want ErrAttachmentConflict", err)
	}
	if receipt.WorkspaceID != "cpw_readback1" || repository.completePurgeCalls != 1 {
		t.Fatalf("mismatched registration cleanup receipt = %#v writes=%d", receipt, repository.completePurgeCalls)
	}
	if repository.registerCalls != 1 || repository.materializeCalls != 0 || repository.beginPurgeCalls != 1 {
		t.Fatalf("repository calls = register %d materialize %d begin purge %d",
			repository.registerCalls, repository.materializeCalls, repository.beginPurgeCalls)
	}
	if _, statErr := os.Lstat(filepath.Join(root, "cpw_readback1")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("workspace residue after mismatched registration readback: %v", statErr)
	}
	formatted := strings.ToLower(receipt.String())
	if strings.Contains(formatted, strings.ToLower(root)) || strings.Contains(formatted, "registered source") ||
		strings.Contains(formatted, processorWorkspaceSourceName) {
		t.Fatalf("receipt contains content-bearing data: %q", formatted)
	}
}

func TestContentProcessorWorkspaceRejectsSymlinkAndPathEscape(t *testing.T) {
	t.Parallel()

	t.Run("symlink root", func(t *testing.T) {
		target := t.TempDir()
		root := filepath.Join(t.TempDir(), "workspace-root")
		if err := os.Symlink(target, root); err != nil {
			t.Fatal(err)
		}
		repository := newFakeProcessorWorkspaceRepository()
		preview := workspaceTestPreviewProcessor()
		workspace, err := NewContentProcessorWorkspace(ContentProcessorWorkspaceConfig{
			Root: root, MaxSourceBytes: 1024, CleanupTimeout: time.Second,
		}, repository, preview)
		if err != nil {
			t.Fatalf("NewContentProcessorWorkspace() error = %v", err)
		}
		claim := workspaceTestClaim([]byte("source"), ProcessorProfileText)
		_, _, err = workspace.Process(context.Background(), ProcessorWorkspaceProcessRequest{
			Claim: claim, WorkspaceID: "cpw_symlinkroot", ExpiresAt: claim.LeaseExpiresAt,
			Source: bytes.NewReader([]byte("source")),
		})
		if !errors.Is(err, ErrUnsafeProcessorWorkspace) {
			t.Fatalf("Process(symlink root) error = %v, want ErrUnsafeProcessorWorkspace", err)
		}
		if repository.registerCalls != 0 {
			t.Fatalf("symlink root registered %d workspaces", repository.registerCalls)
		}
	})

	t.Run("symlink root ancestor", func(t *testing.T) {
		target := t.TempDir()
		base := t.TempDir()
		ancestor := filepath.Join(base, "linked-ancestor")
		if err := os.Symlink(target, ancestor); err != nil {
			t.Fatal(err)
		}
		root := filepath.Join(ancestor, "workspace-root")
		repository := newFakeProcessorWorkspaceRepository()
		workspace, err := NewContentProcessorWorkspace(ContentProcessorWorkspaceConfig{
			Root: root, MaxSourceBytes: 1024, CleanupTimeout: time.Second,
		}, repository, workspaceTestPreviewProcessor())
		if err != nil {
			t.Fatalf("NewContentProcessorWorkspace() error = %v", err)
		}
		claim := workspaceTestClaim([]byte("source"), ProcessorProfileText)
		_, _, err = workspace.Process(context.Background(), ProcessorWorkspaceProcessRequest{
			Claim: claim, WorkspaceID: "cpw_symlinkancestor", ExpiresAt: claim.LeaseExpiresAt,
			Source: bytes.NewReader([]byte("source")),
		})
		if !errors.Is(err, ErrUnsafeProcessorWorkspace) {
			t.Fatalf("Process(symlink root ancestor) error = %v, want ErrUnsafeProcessorWorkspace", err)
		}
		if repository.registerCalls != 0 {
			t.Fatalf("symlink root ancestor registered %d workspaces", repository.registerCalls)
		}
		if _, err := os.Lstat(filepath.Join(target, "workspace-root")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("symlink ancestor target changed: %v", err)
		}
	})

	t.Run("symlink workspace", func(t *testing.T) {
		root := t.TempDir()
		target := t.TempDir()
		if err := os.Symlink(target, filepath.Join(root, "cpw_symlinkworkspace")); err != nil {
			t.Fatal(err)
		}
		repository := newFakeProcessorWorkspaceRepository()
		workspace, err := NewContentProcessorWorkspace(ContentProcessorWorkspaceConfig{
			Root: root, MaxSourceBytes: 1024, CleanupTimeout: time.Second,
		}, repository, workspaceTestPreviewProcessor())
		if err != nil {
			t.Fatal(err)
		}
		claim := workspaceTestClaim([]byte("source"), ProcessorProfileText)
		_, _, err = workspace.Process(context.Background(), ProcessorWorkspaceProcessRequest{
			Claim: claim, WorkspaceID: "cpw_symlinkworkspace", ExpiresAt: claim.LeaseExpiresAt,
			Source: bytes.NewReader([]byte("source")),
		})
		if !errors.Is(err, ErrUnsafeProcessorWorkspace) {
			t.Fatalf("Process(symlink workspace) error = %v, want ErrUnsafeProcessorWorkspace", err)
		}
		entries, readErr := os.ReadDir(target)
		if readErr != nil || len(entries) != 0 {
			t.Fatalf("symlink target changed: entries=%v error=%v", entries, readErr)
		}
		if repository.completePurgeCalls != 1 {
			t.Fatalf("unsafe symlink cleanup produced %d receipts, want 1", repository.completePurgeCalls)
		}
		if _, err := os.Lstat(filepath.Join(root, "cpw_symlinkworkspace")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("workspace symlink residue after cleanup: %v", err)
		}
	})

	t.Run("invalid ID escape", func(t *testing.T) {
		repository := newFakeProcessorWorkspaceRepository()
		workspace, err := NewContentProcessorWorkspace(ContentProcessorWorkspaceConfig{
			Root: t.TempDir(), MaxSourceBytes: 1024, CleanupTimeout: time.Second,
		}, repository, workspaceTestPreviewProcessor())
		if err != nil {
			t.Fatal(err)
		}
		claim := workspaceTestClaim([]byte("source"), ProcessorProfileText)
		_, _, err = workspace.Process(context.Background(), ProcessorWorkspaceProcessRequest{
			Claim: claim, WorkspaceID: "cpw_../escape", ExpiresAt: claim.LeaseExpiresAt,
			Source: bytes.NewReader([]byte("source")),
		})
		if !errors.Is(err, ErrInvalidProcessorCommand) {
			t.Fatalf("Process(path escape) error = %v, want ErrInvalidProcessorCommand", err)
		}
		if repository.registerCalls != 0 {
			t.Fatalf("path escape registered %d workspaces", repository.registerCalls)
		}
	})
}

func TestWorkspaceJanitorPurgeReceiptIsContentFreeAndIdempotent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	paths, err := deriveProcessorWorkspacePaths(root, "cpw_janitor1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(paths.workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.source, []byte("secret source"), 0o600); err != nil {
		t.Fatal(err)
	}
	repository := newFakeProcessorWorkspaceRepository()
	repository.workspace = ProcessorWorkspace{
		WorkspaceID: "cpw_janitor1", ProcessorJobID: "apj_janitor1", Attempt: 1,
		State:               ProcessorWorkspaceStateMaterialized,
		WorkspacePathDigest: sha256.Sum256([]byte(paths.workspace)), ExpiresAt: time.Now().Add(time.Hour),
	}
	janitor := newWorkspaceJanitor(root, repository, time.Second)
	identity := ProcessorWorkspaceTransition{
		WorkspaceID: "cpw_janitor1", WorkspacePathDigest: repository.workspace.WorkspacePathDigest,
		Authorization: NewProcessorWorkspaceReconciliationAuthorization(),
	}
	first, err := janitor.Purge(context.Background(), identity)
	if err != nil {
		t.Fatalf("WorkspaceJanitor.Purge() error = %v", err)
	}
	if err := os.Mkdir(paths.workspace, 0o700); err != nil {
		t.Fatalf("recreate replay residue: %v", err)
	}
	if err := os.WriteFile(paths.source, []byte("reappeared residue"), 0o600); err != nil {
		t.Fatalf("write replay residue: %v", err)
	}
	second, err := janitor.Purge(context.Background(), identity)
	if err != nil {
		t.Fatalf("WorkspaceJanitor.Purge(replay) error = %v", err)
	}
	if first != second {
		t.Fatalf("purge replay receipt = %#v, want immutable %#v", second, first)
	}
	if repository.completePurgeCalls != 1 || repository.receiptMutations != 0 {
		t.Fatalf("receipt writes = %d mutations = %d", repository.completePurgeCalls, repository.receiptMutations)
	}
	if _, err := os.Lstat(paths.workspace); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("workspace remains after purge replay: %v", err)
	}
	formatted := strings.ToLower(first.String())
	for _, forbidden := range []string{root, paths.source, processorWorkspaceSourceName, "secret source"} {
		if strings.Contains(formatted, strings.ToLower(forbidden)) {
			t.Fatalf("receipt %q contains forbidden value %q", formatted, forbidden)
		}
	}
}

func TestWorkspaceJanitorRejectsMismatchedDigestBeforeRootOpen(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "private-root")
	repository := newFakeProcessorWorkspaceRepository()
	janitor := newWorkspaceJanitor(root, repository, time.Second)
	_, err := janitor.Purge(context.Background(), ProcessorWorkspaceTransition{
		WorkspaceID:         "cpw_digestguard1",
		WorkspacePathDigest: sha256.Sum256([]byte("wrong workspace path")),
		Authorization:       NewProcessorWorkspaceReconciliationAuthorization(),
	})
	if !errors.Is(err, ErrAttachmentConflict) {
		t.Fatalf("WorkspaceJanitor.Purge(mismatched digest) error = %v, want ErrAttachmentConflict", err)
	}
	if _, statErr := os.Lstat(root); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("mismatched digest touched root: %v", statErr)
	}
	if repository.beginPurgeCalls != 0 {
		t.Fatalf("mismatched digest began purge %d times, want 0", repository.beginPurgeCalls)
	}
}

func TestWorkspaceJanitorReplaysReceiptAfterWorkspaceRecordDeletion(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	paths, err := deriveProcessorWorkspacePaths(root, "cpw_receiptonly1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(paths.workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.source, []byte("reappeared residue"), 0o600); err != nil {
		t.Fatal(err)
	}
	receipt, err := NewProcessorWorkspacePurgeReceipt(
		"cpw_receiptonly1", 2, time.Now().UTC().Truncate(time.Microsecond),
	)
	if err != nil {
		t.Fatal(err)
	}
	repository := newFakeProcessorWorkspaceRepository()
	repository.receipt = receipt
	transition := ProcessorWorkspaceTransition{
		WorkspaceID: receipt.WorkspaceID, WorkspacePathDigest: sha256.Sum256([]byte(paths.workspace)),
		Authorization: NewProcessorWorkspaceReconciliationAuthorization(),
	}

	janitor := newWorkspaceJanitor(root, repository, time.Second)
	replayed, err := janitor.Purge(context.Background(), transition)
	if err != nil {
		t.Fatalf("WorkspaceJanitor.Purge(receipt-only replay) error = %v", err)
	}
	if replayed != receipt {
		t.Fatalf("receipt-only replay = %#v, want immutable %#v", replayed, receipt)
	}
	if repository.workspace != (ProcessorWorkspace{}) || repository.receipt != receipt ||
		repository.beginPurgeCalls != 1 || repository.completePurgeCalls != 0 || repository.receiptMutations != 0 {
		t.Fatalf("receipt-only replay mutated repository: workspace=%#v receipt=%#v begin=%d complete=%d mutations=%d",
			repository.workspace, repository.receipt, repository.beginPurgeCalls,
			repository.completePurgeCalls, repository.receiptMutations)
	}
	if _, err := os.Lstat(paths.workspace); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("workspace residue remains after receipt-only replay: %v", err)
	}
}

func TestWorkspaceJanitorReturnsConcurrentImmutableReceipt(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	paths, err := deriveProcessorWorkspacePaths(root, "cpw_concurrent1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(paths.workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.source, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	repository := newFakeProcessorWorkspaceRepository()
	repository.workspace = ProcessorWorkspace{
		WorkspaceID: "cpw_concurrent1", ProcessorJobID: "apj_concurrent1", Attempt: 1,
		State:               ProcessorWorkspaceStateMaterialized,
		WorkspacePathDigest: sha256.Sum256([]byte(paths.workspace)), ExpiresAt: time.Now().Add(time.Hour),
	}
	repository.completeConflictAfterPersist = true
	janitor := newWorkspaceJanitor(root, repository, time.Second)
	receipt, err := janitor.Purge(context.Background(), ProcessorWorkspaceTransition{
		WorkspaceID:         repository.workspace.WorkspaceID,
		WorkspacePathDigest: repository.workspace.WorkspacePathDigest,
		Authorization:       NewProcessorWorkspaceReconciliationAuthorization(),
	})
	if err != nil {
		t.Fatalf("WorkspaceJanitor.Purge(concurrent receipt) error = %v", err)
	}
	if receipt != repository.receipt || repository.beginPurgeCalls != 2 || repository.completePurgeCalls != 1 {
		t.Fatalf("concurrent purge receipt = %#v stored=%#v begin=%d complete=%d",
			receipt, repository.receipt, repository.beginPurgeCalls, repository.completePurgeCalls)
	}
}

func TestContentProcessorWorkspaceCleansUpAfterErrorCancellationAndTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		context func(*fakeProcessorWorkspaceRepository) (context.Context, context.CancelFunc)
		source  []byte
		wantErr error
	}{
		{name: "processing error", context: func(*fakeProcessorWorkspaceRepository) (context.Context, context.CancelFunc) {
			return context.Background(), func() {}
		}, source: []byte{'x', 0xff}, wantErr: ErrInvalidPreviewContent},
		{name: "cancellation after materialize", context: func(repository *fakeProcessorWorkspaceRepository) (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(context.Background())
			repository.onMaterialize = func(ProcessorWorkspaceTransition) { cancel() }
			return ctx, cancel
		}, source: []byte("source"), wantErr: context.Canceled},
		{name: "deadline timeout after materialize", context: func(repository *fakeProcessorWorkspaceRepository) (context.Context, context.CancelFunc) {
			ctx := &controlledWorkspaceContext{Context: context.Background()}
			repository.onMaterialize = func(ProcessorWorkspaceTransition) { ctx.err = context.DeadlineExceeded }
			return ctx, func() {}
		}, source: []byte("source"), wantErr: context.DeadlineExceeded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			repository := newFakeProcessorWorkspaceRepository()
			workspace, err := NewContentProcessorWorkspace(ContentProcessorWorkspaceConfig{
				Root: root, MaxSourceBytes: 1024, CleanupTimeout: time.Second,
			}, repository, workspaceTestPreviewProcessor())
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := tt.context(repository)
			defer cancel()
			claim := workspaceTestClaim(tt.source, ProcessorProfileText)
			_, receipt, processErr := workspace.Process(ctx, ProcessorWorkspaceProcessRequest{
				Claim: claim, WorkspaceID: "cpw_cleanup1", ExpiresAt: claim.LeaseExpiresAt,
				Source: bytes.NewReader(tt.source),
			})
			if processErr == nil {
				t.Fatal("Process() error = nil, want processing/cancellation error")
			}
			if !errors.Is(processErr, tt.wantErr) {
				t.Fatalf("Process() error = %v, want %v", processErr, tt.wantErr)
			}
			if receipt.WorkspaceID != "cpw_cleanup1" || repository.completePurgeCalls != 1 {
				t.Fatalf("cleanup receipt = %#v writes = %d", receipt, repository.completePurgeCalls)
			}
			if repository.registerCalls != 1 || repository.materializeCalls != 1 {
				t.Fatalf("cleanup path reached register/materialize = %d/%d, want 1/1",
					repository.registerCalls, repository.materializeCalls)
			}
			if _, err := os.Lstat(filepath.Join(root, "cpw_cleanup1")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("workspace residue after %s: %v", tt.name, err)
			}
		})
	}
}

func TestContentProcessorWorkspaceBoundsDescendantPipeOnCancellationAndExpiry(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("descriptor-anchored process-group cleanup is Linux-specific")
	}
	tests := []struct {
		name             string
		requestLifetime  time.Duration
		cancelAfterReady bool
		wantErr          error
	}{
		{
			name: "caller cancellation", requestLifetime: time.Minute,
			cancelAfterReady: true, wantErr: context.Canceled,
		},
		{
			name: "request expiry", requestLifetime: 750 * time.Millisecond,
			wantErr: context.DeadlineExceeded,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "private-root")
			syncRoot := t.TempDir()
			childPIDPath := filepath.Join(syncRoot, "child.pid")
			childReadyPath := filepath.Join(syncRoot, "child.ready")
			childScriptPath := filepath.Join(syncRoot, "pipe-holder")
			markerDigest := sha256.Sum256([]byte(syncRoot))
			marker := "houfeng-descendant-pipe-" + hex.EncodeToString(markerDigest[:8])
			relativeCacheMarker := filepath.Join("..", "..", "var", "cache", "fontconfig", marker)
			packageCWD, err := os.Getwd()
			if err != nil {
				t.Fatalf("get package working directory: %v", err)
			}
			sharedCacheMarker := filepath.Clean(filepath.Join(packageCWD, relativeCacheMarker))
			if _, err := os.Lstat(sharedCacheMarker); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("shared cache marker precondition = %v, want absent %s", err, sharedCacheMarker)
			}
			t.Cleanup(func() {
				if err := os.RemoveAll(sharedCacheMarker); err != nil {
					t.Errorf("remove test-owned shared cache marker: %v", err)
				}
				for path := filepath.Dir(sharedCacheMarker); path != filepath.Clean(filepath.Join(packageCWD, "..", "..")); path = filepath.Dir(path) {
					_ = os.Remove(path)
				}
			})
			childScript := "#!/bin/sh\nset -eu\n/usr/bin/printf '%s\\n' \"$$\" > " + strconv.Quote(childPIDPath) +
				"\n/usr/bin/touch " + strconv.Quote(childReadyPath) +
				"\nexec /bin/sleep 60\n"
			if err := os.WriteFile(childScriptPath, []byte(childScript), 0o700); err != nil {
				t.Fatal(err)
			}
			pngPath := filepath.Join(syncRoot, "preview.png")
			if err := os.WriteFile(pngPath, tinyPNG(t), 0o600); err != nil {
				t.Fatal(err)
			}
			pdfInfoPath := filepath.Join(syncRoot, "pdfinfo")
			if err := os.WriteFile(pdfInfoPath, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			pdfToPPMPath := filepath.Join(syncRoot, "pdftoppm")
			pdfToPPMScript := "#!/bin/sh\nset -eu\n/usr/bin/mkdir -p " + relativeCacheMarker + "\n" +
				"/usr/bin/printf residue > " + filepath.Join(relativeCacheMarker, "cache") + "\n" +
				strconv.Quote(childScriptPath) + " &\n/usr/bin/cat " + strconv.Quote(pngPath) + "\n"
			if err := os.WriteFile(pdfToPPMPath, []byte(pdfToPPMScript), 0o700); err != nil {
				t.Fatal(err)
			}

			source := []byte("%PDF-1.7\nfixture")
			claim := workspaceTestClaim(source, ProcessorProfilePDF)
			requestExpiry := time.Now().UTC().Add(test.requestLifetime)
			claim.LeaseExpiresAt = requestExpiry.Add(time.Second)
			if claim.ExpiresAt.Before(claim.LeaseExpiresAt) {
				claim.ExpiresAt = claim.LeaseExpiresAt.Add(time.Minute)
			}
			repository := newFakeProcessorWorkspaceRepository()
			preview := newPreviewProcessor(PreviewConfig{
				MaxSourceBytes: 1024, MaxOutputBytes: 1024, MaxImagePixels: 16,
				PDFInfoBinary: pdfInfoPath, PDFToPPMBinary: pdfToPPMPath,
			}, nil)
			workspace, err := NewContentProcessorWorkspace(ContentProcessorWorkspaceConfig{
				Root: root, MaxSourceBytes: 1024, CleanupTimeout: 3 * time.Second,
			}, repository, preview)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			type processResult struct {
				receipt ProcessorWorkspacePurgeReceipt
				err     error
			}
			resultChannel := make(chan processResult, 1)
			go func() {
				_, receipt, processErr := workspace.Process(ctx, ProcessorWorkspaceProcessRequest{
					Claim: claim, WorkspaceID: "cpw_descendantpipe1", ExpiresAt: requestExpiry,
					Source: bytes.NewReader(source),
				})
				resultChannel <- processResult{receipt: receipt, err: processErr}
			}()

			waitForWorkspaceTestFile(t, childReadyPath, 5*time.Second)
			childPID := readWorkspaceTestPID(t, childPIDPath)
			if test.cancelAfterReady {
				cancel()
			}
			var result processResult
			externallyKilled := false
			select {
			case result = <-resultChannel:
			case <-time.After(3 * time.Second):
				externallyKilled = true
				_ = workspaceTestKillProcess(childPID)
				select {
				case result = <-resultChannel:
				case <-time.After(5 * time.Second):
					t.Fatal("Process remained blocked after test killed the pipe-holding descendant")
				}
			}
			if externallyKilled {
				t.Fatal("Process did not return after cancellation/deadline until the test killed the pipe-holding descendant")
			}
			if !errors.Is(result.err, test.wantErr) {
				t.Fatalf("Process() error = %v, want %v", result.err, test.wantErr)
			}
			waitForWorkspaceTestProcessExit(t, childPID, 3*time.Second)
			if result.receipt.WorkspaceID != "cpw_descendantpipe1" ||
				repository.completePurgeCalls != 1 || repository.receiptMutations != 0 {
				t.Fatalf("bounded cleanup receipt = %#v completes=%d mutations=%d",
					result.receipt, repository.completePurgeCalls, repository.receiptMutations)
			}
			if _, err := os.Lstat(filepath.Join(root, "cpw_descendantpipe1")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("workspace/cache residue after bounded cleanup: %v", err)
			}
			if _, err := os.Lstat(sharedCacheMarker); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("shared cache residue after bounded cleanup: %v", err)
			}
			formatted := strings.ToLower(result.receipt.String())
			for _, forbidden := range []string{root, string(source), "fontconfig", "residue"} {
				if strings.Contains(formatted, strings.ToLower(forbidden)) {
					t.Fatalf("bounded cleanup receipt %q contains forbidden value %q", formatted, forbidden)
				}
			}
		})
	}
}

func TestWorkspaceJanitorChecksCleanupContextBeforeFinalRemoval(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	paths, err := deriveProcessorWorkspacePaths(root, "cpw_cleanupfence1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(paths.workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx := &cancelOnErrCallContext{Context: context.Background(), triggerCall: 2}
	if _, err := removeProcessorWorkspaceTree(ctx, paths.workspace); !errors.Is(err, context.Canceled) {
		t.Fatalf("removeProcessorWorkspaceTree() error = %v, want context.Canceled", err)
	}
	if _, err := os.Lstat(paths.workspace); err != nil {
		t.Fatalf("workspace removal after cancellation = %v, want residue preserved", err)
	}
}

func TestSecureWorkspaceRejectsReplacementOutsideRoot(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("descriptor-anchored workspace helpers are Linux-specific")
	}
	t.Parallel()

	root := t.TempDir()
	external := t.TempDir()
	sentinel := []byte("external sentinel")
	if err := os.WriteFile(filepath.Join(external, "sentinel"), sentinel, 0o600); err != nil {
		t.Fatal(err)
	}
	secureRoot, err := openSecureWorkspaceRoot(root)
	if err != nil {
		t.Fatalf("openSecureWorkspaceRoot() error = %q (%#v)", err.Error(), err)
	}
	defer secureRoot.close()
	secureWorkspace, err := secureRoot.openWorkspace(context.Background(), "cpw_secure1")
	if err != nil {
		t.Fatalf("openWorkspace() error = %v", err)
	}
	defer secureWorkspace.close()
	workspacePath := filepath.Join(root, "cpw_secure1")
	if err := os.Remove(workspacePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, workspacePath); err != nil {
		t.Fatal(err)
	}
	if _, err := secureRoot.openWorkspace(context.Background(), "cpw_secure1"); !errors.Is(err, ErrUnsafeProcessorWorkspace) {
		t.Fatalf("openWorkspace(replaced symlink) error = %v, want ErrUnsafeProcessorWorkspace", err)
	}
	got, err := os.ReadFile(filepath.Join(external, "sentinel"))
	if err != nil || !bytes.Equal(got, sentinel) {
		t.Fatalf("external sentinel = %q error=%v, want unchanged", got, err)
	}
}

func TestSecurePreviewCommandUsesHeldSourceAfterPathReplacement(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("descriptor-anchored preview commands are Linux-specific")
	}
	t.Parallel()

	root := t.TempDir()
	secureRoot, err := openSecureWorkspaceRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer secureRoot.close()
	secureWorkspace, err := secureRoot.openWorkspace(context.Background(), "cpw_securecommand1")
	if err != nil {
		t.Fatal(err)
	}
	secure := secureWorkspace
	defer secure.close()
	content := []byte("held source bytes")
	digest := sha256.Sum256(content)
	if err := secure.materializeSource(context.Background(), bytes.NewReader(content), BlobObject{
		Key: "sha256/" + hex.EncodeToString(digest[:]), SHA256: digest,
		ObjectVersion: "secure-source-v1", SizeBytes: int64(len(content)), BackendKind: BackendKindLocal,
	}, 1024); err != nil {
		t.Fatalf("materialize secure source: %v", err)
	}
	if err := secure.preparePreviewDirectories(context.Background(), PreviewConfig{
		MaxSourceBytes: 1024, MaxOutputBytes: 1024, MaxImagePixels: 1,
		PDFInfoBinary: "/configured/pdfinfo", PDFToPPMBinary: "/configured/pdftoppm",
	}); err != nil {
		t.Fatalf("prepare secure command directories: %v", err)
	}
	workspacePath := filepath.Join(root, "cpw_securecommand1")
	sourcePath := filepath.Join(workspacePath, processorWorkspaceSourceName)
	external := filepath.Join(t.TempDir(), "external-source")
	if err := os.WriteFile(external, []byte("external replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(workspacePath, "source.original")
	if err := os.Rename(sourcePath, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, sourcePath); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := runPreviewCommandSecure(context.Background(), "/bin/cat",
		[]string{secure.commandSourcePath()}, secure, &output, io.Discard); err != nil {
		t.Fatalf("runPreviewCommandSecure() error = %v", err)
	}
	if !bytes.Equal(output.Bytes(), content) {
		t.Fatalf("secure command read %q, want held source %q", output.Bytes(), content)
	}
}

func TestSecurePreviewCommandCannotMutateHeldSource(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("descriptor-anchored preview commands are Linux-specific")
	}
	t.Parallel()

	root := t.TempDir()
	secureRoot, err := openSecureWorkspaceRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer secureRoot.close()
	secureWorkspace, err := secureRoot.openWorkspace(context.Background(), "cpw_securereadonly1")
	if err != nil {
		t.Fatal(err)
	}
	secure := secureWorkspace
	defer secure.close()
	content := []byte("held source bytes")
	digest := sha256.Sum256(content)
	if err := secure.materializeSource(context.Background(), bytes.NewReader(content), BlobObject{
		Key: "sha256/" + hex.EncodeToString(digest[:]), SHA256: digest,
		ObjectVersion: "secure-source-v1", SizeBytes: int64(len(content)), BackendKind: BackendKindLocal,
	}, 1024); err != nil {
		t.Fatalf("materialize secure source: %v", err)
	}
	if err := secure.preparePreviewDirectories(context.Background(), PreviewConfig{
		MaxSourceBytes: 1024, MaxOutputBytes: 1024, MaxImagePixels: 1,
		PDFInfoBinary: "/configured/pdfinfo", PDFToPPMBinary: "/configured/pdftoppm",
	}); err != nil {
		t.Fatalf("prepare secure command directories: %v", err)
	}
	if err := runPreviewCommandSecure(context.Background(), "/bin/sh",
		[]string{"-c", "set -eu; printf replacement >&4", "sh"}, secure, io.Discard, io.Discard); err == nil {
		t.Fatal("runPreviewCommandSecure() error = nil, want read-only source rejection")
	}
	got, err := os.ReadFile(filepath.Join(root, "cpw_securereadonly1", processorWorkspaceSourceName))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("held source changed to %q, want %q", got, content)
	}
}

func TestSecureWorkspacePurgeUnlinksReplacementWithoutFollowing(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("descriptor-anchored workspace purge is Linux-specific")
	}
	t.Parallel()

	root := t.TempDir()
	workspacePath := filepath.Join(root, "cpw_securepurge1")
	if err := os.Mkdir(workspacePath, 0o700); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(workspacePath, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "residue"), []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	external := t.TempDir()
	sentinel := []byte("must survive purge")
	if err := os.WriteFile(filepath.Join(external, "sentinel"), sentinel, 0o600); err != nil {
		t.Fatal(err)
	}
	secureRoot, err := openSecureWorkspaceRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer secureRoot.close()
	secureWorkspace, err := secureRoot.openWorkspace(context.Background(), "cpw_securepurge1")
	if err != nil {
		t.Fatal(err)
	}
	if err := secureWorkspace.close(); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(nested); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, nested); err != nil {
		t.Fatal(err)
	}
	if _, err := secureRoot.removeWorkspace(context.Background(), "cpw_securepurge1"); err != nil {
		t.Fatalf("secure purge after replacement: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(external, "sentinel"))
	if err != nil || !bytes.Equal(got, sentinel) {
		t.Fatalf("external purge sentinel = %q error=%v, want unchanged", got, err)
	}
}

func TestSecureWorkspacePurgeRejectsSpecialReplacement(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("descriptor-anchored workspace purge is Linux-specific")
	}
	t.Parallel()

	root := t.TempDir()
	workspacePath := filepath.Join(root, "cpw_securespecial1")
	if err := workspaceTestMakeFIFO(workspacePath, 0o600); err != nil {
		t.Fatal(err)
	}
	secureRoot, err := openSecureWorkspaceRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer secureRoot.close()
	if _, err := secureRoot.removeWorkspace(context.Background(), "cpw_securespecial1"); !errors.Is(err, ErrUnsafeProcessorWorkspace) {
		t.Fatalf("removeWorkspace(special replacement) error = %v, want ErrUnsafeProcessorWorkspace", err)
	}
	info, err := os.Lstat(workspacePath)
	if err != nil {
		t.Fatalf("special replacement removed after fail-closed purge: %v", err)
	}
	if info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("special replacement mode = %v, want named pipe", info.Mode())
	}
}

func TestSecurePreviewDirectoriesCloseDerivedDirectoryFDs(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("descriptor-anchored preview directories are Linux-specific")
	}

	root := filepath.Join(t.TempDir(), "private-root")
	secureRoot, err := openSecureWorkspaceRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	secureWorkspace, err := secureRoot.openWorkspace(context.Background(), "cpw_fdclose1")
	if err != nil {
		_ = secureRoot.close()
		t.Fatal(err)
	}
	if err := secureWorkspace.preparePreviewDirectories(context.Background(), PreviewConfig{
		MaxSourceBytes: 1024, MaxOutputBytes: 1024, MaxImagePixels: 16,
		PDFInfoBinary: "/configured/pdfinfo", PDFToPPMBinary: "/configured/pdftoppm",
	}); err != nil {
		_ = secureWorkspace.close()
		_ = secureRoot.close()
		t.Fatal(err)
	}
	if err := secureWorkspace.close(); err != nil {
		_ = secureRoot.close()
		t.Fatal(err)
	}
	if _, err := secureRoot.removeWorkspace(context.Background(), "cpw_fdclose1"); err != nil {
		_ = secureRoot.close()
		t.Fatal(err)
	}
	if err := secureRoot.close(); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		target, readErr := os.Readlink(filepath.Join("/proc/self/fd", entry.Name()))
		if readErr == nil && strings.Contains(target, "cpw_fdclose1") {
			t.Fatalf("preview directory FD leaked after cleanup: fd=%s target=%q", entry.Name(), target)
		}
	}
}

func waitForWorkspaceTestFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stat synchronization file %s: %v", path, err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for synchronization file %s", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func readWorkspaceTestPID(t *testing.T, path string) int {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read descendant PID: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(content)))
	if err != nil || pid <= 0 {
		t.Fatalf("parse descendant PID %q: %v", content, err)
	}
	return pid
}

func waitForWorkspaceTestProcessExit(t *testing.T, pid int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		err := workspaceTestProbeProcess(pid)
		if errors.Is(err, errWorkspaceTestProcessNotFound) {
			return
		}
		if err != nil && !errors.Is(err, errWorkspaceTestProcessPermissionDenied) {
			t.Fatalf("probe descendant process %d: %v", pid, err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("descendant process %d survived cancellation/deadline", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type controlledWorkspaceContext struct {
	context.Context
	err error
}

type cancelOnErrCallContext struct {
	context.Context
	triggerCall int
	calls       int
}

func (ctx *cancelOnErrCallContext) Err() error {
	ctx.calls++
	if ctx.calls >= ctx.triggerCall {
		return context.Canceled
	}
	return nil
}

func (ctx *controlledWorkspaceContext) Err() error {
	return ctx.err
}

type fakeProcessorWorkspaceRepository struct {
	workspace                    ProcessorWorkspace
	registration                 ProcessorWorkspaceRegistration
	receipt                      ProcessorWorkspacePurgeReceipt
	registerCalls                int
	materializeCalls             int
	beginPurgeCalls              int
	completePurgeCalls           int
	receiptMutations             int
	completeConflictAfterPersist bool
	registerReadback             *ProcessorWorkspace
	onRegister                   func(ProcessorWorkspaceRegistration)
	onMaterialize                func(ProcessorWorkspaceTransition)
}

func newFakeProcessorWorkspaceRepository() *fakeProcessorWorkspaceRepository {
	return &fakeProcessorWorkspaceRepository{}
}

func (repository *fakeProcessorWorkspaceRepository) RegisterProcessorWorkspace(_ context.Context, registration ProcessorWorkspaceRegistration) (ProcessorWorkspace, error) {
	repository.registerCalls++
	repository.registration = registration
	if repository.onRegister != nil {
		repository.onRegister(registration)
	}
	if repository.workspace.WorkspaceID == "" {
		repository.workspace = ProcessorWorkspace{
			WorkspaceID: registration.WorkspaceID, ProcessorJobID: registration.Claim.ProcessorJobID,
			Attempt: registration.Claim.Attempt, State: ProcessorWorkspaceStateRegistered,
			WorkspacePathDigest: registration.WorkspacePathDigest, ExpiresAt: registration.ExpiresAt,
		}
	}
	if repository.registerReadback != nil {
		return *repository.registerReadback, nil
	}
	return repository.workspace, nil
}

func (repository *fakeProcessorWorkspaceRepository) MaterializeProcessorWorkspace(_ context.Context, transition ProcessorWorkspaceTransition) (ProcessorWorkspace, error) {
	repository.materializeCalls++
	if repository.onMaterialize != nil {
		repository.onMaterialize(transition)
	}
	if repository.workspace.WorkspaceID != transition.WorkspaceID || repository.workspace.WorkspacePathDigest != transition.WorkspacePathDigest {
		return ProcessorWorkspace{}, ErrAttachmentConflict
	}
	repository.workspace.State = ProcessorWorkspaceStateMaterialized
	return repository.workspace, nil
}

func (repository *fakeProcessorWorkspaceRepository) BeginProcessorWorkspacePurge(_ context.Context, transition ProcessorWorkspaceTransition) (ProcessorWorkspacePurgePlan, error) {
	repository.beginPurgeCalls++
	if repository.receipt.WorkspaceID != "" {
		return ProcessorWorkspacePurgePlan{Workspace: repository.workspace, Receipt: &repository.receipt}, nil
	}
	if repository.workspace.WorkspaceID != transition.WorkspaceID || repository.workspace.WorkspacePathDigest != transition.WorkspacePathDigest {
		return ProcessorWorkspacePurgePlan{}, ErrAttachmentConflict
	}
	repository.workspace.State = ProcessorWorkspaceStatePurging
	return ProcessorWorkspacePurgePlan{Workspace: repository.workspace}, nil
}

func (repository *fakeProcessorWorkspaceRepository) CompleteProcessorWorkspacePurge(_ context.Context, input ProcessorWorkspacePurgeCompletion) (ProcessorWorkspacePurgeReceipt, error) {
	repository.completePurgeCalls++
	if repository.receipt.WorkspaceID != "" {
		if repository.receipt != input.Receipt {
			repository.receiptMutations++
			return ProcessorWorkspacePurgeReceipt{}, ErrAttachmentConflict
		}
		return repository.receipt, nil
	}
	repository.workspace.State = ProcessorWorkspaceStatePurged
	repository.receipt = input.Receipt
	if repository.completeConflictAfterPersist {
		repository.completeConflictAfterPersist = false
		return ProcessorWorkspacePurgeReceipt{}, ErrAttachmentConflict
	}
	return repository.receipt, nil
}

func workspaceTestPreviewProcessor() *PreviewProcessor {
	return newPreviewProcessor(PreviewConfig{
		MaxSourceBytes: 1024, MaxOutputBytes: 1024, MaxImagePixels: 1,
		PDFInfoBinary: "/configured/pdfinfo", PDFToPPMBinary: "/configured/pdftoppm",
	}, nil)
}

func workspaceTestClaim(source []byte, profile ProcessorProfile) ProcessorClaim {
	digest := sha256.Sum256(source)
	now := time.Now().UTC().Truncate(time.Microsecond)
	return ProcessorClaim{
		ProjectID: "default", ProcessorJobID: "apj_workspace1", UploadID: "aup_workspace1",
		AttachmentID: "att_workspace1", Source: BlobObject{
			Key: "sha256/" + hex.EncodeToString(digest[:]), SHA256: digest,
			ObjectVersion: "workspace-source-v1", SizeBytes: int64(len(source)), BackendKind: BackendKindLocal,
		}, Profile: profile, Attempt: 1, MaxAttempts: 3, OwnerID: "workspace_test",
		OwnerGeneration: 1, LeaseExpiresAt: now.Add(time.Minute), ExpiresAt: now.Add(time.Hour),
	}
}

var _ io.Reader = (*bytes.Reader)(nil)
