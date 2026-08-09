package attachments

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	processorWorkspaceSourceName     = "source.bin"
	processorWorkspacePreviewName    = "preview"
	processorWorkspaceCacheName      = "cache"
	processorWorkspaceConfigName     = "config"
	processorWorkspaceFontConfigName = "fonts.conf"
	processorWorkspaceTempName       = "tmp"
	workspacePurgeSurfaceDomainV1    = "houfeng.attachments.workspace-purge-surface.v1"
	workspacePurgeReceiptDomainV1    = "houfeng.attachments.workspace-purge-receipt.v1"
)

var ErrUnsafeProcessorWorkspace = errors.New("unsafe content processor workspace")

type ProcessorWorkspaceAuthorizationMode string

const (
	ProcessorWorkspaceAuthorizationWorker         ProcessorWorkspaceAuthorizationMode = "worker"
	ProcessorWorkspaceAuthorizationReconciliation ProcessorWorkspaceAuthorizationMode = "reconciliation"
)

type ProcessorWorkspaceAuthorization struct {
	Mode  ProcessorWorkspaceAuthorizationMode
	Claim ProcessorClaim
}

func NewProcessorWorkspaceWorkerAuthorization(claim ProcessorClaim) ProcessorWorkspaceAuthorization {
	return ProcessorWorkspaceAuthorization{Mode: ProcessorWorkspaceAuthorizationWorker, Claim: claim}
}

func NewProcessorWorkspaceReconciliationAuthorization() ProcessorWorkspaceAuthorization {
	return ProcessorWorkspaceAuthorization{Mode: ProcessorWorkspaceAuthorizationReconciliation}
}

type ProcessorWorkspaceTransition struct {
	WorkspaceID         string
	WorkspacePathDigest [sha256.Size]byte
	Authorization       ProcessorWorkspaceAuthorization
}

func (transition ProcessorWorkspaceTransition) Validate() error {
	if ValidateWorkspaceID(transition.WorkspaceID) != nil ||
		transition.WorkspacePathDigest == [sha256.Size]byte{} {
		return ErrInvalidProcessorCommand
	}
	switch transition.Authorization.Mode {
	case ProcessorWorkspaceAuthorizationWorker:
		if transition.Authorization.Claim.Validate() != nil {
			return ErrInvalidProcessorCommand
		}
	case ProcessorWorkspaceAuthorizationReconciliation:
		if transition.Authorization.Claim != (ProcessorClaim{}) {
			return ErrInvalidProcessorCommand
		}
	default:
		return ErrInvalidProcessorCommand
	}
	return nil
}

type ProcessorWorkspacePurgeReceipt struct {
	WorkspaceID          string
	RemovedSurfaceDigest [sha256.Size]byte
	ReceiptDigest        [sha256.Size]byte
	RemovedRowCount      int64
	VerifiedAbsentAt     time.Time
}

func NewProcessorWorkspacePurgeReceipt(
	workspaceID string,
	removedRowCount int64,
	verifiedAbsentAt time.Time,
) (ProcessorWorkspacePurgeReceipt, error) {
	receipt := ProcessorWorkspacePurgeReceipt{
		WorkspaceID: workspaceID, RemovedRowCount: removedRowCount,
		VerifiedAbsentAt: verifiedAbsentAt.UTC().Truncate(time.Microsecond),
	}
	receipt.RemovedSurfaceDigest = processorWorkspaceRemovedSurfaceDigest(receipt.WorkspaceID, receipt.RemovedRowCount)
	receipt.ReceiptDigest = processorWorkspaceReceiptDigest(receipt)
	if receipt.Validate() != nil {
		return ProcessorWorkspacePurgeReceipt{}, ErrInvalidProcessorCommand
	}
	return receipt, nil
}

func (receipt ProcessorWorkspacePurgeReceipt) Validate() error {
	if ValidateWorkspaceID(receipt.WorkspaceID) != nil || receipt.RemovedRowCount < 0 ||
		receipt.RemovedSurfaceDigest == [sha256.Size]byte{} ||
		receipt.ReceiptDigest == [sha256.Size]byte{} || receipt.VerifiedAbsentAt.IsZero() {
		return ErrInvalidProcessorCommand
	}
	wantSurface := processorWorkspaceRemovedSurfaceDigest(receipt.WorkspaceID, receipt.RemovedRowCount)
	if receipt.RemovedSurfaceDigest != wantSurface ||
		receipt.ReceiptDigest != processorWorkspaceReceiptDigest(receipt) {
		return ErrInvalidProcessorCommand
	}
	return nil
}

