package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"houfeng/internal/center/deploy"
)

func legacyC80TestRequest(t *testing.T) legacyC80ServiceManagement {
	t.Helper()
	fence := validRecordDeletionRequest(t)
	return legacyC80ServiceManagement{
		ActorUserID:         "usr_" + strings.Repeat("a", 24),
		RecordID:            "rec_0123456789abcdef",
		AppAdminURL:         fence.AppAdminURL,
		AppPreviousAdminURL: fence.AppPreviousAdminURL,
		Fence:               fence.Fence,
	}
}

func legacyC80TestRequestFile(t *testing.T, body []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "request.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func legacyC80TestArgs(path string) []string {
	return []string{"record-deletion", "legacy-c80-service", "--request-file", path}
}

func legacyC80RequireHandled(t *testing.T, err error) {
	t.Helper()
	var handled *legacyC80HandledResultError
	if !errors.As(err, &handled) {
		t.Fatalf("expected controlled failure, got %v", err)
	}
}

func TestLegacyC80ServiceExactResultsAndAuthority(t *testing.T) {
	request := legacyC80TestRequest(t)
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	path := legacyC80TestRequestFile(t, body)
	const operation = "rpo_0123456789abcdef"
	cases := []struct {
		name    string
		result  deploy.LegacyC80ServiceResult
		failure error
		frame   string
		handled bool
	}{
		{"committed", deploy.LegacyC80ServiceResult{Status: "committed", OperationID: operation, Reason: "app_reserved"}, nil,
			`{"version":1,"status":"committed","operation_id":"rpo_0123456789abcdef","reason":"app_reserved"}` + "\n", false},
		{"rejected", deploy.LegacyC80ServiceResult{Status: "rejected", Reason: "source_denied"}, nil,
			`{"version":1,"status":"rejected","operation_id":"","reason":"source_denied"}` + "\n", false},
		{"recovered original operation", deploy.LegacyC80ServiceResult{Status: "recovered_committed", OperationID: operation, Reason: "reserve_committed"}, nil,
			`{"version":1,"status":"recovered_committed","operation_id":"rpo_0123456789abcdef","reason":"reserve_committed"}` + "\n", false},
		{"inventory unavailable", deploy.LegacyC80ServiceResult{Status: "unavailable", Reason: "inventory_unavailable"}, nil,
			`{"version":1,"status":"unavailable","operation_id":"","reason":"inventory_unavailable"}` + "\n", true},
		{"admission unavailable", deploy.LegacyC80ServiceResult{Status: "unavailable", Reason: "admission_unavailable"}, nil,
			`{"version":1,"status":"unavailable","operation_id":"","reason":"admission_unavailable"}` + "\n", true},
		{"service unavailable", deploy.LegacyC80ServiceResult{Status: "unavailable", Reason: "service_unavailable"}, nil,
			`{"version":1,"status":"unavailable","operation_id":"","reason":"service_unavailable"}` + "\n", true},
		{"realm unavailable", deploy.LegacyC80ServiceResult{Status: "unavailable", Reason: "realm_unavailable"}, errors.New("secret DSN"),
			`{"version":1,"status":"unavailable","operation_id":"","reason":"realm_unavailable"}` + "\n", true},
		{"unexpected session", deploy.LegacyC80ServiceResult{Status: "unavailable", Reason: "unexpected_session"}, errors.New("secret DSN"),
			`{"version":1,"status":"unavailable","operation_id":"","reason":"unexpected_session"}` + "\n", true},
		{"unknown failure", deploy.LegacyC80ServiceResult{}, errors.New("secret DSN"),
			`{"version":1,"status":"unavailable","operation_id":"","reason":"service_unavailable"}` + "\n", true},
		{"failure-only reason without error", deploy.LegacyC80ServiceResult{Status: "unavailable", Reason: "realm_unavailable"}, nil,
			`{"version":1,"status":"unavailable","operation_id":"","reason":"service_unavailable"}` + "\n", true},
		{"unavailable with operation", deploy.LegacyC80ServiceResult{Status: "unavailable", OperationID: operation, Reason: "inventory_unavailable"}, nil,
			`{"version":1,"status":"unavailable","operation_id":"","reason":"service_unavailable"}` + "\n", true},
		{"untrusted failure reason", deploy.LegacyC80ServiceResult{Status: "unavailable", Reason: "secret DSN"}, errors.New("secret DSN"),
			`{"version":1,"status":"unavailable","operation_id":"","reason":"service_unavailable"}` + "\n", true},
		{"error with purported commit", deploy.LegacyC80ServiceResult{Status: "committed", OperationID: operation, Reason: "app_reserved"}, errors.New("secret DSN"),
			`{"version":1,"status":"unavailable","operation_id":"","reason":"service_unavailable"}` + "\n", true},
		{"invalid operation", deploy.LegacyC80ServiceResult{Status: "committed", OperationID: "rpo_secret!", Reason: "app_reserved"}, nil,
			`{"version":1,"status":"unavailable","operation_id":"","reason":"service_unavailable"}` + "\n", true},
		{"rejection with operation", deploy.LegacyC80ServiceResult{Status: "rejected", OperationID: operation, Reason: "source_denied"}, nil,
			`{"version":1,"status":"unavailable","operation_id":"","reason":"service_unavailable"}` + "\n", true},
		{"incorrect recovery reason", deploy.LegacyC80ServiceResult{Status: "recovered_committed", OperationID: operation, Reason: "app_reserved"}, nil,
			`{"version":1,"status":"unavailable","operation_id":"","reason":"service_unavailable"}` + "\n", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			maintenance := func(context.Context, deploy.RecordLedgerMaintenanceRequest, deploy.LegacyC80ServiceRequest) (deploy.LegacyC80ServiceResult, error) {
				return tc.result, tc.failure
			}
			err := runLegacyC80ServiceWith(context.Background(), legacyC80TestArgs(path), &output, func() error { return nil }, maintenance)
			if output.String() != tc.frame {
				t.Fatalf("frame=%q, error=%v", output.String(), err)
			}
			if tc.handled {
				legacyC80RequireHandled(t, err)
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if strings.Contains(output.String(), "secret") || strings.Contains(fmt.Sprint(err), "secret DSN") {
				t.Fatal("private result or underlying error leaked")
			}
		})
	}
}

