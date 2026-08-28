package handlers_test

import (
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"houfeng/internal/center/createidempotency"
)

type testIdempotentCreateReceipt[T any] struct {
	fingerprint [sha256.Size]byte
	record      T
}

type testIdempotentCreateState[T any] struct {
	methodCalls      int
	materializations int
	receipts         map[string]testIdempotentCreateReceipt[T]
}

func (s *testIdempotentCreateState[T]) create(key string, identity any, materialize func() T) (T, bool, error) {
	s.methodCalls++
	encoded, err := json.Marshal(identity)
	if err != nil {
		var zero T
		return zero, false, err
	}
	fingerprint := sha256.Sum256(encoded)
	if receipt, ok := s.receipts[key]; ok {
		if receipt.fingerprint != fingerprint {
			var zero T
			return zero, false, createidempotency.ErrIdempotencyKeyReused
		}
		return receipt.record, true, nil
	}

	record := materialize()
	s.materializations++
	if s.receipts == nil {
		s.receipts = make(map[string]testIdempotentCreateReceipt[T])
	}
	s.receipts[key] = testIdempotentCreateReceipt[T]{fingerprint: fingerprint, record: record}
	return record, false, nil
}

func serveTestIdempotentCreate(handler http.Handler, path, body, key string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Idempotency-Key", key)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func decodeTestResponse[T any](t *testing.T, recorder *httptest.ResponseRecorder) T {
	t.Helper()
	var response T
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal response error type = %T", err)
	}
	return response
}

func assertTestIdempotentCreateCounts[T any](t *testing.T, state *testIdempotentCreateState[T], wantCalls, wantMaterializations int) {
	t.Helper()
	if state.methodCalls != wantCalls || state.materializations != wantMaterializations {
		t.Fatalf("repository calls = %d, materializations = %d, want %d and %d", state.methodCalls, state.materializations, wantCalls, wantMaterializations)
	}
}
