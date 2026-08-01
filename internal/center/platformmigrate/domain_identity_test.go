package platformmigrate

import (
	"bytes"
	"strings"
	"testing"
)

func TestDomainKindValidateAcceptsLocalPostgresDomains(t *testing.T) {
	for _, kind := range []DomainKind{
		DomainKindApplication,
		DomainKindDeletionLedger,
		DomainKindDeletionWitness,
		DomainKindRecoveryControl,
	} {
		if err := kind.Validate(); err != nil {
			t.Fatalf("DomainKind(%q).Validate() error = %v", kind, err)
		}
	}
}

func TestDomainKindValidateRejectsUnknownDomain(t *testing.T) {
	err := DomainKind("shared_ledger").Validate()
	if err == nil {
		t.Fatal("DomainKind.Validate() error = nil, want unknown-domain rejection")
	}
	if !strings.Contains(err.Error(), "domain kind") {
		t.Fatalf("DomainKind.Validate() error = %q, want domain-kind context", err)
	}
}

func TestNewDomainIDUsesExactlyThirtyTwoRandomBytes(t *testing.T) {
	got, err := newDomainID(bytes.NewReader(bytes.Repeat([]byte{0xab}, 32)))
	if err != nil {
		t.Fatalf("newDomainID() error = %v", err)
	}
	if want := "rd-" + strings.Repeat("ab", 32); got != want {
		t.Fatalf("newDomainID() = %q, want %q", got, want)
	}
}

func TestNewDomainIDRejectsInsufficientRandomness(t *testing.T) {
	_, err := newDomainID(bytes.NewReader(bytes.Repeat([]byte{0xab}, 31)))
	if err == nil {
		t.Fatal("newDomainID() error = nil, want short-randomness failure")
	}
}
