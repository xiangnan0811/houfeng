package attachments

import (
	"bytes"
	"context"
	"reflect"
	"testing"
	"time"
)

func TestProcessorWorkerCutpointsCoverClaimAndResultCommit(t *testing.T) {
	content := []byte("cutpoint worker content")
	claim := workerTestClaim(content, ProcessorProfileText)
	repository := &workerRepositoryStub{claim: &claim}
	workspace := &workerWorkspaceStub{artifact: PreviewArtifact{
		HasPreview: true, MediaType: ManagedPreviewMediaTypeTextUTF8, Bytes: content,
	}}
	var got []ProcessorWorkerCutpoint
	worker, err := NewProcessorWorker(repository, &workerBlobStub{data: content}, workspace, ProcessorWorkerConfig{
		OwnerID: "worker1", OwnerLeaseDuration: time.Minute, Limits: DefaultLimits(), PreviewBackendKind: BackendKindS3,
		Cutpoint: func(cutpoint ProcessorWorkerCutpoint) error {
			got = append(got, cutpoint)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	want := []ProcessorWorkerCutpoint{
		ProcessorWorkerCutpointAfterClaim,
		ProcessorWorkerCutpointAfterResultCommit,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("worker cutpoints = %#v, want %#v", got, want)
	}
}

func TestContentProcessorWorkspaceCutpointsCoverFilesystemAndProcessing(t *testing.T) {
	root := t.TempDir() + "/processor-root"
	repository := newFakeProcessorWorkspaceRepository()
	var got []ProcessorWorkspaceCutpoint
	workspace, err := NewContentProcessorWorkspace(ContentProcessorWorkspaceConfig{
		Root: root, MaxSourceBytes: 1024, CleanupTimeout: time.Second,
		Cutpoint: func(cutpoint ProcessorWorkspaceCutpoint) error {
			got = append(got, cutpoint)
			return nil
		},
	}, repository, workspaceTestPreviewProcessor())
	if err != nil {
		t.Fatalf("NewContentProcessorWorkspace() error = %v", err)
	}
	source := []byte("cutpoint workspace content")
	claim := workspaceTestClaim(source, ProcessorProfileText)
	if _, _, err := workspace.Process(context.Background(), ProcessorWorkspaceProcessRequest{
		Claim: claim, WorkspaceID: "cpw_cutpoints1", ExpiresAt: claim.LeaseExpiresAt,
		Source: bytes.NewReader(source),
	}); err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	want := []ProcessorWorkspaceCutpoint{
		ProcessorWorkspaceCutpointAfterMkdir,
		ProcessorWorkspaceCutpointAfterSourceMaterialization,
		ProcessorWorkspaceCutpointAfterProcessing,
		ProcessorWorkspaceCutpointAfterPhysicalPurge,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("workspace cutpoints = %#v, want %#v", got, want)
	}
}