func (receipt ProcessorWorkspacePurgeReceipt) String() string {
	return fmt.Sprintf(
		"workspace_purge_receipt{workspace_id=%q removed_surface_digest=%s receipt_digest=%s removed_row_count=%d verified_absent_at=%s}",
		receipt.WorkspaceID,
		hex.EncodeToString(receipt.RemovedSurfaceDigest[:]),
		hex.EncodeToString(receipt.ReceiptDigest[:]),
		receipt.RemovedRowCount,
		receipt.VerifiedAbsentAt.UTC().Format(time.RFC3339Nano),
	)
}

type ProcessorWorkspacePurgePlan struct {
	Workspace ProcessorWorkspace
	Receipt   *ProcessorWorkspacePurgeReceipt
}

type ProcessorWorkspacePurgeCompletion struct {
	Workspace ProcessorWorkspaceTransition
	Receipt   ProcessorWorkspacePurgeReceipt
}

func (completion ProcessorWorkspacePurgeCompletion) Validate() error {
	if completion.Workspace.Validate() != nil || completion.Receipt.Validate() != nil ||
		completion.Workspace.WorkspaceID != completion.Receipt.WorkspaceID {
		return ErrInvalidProcessorCommand
	}
	return nil
}

type ProcessorWorkspaceRepository interface {
	RegisterProcessorWorkspace(context.Context, ProcessorWorkspaceRegistration) (ProcessorWorkspace, error)
	MaterializeProcessorWorkspace(context.Context, ProcessorWorkspaceTransition) (ProcessorWorkspace, error)
	BeginProcessorWorkspacePurge(context.Context, ProcessorWorkspaceTransition) (ProcessorWorkspacePurgePlan, error)
	CompleteProcessorWorkspacePurge(context.Context, ProcessorWorkspacePurgeCompletion) (ProcessorWorkspacePurgeReceipt, error)
}

// ProcessorWorkspaceCutpoint names content-free filesystem/processing
// boundaries used to prove restart convergence.
type ProcessorWorkspaceCutpoint string

const (
	ProcessorWorkspaceCutpointAfterMkdir                 ProcessorWorkspaceCutpoint = "after_mkdir"
	ProcessorWorkspaceCutpointAfterSourceMaterialization ProcessorWorkspaceCutpoint = "after_source_materialization"
	ProcessorWorkspaceCutpointAfterProcessing            ProcessorWorkspaceCutpoint = "after_processing"
	ProcessorWorkspaceCutpointAfterPhysicalPurge         ProcessorWorkspaceCutpoint = "after_physical_purge"
)

type ContentProcessorWorkspaceConfig struct {
	Root           string
	MaxSourceBytes int64
	CleanupTimeout time.Duration
	Cutpoint       func(ProcessorWorkspaceCutpoint) error
}

type ProcessorWorkspaceProcessRequest struct {
	Claim       ProcessorClaim
	WorkspaceID string
	ExpiresAt   time.Time
	Source      io.Reader
}

func (request ProcessorWorkspaceProcessRequest) validate(maxSourceBytes int64) error {
	if request.Claim.Validate() != nil || ValidateWorkspaceID(request.WorkspaceID) != nil ||
		request.ExpiresAt.IsZero() || request.ExpiresAt.After(request.Claim.ExpiresAt) ||
		request.Source == nil || request.Claim.Source.SizeBytes > maxSourceBytes {
		return ErrInvalidProcessorCommand
	}
	return nil
}

type ContentProcessorWorkspace struct {
	config     ContentProcessorWorkspaceConfig
	repository ProcessorWorkspaceRepository
	preview    *PreviewProcessor
	janitor    *WorkspaceJanitor
	cutpoint   func(ProcessorWorkspaceCutpoint) error
}

