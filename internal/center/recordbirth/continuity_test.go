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

// These framing bytes and MACs were generated independently with Node's
// big-endian Buffer primitives and crypto, using synthetic test key material.
const continuityVectorPrefixHex = "00000022686f7566656e672e7265636f72642d62697274682e636f6e74696e756974792e76310000004372642d3131313131" +
	"3131313131313131313131313131313131313131313131313131313131313131313131313131313131313131313131313131" +
	"31313131313131313100000043626c2d32323232323232323232323232323232323232323232323232323232323232323232" +
	"3232323232323232323232323232323232323232323232323232323232320000004364702d33333333333333333333333333" +
	"3333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333" +
	"330000000764656661756c74000000000000000200000043626b2d6161616161616161616161616161616161616161616161" +
	"61616161616161616161616161616161616161616161616161616161616161616161616161616161610a0a0a0a0a0a0a0a0a" +
	"0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a"

const continuityEnabledTailHex = "00000000000000070c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c01"
const continuityEnabledMACHex = "2b45e47b9fb72b04cdbaeb402a6b092aeb923f18780d9210c9317a8c5248fe8d"
const continuityDisabledTailHex = "0000000000000000000000000000000000000000000000000000000000000000000000000000000000"
const continuityDisabledMACHex = "22cadc7dc450b84dc79eca0cf04c0bec426af6c04d22f4c5eb2682c145685bae"

func continuityTestBytes(t *testing.T, value string) []byte {
	t.Helper()
	decoded, err := hex.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}

func continuityTestDigest(value byte) [32]byte {
	var digest [32]byte
	for index := range digest {
		digest[index] = value
	}
	return digest
}

func continuityTestBinding() Binding {
	return Binding{
		AuthorityID: "rd-" + strings.Repeat("1", 64), LineageID: "bl-" + strings.Repeat("2", 64),
		DeploymentID: "dp-" + strings.Repeat("3", 64), ProjectID: "default", Generation: 2,
		KeyID: "bk-" + strings.Repeat("a", 64), CutoverDigest: continuityTestDigest(10),
	}
}

func continuityTestEqual(t *testing.T, got, want ContinuityObservation, raw []byte) {
	t.Helper()
	if got.Binding != want.Binding || got.Anchor != want.Anchor || got.Enabled != want.Enabled || !bytes.Equal(got.Proof, raw) {
		t.Fatalf("continuity projection = %+v, want binding=%+v anchor=%+v enabled=%v and the original proof", got, want.Binding, want.Anchor, want.Enabled)
	}
}

func TestContinuityProofIndependentVectors(t *testing.T) {
	key := continuityTestDigest(9)
	cases := []struct {
		name        string
		observation ContinuityObservation
		tail        string
		mac         string
	}{
		{
			name: "enabled with checkpoint",
			observation: ContinuityObservation{
				Binding: continuityTestBinding(), Anchor: CheckpointHead{Sequence: 7, Hash: continuityTestDigest(12)}, Enabled: true,
			},
			tail: continuityEnabledTailHex, mac: continuityEnabledMACHex,
		},
		{
			name:        "disabled with empty checkpoint",
			observation: ContinuityObservation{Binding: continuityTestBinding()},
			tail:        continuityDisabledTailHex, mac: continuityDisabledMACHex,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := EncodeContinuityObservation(test.observation)
			if err != nil {
				t.Fatal(err)
			}
			want := continuityTestBytes(t, continuityVectorPrefixHex+test.tail)
			if !bytes.Equal(encoded, want) {
				t.Fatal("continuity framing differs from the independent vector")
			}
			mac := hmac.New(sha256.New, key[:])
			_, _ = mac.Write(encoded)
			if !bytes.Equal(mac.Sum(nil), continuityTestBytes(t, test.mac)) {
				t.Fatal("continuity MAC differs from the independent vector")
			}
			signed := continuityTestBytes(t, continuityVectorPrefixHex+test.tail+test.mac)
			decoded, err := DecodeContinuityProof(signed)
			if err != nil {
				t.Fatalf("decode independent token: %v", err)
			}
			continuityTestEqual(t, decoded, test.observation, signed)
			verified, err := VerifyContinuityProof(signed, key[:])
			if err != nil {
				t.Fatalf("verify independent token: %v", err)
			}
			continuityTestEqual(t, verified, test.observation, signed)
		})
	}
}

