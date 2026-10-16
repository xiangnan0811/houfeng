package migrate

import (
	"bytes"
	_ "embed"
	"fmt"
)

const (
	appACLCurrentTransitionDatabase = "houfeng"
	appACLCurrentTransitionMigrator = "houfeng_migrator"
)

var appACLCurrentTransitionBindings = []AppACLRoleBinding{
	{Subject: AppACLSubjectCenterRuntime, CatalogRole: "houfeng_runtime"},
	{Subject: AppACLSubjectPlatformAdmin, CatalogRole: "houfeng_platform_admin"},
}

//go:embed testdata/app_acl_current_v0.79.4_migrations.v1.bin
var appACLCurrentV0794MigrationGolden []byte

//go:embed testdata/app_acl_current_v0.79.4_privileges.v1.bin
var appACLCurrentV0794PrivilegeGolden []byte

//go:embed testdata/app_acl_current_v0.79.4_manifest_digest.v1.bin
var appACLCurrentV0794ManifestDigestGoldenBytes []byte

//go:embed testdata/app_acl_current_v0.80.2_migrations.v1.bin
var appACLCurrentV0802MigrationGolden []byte

//go:embed testdata/app_acl_current_v0.80.2_privileges.v1.bin
var appACLCurrentV0802PrivilegeGolden []byte

// Exported from the v0.79.6 release compiler, independently of current sources.
//
//go:embed testdata/app_acl_current_v0.79.6_migrations.v1.bin
var appACLCurrentV0796MigrationGolden []byte

//go:embed testdata/app_acl_current_v0.79.6_privileges.v1.bin
var appACLCurrentV0796PrivilegeGolden []byte

// Exported from the v0.80.4 release compiler, independently of current sources.
// v0.80.3 and v0.80.4 embed the same migration and privilege contract.
//
//go:embed testdata/app_acl_current_v0.80.4_migrations.v1.bin
var appACLCurrentV0804MigrationGolden []byte

//go:embed testdata/app_acl_current_v0.80.4_privileges.v1.bin
var appACLCurrentV0804PrivilegeGolden []byte

// Exported from the independent v1.15.7 C68 compiler built from baseline
// commit 8473dc306c47a2f5ad939508ab697b6528889fba (69 migration sources,
// 17 current APP ACL fragments), not from the current C69 source tree.
//
//go:embed testdata/app_acl_current_v1.15.7_migrations.v1.bin
var appACLCurrentV1157MigrationGolden []byte

//go:embed testdata/app_acl_current_v1.15.7_privileges.v1.bin
var appACLCurrentV1157PrivilegeGolden []byte

// The C70 predecessor is frozen from the pre-C71 candidate before the current
// compiler was extended with access-management sources. These bytes are
// embedded separately so P70 never derives its predecessor oracle by slicing
// the C71 compiler output at runtime.
//
//go:embed testdata/app_acl_current_c70_migrations.v1.bin
var appACLCurrentC70MigrationGolden []byte

//go:embed testdata/app_acl_current_c70_privileges.v1.bin
var appACLCurrentC70PrivilegeGolden []byte

// The C71 predecessor is captured independently from a pristine C71
// PostgreSQL deployment. It is never derived from the C72 registry at runtime.
//
//go:embed testdata/app_acl_current_c71_migrations.v1.bin
var appACLCurrentC71MigrationGolden []byte

//go:embed testdata/app_acl_current_c71_privileges.v1.bin
var appACLCurrentC71PrivilegeGolden []byte

// The C72 predecessor is the independent C71 golden plus the immutable 0072
// checksum record; 0072 adds no APP ACL privileges, so the independent C71
// privilege golden is copied unchanged. These bytes are intentionally
// independent of the current C73 source compiler.
var appACLCurrentC72MigrationGolden = func() []byte {
	body := append([]byte(nil), appACLCurrentC71MigrationGolden...)
	body = append(body, 0, 0, 0, 41)
	body = append(body, []byte("0072_add_target_observation_freshness.sql")...)
	body = append(body, []byte{
		0x4a, 0xd9, 0x6e, 0xa2, 0x09, 0x0b, 0x18, 0xed,
		0x6c, 0x55, 0x4e, 0x53, 0xca, 0x88, 0x11, 0xcd,
		0x07, 0x1a, 0x42, 0x0c, 0x88, 0x71, 0xf1, 0x25,
		0x6b, 0x9a, 0x5f, 0x2a, 0xd0, 0x9b, 0x39, 0xf4,
	}...)
	return body
}()

var appACLCurrentC72PrivilegeGolden = append([]byte(nil), appACLCurrentC71PrivilegeGolden...)

// The C73 predecessor is the C72 golden plus the immutable 0073 checksum
// record; 0073 adds no APP ACL privileges, so the C72 privilege golden is
// copied unchanged. These bytes are intentionally independent of the current
// C74 source compiler.
var appACLCurrentC73MigrationGolden = func() []byte {
	body := append([]byte(nil), appACLCurrentC72MigrationGolden...)
	body = append(body, 0, 0, 0, 49)
	body = append(body, []byte("0073_add_monitoring_instance_metadata_version.sql")...)
	body = append(body, []byte{
		0x36, 0xda, 0x69, 0x54, 0xd7, 0x4a, 0x66, 0x24,
		0x9f, 0x3b, 0xf8, 0x4d, 0xa3, 0x05, 0x2c, 0x7c,
		0x18, 0xfa, 0xb0, 0xab, 0x87, 0x4a, 0xbf, 0xec,
		0x65, 0x68, 0x47, 0x3d, 0x6c, 0x21, 0xfc, 0x7f,
	}...)
	return body
}()

var appACLCurrentC73PrivilegeGolden = append([]byte(nil), appACLCurrentC72PrivilegeGolden...)

// The C74 predecessor is the independently frozen C73 golden plus the
// immutable 0074 checksum record; 0074 adds no APP ACL privileges, so the
// C73 privilege golden is copied unchanged. These bytes are independent of
// the current C75 source compiler.
var appACLCurrentC74MigrationGolden = func() []byte {
	body := append([]byte(nil), appACLCurrentC73MigrationGolden...)
	body = append(body, 0, 0, 0, 49)
	body = append(body, []byte("0074_add_subscription_exchange_rate_month_end.sql")...)
	body = append(body, []byte{
		0x3d, 0x79, 0x73, 0x64, 0x6b, 0xdd, 0xa6, 0x11,
		0x30, 0x93, 0x01, 0xa4, 0xf9, 0x9e, 0x19, 0xfa,
		0x48, 0xda, 0x10, 0xf3, 0x57, 0x49, 0xc5, 0x09,
		0x88, 0xdb, 0x6a, 0xbe, 0x08, 0xbf, 0x0d, 0x13,
	}...)
	return body
}()

