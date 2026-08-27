package handlers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestVPSSubscriptionCreateRequestMatchesTypeScriptDTO(t *testing.T) {
	root := findRepoRoot(t)
	manifest := readVPSSubscriptionCreateManifest(t, root)
	goFields := jsonTagsOf(reflect.TypeOf(vpsSubscriptionCreateRequest{}))
	tsFields := parseCreateVPSSubscriptionInputFields(t, filepath.Join(root, "web/src/lib/types.ts"))

	if !reflect.DeepEqual(goFields, manifest) {
		t.Fatalf("Go json tags = %#v, want manifest %#v", goFields, manifest)
	}
	if !reflect.DeepEqual(tsFields, manifest) {
		t.Fatalf("CreateVPSSubscriptionInput fields = %#v, want manifest %#v", tsFields, manifest)
	}
}

func jsonTagsOf(typ reflect.Type) []string {
	fields := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		tag := typ.Field(i).Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		if name == "" || name == "-" {
			continue
		}
		fields = append(fields, name)
	}
	return fields
}

func readVPSSubscriptionCreateManifest(t *testing.T, root string) []string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, "internal/center/http/handlers/vps_subscription_create_fields.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var fields []string
	if err := json.Unmarshal(body, &fields); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if len(fields) == 0 {
		t.Fatal("manifest is empty")
	}
	return fields
}

func parseCreateVPSSubscriptionInputFields(t *testing.T, path string) []string {
	t.Helper()
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	const marker = "export type CreateVPSSubscriptionInput = {"
	start := strings.Index(string(source), marker)
	if start < 0 {
		t.Fatalf("%s does not declare CreateVPSSubscriptionInput", path)
	}
	rest := string(source)[start+len(marker):]
	end := strings.Index(rest, "\n}")
	if end < 0 {
		t.Fatalf("%s CreateVPSSubscriptionInput is not a flat object type", path)
	}
	var fields []string
	for _, line := range strings.Split(rest[:end], "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		name, _, found := strings.Cut(line, ":")
		if !found {
			t.Fatalf("unexpected TypeScript field line %q", line)
		}
		fields = append(fields, strings.TrimSuffix(strings.TrimSpace(name), "?"))
	}
	return fields
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}
