package migrate

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"strings"
	"testing"
)

const appACLR2SourceMagicVector = "HOUFENG-APP-ACL-R2-SOURCE-SET-V1"

const appACLR2SourceDigestVector = "6a2a82332c9646375434689255528565c612bd86e195aa854357b3f386e242a1"

const appACLR2SourceEntryVectors = `
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
7e15c579cd2055d61d1768c35556032f3ec4c17950c2a15ef7e5e22f4350fc01 0052_app_acl_r2_privileged_transition.sql
`

type appACLR2SourceVectorEntry struct {
	filename string
	digest   [32]byte
}

func TestAppACLR2SourceCanonicalVectorRoundTrips(t *testing.T) {
	wantEntries := appACLR2SourceVectorEntries(t)
	wantBody := rawAppACLR2SourceBody(wantEntries, uint16(len(wantEntries)))
	wantDigest := digestFromHex(t, appACLR2SourceDigestVector)
	if got := sha256.Sum256(wantBody); got != wantDigest {
		t.Fatalf("independent source vector digest = %x, want literal %x", got, wantDigest)
	}

	body, err := CompileAppACLSourceSetR2V1()
	if err != nil {
		t.Fatalf("CompileAppACLSourceSetR2V1() error = %v", err)
	}
	if !bytes.Equal(body, wantBody) {
		t.Fatalf("CompileAppACLSourceSetR2V1() bytes differ from independent vector\n got: %x\nwant: %x", body, wantBody)
	}

	validHex := hex.EncodeToString(wantBody)
	decodedHex, err := hex.DecodeString(validHex)
	if err != nil {
		t.Fatalf("DecodeString(valid source vector) error = %v", err)
	}
	parsed, err := ParseCanonicalAppACLSourceSetR2BodyV1(decodedHex)
	if err != nil {
		t.Fatalf("ParseCanonicalAppACLSourceSetR2BodyV1() error = %v", err)
	}
	if len(parsed.Entries) != 53 {
		t.Fatalf("parsed source entry count = %d, want 53", len(parsed.Entries))
	}
	for index, want := range wantEntries {
		got := parsed.Entries[index]
		if got.Filename != want.filename || got.Digest != want.digest {
			t.Fatalf("parsed source entry %d = %#v, want %q/%x", index, got, want.filename, want.digest)
		}
	}
	if parsed.Entries[51].Filename != "0051_create_record_platform_foundation.sql" || parsed.Entries[52].Filename != "0052_app_acl_r2_privileged_transition.sql" {
		t.Fatalf("source tail = %q, %q, want frozen 0051 then isolated 0052", parsed.Entries[51].Filename, parsed.Entries[52].Filename)
	}
	reencoded, err := CanonicalAppACLSourceSetR2BodyV1(parsed.Entries)
	if err != nil {
		t.Fatalf("CanonicalAppACLSourceSetR2BodyV1(parsed) error = %v", err)
	}
	if !bytes.Equal(reencoded, decodedHex) {
		t.Fatalf("source re-encoding = %x, want literal vector %x", reencoded, decodedHex)
	}
	gotDigest, err := AppACLSourceSetR2DigestV1(decodedHex)
	if err != nil {
		t.Fatalf("AppACLSourceSetR2DigestV1() error = %v", err)
	}
	if gotDigest != wantDigest {
		t.Fatalf("AppACLSourceSetR2DigestV1() = %x, want %x", gotDigest, wantDigest)
	}
}

func TestAppACLR2SourceParserRejectsAnythingOutsideFixedSnapshot(t *testing.T) {
	entries := appACLR2SourceVectorEntries(t)
	valid := rawAppACLR2SourceBody(entries, 53)

	badMagic := append([]byte("HOUFENG-APP-MIGRATION-SET-V1"), valid[len(appACLR2SourceMagicVector):]...)
	truncatedLength := append([]byte(appACLR2SourceMagicVector), 0, 1, 0, 53, 0)
	substitutedChecksumEntries := append([]appACLR2SourceVectorEntry(nil), entries...)
	substitutedChecksumEntries[52].digest[0] ^= 0xff
	duplicateEntries := append([]appACLR2SourceVectorEntry(nil), entries...)
	duplicateEntries[1] = duplicateEntries[0]
	reorderedEntries := append([]appACLR2SourceVectorEntry(nil), entries...)
	reorderedEntries[0], reorderedEntries[1] = reorderedEntries[1], reorderedEntries[0]
	substitutedFilenameEntries := append([]appACLR2SourceVectorEntry(nil), entries...)
	substitutedFilenameEntries[52].filename = "0052_app_acl_r2_privileged_transitions.sql"

	tests := []struct {
		name string
		body []byte
	}{
		{name: "r1 magic", body: badMagic},
		{name: "bad version", body: replaceAppACLR2SourceUint16(valid, len(appACLR2SourceMagicVector), 2)},
		{name: "truncated length", body: truncatedLength},
		{name: "substituted checksum", body: rawAppACLR2SourceBody(substitutedChecksumEntries, 53)},
		{name: "duplicate", body: rawAppACLR2SourceBody(duplicateEntries, 53)},
		{name: "reorder", body: rawAppACLR2SourceBody(reorderedEntries, 53)},
		{name: "wrong count 52", body: rawAppACLR2SourceBody(entries, 52)},
		{name: "wrong count 54", body: rawAppACLR2SourceBody(entries, 54)},
		{name: "substituted filename", body: rawAppACLR2SourceBody(substitutedFilenameEntries, 53)},
		{name: "trailing byte", body: append(append([]byte(nil), valid...), 0)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseCanonicalAppACLSourceSetR2BodyV1(tt.body); err == nil {
				t.Fatal("ParseCanonicalAppACLSourceSetR2BodyV1() error = nil, want fixed-snapshot rejection")
			}
		})
	}
}