var appACLCurrentC74PrivilegeGolden = append([]byte(nil), appACLCurrentC73PrivilegeGolden...)

// The C75 predecessor is the frozen C74 literal plus the published 0075
// checksum record. Its privileges are unchanged and independent of C76.
var appACLCurrentC75MigrationGolden = func() []byte {
	body := append([]byte(nil), appACLCurrentC74MigrationGolden...)
	body = append(body, 0, 0, 0, 42)
	body = append(body, []byte("0075_add_settings_deployment_overrides.sql")...)
	body = append(body, []byte{
		0xa0, 0x0b, 0xd6, 0x6a, 0x36, 0x87, 0xe3, 0x27,
		0x8c, 0x0e, 0x94, 0x44, 0x53, 0x8e, 0x6b, 0xa5,
		0x72, 0xf2, 0x45, 0x2f, 0x57, 0x98, 0x2d, 0xd9,
		0xb0, 0x82, 0xee, 0xcc, 0x1e, 0x90, 0x91, 0x1a,
	}...)
	return body
}()

var appACLCurrentC75PrivilegeGolden = append([]byte(nil), appACLCurrentC74PrivilegeGolden...)

// Frozen from the C76 baseline compiler before registering the C77 fragment.
// Neither predecessor body is derived from the current contract at runtime.
//
//go:embed testdata/app_acl_current_c76_migrations.v1.bin
var appACLCurrentC76MigrationGolden []byte

//go:embed testdata/app_acl_current_c76_privileges.v1.bin
var appACLCurrentC76PrivilegeGolden []byte

// Captured by the independent C77 compiler before adding the C78 fragment.
//
//go:embed testdata/app_acl_current_c77_migrations.v1.bin
var appACLCurrentC77MigrationGolden []byte

//go:embed testdata/app_acl_current_c77_privileges.v1.bin
var appACLCurrentC77PrivilegeGolden []byte

// Frozen by the unchanged C78 compiler before registering the C79 index.
// Neither predecessor body is derived from the successor compiler.
//
//go:embed testdata/app_acl_current_c78_migrations.v1.bin
var appACLCurrentC78MigrationGolden []byte

//go:embed testdata/app_acl_current_c78_privileges.v1.bin
var appACLCurrentC78PrivilegeGolden []byte

// The C69 predecessor is the preserved terminal from the baseline source
// tree. Its source golden is the independent C68 golden plus the immutable
// 0069 checksum record; 0069 adds no APP ACL privileges, so the independent
// C68 privilege golden is copied unchanged. These bytes are intentionally
// independent of the current C70 source compiler.
var appACLCurrentC69MigrationGolden = func() []byte {
	body := append([]byte(nil), appACLCurrentV1157MigrationGolden...)
	body = append(body, 0, 0, 0, 28)
	body = append(body, []byte("0069_add_cpu_rates_valid.sql")...)
	body = append(body, []byte{
		0x9e, 0x71, 0x15, 0x22, 0x65, 0xd2, 0x26, 0xf5,
		0x5b, 0x31, 0x37, 0x95, 0x6a, 0xc7, 0x98, 0xc2,
		0x21, 0x10, 0x03, 0x3b, 0xd4, 0x61, 0x92, 0x2a,
		0x4e, 0x23, 0xa3, 0xb8, 0x5f, 0xb6, 0xae, 0x6b,
	}...)
	return body
}()

var appACLCurrentC69PrivilegeGolden = append([]byte(nil), appACLCurrentV1157PrivilegeGolden...)

// Exported from the v1.15.3 baseline compiler, independently of current sources.

//go:embed testdata/app_acl_current_v1.15.3_migrations.v1.bin
var appACLCurrentV1153MigrationGolden []byte

//go:embed testdata/app_acl_current_v1.15.3_privileges.v1.bin
var appACLCurrentV1153PrivilegeGolden []byte

var appACLCurrentV0794ManifestDigestGolden = func() [32]byte {
	var digest [32]byte
	copy(digest[:], appACLCurrentV0794ManifestDigestGoldenBytes)
	return digest
}()

type appACLCurrentProfileID uint8

const (
	appACLCurrentProfileP62 appACLCurrentProfileID = iota + 1
	appACLCurrentProfileP64
	appACLCurrentProfileP63
	appACLCurrentProfileP66
	appACLCurrentProfileP67
	appACLCurrentProfileP68
	appACLCurrentProfileP69
	appACLCurrentProfileP70
	appACLCurrentProfileP71
	appACLCurrentProfileP72
	appACLCurrentProfileP73
	appACLCurrentProfileP74
	appACLCurrentProfileP75
	appACLCurrentProfileP76
	appACLCurrentProfileP77
	appACLCurrentProfileP78
)

type appACLCurrentTransitionDefinition struct {
	profile                         appACLCurrentProfileID
	predecessorLastMigration        string
	successorMigrations             []string
	predecessorMigrationGolden      []byte
	predecessorPrivilegeGolden      []byte
	predecessorManifestDigestGolden []byte
}

type appACLCurrentTransition struct {
	profile                   appACLCurrentProfileID
	predecessor               appACLCurrentSourceContract
	successor                 migrationSourceSnapshot
	predecessorPrivilegeBody  []byte
	predecessorManifestDigest [32]byte
}

