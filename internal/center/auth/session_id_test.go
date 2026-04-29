package auth

import "testing"

func TestNewSessionIDLengthAndCharset(t *testing.T) {
	id, err := NewSessionID()
	if err != nil {
		t.Fatalf("NewSessionID: %v", err)
	}
	if len(id) != 64 {
		t.Fatalf("len(id) = %d, want 64", len(id))
	}
	for _, r := range id {
		isHex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')
		if !isHex {
			t.Fatalf("non-hex character %q in %q", r, id)
		}
	}
}

func TestNewSessionIDUnique(t *testing.T) {
	seen := make(map[string]struct{})
	for i := 0; i < 256; i++ {
		id, err := NewSessionID()
		if err != nil {
			t.Fatalf("NewSessionID: %v", err)
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate session id %q", id)
		}
		seen[id] = struct{}{}
	}
}
