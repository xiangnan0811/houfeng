package recordbirth

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math"
	"strings"
	"testing"
)

// These bytes, the entry hash and the MAC were generated independently using
// Node's Buffer big-endian primitives and crypto, not the Go codec.
const checkpointVectorEntryHex = "0000002c686f7566656e672e7265636f72642d62697274682e6170702d636865636b706f696e742d656e7472792e76310000" +
	"004372642d313131313131313131313131313131313131313131313131313131313131313131313131313131313131313131" +
	"3131313131313131313131313131313131313100000043626c2d323232323232323232323232323232323232323232323232" +
	"323232323232323232323232323232323232323232323232323232323232323232323232323232320000004364702d333333" +
	"3333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333" +
	"33333333333333333333330000000764656661756c74000000000000000100000043626b2d34343434343434343434343434" +
	"3434343434343434343434343434343434343434343434343434343434343434343434343434343434343434343434343434" +
	"3406060606060606060606060606060606060606060606060606060606060606060000000000000001000000000000000000" +
	"0000000000000000000000000000000000000000000000000000247271615f35353535353535353535353535353535353535" +
	"35353535353535353535353535000000077573725f6f6e650808080808080808080808080808080808080808080808080808" +
	"0808080808080000000d7265636f72645f637265617465000000086372656174652d31070707070707070707070707070707" +
	"0707070707070707070707070707070707"

const checkpointVectorPrefixHex = "00000026686f7566656e672e7265636f72642d62697274682e6170702d636865636b706f696e742e76310000004372642d31" +
	"3131313131313131313131313131313131313131313131313131313131313131313131313131313131313131313131313131" +
	"3131313131313131313131313100000043626c2d323232323232323232323232323232323232323232323232323232323232" +
	"323232323232323232323232323232323232323232323232323232323232323232320000004364702d333333333333333333" +
	"3333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333" +
	"33333333330000000764656661756c74000000000000000200000043626b2d61616161616161616161616161616161616161" +
	"6161616161616161616161616161616161616161616161616161616161616161616161616161616161616161610a0a0a0a0a" +
	"0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0000000000000000000000000000000000000000000000" +
	"00000000000000000000000000000000000000000000000001b8c2499cc3100aefa197907e83adbbb30b032df34d50edef32" +
	"19cac19bf673dc"

const checkpointVectorHashHex = "b8c2499cc3100aefa197907e83adbbb30b032df34d50edef3219cac19bf673dc"
const checkpointVectorMACHex = "da286048067c990cbc747e4e714fbdfbfdd33a4d033322b43b4b505d37c88a15"

func checkpointTestBytes(t *testing.T, value string) []byte {
	t.Helper()
	decoded, err := hex.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}

func checkpointTestDigest(value byte) [32]byte {
	var digest [32]byte
	for index := range digest {
		digest[index] = value
	}
	return digest
}

func checkpointTestVector(t *testing.T) CheckpointProof {
	t.Helper()
	original := Binding{
		AuthorityID: "rd-" + strings.Repeat("1", 64), LineageID: "bl-" + strings.Repeat("2", 64),
		DeploymentID: "dp-" + strings.Repeat("3", 64), ProjectID: "default", Generation: 1,
		KeyID: "bk-" + strings.Repeat("4", 64), CutoverDigest: checkpointTestDigest(6),
	}
	current := original
	current.Generation = 2
	current.KeyID = "bk-" + strings.Repeat("a", 64)
	current.CutoverDigest = checkpointTestDigest(10)
	var entryHash [32]byte
	copy(entryHash[:], checkpointTestBytes(t, checkpointVectorHashHex))
	return CheckpointProof{
		Binding:      current,
		ProposedHead: CheckpointHead{Sequence: 1, Hash: entryHash},
		Checkpoint: Checkpoint{
			Binding: original, Sequence: 1, AllocationID: "rqa_" + strings.Repeat("5", 32),
			Request: Request{
				ProjectID: "default", ActorID: "usr_one", ActorScopeDigest: checkpointTestDigest(8),
				Purpose: PurposeCreate, IdempotencyKey: "create-1", RequestFingerprint: checkpointTestDigest(7),
			},
		},
	}
}

func checkpointTestSignedVector(t *testing.T) []byte {
	t.Helper()
	return checkpointTestBytes(t, checkpointVectorPrefixHex+checkpointVectorEntryHex+checkpointVectorHashHex+checkpointVectorMACHex)
}

