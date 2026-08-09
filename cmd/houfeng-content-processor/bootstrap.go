package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/store"
)

const (
	defaultProcessorOwnerID              = "content-processor"
	defaultProcessorOwnerLeaseDuration   = 5 * time.Minute
	defaultProcessorWorkspaceCleanup     = 30 * time.Second
	defaultProcessorReconciliationItems  = 100
	defaultProcessorReconciliationWindow = 30 * time.Second
	defaultProcessorReconciliationRetry  = time.Second
	defaultProcessorMaxAttempts          = int64(3)
	defaultProcessorJobTTL               = 24 * time.Hour
	defaultClamAVDialTimeout             = 5 * time.Second
	defaultClamAVOperationTimeout        = 2 * time.Minute
	defaultClamAVChunkSize               = 64 * 1024
	defaultClamAVResponseLimit           = 4 * 1024
)

type contentProcessorConfig struct {
	DatabaseURL string

	BlobBackend attachments.BackendKind
	BlobRoot    string
	S3Endpoint  string
	S3AccessKey string
	S3SecretKey string
	S3Bucket    string
	S3Secure    bool

	WorkspaceRoot           string
	PDFInfoBinary           string
	PDFToPPMBinary          string
	ProcessorOwnerID        string
	OwnerLeaseDuration      time.Duration
	WorkspaceCleanupTimeout time.Duration

	ClamAVNetwork          string
	ClamAVAddress          string
	ClamAVDialTimeout      time.Duration
	ClamAVOperationTimeout time.Duration
	ClamAVChunkSize        int
	ClamAVResponseLimit    int

	ReconciliationMaxItems   int
	ReconciliationMaxRuntime time.Duration
	ReconciliationRetryDelay time.Duration
	ProcessorMaxAttempts     int64
	ProcessorJobTTL          time.Duration
	Limits                   attachments.Limits
}

func (config contentProcessorConfig) validate() error {
	if strings.TrimSpace(config.DatabaseURL) == "" ||
		attachments.ValidateProcessorWorkspaceRoot(config.WorkspaceRoot) != nil ||
		!filepath.IsAbs(config.PDFInfoBinary) || !filepath.IsAbs(config.PDFToPPMBinary) ||
		!validProcessorOwnerID(config.ProcessorOwnerID) ||
		config.OwnerLeaseDuration <= 0 || config.OwnerLeaseDuration > 24*time.Hour ||
		config.WorkspaceCleanupTimeout <= 0 || config.WorkspaceCleanupTimeout > time.Hour ||
		config.ReconciliationMaxItems <= 0 || config.ReconciliationMaxItems > 10_000 ||
		config.ReconciliationMaxRuntime <= 0 || config.ReconciliationMaxRuntime > 24*time.Hour ||
		config.ReconciliationRetryDelay <= 0 || config.ReconciliationRetryDelay > time.Hour ||
		config.ProcessorMaxAttempts <= 0 || config.ProcessorMaxAttempts > 100 ||
		config.ProcessorJobTTL < config.OwnerLeaseDuration || config.ProcessorJobTTL > 30*24*time.Hour ||
		config.Limits.Validate() != nil {
		return errors.New("invalid content processor configuration")
	}
	switch config.BlobBackend {
	case attachments.BackendKindLocal:
		if !filepath.IsAbs(config.BlobRoot) || isBroadProcessorPath(config.BlobRoot) {
			return errors.New("invalid local Blob configuration")
		}
	case attachments.BackendKindS3:
		if strings.TrimSpace(config.S3Endpoint) != config.S3Endpoint || config.S3Endpoint == "" ||
			strings.Contains(config.S3Endpoint, "://") || strings.TrimSpace(config.S3AccessKey) == "" ||
			strings.TrimSpace(config.S3SecretKey) == "" || strings.TrimSpace(config.S3Bucket) != config.S3Bucket ||
			config.S3Bucket == "" {
			return errors.New("invalid S3 Blob configuration")
		}
	default:
		return errors.New("invalid Blob backend configuration")
	}
	if config.ClamAVAddress == "" {
		if config.ClamAVNetwork != "" {
			return errors.New("invalid ClamAV configuration")
		}
		return nil
	}
	if (config.ClamAVNetwork != "tcp" && config.ClamAVNetwork != "unix") ||
		config.ClamAVDialTimeout <= 0 || config.ClamAVOperationTimeout <= 0 ||
		config.ClamAVChunkSize <= 0 || config.ClamAVResponseLimit <= 0 {
		return errors.New("invalid ClamAV configuration")
	}
	return nil
}

