package handlers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/subscriptions"
)

type vpsSubscriptionCreateField struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Format   string `json:"format,omitempty"`
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
		if got != want {
			t.Fatalf("Go field %d = %#v, want %#v", i, got, want)
		}
		if tsFields[i] != want {
			t.Fatalf("TypeScript field %d = %#v, want %#v", i, tsFields[i], want)
		}
	}
}

func TestVPSSubscriptionCreateRequestMapsEveryWrapperValue(t *testing.T) {
	startedAt := subscriptions.NewDate(time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC))
	renewAt := subscriptions.NewDate(time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC))
	request := vpsSubscriptionCreateRequest{
		Price:               subscriptions.PatchFloat(12.34),
		Currency:            subscriptions.PatchString(" usd "),
		BillingCycle:        subscriptions.PatchString(" custom "),
		BillingMonths:       subscriptions.PatchInt(7),
		BillingPeriodUnit:   subscriptions.PatchString(" week "),
		BillingPeriodLength: subscriptions.PatchInt(3),
		StartedAt:           subscriptions.PatchDate(&startedAt),
		RenewAt:             subscriptions.PatchDate(&renewAt),
		AutoRenew:           subscriptions.PatchBool(true),
		AutoRenewCancelled:  subscriptions.PatchBool(true),
		RenewalMode:         subscriptions.PatchString(" other "),
		PaymentMethod:       subscriptions.PatchString(" card "),
		Note:                subscriptions.PatchString(" production "),
	}

	got, ok := request.toCreateInput("vps_001")
	if !ok {
		t.Fatal("complete request was rejected")
	}
	want := subscriptions.CreateInput{
		VPSID:               "vps_001",
		Price:               12.34,
		Currency:            " usd ",
		BillingCycle:        " custom ",
		BillingMonths:       7,
		BillingPeriodUnit:   " week ",
		BillingPeriodLength: 3,
		StartedAt:           &startedAt,
		RenewAt:             &renewAt,
		AutoRenew:           true,
		AutoRenewCancelled:  true,
		RenewalMode:         " other ",
		Status:              subscriptions.DefaultStatus,
		PaymentMethod:       " card ",
		Note:                " production ",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("create input = %#v, want exact wrapper mapping %#v", got, want)
	}
}

func TestVPSSubscriptionCreateRequestEnforcesEveryRequiredTag(t *testing.T) {
	requestType := reflect.TypeOf(vpsSubscriptionCreateRequest{})
	request := vpsSubscriptionCreateRequest{}
	requestValue := reflect.ValueOf(&request).Elem()
	requiredCount := 0
	for i := 0; i < requestType.NumField(); i++ {
		field := requestType.Field(i)
		if field.Tag.Get("required") != "true" {
			continue
		}
		requiredCount++
		setField := requestValue.Field(i).FieldByName("Set")
		if !setField.IsValid() || setField.Kind() != reflect.Bool {
			t.Fatalf("required field %s does not expose boolean Set presence", field.Name)
		}
		setField.SetBool(true)
	}
	if requiredCount == 0 {
		t.Fatal("request has no required-tagged fields")
	}
	if _, ok := request.toCreateInput("vps_001"); !ok {
		t.Fatal("request with every required-tagged field set was rejected")
	}

	for i := 0; i < requestType.NumField(); i++ {
		field := requestType.Field(i)
		if field.Tag.Get("required") != "true" {
			continue
		}
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		t.Run(name, func(t *testing.T) {
			drifted := request
			driftedValue := reflect.ValueOf(&drifted).Elem()
			driftedValue.Field(i).FieldByName("Set").SetBool(false)
			if _, ok := drifted.toCreateInput("vps_001"); ok {
				t.Fatalf("required field %q was not enforced by toCreateInput", name)
			}
		})
	}
}

func TestVPSSubscriptionCreateManifestRejectsSemanticDrift(t *testing.T) {
	manifest := []vpsSubscriptionCreateField{
		{Name: "price", Type: "number", Required: true, Nullable: false},
		{Name: "auto_renew", Type: "boolean", Required: true, Nullable: false},
		{Name: "renew_at", Type: "string", Format: "date", Required: false, Nullable: true},
		{Name: "note", Type: "string", Required: true, Nullable: false},
	}

	type driftedGo struct {
		Price   subscriptions.OptionalString `json:"price" required:"true"`
		RenewAt subscriptions.OptionalString `json:"renew_at"`
		Note    subscriptions.OptionalString `json:"note"`
	}
	goDrift := jsonFieldContractsOf(reflect.TypeOf(driftedGo{}))
	if goDrift[0].Type == manifest[0].Type {
		t.Fatal("OptionalString price must not classify as number")
	}
	if goDrift[1].Nullable == manifest[2].Nullable {
		t.Fatal("OptionalString renew_at must not stay nullable")
	}
	if goDrift[2].Required == manifest[3].Required {
		t.Fatal("note without required tag must not stay required")
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

func TestJSONFieldContractsSeparateBaseTypeFormatAndNullability(t *testing.T) {
	type sample struct {
		Ordinary subscriptions.OptionalString `json:"ordinary"`
		Date     subscriptions.OptionalDate   `json:"date"`
	}

	want := []vpsSubscriptionCreateField{
		{Name: "ordinary", Type: "string", Nullable: false},
		{Name: "date", Type: "string", Format: "date", Nullable: true},
	}
	if got := jsonFieldContractsOf(reflect.TypeOf(sample{})); !reflect.DeepEqual(got, want) {
		t.Fatalf("Go field contracts = %#v, want base type/format/nullability separated as %#v", got, want)
	}
}

func TestVPSSubscriptionCreateManifestPreservesOptionalDateFormat(t *testing.T) {
	fields, err := decodeVPSSubscriptionCreateManifest([]byte(`[{"name":"renew_at","type":"string","format":"date","required":false,"nullable":true},{"name":"note","type":"string","required":true,"nullable":false}]`))
	if err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	want := []vpsSubscriptionCreateField{
		{Name: "renew_at", Type: "string", Format: "date", Nullable: true},
		{Name: "note", Type: "string", Required: true},
	}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("decoded manifest = %#v, want optional date format preserved as %#v", fields, want)
	}
}

func TestGoJSONTypeNameRejectsUnknownNamedPrimitiveType(t *testing.T) {
	type unknownJSONString string

	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("unknown named Go DTO type returned a mismatch string instead of being rejected")
		}
		if !strings.Contains(fmt.Sprint(recovered), "unsupported Go JSON field type") {
			t.Fatalf("panic = %v, want unsupported Go JSON field type rejection", recovered)
		}
	}()
	goJSONTypeName(reflect.TypeOf(unknownJSONString("")))
}