func TestCheckpointIndependentFramingHashAndMAC(t *testing.T) {
	proof := checkpointTestVector(t)
	entry, err := EncodeCheckpoint(proof.Checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(entry, checkpointTestBytes(t, checkpointVectorEntryHex)) {
		t.Fatal("checkpoint framing differs from the independent vector")
	}
	hash, err := proof.Checkpoint.Hash()
	if err != nil || !bytes.Equal(hash[:], checkpointTestBytes(t, checkpointVectorHashHex)) {
		t.Fatalf("checkpoint hash differs from the independent vector: %v", err)
	}
	encoded, err := EncodeCheckpointProof(proof)
	if err != nil {
		t.Fatal(err)
	}
	want := checkpointTestBytes(t, checkpointVectorPrefixHex+checkpointVectorEntryHex+checkpointVectorHashHex)
	if !bytes.Equal(encoded, want) {
		t.Fatal("checkpoint proof framing differs from the independent vector")
	}
	key := checkpointTestDigest(9)
	mac := hmac.New(sha256.New, key[:])
	_, _ = mac.Write(encoded)
	if !bytes.Equal(mac.Sum(nil), checkpointTestBytes(t, checkpointVectorMACHex)) {
		t.Fatal("checkpoint proof MAC differs from the independent vector")
	}
	signed := checkpointTestSignedVector(t)
	decoded, err := DecodeCheckpointProof(signed)
	if err != nil || decoded != proof {
		t.Fatalf("decode independent proof: %+v, %v", decoded, err)
	}
	verified, err := VerifyCheckpointProof(signed, key[:])
	if err != nil || verified != proof {
		t.Fatalf("verify independent proof: %+v, %v", verified, err)
	}
	if verified.Binding.Generation != 2 || verified.Checkpoint.Binding.Generation != 1 ||
		verified.Binding.KeyID == verified.Checkpoint.Binding.KeyID ||
		verified.Binding.CutoverDigest == verified.Checkpoint.Binding.CutoverDigest {
		t.Fatal("proof did not preserve the entry's original binding after generation advance")
	}
}

func TestCheckpointProofRejectsTamperingAndMalformedFrames(t *testing.T) {
	signed := checkpointTestSignedVector(t)
	key := checkpointTestDigest(9)
	for index := range signed {
		tampered := bytes.Clone(signed)
		tampered[index] ^= 1
		if _, err := VerifyCheckpointProof(tampered, key[:]); err == nil {
			t.Fatalf("accepted tampered byte %d", index)
		}
	}
	for size := range len(signed) {
		if _, err := DecodeCheckpointProof(signed[:size]); err == nil {
			t.Fatalf("accepted truncated proof of %d bytes", size)
		}
	}
	wrongKey := checkpointTestDigest(10)
	if _, err := VerifyCheckpointProof(signed, wrongKey[:]); err == nil {
		t.Fatal("accepted the wrong key")
	}
	if _, err := VerifyCheckpointProof(signed, key[:31]); err == nil {
		t.Fatal("accepted a key of the wrong length")
	}

	// Decoding checks framing only: an unauthenticated MAC is not authority.
	badMAC := bytes.Clone(signed)
	badMAC[len(badMAC)-1] ^= 1
	if _, err := DecodeCheckpointProof(badMAC); err != nil {
		t.Fatalf("structural decode unexpectedly authenticated the MAC: %v", err)
	}
	if _, err := VerifyCheckpointProof(badMAC, key[:]); err == nil {
		t.Fatal("verified an invalid MAC")
	}

	prefixSize := len(checkpointTestBytes(t, checkpointVectorPrefixHex))
	for name, mutate := range map[string]func([]byte) []byte{
		"proof domain": func(raw []byte) []byte {
			raw[4] ^= 1
			return raw
		},
		"entry domain": func(raw []byte) []byte {
			raw[prefixSize+4] ^= 1
			return raw
		},
		"oversized text": func(raw []byte) []byte {
			binary.BigEndian.PutUint32(raw[:4], math.MaxUint32)
			return raw
		},
		"invalid UTF-8": func(raw []byte) []byte {
			index := bytes.Index(raw, []byte("usr_one"))
			if index < 0 {
				t.Fatal("fixture actor missing")
			}
			raw[index] = 0xff
			return raw
		},
		"entry hash": func(raw []byte) []byte {
			raw[len(raw)-2*sha256.Size] ^= 1
			return raw
		},
		"trailing byte after MAC": func(raw []byte) []byte {
			return append(raw, 0)
		},
		"trailing byte before MAC": func(raw []byte) []byte {
			canonical := append([]byte(nil), raw[:len(raw)-sha256.Size]...)
			canonical = append(canonical, 0)
			return append(canonical, raw[len(raw)-sha256.Size:]...)
		},
		"over maximum": func(raw []byte) []byte {
			return append(raw, make([]byte, 4097-len(raw))...)
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeCheckpointProof(mutate(bytes.Clone(signed))); err == nil {
				t.Fatal("accepted a malformed proof")
			}
		})
	}
}