func TestLegacyC80ServicePrivateRequestAndPreparation(t *testing.T) {
	request := legacyC80TestRequest(t)
	valid, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	mutations := map[string]func(map[string]any){
		"missing actor":      func(v map[string]any) { delete(v, "actor_user_id") },
		"extra member":       func(v map[string]any) { v["secret"] = "private" },
		"case alias":         func(v map[string]any) { v["Actor_User_ID"] = "private" },
		"null actor":         func(v map[string]any) { v["actor_user_id"] = nil },
		"invalid actor":      func(v map[string]any) { v["actor_user_id"] = "private" },
		"successor record":   func(v map[string]any) { v["record_id"] = "rec2_" + strings.Repeat("a", 32) },
		"malformed record":   func(v map[string]any) { v["record_id"] = "rec_private!" },
		"extra fence field":  func(v map[string]any) { v["fence"].(map[string]any)["secret"] = "private" },
		"null array element": func(v map[string]any) { v["fence"].(map[string]any)["app_previous_logins"] = []any{"old_admin", nil} },
		"fractional identity": func(v map[string]any) {
			v["fence"].(map[string]any)["app_identity"].(map[string]any)["database_oid"] = 1.5
		},
		"wrong app identity": func(v map[string]any) {
			v["fence"].(map[string]any)["app_identity"].(map[string]any)["system_identifier"] = "101"
		},
	}
	invalid := make(map[string][]byte, len(mutations)+2)
	for name, mutate := range mutations {
		var object map[string]any
		if err := json.Unmarshal(valid, &object); err != nil {
			t.Fatal(err)
		}
		mutate(object)
		invalid[name], err = json.Marshal(object)
		if err != nil {
			t.Fatal(err)
		}
	}
	invalid["duplicate member"] = []byte(strings.Replace(string(valid), `"record_id":`, `"record_id":"rec_secret","record_id":`, 1))
	invalid["trailing value"] = append(append([]byte(nil), valid...), []byte(` {"secret":"private"}`)...)
	for name, body := range invalid {
		t.Run(name, func(t *testing.T) {
			path := legacyC80TestRequestFile(t, body)
			var output bytes.Buffer
			calls := 0
			maintenance := func(context.Context, deploy.RecordLedgerMaintenanceRequest, deploy.LegacyC80ServiceRequest) (deploy.LegacyC80ServiceResult, error) {
				calls++
				return deploy.LegacyC80ServiceResult{}, nil
			}
			err := runLegacyC80ServiceWith(context.Background(), legacyC80TestArgs(path), &output, func() error { return nil }, maintenance)
			legacyC80RequireHandled(t, err)
			if calls != 0 || output.String() != "{\"version\":1,\"status\":\"unavailable\",\"operation_id\":\"\",\"reason\":\"admission_unavailable\"}\n" {
				t.Fatalf("invalid request reached maintenance or produced unexpected frame: calls=%d frame=%q", calls, output.String())
			}
		})
	}

	t.Run("private file", func(t *testing.T) {
		path := legacyC80TestRequestFile(t, valid)
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		calls := 0
		err := runLegacyC80ServiceWith(context.Background(), legacyC80TestArgs(path), &output, func() error { return nil },
			func(context.Context, deploy.RecordLedgerMaintenanceRequest, deploy.LegacyC80ServiceRequest) (deploy.LegacyC80ServiceResult, error) {
				calls++
				return deploy.LegacyC80ServiceResult{}, nil
			})
		legacyC80RequireHandled(t, err)
		if calls != 0 || !strings.Contains(output.String(), `"reason":"admission_unavailable"`) {
			t.Fatalf("public request admitted: calls=%d frame=%q", calls, output.String())
		}
	})

	t.Run("symlink and extra descriptor argument", func(t *testing.T) {
		path := legacyC80TestRequestFile(t, valid)
		link := filepath.Join(t.TempDir(), "link.json")
		if err := os.Symlink(path, link); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{
			legacyC80TestArgs(link),
			append(legacyC80TestArgs(path), "--realm-fd", "9"),
		} {
			var output bytes.Buffer
			calls := 0
			err := runLegacyC80ServiceWith(context.Background(), args, &output, func() error { return nil },
				func(context.Context, deploy.RecordLedgerMaintenanceRequest, deploy.LegacyC80ServiceRequest) (deploy.LegacyC80ServiceResult, error) {
					calls++
					return deploy.LegacyC80ServiceResult{}, nil
				})
			legacyC80RequireHandled(t, err)
			if calls != 0 || !strings.Contains(output.String(), `"reason":"admission_unavailable"`) {
				t.Fatalf("unsafe request admitted: calls=%d frame=%q", calls, output.String())
			}
		}
	})

	t.Run("prepare before reading", func(t *testing.T) {
		path := legacyC80TestRequestFile(t, []byte("invalid before preparation"))
		var output bytes.Buffer
		err := runLegacyC80ServiceWith(context.Background(), legacyC80TestArgs(path), &output,
			func() error { return os.WriteFile(path, valid, 0o600) },
			func(context.Context, deploy.RecordLedgerMaintenanceRequest, deploy.LegacyC80ServiceRequest) (deploy.LegacyC80ServiceResult, error) {
				return deploy.LegacyC80ServiceResult{Status: "rejected", Reason: "source_denied"}, nil
			})
		if err != nil || !strings.Contains(output.String(), `"reason":"source_denied"`) {
			t.Fatalf("request read before coordinator preparation: %v, %q", err, output.String())
		}
	})

	t.Run("preparation failure", func(t *testing.T) {
		path := legacyC80TestRequestFile(t, valid)
		var output bytes.Buffer
		calls := 0
		err := runLegacyC80ServiceWith(context.Background(), legacyC80TestArgs(path), &output,
			func() error { return errors.New("private preparation error") },
			func(context.Context, deploy.RecordLedgerMaintenanceRequest, deploy.LegacyC80ServiceRequest) (deploy.LegacyC80ServiceResult, error) {
				calls++
				return deploy.LegacyC80ServiceResult{}, nil
			})
		legacyC80RequireHandled(t, err)
		if calls != 0 || strings.Contains(output.String(), "private") {
			t.Fatalf("preparation failure reached maintenance or leaked: calls=%d frame=%q", calls, output.String())
		}
	})
}