func TestJSONFieldContractsRejectExportedFieldsWithoutUsableJSONTags(t *testing.T) {
	tests := []struct {
		name string
		typ  reflect.Type
	}{
		{
			name: "missing json tag",
			typ: reflect.TypeOf(struct {
				Exposed string
			}{}),
		},
		{
			name: "empty json tag",
			typ: reflect.TypeOf(struct {
				Exposed string `json:""`
			}{}),
		},
		{
			name: "empty json name with options",
			typ: reflect.TypeOf(struct {
				Exposed string `json:",omitempty"`
			}{}),
		},
		{
			name: "near-miss json tag key",
			typ: reflect.TypeOf(struct {
				Exposed string `notjson:"exposed"`
			}{}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered == nil {
					t.Fatal("exported field without a usable json tag was silently omitted")
				}
			}()
			jsonFieldContractsOf(tt.typ)
		})
	}
}

func TestJSONFieldContractsUseExactRequiredTagKey(t *testing.T) {
	type sample struct {
		Value string `json:"value" notrequired:"true"`
	}

	got := jsonFieldContractsOf(reflect.TypeOf(sample{}))
	if len(got) != 1 || got[0].Required {
		t.Fatalf("json field contracts = %#v, want near-miss required key ignored", got)
	}
}

func TestJSONFieldContractsRejectAnonymousEmbeddedFields(t *testing.T) {
	type embeddedJSONFields struct {
		WireValue string `json:"wire_value"`
	}
	type ExportedEmbeddedJSONFields struct {
		WireValue string `json:"wire_value"`
	}

	wireSample := struct {
		embeddedJSONFields
	}{embeddedJSONFields: embeddedJSONFields{WireValue: "visible"}}
	body, err := json.Marshal(wireSample)
	if err != nil {
		t.Fatalf("marshal anonymous embedded field sample: %v", err)
	}
	if string(body) != `{"wire_value":"visible"}` {
		t.Fatalf("anonymous embedded field JSON = %s, want promoted wire_value", body)
	}
	var decoded struct {
		embeddedJSONFields
	}
	decoder := json.NewDecoder(strings.NewReader(`{"wire_value":"decoded"}`))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		t.Fatalf("strict decode anonymous embedded wire field: %v", err)
	}
	if decoded.WireValue != "decoded" {
		t.Fatalf("decoded anonymous wire value = %q, want decoded", decoded.WireValue)
	}

	tests := []struct {
		name string
		typ  reflect.Type
	}{
		{
			name: "unexported embedded value without tag",
			typ: reflect.TypeOf(struct {
				embeddedJSONFields
			}{}),
		},
		{
			name: "unexported embedded value with named tag",
			typ: reflect.TypeOf(struct {
				embeddedJSONFields `json:"embedded"`
			}{}),
		},
		{
			name: "unexported embedded pointer with named tag",
			typ: reflect.TypeOf(struct {
				*embeddedJSONFields `json:"embedded"`
			}{}),
		},
		{
			name: "dash with options is not the exact ignore tag",
			typ: reflect.TypeOf(struct {
				embeddedJSONFields `json:"-,omitempty"`
			}{}),
		},
		{
			name: "exported embedded value with named tag",
			typ: reflect.TypeOf(struct {
				ExportedEmbeddedJSONFields `json:"embedded"`
			}{}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				recovered := recover()
				if recovered == nil {
					t.Fatal("anonymous embedded Go JSON field was silently accepted or omitted")
				}
				if !strings.Contains(fmt.Sprint(recovered), "anonymous Go JSON field") {
					t.Fatalf("panic = %v, want anonymous Go JSON field rejection", recovered)
				}
			}()
			jsonFieldContractsOf(tt.typ)
		})
	}
}

