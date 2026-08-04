package recordplatform

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestIssuedDeletionRequestTokenV1GeneratesCanonicalTransport(t *testing.T) {
	token, err := NewIssuedDeletionRequestTokenV1()
	if err != nil {
		t.Fatalf("NewIssuedDeletionRequestTokenV1() error = %v", err)
	}

	transport := token.Transport()
	if !strings.HasPrefix(transport, "drt1_") {
		t.Fatalf("token transport = %q, want drt1_ prefix", transport)
	}
	if len(transport) != len("drt1_")+43 {
		t.Fatalf("token transport length = %d, want %d", len(transport), len("drt1_")+43)
	}
	if strings.Contains(transport, "=") {
		t.Fatalf("token transport = %q, must not have base64 padding", transport)
	}

	parsed, err := ParseDeletionRequestTokenTransportV1(transport)
	if err != nil {
		t.Fatalf("ParseDeletionRequestTokenTransportV1(%q) error = %v", transport, err)
	}
	if got := parsed.Transport(); got != transport {
		t.Fatalf("parsed transport = %q, want %q", got, transport)
	}

	for _, invalid := range []string{
		"",
		"drt_" + transport[len("drt1_"):],
		transport + "A",
		"drt1_" + strings.Repeat("A", 42),
		"drt1_" + strings.Repeat("A", 42) + "+",
	} {
		if _, err := ParseDeletionRequestTokenTransportV1(invalid); !errors.Is(err, ErrInvalidDeletionRequestToken) {
			t.Fatalf("ParseDeletionRequestTokenTransportV1(%q) error = %v, want ErrInvalidDeletionRequestToken", invalid, err)
		}
	}
}

func TestDeletionRequestTokenTransportV1CannotProducePersistentCommitment(t *testing.T) {
	const callerSuppliedLowEntropyTransport = "drt1_" + "ccccccccccccccccccccccccccccccccccccccccccc"

	parsed, err := ParseDeletionRequestTokenTransportV1(callerSuppliedLowEntropyTransport)
	if err != nil {
		t.Fatalf("ParseDeletionRequestTokenTransportV1() error = %v, want canonical transport only", err)
	}
	if got := parsed.Transport(); got != callerSuppliedLowEntropyTransport {
		t.Fatalf("parsed transport = %q, want %q", got, callerSuppliedLowEntropyTransport)
	}
	if _, canCommit := any(parsed).(interface {
		Commitment(DeploymentID, ProjectID) ([32]byte, error)
	}); canCommit {
		t.Fatal("parsed caller transport must not be an issued commitment capability")
	}
}

func TestDeletionRequestTokenTransportV1MatchesIssuedCommitmentWithoutExposingIt(t *testing.T) {
	t.Parallel()

	deploymentID := DeploymentID("dp-" + strings.Repeat("a", 64))
	issued, err := NewIssuedDeletionRequestTokenV1()
	if err != nil {
		t.Fatalf("NewIssuedDeletionRequestTokenV1() error = %v", err)
	}
	commitment, err := issued.Commitment(deploymentID, ProjectIDDefault)
	if err != nil {
		t.Fatalf("Commitment() error = %v", err)
	}
	transport, err := ParseDeletionRequestTokenTransportV1(issued.Transport())
	if err != nil {
		t.Fatalf("ParseDeletionRequestTokenTransportV1() error = %v", err)
	}

	if !transport.MatchesCommitment(deploymentID, ProjectIDDefault, commitment) {
		t.Fatal("MatchesCommitment() = false for the issued token commitment")
	}
	wrongCommitment := commitment
	wrongCommitment[0] ^= 0xff
	if transport.MatchesCommitment(deploymentID, ProjectIDDefault, wrongCommitment) {
		t.Fatal("MatchesCommitment() = true for a different commitment")
	}
	if transport.MatchesCommitment(DeploymentID("dp-"+strings.Repeat("b", 64)), ProjectIDDefault, commitment) {
		t.Fatal("MatchesCommitment() accepted a different deployment scope")
	}
	if transport.MatchesCommitment(deploymentID, ProjectID("other"), commitment) {
		t.Fatal("MatchesCommitment() accepted an invalid project scope")
	}
}