var appACLCurrentTransitionDefinitions = []appACLCurrentTransitionDefinition{
	{
		profile:                  appACLCurrentProfileP62,
		predecessorLastMigration: "0062_create_vps_create_idempotency.sql",
		successorMigrations: []string{
			"0063_tune_heartbeat_incident_policy.sql",
			"0064_add_network_rates_valid.sql",
			"0065_extend_vps_lifecycle_audit_and_snapshot.sql",
			"0066_constrain_monitoring_and_target_state_values.sql",
			"0067_refactor_vps_monitoring_lifecycle.sql",
			"0068_normalize_ip_quality_host_address_identity.sql",
			"0069_add_cpu_rates_valid.sql",
			"0070_add_record_import_destination_subject.sql",
			"0071_add_access_management.sql",
			"0072_add_target_observation_freshness.sql",
			"0073_add_monitoring_instance_metadata_version.sql",
			"0074_add_subscription_exchange_rate_month_end.sql",
			"0075_add_settings_deployment_overrides.sql",
			"0076_ip_quality_address_families.sql",
			"0077_ip_quality_product.sql",
			"0078_ip_quality_collection_scope.sql",
			"0079_ip_quality_cache_expiry.sql",
		},
		predecessorMigrationGolden:      appACLCurrentV0794MigrationGolden,
		predecessorPrivilegeGolden:      appACLCurrentV0794PrivilegeGolden,
		predecessorManifestDigestGolden: appACLCurrentV0794ManifestDigestGoldenBytes,
	},
	{
		profile:                  appACLCurrentProfileP64,
		predecessorLastMigration: "0064_add_network_rates_valid.sql",
		successorMigrations: []string{
			"0065_extend_vps_lifecycle_audit_and_snapshot.sql",
			"0066_constrain_monitoring_and_target_state_values.sql",
			"0067_refactor_vps_monitoring_lifecycle.sql",
			"0068_normalize_ip_quality_host_address_identity.sql",
			"0069_add_cpu_rates_valid.sql",
			"0070_add_record_import_destination_subject.sql",
			"0071_add_access_management.sql",
			"0072_add_target_observation_freshness.sql",
			"0073_add_monitoring_instance_metadata_version.sql",
			"0074_add_subscription_exchange_rate_month_end.sql",
			"0075_add_settings_deployment_overrides.sql",
			"0076_ip_quality_address_families.sql",
			"0077_ip_quality_product.sql",
			"0078_ip_quality_collection_scope.sql",
			"0079_ip_quality_cache_expiry.sql",
		},
		predecessorMigrationGolden: appACLCurrentV0802MigrationGolden,
		predecessorPrivilegeGolden: appACLCurrentV0802PrivilegeGolden,
	},
	{
		profile:                  appACLCurrentProfileP63,
		predecessorLastMigration: "0063_tune_heartbeat_incident_policy.sql",
		successorMigrations: []string{
			"0064_add_network_rates_valid.sql",
			"0065_extend_vps_lifecycle_audit_and_snapshot.sql",
			"0066_constrain_monitoring_and_target_state_values.sql",
			"0067_refactor_vps_monitoring_lifecycle.sql",
			"0068_normalize_ip_quality_host_address_identity.sql",
			"0069_add_cpu_rates_valid.sql",
			"0070_add_record_import_destination_subject.sql",
			"0071_add_access_management.sql",
			"0072_add_target_observation_freshness.sql",
			"0073_add_monitoring_instance_metadata_version.sql",
			"0074_add_subscription_exchange_rate_month_end.sql",
			"0075_add_settings_deployment_overrides.sql",
			"0076_ip_quality_address_families.sql",
			"0077_ip_quality_product.sql",
			"0078_ip_quality_collection_scope.sql",
			"0079_ip_quality_cache_expiry.sql",
		},
		predecessorMigrationGolden: appACLCurrentV0796MigrationGolden,
		predecessorPrivilegeGolden: appACLCurrentV0796PrivilegeGolden,
	},
	{
		profile:                  appACLCurrentProfileP66,
		predecessorLastMigration: "0066_constrain_monitoring_and_target_state_values.sql",
		successorMigrations: []string{
			"0067_refactor_vps_monitoring_lifecycle.sql",
			"0068_normalize_ip_quality_host_address_identity.sql",
			"0069_add_cpu_rates_valid.sql",
			"0070_add_record_import_destination_subject.sql",
			"0071_add_access_management.sql",
			"0072_add_target_observation_freshness.sql",
			"0073_add_monitoring_instance_metadata_version.sql",
			"0074_add_subscription_exchange_rate_month_end.sql",
			"0075_add_settings_deployment_overrides.sql",
			"0076_ip_quality_address_families.sql",
			"0077_ip_quality_product.sql",
			"0078_ip_quality_collection_scope.sql",
			"0079_ip_quality_cache_expiry.sql",
		},
		predecessorMigrationGolden: appACLCurrentV0804MigrationGolden,
		predecessorPrivilegeGolden: appACLCurrentV0804PrivilegeGolden,
	},
	{
		profile:                  appACLCurrentProfileP67,
		predecessorLastMigration: "0067_refactor_vps_monitoring_lifecycle.sql",
		successorMigrations: []string{
			"0068_normalize_ip_quality_host_address_identity.sql",
			"0069_add_cpu_rates_valid.sql",
			"0070_add_record_import_destination_subject.sql",
			"0071_add_access_management.sql",
			"0072_add_target_observation_freshness.sql",
			"0073_add_monitoring_instance_metadata_version.sql",
			"0074_add_subscription_exchange_rate_month_end.sql",
			"0075_add_settings_deployment_overrides.sql",
			"0076_ip_quality_address_families.sql",
			"0077_ip_quality_product.sql",
			"0078_ip_quality_collection_scope.sql",
			"0079_ip_quality_cache_expiry.sql",
		},
		predecessorMigrationGolden: appACLCurrentV1153MigrationGolden,
		predecessorPrivilegeGolden: appACLCurrentV1153PrivilegeGolden,
	},
	{
		profile:                  appACLCurrentProfileP68,
		predecessorLastMigration: "0068_normalize_ip_quality_host_address_identity.sql",
		successorMigrations: []string{
			"0069_add_cpu_rates_valid.sql",
			"0070_add_record_import_destination_subject.sql",
			"0071_add_access_management.sql",
			"0072_add_target_observation_freshness.sql",
			"0073_add_monitoring_instance_metadata_version.sql",
			"0074_add_subscription_exchange_rate_month_end.sql",
			"0075_add_settings_deployment_overrides.sql",
			"0076_ip_quality_address_families.sql",
			"0077_ip_quality_product.sql",
			"0078_ip_quality_collection_scope.sql",
			"0079_ip_quality_cache_expiry.sql",
		},
		predecessorMigrationGolden: appACLCurrentV1157MigrationGolden,
		predecessorPrivilegeGolden: appACLCurrentV1157PrivilegeGolden,
	},
	{
		profile:                  appACLCurrentProfileP69,
		predecessorLastMigration: "0069_add_cpu_rates_valid.sql",
		successorMigrations: []string{
			"0070_add_record_import_destination_subject.sql",
			"0071_add_access_management.sql",
			"0072_add_target_observation_freshness.sql",
			"0073_add_monitoring_instance_metadata_version.sql",
			"0074_add_subscription_exchange_rate_month_end.sql",
			"0075_add_settings_deployment_overrides.sql",
			"0076_ip_quality_address_families.sql",
			"0077_ip_quality_product.sql",
			"0078_ip_quality_collection_scope.sql",
			"0079_ip_quality_cache_expiry.sql",
		},
		predecessorMigrationGolden: appACLCurrentC69MigrationGolden,
		predecessorPrivilegeGolden: appACLCurrentC69PrivilegeGolden,
	},
	{
		profile:                  appACLCurrentProfileP70,
		predecessorLastMigration: "0070_add_record_import_destination_subject.sql",
		successorMigrations: []string{
			"0071_add_access_management.sql",
			"0072_add_target_observation_freshness.sql",
			"0073_add_monitoring_instance_metadata_version.sql",
			"0074_add_subscription_exchange_rate_month_end.sql",
			"0075_add_settings_deployment_overrides.sql",
			"0076_ip_quality_address_families.sql",
			"0077_ip_quality_product.sql",
			"0078_ip_quality_collection_scope.sql",
			"0079_ip_quality_cache_expiry.sql",
		},
		predecessorMigrationGolden: appACLCurrentC70MigrationGolden,
		predecessorPrivilegeGolden: appACLCurrentC70PrivilegeGolden,
	},
	{
		profile:                  appACLCurrentProfileP71,
		predecessorLastMigration: "0071_add_access_management.sql",
		successorMigrations: []string{
			"0072_add_target_observation_freshness.sql",
			"0073_add_monitoring_instance_metadata_version.sql",
			"0074_add_subscription_exchange_rate_month_end.sql",
			"0075_add_settings_deployment_overrides.sql",
			"0076_ip_quality_address_families.sql",
			"0077_ip_quality_product.sql",
			"0078_ip_quality_collection_scope.sql",
			"0079_ip_quality_cache_expiry.sql",
		},
		predecessorMigrationGolden: appACLCurrentC71MigrationGolden,
		predecessorPrivilegeGolden: appACLCurrentC71PrivilegeGolden,
	},
	{
		profile:                  appACLCurrentProfileP72,
		predecessorLastMigration: "0072_add_target_observation_freshness.sql",
		successorMigrations: []string{
			"0073_add_monitoring_instance_metadata_version.sql",
			"0074_add_subscription_exchange_rate_month_end.sql",
			"0075_add_settings_deployment_overrides.sql",
			"0076_ip_quality_address_families.sql",
			"0077_ip_quality_product.sql",
			"0078_ip_quality_collection_scope.sql",
			"0079_ip_quality_cache_expiry.sql",
		},
		predecessorMigrationGolden: appACLCurrentC72MigrationGolden,
		predecessorPrivilegeGolden: appACLCurrentC72PrivilegeGolden,
	},
	{
		profile:                  appACLCurrentProfileP73,
		predecessorLastMigration: "0073_add_monitoring_instance_metadata_version.sql",
		successorMigrations: []string{
			"0074_add_subscription_exchange_rate_month_end.sql",
			"0075_add_settings_deployment_overrides.sql",
			"0076_ip_quality_address_families.sql",
			"0077_ip_quality_product.sql",
			"0078_ip_quality_collection_scope.sql",
			"0079_ip_quality_cache_expiry.sql",
		},
		predecessorMigrationGolden: appACLCurrentC73MigrationGolden,
		predecessorPrivilegeGolden: appACLCurrentC73PrivilegeGolden,
	},
	{
		profile:                  appACLCurrentProfileP74,
		predecessorLastMigration: "0074_add_subscription_exchange_rate_month_end.sql",
		successorMigrations: []string{
			"0075_add_settings_deployment_overrides.sql",
			"0076_ip_quality_address_families.sql",
			"0077_ip_quality_product.sql",
			"0078_ip_quality_collection_scope.sql",
			"0079_ip_quality_cache_expiry.sql",
		},
		predecessorMigrationGolden: appACLCurrentC74MigrationGolden,
		predecessorPrivilegeGolden: appACLCurrentC74PrivilegeGolden,
	},
	{
		profile:                  appACLCurrentProfileP75,
		predecessorLastMigration: "0075_add_settings_deployment_overrides.sql",
		successorMigrations: []string{
			"0076_ip_quality_address_families.sql",
			"0077_ip_quality_product.sql",
			"0078_ip_quality_collection_scope.sql",
			"0079_ip_quality_cache_expiry.sql",
		},
		predecessorMigrationGolden: appACLCurrentC75MigrationGolden,
		predecessorPrivilegeGolden: appACLCurrentC75PrivilegeGolden,
	},
	{
		profile:                  appACLCurrentProfileP76,
		predecessorLastMigration: "0076_ip_quality_address_families.sql",
		successorMigrations: []string{
			"0077_ip_quality_product.sql",
			"0078_ip_quality_collection_scope.sql",
			"0079_ip_quality_cache_expiry.sql",
		},
		predecessorMigrationGolden: appACLCurrentC76MigrationGolden,
		predecessorPrivilegeGolden: appACLCurrentC76PrivilegeGolden,
	},
	{
		profile:                  appACLCurrentProfileP77,
		predecessorLastMigration: "0077_ip_quality_product.sql",
		successorMigrations: []string{
			"0078_ip_quality_collection_scope.sql",
			"0079_ip_quality_cache_expiry.sql",
		},
		predecessorMigrationGolden: appACLCurrentC77MigrationGolden,
		predecessorPrivilegeGolden: appACLCurrentC77PrivilegeGolden,
	},
	{
		profile:                  appACLCurrentProfileP78,
		predecessorLastMigration: "0078_ip_quality_collection_scope.sql",
		successorMigrations: []string{
			"0079_ip_quality_cache_expiry.sql",
		},
		predecessorMigrationGolden: appACLCurrentC78MigrationGolden,
		predecessorPrivilegeGolden: appACLCurrentC78PrivilegeGolden,
	},
}

