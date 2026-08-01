package migrate

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io/fs"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	appaclr2migrations "houfeng/db/appaclr2/migrations"
	rootmigrations "houfeng/db/migrations"
)

const appACLR2SourceSetMagic = "HOUFENG-APP-ACL-R2-SOURCE-SET-V1"
const appACLR2SourceSetVersion uint16 = 1
const appACLR2SourceSetEntryCount = 53
const appACLR2RootSourceEntryCount = 52
const appACLR2MaximumBodyBytes = 4 * 1024 * 1024
const appACLR2SourceSetDigestHex = "1d9dc20e71e9f319f8b1cef4b22f9dc92051a88dc9cb8a892b69494658c44dd3"

// appACLR2SourceContract is an R2-owned closed snapshot. It deliberately does
// not reference the frozen R1 source variable or any V1 source encoder/parser.
const appACLR2SourceContract = `
ce142a92c075bd7d3fdd850a2fa42f1220335b379284e45f4e2009459dac9c9a 0001_initial_schema.sql
e1fe4c4c9f6158d2db139d1b4af622e60222a163a4950a068b6d3a2925514c91 0002_normalize_status_defaults.sql
e46942cc6539f35066d8d665c0886d12a2dccdc26c44a2a983ac8bd6346cb47a 0003_add_sync_token_hash.sql
5344ed2d3c21fa47bba6aa66ae7f1a9c941c6a5e7e258e8d8997a725b20f0f6d 0004_add_node_onboarding_binding_state.sql
91bf3bc806ea1bede1a7ce4c4c6582b6deaf6397e5e452b043c9ebada0abf4d6 0004_add_observation_provenance.sql
784f424986a98d01de9336e41b4bc5b543ca93bdfd9bb0d05edcdc31820457c5 0005_add_node_binding_epoch.sql
00ce064480b61da38301f28a3032d52ed62339729279283cf5bcf6b385a1360a 0006_add_center_settings.sql
58f5b4165554f6367d775951b495b511a662f5bd59814a0700c12b665924bfc9 0007_add_telegram_runtime_managed.sql
5a61db4111fef6bb7cb1f5a6e51e5408a3baf92de5f18c511784a85a1981d2ed 0008_add_retention_aggregates.sql
0e0b6e81065e0925ccc4edc1198a8b7fc40492c87c54252118a8f8c335c33542 0009_add_observability_filter_indexes.sql
a7a9c846a47b644a43891940c412fccca2dd15b3a966a378a49619836cececa5 0010_add_users_and_sessions.sql
a5a878b821f84a366b61b8e034d3b22f06a95f58fc029b917d968a2d90f15ab2 0011_normalize_sessions_index_names.sql
38c028429dec06041637f17aed48d3e495127b35fdb29c1af51660475c7374e9 0012_add_node_pending_action.sql
77784fecd480aba0e604c6a317da172b6724152a6ded2fc237e3dc9b5a8139bc 0013_add_node_target_group.sql
930b64128afc7aabc3dea433990efc5541df45a93a1dea3905d70d6eb8516f73 0014_add_feishu_settings.sql
55de4f74813efba1745b7d45bf541b91e10ea75f300b16ade307aeb160f31355 0015_add_host_containers.sql
416523a721af7319bd5e4a586469400f9744aa0f72339558ebe8f22e5296e979 0016_create_asset_ledger.sql
3e5cb7a4161262e6fcd922569a09bd244c83a178a3ab8d883ddb81cf23192b68 0017_add_vps_assets.sql
d294530338a50229ce94488fa03fc7771f6246dd7727b92664a550b37dbb799f 0018_add_subscriptions.sql
27caa6e93b47b681d2be1061abb5ddc629658d4c43e7116a8d4ac75e9a5314e8 0019_create_vps_node_links.sql
240b42d834b7774d57bb1f3f8937d6f9b639c6d9642c208c4a2470cd2c3841ab 0020_create_renewal_decisions.sql
8573b12544750fbc784fe3090981e32ed9a9ee5ca8ff9e8bd077ab9003e1c67c 0021_create_asset_histories.sql
b7e21c062e6c3c0b5ff7e6e48ea6c5a392bb965a0c3edda7439495881f42b15b 0022_create_experience_logs.sql
18462b045da051d4579c5412df11c5c4c29862c9bb4e90cdba08e73e16be7245 0023_create_asset_services.sql
cb6ccb7ddc2bd6cb925de1dd931f5eb7b9bbdece69b1f921e0f1f7b624246dd3 0024_create_asset_domains.sql
52a85d2ed4d032f2c017e241817d573eae1ad21ac7cf01c80ecf42e8ac163195 0025_add_enrollment_token_consumption.sql
4d306f6d35d37b3230bb488d4554893f9afdfaaa1208edc4c6d1863b8482334c 0026_tune_observability_cadence.sql
0068937f3eac1c5786cb395db714199299b6d9c8d1ccb69f91169be37ed922c0 0027_add_host_sample_capacity_bytes.sql
6f2a612a89e88a9e4c7876d15688e560d44646a57be9fd0e59dccd9c6efaa1c7 0028_create_asset_lifecycle_actions.sql
9ec925a0d92530b4ee594a3a44c254c3c873c68e97bc88ed973a02d9606a4872 0029_rename_nodes_to_monitoring_instances.sql
830ab90f392a8d45a5ccd32d9c016bcb56f27d7d9dad3cdef5e76f01f0ae6e42 0030_vps_first_status_semantics.sql
882c0ab29c48dd24d42a95da70404d9c653fb99049b245be69c11219c81372c8 0031_subscription_periods_and_validity_extension.sql
3e8439519e755e30953242dc1971c6810299f89b49eeffac257bea368f64597c 0032_extend_raw_retention_for_monitoring_windows.sql
ccbd350e0cfdf281e23a04a586ada8939a7cb498a40c44be6fc72a0e33b30062 0033_subscription_cost_center.sql
b54257bc406028ed254fb7039fe38c42d7f4de09ddf18c43da433430d28a1244 0034_subscription_monthly_budgets.sql
27a63c232f5676428cb13ca9024606c699f7b08995ce0acb1df30631f6a4dcc0 0035_create_asset_decision_records.sql
884c1bbd9857d7048c1e077a0f09c788e943ee8f7b5c74403cb11a1b19b6ce78 0036_add_asset_decision_member_followups.sql
2d48e923aaf72fc1779371ec7304e78bb89febdace8e642d52da8665cea1ac98 0037_create_asset_decision_manual_groups.sql
c7db689d3667040501182c2138fb9e0739cff022df5ffe89712e5bf483d3caab 0038_create_asset_decision_scenario_templates.sql
34b5935799c40d63dd259e53553f960d7f5474e460e493d100631fe34dddc693 0039_add_ip_quality_settings.sql
b87c6bba93e522f577a70760d894723df7828b27cd1713c52e0363cd3b34a0cb 0040_create_ip_quality_reports.sql
e78f0030444ff3651bdb58dde513767a294bd93a44180a8a5944b478e5e68262 0041_filter_ip_quality_read_models.sql
f80d260524b91dc1f8bffaa1b03c84e674d585408a705c073ce8e4792667e5d4 0042_extend_ip_quality_source_details.sql
e871bd56bb88b208db7cb3a5112f3925f77280e2aa3a0450191eedea0121ba15 0043_add_monitoring_instance_management.sql
cfe742aa08f27fd8453e22f1dce94a10dfbf170a1e4ffe0cfc1fee1b02409255 0044_hash_session_ids.sql
550211d7dd33a9fe738a9c3b592bd0ff60afd10190a8fea87ba8b043d1270b66 0045_create_agent_sync_batches.sql
c3ced98ae37fe0eb0917636f0e68c631755db6550b2eec229edd111051e74c9e 0046_create_command_action_audit.sql
4acd64fd485825d3a4f6c1fa28b12b61f6b73b962a259a588d42104dca4399c3 0047_ip_quality_stale_after_settings.sql
6265169a254210451e9bd9b7c73fd1ba2a88b3f75fa50b4c135536343ce04d36 0048_subscription_gift_renewal_mode.sql
e89272ac520969d25f4a68f7c1ec6cecbd5512b8953cb345e95c20b56e7edf05 0049_vps_asset_state_combination_constraint.sql
bb333c71a9a10e250022a4b5189990aed7344f24920b54916c77eead37736c28 0050_extend_command_action_audit.sql
503d58670dc790c4b852bfb58cf93d2b816c1ce956958567dc605cb28d5cd23f 0051_create_record_platform_foundation.sql
23f79c60dcede45a42aae82da5a9de0d3d650d7eef64dbfd7ce96c6dd5d95fff 0052_app_acl_r2_privileged_transition.sql
`

