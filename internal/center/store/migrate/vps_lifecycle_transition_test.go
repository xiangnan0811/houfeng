package migrate

import "testing"

func TestCurrentLifecycleSettingsExactTransformation(t *testing.T) {
	before := []byte(`{"retention_policy":{"raw_layer_days":31,"aggregate_layer_days":32,"event_layer_days":90,"notification_layer_days":90,"custom":"preserve"},"ip_quality_settings":{"raw_retention_days":30,"history_retention_days":365,"enabled":true},"telegram_chat_id":"preserve"}`)
	got, err := appACLCurrentLifecycleSettings(before)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte(`{"retention_policy":{"raw_layer_days":30,"aggregate_layer_days":365,"custom":"preserve"},"ip_quality_settings":{"enabled":true},"telegram_chat_id":"preserve"}`)
	if !appACLCurrentJSONEqual(got, want) {
		t.Fatalf("lifecycle transition settings = %s, want %s", got, want)
	}
	if _, err := appACLCurrentLifecycleSettings([]byte(`{"retention_policy":null,"ip_quality_settings":{}}`)); err == nil {
		t.Fatal("invalid settings accepted")
	}
}