var appACLCurrentAcceptedPredecessorProfileChainsWithoutP69 = [][]appACLCurrentProfileID{
	{appACLCurrentProfileP62},
	{appACLCurrentProfileP64},
	{appACLCurrentProfileP62, appACLCurrentProfileP64},
	{appACLCurrentProfileP63},
	{appACLCurrentProfileP62, appACLCurrentProfileP63},
	{appACLCurrentProfileP66},
	{appACLCurrentProfileP62, appACLCurrentProfileP66},
	{appACLCurrentProfileP64, appACLCurrentProfileP66},
	{appACLCurrentProfileP62, appACLCurrentProfileP64, appACLCurrentProfileP66},
	{appACLCurrentProfileP63, appACLCurrentProfileP66},
	{appACLCurrentProfileP62, appACLCurrentProfileP63, appACLCurrentProfileP66},
	{appACLCurrentProfileP67},
	{appACLCurrentProfileP62, appACLCurrentProfileP67},
	{appACLCurrentProfileP64, appACLCurrentProfileP67},
	{appACLCurrentProfileP62, appACLCurrentProfileP64, appACLCurrentProfileP67},
	{appACLCurrentProfileP63, appACLCurrentProfileP67},
	{appACLCurrentProfileP62, appACLCurrentProfileP63, appACLCurrentProfileP67},
	{appACLCurrentProfileP66, appACLCurrentProfileP67},
	{appACLCurrentProfileP62, appACLCurrentProfileP66, appACLCurrentProfileP67},
	{appACLCurrentProfileP64, appACLCurrentProfileP66, appACLCurrentProfileP67},
	{appACLCurrentProfileP62, appACLCurrentProfileP64, appACLCurrentProfileP66, appACLCurrentProfileP67},
	{appACLCurrentProfileP63, appACLCurrentProfileP66, appACLCurrentProfileP67},
	{appACLCurrentProfileP62, appACLCurrentProfileP63, appACLCurrentProfileP66, appACLCurrentProfileP67},
	{appACLCurrentProfileP68},
	{appACLCurrentProfileP62, appACLCurrentProfileP68},
	{appACLCurrentProfileP64, appACLCurrentProfileP68},
	{appACLCurrentProfileP62, appACLCurrentProfileP64, appACLCurrentProfileP68},
	{appACLCurrentProfileP63, appACLCurrentProfileP68},
	{appACLCurrentProfileP62, appACLCurrentProfileP63, appACLCurrentProfileP68},
	{appACLCurrentProfileP66, appACLCurrentProfileP68},
	{appACLCurrentProfileP62, appACLCurrentProfileP66, appACLCurrentProfileP68},
	{appACLCurrentProfileP64, appACLCurrentProfileP66, appACLCurrentProfileP68},
	{appACLCurrentProfileP62, appACLCurrentProfileP64, appACLCurrentProfileP66, appACLCurrentProfileP68},
	{appACLCurrentProfileP63, appACLCurrentProfileP66, appACLCurrentProfileP68},
	{appACLCurrentProfileP62, appACLCurrentProfileP63, appACLCurrentProfileP66, appACLCurrentProfileP68},
	{appACLCurrentProfileP67, appACLCurrentProfileP68},
	{appACLCurrentProfileP62, appACLCurrentProfileP67, appACLCurrentProfileP68},
	{appACLCurrentProfileP64, appACLCurrentProfileP67, appACLCurrentProfileP68},
	{appACLCurrentProfileP62, appACLCurrentProfileP64, appACLCurrentProfileP67, appACLCurrentProfileP68},
	{appACLCurrentProfileP63, appACLCurrentProfileP67, appACLCurrentProfileP68},
	{appACLCurrentProfileP62, appACLCurrentProfileP63, appACLCurrentProfileP67, appACLCurrentProfileP68},
	{appACLCurrentProfileP66, appACLCurrentProfileP67, appACLCurrentProfileP68},
	{appACLCurrentProfileP62, appACLCurrentProfileP66, appACLCurrentProfileP67, appACLCurrentProfileP68},
	{appACLCurrentProfileP64, appACLCurrentProfileP66, appACLCurrentProfileP67, appACLCurrentProfileP68},
	{appACLCurrentProfileP62, appACLCurrentProfileP64, appACLCurrentProfileP66, appACLCurrentProfileP67, appACLCurrentProfileP68},
	{appACLCurrentProfileP63, appACLCurrentProfileP66, appACLCurrentProfileP67, appACLCurrentProfileP68},
	{appACLCurrentProfileP62, appACLCurrentProfileP63, appACLCurrentProfileP66, appACLCurrentProfileP67, appACLCurrentProfileP68},
}

