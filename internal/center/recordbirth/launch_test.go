package recordbirth

import (
	"bytes"
	"encoding/hex"
	"errors"
	"maps"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/sourceretirement"
)

func launchTestDigest(b byte) [32]byte {
	var result [32]byte
	for i := range result {
		result[i] = b
	}
	return result
}

func launchInstalledVector() (LaunchBinding, string) {
	value := LaunchBinding{
		ProtocolVersion:  1,
		ProcessKind:      "content-processor",
		DeploymentID:     "dp-" + strings.Repeat("2", 64),
		SourceGeneration: 7,
		BirthInstalled:   true,
		Witness: sourceretirement.DomainIdentity{
			ID: "rd-" + strings.Repeat("1", 64), SystemIdentifier: "123456789",
			DatabaseOID: 42, DatabaseName: "witness_db", Epoch: 3,
		},
		BirthBinding: Binding{
			AuthorityID: "rd-" + strings.Repeat("1", 64), LineageID: "bl-" + strings.Repeat("3", 64),
			DeploymentID: "dp-" + strings.Repeat("2", 64), ProjectID: "default", Generation: 7,
			KeyID: "bk-" + strings.Repeat("4", 64), CutoverDigest: launchTestDigest(5),
		},
		ActivationBundleDigest: launchTestDigest(6), BinaryDigest: launchTestDigest(7),
	}
	// The JSON vector is specified independently of the production encoder.
	raw := `{"protocol_version":1,"process_kind":"content-processor","deployment_id":"dp-` + strings.Repeat("2", 64) +
		`","source_generation":7,"birth_installed":true,"witness":{"id":"rd-` + strings.Repeat("1", 64) +
		`","system_identifier":"123456789","database_oid":42,"database_name":"witness_db","epoch":3},` +
		`"birth_binding":{"authority_id":"rd-` + strings.Repeat("1", 64) + `","lineage_id":"bl-` + strings.Repeat("3", 64) +
		`","deployment_id":"dp-` + strings.Repeat("2", 64) + `","project_id":"default","generation":7,"key_id":"bk-` +
		strings.Repeat("4", 64) + `","cutover_digest":"` + strings.Repeat("05", 32) + `"},` +
		`"activation_bundle_digest":"` + strings.Repeat("06", 32) + `","binary_digest":"` + strings.Repeat("07", 32) + `"}`
	return value, raw
}

func launchOrdinaryVector() (LaunchBinding, string) {
	value := LaunchBinding{
		ProtocolVersion: 1, ProcessKind: "center", DeploymentID: "dp-" + strings.Repeat("a", 64),
		ActivationBundleDigest: launchTestDigest(0x0b), BinaryDigest: launchTestDigest(0x0c),
	}
	raw := `{"protocol_version":1,"process_kind":"center","deployment_id":"dp-` + strings.Repeat("a", 64) +
		`","source_generation":0,"birth_installed":false,"witness":{"id":"","system_identifier":"",` +
		`"database_oid":0,"database_name":"","epoch":0},"birth_binding":{"authority_id":"","lineage_id":"",` +
		`"deployment_id":"","project_id":"","generation":0,"key_id":"","cutover_digest":""},` +
		`"activation_bundle_digest":"` + strings.Repeat("0b", 32) + `","binary_digest":"` + strings.Repeat("0c", 32) + `"}`
	return value, raw
}