func TestJSONFieldContractsOnlyIgnoreExplicitJSONDash(t *testing.T) {
	type ignoredEmbeddedJSONFields struct {
		Promoted string `json:"promoted"`
	}
	type IgnoredExportedEmbeddedJSONFields struct {
		Promoted string `json:"promoted"`
	}
	type sample struct {
		ignoredEmbeddedJSONFields         `json:"-"`
		IgnoredExportedEmbeddedJSONFields `json:"-"`
		Ignored                           string `json:"-"`
		NotIgnored                        string `json:"-,omitempty"`
		Visible                           string `json:"visible"`
	}

	got := jsonFieldContractsOf(reflect.TypeOf(sample{}))
	if len(got) != 2 || got[0].Name != "-" || got[1].Name != "visible" {
		t.Fatalf("json field contracts = %#v, want only exact json dash omitted", got)
	}
}

func TestJSONFieldContractsRejectUnknownNamedPointerType(t *testing.T) {
	type unknownDatePointer *subscriptions.Date
	type driftedGo struct {
		RenewAt unknownDatePointer `json:"renew_at"`
	}

	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("unknown named pointer type was accepted after generic pointer unwrapping")
		}
	}()
	jsonFieldContractsOf(reflect.TypeOf(driftedGo{}))
}

func TestVPSSubscriptionCreateManifestRequiresBooleanSemanticKeys(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "missing required", body: `[{"name":"note","type":"string","nullable":false}]`},
		{name: "null required", body: `[{"name":"note","type":"string","required":null,"nullable":false}]`},
		{name: "missing nullable", body: `[{"name":"note","type":"string","required":false}]`},
		{name: "null nullable", body: `[{"name":"note","type":"string","required":false,"nullable":null}]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if fields, err := decodeVPSSubscriptionCreateManifest([]byte(tt.body)); err == nil {
				t.Fatalf("decode manifest = %#v, want missing/null semantic key rejection", fields)
			}
		})
	}
}

func TestVPSSubscriptionCreateManifestRejectsInvalidTypeAndFormatSemantics(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "missing type", body: `[{"name":"value","required":false,"nullable":false}]`},
		{name: "null type", body: `[{"name":"value","type":null,"required":false,"nullable":false}]`},
		{name: "unknown type", body: `[{"name":"value","type":"date","required":false,"nullable":false}]`},
		{name: "null format", body: `[{"name":"value","type":"string","format":null,"required":false,"nullable":false}]`},
		{name: "unknown format", body: `[{"name":"value","type":"string","format":"datetime","required":false,"nullable":false}]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if fields, err := decodeVPSSubscriptionCreateManifest([]byte(tt.body)); err == nil {
				t.Fatalf("decode manifest = %#v, want type/format semantic rejection", fields)
			}
		})
	}
}

func TestTSJSONTypeNameAcceptsSupportedUnionMembers(t *testing.T) {
	tests := []struct {
		name         string
		typeExpr     string
		wantType     string
		wantNullable bool
	}{
		{name: "number", typeExpr: "number", wantType: "number"},
		{name: "boolean", typeExpr: "boolean", wantType: "boolean"},
		{name: "string", typeExpr: "string", wantType: "string"},
		{name: "nullable string", typeExpr: "string | null", wantType: "string", wantNullable: true},
		{name: "reordered nullable string", typeExpr: "null | string", wantType: "string", wantNullable: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType, gotNullable, ok := tsJSONTypeName(tt.typeExpr, nil)
			if !ok || gotType != tt.wantType || gotNullable != tt.wantNullable {
				t.Fatalf("tsJSONTypeName(%q) = (%q, nullable=%t, ok=%t), want (%q, nullable=%t, ok=true)", tt.typeExpr, gotType, gotNullable, ok, tt.wantType, tt.wantNullable)
			}
		})
	}
}

