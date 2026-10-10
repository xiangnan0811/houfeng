package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"houfeng/internal/center/monitoringinstances"
)

// 资料编辑的 If-Match 只看 metadata_updated_at：同步推进 updated_at 后，用打开页面时的资料令牌仍能保存；
// 别人先改了资料，旧令牌必须冲突。
func TestPostgresIntegrationMonitoringMetadataVersionIgnoresSyncAdvances(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	fixture := newRecordsPostgresFixture(t, ctx)
	pool := fixture.openDirectRuntimePool(t, ctx, "monitoring-metadata-version", 4)
	if _, err := fixture.db.Exec(ctx, `insert into vps_assets(vps_id,display_name,lifecycle_status) values('vps_meta','vps_meta','active')`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(ctx, `insert into monitoring_instances(monitoring_instance_id,vps_id,display_name,"group",region,city,provider,lifecycle_status,monitoring_status,binding_status,current_health_status,labels,note)
		values('mi_meta','vps_meta','mi_meta','edge','','','','已接入','启用','已绑定','正常','{}','keep')`); err != nil {
		t.Fatal(err)
	}
	repo := NewPostgresMonitoringInstanceRepository(pool)
	opened, err := repo.GetMonitoringInstance(ctx, "mi_meta")
	if err != nil {
		t.Fatal(err)
	}
	if opened.MetadataUpdatedAt.IsZero() {
		t.Fatal("metadata_updated_at is not populated for a new instance")
	}

	// 一次真实的同步推进：updated_at 前进，资料令牌不变。
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := advanceMonitoringInstanceSyncState(ctx, tx, "mi_meta", now, now, monitoringinstances.LifecycleInUse); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	synced, err := repo.GetMonitoringInstance(ctx, "mi_meta")
	if err != nil {
		t.Fatal(err)
	}
	if !synced.UpdatedAt.After(opened.UpdatedAt) || !synced.MetadataUpdatedAt.Equal(opened.MetadataUpdatedAt) {
		t.Fatalf("sync advanced updated_at/metadata = %t/%t, want true/false",
			synced.UpdatedAt.After(opened.UpdatedAt), !synced.MetadataUpdatedAt.Equal(opened.MetadataUpdatedAt))
	}

	token := opened.MetadataUpdatedAt
	saved, err := repo.UpdateMonitoringInstanceMetadata(ctx, "mi_meta", monitoringinstances.UpdateMetadataInput{
		Labels:                    []string{"jp"},
		Note:                      "keep",
		ExpectedMetadataUpdatedAt: &token,
	})
	if err != nil {
		t.Fatalf("save with the token read before the sync: %v", err)
	}
	if !saved.MetadataUpdatedAt.After(token) || !saved.UpdatedAt.After(synced.UpdatedAt) || len(saved.Labels) != 1 || saved.Labels[0] != "jp" {
		t.Fatalf("saved metadata token advanced/updated_at advanced/labels = %t/%t/%v",
			saved.MetadataUpdatedAt.After(token), saved.UpdatedAt.After(synced.UpdatedAt), saved.Labels)
	}

	// 资料已被别人改过：旧令牌冲突，且不写入。
	if _, err := repo.UpdateMonitoringInstanceMetadata(ctx, "mi_meta", monitoringinstances.UpdateMetadataInput{
		Labels:                    []string{"stale"},
		Note:                      "keep",
		ExpectedMetadataUpdatedAt: &token,
	}); !errors.Is(err, monitoringinstances.ErrMonitoringInstanceMetadataConflict) {
		t.Fatalf("stale metadata token error = %v, want metadata conflict", err)
	}
	after, err := repo.GetMonitoringInstance(ctx, "mi_meta")
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Labels) != 1 || after.Labels[0] != "jp" {
		t.Fatalf("labels after rejected stale save = %v, want [jp]", after.Labels)
	}
}
