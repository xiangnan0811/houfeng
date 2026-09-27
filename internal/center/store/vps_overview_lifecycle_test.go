package store

import (
	"testing"
	"time"

	"houfeng/internal/center/assetlinks"
	centersettings "houfeng/internal/center/settings"
	"houfeng/internal/center/vpsoverview"
)

func TestOverviewIgnoresRetiredInstancesAndUntrustedHeartbeatFreshness(t *testing.T) {
	now := time.Now().UTC()
	links := []assetlinks.MonitoringInstanceSummary{
		{MonitoringInstanceID: "retired", LifecycleStatus: "已退役", MonitoringStatus: "启用", CurrentHealthStatus: "严重", LastTrustedOnlineAt: &now},
		{MonitoringInstanceID: "pending", LifecycleStatus: "待接入", MonitoringStatus: "启用", CurrentHealthStatus: "正常", LastHeartbeatAt: &now},
	}
	got, err := monitoringFromLinksAt(links, now, centersettings.Default())
	if err != nil {
		t.Fatal(err)
	}
	if got.Count != 1 || got.MonitoringInstanceID != "pending" || got.Health != "unknown" || got.Section.ObservedAt != nil || got.Section.State != vpsoverview.SectionReady {
		t.Fatalf("retired or untrusted heartbeat contaminated current projection: %#v", got)
	}
}

func TestOverviewControlStatesCannotAppearHealthy(t *testing.T) {
	now := time.Now().UTC()
	for _, control := range []string{"暂停", "维护中"} {
		got := monitoringSourceFromLinks([]assetlinks.MonitoringInstanceSummary{{
			MonitoringInstanceID: "current", LifecycleStatus: "已接入", MonitoringStatus: control,
			CurrentHealthStatus: "正常", CurrentActiveIncidentCount: 2, LastTrustedOnlineAt: &now,
		}})
		if got.Health == "正常" || got.ActiveIncidents != 0 || got.Status != control {
			t.Fatalf("control %q appeared healthy or running abnormal: %#v", control, got)
		}
	}
}
