package evidence

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/idna"
	"golang.org/x/text/unicode/norm"
)

const maxCanonicalStringBytes = 64 * 1024

var (
	secretAssignmentPattern = regexp.MustCompile(`(^|[^a-z0-9])(?:client[_ -]?secret|api[_ -]?key|access[_ -]?key|private[_ -]?key|authorization|password|passwd|secret|token|cookie|command[_ -]?output|stdout|stderr)[[:space:]]*["']?[[:space:]]*[:=][[:space:]]*["']?[[:space:]]*[^[:space:]"']+`)
	privateKeyMarkerPattern = regexp.MustCompile(`-----begin (?:pgp private key block|(?:encrypted |rsa |ec |dsa |openssh )?private key)-----`)
	bearerTokenPattern      = regexp.MustCompile(`(^|[^a-z0-9_-])bearer[[:space:]]+[a-z0-9._~+/-]{8,}($|[^a-z0-9._~+/-])`)
	jwtTokenPattern         = regexp.MustCompile(`(^|[^a-z0-9_-])eyj[a-z0-9_-]{5,}\.[a-z0-9_-]{8,}\.[a-z0-9_-]{8,}($|[^a-z0-9_-])`)
	opaqueTokenPattern      = regexp.MustCompile(`(^|[^a-z0-9_-])(?:gh[pousr]_[a-z0-9]{20,}|sk-[a-z0-9_-]{20,}|akia[a-z0-9]{16})($|[^a-z0-9_-])`)
)

type RedactionMode string

const (
	RedactionNormalOnly               RedactionMode = "normal_only"
	RedactionIncludeSensitiveTopology RedactionMode = "include_sensitive_topology"
	RedactionMaskSensitiveTopology    RedactionMode = "mask_sensitive_topology"
)

type RedactionAction string

const (
	RedactionActionIncluded  RedactionAction = "included"
	RedactionActionStripped  RedactionAction = "stripped"
	RedactionActionMasked    RedactionAction = "masked"
	RedactionActionForbidden RedactionAction = "forbidden"
)

type FieldDecision struct {
	Path        string
	Sensitivity Sensitivity
	Action      RedactionAction
}

type RedactionReport struct {
	Decisions []FieldDecision
}

type fieldSchemaIndex struct {
	definitions map[string]FieldDefinition
	paths       []string
}

type payloadRedactor struct {
	schema    fieldSchemaIndex
	mode      RedactionMode
	decisions map[string]FieldDecision
}

func redactPayload(descriptor Descriptor, payload map[string]any, mode RedactionMode) (map[string]any, RedactionReport, error) {
	if !knownRedactionMode(mode) {
		return nil, RedactionReport{}, fmt.Errorf("%w: redaction mode", ErrInvalidCanonicalPayload)
	}
	if err := validateNoForbiddenFieldNames(payload, ""); err != nil {
		return nil, RedactionReport{}, err
	}
	index := fieldSchemaIndex{definitions: make(map[string]FieldDefinition, len(descriptor.Fields))}
	for _, definition := range descriptor.Fields {
		index.definitions[definition.Path] = definition
		index.paths = append(index.paths, definition.Path)
	}
	sort.Strings(index.paths)
	redactor := payloadRedactor{schema: index, mode: mode, decisions: make(map[string]FieldDecision)}
	value, keep, err := redactor.walk(payload, "")
	if err != nil {
		return nil, RedactionReport{}, err
	}
	if !keep {
		value = map[string]any{}
	}
	redacted, ok := value.(map[string]any)
	if !ok {
		return nil, RedactionReport{}, fmt.Errorf("%w: root object", ErrInvalidCanonicalPayload)
	}
	report := redactor.report()
	for _, decision := range report.Decisions {
		if decision.Action == RedactionActionIncluded {
			return redacted, report, nil
		}
	}
	return nil, RedactionReport{}, fmt.Errorf("%w: content-free payload", ErrInvalidCanonicalPayload)
}

