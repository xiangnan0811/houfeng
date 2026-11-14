package sourceretirement

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"houfeng/internal/center/recordauth"
)

func TestTerminalCanonicalRoundTrip(t *testing.T) {
	for _, source := range []struct {
		kind recordauth.SourceKind
		id   string
	}{
		{recordauth.SourceKindVPS, "vps_0123456789abcdef"},
		{recordauth.SourceKindMonitoringInstance, "mi_0123456789abcdef"},
		{recordauth.SourceKindTarget, "tg_0123456789abcdef"},
	} {
		for _, shape := range []string{"project", "restricted", "deny all", "project to restricted"} {
			t.Run(string(source.kind)+"/"+shape, func(t *testing.T) {
				value := testTerminal(t)
				value.Source.Kind, value.Source.ID = source.kind, source.id
				switch shape {
				case "project":
					value.LastLive = testScope(t, recordauth.VisibilityKindProject, nil, nil)
					value.FinalFloor = value.LastLive
				case "deny all":
					value.FinalFloor = testScope(t, recordauth.VisibilityKindRestricted, nil, nil)
				case "project to restricted":
					value.LastLive = testScope(t, recordauth.VisibilityKindProject, nil, nil)
				}
				canonical, err := EncodeTerminal(value)
				if err != nil {
					t.Fatalf("EncodeTerminal() error = %v", err)
				}
				want, _ := testTerminalWire(value, nil)
				if !bytes.Equal(canonical, want) {
					t.Fatalf("wire bytes differ from fixed-order big-endian encoding: got %x, want %x", canonical, want)
				}
				decoded, err := DecodeTerminal(canonical)
				if err != nil {
					t.Fatalf("DecodeTerminal() error = %v", err)
				}
				reencoded, err := EncodeTerminal(decoded)
				if err != nil || !bytes.Equal(canonical, reencoded) {
					t.Fatalf("roundtrip encoding differs: %x, error = %v", reencoded, err)
				}
				hash, err := TerminalHash(value)
				if err != nil || hash != sha256.Sum256(want) {
					t.Fatalf("TerminalHash() = %x, %v", hash, err)
				}
				if err := VerifyTerminal(canonical, decoded, hash); err != nil {
					t.Fatalf("VerifyTerminal() error = %v", err)
				}
				// Neither decoder nor encoder may retain mutable caller bytes.
				clear(canonical)
				if err := VerifyTerminal(want, decoded, hash); err != nil {
					t.Fatalf("input mutation changed decoded evidence: %v", err)
				}
			})
		}
	}
}

func TestTerminalFirstEntryAndIntegerBoundaries(t *testing.T) {
	value := testTerminal(t)
	value.Sequence, value.PreviousHash = 1, [32]byte{}
	value.Domain.SystemIdentifier = "18446744073709551615"
	value.Domain.DatabaseOID = math.MaxUint32
	value.Domain.Epoch, value.OriginGeneration = math.MaxUint64, math.MaxUint64
	value.Domain.DatabaseName = "a" + strings.Repeat("_", 62)
	value.RetirementID = strings.Repeat("a", 128)
	canonical, err := EncodeTerminal(value)
	if err != nil {
		t.Fatalf("EncodeTerminal(boundaries) error = %v", err)
	}
	if err := VerifyTerminal(canonical, value, sha256.Sum256(canonical)); err != nil {
		t.Fatalf("VerifyTerminal(boundaries) error = %v", err)
	}
}

