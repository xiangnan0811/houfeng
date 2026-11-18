package handlers

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"houfeng/internal/center/records"
)

func TestRecordRoutesAndCursorsAcceptIdentityDomains(t *testing.T) {
	updatedAt := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	for _, recordID := range []string{
		"rec_legacy",
		"rec2_70000000000000000000000000000000",
		"rec2_80000000000000000000000000000000",
	} {
		t.Run(recordID, func(t *testing.T) {
			segments, ok := recordPathSegments("/api/records/" + recordID + "/revisions")
			if !ok || len(segments) != 2 || segments[0] != recordID || segments[1] != "revisions" {
				t.Fatalf("recordPathSegments() = %#v, %v", segments, ok)
			}
			cursor := records.RecordCursor{UpdatedAt: updatedAt, RecordID: recordID}
			encoded, err := encodeRecordCursor(cursor)
			if err != nil {
				t.Fatalf("encodeRecordCursor() error = %v", err)
			}
			decoded, err := decodeRecordCursor(encoded)
			if err != nil || decoded.RecordID != recordID || !decoded.UpdatedAt.Equal(updatedAt) {
				t.Fatalf("decodeRecordCursor() = %#v, %v", decoded, err)
			}
		})
	}
	for _, recordID := range []string{"rrv_revision", "rec2_7000000000000000000000000000000g"} {
		if _, ok := recordPathSegments("/api/records/" + recordID); ok {
			t.Fatalf("recordPathSegments(%q) accepted invalid identity", recordID)
		}
		if _, err := encodeRecordCursor(records.RecordCursor{UpdatedAt: updatedAt, RecordID: recordID}); err == nil {
			t.Fatalf("encodeRecordCursor(%q) accepted invalid identity", recordID)
		}
		payload, err := json.Marshal(recordCursorEnvelope{Version: recordCursorVersion, UpdatedAt: updatedAt, RecordID: recordID})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := decodeRecordCursor(base64.RawURLEncoding.EncodeToString(payload)); err == nil {
			t.Fatalf("decodeRecordCursor(%q) accepted invalid identity", recordID)
		}
	}
}