// NormalizeCaptureRedaction converts safe preview display decisions into the
// only dispositions that may be recorded for an immutable captured payload.
// It preserves field identity and sensitivity while ensuring masked and
// permanently forbidden values are represented as stripped at capture time.
func NormalizeCaptureRedaction(descriptor Descriptor, preview []FieldDecision) ([]FieldDecision, error) {
	if err := descriptor.Validate(); err != nil {
		return nil, err
	}
	if len(preview) == 0 {
		return nil, fmt.Errorf("%w: empty preview redaction", ErrInvalidCanonicalPayload)
	}
	definitions := make(map[string]FieldDefinition, len(descriptor.Fields))
	for _, definition := range descriptor.Fields {
		definitions[definition.Path] = definition
	}
	seen := make(map[string]struct{}, len(preview))
	capture := make([]FieldDecision, 0, len(preview))
	for _, decision := range preview {
		definition, exists := definitions[decision.Path]
		if !exists || definition.Sensitivity != decision.Sensitivity {
			return nil, fmt.Errorf("%w: preview redaction field", ErrInvalidCanonicalPayload)
		}
		if _, duplicate := seen[decision.Path]; duplicate {
			return nil, fmt.Errorf("%w: duplicate preview redaction field", ErrInvalidCanonicalPayload)
		}
		seen[decision.Path] = struct{}{}

		captureAction := decision.Action
		switch decision.Sensitivity {
		case SensitivityNormal:
			if decision.Action != RedactionActionIncluded {
				return nil, fmt.Errorf("%w: normal preview disposition", ErrInvalidCanonicalPayload)
			}
		case SensitivitySensitiveTopology:
			switch decision.Action {
			case RedactionActionIncluded, RedactionActionStripped:
			case RedactionActionMasked:
				captureAction = RedactionActionStripped
			default:
				return nil, fmt.Errorf("%w: topology preview disposition", ErrInvalidCanonicalPayload)
			}
		case SensitivityForbidden:
			if decision.Action != RedactionActionForbidden {
				return nil, fmt.Errorf("%w: forbidden preview disposition", ErrInvalidCanonicalPayload)
			}
			captureAction = RedactionActionStripped
		default:
			return nil, fmt.Errorf("%w: preview sensitivity", ErrInvalidCanonicalPayload)
		}
		capture = append(capture, FieldDecision{
			Path:        decision.Path,
			Sensitivity: decision.Sensitivity,
			Action:      captureAction,
		})
	}
	sort.Slice(capture, func(left, right int) bool { return capture[left].Path < capture[right].Path })
	return capture, nil
}

func validateNoForbiddenFieldNames(value any, path string) error {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			childPath := key
			if path != "" {
				childPath = path + "." + key
			}
			if forbiddenFieldPath(childPath) {
				return fmt.Errorf("%w: %s", ErrForbiddenField, childPath)
			}
			if err := validateNoForbiddenFieldNames(item, childPath); err != nil {
				return err
			}
		}
	case []any:
		for _, item := range typed {
			if err := validateNoForbiddenFieldNames(item, path); err != nil {
				return err
			}
		}
	}
	return nil
}

func (redactor *payloadRedactor) walk(value any, path string) (any, bool, error) {
	switch typed := value.(type) {
	case map[string]any:
		if path != "" && !redactor.schema.hasDescendant(path) {
			return nil, false, fmt.Errorf("%w: %s", ErrFieldNotAllowed, path)
		}
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		output := make(map[string]any, len(typed))
		for _, key := range keys {
			childPath := key
			if path != "" {
				childPath = path + "." + key
			}
			if forbiddenFieldPath(childPath) {
				return nil, false, fmt.Errorf("%w: %s", ErrForbiddenField, childPath)
			}
			child, keep, err := redactor.walk(typed[key], childPath)
			if err != nil {
				return nil, false, err
			}
			if keep {
				output[key] = child
			}
		}
		return output, true, nil
	case []any:
		definition, exact := redactor.schema.definitions[path]
		if !exact && !redactor.schema.hasDescendant(path) {
			return nil, false, fmt.Errorf("%w: %s", ErrFieldNotAllowed, path)
		}
		if exact && definition.Sensitivity != SensitivityNormal {
			return redactor.applyDefinition(definition, typed)
		}
		if exact {
			redactor.record(definition, RedactionActionIncluded)
		}
		output := make([]any, 0, len(typed))
		for _, item := range typed {
			child, keep, err := redactor.walk(item, path)
			if err != nil {
				return nil, false, err
			}
			if keep {
				output = append(output, child)
			}
		}
		return output, true, nil
	default:
		definition, exists := redactor.schema.definitions[path]
		if !exists {
			return nil, false, fmt.Errorf("%w: %s", ErrFieldNotAllowed, path)
		}
		return redactor.applyDefinition(definition, typed)
	}
}

