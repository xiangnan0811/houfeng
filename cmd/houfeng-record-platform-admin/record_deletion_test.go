package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func validRecordDeletionRequest(t *testing.T) recordDeletionRequest {
	t.Helper()
	for _, env := range os.Environ() {
		name, value, ok := strings.Cut(env, "=")
		if ok && value != "" && (strings.HasPrefix(name, "PG") || name == "SSL_CERT_FILE" || name == "SSL_CERT_DIR") {
			t.Setenv(name, "")
		}
	}
	url := func(user, database string) string {
		return "postgres://" + user + ":secret-" + user + "@127.0.0.1:5432/" + database + "?sslmode=disable"
	}
	primary := recordDeletionDomainRequest{Identity: recordDeletionPhysicalIdentity{ID: "rd-" + strings.Repeat("1", 64), Kind: "deletion_ledger", SystemIdentifier: "101", DatabaseName: "ledger", DatabaseOID: 101, Epoch: 1},
		BootstrapURL: url("postgres", "ledger"), ReaderURL: url("ledger_reader", "ledger"), WriterURL: url("ledger_writer", "ledger"), PreviousWriters: []string{}}
	witness := recordDeletionDomainRequest{Identity: recordDeletionPhysicalIdentity{ID: "rd-" + strings.Repeat("2", 64), Kind: "deletion_witness", SystemIdentifier: "102", DatabaseName: "witness", DatabaseOID: 102, Epoch: 1},
		BootstrapURL: url("postgres", "witness"), ReaderURL: url("witness_reader", "witness"), WriterURL: url("witness_writer", "witness"), PreviousWriters: []string{}}
	return recordDeletionRequest{DeploymentID: "dp-" + strings.Repeat("3", 64), ProjectID: "default", OperationID: "rpo_operation01", AppAdminURL: strings.Replace(url("app_admin", "application"), "secret-app_admin", strings.Repeat("f", 40), 1),
		AppPreviousAdminURL: strings.Replace(url("app_admin", "application"), "secret-app_admin", "old-admin-secret", 1),
		Fence: recordDeletionFenceRequest{Compose: recordDeletionComposeRequest{RecoveryID: "recover_01", Project: "project", WorkingDirectory: "/work", DatabaseContainerID: strings.Repeat("a", 64), ControlConfigPath: "/private/source.json"},
			AppBootstrapURL: url("postgres", "application"), AppIdentity: recordDeletionApplicationIdentity{ID: "rd-" + strings.Repeat("4", 64), SystemIdentifier: "103", DatabaseName: "application", DatabaseOID: 103, Epoch: 1},
			AppAdminRole: "app_admin", AppPreviousLogins: []string{"old_admin"}, Primary: primary, Witness: witness,
			ComposeContainerIDs: []string{strings.Repeat("a", 64)}, HostCgroups: []string{"/sys/fs/cgroup/houfeng-fixture"}}}
}

func TestRecordDeletionRequestRequiresExistingIdentityOrRecoveryLocator(t *testing.T) {
	request := validRecordDeletionRequest(t)
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var decoded recordDeletionRequest
	if err := decodeSourceEnrollmentRequest(body, &decoded, recordDeletionRequestSchema); err != nil || decoded.validate() != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	for name, mutate := range map[string]func(*recordDeletionRequest){
		"both identities":               func(r *recordDeletionRequest) { r.LedgerSequence = 1 },
		"missing operation and locator": func(r *recordDeletionRequest) { r.OperationID = "" },
		"same ledger physical system": func(r *recordDeletionRequest) {
			r.Fence.Witness.Identity.SystemIdentifier = r.Fence.Primary.Identity.SystemIdentifier
		},
		"reused admin password": func(r *recordDeletionRequest) { r.AppPreviousAdminURL = r.AppAdminURL },
		"short fresh password": func(r *recordDeletionRequest) {
			r.AppAdminURL = strings.Replace(r.AppPreviousAdminURL, "old-admin-secret", "small", 1)
		},
		"different admin identity": func(r *recordDeletionRequest) { r.Fence.AppAdminRole = "other_admin" },
		"invalid operation":        func(r *recordDeletionRequest) { r.OperationID = "rpo_invalid!" },
		"missing cgroup":           func(r *recordDeletionRequest) { r.Fence.HostCgroups = nil },
		"invalid cgroup path":      func(r *recordDeletionRequest) { r.Fence.HostCgroups = []string{"/sys/fs/cgroup/../unrelated"} },
		"ambient URL option":       func(r *recordDeletionRequest) { r.Fence.Primary.ReaderURL += "&options=-crole%3Dpostgres" },
	} {
		t.Run(name, func(t *testing.T) {
			bad := request
			mutate(&bad)
			if err := bad.validate(); err == nil {
				t.Fatal("unsafe request accepted")
			}
		})
	}
	badBody := strings.Replace(string(body), `"operation_id":"rpo_operation01"`, `"operation_id":"rpo_operation01","operation_id":"rpo_other"`, 1)
	if err := decodeSourceEnrollmentRequest([]byte(badBody), &decoded, recordDeletionRequestSchema); err == nil {
		t.Fatal("duplicate request identity accepted")
	}
	recovery := request
	recovery.OperationID = ""
	recovery.LedgerSequence = 1
	recovery.LedgerEntryHash = strings.Repeat("a", 64)
	recovery.PreviousHash = strings.Repeat("0", 64)
	if err := recovery.validate(); err != nil {
		t.Fatalf("valid recovery locator rejected: %v", err)
	}
	recovery.PreviousHash = strings.Repeat("b", 64)
	if err := recovery.validate(); err == nil {
		t.Fatal("nonzero genesis predecessor accepted")
	}
}

func TestRecordDeletionErrorPreservesOnlyBoundedSQLIdentity(t *testing.T) {
	constraint := "record_core_purge_receipts_recovery_failure_test"
	err := safeRecordDeletionAdminError(fmt.Errorf("outer secret: %w", &pgconn.PgError{Code: "23514", ConstraintName: constraint, Message: "credential secret", Detail: "private DSN"}))
	message := err.Error()
	if !strings.Contains(message, "23514") || !strings.Contains(message, constraint) || strings.Contains(message, "secret") || strings.Contains(message, "DSN") {
		t.Fatalf("unsafe or incomplete SQL diagnostic: %s", message)
	}
	err = safeRecordDeletionAdminError(&pgconn.PgError{Code: "23514", ConstraintName: "private credential secret"})
	if strings.Contains(err.Error(), "secret") || !strings.Contains(err.Error(), "23514") {
		t.Fatalf("invalid constraint identifier leaked: %s", err)
	}
	err = safeRecordDeletionAdminError(&pgconn.PgError{Code: "bad code", Message: "secret"})
	if err != errRecordDeletionAdmin {
		t.Fatalf("invalid SQLSTATE accepted: %v", err)
	}
}
