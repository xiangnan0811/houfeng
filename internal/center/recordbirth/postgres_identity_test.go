package recordbirth

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestConfiguredPostgresAuthorityIdentityFile(t *testing.T) {
	identity := postgresPhysicalIdentity{
		ID: "rd-" + strings.Repeat("a", 64), SystemIdentifier: "123456",
		DatabaseOID: 123, DatabaseName: "witness_db", Epoch: 1,
	}
	valid, err := json.Marshal(identity)
	if err != nil {
		t.Fatal(err)
	}
	// Empty pools cannot connect; successful construction proves it performs no
	// network operation. They are never used for a command in this test.
	producer, application := &pgxpool.Pool{}, &pgxpool.Pool{}
	tests := []struct {
		name string
		data string
		mode os.FileMode
		want bool
	}{
		{"valid", string(valid), 0o600, true},
		{"group readable", string(valid), 0o640, true},
		{"group writable", string(valid), 0o620, false},
		{"duplicate identity", strings.TrimSuffix(string(valid), "}") + `,"id":"` + identity.ID + `"}`, 0o600, false},
		{"unknown field", strings.TrimSuffix(string(valid), "}") + `,"extra":true}`, 0o600, false},
		{"second object", string(valid) + `{}`, 0o600, false},
		{"missing field", `{"ID":"` + identity.ID + `"}`, 0o600, false},
		{"noncanonical system", strings.Replace(string(valid), `"123456"`, `"0123456"`, 1), 0o600, false},
		{"zero epoch", strings.Replace(string(valid), `"Epoch":1`, `"Epoch":0`, 1), 0o600, false},
		{"invalid name", strings.Replace(string(valid), `"witness_db"`, `"Witness"`, 1), 0o600, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "identity.json")
			if err := os.WriteFile(path, []byte(tc.data), tc.mode); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, tc.mode); err != nil {
				t.Fatal(err)
			}
			_, err := NewConfiguredPostgresAuthority(producer, application, path)
			if (err == nil) != tc.want {
				t.Fatalf("construction error = %v, want success %v", err, tc.want)
			}
		})
	}
	if _, err := NewConfiguredPostgresAuthority(nil, application, ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil producer = %v", err)
	}
	if _, err := NewConfiguredPostgresAuthority(producer, nil, ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil application = %v", err)
	}
}