func TestAppACLR2SourceEncoderRejectsNonSnapshotInputsBeforeEmission(t *testing.T) {
	entries := appACLR2SourceVectorEntries(t)
	tests := []struct {
		name    string
		entries []AppACLSourceEntryR2V1
	}{
		{name: "52 entries", entries: productionSourceEntries(entries[:52])},
		{name: "duplicate", entries: func() []AppACLSourceEntryR2V1 {
			got := productionSourceEntries(entries)
			got[1] = got[0]
			return got
		}()},
		{name: "wrong checksum", entries: func() []AppACLSourceEntryR2V1 {
			got := productionSourceEntries(entries)
			got[52].Digest[0] ^= 0xff
			return got
		}()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if body, err := CanonicalAppACLSourceSetR2BodyV1(tt.entries); err == nil || body != nil {
				t.Fatalf("CanonicalAppACLSourceSetR2BodyV1() = %x, %v; want nil and rejection", body, err)
			}
		})
	}
}

func appACLR2SourceVectorEntries(t *testing.T) []appACLR2SourceVectorEntry {
	t.Helper()
	fields := strings.Fields(appACLR2SourceEntryVectors)
	if len(fields) != 53*2 {
		t.Fatalf("source vector field count = %d, want %d", len(fields), 53*2)
	}
	entries := make([]appACLR2SourceVectorEntry, 0, 53)
	for index := 0; index < len(fields); index += 2 {
		entries = append(entries, appACLR2SourceVectorEntry{
			filename: fields[index+1],
			digest:   digestFromHex(t, fields[index]),
		})
	}
	return entries
}

func productionSourceEntries(entries []appACLR2SourceVectorEntry) []AppACLSourceEntryR2V1 {
	got := make([]AppACLSourceEntryR2V1, 0, len(entries))
	for _, entry := range entries {
		got = append(got, AppACLSourceEntryR2V1{Filename: entry.filename, Digest: entry.digest})
	}
	return got
}

func rawAppACLR2SourceBody(entries []appACLR2SourceVectorEntry, count uint16) []byte {
	body := append([]byte(nil), appACLR2SourceMagicVector...)
	body = appendTestUint16(body, 1)
	body = appendTestUint16(body, count)
	for _, entry := range entries {
		body = appendTestString16(body, entry.filename)
		body = append(body, entry.digest[:]...)
	}
	return body
}

func replaceAppACLR2SourceUint16(body []byte, offset int, value uint16) []byte {
	got := append([]byte(nil), body...)
	binary.BigEndian.PutUint16(got[offset:offset+2], value)
	return got
}

func digestFromHex(t *testing.T, value string) [32]byte {
	t.Helper()
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != sha256.Size {
		t.Fatalf("decode digest %q = %x, %v", value, decoded, err)
	}
	var digest [32]byte
	copy(digest[:], decoded)
	return digest
}

func appendTestUint16(body []byte, value uint16) []byte {
	var encoded [2]byte
	binary.BigEndian.PutUint16(encoded[:], value)
	return append(body, encoded[:]...)
}

func appendTestUint32(body []byte, value uint32) []byte {
	var encoded [4]byte
	binary.BigEndian.PutUint32(encoded[:], value)
	return append(body, encoded[:]...)
}

func appendTestUint64(body []byte, value uint64) []byte {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	return append(body, encoded[:]...)
}

func appendTestString16(body []byte, value string) []byte {
	body = appendTestUint16(body, uint16(len(value)))
	return append(body, value...)
}

func appendTestBody32(body, value []byte) []byte {
	body = appendTestUint32(body, uint32(len(value)))
	return append(body, value...)
}
