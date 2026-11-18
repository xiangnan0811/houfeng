//go:build linux && amd64 && !cgo

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

const (
	earlyTestCaseSHA = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	earlyTestVariant = "seal-bpf-link"
)

func earlyTestPayload(bodies []string) map[string]any {
	records := make([]any, len(bodies))
	for i, body := range bodies {
		records[i] = map[string]any{"body": body, "sequence": json.Number(strconv.Itoa(i + 1))}
	}
	return map[string]any{
		"version": json.Number("1"), "run_token": authorityRunToken,
		"case_sha256": earlyTestCaseSHA, "seal_stage": earlyTestVariant,
		"init_handoff": false, "terminal_observation": map[string]any{"records": records},
	}
}

func earlyTestExpected(escapedBodies []string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, `{"case_sha256":"%s","init_handoff":false,"run_token":"%s","seal_stage":"%s","terminal_observation":{"records":[`, earlyTestCaseSHA, authorityRunToken, earlyTestVariant)
	for i, body := range escapedBodies {
		if i != 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"body":"%s","sequence":%d}`, body, i+1)
	}
	b.WriteString("]},\"version\":1}\n")
	return []byte(b.String())
}

// The host consumes canonical BEGIN/CHUNK/END frames and reconstructs the
// exact LF-terminated payload before checking its size and SHA-256.
func requireEarlyTestStream(t *testing.T, stream, payload []byte) {
	t.Helper()
	chunks := (len(payload) + 8191) / 8192
	lines := bytes.Split(stream, []byte("\n"))
	if len(lines) != chunks+3 || len(lines[len(lines)-1]) != 0 {
		t.Fatalf("incomplete early stream: got %d lines, want %d plus final newline", len(lines)-1, chunks+2)
	}
	sum := sha256.Sum256(payload)
	begin := fmt.Sprintf(`G2_BPF_EARLY_BEGIN {"bytes":%d,"case_sha256":"%s","chunks":%d,"run_token":"%s","sha256":"%x","variant":"%s","version":1}`, len(payload), earlyTestCaseSHA, chunks, authorityRunToken, sum, earlyTestVariant)
	end := fmt.Sprintf(`G2_BPF_EARLY_END {"bytes":%d,"case_sha256":"%s","chunks":%d,"run_token":"%s","sha256":"%x","version":1}`, len(payload), earlyTestCaseSHA, chunks, authorityRunToken, sum)
	if string(lines[0]) != begin || string(lines[len(lines)-2]) != end {
		t.Fatalf("early stream BEGIN or END differs from host contract")
	}
	for i := range chunks {
		start := i * 8192
		endOff := min(start+8192, len(payload))
		want := fmt.Sprintf(`G2_BPF_EARLY_CHUNK {"data":"%s","sequence":%d}`, base64.StdEncoding.EncodeToString(payload[start:endOff]), i)
		if string(lines[i+1]) != want {
			t.Fatalf("chunk %d differs from the exact payload slice", i)
		}
	}
	for i, line := range lines[:len(lines)-1] {
		prefixEnd := bytes.IndexByte(line, ' ')
		if prefixEnd < 0 || len(line)-prefixEnd > 16*1024 {
			t.Fatalf("frame %d exceeds its 16KiB limit including LF", i)
		}
		if _, err := parseCanonicalControl(line[prefixEnd+1:]); err != nil {
			t.Fatalf("frame %d is not canonical control JSON: %v", i, err)
		}
	}
}

func TestEarlyResultChunkedPayload(t *testing.T) {
	bodies := []string{
		"begin \"quoted\"\\\n\t\b\f\r\x01\x7fé😀",
		strings.Repeat("a", 6000), strings.Repeat("b", 6000), strings.Repeat("c", 6000),
	}
	escaped := []string{
		`begin \"quoted\"\\\n\t\b\f\r\u0001\u007f\u00e9\ud83d\ude00`,
		bodies[1], bodies[2], bodies[3],
	}
	payload := earlyTestPayload(bodies)
	for i, record := range payload["terminal_observation"].(map[string]any)["records"].([]any) {
		if _, err := canonicalControl(record); err != nil {
			t.Fatalf("record %d exceeds the individual control limit: %v", i, err)
		}
	}
	if _, err := canonicalControl(payload); !errors.Is(err, errControlFrame) {
		t.Fatalf("aggregate larger than 16KiB must not be a control frame: %v", err)
	}
	var stream bytes.Buffer
	if err := writeEarlyResult(&stream, payload, earlyTestCaseSHA, earlyTestVariant); err != nil {
		t.Fatal(err)
	}
	requireEarlyTestStream(t, stream.Bytes(), earlyTestExpected(escaped))
}

// Construct independent expected JSON lengths while keeping every individual
// record small enough for the existing control-frame contract.
func earlyTestBoundary(escaped bool) ([]string, []string) {
	const records = 8192
	bodies := make([]string, records)
	encoded := make([]string, records)
	base := len(earlyTestExpected(encoded))
	remaining := 64*1024*1024 - base
	for i := range bodies {
		size := remaining / (records - i)
		remaining -= size
		if escaped {
			bodies[i] = strings.Repeat("\n", size/2) + strings.Repeat("x", size%2)
			encoded[i] = strings.Repeat(`\n`, size/2) + strings.Repeat("x", size%2)
		} else {
			bodies[i] = strings.Repeat("x", size)
			encoded[i] = bodies[i]
		}
	}
	return bodies, encoded
}

