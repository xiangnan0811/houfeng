//go:build linux

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/deletionledger"
	"houfeng/internal/center/deploy"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/sourceretirement"
)

type legacySourceTestFence struct {
	request          deletionledger.SourceRetirementRequest
	token            [32]byte
	entry            deletionledger.SourceRetirementEntry
	installed        bool
	err              error
	maintenanceCalls int
	previewCalls     int
	retireCalls      int
	reconcileCalls   int
}

func (fence *legacySourceTestFence) ProvisionRecordScopes(context.Context) error { return nil }

func (fence *legacySourceTestFence) PreviewLegacySourceRetirement(_ context.Context, _ string, _ sourceretirement.SourceKey, _ recordauth.VisibilityScope) (deletionledger.SourceRetirementRequest, [32]byte, error) {
	fence.previewCalls++
	return fence.request, fence.token, fence.err
}

func (fence *legacySourceTestFence) RetireLegacySourceRetirement(_ context.Context, request deletionledger.SourceRetirementRequest, token [32]byte) (deploy.LegacySourceRetirementResult, error) {
	fence.retireCalls++
	if request.RequestFingerprint != fence.request.RequestFingerprint || token != fence.token {
		return deploy.LegacySourceRetirementResult{}, errors.New("unexpected original request")
	}
	return deploy.LegacySourceRetirementResult{Entry: fence.entry, HandoffInstalled: fence.installed}, fence.err
}

func (fence *legacySourceTestFence) ReconcileLegacySourceRetirement(_ context.Context, request deletionledger.SourceRetirementRequest) (deploy.LegacySourceRetirementResult, error) {
	fence.reconcileCalls++
	if request.RequestFingerprint != fence.request.RequestFingerprint {
		return deploy.LegacySourceRetirementResult{}, errors.New("unexpected original request")
	}
	return deploy.LegacySourceRetirementResult{Entry: fence.entry, HandoffInstalled: fence.installed}, fence.err
}

func (fence *legacySourceTestFence) maintenance(_ context.Context, _ deploy.RecordLedgerMaintenanceRequest, fn func(legacySourceFence) error) error {
	fence.maintenanceCalls++
	return fn(fence)
}