func TestDeletionRequestTokenV1CommitmentDomainSeparatesDeploymentAndProject(t *testing.T) {
	var raw [deletionRequestTokenV1RawLength]byte
	for index := range raw {
		raw[index] = byte(index)
	}
	token := IssuedDeletionRequestTokenV1{raw: raw, issued: true}
	deploymentID := DeploymentID("dp-" + strings.Repeat("a", 64))

	commitment, err := token.Commitment(deploymentID, ProjectIDDefault)
	if err != nil {
		t.Fatalf("Commitment() error = %v", err)
	}
	if got := hex.EncodeToString(commitment[:]); got != "00eeaea1925437b1f7a980b746fd02ec43e45b3d2b558ddae3cdd72aa408f3e2" {
		t.Fatalf("Commitment() = %s, want golden commitment", got)
	}

	for _, scope := range []struct {
		name       string
		deployment DeploymentID
		project    ProjectID
	}{
		{name: "different deployment", deployment: DeploymentID("dp-" + strings.Repeat("b", 64)), project: ProjectIDDefault},
		{name: "different project", deployment: deploymentID, project: ProjectID("other")},
	} {
		t.Run(scope.name, func(t *testing.T) {
			got, err := token.Commitment(scope.deployment, scope.project)
			if scope.name == "different project" {
				if !errors.Is(err, ErrInvalidRecordPlatformInput) {
					t.Fatalf("Commitment() error = %v, want ErrInvalidRecordPlatformInput", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Commitment() error = %v", err)
			}
			if got == commitment {
				t.Fatal("Commitment() did not bind the deployment")
			}
		})
	}
}

func TestDeletionRequestTokenV1FormatsAsRedacted(t *testing.T) {
	token, err := NewIssuedDeletionRequestTokenV1()
	if err != nil {
		t.Fatalf("NewIssuedDeletionRequestTokenV1() error = %v", err)
	}
	if got := fmt.Sprint(token); got != "deletion-request-token-v1(redacted)" {
		t.Fatalf("fmt.Sprint(token) = %q, want redacted value", got)
	}
	if strings.Contains(fmt.Sprint(token), token.Transport()) {
		t.Fatal("formatted token contains the raw token transport")
	}
}

func TestDeletionRequestTokenV1GoSyntaxFormattingIsRedacted(t *testing.T) {
	issued, err := NewIssuedDeletionRequestTokenV1()
	if err != nil {
		t.Fatalf("NewIssuedDeletionRequestTokenV1() error = %v", err)
	}
	transport, err := ParseDeletionRequestTokenTransportV1(issued.Transport())
	if err != nil {
		t.Fatalf("ParseDeletionRequestTokenTransportV1() error = %v", err)
	}

	for _, test := range []struct {
		name      string
		token     any
		transport string
	}{
		{name: "parsed transport", token: transport, transport: transport.Transport()},
		{name: "issued token", token: issued, transport: issued.Transport()},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := fmt.Sprintf("%#v", test.token)
			if got != "deletion-request-token-v1(redacted)" {
				t.Fatalf("fmt.Sprintf(%%#v, token) = %q, want redacted value", got)
			}
			if strings.Contains(got, test.transport) {
				t.Fatal("Go-syntax token formatting contains the raw transport")
			}
		})
	}
}