func TestLaunchBindingCanonicalVectors(t *testing.T) {
	installed, installedJSON := launchInstalledVector()
	ordinary, ordinaryJSON := launchOrdinaryVector()
	for _, tc := range []struct {
		name  string
		value LaunchBinding
		raw   string
	}{
		{"installed", installed, installedJSON},
		{"never enrolled", ordinary, ordinaryJSON},
	} {
		t.Run(tc.name, func(t *testing.T) {
			encoded, err := EncodeLaunchBinding(tc.value)
			if err != nil || string(encoded) != tc.raw {
				t.Fatalf("encoding = %s, error = %v; want %s", encoded, err, tc.raw)
			}
			decoded, err := DecodeLaunchBinding([]byte(tc.raw))
			if err != nil || decoded != tc.value {
				t.Fatalf("decoded = %+v, error = %v; want %+v", decoded, err, tc.value)
			}
		})
	}

	// A source-enrolled deployment that never installed birth still carries the
	// physical witness and generation, with a completely empty birth binding.
	enrolled := installed
	enrolled.BirthInstalled = false
	enrolled.BirthBinding = Binding{}
	encoded, err := EncodeLaunchBinding(enrolled)
	if err != nil {
		t.Fatalf("encode source-enrolled ordinary state: %v", err)
	}
	decoded, err := DecodeLaunchBinding(encoded)
	if err != nil || decoded != enrolled {
		t.Fatalf("decode source-enrolled ordinary state = %+v, %v", decoded, err)
	}

	maximum := installed
	maximum.SourceGeneration = math.MaxInt64
	maximum.BirthBinding.Generation = math.MaxInt64
	maximum.Witness.Epoch = math.MaxInt64
	encoded, err = EncodeLaunchBinding(maximum)
	if err != nil {
		t.Fatalf("encode maximum generation and epoch: %v", err)
	}
	decoded, err = DecodeLaunchBinding(encoded)
	if err != nil || decoded != maximum {
		t.Fatalf("decode maximum generation and epoch = %+v, %v", decoded, err)
	}
}

func TestLaunchBindingRejectsInvalidAuthority(t *testing.T) {
	valid, _ := launchInstalledVector()
	ordinary, _ := launchOrdinaryVector()
	tests := []struct {
		name   string
		base   LaunchBinding
		change func(*LaunchBinding)
	}{
		{"protocol zero", valid, func(v *LaunchBinding) { v.ProtocolVersion = 0 }},
		{"protocol future", valid, func(v *LaunchBinding) { v.ProtocolVersion = 2 }},
		{"unknown process", valid, func(v *LaunchBinding) { v.ProcessKind = "worker" }},
		{"invalid deployment", valid, func(v *LaunchBinding) { v.DeploymentID = "dp-" + strings.Repeat("A", 64) }},
		{"generation overflow", valid, func(v *LaunchBinding) { v.SourceGeneration = uint64(math.MaxInt64) + 1 }},
		{"zero activation digest", valid, func(v *LaunchBinding) { v.ActivationBundleDigest = [32]byte{} }},
		{"zero binary digest", valid, func(v *LaunchBinding) { v.BinaryDigest = [32]byte{} }},
		{"never enrolled with witness", ordinary, func(v *LaunchBinding) { v.Witness = valid.Witness }},
		{"never enrolled with birth", ordinary, func(v *LaunchBinding) { v.BirthInstalled = true }},
		{"enrolled without witness", ordinary, func(v *LaunchBinding) { v.SourceGeneration = 1 }},
		{"noncanonical witness system id", valid, func(v *LaunchBinding) { v.Witness.SystemIdentifier = "0123456789" }},
		{"zero witness database oid", valid, func(v *LaunchBinding) { v.Witness.DatabaseOID = 0 }},
		{"invalid witness database name", valid, func(v *LaunchBinding) { v.Witness.DatabaseName = "Witness" }},
		{"zero witness epoch", valid, func(v *LaunchBinding) { v.Witness.Epoch = 0 }},
		{"witness epoch overflow", valid, func(v *LaunchBinding) { v.Witness.Epoch = uint64(math.MaxInt64) + 1 }},
		{"wrong authority", valid, func(v *LaunchBinding) { v.BirthBinding.AuthorityID = "rd-" + strings.Repeat("f", 64) }},
		{"wrong deployment", valid, func(v *LaunchBinding) { v.BirthBinding.DeploymentID = "dp-" + strings.Repeat("f", 64) }},
		{"wrong generation", valid, func(v *LaunchBinding) { v.BirthBinding.Generation++ }},
		{"invalid lineage", valid, func(v *LaunchBinding) { v.BirthBinding.LineageID = "bl-invalid" }},
		{"wrong project", valid, func(v *LaunchBinding) { v.BirthBinding.ProjectID = "other" }},
		{"zero cutover", valid, func(v *LaunchBinding) { v.BirthBinding.CutoverDigest = [32]byte{} }},
		{"uninstalled with birth identity", valid, func(v *LaunchBinding) { v.BirthInstalled = false }},
		{"installed without birth identity", valid, func(v *LaunchBinding) { v.BirthBinding = Binding{} }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			value := tc.base
			tc.change(&value)
			if _, err := EncodeLaunchBinding(value); !errors.Is(err, ErrInvalid) {
				t.Fatalf("EncodeLaunchBinding accepted invalid binding: %v", err)
			}
		})
	}
}