func NewContentProcessorWorkspace(
	config ContentProcessorWorkspaceConfig,
	repository ProcessorWorkspaceRepository,
	preview *PreviewProcessor,
) (*ContentProcessorWorkspace, error) {
	if config.Root == "" || config.MaxSourceBytes <= 0 || config.CleanupTimeout <= 0 ||
		repository == nil || preview == nil || preview.config.validate() != nil {
		return nil, ErrInvalidProcessorCommand
	}
	if validateWorkspaceRootConfiguration(config.Root) != nil {
		return nil, ErrInvalidProcessorCommand
	}
	config.Root = filepath.Clean(config.Root)
	janitor := newWorkspaceJanitor(config.Root, repository, config.CleanupTimeout)
	janitor.cutpoint = config.Cutpoint
	return &ContentProcessorWorkspace{
		config: config, repository: repository, preview: preview,
		janitor: janitor, cutpoint: config.Cutpoint,
	}, nil
}

// ValidateProcessorWorkspaceRoot exposes the same path-shape guard used by
// the workspace constructor so command wiring does not maintain a second
// reserved-directory inventory.
func ValidateProcessorWorkspaceRoot(root string) error {
	return validateWorkspaceRootConfiguration(root)
}

func (workspace *ContentProcessorWorkspace) Process(
	ctx context.Context,
	request ProcessorWorkspaceProcessRequest,
) (artifact PreviewArtifact, receipt ProcessorWorkspacePurgeReceipt, resultErr error) {
	if ctx == nil || workspace == nil || workspace.repository == nil || workspace.preview == nil ||
		request.validate(workspace.config.MaxSourceBytes) != nil {
		return PreviewArtifact{}, ProcessorWorkspacePurgeReceipt{}, ErrInvalidProcessorCommand
	}
	processContext, cancel := context.WithDeadline(ctx, request.ExpiresAt)
	defer cancel()
	secureRoot, err := openSecureWorkspaceRoot(workspace.config.Root)
	if err != nil {
		return PreviewArtifact{}, ProcessorWorkspacePurgeReceipt{}, err
	}
	defer secureRoot.close()
	paths, err := deriveProcessorWorkspacePaths(workspace.config.Root, request.WorkspaceID)
	if err != nil {
		return PreviewArtifact{}, ProcessorWorkspacePurgeReceipt{}, err
	}
	pathDigest := sha256.Sum256([]byte(paths.workspace))
	registration := ProcessorWorkspaceRegistration{
		Claim: request.Claim, WorkspaceID: request.WorkspaceID,
		WorkspacePathDigest: pathDigest, ExpiresAt: request.ExpiresAt.UTC().Truncate(time.Microsecond),
	}
	registered, err := workspace.repository.RegisterProcessorWorkspace(processContext, registration)
	if err != nil {
		return PreviewArtifact{}, ProcessorWorkspacePurgeReceipt{}, err
	}
	transition := ProcessorWorkspaceTransition{
		WorkspaceID: request.WorkspaceID, WorkspacePathDigest: pathDigest,
		Authorization: NewProcessorWorkspaceWorkerAuthorization(request.Claim),
	}
	defer func() {
		purgeReceipt, purgeErr := workspace.janitor.purgeWithRoot(ctx, transition, secureRoot)
		if purgeErr == nil {
			receipt = purgeReceipt
		}
		if purgeErr != nil {
			resultErr = errors.Join(resultErr, purgeErr)
		}
	}()

	wantRegistered := ProcessorWorkspace{
		WorkspaceID: request.WorkspaceID, ProcessorJobID: request.Claim.ProcessorJobID,
		Attempt: request.Claim.Attempt, State: ProcessorWorkspaceStateRegistered,
		WorkspacePathDigest: pathDigest, ExpiresAt: registration.ExpiresAt,
	}
	if registered != wantRegistered {
		return PreviewArtifact{}, ProcessorWorkspacePurgeReceipt{}, ErrAttachmentConflict
	}
	secureWorkspace, err := secureRoot.openWorkspace(processContext, request.WorkspaceID)
	if err != nil {
		return PreviewArtifact{}, ProcessorWorkspacePurgeReceipt{}, err
	}
	paths.secure = secureWorkspace
	defer secureWorkspace.close()
	if err := workspace.hitCutpoint(ProcessorWorkspaceCutpointAfterMkdir); err != nil {
		return PreviewArtifact{}, ProcessorWorkspacePurgeReceipt{}, err
	}
	if err := materializeProcessorSource(processContext, paths, request.Source, request.Claim.Source, workspace.config.MaxSourceBytes); err != nil {
		return PreviewArtifact{}, ProcessorWorkspacePurgeReceipt{}, err
	}
	if err := workspace.hitCutpoint(ProcessorWorkspaceCutpointAfterSourceMaterialization); err != nil {
		return PreviewArtifact{}, ProcessorWorkspacePurgeReceipt{}, err
	}
	materialized, err := workspace.repository.MaterializeProcessorWorkspace(processContext, transition)
	if err != nil {
		return PreviewArtifact{}, ProcessorWorkspacePurgeReceipt{}, err
	}
	wantMaterialized := wantRegistered
	wantMaterialized.State = ProcessorWorkspaceStateMaterialized
	if materialized != wantMaterialized {
		return PreviewArtifact{}, ProcessorWorkspacePurgeReceipt{}, ErrAttachmentConflict
	}
	if err := ctx.Err(); err != nil {
		return PreviewArtifact{}, ProcessorWorkspacePurgeReceipt{}, err
	}
	artifact, err = workspace.preview.process(processContext, request.Claim.Profile, paths)
	if err != nil {
		return PreviewArtifact{}, ProcessorWorkspacePurgeReceipt{}, err
	}
	if err := workspace.hitCutpoint(ProcessorWorkspaceCutpointAfterProcessing); err != nil {
		return PreviewArtifact{}, ProcessorWorkspacePurgeReceipt{}, err
	}
	if err := processContext.Err(); err != nil {
		return PreviewArtifact{}, ProcessorWorkspacePurgeReceipt{}, err
	}
	return artifact, ProcessorWorkspacePurgeReceipt{}, nil
}

