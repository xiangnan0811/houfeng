//go:build linux

package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallerReenrollmentExplicitlyReplacesExistingSession(t *testing.T) {
	for _, reenroll := range []bool{false, true} {
		name := "upgrade_preserves"
		if reenroll {
			name = "reenroll_replaces"
		}
		t.Run(name, func(t *testing.T) {
			env := newInstallerRuntimeTestEnv(t)
			root := filepath.Join(env.dir, "root")
			for _, directory := range []string{"etc/houfeng-agent", "etc/systemd/system", "var/lib/houfeng-agent", "run/systemd/system"} {
				mustMkdir(t, filepath.Join(root, directory))
			}
			tokenPath := filepath.Join(root, "etc/houfeng-agent/token")
			// A new monitoring instance can have no session history while this
			// host still holds the retired predecessor's evidence-only credential.
			previous := `{"monitoring_instance_id":"mi_retired_a","sync_token":"mas_retired_a.existing-secret"}`
			if err := os.WriteFile(tokenPath, []byte(previous), 0600); err != nil {
				t.Fatal(err)
			}
			script := strings.NewReplacer("/etc/houfeng-agent", filepath.Join(root, "etc/houfeng-agent"), "/etc/systemd/system", filepath.Join(root, "etc/systemd/system"), "/var/lib/houfeng-agent", filepath.Join(root, "var/lib/houfeng-agent"), "/run/systemd/system", filepath.Join(root, "run/systemd/system"), "/usr/local/bin/houfeng-agent", filepath.Join(root, "houfeng-agent")).Replace(Script)
			mustWriteExecutable(t, env.script, script)
			env.writeCommand(t, "minisign", "#!/bin/sh\nexit 0\n")
			env.writeCommand(t, "systemctl", `#!/bin/sh
case "$1" in
  is-active) [ ! -f "$FAKE_INSTALLER_MARKERS/stopped" ] ;;
  stop)
    grep -q '"sync_token"' "$FAKE_INSTALLER_MARKERS/../root/etc/houfeng-agent/token" || exit 33
    touch "$FAKE_INSTALLER_MARKERS/stopped"
    ;;
  start) touch "$FAKE_INSTALLER_MARKERS/started" ;;
  restart) touch "$FAKE_INSTALLER_MARKERS/restarted" ;;
esac
`)
			env.writeCommand(t, "curl", `#!/bin/sh
dest=""
url=""
while [ "$#" -gt 0 ]; do
 case "$1" in
  -o) dest="$2"; shift 2 ;;
  -*) shift ;;
  *) url="$1"; shift ;;
 esac
done
case "$url" in
 */sha256sums.txt) printf '%s  %s\n' '0000000000000000000000000000000000000000000000000000000000000000' 'houfeng-agent_v1.2.3_linux_amd64' > "$dest" ;;
 *) printf 'fixture asset' > "$dest" ;;
esac
`)
			var flags []string
			if reenroll {
				flags = []string{"--reenroll"}
			}
			output, err := env.run(t, flags...)
			if err != nil {
				t.Fatalf("installer failed: %v\n%s", err, output)
			}
			got, err := os.ReadFile(tokenPath)
			if err != nil {
				t.Fatal(err)
			}
			want := previous
			if reenroll {
				want = "enroll_secret"
			}
			if string(got) != want {
				t.Fatal("installer credential behavior differs from explicit intent")
			}
			info, err := os.Stat(tokenPath)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0600 {
				t.Fatal("credential permissions must be 0600")
			}
			if env.exists("stopped") != reenroll || env.exists("started") != reenroll || env.exists("restarted") == reenroll {
				t.Fatalf("incorrect stop/start/restart ordering: %s", output)
			}
			if strings.Contains(output, "existing-secret") || strings.Contains(output, "enroll_secret") {
				t.Fatal("installer printed a secret")
			}
			leftovers, _ := filepath.Glob(tokenPath + ".*")
			if len(leftovers) != 0 {
				t.Fatal("temporary credential file left behind")
			}
		})
	}
}
