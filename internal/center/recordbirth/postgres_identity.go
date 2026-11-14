package recordbirth

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// postgresPhysicalIdentity uses the same JSON field names as the source
// authority's DomainIdentity file. It is never an admission assertion by itself.
type postgresPhysicalIdentity struct {
	ID               string
	SystemIdentifier string
	DatabaseOID      uint32
	DatabaseName     string
	Epoch            uint64
}

// NewConfiguredPostgresAuthority validates the local identity file without
// contacting W2. Each operation verifies the live, admitted producer domain.
// The caller retains ownership of the pool.
func NewConfiguredPostgresAuthority(producer, application *pgxpool.Pool, identityPath string) (*PostgresAuthority, error) {
	if producer == nil || application == nil {
		return nil, fmt.Errorf("%w: birth authority and application pools are required", ErrInvalid)
	}
	identity, err := loadPostgresPhysicalIdentity(strings.TrimSpace(identityPath))
	if err != nil {
		return nil, fmt.Errorf("%w: invalid birth authority identity file", ErrInvalid)
	}
	return &PostgresAuthority{pool: producer, application: application, expected: &identity}, nil
}

func loadPostgresPhysicalIdentity(path string) (postgresPhysicalIdentity, error) {
	var identity postgresPhysicalIdentity
	if path == "" {
		return identity, ErrInvalid
	}
	file, err := os.Open(path)
	if err != nil {
		return identity, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o022 != 0 || info.Size() <= 0 || info.Size() > 65536 {
		return identity, ErrInvalid
	}
	data, err := io.ReadAll(io.LimitReader(file, 65537))
	if err != nil || len(data) == 0 || len(data) > 65536 {
		return identity, ErrInvalid
	}
	// JSON's case-insensitive struct matching must not let duplicate spellings
	// silently replace a trusted physical field.
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return identity, ErrInvalid
	}
	fields := map[string]bool{"id": false, "systemidentifier": false, "databaseoid": false, "databasename": false, "epoch": false}
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return identity, ErrInvalid
		}
		key, ok := token.(string)
		if !ok {
			return identity, ErrInvalid
		}
		key = strings.ToLower(key)
		seen, known := fields[key]
		if !known || seen {
			return identity, ErrInvalid
		}
		fields[key] = true
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return identity, ErrInvalid
		}
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return identity, ErrInvalid
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return identity, ErrInvalid
	}
	for _, seen := range fields {
		if !seen {
			return identity, ErrInvalid
		}
	}
	decoder = json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&identity); err != nil || !validPostgresPhysicalIdentity(identity) {
		return postgresPhysicalIdentity{}, ErrInvalid
	}
	return identity, nil
}

func validPostgresPhysicalIdentity(identity postgresPhysicalIdentity) bool {
	if !hexIdentity(identity.ID, "rd-", 64) || identity.DatabaseOID == 0 || identity.Epoch == 0 || identity.Epoch > math.MaxInt64 {
		return false
	}
	system, err := strconv.ParseUint(identity.SystemIdentifier, 10, 64)
	if err != nil || system == 0 || strconv.FormatUint(system, 10) != identity.SystemIdentifier {
		return false
	}
	name := identity.DatabaseName
	if len(name) == 0 || len(name) > 63 || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	for i := range name {
		c := name[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}
	return true
}
