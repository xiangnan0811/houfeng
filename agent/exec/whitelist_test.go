package exec

import (
	"reflect"
	"testing"

	"houfeng/internal/contracts/agentapi"
)

func TestWhitelistStableCommandDefinitions(t *testing.T) {
	want := map[string]CommandDef{
		"df_h":             {Bin: "df", Args: []string{"-h"}},
		"free_m":           {Bin: "free", Args: []string{"-m"}},
		"uptime":           {Bin: "uptime", Args: nil},
		"top_head":         {Bin: "top", Args: []string{"-bn1"}},
		"journalctl_u":     {Bin: "journalctl", Args: []string{"--lines=50"}},
		"systemctl_status": {Bin: "systemctl", Args: []string{"status"}},
		"dmesg_err":        {Bin: "dmesg", Args: []string{"--level=err"}},
		"docker_ps":        {Bin: "docker", Args: []string{"ps"}},
	}

	if len(whitelist) != len(want) {
		t.Fatalf("len(whitelist) = %d, want %d", len(whitelist), len(want))
	}
	for id, wantDef := range want {
		t.Run(id, func(t *testing.T) {
			bin, args, ok := Lookup(id)
			if !ok {
				t.Fatalf("Lookup(%q) ok = false, want true", id)
			}
			if bin != wantDef.Bin {
				t.Fatalf("Lookup(%q) bin = %q, want %q", id, bin, wantDef.Bin)
			}
			if !reflect.DeepEqual(args, wantDef.Args) {
				t.Fatalf("Lookup(%q) args = %#v, want %#v", id, args, wantDef.Args)
			}
			if !agentapi.IsKnownCommandID(id) {
				t.Fatalf("agentapi.IsKnownCommandID(%q) = false, want center catalog to include agent whitelist command", id)
			}
		})
	}
}

func TestLookupRejectsUnknownCommandIDs(t *testing.T) {
	for _, id := range []string{"nonexistent", "", "sh", "sh -c uptime"} {
		t.Run(id, func(t *testing.T) {
			bin, args, ok := Lookup(id)
			if ok {
				t.Fatalf("Lookup(%q) ok = true, want false with bin=%q args=%#v", id, bin, args)
			}
			if bin != "" || args != nil {
				t.Fatalf("Lookup(%q) = %q, %#v, want zero values", id, bin, args)
			}
		})
	}
}

func TestLookupReturnsDefensiveArgsCopy(t *testing.T) {
	_, args, ok := Lookup("docker_ps")
	if !ok {
		t.Fatal("docker_ps not found in whitelist")
	}
	if len(args) != 1 {
		t.Fatalf("docker_ps args = %#v, want one arg", args)
	}

	args[0] = "rm"

	_, argsAgain, ok := Lookup("docker_ps")
	if !ok {
		t.Fatal("docker_ps not found in whitelist after mutation attempt")
	}
	if !reflect.DeepEqual(argsAgain, []string{"ps"}) {
		t.Fatalf("docker_ps args after caller mutation = %#v, want [ps]", argsAgain)
	}
}