type legacyC80ShortWriter struct{}

func (legacyC80ShortWriter) Write(body []byte) (int, error) { return len(body) - 1, nil }

func TestLegacyC80ServiceIncompleteOutputIsNotHandled(t *testing.T) {
	err := runLegacyC80ServiceWith(context.Background(), legacyC80TestArgs("/unused"), legacyC80ShortWriter{},
		func() error { return errors.New("private failure") },
		func(context.Context, deploy.RecordLedgerMaintenanceRequest, deploy.LegacyC80ServiceRequest) (deploy.LegacyC80ServiceResult, error) {
			return deploy.LegacyC80ServiceResult{}, nil
		})
	var handled *legacyC80HandledResultError
	if errors.As(err, &handled) || !errors.Is(err, errRecordDeletionAdmin) {
		t.Fatalf("incomplete output classified as handled: %v", err)
	}
}

func TestLegacyC80CommandExitAndLogging(t *testing.T) {
	const selector = "HOUFENG_TEST_C80_COMMAND_EXIT"
	if action := os.Getenv(selector); action != "" {
		os.Args = []string{"houfeng-record-platform-admin", "record-deletion", action, "--request-file", "/nonexistent/private-secret-request"}
		main()
		return
	}
	for _, tc := range []struct {
		action     string
		controlled bool
	}{
		{"legacy-c80-service", true},
		{"reconcile-legacy", false},
	} {
		t.Run(tc.action, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestLegacyC80CommandExitAndLogging$")
			cmd.Env = append(os.Environ(), selector+"="+tc.action)
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 1 {
				t.Fatalf("exit=%v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
			}
			if tc.controlled {
				if stdout.String() != "{\"version\":1,\"status\":\"unavailable\",\"operation_id\":\"\",\"reason\":\"admission_unavailable\"}\n" || stderr.Len() != 0 {
					t.Fatalf("controlled failure output differs: stdout=%q stderr=%q", stdout.String(), stderr.String())
				}
			} else if stdout.Len() != 0 || !strings.Contains(stderr.String(), "record platform admin command failed") {
				t.Fatalf("ordinary failure lost logging: stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
			if strings.Contains(stdout.String(), "private-secret") || strings.Contains(stderr.String(), "private-secret") {
				t.Fatal("request path leaked")
			}
		})
	}
}