type processorPostgresDB interface {
	Close()
	Pool() *pgxpool.Pool
}

type pgxProcessorPostgresDB struct {
	pool *pgxpool.Pool
}

func (database pgxProcessorPostgresDB) Close() {
	if database.pool != nil {
		database.pool.Close()
	}
}
func (database pgxProcessorPostgresDB) Pool() *pgxpool.Pool { return database.pool }

type processorWorkspace interface {
	Process(context.Context, attachments.ProcessorWorkspaceProcessRequest) (
		attachments.PreviewArtifact,
		attachments.ProcessorWorkspacePurgeReceipt,
		error,
	)
}

type processorWorker interface {
	Run(context.Context) error
}

type processorReconciler interface {
	RunOnce(context.Context) (bool, error)
}

type processorReconcilerGroup struct {
	reconcilers []processorReconciler
}

func newProcessorReconcilerGroup(reconcilers ...processorReconciler) (*processorReconcilerGroup, error) {
	group := &processorReconcilerGroup{}
	for _, reconciler := range reconcilers {
		if nilProcessorDependency(reconciler) {
			continue
		}
		group.reconcilers = append(group.reconcilers, reconciler)
	}
	if len(group.reconcilers) == 0 {
		return nil, errors.New("content processor has no reconcilers")
	}
	return group, nil
}

func (group *processorReconcilerGroup) RunOnce(ctx context.Context) (bool, error) {
	if ctx == nil || group == nil || len(group.reconcilers) == 0 {
		return false, errors.New("invalid content processor reconciler group")
	}
	for _, reconciler := range group.reconcilers {
		claimed, err := reconciler.RunOnce(ctx)
		if err != nil || claimed {
			return claimed, err
		}
	}
	return false, nil
}

type processorBootstrapDeps struct {
	openPostgres             func(context.Context, string) (processorPostgresDB, error)
	newAttachmentRepository  func(*pgxpool.Pool) attachments.ProcessorRepository
	newBlobStore             func(contentProcessorConfig) (attachments.BlobStore, error)
	newScanner               func(contentProcessorConfig) (attachments.ProcessorScanner, error)
	newPreviewProcessor      func(contentProcessorConfig) (*attachments.PreviewProcessor, error)
	newWorkspace             func(contentProcessorConfig, attachments.ProcessorRepository, *attachments.PreviewProcessor) (processorWorkspace, error)
	newWorkspaceReconciler   func(attachments.ProcessorRepository, contentProcessorConfig) (processorReconciler, error)
	newPublicationReconciler func(attachments.ProcessorRepository, attachments.BlobStore, contentProcessorConfig) (processorReconciler, error)
	newReconciler            func(attachments.ProcessorRepository, attachments.TemporaryObjectStore, contentProcessorConfig) (processorReconciler, error)
	newWorker                func(attachments.ProcessorRepository, attachments.BlobStore, processorWorkspace, contentProcessorConfig, attachments.ProcessorScanner) (processorWorker, error)
	now                      func() time.Time
	sleep                    func(context.Context, time.Duration) error
	logger                   *slog.Logger
	cutpoint                 func(string) error
}

