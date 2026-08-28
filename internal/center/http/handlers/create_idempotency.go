package handlers

import (
	"errors"
	"net/http"

	"houfeng/internal/center/createidempotency"
)

func requestCreateIdempotencyKey(r *http.Request) (string, bool) {
	values := r.Header.Values("Idempotency-Key")
	if len(values) != 1 {
		return "", false
	}
	key, err := createidempotency.NormalizeKey(values[0])
	return key, err == nil
}

func writeInvalidCreateIdempotencyKey(w http.ResponseWriter) {
	writeCodedError(w, http.StatusBadRequest, "invalid idempotency key", "invalid_idempotency_key")
}

func writeCreateIdempotencyError(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, createidempotency.ErrInvalidIdempotencyKey):
		writeInvalidCreateIdempotencyKey(w)
	case errors.Is(err, createidempotency.ErrIdempotencyKeyReused):
		writeCodedError(w, http.StatusConflict, "idempotency key reused", "idempotency_key_reused")
	default:
		return false
	}
	return true
}

func idempotentCreateStatus(replayed bool) int {
	if replayed {
		return http.StatusOK
	}
	return http.StatusCreated
}
