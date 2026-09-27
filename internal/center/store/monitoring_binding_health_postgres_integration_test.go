package store

import (
	"context"
	"testing"
	"time"

	"houfeng/internal/center/enrollment"
	"houfeng/internal/center/monitoringinstances"
)

func TestPostgresIntegrationFingerprintCollisionClearsPersistedHealthyState(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	fixture := newRecordsPostgresFixture(t, ctx)
	pool := fixture.openDirectRuntimePool(t, ctx, "monitoring-binding-health", 4)
	if _, err := fixture.db.Exec(ctx, `insert into vps_assets(vps_id,display_name,lifecycle_status) values('vps_fp_health','Fingerprint health','active')`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(ctx, `insert into monitoring_instances(monitoring_instance_id,vps_id,display_name,region,city,provider,lifecycle_status,monitoring_status,binding_status,binding_fingerprint,current_health_status,ever_connected,last_trusted_online_at,enrollment_token_hash,enrollment_token_issued_at) values('mi_fp_health','vps_fp_health','Fingerprint health','','','','已接入','启用','已绑定','fp_original','正常',true,now(),$1,now())`, hashEnrollmentToken("enroll_collision")); err != nil {
		t.Fatal(err)
	}
	repo := NewPostgresMonitoringInstanceRepository(pool)
	record, token, err := repo.ApplyEnrollment(ctx, enrollment.EnrollInput{Token: "enroll_collision", Fingerprint: "fp_different"})
	if err != nil {
		t.Fatal(err)
	}
	if token != "" || record.BindingStatus != monitoringinstances.BindingPendingConfirmation || record.CurrentHealthStatus == monitoringinstances.HealthNormal {
		t.Fatalf("unconfirmed host received healthy state or credential: record=%+v tokenPresent=%v", record, token != "")
	}
	var binding, health string
	if err := fixture.db.QueryRow(ctx, `select binding_status,current_health_status from monitoring_instances where monitoring_instance_id='mi_fp_health'`).Scan(&binding, &health); err != nil {
		t.Fatal(err)
	}
	if binding != monitoringinstances.BindingPendingConfirmation || health != "数据不可用" {
		t.Fatalf("persisted binding=%q health=%q", binding, health)
	}
}