func TestEarlyResultByteLimits(t *testing.T) {
	for _, tc := range []struct {
		name    string
		escaped bool
		extra   string
	}{
		{"ascii", false, "x"},
		{"escaped", true, "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bodies, encoded := earlyTestBoundary(tc.escaped)
			want := earlyTestExpected(encoded)
			if len(want) != 64*1024*1024 || want[len(want)-1] != '\n' {
				t.Fatalf("bad boundary fixture: %d bytes", len(want))
			}
			payload := earlyTestPayload(bodies)
			for i, record := range payload["terminal_observation"].(map[string]any)["records"].([]any) {
				if _, err := canonicalControl(record); err != nil {
					t.Fatalf("record %d exceeds the control limit: %v", i, err)
				}
			}
			var stream bytes.Buffer
			if err := writeEarlyResult(&stream, payload, earlyTestCaseSHA, earlyTestVariant); err != nil {
				t.Fatalf("exact 64MiB including LF rejected: %v", err)
			}
			requireEarlyTestStream(t, stream.Bytes(), want)

			bodies[len(bodies)-1] += tc.extra
			stream.Reset()
			if err := writeEarlyResult(&stream, earlyTestPayload(bodies), earlyTestCaseSHA, earlyTestVariant); !errors.Is(err, errControlFrame) {
				t.Fatalf("over-limit aggregate accepted: %v", err)
			}
			if stream.Len() != 0 {
				t.Fatalf("over-limit aggregate emitted a partial result")
			}
		})
	}
}

func TestEarlyResultControlAndCanonicalRules(t *testing.T) {
	atLimit := map[string]any{"k": strings.Repeat("x", 16*1024-len(`{"k":""}`))}
	raw, err := canonicalControl(atLimit)
	if err != nil || len(raw) != 16*1024 {
		t.Fatalf("exact 16KiB control frame rejected: len=%d err=%v", len(raw), err)
	}
	if _, err := parseCanonicalControl(raw); err != nil {
		t.Fatalf("exact 16KiB control frame failed parsing: %v", err)
	}
	tooLarge := map[string]any{"k": atLimit["k"].(string) + "x"}
	if _, err := canonicalControl(tooLarge); !errors.Is(err, errControlFrame) {
		t.Fatalf("over-limit control encoding accepted: %v", err)
	}
	if _, err := parseCanonicalControl(append(raw, ' ')); !errors.Is(err, errControlFrame) {
		t.Fatalf("over-limit control parsing accepted: %v", err)
	}
	rootString := strings.Repeat("x", 16*1024-2)
	rootRaw, err := canonicalControl(rootString)
	if err != nil || len(rootRaw) != 16*1024 {
		t.Fatalf("root string at control limit rejected: len=%d err=%v", len(rootRaw), err)
	}
	if _, err := canonicalControl(rootString + "x"); !errors.Is(err, errControlFrame) {
		t.Fatalf("root string over control limit accepted: %v", err)
	}
	key := strings.Repeat("\n", (16*1024-len(`{"":null}`))/2) + "x"
	keyFrame := map[string]any{key: nil}
	keyRaw, err := canonicalControl(keyFrame)
	if err != nil || len(keyRaw) != 16*1024 {
		t.Fatalf("escaped key at control limit rejected: len=%d err=%v", len(keyRaw), err)
	}
	if _, err := canonicalControl(map[string]any{key + "\n": nil}); !errors.Is(err, errControlFrame) {
		t.Fatalf("escaped key over control limit accepted: %v", err)
	}

	var depth64 any = "ok"
	for range 63 {
		depth64 = []any{depth64}
	}
	for _, tc := range []struct {
		name  string
		value any
		want  string
	}{
		{"depth-64", depth64, `{"value":` + strings.Repeat("[", 63) + `"ok"` + strings.Repeat("]", 63) + `}`},
		{"depth-65", []any{depth64}, ""},
		{"max-unsigned", json.Number("18446744073709551615"), `{"value":18446744073709551615}`},
		{"min-signed", json.Number("-9223372036854775808"), `{"value":-9223372036854775808}`},
		{"unsigned-overflow", json.Number("18446744073709551616"), ""},
		{"float-number", json.Number("1e3"), ""},
		{"negative-zero", json.Number("-0"), ""},
		{"unknown-integer-type", 1, ""},
		{"invalid-utf8", string([]byte{0xff}), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := map[string]any{"value": tc.value}
			controlRaw, controlErr := canonicalControl(payload)
			var stream bytes.Buffer
			earlyErr := writeEarlyResult(&stream, payload, earlyTestCaseSHA, earlyTestVariant)
			if tc.want != "" {
				if controlErr != nil || earlyErr != nil || string(controlRaw) != tc.want {
					t.Fatalf("valid value differs: control=%q controlErr=%v earlyErr=%v", controlRaw, controlErr, earlyErr)
				}
				requireEarlyTestStream(t, stream.Bytes(), []byte(tc.want+"\n"))
			} else if !errors.Is(controlErr, errControlFrame) || !errors.Is(earlyErr, errControlFrame) || stream.Len() != 0 {
				t.Fatalf("invalid value accepted or emitted: control=%v early=%v bytes=%d", controlErr, earlyErr, stream.Len())
			}
		})
	}
}

func TestRecoveryDocumentCanonicalLimits(t *testing.T) {
	value := strings.Repeat("z", 17000) + "\n"
	raw := []byte(`{"\u00e9":"` + strings.Repeat("z", 17000) + `\n"}`)
	doc, err := recoveryDocument(raw, len(raw))
	if err != nil || doc["é"] != value {
		t.Fatalf("valid recovery document above 16KiB rejected or changed: %v", err)
	}
	if _, err := recoveryDocument(raw, len(raw)-1); !errors.Is(err, errControlFrame) {
		t.Fatalf("over-limit recovery document accepted: %v", err)
	}
}