// AppACLSourceEntryR2V1 is one filename/full-file-SHA-256 pair in the isolated
// R2 source snapshot.
type AppACLSourceEntryR2V1 struct {
	Filename string
	Digest   [32]byte
}

// AppACLSourceSetR2V1 is the decoded fixed 53-entry R2 source body.
type AppACLSourceSetR2V1 struct {
	Entries []AppACLSourceEntryR2V1
}

// CompileAppACLSourceSetR2V1 verifies both embedded filesystems against the
// R2-owned snapshot before returning its canonical body.
func CompileAppACLSourceSetR2V1() ([]byte, error) {
	expected, err := appACLR2ExpectedSourceEntries()
	if err != nil {
		return nil, err
	}
	if err := verifyAppACLR2EmbeddedSource(rootmigrations.FS, expected[:appACLR2RootSourceEntryCount], "root"); err != nil {
		return nil, err
	}
	if err := verifyAppACLR2EmbeddedSource(appaclr2migrations.FS, expected[appACLR2RootSourceEntryCount:], "isolated"); err != nil {
		return nil, err
	}
	return CanonicalAppACLSourceSetR2BodyV1(expected)
}

// CanonicalAppACLSourceSetR2BodyV1 encodes only the fixed R2 snapshot. The
// caller must supply the exact canonical order; no path cleaning or sorting is
// performed.
func CanonicalAppACLSourceSetR2BodyV1(entries []AppACLSourceEntryR2V1) ([]byte, error) {
	expected, err := appACLR2ExpectedSourceEntries()
	if err != nil {
		return nil, err
	}
	if len(entries) != len(expected) {
		return nil, fmt.Errorf("APP ACL R2 source set has %d entries, want %d", len(entries), len(expected))
	}
	for index, entry := range entries {
		if !validAppACLR2MigrationFilename(entry.Filename) {
			return nil, fmt.Errorf("APP ACL R2 source entry %d has invalid filename", index)
		}
		if index > 0 && entries[index-1].Filename >= entry.Filename {
			return nil, fmt.Errorf("APP ACL R2 source entries are not strictly sorted")
		}
		if entry != expected[index] {
			return nil, fmt.Errorf("APP ACL R2 source entry %d does not match the fixed snapshot", index)
		}
	}

	body := make([]byte, 0, len(appACLR2SourceSetMagic)+4+len(entries)*64)
	body = append(body, appACLR2SourceSetMagic...)
	body = appendAppACLR2Uint16(body, appACLR2SourceSetVersion)
	body = appendAppACLR2Uint16(body, uint16(len(entries)))
	for _, entry := range entries {
		body = appendAppACLR2String16(body, entry.Filename)
		body = append(body, entry.Digest[:]...)
	}
	wantDigest, err := appACLR2DigestFromHex(appACLR2SourceSetDigestHex)
	if err != nil {
		return nil, err
	}
	if sha256.Sum256(body) != wantDigest {
		return nil, fmt.Errorf("APP ACL R2 source body does not match its fixed digest")
	}
	return body, nil
}

