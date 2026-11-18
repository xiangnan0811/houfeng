package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"houfeng/internal/center/sourceretirement"
)

func TestSourceRetirementStrictJSONBoundaries(t *testing.T) {
	valid := `{"actor_user_id":"user-1","source":{"DeploymentID":"deployment","ProjectID":"project","Kind":"vps","ID":"source"}}`
	for _, tc := range []struct {
		name  string
		body  string
		valid bool
	}{
		{"valid", valid, true},
		{"empty", "", false},
		{"null", "null", false},
		{"unknown top-level", strings.TrimSuffix(valid, "}") + `,"secret":"value"}`, false},
		{"unknown nested", strings.Replace(valid, `"ID":"source"`, `"ID":"source","secret":1`, 1), false},
		{"duplicate", strings.Replace(valid, `"actor_user_id":"user-1"`, `"actor_user_id":"user-1","actor_user_id":"user-2"`, 1), false},
		{"case alias", strings.Replace(valid, `"ID":"source"`, `"ID":"source","id":"other"`, 1), false},
		{"missing", `{"source":{}}`, false},
		{"null field", `{"actor_user_id":null,"source":{}}`, false},
		{"second value", valid + ` {}`, false},
		{"trailing junk", valid + ` secret`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var request sourceRetirementPreviewRequest
			err := decodeSourceRetirementJSON([]byte(tc.body), &request, "actor_user_id", "source")
			if (err == nil) != tc.valid {
				t.Fatalf("decode error = %v, valid = %v", err, tc.valid)
			}
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Fatalf("error disclosed input: %v", err)
			}
		})
	}
}

