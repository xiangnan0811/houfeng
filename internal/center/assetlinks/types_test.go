package assetlinks

import (
	"errors"
	"testing"
)

func TestNormalizeAndValidateLinkInput(t *testing.T) {
	input := NormalizeLinkInput(LinkInput{NodeID: " nd_001 ", Note: " primary "})
	if input.NodeID != "nd_001" || input.Note != "primary" {
		t.Fatalf("NormalizeLinkInput() = %#v, want trimmed fields", input)
	}
	if err := ValidateLinkInput(input); err != nil {
		t.Fatalf("ValidateLinkInput() error = %v, want nil", err)
	}
	if err := ValidateLinkInput(NormalizeLinkInput(LinkInput{NodeID: " "})); !errors.Is(err, ErrInvalidVPSNodeLinkInput) {
		t.Fatalf("ValidateLinkInput(blank) error = %v, want ErrInvalidVPSNodeLinkInput", err)
	}
}

func TestNormalizeAndValidateUnlinkInput(t *testing.T) {
	input := NormalizeUnlinkInput(UnlinkInput{NodeID: " nd_001 ", Note: " rotated "})
	if input.NodeID != "nd_001" || input.Note != "rotated" {
		t.Fatalf("NormalizeUnlinkInput() = %#v, want trimmed fields", input)
	}
	if err := ValidateUnlinkInput(input); err != nil {
		t.Fatalf("ValidateUnlinkInput() error = %v, want nil", err)
	}
	if err := ValidateUnlinkInput(NormalizeUnlinkInput(UnlinkInput{NodeID: " "})); !errors.Is(err, ErrInvalidVPSNodeLinkInput) {
		t.Fatalf("ValidateUnlinkInput(blank) error = %v, want ErrInvalidVPSNodeLinkInput", err)
	}
}