var appACLCurrentAcceptedPredecessorProfileChains = func() [][]appACLCurrentProfileID {
	c70Chains := make([][]appACLCurrentProfileID, 0, len(appACLCurrentAcceptedPredecessorProfileChainsWithoutP69)*2+1)
	for _, chain := range appACLCurrentAcceptedPredecessorProfileChainsWithoutP69 {
		c70Chains = append(c70Chains, append([]appACLCurrentProfileID(nil), chain...))
		withC69 := append([]appACLCurrentProfileID(nil), chain...)
		withC69 = append(withC69, appACLCurrentProfileP69)
		c70Chains = append(c70Chains, withC69)
	}
	c70Chains = append(c70Chains, []appACLCurrentProfileID{appACLCurrentProfileP69})

	c71Chains := make([][]appACLCurrentProfileID, 0, len(c70Chains)*2+1)
	c71Chains = append(c71Chains, c70Chains...)
	c71Chains = append(c71Chains, []appACLCurrentProfileID{appACLCurrentProfileP70})
	for _, chain := range c70Chains {
		withC70 := append([]appACLCurrentProfileID(nil), chain...)
		withC70 = append(withC70, appACLCurrentProfileP70)
		c71Chains = append(c71Chains, withC70)
	}

	c72Chains := make([][]appACLCurrentProfileID, 0, len(c71Chains)*2+1)
	c72Chains = append(c72Chains, c71Chains...)
	c72Chains = append(c72Chains, []appACLCurrentProfileID{appACLCurrentProfileP71})
	for _, chain := range c71Chains {
		withC71 := append([]appACLCurrentProfileID(nil), chain...)
		withC71 = append(withC71, appACLCurrentProfileP71)
		c72Chains = append(c72Chains, withC71)
	}

	c73Chains := make([][]appACLCurrentProfileID, 0, len(c72Chains)*2+1)
	c73Chains = append(c73Chains, c72Chains...)
	c73Chains = append(c73Chains, []appACLCurrentProfileID{appACLCurrentProfileP72})
	for _, chain := range c72Chains {
		withC72 := append([]appACLCurrentProfileID(nil), chain...)
		withC72 = append(withC72, appACLCurrentProfileP72)
		c73Chains = append(c73Chains, withC72)
	}

	c74Chains := make([][]appACLCurrentProfileID, 0, len(c73Chains)*2+1)
	c74Chains = append(c74Chains, c73Chains...)
	c74Chains = append(c74Chains, []appACLCurrentProfileID{appACLCurrentProfileP73})
	for _, chain := range c73Chains {
		withP73 := append([]appACLCurrentProfileID(nil), chain...)
		withP73 = append(withP73, appACLCurrentProfileP73)
		c74Chains = append(c74Chains, withP73)
	}

	c75Chains := make([][]appACLCurrentProfileID, 0, len(c74Chains)*2+1)
	c75Chains = append(c75Chains, c74Chains...)
	c75Chains = append(c75Chains, []appACLCurrentProfileID{appACLCurrentProfileP74})
	for _, chain := range c74Chains {
		withP74 := append([]appACLCurrentProfileID(nil), chain...)
		withP74 = append(withP74, appACLCurrentProfileP74)
		c75Chains = append(c75Chains, withP74)
	}
	c76Chains := make([][]appACLCurrentProfileID, 0, len(c75Chains)*2+1)
	c76Chains = append(c76Chains, c75Chains...)
	c76Chains = append(c76Chains, []appACLCurrentProfileID{appACLCurrentProfileP75})
	for _, chain := range c75Chains {
		withP75 := append([]appACLCurrentProfileID(nil), chain...)
		withP75 = append(withP75, appACLCurrentProfileP75)
		c76Chains = append(c76Chains, withP75)
	}
	c77Chains := make([][]appACLCurrentProfileID, 0, len(c76Chains)*2+1)
	c77Chains = append(c77Chains, c76Chains...)
	c77Chains = append(c77Chains, []appACLCurrentProfileID{appACLCurrentProfileP76})
	for _, chain := range c76Chains {
		withP76 := append([]appACLCurrentProfileID(nil), chain...)
		withP76 = append(withP76, appACLCurrentProfileP76)
		c77Chains = append(c77Chains, withP76)
	}
	c78Chains := make([][]appACLCurrentProfileID, 0, len(c77Chains)*2+1)
	c78Chains = append(c78Chains, c77Chains...)
	c78Chains = append(c78Chains, []appACLCurrentProfileID{appACLCurrentProfileP77})
	for _, chain := range c77Chains {
		withP77 := append([]appACLCurrentProfileID(nil), chain...)
		withP77 = append(withP77, appACLCurrentProfileP77)
		c78Chains = append(c78Chains, withP77)
	}
	c79Chains := make([][]appACLCurrentProfileID, 0, len(c78Chains)*2+1)
	c79Chains = append(c79Chains, c78Chains...)
	c79Chains = append(c79Chains, []appACLCurrentProfileID{appACLCurrentProfileP78})
	for _, chain := range c78Chains {
		withP78 := append([]appACLCurrentProfileID(nil), chain...)
		withP78 = append(withP78, appACLCurrentProfileP78)
		c79Chains = append(c79Chains, withP78)
	}
	return c79Chains
}()

