package evidence

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

type FileComparisonIntentSigner struct {
	keys      map[string][]byte
	currentID string
}

func OpenComparisonIntentKeyring(directory, currentKeyID string, reservedPaths []string) (*FileComparisonIntentSigner, error) {
	if !validComparisonKeyID(currentKeyID) {
		return nil, ErrComparisonIntentUnavailable
	}
	reserved, err := reservedKeyIdentities(reservedPaths)
	if err != nil {
		return nil, err
	}
	dirFD, err := unix.Open(directory, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, ErrComparisonIntentUnavailable
	}
	defer func() { _ = unix.Close(dirFD) }()
	dup, err := unix.Dup(dirFD)
	if err != nil {
		return nil, ErrComparisonIntentUnavailable
	}
	listing := os.NewFile(uintptr(dup), directory)
	if listing == nil {
		_ = unix.Close(dup)
		return nil, ErrComparisonIntentUnavailable
	}
	names, err := listing.Readdirnames(-1)
	_ = listing.Close()
	if err != nil {
		return nil, ErrComparisonIntentUnavailable
	}
	keys := make(map[string][]byte)
	for _, name := range names {
		if name == "." || name == ".." || !validComparisonKeyID(name) {
			continue
		}
		material, err := readComparisonKeyAt(dirFD, name, reserved)
		if err != nil {
			return nil, err
		}
		keys[name] = material
	}
	if _, ok := keys[currentKeyID]; !ok {
		return nil, ErrComparisonIntentUnavailable
	}
	return &FileComparisonIntentSigner{keys: keys, currentID: currentKeyID}, nil
}

func (signer *FileComparisonIntentSigner) Sign(claims ComparisonIntentClaims) (ComparisonIntent, error) {
	if signer == nil || claims.Purpose != ComparisonIntentPurpose {
		return ComparisonIntent{}, ErrComparisonIntentInvalid
	}
	if claims.KeyID == "" {
		claims.KeyID = signer.currentID
	}
	if claims.KeyID != signer.currentID {
		return ComparisonIntent{}, ErrComparisonIntentInvalid
	}
	key, ok := signer.keys[claims.KeyID]
	if !ok {
		return ComparisonIntent{}, ErrComparisonIntentUnavailable
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return ComparisonIntent{}, ErrComparisonIntentInvalid
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(payload)
	token := strings.Join([]string{
		"cmp1",
		claims.KeyID,
		base64.RawURLEncoding.EncodeToString(payload),
		base64.RawURLEncoding.EncodeToString(mac.Sum(nil)),
	}, ".")
	return ComparisonIntent{
		Token:     token,
		KeyID:     claims.KeyID,
		IssuedAt:  claims.IssuedAt,
		ExpiresAt: claims.ExpiresAt,
	}, nil
}

func (signer *FileComparisonIntentSigner) Verify(token string, now time.Time) (ComparisonIntentClaims, error) {
	if signer == nil {
		return ComparisonIntentClaims{}, ErrComparisonIntentUnavailable
	}
	parts := strings.Split(token, ".")
	if len(parts) != 4 || parts[0] != "cmp1" || !validComparisonKeyID(parts[1]) {
		return ComparisonIntentClaims{}, ErrComparisonIntentInvalid
	}
	key, ok := signer.keys[parts[1]]
	if !ok {
		return ComparisonIntentClaims{}, ErrComparisonIntentInvalid
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return ComparisonIntentClaims{}, ErrComparisonIntentInvalid
	}
	sum, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil {
		return ComparisonIntentClaims{}, ErrComparisonIntentInvalid
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(payload)
	if !hmac.Equal(sum, mac.Sum(nil)) {
		return ComparisonIntentClaims{}, ErrComparisonIntentInvalid
	}
	var claims ComparisonIntentClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ComparisonIntentClaims{}, ErrComparisonIntentInvalid
	}
	if claims.Purpose != ComparisonIntentPurpose || claims.KeyID != parts[1] {
		return ComparisonIntentClaims{}, ErrComparisonIntentInvalid
	}
	issued, err := time.Parse(time.RFC3339Nano, claims.IssuedAtText)
	if err != nil {
		return ComparisonIntentClaims{}, ErrComparisonIntentInvalid
	}
	expires, err := time.Parse(time.RFC3339Nano, claims.ExpiresAtText)
	if err != nil {
		return ComparisonIntentClaims{}, ErrComparisonIntentInvalid
	}
	claims.IssuedAt = issued.UTC()
	claims.ExpiresAt = expires.UTC()
	if !now.UTC().Before(claims.ExpiresAt) {
		return claims, ErrComparisonIntentExpired
	}
	return claims, nil
}

type keyIdentity struct {
	dev uint64
	ino uint64
}

func reservedKeyIdentities(paths []string) (map[keyIdentity]struct{}, error) {
	out := make(map[keyIdentity]struct{}, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, ErrComparisonIntentUnavailable
		}
		fd, err := unix.Open(absolute, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			return nil, ErrComparisonIntentUnavailable
		}
		var stat unix.Stat_t
		statErr := unix.Fstat(fd, &stat)
		_ = unix.Close(fd)
		if statErr != nil {
			return nil, ErrComparisonIntentUnavailable
		}
		out[keyIdentity{dev: uint64(stat.Dev), ino: uint64(stat.Ino)}] = struct{}{}
	}
	return out, nil
}

func readComparisonKeyAt(dirFD int, name string, reserved map[keyIdentity]struct{}) ([]byte, error) {
	fd, err := unix.Openat(dirFD, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, ErrComparisonIntentUnavailable
	}
	defer func() { _ = unix.Close(fd) }()
	var before unix.Stat_t
	if err := unix.Fstat(fd, &before); err != nil || !safeComparisonKeyStat(before) {
		return nil, ErrComparisonIntentUnavailable
	}
	if _, blocked := reserved[keyIdentity{dev: uint64(before.Dev), ino: uint64(before.Ino)}]; blocked {
		return nil, ErrComparisonIntentUnavailable
	}
	material := make([]byte, 0, 64)
	buf := make([]byte, 4096)
	for {
		n, readErr := unix.Read(fd, buf)
		if n > 0 {
			material = append(material, buf[:n]...)
		}
		if readErr != nil {
			return nil, ErrComparisonIntentUnavailable
		}
		if n == 0 {
			break
		}
	}
	if len(material) < 32 {
		return nil, ErrComparisonIntentUnavailable
	}
	var after unix.Stat_t
	if err := unix.Fstat(fd, &after); err != nil || !sameComparisonKeyStat(before, after) {
		return nil, ErrComparisonIntentUnavailable
	}
	return append([]byte(nil), material...), nil
}

func safeComparisonKeyStat(stat unix.Stat_t) bool {
	return stat.Mode&unix.S_IFMT == unix.S_IFREG && stat.Mode&0777 == 0400 && stat.Nlink == 1
}

func sameComparisonKeyStat(before, after unix.Stat_t) bool {
	return before.Dev == after.Dev && before.Ino == after.Ino && before.Mode == after.Mode &&
		before.Nlink == after.Nlink && before.Uid == after.Uid && before.Gid == after.Gid
}

func validComparisonKeyID(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for index, character := range value {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
			continue
		}
		if index > 0 && (character == '_' || character == '-' || character == '.') {
			continue
		}
		return false
	}
	return true
}