func legacySourceTestFixture(t *testing.T) (legacySourcePreviewManagement, *legacySourceTestFence) {
	t.Helper()
	record := validRecordDeletionRequest(t)
	scope, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{
		Version: recordauth.VisibilityScopeVersionV1, Kind: recordauth.VisibilityKindRestricted,
		ProjectID: recordauth.ProjectIDDefault, AllowedRoles: []recordauth.Role{recordauth.RoleViewer},
		AllowedGroupIDs: []string{"rag_alpha"}, PolicyVersion: recordauth.PolicyVersionV1, PolicyRevision: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	key := sourceretirement.SourceKey{DeploymentID: record.DeploymentID, ProjectID: recordauth.ProjectIDDefault,
		Kind: recordauth.SourceKindVPS, ID: "vps_0123456789abcdef"}
	token := sha256.Sum256([]byte("private source request token"))
	commitment, err := deletionledger.SourceTokenCommitment(key, token)
	if err != nil {
		t.Fatal(err)
	}
	app := record.Fence.AppIdentity
	request := deletionledger.SourceRetirementRequest{OperationID: "source_operation_01", ActorUserID: "usr_" + strings.Repeat("a", 24),
		Origin: deletionledger.SourceOrigin{Application: deletionledger.SourceApplicationIdentity{ID: app.ID, SystemIdentifier: app.SystemIdentifier,
			DatabaseName: app.DatabaseName, DatabaseOID: app.DatabaseOID, Epoch: app.Epoch}, Source: key,
			Revision: sha256.Sum256([]byte("actual source revision")), LastLive: scope,
			TombstoneRecordedAt: time.UnixMicro(1700000000000000).UTC()}, FinalFloor: scope, TokenCommitment: commitment}
	request.RequestFingerprint, err = deletionledger.SourceRequestFingerprint(request)
	if err != nil || request.Validate() != nil {
		t.Fatalf("fixture request invalid: %v", err)
	}
	entry := deletionledger.SourceRetirementEntry{Request: request, LedgerDomain: sha256.Sum256([]byte("ledger domain")),
		Sequence: 1, ConfirmedAt: time.UnixMicro(1700000000000100).UTC()}
	entry.Canonical, err = deletionledger.EncodeSourceRetirementEntry(entry)
	if err != nil {
		t.Fatal(err)
	}
	entry.EntryHash = sha256.Sum256(entry.Canonical)
	// TempDir's numbered child is created with 0777 subject to umask; the
	// source request must be reserved in an explicitly private directory.
	outputDir := t.TempDir()
	if err := os.Chmod(outputDir, 0o700); err != nil {
		t.Fatal(err)
	}
	input := legacySourcePreviewManagement{ActorUserID: request.ActorUserID, Source: legacySourceKeyFrom(key), FinalFloor: legacySourceScopeFrom(scope),
		OutputRequestFile: filepath.Join(outputDir, "original-request.json"), AppAdminURL: record.AppAdminURL,
		AppPreviousAdminURL: record.AppPreviousAdminURL, Fence: record.Fence}
	return input, &legacySourceTestFence{request: request, token: token, entry: entry}
}

func legacySourceTestManagementFile(t *testing.T, value any) string {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "management.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func legacySourceTestRun(command, management string, fence *legacySourceTestFence, output *bytes.Buffer) error {
	return runSourceLegacyRetirementWith(context.Background(), []string{"source-authority", command, "--request-file", management}, output, fence.maintenance)
}

func TestLegacySourcePreviewRejectsMalformedInputsBeforeMaintenance(t *testing.T) {
	input, fence := legacySourceTestFixture(t)
	valid, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(map[string]any){
		"missing nested member": func(v map[string]any) { delete(v["source"].(map[string]any), "id") },
		"extra nested member":   func(v map[string]any) { v["source"].(map[string]any)["other"] = "secret" },
		"case alias":            func(v map[string]any) { v["source"].(map[string]any)["ID"] = "secret" },
		"null array element":    func(v map[string]any) { v["final_floor"].(map[string]any)["allowed_group_ids"] = []any{nil} },
		"bool for integer":      func(v map[string]any) { v["fence"].(map[string]any)["app_identity"].(map[string]any)["epoch"] = true },
		"fractional integer":    func(v map[string]any) { v["fence"].(map[string]any)["app_identity"].(map[string]any)["epoch"] = 1.5 },
		"negative integer": func(v map[string]any) {
			v["fence"].(map[string]any)["app_identity"].(map[string]any)["database_oid"] = -1
		},
		"null member":    func(v map[string]any) { v["source"].(map[string]any)["project_id"] = nil },
		"bad scope hash": func(v map[string]any) { v["final_floor"].(map[string]any)["canonical_hash"] = strings.Repeat("0", 64) },
		"upper digest": func(v map[string]any) {
			v["final_floor"].(map[string]any)["canonical_hash"] = strings.ToUpper(input.FinalFloor.CanonicalHash)
		},
		"wrong source kind":    func(v map[string]any) { v["source"].(map[string]any)["kind"] = "unknown" },
		"unknown fence member": func(v map[string]any) { v["fence"].(map[string]any)["secret"] = "private" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			var object map[string]any
			if err := json.Unmarshal(valid, &object); err != nil {
				t.Fatal(err)
			}
			mutate(object)
			var out bytes.Buffer
			err := legacySourceTestRun("legacy-retirement-preview", legacySourceTestManagementFile(t, object), fence, &out)
			if err == nil || fence.maintenanceCalls != 0 || fence.previewCalls != 0 || out.Len() != 0 {
				t.Fatalf("invalid input reached preview: %v, calls %d, output %q", err, fence.previewCalls, out.String())
			}
			if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), input.AppAdminURL) {
				t.Fatalf("input disclosed in error: %v", err)
			}
			if _, statErr := os.Lstat(input.OutputRequestFile); !os.IsNotExist(statErr) {
				t.Fatalf("invalid input created a request file: %v", statErr)
			}
		})
	}
	duplicate := strings.Replace(string(valid), `"actor_user_id":`, `"actor_user_id":"secret","actor_user_id":`, 1)
	path := filepath.Join(t.TempDir(), "duplicate.json")
	if err := os.WriteFile(path, []byte(duplicate), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := legacySourceTestRun("legacy-retirement-preview", path, fence, &out); err == nil || fence.maintenanceCalls != 0 || fence.previewCalls != 0 {
		t.Fatalf("duplicate reached preview: %v", err)
	}
}

