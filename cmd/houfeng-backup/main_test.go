package main

import (
	"os"
	"strings"
	"testing"
)

func TestBackupCLIWiresPlanCreateVerify(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	text := string(source)
	for _, required := range []string{
		"recordbackup.NewService(",
		`"plan"`,
		`"create"`,
		`"verify"`,
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("houfeng-backup main.go missing %s", required)
		}
	}
	for _, forbidden := range []string{
		"houfeng-record-platform-admin",
		"platformmigrate",
		"AdmissionGateFunc(",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("houfeng-backup main.go must not contain %s", forbidden)
		}
	}
}
