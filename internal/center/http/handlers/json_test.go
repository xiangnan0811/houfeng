package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSONRejectsTrailingValue(t *testing.T) {
	t.Parallel()

	var dst struct {
		Name string `json:"name"`
	}
	req := httptest.NewRequest(http.MethodPost, "/api/test", strings.NewReader(`{"name":"one"}{"name":"two"}`))
	recorder := httptest.NewRecorder()

	err := decodeJSONLimited(recorder, req, &dst, 1024)
	if err == nil {
		t.Fatal("decodeJSONLimited returned nil, want trailing JSON error")
	}
}

func TestDecodeJSONRejectsOversizedBody(t *testing.T) {
	t.Parallel()

	var dst struct {
		Name string `json:"name"`
	}
	req := httptest.NewRequest(http.MethodPost, "/api/test", strings.NewReader(`{"name":"abcdef"}`))
	recorder := httptest.NewRecorder()

	err := decodeJSONLimited(recorder, req, &dst, 8)
	if err == nil {
		t.Fatal("decodeJSONLimited returned nil, want body size error")
	}
}

func TestDecodeJSONRejectsDefaultOversizedBody(t *testing.T) {
	t.Parallel()

	var dst struct {
		Name string `json:"name"`
	}
	req := httptest.NewRequest(http.MethodPost, "/api/test", strings.NewReader(`{"name":"`+strings.Repeat("x", DefaultJSONBodyLimit)+`"}`))

	err := decodeJSON(req, &dst)
	if err == nil {
		t.Fatal("decodeJSON returned nil, want default body size error")
	}
}

func TestDecodeJSONKeepsUnknownFieldRejection(t *testing.T) {
	t.Parallel()

	var dst struct {
		Name string `json:"name"`
	}
	req := httptest.NewRequest(http.MethodPost, "/api/test", strings.NewReader(`{"name":"one","extra":true}`))
	recorder := httptest.NewRecorder()

	err := decodeJSONLimited(recorder, req, &dst, 1024)
	if err == nil {
		t.Fatal("decodeJSONLimited returned nil, want unknown field error")
	}
}

func TestWriteJSONEncodeFailureDoesNotExposeEncoderDetail(t *testing.T) {
	recorder := httptest.NewRecorder()

	writeJSON(recorder, http.StatusOK, func() {})

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
	body := recorder.Body.String()
	if strings.Contains(body, "encode json") || strings.Contains(body, "unsupported type") {
		t.Fatalf("body leaked encoder detail: %q", body)
	}
	if !strings.Contains(body, "internal server error") {
		t.Fatalf("body = %q, want generic internal server error", body)
	}
}