func TestLegacySourcePrivateRequestCustodyAndResume(t *testing.T) {
	input, fence := legacySourceTestFixture(t)
	var preview bytes.Buffer
	if err := legacySourceTestRun("legacy-retirement-preview", legacySourceTestManagementFile(t, input), fence, &preview); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(input.OutputRequestFile)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("request permissions: %v, %v", info, err)
	}
	original, err := readLegacySourceTokenFile(input.OutputRequestFile)
	if err != nil {
		t.Fatal(err)
	}
	var wire legacySourceTokenFile
	if err := decodeLegacySourceJSON(original, &wire, legacySourceTokenSchema); err != nil {
		t.Fatal(err)
	}
	decoded, token, err := wire.request()
	if err != nil || token != fence.token || decoded.RequestFingerprint != fence.request.RequestFingerprint {
		t.Fatalf("original request did not round trip: %v", err)
	}
	secret := hex.EncodeToString(fence.token[:])
	if bytes.Contains(preview.Bytes(), []byte(secret)) || bytes.Contains(preview.Bytes(), []byte(input.AppAdminURL)) {
		t.Fatal("preview output disclosed a secret")
	}
	if err := legacySourceTestRun("legacy-retirement-preview", legacySourceTestManagementFile(t, input), fence, &preview); err == nil || fence.previewCalls != 1 {
		t.Fatalf("existing request was overwritten or reminted: %v", err)
	}
	resume := legacySourceResumeManagement{OriginalRequestFile: input.OutputRequestFile, AppAdminURL: input.AppAdminURL,
		AppPreviousAdminURL: input.AppPreviousAdminURL, Fence: input.Fence}
	management := legacySourceTestManagementFile(t, resume)
	var result bytes.Buffer
	if err := legacySourceTestRun("legacy-retire", management, fence, &result); err != nil {
		t.Fatal(err)
	}
	var published struct {
		State                                 string `json:"state"`
		SourceAuthorizationPermanentlyRetired bool   `json:"source_authorization_permanently_retired"`
		SourceAssetContentRetained            bool   `json:"source_asset_content_retained"`
		W2LegacyBlockerUnchanged              bool   `json:"w2_legacy_blocker_unchanged"`
	}
	if json.Unmarshal(result.Bytes(), &published) != nil || published.State != "witnessed_handoff_pending" ||
		!published.SourceAuthorizationPermanentlyRetired || !published.SourceAssetContentRetained || !published.W2LegacyBlockerUnchanged {
		t.Fatalf("unexpected retirement result: %s", result.String())
	}
	if bytes.Contains(result.Bytes(), []byte(secret)) || bytes.Contains(result.Bytes(), []byte(input.AppAdminURL)) {
		t.Fatal("retirement output disclosed a secret")
	}
	fence.installed = true
	result.Reset()
	if err := legacySourceTestRun("legacy-retirement-reconcile", management, fence, &result); err != nil {
		t.Fatal(err)
	}
	if json.Unmarshal(result.Bytes(), &published) != nil || published.State != "handoff_installed" || fence.retireCalls != 1 || fence.reconcileCalls != 1 {
		t.Fatalf("original request did not reconcile: %s", result.String())
	}
	after, err := readLegacySourceTokenFile(input.OutputRequestFile)
	if err != nil || !bytes.Equal(original, after) {
		t.Fatalf("resume changed original request: %v", err)
	}
}

