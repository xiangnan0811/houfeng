//go:build linux

package deploy

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/subtle"
	"io"
	"os/exec"
	"strings"
)

// Pin the old Center's real configured session secret before any fence effect.
// Docker's archive is read in memory and never written to logs or journals.
func ordinaryCaptureSessionHMAC(ctx context.Context, request ordinaryUpgradeRequest, journal fenceJournal) (string, string, error) {
	var original fenceContainer
	for _, container := range journal.Containers {
		if container.Service == "houfeng" {
			if original.ID != "" {
				return "", "", errOrdinaryUpgrade
			}
			original = container
		}
	}
	if original.ID == "" || !fenceID.MatchString(original.ID) || original.Project != request.Project || original.WorkingDirectory != request.WorkingDirectory {
		return "", "", errOrdinaryUpgrade
	}
	before, err := fenceInspect(ctx, original.ID)
	if err != nil || before.state != "running" || !fenceSameAnchors(original, before.anchor) || !fenceAllowedNetworks(original, before, "", false) {
		return "", "", errOrdinaryUpgrade
	}
	// Emit only a marker, never Config.Env or any environment values.
	const template = `{{range .Config.Env}}{{if eq (index (split . "=") 0) "HOUFENG_SESSION_HMAC_KEY_FILE"}}{{if eq . "HOUFENG_SESSION_HMAC_KEY_FILE=/run/secrets/houfeng_session_hmac_key"}}F{{else}}X{{end}}{{end}}{{end}}`
	marker, err := fenceDocker(ctx, "inspect", "--format", template, original.ID)
	if err != nil || strings.TrimSpace(string(marker)) != "F" {
		return "", "", errOrdinaryUpgrade
	}
	candidate, _, err := readOrdinaryPrivateIdentity(request.Secrets.SessionHMACKey, 64<<10)
	if err != nil {
		return "", "", errOrdinaryUpgrade
	}
	defer clear(candidate)
	command := exec.CommandContext(ctx, "/usr/bin/docker", "--host", "unix:///var/run/docker.sock", "cp", original.ID+":/run/secrets/houfeng_session_hmac_key", "-")
	command.Env = []string{"PATH=/usr/bin:/bin", "HOME=/nonexistent", "DOCKER_CONFIG=/nonexistent"}
	var stderr fenceLimitedBuffer
	command.Stderr = &stderr
	output, err := command.StdoutPipe()
	if err != nil || command.Start() != nil {
		return "", "", errOrdinaryUpgrade
	}
	archive := tar.NewReader(output)
	header, readErr := archive.Next()
	if readErr == nil && (header.Name != "houfeng_session_hmac_key" || (header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA) || header.Linkname != "" || header.Size <= 0 || header.Size > 64<<10 || header.Mode&0o6000 != 0) {
		readErr = errOrdinaryUpgrade
	}
	var old []byte
	if readErr == nil {
		old, readErr = io.ReadAll(io.LimitReader(archive, header.Size+1))
		if readErr == nil && len(old) != int(header.Size) {
			readErr = errOrdinaryUpgrade
		}
	}
	if readErr == nil {
		_, readErr = archive.Next()
		if readErr == io.EOF {
			readErr = nil
		} else if readErr == nil {
			readErr = errOrdinaryUpgrade
		}
	}
	if readErr == nil {
		padding, err := io.ReadAll(io.LimitReader(output, (1<<20)+1))
		if err != nil || len(padding) > 1<<20 || len(bytes.Trim(padding, "\x00")) != 0 {
			readErr = errOrdinaryUpgrade
		}
		clear(padding)
	}
	_ = output.Close()
	waitErr := command.Wait()
	defer clear(old)
	if readErr != nil || waitErr != nil || stderr.overflow {
		return "", "", errOrdinaryUpgrade
	}
	oldCanonical, newCanonical := bytes.TrimSpace(old), bytes.TrimSpace(candidate)
	if len(oldCanonical) < 32 || len(newCanonical) != len(oldCanonical) || subtle.ConstantTimeCompare(oldCanonical, newCanonical) != 1 {
		return "", "", errOrdinaryUpgrade
	}
	after, err := fenceInspect(ctx, original.ID)
	if err != nil || after.state != "running" || !fenceSameAnchors(original, after.anchor) || !fenceAllowedNetworks(original, after, "", false) {
		return "", "", errOrdinaryUpgrade
	}
	return original.ID, ordinaryDigestHex(oldCanonical), nil
}