func TestSourceRetirementRevisionRequiresExactByteArray(t *testing.T) {
	valid := "[" + strings.Repeat("1,", 31) + "1]"
	for _, tc := range []struct {
		name  string
		value string
		valid bool
	}{
		{"exact", valid, true},
		{"short", "[1]", false},
		{"long", "[" + strings.Repeat("1,", 32) + "1]", false},
		{"negative", "[" + strings.Repeat("1,", 31) + "-1]", false},
		{"overflow", "[" + strings.Repeat("1,", 31) + "256]", false},
		{"null element", "[" + strings.Repeat("1,", 31) + "null]", false},
		{"null", "null", false},
		{"string", `"AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var revision exactSourceRevision
			err := json.Unmarshal([]byte(tc.value), &revision)
			if (err == nil) != tc.valid {
				t.Fatalf("revision error = %v, valid = %v", err, tc.valid)
			}
		})
	}
}

func TestSourceRetirementFilesRejectUnsafeInputs(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "private-secret.json")
	if err := os.WriteFile(file, []byte(`{"ID":"domain"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readSourceRetirementFile(file); err != nil {
		t.Fatalf("read private regular file: %v", err)
	}
	link := filepath.Join(dir, "link-secret")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{link, dir, filepath.Join(dir, "missing-secret")} {
		if _, err := readSourceRetirementFile(path); err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), path) {
			t.Fatalf("unsafe file returned unsafe result: %v", err)
		}
	}
	if err := os.Chmod(file, 0o620); err != nil {
		t.Fatal(err)
	}
	if _, err := readSourceRetirementFile(file); err == nil {
		t.Fatal("group-writable file accepted")
	}
	if err := os.Chmod(file, 0o602); err != nil {
		t.Fatal(err)
	}
	if _, err := readSourceRetirementFile(file); err == nil {
		t.Fatal("other-writable file accepted")
	}
	if err := os.Chmod(file, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(strings.Repeat("x", sourceRetirementFileLimit+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readSourceRetirementFile(file); err == nil {
		t.Fatal("oversized file accepted")
	}
}

func TestSourceRetirementIdentityJSONRequiresExactFields(t *testing.T) {
	var identity struct {
		ID               string
		SystemIdentifier string
		DatabaseOID      uint32
		DatabaseName     string
		Epoch            uint64
	}
	valid := `{"ID":"domain","SystemIdentifier":"123","DatabaseOID":1,"DatabaseName":"witness","Epoch":1}`
	for _, body := range []string{
		strings.Replace(valid, `"ID":"domain"`, `"ID":"domain","ID":"other"`, 1),
		strings.Replace(valid, `"Epoch":1`, `"epoch":1`, 1),
		strings.Replace(valid, `"Epoch":1`, `"Epoch":null`, 1),
		strings.Replace(valid, `"DatabaseOID":1`, `"DatabaseOID":-1`, 1),
		strings.TrimSuffix(valid, "}") + `,"credential":"secret"}`,
	} {
		if err := decodeSourceRetirementJSON([]byte(body), &identity, "ID", "SystemIdentifier", "DatabaseOID", "DatabaseName", "Epoch"); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("malformed identity returned unsafe result: %v", err)
		}
	}
	if err := decodeSourceRetirementJSON([]byte(valid), &identity, "ID", "SystemIdentifier", "DatabaseOID", "DatabaseName", "Epoch"); err != nil {
		t.Fatalf("valid identity: %v", err)
	}
}

func TestSourceRetirementApplicationConnectionMatchesEnrolledIdentity(t *testing.T) {
	expected := sourceretirement.DomainIdentity{
		ID: "rd-" + strings.Repeat("a", 64), SystemIdentifier: "123",
		DatabaseOID: 42, DatabaseName: "app", Epoch: 1,
	}
	valid := sourceRetirementApplicationConnection{
		sessionUser: "admin", currentUser: "admin", domainKind: "application",
		identityMode: "postgres_system", domain: expected, epoch: 1,
		databaseOID: 42, databaseName: "app", singleIdentity: true, currentAdmin: true,
	}
	if !valid.matches("admin", expected) {
		t.Fatal("enrolled direct admin was rejected")
	}
	for _, tc := range []struct {
		name   string
		mutate func(*sourceRetirementApplicationConnection)
	}{
		{"independent domain", func(v *sourceRetirementApplicationConnection) { v.domain.ID = "rd-" + strings.Repeat("b", 64) }},
		{"independent system", func(v *sourceRetirementApplicationConnection) { v.domain.SystemIdentifier = "456" }},
		{"row database oid", func(v *sourceRetirementApplicationConnection) { v.domain.DatabaseOID++ }},
		{"row database name", func(v *sourceRetirementApplicationConnection) { v.domain.DatabaseName = "other" }},
		{"current database oid", func(v *sourceRetirementApplicationConnection) { v.databaseOID++ }},
		{"current database name", func(v *sourceRetirementApplicationConnection) { v.databaseName = "other" }},
		{"epoch", func(v *sourceRetirementApplicationConnection) { v.epoch++ }},
		{"kind", func(v *sourceRetirementApplicationConnection) { v.domainKind = "deletion_witness" }},
		{"identity mode", func(v *sourceRetirementApplicationConnection) { v.identityMode = "external_attestation" }},
		{"multiple identities", func(v *sourceRetirementApplicationConnection) { v.singleIdentity = false }},
		{"wrong session role", func(v *sourceRetirementApplicationConnection) { v.sessionUser = "bootstrap" }},
		{"set role", func(v *sourceRetirementApplicationConnection) { v.currentUser = "other" }},
		{"not current admin", func(v *sourceRetirementApplicationConnection) { v.currentAdmin = false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			observed := valid
			tc.mutate(&observed)
			if observed.matches("admin", expected) {
				t.Fatal("unexpected APP connection was accepted")
			}
		})
	}
	// A second independently provisioned APP can be internally consistent, but
	// its self-description must not become the expected W2 enrollment.
	other := valid
	other.domain.ID = "rd-" + strings.Repeat("b", 64)
	other.domain.SystemIdentifier = "456"
	other.domain.DatabaseOID, other.databaseOID = 43, 43
	other.domain.DatabaseName, other.databaseName = "other", "other"
	if other.matches("admin", expected) {
		t.Fatal("self-consistent unexpected APP identity was accepted")
	}
}

func TestSourceRetirementRequiresActivatedEnrollment(t *testing.T) {
	digest := [32]byte{1}
	valid := sourceretirement.ApplicationEnrollment{Generation: 1, DeploymentID: "deployment", ActivationDigest: &digest}
	if !sourceRetirementEnrollmentActivated(valid, "deployment") {
		t.Fatal("activated current enrollment was rejected")
	}
	for _, tc := range []struct {
		name       string
		enrollment sourceretirement.ApplicationEnrollment
		deployment string
	}{
		{"missing activation", sourceretirement.ApplicationEnrollment{Generation: 1, DeploymentID: "deployment"}, "deployment"},
		{"zero activation", sourceretirement.ApplicationEnrollment{Generation: 1, DeploymentID: "deployment", ActivationDigest: &[32]byte{}}, "deployment"},
		{"zero generation", sourceretirement.ApplicationEnrollment{DeploymentID: "deployment", ActivationDigest: &digest}, "deployment"},
		{"wrong deployment", valid, "other"},
		{"empty deployment", sourceretirement.ApplicationEnrollment{Generation: 1, ActivationDigest: &digest}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if sourceRetirementEnrollmentActivated(tc.enrollment, tc.deployment) {
				t.Fatal("invalid enrollment was accepted")
			}
		})
	}
}

func TestSourceRetirementRejectsWrongDeploymentBeforeConnecting(t *testing.T) {
	for key, value := range map[string]string{
		sourceAppAdminDatabaseURLEnv:         "not-a-database-secret",
		sourceWitnessProducerDatabaseURLEnv:  "not-a-witness-secret",
		sourceWitnessIdentityFileEnv:         "not-an-identity-file",
		"HOUFENG_RECORD_INSTANCE_ID":         "instance",
		"HOUFENG_RECORD_DEPLOYMENT_ID":       "expected",
		"HOUFENG_RECORD_INSTANCE_KIND":       "center",
		"HOUFENG_RECORD_INSTANCE_CAPABILITY": "test",
	} {
		t.Setenv(key, value)
	}
	source := `{"DeploymentID":"other","ProjectID":"project","Kind":"vps","ID":"source"}`
	for _, tc := range []struct{ command, body string }{
		{"retirement-preview", `{"actor_user_id":"actor","source":` + source + `}`},
		{"retire", `{"actor_user_id":"actor","source":` + source + `,"retirement_id":"retirement","expected_revision":[` + strings.Repeat("1,", 31) + `1],"final_floor":{}}`},
	} {
		t.Run(tc.command, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "request.json")
			if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := runSourceRetirement(context.Background(), []string{"source-authority", tc.command, "--request-file", path}); err != errSourceRetirementInput {
				t.Fatalf("wrong deployment returned %v", err)
			}
		})
	}
}