func TestTerminalRejectsEveryProjectionMismatch(t *testing.T) {
	base := testTerminal(t)
	canonical, err := EncodeTerminal(base)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(canonical)
	mutations := map[string]func(*Terminal){
		"domain ID":          func(v *Terminal) { v.Domain.ID = "rd-" + strings.Repeat("b", 64) },
		"system identifier":  func(v *Terminal) { v.Domain.SystemIdentifier = "12346" },
		"database OID":       func(v *Terminal) { v.Domain.DatabaseOID++ },
		"database name":      func(v *Terminal) { v.Domain.DatabaseName = "other_db" },
		"domain epoch":       func(v *Terminal) { v.Domain.Epoch++ },
		"deployment":         func(v *Terminal) { v.Source.DeploymentID = "dp-" + strings.Repeat("b", 64) },
		"project":            func(v *Terminal) { v.Source.ProjectID = "other" },
		"kind":               func(v *Terminal) { v.Source.Kind = recordauth.SourceKindTarget; v.Source.ID = "tg_0123456789abcdef" },
		"source ID":          func(v *Terminal) { v.Source.ID = "vps_1123456789abcdef" },
		"retirement ID":      func(v *Terminal) { v.RetirementID = "retirement_2" },
		"origin generation":  func(v *Terminal) { v.OriginGeneration++ },
		"barrier digest":     func(v *Terminal) { v.BarrierDigest[0]++ },
		"last-live scope":    func(v *Terminal) { v.LastLive = testScope(t, recordauth.VisibilityKindProject, nil, nil) },
		"floor scope":        func(v *Terminal) { v.FinalFloor = testScope(t, recordauth.VisibilityKindRestricted, nil, nil) },
		"last-live revision": func(v *Terminal) { v.LastLive.PolicyRevision++; v.LastLive = testNormalizeScope(t, v.LastLive) },
		"floor revision":     func(v *Terminal) { v.FinalFloor.PolicyRevision++; v.FinalFloor = testNormalizeScope(t, v.FinalFloor) },
		"last-live hash":     func(v *Terminal) { v.LastLive.CanonicalHash[0]++ },
		"floor hash":         func(v *Terminal) { v.FinalFloor.CanonicalHash[0]++ },
		"last-live roles":    func(v *Terminal) { v.LastLive.AllowedRoles = nil; v.LastLive = testNormalizeScope(t, v.LastLive) },
		"last-live groups": func(v *Terminal) {
			v.LastLive.AllowedGroupIDs = []string{"rag_alpha"}
			v.LastLive = testNormalizeScope(t, v.LastLive)
		},
		"floor roles": func(v *Terminal) {
			v.FinalFloor.AllowedRoles = []recordauth.Role{recordauth.RoleViewer}
			v.FinalFloor = testNormalizeScope(t, v.FinalFloor)
		},
		"floor groups": func(v *Terminal) {
			v.FinalFloor.AllowedGroupIDs = []string{"rag_beta"}
			v.FinalFloor = testNormalizeScope(t, v.FinalFloor)
		},
		"last-live project":        func(v *Terminal) { v.LastLive.ProjectID = "other" },
		"last-live policy version": func(v *Terminal) { v.LastLive.PolicyVersion++ },
		"last-live version":        func(v *Terminal) { v.LastLive.Version++ },
		"floor kind":               func(v *Terminal) { v.FinalFloor.Kind = recordauth.VisibilityKindProject },
		"floor project":            func(v *Terminal) { v.FinalFloor.ProjectID = "other" },
		"floor policy version":     func(v *Terminal) { v.FinalFloor.PolicyVersion++ },
		"floor version":            func(v *Terminal) { v.FinalFloor.Version++ },
		"sequence":                 func(v *Terminal) { v.Sequence++ },
		"previous hash":            func(v *Terminal) { v.PreviousHash[0]++ },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			projected := testTerminal(t)
			mutate(&projected)
			if err := VerifyTerminal(canonical, projected, hash); !errors.Is(err, ErrInvalidTerminal) {
				t.Fatalf("VerifyTerminal(mismatched projection) error = %v", err)
			}
			// For semantically valid changes, a substituted encoded identity
			// must fail even if its attacker-supplied hash is self-consistent.
			if changed, err := EncodeTerminal(projected); err == nil {
				if err := VerifyTerminal(changed, base, sha256.Sum256(changed)); !errors.Is(err, ErrInvalidTerminal) {
					t.Fatalf("VerifyTerminal(substituted canonical identity) error = %v", err)
				}
				if err := VerifyTerminal(changed, projected, hash); !errors.Is(err, ErrInvalidTerminal) {
					t.Fatalf("VerifyTerminal(stale hash) error = %v", err)
				}
			}
		})
	}
	for index := range hash {
		wrong := hash
		wrong[index] ^= 1
		if err := VerifyTerminal(canonical, base, wrong); !errors.Is(err, ErrInvalidTerminal) {
			t.Fatalf("VerifyTerminal(hash byte %d) error = %v", index, err)
		}
	}
}

