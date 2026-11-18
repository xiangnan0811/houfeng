package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"houfeng/internal/center/deploy"
)

func TestLegacyC81MigrationControlledResults(t *testing.T) {
	base := legacyC80TestRequest(t)
	body, err := json.Marshal(legacyC81MigrationManagement{AppAdminURL: base.AppAdminURL, AppPreviousAdminURL: base.AppPreviousAdminURL, Fence: base.Fence})
	if err != nil {
		t.Fatal(err)
	}
	path := legacyC80TestRequestFile(t, body)
	args := []string{"record-deletion", "legacy-c81-migrate", "--request-file", path}
	digest := strings.Repeat("a", 64)
	for _, tc := range []struct {
		name      string
		result    deploy.LegacyC81MigrationResult
		failure   error
		status    string
		wantError bool
	}{
		{name: "committed", result: deploy.LegacyC81MigrationResult{ManifestDigest: digest}, status: "converged"},
		{name: "recovered", result: deploy.LegacyC81MigrationResult{ManifestDigest: digest, Recovered: true}, status: "recovered_converged"},
		{name: "ambiguous error", result: deploy.LegacyC81MigrationResult{ManifestDigest: digest}, failure: errors.New("private database error"), status: "unavailable", wantError: true},
		{name: "zero digest", result: deploy.LegacyC81MigrationResult{ManifestDigest: strings.Repeat("0", 64)}, status: "unavailable", wantError: true},
		{name: "noncanonical digest", result: deploy.LegacyC81MigrationResult{ManifestDigest: strings.Repeat("A", 64)}, status: "unavailable", wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			calls := 0
			maintenance := func(_ context.Context, got deploy.RecordLedgerMaintenanceRequest) (deploy.LegacyC81MigrationResult, error) {
				calls++
				if got.Compose.RecoveryID != base.Fence.Compose.RecoveryID || len(got.AppPreviousLogins) != len(base.Fence.AppPreviousLogins) {
					t.Fatal("maintenance request changed")
				}
				return tc.result, tc.failure
			}
			err := runLegacyC81MigrationWith(context.Background(), args, &output, func() error { return nil }, maintenance)
			var got legacyC81MigrationResponse
			if json.Unmarshal(output.Bytes(), &got) != nil || got.Version != 1 || got.Status != tc.status || calls != 1 {
				t.Fatalf("unexpected response: %q, calls=%d", output.String(), calls)
			}
			if tc.wantError {
				var handled *legacyC81HandledResultError
				if !errors.As(err, &handled) || got.ManifestDigest != "" {
					t.Fatalf("failure was not controlled: %v %q", err, output.String())
				}
			} else if err != nil || got.ManifestDigest != digest {
				t.Fatalf("unexpected success response: %v %q", err, output.String())
			}
			if strings.Contains(output.String(), "private") {
				t.Fatal("private error leaked")
			}
		})
	}
}

func TestLegacyC81MigrationRejectsNonExactPrivateRequest(t *testing.T) {
	base := legacyC80TestRequest(t)
	body, err := json.Marshal(legacyC81MigrationManagement{AppAdminURL: base.AppAdminURL, AppPreviousAdminURL: base.AppPreviousAdminURL, Fence: base.Fence})
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range [][]byte{
		append(append([]byte(nil), body...), []byte(` {}`)...),
		[]byte(strings.Replace(string(body), `"app_admin_url":`, `"app_admin_url":"private","app_admin_url":`, 1)),
		[]byte(strings.Replace(string(body), `"fence":`, `"unexpected":"private","fence":`, 1)),
	} {
		path := legacyC80TestRequestFile(t, invalid)
		var output bytes.Buffer
		calls := 0
		err := runLegacyC81MigrationWith(context.Background(), []string{"record-deletion", "legacy-c81-migrate", "--request-file", path}, &output, func() error { return nil },
			func(context.Context, deploy.RecordLedgerMaintenanceRequest) (deploy.LegacyC81MigrationResult, error) {
				calls++
				return deploy.LegacyC81MigrationResult{}, nil
			})
		var handled *legacyC81HandledResultError
		if !errors.As(err, &handled) || calls != 0 || output.String() != "{\"version\":1,\"status\":\"unavailable\",\"manifest_digest\":\"\"}\n" {
			t.Fatalf("invalid request admitted: %v %q", err, output.String())
		}
	}
}