func TestParseTSObjectFieldsMapsOnlyExactISODateAliasToDateFormat(t *testing.T) {
	got := parseTSObjectFields(t, `export type ISODate = string
export type Sample = {
  ordinary: string | null
  date: ISODate | null
}`)
	want := []vpsSubscriptionCreateField{
		{Name: "ordinary", Type: "string", Required: true, Nullable: true},
		{Name: "date", Type: "string", Format: "date", Required: true, Nullable: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("TypeScript fields = %#v, want exact ISODate format mapping %#v", got, want)
	}
}

func TestParseTSObjectFieldsRejectsMissingOrWidenedISODateAlias(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{
			name: "missing",
			source: `export type Sample = {
  date: ISODate | null
}`,
		},
		{
			name: "widened",
			source: `export type ISODate = string | number
export type Sample = {
  date: ISODate | null
}`,
		},
		{
			name: "nullable definition",
			source: `export type ISODate = string | null
export type Sample = {
  date: ISODate | null
}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, err := parseTSObjectFieldsSource(tt.source); err == nil {
				t.Fatalf("invalid ISODate source was accepted as %#v", got)
			}
		})
	}
}

func TestTSJSONTypeNameRejectsInvalidUnionMembers(t *testing.T) {
	tests := []struct {
		name     string
		typeExpr string
	}{
		{name: "mixed primitive union", typeExpr: "number | string"},
		{name: "mixed boolean union", typeExpr: "boolean | string"},
		{name: "undefined member", typeExpr: "string | undefined"},
		{name: "unknown member", typeExpr: "string | UnknownAlias"},
		{name: "empty member", typeExpr: "string |"},
		{name: "union without primitive", typeExpr: "null"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, nullable, ok := tsJSONTypeName(tt.typeExpr, nil); ok {
				t.Fatalf("tsJSONTypeName(%q) = (%q, nullable=%t), want fail-closed rejection", tt.typeExpr, got, nullable)
			}
		})
	}
}

func TestParseTSObjectFieldsAcceptsSameSourceStringLiteralAliases(t *testing.T) {
	got := parseTSObjectFields(t, `export type BillingPeriodUnit = 'day' | 'month'
export type RenewalMode = 'auto' | 'manual'
export type Sample = {
  billing_period_unit?: BillingPeriodUnit | string
  renewal_mode?: string | RenewalMode
}`)
	want := []vpsSubscriptionCreateField{
		{Name: "billing_period_unit", Type: "string"},
		{Name: "renewal_mode", Type: "string"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("same-source alias fields = %#v, want %#v", got, want)
	}
}

func TestParseTSObjectFieldsAcceptsEscapedStringAliasLiterals(t *testing.T) {
	got := parseTSObjectFields(t, `export type BillingPeriodUnit = 'day|night' | 'owner\'s' | "quote\"" | 'back\\slash'
export type Sample = {
  billing_period_unit?: BillingPeriodUnit | string
}`)
	want := []vpsSubscriptionCreateField{{Name: "billing_period_unit", Type: "string"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("escaped same-source alias fields = %#v, want %#v", got, want)
	}
}

func TestParseTSObjectFieldsRequiresExactlyOneStringAliasDefinition(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		wantError string
	}{
		{
			name: "missing alias definition",
			source: `export type Sample = {
  value?: BillingPeriodUnit | string
}`,
			wantError: "unsupported TypeScript type expression",
		},
		{
			name: "duplicate alias definitions",
			source: `export type BillingPeriodUnit = 'day'
export type BillingPeriodUnit = 'month'
export type Sample = {
  value?: BillingPeriodUnit | string
}`,
			wantError: "BillingPeriodUnit declared more than once",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTSObjectFieldsSource(tt.source)
			if err == nil {
				t.Fatalf("alias source was accepted as %#v, want exact-one-definition rejection", got)
			}
			if !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("alias source error = %v, want %q", err, tt.wantError)
			}
		})
	}
}

func TestParseTSObjectFieldsRejectsCommentedShadowAliasDefinitions(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		wantError string
	}{
		{
			name: "shadow alias only",
			source: `/*
export type BillingPeriodUnit = 'day'
*/
export type Sample = {
  value?: BillingPeriodUnit | string
}`,
			wantError: "does not declare live BillingPeriodUnit",
		},
		{
			name: "shadow and live alias",
			source: `/*
export type BillingPeriodUnit = 'day'
*/
export type BillingPeriodUnit = 'month'
export type Sample = {
  value?: BillingPeriodUnit | string
}`,
			wantError: "BillingPeriodUnit declared more than once",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTSObjectFieldsSource(tt.source)
			if err == nil {
				t.Fatalf("commented shadow alias was accepted as %#v", got)
			}
			if !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("commented shadow alias error = %v, want %q", err, tt.wantError)
			}
		})
	}
}

func TestParseTSObjectFieldsRejectsInvalidStringAliasDefinitions(t *testing.T) {
	tests := []struct {
		name       string
		aliasName  string
		definition string
	}{
		{name: "BillingPeriodUnit widened with number", aliasName: "BillingPeriodUnit", definition: "'day' | number"},
		{name: "BillingPeriodUnit widened with undefined", aliasName: "BillingPeriodUnit", definition: "'day' | undefined"},
		{name: "RenewalMode widened with number", aliasName: "RenewalMode", definition: "'auto' | number"},
		{name: "RenewalMode widened with undefined", aliasName: "RenewalMode", definition: "'auto' | undefined"},
		{name: "alias with unknown member", aliasName: "BillingPeriodUnit", definition: "'day' | OtherUnit"},
		{name: "alias with empty member", aliasName: "RenewalMode", definition: "'auto' |"},
		{name: "alias with empty string literal", aliasName: "BillingPeriodUnit", definition: "'day' | ''"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := fmt.Sprintf(`export type %s = %s
export type Sample = {
  value?: %s | string
}`, tt.aliasName, tt.definition, tt.aliasName)
			got, err := parseTSObjectFieldsSource(source)
			if err == nil {
				t.Fatalf("invalid same-source alias was accepted as %#v", got)
			}
			if !strings.Contains(err.Error(), "unsupported TypeScript type expression") {
				t.Fatalf("invalid same-source alias error = %v, want unsupported TypeScript type expression", err)
			}
		})
	}
}

func TestParseTSObjectFieldsRejectsMultilineStringAliasWidening(t *testing.T) {
	tests := []struct {
		name          string
		aliasName     string
		firstMember   string
		widenedMember string
	}{
		{name: "BillingPeriodUnit widened with number", aliasName: "BillingPeriodUnit", firstMember: "'day'", widenedMember: "number"},
		{name: "BillingPeriodUnit widened with undefined", aliasName: "BillingPeriodUnit", firstMember: "'day'", widenedMember: "undefined"},
		{name: "RenewalMode widened with number", aliasName: "RenewalMode", firstMember: "'auto'", widenedMember: "number"},
		{name: "RenewalMode widened with undefined", aliasName: "RenewalMode", firstMember: "'auto'", widenedMember: "undefined"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := fmt.Sprintf(`export type %s = %s
  | %s
export type Sample = {
  value?: %s | string
}`, tt.aliasName, tt.firstMember, tt.widenedMember, tt.aliasName)
			got, err := parseTSObjectFieldsSource(source)
			if err == nil {
				t.Fatalf("multiline-widened same-source alias was accepted as %#v", got)
			}
			if !strings.Contains(err.Error(), "unsupported TypeScript type expression") {
				t.Fatalf("multiline-widened same-source alias error = %v, want unsupported TypeScript type expression", err)
			}
		})
	}
}

func TestParseTSObjectFieldsRejectsMultilineStringAliasWideningAfterTrivia(t *testing.T) {
	tests := []struct {
		name          string
		aliasName     string
		firstMember   string
		trivia        string
		widenedMember string
	}{
		{
			name:          "BillingPeriodUnit widened after line-comment trivia",
			aliasName:     "BillingPeriodUnit",
			firstMember:   "'day'",
			trivia:        "  // keep the union continuation hidden behind trivia\n",
			widenedMember: "number",
		},
		{
			name:          "RenewalMode widened after multiline block-comment trivia",
			aliasName:     "RenewalMode",
			firstMember:   "'auto'",
			trivia:        "  /* keep looking\n     across this comment */\n",
			widenedMember: "undefined",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := fmt.Sprintf(`export type %s = %s
%s  | %s
export type Sample = {
  value?: %s | string
}`, tt.aliasName, tt.firstMember, tt.trivia, tt.widenedMember, tt.aliasName)
			got, err := parseTSObjectFieldsSource(source)
			if err == nil {
				t.Fatalf("multiline-widened same-source alias after trivia was accepted as %#v", got)
			}
			if !strings.Contains(err.Error(), "unsupported TypeScript type expression") {
				t.Fatalf("multiline-widened same-source alias error = %v, want unsupported TypeScript type expression", err)
			}
		})
	}
}

func TestParseTSObjectFieldsRejectsDeclarationSuffix(t *testing.T) {
	got, err := parseTSObjectFieldsSource(`export type Sample = {
  value: string
} & { debug?: string }`)
	if err == nil {
		t.Fatalf("TypeScript object intersection was accepted as %#v", got)
	}
	if !strings.Contains(err.Error(), "unsupported declaration suffix") {
		t.Fatalf("TypeScript object intersection error = %v, want unsupported declaration suffix", err)
	}
}

func TestParseTSObjectFieldsRejectsMultilineDeclarationSuffix(t *testing.T) {
	got, err := parseTSObjectFieldsSource(`export type Sample = {
  value: string
}
& { debug?: string }`)
	if err == nil {
		t.Fatalf("multiline TypeScript object continuation was accepted as %#v", got)
	}
	if !strings.Contains(err.Error(), "unsupported declaration suffix") {
		t.Fatalf("multiline TypeScript object continuation error = %v, want unsupported declaration suffix", err)
	}
}

func TestParseTSObjectFieldsRejectsMultilineUnionSuffixAfterTrivia(t *testing.T) {
	tests := []struct {
		name   string
		trivia string
	}{
		{
			name:   "line-comment trivia",
			trivia: "  // the union continuation belongs to Sample\n",
		},
		{
			name:   "multiline block-comment trivia",
			trivia: "  /* keep looking\n     across this comment */\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := fmt.Sprintf(`export type Sample = {
  value: string
}

%s  | { debug?: string }`, tt.trivia)
			got, err := parseTSObjectFieldsSource(source)
			if err == nil {
				t.Fatalf("multiline TypeScript object union after trivia was accepted as %#v", got)
			}
			if !strings.Contains(err.Error(), "unsupported declaration suffix") {
				t.Fatalf("multiline TypeScript object union error = %v, want unsupported declaration suffix", err)
			}
		})
	}
}

func TestParseTSObjectFieldsAcceptsOptionalSemicolonBeforeFollowingDeclaration(t *testing.T) {
	got := parseTSObjectFields(t, `export type Sample = {
  value: string
};
export type Following = { debug?: string }`)
	want := []vpsSubscriptionCreateField{{Name: "value", Type: "string", Required: true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("TypeScript object before following declaration = %#v, want %#v", got, want)
	}
}

func TestParseTSObjectFieldsRejectsCommentedShadowDeclaration(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		wantError string
	}{
		{
			name: "shadow and live declaration",
			source: `/*
export type Sample = {
  value: string
}
*/
export type Sample = {
  value: number
}`,
			wantError: "declared more than once",
		},
		{
			name: "shadow declaration only",
			source: `/*
export type Sample = {
  value: string
}
*/`,
			wantError: "does not declare",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTSObjectFieldsSource(tt.source)
			if err == nil {
				t.Fatalf("commented shadow declaration was accepted as %#v", got)
			}
			if !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("commented shadow declaration error = %v, want %q", err, tt.wantError)
			}
		})
	}
}

func TestParseTSObjectFieldsAcceptsCRLFSemicolonBeforeFollowingDeclaration(t *testing.T) {
	source := strings.ReplaceAll(`export type Sample = {
  value: string
};
export type Following = { debug?: string }`, "\n", "\r\n")
	got := parseTSObjectFields(t, source)
	want := []vpsSubscriptionCreateField{{Name: "value", Type: "string", Required: true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CRLF TypeScript object before following declaration = %#v, want %#v", got, want)
	}
}

func jsonFieldContractsOf(typ reflect.Type) []vpsSubscriptionCreateField {
	fields := make([]vpsSubscriptionCreateField, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		tag, hasJSONTag := field.Tag.Lookup("json")
		if field.Anonymous {
			if hasJSONTag && tag == "-" {
				continue
			}
			panic(fmt.Sprintf("anonymous Go JSON field %s is not supported", field.Name))
		}
		if !field.IsExported() {
			continue
		}
		if tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if !hasJSONTag || name == "" {
			panic(fmt.Sprintf("exported Go JSON field %s must declare a usable json tag", field.Name))
		}
		goType := field.Type
		nullable := goType.Kind() == reflect.Pointer || goType == reflect.TypeOf(subscriptions.OptionalDate{})
		if goType.Kind() == reflect.Pointer {
			if goType.Name() != "" || !isSupportedGoJSONType(goType.Elem()) {
				panic(fmt.Sprintf("unsupported Go JSON pointer field type: %s", goType))
			}
			goType = goType.Elem()
		}
		fields = append(fields, vpsSubscriptionCreateField{
			Name:     name,
			Type:     goJSONTypeName(goType),
			Format:   goJSONFormat(goType),
			Required: field.Tag.Get("required") == "true",
			Nullable: nullable,
		})
	}
	return fields
}

func isSupportedGoJSONType(typ reflect.Type) bool {
	_, ok := supportedGoJSONTypeName(typ)
	return ok
}

func goJSONTypeName(typ reflect.Type) string {
	if name, ok := supportedGoJSONTypeName(typ); ok {
		return name
	}
	panic(fmt.Sprintf("unsupported Go JSON field type: %s", typ))
}

func supportedGoJSONTypeName(typ reflect.Type) (string, bool) {
	switch typ {
	case reflect.TypeOf(subscriptions.OptionalFloat{}), reflect.TypeOf(subscriptions.OptionalInt{}),
		reflect.TypeOf(float32(0)), reflect.TypeOf(float64(0)),
		reflect.TypeOf(int(0)), reflect.TypeOf(int8(0)), reflect.TypeOf(int16(0)),
		reflect.TypeOf(int32(0)), reflect.TypeOf(int64(0)):
		return "number", true
	case reflect.TypeOf(subscriptions.OptionalBool{}), reflect.TypeOf(false):
		return "boolean", true
	case reflect.TypeOf(subscriptions.OptionalString{}), reflect.TypeOf(""):
		return "string", true
	case reflect.TypeOf(subscriptions.OptionalDate{}), reflect.TypeOf(subscriptions.Date{}):
		return "string", true
	default:
		return "", false
	}
}

func goJSONFormat(typ reflect.Type) string {
	switch typ {
	case reflect.TypeOf(subscriptions.OptionalDate{}), reflect.TypeOf(subscriptions.Date{}):
		return "date"
	default:
		return ""
	}
}

func readVPSSubscriptionCreateManifest(t *testing.T, root string) []vpsSubscriptionCreateField {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, "internal/center/http/handlers/vps_subscription_create_fields.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	fields, err := decodeVPSSubscriptionCreateManifest(body)
	if err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	return fields
}

func decodeVPSSubscriptionCreateManifest(body []byte) ([]vpsSubscriptionCreateField, error) {
	var rawFields []struct {
		Name     *string         `json:"name"`
		Type     *string         `json:"type"`
		Format   json.RawMessage `json:"format"`
		Required *bool           `json:"required"`
		Nullable *bool           `json:"nullable"`
	}
	if err := json.Unmarshal(body, &rawFields); err != nil {
		return nil, err
	}
	if len(rawFields) == 0 {
		return nil, fmt.Errorf("manifest is empty")
	}
	fields := make([]vpsSubscriptionCreateField, 0, len(rawFields))
	for i, rawField := range rawFields {
		if rawField.Name == nil || rawField.Type == nil || rawField.Required == nil || rawField.Nullable == nil {
			return nil, fmt.Errorf("manifest field %d must include name, type, required, nullable", i)
		}
		field := vpsSubscriptionCreateField{
			Name:     *rawField.Name,
			Type:     *rawField.Type,
			Required: *rawField.Required,
			Nullable: *rawField.Nullable,
		}
		if field.Name == "" || (field.Type != "number" && field.Type != "string" && field.Type != "boolean") {
			return nil, fmt.Errorf("manifest field missing name/type: %#v", field)
		}
		if len(rawField.Format) > 0 {
			if string(rawField.Format) == "null" || json.Unmarshal(rawField.Format, &field.Format) != nil || field.Format != "date" {
				return nil, fmt.Errorf("manifest field %d has invalid format", i)
			}
			if field.Type != "string" {
				return nil, fmt.Errorf("manifest field %d format requires string type", i)
			}
		}
		fields = append(fields, field)
	}
	return fields, nil
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
	fields, err := parseTSObjectFieldsSource(source)
	if err != nil {
		t.Fatal(err)
	}
	return fields
}

func parseTSObjectFieldsSource(source string) ([]vpsSubscriptionCreateField, error) {
	const marker = "export type CreateVPSSubscriptionInput = {"
	start, found, err := uniqueLiveTSDeclarationStart(source, marker, "CreateVPSSubscriptionInput")
	if err != nil {
		return nil, err
	}
	if found {
		stringAliases, aliasErr := verifiedTSStringLiteralAliases(source)
		if aliasErr != nil {
			return nil, aliasErr
		}
		dateAliases, aliasErr := verifiedTSISODateAliases(source)
		if aliasErr != nil {
			return nil, aliasErr
		}
		return parseTSObjectBody(source[start+len(marker):], stringAliases, dateAliases)
	}

	const sampleMarker = "export type Sample = {"
	start, found, err = uniqueLiveTSDeclarationStart(source, sampleMarker, "Sample")
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("TypeScript source does not declare CreateVPSSubscriptionInput or Sample")
	}
	stringAliases, aliasErr := verifiedTSStringLiteralAliases(source)
	if aliasErr != nil {
		return nil, aliasErr
	}
	dateAliases, aliasErr := verifiedTSISODateAliases(source)
	if aliasErr != nil {
		return nil, aliasErr
	}
	return parseTSObjectBody(source[start+len(sampleMarker):], stringAliases, dateAliases)
}

func uniqueLiveTSDeclarationStart(source string, marker string, declarationName string) (int, bool, error) {
	rawCount := strings.Count(source, marker)
	if rawCount > 1 {
		return 0, false, fmt.Errorf("TypeScript %s declared more than once", declarationName)
	}
	if rawCount == 0 {
		return 0, false, nil
	}

	liveStarts := liveTSDeclarationStarts(source, marker)
	if len(liveStarts) != 1 {
		return 0, false, fmt.Errorf("TypeScript source does not declare live %s", declarationName)
	}
	return liveStarts[0], true, nil
}

func liveTSDeclarationStarts(source string, marker string) []int {
	const (
		sourceCode = iota
		sourceLineComment
		sourceBlockComment
		sourceSingleQuote
		sourceDoubleQuote
		sourceBacktick
	)
	state := sourceCode
	escaped := false
	lineStart := 0
	liveStarts := make([]int, 0, 1)
	for index := 0; index < len(source); index++ {
		character := source[index]
		var nextCharacter byte
		if index+1 < len(source) {
			nextCharacter = source[index+1]
		}
		if character == '\n' {
			lineStart = index + 1
		}

		switch state {
		case sourceLineComment:
			if character == '\n' {
				state = sourceCode
			}
			continue
		case sourceBlockComment:
			if character == '*' && nextCharacter == '/' {
				state = sourceCode
				index++
			}
			continue
		case sourceSingleQuote, sourceDoubleQuote, sourceBacktick:
			quote := byte('\'')
			if state == sourceDoubleQuote {
				quote = '"'
			} else if state == sourceBacktick {
				quote = '`'
			}
			if escaped {
				escaped = false
			} else if character == '\\' {
				escaped = true
			} else if character == quote {
				state = sourceCode
			}
			continue
		}

		switch {
		case character == '/' && nextCharacter == '/':
			state = sourceLineComment
			index++
		case character == '/' && nextCharacter == '*':
			state = sourceBlockComment
			index++
		case character == '\'':
			state = sourceSingleQuote
		case character == '"':
			state = sourceDoubleQuote
		case character == '`':
			state = sourceBacktick
		case strings.HasPrefix(source[index:], marker) && onlyHorizontalWhitespace(source[lineStart:index]):
			liveStarts = append(liveStarts, index)
			index += len(marker) - 1
		}
	}
	return liveStarts
}