func TestDecodeLaunchBindingRejectsNoncanonicalJSON(t *testing.T) {
	_, raw := launchOrdinaryVector()
	tests := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"too large", strings.Repeat(" ", 16385)},
		{"leading whitespace", " " + raw},
		{"trailing whitespace", raw + "\n"},
		{"second value", raw + `{}`},
		{"top-level null", "null"},
		{"unknown field", strings.TrimSuffix(raw, "}") + `,"extra":1}`},
		{"duplicate field", strings.Replace(raw, `"protocol_version":1`, `"protocol_version":1,"protocol_version":1`, 1)},
		{"duplicate nested field", strings.Replace(raw, `"database_oid":0`, `"database_oid":0,"database_oid":0`, 1)},
		{"missing top-level field", strings.Replace(raw, `"birth_installed":false,`, ``, 1)},
		{"missing nested field", strings.Replace(raw, `"system_identifier":"",`, ``, 1)},
		{"wrong field case", strings.Replace(raw, `"process_kind"`, `"Process_Kind"`, 1)},
		{"field reorder", strings.Replace(raw, `"protocol_version":1,"process_kind":"center"`, `"process_kind":"center","protocol_version":1`, 1)},
		{"null boolean", strings.Replace(raw, `"birth_installed":false`, `"birth_installed":null`, 1)},
		{"null generation", strings.Replace(raw, `"source_generation":0`, `"source_generation":null`, 1)},
		{"null witness", strings.Replace(raw, `"witness":{"id":"","system_identifier":"","database_oid":0,"database_name":"","epoch":0}`, `"witness":null`, 1)},
		{"null birth binding", strings.Replace(raw, `"birth_binding":{"authority_id":"","lineage_id":"","deployment_id":"","project_id":"","generation":0,"key_id":"","cutover_digest":""}`, `"birth_binding":null`, 1)},
		{"zero cutover encoded as hex", strings.Replace(raw, `"cutover_digest":""`, `"cutover_digest":"`+strings.Repeat("00", 32)+`"`, 1)},
		{"uppercase digest", strings.Replace(raw, strings.Repeat("0b", 32), strings.Repeat("0B", 32), 1)},
		{"zero binary digest", strings.Replace(raw, strings.Repeat("0c", 32), strings.Repeat("00", 32), 1)},
		{"escaped process spelling", strings.Replace(raw, `"center"`, `"\u0063enter"`, 1)},
		{"fractional version", strings.Replace(raw, `"protocol_version":1`, `"protocol_version":1.0`, 1)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.input == raw {
				t.Fatal("test mutation did not change the vector")
			}
			if _, err := DecodeLaunchBinding([]byte(tc.input)); !errors.Is(err, ErrInvalid) {
				t.Fatalf("DecodeLaunchBinding accepted noncanonical input: %v", err)
			}
		})
	}
}

func TestDigestExecutableStreamsRegularFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "image")
	for _, tc := range []struct {
		name    string
		content []byte
		want    string
	}{
		{"empty", nil, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		{"abc", []byte("abc"), "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"},
		{"million bytes", bytes.Repeat([]byte("a"), 1_000_000), "cdc76e5c9914fb9281a1c7e284d73e67f1809a48a497200e046d39ccc7112cd0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(path, tc.content, 0o600); err != nil {
				t.Fatal(err)
			}
			digest, err := DigestExecutable(path)
			if err != nil || hex.EncodeToString(digest[:]) != tc.want {
				t.Fatalf("digest = %x, error = %v; want %s", digest, err, tc.want)
			}
		})
	}
	if _, err := DigestExecutable(dir); !errors.Is(err, ErrInvalid) {
		t.Fatalf("digesting a directory = %v", err)
	}
	if _, err := DigestExecutable(filepath.Join(dir, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("digesting a missing image = %v", err)
	}
}

func TestWithRuntimeLaunchPreservesConnectionSettings(t *testing.T) {
	t.Setenv("PGSERVICE", "")
	t.Setenv("PGOPTIONS", "")
	digest := launchTestDigest(0x3a)
	inputs := []struct {
		name string
		dsn  string
	}{
		{
			"URI with options and fragment",
			"postgres://app:fixture%40secret@localhost:6432/records?sslmode=disable&pool_max_conns=7&application_name=record%20center&search_path=public&options=" + url.QueryEscape("-c statement_timeout=5000") + "#fragment",
		},
		{
			"keyword values with final empty value",
			"host=localhost port=6432 user=app password='fixture secret' dbname=records sslmode=disable application_name='worker pool' pool_min_conns=1 pool_max_conns=7 options='-c statement_timeout=5000' passfile=",
		},
		{
			"unrelated option containing parameter name",
			"postgres://app:fixture%40secret@localhost:6432/records?sslmode=disable&options=" + url.QueryEscape("-c application_name=houfeng.birth_launch_digest=example"),
		},
	}
	for _, tc := range inputs {
		t.Run(tc.name, func(t *testing.T) {
			before, err := pgxpool.ParseConfig(tc.dsn)
			if err != nil {
				t.Fatal("could not parse fixture connection")
			}
			withLaunch, err := WithRuntimeLaunch(tc.dsn, digest)
			if err != nil {
				t.Fatalf("adding launch binding: %v", err)
			}
			after, err := pgxpool.ParseConfig(withLaunch)
			if err != nil {
				t.Fatal("could not parse launch connection")
			}
			wantDigest := strings.Repeat("3a", 32)
			if got := after.ConnConfig.RuntimeParams["houfeng.birth_launch_digest"]; got != wantDigest {
				t.Fatalf("launch digest = %q, want %q", got, wantDigest)
			}
			otherParams := maps.Clone(after.ConnConfig.RuntimeParams)
			delete(otherParams, "houfeng.birth_launch_digest")
			if !maps.Equal(before.ConnConfig.RuntimeParams, otherParams) ||
				before.ConnConfig.Host != after.ConnConfig.Host || before.ConnConfig.Port != after.ConnConfig.Port ||
				before.ConnConfig.Database != after.ConnConfig.Database || before.ConnConfig.User != after.ConnConfig.User ||
				before.ConnConfig.Password != after.ConnConfig.Password || before.MaxConns != after.MaxConns ||
				before.MinConns != after.MinConns || (before.ConnConfig.TLSConfig == nil) != (after.ConnConfig.TLSConfig == nil) {
				t.Fatal("adding the launch digest changed other connection settings")
			}
		})
	}
}