// ParseCanonicalAppACLSourceSetR2BodyV1 accepts only the fixed 53-entry R2
// snapshot and requires strict EOF and byte-identical re-encoding.
func ParseCanonicalAppACLSourceSetR2BodyV1(body []byte) (AppACLSourceSetR2V1, error) {
	if len(body) > appACLR2MaximumBodyBytes || !bytes.HasPrefix(body, []byte(appACLR2SourceSetMagic)) {
		return AppACLSourceSetR2V1{}, fmt.Errorf("invalid APP ACL R2 source-set magic or size")
	}
	decoder := appACLR2Decoder{body: body, offset: len(appACLR2SourceSetMagic)}
	version, err := decoder.uint16("source-set version")
	if err != nil {
		return AppACLSourceSetR2V1{}, err
	}
	if version != appACLR2SourceSetVersion {
		return AppACLSourceSetR2V1{}, fmt.Errorf("APP ACL R2 source-set version is %d, want %d", version, appACLR2SourceSetVersion)
	}
	count, err := decoder.uint16("source entry count")
	if err != nil {
		return AppACLSourceSetR2V1{}, err
	}
	if count != appACLR2SourceSetEntryCount {
		return AppACLSourceSetR2V1{}, fmt.Errorf("APP ACL R2 source-set count is %d, want %d", count, appACLR2SourceSetEntryCount)
	}
	expected, err := appACLR2ExpectedSourceEntries()
	if err != nil {
		return AppACLSourceSetR2V1{}, err
	}
	entries := make([]AppACLSourceEntryR2V1, 0, count)
	for index := range int(count) {
		filename, err := decoder.string16(1, 255, "source filename")
		if err != nil {
			return AppACLSourceSetR2V1{}, err
		}
		if !validAppACLR2MigrationFilename(filename) {
			return AppACLSourceSetR2V1{}, fmt.Errorf("APP ACL R2 source entry %d has invalid filename", index)
		}
		digest, err := decoder.digest("source digest")
		if err != nil {
			return AppACLSourceSetR2V1{}, err
		}
		entry := AppACLSourceEntryR2V1{Filename: filename, Digest: digest}
		if index > 0 && entries[index-1].Filename >= filename {
			return AppACLSourceSetR2V1{}, fmt.Errorf("APP ACL R2 source entries are not strictly sorted")
		}
		if entry != expected[index] {
			return AppACLSourceSetR2V1{}, fmt.Errorf("APP ACL R2 source entry %d does not match the fixed snapshot", index)
		}
		entries = append(entries, entry)
	}
	if err := decoder.requireEOF("source set"); err != nil {
		return AppACLSourceSetR2V1{}, err
	}
	reencoded, err := CanonicalAppACLSourceSetR2BodyV1(entries)
	if err != nil {
		return AppACLSourceSetR2V1{}, err
	}
	if !bytes.Equal(reencoded, body) {
		return AppACLSourceSetR2V1{}, fmt.Errorf("APP ACL R2 source set is not byte-canonical")
	}
	return AppACLSourceSetR2V1{Entries: entries}, nil
}

