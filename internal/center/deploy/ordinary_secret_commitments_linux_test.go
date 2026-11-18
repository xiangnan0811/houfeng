//go:build linux

package deploy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOrdinarySecretCommitmentsStayPrivateAndReplay(t *testing.T) {
	work := ordinaryBootstrapFixture(t)
	if err := os.Mkdir(filepath.Join(work, "maintenance"), 0o700); err != nil {
		t.Fatal(err)
	}
	request := ordinaryUpgradeRequest{WorkingDirectory: work, OperationID: "commitment-fixture"}
	var identities []ordinarySecretIdentity
	for i := range ordinarySecretNames {
		identities = append(identities, ordinarySecretIdentity{Path: "/private/input", Inode: uint64(i + 1), Size: 1, SHA256: ordinaryDigestHex([]byte{byte(i)})})
	}
	environment := ordinaryDigestHex([]byte("COMPOSE_FILE=compose.yaml:compose.proxy-host.yaml\nPASSWORD=private-value\n"))
	first, err := ordinaryEnsureSecretCommitments(request, identities, environment, true)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ordinaryEnsureSecretCommitments(request, identities, environment, false)
	if err != nil || first != second {
		t.Fatal("exact private commitment did not replay")
	}
	path := filepath.Join(work, "maintenance", "ordinary-secrets", request.OperationID, "secret-commitments.json")
	stat, err := os.Stat(path)
	if err != nil || stat.Mode().Perm() != 0o600 {
		t.Fatal("commitment record is not root private")
	}
	shared, err := json.Marshal(ordinaryUpgradeIntent{Secrets: identities, SecretCommitmentSHA256: first, Recipe: ordinaryComposeRecipe{Environment: ordinaryEnvironmentIdentity{Device: 1, Inode: 2, SHA256: environment}}})
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Recipe struct {
			Environment map[string]any `json:"environment"`
		} `json:"recipe"`
	}
	if json.Unmarshal(shared, &decoded) != nil || len(decoded.Recipe.Environment) != 2 || decoded.Recipe.Environment["device"] != float64(1) || decoded.Recipe.Environment["inode"] != float64(2) {
		t.Fatal("shared environment contains more than filesystem identity")
	}
	for _, id := range identities {
		if strings.Contains(string(shared), id.SHA256) || strings.Contains(string(shared), `"size"`) {
			t.Fatal("shared intent exposed secret verifier")
		}
	}
	if strings.Contains(string(shared), environment) || strings.Contains(string(shared), "environment_sha256") || strings.Contains(string(shared), "database_credential_sha256") {
		t.Fatal("shared intent exposed an environment or authority verifier")
	}
	if !ordinaryVerifyEnvironmentCommitment(request, first, environment) {
		t.Fatal("private environment commitment rejected")
	}
	if ordinaryVerifyEnvironmentCommitment(request, first, ordinaryDigestHex([]byte("changed"))) {
		t.Fatal("changed environment accepted")
	}
	identities[0].SHA256 = ordinaryDigestHex([]byte("changed"))
	if _, err := ordinaryEnsureSecretCommitments(request, identities, environment, false); err == nil {
		t.Fatal("changed secret accepted")
	}
}