func (workspace *ContentProcessorWorkspace) hitCutpoint(cutpoint ProcessorWorkspaceCutpoint) error {
	if workspace == nil || workspace.cutpoint == nil {
		return nil
	}
	return workspace.cutpoint(cutpoint)
}

type WorkspaceJanitor struct {
	root       string
	repository ProcessorWorkspaceRepository
	timeout    time.Duration
	now        func() time.Time
	cutpoint   func(ProcessorWorkspaceCutpoint) error
}

func newWorkspaceJanitor(root string, repository ProcessorWorkspaceRepository, timeout time.Duration) *WorkspaceJanitor {
	return newWorkspaceJanitorWithClock(root, repository, timeout, time.Now)
}

func newWorkspaceJanitorWithClock(
	root string,
	repository ProcessorWorkspaceRepository,
	timeout time.Duration,
	now func() time.Time,
) *WorkspaceJanitor {
	return &WorkspaceJanitor{root: root, repository: repository, timeout: timeout, now: now}
}

func (janitor *WorkspaceJanitor) Purge(
	ctx context.Context,
	transition ProcessorWorkspaceTransition,
) (ProcessorWorkspacePurgeReceipt, error) {
	return janitor.purgeWithRoot(ctx, transition, nil)
}

func (janitor *WorkspaceJanitor) purgeWithRoot(
	ctx context.Context,
	transition ProcessorWorkspaceTransition,
	providedRoot secureProcessorWorkspaceRoot,
) (ProcessorWorkspacePurgeReceipt, error) {
	if ctx == nil || janitor == nil || janitor.repository == nil || janitor.timeout <= 0 || janitor.now == nil ||
		transition.Validate() != nil {
		return ProcessorWorkspacePurgeReceipt{}, ErrInvalidProcessorCommand
	}
	// Cleanup intentionally gets an independent bounded context: caller
	// cancellation must not strand a registered workspace, while the timeout
	// still bounds every janitor operation including final removal.
	cleanupContext, cancel := context.WithTimeout(context.Background(), janitor.timeout)
	defer cancel()
	paths, err := deriveProcessorWorkspacePaths(janitor.root, transition.WorkspaceID)
	if err != nil {
		return ProcessorWorkspacePurgeReceipt{}, err
	}
	if sha256.Sum256([]byte(paths.workspace)) != transition.WorkspacePathDigest {
		return ProcessorWorkspacePurgeReceipt{}, ErrAttachmentConflict
	}
	secureRoot := providedRoot
	closeRoot := false
	if secureRoot == nil {
		secureRoot, err = openSecureWorkspaceRoot(janitor.root)
		if err != nil {
			return ProcessorWorkspacePurgeReceipt{}, err
		}
		closeRoot = true
	}
	if closeRoot {
		defer secureRoot.close()
	}
	plan, err := janitor.repository.BeginProcessorWorkspacePurge(cleanupContext, transition)
	if err != nil {
		return ProcessorWorkspacePurgeReceipt{}, err
	}
	if plan.Receipt != nil {
		if plan.Workspace != (ProcessorWorkspace{}) &&
			(plan.Workspace.WorkspaceID != transition.WorkspaceID ||
				plan.Workspace.WorkspacePathDigest != transition.WorkspacePathDigest) {
			return ProcessorWorkspacePurgeReceipt{}, ErrAttachmentConflict
		}
		return removeProcessorWorkspaceReceiptReplay(cleanupContext, secureRoot, transition.WorkspaceID, transition, *plan.Receipt)
	}
	if plan.Workspace.WorkspaceID != transition.WorkspaceID ||
		plan.Workspace.WorkspacePathDigest != transition.WorkspacePathDigest ||
		plan.Workspace.State != ProcessorWorkspaceStatePurging {
		return ProcessorWorkspacePurgeReceipt{}, ErrAttachmentConflict
	}
	removedCount, err := secureRoot.removeWorkspace(cleanupContext, transition.WorkspaceID)
	if err != nil {
		return ProcessorWorkspacePurgeReceipt{}, err
	}
	if janitor.cutpoint != nil {
		if err := janitor.cutpoint(ProcessorWorkspaceCutpointAfterPhysicalPurge); err != nil {
			return ProcessorWorkspacePurgeReceipt{}, err
		}
	}
	verifiedAt := janitor.now().UTC().Truncate(time.Microsecond)
	receipt, err := NewProcessorWorkspacePurgeReceipt(transition.WorkspaceID, removedCount, verifiedAt)
	if err != nil {
		return ProcessorWorkspacePurgeReceipt{}, err
	}
	completed, err := janitor.repository.CompleteProcessorWorkspacePurge(cleanupContext, ProcessorWorkspacePurgeCompletion{
		Workspace: transition, Receipt: receipt,
	})
	if err != nil {
		if errors.Is(err, ErrAttachmentConflict) {
			replay, replayErr := janitor.repository.BeginProcessorWorkspacePurge(cleanupContext, transition)
			if replayErr == nil && replay.Receipt != nil {
				return removeProcessorWorkspaceReceiptReplay(
					cleanupContext, secureRoot, transition.WorkspaceID, transition, *replay.Receipt,
				)
			}
		}
		return ProcessorWorkspacePurgeReceipt{}, err
	}
	if completed != receipt {
		return ProcessorWorkspacePurgeReceipt{}, ErrAttachmentConflict
	}
	return completed, nil
}

