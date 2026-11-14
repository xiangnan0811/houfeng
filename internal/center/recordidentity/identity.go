// Package recordidentity defines the syntax and domains of public record IDs.
// A domain identifies syntax only; it does not prove allocation or authority.
package recordidentity

// Domain identifies the syntactic domain of a public record ID.
type Domain uint8

const (
	// Invalid identifies an ID outside both record grammars.
	Invalid Domain = iota
	// Legacy identifies a rec_ ID from the historical namespace.
	Legacy
	// Ordinary identifies the rec2_ namespace reserved for ordinary births.
	Ordinary
	// Certifiable identifies the rec2_ namespace eligible for birth certification.
	// The ID alone does not establish certification.
	Certifiable
)

// Classify returns the ID's syntactic domain, or Invalid for malformed IDs.
// Certifiable describes the ID namespace, not a verified birth or permission.
func Classify(value string) Domain {
	if len(value) >= 5 && len(value) <= 68 && value[:4] == "rec_" {
		for index := 4; index < len(value); index++ {
			character := value[index]
			if (character < 'a' || character > 'z') && (character < '0' || character > '9') {
				return Invalid
			}
		}
		return Legacy
	}
	if len(value) != 37 || value[:5] != "rec2_" {
		return Invalid
	}
	for index := 5; index < len(value); index++ {
		character := value[index]
		if (character < 'a' || character > 'f') && (character < '0' || character > '9') {
			return Invalid
		}
	}
	if value[5] <= '7' {
		return Ordinary
	}
	return Certifiable
}

// IsValid reports whether value is a syntactically valid public record ID.
func IsValid(value string) bool {
	return Classify(value) != Invalid
}
