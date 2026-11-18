package sourceretirement

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrAuthorityConfiguration = errors.New("source authority enrollment requires its independent witness reader and exact physical identity configuration")

// OpenConfiguredAuthority binds an enrolled APP to the separately configured
// witness identity. Only an explicitly unenrolled APP may use the legacy path.
// The caller owns both pools and the lifetime of the returned authority.
func OpenConfiguredAuthority(ctx context.Context, local, witness *pgxpool.Pool, identityPath string) (*Authority, error) {
	identityPath = strings.TrimSpace(identityPath)
	if ctx == nil {
		return nil, ErrAuthorityConfiguration
	}
	if local == nil {
		if witness != nil || identityPath != "" {
			return nil, ErrAuthorityConfiguration
		}
		return nil, nil
	}
	var enrolled bool
	if err := local.QueryRow(ctx, `select exists(select 1 from public.source_authority_admission where singleton)`).Scan(&enrolled); err != nil {
		return nil, ErrAuthorityConfiguration
	}
	if witness == nil && identityPath == "" && !enrolled {
		return nil, nil
	}
	if witness == nil || identityPath == "" || !enrolled {
		return nil, ErrAuthorityConfiguration
	}
	file, err := os.Open(identityPath)
	if err != nil {
		return nil, ErrAuthorityConfiguration
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o022 != 0 || info.Size() <= 0 || info.Size() > 65536 {
		return nil, ErrAuthorityConfiguration
	}
	decoder := json.NewDecoder(io.LimitReader(file, 65537))
	decoder.DisallowUnknownFields()
	var identity DomainIdentity
	if err := decoder.Decode(&identity); err != nil {
		return nil, ErrAuthorityConfiguration
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, ErrAuthorityConfiguration
	}
	var matches bool
	if err := local.QueryRow(ctx, `select witness_domain_id=$1 and witness_system_identifier=$2
		and witness_database_oid=$3 and witness_database_name=$4 and witness_identity_epoch=$5
		from public.source_authority_admission where singleton`, identity.ID, identity.SystemIdentifier,
		identity.DatabaseOID, identity.DatabaseName, identity.Epoch).Scan(&matches); err != nil || !matches {
		return nil, ErrAuthorityConfiguration
	}
	authority, err := NewAuthority(witness, identity)
	if err != nil {
		return nil, ErrAuthorityConfiguration
	}
	return authority, nil
}