func cloneAppACLCurrentTransitionDefinitions(source []appACLCurrentTransitionDefinition) []appACLCurrentTransitionDefinition {
	result := make([]appACLCurrentTransitionDefinition, len(source))
	for index, definition := range source {
		result[index] = definition
		result[index].successorMigrations = append([]string(nil), definition.successorMigrations...)
		result[index].predecessorMigrationGolden = append([]byte(nil), definition.predecessorMigrationGolden...)
		result[index].predecessorPrivilegeGolden = append([]byte(nil), definition.predecessorPrivilegeGolden...)
		result[index].predecessorManifestDigestGolden = append([]byte(nil), definition.predecessorManifestDigestGolden...)
	}
	return result
}

func compileAppACLCurrentTransitions(
	current appACLCurrentSourceContract,
	definitions []appACLCurrentTransitionDefinition,
) ([]appACLCurrentTransition, error) {
	if len(definitions) == 0 {
		return nil, fmt.Errorf("current APP ACL transition registry has no definitions")
	}
	if len(definitions) != 16 {
		return nil, fmt.Errorf("current APP ACL transition registry must contain exactly the P62, P64, P63, P66, P67, P68, P69, P70, P71, P72, P73, P74, P75, P76, P77 and P78 profiles")
	}
	expectedDefinitions := cloneAppACLCurrentTransitionDefinitions(appACLCurrentTransitionDefinitions)
	compiled := make([]appACLCurrentTransition, 0, len(definitions))
	claimedProfiles := make(map[appACLCurrentProfileID]struct{}, len(definitions))
	for index, definition := range cloneAppACLCurrentTransitionDefinitions(definitions) {
		if definition.profile != appACLCurrentProfileP62 && definition.profile != appACLCurrentProfileP64 &&
			definition.profile != appACLCurrentProfileP63 && definition.profile != appACLCurrentProfileP66 &&
			definition.profile != appACLCurrentProfileP67 && definition.profile != appACLCurrentProfileP68 &&
			definition.profile != appACLCurrentProfileP69 && definition.profile != appACLCurrentProfileP70 &&
			definition.profile != appACLCurrentProfileP71 && definition.profile != appACLCurrentProfileP72 &&
			definition.profile != appACLCurrentProfileP73 && definition.profile != appACLCurrentProfileP74 &&
			definition.profile != appACLCurrentProfileP75 && definition.profile != appACLCurrentProfileP76 &&
			definition.profile != appACLCurrentProfileP77 && definition.profile != appACLCurrentProfileP78 {
			return nil, fmt.Errorf("current APP ACL transition %d has unknown predecessor profile", index)
		}
		if _, duplicate := claimedProfiles[definition.profile]; duplicate {
			return nil, fmt.Errorf("current APP ACL transition %d duplicates predecessor profile", index)
		}
		claimedProfiles[definition.profile] = struct{}{}
		expected := expectedDefinitions[index]
		if definition.profile != expected.profile ||
			definition.predecessorLastMigration != expected.predecessorLastMigration ||
			!equalAppACLCurrentMigrationNames(definition.successorMigrations, expected.successorMigrations) ||
			!bytes.Equal(definition.predecessorMigrationGolden, expected.predecessorMigrationGolden) ||
			!bytes.Equal(definition.predecessorPrivilegeGolden, expected.predecessorPrivilegeGolden) ||
			!bytes.Equal(definition.predecessorManifestDigestGolden, expected.predecessorManifestDigestGolden) {
			return nil, fmt.Errorf("current APP ACL transition %d does not match the registered profile, goldens, and exact current suffix", index)
		}
		predecessorIndex := indexOfAppACLCurrentMigration(current.sources.names, definition.predecessorLastMigration)
		if predecessorIndex < 0 {
			return nil, fmt.Errorf("current APP ACL transition %d has unknown predecessor %q", index, definition.predecessorLastMigration)
		}
		for successorIndex, name := range definition.successorMigrations {
			currentIndex := indexOfAppACLCurrentMigration(current.sources.names, name)
			if currentIndex != predecessorIndex+1+successorIndex {
				return nil, fmt.Errorf("current APP ACL transition %d successor migrations are out of order", index)
			}
		}
		if len(definition.successorMigrations) != len(current.sources.names)-predecessorIndex-1 {
			return nil, fmt.Errorf("current APP ACL transition %d successor migrations are not the exact current suffix", index)
		}
		transition, err := compileAppACLCurrentTransition(current, predecessorIndex, definition)
		if err != nil {
			return nil, fmt.Errorf("compile current APP ACL transition %d: %w", index, err)
		}
		compiled = append(compiled, transition)
	}

	// P62/P64/P63 predate 0065, so they share one privilege body and still
	// need its two runtime UPDATE grants. P66 already carries them.
	for _, transition := range compiled[1:3] {
		if !bytes.Equal(compiled[0].predecessorPrivilegeBody, transition.predecessorPrivilegeBody) {
			return nil, fmt.Errorf("registered P62, P64 and P63 privilege profiles differ")
		}
	}
	for _, transition := range compiled {
		if err := validateAppACLCurrentTransitionPrivilegeDelta(
			transition.predecessorPrivilegeBody,
			current,
			transition.profile,
		); err != nil {
			return nil, err
		}
	}
	return compiled, nil
}