func TestWithRuntimeLaunchRejectsPreloadedDigestAndRedactsErrors(t *testing.T) {
	t.Setenv("PGSERVICE", "")
	t.Setenv("PGOPTIONS", "")
	const base = "postgres://app:fixture_secret@localhost:6432/records?sslmode=disable"
	digest := launchTestDigest(0x3a)
	if _, err := WithRuntimeLaunch(base, [32]byte{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("zero digest error = %v", err)
	}
	for _, tc := range []struct {
		name string
		dsn  string
	}{
		{"URI parameter", base + "&houfeng.birth_launch_digest=already_set"},
		{"empty URI parameter", base + "&houfeng.birth_launch_digest="},
		{"case variant", base + "&HOUFENG.birth_launch_digest=already_set"},
		{"keyword parameter", "host=localhost user=app password=fixture_secret dbname=records sslmode=disable houfeng.birth_launch_digest=already_set"},
		{"options separated", base + "&options=" + url.QueryEscape("-c houfeng.birth_launch_digest=already_set")},
		{"options attached", base + "&options=" + url.QueryEscape("-choufeng.birth_launch_digest=already_set")},
		{"options long", base + "&options=" + url.QueryEscape("--houfeng.birth-launch-digest=already_set")},
		{"options escaped", base + "&options=" + url.QueryEscape("-c h\\oufeng.birth_launch_digest=already_set")},
		{"invalid connection", "postgres://app:fixture_secret@localhost:bad/records"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := WithRuntimeLaunch(tc.dsn, digest)
			if !errors.Is(err, ErrInvalid) || result != "" {
				t.Fatal("accepted an invalid or preloaded connection")
			}
			if strings.Contains(err.Error(), "fixture_secret") {
				t.Fatal("connection error exposed the password")
			}
		})
	}
	t.Run("environment options", func(t *testing.T) {
		t.Setenv("PGOPTIONS", "-c houfeng.birth_launch_digest=already_set")
		result, err := WithRuntimeLaunch(base, digest)
		if !errors.Is(err, ErrInvalid) || result != "" {
			t.Fatal("accepted a launch digest preloaded through PGOPTIONS")
		}
	})
}

func TestPostgresContinuityReaderRequiresPhysicalIdentityAndDistinctPools(t *testing.T) {
	launch, _ := launchInstalledVector()
	reader, app := &pgxpool.Pool{}, &pgxpool.Pool{}
	authority, err := NewPostgresContinuityReader(reader, app, launch.Witness)
	if err != nil || authority == nil {
		t.Fatalf("construct continuity reader: %v", err)
	}
	for _, tc := range []struct {
		name     string
		reader   *pgxpool.Pool
		app      *pgxpool.Pool
		identity sourceretirement.DomainIdentity
	}{
		{"missing reader", nil, app, launch.Witness},
		{"missing app", reader, nil, launch.Witness},
		{"same pool", app, app, launch.Witness},
		{"missing identity", reader, app, sourceretirement.DomainIdentity{}},
		{"bad authority id", reader, app, sourceretirement.DomainIdentity{
			ID: "rd-invalid", SystemIdentifier: "123456789", DatabaseOID: 42, DatabaseName: "witness_db", Epoch: 3,
		}},
		{"noncanonical system identifier", reader, app, sourceretirement.DomainIdentity{
			ID: launch.Witness.ID, SystemIdentifier: "0123456789", DatabaseOID: 42, DatabaseName: "witness_db", Epoch: 3,
		}},
		{"missing database oid", reader, app, sourceretirement.DomainIdentity{
			ID: launch.Witness.ID, SystemIdentifier: "123456789", DatabaseName: "witness_db", Epoch: 3,
		}},
		{"invalid database name", reader, app, sourceretirement.DomainIdentity{
			ID: launch.Witness.ID, SystemIdentifier: "123456789", DatabaseOID: 42, DatabaseName: "Witness", Epoch: 3,
		}},
		{"missing identity epoch", reader, app, sourceretirement.DomainIdentity{
			ID: launch.Witness.ID, SystemIdentifier: "123456789", DatabaseOID: 42, DatabaseName: "witness_db",
		}},
		{"identity epoch overflow", reader, app, sourceretirement.DomainIdentity{
			ID: launch.Witness.ID, SystemIdentifier: "123456789", DatabaseOID: 42, DatabaseName: "witness_db", Epoch: uint64(math.MaxInt64) + 1,
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := NewPostgresContinuityReader(tc.reader, tc.app, tc.identity)
			if result != nil || !errors.Is(err, ErrInvalid) {
				t.Fatalf("accepted an invalid continuity reader: %v", err)
			}
		})
	}
}