func removeProcessorWorkspaceReceiptReplay(
	ctx context.Context,
	secureRoot secureProcessorWorkspaceRoot,
	workspaceID string,
	transition ProcessorWorkspaceTransition,
	receipt ProcessorWorkspacePurgeReceipt,
) (ProcessorWorkspacePurgeReceipt, error) {
	if receipt.Validate() != nil || receipt.WorkspaceID != transition.WorkspaceID {
		return ProcessorWorkspacePurgeReceipt{}, ErrAttachmentConflict
	}
	if secureRoot == nil {
		return ProcessorWorkspacePurgeReceipt{}, ErrUnsafeProcessorWorkspace
	}
	if _, err := secureRoot.removeWorkspace(ctx, workspaceID); err != nil {
		return ProcessorWorkspacePurgeReceipt{}, err
	}
	return receipt, nil
}

type processorWorkspacePaths struct {
	root          string
	workspace     string
	source        string
	previewPrefix string
	preview       string
	secure        secureProcessorWorkspace
}

func deriveProcessorWorkspacePaths(root, workspaceID string) (processorWorkspacePaths, error) {
	if ValidateWorkspaceID(workspaceID) != nil || validateWorkspaceRootConfiguration(root) != nil {
		return processorWorkspacePaths{}, ErrInvalidProcessorCommand
	}
	absoluteRoot := filepath.Clean(root)
	workspace := filepath.Join(absoluteRoot, workspaceID)
	relative, err := filepath.Rel(absoluteRoot, workspace)
	if err != nil || relative == "." || relative == ".." ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return processorWorkspacePaths{}, ErrUnsafeProcessorWorkspace
	}
	previewPrefix := filepath.Join(workspace, processorWorkspacePreviewName)
	return processorWorkspacePaths{
		root: absoluteRoot, workspace: workspace,
		source:        filepath.Join(workspace, processorWorkspaceSourceName),
		previewPrefix: previewPrefix, preview: previewPrefix + ".png",
	}, nil
}

