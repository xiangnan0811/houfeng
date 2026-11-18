//go:build linux

package deploy

import (
	"bytes"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

type ordinaryRecipeFile struct {
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
	SHA256 string `json:"sha256"`
}

type ordinaryEnvironmentIdentity struct {
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
	SHA256 string `json:"-"`
}

func ordinaryEnvironmentMetadataMatches(a, b ordinaryEnvironmentIdentity) bool {
	return a.Device == b.Device && a.Inode == b.Inode
}

type ordinaryComposeRecipe struct {
	Compose        ordinaryRecipeFile          `json:"compose"`
	Environment    ordinaryEnvironmentIdentity `json:"environment"`
	ProxyName      string                      `json:"proxy_name"`
	Proxy          ordinaryRecipeFile          `json:"proxy"`
	OverrideSHA256 string                      `json:"override_sha256"`
}

// The fixed Compose inputs are root controlled through every rename-relevant
// ancestor. Their exact inodes and hashes must be replayed before recreation.
// The .env bytes may contain secrets and are never included in the intent.
func ordinaryComposeProxySelection(body []byte) (string, error) {
	var selected string
	for _, line := range bytes.Split(body, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		candidate := line
		if len(candidate) > len("export") && bytes.Equal(candidate[:len("export")], []byte("export")) && (candidate[len("export")] == ' ' || candidate[len("export")] == '\t') {
			candidate = bytes.TrimSpace(candidate[len("export"):])
		}
		if !bytes.HasPrefix(candidate, []byte("COMPOSE_FILE")) {
			continue
		}
		key, value, ok := bytes.Cut(candidate, []byte{'='})
		if !ok || !bytes.Equal(bytes.TrimSpace(key), []byte("COMPOSE_FILE")) || selected != "" {
			return "", errOrdinaryUpgrade
		}
		switch {
		case bytes.Equal(value, []byte("compose.yaml:compose.proxy-network.yaml")):
			selected = "compose.proxy-network.yaml"
		case bytes.Equal(value, []byte("compose.yaml:compose.proxy-host.yaml")):
			selected = "compose.proxy-host.yaml"
		default:
			return "", errOrdinaryUpgrade
		}
	}
	if selected == "" {
		return "", errOrdinaryUpgrade
	}
	return selected, nil
}

func ordinaryCaptureComposeRecipe(work string) (ordinaryComposeRecipe, error) {
	var recipe ordinaryComposeRecipe
	dirs, err := openComposeLaunchDirectories(work)
	if err != nil {
		return recipe, errOrdinaryUpgrade
	}
	defer func() {
		for _, dir := range dirs {
			_ = unix.Close(dir.fd)
		}
	}()
	for _, dir := range dirs {
		if dir.before.Uid != 0 || dir.before.Mode&0o022 != 0 || (dir.before.Mode&0o001 == 0 && (dir.before.Gid != composeLaunchRuntimeGID || dir.before.Mode&0o010 == 0)) {
			return recipe, errOrdinaryUpgrade
		}
	}
	read := func(name string, limit int64) (ordinaryRecipeFile, error) {
		var identity ordinaryRecipeFile
		parent := dirs[len(dirs)-1].fd
		fd, err := unix.Openat2(parent, name, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK, Resolve: maintenanceGuardResolve})
		if err != nil {
			return identity, errOrdinaryUpgrade
		}
		file := os.NewFile(uintptr(fd), "ordinary Compose input")
		defer file.Close()
		var before, after, named unix.Stat_t
		if unix.Fstat(fd, &before) != nil || before.Mode&unix.S_IFMT != unix.S_IFREG || before.Mode&0o022 != 0 || before.Mode&0o7000 != 0 || before.Uid != 0 || before.Nlink != 1 || before.Size <= 0 || before.Size > limit {
			return identity, errOrdinaryUpgrade
		}
		body, err := io.ReadAll(io.LimitReader(file, limit+1))
		if err != nil || len(body) != int(before.Size) || unix.Fstat(fd, &after) != nil || unix.Fstatat(parent, name, &named, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameSourceFile(before, after) || !sameSourceFile(before, named) || !composeLaunchDirectoriesUnchanged(dirs) {
			clear(body)
			return identity, errOrdinaryUpgrade
		}
		if name == ".env" {
			recipe.ProxyName, err = ordinaryComposeProxySelection(body)
			if err != nil {
				clear(body)
				return identity, errOrdinaryUpgrade
			}
		}
		identity = ordinaryRecipeFile{Device: uint64(before.Dev), Inode: before.Ino, SHA256: ordinaryDigestHex(body)}
		clear(body)
		return identity, nil
	}
	recipe.Compose, err = read("compose.yaml", 2<<20)
	if err != nil {
		return ordinaryComposeRecipe{}, err
	}
	environment, err := read(".env", 64<<10)
	recipe.Environment = ordinaryEnvironmentIdentity{Device: environment.Device, Inode: environment.Inode, SHA256: environment.SHA256}
	if err != nil {
		return ordinaryComposeRecipe{}, err
	}
	recipe.Proxy, err = read(recipe.ProxyName, 2<<20)
	if err != nil {
		return ordinaryComposeRecipe{}, err
	}
	return recipe, nil
}