func (dependencies processorBootstrapDeps) withDefaults() processorBootstrapDeps {
	if dependencies.openPostgres == nil {
		dependencies.openPostgres = func(ctx context.Context, databaseURL string) (processorPostgresDB, error) {
			pool, err := store.OpenPostgres(ctx, databaseURL)
			if err != nil {
				return nil, err
			}
			return pgxProcessorPostgresDB{pool: pool}, nil
		}
	}
	if dependencies.newAttachmentRepository == nil {
		dependencies.newAttachmentRepository = func(pool *pgxpool.Pool) attachments.ProcessorRepository {
			return store.NewPostgresAttachmentRepository(pool)
		}
	}
	if dependencies.newBlobStore == nil {
		dependencies.newBlobStore = newProcessorBlobStore
	}
	if dependencies.newScanner == nil {
		dependencies.newScanner = newProcessorScanner
	}
	if dependencies.newPreviewProcessor == nil {
		dependencies.newPreviewProcessor = func(config contentProcessorConfig) (*attachments.PreviewProcessor, error) {
			admissionLimits := attachments.DefaultAdmissionLimits(config.Limits)
			return attachments.NewPreviewProcessor(attachments.PreviewConfig{
				MaxSourceBytes: config.Limits.MaxFileBytes,
				MaxOutputBytes: config.Limits.MaxInlineTextPreviewBytes,
				MaxImagePixels: admissionLimits.MaxImagePixels,
				PDFInfoBinary:  config.PDFInfoBinary,
				PDFToPPMBinary: config.PDFToPPMBinary,
			})
		}
	}
	if dependencies.newWorkspace == nil {
		dependencies.newWorkspace = func(
			config contentProcessorConfig,
			repository attachments.ProcessorRepository,
			preview *attachments.PreviewProcessor,
		) (processorWorkspace, error) {
			return attachments.NewContentProcessorWorkspace(attachments.ContentProcessorWorkspaceConfig{
				Root: config.WorkspaceRoot, MaxSourceBytes: config.Limits.MaxFileBytes,
				CleanupTimeout: config.WorkspaceCleanupTimeout,
				Cutpoint: func(cutpoint attachments.ProcessorWorkspaceCutpoint) error {
					return invokeProcessorCutpoint(dependencies.cutpoint, string(cutpoint))
				},
			}, repository, preview)
		}
	}
	if dependencies.newWorkspaceReconciler == nil {
		dependencies.newWorkspaceReconciler = func(
			repository attachments.ProcessorRepository,
			config contentProcessorConfig,
		) (processorReconciler, error) {
			cleanupRepository, ok := repository.(attachments.ProcessorWorkspaceReconcilerRepository)
			if !ok || nilProcessorDependency(cleanupRepository) {
				return nil, errors.New("attachment repository does not support workspace reconciliation")
			}
			return attachments.NewProcessorWorkspaceReconciler(
				cleanupRepository,
				attachments.ProcessorWorkspaceReconcilerConfig{
					Root: config.WorkspaceRoot, CleanupTimeout: config.WorkspaceCleanupTimeout,
					RetryDelay: config.ReconciliationRetryDelay,
					Cutpoint: func(cutpoint attachments.ProcessorWorkspaceCutpoint) error {
						return invokeProcessorCutpoint(dependencies.cutpoint, string(cutpoint))
					},
				},
			)
		}
	}
	if dependencies.newPublicationReconciler == nil {
		dependencies.newPublicationReconciler = func(
			repository attachments.ProcessorRepository,
			blob attachments.BlobStore,
			config contentProcessorConfig,
		) (processorReconciler, error) {
			cleanupRepository, ok := repository.(attachments.BlobPublicationCleanupRepository)
			if !ok || nilProcessorDependency(cleanupRepository) {
				return nil, errors.New("attachment repository does not support Blob publication cleanup")
			}
			resolver, ok := blob.(attachments.BlobPublicationResolver)
			if !ok || nilProcessorDependency(resolver) {
				return nil, errors.New("Blob store does not support final-object publication resolution")
			}
			return attachments.NewBlobPublicationReconciler(
				cleanupRepository,
				resolver,
				blob,
				attachments.BlobPublicationReconcilerConfig{
					ProjectID:          "default",
					BackendKind:        config.BlobBackend,
					OwnerLeaseDuration: attachments.DefaultBlobPublicationCleanupLeaseDuration,
					RetryDelay:         config.ReconciliationRetryDelay,
					Cutpoint: func(cutpoint attachments.BlobPublicationReconcilerCutpoint) error {
						return invokeProcessorCutpoint(dependencies.cutpoint, string(cutpoint))
					},
				},
			)
		}
	}
	if dependencies.newReconciler == nil {
		dependencies.newReconciler = func(
			repository attachments.ProcessorRepository,
			temporary attachments.TemporaryObjectStore,
			config contentProcessorConfig,
		) (processorReconciler, error) {
			cleanupRepository, ok := repository.(attachments.TemporaryObjectReconcilerRepository)
			if !ok || nilProcessorDependency(cleanupRepository) {
				return nil, errors.New("attachment repository does not support temporary object reconciliation")
			}
			return attachments.NewTemporaryObjectReconciler(
				cleanupRepository,
				temporary,
				attachments.TemporaryObjectReconcilerConfig{
					ProjectID: "default", RetryDelay: config.ReconciliationRetryDelay,
					Cutpoint: func(cutpoint attachments.TemporaryObjectReconcilerCutpoint) error {
						return invokeProcessorCutpoint(dependencies.cutpoint, string(cutpoint))
					},
				},
			)
		}
	}
	if dependencies.newWorker == nil {
		dependencies.newWorker = func(
			repository attachments.ProcessorRepository,
			blob attachments.BlobStore,
			workspace processorWorkspace,
			config contentProcessorConfig,
			scanner attachments.ProcessorScanner,
		) (processorWorker, error) {
			return attachments.NewProcessorWorker(repository, blob, workspace, attachments.ProcessorWorkerConfig{
				OwnerID: config.ProcessorOwnerID, OwnerLeaseDuration: config.OwnerLeaseDuration,
				Limits: config.Limits, AdmissionLimits: attachments.DefaultAdmissionLimits(config.Limits),
				PreviewBackendKind: config.BlobBackend, Scan: scanner,
				Cutpoint: func(cutpoint attachments.ProcessorWorkerCutpoint) error {
					return invokeProcessorCutpoint(dependencies.cutpoint, string(cutpoint))
				},
			})
		}
	}
	if dependencies.now == nil {
		dependencies.now = time.Now
	}
	if dependencies.sleep == nil {
		dependencies.sleep = sleepContentProcessor
	}
	if dependencies.logger == nil {
		dependencies.logger = slog.Default()
	}
	return dependencies
}