func TestTerminalRejectsInvalidFieldsAndMutatedScopes(t *testing.T) {
	mutations := map[string]func(*Terminal){
		"domain prefix":                func(v *Terminal) { v.Domain.ID = "dp-" + strings.Repeat("a", 64) },
		"domain uppercase":             func(v *Terminal) { v.Domain.ID = "rd-" + strings.Repeat("A", 64) },
		"domain short":                 func(v *Terminal) { v.Domain.ID = "rd-a" },
		"deployment prefix":            func(v *Terminal) { v.Source.DeploymentID = v.Domain.ID },
		"deployment nonhex":            func(v *Terminal) { v.Source.DeploymentID = "dp-" + strings.Repeat("g", 64) },
		"system zero":                  func(v *Terminal) { v.Domain.SystemIdentifier = "0" },
		"system leading zero":          func(v *Terminal) { v.Domain.SystemIdentifier = "0123" },
		"system overflow":              func(v *Terminal) { v.Domain.SystemIdentifier = "18446744073709551616" },
		"system sign":                  func(v *Terminal) { v.Domain.SystemIdentifier = "+123" },
		"system invalid UTF8":          func(v *Terminal) { v.Domain.SystemIdentifier = "1\xff" },
		"empty database":               func(v *Terminal) { v.Domain.DatabaseName = "" },
		"database uppercase":           func(v *Terminal) { v.Domain.DatabaseName = "Records" },
		"database leading digit":       func(v *Terminal) { v.Domain.DatabaseName = "1records" },
		"database oversized":           func(v *Terminal) { v.Domain.DatabaseName = strings.Repeat("a", 64) },
		"database punctuation":         func(v *Terminal) { v.Domain.DatabaseName = "records-db" },
		"zero OID":                     func(v *Terminal) { v.Domain.DatabaseOID = 0 },
		"zero epoch":                   func(v *Terminal) { v.Domain.Epoch = 0 },
		"zero generation":              func(v *Terminal) { v.OriginGeneration = 0 },
		"zero sequence":                func(v *Terminal) { v.Sequence = 0 },
		"unknown project":              func(v *Terminal) { v.Source.ProjectID = "DEFAULT" },
		"unknown kind":                 func(v *Terminal) { v.Source.Kind = "unknown" },
		"cross-kind source ID":         func(v *Terminal) { v.Source.ID = "tg_0123456789abcdef" },
		"opaque nonregistry source ID": func(v *Terminal) { v.Source.ID = "otherwise_valid_opaque_id" },
		"source uppercase":             func(v *Terminal) { v.Source.ID = "vps_0123456789abcdeF" },
		"source short":                 func(v *Terminal) { v.Source.ID = "vps_0123" },
		"empty retirement":             func(v *Terminal) { v.RetirementID = "" },
		"retirement punctuation":       func(v *Terminal) { v.RetirementID = "retirement.1" },
		"retirement oversized":         func(v *Terminal) { v.RetirementID = strings.Repeat("a", 129) },
		"retirement invalid UTF8":      func(v *Terminal) { v.RetirementID = "retirement_\xff" },
		"zero barrier":                 func(v *Terminal) { v.BarrierDigest = [32]byte{} },
		"missing predecessor":          func(v *Terminal) { v.PreviousHash = [32]byte{} },
		"first entry predecessor":      func(v *Terminal) { v.Sequence = 1 },
		"floor widens to project":      func(v *Terminal) { v.FinalFloor = testScope(t, recordauth.VisibilityKindProject, nil, nil) },
		"floor widens groups": func(v *Terminal) {
			v.FinalFloor = testScope(t, recordauth.VisibilityKindRestricted, nil, []string{"rag_other"})
		},
		"last-live version":      func(v *Terminal) { v.LastLive.Version++ },
		"floor project":          func(v *Terminal) { v.FinalFloor.ProjectID = "other" },
		"floor policy version":   func(v *Terminal) { v.FinalFloor.PolicyVersion++ },
		"floor policy revision":  func(v *Terminal) { v.FinalFloor.PolicyRevision = 0 },
		"last-live stale grants": func(v *Terminal) { v.LastLive.AllowedGroupIDs[0] = "rag_other" },
		"last-live unsorted grants": func(v *Terminal) {
			v.LastLive.AllowedGroupIDs[0], v.LastLive.AllowedGroupIDs[1] = v.LastLive.AllowedGroupIDs[1], v.LastLive.AllowedGroupIDs[0]
		},
		"last-live duplicate grants": func(v *Terminal) { v.LastLive.AllowedGroupIDs = append(v.LastLive.AllowedGroupIDs, "rag_beta") },
		"floor stale hash":           func(v *Terminal) { v.FinalFloor.CanonicalHash = [32]byte{} },
		"project ignored grants":     func(v *Terminal) { v.LastLive.Kind = recordauth.VisibilityKindProject },
		"oversized scope": func(v *Terminal) {
			groups := make([]string, 80)
			for index := range groups {
				groups[index] = "rag_" + strings.Repeat("a", 58) + fmt.Sprintf("%04d", index)
			}
			v.LastLive = testScope(t, recordauth.VisibilityKindRestricted, nil, groups)
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			value := testTerminal(t)
			original, _ := testTerminalWire(value, nil)
			mutate(&value)
			if err := VerifyTerminal(original, value, sha256.Sum256(original)); !errors.Is(err, ErrInvalidTerminal) {
				t.Fatalf("VerifyTerminal(invalid projection) error = %v", err)
			}
			if _, err := EncodeTerminal(value); !errors.Is(err, ErrInvalidTerminal) {
				t.Fatalf("EncodeTerminal() error = %v", err)
			}
			if hash, err := TerminalHash(value); !errors.Is(err, ErrInvalidTerminal) || hash != ([32]byte{}) {
				t.Fatalf("TerminalHash() = %x, %v", hash, err)
			}
			// Serialize public fields independently so decoder validation is
			// not hidden behind the encoder's own rejection. Hash-only scope
			// mutations have no wire representation and are covered above.
			if strings.Contains(name, "hash") || name == "last-live stale grants" || name == "last-live unsorted grants" || name == "last-live duplicate grants" {
				return
			}
			wire, _ := testTerminalWire(value, nil)
			if _, err := DecodeTerminal(wire); !errors.Is(err, ErrInvalidTerminal) {
				t.Fatalf("DecodeTerminal(invalid wire) error = %v", err)
			}
		})
	}
}