func compileAppACLCurrentTransition(
	current appACLCurrentSourceContract,
	predecessorIndex int,
	definition appACLCurrentTransitionDefinition,
) (appACLCurrentTransition, error) {
	predecessorSources, err := appACLCurrentMigrationSubset(current.sources, 0, predecessorIndex+1)
	if err != nil {
		return appACLCurrentTransition{}, err
	}
	successor, err := appACLCurrentMigrationSubset(current.sources, predecessorIndex+1, len(current.sources.names))
	if err != nil {
		return appACLCurrentTransition{}, err
	}
	predecessor := appACLCurrentSourceContract{sources: predecessorSources}
	for _, fragment := range current.fragments {
		if indexOfAppACLCurrentMigration(predecessorSources.names, fragment.Migration) >= 0 {
			predecessor.fragments = append(predecessor.fragments, cloneAppACLCurrentCompiledMigrationFragment(fragment))
		}
	}
	if !bytes.Equal(predecessor.sources.canonicalSet, definition.predecessorMigrationGolden) {
		return appACLCurrentTransition{}, fmt.Errorf("predecessor migration source differs from released golden")
	}
	predecessorPrivileges, err := appACLCurrentTransitionPrivilegeBody(predecessor)
	if err != nil {
		return appACLCurrentTransition{}, err
	}
	if !bytes.Equal(predecessorPrivileges, definition.predecessorPrivilegeGolden) {
		return appACLCurrentTransition{}, fmt.Errorf("predecessor privilege body differs from released golden")
	}
	if definition.profile == appACLCurrentProfileP62 && len(definition.predecessorManifestDigestGolden) != 32 {
		return appACLCurrentTransition{}, fmt.Errorf("P62 predecessor manifest digest golden has invalid length")
	}
	if definition.profile != appACLCurrentProfileP62 && len(definition.predecessorManifestDigestGolden) != 0 {
		return appACLCurrentTransition{}, fmt.Errorf("P63/P64/P66/P67/P68/P69/P70/P71/P72/P73/P74/P75/P76/P77/P78 predecessor must support role-bound manifest identities")
	}
	manifest, err := NewAppACLManifestPersistedV1(1, appACLCurrentTransitionMigrator, [32]byte{}, predecessor.sources.canonicalSet, predecessorPrivileges)
	if err != nil {
		return appACLCurrentTransition{}, fmt.Errorf("build predecessor manifest: %w", err)
	}
	if len(definition.predecessorManifestDigestGolden) > 0 &&
		!bytes.Equal(manifest.ManifestDigest[:], definition.predecessorManifestDigestGolden) {
		return appACLCurrentTransition{}, fmt.Errorf("predecessor manifest digest differs from released golden")
	}
	return appACLCurrentTransition{
		profile:                   definition.profile,
		predecessor:               predecessor,
		successor:                 successor,
		predecessorPrivilegeBody:  append([]byte(nil), predecessorPrivileges...),
		predecessorManifestDigest: manifest.ManifestDigest,
	}, nil
}

