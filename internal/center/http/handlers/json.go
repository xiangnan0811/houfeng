package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

const (
	DefaultJSONBodyLimit = 256 << 10
	AuthJSONBodyLimit    = 16 << 10
	AgentEnrollBodyLimit = 64 << 10
	AgentSyncBodyLimit   = 4 << 20
)

func writeJSON(w http.ResponseWriter, status int, value any) {
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(value); err != nil {
		slog.Error("encode json", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body.Bytes())
}

func decodeJSON(r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, DefaultJSONBodyLimit)
	return decodeJSONValue(r.Body, dst)
}

func decodeJSONLimited(w http.ResponseWriter, r *http.Request, dst any, maxBytes int64) error {
	if maxBytes <= 0 {
		maxBytes = DefaultJSONBodyLimit
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	return decodeJSONValue(r.Body, dst)
}

func decodeJSONValue(body io.Reader, dst any) error {
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if err := decoder.Decode(new(struct{})); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("request body must contain a single json value")
		}
		return err
	}
	return nil
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeCodedError(w http.ResponseWriter, status int, message, code string) {
	body := map[string]string{"error": message}
	if strings.TrimSpace(code) != "" {
		body["code"] = strings.TrimSpace(code)
	}
	writeJSON(w, status, body)
}