type legacySourceFailWriter struct{ message string }

func (writer legacySourceFailWriter) Write([]byte) (int, error) { return 0, errors.New(writer.message) }

func TestLegacySourceOutputFailurePreservesOriginalAndSanitizesErrors(t *testing.T) {
	input, fence := legacySourceTestFixture(t)
	secret := hex.EncodeToString(fence.token[:])
	management := legacySourceTestManagementFile(t, input)
	err := runSourceLegacyRetirementWith(context.Background(), []string{"source-authority", "legacy-retirement-preview", "--request-file", management},
		legacySourceFailWriter{message: secret + input.AppAdminURL}, fence.maintenance)
	if err == nil || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), input.AppAdminURL) {
		t.Fatalf("unsafe preview failure: %v", err)
	}
	original, err := readLegacySourceTokenFile(input.OutputRequestFile)
	if err != nil {
		t.Fatalf("preview output failure lost original request: %v", err)
	}
	resume := legacySourceResumeManagement{OriginalRequestFile: input.OutputRequestFile, AppAdminURL: input.AppAdminURL,
		AppPreviousAdminURL: input.AppPreviousAdminURL, Fence: input.Fence}
	resumeFile := legacySourceTestManagementFile(t, resume)
	err = runSourceLegacyRetirementWith(context.Background(), []string{"source-authority", "legacy-retire", "--request-file", resumeFile},
		legacySourceFailWriter{message: secret + input.AppAdminURL}, fence.maintenance)
	if err == nil || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), input.AppAdminURL) {
		t.Fatalf("unsafe retirement failure: %v", err)
	}
	after, err := readLegacySourceTokenFile(input.OutputRequestFile)
	if err != nil || !bytes.Equal(original, after) {
		t.Fatalf("retirement output failure changed original request: %v", err)
	}
	var out bytes.Buffer
	if err := legacySourceTestRun("legacy-retirement-reconcile", resumeFile, fence, &out); err != nil || fence.reconcileCalls != 1 {
		t.Fatalf("original request did not resume after output failure: %v", err)
	}
	fence.err = errors.New(secret + input.AppAdminURL)
	if err := legacySourceTestRun("legacy-retirement-reconcile", resumeFile, fence, &out); err == nil ||
		strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), input.AppAdminURL) {
		t.Fatalf("unsafe underlying error: %v", err)
	}
}