func (redactor *payloadRedactor) applyDefinition(definition FieldDefinition, value any) (any, bool, error) {
	switch definition.Sensitivity {
	case SensitivityForbidden:
		redactor.record(definition, RedactionActionForbidden)
		return nil, false, fmt.Errorf("%w: %s", ErrForbiddenField, definition.Path)
	case SensitivitySensitiveTopology:
		switch redactor.mode {
		case RedactionNormalOnly:
			redactor.record(definition, RedactionActionStripped)
			return nil, false, nil
		case RedactionMaskSensitiveTopology:
			redactor.record(definition, RedactionActionMasked)
			return maskSensitiveValue(value), true, nil
		case RedactionIncludeSensitiveTopology:
			// Continue through format normalization and content checks below.
		}
	case SensitivityNormal:
	default:
		return nil, false, fmt.Errorf("%w: field sensitivity", ErrInvalidKindDescriptor)
	}

	if items, ok := value.([]any); ok {
		normalized := make([]any, len(items))
		for index, item := range items {
			var err error
			normalized[index], err = normalizeFieldValue(definition, item)
			if err != nil {
				return nil, false, err
			}
		}
		redactor.record(definition, RedactionActionIncluded)
		return normalized, true, nil
	}
	normalized, err := normalizeFieldValue(definition, value)
	if err != nil {
		return nil, false, err
	}
	redactor.record(definition, RedactionActionIncluded)
	return normalized, true, nil
}

func normalizeFieldValue(definition FieldDefinition, value any) (any, error) {
	if definition.Format == FieldFormatURL {
		stringValue, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("%w: URL field %s", ErrInvalidCanonicalPayload, definition.Path)
		}
		normalized, err := normalizeEvidenceURL(stringValue)
		if err != nil {
			return nil, fmt.Errorf("%w: URL field %s", ErrInvalidCanonicalPayload, definition.Path)
		}
		value = normalized
	}
	if stringValue, ok := value.(string); ok {
		if !utf8.ValidString(stringValue) || len(stringValue) > maxCanonicalStringBytes {
			return nil, fmt.Errorf("%w: string field %s", ErrInvalidCanonicalPayload, definition.Path)
		}
		if forbiddenStringContent(stringValue) {
			return nil, fmt.Errorf("%w: secret content in %s", ErrForbiddenField, definition.Path)
		}
	}
	if err := walkSafeStructuredValue(value, definition.Path); err != nil {
		return nil, fmt.Errorf("%w: unsafe content in %s", err, definition.Path)
	}
	return value, nil
}