func TestContinuityProofRejectsTamperingAndMalformedFrames(t *testing.T) {
	signed := continuityTestBytes(t, continuityVectorPrefixHex+continuityEnabledTailHex+continuityEnabledMACHex)
	key := continuityTestDigest(9)
	for index := range signed {
		tampered := bytes.Clone(signed)
		tampered[index] ^= 1
		if _, err := VerifyContinuityProof(tampered, key[:]); err == nil {
			t.Fatalf("accepted tampered byte %d", index)
		}
	}
	for size := range len(signed) {
		if _, err := DecodeContinuityProof(signed[:size]); err == nil {
			t.Fatalf("accepted truncated token of %d bytes", size)
		}
	}
	wrongKey := continuityTestDigest(10)
	if _, err := VerifyContinuityProof(signed, wrongKey[:]); err == nil {
		t.Fatal("accepted the wrong key")
	}
	if _, err := VerifyContinuityProof(signed, key[:31]); err == nil {
		t.Fatal("accepted a key of the wrong length")
	}

	badMAC := bytes.Clone(signed)
	badMAC[len(badMAC)-1] ^= 1
	if _, err := DecodeContinuityProof(badMAC); err != nil {
		t.Fatalf("structural decode unexpectedly authenticated the MAC: %v", err)
	}
	if _, err := VerifyContinuityProof(badMAC, key[:]); err == nil {
		t.Fatal("verified an invalid MAC")
	}

	prefixSize := len(continuityTestBytes(t, continuityVectorPrefixHex))
	for name, mutate := range map[string]func([]byte) []byte{
		"domain": func(raw []byte) []byte {
			raw[4] ^= 1
			return raw
		},
		"oversized text": func(raw []byte) []byte {
			binary.BigEndian.PutUint32(raw[:4], math.MaxUint32)
			return raw
		},
		"invalid UTF-8 binding": func(raw []byte) []byte {
			index := bytes.Index(raw, []byte("rd-"))
			if index < 0 {
				t.Fatal("fixture authority missing")
			}
			raw[index+3] = 0xff
			return raw
		},
		"invalid boolean": func(raw []byte) []byte {
			raw[len(raw)-sha256.Size-1] = 2
			return raw
		},
		"zero anchor with hash": func(raw []byte) []byte {
			binary.BigEndian.PutUint64(raw[prefixSize:], 0)
			return raw
		},
		"positive anchor without hash": func(raw []byte) []byte {
			clear(raw[prefixSize+8 : prefixSize+8+sha256.Size])
			return raw
		},
		"overflow anchor": func(raw []byte) []byte {
			binary.BigEndian.PutUint64(raw[prefixSize:], uint64(math.MaxInt64)+1)
			return raw
		},
		"zero generation": func(raw []byte) []byte {
			index := bytes.Index(raw, []byte("default"))
			if index < 0 {
				t.Fatal("fixture project missing")
			}
			binary.BigEndian.PutUint64(raw[index+len("default"):], 0)
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
			if _, err := DecodeContinuityProof(mutate(bytes.Clone(signed))); err == nil {
				t.Fatal("accepted a malformed token")
			}
		})
	}

	// Even a valid MAC does not make an invalid wire projection acceptable.
	invalidFlag := bytes.Clone(signed)
	invalidFlag[len(invalidFlag)-sha256.Size-1] = 2
	mac := hmac.New(sha256.New, key[:])
	_, _ = mac.Write(invalidFlag[:len(invalidFlag)-sha256.Size])
	copy(invalidFlag[len(invalidFlag)-sha256.Size:], mac.Sum(nil))
	if _, err := VerifyContinuityProof(invalidFlag, key[:]); err == nil {
		t.Fatal("accepted a valid MAC over a noncanonical boolean")
	}
}

func TestContinuityProofRetainsIndependentBytes(t *testing.T) {
	raw := continuityTestBytes(t, continuityVectorPrefixHex+continuityEnabledTailHex+continuityEnabledMACHex)
	want := bytes.Clone(raw)
	observation, err := DecodeContinuityProof(raw)
	if err != nil {
		t.Fatal(err)
	}
	raw[0] ^= 1
	if !bytes.Equal(observation.Proof, want) {
		t.Fatal("mutating the input changed the retained proof")
	}
	observation.Proof[1] ^= 1
	if raw[1] != want[1] {
		t.Fatal("mutating the retained proof changed the input")
	}
}

func TestEncodeContinuityObservationValidatesProjection(t *testing.T) {
	original := ContinuityObservation{
		Binding: continuityTestBinding(), Anchor: CheckpointHead{Sequence: 7, Hash: continuityTestDigest(12)}, Enabled: true,
	}
	cases := []struct {
		name   string
		change func(*ContinuityObservation)
		valid  bool
	}{
		{"valid", func(*ContinuityObservation) {}, true},
		{"empty anchor", func(o *ContinuityObservation) { o.Anchor = CheckpointHead{} }, true},
		{"maximum anchor", func(o *ContinuityObservation) { o.Anchor.Sequence = math.MaxInt64 }, true},
		{"zero anchor with hash", func(o *ContinuityObservation) { o.Anchor.Sequence = 0 }, false},
		{"positive anchor without hash", func(o *ContinuityObservation) { o.Anchor.Hash = [32]byte{} }, false},
		{"overflow anchor", func(o *ContinuityObservation) { o.Anchor.Sequence = uint64(math.MaxInt64) + 1 }, false},
		{"invalid binding", func(o *ContinuityObservation) { o.Binding.KeyID = "invalid" }, false},
		{"zero generation", func(o *ContinuityObservation) { o.Binding.Generation = 0 }, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			observation := original
			test.change(&observation)
			if _, err := EncodeContinuityObservation(observation); (err == nil) != test.valid {
				t.Fatalf("EncodeContinuityObservation() = %v, want valid=%v", err, test.valid)
			}
		})
	}
}
