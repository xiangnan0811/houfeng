package config_test

import (
	"testing"

	centerconfig "houfeng/internal/center/config"
)

func TestLoadCenterConfigRequiresDatabaseURL(t *testing.T) {
	t.Setenv("HOUFENG_HTTP_ADDR", ":8080")
	t.Setenv("HOUFENG_WEB_DIST_DIR", "web/dist")
	t.Setenv("HOUFENG_DATABASE_URL", "")

	if _, err := centerconfig.LoadCenterConfig(); err == nil {
		t.Fatal("LoadCenterConfig() error = nil, want non-nil")
	}
}