func TestTerminalRejectsMalformedWire(t *testing.T) {
	value := testTerminal(t)
	wire, spans := testTerminalWire(value, nil)
	for length := range len(wire) {
		if _, err := DecodeTerminal(wire[:length]); !errors.Is(err, ErrInvalidTerminal) {
			t.Fatalf("DecodeTerminal(truncated at %d) error = %v", length, err)
		}
	}
	malformed := map[string][]byte{
		"trailing byte":      append(bytes.Clone(wire), 0),
		"duplicate entry":    append(bytes.Clone(wire), wire...),
		"oversized terminal": make([]byte, 65537),
	}
	for _, field := range []string{"tag", "domain", "system", "database", "deployment", "project", "kind", "source", "retirement", "barrier", "last-live", "floor", "previous"} {
		span := spans[field]
		overflow := bytes.Clone(wire)
		binary.BigEndian.PutUint32(overflow[span.start:span.start+4], math.MaxUint32)
		malformed[field+" overflowing length"] = overflow
		oversized := bytes.Clone(wire)
		limit := uint32(129)
		switch field {
		case "domain", "deployment":
			limit = 68
		case "last-live", "floor":
			limit = 4097
		case "barrier", "previous":
			limit = 33
		}
		binary.BigEndian.PutUint32(oversized[span.start:span.start+4], limit)
		malformed[field+" oversized length"] = oversized
	}
	for _, field := range []string{"barrier", "previous"} {
		replacement, _ := testTerminalWire(value, map[string][]byte{field: make([]byte, 31)})
		malformed[field+" short digest"] = replacement
	}
	unknownVersion := bytes.Clone(wire)
	binary.BigEndian.PutUint32(unknownVersion[spans["version"].start:], 3)
	malformed["unknown version"] = unknownVersion
	unknownTag := bytes.Clone(wire)
	unknownTag[spans["tag"].start+4] = 'X'
	malformed["unknown domain tag"] = unknownTag
	invalidUTF8, _ := testTerminalWire(value, map[string][]byte{"retirement": {'r', 0xff}})
	malformed["invalid UTF8"] = invalidUTF8
	for name, raw := range malformed {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeTerminal(raw); !errors.Is(err, ErrInvalidTerminal) {
				t.Fatalf("DecodeTerminal() error = %v", err)
			}
			if err := VerifyTerminal(raw, value, sha256.Sum256(raw)); !errors.Is(err, ErrInvalidTerminal) {
				t.Fatalf("VerifyTerminal(self-hashed malformed wire) error = %v", err)
			}
		})
	}
}