func TestLegacySourceTokenFileRejectsUnsafeFilesAndConflicts(t *testing.T) {
	input, fence := legacySourceTestFixture(t)
	management := legacySourceTestManagementFile(t, input)
	var out bytes.Buffer
	if err := legacySourceTestRun("legacy-retirement-preview", management, fence, &out); err != nil {
		t.Fatal(err)
	}
	original, err := readLegacySourceTokenFile(input.OutputRequestFile)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(input.OutputRequestFile)
	link := filepath.Join(dir, "link")
	if err := os.Symlink(input.OutputRequestFile, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readLegacySourceTokenFile(link); err == nil {
		t.Fatal("symlink was accepted")
	}
	hardlink := filepath.Join(dir, "hardlink")
	if err := os.Link(input.OutputRequestFile, hardlink); err != nil {
		t.Fatal(err)
	}
	if _, err := readLegacySourceTokenFile(input.OutputRequestFile); err == nil {
		t.Fatal("multiply linked request was accepted")
	}
	if err := os.Remove(hardlink); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(input.OutputRequestFile, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := readLegacySourceTokenFile(input.OutputRequestFile); err == nil {
		t.Fatal("nonprivate request was accepted")
	}
	if err := os.Chmod(input.OutputRequestFile, 0o600); err != nil {
		t.Fatal(err)
	}
	var wire legacySourceTokenFile
	if decodeLegacySourceJSON(original, &wire, legacySourceTokenSchema) != nil {
		t.Fatal("valid request failed to decode")
	}
	wire.Token = strings.Repeat("1", 64)
	bad, err := json.Marshal(wire)
	if err != nil || os.WriteFile(input.OutputRequestFile, bad, 0o600) != nil {
		t.Fatal("write invalid request")
	}
	resume := legacySourceResumeManagement{OriginalRequestFile: input.OutputRequestFile, AppAdminURL: input.AppAdminURL,
		AppPreviousAdminURL: input.AppPreviousAdminURL, Fence: input.Fence}
	if err := legacySourceTestRun("legacy-retire", legacySourceTestManagementFile(t, resume), fence, &out); err == nil || fence.retireCalls != 0 {
		t.Fatalf("wrong token reached retirement: %v", err)
	}
	if err := os.WriteFile(input.OutputRequestFile, bytes.Repeat([]byte("x"), sourceRetirementFileLimit+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readLegacySourceTokenFile(input.OutputRequestFile); err == nil {
		t.Fatal("oversized request accepted")
	}
}

func TestLegacySourceOriginalRequestRejectsMalformedEvidenceBeforeMaintenance(t *testing.T) {
	input, fence := legacySourceTestFixture(t)
	valid, err := json.Marshal(legacySourceTokenFileFrom(fence.request, fence.token))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(map[string]any){
		"missing application identity": func(v map[string]any) {
			delete(v["request"].(map[string]any)["origin"].(map[string]any)["application"].(map[string]any), "database_oid")
		},
		"extra scope member": func(v map[string]any) {
			v["request"].(map[string]any)["final_floor"].(map[string]any)["extra"] = true
		},
		"invalid revision digest": func(v map[string]any) {
			v["request"].(map[string]any)["origin"].(map[string]any)["revision"] = "broken"
		},
		"altered fingerprint": func(v map[string]any) {
			v["request"].(map[string]any)["request_fingerprint"] = strings.Repeat("1", 64)
		},
		"altered token": func(v map[string]any) { v["token"] = strings.Repeat("1", 64) },
		"null nested grant": func(v map[string]any) {
			v["request"].(map[string]any)["origin"].(map[string]any)["last_live"].(map[string]any)["allowed_roles"] = []any{nil}
		},
		"boolean contract":   func(v map[string]any) { v["deletion_contract_version"] = true },
		"fractional version": func(v map[string]any) { v["version"] = 1.5 },
		"wrong action":       func(v map[string]any) { v["action"] = "physical_purge" },
		"noncanonical time": func(v map[string]any) {
			v["request"].(map[string]any)["origin"].(map[string]any)["tombstone_recorded_at"] = "2023-11-14T22:13:20+00:00"
		},
	}
	check := func(t *testing.T, content []byte) {
		t.Helper()
		path := filepath.Join(t.TempDir(), "original.json")
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
		resume := legacySourceResumeManagement{OriginalRequestFile: path, AppAdminURL: input.AppAdminURL,
			AppPreviousAdminURL: input.AppPreviousAdminURL, Fence: input.Fence}
		var out bytes.Buffer
		err := legacySourceTestRun("legacy-retire", legacySourceTestManagementFile(t, resume), fence, &out)
		if err == nil || fence.maintenanceCalls != 0 || fence.retireCalls != 0 || out.Len() != 0 {
			t.Fatalf("malformed original reached maintenance: %v", err)
		}
		if strings.Contains(err.Error(), hex.EncodeToString(fence.token[:])) || strings.Contains(err.Error(), input.AppAdminURL) {
			t.Fatalf("malformed request disclosed secret: %v", err)
		}
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			var object map[string]any
			if json.Unmarshal(valid, &object) != nil {
				t.Fatal("decode fixture")
			}
			mutate(object)
			body, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			check(t, body)
		})
	}
	duplicate := strings.Replace(string(valid), `"token_commitment":`, `"token_commitment":"secret","token_commitment":`, 1)
	check(t, []byte(duplicate))
}

func TestLegacySourcePreviewNeverRemintsAfterReservationOrDisclosesToken(t *testing.T) {
	input, fence := legacySourceTestFixture(t)
	fence.err = errors.New("backend response with private credential")
	management := legacySourceTestManagementFile(t, input)
	var out bytes.Buffer
	if err := legacySourceTestRun("legacy-retirement-preview", management, fence, &out); err == nil || fence.previewCalls != 1 {
		t.Fatalf("expected preview failure: %v", err)
	}
	info, err := os.Stat(input.OutputRequestFile)
	if err != nil || info.Size() != 0 || info.Mode().Perm() != 0o600 {
		t.Fatalf("reserved request was not retained: %v, %v", info, err)
	}
	fence.err = nil
	if err := legacySourceTestRun("legacy-retirement-preview", management, fence, &out); err == nil || fence.previewCalls != 1 {
		t.Fatalf("reserved request was replaced or token reminted: %v", err)
	}

	other, otherFence := legacySourceTestFixture(t)
	secret := hex.EncodeToString(otherFence.token[:])
	otherFence.request.OperationID = "source_" + secret
	otherFence.request.RequestFingerprint, err = deletionledger.SourceRequestFingerprint(otherFence.request)
	if err != nil {
		t.Fatal(err)
	}
	out.Reset()
	err = legacySourceTestRun("legacy-retirement-preview", legacySourceTestManagementFile(t, other), otherFence, &out)
	if err == nil || out.Len() != 0 || strings.Contains(err.Error(), secret) {
		t.Fatalf("token-shaped operation ID leaked through output: %v", err)
	}
	if _, err := readLegacySourceTokenFile(other.OutputRequestFile); err != nil {
		t.Fatalf("withheld public output lost original request: %v", err)
	}
}

func TestLegacySourcePreviewRejectsSymlinkAndPublicOutputDirectory(t *testing.T) {
	input, fence := legacySourceTestFixture(t)
	privateDir := filepath.Dir(input.OutputRequestFile)
	existing := filepath.Join(privateDir, "existing")
	if err := os.WriteFile(existing, []byte("original private evidence"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(privateDir, "output-link")
	if err := os.Symlink(existing, link); err != nil {
		t.Fatal(err)
	}
	input.OutputRequestFile = link
	var out bytes.Buffer
	if err := legacySourceTestRun("legacy-retirement-preview", legacySourceTestManagementFile(t, input), fence, &out); err == nil || fence.maintenanceCalls != 0 {
		t.Fatalf("symlink output reached maintenance: %v", err)
	}
	data, err := os.ReadFile(existing)
	if err != nil || string(data) != "original private evidence" {
		t.Fatalf("symlink target changed: %v", err)
	}
	alias := filepath.Join(t.TempDir(), "directory-link")
	if err := os.Symlink(privateDir, alias); err != nil {
		t.Fatal(err)
	}
	input.OutputRequestFile = filepath.Join(alias, "new-request")
	if err := legacySourceTestRun("legacy-retirement-preview", legacySourceTestManagementFile(t, input), fence, &out); err == nil || fence.maintenanceCalls != 0 {
		t.Fatalf("symlink parent reached maintenance: %v", err)
	}
	publicDir := t.TempDir()
	if err := os.Chmod(publicDir, 0o755); err != nil {
		t.Fatal(err)
	}
	input.OutputRequestFile = filepath.Join(publicDir, "new-request")
	if err := legacySourceTestRun("legacy-retirement-preview", legacySourceTestManagementFile(t, input), fence, &out); err == nil || fence.maintenanceCalls != 0 {
		t.Fatalf("public output directory reached maintenance: %v", err)
	}
}