func TestCheckpointHeadValidation(t *testing.T) {
	nonzero := checkpointTestDigest(1)
	cases := []struct {
		name  string
		head  CheckpointHead
		valid bool
	}{
		{"empty", CheckpointHead{}, true},
		{"first", CheckpointHead{Sequence: 1, Hash: nonzero}, true},
		{"maximum", CheckpointHead{Sequence: math.MaxInt64, Hash: nonzero}, true},
		{"empty with hash", CheckpointHead{Hash: nonzero}, false},
		{"positive without hash", CheckpointHead{Sequence: 1}, false},
		{"overflow", CheckpointHead{Sequence: uint64(math.MaxInt64) + 1, Hash: nonzero}, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if err := test.head.Validate(); (err == nil) != test.valid {
				t.Fatalf("Validate() = %v, want valid=%v", err, test.valid)
			}
		})
	}
}

func TestCheckpointValidation(t *testing.T) {
	original := checkpointTestVector(t).Checkpoint
	cases := []struct {
		name   string
		change func(*Checkpoint)
		valid  bool
	}{
		{"first entry", func(*Checkpoint) {}, true},
		{"later entry", func(c *Checkpoint) { c.Sequence = 2; c.PreviousHash = checkpointTestDigest(1) }, true},
		{"maximum sequence", func(c *Checkpoint) { c.Sequence = math.MaxInt64; c.PreviousHash = checkpointTestDigest(1) }, true},
		{"zero sequence", func(c *Checkpoint) { c.Sequence = 0 }, false},
		{"overflow sequence", func(c *Checkpoint) { c.Sequence = uint64(math.MaxInt64) + 1; c.PreviousHash = checkpointTestDigest(1) }, false},
		{"first with predecessor", func(c *Checkpoint) { c.PreviousHash = checkpointTestDigest(1) }, false},
		{"later without predecessor", func(c *Checkpoint) { c.Sequence = 2 }, false},
		{"invalid allocation prefix", func(c *Checkpoint) { c.AllocationID = "rba_" + strings.Repeat("5", 32) }, false},
		{"noncanonical allocation", func(c *Checkpoint) { c.AllocationID = "rqa_" + strings.Repeat("A", 32) }, false},
		{"cross project", func(c *Checkpoint) { c.Request.ProjectID = "other" }, false},
		{"invalid UTF-8 actor", func(c *Checkpoint) { c.Request.ActorID = "usr_\xff" }, false},
		{"zero fingerprint", func(c *Checkpoint) { c.Request.RequestFingerprint = [32]byte{} }, false},
		{"invalid original binding", func(c *Checkpoint) { c.Binding.Generation = 0 }, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			checkpoint := original
			test.change(&checkpoint)
			if err := checkpoint.Validate(); (err == nil) != test.valid {
				t.Fatalf("Validate() = %v, want valid=%v", err, test.valid)
			}
			if _, err := EncodeCheckpoint(checkpoint); (err == nil) != test.valid {
				t.Fatalf("EncodeCheckpoint() = %v, want valid=%v", err, test.valid)
			}
			if _, err := checkpoint.Hash(); (err == nil) != test.valid {
				t.Fatalf("Hash() = %v, want valid=%v", err, test.valid)
			}
		})
	}
}