func TestTerminalRejectsNoncanonicalScopeWire(t *testing.T) {
	value := testTerminal(t)
	base := testVisibilityWire(value.LastLive)
	// Scope lengths/counts have their own framing and cannot be trusted just
	// because the outer terminal's 4096-byte scope limit was respected.
	roleCount := 4 + len("recordauth.visibility.v1") + 1 + 4 + len(value.LastLive.Kind) + 4 + len(value.LastLive.ProjectID)
	badCount := bytes.Clone(base)
	binary.BigEndian.PutUint32(badCount[roleCount:], math.MaxUint32)
	groupCount := roleCount + 4
	for _, role := range value.LastLive.AllowedRoles {
		groupCount += 4 + len(role)
	}
	badGroups := bytes.Clone(base)
	binary.BigEndian.PutUint32(badGroups[groupCount:], math.MaxUint32)
	duplicate := value.LastLive
	duplicate.AllowedGroupIDs = []string{"rag_alpha", "rag_alpha", "rag_beta"}
	reordered := value.LastLive
	reordered.AllowedGroupIDs = []string{"rag_beta", "rag_alpha"}
	duplicateRoles := value.LastLive
	duplicateRoles.AllowedRoles = []recordauth.Role{recordauth.RoleViewer, recordauth.RoleViewer}
	unknownVersion := bytes.Clone(base)
	unknownVersion[4+len("recordauth.visibility.v1")] = 2
	for name, scopeWire := range map[string][]byte{
		"role count overflow":   badCount,
		"group count overflow":  badGroups,
		"duplicate groups":      testVisibilityWire(duplicate),
		"reordered groups":      testVisibilityWire(reordered),
		"duplicate roles":       testVisibilityWire(duplicateRoles),
		"unknown scope version": unknownVersion,
		"trailing scope bytes":  append(bytes.Clone(base), 0),
		"truncated scope":       base[:len(base)-1],
		"empty scope":           {},
	} {
		for _, field := range []string{"last-live", "floor"} {
			t.Run(name+"/"+field, func(t *testing.T) {
				wire, _ := testTerminalWire(value, map[string][]byte{field: scopeWire})
				if _, err := DecodeTerminal(wire); !errors.Is(err, ErrInvalidTerminal) {
					t.Fatalf("DecodeTerminal(noncanonical scope) error = %v", err)
				}
			})
		}
	}
}

func testTerminal(t *testing.T) Terminal {
	t.Helper()
	return Terminal{
		Domain: DomainIdentity{
			ID:               "rd-" + strings.Repeat("a", 64),
			SystemIdentifier: "12345",
			DatabaseOID:      16384,
			DatabaseName:     "witness_db",
			Epoch:            3,
		},
		Source: SourceKey{
			DeploymentID: "dp-" + strings.Repeat("c", 64),
			ProjectID:    recordauth.ProjectIDDefault,
			Kind:         recordauth.SourceKindVPS,
			ID:           "vps_0123456789abcdef",
		},
		RetirementID:     "retirement_1-test",
		OriginGeneration: 7,
		BarrierDigest:    [32]byte{1, 2, 3},
		LastLive:         testScope(t, recordauth.VisibilityKindRestricted, []recordauth.Role{recordauth.RoleViewer}, []string{"rag_alpha", "rag_beta"}),
		FinalFloor:       testScope(t, recordauth.VisibilityKindRestricted, nil, []string{"rag_alpha"}),
		Sequence:         9,
		PreviousHash:     [32]byte{4, 5, 6},
	}
}

