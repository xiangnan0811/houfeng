package main

import (
	"os"
	"strings"
	"testing"
)

func TestRestoreCLIWiresPlanApplyVerify(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	text := string(source)
	for _, required := range []string{
		"recordrestore.NewService(",
		`"plan"`,
		`"apply"`,
		`"verify"`,
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("houfeng-restore main.go missing %s", required)
		}
	}
	for _, forbidden := range []string{
		"houfeng-record-platform-admin",
		"platformmigrate",
		"AdmissionGateFunc(",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("houfeng-restore main.go must not contain %s", forbidden)
		}
	}
}