func TestCheckpointProofRejectsInvalidEndpointsAndBindings(t *testing.T) {
	original := checkpointTestVector(t)
	first := checkpointTestDigest(11)
	second := checkpointTestDigest(12)
	cases := map[string]func(*CheckpointProof){
		"zero proposed":             func(p *CheckpointProof) { p.ProposedHead = CheckpointHead{} },
		"nonzero empty expected":    func(p *CheckpointProof) { p.ExpectedAnchor.Hash = first },
		"expected after proposed":   func(p *CheckpointProof) { p.ExpectedAnchor = CheckpointHead{Sequence: 2, Hash: first} },
		"checkpoint after proposed": func(p *CheckpointProof) { p.Checkpoint.Sequence = 2; p.Checkpoint.PreviousHash = first },
		"expected overflow": func(p *CheckpointProof) {
			p.ExpectedAnchor = CheckpointHead{Sequence: uint64(math.MaxInt64) + 1, Hash: first}
		},
		"proposed overflow": func(p *CheckpointProof) {
			p.ProposedHead = CheckpointHead{Sequence: uint64(math.MaxInt64) + 1, Hash: first}
		},
		"equal heads differ": func(p *CheckpointProof) {
			p.ExpectedAnchor = CheckpointHead{Sequence: 2, Hash: first}
			p.ProposedHead = CheckpointHead{Sequence: 2, Hash: second}
		},
		"expected differs from entry": func(p *CheckpointProof) {
			p.ExpectedAnchor = CheckpointHead{Sequence: 1, Hash: first}
			p.ProposedHead = CheckpointHead{Sequence: 2, Hash: second}
		},
		"proposed differs from entry": func(p *CheckpointProof) { p.ProposedHead.Hash = first },
		"other authority":             func(p *CheckpointProof) { p.Binding.AuthorityID = "rd-" + strings.Repeat("f", 64) },
		"other lineage":               func(p *CheckpointProof) { p.Binding.LineageID = "bl-" + strings.Repeat("f", 64) },
		"other deployment":            func(p *CheckpointProof) { p.Binding.DeploymentID = "dp-" + strings.Repeat("f", 64) },
		"other project":               func(p *CheckpointProof) { p.Checkpoint.Request.ProjectID = "other" },
		"original generation newer":   func(p *CheckpointProof) { p.Checkpoint.Binding.Generation = 3 },
		"invalid current binding":     func(p *CheckpointProof) { p.Binding.CutoverDigest = [32]byte{} },
		"invalid original entry":      func(p *CheckpointProof) { p.Checkpoint.AllocationID = "rqa_invalid" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			proof := original
			mutate(&proof)
			if err := proof.Validate(); err == nil {
				t.Fatal("Validate accepted an invalid proof")
			}
			if _, err := EncodeCheckpointProof(proof); err == nil {
				t.Fatal("EncodeCheckpointProof accepted an invalid proof")
			}
		})
	}
}

func TestCheckpointProofDecodingRejectsConflictingEndpoints(t *testing.T) {
	signed := checkpointTestSignedVector(t)
	prefixSize := len(checkpointTestBytes(t, checkpointVectorPrefixHex))
	expectedOffset := prefixSize - 80
	proposedOffset := prefixSize - 40
	first := checkpointTestDigest(11)
	second := checkpointTestDigest(12)
	cases := map[string]func([]byte){
		"proposed differs from entry": func(raw []byte) {
			copy(raw[proposedOffset+8:], first[:])
		},
		"expected differs from entry": func(raw []byte) {
			binary.BigEndian.PutUint64(raw[expectedOffset:], 1)
			copy(raw[expectedOffset+8:], first[:])
			binary.BigEndian.PutUint64(raw[proposedOffset:], 2)
			copy(raw[proposedOffset+8:], second[:])
		},
		"equal heads differ": func(raw []byte) {
			binary.BigEndian.PutUint64(raw[expectedOffset:], 2)
			copy(raw[expectedOffset+8:], first[:])
			binary.BigEndian.PutUint64(raw[proposedOffset:], 2)
			copy(raw[proposedOffset+8:], second[:])
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			raw := bytes.Clone(signed)
			mutate(raw)
			if _, err := DecodeCheckpointProof(raw); err == nil {
				t.Fatal("decoded conflicting endpoints")
			}
		})
	}
}

func TestCheckpointProofAllowsCoveredOlderEntry(t *testing.T) {
	original := checkpointTestVector(t)
	cases := map[string]func(*CheckpointProof){
		"older than expected anchor": func(p *CheckpointProof) {
			p.ExpectedAnchor = CheckpointHead{Sequence: 2, Hash: checkpointTestDigest(11)}
			p.ProposedHead = CheckpointHead{Sequence: 3, Hash: checkpointTestDigest(12)}
		},
		"older than equal heads": func(p *CheckpointProof) {
			p.ExpectedAnchor = CheckpointHead{Sequence: 2, Hash: checkpointTestDigest(11)}
			p.ProposedHead = p.ExpectedAnchor
		},
		"entry is both heads": func(p *CheckpointProof) { p.ExpectedAnchor = p.ProposedHead },
		"maximum equal heads": func(p *CheckpointProof) {
			p.ExpectedAnchor = CheckpointHead{Sequence: math.MaxInt64, Hash: checkpointTestDigest(11)}
			p.ProposedHead = p.ExpectedAnchor
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			proof := original
			mutate(&proof)
			if err := proof.Validate(); err != nil {
				t.Fatalf("valid proof: %v", err)
			}
			encoded, err := EncodeCheckpointProof(proof)
			if err != nil {
				t.Fatal(err)
			}
			// A zero MAC is sufficient for structural decoding, which does not
			// claim to authenticate the proof or establish chain ancestry.
			decoded, err := DecodeCheckpointProof(append(encoded, make([]byte, sha256.Size)...))
			if err != nil || decoded != proof {
				t.Fatalf("decode valid proof: %+v, %v", decoded, err)
			}
		})
	}
}