type contentProcessorRuntime struct {
	reconciler processorReconciler
	worker     processorWorker
	config     contentProcessorConfig
	now        func() time.Time
	sleep      func(context.Context, time.Duration) error
	logger     *slog.Logger
}

func bootstrapContentProcessor(
	ctx context.Context,
	config contentProcessorConfig,
	dependencies processorBootstrapDeps,
) (*contentProcessorRuntime, func(), error) {
	if ctx == nil || config.validate() != nil {
		return nil, nil, errors.New("invalid content processor bootstrap")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	dependencies = dependencies.withDefaults()
	database, err := dependencies.openPostgres(ctx, config.DatabaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("open content processor postgres: %w", err)
	}
	if nilProcessorDependency(database) {
		return nil, nil, errors.New("open content processor postgres returned nil database")
	}
	closeDatabase := true
	defer func() {
		if closeDatabase {
			database.Close()
		}
	}()

	repository := dependencies.newAttachmentRepository(database.Pool())
	if nilProcessorDependency(repository) {
		return nil, nil, errors.New("create content processor repository")
	}
	blob, err := dependencies.newBlobStore(config)
	if err != nil {
		return nil, nil, fmt.Errorf("create content processor Blob store: %w", err)
	}
	if nilProcessorDependency(blob) {
		return nil, nil, errors.New("create content processor Blob store returned nil")
	}
	scanner, err := dependencies.newScanner(config)
	if err != nil {
		return nil, nil, fmt.Errorf("create content processor scanner: %w", err)
	}
	preview, err := dependencies.newPreviewProcessor(config)
	if err != nil {
		return nil, nil, fmt.Errorf("create content processor preview: %w", err)
	}
	if preview == nil {
		return nil, nil, errors.New("create content processor preview returned nil")
	}
	workspace, err := dependencies.newWorkspace(config, repository, preview)
	if err != nil {
		return nil, nil, fmt.Errorf("create content processor workspace: %w", err)
	}
	if nilProcessorDependency(workspace) {
		return nil, nil, errors.New("create content processor workspace returned nil")
	}

	workspaceReconciler, err := dependencies.newWorkspaceReconciler(repository, config)
	if err != nil {
		return nil, nil, fmt.Errorf("create processor workspace reconciler: %w", err)
	}
	if nilProcessorDependency(workspaceReconciler) {
		return nil, nil, errors.New("create processor workspace reconciler returned nil")
	}
	publicationReconciler, err := dependencies.newPublicationReconciler(repository, blob, config)
	if err != nil {
		return nil, nil, fmt.Errorf("create Blob publication reconciler: %w", err)
	}
	if nilProcessorDependency(publicationReconciler) {
		return nil, nil, errors.New("create Blob publication reconciler returned nil")
	}
	var temporaryReconciler processorReconciler
	if config.BlobBackend == attachments.BackendKindS3 {
		temporary, ok := blob.(attachments.TemporaryObjectStore)
		if !ok || nilProcessorDependency(temporary) {
			return nil, nil, errors.New("S3 Blob store does not support temporary object reconciliation")
		}
		temporaryReconciler, err = dependencies.newReconciler(repository, temporary, config)
		if err != nil {
			return nil, nil, fmt.Errorf("create temporary object reconciler: %w", err)
		}
		if nilProcessorDependency(temporaryReconciler) {
			return nil, nil, errors.New("create temporary object reconciler returned nil")
		}
	}
	reconciler, err := newProcessorReconcilerGroup(workspaceReconciler, publicationReconciler, temporaryReconciler)
	if err != nil {
		return nil, nil, err
	}
	worker, err := dependencies.newWorker(repository, blob, workspace, config, scanner)
	if err != nil {
		return nil, nil, fmt.Errorf("create content processor worker: %w", err)
	}
	if nilProcessorDependency(worker) {
		return nil, nil, errors.New("create content processor worker returned nil")
	}

	runtime := &contentProcessorRuntime{
		reconciler: reconciler, worker: worker, config: config,
		now: dependencies.now, sleep: dependencies.sleep, logger: dependencies.logger,
	}
	var closeOnce sync.Once
	cleanup := func() {
		closeOnce.Do(database.Close)
	}
	closeDatabase = false
	return runtime, cleanup, nil
}

func (runtime *contentProcessorRuntime) Run(ctx context.Context) error {
	if ctx == nil || runtime == nil || nilProcessorDependency(runtime.worker) ||
		runtime.config.validate() != nil || runtime.now == nil || runtime.sleep == nil || runtime.logger == nil {
		return errors.New("invalid content processor runtime")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := runtime.runStartupReconciliation(ctx); err != nil {
		return err
	}
	if nilProcessorDependency(runtime.reconciler) {
		return runtime.worker.Run(ctx)
	}
	runContext, cancel := context.WithCancel(ctx)
	reconciliationDone := make(chan struct{})
	go func() {
		defer close(reconciliationDone)
		runtime.runContinuousReconciliation(runContext)
	}()
	err := runtime.worker.Run(runContext)
	cancel()
	<-reconciliationDone
	return err
}

func (runtime *contentProcessorRuntime) runStartupReconciliation(ctx context.Context) error {
	if nilProcessorDependency(runtime.reconciler) {
		return nil
	}
	startupContext, cancel := context.WithTimeout(ctx, runtime.config.ReconciliationMaxRuntime)
	defer cancel()
	deadline := runtime.now().Add(runtime.config.ReconciliationMaxRuntime)
	for attempt := 0; attempt < runtime.config.ReconciliationMaxItems; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if startupContext.Err() != nil {
			return nil
		}
		if !runtime.now().Before(deadline) {
			return nil
		}
		claimed, err := runtime.reconciler.RunOnce(startupContext)
		if err == nil && !claimed {
			return nil
		}
		if err == nil {
			continue
		}
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		if startupContext.Err() != nil {
			return nil
		}
		runtime.logger.Error(
			"content processor startup reconciliation deferred",
			"class", safeProcessorErrorClass(err),
		)
		if err := runtime.sleep(startupContext, runtime.config.ReconciliationRetryDelay); err != nil {
			if contextErr := ctx.Err(); contextErr != nil {
				return contextErr
			}
			if startupContext.Err() != nil {
				return nil
			}
			return err
		}
	}
	return nil
}

func (runtime *contentProcessorRuntime) runContinuousReconciliation(ctx context.Context) {
	for {
		if err := runtime.sleep(ctx, runtime.config.ReconciliationRetryDelay); err != nil {
			return
		}
		_, err := runtime.reconciler.RunOnce(ctx)
		if err == nil {
			continue
		}
		if ctx.Err() != nil {
			return
		}
		runtime.logger.Error(
			"content processor background reconciliation deferred",
			"class", safeProcessorErrorClass(err),
		)
	}
}

func newProcessorBlobStore(config contentProcessorConfig) (attachments.BlobStore, error) {
	switch config.BlobBackend {
	case attachments.BackendKindLocal:
		return attachments.NewLocalBlobStore(config.BlobRoot)
	case attachments.BackendKindS3:
		client, err := minio.New(config.S3Endpoint, &minio.Options{
			Creds:  credentials.NewStaticV4(config.S3AccessKey, config.S3SecretKey, ""),
			Secure: config.S3Secure,
		})
		if err != nil {
			return nil, errors.New("create S3 client")
		}
		return attachments.NewS3BlobStore(client, config.S3Bucket)
	default:
		return nil, errors.New("unsupported content processor Blob backend")
	}
}

func newProcessorScanner(config contentProcessorConfig) (attachments.ProcessorScanner, error) {
	if config.ClamAVAddress == "" {
		return nil, nil
	}
	scanner, err := attachments.NewClamAVScanner(attachments.ClamAVScannerConfig{
		Network: config.ClamAVNetwork, Address: config.ClamAVAddress,
		DialTimeout: config.ClamAVDialTimeout, OperationTimeout: config.ClamAVOperationTimeout,
		ChunkSize: config.ClamAVChunkSize, MaxInputBytes: config.Limits.MaxFileBytes,
		MaxResponseBytes: config.ClamAVResponseLimit,
	})
	if err != nil {
		return nil, err
	}
	return attachments.ProcessorScanner(scanner.Scan), nil
}

func loadContentProcessorConfig() (contentProcessorConfig, error) {
	databaseURL, err := requiredProcessorEnv("HOUFENG_DATABASE_URL")
	if err != nil {
		return contentProcessorConfig{}, err
	}
	backendValue, err := processorEnvOrDefault("HOUFENG_ATTACHMENT_BLOB_BACKEND", string(attachments.BackendKindLocal))
	if err != nil {
		return contentProcessorConfig{}, err
	}
	workspaceRoot, err := requiredProcessorEnv("HOUFENG_CONTENT_PROCESSOR_WORKSPACE_ROOT")
	if err != nil {
		return contentProcessorConfig{}, err
	}
	config := contentProcessorConfig{
		DatabaseURL: databaseURL, BlobBackend: attachments.BackendKind(strings.ToLower(backendValue)),
		WorkspaceRoot:            workspaceRoot,
		PDFInfoBinary:            processorOptionalEnv("HOUFENG_PDFINFO_BINARY", "/usr/bin/pdfinfo"),
		PDFToPPMBinary:           processorOptionalEnv("HOUFENG_PDFTOPPM_BINARY", "/usr/bin/pdftoppm"),
		ProcessorOwnerID:         processorOptionalEnv("HOUFENG_CONTENT_PROCESSOR_OWNER_ID", defaultProcessorOwnerID),
		OwnerLeaseDuration:       defaultProcessorOwnerLeaseDuration,
		WorkspaceCleanupTimeout:  defaultProcessorWorkspaceCleanup,
		ReconciliationMaxItems:   defaultProcessorReconciliationItems,
		ReconciliationMaxRuntime: defaultProcessorReconciliationWindow,
		ReconciliationRetryDelay: defaultProcessorReconciliationRetry,
		ProcessorMaxAttempts:     defaultProcessorMaxAttempts,
		ProcessorJobTTL:          defaultProcessorJobTTL,
		Limits:                   attachments.DefaultLimits(),
	}
	if config.OwnerLeaseDuration, err = processorDurationEnv("HOUFENG_CONTENT_PROCESSOR_LEASE_DURATION", config.OwnerLeaseDuration); err != nil {
		return contentProcessorConfig{}, err
	}
	if config.WorkspaceCleanupTimeout, err = processorDurationEnv("HOUFENG_CONTENT_PROCESSOR_CLEANUP_TIMEOUT", config.WorkspaceCleanupTimeout); err != nil {
		return contentProcessorConfig{}, err
	}
	if config.ReconciliationMaxItems, err = processorIntEnv("HOUFENG_CONTENT_PROCESSOR_RECONCILIATION_MAX_ITEMS", config.ReconciliationMaxItems); err != nil {
		return contentProcessorConfig{}, err
	}
	if config.ReconciliationMaxRuntime, err = processorDurationEnv("HOUFENG_CONTENT_PROCESSOR_RECONCILIATION_MAX_RUNTIME", config.ReconciliationMaxRuntime); err != nil {
		return contentProcessorConfig{}, err
	}
	if config.ReconciliationRetryDelay, err = processorDurationEnv("HOUFENG_CONTENT_PROCESSOR_RECONCILIATION_RETRY_DELAY", config.ReconciliationRetryDelay); err != nil {
		return contentProcessorConfig{}, err
	}
	if config.ProcessorMaxAttempts, err = processorInt64Env("HOUFENG_CONTENT_PROCESSOR_MAX_ATTEMPTS", config.ProcessorMaxAttempts); err != nil {
		return contentProcessorConfig{}, err
	}
	if config.ProcessorJobTTL, err = processorDurationEnv("HOUFENG_CONTENT_PROCESSOR_JOB_TTL", config.ProcessorJobTTL); err != nil {
		return contentProcessorConfig{}, err
	}

	switch config.BlobBackend {
	case attachments.BackendKindLocal:
		config.BlobRoot, err = requiredProcessorEnv("HOUFENG_ATTACHMENT_BLOB_ROOT")
	case attachments.BackendKindS3:
		config.S3Endpoint, err = requiredProcessorEnv("HOUFENG_ATTACHMENT_S3_ENDPOINT")
		if err == nil {
			config.S3AccessKey, err = processorSecretEnvOrFile("HOUFENG_ATTACHMENT_S3_ACCESS_KEY")
		}
		if err == nil {
			config.S3SecretKey, err = processorSecretEnvOrFile("HOUFENG_ATTACHMENT_S3_SECRET_KEY")
		}
		if err == nil {
			config.S3Bucket, err = requiredProcessorEnv("HOUFENG_ATTACHMENT_S3_BUCKET")
		}
		if err == nil {
			config.S3Secure, err = processorBoolEnv("HOUFENG_ATTACHMENT_S3_SECURE", false)
		}
	default:
		err = errors.New("HOUFENG_ATTACHMENT_BLOB_BACKEND must be local or s3")
	}
	if err != nil {
		return contentProcessorConfig{}, err
	}

	config.ClamAVAddress = strings.TrimSpace(os.Getenv("HOUFENG_CLAMAV_ADDRESS"))
	if config.ClamAVAddress != "" {
		config.ClamAVNetwork = processorOptionalEnv("HOUFENG_CLAMAV_NETWORK", "unix")
		config.ClamAVDialTimeout, err = processorDurationEnv("HOUFENG_CLAMAV_DIAL_TIMEOUT", defaultClamAVDialTimeout)
		if err == nil {
			config.ClamAVOperationTimeout, err = processorDurationEnv("HOUFENG_CLAMAV_OPERATION_TIMEOUT", defaultClamAVOperationTimeout)
		}
		if err == nil {
			config.ClamAVChunkSize, err = processorIntEnv("HOUFENG_CLAMAV_CHUNK_SIZE", defaultClamAVChunkSize)
		}
		if err == nil {
			config.ClamAVResponseLimit, err = processorIntEnv("HOUFENG_CLAMAV_RESPONSE_LIMIT", defaultClamAVResponseLimit)
		}
		if err != nil {
			return contentProcessorConfig{}, err
		}
	}
	if err := config.validate(); err != nil {
		return contentProcessorConfig{}, err
	}
	return config, nil
}

func safeProcessorErrorClass(err error) string {
	switch {
	case err == nil:
		return "none"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, attachments.ErrClamAVScannerTimeout):
		return "timeout"
	case errors.Is(err, attachments.ErrBlobNotFound):
		return "object_missing"
	case errors.Is(err, attachments.ErrBlobVersionMismatch),
		errors.Is(err, attachments.ErrTemporaryObjectReconciliationConflict),
		errors.Is(err, attachments.ErrAttachmentConflict):
		return "identity_conflict"
	case errors.Is(err, attachments.ErrArchiveScannerUnavailable),
		errors.Is(err, attachments.ErrClamAVScannerDaemon):
		return "scanner_unavailable"
	case errors.Is(err, attachments.ErrProcessorClaimLost):
		return "claim_lost"
	default:
		return "internal"
	}
}

