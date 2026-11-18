package recordbirth

import (
	"encoding/hex"
	"fmt"
	"maps"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

const birthLaunchRuntimeParameter = "houfeng.birth_launch_digest"

// WithRuntimeLaunch adds the validated public launch-file digest to a single
// APP connection string. It does not alter process environment, database role
// settings, or the connection settings used by independent witness pools.
func WithRuntimeLaunch(databaseURL string, digest [32]byte) (string, error) {
	if digest == ([32]byte{}) {
		return "", fmt.Errorf("%w: invalid launch connection", ErrInvalid)
	}
	original, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return "", fmt.Errorf("%w: invalid launch connection", ErrInvalid)
	}
	for name, value := range original.ConnConfig.RuntimeParams {
		if launchParameterName(name) || (strings.EqualFold(name, "options") && launchOptionPresent(value)) {
			return "", fmt.Errorf("%w: launch connection already specifies a digest", ErrInvalid)
		}
	}

	encodedDigest := hex.EncodeToString(digest[:])
	parameter := birthLaunchRuntimeParameter + "=" + encodedDigest
	var result string
	if strings.HasPrefix(databaseURL, "postgres://") || strings.HasPrefix(databaseURL, "postgresql://") {
		base, fragment := databaseURL, ""
		if index := strings.IndexByte(base, '#'); index >= 0 {
			base, fragment = base[:index], base[index:]
		}
		separator := "?"
		if strings.ContainsRune(base, '?') {
			separator = "&"
		}
		result = base + separator + parameter + fragment
	} else {
		// A prefix keeps a final empty keyword value in the original DSN empty.
		result = parameter + " " + databaseURL
	}

	// ConnString returns pgx's original input, not a serialization of mutated
	// RuntimeParams. Reparse the actual result and check the intended change.
	parsed, err := pgxpool.ParseConfig(result)
	if err != nil {
		return "", fmt.Errorf("%w: invalid launch connection", ErrInvalid)
	}
	expected := original.Copy()
	expected.ConnConfig.RuntimeParams[birthLaunchRuntimeParameter] = encodedDigest
	if !sameLaunchConnectionSettings(expected, parsed) {
		return "", fmt.Errorf("%w: launch connection changed settings", ErrInvalid)
	}
	return result, nil
}

func sameLaunchConnectionSettings(a, b *pgxpool.Config) bool {
	ac, bc := a.ConnConfig, b.ConnConfig
	return ac.Host == bc.Host && ac.Port == bc.Port && ac.Database == bc.Database &&
		ac.User == bc.User && ac.Password == bc.Password && ac.ConnectTimeout == bc.ConnectTimeout &&
		ac.KerberosSrvName == bc.KerberosSrvName && ac.KerberosSpn == bc.KerberosSpn &&
		ac.SSLNegotiation == bc.SSLNegotiation && ac.ChannelBinding == bc.ChannelBinding &&
		ac.MinProtocolVersion == bc.MinProtocolVersion && ac.MaxProtocolVersion == bc.MaxProtocolVersion &&
		(ac.TLSConfig == nil) == (bc.TLSConfig == nil) &&
		ac.StatementCacheCapacity == bc.StatementCacheCapacity && ac.DescriptionCacheCapacity == bc.DescriptionCacheCapacity &&
		ac.DefaultQueryExecMode == bc.DefaultQueryExecMode && maps.Equal(ac.RuntimeParams, bc.RuntimeParams) &&
		a.MaxConnLifetime == b.MaxConnLifetime && a.MaxConnLifetimeJitter == b.MaxConnLifetimeJitter &&
		a.MaxConnIdleTime == b.MaxConnIdleTime && a.PingTimeout == b.PingTimeout &&
		a.MaxConns == b.MaxConns && a.MinConns == b.MinConns && a.MinIdleConns == b.MinIdleConns &&
		a.HealthCheckPeriod == b.HealthCheckPeriod
}

func launchParameterName(name string) bool {
	return strings.EqualFold(strings.ReplaceAll(name, "-", "_"), birthLaunchRuntimeParameter)
}

// PostgreSQL splits the startup options string on unescaped ASCII whitespace,
// then accepts both -c name=value and --name=value. Inspect the parsed options
// so an ambient PGOPTIONS or a DSN options entry cannot preload this parameter.
func launchOptionPresent(options string) bool {
	var words []string
	for index := 0; index < len(options); {
		for index < len(options) && launchOptionSpace(options[index]) {
			index++
		}
		if index == len(options) {
			break
		}
		var word strings.Builder
		for index < len(options) && !launchOptionSpace(options[index]) {
			if options[index] == '\\' && index+1 < len(options) {
				index++
			}
			word.WriteByte(options[index])
			index++
		}
		words = append(words, word.String())
	}
	for index, word := range words {
		var argument string
		if strings.HasPrefix(word, "--") {
			argument = word[2:]
		} else if len(word) > 1 && word[0] == '-' {
			// A -c option can be grouped with preceding short options.
			if position := strings.IndexByte(word[1:], 'c'); position >= 0 {
				argument = word[position+2:]
				if argument == "" && index+1 < len(words) {
					argument = words[index+1]
				}
			}
		}
		name, _, _ := strings.Cut(argument, "=")
		if launchParameterName(name) {
			return true
		}
	}
	return false
}

func launchOptionSpace(ch byte) bool {
	switch ch {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	default:
		return false
	}
}
