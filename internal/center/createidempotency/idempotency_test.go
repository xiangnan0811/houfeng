package createidempotency

import (
	"errors"
	"regexp"
	"strings"
	"testing"
)

type leakingJSONMarshaler struct {
	secret string
}

func (m leakingJSONMarshaler) MarshalJSON() ([]byte, error) {
	return nil, errors.New(m.secret)
}

func TestNormalizeKey(t *testing.T) {
	t.Parallel()

	key, err := NormalizeKey("  request_01:retry-2.ok  ")
	if err != nil {
		t.Fatalf("NormalizeKey() error = %T", err)
	}
	if key != "request_01:retry-2.ok" {
		t.Fatal("NormalizeKey() returned an unexpected normalized value")
	}
}

func TestNormalizeKeyRejectsInvalidValuesWithoutLeakingThem(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name  string
		value string
	}{
		{name: "too short", value: "short"},
		{name: "too long", value: strings.Repeat("a", 129)},
		{name: "space", value: "contains space"},
		{name: "slash", value: "contains/slash"},
		{name: "unicode", value: "敏感幂等键不能回显"},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := NormalizeKey(test.value)
			if !errors.Is(err, ErrInvalidIdempotencyKey) {
				t.Fatalf("NormalizeKey() error = %T, want ErrInvalidIdempotencyKey", err)
			}
			if strings.Contains(err.Error(), test.value) {
				t.Fatalf("NormalizeKey() error leaked rejected key")
			}
		})
	}
}

func TestDigestNormalizedRequestIsStableAndSensitiveToFields(t *testing.T) {
	t.Parallel()

	type normalizedRequest struct {
		VPSID   string   `json:"vps_id"`
		Summary string   `json:"summary"`
		Labels  []string `json:"labels"`
	}
	first := normalizedRequest{VPSID: "vps_01", Summary: "packet loss", Labels: []string{"prod", "edge"}}
	second := normalizedRequest{VPSID: "vps_01", Summary: "packet loss", Labels: []string{"prod", "edge"}}

	firstDigest, err := DigestNormalizedRequest(first)
	if err != nil {
		t.Fatalf("DigestNormalizedRequest(first) error = %T", err)
	}
	secondDigest, err := DigestNormalizedRequest(second)
	if err != nil {
		t.Fatalf("DigestNormalizedRequest(second) error = %T", err)
	}
	if firstDigest != secondDigest {
		t.Fatalf("equal normalized requests produced different digests")
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(firstDigest) {
		t.Fatal("DigestNormalizedRequest() returned a non-SHA-256 value")
	}

	second.Summary = "latency"
	changedDigest, err := DigestNormalizedRequest(second)
	if err != nil {
		t.Fatalf("DigestNormalizedRequest(changed) error = %T", err)
	}
	if changedDigest == firstDigest {
		t.Fatal("field change did not change request digest")
	}
}

func TestDigestNormalizedRequestRejectsUnencodableInputWithoutLeakingIt(t *testing.T) {
	t.Parallel()

	secret := "raw-secret-body-must-not-leak"
	_, err := DigestNormalizedRequest(struct {
		Secret string
		Value  chan int
	}{Secret: secret, Value: make(chan int)})
	if err == nil {
		t.Fatal("DigestNormalizedRequest() error = nil, want encoding error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatal("DigestNormalizedRequest() error leaked request content")
	}
}

func TestDigestNormalizedRequestSanitizesCustomMarshalerErrors(t *testing.T) {
	t.Parallel()

	secret := "custom-marshaler-secret-must-not-leak"
	_, err := DigestNormalizedRequest(leakingJSONMarshaler{secret: secret})
	if err == nil {
		t.Fatal("DigestNormalizedRequest() error = nil, want encoding error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatal("DigestNormalizedRequest() error leaked custom marshaler content")
	}
}

func TestNamespacedLockKeySeparatesOperations(t *testing.T) {
	t.Parallel()

	key := "request_01"
	experience := NamespacedLockKey("experience-log.create", key)
	service := NamespacedLockKey("asset-service.create", key)
	if experience == service {
		t.Fatal("different operations produced the same advisory lock namespace")
	}
	if strings.ContainsRune(experience, '\x00') {
		t.Fatal("NamespacedLockKey() contains PostgreSQL-incompatible NUL")
	}
	if experience != "experience-log.create:request_01" {
		t.Fatal("NamespacedLockKey() returned an unexpected namespace")
	}
}

func TestSentinelsAreStableAndDistinct(t *testing.T) {
	t.Parallel()

	if ErrInvalidIdempotencyKey.Error() != "invalid idempotency key" {
		t.Fatal("ErrInvalidIdempotencyKey text changed")
	}
	if ErrIdempotencyKeyReused.Error() != "idempotency key reused" {
		t.Fatal("ErrIdempotencyKeyReused text changed")
	}
	if errors.Is(ErrInvalidIdempotencyKey, ErrIdempotencyKeyReused) {
		t.Fatal("idempotency sentinels must be distinct")
	}
}
