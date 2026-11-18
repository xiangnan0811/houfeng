package platformmigrate

import (
	"errors"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

var errInvalidSourceAuthorityConfiguration = errors.New("source authority provisioning configuration is invalid")

// ParseSourceAuthorityPostgresConfig parses an explicit source authority URL
// without accepting ambient PostgreSQL settings or home-directory defaults.
// Configuration failures are opaque and do not disclose connection secrets.
func ParseSourceAuthorityPostgresConfig(databaseURL string) (*pgxpool.Config, error) {
	// pgx merges environment and home-directory defaults before URL settings.
	// Refuse ambient connection overrides rather than mutating process-wide env.
	for _, entry := range os.Environ() {
		name, value, found := strings.Cut(entry, "=")
		if found && value != "" && (strings.HasPrefix(name, "PG") || name == "SSL_CERT_FILE" || name == "SSL_CERT_DIR") {
			return nil, errInvalidSourceAuthorityConfiguration
		}
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil || parsed == nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.Opaque != "" || strings.Contains(databaseURL, "#") || parsed.User == nil {
		return nil, errInvalidSourceAuthorityConfiguration
	}
	user := parsed.User.Username()
	password, hasPassword := parsed.User.Password()
	if user == "" || !hasPassword || password == "" || strings.ContainsRune(user+password, '\x00') {
		return nil, errInvalidSourceAuthorityConfiguration
	}
	if parsed.Hostname() == "" || parsed.Port() == "" || strings.ContainsAny(parsed.Hostname(), ",/\\\x00") {
		return nil, errInvalidSourceAuthorityConfiguration
	}
	port, err := strconv.ParseUint(parsed.Port(), 10, 16)
	if err != nil || port == 0 {
		return nil, errInvalidSourceAuthorityConfiguration
	}
	database := strings.TrimPrefix(parsed.Path, "/")
	if database == "" || strings.ContainsAny(database, "/\x00") {
		return nil, errInvalidSourceAuthorityConfiguration
	}
	parameters, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return nil, errInvalidSourceAuthorityConfiguration
	}
	for name, values := range parameters {
		if len(values) != 1 || strings.ContainsRune(values[0], '\x00') {
			return nil, errInvalidSourceAuthorityConfiguration
		}
		switch name {
		case "sslmode", "sslrootcert", "sslcert", "sslkey", "sslpassword", "sslsni", "sslnegotiation":
		default:
			// In particular, never accept service/passfile, identity/endpoint
			// overrides, options, role, or arbitrary startup SET parameters.
			return nil, errInvalidSourceAuthorityConfiguration
		}
	}
	switch parameters.Get("sslmode") {
	case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
	default:
		return nil, errInvalidSourceAuthorityConfiguration
	}
	// Empty explicit file settings suppress pgx's ~/.pgpass and ~/.postgresql
	// defaults. Deliberately supplied TLS files remain usable, including a CA
	// and client certificate for verified TLS; never replace TLS with plaintext.
	parameters.Set("passfile", "")
	parameters.Set("servicefile", "")
	for _, name := range []string{"sslrootcert", "sslcert", "sslkey", "sslpassword"} {
		if _, present := parameters[name]; !present {
			parameters.Set(name, "")
		}
	}
	parsed.RawQuery = parameters.Encode()
	config, err := pgxpool.ParseConfig(parsed.String())
	if err != nil {
		return nil, errInvalidSourceAuthorityConfiguration
	}
	// Keep the validated identity explicit. No runtime parameter (especially
	// role or options) may turn this into a SET ROLE bootstrap connection.
	config.ConnConfig.User = user
	config.ConnConfig.Password = password
	config.ConnConfig.Host = parsed.Hostname()
	config.ConnConfig.Port = uint16(port)
	config.ConnConfig.Database = database
	if len(config.ConnConfig.RuntimeParams) != 0 {
		return nil, errInvalidSourceAuthorityConfiguration
	}
	return config, nil
}
