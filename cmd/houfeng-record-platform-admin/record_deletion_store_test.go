package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
)

func TestRecordDeletionLegacyIdentityBinding(t *testing.T) {
	projection := recordDeletionLegacyProjection{
		DeploymentID: "dp-" + strings.Repeat("1", 64), ProjectID: "default", OperationID: "rpo_operation01",
		ActorID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", ReasonCode: "user_confirmed", DeletionContractVersion: 1,
		OperationState: "provisional_fenced", ObjectKind: "record", ObjectID: "rec_01",
		TokenCommitment: strings.Repeat("2", 64), RequestFingerprint: strings.Repeat("3", 64),
	}
	body, err := json.Marshal(projection)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeRecordDeletionLegacyIdentity(body)
	if err != nil || got.Request.OperationID != projection.OperationID || got.Request.EntryType != recorddeletion.LedgerEntryDeleteCommit {
		t.Fatalf("valid persisted operation rejected: %+v %v", got, err)
	}
	for name, mutate := range map[string]func(*recordDeletionLegacyProjection){
		"new contract":            func(p *recordDeletionLegacyProjection) { p.DeletionContractVersion = 2 },
		"partial ledger position": func(p *recordDeletionLegacyProjection) { p.LedgerSequence = 1 },
		"unknown entry type":      func(p *recordDeletionLegacyProjection) { p.LedgerEntryType = "wrong" },
		"different object kind":   func(p *recordDeletionLegacyProjection) { p.ObjectKind = "vps" },
		"noncanonical token":      func(p *recordDeletionLegacyProjection) { p.TokenCommitment = strings.Repeat("A", 64) },
		"zero fingerprint":        func(p *recordDeletionLegacyProjection) { p.RequestFingerprint = strings.Repeat("0", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			bad := projection
			mutate(&bad)
			encoded, err := json.Marshal(bad)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decodeRecordDeletionLegacyIdentity(encoded); err == nil {
				t.Fatal("invalid persisted operation accepted")
			}
		})
	}
	if _, err := decodeRecordDeletionLegacyIdentity(append(body[:len(body)-1], []byte(`,"operation_id":"rpo_other"}`)...)); err == nil {
		t.Fatal("duplicate operation identity accepted")
	}

	outcome := projection
	outcome.OperationState = "release_pending"
	outcome.ReleaseEpoch = 3
	outcome.LedgerEntryType = "attempt_not_committed"
	outcome.LedgerSequence = 8
	outcome.LedgerEntryHash = strings.Repeat("4", 64)
	body, err = json.Marshal(outcome)
	if err != nil {
		t.Fatal(err)
	}
	got, err = decodeRecordDeletionLegacyIdentity(body)
	if err != nil || got.Request.EntryType != recorddeletion.LedgerEntryAttemptNotCommitted || got.Request.ReleaseEpoch != 3 || got.Request.DeletionContractVersion != 0 {
		t.Fatalf("existing outcome identity rejected: %+v %v", got, err)
	}
}

func TestLegacyInstallerRejectsDifferentDeploymentBeforeSQL(t *testing.T) {
	installer := recordDeletionAdminTx{deployment: recordplatform.DeploymentID("dp-" + strings.Repeat("1", 64)), project: recordplatform.ProjectID("default")}
	wrongDeployment := []byte(`{"deployment_id":"dp-` + strings.Repeat("2", 64) + `","project_id":"default"}`)
	wrongProject := []byte(`{"deployment_id":"dp-` + strings.Repeat("1", 64) + `","project_id":"other"}`)
	for _, payload := range [][]byte{wrongDeployment, wrongProject} {
		if err := installer.InstallVerifiedEntry(context.Background(), payload); err == nil {
			t.Fatal("different deployment or project accepted")
		}
	}
}