func ensurePrivateWorkspaceRoot(root string) error {
	if validateWorkspaceRootConfiguration(root) != nil {
		return ErrUnsafeProcessorWorkspace
	}
	if err := rejectWorkspaceSymlinkAncestors(root); err != nil {
		return err
	}
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(root, 0o700); err != nil {
			return ErrUnsafeProcessorWorkspace
		}
		info, err = os.Lstat(root)
	}
	if ancestorErr := rejectWorkspaceSymlinkAncestors(root); ancestorErr != nil {
		return ancestorErr
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return ErrUnsafeProcessorWorkspace
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return ErrUnsafeProcessorWorkspace
	}
	for _, entry := range entries {
		if ValidateWorkspaceID(entry.Name()) != nil {
			return ErrUnsafeProcessorWorkspace
		}
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return ErrUnsafeProcessorWorkspace
	}
	return nil
}

func validateWorkspaceRootConfiguration(root string) error {
	if root == "" || !filepath.IsAbs(root) {
		return ErrInvalidProcessorCommand
	}
	cleaned := filepath.Clean(root)
	if cleaned == filepath.VolumeName(cleaned)+string(filepath.Separator) || filepath.Dir(cleaned) == cleaned {
		return ErrInvalidProcessorCommand
	}
	switch strings.ToLower(filepath.Base(cleaned)) {
	case "bin", "cache", "data", "dev", "etc", "home", "houfeng", "lib", "lib64", "opt", "proc", "record-platform", "root", "run", "sbin", "storage", "sys", "tmp", "usr", "var", "workspace", "workspaces":
		return ErrInvalidProcessorCommand
	default:
		return nil
	}
}

func rejectWorkspaceSymlinkAncestors(path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return ErrUnsafeProcessorWorkspace
	}
	absolute = filepath.Clean(absolute)
	volume := filepath.VolumeName(absolute)
	remainder := strings.TrimPrefix(absolute, volume)
	remainder = strings.TrimPrefix(remainder, string(filepath.Separator))
	current := volume + string(filepath.Separator)
	for _, component := range strings.Split(remainder, string(filepath.Separator)) {
		if component == "" {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return ErrUnsafeProcessorWorkspace
		}
	}
	return nil
}

func materializeProcessorSource(
	ctx context.Context,
	paths processorWorkspacePaths,
	source io.Reader,
	expected BlobObject,
	maxSourceBytes int64,
) error {
	if paths.secure != nil {
		return paths.secure.materializeSource(ctx, source, expected, maxSourceBytes)
	}
	if err := os.Mkdir(paths.workspace, 0o700); err != nil {
		return ErrUnsafeProcessorWorkspace
	}
	workspaceInfo, err := os.Lstat(paths.workspace)
	if err != nil || workspaceInfo.Mode()&os.ModeSymlink != 0 || !workspaceInfo.IsDir() ||
		workspaceInfo.Mode().Perm() != 0o700 {
		return ErrUnsafeProcessorWorkspace
	}
	file, err := os.OpenFile(paths.source, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return ErrUnsafeProcessorWorkspace
	}
	return materializeProcessorSourceFile(ctx, file, source, expected, maxSourceBytes)
}

