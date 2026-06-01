package assetlinks

import (
	"errors"
	"testing"
)

func TestNormalizeAndValidateLinkInput(t *testing.T) {
	input := NormalizeLinkInput(LinkInput{MonitoringInstanceID: " mi_001 ", Note: " primary "})
	if input.MonitoringInstanceID != "mi_001" || input.Note != "primary" {
		t.Fatalf("NormalizeLinkInput() = %#v, want trimmed fields", input)
	}
	if err := ValidateLinkInput(input); err != nil {
		t.Fatalf("ValidateLinkInput() error = %v, want nil", err)
	}
	if err := ValidateLinkInput(NormalizeLinkInput(LinkInput{MonitoringInstanceID: " "})); !errors.Is(err, ErrInvalidVPSMonitoringInstanceLinkInput) {
		t.Fatalf("ValidateLinkInput(blank) error = %v, want ErrInvalidVPSMonitoringInstanceLinkInput", err)
	}
}

func TestNormalizeAndValidateUnlinkInput(t *testing.T) {
	input := NormalizeUnlinkInput(UnlinkInput{MonitoringInstanceID: " mi_001 ", Note: " rotated "})
	if input.MonitoringInstanceID != "mi_001" || input.Note != "rotated" {
		t.Fatalf("NormalizeUnlinkInput() = %#v, want trimmed fields", input)
	}
	if err := ValidateUnlinkInput(input); err != nil {
		t.Fatalf("ValidateUnlinkInput() error = %v, want nil", err)
	}
	if err := ValidateUnlinkInput(NormalizeUnlinkInput(UnlinkInput{MonitoringInstanceID: " "})); !errors.Is(err, ErrInvalidVPSMonitoringInstanceLinkInput) {
		t.Fatalf("ValidateUnlinkInput(blank) error = %v, want ErrInvalidVPSMonitoringInstanceLinkInput", err)
	}
}