// AppACLSourceSetR2DigestV1 validates then hashes the complete source body.
func AppACLSourceSetR2DigestV1(body []byte) ([32]byte, error) {
	if _, err := ParseCanonicalAppACLSourceSetR2BodyV1(body); err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(body), nil
}

func appACLR2ExpectedSourceEntries() ([]AppACLSourceEntryR2V1, error) {
	fields := strings.Fields(appACLR2SourceContract)
	if len(fields) != appACLR2SourceSetEntryCount*2 {
		return nil, fmt.Errorf("APP ACL R2 source contract has %d fields, want %d", len(fields), appACLR2SourceSetEntryCount*2)
	}
	entries := make([]AppACLSourceEntryR2V1, 0, appACLR2SourceSetEntryCount)
	for index := 0; index < len(fields); index += 2 {
		digest, err := appACLR2DigestFromHex(fields[index])
		if err != nil {
			return nil, fmt.Errorf("decode APP ACL R2 source digest %d: %w", index/2, err)
		}
		entries = append(entries, AppACLSourceEntryR2V1{Filename: fields[index+1], Digest: digest})
	}
	return entries, nil
}

func verifyAppACLR2EmbeddedSource(fsys fs.FS, expected []AppACLSourceEntryR2V1, label string) error {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return fmt.Errorf("read %s APP ACL R2 source filesystem: %w", label, err)
	}
	actualNames := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		actualNames = append(actualNames, entry.Name())
	}
	if len(actualNames) != len(expected) {
		return fmt.Errorf("%s APP ACL R2 source inventory has %d SQL files, want %d", label, len(actualNames), len(expected))
	}
	for index, expectedEntry := range expected {
		if actualNames[index] != expectedEntry.Filename {
			return fmt.Errorf("%s APP ACL R2 source file %d is %q, want %q", label, index, actualNames[index], expectedEntry.Filename)
		}
		payload, err := fs.ReadFile(fsys, expectedEntry.Filename)
		if err != nil {
			return fmt.Errorf("read %s APP ACL R2 source %q: %w", label, expectedEntry.Filename, err)
		}
		if sha256.Sum256(payload) != expectedEntry.Digest {
			return fmt.Errorf("%s APP ACL R2 source %q has different bytes", label, expectedEntry.Filename)
		}
	}
	return nil
}