func materializeProcessorSourceFile(
	ctx context.Context,
	file *os.File,
	source io.Reader,
	expected BlobObject,
	maxSourceBytes int64,
) error {
	if ctx == nil || file == nil || source == nil || maxSourceBytes <= 0 {
		if file != nil {
			_ = file.Close()
		}
		return ErrInvalidProcessorCommand
	}
	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hasher), io.LimitReader(&contextReader{ctx: ctx, reader: source}, maxSourceBytes+1))
	if copyErr == nil {
		copyErr = file.Sync()
	}
	closeErr := file.Close()
	if contextError := ctx.Err(); contextError != nil {
		return contextError
	}
	if copyErr != nil || closeErr != nil {
		return ErrInvalidPreviewContent
	}
	if written > maxSourceBytes {
		return ErrPreviewLimitExceeded
	}
	if written != expected.SizeBytes || !equalHash(hasher, expected.SHA256) {
		return ErrInvalidPreviewContent
	}
	return nil
}

func equalHash(hasher hash.Hash, expected [sha256.Size]byte) bool {
	actual := hasher.Sum(nil)
	return len(actual) == len(expected) && string(actual) == string(expected[:])
}

func removeProcessorWorkspaceTree(ctx context.Context, workspacePath string) (int64, error) {
	info, err := os.Lstat(workspacePath)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, ErrUnsafeProcessorWorkspace
	}
	if info.Mode()&os.ModeSymlink != 0 {
		if err := os.Remove(workspacePath); err != nil {
			return 0, ErrUnsafeProcessorWorkspace
		}
		if _, err := os.Lstat(workspacePath); !errors.Is(err, os.ErrNotExist) {
			return 0, ErrUnsafeProcessorWorkspace
		}
		return 1, nil
	}
	if !info.IsDir() {
		return 0, ErrUnsafeProcessorWorkspace
	}
	var removed int64
	err = filepath.WalkDir(workspacePath, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return ErrUnsafeProcessorWorkspace
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return ErrUnsafeProcessorWorkspace
		}
		if info.Mode()&os.ModeSymlink != 0 {
			removed++
			return nil
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return ErrUnsafeProcessorWorkspace
		}
		if path != workspacePath && info.Mode().IsRegular() {
			removed++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := os.RemoveAll(workspacePath); err != nil {
		return 0, ErrUnsafeProcessorWorkspace
	}
	if _, err := os.Lstat(workspacePath); !errors.Is(err, os.ErrNotExist) {
		return 0, ErrUnsafeProcessorWorkspace
	}
	return removed, nil
}

func processorWorkspaceRemovedSurfaceDigest(workspaceID string, removedRowCount int64) [sha256.Size]byte {
	hasher := sha256.New()
	writeWorkspaceDigestString(hasher, workspacePurgeSurfaceDomainV1)
	writeWorkspaceDigestString(hasher, workspaceID)
	writeWorkspaceDigestInt64(hasher, removedRowCount)
	writeWorkspaceDigestString(hasher, "verified_absent")
	var digest [sha256.Size]byte
	copy(digest[:], hasher.Sum(nil))
	return digest
}

func processorWorkspaceReceiptDigest(receipt ProcessorWorkspacePurgeReceipt) [sha256.Size]byte {
	hasher := sha256.New()
	writeWorkspaceDigestString(hasher, workspacePurgeReceiptDomainV1)
	writeWorkspaceDigestString(hasher, receipt.WorkspaceID)
	_, _ = hasher.Write(receipt.RemovedSurfaceDigest[:])
	writeWorkspaceDigestInt64(hasher, receipt.RemovedRowCount)
	writeWorkspaceDigestInt64(hasher, receipt.VerifiedAbsentAt.UTC().UnixMicro())
	var digest [sha256.Size]byte
	copy(digest[:], hasher.Sum(nil))
	return digest
}

func writeWorkspaceDigestString(writer io.Writer, value string) {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(value)))
	_, _ = writer.Write(length[:])
	_, _ = writer.Write([]byte(value))
}

func writeWorkspaceDigestInt64(writer io.Writer, value int64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(value))
	_, _ = writer.Write(encoded[:])
}
