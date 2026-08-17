package notify

import (
	"encoding/json"
	"io"
)

const maxProviderResponseBytes = 4096

func decodeBoundedProviderResponse(body io.Reader, target any) bool {
	if body == nil || target == nil {
		return false
	}
	payload, err := io.ReadAll(io.LimitReader(body, maxProviderResponseBytes+1))
	if err != nil || len(payload) == 0 || len(payload) > maxProviderResponseBytes {
		return false
	}
	return json.Unmarshal(payload, target) == nil
}

func classifyClosedProviderCode(code int) (SendFailureClass, bool) {
	switch {
	case code == 408, code == 425, code == 429, code >= 500 && code <= 599:
		return SendFailureTemporary, true
	case code >= 400 && code <= 499:
		return SendFailurePermanent, true
	default:
		return "", false
	}
}

func classifyProviderHTTPStatus(status int) (SendFailureClass, bool) {
	if status >= 200 && status <= 299 {
		return "", false
	}
	if class, closed := classifyClosedProviderCode(status); closed {
		return class, true
	}
	return SendFailureUnknown, true
}
