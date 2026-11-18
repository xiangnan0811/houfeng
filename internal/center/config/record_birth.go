package config

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sys/unix"

	"houfeng/internal/center/platformmigrate"
)

// RecordBirthLaunchPath is the Center's protected runtime mount location.
const RecordBirthLaunchPath = "/run/houfeng/records-authority/birth-launch.json"

// RecordBirthProcessorLaunchPath selects the processor's distinct executable binding.
const RecordBirthProcessorLaunchPath = "/run/houfeng/records-authority/birth-launch-processor.json"

const (
	birthProducerDatabaseURLEnv = "HOUFENG_RECORD_BIRTH_PRODUCER_DATABASE_URL"
	birthAuthorityIdentityEnv   = "HOUFENG_RECORD_BIRTH_AUTHORITY_IDENTITY_FILE"
	birthProducerCredentialEnv  = "HOUFENG_RECORD_BIRTH_PRODUCER_CREDENTIAL_FILE"
	birthReaderDatabaseURLEnv   = "HOUFENG_RECORD_BIRTH_READER_DATABASE_URL"
	birthReaderIdentityEnv      = "HOUFENG_RECORD_BIRTH_READER_IDENTITY_FILE"
	birthReaderCredentialEnv    = "HOUFENG_RECORD_BIRTH_READER_CREDENTIAL_FILE"
	birthLaunchFileEnv          = "HOUFENG_RECORD_BIRTH_LAUNCH_FILE"
	birthCredentialLimit        = 64 * 1024
)

// BirthProducerConfig holds the separately authenticated W2 producer connection.
// A nil Connection leaves the producer unavailable; choosing the ordinary
// domain still requires a successful continuity check when applicable.
type BirthProducerConfig struct {
	Connection     *pgxpool.Config
	IdentityFile   string
	CredentialFile string
}

// BirthReaderConfig holds the independent, read-only W2 continuity connection.
// A nil Connection indicates that the reader is not configured; it does not
// establish whether birth was previously installed.
type BirthReaderConfig struct {
	Connection     *pgxpool.Config
	IdentityFile   string
	CredentialFile string
}

type birthConnectionConfig struct {
	Connection     *pgxpool.Config
	IdentityFile   string
	CredentialFile string
}

func loadBirthProducerConfig(mode RecordPlatformMode) (BirthProducerConfig, error) {
	result, err := loadBirthConnectionConfig(mode, "producer", [3]string{
		birthProducerDatabaseURLEnv, birthAuthorityIdentityEnv, birthProducerCredentialEnv,
	}, false)
	return BirthProducerConfig(result), err
}

// LoadBirthReaderConfig parses the separately authenticated continuity reader.
// All three inputs may be absent; when configured they require runtime admission.
func LoadBirthReaderConfig(mode RecordPlatformMode) (BirthReaderConfig, error) {
	result, err := loadBirthConnectionConfig(mode, "reader", [3]string{
		birthReaderDatabaseURLEnv, birthReaderIdentityEnv, birthReaderCredentialEnv,
	}, true)
	return BirthReaderConfig(result), err
}

func loadBirthConnectionConfig(mode RecordPlatformMode, kind string, keys [3]string, rejectTLSPassword bool) (birthConnectionConfig, error) {
	var result birthConnectionConfig
	var values [3]string
	present := 0
	for index, key := range keys {
		value, ok := os.LookupEnv(key)
		if ok {
			present++
		}
		values[index] = strings.TrimSpace(value)
	}
	if present == 0 {
		return result, nil
	}
	if present != len(keys) || values[0] == "" || values[1] == "" || values[2] == "" || mode != RecordPlatformModeRuntimeAdmission {
		return result, fmt.Errorf("record birth %s configuration is incomplete or records are disabled", kind)
	}

	// Keep the database password out of the environment and URL. Inject it
	// into a parsed copy only for the existing strict source-authority parser.
	parsed, err := url.Parse(values[0])
	if err != nil || parsed == nil || parsed.User == nil || !validBirthDatabaseRole(parsed.User.Username()) || strings.Contains(values[0], "#") {
		return result, fmt.Errorf("record birth %s configuration is invalid", kind)
	}
	if _, hasPassword := parsed.User.Password(); hasPassword {
		return result, fmt.Errorf("record birth %s configuration is invalid", kind)
	}
	if rejectTLSPassword {
		parameters, err := url.ParseQuery(parsed.RawQuery)
		if err != nil {
			return result, fmt.Errorf("record birth %s configuration is invalid", kind)
		}
		if _, present := parameters["sslpassword"]; present {
			return result, fmt.Errorf("record birth %s configuration is invalid", kind)
		}
	}
	password, err := readBirthCredential(values[2])
	if err != nil {
		return result, fmt.Errorf("record birth %s credential file is invalid", kind)
	}
	parsed.User = url.UserPassword(parsed.User.Username(), password)
	connection, err := platformmigrate.ParseSourceAuthorityPostgresConfig(parsed.String())
	if err != nil {
		return result, fmt.Errorf("record birth %s configuration is invalid", kind)
	}
	result.Connection = connection
	result.IdentityFile = values[1]
	result.CredentialFile = values[2]
	return result, nil
}

func loadBirthLaunchFile(mode RecordPlatformMode) (string, error) {
	path, present := os.LookupEnv(birthLaunchFileEnv)
	if !present {
		if mode == RecordPlatformModeRuntimeAdmission {
			return RecordBirthLaunchPath, nil
		}
		return "", nil
	}
	if path == "" || path != strings.TrimSpace(path) || !filepath.IsAbs(path) ||
		filepath.Clean(path) != path || strings.ContainsRune(path, '\x00') {
		return "", fmt.Errorf("%s must be a clean absolute path", birthLaunchFileEnv)
	}
	return path, nil
}

func validBirthDatabaseRole(value string) bool {
	if len(value) == 0 || len(value) > 63 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for _, ch := range value {
		if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '_' {
			return false
		}
	}
	return true
}

// A birth credential is either owned privately by this process or staged for
// the Compose runtime's UID 10001 by UID 10002 with its exact group/mode.
func readBirthCredential(path string) (string, error) {
	invalid := fmt.Errorf("invalid birth credential")
	before, err := os.Lstat(path)
	if err != nil || !privateBirthCredentialFile(before) {
		return "", invalid
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return "", invalid
	}
	file := os.NewFile(uintptr(fd), "record-birth-credential")
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !privateBirthCredentialFile(after) || !os.SameFile(before, after) {
		return "", invalid
	}
	body, err := io.ReadAll(io.LimitReader(file, birthCredentialLimit+1))
	if err != nil || len(body) == 0 || len(body) > birthCredentialLimit {
		return "", invalid
	}
	password := strings.TrimSuffix(string(body), "\n")
	if strings.TrimSpace(password) == "" || strings.ContainsAny(password, "\x00\r\n") {
		return "", invalid
	}
	return password, nil
}

func privateBirthCredentialFile(info os.FileInfo) bool {
	if info == nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > birthCredentialLimit ||
		info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	mode := info.Mode().Perm()
	if stat.Uid == uint32(os.Geteuid()) && (mode == 0o400 || mode == 0o600) {
		return true
	}
	return os.Geteuid() == 10001 && os.Getegid() == 10001 &&
		stat.Uid == 10002 && stat.Gid == 10001 && mode == 0o040
}
