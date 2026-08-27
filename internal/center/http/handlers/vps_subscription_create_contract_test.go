package handlers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"houfeng/internal/center/subscriptions"
)

type vpsSubscriptionCreateField struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
	Nullable bool   `json:"nullable"`
}

func TestVPSSubscriptionCreateRequestMatchesTypeScriptDTO(t *testing.T) {
	root := findRepoRoot(t)
	manifest := readVPSSubscriptionCreateManifest(t, root)
	goFields := jsonFieldContractsOf(reflect.TypeOf(vpsSubscriptionCreateRequest{}))
	tsFields := parseCreateVPSSubscriptionInputFields(t, filepath.Join(root, "web/src/lib/types.ts"))

	if !reflect.DeepEqual(namesOf(goFields), namesOf(manifest)) {
		t.Fatalf("Go json tags = %#v, want manifest names %#v", namesOf(goFields), namesOf(manifest))
	}
	if !reflect.DeepEqual(namesOf(tsFields), namesOf(manifest)) {
		t.Fatalf("CreateVPSSubscriptionInput fields = %#v, want manifest names %#v", namesOf(tsFields), namesOf(manifest))
	}
	for i, want := range manifest {
		got := goFields[i]
		if got.Name != want.Name || got.Type != want.Type || got.Nullable != want.Nullable {
			t.Fatalf("Go field %d = %#v, want name/type/nullable %#v", i, got, want)
		}
		if tsFields[i] != want {
			t.Fatalf("TypeScript field %d = %#v, want %#v", i, tsFields[i], want)
		}
	}
}

func TestVPSSubscriptionCreateManifestRejectsSemanticDrift(t *testing.T) {
	manifest := []vpsSubscriptionCreateField{
		{Name: "price", Type: "number", Required: true, Nullable: false},
		{Name: "auto_renew", Type: "boolean", Required: true, Nullable: false},
		{Name: "renew_at", Type: "date", Required: false, Nullable: true},
		{Name: "note", Type: "string", Required: true, Nullable: false},
	}

	type wrongPrice struct {
		Price string `json:"price"`
	}
	got := jsonFieldContractsOf(reflect.TypeOf(wrongPrice{}))[0]
	if got.Type == "number" {
		t.Fatal("string price must not classify as number")
	}

	ts := parseTSObjectFields(t, `export type Sample = {
  price: string
  auto_renew: string
  renew_at?: string
  note?: string
}`)
	if ts[0].Type == "number" {
		t.Fatal("price: string must not classify as number")
	}
	if ts[1].Type == "boolean" {
		t.Fatal("auto_renew: string must not classify as boolean")
	}
	if ts[2].Nullable {
		t.Fatal("renew_at?: string must not stay nullable")
	}
	if ts[3].Required {
		t.Fatal("note?: string must not stay required")
	}
	if ts[0].Type == manifest[0].Type && ts[3].Required == manifest[3].Required {
		t.Fatal("drift sample unexpectedly matched the manifest")
	}
}

func jsonFieldContractsOf(typ reflect.Type) []vpsSubscriptionCreateField {
	fields := make([]vpsSubscriptionCreateField, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		tag := field.Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		if name == "" || name == "-" {
			continue
		}
		goType := field.Type
		nullable := goType.Kind() == reflect.Pointer
		if nullable {
			goType = goType.Elem()
		}
		fields = append(fields, vpsSubscriptionCreateField{
			Name:     name,
			Type:     goJSONTypeName(goType),
			Nullable: nullable,
		})
	}
	return fields
}

func goJSONTypeName(typ reflect.Type) string {
	switch typ.Kind() {
	case reflect.Float32, reflect.Float64, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return "number"
	case reflect.Bool:
		return "boolean"
	case reflect.String:
		return "string"
	default:
		if typ == reflect.TypeOf(subscriptions.Date{}) {
			return "date"
		}
		return typ.String()
	}
}

func readVPSSubscriptionCreateManifest(t *testing.T, root string) []vpsSubscriptionCreateField {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, "internal/center/http/handlers/vps_subscription_create_fields.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var fields []vpsSubscriptionCreateField
	if err := json.Unmarshal(body, &fields); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if len(fields) == 0 {
		t.Fatal("manifest is empty")
	}
	for _, field := range fields {
		if field.Name == "" || field.Type == "" {
			t.Fatalf("manifest field missing name/type: %#v", field)
		}
	}
	return fields
}

func parseCreateVPSSubscriptionInputFields(t *testing.T, path string) []vpsSubscriptionCreateField {
	t.Helper()
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return parseTSObjectFields(t, string(source))
}

func parseTSObjectFields(t *testing.T, source string) []vpsSubscriptionCreateField {
	t.Helper()
	const marker = "export type CreateVPSSubscriptionInput = {"
	start := strings.Index(source, marker)
	sampleMarker := "export type Sample = {"
	if start < 0 {
		start = strings.Index(source, sampleMarker)
		if start < 0 {
			t.Fatal("TypeScript source does not declare CreateVPSSubscriptionInput or Sample")
		}
		return parseTSObjectBody(t, source[start+len(sampleMarker):])
	}
	return parseTSObjectBody(t, source[start+len(marker):])
}

func parseTSObjectBody(t *testing.T, rest string) []vpsSubscriptionCreateField {
	t.Helper()
	end := strings.Index(rest, "\n}")
	if end < 0 {
		t.Fatal("TypeScript object type is not a flat object type")
	}
	var fields []vpsSubscriptionCreateField
	for _, line := range strings.Split(rest[:end], "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		name, typeExpr, found := strings.Cut(line, ":")
		if !found {
			t.Fatalf("unexpected TypeScript field line %q", line)
		}
		required := !strings.HasSuffix(strings.TrimSpace(name), "?")
		name = strings.TrimSuffix(strings.TrimSpace(name), "?")
		typeExpr = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(typeExpr), ";"))
		nullable := strings.Contains(typeExpr, "null")
		fields = append(fields, vpsSubscriptionCreateField{
			Name:     name,
			Type:     tsJSONTypeName(typeExpr, nullable),
			Required: required,
			Nullable: nullable,
		})
	}
	return fields
}

func tsJSONTypeName(typeExpr string, nullable bool) string {
	if strings.Contains(typeExpr, "number") {
		return "number"
	}
	if strings.Contains(typeExpr, "boolean") {
		return "boolean"
	}
	if nullable && strings.Contains(typeExpr, "string") {
		return "date"
	}
	return "string"
}

func namesOf(fields []vpsSubscriptionCreateField) []string {
	names := make([]string, 0, len(fields))
	for _, field := range fields {
		names = append(names, field.Name)
	}
	return names
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