func testScope(t *testing.T, kind recordauth.VisibilityKind, roles []recordauth.Role, groups []string) recordauth.VisibilityScope {
	t.Helper()
	return testNormalizeScope(t, recordauth.VisibilityScope{
		Version:         recordauth.VisibilityScopeVersionV1,
		Kind:            kind,
		ProjectID:       recordauth.ProjectIDDefault,
		AllowedRoles:    roles,
		AllowedGroupIDs: groups,
		PolicyVersion:   recordauth.PolicyVersionV1,
		PolicyRevision:  5,
	})
}

func testNormalizeScope(t *testing.T, scope recordauth.VisibilityScope) recordauth.VisibilityScope {
	t.Helper()
	normalized, err := recordauth.NormalizeVisibilityScope(scope)
	if err != nil {
		t.Fatalf("NormalizeVisibilityScope() error = %v", err)
	}
	return normalized
}

type testWireSpan struct {
	start int
}

// Independent wire assembly deliberately does not invoke production codec
// helpers, so tests can express malformed and noncanonical documents.
func testTerminalWire(value Terminal, overrides map[string][]byte) ([]byte, map[string]testWireSpan) {
	var wire []byte
	spans := make(map[string]testWireSpan)
	field := func(name string, raw []byte) {
		if replacement, ok := overrides[name]; ok {
			raw = replacement
		}
		spans[name] = testWireSpan{start: len(wire)}
		wire = binary.BigEndian.AppendUint32(wire, uint32(len(raw)))
		wire = append(wire, raw...)
	}
	field("tag", []byte("HOUFENG-SOURCE-TERMINAL-V2"))
	spans["version"] = testWireSpan{start: len(wire)}
	wire = binary.BigEndian.AppendUint32(wire, 2)
	field("domain", []byte(value.Domain.ID))
	field("system", []byte(value.Domain.SystemIdentifier))
	wire = binary.BigEndian.AppendUint32(wire, value.Domain.DatabaseOID)
	field("database", []byte(value.Domain.DatabaseName))
	wire = binary.BigEndian.AppendUint64(wire, value.Domain.Epoch)
	field("deployment", []byte(value.Source.DeploymentID))
	field("project", []byte(value.Source.ProjectID))
	field("kind", []byte(value.Source.Kind))
	field("source", []byte(value.Source.ID))
	field("retirement", []byte(value.RetirementID))
	wire = binary.BigEndian.AppendUint64(wire, value.OriginGeneration)
	field("barrier", value.BarrierDigest[:])
	field("last-live", testVisibilityWire(value.LastLive))
	field("floor", testVisibilityWire(value.FinalFloor))
	wire = binary.BigEndian.AppendUint64(wire, value.Sequence)
	field("previous", value.PreviousHash[:])
	return wire, spans
}

func testVisibilityWire(scope recordauth.VisibilityScope) []byte {
	var wire []byte
	text := func(value string) {
		wire = binary.BigEndian.AppendUint32(wire, uint32(len(value)))
		wire = append(wire, value...)
	}
	text("recordauth.visibility.v1")
	wire = append(wire, byte(scope.Version))
	text(string(scope.Kind))
	text(string(scope.ProjectID))
	wire = binary.BigEndian.AppendUint32(wire, uint32(len(scope.AllowedRoles)))
	for _, role := range scope.AllowedRoles {
		text(string(role))
	}
	wire = binary.BigEndian.AppendUint32(wire, uint32(len(scope.AllowedGroupIDs)))
	for _, group := range scope.AllowedGroupIDs {
		text(group)
	}
	wire = binary.BigEndian.AppendUint64(wire, scope.PolicyVersion)
	wire = binary.BigEndian.AppendUint64(wire, scope.PolicyRevision)
	return wire
}