func onlyHorizontalWhitespace(value string) bool {
	for _, character := range value {
		if character != ' ' && character != '\t' {
			return false
		}
	}
	return true
}

func parseTSObjectBody(rest string, stringAliases map[string]struct{}, dateAliases map[string]struct{}) ([]vpsSubscriptionCreateField, error) {
	end := strings.Index(rest, "\n}")
	if end < 0 {
		return nil, fmt.Errorf("TypeScript object type is not a flat object type")
	}
	suffixStart := end + 2
	suffixEnd := strings.IndexByte(rest[suffixStart:], '\n')
	if suffixEnd < 0 {
		suffixEnd = len(rest)
	} else {
		suffixEnd += suffixStart
	}
	suffix := strings.TrimSpace(rest[suffixStart:suffixEnd])
	if suffix != "" && suffix != ";" {
		return nil, fmt.Errorf("TypeScript object type has unsupported declaration suffix %q", suffix)
	}
	if suffix == "" && suffixEnd < len(rest) && startsWithTSTypeContinuationAfterTrivia(rest[suffixEnd+1:]) {
		return nil, fmt.Errorf("TypeScript object type has unsupported declaration suffix after its closing brace")
	}
	var fields []vpsSubscriptionCreateField
	for _, line := range strings.Split(rest[:end], "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		name, typeExpr, found := strings.Cut(line, ":")
		if !found {
			return nil, fmt.Errorf("unexpected TypeScript field line %q", line)
		}
		required := !strings.HasSuffix(strings.TrimSpace(name), "?")
		name = strings.TrimSuffix(strings.TrimSpace(name), "?")
		typeExpr = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(typeExpr), ";"))
		typeName, format, nullable, ok := tsJSONFieldContract(typeExpr, stringAliases, dateAliases)
		if !ok {
			return nil, fmt.Errorf("unsupported TypeScript type expression %q", typeExpr)
		}
		fields = append(fields, vpsSubscriptionCreateField{
			Name:     name,
			Type:     typeName,
			Format:   format,
			Required: required,
			Nullable: nullable,
		})
	}
	return fields, nil
}