func validateAppACLCurrentTransitionPrivilegeDelta(
	predecessorPrivilegeBody []byte,
	current appACLCurrentSourceContract,
	profile appACLCurrentProfileID,
) error {
	oldSet, err := ParseCanonicalPrivilegeSetBodyV1(predecessorPrivilegeBody)
	if err != nil {
		return fmt.Errorf("parse registered APP predecessor privileges: %w", err)
	}
	currentBody, err := appACLCurrentTransitionPrivilegeBody(current)
	if err != nil {
		return err
	}
	currentSet, err := ParseCanonicalPrivilegeSetBodyV1(currentBody)
	if err != nil {
		return fmt.Errorf("parse current APP privileges: %w", err)
	}
	if !equalAppACLRoleBindings(oldSet.RoleBindings, currentSet.RoleBindings) {
		return fmt.Errorf("registered APP transition changed role bindings")
	}
	currentPrivileges := make(map[AppACLPrivilege]struct{}, len(currentSet.Privileges))
	for _, privilege := range currentSet.Privileges {
		currentPrivileges[privilege] = struct{}{}
	}
	for _, privilege := range oldSet.Privileges {
		if _, retained := currentPrivileges[privilege]; !retained {
			return fmt.Errorf("registered APP transition removes predecessor privilege")
		}
	}
	oldPrivileges := make(map[AppACLPrivilege]struct{}, len(oldSet.Privileges))
	for _, privilege := range oldSet.Privileges {
		oldPrivileges[privilege] = struct{}{}
	}
	additions := make(map[AppACLPrivilege]struct{}, len(currentSet.Privileges)-len(oldSet.Privileges))
	for _, privilege := range currentSet.Privileges {
		if _, existed := oldPrivileges[privilege]; !existed {
			additions[privilege] = struct{}{}
		}
	}
	// C78 replaces an existing view and C79 adds an ordinary index only.
	// Their predecessors already own every approved grant; additions are drift.
	if profile == appACLCurrentProfileP77 || profile == appACLCurrentProfileP78 {
		if len(additions) != 0 {
			return fmt.Errorf("registered APP transition privilege delta is not exactly the approved current fragments")
		}
		return nil
	}
	expectedAdditions := map[AppACLPrivilege]struct{}{}
	switch profile {
	case appACLCurrentProfileP62, appACLCurrentProfileP64, appACLCurrentProfileP63:
		for _, table := range []string{"asset_services", "asset_domains"} {
			expectedAdditions[AppACLPrivilege{
				Subject:        AppACLSubjectCenterRuntime,
				ObjectClass:    AppACLObjectClassTable,
				SchemaName:     appACLManagedPublicSchemaR1,
				ObjectIdentity: table,
				Privilege:      AppACLPrivilegeUpdate,
			}] = struct{}{}
		}
	case appACLCurrentProfileP66, appACLCurrentProfileP67, appACLCurrentProfileP68, appACLCurrentProfileP69, appACLCurrentProfileP70, appACLCurrentProfileP71, appACLCurrentProfileP72, appACLCurrentProfileP73, appACLCurrentProfileP74, appACLCurrentProfileP75, appACLCurrentProfileP76:
	default:
		return fmt.Errorf("registered APP transition has unknown predecessor profile %d", profile)
	}
	if profile != appACLCurrentProfileP67 && profile != appACLCurrentProfileP68 &&
		profile != appACLCurrentProfileP69 && profile != appACLCurrentProfileP70 &&
		profile != appACLCurrentProfileP71 && profile != appACLCurrentProfileP72 &&
		profile != appACLCurrentProfileP73 && profile != appACLCurrentProfileP74 &&
		profile != appACLCurrentProfileP75 && profile != appACLCurrentProfileP76 {
		for _, privilege := range vpsMonitoringLifecycleAppACLCurrentMigrationFragment().Privileges(appACLCurrentTransitionDatabase) {
			expectedAdditions[privilege] = struct{}{}
		}
	}
	if profile != appACLCurrentProfileP68 && profile != appACLCurrentProfileP69 &&
		profile != appACLCurrentProfileP70 && profile != appACLCurrentProfileP71 &&
		profile != appACLCurrentProfileP72 && profile != appACLCurrentProfileP73 &&
		profile != appACLCurrentProfileP74 && profile != appACLCurrentProfileP75 && profile != appACLCurrentProfileP76 {
		for _, privilege := range ipQualityHostAddressIdentityAppACLCurrentMigrationFragment().Privileges(appACLCurrentTransitionDatabase) {
			expectedAdditions[privilege] = struct{}{}
		}
	}
	if profile != appACLCurrentProfileP71 && profile != appACLCurrentProfileP72 &&
		profile != appACLCurrentProfileP73 && profile != appACLCurrentProfileP74 &&
		profile != appACLCurrentProfileP75 && profile != appACLCurrentProfileP76 {
		for _, privilege := range accessManagementAppACLCurrentMigrationFragment().Privileges(appACLCurrentTransitionDatabase) {
			expectedAdditions[privilege] = struct{}{}
		}
	}
	if profile != appACLCurrentProfileP76 {
		expectedAdditions[AppACLPrivilege{
			Subject:        AppACLSubjectCenterRuntime,
			ObjectClass:    AppACLObjectClassView,
			SchemaName:     appACLManagedPublicSchemaR1,
			ObjectIdentity: "ip_quality_vps_family_state",
			Privilege:      AppACLPrivilegeSelect,
		}] = struct{}{}
	}
	// Keep the seven approved tuples independent of the fragment under test.
	for _, table := range []string{"ip_quality_intelligence", "ip_quality_source_cache"} {
		for _, kind := range []AppACLPrivilegeKind{AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeUpdate} {
			expectedAdditions[AppACLPrivilege{Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassTable, SchemaName: appACLManagedPublicSchemaR1, ObjectIdentity: table, Privilege: kind}] = struct{}{}
		}
	}
	expectedAdditions[AppACLPrivilege{Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassTable, SchemaName: appACLManagedPublicSchemaR1, ObjectIdentity: "ip_quality_source_cache", Privilege: AppACLPrivilegeDelete}] = struct{}{}
	if len(additions) != len(expectedAdditions) {
		return fmt.Errorf("registered APP transition privilege delta is not exactly the approved current fragments")
	}
	for privilege := range expectedAdditions {
		if _, present := additions[privilege]; !present {
			return fmt.Errorf("registered APP transition privilege delta is not exactly the approved current fragments")
		}
	}
	return nil
}

func equalAppACLRoleBindings(left, right []AppACLRoleBinding) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func appACLCurrentMigrationSubset(source migrationSourceSnapshot, start, end int) (migrationSourceSnapshot, error) {
	entries, err := ParseCanonicalMigrationSetBodyV1(source.canonicalSet)
	if err != nil {
		return migrationSourceSnapshot{}, fmt.Errorf("parse current migration source body: %w", err)
	}
	if start < 0 || end > len(source.names) || start >= end || len(entries) != len(source.names) {
		return migrationSourceSnapshot{}, fmt.Errorf("current migration source subset is invalid")
	}
	canonical, err := CanonicalMigrationSetBodyV1(entries[start:end])
	if err != nil {
		return migrationSourceSnapshot{}, fmt.Errorf("build current migration source subset: %w", err)
	}
	result := migrationSourceSnapshot{
		sources:      make(map[string]migrationSource, end-start),
		names:        append([]string(nil), source.names[start:end]...),
		canonicalSet: canonical,
	}
	for _, name := range result.names {
		result.sources[name] = source.sources[name]
	}
	return result, nil
}

func appACLCurrentTransitionPrivilegeBody(source appACLCurrentSourceContract) ([]byte, error) {
	return appACLCurrentTransitionPrivilegeBodyFor(
		source,
		appACLCurrentTransitionDatabase,
		appACLCurrentTransitionBindings,
		appACLCurrentTransitionMigrator,
	)
}

func appACLCurrentTransitionPrivilegeBodyFor(
	source appACLCurrentSourceContract,
	databaseName string,
	bindings []AppACLRoleBinding,
	migratorRole string,
) ([]byte, error) {
	catalog, err := compileAppACLCurrentCatalogContract(source, databaseName, bindings, migratorRole)
	if err != nil {
		return nil, fmt.Errorf("compile transition privilege contract: %w", err)
	}
	body, err := CanonicalPrivilegeSetBodyV1(catalog.RoleBindings, catalog.Privileges)
	if err != nil {
		return nil, fmt.Errorf("encode transition privilege contract: %w", err)
	}
	return body, nil
}

func cloneAppACLCurrentCompiledMigrationFragment(source appACLCurrentCompiledMigrationFragment) appACLCurrentCompiledMigrationFragment {
	return appACLCurrentCompiledMigrationFragment{
		Migration:           source.Migration,
		Objects:             append([]AppACLManagedObjectR1(nil), source.Objects...),
		Privileges:          append([]AppACLPrivilege(nil), source.Privileges...),
		AuxiliaryPrivileges: append([]AppACLCurrentAuxiliaryPrivilege(nil), source.AuxiliaryPrivileges...),
		Functions:           cloneAppACLCurrentFunctionContracts(source.Functions),
	}
}

func indexOfAppACLCurrentMigration(names []string, target string) int {
	for index, name := range names {
		if name == target {
			return index
		}
	}
	return -1
}
func equalAppACLCurrentMigrationNames(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