func appACLR2DigestFromHex(value string) ([32]byte, error) {
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != sha256.Size {
		return [32]byte{}, fmt.Errorf("invalid SHA-256 hex")
	}
	var digest [32]byte
	copy(digest[:], decoded)
	return digest, nil
}

func validAppACLR2MigrationFilename(filename string) bool {
	if len(filename) < len("0000_a.sql") || len(filename) > 255 || !utf8.ValidString(filename) {
		return false
	}
	for index := 0; index < 4; index++ {
		if filename[index] < '0' || filename[index] > '9' {
			return false
		}
	}
	if filename[4] != '_' || !strings.HasSuffix(filename, ".sql") {
		return false
	}
	stem := filename[5 : len(filename)-4]
	if stem == "" {
		return false
	}
	for _, char := range []byte(stem) {
		if char != '_' && (char < 'a' || char > 'z') && (char < '0' || char > '9') {
			return false
		}
	}
	return true
}

func validAppACLR2Text(value string, minimum, maximum int) bool {
	if len(value) < minimum || len(value) > maximum || !utf8.ValidString(value) || !norm.NFC.IsNormalString(value) {
		return false
	}
	for _, char := range value {
		if char <= 0x1f || char >= 0x7f && char <= 0x9f {
			return false
		}
	}
	return true
}

func validAppACLR2RoleName(value string) bool {
	if !validAppACLR2Text(value, 1, 63) {
		return false
	}
	for index, char := range []byte(value) {
		if index == 0 {
			if char != '_' && (char < 'a' || char > 'z') {
				return false
			}
			continue
		}
		if char != '_' && (char < 'a' || char > 'z') && (char < '0' || char > '9') {
			return false
		}
	}
	return true
}

func appendAppACLR2Uint16(body []byte, value uint16) []byte {
	var encoded [2]byte
	binary.BigEndian.PutUint16(encoded[:], value)
	return append(body, encoded[:]...)
}

func appendAppACLR2Uint32(body []byte, value uint32) []byte {
	var encoded [4]byte
	binary.BigEndian.PutUint32(encoded[:], value)
	return append(body, encoded[:]...)
}