func TestDeletionRequestTokenV1CannotBeUsedAsPersistedPrimitiveIdentifier(t *testing.T) {
	issued, err := NewIssuedDeletionRequestTokenV1()
	if err != nil {
		t.Fatalf("NewIssuedDeletionRequestTokenV1() error = %v", err)
	}
	transports := []string{
		issued.Transport(),
		"drt1_" + strings.Repeat("c", 43),
	}

	expiresAt := time.Date(2026, time.July, 24, 10, 0, 0, 0, time.UTC)
	for _, transport := range transports {
		t.Run("transport is never a durable primitive identifier", func(t *testing.T) {
			for _, test := range []struct {
				name     string
				validate func() error
				want     error
			}{
				{
					name: "idempotency key",
					validate: func() error {
						return (IdempotencyKey{ProjectID: ProjectIDDefault, OperationKind: OperationKindRecordCreate, Key: transport}).Validate()
					},
					want: ErrInvalidIdempotencyKey,
				},
				{
					name: "outbox subject id",
					validate: func() error {
						return (OutboxEvent{
							ProjectID:   string(ProjectIDDefault),
							EventKind:   OutboxEventKindRecordCreated,
							SubjectKind: OutboxSubjectKindRecord,
							SubjectID:   transport,
						}).Validate()
					},
					want: ErrInvalidOutboxEvent,
				},
				{
					name: "object kind",
					validate: func() error {
						return (ObjectRef{ProjectID: string(ProjectIDDefault), ObjectKind: transport, ObjectID: "rec_01"}).Validate()
					},
					want: ErrInvalidObjectRef,
				},
				{
					name: "object id",
					validate: func() error {
						return (ObjectRef{ProjectID: string(ProjectIDDefault), ObjectKind: "record", ObjectID: transport}).Validate()
					},
					want: ErrInvalidObjectRef,
				},
				{
					name: "owner id",
					validate: func() error {
						return (OwnerLease{OwnerID: transport, Generation: 1, ExpiresAt: expiresAt}).Validate()
					},
					want: ErrInvalidOwnerLease,
				},
				{
					name: "client id",
					validate: func() error {
						return (ClientContentLeaseKeyV1{ProjectID: string(ProjectIDDefault), ClientID: transport}).Validate()
					},
					want: ErrInvalidClientContentKey,
				},
			} {
				t.Run(test.name, func(t *testing.T) {
					err := test.validate()
					if !errors.Is(err, test.want) {
						t.Fatalf("persisted primitive identifier error = %v, want %v", err, test.want)
					}
					if strings.Contains(fmt.Sprint(err), transport) {
						t.Fatal("persisted primitive identifier error contains the raw deletion token transport")
					}
				})
			}
		})
	}
}