var approvedTSStringAliasNames = [...]string{"BillingPeriodUnit", "RenewalMode"}

func verifiedTSStringLiteralAliases(source string) (map[string]struct{}, error) {
	aliases := make(map[string]struct{}, len(approvedTSStringAliasNames))
	for _, aliasName := range approvedTSStringAliasNames {
		marker := "export type " + aliasName + " ="
		start, found, err := uniqueLiveTSDeclarationStart(source, marker, aliasName)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		definitionRest := source[start+len(marker):]
		if lineEnd := strings.IndexByte(definitionRest, '\n'); lineEnd >= 0 {
			if startsWithTSTypeContinuationAfterTrivia(definitionRest[lineEnd+1:]) {
				continue
			}
			definitionRest = definitionRest[:lineEnd]
		}
		definition := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(definitionRest), ";"))
		members, splitOK := splitTSUnionMembers(definition)
		validDefinition := splitOK && len(members) > 0
		for _, member := range members {
			if !isNonEmptyTypeScriptStringLiteral(strings.TrimSpace(member)) {
				validDefinition = false
				break
			}
		}
		if validDefinition {
			aliases[aliasName] = struct{}{}
		}
	}
	return aliases, nil
}

func verifiedTSISODateAliases(source string) (map[string]struct{}, error) {
	const aliasName = "ISODate"
	aliases := make(map[string]struct{}, 1)
	marker := "export type " + aliasName + " ="
	start, found, err := uniqueLiveTSDeclarationStart(source, marker, aliasName)
	if err != nil {
		return nil, err
	}
	if !found {
		return aliases, nil
	}
	definitionRest := source[start+len(marker):]
	if lineEnd := strings.IndexByte(definitionRest, '\n'); lineEnd >= 0 {
		if startsWithTSTypeContinuationAfterTrivia(definitionRest[lineEnd+1:]) {
			return aliases, nil
		}
		definitionRest = definitionRest[:lineEnd]
	}
	definition := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(definitionRest), ";"))
	if definition == "string" {
		aliases[aliasName] = struct{}{}
	}
	return aliases, nil
}