func normalizeEvidenceURL(value string) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Opaque != "" {
		return "", ErrInvalidCanonicalPayload
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	switch parsed.Scheme {
	case "http", "https", "ssh", "tcp", "tls":
	default:
		return "", ErrInvalidCanonicalPayload
	}
	hostname := parsed.Hostname()
	if hostname == "" || strings.Contains(hostname, "%") {
		return "", ErrInvalidCanonicalPayload
	}
	normalizedHost := ""
	if address := net.ParseIP(hostname); address != nil {
		normalizedHost = address.String()
	} else {
		asciiHost, asciiErr := idna.Lookup.ToASCII(hostname)
		if asciiErr != nil || asciiHost == "" {
			return "", ErrInvalidCanonicalPayload
		}
		normalizedHost = strings.ToLower(asciiHost)
	}
	port := parsed.Port()
	if port != "" {
		portNumber, portErr := strconv.ParseUint(port, 10, 16)
		if portErr != nil || portNumber == 0 {
			return "", ErrInvalidCanonicalPayload
		}
		port = strconv.FormatUint(portNumber, 10)
		if defaultEvidenceURLPort(parsed.Scheme, port) {
			port = ""
		}
	}
	if port != "" {
		parsed.Host = net.JoinHostPort(normalizedHost, port)
	} else if strings.Contains(normalizedHost, ":") {
		parsed.Host = "[" + normalizedHost + "]"
	} else {
		parsed.Host = normalizedHost
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	parsed.RawFragment = ""
	return parsed.String(), nil
}

func defaultEvidenceURLPort(scheme, port string) bool {
	switch scheme {
	case "http":
		return port == "80"
	case "https", "tls":
		return port == "443"
	case "ssh":
		return port == "22"
	default:
		return false
	}
}

func (index fieldSchemaIndex) hasDescendant(path string) bool {
	prefix := path + "."
	for _, candidate := range index.paths {
		if strings.HasPrefix(candidate, prefix) {
			return true
		}
	}
	return false
}

func (redactor *payloadRedactor) record(definition FieldDefinition, action RedactionAction) {
	redactor.decisions[definition.Path] = FieldDecision{
		Path:        definition.Path,
		Sensitivity: definition.Sensitivity,
		Action:      action,
	}
}

func (redactor *payloadRedactor) report() RedactionReport {
	paths := make([]string, 0, len(redactor.decisions))
	for path := range redactor.decisions {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	report := RedactionReport{Decisions: make([]FieldDecision, 0, len(paths))}
	for _, path := range paths {
		report.Decisions = append(report.Decisions, redactor.decisions[path])
	}
	return report
}

func forbiddenFieldPath(path string) bool {
	segments := strings.Split(path, ".")
	normalizedSegments := make([]string, 0, len(segments))
	for _, segment := range segments {
		normalized := normalizeIdentifier(segment)
		normalizedSegments = append(normalizedSegments, normalized)
		if forbiddenIdentifier(normalized) {
			return true
		}
	}
	return forbiddenIdentifier(strings.Join(normalizedSegments, "_"))
}

func forbiddenIdentifier(normalized string) bool {
	tokens := strings.FieldsFunc(normalized, func(character rune) bool { return character == '_' })
	for start := range tokens {
		for end := start + 1; end <= len(tokens); end++ {
			if forbiddenIdentifierConcept(strings.Join(tokens[start:end], "_")) {
				return true
			}
		}
	}
	return false
}

func forbiddenIdentifierConcept(candidate string) bool {
	switch candidate {
	case "token", "password", "passwd", "secret", "key", "authorization", "authorization_header",
		"cookie", "set_cookie", "header", "headers", "request_headers", "response_headers",
		"env", "environment", "mount", "mounts", "container_id", "fingerprint", "raw",
		"raw_json", "json", "stdout", "stderr", "output", "details", "userinfo", "user_info", "query",
		"query_params", "fragment":
		return true
	default:
		return false
	}
}

func forbiddenStringContent(value string) bool {
	normalized := strings.ToLower(norm.NFKC.String(value))
	return secretAssignmentPattern.MatchString(normalized) ||
		bearerTokenPattern.MatchString(normalized) ||
		privateKeyMarkerPattern.MatchString(normalized) ||
		jwtTokenPattern.MatchString(normalized) ||
		opaqueTokenPattern.MatchString(normalized)
}

func normalizeIdentifier(value string) string {
	value = norm.NFKC.String(value)
	runes := []rune(value)
	var builder strings.Builder
	lastUnderscore := false
	for index, character := range runes {
		if unicode.IsUpper(character) {
			previousIsLowerOrDigit := index > 0 && (unicode.IsLower(runes[index-1]) || unicode.IsDigit(runes[index-1]))
			acronymBoundary := index > 0 && index+1 < len(runes) && unicode.IsUpper(runes[index-1]) && unicode.IsLower(runes[index+1])
			if (previousIsLowerOrDigit || acronymBoundary) && builder.Len() > 0 && !lastUnderscore {
				builder.WriteByte('_')
			}
			builder.WriteRune(unicode.ToLower(character))
			lastUnderscore = false
			continue
		}
		if character == '-' || character == ' ' {
			if !lastUnderscore {
				builder.WriteByte('_')
			}
			lastUnderscore = true
			continue
		}
		builder.WriteRune(unicode.ToLower(character))
		lastUnderscore = character == '_'
	}
	return builder.String()
}

func maskSensitiveValue(value any) any {
	items, ok := value.([]any)
	if !ok {
		return "[redacted]"
	}
	masked := make([]any, len(items))
	for index := range items {
		masked[index] = "[redacted]"
	}
	return masked
}

func knownRedactionMode(mode RedactionMode) bool {
	switch mode {
	case RedactionNormalOnly, RedactionIncludeSensitiveTopology, RedactionMaskSensitiveTopology:
		return true
	default:
		return false
	}
}
