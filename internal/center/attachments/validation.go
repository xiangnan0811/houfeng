package attachments

import "fmt"

func ValidateAttachmentID(value string) error {
	if !validPrefixedID(value, "att_") {
		return ErrInvalidAttachmentID
	}
	return nil
}

func ValidateUploadID(value string) error {
	if !validPrefixedID(value, "aup_") {
		return ErrInvalidUploadID
	}
	return nil
}

func ValidateProcessorJobID(value string) error {
	if !validPrefixedID(value, "apj_") {
		return ErrInvalidProcessorJobID
	}
	return nil
}

func ValidateWorkspaceID(value string) error {
	if !validPrefixedID(value, "cpw_") {
		return ErrInvalidWorkspaceID
	}
	return nil
}

func ValidateBlobGCPinID(value string) error {
	if !validPrefixedID(value, "bgp_") {
		return ErrInvalidBlobGCPinID
	}
	return nil
}

func NormalizeAttachmentReferences(values []AttachmentReference) ([]AttachmentReference, error) {
	normalized := make([]AttachmentReference, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if err := ValidateAttachmentID(value.AttachmentID); err != nil {
			return nil, fmt.Errorf("%w: attachment id", ErrInvalidAttachmentReferences)
		}
		if _, exists := seen[value.AttachmentID]; exists {
			return nil, fmt.Errorf("%w: duplicate attachment id", ErrInvalidAttachmentReferences)
		}
		seen[value.AttachmentID] = struct{}{}
		normalized = append(normalized, value)
	}
	return normalized, nil
}

func validPrefixedID(value, prefix string) bool {
	if len(value) < len(prefix)+1 || len(value) > len(prefix)+64 || value[:len(prefix)] != prefix {
		return false
	}
	for _, character := range value[len(prefix):] {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return false
		}
	}
	return true
}