func startsWithTSTypeContinuationAfterTrivia(source string) bool {
	for index := 0; index < len(source); {
		switch source[index] {
		case ' ', '\t', '\r', '\n':
			index++
			continue
		case '/':
			if index+1 >= len(source) {
				return false
			}
			switch source[index+1] {
			case '/':
				lineEnd := strings.IndexByte(source[index+2:], '\n')
				if lineEnd < 0 {
					return false
				}
				index += lineEnd + 3
				continue
			case '*':
				commentEnd := strings.Index(source[index+2:], "*/")
				if commentEnd < 0 {
					return true
				}
				index += commentEnd + 4
				continue
			}
		}
		return source[index] == '&' || source[index] == '|'
	}
	return false
}

func splitTSUnionMembers(expression string) ([]string, bool) {
	members := make([]string, 0, 1+strings.Count(expression, "|"))
	memberStart := 0
	var quote byte
	escaped := false
	for index := 0; index < len(expression); index++ {
		character := expression[index]
		if quote != 0 {
			if escaped {
				escaped = false
			} else if character == '\\' {
				escaped = true
			} else if character == quote {
				quote = 0
			}
			continue
		}
		if character == '\'' || character == '"' {
			quote = character
		} else if character == '|' {
			members = append(members, expression[memberStart:index])
			memberStart = index + 1
		}
	}
	if quote != 0 || escaped {
		return nil, false
	}
	return append(members, expression[memberStart:]), true
}