func appendAppACLR2Uint64(body []byte, value uint64) []byte {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	return append(body, encoded[:]...)
}

func appendAppACLR2String16(body []byte, value string) []byte {
	body = appendAppACLR2Uint16(body, uint16(len(value)))
	return append(body, value...)
}

func appendAppACLR2Body32(body, value []byte) []byte {
	body = appendAppACLR2Uint32(body, uint32(len(value)))
	return append(body, value...)
}

type appACLR2Decoder struct {
	body   []byte
	offset int
}

func (decoder *appACLR2Decoder) uint8(field string) (byte, error) {
	if len(decoder.body)-decoder.offset < 1 {
		return 0, fmt.Errorf("truncated APP ACL R2 %s", field)
	}
	value := decoder.body[decoder.offset]
	decoder.offset++
	return value, nil
}

func (decoder *appACLR2Decoder) uint16(field string) (uint16, error) {
	if len(decoder.body)-decoder.offset < 2 {
		return 0, fmt.Errorf("truncated APP ACL R2 %s", field)
	}
	value := binary.BigEndian.Uint16(decoder.body[decoder.offset : decoder.offset+2])
	decoder.offset += 2
	return value, nil
}

func (decoder *appACLR2Decoder) uint32(field string) (uint32, error) {
	if len(decoder.body)-decoder.offset < 4 {
		return 0, fmt.Errorf("truncated APP ACL R2 %s", field)
	}
	value := binary.BigEndian.Uint32(decoder.body[decoder.offset : decoder.offset+4])
	decoder.offset += 4
	return value, nil
}

func (decoder *appACLR2Decoder) uint64(field string) (uint64, error) {
	if len(decoder.body)-decoder.offset < 8 {
		return 0, fmt.Errorf("truncated APP ACL R2 %s", field)
	}
	value := binary.BigEndian.Uint64(decoder.body[decoder.offset : decoder.offset+8])
	decoder.offset += 8
	return value, nil
}

func (decoder *appACLR2Decoder) string16(minimum, maximum int, field string) (string, error) {
	length, err := decoder.uint16(field + " length")
	if err != nil {
		return "", err
	}
	if int(length) < minimum || int(length) > maximum || len(decoder.body)-decoder.offset < int(length) {
		return "", fmt.Errorf("invalid APP ACL R2 %s length", field)
	}
	value := string(decoder.body[decoder.offset : decoder.offset+int(length)])
	decoder.offset += int(length)
	if !validAppACLR2Text(value, minimum, maximum) {
		return "", fmt.Errorf("invalid APP ACL R2 %s text", field)
	}
	return value, nil
}

func (decoder *appACLR2Decoder) body32(minimum, maximum int, field string) ([]byte, error) {
	length, err := decoder.uint32(field + " length")
	if err != nil {
		return nil, err
	}
	if uint64(length) < uint64(minimum) || uint64(length) > uint64(maximum) || uint64(length) > uint64(len(decoder.body)-decoder.offset) {
		return nil, fmt.Errorf("invalid APP ACL R2 %s length", field)
	}
	value := append([]byte(nil), decoder.body[decoder.offset:decoder.offset+int(length)]...)
	decoder.offset += int(length)
	return value, nil
}

func (decoder *appACLR2Decoder) digest(field string) ([32]byte, error) {
	if len(decoder.body)-decoder.offset < sha256.Size {
		return [32]byte{}, fmt.Errorf("truncated APP ACL R2 %s", field)
	}
	var value [32]byte
	copy(value[:], decoder.body[decoder.offset:decoder.offset+sha256.Size])
	decoder.offset += sha256.Size
	return value, nil
}

func (decoder *appACLR2Decoder) requireEOF(field string) error {
	if decoder.offset != len(decoder.body) {
		return fmt.Errorf("APP ACL R2 %s has trailing bytes", field)
	}
	return nil
}
