package exec

import "testing"

func TestLookupReturnsCorrectCommand(t *testing.T) {
	tests := []struct {
		id      string
		wantBin string
		wantOK  bool
	}{
		{id: "df_h", wantBin: "df", wantOK: true},
		{id: "free_m", wantBin: "free", wantOK: true},
		{id: "uptime", wantBin: "uptime", wantOK: true},
		{id: "top_head", wantBin: "top", wantOK: true},
		{id: "journalctl_u", wantBin: "journalctl", wantOK: true},
		{id: "systemctl_status", wantBin: "systemctl", wantOK: true},
		{id: "dmesg_err", wantBin: "dmesg", wantOK: true},
		{id: "docker_ps", wantBin: "docker", wantOK: true},
		{id: "nonexistent", wantOK: false},
		{id: "", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			bin, args, ok := Lookup(tt.id)
			if ok != tt.wantOK {
				t.Fatalf("Lookup(%q) ok = %v, want %v", tt.id, ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if bin != tt.wantBin {
				t.Fatalf("Lookup(%q) bin = %q, want %q", tt.id, bin, tt.wantBin)
			}
			// args slice is tested for the ones that have specific args.
			if (args == nil) != (tt.id == "uptime") {
				t.Fatalf("Lookup(%q) args = %v, nil-ness unexpected", tt.id, args)
			}
		})
	}
}

func TestLookupDfHArgsAreCorrect(t *testing.T) {
	_, args, ok := Lookup("df_h")
	if !ok {
		t.Fatal("df_h not found in whitelist")
	}
	if len(args) != 1 || args[0] != "-h" {
		t.Fatalf("df_h args = %v, want [-h]", args)
	}
}

func TestWhitelistContainsAllEight(t *testing.T) {
	ids := []string{"df_h", "free_m", "uptime", "top_head", "journalctl_u", "systemctl_status", "dmesg_err", "docker_ps"}
	for _, id := range ids {
		_, _, ok := Lookup(id)
		if !ok {
			t.Fatalf("whitelist missing command_id %q", id)
		}
	}
}