func isNonEmptyTypeScriptStringLiteral(member string) bool {
	if len(member) < 3 {
		return false
	}
	quote := member[0]
	if (quote != '\'' && quote != '"') || member[len(member)-1] != quote {
		return false
	}
	escaped := false
	for index := 1; index < len(member)-1; index++ {
		character := member[index]
		if escaped {
			escaped = false
			continue
		}
		if character == '\\' {
			escaped = true
			continue
		}
		if character == quote {
			return false
		}
	}
	return !escaped
}

func tsJSONTypeName(typeExpr string, stringAliases map[string]struct{}) (string, bool, bool) {
	typeName, _, nullable, ok := tsJSONFieldContract(typeExpr, stringAliases, nil)
	return typeName, nullable, ok
}

func tsJSONFieldContract(typeExpr string, stringAliases map[string]struct{}, dateAliases map[string]struct{}) (string, string, bool, bool) {
	primitiveKind := ""
	nullable := false
	hasDateAlias := false
	hasOrdinaryString := false
	for _, rawMember := range strings.Split(typeExpr, "|") {
		member := strings.TrimSpace(rawMember)
		if member == "" {
			return "", "", false, false
		}

		kind := ""
		switch member {
		case "null":
			nullable = true
			continue
		case "number":
			kind = "number"
		case "boolean":
			kind = "boolean"
		case "string":
			kind = "string"
			hasOrdinaryString = true
		default:
			if _, ok := dateAliases[member]; ok {
				kind = "string"
				hasDateAlias = true
			} else if _, ok := stringAliases[member]; ok {
				kind = "string"
				hasOrdinaryString = true
			} else {
				return "", "", false, false
			}
		}
		if primitiveKind != "" && primitiveKind != kind {
			return "", "", false, false
		}
		primitiveKind = kind
	}
	if primitiveKind == "" {
		return "", "", false, false
	}
	if hasDateAlias && hasOrdinaryString {
		return "", "", false, false
	}
	if hasDateAlias {
		return "string", "date", nullable, true
	}
	return primitiveKind, "", nullable, true
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