func invokeProcessorCutpoint(cutpoint func(string) error, name string) error {
	if cutpoint == nil {
		return nil
	}
	return cutpoint(name)
}

func sleepContentProcessor(ctx context.Context, delay time.Duration) error {
	if ctx == nil || delay <= 0 {
		return errors.New("invalid content processor sleep")
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func requiredProcessorEnv(key string) (string, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return "", fmt.Errorf("%s must not be empty", key)
	}
	return value, nil
}

func processorSecretEnvOrFile(key string) (string, error) {
	fileKey := key + "_FILE"
	if path := strings.TrimSpace(os.Getenv(fileKey)); path != "" {
		body, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", fileKey, err)
		}
		value := strings.TrimSpace(string(body))
		if value == "" {
			return "", fmt.Errorf("%s must not be empty", fileKey)
		}
		return value, nil
	}
	return requiredProcessorEnv(key)
}

func processorEnvOrDefault(key, fallback string) (string, error) {
	value, ok := os.LookupEnv(key)
	if !ok {
		value = fallback
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s must not be empty", key)
	}
	return value, nil
}

func processorOptionalEnv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func processorDurationEnv(key string, fallback time.Duration) (time.Duration, error) {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback, nil
	}
	duration, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	return duration, nil
}

func processorIntEnv(key string, fallback int) (int, error) {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	return parsed, nil
}

func processorInt64Env(key string, fallback int64) (int64, error) {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	return parsed, nil
}

func processorBoolEnv(key string, fallback bool) (bool, error) {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(strings.TrimSpace(value))
	if err != nil {
		return false, fmt.Errorf("parse %s: %w", key, err)
	}
	return parsed, nil
}

func validProcessorOwnerID(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') &&
			character != '_' && character != '-' {
			return false
		}
	}
	return true
}

func isBroadProcessorPath(path string) bool {
	cleaned := filepath.Clean(path)
	return cleaned == filepath.VolumeName(cleaned)+string(filepath.Separator) || filepath.Dir(cleaned) == cleaned
}

func nilProcessorDependency(dependency any) bool {
	if dependency == nil {
		return true
	}
	value := reflect.ValueOf(dependency)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
