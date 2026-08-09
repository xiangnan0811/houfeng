package main

import (
	"context"
	"os"
	"testing"

	"houfeng/internal/center/attachments"
)

const contentProcessorCrashHelperExitCode = 86

func TestContentProcessorCrashHelper(t *testing.T) {
	if os.Getenv("HOUFENG_CONTENT_PROCESSOR_CRASH_HELPER") != "1" {
		t.Skip("only executed as the content processor crash integration helper")
	}
	phase := os.Getenv("HOUFENG_CONTENT_PROCESSOR_CRASH_PHASE")
	cutpoint := os.Getenv("HOUFENG_CONTENT_PROCESSOR_CRASH_CUTPOINT")
	if phase != "crash" && phase != "recover" {
		t.Fatalf("invalid crash helper phase %q", phase)
	}
	if phase == "crash" && !knownContentProcessorCrashCutpoint(cutpoint) {
		t.Fatalf("invalid crash helper cutpoint %q", cutpoint)
	}
	if phase == "recover" && cutpoint != "" {
		t.Fatalf("recovery helper unexpectedly received cutpoint %q", cutpoint)
	}

	config, err := loadContentProcessorConfig()
	if err != nil {
		t.Fatalf("loadContentProcessorConfig() error = %v", err)
	}
	dependencies := processorBootstrapDeps{}
	if phase == "crash" {
		dependencies.cutpoint = func(current string) error {
			if current == cutpoint {
				os.Exit(contentProcessorCrashHelperExitCode)
			}
			return nil
		}
	}
	runtime, cleanup, err := bootstrapContentProcessor(context.Background(), config, dependencies)
	if err != nil {
		t.Fatalf("bootstrapContentProcessor() error = %v", err)
	}
	defer cleanup()
	if err := runtime.runStartupReconciliation(context.Background()); err != nil {
		t.Fatalf("runStartupReconciliation() error = %v", err)
	}
	worker, ok := runtime.worker.(*attachments.ProcessorWorker)
	if !ok {
		t.Fatalf("content processor worker type = %T", runtime.worker)
	}
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("ProcessorWorker.RunOnce() error = %v", err)
	}
	if phase == "crash" {
		t.Fatalf("crash helper did not reach cutpoint %q", cutpoint)
	}
	if err := runtime.runStartupReconciliation(context.Background()); err != nil {
		t.Fatalf("post-worker reconciliation error = %v", err)
	}
}

func knownContentProcessorCrashCutpoint(value string) bool {
	switch value {
	case string(attachments.ProcessorWorkerCutpointAfterClaim),
		string(attachments.ProcessorWorkspaceCutpointAfterMkdir),
		string(attachments.ProcessorWorkspaceCutpointAfterSourceMaterialization),
		string(attachments.ProcessorWorkspaceCutpointAfterProcessing),
		string(attachments.ProcessorWorkerCutpointAfterResultCommit),
		string(attachments.ProcessorWorkspaceCutpointAfterPhysicalPurge):
		return true
	default:
		return false
	}
}