func TestDeletionRequestTokenAliasCannotBeUsedAsPersistedPrimitiveIdentifier(t *testing.T) {
	canonical := "drt1_" + strings.Repeat("c", 43)
	alias := canonical[:len(canonical)-1] + "d"
	if _, err := ParseDeletionRequestTokenTransportV1(alias); !errors.Is(err, ErrInvalidDeletionRequestToken) {
		t.Fatalf("ParseDeletionRequestTokenTransportV1(alias) error = %v, want ErrInvalidDeletionRequestToken", err)
	}

	expiresAt := time.Date(2026, time.July, 24, 10, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name     string
		validate func() error
		want     error
	}{
		{
			name: "owner id",
			validate: func() error {
				return (OwnerLease{OwnerID: alias, Generation: 1, ExpiresAt: expiresAt}).Validate()
			},
			want: ErrInvalidOwnerLease,
		},
		{
			name: "client id",
			validate: func() error {
				return (ClientContentLeaseKeyV1{ProjectID: string(ProjectIDDefault), ClientID: alias}).Validate()
			},
			want: ErrInvalidClientContentKey,
		},
		{
			name: "idempotency key",
			validate: func() error {
				return (IdempotencyKey{ProjectID: ProjectIDDefault, OperationKind: OperationKindRecordCreate, Key: alias}).Validate()
			},
			want: ErrInvalidIdempotencyKey,
		},
		{
			name: "outbox subject id",
			validate: func() error {
				return (OutboxEvent{
					ProjectID:   string(ProjectIDDefault),
					EventKind:   OutboxEventKindRecordCreated,
					SubjectKind: OutboxSubjectKindRecord,
					SubjectID:   alias,
				}).Validate()
			},
			want: ErrInvalidOutboxEvent,
		},
		{
			name: "object kind",
			validate: func() error {
				return (ObjectRef{ProjectID: string(ProjectIDDefault), ObjectKind: alias, ObjectID: "rec_01"}).Validate()
			},
			want: ErrInvalidObjectRef,
		},
		{
			name: "object id",
			validate: func() error {
				return (ObjectRef{ProjectID: string(ProjectIDDefault), ObjectKind: "record", ObjectID: alias}).Validate()
			},
			want: ErrInvalidObjectRef,
		},
		{
			name: "mutation kind",
			validate: func() error {
				return (IdentityMutationGuardKeyV1{
					Object:       ObjectRef{ProjectID: string(ProjectIDDefault), ObjectKind: "record", ObjectID: "rec_01"},
					MutationKind: alias,
				}).Validate()
			},
			want: ErrInvalidIdentityMutationGuard,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.validate(); !errors.Is(err, test.want) {
				t.Fatalf("token alias validation error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestDeletionRequestTokenTransportEmbeddedInDurablePrimitiveIdentifierIsRejected(t *testing.T) {
	canonical := "drt1_" + strings.Repeat("c", 42) + "g"
	alias := canonical[:len(canonical)-1] + "h"
	if _, err := ParseDeletionRequestTokenTransportV1(canonical); err != nil {
		t.Fatalf("ParseDeletionRequestTokenTransportV1(canonical) error = %v", err)
	}
	if _, err := ParseDeletionRequestTokenTransportV1(alias); !errors.Is(err, ErrInvalidDeletionRequestToken) {
		t.Fatalf("ParseDeletionRequestTokenTransportV1(alias) error = %v, want ErrInvalidDeletionRequestToken", err)
	}

	expiresAt := time.Date(2026, time.July, 24, 10, 0, 0, 0, time.UTC)
	for _, transport := range []struct {
		name  string
		value string
	}{
		{name: "canonical", value: canonical},
		{name: "noncanonical alias", value: alias},
	} {
		t.Run(transport.name, func(t *testing.T) {
			decoded, err := base64.RawURLEncoding.DecodeString(transport.value[len("drt1_"):])
			if err != nil || len(decoded) != 32 {
				t.Fatalf("test transport must be a decodable 32-byte base64url spelling: bytes=%d error=%v", len(decoded), err)
			}
			embedded := "pre_" + transport.value + "_post"
			for _, test := range []struct {
				name     string
				validate func() error
				want     error
			}{
				{
					name: "owner id",
					validate: func() error {
						return (OwnerLease{OwnerID: embedded, Generation: 1, ExpiresAt: expiresAt}).Validate()
					},
					want: ErrInvalidOwnerLease,
				},
				{
					name: "idempotency key",
					validate: func() error {
						return (IdempotencyKey{ProjectID: ProjectIDDefault, OperationKind: OperationKindRecordCreate, Key: embedded}).Validate()
					},
					want: ErrInvalidIdempotencyKey,
				},
				{
					name: "outbox subject id",
					validate: func() error {
						return (OutboxEvent{
							ProjectID:   string(ProjectIDDefault),
							EventKind:   OutboxEventKindRecordCreated,
							SubjectKind: OutboxSubjectKindRecord,
							SubjectID:   embedded,
						}).Validate()
					},
					want: ErrInvalidOutboxEvent,
				},
				{
					name: "object kind",
					validate: func() error {
						return (ObjectRef{ProjectID: string(ProjectIDDefault), ObjectKind: embedded, ObjectID: "rec_01"}).Validate()
					},
					want: ErrInvalidObjectRef,
				},
				{
					name: "object id",
					validate: func() error {
						return (ObjectRef{ProjectID: string(ProjectIDDefault), ObjectKind: "record", ObjectID: embedded}).Validate()
					},
					want: ErrInvalidObjectRef,
				},
				{
					name: "mutation kind",
					validate: func() error {
						return (IdentityMutationGuardKeyV1{
							Object:       ObjectRef{ProjectID: string(ProjectIDDefault), ObjectKind: "record", ObjectID: "rec_01"},
							MutationKind: embedded,
						}).Validate()
					},
					want: ErrInvalidIdentityMutationGuard,
				},
				{
					name: "client id",
					validate: func() error {
						return (ClientContentLeaseKeyV1{ProjectID: string(ProjectIDDefault), ClientID: embedded}).Validate()
					},
					want: ErrInvalidClientContentKey,
				},
			} {
				t.Run(test.name, func(t *testing.T) {
					err := test.validate()
					if !errors.Is(err, test.want) {
						t.Fatalf("embedded token identifier error = %v, want %v", err, test.want)
					}
					if strings.Contains(fmt.Sprint(err), transport.value) {
						t.Fatal("embedded token identifier error contains the raw deletion token transport")
					}
				})
			}
		})
	}
}

func TestRequestFingerprintV1UsesFixedVersionedLengthPrefixedBody(t *testing.T) {
	input := testRequestFingerprintInputV1()

	body, err := input.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	wantBody := "010000000d7265636f72645f6372656174650000000764656661756c74" +
		"00000020" + strings.Repeat("a1", 32) +
		"00000020" + strings.Repeat("b2", 32) +
		"00000020" + strings.Repeat("c3", 32)
	if got := hex.EncodeToString(body); got != wantBody {
		t.Fatalf("MarshalBinary() = %s, want canonical body", got)
	}

	got, err := FingerprintRequestV1(input)
	if err != nil {
		t.Fatalf("FingerprintRequestV1() error = %v", err)
	}
	persisted, err := got.PersistedBytes()
	if err != nil {
		t.Fatalf("PersistedBytes() error = %v", err)
	}
	if got := hex.EncodeToString(persisted[:]); got != "e2b6baacb4a3ad612f9a0aefdf89d60ea248f24dfeb6a3bece03ddc8379d534d" {
		t.Fatalf("FingerprintRequestV1() = %s, want golden digest", got)
	}
}

func TestRequestFingerprintV1TrustedPersistedReadbackIsReadOnlyAndOpaque(t *testing.T) {
	fingerprint, err := FingerprintRequestV1(testRequestFingerprintInputV1())
	if err != nil {
		t.Fatalf("FingerprintRequestV1() error = %v", err)
	}
	persisted, err := fingerprint.PersistedBytes()
	if err != nil {
		t.Fatalf("PersistedBytes() error = %v", err)
	}

	restored, err := ParseTrustedPersistedRequestFingerprintV1(persisted[:])
	if err != nil {
		t.Fatalf("ParseTrustedPersistedRequestFingerprintV1() error = %v", err)
	}
	if reflect.TypeOf(restored) == reflect.TypeOf(fingerprint) {
		t.Fatal("trusted persisted readback must not issue the canonical write-capable fingerprint type")
	}
	if reflect.TypeOf(restored).ConvertibleTo(reflect.TypeOf(fingerprint)) {
		t.Fatal("trusted persisted readback must not be explicitly convertible to the canonical write-capable fingerprint type")
	}
	if _, writeCapable := any(restored).(interface {
		PersistedBytes() ([32]byte, error)
	}); writeCapable {
		t.Fatal("trusted persisted readback must not expose canonical persistence bytes")
	}
	if err := restored.Validate(); err != nil {
		t.Fatalf("trusted persisted fingerprint Validate() error = %v", err)
	}

	mutated := persisted
	mutated[0] ^= 0xff
	afterMutation, err := fingerprint.PersistedBytes()
	if err != nil {
		t.Fatalf("PersistedBytes() after mutation error = %v", err)
	}
	if afterMutation != persisted {
		t.Fatal("PersistedBytes() exposed mutable fingerprint storage")
	}

	for _, raw := range [][]byte{nil, make([]byte, 31), make([]byte, 33)} {
		if _, err := ParseTrustedPersistedRequestFingerprintV1(raw); !errors.Is(err, ErrInvalidRequestFingerprint) {
			t.Fatalf("ParseTrustedPersistedRequestFingerprintV1(%d bytes) error = %v, want ErrInvalidRequestFingerprint", len(raw), err)
		}
	}
}

func TestParseRequestFingerprintInputV1RoundTripsCanonicalBody(t *testing.T) {
	input := testRequestFingerprintInputV1()
	body, err := input.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	parsed, err := ParseRequestFingerprintInputV1(body)
	if err != nil {
		t.Fatalf("ParseRequestFingerprintInputV1() error = %v", err)
	}
	if parsed != input {
		t.Fatalf("ParseRequestFingerprintInputV1() = %#v, want %#v", parsed, input)
	}
}

func TestParseRequestFingerprintInputV1RejectsMalformedOrNonCanonicalBodies(t *testing.T) {
	body, err := testRequestFingerprintInputV1().MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}

	withMutation := func(mutate func([]byte)) []byte {
		copyBody := append([]byte(nil), body...)
		mutate(copyBody)
		return copyBody
	}
	actorDigestLengthOffset := 1 + 4 + len(OperationKindRecordCreate) + 4 + len(ProjectIDDefault)
	for _, test := range []struct {
		name string
		body []byte
	}{
		{name: "empty", body: nil},
		{name: "unknown version", body: withMutation(func(raw []byte) { raw[0] = 2 })},
		{name: "unknown operation", body: withMutation(func(raw []byte) { raw[5] = 'x' })},
		{name: "unknown project", body: withMutation(func(raw []byte) { raw[1+4+len(OperationKindRecordCreate)+4] = 'x' })},
		{name: "short digest", body: withMutation(func(raw []byte) { binary.BigEndian.PutUint32(raw[actorDigestLengthOffset:], 31) })},
		{name: "trailing byte", body: append(append([]byte(nil), body...), 0)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ParseRequestFingerprintInputV1(test.body); !errors.Is(err, ErrInvalidRequestFingerprint) {
				t.Fatalf("ParseRequestFingerprintInputV1() error = %v, want ErrInvalidRequestFingerprint", err)
			}
		})
	}
}

func TestOwnerLeaseValidationAndLocalSchedulingLiveness(t *testing.T) {
	expiresAt := time.Date(2026, time.July, 24, 10, 0, 0, 0, time.UTC)
	lease := OwnerLease{OwnerID: "worker_01", Generation: 1, ExpiresAt: expiresAt}
	if err := lease.Validate(); err != nil {
		t.Fatalf("OwnerLease.Validate() error = %v", err)
	}

	clock := &testRecordPlatformClock{now: expiresAt.Add(-time.Nanosecond)}
	if !lease.LocallyLive(clock) {
		t.Fatal("OwnerLease.LocallyLive() = false before observed expiry")
	}
	clock.now = expiresAt
	if lease.LocallyLive(clock) {
		t.Fatal("OwnerLease.LocallyLive() = true at observed expiry")
	}

	for _, invalid := range []OwnerLease{
		{Generation: 1, ExpiresAt: expiresAt},
		{OwnerID: "worker_01", ExpiresAt: expiresAt},
		{OwnerID: "worker_01", Generation: 1},
		{OwnerID: "Worker_01", Generation: 1, ExpiresAt: expiresAt},
	} {
		if err := invalid.Validate(); !errors.Is(err, ErrInvalidOwnerLease) {
			t.Fatalf("OwnerLease.Validate(%#v) error = %v, want ErrInvalidOwnerLease", invalid, err)
		}
	}
}

func testRequestFingerprintInputV1() RequestFingerprintInputV1 {
	return RequestFingerprintInputV1{
		Version:            RequestFingerprintVersionV1,
		OperationKind:      OperationKindRecordCreate,
		ProjectID:          ProjectIDDefault,
		ActorScopeDigest:   testRecordPlatformDigest(0xa1),
		RequestScopeDigest: testRecordPlatformDigest(0xb2),
		PayloadDigest:      testRecordPlatformDigest(0xc3),
	}
}

func testRecordPlatformDigest(value byte) [32]byte {
	var digest [32]byte
	for index := range digest {
		digest[index] = value
	}
	return digest
}

type testRecordPlatformClock struct {
	now time.Time
}

func (clock *testRecordPlatformClock) Now() time.Time {
	return clock.now
}
